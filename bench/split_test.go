package luabench

import (
	"testing"

	shopify "github.com/Shopify/go-lua"
	glua "github.com/yuin/gopher-lua"
)

// Same maths, but results go into a Lua table instead of a Go call per pixel.
const luaNoCall = `
local sin = math.sin
local buf = {}
function frame(t)
  local i = 1
  for y = 0, 99 do
    for x = 0, 199 do
      buf[i] = sin(x*0.1+t) + sin(y*0.07+t) + sin((x+y)*0.05+t)
      i = i + 1
    end
  end
end
`

func BenchmarkGopherLuaNoCall(b *testing.B) {
	L := glua.NewState()
	defer L.Close()
	if err := L.DoString(luaNoCall); err != nil {
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

func BenchmarkShopifyNoCall(b *testing.B) {
	l := shopify.NewState()
	shopify.OpenLibraries(l)
	if err := shopify.DoString(l, luaNoCall); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Global("frame")
		l.PushNumber(float64(i))
		l.Call(1, 0)
	}
}
