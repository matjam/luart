//go:build darwin || linux

#include "textflag.h"

// func Call(entry uintptr, ctx unsafe.Pointer) uint64
TEXT ·Call(SB), NOSPLIT, $8-24
	MOVQ	entry+0(FP), AX
	MOVQ	ctx+8(FP), DI
	CALL	AX
	MOVQ	AX, ret+16(FP)
	RET
