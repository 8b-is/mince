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

// FromInt returns n as an exact rational.
func FromInt(n int64) RationalPQ { return RationalPQ{Num: n, Den: 1} }

// Neg returns -r exactly.
func (r RationalPQ) Neg() RationalPQ {
	if r.Num == math.MinInt64 {
		return New(math.MaxInt64, 1)
	}
	return RationalPQ{Num: -r.Num, Den: r.Den}
}

// Sub returns r-o exactly.
func (r RationalPQ) Sub(o RationalPQ) RationalPQ { return r.Add(o.Neg()) }

// Mul returns r*o exactly, reduced and clamped to the representable range.
func (r RationalPQ) Mul(o RationalPQ) RationalPQ {
	n := new(big.Int).Mul(big.NewInt(r.Num), big.NewInt(o.Num))
	d := new(big.Int).Mul(new(big.Int).SetUint64(r.Den), new(big.Int).SetUint64(o.Den))
	return fromBig(n, d)
}

// Div returns r/o exactly. It panics if o is zero; use IsZero first when the
// divisor is data-dependent.
func (r RationalPQ) Div(o RationalPQ) RationalPQ {
	if o.Num == 0 {
		panic("pq: division by zero")
	}
	n := new(big.Int).Mul(big.NewInt(r.Num), new(big.Int).SetUint64(o.Den))
	d := new(big.Int).Mul(new(big.Int).SetUint64(r.Den), big.NewInt(o.Num))
	if d.Sign() < 0 {
		n.Neg(n)
		d.Neg(d)
	}
	return fromBig(n, d)
}

// IsZero reports whether r is exactly zero.
func (r RationalPQ) IsZero() bool { return r.Num == 0 }

// Sign returns -1, 0 or 1.
func (r RationalPQ) Sign() int {
	switch {
	case r.Num < 0:
		return -1
	case r.Num > 0:
		return 1
	default:
		return 0
	}
}

// Min returns the smaller of r and o.
func (r RationalPQ) Min(o RationalPQ) RationalPQ {
	if r.Cmp(o) <= 0 {
		return r
	}
	return o
}

// Max returns the larger of r and o.
func (r RationalPQ) Max(o RationalPQ) RationalPQ {
	if r.Cmp(o) >= 0 {
		return r
	}
	return o
}

func fromBig(n, d *big.Int) RationalPQ {
	if d.Sign() == 0 {
		panic("pq: zero denominator")
	}
	if n.Sign() == 0 {
		return RationalPQ{Num: 0, Den: 1}
	}
	if d.Sign() < 0 {
		n.Neg(n)
		d.Neg(d)
	}
	g := new(big.Int).GCD(nil, nil, new(big.Int).Abs(n), d)
	n.Quo(n, g)
	d.Quo(d, g)
	if !n.IsInt64() || !d.IsUint64() {
		if n.Sign() < 0 {
			return New(-math.MaxInt64, 1)
		}
		return New(math.MaxInt64, 1)
	}
	return New(n.Int64(), d.Uint64())
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
