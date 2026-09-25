package lua

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"unsafe"
)

func TestValueIsTwoWords(t *testing.T) {
	if n := unsafe.Sizeof(value{}); n != 16 {
		t.Fatalf("value is %d bytes, want 16", n)
	}
}

func TestNumberToStringMatchesFormat(t *testing.T) {
	values := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.5, -0.1, 1.0 / 3, 123, 4096, 1e13, 99999999999999,
		1e14, -1e14, 1e15, 123456789012345, 1e20, 1e-5, 1.5e-300, math.MaxFloat64,
		math.SmallestNonzeroFloat64, 1 << 53, -(1 << 53),
	}
	for i := -2000; i <= 2000; i++ {
		values = append(values, float64(i), float64(i)*0.25)
	}
	for _, f := range values {
		if got, want := numberToString(f), fmt.Sprintf("%.14g", f); got != want {
			t.Errorf("numberToString(%v) = %q, want %q", f, got, want)
		}
	}
	// Go's %g spells these +Inf and NaN; C's printf, which Lua uses, does not.
	for _, tt := range []struct {
		f    float64
		want string
	}{
		{math.Inf(1), "inf"}, {math.Inf(-1), "-inf"},
		{math.NaN(), "nan"}, {math.Copysign(math.NaN(), -1), "-nan"},
	} {
		if got := numberToString(tt.f); got != tt.want {
			t.Errorf("numberToString(%v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestArithPowerOfTenIsCorrectlyRounded(t *testing.T) {
	for n := -323; n <= 308; n++ {
		want, _ := strconv.ParseFloat("1e"+strconv.Itoa(n), 64)
		if got := arith(OpPow, 10, float64(n)); got != want {
			t.Errorf("10^%d = %v, want %v", n, got, want)
		}
	}
}
