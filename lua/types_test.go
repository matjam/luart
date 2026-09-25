package lua

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/matjam/luart/internal/bytecode"
)

func TestValueIsTwoWords(t *testing.T) {
	if n := unsafe.Sizeof(value{}); n != 16 {
		t.Fatalf("value is %d bytes, want 16", n)
	}
}

// Floats print as Lua 5.5 prints them: %.15g, or %.17g when that does not
// read back, with ".0" when the result looks like an integer. Integers
// print in decimal.
func TestNumberToStringMatchesFormat(t *testing.T) {
	values := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.5, -0.1, 1.0 / 3, 123, 4096, 1e13, 99999999999999,
		1e14, -1e14, 1e15, 123456789012345, 1e20, 1e-5, 1.5e-300, math.MaxFloat64,
		math.SmallestNonzeroFloat64, 1 << 53, -(1 << 53), 0.1, 1.1,
	}
	for i := -2000; i <= 2000; i++ {
		values = append(values, float64(i), float64(i)*0.25)
	}
	for _, f := range values {
		want := fmt.Sprintf("%.15g", f)
		if back, _ := strconv.ParseFloat(want, 64); back != f {
			want = fmt.Sprintf("%.17g", f)
		}
		if strings.Trim(want, "-0123456789") == "" {
			want += ".0"
		}
		if got := numberToString(numberValue(f)); got != want {
			t.Errorf("numberToString(%v) = %q, want %q", f, got, want)
		}
	}
	for _, i := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64, 1 << 53} {
		if got, want := numberToString(integerValue(i)), strconv.FormatInt(i, 10); got != want {
			t.Errorf("numberToString(%d) = %q, want %q", i, got, want)
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
		if got := numberToString(numberValue(tt.f)); got != tt.want {
			t.Errorf("numberToString(%v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestArithPowerOfTenIsCorrectlyRounded(t *testing.T) {
	for n := -323; n <= 308; n++ {
		want, _ := strconv.ParseFloat("1e"+strconv.Itoa(n), 64)
		if got := bytecode.Pow(10, float64(n)); got != want {
			t.Errorf("10^%d = %v, want %v", n, got, want)
		}
	}
}
