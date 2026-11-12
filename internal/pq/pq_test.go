package pq

import "testing"

func TestReduceAndFormat(t *testing.T) {
	r := New(2, 4)
	if r.String() != "1/2" {
		t.Fatalf("got %s", r.String())
	}
	if got := r.Percent(); got != "50.00%" {
		t.Fatalf("percent %s", got)
	}
	if got := New(1, 3).Percent(); got != "33.33%" {
		t.Fatalf("percent %s", got)
	}
	if got := New(2, 3).Percent(); got != "66.67%" {
		t.Fatalf("percent %s", got)
	}
}

func TestCmpAndAdd(t *testing.T) {
	if New(1, 2).Cmp(New(1, 3)) <= 0 {
		t.Fatal("1/2 should exceed 1/3")
	}
	if New(1, 3).Cmp(New(2, 6)) != 0 {
		t.Fatal("reduced equal ratios must compare equal")
	}
	sum := New(1, 2).Add(New(1, 3))
	if sum.String() != "5/6" {
		t.Fatalf("sum %s", sum)
	}
}

func TestFromRatio(t *testing.T) {
	if got := FromRatio(50, 100).String(); got != "1/2" {
		t.Fatalf("got %s", got)
	}
	if got := FromRatio(0, 100).String(); got != "0/1" {
		t.Fatalf("got %s", got)
	}
	if got := FromRatio(10, 0).String(); got != "0/1" {
		t.Fatalf("got %s", got)
	}
}

func TestArithmetic(t *testing.T) {
	if got := New(1, 2).Sub(New(1, 3)).String(); got != "1/6" {
		t.Fatalf("sub %s", got)
	}
	if got := New(2, 3).Mul(New(3, 4)).String(); got != "1/2" {
		t.Fatalf("mul %s", got)
	}
	if got := New(1, 2).Div(New(3, 4)).String(); got != "2/3" {
		t.Fatalf("div %s", got)
	}
	if got := New(1, 2).Neg().String(); got != "-1/2" {
		t.Fatalf("neg %s", got)
	}
	if got := New(1, 2).Min(New(2, 3)).String(); got != "1/2" {
		t.Fatalf("min %s", got)
	}
	if got := New(1, 2).Max(New(2, 3)).String(); got != "2/3" {
		t.Fatalf("max %s", got)
	}
	if !New(0, 7).IsZero() || New(0, 7).Sign() != 0 {
		t.Fatal("zero checks failed")
	}
}
