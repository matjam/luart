package luabench

import (
	"testing"

	luart "github.com/matjam/luart"
)

func newLuart(b *testing.B, src string) *luart.State {
	b.Helper()
	l := luart.NewState()
	luart.OpenLibraries(l)
	l.Register("set", func(l *luart.State) int {
		x, _ := l.ToNumber(1)
		y, _ := l.ToNumber(2)
		v, _ := l.ToNumber(3)
		set(int(x), int(y), v)
		return 0
	})
	if err := luart.DoString(l, src); err != nil {
		b.Fatal(err)
	}
	return l
}

func runLuartFrames(b *testing.B, l *luart.State) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Global("frame")
		l.PushNumber(float64(i))
		l.Call(1, 0)
	}
}

func BenchmarkLuart(b *testing.B) { runLuartFrames(b, newLuart(b, luaSrc)) }

func BenchmarkLuartNoCall(b *testing.B) { runLuartFrames(b, newLuart(b, luaNoCall)) }
