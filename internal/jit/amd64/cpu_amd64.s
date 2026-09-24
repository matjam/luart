#include "textflag.h"

// func cpuid1ECX() uint32
TEXT ·cpuid1ECX(SB), NOSPLIT, $0-4
	MOVL	$1, AX
	XORL	CX, CX
	CPUID
	MOVL	CX, ret+0(FP)
	RET
