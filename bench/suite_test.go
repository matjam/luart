package luabench

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"

	shopify "github.com/Shopify/go-lua"
	luart "github.com/matjam/luart"
)

// A workload is one unit of work, written once in Lua (as a global function
// run) and once in Go. The Lua source may call gofn(x), a Go function that
// returns its argument.
type workload struct {
	name   string
	lua    string
	native func() float64
}

var sink float64

var workloads = []workload{
	{"fib", `
		local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end
		function run() return fib(25) end`,
		func() float64 { return fib(25) }},

	{"numeric-loop", `
		function run()
		  local s = 0
		  for i = 1, 1000000 do s = s + (i * i) % 7 end
		  return s
		end`,
		func() float64 {
			s := 0.0
			for i := 1.0; i <= 1000000; i++ {
				v := i * i
				s += v - math.Floor(v/7)*7 // Lua's %, not math.Mod
			}
			return s
		}},

	{"array-fill-sum", `
		function run()
		  local t = {}
		  for i = 1, 100000 do t[i] = i end
		  local s = 0
		  for i = 1, #t do s = s + t[i] end
		  return s
		end`,
		func() float64 {
			var t []float64
			for i := 1; i <= 100000; i++ {
				t = append(t, float64(i))
			}
			s := 0.0
			for _, v := range t {
				s += v
			}
			return s
		}},

	{"records", `
		function run()
		  local ps = {}
		  for i = 1, 10000 do ps[i] = {x = i, y = i * 2} end
		  local s = 0
		  for i = 1, #ps do local p = ps[i]; s = s + p.x + p.y end
		  return s
		end`,
		func() float64 {
			type rec struct{ x, y float64 }
			ps := make([]*rec, 0)
			for i := 1; i <= 10000; i++ {
				ps = append(ps, &rec{float64(i), float64(i * 2)})
			}
			s := 0.0
			for _, p := range ps {
				s += p.x + p.y
			}
			return s
		}},

	{"closures", `
		function run()
		  local s = 0
		  for i = 1, 100000 do
		    local f = function(x) return x + i end
		    s = s + f(1)
		  end
		  return s
		end`,
		func() float64 {
			s := 0.0
			for i := 1; i <= 100000; i++ {
				f := func(x float64) float64 { return x + float64(i) }
				s += nativeCall(f, 1)
			}
			return s
		}},

	{"sort", `
		function run()
		  local t, seed = {}, 42
		  for i = 1, 10000 do seed = (seed * 16807) % 2147483647; t[i] = seed end
		  table.sort(t, function(a, b) return a < b end)
		  return t[1]
		end`,
		func() float64 {
			t, seed := make([]float64, 10000), 42.0
			for i := range t {
				seed = math.Mod(seed*16807, 2147483647)
				t[i] = seed
			}
			sort.Slice(t, func(i, j int) bool { return t[i] < t[j] })
			return t[0]
		}},

	{"string-build", `
		function run()
		  local t = {}
		  for i = 1, 10000 do t[i] = tostring(i) end
		  return #table.concat(t, ",")
		end`,
		func() float64 {
			t := make([]string, 0)
			for i := 1; i <= 10000; i++ {
				t = append(t, strconv.FormatFloat(float64(i), 'g', 14, 64))
			}
			return float64(len(strings.Join(t, ",")))
		}},

	{"go-calls", `
		function run()
		  local s = 0
		  for i = 1, 100000 do s = s + gofn(i) end
		  return s
		end`,
		func() float64 {
			s := 0.0
			for i := 1; i <= 100000; i++ {
				s += nativeCall(identity, float64(i))
			}
			return s
		}},

	{"plasma", luaSrc + `
		function run() frame(1) return 0 end`,
		func() float64 {
			for y := range H {
				for x := range W {
					set(x, y, math.Sin(float64(x)*0.1+1)+math.Sin(float64(y)*0.07+1)+math.Sin(float64(x+y)*0.05+1))
				}
			}
			return 0
		}},

	{"particles", particlesSrc + `
		function run() frame(1) return 0 end`,
		nil}, // BenchmarkGoParticles covers native Go
}

func fib(n float64) float64 {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func identity(x float64) float64 { return x }

// nativeCall stops the compiler inlining f, as a Lua call can't be inlined.
//
//go:noinline
func nativeCall(f func(float64) float64, x float64) float64 { return f(x) }

func newSuiteLuart(tb testing.TB, src string, options ...luart.Option) *luart.State {
	l := luart.NewState(options...)
	luart.OpenLibraries(l)
	l.Register("gofn", func(l *luart.State) int { v, _ := l.ToNumber(1); l.PushNumber(v); return 1 })
	l.Register("set", func(l *luart.State) int {
		x, _ := l.ToNumber(1)
		y, _ := l.ToNumber(2)
		v, _ := l.ToNumber(3)
		setClipped(x, y, v)
		return 0
	})
	if err := luart.DoString(l, src); err != nil {
		tb.Fatal(err)
	}
	return l
}

func newSuiteShopify(tb testing.TB, src string) *shopify.State {
	l := shopify.NewState()
	shopify.OpenLibraries(l)
	l.Register("gofn", func(l *shopify.State) int { v, _ := l.ToNumber(1); l.PushNumber(v); return 1 })
	l.Register("set", func(l *shopify.State) int {
		x, _ := l.ToNumber(1)
		y, _ := l.ToNumber(2)
		v, _ := l.ToNumber(3)
		setClipped(x, y, v)
		return 0
	})
	if err := shopify.DoString(l, src); err != nil {
		tb.Fatal(err)
	}
	return l
}

func runLuart(l *luart.State) float64 {
	l.Global("run")
	l.Call(0, 1)
	v, _ := l.ToNumber(-1)
	l.Pop(1)
	return v
}

func runShopify(l *shopify.State) float64 {
	l.Global("run")
	l.Call(0, 1)
	v, _ := l.ToNumber(-1)
	l.Pop(1)
	return v
}

// The three implementations of each workload must compute the same result.
func TestSuiteAgrees(t *testing.T) {
	for _, w := range workloads {
		t.Run(w.name, func(t *testing.T) {
			lr, sh := runLuart(newSuiteLuart(t, w.lua)), runShopify(newSuiteShopify(t, w.lua))
			if lr != sh {
				t.Errorf("luart %v, shopify %v", lr, sh)
			}
			lj := newSuiteLuart(t, w.lua, luart.WithJIT())
			for range 3 { // later runs use code compiled during earlier ones
				if j := runLuart(lj); j != lr {
					t.Errorf("luart with JIT %v, luart %v", j, lr)
				}
			}
			if w.native != nil {
				if g := w.native(); g != lr {
					t.Errorf("go %v, luart %v", g, lr)
				}
			}
		})
	}
}

func BenchmarkSuite(b *testing.B) {
	for _, w := range workloads {
		if w.native != nil {
			b.Run(w.name+"/go", func(b *testing.B) {
				for b.Loop() {
					sink = w.native()
				}
			})
		}
		b.Run(w.name+"/luart", func(b *testing.B) {
			l := newSuiteLuart(b, w.lua)
			for b.Loop() {
				sink = runLuart(l)
			}
		})
		b.Run(w.name+"/luart-jit", func(b *testing.B) {
			l := newSuiteLuart(b, w.lua, luart.WithJIT())
			for b.Loop() {
				sink = runLuart(l)
			}
		})
		b.Run(w.name+"/shopify", func(b *testing.B) {
			l := newSuiteShopify(b, w.lua)
			for b.Loop() {
				sink = runShopify(l)
			}
		})
	}
}
