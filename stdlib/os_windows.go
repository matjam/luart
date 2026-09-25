package stdlib

import "github.com/matjam/luart/lua"

func clock(l *lua.State) int {
	l.Errorf("os.clock not yet supported on Windows")
	panic("unreachable")
}
