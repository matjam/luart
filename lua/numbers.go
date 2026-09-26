package lua

import (
	"fmt"
	"math"
	"strconv"

	"github.com/matjam/luart/internal/bytecode"
)

// numeric returns the number v as a bytecode.Number, if it is a number.
func (v value) numeric() (bytecode.Number, bool) {
	switch v.p {
	case integerPtr():
		return bytecode.Integer(v.i()), true
	case numberPtr():
		return bytecode.Float(v.n), true
	}
	return bytecode.Number{}, false
}

// numberOf is the value of n.
func numberOf(n bytecode.Number) value {
	if n.IsInt {
		return integerValue(n.Int)
	}
	return numberValue(n.Float)
}

// toNumeric converts v to a number as arithmetic does: a number, or a
// string holding a numeral, whose type it keeps ("10" is an integer).
func toNumeric(v value) (bytecode.Number, bool) {
	if n, ok := v.numeric(); ok {
		return n, true
	}
	if s, ok := v.str(); ok {
		return bytecode.ParseNumber(s)
	}
	return bytecode.Number{}, false
}

// toNumberValue is toNumeric's result as a value.
func toNumberValue(v value) (value, bool) {
	if v.isNumber() {
		return v, true
	}
	if n, ok := toNumeric(v); ok {
		return numberOf(n), true
	}
	return nilValue, false
}

// toNumber converts v to a float as lua_tonumberx does: a number, or a
// string holding a numeral.
func (l *State) toNumber(v value) (float64, bool) {
	if n, ok := toNumeric(v); ok {
		return n.ToFloat(), true
	}
	return 0, false
}

// toInteger converts v to an integer as lua_tointegerx does: an integer, a
// float with an integral value, or a string holding a numeral of either.
func toInteger(v value) (int64, bool) {
	if n, ok := toNumeric(v); ok {
		return n.ToInteger()
	}
	return 0, false
}

// numberToString formats the number v as tostring does: an integer in
// decimal, a float as Lua 5.5 prints it (see bytecode.FormatFloat).
func numberToString(v value) string {
	if v.isInteger() {
		return strconv.FormatInt(v.i(), 10)
	}
	return bytecode.FormatFloat(v.f())
}

// arith computes the arithmetic or bitwise operator whose event is event
// on rb and rc, trying their metamethods when they are not numbers, and
// raising Lua's errors: division by zero, a bitwise operand without an
// integer representation, or operands of the wrong type. Arithmetic takes
// strings holding numerals; bitwise operators do not, as in Lua 5.4.
func (l *State) arith(rb, rc value, event tm) value {
	if rb.isNumber() && rc.isNumber() && !(rb.isInteger() && rc.isInteger()) {
		// An integer and a float, as in i * 0.5: the opcodes' fast paths
		// take two floats or two integers, and a float operand makes the
		// result a float. The common operators come first, without
		// FloatArith's dispatch.
		x, y := rb.toFloat(), rc.toFloat()
		switch event {
		case tmAdd:
			return numberValue(x + y)
		case tmSub:
			return numberValue(x - y)
		case tmMul:
			return numberValue(x * y)
		case tmDiv:
			return numberValue(x / y)
		}
		if op := bytecode.ArithOp(event - tmAdd); !op.IsBitwise() {
			return numberValue(bytecode.FloatArith(op, x, y))
		}
	}
	op := bytecode.ArithOp(event - tmAdd)
	var a, b bytecode.Number
	var ok bool
	if op.IsBitwise() {
		var okB bool
		a, ok = rb.numeric()
		b, okB = rc.numeric()
		ok = ok && okB
	} else {
		var okB bool
		a, ok = toNumeric(rb)
		b, okB = toNumeric(rc)
		ok = ok && okB
	}
	if ok {
		switch r, err := bytecode.Arith(op, a, b); err {
		case bytecode.ArithOK:
			return numberOf(r)
		case bytecode.ArithDivZero:
			l.runtimeError("attempt to divide by zero")
		case bytecode.ArithModZero:
			l.runtimeError("attempt to perform 'n%0'")
		}
		// A float without an integer representation: its metamethods first.
	}
	if result, ok := l.callBinaryTagMethod(rb, rc, event); ok {
		return result
	}
	switch {
	case !op.IsBitwise():
		l.arithError(rb, rc)
	case rb.isNumber() && rc.isNumber():
		l.integerRepresentationError(rb, rc)
	default:
		l.bitwiseError(rb, rc)
	}
	return nilValue
}

// lessNumbers and lessOrEqualNumbers compare two numbers exactly, as Lua
// 5.4's LTnum and LEnum do, an integer and a float included.
func lessNumbers(a, b value) bool {
	switch {
	case a.isInteger() && b.isInteger():
		return a.i() < b.i()
	case a.isFloat() && b.isFloat():
		return a.f() < b.f()
	case a.isInteger():
		return lessIntFloat(a.i(), b.f())
	}
	return lessFloatInt(a.f(), b.i())
}

func lessOrEqualNumbers(a, b value) bool {
	switch {
	case a.isInteger() && b.isInteger():
		return a.i() <= b.i()
	case a.isFloat() && b.isFloat():
		return a.f() <= b.f()
	case a.isInteger():
		return lessOrEqualIntFloat(a.i(), b.f())
	}
	return lessOrEqualFloatInt(a.f(), b.i())
}

// fitsFloat reports whether the integer i converts to a float exactly.
func fitsFloat(i int64) bool { return -(1<<53) <= i && i <= 1<<53 }

// i < f: i < ceil(f), and when ceil(f) is out of the integers' range, f is
// above them all if positive, and NaN compares false.
func lessIntFloat(i int64, f float64) bool {
	if fitsFloat(i) {
		return float64(i) < f
	}
	if fi, ok := floatToInteger(math.Ceil(f)); ok {
		return i < fi
	}
	return f > 0
}

func lessOrEqualIntFloat(i int64, f float64) bool {
	if fitsFloat(i) {
		return float64(i) <= f
	}
	if fi, ok := floatToInteger(math.Floor(f)); ok {
		return i <= fi
	}
	return f > 0
}

func lessFloatInt(f float64, i int64) bool {
	if fitsFloat(i) {
		return f < float64(i)
	}
	if fi, ok := floatToInteger(math.Floor(f)); ok {
		return fi < i
	}
	return f < 0
}

func lessOrEqualFloatInt(f float64, i int64) bool {
	if fitsFloat(i) {
		return f <= float64(i)
	}
	if fi, ok := floatToInteger(math.Ceil(f)); ok {
		return fi <= i
	}
	return f < 0
}

// floatEqualsInteger reports whether f has i's value.
func floatEqualsInteger(f float64, i int64) bool {
	fi, ok := floatToInteger(f)
	return ok && fi == i
}

// forPrep prepares the numeric for loop whose registers are r: index,
// limit, step and control variable. It reports whether the loop runs no
// times; otherwise the body runs next, with the control variable set. As in
// Lua 5.4, when the initial value and the step are integers the loop is on
// integers and never wraps around: the limit's register holds how many
// more times it runs. Otherwise it is on floats.
func (l *State) forPrep(r []value) (skip bool) {
	init, limit, step := r[0], r[1], r[2]
	if init.isInteger() && step.isInteger() {
		i, s := init.i(), step.i()
		if s == 0 {
			l.runtimeError("'for' step is zero")
		}
		last, skip := l.forLimit(i, limit, s)
		if skip {
			return true
		}
		var count uint64
		if s > 0 {
			count = uint64(last) - uint64(i)
			if s != 1 {
				count /= uint64(s)
			}
		} else {
			count = uint64(i) - uint64(last)
			count /= uint64(-(s + 1)) + 1 // -(s+1) avoids negating minint
		}
		r[1] = integerValue(int64(count))
		r[3] = init
		return false
	}
	fl, ok := l.toNumber(limit)
	if !ok {
		l.forError(limit, "limit")
	}
	fs, ok := l.toNumber(step)
	if !ok {
		l.forError(step, "step")
	}
	fi, ok := l.toNumber(init)
	if !ok {
		l.forError(init, "initial value")
	}
	if fs == 0 {
		l.runtimeError("'for' step is zero")
	}
	if 0 < fs && fl < fi || fs < 0 && fi < fl {
		return true
	}
	r[0], r[1], r[2], r[3] = numberValue(fi), numberValue(fl), numberValue(fs), numberValue(fi)
	return false
}

// forLimit converts an integer loop's limit to an integer, rounding toward
// the loop's start, as Lua 5.4's forlimit does. A limit beyond the integers
// clips to the largest or smallest one, or means the loop runs no times.
func (l *State) forLimit(init int64, limit value, step int64) (last int64, skip bool) {
	n, ok := toNumeric(limit)
	if !ok {
		l.forError(limit, "limit")
	}
	if n.IsInt {
		last = n.Int
	} else {
		f := math.Floor(n.Float)
		if step < 0 {
			f = math.Ceil(n.Float)
		}
		if last, ok = floatToInteger(f); !ok {
			if 0 < n.Float { // too large
				if step < 0 {
					return 0, true
				}
				last = math.MaxInt64
			} else { // too small, or NaN
				if step > 0 {
					return 0, true
				}
				last = math.MinInt64
			}
		}
	}
	if step > 0 {
		return last, init > last
	}
	return last, init < last
}

// forError raises "bad 'for' <what> (number expected, got <type>)".
func (l *State) forError(v value, what string) {
	l.runtimeError(fmt.Sprintf("bad 'for' %s (number expected, got %s)", what, objectTypeName(v)))
}
