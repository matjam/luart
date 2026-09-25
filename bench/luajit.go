//go:build luajit

package luabench

// #cgo pkg-config: luajit
import "C"

// cName is the linked interpreter's name: see clua54.go.
const cName = "luajit"
