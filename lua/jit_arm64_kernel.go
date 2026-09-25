//go:build (darwin || linux) && arm64

package lua

import (
	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/arm64"
)

// Numeric loop kernels on arm64; see jit_kernel.go.

// Kernels keep Lua registers in D8 to D31. D0 to D7 stay free as
// temporaries.
const (
	kernelFirst FReg = 8
	kernelCount      = 24
	fZero       FReg = 7 // 0.0, for the loop step's sign
)

type kernel struct {
	*kernelPlan
	labels []Label // kernel code for each body pc
}

// reg returns the floating-point register kernel k keeps Lua register r
// in.
func (k *kernel) reg(r int) FReg { return kernelFirst + FReg(k.regs[r]) }

// findKernel returns the kernel for the FORLOOP at latch, or nil when its
// loop does not qualify.
func (c *arm64Compiler) findKernel(latch int) *kernel {
	plan := planKernel(c.p, latch, kernelCount, func(k int) bool { _, ok := c.constant(k); return ok })
	if plan == nil {
		return nil
	}
	k := &kernel{kernelPlan: plan, labels: make([]Label, latch-plan.start)}
	for i := range k.labels {
		k.labels[i] = c.a.NewLabel()
	}
	return k
}

// emitKernel compiles k at the FORLOOP's pc. It falls through to the
// ordinary FORLOOP code, at normal, when the entry check fails.
func (c *arm64Compiler) emitKernel(k *kernel, normal Label) {
	a := &c.a
	fl := c.p.Code[k.latch]
	base := fl.A()
	idx, limit, step, ext := k.reg(base), k.reg(base+1), k.reg(base+2), k.reg(base+3)

	// Entry: the write barrier is off, and the live-in registers hold
	// numbers.
	a.Cbnz(rBarrier, normal)
	for _, r := range k.liveIn {
		a.Ldr(rTmp, rFrame, reg(r).off+offP)
		a.Cmp(rTmp, rNumber)
		a.BCond(NE, normal)
	}
	for _, r := range k.liveIn {
		a.LdrD(k.reg(r), rFrame, reg(r).off+offN)
	}
	a.FmovToF(fZero, ZR)
	body, latch, done := a.NewLabel(), a.NewLabel(), a.NewLabel()
	// The first FORLOOP: nothing has changed if the loop does not run.
	c.kernelStep(idx, limit, step, ext, body, c.pcs[k.latch+1])

	a.Bind(body)
	for ip := k.start; ip < k.latch; ip++ {
		a.Bind(k.labels[ip-k.start])
		ip += c.kernelInstruction(k, ip, latch)
	}
	a.Bind(latch)
	next := a.NewLabel()
	c.kernelStep(idx, limit, step, ext, next, done)
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
func (c *arm64Compiler) kernelStep(idx, limit, step, ext FReg, take, end Label) {
	a := &c.a
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
		c.storeNumber(reg(r), k.reg(r))
	}
}

// kernelOperand returns the register holding RK field, loading a constant
// into tmp.
func (c *arm64Compiler) kernelOperand(k *kernel, field int, tmp FReg) FReg {
	if !bytecode.IsConstant(field) {
		return k.reg(field)
	}
	o, _ := c.constant(bytecode.ConstantIndex(field))
	c.a.LdrD(tmp, o.base, o.off+offN)
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
	switch op := i.OpCode(); op {
	case bytecode.OpMove:
		a.Fmov(k.reg(i.A()), k.reg(i.B()))
	case bytecode.OpLoadConstant:
		o, _ := c.constant(i.Bx())
		a.LdrD(k.reg(i.A()), o.base, o.off+offN)
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv:
		b, cc, d := c.kernelOperand(k, i.B(), 0), c.kernelOperand(k, i.C(), 1), k.reg(i.A())
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
	case bytecode.OpUnaryMinus:
		a.Fneg(k.reg(i.A()), k.reg(i.B()))
	case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		t, _ := kernelJump(c.p.Code, ip, k.latch)
		b, cc := c.kernelOperand(k, i.B(), 0), c.kernelOperand(k, i.C(), 1)
		a.Fcmp(b, cc)
		var when Cond
		switch op {
		case bytecode.OpEqual:
			when = EQ
		case bytecode.OpLessThan:
			when = MI
		case bytecode.OpLessOrEqual:
			when = LS
		}
		if i.A() == 0 {
			when = negate(when)
		}
		a.BCond(when, k.target(t, latch))
		a.B(k.target(ip+2, latch))
		return 1
	case bytecode.OpJump:
		t, _ := kernelJump(c.p.Code, ip, k.latch)
		a.B(k.target(t, latch))
	}
	return 0
}
