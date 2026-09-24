//go:build darwin || linux

#include "textflag.h"

// func Call(entry uintptr, ctx unsafe.Pointer) uint64
//
// The 16-byte frame makes the assembler save and restore the link
// register around the call.
TEXT ·Call(SB), NOSPLIT, $16-24
	MOVD	entry+0(FP), R1
	MOVD	ctx+8(FP), R0
	CALL	(R1)
	MOVD	R0, ret+16(FP)
	RET
