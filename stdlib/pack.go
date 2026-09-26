package stdlib

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/matjam/luart/lua"
)

// string.pack, string.packsize and string.unpack, after lstrlib.c. Sizes
// are those of C on a 64-bit machine, which is where the JIT runs: short
// 2, int 4, long, size_t and lua_Integer 8, float 4, double and
// lua_Number 8, with at most 8-byte alignment. The native order is
// little-endian on every platform luart builds for.

const (
	maxIntSize  = 16 // lstrlib.c's MAXINTSIZE
	intSize     = 8  // SZINT
	nativeAlign = 8  // offsetof(struct cD, u)
	maxPackSize = math.MaxInt64
)

// A packOption is a format option, lstrlib.c's KOption.
type packOption int

const (
	packInt      packOption = iota // signed integers
	packUint                       // unsigned integers
	packFloat                      // C floats
	packNumber                     // Lua floats
	packDouble                     // C doubles
	packChar                       // fixed-length strings
	packString                     // strings with prefixed length
	packZstr                       // zero-terminated strings
	packPadding                    // a padding byte
	packPadAlign                   // padding for alignment
	packNop                        // configuration or spaces
)

// A packHeader is the state of a format as it is read.
type packHeader struct {
	l        *lua.State
	little   bool
	maxAlign int
	fmt      string
}

func isPackDigit(c byte) bool { return '0' <= c && c <= '9' }

// number reads a numeral from the format, or returns def if there is none.
func (h *packHeader) number(def int) int {
	if h.fmt == "" || !isPackDigit(h.fmt[0]) {
		return def
	}
	a := 0
	for h.fmt != "" && isPackDigit(h.fmt[0]) && a <= (math.MaxInt-9)/10 {
		a = a*10 + int(h.fmt[0]-'0')
		h.fmt = h.fmt[1:]
	}
	return a
}

// numberLimit reads a size, which must be from 1 to 16.
func (h *packHeader) numberLimit(def int) int {
	n := h.number(def)
	if n < 1 || n > maxIntSize {
		h.l.Errorf("integral size (%d) out of limits [1,%d]", n, maxIntSize)
	}
	return n
}

// option reads and classifies the next option, returning its size.
func (h *packHeader) option() (packOption, int) {
	c := h.fmt[0]
	h.fmt = h.fmt[1:]
	switch c {
	case 'b':
		return packInt, 1
	case 'B':
		return packUint, 1
	case 'h':
		return packInt, 2
	case 'H':
		return packUint, 2
	case 'l', 'j':
		return packInt, 8
	case 'L', 'J', 'T':
		return packUint, 8
	case 'f':
		return packFloat, 4
	case 'n':
		return packNumber, 8
	case 'd':
		return packDouble, 8
	case 'i':
		return packInt, h.numberLimit(4)
	case 'I':
		return packUint, h.numberLimit(4)
	case 's':
		return packString, h.numberLimit(8)
	case 'c':
		n := h.number(-1)
		if n == -1 {
			h.l.Errorf("missing size for format option 'c'")
		}
		return packChar, n
	case 'z':
		return packZstr, 0
	case 'x':
		return packPadding, 1
	case 'X':
		return packPadAlign, 0
	case ' ':
	case '<':
		h.little = true
	case '>':
		h.little = false
	case '=':
		h.little = true // native
	case '!':
		h.maxAlign = h.numberLimit(nativeAlign)
	default:
		h.l.Errorf("invalid format option '%c'", c)
	}
	return packNop, 0
}

// details reads the next option, returning its size and the padding that
// aligns it after total bytes.
func (h *packHeader) details(total int) (opt packOption, size, pad int) {
	opt, size = h.option()
	align := size            // usually, alignment follows size
	if opt == packPadAlign { // 'X' takes its alignment from the next option
		if h.fmt == "" {
			h.l.ArgumentError(1, "invalid next option for option 'X'")
		}
		var next packOption
		if next, align = h.option(); next == packChar || align == 0 {
			h.l.ArgumentError(1, "invalid next option for option 'X'")
		}
	}
	if align <= 1 || opt == packChar {
		return opt, size, 0
	}
	align = min(align, h.maxAlign)
	if align&(align-1) != 0 {
		h.l.ArgumentError(1, "format asks for alignment not power of 2")
	}
	return opt, size, (align - total&(align-1)) & (align - 1)
}

// appendInt appends n in size bytes, sign-extending a negative n past 8.
func appendInt(b []byte, n uint64, little bool, size int, negative bool) []byte {
	buf := make([]byte, size)
	for i := range size {
		c := byte(n)
		if i >= intSize {
			c = 0
			if negative {
				c = 0xff
			}
		}
		if little {
			buf[i] = c
		} else {
			buf[size-1-i] = c
		}
		n >>= 8
	}
	return append(b, buf...)
}

// appendOrdered appends raw, little-endian bytes in the format's order.
func appendOrdered(b, raw []byte, little bool) []byte {
	if !little {
		for i, j := 0, len(raw)-1; i < j; i, j = i+1, j-1 {
			raw[i], raw[j] = raw[j], raw[i]
		}
	}
	return append(b, raw...)
}

func stringPack(l *lua.State) int {
	h := packHeader{l: l, little: true, maxAlign: 1, fmt: l.CheckString(1)}
	var b []byte
	arg, total := 1, 0
	for h.fmt != "" {
		opt, size, pad := h.details(total)
		l.ArgumentCheck(size+pad <= maxPackSize-total, arg, "result too long")
		total += pad + size
		for range pad {
			b = append(b, 0)
		}
		arg++
		switch opt {
		case packInt:
			n := l.CheckInteger(arg)
			if size < intSize {
				lim := int64(1) << (size*8 - 1)
				l.ArgumentCheck(-lim <= n && n < lim, arg, "integer overflow")
			}
			b = appendInt(b, uint64(n), h.little, size, n < 0)
		case packUint:
			n := l.CheckInteger(arg)
			if size < intSize {
				l.ArgumentCheck(uint64(n) < uint64(1)<<(size*8), arg, "unsigned overflow")
			}
			b = appendInt(b, uint64(n), h.little, size, false)
		case packFloat:
			raw := binary.LittleEndian.AppendUint32(nil, math.Float32bits(float32(l.CheckNumber(arg))))
			b = appendOrdered(b, raw, h.little)
		case packNumber, packDouble:
			raw := binary.LittleEndian.AppendUint64(nil, math.Float64bits(l.CheckNumber(arg)))
			b = appendOrdered(b, raw, h.little)
		case packChar:
			s := l.CheckString(arg)
			l.ArgumentCheck(len(s) <= size, arg, "string longer than given size")
			b = append(b, s...)
			for range size - len(s) {
				b = append(b, 0)
			}
		case packString:
			s := l.CheckString(arg)
			l.ArgumentCheck(size >= 8 || uint64(len(s)) < uint64(1)<<(size*8), arg, "string length does not fit in given size")
			b = appendInt(b, uint64(len(s)), h.little, size, false)
			b = append(b, s...)
			total += len(s)
		case packZstr:
			s := l.CheckString(arg)
			l.ArgumentCheck(!strings.ContainsRune(s, 0), arg, "string contains zeros")
			b = append(append(b, s...), 0)
			total += len(s) + 1
		case packPadding:
			b = append(b, 0)
			fallthrough
		case packPadAlign, packNop:
			arg-- // no argument
		}
	}
	l.PushString(string(b))
	return 1
}

func stringPackSize(l *lua.State) int {
	h := packHeader{l: l, little: true, maxAlign: 1, fmt: l.CheckString(1)}
	total := 0
	for h.fmt != "" {
		opt, size, pad := h.details(total)
		l.ArgumentCheck(opt != packString && opt != packZstr, 1, "variable-length format")
		size += pad
		l.ArgumentCheck(total <= maxPackSize-size, 1, "format result too large")
		total += size
	}
	l.PushInteger(total)
	return 1
}

// unpackInt reads a size-byte integer, sign-extending it if signed, and
// checking that bytes past 8 are only sign.
func unpackInt(l *lua.State, s string, little bool, size int, signed bool) int64 {
	at := func(i int) byte {
		if little {
			return s[i]
		}
		return s[size-1-i]
	}
	var res uint64
	for i := min(size, intSize) - 1; i >= 0; i-- {
		res = res<<8 | uint64(at(i))
	}
	if size < intSize {
		if signed {
			mask := uint64(1) << (size*8 - 1)
			res = (res ^ mask) - mask
		}
	} else if size > intSize {
		var mask byte
		if signed && int64(res) < 0 {
			mask = 0xff
		}
		for i := intSize; i < size; i++ {
			if at(i) != mask {
				l.Errorf("%d-byte integer does not fit into Lua Integer", size)
			}
		}
	}
	return int64(res)
}

// orderedBytes returns size bytes at s, little-endian whatever their order.
func orderedBytes(s string, size int, little bool) []byte {
	raw := []byte(s[:size])
	if !little {
		for i, j := 0, len(raw)-1; i < j; i, j = i+1, j-1 {
			raw[i], raw[j] = raw[j], raw[i]
		}
	}
	return raw
}

func stringUnpack(l *lua.State) int {
	h := packHeader{l: l, little: true, maxAlign: 1, fmt: l.CheckString(1)}
	data := l.CheckString(2)
	ld := len(data)
	pos := unpackPosition(l.OptInteger(3, 1), ld) - 1
	l.ArgumentCheck(pos <= ld, 3, "initial position out of string")
	n := 0
	for h.fmt != "" {
		opt, size, pad := h.details(pos)
		l.ArgumentCheck(pad+size <= ld-pos, 2, "data string too short")
		pos += pad
		l.CheckStackWithMessage(2, "too many results")
		n++
		switch opt {
		case packInt, packUint:
			l.PushInteger(unpackInt(l, data[pos:], h.little, size, opt == packInt))
		case packFloat:
			l.PushNumber(float64(math.Float32frombits(binary.LittleEndian.Uint32(orderedBytes(data[pos:], 4, h.little)))))
		case packNumber, packDouble:
			l.PushNumber(math.Float64frombits(binary.LittleEndian.Uint64(orderedBytes(data[pos:], 8, h.little))))
		case packChar:
			l.PushString(data[pos : pos+size])
		case packString:
			length := uint64(unpackInt(l, data[pos:], h.little, size, false))
			l.ArgumentCheck(length <= uint64(ld-pos-size), 2, "data string too short")
			l.PushString(data[pos+size : pos+size+int(length)])
			pos += int(length)
		case packZstr:
			length := strings.IndexByte(data[pos:], 0)
			l.ArgumentCheck(length >= 0, 2, "unfinished string for format 'z'")
			l.PushString(data[pos : pos+length])
			pos += length + 1
		case packPadAlign, packPadding, packNop:
			n--
		}
		pos += size
	}
	l.PushInteger(pos + 1) // the next position
	return n + 1
}

// unpackPosition is lstrlib.c's posrelatI: a position from 1, counting
// back from the end if negative, clipped to 1.
func unpackPosition(pos int64, length int) int {
	switch {
	case pos > 0:
		if pos > int64(length)+1 {
			return length + 2 // out of the string: the caller's check fails
		}
		return int(pos)
	case pos == 0 || pos < -int64(length):
		return 1
	}
	return length + int(pos) + 1
}
