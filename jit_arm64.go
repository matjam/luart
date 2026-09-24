//go:build (darwin || linux) && arm64

package lua

import (
	"unsafe"

	. "github.com/matjam/luart/internal/jit/arm64"
)

const jitSupported = true

// Registers the generated code keeps for its whole run.
const (
	rCtx    Reg = 0 // *jitContext
	rFrame  Reg = 1 // &frame[0]
	rConst  Reg = 2 // &constants[0]
	rNumber Reg = 3 // numberPtr(), the p of every number
	rBudget Reg = 4 // back-edges left
	rTmp    Reg = 9
	rTmp2   Reg = 10
	rExitPC Reg = 11
)

const (
	offFrame     = uint32(unsafe.Offsetof(jitContext{}.frame))
	offConstants = uint32(unsafe.Offsetof(jitContext{}.constants))
	offTarget    = uint32(unsafe.Offsetof(jitContext{}.target))
	offExitPC    = uint32(unsafe.Offsetof(jitContext{}.exitPC))
	offBudget    = uint32(unsafe.Offsetof(jitContext{}.budget))
	valueSize    = uint32(unsafe.Sizeof(value{}))
	offP         = uint32(unsafe.Offsetof(value{}.p))
	offN         = uint32(unsafe.Offsetof(value{}.n))
)

// arm64Compiler translates a prototype into arm64 code. Each instruction's
// code checks everything it needs before it writes anything, so a failed
// check can exit to the interpreter at that instruction.
type arm64Compiler struct {
	a      Asm
	p      *prototype
	code   []instruction
	pcs    []Label // start of each pc's code
	exits  []Label // exit to the interpreter at each pc, created on demand
	budget []Label // budget exits by back-edge target pc, created on demand
}

func compileJIT(p *prototype) (code []byte, offsets []int32, entries []int) {
	if len(p.code) > 1<<16 || p.maxStackSize*int(valueSize) >= 32768 {
		return nil, nil, nil
	}
	c := &arm64Compiler{p: p, code: p.jitOrig}
	c.pcs = make([]Label, len(c.code))
	c.exits = make([]Label, len(c.code))
	c.budget = make([]Label, len(c.code))
	for i := range c.pcs {
		c.pcs[i], c.exits[i], c.budget[i] = c.a.NewLabel(), -1, -1
	}
	c.prologue()
	for ip := 0; ip < len(c.code); ip++ {
		c.a.Bind(c.pcs[ip])
		ip += c.instruction(ip)
	}
	c.stubs()
	code, err := c.a.Code()
	if err != nil {
		return nil, nil, nil
	}
	offsets = make([]int32, len(c.code))
	for i, l := range c.pcs {
		offsets[i] = int32(c.a.Offset(l))
		if isExtraArg(c.p.code, i) {
			offsets[i] = -1
		}
	}
	exits := make([]bool, len(c.exits))
	for i, l := range c.exits {
		exits[i] = l >= 0
	}
	return code, offsets, jitEntries(c.p, exits)
}

// prologue loads the fixed registers and branches to ctx.target.
func (c *arm64Compiler) prologue() {
	a := &c.a
	a.Ldr(rFrame, rCtx, offFrame)
	a.Ldr(rConst, rCtx, offConstants)
	a.Ldr(rBudget, rCtx, offBudget)
	a.MovImm(rNumber, uint64(uintptr(numberPtr())))
	a.Ldr(rTmp, rCtx, offTarget)
	a.Br(rTmp)
}

// stubs emits the exits that instructions branch to.
func (c *arm64Compiler) stubs() {
	a := &c.a
	common := a.NewLabel()
	budget := a.NewLabel()
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
}

// exit returns the label that resumes the interpreter at ip.
func (c *arm64Compiler) exit(ip int) Label {
	if c.exits[ip] < 0 {
		c.exits[ip] = c.a.NewLabel()
	}
	return c.exits[ip]
}

// operand locates a register or RK operand: its base register and the byte
// offset of its value.
type operand struct {
	base Reg
	off  uint32
}

func reg(r int) operand { return operand{rFrame, uint32(r) * valueSize} }

// rk returns the operand for an RK field, and false for a constant that is
// not a number, which the generated code cannot use.
func (c *arm64Compiler) rk(field int) (operand, bool) {
	if !isConstant(field) {
		return reg(field), true
	}
	k := constantIndex(field)
	if !c.p.constants[k].isNumber() || uint32(k)*valueSize >= 32768 {
		return operand{}, false
	}
	return operand{rConst, uint32(k) * valueSize}, true
}

// guardNumber exits at ip unless o holds a number. Constants were checked
// when compiling.
func (c *arm64Compiler) guardNumber(o operand, ip int) {
	if o.base == rConst {
		return
	}
	c.a.Ldr(rTmp, o.base, o.off+offP)
	c.a.Cmp(rTmp, rNumber)
	c.a.BCond(NE, c.exit(ip))
}

// guardStore exits at ip unless register r holds nil or a number. Stores
// then never overwrite a heap pointer, so they need no GC write barrier.
func (c *arm64Compiler) guardStore(r int, ip int) {
	ok := c.a.NewLabel()
	o := reg(r)
	c.a.Ldr(rTmp, o.base, o.off+offP)
	c.a.Cmp(rTmp, rNumber)
	c.a.BCond(EQ, ok)
	c.a.Cbnz(rTmp, c.exit(ip))
	c.a.Bind(ok)
}

// storeNumber writes the number in f to register r.
func (c *arm64Compiler) storeNumber(r int, f FReg) {
	o := reg(r)
	c.a.StrD(f, o.base, o.off+offN)
	c.a.Str(rNumber, o.base, o.off+offP)
}

// instruction compiles the instruction at ip and returns how many extra
// code words it consumed.
func (c *arm64Compiler) instruction(ip int) int {
	a := &c.a
	i := c.code[ip]
	switch op := c.p.code[ip].opCode(); op {
	case opMove:
		c.guardNumber(reg(i.b()), ip)
		c.guardStore(i.a(), ip)
		a.LdrD(0, rFrame, reg(i.b()).off+offN)
		c.storeNumber(i.a(), 0)
	case opLoadConstant:
		k := c.p.constants[i.bx()]
		if !k.isNumber() || uint32(i.bx())*valueSize >= 32768 {
			a.B(c.exit(ip))
			break
		}
		c.guardStore(i.a(), ip)
		a.LdrD(0, rConst, uint32(i.bx())*valueSize+offN)
		c.storeNumber(i.a(), 0)
	case opAdd, opSub, opMul, opDiv:
		orig := c.p.code[ip]
		b, okB := c.rk(orig.b())
		cc, okC := c.rk(orig.c())
		if !okB || !okC {
			a.B(c.exit(ip))
			break
		}
		c.guardNumber(b, ip)
		c.guardNumber(cc, ip)
		c.guardStore(orig.a(), ip)
		a.LdrD(0, b.base, b.off+offN)
		a.LdrD(1, cc.base, cc.off+offN)
		switch op {
		case opAdd:
			a.Fadd(0, 0, 1)
		case opSub:
			a.Fsub(0, 0, 1)
		case opMul:
			a.Fmul(0, 0, 1)
		case opDiv:
			a.Fdiv(0, 0, 1)
		}
		c.storeNumber(orig.a(), 0)
	case opUnaryMinus:
		c.guardNumber(reg(i.b()), ip)
		c.guardStore(i.a(), ip)
		a.LdrD(0, rFrame, reg(i.b()).off+offN)
		a.Fneg(0, 0)
		c.storeNumber(i.a(), 0)
	case opLoadConstantEx, opSetList:
		a.B(c.exit(ip))
		if op == opLoadConstantEx || i.c() == 0 {
			return 1 // the extra-argument word is not an instruction
		}
	default:
		a.B(c.exit(ip))
	}
	return 0
}
