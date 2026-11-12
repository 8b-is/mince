#include "textflag.h"

// func dotPlaneSliceASM(aPos, aNeg, bPos, bNeg []uint64) int64
TEXT ·dotPlaneSliceASM(SB), NOSPLIT, $0-104
	MOVD aPos_base+0(FP), R0
	MOVD aNeg_base+24(FP), R1
	MOVD bPos_base+48(FP), R2
	MOVD bNeg_base+72(FP), R3
	MOVD aPos_len+8(FP), R4
	MOVD $0, R5
	MOVD $0, R6
loop:
	CMP R4, R6
	BGE done
	MOVD (R0)(R6<<3), R7
	MOVD (R1)(R6<<3), R8
	MOVD (R2)(R6<<3), R9
	MOVD (R3)(R6<<3), R10
	AND R9, R7, R11
	VMOV R11, V0.D[0]
	VCNT V0.B16, V0.B16
	VADDV V0.B16, V1
	VMOV V1.D[0], R12
	ADD R12, R5, R5
	AND R10, R8, R11
	VMOV R11, V0.D[0]
	VCNT V0.B16, V0.B16
	VADDV V0.B16, V1
	VMOV V1.D[0], R12
	ADD R12, R5, R5
	AND R10, R7, R11
	VMOV R11, V0.D[0]
	VCNT V0.B16, V0.B16
	VADDV V0.B16, V1
	VMOV V1.D[0], R12
	SUB R12, R5, R5
	AND R9, R8, R11
	VMOV R11, V0.D[0]
	VCNT V0.B16, V0.B16
	VADDV V0.B16, V1
	VMOV V1.D[0], R12
	SUB R12, R5, R5
	ADD $1, R6
	B loop
done:
	MOVD R5, ret+96(FP)
	RET
