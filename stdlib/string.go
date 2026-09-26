package stdlib

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

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

// The flags each conversion takes, as lstrlib.c's L_FMTFLAGS.
const (
	formatFlagsF = "-+#0 " // a, A, e, E, f, g, G
	formatFlagsX = "-#0"   // o, x, X
	formatFlagsI = "-+0 "  // d, i
	formatFlagsU = "-0"    // u
	formatFlagsC = "-"     // c, p, s
)

// getFormat returns the conversion spec at the start of fs, after its '%':
// flags, width, precision and the conversion, as lstrlib.c's getformat.
func getFormat(l *lua.State, fs string) string {
	n := 0
	for n < len(fs) && strings.IndexByte(formatFlagsF+"123456789.", fs[n]) >= 0 {
		n++
	}
	if n++; n >= 32-10 {
		l.Errorf("invalid format (too long)")
	}
	if n > len(fs) {
		n = len(fs)
	}
	return "%" + fs[:n]
}

// checkFormat raises an error unless spec takes only flags, and a width
// and precision of up to two digits each, as lstrlib.c's checkformat.
func checkFormat(l *lua.State, spec, flags string, precision bool) {
	s := spec[1:]
	for s != "" && strings.IndexByte(flags, s[0]) >= 0 {
		s = s[1:]
	}
	twoDigits := func() {
		for k := 0; k < 2 && s != "" && '0' <= s[0] && s[0] <= '9'; k++ {
			s = s[1:]
		}
	}
	if s == "" || s[0] != '0' {
		twoDigits()
		if s != "" && s[0] == '.' && precision {
			s = s[1:]
			twoDigits()
		}
	}
	if s == "" || !('a' <= s[0] && s[0] <= 'z' || 'A' <= s[0] && s[0] <= 'Z') {
		l.Errorf("invalid conversion specification: '%s'", spec)
	}
}

// formatHelper is string.format, after Lua 5.5's str_format.
func formatHelper(l *lua.State, fs string, argCount int) string {
	var b bytes.Buffer
	for i, arg := 0, 1; i < len(fs); i++ {
		if fs[i] != '%' {
			b.WriteByte(fs[i])
			continue
		}
		if i++; i < len(fs) && fs[i] == '%' {
			b.WriteByte('%')
			continue
		}
		if arg++; arg > argCount {
			l.ArgumentError(arg, "no value")
		}
		spec := getFormat(l, fs[i:])
		i += len(spec) - 2
		switch conv := spec[len(spec)-1]; conv {
		case 'c':
			checkFormat(l, spec, formatFlagsC, false)
			c := byte(l.CheckInteger(arg))
			fmt.Fprintf(&b, spec, 'x')
			buf := b.Bytes()
			buf[len(buf)-1] = c // one byte, whatever its value, as C writes it
		case 'd', 'i', 'u', 'o', 'x', 'X':
			n := l.CheckInteger(arg)
			flags := formatFlagsX
			switch conv {
			case 'd', 'i':
				flags = formatFlagsI
			case 'u':
				flags = formatFlagsU
			}
			checkFormat(l, spec, flags, true)
			switch conv {
			case 'd', 'i':
				fmt.Fprintf(&b, spec[:len(spec)-1]+"d", n)
			case 'u':
				fmt.Fprintf(&b, spec[:len(spec)-1]+"d", uint64(n))
			default:
				fmt.Fprintf(&b, spec, uint64(n))
			}
		case 'a', 'A':
			checkFormat(l, spec, formatFlagsF, true)
			b.WriteString(formatHexFloat(spec, l.CheckNumber(arg)))
		case 'e', 'E', 'f', 'F', 'g', 'G':
			n := l.CheckNumber(arg)
			checkFormat(l, spec, formatFlagsF, true)
			if math.IsInf(n, 0) || math.IsNaN(n) {
				b.WriteString(formatNonFinite(spec, n))
			} else if (conv == 'g' || conv == 'G') && !strings.Contains(spec, ".") {
				fmt.Fprintf(&b, spec[:len(spec)-1]+".6"+string(conv), n) // C's default precision
			} else {
				fmt.Fprintf(&b, spec, n)
			}
		case 'p':
			checkFormat(l, spec, formatFlagsC, false)
			if p := l.ToPointer(arg); p != 0 {
				fmt.Fprintf(&b, spec[:len(spec)-1]+"s", fmt.Sprintf("%#x", p))
			} else {
				fmt.Fprintf(&b, spec[:len(spec)-1]+"s", "(null)")
			}
		case 'q':
			if len(spec) > 2 {
				l.Errorf("specifier '%%q' cannot have modifiers")
			}
			addLiteral(l, &b, arg)
		case 's':
			str, _ := l.ToStringMeta(arg)
			l.Pop(1)
			if len(spec) == 2 {
				b.WriteString(str) // no modifiers: the whole string
				break
			}
			l.ArgumentCheck(strings.IndexByte(str, 0) < 0, arg, "string contains zeros")
			checkFormat(l, spec, formatFlagsC, true)
			if !strings.Contains(spec, ".") && len(str) >= 100 {
				b.WriteString(str) // too long to format: kept whole
			} else {
				fmt.Fprintf(&b, spec, str)
			}
		default:
			l.Errorf("invalid conversion '%s' to 'format'", spec)
		}
	}
	return b.String()
}

// addLiteral writes the value at arg as Lua source that reads it back:
// strings quoted, integers in decimal (minint in hexadecimal), floats in
// hexadecimal, as lstrlib.c's addliteral does.
func addLiteral(l *lua.State, b *bytes.Buffer, arg int) {
	switch l.TypeOf(arg) {
	case lua.TypeString:
		s, _ := l.ToString(arg)
		b.WriteByte('"')
		for i := 0; i < len(s); i++ {
			switch c := s[i]; {
			case c == '"' || c == '\\' || c == '\n':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c < 0x20 || c == 0x7f:
				if i+1 < len(s) && '0' <= s[i+1] && s[i+1] <= '9' {
					fmt.Fprintf(b, "\\%03d", c)
				} else {
					fmt.Fprintf(b, "\\%d", c)
				}
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
	case lua.TypeNumber:
		if n, ok := l.ToInteger(arg); ok && l.IsInteger(arg) {
			if n == math.MinInt64 {
				fmt.Fprintf(b, "0x%x", uint64(n))
			} else {
				fmt.Fprintf(b, "%d", n)
			}
			break
		}
		switch n, _ := l.ToNumber(arg); {
		case math.IsInf(n, 1):
			b.WriteString("1e9999")
		case math.IsInf(n, -1):
			b.WriteString("-1e9999")
		case math.IsNaN(n):
			b.WriteString("(0/0)")
		default:
			b.WriteString(formatHexFloat("%a", n))
		}
	case lua.TypeNil, lua.TypeBoolean:
		s, _ := l.ToStringMeta(arg)
		l.Pop(1)
		b.WriteString(s)
	default:
		l.ArgumentError(arg, "value has no literal form")
	}
}

var stringLibrary = []lua.RegistryFunction{
	{Name: "byte", Function: func(l *lua.State) int {
		s := l.CheckString(1)
		start := relativePosition(optInt(l, 2, 1), len(s))
		end := relativePosition(optInt(l, 3, start), len(s))
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
			c := checkInt(l, i)
			l.ArgumentCheck(int(byte(c)) == c, i, "value out of range")
			b.WriteByte(byte(c))
		}
		l.PushString(b.String())
		return 1
	}},
	{Name: "dump", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeFunction)
		strip := l.ToBoolean(2)
		l.SetTop(1)
		var b bytes.Buffer
		if err := l.Dump(&b, strip); err != nil {
			l.Errorf("unable to dump given function")
		}
		l.PushString(b.String())
		return 1
	}},
	{Name: "find", Function: func(l *lua.State) int { return find(l, true) }},
	{Name: "format", Function: func(l *lua.State) int {
		l.PushString(formatHelper(l, l.CheckString(1), l.Top()))
		return 1
	}},
	{Name: "gmatch", Function: gmatch},
	{Name: "gsub", Function: gsub},
	{Name: "len", Function: func(l *lua.State) int { l.PushInteger(len(l.CheckString(1))); return 1 }},
	{Name: "lower", Function: func(l *lua.State) int { l.PushString(changeCase(l.CheckString(1), 'A', 'Z')); return 1 }},
	{Name: "match", Function: func(l *lua.State) int { return find(l, false) }},
	{Name: "rep", Function: func(l *lua.State) int {
		s, n, sep := l.CheckString(1), checkInt(l, 2), l.OptString(3, "")
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
		b := []byte(l.CheckString(1))
		slices.Reverse(b)
		l.PushString(string(b))
		return 1
	}},
	{Name: "sub", Function: func(l *lua.State) int {
		s := l.CheckString(1)
		start, end := relativePosition(checkInt(l, 2), len(s)), relativePosition(optInt(l, 3, -1), len(s))
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
	{Name: "upper", Function: func(l *lua.State) int { l.PushString(changeCase(l.CheckString(1), 'a', 'z')); return 1 }},
}

// formatNonFinite formats an infinity or NaN n as C's printf does for the
// conversion spec, such as "%+8.2E": "inf" or "nan", signed by n's sign
// bit or the '+' and ' ' flags, in capitals for E and G, and padded to
// the width with spaces; the precision and the '0' flag do not apply.
func formatNonFinite(spec string, n float64) string {
	s := "inf"
	if math.IsNaN(n) {
		s = "nan"
	}
	flags := spec[1 : len(spec)-1] // flags, width and precision
	switch {
	case math.Signbit(n):
		s = "-" + s
	case strings.Contains(flags, "+"):
		s = "+" + s
	case strings.Contains(flags, " "):
		s = " " + s
	}
	if verb := spec[len(spec)-1]; verb == 'E' || verb == 'G' {
		s = strings.ToUpper(s)
	}
	width := strings.TrimLeft(flags, "-+ #0")
	if i := strings.IndexByte(width, '.'); i >= 0 {
		width = width[:i]
	}
	w, _ := strconv.Atoi(width)
	pad := strings.Repeat(" ", max(0, w-len(s)))
	if strings.Contains(flags, "-") {
		return s + pad
	}
	return pad + s
}

// formatHexFloat formats n as glibc's printf does for the %a or %A
// conversion spec: a hexadecimal fraction and a binary exponent without
// leading zeros, such as 0x1.8p+1, with C's flags, width and precision.
func formatHexFloat(spec string, n float64) string {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return formatNonFinite(spec, n)
	}
	body := spec[1 : len(spec)-1]
	i := 0
	for i < len(body) && strings.IndexByte("-+ #0", body[i]) >= 0 {
		i++
	}
	flags, rest := body[:i], body[i:]
	precision := -1 // as many digits as needed
	if j := strings.IndexByte(rest, '.'); j >= 0 {
		precision, _ = strconv.Atoi(rest[j+1:]) // "." alone is 0, as in C
		rest = rest[:j]
	}
	width, _ := strconv.Atoi(rest)
	s := strconv.FormatFloat(math.Abs(n), 'x', precision, 64) // such as 0x1.8p+01
	p := strings.IndexByte(s, 'p')
	exponent := strings.TrimLeft(s[p+2:], "0")
	if exponent == "" {
		exponent = "0"
	}
	mantissa := s[:p]
	if strings.Contains(flags, "#") && !strings.Contains(mantissa, ".") {
		mantissa += "."
	}
	s = mantissa + s[p:p+2] + exponent
	if spec[len(spec)-1] == 'A' {
		s = strings.ToUpper(s)
	}
	sign := ""
	switch {
	case math.Signbit(n):
		sign = "-"
	case strings.Contains(flags, "+"):
		sign = "+"
	case strings.Contains(flags, " "):
		sign = " "
	}
	pad := width - len(sign) - len(s)
	switch {
	case pad <= 0:
		return sign + s
	case strings.Contains(flags, "-"):
		return sign + s + strings.Repeat(" ", pad)
	case strings.Contains(flags, "0"): // zeros after the 0x
		return sign + s[:2] + strings.Repeat("0", pad) + s[2:]
	}
	return strings.Repeat(" ", pad) + sign + s
}

// changeCase flips the case of the letters from lo to hi in s. string.lower
// and string.upper change only ASCII letters, as Lua's do in the C locale,
// and leave other bytes as they are.
func changeCase(s string, lo, hi byte) string {
	i := 0
	for i < len(s) && (s[i] < lo || hi < s[i]) {
		i++
	}
	if i == len(s) {
		return s
	}
	b := []byte(s)
	for ; i < len(b); i++ {
		if lo <= b[i] && b[i] <= hi {
			b[i] ^= 'a' - 'A'
		}
	}
	return string(b)
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
