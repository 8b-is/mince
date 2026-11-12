// Package litany implements the 64-byte LTNY v3 wire frame used to carry
// capability-scoped proposals, verdicts and policy swaps between mince
// components. Frames are hash-chained and CRC32C protected so a single
// flipped bit is rejected before any payload is trusted.
package litany

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"mince/internal/pq"
)

const (
	// Magic is the four-byte frame prefix.
	Magic = "LTNY"
	// Version is the wire format version.
	Version = 0x03
	// HeaderSize is fixed: 4 magic + 1 version + 1 kind + 2 flags +
	// 4 payload length + 8 capability mask + 4 budget numerator +
	// 4 budget denominator + 32 previous hash + 4 payload CRC32C.
	HeaderSize = 64
)

// FrameKind identifies the semantic payload.
type FrameKind uint8

const (
	KindControl FrameKind = iota + 1
	KindProposal
	KindVerdict
	KindPolicy
	KindTelemetry
)

// Capability bits. A frame may only exercise capabilities present in its
// mask; consumers must reject required bits that are not granted (POLA).
const (
	CapRetrieve uint64 = 1 << iota
	CapFelix
	CapModel
	CapNetwork
	CapFSRead
	CapFSWrite
	CapExec
	CapRalph
	CapTelemetry
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Header is the decoded fixed-size frame header.
type Header struct {
	Kind       FrameKind
	Flags      uint16
	CapMask    uint64
	Budget     pq.RationalPQ
	PrevHash   [32]byte
	PayloadCRC uint32
}

// Frame is a header plus its immutable payload.
type Frame struct {
	Header
	Payload []byte
}

// ErrShortFrame is returned when fewer than HeaderSize bytes are supplied.
var ErrShortFrame = errors.New("litany: short frame")

// Allowed reports whether every required capability bit is granted.
func Allowed(granted, required uint64) bool { return granted&required == required }

// Seal builds a frame, computing its CRC32C and copying the payload.
func Seal(kind FrameKind, flags uint16, caps uint64, budget pq.RationalPQ, prev [32]byte, payload []byte) (*Frame, error) {
	if kind < KindControl || kind > KindTelemetry {
		return nil, fmt.Errorf("litany: invalid frame kind %d", kind)
	}
	if budget.Den == 0 {
		budget = pq.New(0, 1)
	}
	if budget.Den > 1<<32-1 {
		return nil, errors.New("litany: budget denominator exceeds uint32")
	}
	if budget.Num < -1<<31 || budget.Num > 1<<31-1 {
		return nil, errors.New("litany: budget numerator exceeds int32")
	}
	f := &Frame{Header: Header{
		Kind:     kind,
		Flags:    flags,
		CapMask:  caps,
		Budget:   budget,
		PrevHash: prev,
	}, Payload: append([]byte(nil), payload...)}
	f.PayloadCRC = crc32.Checksum(f.Payload, castagnoli)
	return f, nil
}

// Encode serializes the frame as a fixed header followed by the payload.
func (f *Frame) Encode() ([]byte, error) {
	if f == nil {
		return nil, errors.New("litany: nil frame")
	}
	if len(f.Payload) > 1<<32-1 {
		return nil, errors.New("litany: payload exceeds uint32")
	}
	if f.Budget.Den == 0 {
		f.Budget = pq.New(0, 1)
	}
	if f.Budget.Den > 1<<32-1 {
		return nil, errors.New("litany: budget denominator exceeds uint32")
	}
	out := make([]byte, HeaderSize+len(f.Payload))
	copy(out[0:4], Magic)
	out[4] = Version
	out[5] = byte(f.Kind)
	binary.BigEndian.PutUint16(out[6:8], f.Flags)
	binary.BigEndian.PutUint32(out[8:12], uint32(len(f.Payload)))
	binary.BigEndian.PutUint64(out[12:20], f.CapMask)
	binary.BigEndian.PutUint32(out[20:24], uint32(int32(f.Budget.Num)))
	binary.BigEndian.PutUint32(out[24:28], uint32(f.Budget.Den))
	copy(out[28:60], f.PrevHash[:])
	binary.BigEndian.PutUint32(out[60:64], f.PayloadCRC)
	copy(out[64:], f.Payload)
	return out, nil
}

// Decode parses and validates a frame. It rejects bad magic, unsupported
// versions, truncated payloads, trailing bytes and CRC mismatches.
func Decode(b []byte) (*Frame, error) {
	if len(b) < HeaderSize {
		return nil, ErrShortFrame
	}
	if string(b[0:4]) != Magic {
		return nil, errors.New("litany: bad magic")
	}
	if b[4] != Version {
		return nil, fmt.Errorf("litany: unsupported version %d", b[4])
	}
	kind := FrameKind(b[5])
	if kind < KindControl || kind > KindTelemetry {
		return nil, fmt.Errorf("litany: invalid frame kind %d", kind)
	}
	plen := int(binary.BigEndian.Uint32(b[8:12]))
	if plen != len(b)-HeaderSize {
		return nil, fmt.Errorf("litany: payload length %d does not match %d", plen, len(b)-HeaderSize)
	}
	num := int64(int32(binary.BigEndian.Uint32(b[20:24])))
	den := uint64(binary.BigEndian.Uint32(b[24:28]))
	if den == 0 {
		return nil, errors.New("litany: zero budget denominator")
	}
	f := &Frame{Header: Header{
		Kind:       kind,
		Flags:      binary.BigEndian.Uint16(b[6:8]),
		CapMask:    binary.BigEndian.Uint64(b[12:20]),
		Budget:     pq.New(num, den),
		PayloadCRC: binary.BigEndian.Uint32(b[60:64]),
	}, Payload: append([]byte(nil), b[HeaderSize:]...)}
	copy(f.PrevHash[:], b[28:60])
	if got := crc32.Checksum(f.Payload, castagnoli); got != f.PayloadCRC {
		return nil, fmt.Errorf("litany: crc mismatch %08x != %08x", got, f.PayloadCRC)
	}
	return f, nil
}

// Hash returns SHA-256 over the encoded frame.
func (f *Frame) Hash() [32]byte {
	b, err := f.Encode()
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(b)
}

// VerifyChain checks that each frame points at the previous frame's hash and
// that the first frame points at root. Pass a zero root for a fresh chain.
func VerifyChain(frames []*Frame, root [32]byte) error {
	prev := root
	for i, f := range frames {
		if f == nil {
			return fmt.Errorf("litany: nil frame at %d", i)
		}
		if f.PrevHash != prev {
			return fmt.Errorf("litany: broken chain at frame %d", i)
		}
		prev = f.Hash()
	}
	return nil
}
