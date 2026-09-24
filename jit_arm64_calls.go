//go:build (darwin || linux) && arm64

package lua

import (
	. "github.com/matjam/luart/internal/jit/arm64"
)

// Lua-to-Lua calls and returns between compiled functions, in machine
// code. They do what the interpreter's fast paths do (callLua and
// pushLuaFrame, and opReturn's fixed-results case) and jump straight into
// the other function's code, so recursion and method calls stay compiled.
// They store pointers into call frames, so they run only while the write
// barrier is off; otherwise they exit and runJIT makes the call in Go.

const (
	rState Reg = 22 // *State
	rCI    Reg = 23 // the running *callInfo
	rNext  Reg = 24 // the callee's or caller's *callInfo
	rStack Reg = 25 // &l.stack[0], or a jump target
)

// callLua compiles the CALL i at ip for a callee in rT that is a compiled,
// fixed-parameter Lua closure; for anything else it exits.
func (c *arm64Compiler) callLua(ip int, i instruction) {
	a := &c.a
	ra, b, results := i.a(), i.b(), i.c()-1
	fn := reg(ra)
	exit := c.exit(ip)
	c.objectOf(fn, vkLuaClosure, rT, ip)
	a.Cbnz(rBarrier, exit)
	a.Ldr(rT2, rT, offClProto)
	a.Ldrb(rTmp, rT2, offPVarArg)
	a.Cbnz(rTmp, exit)
	a.Ldr(rCache, rT2, offPJit)
	a.Cbz(rCache, exit)
	a.Ldr(rState, rCtx, offCtxS)
	a.Ldr(rStack, rState, offStack)
	// function := ci.stackIndex(a); l.top = function + 1 + argCount.
	a.AddImm(rIdx, rFrame, uint32(ra)*valueSize)
	a.Sub(rIdx, rIdx, rStack)
	a.Lsr(rIdx, rIdx, 4)
	a.AddImm(rSlot, rIdx, uint32(b))
	// checkStack(p.maxStackSize) would grow the stack.
	a.Ldr(rLen, rT2, offPMaxStack)
	a.Ldr(rNext, rState, offLStackLast)
	a.Sub(rNext, rNext, rSlot)
	a.Cmp(rNext, rLen)
	a.BCond(LE, exit)
	// pushLuaFrame reuses l.callInfo.next, which must have Lua storage.
	a.Ldr(rCI, rState, offLCallInfo)
	a.Ldr(rNext, rCI, offCINext)
	a.Cbz(rNext, exit)
	a.Ldr(rP, rNext, offCILua)
	a.Cbz(rP, exit)
	c.spend(ip)

	// Clear the parameters the call does not pass.
	loop, cleared := a.NewLabel(), a.NewLabel()
	a.Ldr(rN, rT2, offPParams)
	a.MovImm(rTmp, uint64(b-1))
	a.Bind(loop)
	a.Cmp(rTmp, rN)
	a.BCond(GE, cleared)
	a.AddShifted(rTmp2, rFrame, rTmp, 4)
	a.Str(ZR, rTmp2, uint32(ra+1)*valueSize+offP)
	a.Str(ZR, rTmp2, uint32(ra+1)*valueSize+offN)
	a.AddImm(rTmp, rTmp, 1)
	a.B(loop)
	a.Bind(cleared)

	// The caller resumes after the call.
	a.Ldr(rTmp, rCI, offCILua)
	a.MovImm(rTmp2, uint64(ip+1))
	a.Str(rTmp2, rTmp, offLSavedPC)

	// pushLuaFrame(function, function+1, results, closure), then
	// setCallStatus(callStatusReentry). rP holds the new luaCallInfo.
	a.Str(ZR, rP, offLSavedPC)
	for w := uint32(0); w < 24; w += 8 {
		a.Ldr(rTmp2, rT2, offPCode+w)
		a.Str(rTmp2, rP, offLCode+w)
	}
	a.Str(rT, rP, offLClosure)
	a.Str(rIdx, rNext, offCIFunction)
	a.AddImm(rSlot, rIdx, 1)           // base
	a.AddShifted(rLen, rSlot, rLen, 0) // top = base + maxStackSize
	a.Str(rLen, rNext, offCITop)
	a.MovImm(rTmp2, uint64(int64(results)))
	a.Str(rTmp2, rNext, offCIResults)
	a.MovImm(rTmp2, uint64(callStatusLua|callStatusReentry))
	a.Strb(rTmp2, rNext, offCIStatus)
	// frame = l.stack[base:top]
	a.AddShifted(rTmp, rStack, rSlot, 4)
	a.Str(rTmp, rP, offLFrame)
	a.Sub(rTmp2, rLen, rSlot)
	a.Str(rTmp2, rP, offLFrame+offSliceLen)
	a.Ldr(rTmp2, rState, offStack+offSliceCap)
	a.Sub(rTmp2, rTmp2, rSlot)
	a.Str(rTmp2, rP, offLFrame+offSliceCap)
	a.Str(rNext, rState, offLCallInfo)
	a.Str(rLen, rState, offLTop)

	// Enter the callee.
	a.Mov(rFrame, rTmp)
	a.Ldr(rConst, rT2, offPConsts)
	a.Ldr(rUpVals, rT, offClUpVals)
	a.Ldr(rTmp, rCache, offJCEntry)
	a.Br(rTmp)
}

// spend spends one unit of budget at ip, where compiled code starts again
// once runJIT has let the goroutine be preempted.
func (c *arm64Compiler) spend(ip int) {
	if c.budget[ip] < 0 {
		c.budget[ip] = c.a.NewLabel()
	}
	c.a.SubsImm(rBudget, rBudget, 1)
	c.a.BCond(EQ, c.budget[ip])
}

// returnLua compiles RETURN i at ip returning a fixed number of results to
// a compiled Lua caller in the same interpreter loop that wants a fixed
// number; anything else exits.
func (c *arm64Compiler) returnLua(ip int, i instruction) {
	a := &c.a
	ra, b := i.a(), i.b()
	if b == 0 || len(c.p.prototypes) > 0 { // results to l.top, or upvalues to close
		c.exitAlways(ip)
		return
	}
	exit := c.exit(ip)
	a.Cbnz(rBarrier, exit)
	a.Ldr(rState, rCtx, offCtxS)
	a.Ldr(rCI, rState, offLCallInfo)
	a.Ldrb(rTmp, rCI, offCIStatus)
	a.Tbz(rTmp, bitOf(callStatusReentry), exit)
	a.Ldr(rLen, rCI, offCIResults) // wanted
	a.Tbnz(rLen, 63, exit)
	// The caller must be compiled at the pc it resumes at.
	a.Ldr(rNext, rCI, offCIPrev)
	a.Ldr(rTmp, rNext, offCILua)
	a.Cbz(rTmp, exit)
	a.Ldr(rT, rTmp, offLClosure)
	a.Ldr(rT2, rT, offClProto)
	a.Ldr(rCache, rT2, offPJit)
	a.Cbz(rCache, exit)
	a.Ldr(rIdx, rTmp, offLSavedPC)
	a.Ldr(rSlot, rCache, offJCOffsets)
	a.AddShifted(rSlot, rSlot, rIdx, 2)
	a.LdrW(rSlot, rSlot, 0)
	a.Tbnz(rSlot, 31, exit)
	a.Ldr(rStack, rCache, offJCBase)
	a.AddShifted(rStack, rStack, rSlot, 0)

	// Copy min(b-1, wanted) results to stack[ci.function:], then nil up
	// to wanted.
	a.Ldr(rIdx, rCI, offCIFunction)
	a.Ldr(rTmp2, rState, offStack)
	a.AddShifted(rIdx, rTmp2, rIdx, 4)
	copied := a.NewLabel()
	for k := range b - 1 {
		a.CmpImm(rLen, uint32(k))
		a.BCond(LS, copied)
		c.load(reg(ra + k))
		a.Str(rP, rIdx, uint32(k)*valueSize+offP)
		a.Str(rN, rIdx, uint32(k)*valueSize+offN)
	}
	a.Bind(copied)
	loop, cleared := a.NewLabel(), a.NewLabel()
	a.MovImm(rTmp, uint64(b-1))
	a.Bind(loop)
	a.Cmp(rTmp, rLen)
	a.BCond(GE, cleared)
	a.AddShifted(rTmp2, rIdx, rTmp, 4)
	a.Str(ZR, rTmp2, offP)
	a.Str(ZR, rTmp2, offN)
	a.AddImm(rTmp, rTmp, 1)
	a.B(loop)
	a.Bind(cleared)

	// l.callInfo, l.top = ci.previous, ci.previous.top; resume the caller.
	a.Str(rNext, rState, offLCallInfo)
	a.Ldr(rTmp2, rNext, offCITop)
	a.Str(rTmp2, rState, offLTop)
	a.Ldr(rTmp, rNext, offCILua)
	a.Ldr(rFrame, rTmp, offLFrame)
	a.Ldr(rConst, rT2, offPConsts)
	a.Ldr(rUpVals, rT, offClUpVals)
	a.Br(rStack)
}

// bitOf returns the bit number of a single-bit flag.
func bitOf(f callStatus) uint32 {
	for b := range uint32(8) {
		if f == 1<<b {
			return b
		}
	}
	panic("bitOf: not a single bit")
}
