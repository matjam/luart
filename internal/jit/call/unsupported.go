//go:build !((darwin || linux) && (arm64 || amd64))

package call

import "unsafe"

// Call is never reached on platforms without generated code.
func Call(entry uintptr, ctx unsafe.Pointer) uint64 { panic("call: unsupported platform") }
