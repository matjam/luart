//go:build (darwin || linux) && amd64

package lua

import (
	"math"

	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/amd64"
)

// Numeric loop kernels on amd64; see jit_kernel.go.

// Kernels keep float registers in X6 to X14 (X15 is Go's zero register)
// and integer registers in R8 to R13 and CX. X0 to X4, AX and DX stay free
// as temporaries: IDIV divides RDX:RAX.
const (
	kernelFirst XReg = 6
	kernelCount      = 9
)

var kernelInts = []Reg{R8, R9, R10, R11, R12, R13, CX}

type kernel struct {
	*kernelPlan
	labels []Label
}

func (k *kernel) reg(r int) XReg { return kernelFirst + XReg(k.regs[r]) }
func (k *kernel) ireg(r int) Reg { return kernelInts[k.regs[r]] }

// findKernels returns the kernels for the FORLOOP at latch, an integer
// loop's first, or nil when its loop does not qualify.
func (c *amd64Compiler) findKernels(latch int) []*kernel {
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

// emitKernel compiles k at the FORLOOP's pc, jumping to normal, the next
// kernel or the ordinary FORLOOP code, when the entry check fails.
func (c *amd64Compiler) emitKernel(k *kernel, normal Label) {
	a := &c.a
	base := c.p.Code[k.latch].A()

	a.CmpMem(rCtx, offBarrier, 0)
	a.J(NE, normal)
	for _, r := range k.liveIn {
		a.Load(rTmp, rFrame, reg(r).off+offP)
		a.Sub(rTmp, rNumber) // 0 for a float, 1 for an integer
		if k.types[r] == kindInt {
			a.CmpImm(rTmp, 1)
		} else {
			a.Test(rTmp, rTmp)
		}
		a.J(NE, normal)
	}
	for _, r := range k.liveIn {
		if k.types[r] == kindInt {
			a.Load(k.ireg(r), rFrame, reg(r).off+offN)
		} else {
			a.LoadSD(k.reg(r), rFrame, reg(r).off+offN)
		}
	}
	counter := offKernels
	if k.intLoop {
		counter += 8
	}
	a.SubMem(rCtx, counter, -1)
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
func (c *amd64Compiler) kernelStep(k *kernel, base int, take, end Label) {
	a := &c.a
	if k.intLoop {
		idx, count, step, ext := k.ireg(base), k.ireg(base+1), k.ireg(base+2), k.ireg(base+3)
		a.Test(count, count)
		a.J(E, end)
		a.SubImm(count, 1)
		a.Add(idx, step)
		a.Mov(ext, idx)
		a.Jmp(take)
		return
	}
	idx, limit, step, ext := k.reg(base), k.reg(base+1), k.reg(base+2), k.reg(base+3)
	yes := a.NewLabel()
	c.forStep(idx, limit, step, yes, end)
	a.Bind(yes)
	a.MovSD(idx, 0)
	a.MovSD(ext, 0)
	a.Jmp(take)
}

func (c *amd64Compiler) flush(k *kernel) {
	for _, r := range k.writtenOnce() {
		if k.types[r] == kindInt {
			c.storeInteger(reg(r), k.ireg(r))
		} else {
			c.storeNumber(reg(r), k.reg(r))
		}
	}
}

// floatOperand returns an SSE register holding RK field as a float: its
// kernel register, or tmp, into which it loads a constant or converts an
// integer.
func (c *amd64Compiler) floatOperand(k *kernel, field int, tmp XReg) XReg {
	a := &c.a
	if !bytecode.IsConstant(field) {
		if k.types[field] == kindInt {
			a.Cvtsi2sd(tmp, k.ireg(field))
			return tmp
		}
		return k.reg(field)
	}
	kk := bytecode.ConstantIndex(field)
	if v := c.p.Constants[kk]; v.isInteger() {
		a.MovImm(rTmp, math.Float64bits(float64(v.i())))
		a.MovqToX(tmp, rTmp)
		return tmp
	}
	o, _ := c.constant(kk)
	a.LoadSD(tmp, o.base, o.off+offN)
	return tmp
}

// intOperand returns a register holding the integer RK field: its kernel
// register, or tmp, into which it loads a constant.
func (c *amd64Compiler) intOperand(k *kernel, field int, tmp Reg) Reg {
	if !bytecode.IsConstant(field) {
		return k.ireg(field)
	}
	o, _ := c.constant(bytecode.ConstantIndex(field))
	c.a.Load(tmp, o.base, o.off+offN)
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
	p := c.p
	i := p.Code[ip]
	switch op := i.OpCode(); op {
	case bytecode.OpMove:
		if k.types[i.A()] == kindInt {
			a.Mov(k.ireg(i.A()), k.ireg(i.B()))
		} else {
			a.MovSD(k.reg(i.A()), k.reg(i.B()))
		}
	case bytecode.OpLoadConstant:
		o, _ := c.constant(i.Bx())
		if k.types[i.A()] == kindInt {
			a.Load(k.ireg(i.A()), o.base, o.off+offN)
		} else {
			a.LoadSD(k.reg(i.A()), o.base, o.off+offN)
		}
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv:
		if k.types[i.A()] == kindInt {
			// In AX, as the destination may be an operand.
			if b := c.intOperand(k, i.B(), AX); b != AX {
				a.Mov(AX, b)
			}
			cc := c.intOperand(k, i.C(), DX)
			switch op {
			case bytecode.OpAdd:
				a.Add(AX, cc)
			case bytecode.OpSub:
				a.Sub(AX, cc)
			case bytecode.OpMul:
				a.Imul(AX, cc)
			}
			a.Mov(k.ireg(i.A()), AX)
			break
		}
		b, cc := c.floatOperand(k, i.B(), 0), c.floatOperand(k, i.C(), 1)
		c.arith(op, 4, b, cc) // in X4, as the destination may be an operand
		a.MovSD(k.reg(i.A()), 4)
	case bytecode.OpMod, bytecode.OpIDiv:
		// By a nonzero constant: floor the quotient toward minus infinity,
		// and give the modulo the divisor's sign.
		kk := bytecode.ConstantIndex(i.C())
		divisor := p.Constants[kk].i()
		b, d := k.ireg(i.B()), k.ireg(i.A())
		if divisor == -1 { // IDIV would trap on minint / -1
			if op == bytecode.OpMod {
				a.MovImm(d, 0)
			} else {
				a.Mov(d, b)
				a.Neg(d)
			}
			break
		}
		o, _ := c.constant(kk)
		a.Mov(AX, b)
		a.Cqo()
		a.IdivMem(o.base, o.off+offN) // AX: toward zero; DX: b's sign
		adjust := a.NewLabel()
		a.Test(DX, DX)
		a.J(E, adjust)
		if divisor > 0 {
			a.J(NS, adjust)
		} else {
			a.J(S, adjust)
		}
		if op == bytecode.OpMod {
			a.AddImm(DX, int32(divisor))
		} else {
			a.SubImm(AX, 1)
		}
		a.Bind(adjust)
		if op == bytecode.OpMod {
			a.Mov(d, DX)
		} else {
			a.Mov(d, AX)
		}
	case bytecode.OpUnaryMinus:
		if k.types[i.A()] == kindInt {
			a.Mov(k.ireg(i.A()), k.ireg(i.B()))
			a.Neg(k.ireg(i.A()))
			break
		}
		a.MovSD(4, k.reg(i.B()))
		c.signMask(3)
		a.XorPD(4, 3)
		a.MovSD(k.reg(i.A()), 4)
	case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		t, _ := kernelJump(p.Code, ip, k.latch)
		yes, no := k.target(t, latch), k.target(ip+2, latch)
		if k.kind(p, i.B()) == kindInt && k.kind(p, i.C()) == kindInt {
			a.Cmp(c.intOperand(k, i.B(), AX), c.intOperand(k, i.C(), DX))
			when := map[bytecode.OpCode]Cond{bytecode.OpEqual: E, bytecode.OpLessThan: L, bytecode.OpLessOrEqual: LE}[op]
			if i.A() == 0 {
				when ^= 1
			}
			a.J(when, yes)
			a.Jmp(no)
			return 1
		}
		b, cc := c.floatOperand(k, i.B(), 0), c.floatOperand(k, i.C(), 1)
		c.compare(op, i.A() != 0, b, cc, yes, no)
		return 1
	case bytecode.OpJump:
		t, _ := kernelJump(p.Code, ip, k.latch)
		a.Jmp(k.target(t, latch))
	}
	return 0
}
