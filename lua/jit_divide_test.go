//go:build (darwin || linux) && (arm64 || amd64)

package lua

import (
	"math"
	"math/rand/v2"
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
