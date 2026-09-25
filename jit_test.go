package luart

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matjam/luart/internal/jitvm"
)

// skipWithoutJIT skips a test of compiled code where nothing compiles: on
// platforms without the JIT, or with LUART_JIT=off.
func skipWithoutJIT(t *testing.T) {
	t.Helper()
	if !jitSupported {
		t.Skip("no JIT on this platform")
	}
	if jitDisabled {
		t.Skip("LUART_JIT=off")
	}
}

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
	return runBothWith(t, src, func(*State) {})
}

// runBothWith is runBoth with setup run on each state first.
func runBothWith(t *testing.T, src string, setup func(*State)) (jit, interp string, lj *State) {
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
	setup(lj)
	jit = result(lj)
	li := NewState(WithoutJIT())
	OpenLibraries(li)
	setup(li)
	interp = result(li)
	return jit, interp, lj
}

func TestJITMatchesInterpreter(t *testing.T) {
	skipWithoutJIT(t)
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
	skipWithoutJIT(t)
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

func TestJITTablesAndCalls(t *testing.T) {
	skipWithoutJIT(t)
	tests := []struct {
		name string
		src  string
	}{
		{"fields", `function run() local p = {x = 1, y = 2}; local s = 0; for i = 1, 10 do p.x = p.x + i; s = s + p.x * p.y end; return s, p.x end`},
		{"field added in loop", `function run() local t = {}; for i = 1, 5 do t.a = i; t.b = (t.b or 0) + t.a end; return t.a, t.b end`},
		{"field set to nil", `function run() local t = {a = 1}; for i = 1, 3 do t.a = nil; t.a = i end; return t.a end`},
		{"globals", `g = 0; function run() for i = 1, 10 do g = g + i end; return g end`},
		{"__index table", `
			local B = {v = 5}; local M = {__index = B}
			function run() local o = setmetatable({}, M); local s = 0; for i = 1, 10 do s = s + o.v end; B.v = 7; return s + o.v end`},
		{"__index function", `
			local o = setmetatable({}, {__index = function(t, k) return #k end})
			function run() local s = 0; for i = 1, 10 do s = s + o.abc end; return s end`},
		{"__newindex", `
			local log = 0
			local o = setmetatable({}, {__newindex = function(t, k, v) log = log + v end})
			function run() for i = 1, 10 do o.x = i end; return log, rawget(o, "x") end`},
		{"shape changes", `
			function run()
			  local s = 0
			  for i = 1, 20 do
			    local t = (i % 2 == 0) and {a = i, b = 1} or {b = 2, a = i}
			    s = s + t.a * t.b
			  end
			  return s
			end`},
		{"methods", `
			local P = {}; P.__index = P
			function P.new(x) return setmetatable({x = x}, P) end
			function P:add(n) self.x = self.x + n; return self.x end
			function run() local p = P.new(1); local s = 0; for i = 1, 10 do s = s + p:add(i) end; return s end`},
		{"arrays", `function run() local t = {1, 2, 3, 4}; for i = 1, 4 do t[i] = t[i] * 2 end; return t[1], t[4], t[5] end`},
		{"array holes and growth", `function run() local t = {}; for i = 1, 10 do t[i] = i end; t[5] = nil; local s = 0; for i = 1, 10 do s = s + (t[i] or 100) end; return s, #t >= 4 end`},
		{"fractional and odd keys", `function run() local t = {1, 2}; t[1.5] = 3; t[-1] = 4; t[0] = 5; return t[1.5], t[-1], t[0], t[1] end`},
		{"string values", `function run() local t = {"a", "b"}; local s = ""; for i = 1, 2 do s = s .. t[i]; t[i] = s end; return s, t[2] end`},
		{"indexing a non-table", `function run() local ok, err = pcall(function() local x = 5; return x.y end); return ok, err end`},
		{"string methods", `function run() local s = "abc"; return s:upper(), s:len() end`},
		{"lua calls", `
			local function add(a, b) return a + b end
			function run() local s = 0; for i = 1, 100 do s = add(s, i) end; return s end`},
		{"recursion", `local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end; function run() return fib(20) end`},
		{"multiple results", `
			local function two(x) return x, x * 2 end
			function run() local s = 0; for i = 1, 10 do local a, b = two(i); s = s + a + b end; return s, two(3) end`},
		{"missing and extra arguments", `
			local function f(a, b, c) return (a or 0) + (b or 0) + (c or 0) end
			function run() return f(1), f(1, 2), f(1, 2, 3, 4) end`},
		{"varargs", `local function f(...) return select("#", ...) end; function run() local s = 0; for i = 1, 5 do s = s + f(i, i) end; return s end`},
		{"go calls", `function run() local s = 0; for i = 1, 10 do s = s + math.max(i, 5) + select(2, i, i * 2) end; return s end`},
		{"intrinsics", `
			local floor, ceil, sqrt, abs, sin, cos = math.floor, math.ceil, math.sqrt, math.abs, math.sin, math.cos
			function run()
			  local s = 0
			  for i = -10, 10 do local x = i * 0.37; s = s + floor(x) + ceil(x) + abs(x) + sqrt(abs(x)) + sin(x) + cos(x) end
			  return s, sin(-0.0), sin(1e10), cos(1e300), floor(-0.5), sqrt(-1) ~= sqrt(-1)
			end`},
		{"intrinsic replaced", `
			local f = math.floor
			function run() local s = 0; for i = 1, 10 do s = s + f(i / 3); if i == 5 then f = math.ceil end end; return s end`},
		{"intrinsic on string", `function run() return math.floor("2.5"), math.sin("0") end`},
		{"errors in callees", `
			local function bad(x) if x > 3 then error("boom " .. x) end; return x end
			function run() local s = 0; local ok, err = pcall(function() for i = 1, 10 do s = s + bad(i) end end); return s, ok, err end`},
		{"runtime error position", `
			local function bad(t) return t.x.y end
			function run() local ok, err = pcall(function() for i = 1, 3 do bad({x = (i < 3) and {y = 1} or nil}) end end); return ok, err end`},
		{"deep recursion", `local function d(n) if n == 0 then return 0 end return 1 + d(n - 1) end; function run() return d(5000) end`},
		{"closures in loops", `function run() local s = 0; for i = 1, 20 do local f = function(x) return x + i end; s = s + f(1) end; return s end`},
		{"tail calls", `local function t(n, acc) if n == 0 then return acc end return t(n - 1, acc + n) end; function run() return t(100, 0) end`},
		{"absent keys without a metatable", `function run() local t = {1, nil, 3, x = 1}; local n = 0; for i = 1, 4 do if t[i] == nil then n = n + 1 end; if t.y == nil then n = n + 10 end end; return n end`},
		{"absent keys with a metatable", `
			local t = setmetatable({1, nil, 3}, {__index = function(_, k) return k end})
			function run() local s = 0; for i = 1, 3 do s = s + t[i] end; return s end`},
		{"appends", `function run() local t = {}; for i = 1, 1000 do t[i] = i * 2 end; local s = 0; for i = 1, #t do s = s + t[i] end; return s, #t end`},
		{"appends with __newindex", `
			local log = {}
			local t = setmetatable({}, {__newindex = function(t, k, v) log[#log + 1] = k; rawset(t, k, v) end})
			function run() for i = 1, 5 do t[i] = i end; return #log, t[5] end`},
		{"arrays in upvalues", `
			local t, u = {1, 2, 3, 4}, {}
			function run()
			  for i = 1, 8 do u[i] = (t[i] or 0) * 2 end
			  t[2] = nil
			  local s = 0
			  for i = 1, 8 do s = s + (t[i] or 100) + u[i] end
			  return s, #u, t[5]
			end`},
		{"arrays in upvalues with metatables", `
			local log = 0
			local t = setmetatable({1}, {__index = function(_, k) return k * 10 end,
			  __newindex = function(t, k, v) log = log + v; rawset(t, k, v) end})
			function run() local s = 0; for i = 1, 5 do s = s + t[i]; t[i + 1] = i end; return s, log end`},
		{"indexing a non-table upvalue", `
			local n = 5
			function run() local ok, err = pcall(function() for i = 1, 3 do local x = n[i] end end); return ok, err end`},
		{"new fields on shaped tables", `function run() local s = 0; for i = 1, 10 do local t = {}; t.a = i; t.b = i * 2; s = s + t.a + t.b end; return s end`},
		{"dictionary tables", `
			function run()
			  local t = {}
			  for i = 1, 100 do t["k" .. i] = i end
			  for i = 1, 100, 2 do t["k" .. i] = nil end
			  local s = 0; for i = 1, 10 do t.k1 = i; s = s + t.k1 + (t.k3 or 0) + t.k2 end
			  return s
			end`},
		{"captured loop variables", `function run() local fs = {}; for i = 1, 10 do fs[i] = function() return i end end; local s = 0; for i = 1, 10 do s = s + fs[i]() end; return s end`},
		{"while with captured locals", `function run() local fs, i = {}, 0; while i < 5 do i = i + 1; local j = i; fs[i] = function() return j end end; return fs[1]() + fs[5]() end`},
		{"sort with comparator", `function run() local t = {5, 3, 9, 1, 7}; table.sort(t, function(a, b) return a > b end); return t[1], t[5] end`},
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

// A Go function called from compiled code that sets a hook sees it fire
// from the next instruction, as in the interpreter.
func TestJITHookSetFromGo(t *testing.T) {
	skipWithoutJIT(t)
	src := `function run()
		local s = 0
		for i = 1, 50 do
			s = s + i
			if i == 20 then hookon() end
		end
		return s, hooks()
	end`
	for _, mask := range []byte{MaskCount, MaskLine} {
		jit, interp, _ := runBothWith(t, src, func(l *State) {
			count := 0
			l.Register("hookon", func(l *State) int {
				SetDebugHook(l, func(*State, Debug) { count++ }, mask, 1)
				return 0
			})
			l.Register("hooks", func(l *State) int {
				SetDebugHook(l, nil, 0, 0)
				l.PushInteger(count)
				return 1
			})
		})
		if jit != interp {
			t.Fatalf("mask %d: JIT %q, interpreter %q", mask, jit, interp)
		}
	}
}

// Compiled code exits to runJIT for each call of a Go function, Go closure
// or number function that is not an intrinsic, and goes on after it.
func TestJITGoCallExits(t *testing.T) {
	skipWithoutJIT(t)
	src := `local function inner(x) return gofn(x) + 1 end
	function run()
		local exp, s = math.exp, 0
		for i = 1, 50 do
			s = s + inner(i) -- a Go call from a frame compiled code entered
			s = s + gofn(i) + counter() + exp(i / 50) + gofn(i, s)
			local ok = pcall(fail, i)
			if ok then s = s + 1 end
			local p, q, r = two(i)
			local u = two(i)
			if r == nil then s = s + p + q + u end
			-- number functions: frameless, and falling back when the
			-- arguments do not fit
			s = s + mad(i, 2, 3) + mad(i, 2, 3, 4) + mad(i, "2", 3) + (hyp(i, 1))
			record(i, s, 1)
			local w, z = hyp(3, i)
			if z == nil then s = s + w end
		end
		return s, counter(), recorded()
	end`
	jit, interp, _ := runBothWith(t, src, func(l *State) {
		l.Register("gofn", func(l *State) int {
			v, _ := l.ToNumber(1)
			l.PushNumber(v * 2)
			return 1
		})
		l.PushNumberFunction(func(a, b, c float64) float64 { return a*b + c })
		l.SetGlobal("mad")
		l.PushNumberFunction(math.Hypot)
		l.SetGlobal("hyp")
		recorded := 0.0
		l.PushNumberFunction(func(a, b, c float64) { recorded += a + b*c })
		l.SetGlobal("record")
		l.Register("recorded", func(l *State) int { l.PushNumber(recorded); return 1 })
		l.Register("two", func(l *State) int {
			v, _ := l.ToNumber(1)
			l.PushNumber(v + 1)
			l.PushNumber(v * 3)
			return 2
		})
		l.Register("fail", func(l *State) int {
			if n, _ := l.ToNumber(1); int(n)%7 == 0 {
				Errorf(l, "fail %d", int(n))
			}
			return 0
		})
		l.PushInteger(0)
		l.PushGoClosure(func(l *State) int {
			n, _ := l.ToInteger(UpValueIndex(1))
			l.PushInteger(n + 1)
			l.PushValue(-1)
			l.Replace(UpValueIndex(1))
			return 1
		}, 1)
		l.SetGlobal("counter")
	})
	if jit != interp {
		t.Fatalf("JIT %q, interpreter %q", jit, interp)
	}
}

// Go calling a compiled Lua function runs it from l.call without the
// interpreter, returning to Go from compiled code, or hands over to the
// interpreter part way through.
func TestJITCalledFromGo(t *testing.T) {
	skipWithoutJIT(t)
	src := `
		local function fixed(a, b) return a + b, a * b end
		local function tail(a) return fixed(a, 2) end
		local function interpreted(a) local s = "x" .. a; return #s end
		local function closes(a) local f = function() return a end; return f() + 1 end
		local function fails(a) if a % 5 == 0 then error("five") end return a end
		local function varResults(...) return ... end
		function run()
			local s = 0
			s = s + callEach(fixed, 1) + callEach(fixed, 2) + callEach(tail, 2)
			s = s + callEach(interpreted, 1) + callEach(closes, 1) + callEach(varResults, 3)
			local ok, err = pcall(callEach, fails, 1)
			local t = {}
			for i = 1, 40 do t[i] = (i * 7919) % 41 end
			table.sort(t, function(a, b) return a > b end)
			return s, ok, err, t[1], t[40]
		end`
	jit, interp, _ := runBothWith(t, src, func(l *State) {
		// callEach(f, n) calls f(i, i) from Go for i = 1..20, wanting n
		// results each time (MultipleReturns when n is 3), and sums them.
		l.Register("callEach", func(l *State) int {
			want, _ := l.ToInteger(2)
			if want == 3 {
				want = MultipleReturns
			}
			sum := 0.0
			for i := 1; i <= 20; i++ {
				top := l.Top()
				l.PushValue(1)
				l.PushInteger(i)
				l.PushInteger(i)
				l.Call(2, want)
				for k := top + 1; k <= l.Top(); k++ {
					v, _ := l.ToNumber(k)
					sum += v
				}
				l.SetTop(top)
			}
			l.PushNumber(sum)
			return 1
		})
	})
	if jit != interp {
		t.Fatalf("JIT %q, interpreter %q", jit, interp)
	}
}

// A Go function called from compiled code may grow the stack, moving every
// frame.
func TestJITStackGrowsInGoCall(t *testing.T) {
	skipWithoutJIT(t)
	src := `function run()
		local a, b, s = 1, 2, 0
		for i = 1, 20 do
			local t = {i}
			grow(i * 50)
			s = s + a + b + t[1]
		end
		return s
	end`
	jit, interp, _ := runBothWith(t, src, func(l *State) {
		l.Register("grow", func(l *State) int {
			n := CheckInteger(l, 1)
			CheckStackWithMessage(l, n, "grow")
			for range n {
				l.PushNil()
			}
			l.Pop(n)
			return 0
		})
	})
	if jit != interp {
		t.Fatalf("JIT %q, interpreter %q", jit, interp)
	}
}

// Compiled sin and cos follow math.Sin and math.Cos bit for bit.
func TestJITTrigMatchesGo(t *testing.T) {
	skipWithoutJIT(t)
	r := rand.New(rand.NewPCG(3, 4))
	xs := []float64{0, math.Copysign(0, -1), 1, -1, math.Pi, math.Pi / 2, math.Pi / 4, 1<<29 - 1, -(1<<29 - 1), 1e-300, 5e-324, 1e-8}
	for range 200000 {
		switch r.IntN(3) {
		case 0:
			xs = append(xs, (r.Float64()*2-1)*10)
		case 1:
			xs = append(xs, (r.Float64()*2-1)*(1<<29))
		default:
			xs = append(xs, math.Ldexp(r.Float64()*2-1, r.IntN(60)-30))
		}
	}
	saved := jitThreshold
	jitThreshold = 0
	defer func() { jitThreshold = saved }()
	l := NewState(WithJIT())
	OpenLibraries(l)
	var bad int
	l.Register("x", func(l *State) int { l.PushNumber(xs[CheckInteger(l, 1)-1]); return 1 })
	l.Register("check", func(l *State) int {
		x, s, c := CheckNumber(l, 1), CheckNumber(l, 2), CheckNumber(l, 3)
		if math.Float64bits(s) != math.Float64bits(math.Sin(x)) || math.Float64bits(c) != math.Float64bits(math.Cos(x)) {
			if bad++; bad < 5 {
				t.Errorf("x %v (%#x): sin %v, want %v; cos %v, want %v", x, math.Float64bits(x), s, math.Sin(x), c, math.Cos(x))
			}
		}
		return 0
	})
	src := fmt.Sprintf(`local sin, cos = math.sin, math.cos
		for i = 1, %d do local v = x(i); check(v, sin(v), cos(v)) end`, len(xs))
	if err := DoString(l, src); err != nil {
		t.Fatal(err)
	}
	if bad > 0 {
		t.Fatalf("%d of %d mismatches", bad, len(xs))
	}
	if l.jitRuns == 0 {
		t.Fatal("compiled code never ran")
	}
}

// Numeric loops compile to kernels that keep numbers in registers; each
// case says how many kernels it should compile.
func TestJITKernels(t *testing.T) {
	skipWithoutJIT(t)
	tests := []struct {
		name    string
		kernels int
		src     string
	}{
		{"sum", 1, `function run() local s = 0; for i = 1, 1000 do s = s + i * 0.5 end; return s end`},
		{"modulo", 1, `function run() local s = 0; for i = 1, 1000 do s = s + (i * i) % 7 end; return s end`},
		{"temporaries", 1, `function run() local s, t = 0, {}; for i = 1, 100 do local a = i * 2; local b = a - 1; s = s + a / b end; return s, type(t) end`},
		{"old value in a temporary", 1, `function run() local s = 0; do local x = {} end; for i = 1, 10 do local y = i; s = s + y end; return s end`},
		{"branches", 1, `function run() local a, b = 0, 0; for i = 1, 100 do if i % 3 == 0 then a = a + i elseif i < 50 then b = b - 1 else b = b + 2 end end; return a, b end`},
		{"conditional write keeps old value", 1, `function run() local x, s = 5, 0; for i = 1, 10 do if i > 5 then x = i end; s = s + x end; return x, s end`},
		{"zero iterations", 1, `function run() local s = 3; for i = 5, 1 do s = s + i end; return s end`},
		{"negative and fractional steps", 2, `function run() local s = 0; for i = 10, 1, -0.5 do s = s + i end; for j = 0, 1, 0.1 do s = s * 1.01 + j end; return s end`},
		{"NaN step", 1, `function run() local s, z = 0, 0; for i = 1, 3, z/z do s = s + 1 end; return s end`},
		{"non-number live-in", 1, `function run() local s = "1"; for i = 1, 3 do s = s + i end; return s end`},
		{"number constants", 1, `function run() local s = 0; for i = 1, 10 do local k = 2.5; s = s - k + i end; return s end`},
		{"unary minus and equality", 1, `function run() local s = 0; for i = 1, 20 do local n = -i; if n == -10 then s = s + 100 end; s = s + n end; return s end`},
		{"long loop spends budget", 1, `function run() local s = 0; for i = 1, 300000 do s = s + 1 end; return s end`},
		{"loop variable after the loop", 1, `function run() local last = 0; for i = 1, 7 do last = i end; return last end`},
		{"nested: inner only", 1, `function run() local s = 0; for i = 1, 10 do for j = 1, 10 do s = s + i * j end end; return s end`},
		{"break is not a kernel", 0, `function run() local s = 0; for i = 1, 10 do s = s + i; if s > 20 then break end end; return s end`},
		{"calls are not kernels", 0, `function run() local s = 0; for i = 1, 10 do s = s + math.floor(i / 2) end; return s end`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jit, interp, lj := runBoth(t, tt.src)
			if jit != interp {
				t.Fatalf("JIT %q, interpreter %q", jit, interp)
			}
			lj.Global("run")
			p := lj.ToValue(-1).(*luaClosure).prototype
			if p.jit == nil {
				t.Fatal("run was not compiled")
			}
			if p.jit.kernels != tt.kernels {
				t.Fatalf("%d kernels, want %d", p.jit.kernels, tt.kernels)
			}
		})
	}
}

// A loop longer than the budget returns to Go on the way.
func TestJITBudget(t *testing.T) {
	skipWithoutJIT(t)
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
	skipWithoutJIT(t)
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
		local r = {a = {}, b = "x", [1] = {}, [2] = "y"}
		local function id(x, y) return y, x end
		for i = 1, 20000 do
			r.a, r.b = id(r.a, r.b)
			local t = {i}
			local u = t
			local s = "k"
			local v = s
			r.a, r.b, r[1], r[2] = r.b, r.a, r[2], r[1]
			local w = r.a
			if u[1] == i and v == "k" and w ~= nil then n = n + 1 end
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
	skipWithoutJIT(t)
	_, _, lj := runBoth(t, `function run() local a = 1; local b = a + 2; return b * 3 end`)
	if lj.jitRuns == 0 {
		t.Fatal("compiled code never ran")
	}
}

// Random arithmetic over locals, checked against the interpreter.
func TestJITRandomArithmetic(t *testing.T) {
	skipWithoutJIT(t)
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
