package lua_test

import (
	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// The package's internal tests open the standard libraries through this.
func init() { lua.SetTestLibraries(func(l *lua.State) { stdlib.Open(l) }) }
