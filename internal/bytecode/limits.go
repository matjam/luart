package bytecode

import "math"

// Limits the compiler and the VM share.
const (
	MaxStack     = 1000000       // stack slots a thread may use
	MaxUpValue   = math.MaxUint8 // upvalues a function may have
	MaxCallCount = 200           // nested Go calls, parser levels included
)
