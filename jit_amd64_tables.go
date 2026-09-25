//go:build (darwin || linux) && amd64

package luart

import (
	"math"
	"unsafe"

	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/amd64"
)

// Table access and calls on amd64, mirroring jit_arm64_tables.go. Lua
// calls and returns go through runJIT.

func (c *amd64Compiler) tableAccess(ip int, i bytecode.Instruction) {
	switch i.OpCode() {
	case opGetField:
		c.tableOf(reg(i.B()), ip)
		c.getField(ip, reg(i.A()))
	case opGetFieldUp:
		c.upValueAddr(i.B())
		c.tableOf(operand{rAddr, 0}, ip)
		c.getField(ip, reg(i.A()))
	case opSelfField:
		c.selfField(ip, i)
	case opSetField:
		c.setField(ip, i, false)
	case opSetFieldUp:
		c.setField(ip, i, true)
	case bytecode.OpGetTable, bytecode.OpGetTableUp:
		c.getIndex(ip, i, i.OpCode() == bytecode.OpGetTableUp)
	case bytecode.OpSetTable, bytecode.OpSetTableUp:
		c.setIndex(ip, i, i.OpCode() == bytecode.OpSetTableUp)
	default:
		c.exitAlways(ip)
	}
}

func (c *amd64Compiler) tableOf(o operand, ip int) { c.objectOf(o, vkTable, rT, ip) }

// objectOf puts the object the value at o holds in r, exiting at ip unless
// it is of kind k.
func (c *amd64Compiler) objectOf(o operand, k valueKind, r Reg, ip int) {
	a := &c.a
	a.Load(rTmp, o.base, o.off+offN)
	a.MovImm(rTmp2, tagOf(k))
	a.Cmp(rTmp, rTmp2)
	a.J(NE, c.exit(ip))
	a.Load(r, o.base, o.off+offP)
	a.Cmp(r, rNumber) // a number whose bits match the tag
	a.J(E, c.exit(ip))
}

// element puts the address of element idx of the []value at obj+off in
// out, exiting at ip unless idx, taken as unsigned, is in range.
func (c *amd64Compiler) element(obj Reg, off uint32, idx, out Reg, ip int) {
	a := &c.a
	a.Load(rLen, obj, off+offSliceLen)
	a.Cmp(idx, rLen)
	a.J(AE, c.exit(ip))
	a.Load(rLen, obj, off)
	a.Mov(out, idx)
	a.Shl(out, 4)
	a.Add(out, rLen)
}

// cachedSlot puts in rSlot the address of the slot the fieldCache of the
// instruction at ip names for the table in rT.
func (c *amd64Compiler) cachedSlot(ip int, withIndex bool) {
	a := &c.a
	exit := c.exit(ip)
	a.MovImm(rCache, uint64(uintptr(unsafe.Pointer(&c.p.fields[ip]))))
	a.Load(rIdx, rT, offTShape)
	a.Test(rIdx, rIdx)
	a.J(E, exit)
	a.Load(rLen, rCache, offCShape)
	a.Cmp(rIdx, rLen)
	a.J(NE, exit)
	a.Load32(rIdx, rCache, offCSlot)
	a.Bt(rIdx, 31)
	if !withIndex {
		a.J(B, exit)
		c.element(rT, offTSlots, rIdx, rSlot, ip)
		return
	}
	fromIndex, done := a.NewLabel(), a.NewLabel()
	a.J(B, fromIndex)
	c.element(rT, offTSlots, rIdx, rSlot, ip)
	a.Jmp(done)
	a.Bind(fromIndex)
	a.Load(rT2, rT, offTMeta)
	a.Test(rT2, rT2)
	a.J(E, exit)
	a.Load(rIdx, rT2, offTShape)
	a.Load(rLen, rCache, offCMtShape)
	a.Cmp(rIdx, rLen)
	a.J(NE, exit)
	a.Load32(rIdx, rCache, offCMtSlot)
	a.Bt(rIdx, 31)
	a.J(B, exit)
	c.element(rT2, offTSlots, rIdx, rSlot, ip)
	c.objectOf(operand{rSlot, 0}, vkTable, rT2, ip)
	a.Load(rIdx, rT2, offTShape)
	a.Load(rLen, rCache, offCIndex)
	a.Cmp(rIdx, rLen)
	a.J(NE, exit)
	a.Load32(rIdx, rCache, offCIdxSlot)
	a.Bt(rIdx, 31)
	a.J(B, exit)
	c.element(rT2, offTSlots, rIdx, rSlot, ip)
	a.Bind(done)
}

// absentIsNil handles a nil just read from the table in rT: it exits at ip
// unless the table has no metatable, when nil is the result.
func (c *amd64Compiler) absentIsNil(ip int) {
	a := &c.a
	ok := a.NewLabel()
	a.Test(rP, rP)
	a.J(NE, ok)
	a.Load(rTmp, rT, offTMeta)
	a.Test(rTmp, rTmp)
	a.J(NE, c.exit(ip))
	a.MovImm(rN, 0)
	a.Bind(ok)
}

func (c *amd64Compiler) getField(ip int, dst operand) {
	c.cachedSlot(ip, true)
	c.load(operand{rSlot, 0})
	c.absentIsNil(ip)
	c.guardStore(dst, rP, ip)
	c.store(dst)
}

func (c *amd64Compiler) selfField(ip int, i bytecode.Instruction) {
	a := &c.a
	fn, self := reg(i.A()), reg(i.A()+1)
	c.tableOf(reg(i.B()), ip)
	c.cachedSlot(ip, true)
	c.load(operand{rSlot, 0})
	c.absentIsNil(ip)
	c.guardStore(fn, rP, ip)
	c.guardStore(self, rT, ip)
	c.store(fn)
	a.MovImm(rTmp, tagOf(vkTable))
	a.Store(self.base, self.off+offN, rTmp)
	a.Store(self.base, self.off+offP, rT)
}

// loadRK loads RK field into rP and rN, exiting at ip if it is nil. It
// reports false for a nil constant or one out of reach.
func (c *amd64Compiler) loadRK(field, ip int) bool {
	src := reg(field)
	if bytecode.IsConstant(field) {
		k, ok := c.constant(bytecode.ConstantIndex(field))
		if !ok || c.p.Constants[bytecode.ConstantIndex(field)].isNil() {
			return false
		}
		src = k
	}
	c.load(src)
	c.a.Test(rP, rP)
	c.a.J(E, c.exit(ip))
	return true
}

func (c *amd64Compiler) setField(ip int, i bytecode.Instruction, up bool) {
	a := &c.a
	if !c.loadRK(i.C(), ip) {
		c.exitAlways(ip)
		return
	}
	t := reg(i.A())
	if up {
		c.upValueAddr(i.A())
		t = operand{rAddr, 0}
	}
	c.tableOf(t, ip)
	c.cachedSlot(ip, false)
	// An absent key: setField stores it only in a table without a
	// metatable and with a shared shape.
	slot, present := operand{rSlot, 0}, a.NewLabel()
	a.Load(rTmp, rSlot, offP)
	a.Test(rTmp, rTmp)
	a.J(NE, present)
	a.Load(rTmp, rT, offTMeta)
	a.Test(rTmp, rTmp)
	a.J(NE, c.exit(ip))
	a.Load(rTmp, rT, offTShape)
	a.Load8(rTmp, rTmp, offShapeDict)
	a.Test(rTmp, rTmp)
	a.J(NE, c.exit(ip))
	a.Bind(present)
	c.guardStore(slot, rP, ip)
	c.store(slot)
	a.StoreZero8(rT, offTFlags) // invalidateTagMethodCache
}

// arrayIndex puts the zero-based array index for the number key RK(field)
// in rIdx, exiting at ip unless it is an integer.
func (c *amd64Compiler) arrayIndex(field, ip int) bool {
	a := &c.a
	k, ok := c.rkNumber(field)
	if !ok {
		return false
	}
	c.guardNumber(k, ip)
	a.LoadSD(0, k.base, k.off+offN)
	a.Cvttsd2si(rIdx, 0)
	a.Cvtsi2sd(1, rIdx)
	a.Ucomisd(0, 1)
	a.J(P, c.exit(ip))
	a.J(NE, c.exit(ip))
	a.SubImm(rIdx, 1)
	return true
}

// upTableOf puts the table in upvalue n in rT, exiting at ip unless it
// holds one.
func (c *amd64Compiler) upTableOf(n, ip int) {
	c.upValueAddr(n)
	c.tableOf(operand{rAddr, 0}, ip)
}

// getIndex compiles GETTABLE, or GETTABUP when up is set, for an array
// element; other keys exit.
func (c *amd64Compiler) getIndex(ip int, i bytecode.Instruction, up bool) {
	if up {
		c.upTableOf(i.B(), ip)
	} else {
		c.tableOf(reg(i.B()), ip)
	}
	if !c.arrayIndex(i.C(), ip) {
		c.exitAlways(ip)
		return
	}
	c.element(rT, offTArray, rIdx, rSlot, ip)
	c.load(operand{rSlot, 0})
	c.absentIsNil(ip)
	dst := reg(i.A())
	c.guardStore(dst, rP, ip)
	c.store(dst)
}

// setIndex compiles SETTABLE, or SETTABUP when up is set, for an array
// element; other keys exit.
func (c *amd64Compiler) setIndex(ip int, i bytecode.Instruction, up bool) {
	a := &c.a
	if !c.loadRK(i.C(), ip) {
		c.exitAlways(ip)
		return
	}
	if up {
		c.upTableOf(i.A(), ip)
	} else {
		c.tableOf(reg(i.A()), ip)
	}
	if !c.arrayIndex(i.B(), ip) {
		c.exitAlways(ip)
		return
	}
	c.element(rT, offTArray, rIdx, rSlot, ip)
	slot, present := operand{rSlot, 0}, a.NewLabel()
	a.Load(rTmp, rSlot, offP)
	a.Test(rTmp, rTmp)
	a.J(NE, present)
	a.Load(rTmp, rT, offTMeta)
	a.Test(rTmp, rTmp)
	a.J(NE, c.exit(ip))
	a.Bind(present)
	c.guardStore(slot, rP, ip)
	c.store(slot)
	a.StoreZero8(rT, offTFlags) // invalidateTagMethodCache
}

// intrinsic is a unary number function compiled inline: emit computes
// x0 from x0 bit for bit as the Go function does.
type intrinsic struct {
	fn   uint64
	emit func()
}

// intrinsics returns the functions compiled inline on amd64: floor and
// ceil need SSE4.1, and sin and cos a GOAMD64 level Go does not fuse at.
func (c *amd64Compiler) intrinsics() []intrinsic {
	a := &c.a
	list := []intrinsic{
		{funcValue(math.Sqrt), func() { a.SqrtSD(0, 0) }},
		{funcValue(math.Abs), func() {
			a.MovImm(rTmp, 1<<63-1)
			a.MovqToX(1, rTmp)
			a.AndPD(0, 1)
		}},
	}
	if c.sse41 {
		list = append(list,
			intrinsic{funcValue(math.Floor), func() { a.RoundSD(0, 0, 1) }},
			intrinsic{funcValue(math.Ceil), func() { a.RoundSD(0, 0, 2) }})
	}
	return append(list, c.trigIntrinsics()...)
}

func funcValue(f func(float64) float64) uint64 { return uint64(*(*uintptr)(unsafe.Pointer(&f))) }

// call compiles CALL. A unary intrinsic applied to a number runs here, and
// a call to a compiled Lua function enters it directly. A call to any
// other Go function exits with jitExitCallGo, and runJIT makes it; other
// calls exit to the interpreter.
func (c *amd64Compiler) call(ip int, i bytecode.Instruction) {
	if i.B() == 0 || i.C() == 0 { // arguments or results up to l.top
		c.exitAlways(ip)
		return
	}
	notGoFunction := c.a.NewLabel()
	if i.B() == 2 && i.C() == 2 {
		c.intrinsic(ip, i, notGoFunction)
	}
	c.a.Bind(notGoFunction)
	c.notLua[ip] = c.a.NewLabel()
	c.callLua(ip, i, c.notLua[ip])
}

// goCallee exits with jitExitCallGo when the callee of the CALL i at ip is
// a Go function or Go closure, and otherwise falls through. stubs emits it
// out of line, after the Lua closure check, so calls between Lua functions
// neither run it nor have it in their way.
func (c *amd64Compiler) goCallee(ip int, i bytecode.Instruction) {
	a := &c.a
	fn := reg(i.A())
	notGo, closure := a.NewLabel(), a.NewLabel()
	a.Load(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkGoClosure))
	a.Cmp(rTmp, rTmp2)
	a.J(E, closure)
	a.MovImm(rTmp2, tagOf(vkGoFunction))
	a.Cmp(rTmp, rTmp2)
	a.J(NE, notGo)
	a.Load(rT, fn.base, fn.off+offP)
	a.Cmp(rT, rNumber) // a number whose bits match the tag
	a.J(E, notGo)
	a.Load(rTmp, rT, offGFNumber)
	a.Test(rTmp, rTmp)
	a.J(NE, c.numCallExit(ip)) // runJIT may call it frameless
	a.Jmp(c.goCallExit(ip))
	a.Bind(closure)
	a.Load(rT, fn.base, fn.off+offP)
	a.Cmp(rT, rNumber)
	a.J(NE, c.goCallExit(ip))
	a.Bind(notGo)
}

// intrinsic compiles a unary intrinsic call, jumping to notGo when the
// callee is not a Go function.
func (c *amd64Compiler) intrinsic(ip int, i bytecode.Instruction, notGo Label) {
	a := &c.a
	c.ip = ip
	fn, arg := reg(i.A()), reg(i.A()+1)
	a.Load(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkGoFunction))
	a.Cmp(rTmp, rTmp2)
	a.J(NE, notGo)
	a.Load(rT, fn.base, fn.off+offP)
	a.Cmp(rT, rNumber)
	a.J(E, notGo)
	a.Load(rT, rT, offGFNumber)
	a.Test(rT, rT)
	a.J(E, c.goCallExit(ip))
	a.Load(rT, rT, offNFUnary)
	c.guardNumber(arg, ip)
	a.LoadSD(0, arg.base, arg.off+offN)
	done := a.NewLabel()
	for _, in := range c.intrinsics() {
		next := a.NewLabel()
		a.MovImm(rIdx, in.fn)
		a.Cmp(rT, rIdx)
		a.J(NE, next)
		in.emit()
		a.Jmp(done)
		a.Bind(next)
	}
	a.Jmp(c.numCallExit(ip)) // a number function runJIT may call frameless
	a.Bind(done)
	c.guardStore(fn, noReg, ip)
	c.storeNumber(fn, 0)
	a.Jmp(c.pcs[ip+1])
}
