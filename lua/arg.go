package lua

// ArgType lists the Go types Arg can convert a Lua argument to.
type ArgType interface {
	float64 | int | string | bool
}

// Arg returns the Go function argument at index as a T, converting it as
// CheckNumber, CheckInteger or CheckString would. For bool it returns the
// value's truth, as ToBoolean does. It raises a Lua error when the
// argument cannot convert.
func (l *State) Arg[T ArgType](index int) T {
	var r T
	switch p := any(&r).(type) {
	case *float64:
		*p = l.CheckNumber(index)
	case *int:
		*p = l.CheckInteger(index)
	case *string:
		*p = l.CheckString(index)
	case *bool:
		*p = l.ToBoolean(index)
	}
	return r
}
