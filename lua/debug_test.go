package lua_test

import (
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// debug.sethook with a Lua function installs it as the hook, and
// debug.gethook returns it with its mask and count.
func TestDebugLuaHook(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	src := `
		local calls = 0
		local function hook(event) calls = calls + 1 end
		debug.sethook(hook, "c")
		local f, mask, count = debug.gethook()
		local function g() end
		g(); g()
		debug.sethook()
		local after = calls
		g()
		return f == hook, mask, count, after > 0, calls == after, debug.gethook() == nil`
	if err := l.DoString("function run() " + src + " end"); err != nil {
		t.Fatal(err)
	}
	l.Global("run")
	if err := l.ProtectedCall(0, lua.MultipleReturns, 0); err != nil {
		t.Fatal(err)
	}
	same, _ := l.ToString(1)
	if !l.ToBoolean(1) {
		t.Errorf("gethook returned another function: %s", same)
	}
	if mask, _ := l.ToString(2); mask != "c" {
		t.Errorf("mask %q, want \"c\"", mask)
	}
	if count, _ := l.ToInteger(3); count != 0 {
		t.Errorf("count %d, want 0", count)
	}
	if !l.ToBoolean(4) {
		t.Error("the hook never ran")
	}
	if !l.ToBoolean(5) {
		t.Error("the hook ran after it was removed")
	}
	if !l.ToBoolean(6) {
		t.Error("gethook still returns a hook after it was removed")
	}
}
