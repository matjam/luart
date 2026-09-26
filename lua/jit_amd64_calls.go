//go:build (darwin || linux) && amd64

package lua

import (
	"github.com/matjam/apogee/internal/bytecode"
	. "github.com/matjam/apogee/internal/jit/amd64"
)

// Lua-to-Lua calls and returns between compiled functions on amd64; see
// jit_arm64_calls.go. With fewer registers, values are reloaded from the
// structures they live in rather than kept.

// callLua compiles the CALL i at ip for a compiled, fixed-parameter Lua
// closure. It jumps to notLua when the callee is not a Lua closure, and
// exits for any other Lua closure.
func (c *amd64Compiler) callLua(ip int, i bytecode.Instruction, notLua Label) {
	a := &c.a
	ra, b, results := i.A(), i.B(), i.C()-1
	exit := c.exit(ip)
	fn := reg(ra)
	a.Load(rTmp, fn.base, fn.off+offN)
	a.MovImm(rTmp2, tagOf(vkLuaClosure))
	a.Cmp(rTmp, rTmp2)
	a.J(NE, notLua)
	a.Load(R10, fn.base, fn.off+offP) // closure
	c.branchNumber(R10, exit)         // a number whose bits match the tag
	a.CmpMem(rCtx, offBarrier, 0)
	a.J(NE, exit)
	a.Load(R11, R10, offClProto) // prototype
	a.Load8(AX, R11, offPVarArg)
	a.Test(AX, AX)
	a.J(NE, exit)
	a.Load(AX, R11, offPJit)
	a.Test(AX, AX)
	a.J(E, exit)
	a.Load(R12, rCtx, offCtxS) // state
	// function := ci.stackIndex(a); l.top = function + 1 + argCount.
	a.Mov(R13, rFrame)
	a.AddImm(R13, int32(uint32(ra)*valueSize))
	a.Load(AX, R12, offStack)
	a.Sub(R13, AX)
	a.Shr(R13, 4) // function
	a.Mov(DX, R13)
	a.AddImm(DX, int32(b)) // l.top
	// checkStack(p.maxStackSize) would grow the stack.
	a.Load(CX, R12, offLStackLast)
	a.Sub(CX, DX)
	a.Load(AX, R11, offPMaxStack)
	a.Cmp(CX, AX)
	a.J(LE, exit)
	// pushLuaFrame reuses l.callInfo.next, which must have Lua storage.
	a.Load(R8, R12, offLCallInfo) // ci
	a.Load(R9, R8, offCINext)     // the callee's callInfo
	a.Test(R9, R9)
	a.J(E, exit)
	a.Load(DX, R9, offCILua) // its luaCallInfo
	a.Test(DX, DX)
	a.J(E, exit)
	c.spend(ip)

	// Clear the parameters the call does not pass.
	loop, cleared := a.NewLabel(), a.NewLabel()
	a.Load(CX, R11, offPParams)
	a.Shl(CX, 4)
	a.Add(CX, rFrame)
	a.AddImm(CX, int32(uint32(ra+1)*valueSize)) // end
	a.Mov(AX, rFrame)
	a.AddImm(AX, int32(uint32(ra+b)*valueSize)) // first missing
	a.Bind(loop)
	a.Cmp(AX, CX)
	a.J(AE, cleared)
	a.StoreZero(AX, offP)
	a.StoreZero(AX, offN)
	a.AddImm(AX, int32(valueSize))
	a.Jmp(loop)
	a.Bind(cleared)

	// The caller resumes after the call.
	a.Load(AX, R8, offCILua)
	a.MovImm(CX, uint64(ip+1))
	a.Store(AX, offLSavedPC, CX)

	// pushLuaFrame(function, function+1, results, closure), then
	// setCallStatus(callStatusReentry).
	a.StoreZero(DX, offLSavedPC)
	for w := uint32(0); w < 24; w += 8 {
		a.Load(AX, R11, offPCode+w)
		a.Store(DX, offLCode+w, AX)
	}
	a.Store(DX, offLClosure, R10)
	a.Store(R9, offCIFunction, R13)
	a.AddImm(R13, 1) // base
	a.Load(AX, R11, offPMaxStack)
	a.Add(AX, R13) // top
	a.Store(R9, offCITop, AX)
	a.MovImm(CX, uint64(int64(results)))
	a.Store(R9, offCIResults, CX)
	a.MovImm(CX, uint64(callStatusLua|callStatusReentry))
	a.Store8(R9, offCIStatus, CX)
	a.StoreZero8(R9, offCIMeta) // no __call metamethods
	// frame = l.stack[base:top]
	a.Load(CX, R12, offStack)
	a.Mov(R8, R13)
	a.Shl(R8, 4)
	a.Add(R8, CX)
	a.Store(DX, offLFrame, R8)
	a.Mov(CX, AX)
	a.Sub(CX, R13)
	a.Store(DX, offLFrame+offSliceLen, CX)
	a.Load(CX, R12, offStack+offSliceCap)
	a.Sub(CX, R13)
	a.Store(DX, offLFrame+offSliceCap, CX)
	a.Store(R12, offLCallInfo, R9)
	a.Store(R12, offLTop, AX)

	// Enter the callee.
	a.Mov(rFrame, R8)
	a.Load(rConst, R11, offPConsts)
	a.Load(AX, R10, offClUpVals)
	a.Store(rCtx, offUpValues, AX)
	a.Load(AX, R11, offPJit)
	a.Load(AX, AX, offJCEntry)
	a.JmpReg(AX)
}

// returnLua compiles RETURN i at ip returning a fixed number of results to
// a compiled Lua caller that wants a fixed number; anything else exits.
func (c *amd64Compiler) returnLua(ip int, i bytecode.Instruction) {
	a := &c.a
	ra, b := i.A(), i.B()
	if b == 0 || len(c.p.Prototypes) > 0 {
		c.exitAlways(ip)
		return
	}
	exit := c.exit(ip)
	a.CmpMem(rCtx, offBarrier, 0)
	a.J(NE, exit)
	a.Load(R12, rCtx, offCtxS)    // state
	a.Load(R8, R12, offLCallInfo) // ci
	a.Load8(AX, R8, offCIStatus)
	a.Bt(AX, uint8(bitOf(callStatusReentry)))
	a.J(AE, exit)
	a.Load(CX, R8, offCIResults) // wanted
	a.Bt(CX, 63)
	a.J(B, exit)
	// The caller must be compiled at the pc it resumes at.
	a.Load(R9, R8, offCIPrev)
	a.Load(DX, R9, offCILua)
	a.Test(DX, DX)
	a.J(E, exit)
	a.Load(R10, DX, offLClosure)
	a.Load(R11, R10, offClProto)
	a.Load(R13, R11, offPJit)
	a.Test(R13, R13)
	a.J(E, exit)
	a.Load(AX, DX, offLSavedPC)
	a.Shl(AX, 2)
	a.Load(R10, R13, offJCOffsets)
	a.Add(AX, R10)
	a.Load32(AX, AX, 0)
	a.Bt(AX, 31)
	a.J(B, exit)
	a.Load(R10, R13, offJCBase)
	a.Add(AX, R10)
	a.Store(rCtx, offTarget, AX)

	// Copy min(b-1, wanted) results to stack[ci.function:], then nil up
	// to wanted.
	a.Load(R10, R8, offCIFunction)
	a.Shl(R10, 4)
	a.Load(R11, R12, offStack)
	a.Add(R10, R11)
	copied := a.NewLabel()
	for k := range b - 1 {
		a.CmpImm(CX, int32(k))
		a.J(BE, copied)
		src := reg(ra + k)
		a.Load(AX, rFrame, src.off+offP)
		a.Store(R10, uint32(k)*valueSize+offP, AX)
		a.Load(AX, rFrame, src.off+offN)
		a.Store(R10, uint32(k)*valueSize+offN, AX)
	}
	a.Bind(copied)
	loop, cleared := a.NewLabel(), a.NewLabel()
	a.MovImm(R11, uint64(b-1))
	a.Bind(loop)
	a.Cmp(R11, CX)
	a.J(GE, cleared)
	a.Mov(AX, R11)
	a.Shl(AX, 4)
	a.Add(AX, R10)
	a.StoreZero(AX, offP)
	a.StoreZero(AX, offN)
	a.AddImm(R11, 1)
	a.Jmp(loop)
	a.Bind(cleared)

	// l.callInfo, l.top = ci.previous, ci.previous.top; resume the caller.
	a.Store(R12, offLCallInfo, R9)
	a.Load(AX, R9, offCITop)
	a.Store(R12, offLTop, AX)
	a.Load(DX, R9, offCILua)
	a.Load(rFrame, DX, offLFrame)
	a.Load(R10, DX, offLClosure)
	a.Load(R11, R10, offClProto)
	a.Load(rConst, R11, offPConsts)
	a.Load(AX, R10, offClUpVals)
	a.Store(rCtx, offUpValues, AX)
	a.Load(AX, rCtx, offTarget)
	a.JmpReg(AX)
}
