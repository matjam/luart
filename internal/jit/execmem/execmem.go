//go:build (darwin || linux) && (arm64 || amd64)

// Package execmem maps machine code into executable memory.
//
// Code is written to a private anonymous mapping while it is writable, the
// instruction cache is flushed where the architecture needs it, and the
// mapping is then made read-only and executable. Pages are never writable
// and executable at once.
package execmem

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Supported reports whether this platform can run generated code.
const Supported = true

// Code is machine code in executable memory. It is unmapped when it
// becomes unreachable.
type Code struct {
	mem []byte
}

// Load copies code into a new executable mapping.
func Load(code []byte) (*Code, error) {
	if len(code) == 0 {
		return nil, fmt.Errorf("execmem: no code")
	}
	size := (len(code) + syscall.Getpagesize() - 1) &^ (syscall.Getpagesize() - 1)
	mem, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("execmem: mmap: %w", err)
	}
	copy(mem, code)
	flushICache(unsafe.Pointer(&mem[0]), uintptr(len(code)))
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		syscall.Munmap(mem)
		return nil, fmt.Errorf("execmem: mprotect: %w", err)
	}
	c := &Code{mem: mem}
	runtime.AddCleanup(c, func(mem []byte) { syscall.Munmap(mem) }, mem)
	return c, nil
}

// Addr returns the address of the code's first byte plus offset.
func (c *Code) Addr(offset int) uintptr {
	return uintptr(unsafe.Pointer(&c.mem[0])) + uintptr(offset)
}
