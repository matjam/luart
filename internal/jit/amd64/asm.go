// Package amd64 encodes the subset of x86-64 instructions the JIT emits.
//
// Methods append one instruction each. Memory operands are a base register
// and a displacement, always encoded with a 32-bit displacement. Branches
// take labels, resolved by Code, always with 32-bit offsets. asm_test.go
// checks encodings against the system assembler.
package amd64

import (
	"encoding/binary"
	"fmt"
)

// Reg is a general-purpose register, RAX to R15.
type Reg uint8

const (
	AX Reg = iota
	CX
	DX
	BX
	SP
	BP
	SI
	DI
	R8
	R9
	R10
	R11
	R12
	R13
	R14
	R15
)

// XReg is an SSE register, X0 to X15.
type XReg uint8

// Cond is a condition code, as in Jcc.
type Cond uint8

const (
	O  Cond = 0x0
	NO Cond = 0x1
	B  Cond = 0x2 // below, unsigned; carry
	AE Cond = 0x3 // above or equal, unsigned; no carry
	E  Cond = 0x4
	NE Cond = 0x5
	BE Cond = 0x6
	A  Cond = 0x7
	S  Cond = 0x8
	NS Cond = 0x9
	P  Cond = 0xa // parity: unordered after UCOMISD
	NP Cond = 0xb
	L  Cond = 0xc
	GE Cond = 0xd
	LE Cond = 0xe
	G  Cond = 0xf
)

// Label marks a position in the code.
type Label int

type fixup struct {
	at    int // offset of the rel32 field
	label Label
}

// Asm accumulates instructions.
type Asm struct {
	buf    []byte
	labels []int // byte offset, or -1 while unbound
	fixups []fixup
}

// Len returns the number of bytes emitted so far.
func (a *Asm) Len() int { return len(a.buf) }

// NewLabel returns a new, unbound label.
func (a *Asm) NewLabel() Label {
	a.labels = append(a.labels, -1)
	return Label(len(a.labels) - 1)
}

// Bind binds l to the current position.
func (a *Asm) Bind(l Label) { a.labels[l] = len(a.buf) }

// Offset returns the byte offset l is bound to.
func (a *Asm) Offset(l Label) int { return a.labels[l] }

// Code resolves branches and returns the machine code.
func (a *Asm) Code() ([]byte, error) {
	for _, f := range a.fixups {
		target := a.labels[f.label]
		if target < 0 {
			return nil, fmt.Errorf("amd64: label %d not bound", f.label)
		}
		binary.LittleEndian.PutUint32(a.buf[f.at:], uint32(int32(target-(f.at+4))))
	}
	return a.buf, nil
}

func (a *Asm) byte(b ...byte) { a.buf = append(a.buf, b...) }

func (a *Asm) u32(v uint32) { a.buf = binary.LittleEndian.AppendUint32(a.buf, v) }

// rex emits a REX prefix for operand size w, ModRM reg r and ModRM rm or
// SIB base b, when one is needed. byteReg forces one, so that SIL and DIL
// rather than DH and BH are addressed.
func (a *Asm) rex(w bool, r, b uint8, byteReg bool) {
	p := byte(0x40)
	if w {
		p |= 8
	}
	if r&8 != 0 {
		p |= 4
	}
	if b&8 != 0 {
		p |= 1
	}
	if p != 0x40 || byteReg {
		a.byte(p)
	}
}

// modrmReg emits a register-direct ModRM byte.
func (a *Asm) modrmReg(r, rm uint8) { a.byte(0xc0 | (r&7)<<3 | rm&7) }

// modrmMem emits a ModRM byte, and a SIB byte for RSP or R12, addressing
// base+disp with a 32-bit displacement.
func (a *Asm) modrmMem(r uint8, base Reg, disp uint32) {
	a.byte(0x80 | (r&7)<<3 | uint8(base)&7)
	if base&7 == SP {
		a.byte(0x24)
	}
	a.u32(disp)
}

// opRR emits a 64-bit operation with opcode bytes op on register r and
// register rm.
func (a *Asm) opRR(op []byte, r, rm uint8) {
	a.rex(true, r, rm, false)
	a.byte(op...)
	a.modrmReg(r, rm)
}

func (a *Asm) opRM(w bool, op []byte, r uint8, base Reg, disp uint32) {
	a.rex(w, r, uint8(base), false)
	a.byte(op...)
	a.modrmMem(r, base, disp)
}

// MovImm loads v into rd.
func (a *Asm) MovImm(rd Reg, v uint64) {
	switch {
	case v == 0:
		a.rex(false, uint8(rd), uint8(rd), false) // xor r32, r32
		a.byte(0x31)
		a.modrmReg(uint8(rd), uint8(rd))
	case v <= 0xffffffff:
		a.rex(false, 0, uint8(rd), false) // mov r32, imm32 zero-extends
		a.byte(0xb8 | uint8(rd)&7)
		a.u32(uint32(v))
	default:
		a.rex(true, 0, uint8(rd), false)
		a.byte(0xb8 | uint8(rd)&7)
		a.buf = binary.LittleEndian.AppendUint64(a.buf, v)
	}
}

// Mov copies rs to rd.
func (a *Asm) Mov(rd, rs Reg) { a.opRR([]byte{0x89}, uint8(rs), uint8(rd)) }

// Load loads the 64-bit word at base+disp into rd.
func (a *Asm) Load(rd, base Reg, disp uint32) { a.opRM(true, []byte{0x8b}, uint8(rd), base, disp) }

// Store stores rs at base+disp.
func (a *Asm) Store(base Reg, disp uint32, rs Reg) { a.opRM(true, []byte{0x89}, uint8(rs), base, disp) }

// Load32 loads the 32-bit word at base+disp into rd, zero-extended.
func (a *Asm) Load32(rd, base Reg, disp uint32) { a.opRM(false, []byte{0x8b}, uint8(rd), base, disp) }

// Load8 loads the byte at base+disp into rd, zero-extended.
func (a *Asm) Load8(rd, base Reg, disp uint32) {
	a.opRM(false, []byte{0x0f, 0xb6}, uint8(rd), base, disp)
}

// Store8 stores the low byte of rs at base+disp.
func (a *Asm) Store8(base Reg, disp uint32, rs Reg) {
	a.rex(false, uint8(rs), uint8(base), rs >= SP && rs <= DI)
	a.byte(0x88)
	a.modrmMem(uint8(rs), base, disp)
}

// StoreZero stores a zero 64-bit word at base+disp.
func (a *Asm) StoreZero(base Reg, disp uint32) {
	a.opRM(true, []byte{0xc7}, 0, base, disp)
	a.u32(0)
}

// StoreZero8 stores a zero byte at base+disp.
func (a *Asm) StoreZero8(base Reg, disp uint32) {
	a.opRM(false, []byte{0xc6}, 0, base, disp)
	a.byte(0)
}

// Lea computes rd = base + index<<scale + disp, for scale 0 to 3.
func (a *Asm) Lea(rd, base, index Reg, scale uint8, disp uint32) {
	p := byte(0x48) | (uint8(rd)&8)>>1 | (uint8(index)&8)>>2 | (uint8(base)&8)>>3
	a.byte(p, 0x8d)
	a.byte(0x84 | (uint8(rd)&7)<<3) // mod=10, rm=100: SIB follows
	a.byte(scale<<6 | (uint8(index)&7)<<3 | uint8(base)&7)
	a.u32(disp)
}

// arith is an ALU operation: ADD, SUB or CMP.
type arith struct{ rr, ext byte }

var (
	opAdd = arith{0x01, 0}
	opSub = arith{0x29, 5}
	opCmp = arith{0x39, 7}
)

func (a *Asm) aluImm(op arith, rd Reg, imm int32) {
	a.rex(true, 0, uint8(rd), false)
	a.byte(0x81)
	a.modrmReg(op.ext, uint8(rd))
	a.u32(uint32(imm))
}

// AddImm computes rd += imm.
func (a *Asm) AddImm(rd Reg, imm int32) { a.aluImm(opAdd, rd, imm) }

// SubImm computes rd -= imm.
func (a *Asm) SubImm(rd Reg, imm int32) { a.aluImm(opSub, rd, imm) }

// CmpImm compares rd with imm.
func (a *Asm) CmpImm(rd Reg, imm int32) { a.aluImm(opCmp, rd, imm) }

// Add computes rd += rs.
func (a *Asm) Add(rd, rs Reg) { a.opRR([]byte{opAdd.rr}, uint8(rs), uint8(rd)) }

// Sub computes rd -= rs.
func (a *Asm) Sub(rd, rs Reg) { a.opRR([]byte{opSub.rr}, uint8(rs), uint8(rd)) }

// Cmp compares rd with rs, setting flags for rd - rs.
func (a *Asm) Cmp(rd, rs Reg) { a.opRR([]byte{opCmp.rr}, uint8(rs), uint8(rd)) }

// Imul computes rd *= rs, keeping the low 64 bits.
func (a *Asm) Imul(rd, rs Reg) { a.opRR([]byte{0x0f, 0xaf}, uint8(rd), uint8(rs)) }

// Neg computes rd = -rd.
func (a *Asm) Neg(rd Reg) { a.opRR([]byte{0xf7}, 3, uint8(rd)) }

// Div divides RDX:RAX by rs, unsigned, leaving the quotient in RAX and
// the remainder in RDX.
func (a *Asm) Div(rs Reg) { a.opRR([]byte{0xf7}, 6, uint8(rs)) }

// Idiv divides RDX:RAX by rs, signed, leaving the quotient, rounded
// toward zero, in RAX and the remainder in RDX. It traps on a zero
// divisor and on overflow.
func (a *Asm) Idiv(rs Reg) { a.opRR([]byte{0xf7}, 7, uint8(rs)) }

// IdivMem is Idiv by the 64-bit word at base+disp.
func (a *Asm) IdivMem(base Reg, disp uint32) { a.opRM(true, []byte{0xf7}, 7, base, disp) }

// Cqo sign-extends RAX into RDX.
func (a *Asm) Cqo() { a.byte(0x48, 0x99) }

// And, Or and Xor compute rd &= rs, rd |= rs and rd ^= rs.
func (a *Asm) And(rd, rs Reg) { a.opRR([]byte{0x21}, uint8(rs), uint8(rd)) }
func (a *Asm) Or(rd, rs Reg)  { a.opRR([]byte{0x09}, uint8(rs), uint8(rd)) }
func (a *Asm) Xor(rd, rs Reg) { a.opRR([]byte{0x31}, uint8(rs), uint8(rd)) }

// Not computes rd = ^rd.
func (a *Asm) Not(rd Reg) { a.opRR([]byte{0xf7}, 2, uint8(rd)) }

// ShlCL and ShrCL shift rd left or right, logically, by CL mod 64.
func (a *Asm) ShlCL(rd Reg) { a.opRR([]byte{0xd3}, 4, uint8(rd)) }
func (a *Asm) ShrCL(rd Reg) { a.opRR([]byte{0xd3}, 5, uint8(rd)) }

// Test sets flags for rd & rs.
func (a *Asm) Test(rd, rs Reg) { a.opRR([]byte{0x85}, uint8(rs), uint8(rd)) }

// CmpMem compares the 64-bit word at base+disp with imm.
func (a *Asm) CmpMem(base Reg, disp uint32, imm int32) {
	a.opRM(true, []byte{0x81}, 7, base, disp)
	a.u32(uint32(imm))
}

// SubMem subtracts imm from the 64-bit word at base+disp.
func (a *Asm) SubMem(base Reg, disp uint32, imm int32) {
	a.opRM(true, []byte{0x81}, 5, base, disp)
	a.u32(uint32(imm))
}

// Shl shifts rd left by n.
func (a *Asm) Shl(rd Reg, n uint8) {
	a.rex(true, 0, uint8(rd), false)
	a.byte(0xc1)
	a.modrmReg(4, uint8(rd))
	a.byte(n)
}

// Shr shifts rd right, unsigned, by n.
func (a *Asm) Shr(rd Reg, n uint8) {
	a.rex(true, 0, uint8(rd), false)
	a.byte(0xc1)
	a.modrmReg(5, uint8(rd))
	a.byte(n)
}

// Bt copies bit n of rd to the carry flag.
func (a *Asm) Bt(rd Reg, n uint8) {
	a.rex(true, 0, uint8(rd), false)
	a.byte(0x0f, 0xba)
	a.modrmReg(4, uint8(rd))
	a.byte(n)
}

// J branches to l when c holds.
func (a *Asm) J(c Cond, l Label) {
	a.byte(0x0f, 0x80|byte(c))
	a.fixups = append(a.fixups, fixup{len(a.buf), l})
	a.u32(0)
}

// Jmp branches to l.
func (a *Asm) Jmp(l Label) {
	a.byte(0xe9)
	a.fixups = append(a.fixups, fixup{len(a.buf), l})
	a.u32(0)
}

// JmpReg branches to the address in r.
func (a *Asm) JmpReg(r Reg) {
	a.rex(false, 0, uint8(r), false)
	a.byte(0xff)
	a.modrmReg(4, uint8(r))
}

// Ret returns.
func (a *Asm) Ret() { a.byte(0xc3) }

// sse emits an SSE instruction: prefix, REX, 0F, op, ModRM.
func (a *Asm) sse(prefix byte, w bool, op []byte, r, rm uint8) {
	if prefix != 0 {
		a.byte(prefix)
	}
	a.rex(w, r, rm, false)
	a.byte(0x0f)
	a.byte(op...)
	a.modrmReg(r, rm)
}

func (a *Asm) sseMem(prefix byte, op byte, r uint8, base Reg, disp uint32) {
	a.byte(prefix)
	a.rex(false, r, uint8(base), false)
	a.byte(0x0f, op)
	a.modrmMem(r, base, disp)
}

// LoadSD loads the double at base+disp into xd.
func (a *Asm) LoadSD(xd XReg, base Reg, disp uint32) { a.sseMem(0xf2, 0x10, uint8(xd), base, disp) }

// StoreSD stores the double in xs at base+disp.
func (a *Asm) StoreSD(base Reg, disp uint32, xs XReg) { a.sseMem(0xf2, 0x11, uint8(xs), base, disp) }

// MovSD copies xs to xd, all 128 bits.
func (a *Asm) MovSD(xd, xs XReg) { a.sse(0x66, false, []byte{0x28}, uint8(xd), uint8(xs)) }

// AddSD computes xd += xs.
func (a *Asm) AddSD(xd, xs XReg) { a.sse(0xf2, false, []byte{0x58}, uint8(xd), uint8(xs)) }

// SubSD computes xd -= xs.
func (a *Asm) SubSD(xd, xs XReg) { a.sse(0xf2, false, []byte{0x5c}, uint8(xd), uint8(xs)) }

// MulSD computes xd *= xs.
func (a *Asm) MulSD(xd, xs XReg) { a.sse(0xf2, false, []byte{0x59}, uint8(xd), uint8(xs)) }

// AddSDMem computes xd += the double at base+disp.
func (a *Asm) AddSDMem(xd XReg, base Reg, disp uint32) { a.sseMem(0xf2, 0x58, uint8(xd), base, disp) }

// SubSDMem computes xd -= the double at base+disp.
func (a *Asm) SubSDMem(xd XReg, base Reg, disp uint32) { a.sseMem(0xf2, 0x5c, uint8(xd), base, disp) }

// MulSDMem computes xd *= the double at base+disp.
func (a *Asm) MulSDMem(xd XReg, base Reg, disp uint32) { a.sseMem(0xf2, 0x59, uint8(xd), base, disp) }

// UcomisdMem compares xa with the double at base+disp, setting the flags
// as Ucomisd does.
func (a *Asm) UcomisdMem(xa XReg, base Reg, disp uint32) { a.sseMem(0x66, 0x2e, uint8(xa), base, disp) }

// DivSD computes xd /= xs.
func (a *Asm) DivSD(xd, xs XReg) { a.sse(0xf2, false, []byte{0x5e}, uint8(xd), uint8(xs)) }

// SqrtSD computes xd = sqrt(xs).
func (a *Asm) SqrtSD(xd, xs XReg) { a.sse(0xf2, false, []byte{0x51}, uint8(xd), uint8(xs)) }

// XorPD computes xd ^= xs.
func (a *Asm) XorPD(xd, xs XReg) { a.sse(0x66, false, []byte{0x57}, uint8(xd), uint8(xs)) }

// AndPD computes xd &= xs.
func (a *Asm) AndPD(xd, xs XReg) { a.sse(0x66, false, []byte{0x54}, uint8(xd), uint8(xs)) }

// Ucomisd compares xa with xb: ZF, PF and CF are all set when unordered;
// otherwise CF means below and ZF equal.
func (a *Asm) Ucomisd(xa, xb XReg) { a.sse(0x66, false, []byte{0x2e}, uint8(xa), uint8(xb)) }

// RoundSD rounds xs into xd with SSE4.1 ROUNDSD; mode 1 rounds toward
// minus infinity and 2 toward plus infinity.
func (a *Asm) RoundSD(xd, xs XReg, mode byte) {
	a.sse(0x66, false, []byte{0x3a, 0x0b}, uint8(xd), uint8(xs))
	a.byte(mode)
}

// Cvttsd2si converts xs to a signed integer in rd, truncating; out of
// range values give 1<<63.
func (a *Asm) Cvttsd2si(rd Reg, xs XReg) { a.sse(0xf2, true, []byte{0x2c}, uint8(rd), uint8(xs)) }

// Cvtsi2sd converts the signed integer in rs to a double in xd.
func (a *Asm) Cvtsi2sd(xd XReg, rs Reg) { a.sse(0xf2, true, []byte{0x2a}, uint8(xd), uint8(rs)) }

// MovqToX copies the bits of rs to xd.
func (a *Asm) MovqToX(xd XReg, rs Reg) { a.sse(0x66, true, []byte{0x6e}, uint8(xd), uint8(rs)) }

// MovqFromX copies the bits of xs to rd.
func (a *Asm) MovqFromX(rd Reg, xs XReg) { a.sse(0x66, true, []byte{0x7e}, uint8(xs), uint8(rd)) }
