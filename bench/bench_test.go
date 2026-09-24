package luabench

import (
	"io"
	"math"
	"testing"

	shopify "github.com/Shopify/go-lua"
	arno "github.com/arnodel/golua/lib"
	"github.com/arnodel/golua/lib/base"
	"github.com/arnodel/golua/lib/mathlib"
	"github.com/arnodel/golua/lib/packagelib"
	rt "github.com/arnodel/golua/runtime"
	"github.com/dop251/goja"
	glua "github.com/yuin/gopher-lua"
)

// One full-screen plasma frame: 200x100 half-block pixels, a Go call per pixel.
const W, H = 200, 100

var canvas [W * H]float64

func set(x, y int, v float64) { canvas[y*W+x] = v }

const luaSrc = `
local sin = math.sin
function frame(t)
  for y = 0, 99 do
    for x = 0, 199 do
      set(x, y, sin(x*0.1+t) + sin(y*0.07+t) + sin((x+y)*0.05+t))
    end
  end
end
`

const jsSrc = `
var sin = Math.sin;
function frame(t) {
  for (var y = 0; y < 100; y++)
    for (var x = 0; x < 200; x++)
      set(x, y, sin(x*0.1+t) + sin(y*0.07+t) + sin((x+y)*0.05+t));
}
`

func BenchmarkGo(b *testing.B) {
	for i := 0; i < b.N; i++ {
		t := float64(i)
		for y := range H {
			for x := range W {
				set(x, y, math.Sin(float64(x)*0.1+t)+math.Sin(float64(y)*0.07+t)+math.Sin(float64(x+y)*0.05+t))
			}
		}
	}
}

func BenchmarkGopherLua(b *testing.B) {
	L := glua.NewState()
	defer L.Close()
	L.SetGlobal("set", L.NewFunction(func(L *glua.LState) int {
		set(int(L.CheckNumber(1)), int(L.CheckNumber(2)), float64(L.CheckNumber(3)))
		return 0
	}))
	if err := L.DoString(luaSrc); err != nil {
		b.Fatal(err)
	}
	fn := L.GetGlobal("frame")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := L.CallByParam(glua.P{Fn: fn, NRet: 0, Protect: true}, glua.LNumber(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShopifyGoLua(b *testing.B) {
	l := shopify.NewState()
	shopify.OpenLibraries(l)
	l.Register("set", func(l *shopify.State) int {
		x, _ := l.ToNumber(1)
		y, _ := l.ToNumber(2)
		v, _ := l.ToNumber(3)
		set(int(x), int(y), v)
		return 0
	})
	if err := shopify.DoString(l, luaSrc); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Global("frame")
		l.PushNumber(float64(i))
		l.Call(1, 0)
	}
}

func BenchmarkArnodelGolua(b *testing.B) {
	r := rt.New(io.Discard)
	arno.LoadLibs(r, base.LibLoader, packagelib.LibLoader, mathlib.LibLoader)
	r.SetEnvGoFunc(r.GlobalEnv(), "set", func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		x, _ := rt.ToFloat(c.Arg(0))
		y, _ := rt.ToFloat(c.Arg(1))
		v, _ := rt.ToFloat(c.Arg(2))
		set(int(x), int(y), v)
		return c.Next(), nil
	}, 3, false)
	chunk, err := r.CompileAndLoadLuaChunk("bench", []byte(luaSrc), rt.TableValue(r.GlobalEnv()))
	if err != nil {
		b.Fatal(err)
	}
	if _, err := rt.Call1(r.MainThread(), rt.FunctionValue(chunk)); err != nil {
		b.Fatal(err)
	}
	fn := r.GlobalEnv().Get(rt.StringValue("frame"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rt.Call(r.MainThread(), fn, []rt.Value{rt.FloatValue(float64(i))}, rt.NewTerminationWith(nil, 0, false)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGojaJS(b *testing.B) {
	vm := goja.New()
	vm.Set("set", func(x, y int, v float64) { set(x, y, v) })
	if _, err := vm.RunString(jsSrc); err != nil {
		b.Fatal(err)
	}
	frame, _ := goja.AssertFunction(vm.Get("frame"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := frame(goja.Undefined(), vm.ToValue(float64(i))); err != nil {
			b.Fatal(err)
		}
	}
}
