package luart

func clock(l *State) int {
	l.Errorf("os.clock not yet supported on Windows")
	panic("unreachable")
}
