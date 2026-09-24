//go:build darwin || linux

package execmem

import "unsafe"

// flushICache does nothing: amd64 keeps instruction fetch coherent with
// stores.
func flushICache(p unsafe.Pointer, n uintptr) {}
