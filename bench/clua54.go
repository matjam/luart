//go:build clua54

package luabench

// #cgo pkg-config: lua5.4
import "C"

// cName is the linked interpreter's name. Each tag's file declares it, so
// building with both tags fails here rather than linking two Lua APIs.
const cName = "lua54"
