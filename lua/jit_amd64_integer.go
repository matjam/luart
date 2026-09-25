//go:build (darwin || linux) && amd64

package lua

import (
	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/amd64"
)

// divide compiles % and // of two integers, as IntMod and IntFloorDiv
// compute them, and // of floats where ROUNDSD is available. A zero
// divisor exits, for Go to raise the error, as does % of floats, which Go
// computes with fmod. IDIV divides RDX:RAX, so rAddr and rTmp are used.
func (c *amd64Compiler) divide(ip int, op bytecode.OpCode, i bytecode.Instruction) {
	a := &c.a
	b, kb, okB := c.rkArith(i.B())
	cc, kc, okC := c.rkArith(i.C())
	floatsExit := op == bytecode.OpMod || !c.sse41
	if !okB || !okC || floatsExit && (kb == kindFloat || kc == kindFloat) {
		c.exitAlways(ip)
		return
	}
	dst := reg(i.A())
	c.guardStore(dst, noReg, ip)
	floats, done := a.NewLabel(), a.NewLabel()
	if floatsExit {
		floats = c.exit(ip)
	}
	if kb != kindFloat && kc != kindFloat {
		c.branchUnlessInteger(b, kb, floats)
		c.branchUnlessInteger(cc, kc, floats)
		a.Load(rP, b.base, b.off+offN)
		a.Load(rN, cc.base, cc.off+offN)
		a.Test(rN, rN)
		a.J(E, c.exit(ip)) // 'n%0' or 'n//0'
		minusOne, adjust := a.NewLabel(), a.NewLabel()
		a.CmpImm(rN, -1)
		a.J(E, minusOne) // IDIV would trap on minint / -1
		a.Mov(AX, rP)
		a.Cqo()
		a.Idiv(rN)     // AX: the quotient, toward zero; DX: the remainder
		a.Test(DX, DX) // exact: nothing to adjust
		a.J(E, adjust)
		a.Mov(rTmp2, DX) // signs differ: floor is one lower, and the
		a.Xor(rTmp2, rN) // modulo takes the divisor's sign
		a.J(NS, adjust)
		if op == bytecode.OpMod {
			a.Add(DX, rN)
		} else {
			a.SubImm(AX, 1)
		}
		a.Bind(adjust)
		if op == bytecode.OpMod {
			c.storeInteger(dst, DX)
		} else {
			c.storeInteger(dst, AX)
		}
		a.Jmp(done)
		a.Bind(minusOne) // n % -1 is 0 and n // -1 is -n, wrapping for minint
		if op == bytecode.OpMod {
			a.MovImm(rP, 0)
		} else {
			a.Neg(rP)
		}
		c.storeInteger(dst, rP)
		a.Jmp(done)
	}
	if !floatsExit {
		a.Bind(floats)
		c.loadFloat(0, b, kb, false, ip)
		c.loadFloat(1, cc, kc, false, ip)
		a.DivSD(0, 1)
		a.RoundSD(0, 0, 1) // toward minus infinity
		c.storeNumber(dst, 0)
	}
	a.Bind(done)
}

// bitwise compiles a bitwise operator on two integers, or one for ~. A
// float operand exits, for Go to convert it or raise the error. Shifts
// count in CL, which is rTmp2.
func (c *amd64Compiler) bitwise(ip int, i bytecode.Instruction, op bytecode.ArithOp) {
	a := &c.a
	b, kb, okB := c.rkArith(i.B())
	cc, kc, okC := b, kb, okB
	if op != bytecode.ArithBNot {
		cc, kc, okC = c.rkArith(i.C())
	}
	if !okB || !okC || kb == kindFloat || kc == kindFloat {
		c.exitAlways(ip)
		return
	}
	dst := reg(i.A())
	c.guardStore(dst, noReg, ip)
	c.branchUnlessInteger(b, kb, c.exit(ip))
	c.branchUnlessInteger(cc, kc, c.exit(ip))
	a.Load(rP, b.base, b.off+offN)
	a.Load(rN, cc.base, cc.off+offN)
	switch op {
	case bytecode.ArithBAnd:
		a.And(rP, rN)
	case bytecode.ArithBOr:
		a.Or(rP, rN)
	case bytecode.ArithBXor:
		a.Xor(rP, rN)
	case bytecode.ArithBNot:
		a.Not(rP)
	case bytecode.ArithShl, bytecode.ArithShr:
		// ShiftLeft: logical, the other way for a negative count, and 0
		// for a count of 64 or more either way.
		if op == bytecode.ArithShr {
			a.Neg(rN)
		}
		left, right, done := a.NewLabel(), a.NewLabel(), a.NewLabel()
		a.CmpImm(rN, 64)
		a.J(B, left)
		a.Mov(rTmp2, rN)
		a.Neg(rTmp2)
		a.CmpImm(rTmp2, 64)
		a.J(B, right)
		a.MovImm(rP, 0)
		a.Jmp(done)
		a.Bind(left)
		a.Mov(rTmp2, rN)
		a.ShlCL(rP)
		a.Jmp(done)
		a.Bind(right)
		a.ShrCL(rP)
		a.Bind(done)
	}
	c.storeInteger(dst, rP)
}
