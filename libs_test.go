package luart_test

import (
	"github.com/matjam/luart"
	"github.com/matjam/luart/stdlib"
)

// The package's internal tests open the standard libraries through this.
func init() { luart.SetTestLibraries(func(l *luart.State) { stdlib.Open(l) }) }
