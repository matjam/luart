package lua_test

import (
	"strings"
	"testing"

	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

// Lua 5.5's global declarations and <const> locals, each case checked
// against C Lua 5.5.1. want is the chunk's result, or the error after its
// position.
func TestDeclarations(t *testing.T) {
	tests := []struct{ src, want string }{
		{"local x <const> = 10; return x + 1", "11"},
		{"local x <const> = 10; x = 1", "attempt to assign to const variable 'x'"},
		{"local x <const> = {}; x.y = 1; return x.y", "1"},
		{"local x <const> = {}; x = 1", "attempt to assign to const variable 'x'"},
		{"local x, y <const> = 1; return y", "nil"},
		{"local <const> a, b = 1, 2; b = 3", "attempt to assign to const variable 'b'"},
		{"local x <const> = 'hi'; local f = function() return x .. '!' end; return f()", "hi!"},
		{"local x <const> = 7 // 2; local y <const> = x * 2; return math.type(y)", "integer"},
		{"local t <const> = {}; local function f() return function() t = 2 end end", "attempt to assign to const variable 't'"},
		{"for i = 1, 3 do i = 5 end", "attempt to assign to const variable 'i'"},
		{"for k, v in pairs({a = 1}) do v = 2; return v end", "2"},
		{"for k, v in pairs({}) do k = 2 end", "attempt to assign to const variable 'k'"},
		{"local x <const> = 1; function x() end", "attempt to assign to const variable 'x'"},
		{"local x <foo> = 1", "unknown attribute 'foo'"},
		{"local x <const> = 1; local x = 2; x = 3; return x", "3"},
		{"local x <const> = 1; do local x = 5; x = 6 end; return x", "1"},
		{"local y = 5; local x <const> = 1; return debug.getlocal(1, 2)", "nil"}, // a compile-time constant has no register
		{"local x <const> = 1; x, y = 1, 2", "attempt to assign to const variable 'x'"},
		{"local _ENV <const> = 11; X = 1", "attempt to index a number value"},
		{"global x; x = 1; return x", "1"},
		{"global x; y = 1", "variable 'y' not declared"},
		{"global x; do global * end; y = 1; return y", "variable 'y' not declared"},
		{"global *; global y; z = 3; return z", "3"},
		{"global <const> x; x = 1", "attempt to assign to const variable 'x'"},
		{"global <const> *; print = 1", "attempt to assign to const variable 'print'"},
		{"global x <const>, y; y = 2; return y", "2"},
		{"global <const> x; function x() end", "attempt to assign to const variable 'x'"},
		{"global x = 10; return x", "10"},
		{"x = 5; global x = 10", "global 'x' already defined"},
		{"global function f() return f ~= nil end; return f()", "true"},
		{"function g() end; global function g() end", "global 'g' already defined"},
		{"global a, b = 1, 2; return a + b", "3"},
		{"global a, b = 1; return b", "nil"},
		{"b = 1; global a, b = 1", "global 'b' already defined"},
		{"global print; local x = 1; print(x); return y", "variable 'y' not declared"},
		{"local x = 1; global x; x = 2; return x", "2"},
		{"global x; local x = 1; x = 2; return x", "2"},
		{"global <close> x", "global variables cannot be to-be-closed"},
		{"global <foo> x", "unknown attribute 'foo'"},
		{"global x; local function f() return x end; x = 7; return f()", "7"},
		{"global print; local function f() return zz end", "variable 'zz' not declared"},
		{"do global x end; y = 1; return y", "1"},
		{"global = 5; return global", "5"},
		{"local global = 5; return global", "5"},
		{"global _ENV; x = 1", "variable 'x' not declared"},
		{"local _ENV = {}; global x; x = 1; return x", "1"},
	}
	for _, tt := range tests {
		l := lua.NewState()
		stdlib.Open(l)
		if err := l.LoadString(tt.src); err == nil {
			l.ProtectedCall(0, 1, 0)
		}
		msg, _ := l.ToStringMeta(-1) // the result or the error
		if !strings.HasSuffix(msg, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.src, msg, tt.want)
		}
	}
}
