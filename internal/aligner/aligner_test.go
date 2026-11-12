package aligner

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"mince/internal/ccr"
)

func bigLog() string {
	return strings.Repeat("[12:00:00] compiling module: ok\n", 300) + "panic: boom\n"
}

func TestAnthropicByteIdentityAndCompression(t *testing.T) {
	logPayload := bigLog()
	logJSON, _ := json.Marshal(logPayload)
	body := []byte(`{"model":"claude-x","system":"SYSTEM EXACT","tools":[{"name":"bash"}],"messages":[` +
		`{"role":"user","content":"hello"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"bash","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":` + string(logJSON) + `}]},` +
		`{"role":"assistant","content":"done"},` +
		`{"role":"user","content":"tail protected"}]}`)

	store := ccr.NewMemory()
	out, stats, err := AlignAnthropic(context.Background(), body, Options{
		MinBytes:         100,
		ProtectTailTurns: 2,
		Store:            store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Minced != 1 {
		t.Fatalf("minced %d", stats.Minced)
	}
	if len(out) >= len(body) {
		t.Fatal("expected a smaller request")
	}

	for _, key := range []string{"system", "tools"} {
		s1, e1, ok := findValueSpan(body, key)
		if !ok {
			t.Fatalf("missing %s", key)
		}
		s2, e2, ok := findValueSpan(out, key)
		if !ok {
			t.Fatalf("missing %s in output", key)
		}
		if !bytes.Equal(body[s1:e1], out[s2:e2]) {
			t.Fatalf("%s bytes changed", key)
		}
	}

	s1, e1, _ := findValueSpan(body, "messages")
	s2, e2, _ := findValueSpan(out, "messages")
	before, _ := splitRawArray(body[s1:e1])
	after, _ := splitRawArray(out[s2:e2])
	if len(before) != len(after) {
		t.Fatalf("message count %d -> %d", len(before), len(after))
	}
	for _, i := range []int{0, 1, 3, 4} {
		if !bytes.Equal(before[i], after[i]) {
			t.Fatalf("protected message %d changed", i)
		}
	}
	if bytes.Equal(before[2], after[2]) {
		t.Fatal("eligible tool_result was not rewritten")
	}

	re := regexp.MustCompile(`ccr_[0-9a-f]{12,16}`)
	id := re.FindString(string(after[2]))
	if id == "" {
		t.Fatal("no ccr pointer in rewritten message")
	}
	got, err := store.Retrieve(strings.TrimPrefix(id, "ccr_"), "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != logPayload {
		t.Fatalf("retrieved payload mismatch: %q", got[:40])
	}
}

func TestOpenAIToolMessage(t *testing.T) {
	logPayload := bigLog()
	logJSON, _ := json.Marshal(logPayload)
	body := []byte(`{"model":"gpt","messages":[` +
		`{"role":"system","content":"SYS EXACT"},` +
		`{"role":"user","content":"hi"},` +
		`{"role":"tool","tool_call_id":"c1","content":` + string(logJSON) + `},` +
		`{"role":"assistant","content":"ok"},` +
		`{"role":"user","content":"tail"}]}`)

	out, stats, err := AlignOpenAI(context.Background(), body, Options{
		MinBytes:         100,
		ProtectTailTurns: 2,
		Store:            ccr.NewMemory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Minced != 1 {
		t.Fatalf("minced %d", stats.Minced)
	}
	s1, e1, _ := findValueSpan(body, "messages")
	s2, e2, _ := findValueSpan(out, "messages")
	before, _ := splitRawArray(body[s1:e1])
	after, _ := splitRawArray(out[s2:e2])
	for _, i := range []int{0, 1, 3, 4} {
		if !bytes.Equal(before[i], after[i]) {
			t.Fatalf("message %d changed", i)
		}
	}
	if !strings.Contains(string(after[2]), "mince:ccr") {
		t.Fatal("tool message lacks footer")
	}
}

func TestNoEligibleContentReturnsOriginal(t *testing.T) {
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"short"}]}`)
	out, stats, err := AlignAnthropic(context.Background(), body, Options{
		ProtectTailTurns: 2,
		Store:            ccr.NewMemory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Minced != 0 || !bytes.Equal(out, body) {
		t.Fatal("no-op alignment must be byte-identical")
	}
}

func TestAutoDetect(t *testing.T) {
	openai := []byte(`{"messages":[{"role":"tool","content":"x"}]}`)
	if _, _, err := Align(context.Background(), openai, Options{Store: ccr.NewMemory()}); err != nil {
		t.Fatal(err)
	}
	anthropic := []byte(`{"system":"s","messages":[{"role":"user","content":"x"}]}`)
	if _, _, err := Align(context.Background(), anthropic, Options{Store: ccr.NewMemory()}); err != nil {
		t.Fatal(err)
	}
}
