// Package mcp implements the stdio JSON-RPC MCP server exposing reversible
// retrieval, ad-hoc compression and the savings ledger.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"mince/internal/compression/mincer"
	"mince/internal/compression/router"
	"mince/internal/felix"
	"mince/internal/pq"
	"mince/internal/storage/ccr"
)

const protocolVersion = "2024-11-05"

// Server holds the MCP session ledger.
type Server struct {
	store *ccr.Store
	mu    sync.Mutex
	calls int
	raw   int64
	out   int64
}

// New builds an MCP server over store.
func New(store *ccr.Store) *Server { return &Server{store: store} }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve processes newline-delimited JSON-RPC until EOF.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			// Notification: no response, but still handle known methods.
			if req.Method == "notifications/initialized" {
				continue
			}
			continue
		}
		resp := s.dispatch(req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) dispatch(req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mince", "version": "0.2.0"},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefs()}
	case "tools/call":
		res, err := s.callTool(req.Params)
		if err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
		} else {
			resp.Result = res
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "mince_retrieve",
			"description": "Return exact raw lines from a mince CCR id. Optional regex/substring query and line offset/limit.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "ccr_<id> or bare id"},
					"query":  map[string]any{"type": "string", "description": "regexp or literal substring filter"},
					"offset": map[string]any{"type": "integer", "minimum": 0},
					"limit":  map[string]any{"type": "integer", "minimum": 0},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "mince_compress",
			"description": "Compress arbitrary text/JSON/log/code locally and return the minced text plus a reversible ccr id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"kind":    map[string]any{"type": "string", "enum": []string{"auto", "json", "log", "code"}},
				},
				"required": []string{"content"},
			},
		},
		{
			"name":        "mince_stats",
			"description": "Return session and store savings as exact p/q ratios.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "felix_eval",
			"description": "Compile and run a FELIX source with fn main() and return its value.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"source": map[string]any{"type": "string"}},
				"required":   []string{"source"},
			},
		},
	}
}

func (s *Server) callTool(params json.RawMessage) (any, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	switch p.Name {
	case "mince_compress":
		var args struct {
			Content string `json:"content"`
			Kind    string `json:"kind"`
		}
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if args.Content == "" {
			return nil, fmt.Errorf("content is required")
		}
		kind := router.KindText
		if args.Kind != "" && args.Kind != "auto" {
			kind = router.ParseKind(args.Kind)
		}
		res := mincer.Mince(context.Background(), []byte(args.Content), mincer.Options{
			Kind:     kind,
			MinBytes: 1,
			Store:    s.store,
		})
		s.mu.Lock()
		s.calls++
		s.raw += int64(res.RawBytes)
		s.out += int64(res.MincedBytes)
		s.mu.Unlock()
		out := map[string]any{
			"minced":        string(res.Minced),
			"ccr_id":        res.CCRID,
			"saved":         res.Saved.String(),
			"saved_percent": res.Saved.Percent(),
			"kind":          res.Kind.String(),
			"passthrough":   res.Passthrough,
		}
		return textResult(out, false), nil
	case "mince_retrieve":
		var args struct {
			ID     string `json:"id"`
			Query  string `json:"query"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if args.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		id := strings.TrimPrefix(args.ID, "ccr_")
		text, err := s.store.Retrieve(id, args.Query, args.Offset, args.Limit)
		if err != nil {
			return textResult(map[string]any{"error": err.Error()}, true), nil
		}
		return textResult(map[string]any{"id": "ccr_" + id, "content": text}, false), nil
	case "felix_eval":
		var args struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if args.Source == "" {
			return nil, fmt.Errorf("source is required")
		}
		prog, err := felix.Compile(args.Source)
		if err != nil {
			return textResult(map[string]any{"error": err.Error()}, true), nil
		}
		v, err := prog.Call("main", nil)
		if err != nil {
			return textResult(map[string]any{"error": err.Error()}, true), nil
		}
		return textResult(map[string]any{"value": v.String(prog)}, false), nil
	case "mince_stats":
		s.mu.Lock()
		calls, raw, out := s.calls, s.raw, s.out
		s.mu.Unlock()
		hits, misses, puts, entries := s.store.Stats()
		ratio := pq.FromRatio(raw-out, raw)
		return textResult(map[string]any{
			"calls":         calls,
			"raw_bytes":     raw,
			"out_bytes":     out,
			"saved":         ratio.String(),
			"saved_percent": ratio.Percent(),
			"ccr_hits":      hits,
			"ccr_misses":    misses,
			"ccr_puts":      puts,
			"ccr_entries":   entries,
		}, false), nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}
}

func textResult(v any, isError bool) map[string]any {
	b, _ := json.MarshalIndent(v, "", "  ")
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(b)}},
		"isError": isError,
	}
}
