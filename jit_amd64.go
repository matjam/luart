//go:build (darwin || linux) && amd64

package luart

import (
	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/amd64"
)

const jitSupported = true

// Registers the generated code keeps for its whole run. The budget, the
// write barrier flag and the upvalues stay in the context, as amd64 has
// too few registers to keep them all.
const (
	rCtx    Reg = DI  // *jitContext
	rFrame  Reg = BX  // &frame[0]
	rConst  Reg = SI  // &constants[0]
	rNumber Reg = R15 // numberPtr(), the p of every number
	rTmp    Reg = AX
	rTmp2   Reg = CX
	rAddr   Reg = DX  // an upvalue's address, or a table slot's
	rP      Reg = R8  // a value's p, while it is copied
	rN      Reg = R9  // a value's second word, while it is copied
	rT      Reg = R10 // a table, while it is indexed
	rT2     Reg = R11 // a second table: a metatable or its __index
	rCache  Reg = R12 // the instruction's *fieldCache
	rIdx    Reg = R13 // a slot or array index
	rLen        = rTmp2
	rSlot       = rAddr
)

// maxOffset bounds frame and constant offsets, which are 32-bit
// displacements.
const maxOffset = 1 << 30

// amd64Compiler translates a prototype into amd64 code. Like the arm64
// compiler, each instruction's code checks everything it needs before it
// writes anything.
type amd64Compiler struct {
	a       Asm
	p       *prototype
	code    []bytecode.Instruction
	pcs     []Label
	exits   []Label
	budget  []Label
	goCall  []Label // exits at a CALL of a Go function, created on demand
	numCall []Label // exits at a CALL of a number function, created on demand
	notLua  []Label // a CALL's out-of-line code for callees other than Lua closures
	always  []bool
	sse41   bool // ROUNDSD is available, for floor and modulo
	ip      int  // the instruction being compiled, for intrinsics' exits
}

func compileJIT(p *prototype) (code []byte, offsets []int32, entries []int, kernels int) {
	if len(p.Code) > 1<<16 {
		return nil, nil, nil, 0
	}
	c := &amd64Compiler{p: p, code: p.jitOrig, sse41: HasSSE41()}
	c.pcs = make([]Label, len(c.code))
	c.exits = make([]Label, len(c.code))
	c.budget = make([]Label, len(c.code))
	c.goCall = make([]Label, len(c.code))
	c.numCall = make([]Label, len(c.code))
	c.notLua = make([]Label, len(c.code))
	c.always = make([]bool, len(c.code))
	for i := range c.pcs {
		c.pcs[i], c.exits[i], c.budget[i] = c.a.NewLabel(), -1, -1
		c.goCall[i], c.numCall[i], c.notLua[i] = -1, -1, -1
	}
	c.prologue()
	loops := map[int]*kernel{}
	for ip, i := range c.p.Code {
		if i.OpCode() == bytecode.OpForLoop && !isExtraArg(c.p.Code, ip) {
			if k := c.findKernel(ip); k != nil {
				loops[ip] = k
			}
		}
	}
	for ip := 0; ip < len(c.code); ip++ {
		c.a.Bind(c.pcs[ip])
		if k := loops[ip]; k != nil {
			normal := c.a.NewLabel()
			c.emitKernel(k, normal)
			c.a.Bind(normal)
		}
		ip += c.instruction(ip)
	}
	c.stubs()
	code, err := c.a.Code()
	if err != nil {
		return nil, nil, nil, 0
	}
	offsets = make([]int32, len(c.code))
	for i, l := range c.pcs {
		offsets[i] = int32(c.a.Offset(l))
		if isExtraArg(c.p.Code, i) {
			offsets[i] = -1
		}
	}
	exits := make([]bool, len(c.exits))
	for i, l := range c.exits {
		exits[i] = l >= 0
	}
	return code, offsets, jitEntries(c.p, exits, c.always), len(loops)
}

// prologue loads the fixed registers and jumps to ctx.target.
func (c *amd64Compiler) prologue() {
	a := &c.a
	a.Load(rFrame, rCtx, offFrame)
	a.Load(rConst, rCtx, offConstants)
	a.MovImm(rNumber, uint64(uintptr(numberPtr())))
	a.Load(rTmp, rCtx, offTarget)
	a.JmpReg(rTmp)
}

// stubs emits the exits that instructions branch to.
func (c *amd64Compiler) stubs() {
	a := &c.a
	for ip, l := range c.notLua {
		if l >= 0 {
			a.Bind(l)
			c.goCallee(ip, c.code[ip])
			a.Jmp(c.exit(ip))
		}
	}
	for ip, l := range c.exits {
		if l >= 0 {
			a.Bind(l)
			c.exitWith(ip, jitExitInstruction)
		}
	}
	for ip, l := range c.budget {
		if l >= 0 {
			a.Bind(l)
			c.exitWith(ip, jitExitBudget)
		}
	}
	for _, calls := range []struct {
		labels []Label
		reason uint64
	}{{c.goCall, jitExitCallGo}, {c.numCall, jitExitCallNumber}} {
		for ip, l := range calls.labels {
			if l >= 0 {
				a.Bind(l)
				fn := reg(c.code[ip].A())
				a.Load(rTmp, fn.base, fn.off+offP)
				a.Store(rCtx, offCallee, rTmp)
				a.Store(rCtx, offFrame, rFrame) // compiled calls may have moved it
				c.exitWith(ip, calls.reason)
			}
		}
	}
}

func (c *amd64Compiler) exitWith(ip int, reason uint64) {
	c.a.MovImm(rTmp, uint64(ip))
	c.a.Store(rCtx, offExitPC, rTmp)
	c.a.MovImm(rTmp, reason)
	c.a.Ret()
}

// exit returns the label that resumes the interpreter at ip.
func (c *amd64Compiler) exit(ip int) Label {
	if c.exits[ip] < 0 {
		c.exits[ip] = c.a.NewLabel()
	}
	return c.exits[ip]
}

// goCallExit returns the label that exits at the CALL at ip for runJIT to
// call the Go function in its register.
func (c *amd64Compiler) goCallExit(ip int) Label {
	if c.goCall[ip] < 0 {
		c.goCall[ip] = c.a.NewLabel()
	}
	return c.goCall[ip]
}

// numCallExit returns the label that exits at the CALL at ip for runJIT to
// call the number function in its register.
func (c *amd64Compiler) numCallExit(ip int) Label {
	if c.numCall[ip] < 0 {
		c.numCall[ip] = c.a.NewLabel()
	}
	return c.numCall[ip]
}

// exitAlways compiles the instruction at ip as an exit.
func (c *amd64Compiler) exitAlways(ip int) {
	c.always[ip] = true
	c.a.Jmp(c.exit(ip))
}

// operand locates a value in memory: its base register and byte offset.
type operand struct {
	base Reg
	off  uint32
}

func reg(r int) operand { return operand{rFrame, uint32(r) * valueSize} }

func (c *amd64Compiler) constant(k int) (operand, bool) {
	if uint64(k)*uint64(valueSize) >= maxOffset {
		return operand{}, false
	}
	return operand{rConst, uint32(k) * valueSize}, true
}

// rkNumber returns the operand for an RK field that must be a number, and
// false for a constant that is not one.
func (c *amd64Compiler) rkNumber(field int) (operand, bool) {
	if !bytecode.IsConstant(field) {
		return reg(field), true
	}
	k := bytecode.ConstantIndex(field)
	if !c.p.Constants[k].isNumber() {
		return operand{}, false
	}
	return c.constant(k)
}

// guardNumber exits at ip unless o holds a number.
func (c *amd64Compiler) guardNumber(o operand, ip int) {
	if o.base == rConst {
		return
	}
	c.a.Load(rTmp, o.base, o.off+offP)
	c.a.Cmp(rTmp, rNumber)
	c.a.J(NE, c.exit(ip))
}

// guardScalar exits at ip unless p, a value's first word, is nil, a number
// or a boolean. It uses rTmp, so p must be another register.
func (c *amd64Compiler) guardScalar(p Reg, ip int) {
	a := &c.a
	ok := a.NewLabel()
	a.Test(p, p)
	a.J(E, ok)
	a.Cmp(p, rNumber)
	a.J(E, ok)
	a.MovImm(rTmp, uint64(uintptr(boolPtr())))
	a.Cmp(p, rTmp)
	a.J(NE, c.exit(ip))
	a.Bind(ok)
}

// guardStore exits at ip if the write barrier is on and dst holds a heap
// pointer, or newP, when it is not noReg, is one.
func (c *amd64Compiler) guardStore(dst operand, newP Reg, ip int) {
	a := &c.a
	done := a.NewLabel()
	a.CmpMem(rCtx, offBarrier, 0)
	a.J(E, done)
	if newP != noReg {
		c.guardScalar(newP, ip)
	}
	a.Load(rTmp2, dst.base, dst.off+offP)
	c.guardScalar(rTmp2, ip)
	a.Bind(done)
}

const noReg Reg = 255

// storeNumber writes the number in x to dst. Its guardStore must come
// first.
func (c *amd64Compiler) storeNumber(dst operand, x XReg) {
	c.a.StoreSD(dst.base, dst.off+offN, x)
	c.a.Store(dst.base, dst.off+offP, rNumber)
}

// load reads the value at o into rP and rN.
func (c *amd64Compiler) load(o operand) {
	c.a.Load(rP, o.base, o.off+offP)
	c.a.Load(rN, o.base, o.off+offN)
}

// store writes rP and rN to dst. Its guardStore must come first.
func (c *amd64Compiler) store(dst operand) {
	c.a.Store(dst.base, dst.off+offN, rN)
	c.a.Store(dst.base, dst.off+offP, rP)
}

func (c *amd64Compiler) copyValue(dst, src operand, ip int) {
	c.load(src)
	c.guardStore(dst, rP, ip)
	c.store(dst)
}

// storeBool writes the boolean with tag bits bits to dst.
func (c *amd64Compiler) storeBool(dst operand, bits uint64) {
	c.a.MovImm(rTmp, bits)
	c.a.Store(dst.base, dst.off+offN, rTmp)
	c.a.MovImm(rTmp, uint64(uintptr(boolPtr())))
	c.a.Store(dst.base, dst.off+offP, rTmp)
}

// branchFalsy branches to l when the value at o is nil or false.
func (c *amd64Compiler) branchFalsy(o operand, l Label) {
	a := &c.a
	truthy := a.NewLabel()
	a.Load(rTmp, o.base, o.off+offP)
	a.Test(rTmp, rTmp)
	a.J(E, l)
	a.MovImm(rTmp2, uint64(uintptr(boolPtr())))
	a.Cmp(rTmp, rTmp2)
	a.J(NE, truthy)
	a.Load(rTmp, o.base, o.off+offN)
	a.Bt(rTmp, 0)
	a.J(AE, l) // bit 0 clear: false
	a.Bind(truthy)
}

// spend spends one unit of budget, exiting at ip to resume there once it
// runs out.
func (c *amd64Compiler) spend(ip int) {
	if c.budget[ip] < 0 {
		c.budget[ip] = c.a.NewLabel()
	}
	c.a.SubMem(rCtx, offBudget, 1)
	c.a.J(E, c.budget[ip])
}

// backEdge spends budget and jumps to target.
func (c *amd64Compiler) backEdge(target int) {
	c.spend(target)
	c.a.Jmp(c.pcs[target])
}

// jumpTo jumps to target from ip, spending budget if it is backward.
func (c *amd64Compiler) jumpTo(ip, target int) {
	if target <= ip {
		c.backEdge(target)
	} else {
		c.a.Jmp(c.pcs[target])
	}
}

func (c *amd64Compiler) jumpAfter(ip int) (int, bool) {
	j := c.code[ip+1]
	if j.A() != 0 {
		return 0, false
	}
	return ip + 2 + j.SBx(), true
}

// compare branches to yes when x op y is jump, where op is EQ, LT or LE,
// and to no otherwise. NaN makes every comparison false.
func (c *amd64Compiler) compare(op bytecode.OpCode, jump bool, x, y XReg, yes, no Label) {
	a := &c.a
	switch op {
	case bytecode.OpEqual:
		a.Ucomisd(x, y)
		if jump {
			a.J(P, no)
			a.J(E, yes)
		} else {
			a.J(P, yes)
			a.J(NE, yes)
		}
	case bytecode.OpLessThan:
		a.Ucomisd(y, x) // A: y > x
		if jump {
			a.J(A, yes)
		} else {
			a.J(BE, yes)
		}
	case bytecode.OpLessOrEqual:
		a.Ucomisd(y, x) // AE: y >= x
		if jump {
			a.J(AE, yes)
		} else {
			a.J(B, yes)
		}
	}
	a.Jmp(no)
}

// signMask loads the sign bit into x.
func (c *amd64Compiler) signMask(x XReg) {
	c.a.MovImm(rTmp, 1<<63)
	c.a.MovqToX(x, rTmp)
}

// arith computes x0 = x0 op x1 for ADD to MOD. It reports false for MOD
// without ROUNDSD.
func (c *amd64Compiler) arith(op bytecode.OpCode, d, x, y XReg) bool {
	a := &c.a
	if d != x {
		a.MovSD(d, x)
	}
	switch op {
	case bytecode.OpAdd:
		a.AddSD(d, y)
	case bytecode.OpSub:
		a.SubSD(d, y)
	case bytecode.OpMul:
		a.MulSD(d, y)
	case bytecode.OpDiv:
		a.DivSD(d, y)
	case bytecode.OpMod: // b - floor(b/c)*c, rounded step by step as arith does
		if !c.sse41 {
			return false
		}
		a.MovSD(2, d)
		a.DivSD(2, y)
		a.RoundSD(2, 2, 1)
		a.MulSD(2, y)
		a.SubSD(d, 2)
	}
	return true
}

// instruction compiles the instruction at ip and returns how many extra
// code words it consumed.
func (c *amd64Compiler) instruction(ip int) int {
	a := &c.a
	orig := c.p.Code[ip]
	switch op := orig.OpCode(); op {
	case bytecode.OpMove:
		c.copyValue(reg(orig.A()), reg(orig.B()), ip)
	case bytecode.OpLoadConstant:
		k, ok := c.constant(orig.Bx())
		if !ok {
			c.exitAlways(ip)
			break
		}
		c.copyValue(reg(orig.A()), k, ip)
	case bytecode.OpLoadBool:
		dst := reg(orig.A())
		c.guardStore(dst, noReg, ip)
		bits := tagOf(vkBool)
		if orig.B() != 0 {
			bits |= 1
		}
		c.storeBool(dst, bits)
		if orig.C() != 0 {
			a.Jmp(c.pcs[ip+2])
		}
	case bytecode.OpLoadNil:
		for r := orig.A(); r <= orig.A()+orig.B(); r++ {
			c.guardStore(reg(r), noReg, ip)
		}
		for r := orig.A(); r <= orig.A()+orig.B(); r++ {
			a.StoreZero(rFrame, reg(r).off+offP)
			a.StoreZero(rFrame, reg(r).off+offN)
		}
	case bytecode.OpGetUpValue:
		c.upValueAddr(orig.B())
		c.copyValue(reg(orig.A()), operand{rAddr, 0}, ip)
	case bytecode.OpSetUpValue:
		c.upValueAddr(orig.B())
		c.copyValue(operand{rAddr, 0}, reg(orig.A()), ip)
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod:
		b, okB := c.rkNumber(orig.B())
		cc, okC := c.rkNumber(orig.C())
		if !okB || !okC || op == bytecode.OpMod && !c.sse41 {
			c.exitAlways(ip)
			break
		}
		dst := reg(orig.A())
		c.guardNumber(b, ip)
		c.guardNumber(cc, ip)
		c.guardStore(dst, noReg, ip)
		a.LoadSD(0, b.base, b.off+offN)
		a.LoadSD(1, cc.base, cc.off+offN)
		c.arith(op, 0, 0, 1)
		c.storeNumber(dst, 0)
	case bytecode.OpUnaryMinus:
		src, dst := reg(orig.B()), reg(orig.A())
		c.guardNumber(src, ip)
		c.guardStore(dst, noReg, ip)
		a.LoadSD(0, src.base, src.off+offN)
		c.signMask(1)
		a.XorPD(0, 1)
		c.storeNumber(dst, 0)
	case bytecode.OpNot:
		src, dst := reg(orig.B()), reg(orig.A())
		c.guardStore(dst, noReg, ip)
		falsy, done := a.NewLabel(), a.NewLabel()
		c.branchFalsy(src, falsy)
		c.storeBool(dst, tagOf(vkBool))
		a.Jmp(done)
		a.Bind(falsy)
		c.storeBool(dst, tagOf(vkBool)|1)
		a.Bind(done)
	case bytecode.OpJump:
		if orig.A() != 0 {
			c.exitAlways(ip)
			break
		}
		c.jumpTo(ip, ip+1+orig.SBx())
	case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		target, ok := c.jumpAfter(ip)
		b, okB := c.rkNumber(orig.B())
		cc, okC := c.rkNumber(orig.C())
		if !ok || !okB || !okC {
			c.exitAlways(ip)
			break
		}
		c.guardNumber(b, ip)
		c.guardNumber(cc, ip)
		a.LoadSD(0, b.base, b.off+offN)
		a.LoadSD(1, cc.base, cc.off+offN)
		// The JMP runs when the comparison's result equals A.
		yes := c.pcs[target]
		back := a.NewLabel()
		if target <= ip {
			yes = back
		}
		c.compare(op, orig.A() != 0, 0, 1, yes, c.pcs[ip+2])
		if target <= ip {
			a.Bind(back)
			c.backEdge(target)
		}
	case bytecode.OpTest:
		target, ok := c.jumpAfter(ip)
		if !ok || target <= ip {
			c.exitAlways(ip)
			break
		}
		jump, skip := c.pcs[target], c.pcs[ip+2]
		if orig.C() == 0 {
			c.branchFalsy(reg(orig.A()), jump)
			a.Jmp(skip)
		} else {
			c.branchFalsy(reg(orig.A()), skip)
			a.Jmp(jump)
		}
	case bytecode.OpTestSet:
		target, ok := c.jumpAfter(ip)
		if !ok || target <= ip {
			c.exitAlways(ip)
			break
		}
		src, dst := reg(orig.B()), reg(orig.A())
		assign, skip := a.NewLabel(), c.pcs[ip+2]
		if orig.C() == 0 {
			c.branchFalsy(src, assign)
			a.Jmp(skip)
		} else {
			c.branchFalsy(src, skip)
		}
		a.Bind(assign)
		c.copyValue(dst, src, ip)
		a.Jmp(c.pcs[target])
	case bytecode.OpForPrep:
		init, limit, step := reg(orig.A()), reg(orig.A()+1), reg(orig.A()+2)
		c.guardNumber(init, ip)
		c.guardNumber(limit, ip)
		c.guardNumber(step, ip)
		a.LoadSD(0, init.base, init.off+offN)
		a.LoadSD(2, step.base, step.off+offN)
		a.SubSD(0, 2)
		a.StoreSD(init.base, init.off+offN, 0)
		a.Jmp(c.pcs[ip+1+orig.SBx()])
	case bytecode.OpForLoop:
		idx, limit, step, ext := reg(orig.A()), reg(orig.A()+1), reg(orig.A()+2), reg(orig.A()+3)
		take := a.NewLabel()
		a.LoadSD(0, idx.base, idx.off+offN)
		a.LoadSD(1, limit.base, limit.off+offN)
		a.LoadSD(2, step.base, step.off+offN)
		c.forStep(0, 1, 2, take, c.pcs[ip+1])
		a.Bind(take)
		c.guardStore(ext, noReg, ip)
		a.StoreSD(idx.base, idx.off+offN, 0)
		c.storeNumber(ext, 0)
		c.backEdge(ip + 1 + orig.SBx())
	case bytecode.OpGetTable, bytecode.OpGetTableUp, bytecode.OpSelf, bytecode.OpSetTable, bytecode.OpSetTableUp:
		c.tableAccess(ip, c.code[ip])
	case bytecode.OpCall:
		c.call(ip, orig)
	case bytecode.OpReturn:
		c.returnLua(ip, orig)
	case bytecode.OpLoadConstantEx, bytecode.OpSetList:
		c.exitAlways(ip)
		if op == bytecode.OpLoadConstantEx || orig.C() == 0 {
			return 1 // the extra-argument word is not an instruction
		}
	default:
		c.exitAlways(ip)
	}
	return 0
}

// forStep is FORLOOP's test: with next = index + step in x0 computed here
// from idx, limit and step, it branches to take when the loop goes on and
// to end otherwise. X3 is clobbered.
func (c *amd64Compiler) forStep(idx, limit, step XReg, take, end Label) {
	a := &c.a
	positive, notPositive := a.NewLabel(), a.NewLabel()
	if idx != 0 {
		a.MovSD(0, idx)
	}
	a.AddSD(0, step)
	a.XorPD(3, 3)
	a.Ucomisd(step, 3)
	a.J(P, end) // NaN step: the loop ends
	a.J(A, positive)
	a.Bind(notPositive) // step <= 0: go on while limit <= next
	a.Ucomisd(0, limit)
	a.J(P, end)
	a.J(AE, take)
	a.Jmp(end)
	a.Bind(positive) // go on while next <= limit
	a.Ucomisd(limit, 0)
	a.J(P, end)
	a.J(AE, take)
	a.Jmp(end)
}

// upValueAddr puts the address of upvalue n's value in rAddr.
func (c *amd64Compiler) upValueAddr(n int) {
	a := &c.a
	closed, done := a.NewLabel(), a.NewLabel()
	a.Load(rTmp, rCtx, offUpValues)
	a.Load(rTmp, rTmp, uint32(n)*8) // *upValue
	a.Load(rTmp2, rTmp, offUVState)
	a.Test(rTmp2, rTmp2)
	a.J(E, closed)
	a.Load(rTmp2, rTmp2, offStack)  // &state.stack[0]
	a.Load(rAddr, rTmp, offUVIndex) // index
	a.Shl(rAddr, 4)
	a.Add(rAddr, rTmp2)
	a.Jmp(done)
	a.Bind(closed)
	a.Mov(rAddr, rTmp)
	a.AddImm(rAddr, int32(offUVClosed))
	a.Bind(done)
}
