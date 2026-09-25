package amd64

import (
	"encoding/hex"
	"strings"
	"testing"
)

// The expected bytes come from clang -c -arch x86_64 on the Intel-syntax
// assembly in each comment. Displacements are large so that clang, like
// this package, uses 32-bit ones.
func TestEncodings(t *testing.T) {
	var a Asm
	a.MovImm(AX, 0)               // xor eax, eax
	a.MovImm(R9, 0x1234)          // mov r9d, 0x1234
	a.MovImm(BX, 0x123456789)     // movabs rbx, 0x123456789
	a.Mov(R10, SI)                // mov r10, rsi
	a.Load(AX, R12, 0x1000)       // mov rax, qword ptr [r12 + 0x1000]
	a.Store(BP, 0x1000, R15)      // mov qword ptr [rbp + 0x1000], r15
	a.Store(R13, 0x1000, CX)      // mov qword ptr [r13 + 0x1000], rcx
	a.Load32(R8, SP, 0x1000)      // mov r8d, dword ptr [rsp + 0x1000]
	a.Load8(DX, DI, 0x1000)       // movzx edx, byte ptr [rdi + 0x1000]
	a.Store8(BX, 0x1000, SI)      // mov byte ptr [rbx + 0x1000], sil
	a.Store8(R9, 0x1000, R11)     // mov byte ptr [r9 + 0x1000], r11b
	a.StoreZero(AX, 0x1000)       // mov qword ptr [rax + 0x1000], 0
	a.StoreZero8(R14, 0x1000)     // mov byte ptr [r14 + 0x1000], 0
	a.Lea(R11, SI, R9, 3, 0x1000) // lea r11, [rsi + r9*8 + 0x1000]
	a.Lea(AX, R13, CX, 0, 0x1000) // lea rax, [r13 + rcx*1 + 0x1000]
	a.AddImm(R8, 0x1000)          // add r8, 0x1000
	a.SubImm(SP, 0x1000)          // sub rsp, 0x1000
	a.CmpImm(DX, 0x1000)          // cmp rdx, 0x1000
	a.Add(AX, R15)                // add rax, r15
	a.Sub(R9, BX)                 // sub r9, rbx
	a.Cmp(CX, DX)                 // cmp rcx, rdx
	a.Test(R10, R10)              // test r10, r10
	a.CmpMem(DI, 0x1000, 0x1000)  // cmp qword ptr [rdi + 0x1000], 0x1000
	a.SubMem(DI, 0x1000, 0x1000)  // sub qword ptr [rdi + 0x1000], 0x1000
	a.Shr(R12, 4)                 // shr r12, 4
	a.Bt(AX, 63)                  // bt rax, 63
	a.JmpReg(R11)                 // jmp r11
	a.Ret()                       // ret
	a.LoadSD(3, R15, 0x1000)      // movsd xmm3, qword ptr [r15 + 0x1000]
	a.StoreSD(SP, 0x1000, 12)     // movsd qword ptr [rsp + 0x1000], xmm12
	a.MovSD(1, 9)                 // movapd xmm1, xmm9
	a.AddSD(0, 1)                 // addsd xmm0, xmm1
	a.SubSD(10, 2)                // subsd xmm10, xmm2
	a.MulSD(3, 11)                // mulsd xmm3, xmm11
	a.DivSD(4, 5)                 // divsd xmm4, xmm5
	a.SqrtSD(6, 7)                // sqrtsd xmm6, xmm7
	a.XorPD(8, 8)                 // xorpd xmm8, xmm8
	a.AndPD(1, 14)                // andpd xmm1, xmm14
	a.Ucomisd(2, 13)              // ucomisd xmm2, xmm13
	a.RoundSD(3, 4, 1)            // roundsd xmm3, xmm4, 1
	a.Cvttsd2si(R10, 1)           // cvttsd2si r10, xmm1
	a.Cvtsi2sd(9, AX)             // cvtsi2sd xmm9, rax
	a.MovqToX(2, R8)              // movq xmm2, r8
	a.MovqFromX(CX, 11)           // movq rcx, xmm11
	check(t, &a, golden)
}

const golden = "" +
	"31c041b93412000048bb89674523010000004989f2498b8424001000004c89bd" +
	"0010000049898d00100000448b8424001000000fb697001000004088b3001000" +
	"004588990010000048c780001000000000000041c68600100000004e8d9cce00" +
	"100000498d840d001000004981c0001000004881ec001000004881fa00100000" +
	"4c01f84929d94839d14d85d24881bf00100000001000004881af001000000010" +
	"000049c1ec04480fbae03f41ffe3c3f2410f109f00100000f2440f11a4240010" +
	"000066410f28c9f20f58c1f2440f5cd2f2410f59dbf20f5ee5f20f51f766450f" +
	"57c066410f54ce66410f2ed5660f3a0bdc01f24c0f2cd1f24c0f2ac866490f6e" +
	"d0664c0f7ed9"

func TestSSEMemoryOperands(t *testing.T) {
	var a Asm
	a.AddSDMem(4, R11, 0x1000)   // addsd xmm4, qword ptr [r11 + 0x1000]
	a.SubSDMem(9, AX, 0x1000)    // subsd xmm9, qword ptr [rax + 0x1000]
	a.MulSDMem(2, SP, 0x1000)    // mulsd xmm2, qword ptr [rsp + 0x1000]
	a.UcomisdMem(1, R12, 0x1000) // ucomisd xmm1, qword ptr [r12 + 0x1000]
	check(t, &a, "f2410f58a300100000 f2440f5c8800100000 f20f59942400100000 66410f2e8c2400100000")
}

func TestIntegerArithmetic(t *testing.T) {
	var a Asm
	a.Imul(R9, BX)         // imul r9, rbx
	a.Imul(AX, R13)        // imul rax, r13
	a.Neg(R8)              // neg r8
	a.Neg(CX)              // neg rcx
	a.Div(R13)             // div r13
	a.Div(CX)              // div rcx
	a.And(R9, BX)          // and r9, rbx
	a.Or(AX, R13)          // or rax, r13
	a.Xor(R8, CX)          // xor r8, rcx
	a.Not(R9)              // not r9
	a.Not(AX)              // not rax
	a.ShlCL(R8)            // shl r8, cl
	a.ShrCL(AX)            // shr rax, cl
	a.Idiv(R13)            // idiv r13
	a.Idiv(CX)             // idiv rcx
	a.Cqo()                // cqo
	a.IdivMem(SI, 0x1000)  // idiv qword ptr [rsi + 0x1000]
	a.IdivMem(R12, 0x1000) // idiv qword ptr [r12 + 0x1000]
	check(t, &a, "4c0fafcb 490fafc5 49f7d8 48f7d9 49f7f5 48f7f1"+
		"4921d9 4c09e8 4931c8 49f7d1 48f7d0 49d3e0 48d3e8 49f7fd 48f7f9 4899 48f7be00100000 49f7bc2400100000")
}

func TestShl(t *testing.T) {
	var a Asm
	a.Shl(R13, 4) // shl r13, 4
	a.Shl(AX, 63) // shl rax, 63
	check(t, &a, "49c1e50448c1e03f")
}

func check(t *testing.T, a *Asm, want string) {
	t.Helper()
	got, err := a.Code()
	if err != nil {
		t.Fatal(err)
	}
	w, err := hex.DecodeString(strings.ReplaceAll(want, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(w) {
		t.Fatalf("got\n%x\nwant\n%x", got, w)
	}
}
