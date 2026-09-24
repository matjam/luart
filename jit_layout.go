//go:build (darwin || linux) && (arm64 || amd64)

package lua

import "unsafe"

// Offsets the compilers use to reach the interpreter's structures.
const (
	offFrame     = uint32(unsafe.Offsetof(jitContext{}.frame))
	offConstants = uint32(unsafe.Offsetof(jitContext{}.constants))
	offTarget    = uint32(unsafe.Offsetof(jitContext{}.target))
	offExitPC    = uint32(unsafe.Offsetof(jitContext{}.exitPC))
	offBudget    = uint32(unsafe.Offsetof(jitContext{}.budget))
	offUpValues  = uint32(unsafe.Offsetof(jitContext{}.upValues))
	offBarrier   = uint32(unsafe.Offsetof(jitContext{}.barrier))
	valueSize    = uint32(unsafe.Sizeof(value{}))
	offP         = uint32(unsafe.Offsetof(value{}.p))
	offN         = uint32(unsafe.Offsetof(value{}.n))
	offUVState   = uint32(unsafe.Offsetof(upValue{}.state))
	offUVIndex   = uint32(unsafe.Offsetof(upValue{}.index))
	offUVClosed  = uint32(unsafe.Offsetof(upValue{}.closed))
	offStack     = uint32(unsafe.Offsetof(State{}.stack))
	offTShape    = uint32(unsafe.Offsetof(table{}.shape))
	offTSlots    = uint32(unsafe.Offsetof(table{}.slots))
	offTArray    = uint32(unsafe.Offsetof(table{}.array))
	offTMeta     = uint32(unsafe.Offsetof(table{}.metaTable))
	offTFlags    = uint32(unsafe.Offsetof(table{}.flags))
	offShapeDict = uint32(unsafe.Offsetof(shape{}.dict))
	offCShape    = uint32(unsafe.Offsetof(fieldCache{}.shape))
	offCSlot     = uint32(unsafe.Offsetof(fieldCache{}.slot))
	offCMtShape  = uint32(unsafe.Offsetof(fieldCache{}.mtShape))
	offCMtSlot   = uint32(unsafe.Offsetof(fieldCache{}.mtSlot))
	offCIndex    = uint32(unsafe.Offsetof(fieldCache{}.index))
	offCIdxSlot  = uint32(unsafe.Offsetof(fieldCache{}.indexSlot))
	offGFNumber  = uint32(unsafe.Offsetof(goFunction{}.number))
	offNFUnary   = uint32(unsafe.Offsetof(numberFunction{}.unary))
	offSliceLen  = 8
	offCtxS      = uint32(unsafe.Offsetof(jitContext{}.state))

	offClProto   = uint32(unsafe.Offsetof(luaClosure{}.prototype))
	offClUpVals  = uint32(unsafe.Offsetof(luaClosure{}.upValues))
	offPConsts   = uint32(unsafe.Offsetof(prototype{}.constants))
	offPCode     = uint32(unsafe.Offsetof(prototype{}.code))
	offPParams   = uint32(unsafe.Offsetof(prototype{}.parameterCount))
	offPMaxStack = uint32(unsafe.Offsetof(prototype{}.maxStackSize))
	offPVarArg   = uint32(unsafe.Offsetof(prototype{}.isVarArg))
	offPJit      = uint32(unsafe.Offsetof(prototype{}.jit))
	offJCEntry   = uint32(unsafe.Offsetof(jitCode{}.entry))
	offJCBase    = uint32(unsafe.Offsetof(jitCode{}.base))
	offJCOffsets = uint32(unsafe.Offsetof(jitCode{}.offsets))

	offLTop       = uint32(unsafe.Offsetof(State{}.top))
	offLCallInfo  = uint32(unsafe.Offsetof(State{}.callInfo))
	offLStackLast = uint32(unsafe.Offsetof(State{}.stackLast))

	offCIFunction = uint32(unsafe.Offsetof(callInfo{}.function))
	offCITop      = uint32(unsafe.Offsetof(callInfo{}.top))
	offCIResults  = uint32(unsafe.Offsetof(callInfo{}.resultCount))
	offCIPrev     = uint32(unsafe.Offsetof(callInfo{}.previous))
	offCINext     = uint32(unsafe.Offsetof(callInfo{}.next))
	offCIStatus   = uint32(unsafe.Offsetof(callInfo{}.callStatus))
	offCILua      = uint32(unsafe.Offsetof(callInfo{}.luaCallInfo))
	offLFrame     = uint32(unsafe.Offsetof(luaCallInfo{}.frame))
	offLSavedPC   = uint32(unsafe.Offsetof(luaCallInfo{}.savedPC))
	offLCode      = uint32(unsafe.Offsetof(luaCallInfo{}.code))
	offLClosure   = uint32(unsafe.Offsetof(luaCallInfo{}.closure))
	offSliceCap   = 16
)

// Generated code scales a stack index by shifting it left by 4, so a value
// must be 16 bytes. Each array length below is negative otherwise.
var (
	_ [valueSize - 16]struct{}
	_ [16 - valueSize]struct{}
)
