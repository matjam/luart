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

// BenchmarkApogeeBuffer gives the script the canvas itself, as a buffer,
// which compiled code stores into without a call.
func BenchmarkApogeeBuffer(b *testing.B) {
	for _, jit := range []bool{true, false} {
		b.Run(map[bool]string{true: "jit", false: "interpreted"}[jit], func(b *testing.B) {
			var opts []lua.Option
			if !jit {
				opts = append(opts, lua.WithoutJIT())
			}
			l := lua.NewState(opts...)
			stdlib.Open(l)
			l.PushBuffer(canvas[:])
			l.SetGlobal("canvas")
			if err := l.DoString(luaBuffer); err != nil {
				b.Fatal(err)
			}
			runApogeeFrames(b, l)
		})
	}
}
