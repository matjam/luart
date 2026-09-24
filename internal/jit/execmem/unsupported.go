//go:build !((darwin || linux) && (arm64 || amd64))

package execmem

import "errors"

// Supported reports whether this platform can run generated code.
const Supported = false

// Code is machine code in executable memory.
type Code struct{}

// Load reports that generated code is not supported here.
func Load(code []byte) (*Code, error) { return nil, errors.New("execmem: unsupported platform") }

// Addr is never called on unsupported platforms.
func (c *Code) Addr(offset int) uintptr { return 0 }
