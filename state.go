package luart

type pc int
type callStatus byte

const (
	callStatusLua                callStatus = 1 << iota // call is running a Lua function
	callStatusHooked                                    // call is running a debug hook
	callStatusReentry                                   // call is running on same invocation of execute of previous call
	callStatusYielded                                   // call reentered after suspension
	callStatusYieldableProtected                        // call is a yieldable protected call
	callStatusError                                     // call has an error status (pcall)
	callStatusTail                                      // call was tail called
	callStatusHookYielded                               // last hook called yielded
)

// A State is an opaque structure representing per thread Lua state.
type State struct {
	error                 error
	shouldYield           bool
	top                   int // first free slot in the stack
	global                *globalState
	callInfo              *callInfo // call info for current function
	oldPC                 pc        // last pC traced
	stackLast             int       // last free slot in the stack
	stack                 []value
	nonYieldableCallCount int
	nestedGoCallCount     int
	hookMask              byte
	allowHook             bool
	baseHookCount         int
	hookCount             int
	hooker                Hook
	upValues              *upValue // open upvalues, sorted by stack index, highest first
	errorFunction         int      // current error handling function (stack index)
	baseCallInfo          callInfo // callInfo for first level (go calling lua)
	protectFunction       func()
	jitCtx                jitContext // shared with generated code while it runs
	jitRuns               uint64     // entries into compiled code, for tests
	jitBarrierRuns        uint64     // entries while the write barrier was on, for tests
}

type globalState struct {
	mainThread         *State
	tagMethodNames     [tmCount]string
	metaTables         [typeCount]*table // metatables for basic types
	registry           *table
	panicFunction      Function // to be called in unprotected errors
	memoryErrorMessage string
	rootShape          *shape // shape tree for this state's tables
	lightBoxes         map[any]*lightUserData
	jit                bool // compile hot functions; see WithJIT
	// seed uint // randomized seed for hashes
	// upValueHead upValue // head of double-linked list of all open upvalues
}

func (g *globalState) metaTable(o value) *table {
	if t := typeOf(o); t != TypeNone {
		return g.metaTables[t]
	}
	return nil
}

// typeOf returns the Lua type of v, or TypeNone for none.
func typeOf(v value) Type {
	switch v.kind() {
	case vkNil:
		return TypeNil
	case vkBool:
		return TypeBoolean
	case vkNumber:
		return TypeNumber
	case vkString:
		return TypeString
	case vkTable:
		return TypeTable
	case vkLuaClosure, vkGoClosure, vkGoFunction:
		return TypeFunction
	case vkUserData:
		return TypeUserData
	case vkThread:
		return TypeThread
	case vkLightUserData:
		return TypeLightUserData
	}
	return TypeNone
}

// NewState creates a new thread running in a new, independent state.
//
// http://www.lua.org/manual/5.2/manual.html#lua_newstate
func NewState(options ...Option) *State {
	l := &State{allowHook: true, error: nil, nonYieldableCallCount: 1}
	g := &globalState{mainThread: l, registry: newTable(), memoryErrorMessage: "not enough memory", rootShape: newRootShape()}
	l.global = g
	l.initializeStack()
	g.registry.putAtInt(RegistryIndexMainThread, objectValue(l))
	g.registry.putAtInt(RegistryIndexGlobals, objectValue(newTable()))
	copy(g.tagMethodNames[:], eventNames)
	g.jit = jitDefault && jitSupported && !jitDisabled
	for _, o := range options {
		o(l)
	}
	return l
}

// AtPanic sets a new panic function and returns the old one.
func (l *State) AtPanic(panicFunction Function) Function {
	panicFunction, l.global.panicFunction = l.global.panicFunction, panicFunction
	return panicFunction
}

func (l *State) valueToType(v value) Type { return typeOf(v) }
