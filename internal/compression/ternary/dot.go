package ternary

import "math/bits"

// DotInterleavedPlanes computes the ternary dot product over parallel
// uint64 plane slices. It is the block form used by the salience head.
func DotInterleavedPlanes(aPos, aNeg, bPos, bNeg []uint64) int64 {
	n := len(aPos)
	if len(aNeg) < n {
		n = len(aNeg)
	}
	if len(bPos) < n {
		n = len(bPos)
	}
	if len(bNeg) < n {
		n = len(bNeg)
	}
	if n == 0 {
		return 0
	}
	return dotPlaneSliceASM(aPos[:n], aNeg[:n], bPos[:n], bNeg[:n])
}

func dotPlaneSliceGeneric(aPos, aNeg, bPos, bNeg []uint64) int64 {
	var sum int64
	for i := range aPos {
		sum += int64(bits.OnesCount64(aPos[i]&bPos[i])) +
			int64(bits.OnesCount64(aNeg[i]&bNeg[i])) -
			int64(bits.OnesCount64(aPos[i]&bNeg[i])) -
			int64(bits.OnesCount64(aNeg[i]&bPos[i]))
	}
	return sum
}
