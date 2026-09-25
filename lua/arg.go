package lua

// ArgType lists the Go types Arg can convert a Lua argument to.
type ArgType interface {
	float64 | float32 | int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | string | bool
}

// Arg returns the Go function argument at index as a T, converting it as
// CheckNumber, CheckInteger or CheckString would: an integer type takes an
// integer, or a float or numeral with an integral value, converted as a C
// cast would. For bool it returns the value's truth, as ToBoolean does. It
// raises a Lua error when the argument cannot convert.
func (l *State) Arg[T ArgType](index int) T {
	var r T
	switch p := any(&r).(type) {
	case *float64:
		*p = l.CheckNumber(index)
	case *float32:
		*p = float32(l.CheckNumber(index))
	case *int:
		*p = int(l.CheckInteger(index))
	case *int8:
		*p = int8(l.CheckInteger(index))
	case *int16:
		*p = int16(l.CheckInteger(index))
	case *int32:
		*p = int32(l.CheckInteger(index))
	case *int64:
		*p = l.CheckInteger(index)
	case *uint:
		*p = uint(l.CheckInteger(index))
	case *uint8:
		*p = uint8(l.CheckInteger(index))
	case *uint16:
		*p = uint16(l.CheckInteger(index))
	case *uint32:
		*p = uint32(l.CheckInteger(index))
	case *uint64:
		*p = uint64(l.CheckInteger(index))
	case *string:
		*p = l.CheckString(index)
	case *bool:
		*p = l.ToBoolean(index)
	}
	return r
}
