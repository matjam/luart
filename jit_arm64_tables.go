//go:build (darwin || linux) && arm64

package lua

import (
	"unsafe"

	. "github.com/matjam/luart/internal/jit/arm64"
)

// Table access and calls. Each hit path mirrors the interpreter's fast
// path exactly (getField, setField, table.at and tryPut, and the unary
// number function call); anything else exits so the interpreter runs the
// instruction, filling the caches the next run hits.

// tableAccess compiles i, the exec form of a table instruction at ip.
func (c *arm64Compiler) tableAccess(ip int, i instruction) {
	switch i.opCode() {
	case opGetField:
		c.tableOf(reg(i.b()), ip)
		c.getField(ip, reg(i.a()))
	case opGetFieldUp:
		c.upValueAddr(i.b())
		c.tableOf(operand{rAddr, 0}, ip)
		c.getField(ip, reg(i.a()))
	case opSelfField:
		c.selfField(ip, i)
	case opSetField:
		c.setField(ip, i, false)
	case opSetFieldUp:
		c.setField(ip, i, true)
	case opGetTable, opGetTableUp:
		c.getIndex(ip, i, i.opCode() == opGetTableUp)
	case opSetTable, opSetTableUp:
		c.setIndex(ip, i, i.opCode() == opSetTableUp)
	default:
		c.exitAlways(ip)
	}
}

// tableOf puts the table the value at o holds in rT, exiting at ip if it
// holds anything else.
func (c *arm64Compiler) tableOf(o operand, ip int) {
	c.objectOf(o, vkTable, rT, ip)
}

// objectOf puts the object the value at o holds in r, exiting at ip unless
// it is of kind k.
func (c *arm64Compiler) objectOf(o operand, k valueKind, r Reg, ip int) {
	a := &c.a
	a.Ldr(rTmp, o.base, o.off+offN)
	a.MovImm(rTmp2, tagOf(k))
	a.Cmp(rTmp, rTmp2)
	a.BCond(NE, c.exit(ip))
	a.Ldr(r, o.base, o.off+offP)
	a.Cmp(r, rNumber) // a number whose bits match the tag
	a.BCond(EQ, c.exit(ip))
}

// element puts the address of element idx of the []value at obj+off in
// out, exiting at ip unless idx, taken as unsigned, is in range.
func (c *arm64Compiler) element(obj Reg, off uint32, idx, out Reg, ip int) {
	a := &c.a
	a.Ldr(rLen, obj, off+offSliceLen)
	a.Cmp(idx, rLen)
	a.BCond(HS, c.exit(ip))
	a.Ldr(rLen, obj, off)
	a.AddShifted(out, rLen, idx, 4)
}

// cachedSlot puts in rSlot the address of the slot the fieldCache of the
// instruction at ip names for the table in rT: its own slot, or its
// metatable's __index table's, as getField would read.
func (c *arm64Compiler) cachedSlot(ip int, withIndex bool) {
	a := &c.a
	a.MovImm(rCache, uint64(uintptr(unsafe.Pointer(&c.p.fields[ip]))))
	a.Ldr(rIdx, rT, offTShape)
	a.Cbz(rIdx, c.exit(ip))
	a.Ldr(rLen, rCache, offCShape)
	a.Cmp(rIdx, rLen)
	a.BCond(NE, c.exit(ip))
	a.LdrW(rIdx, rCache, offCSlot)
	if !withIndex {
		a.Tbnz(rIdx, 31, c.exit(ip))
		c.element(rT, offTSlots, rIdx, rSlot, ip)
		return
	}
	own, done := a.NewLabel(), a.NewLabel()
	a.Tbz(rIdx, 31, own)
	// fromIndex: the metatable's shape, then its __index table's.
	a.Ldr(rT2, rT, offTMeta)
	a.Cbz(rT2, c.exit(ip))
	a.Ldr(rIdx, rT2, offTShape)
	a.Ldr(rLen, rCache, offCMtShape)
	a.Cmp(rIdx, rLen)
	a.BCond(NE, c.exit(ip))
	a.LdrW(rIdx, rCache, offCMtSlot)
	a.Tbnz(rIdx, 31, c.exit(ip))
	c.element(rT2, offTSlots, rIdx, rSlot, ip)
	c.objectOf(operand{rSlot, 0}, vkTable, rT2, ip)
	a.Ldr(rIdx, rT2, offTShape)
	a.Ldr(rLen, rCache, offCIndex)
	a.Cmp(rIdx, rLen)
	a.BCond(NE, c.exit(ip))
	a.LdrW(rIdx, rCache, offCIdxSlot)
	a.Tbnz(rIdx, 31, c.exit(ip))
	c.element(rT2, offTSlots, rIdx, rSlot, ip)
	a.B(done)
	a.Bind(own)
	c.element(rT, offTSlots, rIdx, rSlot, ip)
	a.Bind(done)
}

// getField stores the field of the table in rT that the instruction at ip
// caches to dst, exiting unless the cache hits.
func (c *arm64Compiler) getField(ip int, dst operand) {
	c.cachedSlot(ip, true)
	c.load(operand{rSlot, 0})
	c.absentIsNil(ip)
	c.guardStore(dst, rP, ip)
	c.store(dst)
}

// absentIsNil handles a nil just read from the table in rT, which the
// interpreter looks up in the table's metatable: it exits at ip unless the
// table has none, when nil is the result.
func (c *arm64Compiler) absentIsNil(ip int) {
	ok := c.a.NewLabel()
	c.a.Cbnz(rP, ok)
	c.a.Ldr(rTmp, rT, offTMeta)
	c.a.Cbnz(rTmp, c.exit(ip))
	c.a.Mov(rN, ZR)
	c.a.Bind(ok)
}

// selfField compiles SELF with a constant string key: R(A+1) := R(B);
// R(A) := R(B)[K(C)].
func (c *arm64Compiler) selfField(ip int, i instruction) {
	a := &c.a
	fn, self := reg(i.a()), reg(i.a()+1)
	c.tableOf(reg(i.b()), ip)
	c.cachedSlot(ip, true)
	c.load(operand{rSlot, 0})
	c.absentIsNil(ip)
	c.guardStore(fn, rP, ip)
	c.guardStore(self, rT, ip)
	c.store(fn)
	a.MovImm(rTmp, tagOf(vkTable))
	a.Str(rTmp, self.base, self.off+offN)
	a.Str(rT, self.base, self.off+offP)
}

// loadRK loads RK field into rP and rN, exiting at ip if it is nil or a
// constant out of reach.
func (c *arm64Compiler) loadRK(field, ip int) bool {
	src := reg(field)
	if isConstant(field) {
		k, ok := c.constant(constantIndex(field))
		if !ok || c.p.constants[constantIndex(field)].isNil() {
			return false
		}
		src = k
	}
	c.load(src)
	c.a.Cbz(rP, c.exit(ip))
	return true
}

// setField compiles SETTABLE, or SETTABUP when up is true, with a constant
// string key, storing RK(C) to the cached slot as setField does.
func (c *arm64Compiler) setField(ip int, i instruction, up bool) {
	a := &c.a
	if !c.loadRK(i.c(), ip) {
		c.exitAlways(ip)
		return
	}
	t := reg(i.a())
	if up {
		c.upValueAddr(i.a())
		t = operand{rAddr, 0}
	}
	c.tableOf(t, ip)
	c.cachedSlot(ip, false)
	slot := operand{rSlot, 0}
	// An absent key: setField stores it only in a table without a
	// metatable and with a shared shape.
	present := a.NewLabel()
	a.Ldr(rTmp, rSlot, offP)
	a.Cbnz(rTmp, present)
	a.Ldr(rTmp, rT, offTMeta)
	a.Cbnz(rTmp, c.exit(ip))
	a.Ldr(rTmp, rT, offTShape)
	a.Ldrb(rTmp, rTmp, offShapeDict)
	a.Cbnz(rTmp, c.exit(ip))
	a.Bind(present)
	c.guardStore(slot, rP, ip)
	c.store(slot)
	a.Strb(ZR, rT, offTFlags) // invalidateTagMethodCache
}

// arrayIndex puts the zero-based array index for the number key RK(field)
// in rIdx, exiting at ip unless it is an integer. It reports false for a
// constant key that is not a number.
func (c *arm64Compiler) arrayIndex(field, ip int) bool {
	a := &c.a
	k, ok := c.rkNumber(field)
	if !ok {
		return false
	}
	c.guardNumber(k, ip)
	a.LdrD(0, k.base, k.off+offN)
	a.Fcvtzs(rIdx, 0)
	a.Scvtf(1, rIdx)
	a.Fcmp(0, 1)
	a.BCond(NE, c.exit(ip))
	a.SubImm(rIdx, rIdx, 1)
	return true
}

// getIndex compiles GETTABLE with a number key in the table's array part.
// upTableOf puts the table in upvalue n in rT, exiting at ip unless it
// holds one.
func (c *arm64Compiler) upTableOf(n, ip int) {
	c.upValueAddr(n)
	c.tableOf(operand{rAddr, 0}, ip)
}

// getIndex compiles GETTABLE, or GETTABUP when up is set, for an array
// element; other keys exit.
func (c *arm64Compiler) getIndex(ip int, i instruction, up bool) {
	if up {
		c.upTableOf(i.b(), ip)
	} else {
		c.tableOf(reg(i.b()), ip)
	}
	if !c.arrayIndex(i.c(), ip) {
		c.exitAlways(ip)
		return
	}
	c.element(rT, offTArray, rIdx, rSlot, ip)
	c.load(operand{rSlot, 0})
	c.absentIsNil(ip)
	dst := reg(i.a())
	c.guardStore(dst, rP, ip)
	c.store(dst)
}

// setIndex compiles SETTABLE storing a value to an element of the table's
// array part, as tryPut, or put for a table without a metatable, does.
// setIndex compiles SETTABLE, or SETTABUP when up is set, for an array
// element; other keys exit.
func (c *arm64Compiler) setIndex(ip int, i instruction, up bool) {
	a := &c.a
	if !c.loadRK(i.c(), ip) {
		c.exitAlways(ip)
		return
	}
	if up {
		c.upTableOf(i.a(), ip)
	} else {
		c.tableOf(reg(i.a()), ip)
	}
	if !c.arrayIndex(i.b(), ip) {
		c.exitAlways(ip)
		return
	}
	c.element(rT, offTArray, rIdx, rSlot, ip)
	// A nil element: setTableAt stores over it when there is no
	// metatable to consult for __newindex.
	slot, present := operand{rSlot, 0}, a.NewLabel()
	a.Ldr(rTmp, rSlot, offP)
	a.Cbnz(rTmp, present)
	a.Ldr(rTmp, rT, offTMeta)
	a.Cbnz(rTmp, c.exit(ip))
	a.Bind(present)
	c.guardStore(slot, rP, ip)
	c.store(slot)
	a.Strb(ZR, rT, offTFlags) // invalidateTagMethodCache
}

// call compiles CALL. A unary intrinsic applied to a number runs here, and
// a call to a compiled Lua function enters it directly. A call to any
// other Go function exits with jitExitCallGo, and runJIT makes it; other
// calls exit to the interpreter.
func (c *arm64Compiler) call(ip int, i instruction) {
	if i.b() == 0 || i.c() == 0 { // arguments or results up to l.top
		c.exitAlways(ip)
		return
	}
	notGoFunction := c.a.NewLabel()
	if i.b() == 2 && i.c() == 2 {
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
func (c *arm64Compiler) goCallee(ip int, i instruction) {
	a := &c.a
	fn := reg(i.a())
	notGo, closure := a.NewLabel(), a.NewLabel()
	a.Ldr(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkGoClosure))
	a.Cmp(rTmp, rTmp2)
	a.BCond(EQ, closure)
	a.MovImm(rTmp2, tagOf(vkGoFunction))
	a.Cmp(rTmp, rTmp2)
	a.BCond(NE, notGo)
	a.Ldr(rT, fn.base, fn.off+offP)
	a.Cmp(rT, rNumber) // a number whose bits match the tag
	a.BCond(EQ, notGo)
	a.Ldr(rTmp, rT, offGFNumber)
	a.Cbnz(rTmp, c.numCallExit(ip)) // runJIT may call it frameless
	a.B(c.goCallExit(ip))
	a.Bind(closure)
	a.Ldr(rT, fn.base, fn.off+offP)
	a.Cmp(rT, rNumber)
	a.BCond(NE, c.goCallExit(ip))
	a.Bind(notGo)
}

// intrinsic compiles a unary intrinsic call, branching to notGo when the
// callee is not a Go function.
func (c *arm64Compiler) intrinsic(ip int, i instruction, notGo Label) {
	a := &c.a
	fn, arg := reg(i.a()), reg(i.a()+1)
	a.Ldr(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkGoFunction))
	a.Cmp(rTmp, rTmp2)
	a.BCond(NE, notGo)
	a.Ldr(rT, fn.base, fn.off+offP)
	a.Cmp(rT, rNumber)
	a.BCond(EQ, notGo)
	a.Ldr(rT, rT, offGFNumber)
	a.Cbz(rT, c.goCallExit(ip))
	a.Ldr(rT, rT, offNFUnary)
	c.guardNumber(arg, ip)
	a.LdrD(0, arg.base, arg.off+offN)
	done := a.NewLabel()
	for _, in := range intrinsics {
		next := a.NewLabel()
		a.MovImm(rIdx, in.fn)
		a.Cmp(rT, rIdx)
		a.BCond(NE, next)
		in.emit(c, ip)
		a.B(done)
		a.Bind(next)
	}
	a.B(c.numCallExit(ip)) // a number function runJIT may call frameless
	a.Bind(done)
	c.guardStore(fn, noReg, ip)
	c.storeNumber(fn, 0)
	a.B(c.pcs[ip+1])
}
