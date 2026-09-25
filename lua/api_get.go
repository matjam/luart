package lua

import "github.com/matjam/luart/internal/bytecode"

// TypeOf returns the type of the value at index, or TypeNone for a
// non-valid (but acceptable) index.
//
// http://www.lua.org/manual/5.2/manual.html#lua_type
func (l *State) TypeOf(index int) Type {
	return l.valueToType(l.indexToValue(index))
}

// IsGoFunction verifies that the value at index is a Go function.
//
// http://www.lua.org/manual/5.2/manual.html#lua_iscfunction
func (l *State) IsGoFunction(index int) bool {
	switch l.indexToValue(index).kind() {
	case vkGoFunction, vkGoClosure:
		return true
	}
	return false
}

// IsNumber verifies that the value at index is a number, or a string
// convertible to one.
//
// http://www.lua.org/manual/5.5/manual.html#lua_isnumber
func (l *State) IsNumber(index int) bool {
	_, ok := l.toNumber(l.indexToValue(index))
	return ok
}

// IsInteger verifies that the value at index is an integer: a number with
// the integer subtype, not a float with an integral value.
//
// http://www.lua.org/manual/5.5/manual.html#lua_isinteger
func (l *State) IsInteger(index int) bool { return l.indexToValue(index).isInteger() }

// IsString verifies that the value at index is a string, or a number (which
// is always convertible to a string).
//
// http://www.lua.org/manual/5.2/manual.html#lua_isstring
func (l *State) IsString(index int) bool {
	v := l.indexToValue(index)
	_, ok := v.str()
	return ok || v.isNumber()
}

// IsUserData verifies that the value at index is a userdata.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isuserdata
func (l *State) IsUserData(index int) bool {
	return l.indexToValue(index).userData() != nil
}

// IsFunction verifies that the value at index is a function, either Go or
// Lua function.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isfunction
func (l *State) IsFunction(index int) bool { return l.TypeOf(index) == TypeFunction }

// IsTable verifies that the value at index is a table.
//
// http://www.lua.org/manual/5.2/manual.html#lua_istable
func (l *State) IsTable(index int) bool { return l.TypeOf(index) == TypeTable }

// IsLightUserData verifies that the value at index is a light userdata.
//
// http://www.lua.org/manual/5.2/manual.html#lua_islightuserdata
func (l *State) IsLightUserData(index int) bool { return l.TypeOf(index) == TypeLightUserData }

// IsNil verifies that the value at index is nil.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isnil
func (l *State) IsNil(index int) bool { return l.TypeOf(index) == TypeNil }

// IsBoolean verifies that the value at index is a boolean.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isboolean
func (l *State) IsBoolean(index int) bool { return l.TypeOf(index) == TypeBoolean }

// IsThread verifies that the value at index is a thread.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isthread
func (l *State) IsThread(index int) bool { return l.TypeOf(index) == TypeThread }

// IsNone verifies that the value at index is not valid.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isnone
func (l *State) IsNone(index int) bool { return l.TypeOf(index) == TypeNone }

// IsNoneOrNil verifies that the value at index is either nil or invalid.
//
// http://www.lua.org/manual/5.2/manual.html#lua_isnonornil.
func (l *State) IsNoneOrNil(index int) bool {
	if index > 0 { // an argument: arg returns nil for none
		return l.arg(index).isNil()
	}
	return l.TypeOf(index) <= TypeNil
}

// ToInteger converts the Lua value at index into a signed integer. The Lua
// value must be an integer, a float with an integral value, or a string
// holding a numeral of either.
//
// If the operation failed, the second return value will be false.
//
// http://www.lua.org/manual/5.5/manual.html#lua_tointegerx
func (l *State) ToInteger(index int) (int64, bool) {
	if v := l.arg(index); v.isInteger() {
		return v.i(), true
	}
	return toInteger(l.indexToValue(index))
}

// ToString  converts the Lua value at index to a Go string.  The Lua value
// must also be a string or a number; otherwise the function returns
// false for its second return value.
//
// http://www.lua.org/manual/5.2/manual.html#lua_tolstring
func (l *State) ToString(index int) (s string, ok bool) {
	if s, ok = l.arg(index).str(); ok { // a string argument, the usual case
		return s, true
	}
	v := l.indexToValue(index)
	if s, ok = v.str(); ok {
		return s, true
	}
	if s, ok = toString(v); ok { // Bug compatibility: replace a number with its string representation.
		l.setIndexToValue(index, stringValue(s))
	}
	return
}

// ToNumber converts the Lua value at index to the Go type for a Lua number
// (float64). The Lua value must be a number or a string convertible to a
// number.
//
// If the operation failed, the second return value will be false.
//
// http://www.lua.org/manual/5.2/manual.html#lua_tonumberx
func (l *State) ToNumber(index int) (float64, bool) {
	if v := l.arg(index); v.isNumber() {
		return v.toFloat(), true
	}
	return l.toNumber(l.indexToValue(index))
}

// ToBoolean converts the Lua value at index to a Go boolean. Like all
// tests in Lua, the only false values are false booleans and nil.
// Otherwise, all other Lua values evaluate to true.
//
// To accept only actual boolean values, use the test IsBoolean.
//
// http://www.lua.org/manual/5.2/manual.html#lua_toboolean
func (l *State) ToBoolean(index int) bool { return !isFalse(l.indexToValue(index)) }

// RawLength returns the length of the value at index.  For strings, this is
// the length.  For tables, this is the result of the # operator with no
// metamethods.  For userdata, this is the size of the block of memory
// allocated for the userdata (not implemented yet). For other values, it is 0.
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawlen
func (l *State) RawLength(index int) int {
	v := l.indexToValue(index)
	if s, ok := v.str(); ok {
		return len(s)
	} else if t := v.table(); t != nil {
		return t.length()
	}
	return 0
}

// ToGoFunction converts a value at index into a Go function.  That value
// must be a Go function, otherwise it returns nil.
//
// http://www.lua.org/manual/5.2/manual.html#lua_tocfunction
func (l *State) ToGoFunction(index int) Function {
	v := l.indexToValue(index)
	if f := v.goFunction(); f != nil {
		return f.Function
	} else if c := v.goClosure(); c != nil {
		return c.function
	}
	return nil
}

// ToUserData returns the Go value held by the userdata at index.
// Otherwise, it returns nil.
//
// http://www.lua.org/manual/5.2/manual.html#lua_touserdata
func (l *State) ToUserData(index int) any {
	if d := l.indexToValue(index).userData(); d != nil {
		return d.data
	}
	return nil
}

// UserData returns the Go value held by the userdata at index as a T. It
// reports false when the value is not a userdata or does not hold a T.
func (l *State) UserData[T any](index int) (T, bool) {
	d, ok := l.ToUserData(index).(T)
	return d, ok
}

// ToThread converts the value at index to a Lua thread (a State). This
// value must be a thread, otherwise the return value will be nil.
//
// http://www.lua.org/manual/5.2/manual.html#lua_tothread
func (l *State) ToThread(index int) *State {
	return l.indexToValue(index).thread()
}

// ToValue converts the value at index into a Go value of type any. The
// value can be a table, a thread, a function, a Go string, bool or float64,
// the Go value held by a userdata, or the Go value pushed as light userdata.
// Otherwise, the function returns nil.
//
// Different objects will give different values.  There is no way to convert
// the value back into its original value.
//
// Typically, this function is used only for debug information.
//
// http://www.lua.org/manual/5.2/manual.html#lua_tovalue
func (l *State) ToValue(index int) any {
	v := l.indexToValue(index)
	if v.kind() == vkLightUserData {
		return (*lightUserData)(v.p).v
	}
	switch o := v.obj().(type) {
	case string, int64, float64, bool, *table, *luaClosure, *goClosure, *goFunction, *State:
		return o
	case *userData:
		return o.data
	}
	return nil
}

// ToPointer returns the address of the value at index if it is a table,
// function, userdata, thread or string, and 0 for other values. Different
// objects give different addresses; it is for debug information and
// hashing, as string.format's %p uses it.
//
// http://www.lua.org/manual/5.5/manual.html#lua_topointer
func (l *State) ToPointer(index int) uintptr {
	v := l.indexToValue(index)
	switch v.kind() {
	case vkTable, vkLuaClosure, vkGoClosure, vkGoFunction, vkUserData, vkThread, vkString, vkLightUserData:
		if v.p != emptyStringPtr() {
			return uintptr(v.p)
		}
	}
	return 0
}

// RawEqual verifies that the values at index1 and index2 are primitively
// equal (that is, without calling their metamethods).
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawequal
func (l *State) RawEqual(index1, index2 int) bool {
	if o1, o2 := l.indexToValue(index1), l.indexToValue(index2); !o1.isNil() && !o2.isNil() {
		return rawEqual(o1, o2)
	}
	return false
}

// Compare compares two values.
//
// http://www.lua.org/manual/5.2/manual.html#lua_compare
func (l *State) Compare(index1, index2 int, op ComparisonOperator) bool {
	if o1, o2 := l.indexToValue(index1), l.indexToValue(index2); !o1.isNil() && !o2.isNil() {
		switch op {
		case OpEq:
			return l.equalObjects(o1, o2)
		case OpLT:
			return l.lessThan(o1, o2)
		case OpLE:
			return l.lessOrEqual(o1, o2)
		default:
			panic("invalid option")
		}
	}
	return false
}

// Arith performs an arithmetic operation over the two values (or one, in
// case of negation) at the top of the stack, with the value at the top being
// the second operand, ops these values and pushes the result of the operation.
// The function follows the semantics of the corresponding Lua operator
// (that is, it may call metamethods).
//
// http://www.lua.org/manual/5.2/manual.html#lua_arith
func (l *State) Arith(op Operator) {
	if op != OpUnaryMinus && op != OpBNot {
		l.checkElementCount(2)
	} else {
		l.checkElementCount(1)
		l.push(l.stack[l.top-1])
	}
	o1, o2 := l.stack[l.top-2], l.stack[l.top-1]
	l.stack[l.top-2] = l.arith(o1, o2, arithEvent(bytecode.ArithOp(op)))
	l.top--
}

// Length of the value at index; it is equivalent to the # operator in
// Lua. The result is pushed on the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_len
func (l *State) Length(index int) { l.apiPush(l.objectLength(l.indexToValue(index))) }

// Concat concatenates the n values at the top of the stack, pops them, and
// leaves the result at the top. If n is 1, the result is the single value
// on the stack (that is, the function does nothing); if n is 0, the result
// is the empty string. Concatenation is performed following the usual
// semantic of Lua.
//
// http://www.lua.org/manual/5.2/manual.html#lua_concat
func (l *State) Concat(n int) {
	l.checkElementCount(n)
	if n >= 2 {
		l.concat(n)
	} else if n == 0 { // push empty string
		l.apiPush(stringValue(""))
	} // else n == 1; nothing to do
}
