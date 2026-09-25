package bytecode

// A Proto is a compiled function: what the compiler produces and binary
// chunks hold. The VM builds its own prototype from it, with runtime
// state alongside.
type Proto struct {
	Constants                    []any // nil, bool, int64, float64 or string
	Code                         []Instruction
	Prototypes                   []*Proto
	LineInfo                     []int32
	LocalVariables               []LocalVariable
	UpValues                     []UpValueDesc
	Source                       string
	LineDefined, LastLineDefined int
	ParameterCount, MaxStackSize int
	IsVarArg                     bool
}

// A LocalVariable is a local's name and the pcs where it is live.
type LocalVariable struct {
	Name           string
	StartPC, EndPC int
}

// An UpValueDesc says where a closure finds an upvalue: in the enclosing
// function's register Index if IsLocal, or in its upvalue Index.
type UpValueDesc struct {
	Name    string
	IsLocal bool
	Index   int
}

// MultipleReturns is a CALL or RETURN count of "up to the top of the
// stack", as lua.MultipleReturns.
const MultipleReturns = -1
