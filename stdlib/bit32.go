package stdlib

import (
	"math"

	"github.com/matjam/luart/lua"
)

const bitCount = 32

func trim(x uint) uint { return x & math.MaxUint32 }
func mask(n uint) uint { return ^(math.MaxUint32 << n) }

func shift(l *lua.State, r uint, i int) int {
	if i < 0 {
		if i, r = -i, trim(r); i >= bitCount {
			r = 0
		} else {
			r >>= uint(i)
		}
	} else {
		if i >= bitCount {
			r = 0
		} else {
			r <<= uint(i)
		}
		r = trim(r)
	}
	l.PushUnsigned(r)
	return 1
}

func rotate(l *lua.State, i int) int {
	r := trim(l.CheckUnsigned(1))
	if i &= bitCount - 1; i != 0 {
		r = trim((r << uint(i)) | (r >> uint(bitCount-i)))
	}
	l.PushUnsigned(r)
	return 1
}

func bitOp(l *lua.State, init uint, f func(a, b uint) uint) uint {
	r := init
	for i, n := 1, l.Top(); i <= n; i++ {
		r = f(r, l.CheckUnsigned(i))
	}
	return trim(r)
}

func andHelper(l *lua.State) uint {
	x := bitOp(l, ^uint(0), func(a, b uint) uint { return a & b })
	return x
}

func fieldArguments(l *lua.State, fieldIndex int) (uint, uint) {
	f, w := l.CheckInteger(fieldIndex), l.OptInteger(fieldIndex+1, 1)
	l.ArgumentCheck(0 <= f, fieldIndex, "field cannot be negative")
	l.ArgumentCheck(0 < w, fieldIndex+1, "width must be positive")
	if f+w > bitCount {
		l.Errorf("trying to access non-existent bits")
	}
	return uint(f), uint(w)
}

var bitLibrary = []lua.RegistryFunction{
	{Name: "arshift", Function: func(l *lua.State) int {
		r, i := l.CheckUnsigned(1), l.CheckInteger(2)
		if i < 0 || (r&(1<<(bitCount-1)) == 0) {
			return shift(l, r, -i)
		}

		if i >= bitCount {
			r = math.MaxUint32
		} else {
			r = trim((r >> uint(i)) | ^(math.MaxUint32 >> uint(i)))
		}
		l.PushUnsigned(r)
		return 1
	}},
	{Name: "band", Function: func(l *lua.State) int { l.PushUnsigned(andHelper(l)); return 1 }},
	{Name: "bnot", Function: func(l *lua.State) int { l.PushUnsigned(trim(^l.CheckUnsigned(1))); return 1 }},
	{Name: "bor", Function: func(l *lua.State) int { l.PushUnsigned(bitOp(l, 0, func(a, b uint) uint { return a | b })); return 1 }},
	{Name: "bxor", Function: func(l *lua.State) int { l.PushUnsigned(bitOp(l, 0, func(a, b uint) uint { return a ^ b })); return 1 }},
	{Name: "btest", Function: func(l *lua.State) int { l.PushBoolean(andHelper(l) != 0); return 1 }},
	{Name: "extract", Function: func(l *lua.State) int {
		r := l.CheckUnsigned(1)
		f, w := fieldArguments(l, 2)
		l.PushUnsigned((r >> f) & mask(w))
		return 1
	}},
	{Name: "lrotate", Function: func(l *lua.State) int { return rotate(l, l.CheckInteger(2)) }},
	{Name: "lshift", Function: func(l *lua.State) int { return shift(l, l.CheckUnsigned(1), l.CheckInteger(2)) }},
	{Name: "replace", Function: func(l *lua.State) int {
		r, v := l.CheckUnsigned(1), l.CheckUnsigned(2)
		f, w := fieldArguments(l, 3)
		m := mask(w)
		v &= m
		l.PushUnsigned((r & ^(m << f)) | (v << f))
		return 1
	}},
	{Name: "rrotate", Function: func(l *lua.State) int { return rotate(l, -l.CheckInteger(2)) }},
	{Name: "rshift", Function: func(l *lua.State) int { return shift(l, l.CheckUnsigned(1), -l.CheckInteger(2)) }},
}

// OpenBit32 opens the bit32 library. Usually passed to Require.
func OpenBit32(l *lua.State) int {
	l.NewLibrary(bitLibrary)
	return 1
}
