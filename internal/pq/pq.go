// Package pq provides exact rational arithmetic for compression accounting.
// No float64 is used in core ratios, thresholds, or ledger totals.
package pq

import (
	"fmt"
	"math"
	"math/big"
)

// RationalPQ is an exact rational number: Num/Den, always reduced, Den > 0.
type RationalPQ struct {
	Num int64
	Den uint64
}

// New builds a reduced rational. A zero denominator panics: callers must
// never construct an undefined ratio.
func New(num int64, den uint64) RationalPQ {
	if den == 0 {
		panic("pq: zero denominator")
	}
	if num == 0 {
		return RationalPQ{Num: 0, Den: 1}
	}
	g := gcd(uabs(num), den)
	return RationalPQ{Num: num / int64(g), Den: den / g}
}

// FromRatio returns saved/raw as an exact rational. Non-positive raw yields 0/1.
func FromRatio(saved, raw int64) RationalPQ {
	if raw <= 0 || saved <= 0 {
		return RationalPQ{Num: 0, Den: 1}
	}
	if saved > raw {
		saved = raw
	}
	return New(saved, uint64(raw))
}

// Add returns a+b exactly, reduced. Values are clamped to the int64 range
// rather than silently wrapping.
func (r RationalPQ) Add(o RationalPQ) RationalPQ {
	a := new(big.Int).Mul(big.NewInt(r.Num), new(big.Int).SetUint64(o.Den))
	b := new(big.Int).Mul(big.NewInt(o.Num), new(big.Int).SetUint64(r.Den))
	n := a.Add(a, b)
	d := new(big.Int).Mul(new(big.Int).SetUint64(r.Den), new(big.Int).SetUint64(o.Den))
	g := new(big.Int).GCD(nil, nil, n, d)
	n.Quo(n, g)
	d.Quo(d, g)
	if !n.IsInt64() || !d.IsUint64() {
		return New(math.MaxInt64, 1)
	}
	return New(n.Int64(), d.Uint64())
}

// Cmp returns -1, 0 or 1 as r < o, r == o, r > o. Cross multiplication is
// done with big.Int so it cannot overflow.
func (r RationalPQ) Cmp(o RationalPQ) int {
	a := new(big.Int).Mul(big.NewInt(r.Num), new(big.Int).SetUint64(o.Den))
	b := new(big.Int).Mul(big.NewInt(o.Num), new(big.Int).SetUint64(r.Den))
	return a.Cmp(b)
}

// String renders the exact ratio as "p/q".
func (r RationalPQ) String() string { return fmt.Sprintf("%d/%d", r.Num, r.Den) }

// Percent renders the ratio with two decimal places using integer arithmetic.
func (r RationalPQ) Percent() string {
	scaled := r.Num * 10000
	q := scaled / int64(r.Den)
	rem := scaled % int64(r.Den)
	if rem < 0 {
		rem = -rem
	}
	if rem*2 >= int64(r.Den) {
		if scaled < 0 {
			q--
		} else {
			q++
		}
	}
	sign := ""
	if q < 0 {
		sign = "-"
		q = -q
	}
	return fmt.Sprintf("%s%d.%02d%%", sign, q/100, q%100)
}

func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func uabs(n int64) uint64 {
	if n >= 0 {
		return uint64(n)
	}
	return uint64(-(n + 1)) + 1
}
