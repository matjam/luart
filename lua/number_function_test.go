package lua_test

import (
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

func TestNumberFunction(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"one result", `assert(add(1, 2) == 3)`},
		{"results adjusted to wanted", `
			local a, b, c = add(1, 2)
			assert(a == 3 and b == nil and c == nil)`},
		{"results in a table constructor", `
			local t = {add(1, 2)}
			assert(#t == 1 and t[1] == 3)`},
		{"no result", `
			record(4)
			local r = record(5)
			assert(r == nil and last == 5)`},
		{"no result in a table constructor", `
			local t = {record(6)}
			assert(#t == 0 and last == 6)`},
		{"strings convert on the slow path", `assert(add("1", 2) == 3)`},
		{"extra arguments are ignored on the slow path", `assert(add(1, 2, 3) == 3)`},
		{"missing arguments raise an error", `
			local ok, err = pcall(add, 1)
			assert(not ok and err:find("number expected"))`},
		{"four arguments", `assert(sum4(1, 2, 3, 4) == 10)`},
		{"called through a variable", `
			local f = add
			assert(f(2, 2) == 4)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			stdlib.Open(l)
			l.RegisterNumberFunction("add", func(a, b float64) float64 { return a + b })
			l.RegisterNumberFunction("sum4", func(a, b, c, d float64) float64 { return a + b + c + d })
			l.RegisterNumberFunction("record", func(v float64) {
				l.PushNumber(v)
				l.SetGlobal("last")
			})
			if err := l.DoString(tt.src); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNumberFunctionDuringCallHook(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	calls := 0
	l.RegisterNumberFunction("add", func(a, b float64) float64 { return a + b })
	l.SetHook(func(l *lua.State, ar lua.Debug) { calls++ }, lua.MaskCall, 0)
	if err := l.DoString(`assert(add(1, 2) == 3)`); err != nil {
		t.Fatal(err)
	}
	// The chunk, add and assert each report a call.
	if calls < 3 {
		t.Fatalf("call hook ran %d times; add bypassed it", calls)
	}
}
