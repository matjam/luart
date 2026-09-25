package lua

import (
	"errors"

	"github.com/matjam/luart/internal/chunk"
)

// MultipleReturns is the argument for argCount or resultCount in ProtectedCall and Call.
const MultipleReturns = -1

// Debug.Event and SetDebugHook mask argument values.
const (
	HookCall, MaskCall = iota, 1 << iota
	HookReturn, MaskReturn
	HookLine, MaskLine
	HookCount, MaskCount
	HookTailCall, MaskTailCall
)

// Errors introduced by the Lua VM.
var (
	ErrSyntax       = errors.New("syntax error")
	ErrMemory       = errors.New("memory error")
	ErrErrorHandler = errors.New("error within the error handler")
	ErrFile         = errors.New("file error")
)

// A RuntimeError is an error raised internally by the Lua VM or through Error.
type RuntimeError string

func (r RuntimeError) Error() string { return "runtime error: " + string(r) }

// A Type is a symbolic representation of a Lua VM type.
type Type int

// Valid Type values.
const (
	TypeNil Type = iota
	TypeBoolean
	TypeLightUserData
	TypeNumber
	TypeString
	TypeTable
	TypeFunction
	TypeUserData
	TypeThread

	TypeNone = TypeNil - 1
)

// typeCount is the number of Types other than TypeNone.
const typeCount = TypeThread + 1

// An Operator is an op argument for Arith.
type Operator int

// Valid Operator values for Arith.
const (
	OpAdd        Operator = iota // Performs addition (+).
	OpSub                        // Performs subtraction (-).
	OpMul                        // Performs multiplication (*).
	OpDiv                        // Performs division (/).
	OpMod                        // Performs modulo (%).
	OpPow                        // Performs exponentiation (^).
	OpUnaryMinus                 // Performs mathematical negation (unary -).
)

// A ComparisonOperator is an op argument for Compare.
type ComparisonOperator int

// Valid ComparisonOperator values for Compare.
const (
	OpEq ComparisonOperator = iota // Compares for equality (==).
	OpLT                           // Compares for less than (<).
	OpLE                           // Compares for less or equal (<=).
)

// Lua provides a registry, a predefined table, that can be used by any Go code
// to store whatever Lua values it needs to store. The registry table is always
// located at pseudo-index RegistryIndex, which is a valid index. Any Go
// library can store data into this table, but it should take care to choose
// keys that are different from those used by other libraries, to avoid
// collisions. Typically, you should use as key a string containing your
// library name, or a light userdata object in your code, or any Lua object
// created by your code. As with global names, string keys starting with an
// underscore followed by uppercase letters are reserved for Lua.
//
// The integer keys in the registry are used by the reference mechanism
// and by some predefined values. Therefore, integer keys should not be used
// for other purposes.
//
// When you create a new Lua state, its registry comes with some predefined
// values. These predefined values are indexed with integer keys defined as
// constants.
const (
	// RegistryIndex is the pseudo-index for the registry table.
	RegistryIndex = firstPseudoIndex

	// RegistryIndexMainThread is the registry index for the main thread of the
	// State. (The main thread is the one created together with the State.)
	RegistryIndexMainThread = iota

	// RegistryIndexGlobals is the registry index for the global environment.
	RegistryIndexGlobals
)

// Signature is the mark for precompiled code ('<esc>Lua').
const Signature = chunk.Signature

// MinStack is the minimum Lua stack available to a Go function.
const MinStack = 20

const (
	VersionMajor  = 5
	VersionMinor  = 2
	VersionNumber = 502
	VersionString = "Lua " + string('0'+VersionMajor) + "." + string('0'+VersionMinor)
)

// A RegistryFunction is used for arrays of functions to be registered by
// SetFunctions. Name is the function name and Function is the function.
type RegistryFunction struct {
	Name     string
	Function Function
}

// A Debug carries different pieces of information about a function or an
// activation record. Stack fills only the private part of this structure, for
// later use. To fill the other fields of a Debug with useful information, call
// Info.
type Debug struct {
	Event int

	// Name is a reasonable name for the given function. Because functions in
	// Lua are first-class values, they do not have a fixed name. Some functions
	// can be the value of multiple global variables, while others can be stored
	// only in a table field. The Info function checks how the function was
	// called to find a suitable name. If it cannot find a name, then Name is "".
	Name string

	// NameKind explains the name field. The value of NameKind can be "global",
	// "local", "method", "field", "upvalue", or "" (the empty string), according
	// to how the function was called. (Lua uses the empty string when no other
	// option seems to apply.)
	NameKind string

	// What is the string "Lua" if the function is a Lua function, "Go" if it is
	// a Go function, "main" if it is the main part of a chunk.
	//
	// With the environment variable LUART_GO_AS_C=1 when a state is created,
	// its debug information calls Go functions "C", with the source "=[C]",
	// as C Lua's does for its C functions, for scripts and tests that expect
	// that.
	What string

	// Source is the source of the chunk that created the function. If Source
	// starts with a '@', it means that the function was defined in a file where
	// the file name follows the '@'. If Source starts with a '=', the remainder
	// of its contents describe the source in a user-dependent manner. Otherwise,
	// the function was defined in a string where Source is that string.
	Source string

	// ShortSource is a "printable" version of source, to be used in error messages.
	ShortSource string

	// CurrentLine is the current line where the given function is executing.
	// When no line information is available, CurrentLine is set to -1.
	CurrentLine int

	// LineDefined is the line number where the definition of the function starts.
	LineDefined int

	// LastLineDefined is the line number where the definition of the function ends.
	LastLineDefined int

	// UpValueCount is the number of upvalues of the function.
	UpValueCount int

	// ParameterCount is the number of fixed parameters of the function (always 0
	// for Go functions).
	ParameterCount int

	// IsVarArg is true if the function is a vararg function (always true for Go
	// functions).
	IsVarArg bool

	// IsTailCall is true if this function invocation was called by a tail call.
	// In this case, the caller of this level is not in the stack.
	IsTailCall bool

	// callInfo is the active function.
	callInfo *callInfo
}

// A Hook is a callback function that can be registered with SetDebugHook to trace various VM events.
type Hook func(state *State, activationRecord Debug)

// A Function is a Go function intended to be called from Lua.
type Function func(state *State) int

// TODO Set functions (stack -> Lua)
// RawSetValue(index int, p any)
//
// Debug API
// Local(activationRecord *Debug, index int) string
// SetLocal(activationRecord *Debug, index int) string

// String returns the name of Type t.
//
// http://www.lua.org/manual/5.2/manual.html#lua_typename
func (t Type) String() string { return typeNames[t+1] }
