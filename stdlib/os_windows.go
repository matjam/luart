package stdlib

import "github.com/matjam/luart"

func clock(l *luart.State) int {
	l.Errorf("os.clock not yet supported on Windows")
	panic("unreachable")
}
