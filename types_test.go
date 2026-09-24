package lua

import (
	"strconv"
	"testing"
)

func TestArithPowerOfTenIsCorrectlyRounded(t *testing.T) {
	for n := -323; n <= 308; n++ {
		want, _ := strconv.ParseFloat("1e"+strconv.Itoa(n), 64)
		if got := arith(OpPow, 10, float64(n)); got != want {
			t.Errorf("10^%d = %v, want %v", n, got, want)
		}
	}
}
