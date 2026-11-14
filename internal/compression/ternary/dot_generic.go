//go:build !amd64 && !arm64

package ternary

func dotPlaneSliceASM(aPos, aNeg, bPos, bNeg []uint64) int64 {
	return dotPlaneSliceGeneric(aPos, aNeg, bPos, bNeg)
}
