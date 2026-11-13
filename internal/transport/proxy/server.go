// Package proxy is the local fail-open compression proxy for Anthropic and
// OpenAI-compatible APIs. Streaming responses pass through unbuffered.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"mince/internal/compression/aligner"
	"mince/internal/config"
	"mince/internal/pq"
	"mince/internal/storage/ccr"
	"mince/internal/transport/wireagent"
)

// Config configures the proxy.
type Config struct {
	Addr              string
	AnthropicUpstream string
	OpenAIUpstream    string
	MinBytes          int
	ProtectTail       int
	Store             *ccr.Store
	Logger            *slog.Logger
	CompressTimeout   time.Duration
	WireAgents        *wireagent.Pipeline
}

// Stats is the in-process savings ledger.
type Stats struct {
	mu          sync.Mutex
	Requests    uint64
	Minced      uint64
	Passthrough uint64
	Failed      uint64
	RawBytes    int64
	OutBytes    int64
	latencies   []int64
}

// Server is the HTTP proxy.
type Server struct {
	cfg    Config
	client *http.Client
	stats  *Stats
}

// New builds a proxy server with defaults applied.
func New(cfg Config) *Server {
	defaults := config.DefaultProxySettings()
	if cfg.Addr == "" {
		cfg.Addr = defaults.Addr
	}
	if cfg.AnthropicUpstream == "" {
		cfg.AnthropicUpstream = defaults.AnthropicUpstream
	}
	if cfg.OpenAIUpstream == "" {
		cfg.OpenAIUpstream = defaults.OpenAIUpstream
	}
	if cfg.MinBytes <= 0 {
		cfg.MinBytes = defaults.MinBytes
	}
	if cfg.ProtectTail < 0 {
		cfg.ProtectTail = 2
	}
	if cfg.CompressTimeout <= 0 {
		cfg.CompressTimeout = defaults.CompressTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		cfg: cfg,
		client: &http.Client{
			Transport: &http.Transport{
				DisableCompression: true,
				ForceAttemptHTTP2:  true,
				Proxy:              http.ProxyFromEnvironment,
			},
		},
		stats: &Stats{},
	}
}

// Stats exposes the ledger.
func (s *Server) Stats() *Stats { return s.stats }

// Handler returns the proxy mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_mince/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": "0.2.0"})
	})
	mux.HandleFunc("/_mince/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.stats.Snapshot(s.cfg.Store))
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		s.handle(w, r, "anthropic")
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		s.handle(w, r, "openai")
	})
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		s.handle(w, r, "openai")
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		s.handle(w, r, "openai")
	})
	return mux
}

// Start listens on the configured address.
func (s *Server) Start() (net.Listener, error) {
	return net.Listen("tcp", s.cfg.Addr)
}

// Serve serves until the listener closes.
func (s *Server) Serve(l net.Listener) error {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	err := srv.Serve(l)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request, provider string) {
	start := time.Now()
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body []byte
	var err error
	if r.Method == http.MethodPost {
		body, err = io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
	}

	aligned := body
	status := "passthrough"
	if len(body) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.CompressTimeout)
		type result struct {
			out []byte
			err error
		}
		ch := make(chan result, 1)
		go func() {
			out, _, err := aligner.Align(ctx, body, aligner.Options{
				MinBytes:         s.cfg.MinBytes,
				ProtectTailTurns: s.cfg.ProtectTail,
				Store:            s.cfg.Store,
			})
			ch <- result{out, err}
		}()
		select {
		case res := <-ch:
			if res.err == nil && len(res.out) > 0 && len(res.out) < len(body) {
				aligned = res.out
				status = "minced"
			}
		case <-ctx.Done():
			status = "passthrough"
		}
		cancel()
	}

	if s.cfg.WireAgents != nil {
		wc := &wireagent.Context{Body: aligned, Headers: r.Header.Clone()}
		if werr := s.cfg.WireAgents.Run(wc); werr == nil && len(wc.Body) > 0 {
			aligned = wc.Body
			for k, vv := range wc.Headers {
				for _, v := range vv {
					r.Header.Set(k, v)
				}
			}
		}
	}

	base := s.cfg.OpenAIUpstream
	if provider == "anthropic" {
		base = s.cfg.AnthropicUpstream
	}
	target := strings.TrimRight(base, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	var reader io.Reader
	if r.Method == http.MethodPost {
		reader = bytes.NewReader(aligned)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, reader)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusBadGateway)
		return
	}
	copyHeaders(req.Header, r.Header)
	req.Header.Set("X-Mince-Status", status)

	resp, err := s.client.Do(req)
	if err != nil {
		s.stats.record(status, len(body), len(aligned), time.Since(start), true)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Mince-Status", status)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	s.stats.record(status, len(body), len(aligned), time.Since(start), false)
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		switch strings.ToLower(k) {
		case "connection", "proxy-connection", "keep-alive", "transfer-encoding", "upgrade", "host", "content-length":
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Stats) record(status string, raw, out int, d time.Duration, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests++
	s.RawBytes += int64(raw)
	s.OutBytes += int64(out)
	s.latencies = append(s.latencies, d.Microseconds())
	if failed {
		s.Failed++
		return
	}
	if status == "minced" {
		s.Minced++
	} else {
		s.Passthrough++
	}
}

// Snapshot renders the ledger with exact rational totals.
func (s *Stats) Snapshot(store *ccr.Store) map[string]any {
	s.mu.Lock()
	requests := s.Requests
	minced := s.Minced
	passthrough := s.Passthrough
	failed := s.Failed
	raw := s.RawBytes
	out := s.OutBytes
	lats := append([]int64(nil), s.latencies...)
	s.mu.Unlock()

	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	p50, p99 := int64(0), int64(0)
	if len(lats) > 0 {
		p50 = lats[len(lats)*50/100]
		p99 = lats[len(lats)*99/100]
	}
	saved := raw - out
	ratio := pq.FromRatio(saved, raw)
	resp := map[string]any{
		"version":       "0.1.0",
		"requests":      requests,
		"minced":        minced,
		"passthrough":   passthrough,
		"failed":        failed,
		"raw_bytes":     raw,
		"out_bytes":     out,
		"saved_bytes":   saved,
		"saved":         ratio.String(),
		"saved_percent": ratio.Percent(),
		"p50_us":        p50,
		"p99_us":        p99,
	}
	if store != nil {
		hits, misses, puts, entries := store.Stats()
		resp["ccr_hits"] = hits
		resp["ccr_misses"] = misses
		resp["ccr_puts"] = puts
		resp["ccr_entries"] = entries
	}
	return resp
}
