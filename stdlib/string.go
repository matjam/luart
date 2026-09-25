package stdlib

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/matjam/luart/lua"
)

func relativePosition(pos, length int) int {
	if pos >= 0 {
		return pos
	} else if -pos > length {
		return 0
	}
	return length + pos + 1
}

func findHelper(l *lua.State, isFind bool) int {
	s, p := l.CheckString(1), l.CheckString(2)
	init := relativePosition(l.OptInteger(3, 1), len(s))
	if init < 1 {
		init = 1
	} else if init > len(s)+1 {
		l.PushNil()
		return 1
	}
	isPlain := l.TypeOf(4) == lua.TypeNone || l.ToBoolean(4)
	if isFind && (isPlain || !strings.ContainsAny(p, "^$*+?.([%-")) {
		if start := strings.Index(s[init-1:], p); start >= 0 {
			l.PushInteger(start + init)
			l.PushInteger(start + init + len(p) - 1)
			return 2
		}
	} else {
		l.Errorf("patterns are not supported yet") // TODO implement pattern matching
	}
	l.PushNil()
	return 1
}

func scanFormat(l *lua.State, fs string) string {
	i := 0
	skipDigit := func() {
		if unicode.IsDigit(rune(fs[i])) {
			i++
		}
	}
	flags := "-+ #0"
	for i < len(fs) && strings.ContainsRune(flags, rune(fs[i])) {
		i++
	}
	if i >= len(flags) {
		l.Errorf("invalid format (repeated flags)")
	}
	skipDigit()
	skipDigit()
	if fs[i] == '.' {
		i++
		skipDigit()
		skipDigit()
	}
	if unicode.IsDigit(rune(fs[i])) {
		l.Errorf("invalid format (width or precision too long)")
	}
	i++
	return "%" + fs[:i]
}

func formatHelper(l *lua.State, fs string, argCount int) string {
	var b bytes.Buffer
	for i, arg := 0, 1; i < len(fs); i++ {
		if fs[i] != '%' {
			b.WriteByte(fs[i])
		} else if i++; fs[i] == '%' {
			b.WriteByte(fs[i])
		} else {
			if arg++; arg > argCount {
				l.ArgumentError(arg, "no value")
			}
			f := scanFormat(l, fs[i:])
			switch i += len(f) - 2; fs[i] {
			case 'c':
				// Ensure each character is represented by a single byte, while preserving format modifiers.
				c := l.CheckInteger(arg)
				fmt.Fprintf(&b, f, 'x')
				buf := b.Bytes()
				buf[len(buf)-1] = byte(c)
			case 'i': // The fmt package doesn't support %i.
				f = f[:len(f)-1] + "d"
				fallthrough
			case 'd':
				n := l.CheckNumber(arg)
				l.ArgumentCheck(math.Floor(n) == n && -math.Pow(2, 63) <= n && n < math.Pow(2, 63), arg, "number has no integer representation")
				ni := int(n)
				fmt.Fprintf(&b, f, ni)
			case 'u': // The fmt package doesn't support %u.
				f = f[:len(f)-1] + "d"
				n := l.CheckNumber(arg)
				l.ArgumentCheck(math.Floor(n) == n && 0.0 <= n && n < math.Pow(2, 64), arg, "not a non-negative number in proper range")
				ni := uint(n)
				fmt.Fprintf(&b, f, ni)
			case 'o', 'x', 'X':
				n := l.CheckNumber(arg)
				l.ArgumentCheck(0.0 <= n && n < math.Pow(2, 64), arg, "not a non-negative number in proper range")
				ni := uint(n)
				fmt.Fprintf(&b, f, ni)
			case 'e', 'E', 'f', 'g', 'G':
				fmt.Fprintf(&b, f, l.CheckNumber(arg))
			case 'q':
				s := l.CheckString(arg)
				b.WriteByte('"')
				for i := 0; i < len(s); i++ {
					switch s[i] {
					case '"', '\\', '\n':
						b.WriteByte('\\')
						b.WriteByte(s[i])
					default:
						if 0x20 <= s[i] && s[i] != 0x7f { // ASCII control characters don't correspond to a Unicode range.
							b.WriteByte(s[i])
						} else if i+1 < len(s) && unicode.IsDigit(rune(s[i+1])) {
							fmt.Fprintf(&b, "\\%03d", s[i])
						} else {
							fmt.Fprintf(&b, "\\%d", s[i])
						}
					}
				}
				b.WriteByte('"')
			case 's':
				if s, _ := l.ToStringMeta(arg); !strings.ContainsRune(f, '.') && len(s) >= 100 {
					b.WriteString(s)
				} else {
					fmt.Fprintf(&b, f, s)
				}
			default:
				l.Errorf(fmt.Sprintf("invalid option '%%%c' to 'format'", fs[i]))
			}
		}
	}
	return b.String()
}

var stringLibrary = []lua.RegistryFunction{
	{Name: "byte", Function: func(l *lua.State) int {
		s := l.CheckString(1)
		start := relativePosition(l.OptInteger(2, 1), len(s))
		end := relativePosition(l.OptInteger(3, start), len(s))
		if start < 1 {
			start = 1
		}
		if end > len(s) {
			end = len(s)
		}
		if start > end {
			return 0
		}
		n := end - start + 1
		if start+n <= end {
			l.Errorf("string slice too long")
		}
		l.CheckStackWithMessage(n, "string slice too long")
		for _, c := range []byte(s[start-1 : end]) {
			l.PushInteger(int(c))
		}
		return n
	}},
	{Name: "char", Function: func(l *lua.State) int {
		var b bytes.Buffer
		for i, n := 1, l.Top(); i <= n; i++ {
			c := l.CheckInteger(i)
			l.ArgumentCheck(int(byte(c)) == c, i, "value out of range")
			b.WriteByte(byte(c))
		}
		l.PushString(b.String())
		return 1
	}},
	// {"dump", ...},
	{Name: "find", Function: func(l *lua.State) int { return findHelper(l, true) }},
	{Name: "format", Function: func(l *lua.State) int {
		l.PushString(formatHelper(l, l.CheckString(1), l.Top()))
		return 1
	}},
	// {"gmatch", ...},
	// {"gsub", ...},
	{Name: "len", Function: func(l *lua.State) int { l.PushInteger(len(l.CheckString(1))); return 1 }},
	{Name: "lower", Function: func(l *lua.State) int { l.PushString(strings.ToLower(l.CheckString(1))); return 1 }},
	// {"match", ...},
	{Name: "rep", Function: func(l *lua.State) int {
		s, n, sep := l.CheckString(1), l.CheckInteger(2), l.OptString(3, "")
		if n <= 0 {
			l.PushString("")
		} else if len(s)+len(sep) < len(s) || len(s)+len(sep) >= math.MaxInt/n {
			l.Errorf("resulting string too large")
		} else if sep == "" {
			l.PushString(strings.Repeat(s, n))
		} else {
			var b bytes.Buffer
			b.Grow(n*len(s) + (n-1)*len(sep))
			b.WriteString(s)
			for ; n > 1; n-- {
				b.WriteString(sep)
				b.WriteString(s)
			}
			l.PushString(b.String())
		}
		return 1
	}},
	{Name: "reverse", Function: func(l *lua.State) int {
		r := []rune(l.CheckString(1))
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		l.PushString(string(r))
		return 1
	}},
	{Name: "sub", Function: func(l *lua.State) int {
		s := l.CheckString(1)
		start, end := relativePosition(l.CheckInteger(2), len(s)), relativePosition(l.OptInteger(3, -1), len(s))
		if start < 1 {
			start = 1
		}
		if end > len(s) {
			end = len(s)
		}
		if start <= end {
			l.PushString(s[start-1 : end])
		} else {
			l.PushString("")
		}
		return 1
	}},
	{Name: "upper", Function: func(l *lua.State) int { l.PushString(strings.ToUpper(l.CheckString(1))); return 1 }},
}

// OpenString opens the string library. Usually passed to Require.
func OpenString(l *lua.State) int {
	l.NewLibrary(stringLibrary)
	l.CreateTable(0, 1)
	l.PushString("")
	l.PushValue(-2)
	l.SetMetaTable(-2)
	l.Pop(1)
	l.PushValue(-2)
	l.SetField(-2, "__index")
	l.Pop(1)
	return 1
}
