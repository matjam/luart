//go:build !windows

package stdlib

import (
	"syscall"

	"github.com/matjam/luart"
)

func clock(l *luart.State) int {
	var rusage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusage) // ignore errors
	l.PushNumber(float64(rusage.Utime.Sec+rusage.Stime.Sec) + float64(rusage.Utime.Usec+rusage.Stime.Usec)/1000000.0)
	return 1

}
