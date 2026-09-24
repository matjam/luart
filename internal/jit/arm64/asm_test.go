package arm64

import (
	"encoding/binary"
	"testing"
)

// The expected words come from clang -c -arch arm64 on the assembly in each
// case's comment.
func TestEncodings(t *testing.T) {
	var a Asm
	a.MovImm(3, 0x0007_0000_abcd_1234) // movz x3,#0x1234; movk x3,#0xabcd,lsl #16; movk x3,#0x7,lsl #48
	a.Mov(5, 9)                        // mov x5, x9
	a.AddImm(1, 2, 4095)               // add x1, x2, #4095
	a.SubImm(4, 4, 1)                  // sub x4, x4, #1
	a.SubsImm(7, 8, 12)                // subs x7, x8, #12
	a.CmpImm(2, 0)                     // cmp x2, #0
	a.Cmp(3, 4)                        // cmp x3, x4
	a.AddShifted(6, 1, 2, 4)           // add x6, x1, x2, lsl #4
	a.Ldr(10, 1, 32760)                // ldr x10, [x1, #32760]
	a.Str(11, 2, 8)                    // str x11, [x2, #8]
	a.LdrW(12, 3, 16380)               // ldr w12, [x3, #16380]
	a.StrW(13, 4, 4)                   // str w13, [x4, #4]
	a.LdrD(0, 1, 24)                   // ldr d0, [x1, #24]
	a.StrD(31, 2, 32760)               // str d31, [x2, #32760]
	a.Ret()                            // ret
	a.Br(16)                           // br x16
	a.Fadd(1, 2, 3)                    // fadd d1, d2, d3
	a.Fsub(4, 5, 6)                    // fsub d4, d5, d6
	a.Fmul(7, 8, 9)                    // fmul d7, d8, d9
	a.Fdiv(10, 11, 12)                 // fdiv d10, d11, d12
	a.Fneg(13, 14)                     // fneg d13, d14
	a.Fabs(15, 16)                     // fabs d15, d16
	a.Fsqrt(17, 18)                    // fsqrt d17, d18
	a.Frintm(19, 20)                   // frintm d19, d20
	a.Frintp(21, 22)                   // frintp d21, d22
	a.Fcmp(23, 24)                     // fcmp d23, d24
	a.FmovToF(25, 26)                  // fmov d25, x26
	a.FmovFromF(27, 28)                // fmov x27, d28
	a.Fmov(29, 30)                     // fmov d29, d30
	want := []uint32{
		0xd2824683, 0xf2b579a3, 0xf2e000e3, 0xaa0903e5, 0x913ffc41, 0xd1000484,
		0xf1003107, 0xf100005f, 0xeb04007f, 0x8b021026, 0xf97ffc2a, 0xf900044b,
		0xb97ffc6c, 0xb900048d, 0xfd400c20, 0xfd3ffc5f, 0xd65f03c0, 0xd61f0200,
		0x1e632841, 0x1e6638a4, 0x1e690907, 0x1e6c196a, 0x1e6141cd, 0x1e60c20f,
		0x1e61c251, 0x1e654293, 0x1e64c2d5, 0x1e7822e0, 0x9e670359, 0x9e66039b,
		0x1e6043dd,
	}
	check(t, &a, want)
}

func TestBranches(t *testing.T) {
	var a Asm
	fwd, back := a.NewLabel(), a.NewLabel()
	a.B(fwd)         // b 1f
	a.BCond(NE, fwd) // b.ne 1f
	a.Cbz(3, fwd)    // cbz x3, 1f
	a.Cbnz(4, back)  // cbnz x4, 2f
	a.Bind(fwd)      // 1:
	a.Ret()
	a.BCond(MI, fwd) // b.mi 1b
	a.Bind(back)     // 2:
	a.Ret()
	want := []uint32{0x14000004, 0x54000061, 0xb4000043, 0xb5000064, 0xd65f03c0, 0x54ffffe4, 0xd65f03c0}
	check(t, &a, want)
}

func TestTestBranches(t *testing.T) {
	var a Asm
	l := a.NewLabel()
	a.Tbz(13, 0, l)  // tbz x13, #0, 1f
	a.Tbnz(7, 63, l) // tbnz x7, #63, 1f
	a.Bind(l)        // 1:
	a.Ret()
	a.Tbz(2, 3, l) // tbz w2, #3, 1b
	check(t, &a, []uint32{0x3600004d, 0xb7f80027, 0xd65f03c0, 0x361fffe2})
}

func TestMovImm(t *testing.T) {
	tests := []struct {
		v    uint64
		want []uint32
	}{
		{0, []uint32{0xd2800000}},
		{0xffff, []uint32{0xd29fffe0}},
		{1 << 48, []uint32{0xd2e00020}},
	}
	for _, tt := range tests {
		var a Asm
		a.MovImm(0, tt.v)
		check(t, &a, tt.want)
	}
}

func TestUnboundLabel(t *testing.T) {
	var a Asm
	a.B(a.NewLabel())
	if _, err := a.Code(); err == nil {
		t.Fatal("Code succeeded with an unbound label")
	}
}

func check(t *testing.T, a *Asm, want []uint32) {
	t.Helper()
	code, err := a.Code()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 4*len(want) {
		t.Fatalf("got %d instructions, want %d", len(code)/4, len(want))
	}
	for i, w := range want {
		if got := binary.LittleEndian.Uint32(code[4*i:]); got != w {
			t.Errorf("instruction %d = %#08x, want %#08x", i, got, w)
		}
	}
}
