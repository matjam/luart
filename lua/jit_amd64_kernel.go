//go:build (darwin || linux) && amd64

package lua

import (
	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/amd64"
)

// Numeric loop kernels on amd64; see jit_kernel.go.

// Kernels keep Lua registers in X6 to X14; X15 is Go's zero register. X0
// to X4 stay free as temporaries.
const (
	kernelFirst XReg = 6
	kernelCount      = 9
)

type kernel struct {
	*kernelPlan
	labels []Label
}

func (k *kernel) reg(r int) XReg { return kernelFirst + XReg(k.regs[r]) }

func (c *amd64Compiler) findKernel(latch int) *kernel {
	plan := planKernel(c.p, latch, kernelCount, func(k int) bool { _, ok := c.constant(k); return ok })
	if plan == nil {
		return nil
	}
	if !c.sse41 {
		for ip := plan.start; ip < latch; ip++ {
			if c.p.Code[ip].OpCode() == bytecode.OpMod {
				return nil
			}
		}
	}
	k := &kernel{kernelPlan: plan, labels: make([]Label, latch-plan.start)}
	for i := range k.labels {
		k.labels[i] = c.a.NewLabel()
	}
	return k
}

// emitKernel compiles k at the FORLOOP's pc, jumping to the ordinary
// FORLOOP code at normal when the entry check fails.
func (c *amd64Compiler) emitKernel(k *kernel, normal Label) {
	a := &c.a
	base := c.p.Code[k.latch].A()
	idx, limit, step, ext := k.reg(base), k.reg(base+1), k.reg(base+2), k.reg(base+3)

	a.CmpMem(rCtx, offBarrier, 0)
	a.J(NE, normal)
	for _, r := range k.liveIn {
		a.Load(rTmp, rFrame, reg(r).off+offP)
		a.Cmp(rTmp, rNumber)
		a.J(NE, normal)
	}
	for _, r := range k.liveIn {
		a.LoadSD(k.reg(r), rFrame, reg(r).off+offN)
	}
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
	out := a.NewLabel()
	a.SubMem(rCtx, offBudget, 1)
	a.J(E, out)
	a.Jmp(body)
	a.Bind(out)
	c.flush(k)
	if c.budget[k.start] < 0 {
		c.budget[k.start] = a.NewLabel()
	}
	a.Jmp(c.budget[k.start])
	a.Bind(done)
	c.flush(k)
	a.Jmp(c.pcs[k.latch+1])
}

// kernelStep is FORLOOP on registers.
func (c *amd64Compiler) kernelStep(idx, limit, step, ext XReg, take, end Label) {
	a := &c.a
	yes := a.NewLabel()
	c.forStep(idx, limit, step, yes, end)
	a.Bind(yes)
	a.MovSD(idx, 0)
	a.MovSD(ext, 0)
	a.Jmp(take)
}

func (c *amd64Compiler) flush(k *kernel) {
	for _, r := range k.writtenOnce() {
		c.storeNumber(reg(r), k.reg(r))
	}
}

func (c *amd64Compiler) kernelOperand(k *kernel, field int, tmp XReg) XReg {
	if !bytecode.IsConstant(field) {
		return k.reg(field)
	}
	o, _ := c.constant(bytecode.ConstantIndex(field))
	c.a.LoadSD(tmp, o.base, o.off+offN)
	return tmp
}

func (k *kernel) target(t int, latch Label) Label {
	if t == k.latch {
		return latch
	}
	return k.labels[t-k.start]
}

func (c *amd64Compiler) kernelInstruction(k *kernel, ip int, latch Label) int {
	a := &c.a
	i := c.p.Code[ip]
	switch op := i.OpCode(); op {
	case bytecode.OpMove:
		a.MovSD(k.reg(i.A()), k.reg(i.B()))
	case bytecode.OpLoadConstant:
		o, _ := c.constant(i.Bx())
		a.LoadSD(k.reg(i.A()), o.base, o.off+offN)
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod:
		b, cc := c.kernelOperand(k, i.B(), 0), c.kernelOperand(k, i.C(), 1)
		c.arith(op, 4, b, cc) // in X4, as the destination may be an operand
		a.MovSD(k.reg(i.A()), 4)
	case bytecode.OpUnaryMinus:
		a.MovSD(4, k.reg(i.B()))
		c.signMask(3)
		a.XorPD(4, 3)
		a.MovSD(k.reg(i.A()), 4)
	case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		t, _ := kernelJump(c.p.Code, ip, k.latch)
		b, cc := c.kernelOperand(k, i.B(), 0), c.kernelOperand(k, i.C(), 1)
		c.compare(op, i.A() != 0, b, cc, k.target(t, latch), k.target(ip+2, latch))
		return 1
	case bytecode.OpJump:
		t, _ := kernelJump(c.p.Code, ip, k.latch)
		a.Jmp(k.target(t, latch))
	}
	return 0
}
