package ternary

import "testing"

func TestPlaneSetGetInvariant(t *testing.T) {
	var p TritPlane64
	vals := []Trit{Pos, Neg, Zero, Pos, Neg}
	for i, v := range vals {
		p.Set(i, v)
	}
	if !p.Valid() {
		t.Fatal("plane invariant violated")
	}
	for i, v := range vals {
		if got := p.Get(i); got != v {
			t.Fatalf("index %d: got %d want %d", i, got, v)
		}
	}
	if p.Get(63) != Zero {
		t.Fatal("unset trit should be zero")
	}
}

func TestDotAndSimilarity(t *testing.T) {
	var a, b TritPlane64
	for i := 0; i < 10; i++ {
		a.Set(i, Pos)
		b.Set(i, Pos)
	}
	for i := 10; i < 20; i++ {
		a.Set(i, Neg)
		b.Set(i, Neg)
	}
	if got := DotBitboard(a, b); got != 20 {
		t.Fatalf("dot %d", got)
	}
	if got := Similarity(a, b).String(); got != "5/16" {
		t.Fatalf("similarity %s", got)
	}
	var c TritPlane64
	for i := 0; i < 10; i++ {
		c.Set(i, Neg)
	}
	if got := DotBitboard(a, c); got != -10 {
		t.Fatalf("dot %d", got)
	}
}

func TestPackTrit5RoundTrip(t *testing.T) {
	in := []Trit{Pos, Neg, Zero, Pos, Neg, Zero, Pos, Neg, Zero, Pos, Neg, Zero}
	packed := PackTrit5(in)
	if len(packed) != 3 {
		t.Fatalf("packed len %d", len(packed))
	}
	out := UnpackTrit5(packed, len(in))
	for i := range in {
		if in[i] != out[i] {
			t.Fatalf("index %d: got %d want %d", i, out[i], in[i])
		}
	}
}

func TestSimHashDeterministic(t *testing.T) {
	a := SimHash64([]string{"compile", "module", "ok", "step", "42"})
	b := SimHash64([]string{"compile", "module", "ok", "step", "42"})
	if a != b {
		t.Fatal("simhash must be deterministic")
	}
	c := SimHash64([]string{"panic", "runtime", "error", "index", "out", "range"})
	if HammingDistance(a, b) != 0 {
		t.Fatal("identical token sets must have zero distance")
	}
	if HammingDistance(a, c) == 0 {
		t.Fatal("different token sets should differ")
	}
}
