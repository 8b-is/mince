package ternary

import "testing"

func TestDotInterleavedPlanesMatchesGeneric(t *testing.T) {
	aPos := []uint64{0b1011, 0xffffffffffffffff, 0}
	aNeg := []uint64{0b0100, 0, 0xaaaaaaaaaaaaaaaa}
	bPos := []uint64{0b1001, 0x0f0f0f0f0f0f0f0f, 0x5555555555555555}
	bNeg := []uint64{0b0110, 0xf0f0f0f0f0f0f0f0, 0x5555555555555555}
	got := DotInterleavedPlanes(aPos, aNeg, bPos, bNeg)
	want := dotPlaneSliceGeneric(aPos, aNeg, bPos, bNeg)
	if got != want {
		t.Fatalf("asm %d generic %d", got, want)
	}
}

func TestDotInterleavedPlanesEmpty(t *testing.T) {
	if got := DotInterleavedPlanes(nil, nil, nil, nil); got != 0 {
		t.Fatalf("got %d", got)
	}
}
