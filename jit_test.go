package lua

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matjam/luart/internal/jitvm"
)

func TestJITInterpreterIsGenerated(t *testing.T) {
	vm, err := os.ReadFile("vm.go")
	if err != nil {
		t.Fatal(err)
	}
	want, err := jitvm.Generate(vm)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("vm_jit.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("vm_jit.go is stale: run go generate")
	}
}

// runBoth runs src in a state with the JIT compiling on first use and in
// one without, and returns what the global function run returns in each,
// formatted with %.17g, and the state that compiled.
func runBoth(t *testing.T, src string) (jit, interp string, lj *State) {
	t.Helper()
	saved := jitThreshold
	jitThreshold = 0
	defer func() { jitThreshold = saved }()
	result := func(l *State) string {
		if err := DoString(l, src); err != nil {
			t.Fatal(err)
		}
		l.Global("run")
		l.Call(0, MultipleReturns)
		var parts []string
		for i := 1; i <= l.Top(); i++ {
			if n, ok := l.ToNumber(i); ok && l.TypeOf(i) == TypeNumber {
				parts = append(parts, fmt.Sprintf("%.17g", n))
			} else {
				parts = append(parts, fmt.Sprint(l.ToValue(i)))
			}
		}
		return strings.Join(parts, ",")
	}
	lj = NewState(WithJIT())
	OpenLibraries(lj)
	jit = result(lj)
	li := NewState()
	OpenLibraries(li)
	interp = result(li)
	return jit, interp, lj
}

func TestJITMatchesInterpreter(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	tests := []struct {
		name string
		src  string
	}{
		{"arithmetic", `function run() local a, b = 3, 4.5; local c = a * b - a / b + -a; return c, a + b end`},
		{"constants", `function run() local x = 2; return x * 0.5 + 10, 1e300 * 1e10, -0.0 end`},
		{"moves", `function run() local a = 7; local b = a; local c = b; return c end`},
		{"non-number operand exits", `function run() local s = "3"; return s + 1, s * 2 end`},
		{"string move exits", `function run() local s = "x"; local t = s; return t end`},
		{"metamethod exits", `
			local V = setmetatable({}, {__add = function() return 42 end})
			function run() local v = V; return v + 1 end`},
		{"nan and infinity", `function run() local z = 0; return z / z ~= z / z, 1 / z, -1 / z end`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jit, interp, _ := runBoth(t, tt.src)
			if jit != interp {
				t.Fatalf("JIT %q, interpreter %q", jit, interp)
			}
		})
	}
}

func TestJITControlFlow(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	tests := []struct {
		name string
		src  string
	}{
		{"numeric for", `function run() local s = 0; for i = 1, 100 do s = s + i end; return s end`},
		{"for with fractional step", `function run() local s = 0; for i = 0, 1, 0.1 do s = s + i end; return s end`},
		{"for counting down", `function run() local s = 0; for i = 10, 1, -3 do s = s * 2 + i end; return s end`},
		{"for that never runs", `function run() local s = 7; for i = 5, 1 do s = 0 end; return s end`},
		{"for with NaN step", `function run() local s = 0; local z = 0; for i = 1, 3, z/z do s = s + 1 end; return s end`},
		{"loop variable reassigned", `function run() local s = 0; for i = 1, 5 do s = s + i; i = "x" end; return s end`},
		{"nested loops", `function run() local s = 0; for i = 1, 20 do for j = i, 20 do s = s + i * j % 7 end end; return s end`},
		{"while", `function run() local i, s = 0, 0; while i < 50 do i = i + 1; s = s + i end; return i, s end`},
		{"repeat", `function run() local i = 0; repeat i = i + 2 until i >= 9; return i end`},
		{"if chain", `function run() local a = 0; for i = 1, 30 do if i < 10 then a = a + 1 elseif i <= 20 then a = a + 10 else a = a + 100 end end; return a end`},
		{"equality", `function run() local n = 0; for i = 1, 10 do if i == 5 then n = n + 1 end; if i ~= 5 then n = n + 2 end end; return n end`},
		{"comparisons with NaN", `function run() local z = 0; local n = z/z; return n < 1, n <= 1, n > 1, n >= 1, n == n, n ~= n end`},
		{"and or not", `function run() local a, b = nil, false; return a or 3, b and 4, not a, not 0, a == nil end`},
		{"test on values", `function run() local t, n = {}, 0; for i = 1, 5 do local v = (i % 2 == 0) and t or nil; if v then n = n + 1 end end; return n end`},
		{"modulo", `function run() local r = {}; local a, b = -7, 3; return a % b, 7 % -3, 5.5 % 2, a % 0.5 end`},
		{"booleans and nil", `function run() local a, b, c = true, false, nil; local d = a; return a, b, c, d, not b end`},
		{"upvalues", `
			local count = 0
			local function bump(n) for i = 1, n do count = count + i end end
			function run() bump(10); bump(5); return count end`},
		{"closed upvalues", `
			local function counter() local n = 0; return function() for i = 1, 3 do n = n + 1 end; return n end end
			local c = counter()
			function run() c(); return c() end`},
		{"pointer copies", `function run() local t = {1}; local u = t; local s = "s"; local v = s; return u[1], v end`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jit, interp, _ := runBoth(t, tt.src)
			if jit != interp {
				t.Fatalf("JIT %q, interpreter %q", jit, interp)
			}
		})
	}
}

// A loop longer than the budget returns to Go on the way.
func TestJITBudget(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	jit, interp, lj := runBoth(t, `function run() local s = 0; for i = 1, 1000000 do s = s + 1 end; return s end`)
	if jit != interp {
		t.Fatalf("JIT %q, interpreter %q", jit, interp)
	}
	if runs := lj.jitRuns; runs < 1000000/jitBudget {
		t.Fatalf("compiled code ran %d times, want at least %d budget exits", runs, 1000000/jitBudget)
	}
}

// Compiled code copies pointers while another goroutine keeps the GC
// marking, so some runs start with the write barrier on.
func TestJITUnderGC(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	var stop atomic.Bool
	defer stop.Store(true)
	go func() {
		var keep [][]byte
		for !stop.Load() {
			keep = append(keep, make([]byte, 1<<16))
			if len(keep) > 64 {
				keep = keep[:0]
			}
		}
	}()
	src := `function run()
		local n = 0
		for i = 1, 20000 do
			local t = {i}
			local u = t
			local s = "k"
			local v = s
			if u[1] == i and v == "k" then n = n + 1 end
		end
		return n
	end`
	var barrierRuns uint64
	for range 20 {
		jit, interp, lj := runBoth(t, src)
		if jit != interp {
			t.Fatalf("JIT %q, interpreter %q", jit, interp)
		}
		barrierRuns += lj.jitBarrierRuns
	}
	if barrierRuns == 0 {
		t.Fatal("no compiled run started with the write barrier on")
	}
}

func TestJITRuns(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	_, _, lj := runBoth(t, `function run() local a = 1; local b = a + 2; return b * 3 end`)
	if lj.jitRuns == 0 {
		t.Fatal("compiled code never ran")
	}
}

// Random arithmetic over locals, checked against the interpreter.
func TestJITRandomArithmetic(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	r := rand.New(rand.NewPCG(1, 2))
	ops := []string{"+", "-", "*", "/"}
	for n := range 200 {
		var b strings.Builder
		b.WriteString("function run()\n local v0, v1, v2, v3 = 1.5, -2, 3.25, 1e-3\n")
		for range 12 {
			dst := r.IntN(4)
			x := fmt.Sprintf("v%d", r.IntN(4))
			y := fmt.Sprintf("v%d", r.IntN(4))
			if r.IntN(3) == 0 {
				y = fmt.Sprintf("%g", r.Float64()*10-5)
			}
			if r.IntN(5) == 0 {
				x = "-" + x
			}
			fmt.Fprintf(&b, " v%d = %s %s %s\n", dst, x, ops[r.IntN(len(ops))], y)
		}
		b.WriteString(" return v0, v1, v2, v3\nend")
		jit, interp, _ := runBoth(t, b.String())
		if jit != interp {
			t.Fatalf("program %d:\n%s\nJIT %q, interpreter %q", n, b.String(), jit, interp)
		}
	}
}
