package wireagent

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"mince/internal/storage/ccr"
)

func TestCacheGuardRestores(t *testing.T) {
	st := ccr.NewMemory()
	id, err := st.Put([]byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Context{Body: []byte("x"), Headers: http.Header{"X-Mince-Ccr": []string{id}}}
	if err := (&CacheGuardAgent{Store: st}).Apply(c); err != nil {
		t.Fatal(err)
	}
	if string(c.Body) != "hello world" {
		t.Fatalf("body %q", c.Body)
	}
}

func TestPanicSplice(t *testing.T) {
	c := &Context{Body: []byte("{broken"), Headers: http.Header{"Content-Type": []string{"application/json"}}, Status: 500}
	if err := (&PanicSpliceAgent{}).Apply(c); err != nil {
		t.Fatal(err)
	}
	if c.Status != 502 || !strings.Contains(string(c.Body), "upstream panic") {
		t.Fatalf("status %d body %s", c.Status, c.Body)
	}
}

func TestInlineRetrieve(t *testing.T) {
	st := ccr.NewMemory()
	id, err := st.Put([]byte("alpha\nbeta\ngamma"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Context{
		Body:    []byte("beta"),
		Headers: http.Header{"X-Mince-Ccr": []string{id}, "X-Mince-Inline-Tool": []string{"mince_retrieve"}},
	}
	if err := (&InlineCCRAgent{Store: st}).Apply(c); err != nil {
		t.Fatal(err)
	}
	if string(c.Body) != "beta" {
		t.Fatalf("body %q", c.Body)
	}
}

func TestInlineFelix(t *testing.T) {
	c := &Context{Body: []byte(`fn main() -> int { return 7; }`), Headers: http.Header{"X-Mince-Inline-Tool": []string{"felix_eval"}}}
	if err := (&InlineCCRAgent{}).Apply(c); err != nil {
		t.Fatal(err)
	}
	if string(c.Body) != "7" {
		t.Fatalf("body %q", c.Body)
	}
}

func TestPipelineUnderTwoMilliseconds(t *testing.T) {
	st := ccr.NewMemory()
	p := NewPipeline(&TierShuntAgent{}, &PanicSpliceAgent{}, &CacheGuardAgent{Store: st}, &InlineCCRAgent{Store: st})
	start := time.Now()
	for i := 0; i < 1000; i++ {
		c := &Context{Body: []byte("hello"), Headers: http.Header{}}
		if err := p.Run(c); err != nil {
			t.Fatal(err)
		}
	}
	avg := time.Since(start) / 1000
	if avg > 2*time.Millisecond {
		t.Fatalf("average %s", avg)
	}
}
