// Package stdlib holds Lua's standard libraries for luart: basic, package,
// coroutine, string, utf8, table, math, io, os and debug. They are
// written against luart's public API only.
//
// Open opens them all. A host can instead open some of them with
// State.Require and OpenBase, OpenPackage, OpenCoroutine, OpenString,
// OpenUTF8, OpenTable, OpenMath, OpenIO, OpenOS and OpenDebug. Except for the basic
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
		{Name: "coroutine", Function: OpenCoroutine},
		{Name: "table", Function: OpenTable},
		{Name: "io", Function: OpenIO},
		{Name: "os", Function: OpenOS},
		{Name: "string", Function: OpenString},
		{Name: "utf8", Function: OpenUTF8},
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

// checkInt and optInt are CheckInteger and OptInteger for arguments that
// are positions or counts in Go ints.
func checkInt(l *lua.State, index int) int { return int(l.CheckInteger(index)) }

func optInt(l *lua.State, index, def int) int { return int(l.OptInteger(index, int64(def))) }
