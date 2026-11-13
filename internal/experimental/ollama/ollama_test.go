package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatPinsOptions(t *testing.T) {
	gotCh := make(chan chatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path %s", r.URL.Path)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		gotCh <- req
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hel"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"lo"},"done":true}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "gemma4:12b-nvfp4")
	text, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("text %q", text)
	}
	got := <-gotCh
	if got.Stream || got.KeepAlive != -1 || got.Think {
		t.Fatalf("bad options: %+v", got)
	}
	if string(got.Format) != `{"type":"object"}` {
		t.Fatalf("format %s", got.Format)
	}
}

func TestChatErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(srv.URL, "x")
	if _, err := c.Chat(context.Background(), nil, nil); err == nil {
		t.Fatal("error status accepted")
	}
}
