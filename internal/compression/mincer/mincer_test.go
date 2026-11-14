package mincer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"mince/internal/compression/mincer"
	"mince/internal/compression/router"
	"mince/internal/storage/ccr"
)

func TestJSONCrusherPreservesAnomaly(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 200; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if i == 137 {
			b.WriteString(`{"id":137,"status":"error","msg":"anomaly: disk failure on node 7"}`)
			continue
		}
		fmt.Fprintf(&b, `{"id":%d,"status":"ok","msg":"routine success payload number %d"}`, i, i)
	}
	b.WriteByte(']')
	raw := []byte(b.String())
	store := ccr.NewMemory()
	res := mincer.Mince(context.Background(), raw, mincer.Options{
		Kind:  router.KindJSON,
		Store: store,
	})
	if res.Passthrough {
		t.Fatal("expected JSON to be minced")
	}
	if len(res.Minced)*4 > len(raw) {
		t.Fatalf("reduction too weak: raw=%d minced=%d", len(raw), len(res.Minced))
	}
	var obj map[string]any
	if err := json.Unmarshal(res.Minced, &obj); err != nil {
		t.Fatalf("minced JSON invalid: %v", err)
	}
	if obj["_mince"] != "json_array" {
		t.Fatalf("missing marker: %v", obj["_mince"])
	}
	if _, ok := obj["_ccr"]; !ok {
		t.Fatal("missing in-band ccr pointer")
	}
	if !strings.Contains(string(res.Minced), "anomaly: disk failure") {
		t.Fatal("anomalous record was dropped")
	}
	id := strings.TrimPrefix(res.CCRID, "ccr_")
	got, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("ccr round trip mismatch")
	}
}

func TestLogMincerKeepsPanic(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 490; i++ {
		fmt.Fprintf(&b, "[12:00:%02d] compiling module pkg%03d: ok\n", i%60, i%17)
	}
	b.WriteString("panic: runtime error: index out of range [7]\n\ngoroutine 1 [running]:\nmain.main()\n\t/src/main.go:42 +0x1d\n")
	raw := []byte(b.String())
	res := mincer.Mince(context.Background(), raw, mincer.Options{
		Kind:  router.KindLog,
		Store: ccr.NewMemory(),
	})
	if res.Passthrough {
		t.Fatal("expected log to be minced")
	}
	if len(res.Minced)*5 > len(raw) {
		t.Fatalf("reduction too weak: raw=%d minced=%d", len(raw), len(res.Minced))
	}
	for _, want := range []string{"panic: runtime error", "goroutine 1", "main.main()", "repeated"} {
		if !strings.Contains(string(res.Minced), want) {
			t.Fatalf("missing %q in minced log", want)
		}
	}
}

func TestCodeMincerFoldsBodies(t *testing.T) {
	var b strings.Builder
	b.WriteString("package demo\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "\tfmt.Println(\"line %d\")\n", i)
	}
	b.WriteString("}\n")
	raw := []byte(b.String())
	res := mincer.Mince(context.Background(), raw, mincer.Options{
		Kind:     router.KindCode,
		MinBytes: 100,
		Store:    ccr.NewMemory(),
	})
	if res.Passthrough {
		t.Fatal("expected code to be minced")
	}
	if !strings.Contains(string(res.Minced), "minced L") {
		t.Fatal("missing fold marker")
	}
	if !strings.Contains(string(res.Minced), "func main()") {
		t.Fatal("declaration was folded away")
	}
}

func TestBelowThresholdPassthrough(t *testing.T) {
	raw := []byte(`{"a":1}`)
	res := mincer.Mince(context.Background(), raw, mincer.Options{Store: ccr.NewMemory()})
	if !res.Passthrough || !bytes.Equal(res.Minced, raw) {
		t.Fatal("small content must pass through verbatim")
	}
}
