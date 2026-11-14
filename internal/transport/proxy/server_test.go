package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mince/internal/storage/ccr"
)

func bigLog() string {
	return strings.Repeat("[12:00:00] compiling module: ok\n", 300) + "panic: boom\n"
}

func TestProxyStreamsAndMinces(t *testing.T) {
	var gotBody []byte
	var gotStatus string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotStatus = r.Header.Get("X-Mince-Status")
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, chunk := range []string{"data: one\n\n", "data: two\n\n", "data: [DONE]\n\n"} {
			fmt.Fprint(w, chunk)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer upstream.Close()

	store := ccr.NewMemory()
	srv := httptest.NewServer(New(Config{
		OpenAIUpstream:  upstream.URL,
		MinBytes:        100,
		ProtectTail:     1,
		CompressTimeout: 2 * time.Second,
		Store:           store,
	}).Handler())
	defer srv.Close()

	logJSON, _ := json.Marshal(bigLog())
	body := []byte(`{"model":"gpt","messages":[` +
		`{"role":"system","content":"SYS"},` +
		`{"role":"tool","tool_call_id":"c1","content":` + string(logJSON) + `},` +
		`{"role":"user","content":"tail"}]}`)

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	streamed, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(streamed), "data: one") || !strings.Contains(string(streamed), "data: [DONE]") {
		t.Fatalf("stream not passed through: %q", streamed)
	}
	if gotStatus != "minced" {
		t.Fatalf("upstream status %q", gotStatus)
	}
	if len(gotBody) >= len(body) {
		t.Fatalf("upstream body not compressed: %d >= %d", len(gotBody), len(body))
	}
	if !strings.Contains(string(gotBody), "mince:ccr") {
		t.Fatal("compressed body lacks footer")
	}

	// Fail-open on malformed JSON.
	bad := []byte(`{"messages":`)
	resp2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	_, _ = io.ReadAll(resp2.Body)
	if gotStatus != "passthrough" {
		t.Fatalf("fail-open status %q", gotStatus)
	}
	if !bytes.Equal(gotBody, bad) {
		t.Fatal("malformed body was not forwarded verbatim")
	}

	// Ledger.
	st, err := http.Get(srv.URL + "/_mince/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Body.Close()
	var ledger map[string]any
	if err := json.NewDecoder(st.Body).Decode(&ledger); err != nil {
		t.Fatal(err)
	}
	if ledger["requests"].(float64) < 2 {
		t.Fatalf("requests %v", ledger["requests"])
	}
	if ledger["saved_bytes"].(float64) <= 0 {
		t.Fatalf("saved %v", ledger["saved_bytes"])
	}
}

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(New(Config{Store: ccr.NewMemory()}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/_mince/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestProtectTailZeroIsValid(t *testing.T) {
	s := New(Config{ProtectTail: 0})
	if s.cfg.ProtectTail != 0 {
		t.Fatalf("ProtectTail 0 was overridden to %d", s.cfg.ProtectTail)
	}
	if got := New(Config{ProtectTail: -1}).cfg.ProtectTail; got != 2 {
		t.Fatalf("ProtectTail sentinel -1 = %d, want 2", got)
	}
}
