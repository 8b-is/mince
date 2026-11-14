// Package ternary implements balanced-ternary {-1,0,+1} bitboards used for
// salience signatures. Similarity is computed with POPCNT, never floats.
package ternary

import (
	"hash/fnv"
	"math/bits"

	"mince/internal/pq"
)

// Trit is a balanced ternary digit.
type Trit int8

const (
	Neg  Trit = -1
	Zero Trit = 0
	Pos  Trit = 1
)

// TritPlane64 stores 64 trits as two disjoint bitsets.
// Invariant: Pos&Neg == 0.
type TritPlane64 struct {
	Pos uint64
	Neg uint64
}

// Set assigns trit v at index i (0..63).
func (t *TritPlane64) Set(i int, v Trit) {
	if i < 0 || i > 63 {
		return
	}
	mask := uint64(1) << uint(i)
	t.Pos &^= mask
	t.Neg &^= mask
	switch v {
	case Pos:
		t.Pos |= mask
	case Neg:
		t.Neg |= mask
	}
}

// Get returns the trit at index i.
func (t TritPlane64) Get(i int) Trit {
	if i < 0 || i > 63 {
		return Zero
	}
	mask := uint64(1) << uint(i)
	switch {
	case t.Pos&mask != 0:
		return Pos
	case t.Neg&mask != 0:
		return Neg
	default:
		return Zero
	}
}

// Valid reports whether the plane invariant holds.
func (t TritPlane64) Valid() bool { return t.Pos&t.Neg == 0 }

// DotBitboard returns the ternary dot product in [-64,64].
func DotBitboard(a, b TritPlane64) int {
	return bits.OnesCount64(a.Pos&b.Pos) +
		bits.OnesCount64(a.Neg&b.Neg) -
		bits.OnesCount64(a.Pos&b.Neg) -
		bits.OnesCount64(a.Neg&b.Pos)
}

// Similarity returns dot/64 as an exact rational.
func Similarity(a, b TritPlane64) pq.RationalPQ {
	return pq.New(int64(DotBitboard(a, b)), 64)
}

// HammingDistance counts trits whose signs differ.
func HammingDistance(a, b TritPlane64) int {
	return bits.OnesCount64((a.Pos ^ b.Pos) | (a.Neg ^ b.Neg))
}

// SimHash64 folds tokens into a 64-trit signature by majority vote of hashed
// token bits. Equal positive/negative votes leave the trit at zero.
func SimHash64(tokens []string) TritPlane64 {
	var score [64]int32
	for _, tok := range tokens {
		h := fnv.New64a()
		_, _ = h.Write([]byte(tok))
		v := h.Sum64()
		for i := 0; i < 64; i++ {
			if v&(uint64(1)<<uint(i)) != 0 {
				score[i]++
			} else {
				score[i]--
			}
		}
	}
	var p TritPlane64
	for i := 0; i < 64; i++ {
		switch {
		case score[i] > 0:
			p.Set(i, Pos)
		case score[i] < 0:
			p.Set(i, Neg)
		}
	}
	return p
}

// PackTrit5 packs 5 trits per byte (3^5 = 243 <= 255).
func PackTrit5(ts []Trit) []byte {
	out := make([]byte, (len(ts)+4)/5)
	pow := [5]int{1, 3, 9, 27, 81}
	for i, t := range ts {
		v := int(t) + 1
		if v < 0 || v > 2 {
			v = 1
		}
		out[i/5] += byte(v * pow[i%5])
	}
	return out
}

// UnpackTrit5 decodes n trits from packed bytes.
func UnpackTrit5(b []byte, n int) []Trit {
	out := make([]Trit, n)
	pow := [5]int{1, 3, 9, 27, 81}
	for i := 0; i < n; i++ {
		if i/5 >= len(b) {
			break
		}
		digit := (int(b[i/5]) / pow[i%5]) % 3
		out[i] = Trit(digit - 1)
	}
	return out
}
