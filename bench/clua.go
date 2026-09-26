package luabench

// The C Lua interpreters the benchmarks compare with, linked with cgo:
// Lua 5.5 with the clua55 build tag, Lua 5.4 with clua54, or LuaJIT with
// the luajit tag (see README.md). All define the Lua C API, so a binary can
// link one of them. Without a tag there are none, and the benchmarks need
// no C compiler.

// A cLua is a C Lua interpreter, named as its sub-benchmarks are.
type cLua struct {
	name string
	new  func(src string) (cState, error) // a state that has run src
}

// A cState is a C Lua state.
type cState interface {
	run() (float64, error) // calls the global run and returns its result
	close()
}

var cLuas []cLua // the one linked, if any
