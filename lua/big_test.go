package lua_test

import (
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// A function with more constants than an instruction can index loads the
// rest with LOADKX, as the first part of the Lua suite's big.lua does.
func TestManyConstants(t *testing.T) {
	for _, jit := range []bool{false, true} {
		var l *lua.State
		if jit {
			l = lua.NewState()
		} else {
			l = lua.NewState(lua.WithoutJIT())
		}
		stdlib.Open(l)
		if err := l.DoString(`
			local lim = 2^18 + 1000
			local prog = {"local y = {0"}
			for i = 1, lim do prog[#prog + 1] = i end
			prog[#prog + 1] = "}\n"
			prog[#prog + 1] = "X = y\n"
			prog[#prog + 1] = ("assert(X[%d] == %d)"):format(lim - 1, lim - 2)
			prog[#prog + 1] = "return 0"
			local env = {assert = assert}
			local f = assert(load(table.concat(prog, ";"), nil, nil, env))
			assert(f() == 0)
			assert(env.X[lim] == lim - 1 and env.X[lim + 1] == lim)
		`); err != nil {
			msg, _ := l.ToString(-1)
			t.Fatalf("jit %v: %v %s", jit, err, msg)
		}
	}
}

// A Go function's panic with a value that is not an error passes through
// a protected call unchanged.
func TestProtectedCallRepanics(t *testing.T) {
	l := lua.NewState()
	l.Register("boom", func(*lua.State) int { panic("boom") })
	defer func() {
		if r := recover(); r != "boom" {
			t.Errorf("recovered %#v, want \"boom\"", r)
		}
	}()
	l.Global("boom")
	l.ProtectedCall(0, 0, 0)
	t.Error("ProtectedCall returned")
}
