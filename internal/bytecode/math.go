package bytecode

import "strconv"

// pow10 holds correctly rounded powers of ten from 1e-323 to 1e308.
var pow10 = func() (t [maxPow10 - minPow10 + 1]float64) {
	for i := range t {
		t[i], _ = strconv.ParseFloat("1e"+strconv.Itoa(i+minPow10), 64)
	}
	return
}()

const minPow10, maxPow10 = -323, 308

// Float8FromInt converts x to a "floating point byte", (eeeeexxx), whose
// value is (1xxx) * 2^(eeeee - 1) if eeeee != 0 and (xxx) otherwise. NEWTABLE
// encodes its size hints so.
func Float8FromInt(x int) int {
	if x < 8 {
		return x
	}
	e := 0
	for ; x >= 0x10; e++ {
		x = (x + 1) >> 1
	}
	return ((e + 1) << 3) | (x - 8)
}

// IntFromFloat8 converts a floating point byte back to an integer.
func IntFromFloat8(x int) int {
	e := x >> 3 & 0x1f
	if e == 0 {
		return x
	}
	return (x&7 + 8) << uint(e-1)
}
