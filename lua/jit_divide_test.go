//go:build (darwin || linux) && (arm64 || amd64)

package lua

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"

	"github.com/matjam/apogee/internal/bytecode"
)

// divRef, the arithmetic compiled code does for a constant divisor, agrees
// with the interpreter's floor division and modulo.
func TestDivideByConstant(t *testing.T) {
	var divisors []int64
	for d := int64(-300); d <= 300; d++ {
		divisors = append(divisors, d)
	}
	for k := range 31 {
		divisors = append(divisors, 1<<k, -(1 << k), 1<<k-1, 1<<k+1, -(1<<k - 1), -(1<<k + 1))
	}
	divisors = append(divisors, math.MaxInt32, math.MinInt32, 1e9+7, -(1e9 + 7), 641, 6700417, 1<<32, math.MaxInt64, math.MinInt64)
	numerators := []int64{0, 1, -1, 2, -2, math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1, math.MaxInt32, math.MinInt32}
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		numerators = append(numerators, int64(r.Uint64()), int64(r.Uint64())>>r.IntN(63), r.Int64N(1000)-500)
	}
	planned := 0
	for _, d := range divisors {
		p, ok := planDivide(d)
		if !ok {
			continue
		}
		planned++
		for _, n := range append(numerators, d, -d, d*3, d*3-1, d*3+1, -d*3, -d*3-1, -d*3+1) {
			q, m := divRef(n, p)
			if wq, wm := bytecode.IntFloorDiv(n, d), bytecode.IntMod(n, d); q != wq || m != wm {
				t.Fatalf("%d // %d, %% %d: got %d, %d; want %d, %d (plan %+v)", n, d, d, q, m, wq, wm, p)
			}
		}
	}
	if planned < 600 {
		t.Fatalf("only %d divisors planned", planned)
	}
}

// % and // by constants, which compiled code computes with a multiply or a
// shift, in ordinary code and in kernels, agree with the interpreter across
// the integers' range.
func TestJITDivideByConstant(t *testing.T) {
	skipWithoutJIT(t)
	runtime.GC()
	defer debug.SetGCPercent(debug.SetGCPercent(-1)) // kernels run while the barrier is off
	divisors := []string{"1", "2", "3", "-3", "5", "7", "-7", "10", "16", "-16", "641", "1024", "12345", "-65536",
		"1000000007", "2147483647", "-2147483648", "2147483648", "-1", "4611686018427387904"}
	for _, d := range divisors {
		src := fmt.Sprintf(`
			local ns = {0, 1, -1, 2, -2, 6, -6, 7, -7, 8, -9, 1000, -1001, 2^31 | 0, -(2^31 | 0), 123456789012, -98765432109,
				math.maxinteger, math.mininteger, math.maxinteger - 1, math.mininteger + 1, math.maxinteger // 3, math.mininteger // 7}
			function run()
				local s = 0
				for _, n in ipairs(ns) do s = s ~ (n %% %[1]s) ~ (n // %[1]s) * 3 end -- ordinary code
				local k = 0 -- kernels
				for n = -3000, 3000 do k = k + (n %% %[1]s) * 3 + (n // %[1]s) end
				for n = math.maxinteger - 300, math.maxinteger do k = k + (n %% %[1]s) * 3 + (n // %[1]s) end
				for n = math.mininteger, math.mininteger + 300 do k = k + (n %% %[1]s) * 3 + (n // %[1]s) end
				return s, k
			end`, d)
		jit, interp, lj := runBoth(t, src)
		if jit != interp {
			t.Fatalf("divisor %s: JIT %q, interpreter %q", d, jit, interp)
		}
		lj.Global("run")
		if p := lj.ToValue(-1).(*luaClosure).prototype; p.jit == nil {
			t.Fatalf("divisor %s: run was not compiled", d)
		}
		if n, _ := strconv.ParseInt(d, 10, 64); kernelDivisor(integerValue(n)) && lj.jitCtx.kernels[1] == 0 {
			t.Fatalf("divisor %s: no integer kernel ran", d)
		}
	}
}
