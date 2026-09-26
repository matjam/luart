package stdlib

import (
	"github.com/matjam/luart/internal/luautf8"
	"github.com/matjam/luart/lua"
)

// The utf8 library, after lutf8lib.c. It decodes as Lua does: strictly,
// code points up to U+10FFFF without surrogates, or, lax, any value up to
// 2^31-1 in up to six bytes.

const invalidUTF8 = "invalid UTF-8 code"

// utf8Position is lutf8lib.c's u_posrelat: negative counts back from the
// end, and before the start is 0.
func utf8Position(pos int64, length int) int64 {
	switch {
	case pos >= 0:
		return pos
	case -pos > int64(length):
		return 0
	}
	return int64(length) + pos + 1
}

// continuationAt reports whether s has a continuation byte at i; past the
// end is C's terminating 0, which is not one.
func continuationAt(s string, i int64) bool {
	return 0 <= i && i < int64(len(s)) && luautf8.IsContinuation(s[i])
}

// utf8Len is utf8.len(s [, i [, j [, lax]]]): how many characters start
// in [i, j], or fail and the position of the first invalid byte.
func utf8Len(l *lua.State) int {
	s := l.CheckString(1)
	i := utf8Position(l.OptInteger(2, 1), len(s))
	j := utf8Position(l.OptInteger(3, -1), len(s))
	lax := l.ToBoolean(4)
	i--
	l.ArgumentCheck(0 <= i && i <= int64(len(s)), 2, "initial position out of bounds")
	j--
	l.ArgumentCheck(j < int64(len(s)), 3, "final position out of bounds")
	var n int64
	for i <= j {
		_, size, ok := luautf8.Decode(s[i:], !lax)
		if !ok {
			l.PushNil()
			l.PushInteger(i + 1)
			return 2
		}
		i += int64(size)
		n++
	}
	l.PushInteger(n)
	return 1
}

// utf8CodePoint is utf8.codepoint(s [, i [, j [, lax]]]): the code points
// of the characters that start in [i, j].
func utf8CodePoint(l *lua.State) int {
	s := l.CheckString(1)
	i := utf8Position(l.OptInteger(2, 1), len(s))
	j := utf8Position(l.OptInteger(3, i), len(s))
	lax := l.ToBoolean(4)
	l.ArgumentCheck(i >= 1, 2, "out of bounds")
	l.ArgumentCheck(j <= int64(len(s)), 3, "out of bounds")
	if i > j {
		return 0
	}
	l.CheckStackWithMessage(int(j-i)+1, "string slice too long")
	n := 0
	for p := i - 1; p < j; {
		code, size, ok := luautf8.Decode(s[p:], !lax)
		if !ok {
			l.Errorf(invalidUTF8)
		}
		l.PushInteger(int64(code))
		p += int64(size)
		n++
	}
	return n
}

// utf8Char is utf8.char(...): the characters of the given code points.
func utf8Char(l *lua.State) int {
	var b []byte
	for i := 1; i <= l.Top(); i++ {
		code := uint64(l.CheckInteger(i))
		l.ArgumentCheck(code <= luautf8.MaxUTF, i, "value out of range")
		b = append(b, luautf8.Encode(uint32(code))...)
	}
	l.PushString(string(b))
	return 1
}

// utf8Offset is utf8.offset(s, n [, i]): where the n-th character from
// position i starts and ends, 0 meaning the one at i.
func utf8Offset(l *lua.State) int {
	s := l.CheckString(1)
	n := l.CheckInteger(2)
	def := int64(1)
	if n < 0 {
		def = int64(len(s)) + 1
	}
	i := utf8Position(l.OptInteger(3, def), len(s))
	i--
	l.ArgumentCheck(0 <= i && i <= int64(len(s)), 3, "position out of bounds")
	if n == 0 { // the start of the character at i
		for i > 0 && continuationAt(s, i) {
			i--
		}
	} else {
		if continuationAt(s, i) {
			l.Errorf("initial position is a continuation byte")
		}
		if n < 0 {
			for ; n < 0 && i > 0; n++ { // back to the start of the previous
				for i--; i > 0 && continuationAt(s, i); i-- {
				}
			}
		} else {
			for n--; n > 0 && i < int64(len(s)); n-- { // on to the next
				for i++; continuationAt(s, i); i++ {
				}
			}
		}
	}
	if n != 0 { // no such character
		l.PushNil()
		return 1
	}
	l.PushInteger(i + 1)
	if i < int64(len(s)) && s[i]&0x80 != 0 { // a multi-byte character: to its last byte
		if luautf8.IsContinuation(s[i]) {
			l.Errorf("initial position is a continuation byte")
		}
		for continuationAt(s, i+1) {
			i++
		}
	}
	l.PushInteger(i + 1)
	return 2
}

// utf8Iterate is utf8.codes' iterator: the position and code point of the
// character after position n.
func utf8Iterate(strict bool) lua.Function {
	return func(l *lua.State) int {
		s := l.CheckString(1)
		n, _ := l.ToInteger(2)
		p := uint64(n)
		for p < uint64(len(s)) && luautf8.IsContinuation(s[p]) {
			p++ // to the next character
		}
		if p >= uint64(len(s)) { // a negative n too
			return 0
		}
		code, size, ok := luautf8.Decode(s[p:], strict)
		if !ok || continuationAt(s, int64(p)+int64(size)) {
			l.Errorf(invalidUTF8)
		}
		l.PushInteger(int64(p) + 1)
		l.PushInteger(int64(code))
		return 2
	}
}

var utf8Strict, utf8Lax = utf8Iterate(true), utf8Iterate(false)

var utf8Library = []lua.RegistryFunction{
	{Name: "offset", Function: utf8Offset},
	{Name: "codepoint", Function: utf8CodePoint},
	{Name: "char", Function: utf8Char},
	{Name: "len", Function: utf8Len},
	{Name: "codes", Function: func(l *lua.State) int {
		lax := l.ToBoolean(2)
		s := l.CheckString(1)
		l.ArgumentCheck(!continuationAt(s, 0), 1, invalidUTF8)
		if lax {
			l.PushGoFunction(utf8Lax)
		} else {
			l.PushGoFunction(utf8Strict)
		}
		l.PushValue(1)
		l.PushInteger(0)
		return 3
	}},
}

// OpenUTF8 opens the utf8 library. Usually passed to Require.
func OpenUTF8(l *lua.State) int {
	l.NewLibrary(utf8Library)
	l.PushString("[\x00-\x7F\xC2-\xFD][\x80-\xBF]*")
	l.SetField(-2, "charpattern")
	return 1
}
