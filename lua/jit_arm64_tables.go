//go:build (darwin || linux) && arm64

package lua

import (
	"unsafe"

	"github.com/matjam/luart/internal/bytecode"
	. "github.com/matjam/luart/internal/jit/arm64"
)

// Table access and calls. Each hit path mirrors the interpreter's fast
// path exactly (getField, setField, table.at and tryPut, and the unary
// number function call); anything else exits so the interpreter runs the
// instruction, filling the caches the next run hits.

// tableAccess compiles i, the exec form of a table instruction at ip.
func (c *arm64Compiler) tableAccess(ip int, i bytecode.Instruction) {
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
	c.branchNumber(r, c.exit(ip)) // a number whose bits match the tag
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

// cachedShape checks that the table in rT has the shape the fieldCache of
// the instruction at ip names, which it leaves in rCache, and puts the
// cache's own slot in rIdx.
func (c *arm64Compiler) cachedShape(ip int) {
	a := &c.a
	a.MovImm(rCache, uint64(uintptr(unsafe.Pointer(&c.p.fields[ip]))))
	a.Ldr(rIdx, rT, offTShape)
	a.Cbz(rIdx, c.exit(ip))
	a.Ldr(rLen, rCache, offCShape)
	a.Cmp(rIdx, rLen)
	a.BCond(NE, c.exit(ip))
	a.LdrW(rIdx, rCache, offCSlot)
}

// cachedSlot puts in rSlot the address of the table in rT's own slot the
// fieldCache of the instruction at ip names, exiting if it names none.
func (c *arm64Compiler) cachedSlot(ip int) {
	c.cachedShape(ip)
	c.a.Tbnz(rIdx, 31, c.exit(ip))
	c.element(rT, offTSlots, rIdx, rSlot, ip)
}

// readField loads into rP and rN the field of the table in rT that the
// fieldCache of the instruction at ip names: its own, or when that is
// absent or nil, the one in its metatable's __index table or in the table
// after that. It exits for anything else, and for a nil found through the
// metatable, after which Go goes on looking.
func (c *arm64Compiler) readField(ip int) {
	a := &c.a
	c.cachedShape(ip)
	own, haveMeta, done := a.NewLabel(), a.NewLabel(), a.NewLabel()
	a.Tbz(rIdx, 31, own)
	// fromIndex: the metatable's shape, then its __index table's.
	a.Ldr(rT2, rT, offTMeta)
	a.Cbz(rT2, c.exit(ip))
	a.Bind(haveMeta)
	c.readIndex(ip)
	a.B(done)
	a.Bind(own)
	c.element(rT, offTSlots, rIdx, rSlot, ip)
	c.load(operand{rSlot, 0})
	a.Cbnz(rP, done)
	// An own field holding nil: nil without a metatable, or the metatable's.
	a.Ldr(rT2, rT, offTMeta)
	a.Cbnz(rT2, haveMeta)
	a.Mov(rN, ZR)
	a.Bind(done)
}

// readIndex loads into rP and rN the field that the fieldCache in rCache,
// of the instruction at ip, names through the metatable in rT2: in its
// __index table or in the table after that. It exits for anything else,
// and for nil, after which Go goes on looking.
func (c *arm64Compiler) readIndex(ip int) {
	a := &c.a
	chain, load := a.NewLabel(), a.NewLabel()
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
	a.Tbnz(rIdx, 31, chain)
	c.element(rT2, offTSlots, rIdx, rSlot, ip)
	a.B(load)
	// One more __index table, as a method two classes up needs; longer
	// chains, and keys no table has, exit.
	a.Bind(chain)
	a.Ldr(rTmp, rCache, offCChain)
	a.Cbz(rTmp, c.exit(ip))
	a.Ldr(rTmp2, rTmp, offChLevels+offSliceLen)
	a.CmpImm(rTmp2, 1)
	a.BCond(NE, c.exit(ip))
	a.LdrW(rN, rTmp, offChSlot)
	a.Tbnz(rN, 31, c.exit(ip))
	a.Ldr(rCache, rTmp, offChLevels) // &levels[0]
	a.Ldr(rIdx, rT2, offTMeta)
	a.Cbz(rIdx, c.exit(ip))
	a.Ldr(rP, rIdx, offTShape)
	a.Ldr(rTmp, rCache, offLvMtShape)
	a.Cmp(rP, rTmp)
	a.BCond(NE, c.exit(ip))
	a.LdrW(rP, rCache, offLvMtSlot)
	c.element(rIdx, offTSlots, rP, rSlot, ip)
	c.objectOf(operand{rSlot, 0}, vkTable, rT2, ip)
	a.Ldr(rP, rT2, offTShape)
	a.Ldr(rTmp, rCache, offLvIndex)
	a.Cmp(rP, rTmp)
	a.BCond(NE, c.exit(ip))
	c.element(rT2, offTSlots, rN, rSlot, ip)
	a.Bind(load)
	c.load(operand{rSlot, 0})
	a.Cbz(rP, c.exit(ip))
}

// readStringMethod loads into rP and rN the field of a string that the
// fieldCache of the instruction at ip names, through the string metatable.
func (c *arm64Compiler) readStringMethod(ip int) {
	a := &c.a
	a.MovImm(rCache, uint64(uintptr(unsafe.Pointer(&c.p.fields[ip]))))
	a.Ldr(rIdx, rCache, offCShape)
	a.MovImm(rLen, uint64(uintptr(unsafe.Pointer(stringShape))))
	a.Cmp(rIdx, rLen)
	a.BCond(NE, c.exit(ip))
	a.MovImm(rT2, uint64(uintptr(unsafe.Pointer(&c.g.metaTables[TypeString]))))
	a.Ldr(rT2, rT2, 0)
	a.Cbz(rT2, c.exit(ip))
	c.readIndex(ip)
}

// getField stores the field of the table in rT that the instruction at ip
// caches to dst, exiting unless the cache hits.
func (c *arm64Compiler) getField(ip int, dst operand) {
	c.readField(ip)
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
func (c *arm64Compiler) selfField(ip int, i bytecode.Instruction) {
	a := &c.a
	fn, self, recv := reg(i.A()), reg(i.A()+1), reg(i.B())
	// tableOf, going to selfString's code, which stubs emits out of line,
	// for other kinds.
	c.strSelf[ip] = a.NewLabel()
	a.Ldr(rTmp, recv.base, recv.off+offN)
	a.MovImm(rTmp2, tagOf(vkTable))
	a.Cmp(rTmp, rTmp2)
	a.BCond(NE, c.strSelf[ip])
	a.Ldr(rT, recv.base, recv.off+offP)
	c.branchNumber(rT, c.exit(ip)) // a number whose bits match the tag
	c.readField(ip)
	c.guardStore(fn, rP, ip)
	c.guardStore(self, rT, ip)
	c.store(fn)
	a.MovImm(rTmp, tagOf(vkTable))
	a.Str(rTmp, self.base, self.off+offN)
	a.Str(rT, self.base, self.off+offP)
}

// selfString runs the SELF i at ip for a receiver that is not a table,
// with its second word in rTmp: a string, whose methods come through the
// string metatable, and anything else exits. Self is the string, both of
// whose words are kept before fn is stored, as fn may be the register the
// string is in.
func (c *arm64Compiler) selfString(ip int, i bytecode.Instruction) {
	a := &c.a
	fn, self, recv := reg(i.A()), reg(i.A()+1), reg(i.B())
	a.Lsr(rTmp, rTmp, kindShift)
	a.CmpImm(rTmp, uint32(vkString))
	a.BCond(NE, c.exit(ip))
	a.Ldr(rT, recv.base, recv.off+offP)
	c.branchNumber(rT, c.exit(ip)) // a number whose bits match the tag
	c.readStringMethod(ip)
	c.guardStore(fn, rP, ip)
	c.guardStore(self, rT, ip)
	a.Ldr(rCache, recv.base, recv.off+offN)
	c.store(fn)
	a.Str(rCache, self.base, self.off+offN)
	a.Str(rT, self.base, self.off+offP)
}

// loadRK loads RK field into rP and rN, exiting at ip if it is nil unless
// nilOK. It reports false for a constant out of reach, or a nil one unless
// nilOK.
func (c *arm64Compiler) loadRK(field, ip int, nilOK bool) bool {
	src := reg(field)
	if bytecode.IsConstant(field) {
		k, ok := c.constant(bytecode.ConstantIndex(field))
		if !ok || !nilOK && c.p.Constants[bytecode.ConstantIndex(field)].isNil() {
			return false
		}
		src = k
	}
	c.load(src)
	if !nilOK {
		c.a.Cbz(rP, c.exit(ip))
	}
	return true
}

// setField compiles SETTABLE, or SETTABUP when up is true, with a constant
// string key, storing RK(C) to the cached slot as setField does.
func (c *arm64Compiler) setField(ip int, i bytecode.Instruction, up bool) {
	a := &c.a
	if !c.loadRK(i.C(), ip, true) {
		c.exitAlways(ip)
		return
	}
	t := reg(i.A())
	if up {
		c.upValueAddr(i.A())
		t = operand{rAddr, 0}
	}
	c.tableOf(t, ip)
	c.cachedSlot(ip)
	slot := operand{rSlot, 0}
	// An absent key: setField stores it only in a table with a shared
	// shape and without a metatable, or one known to lack __newindex. A
	// dictionary counts its nil slots, so storing nil in one exits too.
	present, store := a.NewLabel(), a.NewLabel()
	a.Ldr(rTmp, rSlot, offP)
	a.Cbnz(rTmp, present)
	a.Ldr(rTmp, rT, offTShape)
	a.Ldrb(rTmp, rTmp, offShapeDict)
	a.Cbnz(rTmp, c.exit(ip))
	a.Ldr(rTmp, rT, offTMeta)
	a.Cbz(rTmp, store)
	a.Ldrb(rTmp, rTmp, offTFlags)
	a.Tbz(rTmp, uint32(tmNewIndex), c.exit(ip)) // __newindex may be there
	a.B(store)
	a.Bind(present)
	a.Cbnz(rP, store)
	a.Ldr(rTmp, rT, offTShape)
	a.Ldrb(rTmp, rTmp, offShapeDict)
	a.Cbnz(rTmp, c.exit(ip))
	a.Bind(store)
	c.guardStore(slot, rP, ip)
	c.store(slot)
	a.Strb(ZR, rT, offTFlags) // invalidateTagMethodCache
}

// arrayIndex puts the zero-based array index for the number key RK(field)
// in rIdx: an integer, or a float with an integer value, as the table
// normalises it; anything else exits at ip. It reports false for a
// constant key that is not a number.
func (c *arm64Compiler) arrayIndex(field, ip int) bool {
	a := &c.a
	k, kind, ok := c.rkArith(field)
	if !ok {
		return false
	}
	floatKey, done := a.NewLabel(), a.NewLabel()
	c.branchUnlessInteger(k, kind, floatKey)
	a.Ldr(rIdx, k.base, k.off+offN)
	a.B(done)
	a.Bind(floatKey)
	if kind == kindInt {
		a.B(done) // never reached
	} else {
		if kind == kindAny {
			a.Cmp(rTmp, rNumber)
			a.BCond(NE, c.exit(ip))
		}
		a.LdrD(0, k.base, k.off+offN)
		a.Fcvtzs(rIdx, 0)
		a.Scvtf(1, rIdx)
		a.Fcmp(0, 1)
		a.BCond(NE, c.exit(ip))
	}
	a.Bind(done)
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
func (c *arm64Compiler) getIndex(ip int, i bytecode.Instruction, up bool) {
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

// setIndex compiles SETTABLE storing a value to an element of the table's
// array part, as tryPut, or put for a table without a metatable, does.
// setIndex compiles SETTABLE, or SETTABUP when up is set, for an array
// element; other keys exit.
func (c *arm64Compiler) setIndex(ip int, i bytecode.Instruction, up bool) {
	a := &c.a
	if !c.loadRK(i.C(), ip, false) {
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
func (c *arm64Compiler) call(ip int, i bytecode.Instruction) {
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
func (c *arm64Compiler) goCallee(ip int, i bytecode.Instruction) {
	a := &c.a
	fn := reg(i.A())
	notGo, closure := a.NewLabel(), a.NewLabel()
	a.Ldr(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkGoClosure))
	a.Cmp(rTmp, rTmp2)
	a.BCond(EQ, closure)
	a.MovImm(rTmp2, tagOf(vkGoFunction))
	a.Cmp(rTmp, rTmp2)
	a.BCond(NE, notGo)
	a.Ldr(rT, fn.base, fn.off+offP)
	c.branchNumber(rT, notGo) // a number whose bits match the tag
	a.Ldr(rTmp, rT, offGFNumber)
	a.Cbnz(rTmp, c.numCallExit(ip)) // runJIT may call it frameless
	a.B(c.goCallExit(ip))
	a.Bind(closure)
	a.Ldr(rT, fn.base, fn.off+offP)
	c.branchNumber(rT, notGo) // a number whose bits match the tag
	a.B(c.goCallExit(ip))
	a.Bind(notGo)
}

// intrinsic compiles a unary intrinsic call, branching to notGo when the
// callee is not a Go function.
func (c *arm64Compiler) intrinsic(ip int, i bytecode.Instruction, notGo Label) {
	a := &c.a
	fn, arg := reg(i.A()), reg(i.A()+1)
	a.Ldr(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkGoFunction))
	a.Cmp(rTmp, rTmp2)
	a.BCond(NE, notGo)
	a.Ldr(rT, fn.base, fn.off+offP)
	c.branchNumber(rT, notGo) // a number whose bits match the tag
	a.Ldr(rT, rT, offGFNumber)
	a.Cbz(rT, c.goCallExit(ip))
	a.Ldr(rT, rT, offNFUnary)
	c.loadFloat(0, arg, kindAny, false, ip) // an integer converts, as for a number function
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
