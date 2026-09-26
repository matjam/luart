// Package luautf8 is Lua's UTF-8, which differs from Go's: it encodes
// and, lax, decodes values up to 2^31-1 in up to six bytes, as the
// original UTF-8 did, for \u{...} escapes and the utf8 library.
package luautf8

const (
	MaxUnicode = 0x10FFFF   // the largest code point, strictly
	MaxUTF     = 0x7FFFFFFF // the largest value Lua encodes
)

// Encode returns x, at most MaxUTF, in UTF-8, as lobject.c's luaO_utf8esc.
func Encode(x uint32) []byte {
	if x < 0x80 {
		return []byte{byte(x)}
	}
	var buf [8]byte
	n := len(buf)
	mfb := uint32(0x3f) // the most that fits in the first byte
	for {
		n--
		buf[n] = byte(0x80 | x&0x3f)
		x >>= 6
		mfb >>= 1
		if x <= mfb {
			break
		}
	}
	n--
	buf[n] = byte(^mfb<<1 | x)
	return buf[n:]
}

// IsContinuation reports whether c continues a UTF-8 sequence.
func IsContinuation(c byte) bool { return c&0xC0 == 0x80 }

// Decode decodes the sequence at the start of s, as lutf8lib.c's
// utf8_decode, returning its value and length, or ok false if it is
// invalid: overlong, too large, cut short, or, if strict, a surrogate or
// past MaxUnicode.
func Decode(s string, strict bool) (code uint32, size int, ok bool) {
	limits := [...]uint32{^uint32(0), 0x80, 0x800, 0x10000, 0x200000, 0x4000000}
	if s == "" {
		return 0, 0, false
	}
	c := uint32(s[0])
	var res uint32
	count := 0
	switch {
	case c < 0x80:
		res = c
	case c >= 0xfe:
		return 0, 0, false
	default:
		for ; c&0x40 != 0; c <<= 1 {
			count++
			if count >= len(s) || !IsContinuation(s[count]) {
				return 0, 0, false
			}
			res = res<<6 | uint32(s[count]&0x3F)
		}
		res |= (c & 0x7F) << (count * 5)
		if res > MaxUTF || res < limits[count] {
			return 0, 0, false
		}
	}
	if strict && (res > MaxUnicode || 0xD800 <= res && res <= 0xDFFF) {
		return 0, 0, false
	}
	return res, count + 1, true
}
