//go:build clua55

package luabench

// #cgo pkg-config: lua5.5
import "C"

// cName is the linked interpreter's name: see clua54.go.
const cName = "lua55"
