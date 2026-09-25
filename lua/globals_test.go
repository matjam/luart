package lua_test

import (
	"strings"
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// Lua 5.5's global declarations, each case checked against C Lua 5.5.1.
// want is the chunk's result, or the error after its position.
func TestGlobalDeclarations(t *testing.T) {
	tests := []struct{ src, want string }{
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
