//go:build darwin || linux

package execmem

import "unsafe"

// flushICache makes the instructions written at [p, p+n) visible to
// instruction fetch: it cleans the data cache to the point of unification,
// invalidates the instruction cache, and synchronises.
//
//go:noescape
func flushICache(p unsafe.Pointer, n uintptr)
