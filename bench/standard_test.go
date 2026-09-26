package luabench

import (
	"fmt"
	"testing"

	"github.com/matjam/apogee/lua"
)

// The standard benchmarks: the 14 of Are We Fast Yet (Marr, Daloze and
// Mössenböck, DLS 2016), unchanged from its Lua port, and the three of the
// Computer Language Benchmarks Game's programs that it lacks. Each defines
// a global run for the harness in suite_test.go, whose result the
// interpreters must agree on. See standard/README.md.
//
// noShopify says why go-lua does not run a benchmark, if it does not.
var standard = []struct{ name, lua, noShopify string }{
	{"bounce", awfy("bounce", 1), ""},
	{"cd", awfy("cd", 10), ""},
	{"deltablue", awfy("deltablue", 1000), ""},
	{"havlak", awfy("havlak", 1), "does not finish in 10 minutes"},
	{"json", awfy("json", 1), ""},
	{"list", awfy("list", 1), ""},
	{"mandelbrot", awfy("mandelbrot", 500), ""},
	{"nbody", awfyPath + `
		local b = require "nbody"
		local energy
		function b:verify_result(e) energy = e return true end -- checked by agreement
		function run() assert(b:inner_benchmark_loop(1000)) return energy end`, ""},
	{"permute", awfy("permute", 1), ""},
	{"queens", awfy("queens", 1), ""},
	{"richards", awfy("richards", 1), ""},
	{"sieve", awfy("sieve", 1), ""},
	{"storage", awfy("storage", 1), ""},
	{"towers", awfy("towers", 1), ""},
	{"binary-trees", clbg("binary-trees", "return f(12)"), ""},
	{"fannkuch-redux", clbg("fannkuch-redux", "local sum, flips = f(9) return sum * 100 + flips"), ""},
	{"spectral-norm", clbg("spectral-norm", "return f(200)"), ""},
}

const awfyPath = `package.path = "standard/awfy/?.lua"`

// awfy runs an Are We Fast Yet benchmark with its inner iterations, the
// problem size for some, which it checks its result for.
func awfy(name string, inner int) string {
	return fmt.Sprintf(`%s
		local b = require %q
		function run() assert(b:inner_benchmark_loop(%d), "wrong result") return 1 end`, awfyPath, name, inner)
}

// clbg runs a Benchmarks Game program, the function its file returns,
// as body calls it f.
func clbg(name, body string) string {
	return fmt.Sprintf(`package.path = "standard/clbg/?.lua"
		local f = require %q
		function run() %s end`, name, body)
}

func BenchmarkStandard(b *testing.B) {
	for _, w := range standard {
		b.Run(w.name+"/apogee", func(b *testing.B) {
			l := newSuiteApogee(b, w.lua, lua.WithoutJIT())
			for b.Loop() {
				sink = runApogee(l)
			}
		})
		b.Run(w.name+"/apogee-jit", func(b *testing.B) {
			l := newSuiteApogee(b, w.lua)
			for b.Loop() {
				sink = runApogee(l)
			}
		})
		if w.noShopify == "" {
			b.Run(w.name+"/shopify", func(b *testing.B) {
				l := newSuiteShopify(b, w.lua)
				for b.Loop() {
					sink = runShopify(l)
				}
			})
		}
		for _, c := range cLuas {
			b.Run(w.name+"/"+c.name, func(b *testing.B) {
				l := newSuiteC(b, c, w.lua)
				for b.Loop() {
					sink = runC(b, l)
				}
			})
		}
	}
}

// Every interpreter must compute each benchmark's result.
func TestStandardAgrees(t *testing.T) {
	for _, w := range standard {
		t.Run(w.name, func(t *testing.T) {
			lr := runApogee(newSuiteApogee(t, w.lua, lua.WithoutJIT()))
			if w.noShopify != "" {
				t.Logf("go-lua skipped: %s", w.noShopify)
			} else if sh := runShopify(newSuiteShopify(t, w.lua)); sh != lr {
				t.Errorf("shopify %v, apogee %v", sh, lr)
			}
			lj := newSuiteApogee(t, w.lua)
			for range 2 { // the second run uses code compiled during the first
				if j := runApogee(lj); j != lr {
					t.Errorf("apogee with JIT %v, apogee %v", j, lr)
				}
			}
			for _, c := range cLuas {
				if v := runC(t, newSuiteC(t, c, w.lua)); v != lr {
					t.Errorf("%s %v, apogee %v", c.name, v, lr)
				}
			}
		})
	}
}
