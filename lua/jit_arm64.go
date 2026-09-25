//go:build (darwin || linux) && arm64

package lua

import (
	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/arm64"
)

const jitSupported = true

// Registers the generated code keeps for its whole run.
const (
	rCtx     Reg = 0  // *jitContext
	rFrame   Reg = 1  // &frame[0]
	rConst   Reg = 2  // &constants[0]
	rNumber  Reg = 3  // numberPtr(), the p of every number
	rBudget  Reg = 4  // back-edges left
	rBarrier Reg = 5  // nonzero while the GC write barrier is on
	rBool    Reg = 6  // boolPtr(), the p of every boolean
	rUpVals  Reg = 7  // &closure.upValues[0]
	rP       Reg = 12 // a value's p, while it is copied
	rN       Reg = 13 // a value's second word, while it is copied
	rTmp     Reg = 9
	rTmp2    Reg = 10
	rExitPC  Reg = 11
	rAddr    Reg = 14
	rT       Reg = 15 // a table, while it is indexed
	rT2      Reg = 16 // a second table: a metatable or its __index
	rCache   Reg = 17 // the instruction's *fieldCache
	rIdx     Reg = 19 // a slot or array index
	rLen     Reg = 20
	rSlot    Reg = 21 // the address of a table slot
)

// maxOffset bounds the offsets LDR and STR reach.
const maxOffset = 32768

// arm64Compiler translates a prototype into arm64 code.
//
// Each instruction's code checks everything it needs before it writes
// anything, so a failed check can exit to the interpreter at that
// instruction, which then runs it from the start.
type arm64Compiler struct {
	a       Asm
	p       *prototype
	g       *globalState // the state p runs in, whose string metatable SELF reads
	code    []bytecode.Instruction
	pcs     []Label // start of each pc's code
	exits   []Label // exit to the interpreter at each pc, created on demand
	budget  []Label // budget exits by back-edge target pc, created on demand
	goCall  []Label // exits at a CALL of a Go function, created on demand
	numCall []Label // exits at a CALL of a number function, created on demand
	notLua  []Label // a CALL's out-of-line code for callees other than Lua closures
	strSelf []Label // a SELF's out-of-line code for receivers other than tables
	always  []bool  // instructions compiled as an unconditional exit
}

func compileJIT(p *prototype, g *globalState) (code []byte, offsets []int32, entries []int, kernels int) {
	if len(p.Code) > 1<<16 || uint32(p.MaxStackSize+3)*valueSize >= maxOffset {
		return nil, nil, nil, 0
	}
	c := &arm64Compiler{p: p, g: g, code: p.jitOrig}
	c.pcs = make([]Label, len(c.code))
	c.exits = make([]Label, len(c.code))
	c.budget = make([]Label, len(c.code))
	c.goCall = make([]Label, len(c.code))
	c.numCall = make([]Label, len(c.code))
	c.notLua = make([]Label, len(c.code))
	c.strSelf = make([]Label, len(c.code))
	c.always = make([]bool, len(c.code))
	for i := range c.pcs {
		c.pcs[i], c.exits[i], c.budget[i] = c.a.NewLabel(), -1, -1
		c.goCall[i], c.numCall[i], c.notLua[i], c.strSelf[i] = -1, -1, -1, -1
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

// prologue loads the fixed registers and branches to ctx.target.
func (c *arm64Compiler) prologue() {
	a := &c.a
	a.Ldr(rFrame, rCtx, offFrame)
	a.Ldr(rConst, rCtx, offConstants)
	a.Ldr(rBudget, rCtx, offBudget)
	a.Ldr(rBarrier, rCtx, offBarrier)
	a.Ldr(rUpVals, rCtx, offUpValues)
	a.MovImm(rNumber, uint64(uintptr(numberPtr())))
	a.MovImm(rBool, uint64(uintptr(boolPtr())))
	a.Ldr(rTmp, rCtx, offTarget)
	a.Br(rTmp)
}

// stubs emits the exits that instructions branch to.
func (c *arm64Compiler) stubs() {
	a := &c.a
	common := a.NewLabel()
	budget := a.NewLabel()
	for ip, l := range c.notLua {
		if l >= 0 {
			a.Bind(l)
			c.goCallee(ip, c.code[ip])
			a.B(c.exit(ip))
		}
	}
	for ip, l := range c.strSelf {
		if l >= 0 {
			a.Bind(l)
			c.selfString(ip, c.code[ip])
			a.B(c.pcs[ip+1])
		}
	}
	for ip, l := range c.exits {
		if l >= 0 {
			a.Bind(l)
			a.MovImm(rExitPC, uint64(ip))
			a.B(common)
		}
	}
	for ip, l := range c.budget {
		if l >= 0 {
			a.Bind(l)
			a.MovImm(rExitPC, uint64(ip))
			a.B(budget)
		}
	}
	a.Bind(common)
	a.Str(rExitPC, rCtx, offExitPC)
	a.MovImm(0, jitExitInstruction)
	a.Ret()
	a.Bind(budget)
	a.Str(rExitPC, rCtx, offExitPC)
	a.MovImm(0, jitExitBudget)
	a.Ret()
	for _, calls := range []struct {
		labels []Label
		reason uint64
	}{{c.goCall, jitExitCallGo}, {c.numCall, jitExitCallNumber}} {
		tail := a.NewLabel()
		for ip, l := range calls.labels {
			if l >= 0 {
				a.Bind(l)
				fn := reg(c.code[ip].A())
				a.Ldr(rTmp, fn.base, fn.off+offP)
				a.Str(rTmp, rCtx, offCallee)
				a.MovImm(rExitPC, uint64(ip))
				a.B(tail)
			}
		}
		a.Bind(tail)
		a.Str(rFrame, rCtx, offFrame) // compiled calls may have moved it
		a.Str(rExitPC, rCtx, offExitPC)
		a.MovImm(0, calls.reason)
		a.Ret()
	}
}

// numCallExit returns the label that exits at the CALL at ip for runJIT to
// call the number function in its register.
func (c *arm64Compiler) numCallExit(ip int) Label {
	if c.numCall[ip] < 0 {
		c.numCall[ip] = c.a.NewLabel()
	}
	return c.numCall[ip]
}

// goCallExit returns the label that exits at the CALL at ip for runJIT to
// call the Go function in its register.
func (c *arm64Compiler) goCallExit(ip int) Label {
	if c.goCall[ip] < 0 {
		c.goCall[ip] = c.a.NewLabel()
	}
	return c.goCall[ip]
}

// exit returns the label that resumes the interpreter at ip.
func (c *arm64Compiler) exit(ip int) Label {
	if c.exits[ip] < 0 {
		c.exits[ip] = c.a.NewLabel()
	}
	return c.exits[ip]
}

// exitAlways compiles the instruction at ip as an exit: the interpreter,
// or runJIT for calls and returns, always runs it.
func (c *arm64Compiler) exitAlways(ip int) {
	c.always[ip] = true
	c.a.B(c.exit(ip))
}

// operand locates a value in memory: its base register and byte offset.
type operand struct {
	base Reg
	off  uint32
}

func reg(r int) operand { return operand{rFrame, uint32(r) * valueSize} }

// constant returns the operand for constant k, and false when it is out of
// reach of an immediate offset.
func (c *arm64Compiler) constant(k int) (operand, bool) {
	if uint32(k)*valueSize >= maxOffset {
		return operand{}, false
	}
	return operand{rConst, uint32(k) * valueSize}, true
}

// isNumberConstant reports whether an RK field is a number constant.
func (c *arm64Compiler) isNumberConstant(field int) bool {
	return bytecode.IsConstant(field) && c.p.Constants[bytecode.ConstantIndex(field)].isFloat()
}

// length compiles LEN of a string, whose length is its second word less
// the kind, as an integer; anything else exits, and Go measures a table.
func (c *arm64Compiler) length(ip int, i bytecode.Instruction) {
	a := &c.a
	src, dst := reg(i.B()), reg(i.A())
	a.Ldr(rP, src.base, src.off+offP)
	c.branchNumber(rP, c.exit(ip)) // a number whose bits match the tag
	a.Ldr(rN, src.base, src.off+offN)
	a.Lsr(rTmp, rN, kindShift)
	a.CmpImm(rTmp, uint32(vkString))
	a.BCond(NE, c.exit(ip))
	a.MovImm(rTmp, tagOf(vkString))
	a.Sub(rN, rN, rTmp)
	c.guardStore(dst, noReg, ip)
	a.Str(rN, dst.base, dst.off+offN)
	a.AddImm(rTmp, rNumber, 1) // integerPtr, the next byte
	a.Str(rTmp, dst.base, dst.off+offP)
}

// rk returns the operand for an RK field.
func (c *arm64Compiler) rk(field int) (operand, bool) {
	if !bytecode.IsConstant(field) {
		return reg(field), true
	}
	return c.constant(bytecode.ConstantIndex(field))
}

// equal compiles EQ, whose operands may be any values. Numbers compare as
// floats. Other values are equal when both words are, and when they are
// not can be equal only as strings of the same length, whose bytes it
// compares up to maxInlineCompare, or through __eq, which only two tables or two userdata try: it exits
// for those, unless the first table's metatable is known to lack __eq.
func (c *arm64Compiler) equal(ip int, i bytecode.Instruction) {
	a := &c.a
	target, ok := c.jumpAfter(ip)
	b, okB := c.rk(i.B())
	cc, okC := c.rk(i.C())
	if !ok || !okB || !okC {
		c.exitAlways(ip)
		return
	}
	// The JMP runs when the comparison's result equals A.
	yes, no := c.pcs[target], c.pcs[ip+2]
	back := a.NewLabel()
	if target <= ip {
		yes = back
	}
	eq, ne := yes, no
	if i.A() == 0 {
		eq, ne = no, yes
	}
	if kb, kc := c.isNumberConstant(i.B()), c.isNumberConstant(i.C()); kb || kc {
		// Against a float, only another number can be equal; Go compares
		// an integer with it.
		if o := cc; !kb || !kc {
			if !kb {
				o = b
			}
			floats := a.NewLabel()
			a.Ldr(rP, o.base, o.off+offP)
			a.Cmp(rP, rNumber)
			a.BCond(EQ, floats)
			c.branchNumber(rP, c.exit(ip))
			a.B(ne)
			a.Bind(floats)
		}
		a.LdrD(0, b.base, b.off+offN)
		a.LdrD(1, cc.base, cc.off+offN)
		a.Fcmp(0, 1)
		a.BCond(EQ, eq) // NaN is unordered: not EQ
		a.B(ne)
		if target <= ip {
			a.Bind(back)
			c.backEdge(target)
		}
		return
	}
	exit := c.exit(ip)
	notNumber, differ := a.NewLabel(), a.NewLabel()
	a.Ldr(rP, b.base, b.off+offP)
	a.Ldr(rTmp, cc.base, cc.off+offP)
	a.Cmp(rP, rNumber)
	a.BCond(NE, notNumber)
	floats := a.NewLabel()
	a.Cmp(rTmp, rNumber)
	a.BCond(EQ, floats)
	c.branchNumber(rTmp, exit) // a float and an integer: Go compares them
	a.B(ne)
	a.Bind(floats)
	a.LdrD(0, b.base, b.off+offN)
	a.LdrD(1, cc.base, cc.off+offN)
	a.Fcmp(0, 1)
	a.BCond(EQ, eq) // NaN is unordered: not EQ
	a.B(ne)
	a.Bind(notNumber)
	a.Ldr(rN, b.base, b.off+offN)
	a.Ldr(rTmp2, cc.base, cc.off+offN)
	a.Cmp(rP, rTmp)
	a.BCond(NE, differ)
	a.Cmp(rN, rTmp2)
	a.BCond(EQ, eq)
	a.B(ne) // one address, other bits: true and false, integers, or strings' lengths
	a.Bind(differ)
	// Other first words. An integer and a float may be equal, which Go
	// decides; a number and anything else are not.
	notInteger := a.NewLabel()
	a.Sub(rIdx, rP, rNumber)
	a.CmpImm(rIdx, 1)
	a.BCond(NE, notInteger)
	a.Cmp(rTmp, rNumber)
	a.BCond(EQ, exit)
	a.B(ne)
	a.Bind(notInteger)
	a.Sub(rIdx, rTmp, rNumber)
	a.CmpImm(rIdx, 1)
	a.BCond(LS, ne)
	a.Lsr(rIdx, rN, kindShift)
	a.Lsr(rT, rTmp2, kindShift)
	a.Cmp(rIdx, rT)
	a.BCond(NE, ne) // different kinds
	strs, tables := a.NewLabel(), a.NewLabel()
	a.CmpImm(rIdx, uint32(vkString))
	a.BCond(EQ, strs)
	a.CmpImm(rIdx, uint32(vkTable))
	a.BCond(EQ, tables)
	a.CmpImm(rIdx, uint32(vkUserData))
	a.BCond(EQ, exit)
	a.B(ne) // other objects are equal only at one address
	// Strings: compare the bytes of short ones; Go compares long ones.
	a.Bind(strs)
	a.Cmp(rN, rTmp2)
	a.BCond(NE, ne) // lengths differ
	a.MovImm(rT, tagOf(vkString))
	a.Sub(rIdx, rN, rT) // the length, at least 1: the empty string has one address
	a.CmpImm(rIdx, maxInlineCompare)
	a.BCond(HI, exit)
	loop := a.NewLabel()
	a.Bind(loop)
	a.Ldrb(rT, rP, 0)
	a.Ldrb(rT2, rTmp, 0)
	a.Cmp(rT, rT2)
	a.BCond(NE, ne)
	a.AddImm(rP, rP, 1)
	a.AddImm(rTmp, rTmp, 1)
	a.SubsImm(rIdx, rIdx, 1)
	a.BCond(NE, loop)
	a.B(eq)
	a.Bind(tables)
	a.Ldr(rT, rP, offTMeta)
	a.Cbz(rT, ne)
	a.Ldrb(rIdx, rT, offTFlags)
	a.Tbnz(rIdx, uint32(tmEq), ne) // the metatable has no __eq
	a.B(exit)
	if target <= ip {
		a.Bind(back)
		c.backEdge(target)
	}
}

// rkNumber returns the operand for an RK field that must be a number, and
// false for a constant that is not one.
func (c *arm64Compiler) rkNumber(field int) (operand, bool) {
	if !bytecode.IsConstant(field) {
		return reg(field), true
	}
	k := bytecode.ConstantIndex(field)
	if !c.p.Constants[k].isFloat() {
		return operand{}, false
	}
	return c.constant(k)
}

// branchNumber branches to l when p, a value's first word, is a number's:
// the float or the integer sentinel, which are adjacent. An integer's
// second word can match any tag, so tests for objects and strings must
// rule both out. It uses rTmp2.
func (c *arm64Compiler) branchNumber(p Reg, l Label) {
	c.a.Sub(rTmp2, p, rNumber)
	c.a.CmpImm(rTmp2, 1)
	c.a.BCond(LS, l)
}

// guardNumber exits at ip unless o holds a float. Constants were checked
// when compiling.
func (c *arm64Compiler) guardNumber(o operand, ip int) {
	if o.base == rConst {
		return
	}
	c.a.Ldr(rTmp, o.base, o.off+offP)
	c.a.Cmp(rTmp, rNumber)
	c.a.BCond(NE, c.exit(ip))
}

// guardScalar exits at ip unless p, a value's first word, is nil, a number
// or a boolean: a value without a heap pointer.
func (c *arm64Compiler) guardScalar(p Reg, ip int) {
	ok := c.a.NewLabel()
	c.a.Cbz(p, ok)
	c.a.Cmp(p, rNumber)
	c.a.BCond(EQ, ok)
	c.a.Cmp(p, rBool)
	c.a.BCond(NE, c.exit(ip))
	c.a.Bind(ok)
}

// guardStore exits at ip if the write barrier is on and dst holds a heap
// pointer, or newP, when it is not NoReg, is one. With the barrier off Go
// stores pointers without one too.
func (c *arm64Compiler) guardStore(dst operand, newP Reg, ip int) {
	done := c.a.NewLabel()
	c.a.Cbz(rBarrier, done)
	if newP != noReg {
		c.guardScalar(newP, ip)
	}
	c.a.Ldr(rTmp2, dst.base, dst.off+offP)
	c.guardScalar(rTmp2, ip)
	c.a.Bind(done)
}

const noReg Reg = 255

// storeNumber writes the number in f to dst. Its guardStore must come
// first.
func (c *arm64Compiler) storeNumber(dst operand, f FReg) {
	c.a.StrD(f, dst.base, dst.off+offN)
	c.a.Str(rNumber, dst.base, dst.off+offP)
}

// load reads the value at o into rP and rN.
func (c *arm64Compiler) load(o operand) {
	c.a.Ldr(rP, o.base, o.off+offP)
	c.a.Ldr(rN, o.base, o.off+offN)
}

// store writes rP and rN to dst. Its guardStore must come first.
func (c *arm64Compiler) store(dst operand) {
	c.a.Str(rN, dst.base, dst.off+offN)
	c.a.Str(rP, dst.base, dst.off+offP)
}

// copyValue copies src to dst, exiting at ip first if the write barrier
// forbids it.
func (c *arm64Compiler) copyValue(dst, src operand, ip int) {
	c.load(src)
	c.guardStore(dst, rP, ip)
	c.store(dst)
}

// branchFalsy branches to l when the value at o is nil or false.
func (c *arm64Compiler) branchFalsy(o operand, l Label) {
	truthy := c.a.NewLabel()
	c.a.Ldr(rTmp, o.base, o.off+offP)
	c.a.Cbz(rTmp, l)
	c.a.Cmp(rTmp, rBool)
	c.a.BCond(NE, truthy)
	c.a.Ldr(rTmp, o.base, o.off+offN)
	c.a.Tbz(rTmp, 0, l)
	c.a.Bind(truthy)
}

// backEdge spends one unit of budget and jumps to target, or exits there
// once the budget runs out.
func (c *arm64Compiler) backEdge(target int) {
	if c.budget[target] < 0 {
		c.budget[target] = c.a.NewLabel()
	}
	c.a.SubsImm(rBudget, rBudget, 1)
	c.a.BCond(EQ, c.budget[target])
	c.a.B(c.pcs[target])
}

// jumpAfter returns the target of the JMP at ip+1 that a test instruction
// at ip consumes, and false when that JMP closes upvalues.
func (c *arm64Compiler) jumpAfter(ip int) (int, bool) {
	j := c.code[ip+1]
	if j.A() != 0 {
		return 0, false
	}
	return ip + 2 + j.SBx(), true
}

// instruction compiles the instruction at ip and returns how many extra
// code words it consumed.
func (c *arm64Compiler) instruction(ip int) int {
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
		a.MovImm(rN, bits)
		a.Str(rN, dst.base, dst.off+offN)
		a.Str(rBool, dst.base, dst.off+offP)
		if orig.C() != 0 {
			a.B(c.pcs[ip+2])
		}
	case bytecode.OpLoadNil:
		for r := orig.A(); r <= orig.A()+orig.B(); r++ {
			c.guardStore(reg(r), noReg, ip)
		}
		for r := orig.A(); r <= orig.A()+orig.B(); r++ {
			a.Str(ZR, rFrame, reg(r).off+offP)
			a.Str(ZR, rFrame, reg(r).off+offN)
		}
	case bytecode.OpGetUpValue:
		c.upValueAddr(orig.B())
		c.copyValue(reg(orig.A()), operand{rAddr, 0}, ip)
	case bytecode.OpSetUpValue:
		c.upValueAddr(orig.B())
		c.copyValue(operand{rAddr, 0}, reg(orig.A()), ip)
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv:
		// % is left to Go, which computes it as Lua 5.4 does, with fmod.
		b, okB := c.rkNumber(orig.B())
		cc, okC := c.rkNumber(orig.C())
		if !okB || !okC {
			c.exitAlways(ip)
			break
		}
		dst := reg(orig.A())
		c.guardNumber(b, ip)
		c.guardNumber(cc, ip)
		c.guardStore(dst, noReg, ip)
		a.LdrD(0, b.base, b.off+offN)
		a.LdrD(1, cc.base, cc.off+offN)
		switch op {
		case bytecode.OpAdd:
			a.Fadd(0, 0, 1)
		case bytecode.OpSub:
			a.Fsub(0, 0, 1)
		case bytecode.OpMul:
			a.Fmul(0, 0, 1)
		case bytecode.OpDiv:
			a.Fdiv(0, 0, 1)
		}
		c.storeNumber(dst, 0)
	case bytecode.OpUnaryMinus:
		src, dst := reg(orig.B()), reg(orig.A())
		c.guardNumber(src, ip)
		c.guardStore(dst, noReg, ip)
		a.LdrD(0, src.base, src.off+offN)
		a.Fneg(0, 0)
		c.storeNumber(dst, 0)
	case bytecode.OpNot:
		src, dst := reg(orig.B()), reg(orig.A())
		c.guardStore(dst, noReg, ip)
		falsy, done := a.NewLabel(), a.NewLabel()
		c.branchFalsy(src, falsy)
		a.MovImm(rN, tagOf(vkBool))
		a.B(done)
		a.Bind(falsy)
		a.MovImm(rN, tagOf(vkBool)|1)
		a.Bind(done)
		a.Str(rN, dst.base, dst.off+offN)
		a.Str(rBool, dst.base, dst.off+offP)
	case bytecode.OpJump:
		if orig.A() != 0 {
			c.exitAlways(ip)
			break
		}
		if target := ip + 1 + orig.SBx(); target <= ip {
			c.backEdge(target)
		} else {
			a.B(c.pcs[target])
		}
	case bytecode.OpEqual:
		c.equal(ip, orig)
	case bytecode.OpLessThan, bytecode.OpLessOrEqual:
		target, ok := c.jumpAfter(ip)
		b, okB := c.rkNumber(orig.B())
		cc, okC := c.rkNumber(orig.C())
		if !ok || !okB || !okC {
			c.exitAlways(ip)
			break
		}
		c.guardNumber(b, ip)
		c.guardNumber(cc, ip)
		a.LdrD(0, b.base, b.off+offN)
		a.LdrD(1, cc.base, cc.off+offN)
		a.Fcmp(0, 1)
		// The JMP runs when the comparison's result equals A.
		when := MI
		if op == bytecode.OpLessOrEqual {
			when = LS
		}
		if orig.A() == 0 {
			when = negate(when)
		}
		c.branchTo(when, ip, target)
		a.B(c.pcs[ip+2])
	case bytecode.OpTest:
		target, ok := c.jumpAfter(ip)
		if !ok || target <= ip { // backward tests (repeat-until) spend no budget here
			c.exitAlways(ip)
			break
		}
		// The JMP runs when the value's truth differs from C.
		jump, skip := c.pcs[target], c.pcs[ip+2]
		if orig.C() == 0 {
			c.branchFalsy(reg(orig.A()), jump)
			a.B(skip)
		} else {
			c.branchFalsy(reg(orig.A()), skip)
			a.B(jump)
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
			a.B(skip)
		} else {
			c.branchFalsy(src, skip)
		}
		a.Bind(assign)
		c.copyValue(dst, src, ip)
		a.B(c.pcs[target])
	case bytecode.OpForPrep:
		// A float loop, as forPrep runs it: the body next with the control
		// variable set, or past the FORLOOP when the loop runs no times.
		// Integer loops, a zero step and other values exit.
		init, limit, step, ext := reg(orig.A()), reg(orig.A()+1), reg(orig.A()+2), reg(orig.A()+3)
		c.guardNumber(init, ip)
		c.guardNumber(limit, ip)
		c.guardNumber(step, ip)
		a.LdrD(0, init.base, init.off+offN)
		a.LdrD(1, limit.base, limit.off+offN)
		a.LdrD(2, step.base, step.off+offN)
		a.FmovToF(3, ZR)
		a.Fcmp(2, 3)
		a.BCond(EQ, c.exit(ip)) // 'for' step is zero
		positive, body, skip := a.NewLabel(), a.NewLabel(), a.NewLabel()
		a.BCond(GT, positive)
		a.Fcmp(0, 1) // step < 0 or NaN: no times if init < limit
		a.BCond(MI, skip)
		a.B(body)
		a.Bind(positive) // no times if limit < init
		a.Fcmp(1, 0)
		a.BCond(MI, skip)
		a.Bind(body)
		c.guardStore(ext, noReg, ip)
		c.storeNumber(ext, 0)
		a.B(c.pcs[ip+1])
		a.Bind(skip)
		a.B(c.pcs[ip+2+orig.SBx()])
	case bytecode.OpForLoop:
		// A float loop's registers are floats, which Lua code cannot change;
		// an integer loop, whose step is an integer, exits.
		idx, limit, step, ext := reg(orig.A()), reg(orig.A()+1), reg(orig.A()+2), reg(orig.A()+3)
		c.guardNumber(step, ip)
		target := ip + 1 + orig.SBx()
		take, positive, done := a.NewLabel(), a.NewLabel(), a.NewLabel()
		a.LdrD(0, idx.base, idx.off+offN)
		a.LdrD(1, limit.base, limit.off+offN)
		a.LdrD(2, step.base, step.off+offN)
		a.Fadd(0, 0, 2)
		a.FmovToF(3, ZR)
		a.Fcmp(2, 3)
		a.BCond(GT, positive)
		a.BCond(LS, done) // step <= 0: continue while limit <= idx
		a.B(c.pcs[ip+1])  // step is NaN: the loop ends
		a.Bind(done)
		a.Fcmp(1, 0)
		a.BCond(LS, take)
		a.B(c.pcs[ip+1])
		a.Bind(positive)
		a.Fcmp(0, 1)
		a.BCond(LS, take)
		a.B(c.pcs[ip+1])
		a.Bind(take)
		c.guardStore(ext, noReg, ip)
		a.StrD(0, idx.base, idx.off+offN)
		c.storeNumber(ext, 0)
		c.backEdge(target)
	case bytecode.OpGetTable, bytecode.OpGetTableUp, bytecode.OpSelf, bytecode.OpSetTable, bytecode.OpSetTableUp:
		c.tableAccess(ip, c.code[ip])
	case bytecode.OpCall:
		c.call(ip, orig)
	case bytecode.OpReturn:
		c.returnLua(ip, orig)
	case bytecode.OpLength:
		c.length(ip, orig)
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

// branchTo branches to target when cond holds after a test at ip,
// spending budget on backward branches.
func (c *arm64Compiler) branchTo(cond Cond, ip, target int) {
	if target > ip {
		c.a.BCond(cond, c.pcs[target])
		return
	}
	skip := c.a.NewLabel()
	c.a.BCond(negate(cond), skip)
	c.backEdge(target)
	c.a.Bind(skip)
}

// negate returns the condition that holds exactly when c does not, for
// the conditions a floating-point comparison sets: NE for EQ, and for the
// orderings the condition that is also true when either side is NaN.
func negate(c Cond) Cond {
	switch c {
	case EQ:
		return NE
	case NE:
		return EQ
	case MI: // less
		return PL // greater, equal or unordered
	case PL:
		return MI
	case LS: // less or equal
		return HI // greater or unordered
	case HI:
		return LS
	}
	panic("negate: unexpected condition")
}

// upValueAddr puts the address of upvalue n's value in rAddr: the stack
// slot while the upvalue is open, its closed copy after.
func (c *arm64Compiler) upValueAddr(n int) {
	a := &c.a
	closed, done := a.NewLabel(), a.NewLabel()
	a.Ldr(rTmp, rUpVals, uint32(n)*8) // *upValue
	a.Ldr(rTmp2, rTmp, offUVState)
	a.Cbz(rTmp2, closed)
	a.Ldr(rTmp2, rTmp2, offStack)  // &state.stack[0]
	a.Ldr(rAddr, rTmp, offUVIndex) // index
	a.AddShifted(rAddr, rTmp2, rAddr, 4)
	a.B(done)
	a.Bind(closed)
	a.AddImm(rAddr, rTmp, offUVClosed)
	a.Bind(done)
}
