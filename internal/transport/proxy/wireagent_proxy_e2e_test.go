package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mince/internal/storage/ccr"
	"mince/internal/transport/wireagent"
)

func TestWireAgentsInlineRetrieveAndKVPrefix(t *testing.T) {
	var gotBody []byte
	var gotKind string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotKind = r.Header.Get("X-Mince-Kind")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := ccr.NewMemory()
	id, err := store.Put([]byte("keep\nRETRIEVED-RAW"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(Config{
		OpenAIUpstream:  upstream.URL,
		MinBytes:        100,
		ProtectTail:     1,
		CompressTimeout: time.Second,
		Store:           store,
		WireAgents:      wireagent.NewPipeline(&wireagent.TierShuntAgent{}, &wireagent.InlineCCRAgent{Store: store}),
	}).Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader("RETRIEVED"))
	req.Header.Set("X-Mince-Inline-Tool", "mince_retrieve")
	req.Header.Set("X-Mince-CCR", id)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if string(gotBody) != "RETRIEVED-RAW" {
		t.Fatalf("inline body %q", gotBody)
	}
	if gotKind == "" {
		t.Fatal("tier shunt did not set X-Mince-Kind")
	}

	system := "SYSTEM-PROMPT-BYTE-IDENTICAL-123"
	logJSON, _ := json.Marshal(bigLog())
	body := []byte(`{"model":"gpt","messages":[{"role":"system","content":"` + system + `"},{"role":"tool","content":` + string(logJSON) + `},{"role":"user","content":"tail"}]}`)
	resp2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	_, _ = io.ReadAll(resp2.Body)
	if !bytes.Contains(gotBody, []byte(system)) {
		t.Fatal("system KV prefix changed")
	}
}
