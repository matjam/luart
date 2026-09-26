package luabench

import (
	"testing"

	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

func newApogee(b *testing.B, src string, options ...lua.Option) *lua.State {
	b.Helper()
	l := lua.NewState(options...)
	stdlib.Open(l)
	l.Register("set", func(l *lua.State) int {
		x, _ := l.ToNumber(1)
		y, _ := l.ToNumber(2)
		v, _ := l.ToNumber(3)
		set(int(x), int(y), v)
		return 0
	})
	if err := l.DoString(src); err != nil {
		b.Fatal(err)
	}
	return l
}

func runApogeeFrames(b *testing.B, l *lua.State) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Global("frame")
		l.PushNumber(float64(i))
		l.Call(1, 0)
	}
}

func BenchmarkApogee(b *testing.B) { runApogeeFrames(b, newApogee(b, luaSrc)) }

// BenchmarkApogeeNumberFunction registers set as a number function, which
// the VM calls without a call frame.
func BenchmarkApogeeNumberFunction(b *testing.B) {
	l := newApogee(b, luaSrc)
	l.RegisterNumberFunction("set", func(x, y, v float64) { set(int(x), int(y), v) })
	runApogeeFrames(b, l)
}

func BenchmarkApogeeNoCall(b *testing.B) { runApogeeFrames(b, newApogee(b, luaNoCall)) }
