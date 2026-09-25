package luart

import "testing"

func BenchmarkPairs(b *testing.B) {
	l := NewState()
	OpenLibraries(l)
	if err := l.DoString(`t = {}
for i = 1, 10000 do t["k" .. i] = i end
function walk() local n = 0 for _, v in pairs(t) do n = n + v end return n end`); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		l.Global("walk")
		l.Call(0, 1)
		l.Pop(1)
	}
}
