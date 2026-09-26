package bytecode

import (
	"math"
	"strconv"
	"strings"
)

// A Number is a Lua number: an integer or a float.
type Number struct {
	Int   int64
	Float float64
	IsInt bool
}

// Integer and Float make Numbers.
func Integer(i int64) Number { return Number{Int: i, IsInt: true} }
func Float(f float64) Number { return Number{Float: f} }

// ToFloat returns n as a float.
func (n Number) ToFloat() float64 {
	if n.IsInt {
		return float64(n.Int)
	}
	return n.Float
}

// ToInteger returns n as an integer if it is one, or a float with an
// integral value an integer can hold.
func (n Number) ToInteger() (int64, bool) {
	if n.IsInt {
		return n.Int, true
	}
	return FloatToInteger(n.Float)
}

// Value returns n as the Go value a Proto constant holds: int64 or float64.
func (n Number) Value() any {
	if n.IsInt {
		return n.Int
	}
	return n.Float
}

// FloatToInteger returns f as an integer if it has an integral value that
// an integer can hold.
func FloatToInteger(f float64) (int64, bool) {
	// -2^63 converts exactly; 2^63 is the first float too large.
	if f >= -(1<<63) && f < 1<<63 {
		if i := int64(f); float64(i) == f {
			return i, true
		}
	}
	return 0, false
}

// An ArithOp is an arithmetic or bitwise operator, in the order of Lua
// 5.5's LUA_OP constants.
type ArithOp int

const (
	ArithAdd ArithOp = iota
	ArithSub
	ArithMul
	ArithMod
	ArithPow
	ArithDiv
	ArithIDiv
	ArithBAnd
	ArithBOr
	ArithBXor
	ArithShl
	ArithShr
	ArithUnm
	ArithBNot
)

// IsBitwise reports whether op works on integers only.
func (op ArithOp) IsBitwise() bool {
	return op == ArithBAnd || op == ArithBOr || op == ArithBXor || op == ArithShl || op == ArithShr || op == ArithBNot
}

// IsUnary reports whether op takes one operand.
func (op ArithOp) IsUnary() bool { return op == ArithUnm || op == ArithBNot }

// An ArithError says why Arith could not compute a result.
type ArithError int

const (
	ArithOK        ArithError = iota
	ArithDivZero              // integer division by zero: "attempt to divide by zero"
	ArithModZero              // integer modulo by zero: "attempt to perform 'n%0'"
	ArithNoInteger            // a bitwise operand that is a float with no integer value
)

// Arith computes op on a and b as Lua 5.5 does. For a unary operator, b is
// ignored. Integers give integers, except for / and ^; a float operand
// makes the result a float. Bitwise operators convert floats with integral
// values to integers.
func Arith(op ArithOp, a, b Number) (Number, ArithError) {
	if op.IsBitwise() {
		x, ok := a.ToInteger()
		y, ok2 := b.ToInteger()
		if op == ArithBNot {
			ok2, y = true, 0
		}
		if !ok || !ok2 {
			return Number{}, ArithNoInteger
		}
		return Integer(IntBitwise(op, x, y)), ArithOK
	}
	if op != ArithDiv && op != ArithPow && a.IsInt && (b.IsInt || op == ArithUnm) {
		switch op {
		case ArithIDiv:
			if b.Int == 0 {
				return Number{}, ArithDivZero
			}
		case ArithMod:
			if b.Int == 0 {
				return Number{}, ArithModZero
			}
		}
		return Integer(IntArith(op, a.Int, b.Int)), ArithOK
	}
	return Float(FloatArith(op, a.ToFloat(), b.ToFloat())), ArithOK
}

// IntArith computes an arithmetic op on integers, wrapping around on
// overflow. // and % by zero must be ruled out first.
func IntArith(op ArithOp, a, b int64) int64 {
	switch op {
	case ArithAdd:
		return a + b
	case ArithSub:
		return a - b
	case ArithMul:
		return a * b
	case ArithIDiv:
		return IntFloorDiv(a, b)
	case ArithMod:
		return IntMod(a, b)
	case ArithUnm:
		return -a
	}
	panic("not an integer arithmetic operator")
}

// IntFloorDiv is a // b for b != 0, rounding toward minus infinity. -1 is
// a special case: minint // -1 wraps around to minint, as in C Lua.
func IntFloorDiv(a, b int64) int64 {
	if b == -1 {
		return -a
	}
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// IntMod is a % b for b != 0, with the sign of b.
func IntMod(a, b int64) int64 {
	if b == -1 {
		return 0
	}
	m := a % b
	if m != 0 && (m^b) < 0 {
		m += b
	}
	return m
}

// IntBitwise computes a bitwise op on integers. Shifts are logical, a
// negative shift goes the other way, and a shift of 64 or more gives 0.
func IntBitwise(op ArithOp, a, b int64) int64 {
	switch op {
	case ArithBAnd:
		return a & b
	case ArithBOr:
		return a | b
	case ArithBXor:
		return a ^ b
	case ArithShl:
		return ShiftLeft(a, b)
	case ArithShr:
		return ShiftLeft(a, -b)
	case ArithBNot:
		return ^a
	}
	panic("not a bitwise operator")
}

// ShiftLeft shifts a left by n bits, or right by -n, logically.
func ShiftLeft(a, n int64) int64 {
	switch {
	case n <= -64 || n >= 64:
		return 0
	case n >= 0:
		return int64(uint64(a) << uint(n))
	}
	return int64(uint64(a) >> uint(-n))
}

// FloatArith computes an arithmetic op on floats.
func FloatArith(op ArithOp, a, b float64) float64 {
	switch op {
	case ArithAdd:
		return a + b
	case ArithSub:
		return a - b
	case ArithMul:
		return a * b
	case ArithDiv:
		return a / b
	case ArithMod:
		return FloatMod(a, b)
	case ArithIDiv:
		return math.Floor(a / b)
	case ArithPow:
		return Pow(a, b)
	case ArithUnm:
		return -a
	}
	panic("not a float arithmetic operator")
}

// FloatMod is a % b for floats, as Lua 5.4's luai_nummod: fmod, then the
// divisor's sign.
func FloatMod(a, b float64) float64 {
	m := math.Mod(a, b)
	if (m > 0 && b < 0) || (m < 0 && b != m && b > 0) {
		m += b
	}
	return m
}

// Pow is a ^ b. math.Pow can be 1 ulp off for powers of ten, which C folds
// exactly.
func Pow(a, b float64) float64 {
	if a == 10.0 && minPow10 <= b && b <= maxPow10 && math.Trunc(b) == b {
		return pow10[int(b)-minPow10]
	}
	return math.Pow(a, b)
}

// ParseNumber converts s, a numeral with an optional sign and surrounding
// space, to a number, as Lua's tonumber does: a decimal or hexadecimal
// integer is an integer, wrapping around if hexadecimal; a decimal integer
// too large is a float, as is any numeral with a point or an exponent.
func ParseNumber(s string) (Number, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.IndexByte(s, 0) >= 0 {
		return Number{}, false
	}
	negative := false
	body := s
	if body[0] == '-' || body[0] == '+' {
		negative, body = body[0] == '-', body[1:]
	}
	if negative && strings.Trim(body, "0123456789") == "" {
		// -2^63 is an integer, though 2^63 is not: l_str2int reads the
		// sign first.
		if u, err := strconv.ParseUint(body, 10, 64); err == nil && u == 1<<63 {
			return Integer(math.MinInt64), true
		}
	}
	n, ok := Numeral(body)
	if !ok {
		return Number{}, false
	}
	if negative {
		if n.IsInt {
			n.Int = -n.Int
		} else {
			n.Float = -n.Float
		}
	}
	return n, true
}

// Numeral converts an unsigned numeral as llex.c reads one: digits, or 0x
// and hexadecimal digits, with an optional fraction and exponent.
func Numeral(s string) (Number, bool) {
	hex := len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X')
	if hex {
		digits := s[2:]
		if digits != "" && strings.Trim(digits, "0123456789abcdefABCDEF") == "" {
			var i uint64
			for j := 0; j < len(digits); j++ {
				i = i<<4 | uint64(hexDigit(digits[j])) // wraps around
			}
			return Integer(int64(i)), true
		}
		f, ok := hexFloat(digits)
		return Float(f), ok
	}
	if s == "" || !isDigit(s[0]) && !(s[0] == '.' && len(s) > 1 && isDigit(s[1])) {
		return Number{}, false
	}
	if strings.Trim(s, "0123456789") == "" {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return Integer(i), true
		}
		// Too large: a float, as Lua 5.4 reads it.
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		var numErr *strconv.NumError
		if !isRange(err, &numErr) {
			return Number{}, false
		}
	}
	return Float(f), true
}

func isRange(err error, target **strconv.NumError) bool {
	ne, ok := err.(*strconv.NumError)
	if !ok {
		return false
	}
	*target = ne
	return ne.Err == strconv.ErrRange
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func hexDigit(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// hexFloat converts a hexadecimal numeral after its 0x, as lobject.c's
// lua_strx2number does: digits, an optional fraction and an optional
// binary exponent.
func hexFloat(s string) (float64, bool) {
	var r float64
	var digits, e int
	anyDigit := false
	for ; s != "" && isHex(s[0]); s = s[1:] {
		r = r*16 + float64(hexDigit(s[0]))
		anyDigit = true
		digits++
	}
	if s != "" && s[0] == '.' {
		s = s[1:]
		for ; s != "" && isHex(s[0]); s = s[1:] {
			r = r*16 + float64(hexDigit(s[0]))
			anyDigit = true
			e -= 4
		}
	}
	if !anyDigit {
		return 0, false
	}
	if s != "" && (s[0] == 'p' || s[0] == 'P') {
		s = s[1:]
		negative := false
		if s != "" && (s[0] == '+' || s[0] == '-') {
			negative, s = s[0] == '-', s[1:]
		}
		if s == "" {
			return 0, false
		}
		exp := 0
		for ; s != "" && isDigit(s[0]); s = s[1:] {
			if exp < 1<<20 {
				exp = exp*10 + int(s[0]-'0')
			}
		}
		if negative {
			exp = -exp
		}
		e += exp
	}
	if s != "" {
		return 0, false
	}
	return math.Ldexp(r, e), true
}

func isHex(c byte) bool { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }

// FormatFloat formats f as Lua 5.5's tostring does: %.15g, or %.17g when
// that would not read back as f, with ".0" added when the result looks
// like an integer. Infinities and NaNs are "inf", "-inf", "nan" and
// "-nan", as C's printf writes them.
func FormatFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f) && math.Signbit(f):
		return "-nan"
	case math.IsNaN(f):
		return "nan"
	}
	s := FormatG(f, 15)
	if back, err := strconv.ParseFloat(s, 64); err != nil || back != f {
		s = FormatG(f, 17)
	}
	if strings.Trim(s, "-0123456789") == "" {
		s += ".0"
	}
	return s
}

// FormatG formats f as C's %.<precision>g does.
func FormatG(f float64, precision int) string { return strconv.FormatFloat(f, 'g', precision, 64) }
