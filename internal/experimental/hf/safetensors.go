// Package hf reads the subset of SafeTensors needed by mince's local
// salience heads. The reader is pure Go, verifies the file SHA-256 before
// parsing, and never trusts offsets without bounds checks.
package hf

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Tensor is one named tensor view into the file payload.
type Tensor struct {
	Name   string
	Dtype  string
	Shape  []int64
	Offset int
	Data   []byte
}

// File is a parsed SafeTensors file.
type File struct {
	Path    string
	Header  map[string]tensorInfo
	Tensors map[string]*Tensor
	Payload []byte
	SHA256  string
}

type tensorInfo struct {
	Dtype       string  `json:"dtype"`
	Shape       []int64 `json:"shape"`
	DataOffsets [2]int  `json:"data_offsets"`
}

// Load reads and parses a SafeTensors file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(path, b, "")
}

// LoadVerified verifies the whole-file SHA-256 (lowercase hex) before parsing.
func LoadVerified(path, expectedSHA256 string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(path, b, strings.ToLower(strings.TrimSpace(expectedSHA256)))
}

func parse(path string, b []byte, expected string) (*File, error) {
	if len(b) < 8 {
		return nil, errors.New("hf: short safetensors file")
	}
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	if expected != "" && got != expected {
		return nil, fmt.Errorf("hf: sha256 mismatch: got %s want %s", got, expected)
	}
	hlen := binary.LittleEndian.Uint64(b[:8])
	if hlen == 0 || hlen > 100<<20 {
		return nil, errors.New("hf: implausible header length")
	}
	if hlen > uint64(len(b)-8) {
		return nil, errors.New("hf: header exceeds file")
	}
	headerBytes := b[8 : 8+hlen]
	var header map[string]tensorInfo
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("hf: bad header: %w", err)
	}
	payload := b[8+hlen:]
	f := &File{Path: path, Header: header, Tensors: map[string]*Tensor{}, Payload: payload, SHA256: got}
	for name, info := range header {
		start, end := info.DataOffsets[0], info.DataOffsets[1]
		if start < 0 || end < start || end > len(payload) {
			return nil, fmt.Errorf("hf: tensor %q offsets out of range", name)
		}
		f.Tensors[name] = &Tensor{
			Name:   name,
			Dtype:  info.Dtype,
			Shape:  info.Shape,
			Offset: start,
			Data:   payload[start:end],
		}
	}
	return f, nil
}

// Tensor returns a named tensor.
func (f *File) Tensor(name string) (*Tensor, bool) {
	t, ok := f.Tensors[name]
	return t, ok
}

// Uint64 returns element i of a U64/I64/U8/I8 tensor as uint64.
func (t *Tensor) Uint64(i int) (uint64, error) {
	if i < 0 {
		return 0, errors.New("hf: negative index")
	}
	switch strings.ToUpper(t.Dtype) {
	case "U64", "I64":
		if (i+1)*8 > len(t.Data) {
			return 0, errors.New("hf: index out of range")
		}
		return binary.LittleEndian.Uint64(t.Data[i*8:]), nil
	case "U8":
		if i >= len(t.Data) {
			return 0, errors.New("hf: index out of range")
		}
		return uint64(t.Data[i]), nil
	case "I8":
		if i >= len(t.Data) {
			return 0, errors.New("hf: index out of range")
		}
		return uint64(int64(int8(t.Data[i]))), nil
	default:
		return 0, fmt.Errorf("hf: dtype %s is not integer", t.Dtype)
	}
}

// Int64 returns element i as int64.
func (t *Tensor) Int64(i int) (int64, error) {
	u, err := t.Uint64(i)
	if err != nil {
		return 0, err
	}
	if strings.EqualFold(t.Dtype, "I8") || strings.EqualFold(t.Dtype, "I64") {
		if i < 0 {
			return 0, errors.New("hf: negative index")
		}
		switch strings.ToUpper(t.Dtype) {
		case "I8":
			return int64(int8(t.Data[i])), nil
		case "I64":
			return int64(binary.LittleEndian.Uint64(t.Data[i*8:])), nil
		}
	}
	return int64(u), nil
}

// Len returns the element count implied by the shape, or the byte length when
// the shape is empty.
func (t *Tensor) Len() int {
	if len(t.Shape) == 0 {
		return len(t.Data)
	}
	n := int64(1)
	for _, d := range t.Shape {
		if d <= 0 {
			return 0
		}
		n *= d
	}
	if n > int64(len(t.Data))*8 {
		return 0
	}
	return int(n)
}
