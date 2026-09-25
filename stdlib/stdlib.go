// Package stdlib holds Lua's standard libraries for luart: basic, package,
// string, table, math, bit32, io, os and debug. They are written against
// luart's public API only.
//
// Open opens them all. A host can instead open some of them with
// State.Require and OpenBase, OpenPackage, OpenString, OpenTable,
// OpenMath, OpenBit32, OpenIO, OpenOS and OpenDebug. Except for the basic
// and package libraries, each library provides its functions as fields of
// a global table or as methods of its objects.
package stdlib

import "github.com/matjam/luart/lua"

// Open opens all the standard libraries in l, and adds each of preloaded
// to package.preload under its Name, for require to open on first use.
func Open(l *lua.State, preloaded ...lua.RegistryFunction) {
	libs := []lua.RegistryFunction{
		{Name: "_G", Function: OpenBase},
		{Name: "package", Function: OpenPackage},
		// {"coroutine", CoroutineOpen},
		{Name: "table", Function: OpenTable},
		{Name: "io", Function: OpenIO},
		{Name: "os", Function: OpenOS},
		{Name: "string", Function: OpenString},
		{Name: "bit32", Function: OpenBit32},
		{Name: "math", Function: OpenMath},
		{Name: "debug", Function: OpenDebug},
	}
	for _, lib := range libs {
		l.Require(lib.Name, lib.Function, true)
		l.Pop(1)
	}
	l.SubTable(lua.RegistryIndex, "_PRELOAD")
	for _, lib := range preloaded {
		l.PushGoFunction(lib.Function)
		l.SetField(-2, lib.Name)
	}
	l.Pop(1)
}
