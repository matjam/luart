package stdlib

import "github.com/matjam/apogee/lua"

func clock(l *lua.State) int {
	l.Errorf("os.clock not yet supported on Windows")
	panic("unreachable")
}
