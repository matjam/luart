//go:build darwin || linux

#include "textflag.h"

// func flushICache(p unsafe.Pointer, n uintptr)
//
// 64 bytes is no larger than any arm64 cache line, so stepping by it
// covers every line in the range.
TEXT ·flushICache(SB), NOSPLIT, $0-16
	MOVD	p+0(FP), R0
	MOVD	n+8(FP), R1
	ADD	R0, R1, R1           // R1 = end
	AND	$~63, R0, R2         // R2 = first line
clean:
	WORD	$0xd50b7b22          // DC CVAU, X2
	ADD	$64, R2, R2
	CMP	R1, R2
	BLO	clean
	WORD	$0xd5033b9f          // DSB ISH
	AND	$~63, R0, R2
invalidate:
	WORD	$0xd50b7522          // IC IVAU, X2
	ADD	$64, R2, R2
	CMP	R1, R2
	BLO	invalidate
	WORD	$0xd5033b9f          // DSB ISH
	WORD	$0xd5033fdf          // ISB
	RET
