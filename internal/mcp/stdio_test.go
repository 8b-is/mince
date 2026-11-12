package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"mince/internal/ccr"
)

func bigLog() string {
	return strings.Repeat("[12:00:00] compiling module: ok\n", 300) + "panic: boom\n"
}

func TestStdioCompressRetrieve(t *testing.T) {
	store := ccr.NewMemory()
	srv := New(store)

	payload := bigLog()
	args, _ := json.Marshal(map[string]any{
		"name":      "mince_compress",
		"arguments": map[string]any{"content": payload, "kind": "log"},
	})
	var in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":` + string(args) + `}` + "\n")

	var out bytes.Buffer
	if err := srv.Serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses %d: %s", len(lines), out.String())
	}

	var initResp struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatal(err)
	}
	if initResp.Result.ServerInfo.Name != "mince" {
		t.Fatalf("server name %q", initResp.Result.ServerInfo.Name)
	}

	var listResp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Result.Tools) != 4 {
		t.Fatalf("tools %d", len(listResp.Result.Tools))
	}

	var callResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &callResp); err != nil {
		t.Fatal(err)
	}
	if callResp.Result.IsError {
		t.Fatalf("compress failed: %s", callResp.Result.Content[0].Text)
	}
	var compressed struct {
		Minced  string `json:"minced"`
		CCRID   string `json:"ccr_id"`
		Saved   string `json:"saved"`
		Percent string `json:"saved_percent"`
	}
	if err := json.Unmarshal([]byte(callResp.Result.Content[0].Text), &compressed); err != nil {
		t.Fatal(err)
	}
	if compressed.CCRID == "" {
		t.Fatal("no ccr id")
	}
	if !strings.Contains(compressed.Minced, "panic: boom") {
		t.Fatal("panic lost")
	}

	retArgs, _ := json.Marshal(map[string]any{
		"name":      "mince_retrieve",
		"arguments": map[string]any{"id": compressed.CCRID},
	})
	var in2 bytes.Buffer
	in2.WriteString(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":` + string(retArgs) + `}` + "\n")
	var out2 bytes.Buffer
	if err := srv.Serve(&in2, &out2); err != nil {
		t.Fatal(err)
	}
	var retResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out2.Bytes()), &retResp); err != nil {
		t.Fatal(err)
	}
	var retrieved struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(retResp.Result.Content[0].Text), &retrieved); err != nil {
		t.Fatal(err)
	}
	if retrieved.Content != payload {
		t.Fatalf("retrieved mismatch: %q", retrieved.Content[:40])
	}
}

func TestUnknownTool(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}` + "\n")
	var out bytes.Buffer
	if err := New(ccr.NewMemory()).Serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("unexpected response %s", out.String())
	}
}
