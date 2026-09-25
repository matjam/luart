//go:build (darwin || linux) && amd64 && amd64.v3

package luart

// At GOAMD64=v3 Go fuses some of math/sin.go's multiply-adds, so sin and
// cos are left to Go.
func (c *amd64Compiler) trigIntrinsics() []intrinsic { return nil }
