package felix

import (
	"strings"
	"testing"

	"mince/internal/pq"
)

func mustCompile(t *testing.T, src string) *Program {
	t.Helper()
	p, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIntAdd(t *testing.T) {
	p := mustCompile(t, `fn add(a: int, b: int) -> int { return a + b; }`)
	got, err := p.Call("add", []Value{IntValue(2), IntValue(3)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KInt || got.Int != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestExactDivisionAndPQ(t *testing.T) {
	p := mustCompile(t, `fn half(x: int) -> pq { return x / 2; }`)
	got, err := p.Call("half", []Value{IntValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KPQ || got.PQ.String() != "1/2" {
		t.Fatalf("got %+v", got)
	}
}

func TestMaxPQAndDotTrit(t *testing.T) {
	p := mustCompile(t, `
fn pick(a: pq, b: pq) -> pq { return max_pq(a, b); }
fn dot(a: trit, b: trit) -> pq { return dot_trit(a, b); }
`)
	got, err := p.Call("pick", []Value{PQValue(pq.New(1, 2)), PQValue(pq.New(2, 3))})
	if err != nil {
		t.Fatal(err)
	}
	if got.PQ.String() != "2/3" {
		t.Fatalf("pick %s", got.PQ)
	}
	got, err = p.Call("dot", []Value{TritValue(-1), TritValue(-1)})
	if err != nil {
		t.Fatal(err)
	}
	if got.PQ.String() != "1/1" {
		t.Fatalf("dot %s", got.PQ)
	}
}

func TestIfElse(t *testing.T) {
	p := mustCompile(t, `fn sign(x: int) -> int { if x < 0 { return -1; } else { return 1; } }`)
	got, err := p.Call("sign", []Value{IntValue(-9)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Int != -1 {
		t.Fatalf("negative %+v", got)
	}
	got, err = p.Call("sign", []Value{IntValue(9)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Int != 1 {
		t.Fatalf("positive %+v", got)
	}
}

func TestCapsuleConsume(t *testing.T) {
	p := mustCompile(t, `fn shrink(c: capsule<sym>) -> sym { return consume(c); }`)
	id := p.Intern("hello")
	got, err := p.Call("shrink", []Value{SymValue(id)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KSym || got.Sym != id {
		t.Fatalf("got %+v", got)
	}
}

func TestCapsuleBuiltin(t *testing.T) {
	p := mustCompile(t, `fn roundtrip(s: sym) -> sym { let c = capsule(s); return consume(c); }`)
	id := p.Intern("roundtrip")
	got, err := p.Call("roundtrip", []Value{SymValue(id)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Sym != id {
		t.Fatalf("got %+v", got)
	}
}

func TestRejectsDoubleConsume(t *testing.T) {
	_, err := Compile(`fn bad(c: capsule<sym>) -> sym { consume(c); consume(c); }`)
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected linearity error, got %v", err)
	}
}

func TestRejectsUseOutsideConsume(t *testing.T) {
	_, err := Compile(`fn bad(c: capsule<sym>) -> sym { let x = c; return consume(c); }`)
	if err == nil || !strings.Contains(err.Error(), "outside consume") {
		t.Fatalf("expected linearity error, got %v", err)
	}
}

func TestRejectsReturnTypeMismatch(t *testing.T) {
	_, err := Compile(`fn bad(x: int) -> bool { return x; }`)
	if err == nil {
		t.Fatal("type mismatch accepted")
	}
}

func TestOutOfGas(t *testing.T) {
	p := mustCompile(t, `fn add(a: int, b: int) -> int { return a + b; }`)
	p.Gas = 1
	if _, err := p.Call("add", []Value{IntValue(1), IntValue(2)}); err == nil {
		t.Fatal("out of gas accepted")
	}
}

func TestSymEqualityAndBool(t *testing.T) {
	p := mustCompile(t, `fn same(a: sym, b: sym) -> bool { return a == b && !false; }`)
	id := p.Intern("x")
	got, err := p.Call("same", []Value{SymValue(id), SymValue(id)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Bool {
		t.Fatalf("got %+v", got)
	}
	other := p.Intern("y")
	got, err = p.Call("same", []Value{SymValue(id), SymValue(other)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Bool {
		t.Fatalf("got %+v", got)
	}
}
