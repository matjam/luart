package lua_test

import (
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

func TestObjectAllocations(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want float64
	}{
		{"small record", `function run() return {x = 1, y = 2} end`, 1},
		{"record with a metatable", `local mt = {} function run() return setmetatable({x = 1, y = 2}, mt) end`, 1},
		{"closure capturing a local", `function run() local n = 0 return function() n = n + 1 return n end end`, 1},
		{"closure over unchanged upvalues is reused", `local g = 1 function run() return function() return g end end`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			stdlib.Open(l)
			if err := l.DoString(tt.src); err != nil {
				t.Fatal(err)
			}
			run := func() {
				l.Global("run")
				l.Call(0, 1)
				l.Pop(1)
			}
			run()
			if n := testing.AllocsPerRun(50, run); n != tt.want {
				t.Fatalf("allocated %v times per run, want %v", n, tt.want)
			}
		})
	}
}

// Numeric code and calls into Go must not allocate, so scripts can run
// every frame without feeding the garbage collector.
func TestNumericFrameDoesNotAllocate(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	var sum float64
	l.Register("set", func(l *lua.State) int {
		sum += l.Arg[float64](3)
		return 0
	})
	// shade and set alternate a Lua call and a Go call in one call slot.
	const src = `
		local sin = math.sin
		local function shade(v) return v * 0.5 end
		function frame(t)
		  for y = 0, 9 do
		    for x = 0, 19 do
		      set(x, y, shade(sin(x*0.1+t) + sin(y*0.07+t) % 1 - (x+y)^2 / 3))
		    end
		  end
		end`
	if err := l.DoString(src); err != nil {
		t.Fatal(err)
	}
	frame := func() {
		l.Global("frame")
		l.PushNumber(1.5)
		l.Call(1, 0)
	}
	// Run past the JIT's threshold first: compiling allocates, once.
	for range 10 {
		frame()
	}
	if n := testing.AllocsPerRun(20, frame); n != 0 {
		t.Fatalf("frame allocated %v times per run, want 0", n)
	}
}
