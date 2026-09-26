//go:build (darwin || linux) && arm64

package lua

import (
	"math"

	"github.com/matjam/apogee/internal/bytecode"
	. "github.com/matjam/apogee/internal/jit/arm64"
)

// Numeric loop kernels on arm64; see jit_kernel.go.

// Kernels keep float registers in D8 to D31 and integer registers in
// X12 to X17 and X19 to X25. D0 to D7, X9 and X10 stay free as
// temporaries.
const (
	kernelFirst FReg = 8
	kernelCount      = 24
	fZero       FReg = 7 // 0.0, for the loop step's sign
)

var kernelInts = []Reg{12, 13, 14, 15, 16, 17, 19, 20, 21, 22, 23, 24, 25}

type kernel struct {
	*kernelPlan
	labels []Label // kernel code for each body pc
}

// reg returns the floating-point register kernel k keeps float register r
// in, and ireg the general-purpose one for integer register r.
func (k *kernel) reg(r int) FReg { return kernelFirst + FReg(k.regs[r]) }
func (k *kernel) ireg(r int) Reg { return kernelInts[k.regs[r]] }

// findKernels returns the kernels for the FORLOOP at latch, an integer
// loop's first, or nil when its loop does not qualify.
func (c *arm64Compiler) findKernels(latch int) []*kernel {
	var ks []*kernel
	for _, intLoop := range []bool{true, false} {
		plan := planKernel(c.p, latch, intLoop, kernelCount, len(kernelInts), func(k int) bool { _, ok := c.constant(k); return ok })
		if plan == nil {
			continue
		}
		k := &kernel{kernelPlan: plan, labels: make([]Label, latch-plan.start)}
		for i := range k.labels {
			k.labels[i] = c.a.NewLabel()
		}
		ks = append(ks, k)
	}
	return ks
}

// emitKernel compiles k at the FORLOOP's pc. It falls through to normal,
// the next kernel or the ordinary FORLOOP code, when the entry check
// fails.
func (c *arm64Compiler) emitKernel(k *kernel, normal Label) {
	a := &c.a
	base := c.p.Code[k.latch].A()

	// Entry: the write barrier is off, and the live-in registers hold
	// numbers of their types.
	a.Cbnz(rBarrier, normal)
	for _, r := range k.liveIn {
		a.Ldr(rTmp, rFrame, reg(r).off+offP)
		if k.types[r] == kindInt {
			a.Cmp(rTmp, rInteger)
		} else {
			a.Cmp(rTmp, rNumber)
		}
		a.BCond(NE, normal)
	}
	for _, r := range k.liveIn {
		if k.types[r] == kindInt {
			a.Ldr(k.ireg(r), rFrame, reg(r).off+offN)
		} else {
			a.LdrD(k.reg(r), rFrame, reg(r).off+offN)
		}
	}
	a.FmovToF(fZero, ZR)
	counter := offKernels
	if k.intLoop {
		counter += 8
	}
	a.Ldr(rTmp, rCtx, counter)
	a.AddImm(rTmp, rTmp, 1)
	a.Str(rTmp, rCtx, counter)
	body, latch, done := a.NewLabel(), a.NewLabel(), a.NewLabel()
	// The first FORLOOP: nothing has changed if the loop does not run.
	c.kernelStep(k, base, body, c.pcs[k.latch+1])

	a.Bind(body)
	for ip := k.start; ip < k.latch; ip++ {
		a.Bind(k.labels[ip-k.start])
		ip += c.kernelInstruction(k, ip, latch)
	}
	a.Bind(latch)
	next := a.NewLabel()
	c.kernelStep(k, base, next, done)
	a.Bind(next)
	// Spend budget; when it runs out, write back and resume at the body,
	// as the interpreter would after this FORLOOP.
	out := a.NewLabel()
	a.SubsImm(rBudget, rBudget, 1)
	a.BCond(EQ, out)
	a.B(body)
	a.Bind(out)
	c.flush(k)
	if c.budget[k.start] < 0 {
		c.budget[k.start] = a.NewLabel()
	}
	a.B(c.budget[k.start])
	a.Bind(done)
	c.flush(k)
	a.B(c.pcs[k.latch+1])
}

// kernelStep is FORLOOP on registers: it advances the index and branches
// to take, having set the external index, or to end.
func (c *arm64Compiler) kernelStep(k *kernel, base int, take, end Label) {
	a := &c.a
	if k.intLoop {
		idx, count, step, ext := k.ireg(base), k.ireg(base+1), k.ireg(base+2), k.ireg(base+3)
		a.Cbz(count, end)
		a.SubImm(count, count, 1)
		a.Add(idx, idx, step)
		a.Mov(ext, idx)
		a.B(take)
		return
	}
	idx, limit, step, ext := k.reg(base), k.reg(base+1), k.reg(base+2), k.reg(base+3)
	positive, notPositive, yes := a.NewLabel(), a.NewLabel(), a.NewLabel()
	a.Fadd(0, idx, step)
	a.Fcmp(step, fZero)
	a.BCond(GT, positive)
	a.BCond(LS, notPositive)
	a.B(end) // NaN step
	a.Bind(notPositive)
	a.Fcmp(limit, 0)
	a.BCond(LS, yes)
	a.B(end)
	a.Bind(positive)
	a.Fcmp(0, limit)
	a.BCond(LS, yes)
	a.B(end)
	a.Bind(yes)
	a.Fmov(idx, 0)
	a.Fmov(ext, 0)
	a.B(take)
}

// flush writes the kernel's written registers back to the frame.
func (c *arm64Compiler) flush(k *kernel) {
	for _, r := range k.writtenOnce() {
		if k.types[r] == kindInt {
			c.storeInteger(reg(r), k.ireg(r))
		} else {
			c.storeNumber(reg(r), k.reg(r))
		}
	}
}

// floatOperand returns a floating-point register holding RK field as a
// float: its kernel register, or tmp, into which it loads a constant or
// converts an integer.
func (c *arm64Compiler) floatOperand(k *kernel, field int, tmp FReg) FReg {
	a := &c.a
	if !bytecode.IsConstant(field) {
		if k.types[field] == kindInt {
			a.Scvtf(tmp, k.ireg(field))
			return tmp
		}
		return k.reg(field)
	}
	kk := bytecode.ConstantIndex(field)
	if v := c.p.Constants[kk]; v.isInteger() {
		a.MovImm(rTmp, math.Float64bits(float64(v.i())))
		a.FmovToF(tmp, rTmp)
		return tmp
	}
	o, _ := c.constant(kk)
	a.LdrD(tmp, o.base, o.off+offN)
	return tmp
}

// intOperand returns a general-purpose register holding the integer RK
// field: its kernel register, or tmp, into which it loads a constant.
func (c *arm64Compiler) intOperand(k *kernel, field int, tmp Reg) Reg {
	if !bytecode.IsConstant(field) {
		return k.ireg(field)
	}
	o, _ := c.constant(bytecode.ConstantIndex(field))
	c.a.Ldr(tmp, o.base, o.off+offN)
	return tmp
}

// target returns the kernel label for pc t in k's body, or latch.
func (k *kernel) target(t int, latch Label) Label {
	if t == k.latch {
		return latch
	}
	return k.labels[t-k.start]
}

// kernelInstruction compiles the body instruction at ip for k and returns
// how many extra code words it consumed.
func (c *arm64Compiler) kernelInstruction(k *kernel, ip int, latch Label) int {
	a := &c.a
	i := c.p.Code[ip]
	p := c.p
	switch op := i.OpCode(); op {
	case bytecode.OpMove:
		if k.types[i.A()] == kindInt {
			a.Mov(k.ireg(i.A()), k.ireg(i.B()))
		} else {
			a.Fmov(k.reg(i.A()), k.reg(i.B()))
		}
	case bytecode.OpLoadConstant:
		o, _ := c.constant(i.Bx())
		if k.types[i.A()] == kindInt {
			a.Ldr(k.ireg(i.A()), o.base, o.off+offN)
		} else {
			a.LdrD(k.reg(i.A()), o.base, o.off+offN)
		}
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv:
		if k.types[i.A()] == kindInt {
			b, cc, d := c.intOperand(k, i.B(), rTmp), c.intOperand(k, i.C(), rTmp2), k.ireg(i.A())
			switch op {
			case bytecode.OpAdd:
				a.Add(d, b, cc)
			case bytecode.OpSub:
				a.Sub(d, b, cc)
			case bytecode.OpMul:
				a.Mul(d, b, cc)
			}
			break
		}
		b, cc, d := c.floatOperand(k, i.B(), 0), c.floatOperand(k, i.C(), 1), k.reg(i.A())
		switch op {
		case bytecode.OpAdd:
			a.Fadd(d, b, cc)
		case bytecode.OpSub:
			a.Fsub(d, b, cc)
		case bytecode.OpMul:
			a.Fmul(d, b, cc)
		case bytecode.OpDiv:
			a.Fdiv(d, b, cc)
		}
	case bytecode.OpMod, bytecode.OpIDiv:
		// By a nonzero constant: floor the quotient toward minus infinity,
		// and give the modulo the divisor's sign.
		divisor := p.Constants[bytecode.ConstantIndex(i.C())].i()
		b, d := k.ireg(i.B()), k.ireg(i.A())
		a.MovImm(rTmp2, uint64(divisor))
		a.Sdiv(rTmp, b, rTmp2)          // toward zero; minint / -1 wraps
		a.Msub(rExitPC, rTmp, rTmp2, b) // the remainder, with b's sign
		adjust := a.NewLabel()
		a.Cbz(rExitPC, adjust)
		if divisor > 0 {
			a.Tbz(rExitPC, 63, adjust)
		} else {
			a.Tbnz(rExitPC, 63, adjust)
		}
		if op == bytecode.OpMod {
			a.Add(rExitPC, rExitPC, rTmp2)
		} else {
			a.SubImm(rTmp, rTmp, 1)
		}
		a.Bind(adjust)
		if op == bytecode.OpMod {
			a.Mov(d, rExitPC)
		} else {
			a.Mov(d, rTmp)
		}
	case bytecode.OpUnaryMinus:
		if k.types[i.A()] == kindInt {
			a.Neg(k.ireg(i.A()), k.ireg(i.B()))
		} else {
			a.Fneg(k.reg(i.A()), k.reg(i.B()))
		}
	case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		t, _ := kernelJump(p.Code, ip, k.latch)
		var when Cond
		if k.kind(p, i.B()) == kindInt && k.kind(p, i.C()) == kindInt {
			a.Cmp(c.intOperand(k, i.B(), rTmp), c.intOperand(k, i.C(), rTmp2))
			when = map[bytecode.OpCode]Cond{bytecode.OpEqual: EQ, bytecode.OpLessThan: LT, bytecode.OpLessOrEqual: LE}[op]
		} else {
			a.Fcmp(c.floatOperand(k, i.B(), 0), c.floatOperand(k, i.C(), 1))
			when = map[bytecode.OpCode]Cond{bytecode.OpEqual: EQ, bytecode.OpLessThan: MI, bytecode.OpLessOrEqual: LS}[op]
		}
		if i.A() == 0 {
			when = negate(when)
		}
		a.BCond(when, k.target(t, latch))
		a.B(k.target(ip+2, latch))
		return 1
	case bytecode.OpJump:
		t, _ := kernelJump(p.Code, ip, k.latch)
		a.B(k.target(t, latch))
	}
	return 0
}
