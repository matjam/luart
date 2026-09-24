package lua

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
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
// formatted with %.17g, and how often compiled code was entered.
func runBoth(t *testing.T, src string) (jit, interp string, runs uint64) {
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
	lj := NewState(WithJIT())
	OpenLibraries(lj)
	jit = result(lj)
	li := NewState()
	OpenLibraries(li)
	interp = result(li)
	return jit, interp, lj.jitRuns
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

func TestJITRuns(t *testing.T) {
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	_, _, runs := runBoth(t, `function run() local a = 1; local b = a + 2; return b * 3 end`)
	if runs == 0 {
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
