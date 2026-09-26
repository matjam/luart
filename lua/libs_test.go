package lua_test

import (
	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

// The package's internal tests open the standard libraries through this.
func init() { lua.SetTestLibraries(func(l *lua.State) { stdlib.Open(l) }) }
