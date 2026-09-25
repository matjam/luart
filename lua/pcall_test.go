package lua_test

import (
	"testing"

	"github.com/matjam/luart/lua"
)

// ProtectedCall's error handler index is relative to the running
// function, like any index, also in a Go function that Lua called.
func TestProtectedCallHandlerInGoFunction(t *testing.T) {
	l := lua.NewState()
	var got string
	l.Register("f", func(l *lua.State) int {
		l.PushGoFunction(func(l *lua.State) int {
			m, _ := l.ToString(1)
			l.PushString("handled: " + m)
			return 1
		})
		handler := l.Top()
		l.PushGoFunction(func(l *lua.State) int {
			l.PushString("boom")
			l.Error()
			return 0
		})
		if err := l.ProtectedCall(0, 0, handler); err == nil {
			t.Error("ProtectedCall succeeded")
		}
		got, _ = l.ToString(-1)
		return 0
	})
	if err := l.DoString("local a, b, c = 1, 2, 3; f()"); err != nil {
		t.Fatal(err)
	}
	if got != "handled: boom" {
		t.Errorf("error %q, want %q", got, "handled: boom")
	}
}
