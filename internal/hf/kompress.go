package hf

import (
	"errors"
	"fmt"

	"mince/internal/pq"
	"mince/internal/ternary"
)

// KompressUltra is the integer-only salience head distilled from the
// PeetPedro/kompress-v17 family. It scores a 64-trit SimHash with POPCNT
// only: no float multiply, no model runtime.
type KompressUltra struct {
	Pos    uint64
	Neg    uint64
	Bias   int64
	Model  string
	SHA256 string
}

// LoadKompressUltraSafetensors loads trit_pos, trit_neg and bias tensors.
// expectedSHA256 may be empty for development; production callers pass it.
func LoadKompressUltraSafetensors(path, expectedSHA256 string) (*KompressUltra, error) {
	f, err := LoadVerified(path, expectedSHA256)
	if err != nil {
		return nil, err
	}
	posT, ok := f.Tensor("trit_pos")
	if !ok {
		return nil, errors.New("hf: missing trit_pos")
	}
	negT, ok := f.Tensor("trit_neg")
	if !ok {
		return nil, errors.New("hf: missing trit_neg")
	}
	pos, err := posT.Uint64(0)
	if err != nil {
		return nil, fmt.Errorf("hf: trit_pos: %w", err)
	}
	neg, err := negT.Uint64(0)
	if err != nil {
		return nil, fmt.Errorf("hf: trit_neg: %w", err)
	}
	if pos&neg != 0 {
		return nil, errors.New("hf: trit planes overlap")
	}
	k := &KompressUltra{Pos: pos, Neg: neg, Model: "PeetPedro/kompress-v17", SHA256: f.SHA256}
	if biasT, ok := f.Tensor("bias"); ok {
		bias, err := biasT.Int64(0)
		if err != nil {
			return nil, fmt.Errorf("hf: bias: %w", err)
		}
		k.Bias = bias
	}
	return k, nil
}

// ScoreSimHash64 returns the exact salience score in [-1,1] as dot/64 plus
// bias/64. It uses only the bitboard dot product.
func (k *KompressUltra) ScoreSimHash64(h ternary.TritPlane64) pq.RationalPQ {
	if k == nil || !h.Valid() || k.Pos&k.Neg != 0 {
		return pq.New(0, 1)
	}
	dot := ternary.DotBitboard(h, ternary.TritPlane64{Pos: k.Pos, Neg: k.Neg})
	return pq.New(int64(dot)+k.Bias, 64)
}

// RawScore returns the integer numerator used by ScoreSimHash64.
func (k *KompressUltra) RawScore(h ternary.TritPlane64) int64 {
	if k == nil || !h.Valid() || k.Pos&k.Neg != 0 {
		return 0
	}
	return int64(ternary.DotBitboard(h, ternary.TritPlane64{Pos: k.Pos, Neg: k.Neg})) + k.Bias
}
