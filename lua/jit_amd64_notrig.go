//go:build (darwin || linux) && amd64 && amd64.v3

package lua

// At GOAMD64=v3 Go fuses some of math/sin.go's multiply-adds, so sin and
// cos are left to Go.
func (c *amd64Compiler) trigIntrinsics() []intrinsic { return nil }

// trigInline reports whether compiled code computes sin and cos itself.
const trigInline = false
