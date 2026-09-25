package luart_test

import (
	"fmt"

	"github.com/matjam/luart"
)

type step struct {
	name     string
	function any
}

func Example() {
	steps := []step{}
	l := luart.NewState()
	luart.BaseOpen(l)
	_ = l.NewMetaTable("stepMetaTable")
	l.SetFunctions([]luart.RegistryFunction{{"__newindex", func(l *luart.State) int {
		k, v := l.CheckString(2), l.ToValue(3)
		steps = append(steps, step{name: k, function: v})
		return 0
	}}}, 0)
	l.PushUserData(steps)
	l.PushValue(-1)
	l.SetGlobal("step")
	l.SetMetaTableNamed("stepMetaTable")
	l.LoadString(`step.request_tracking_js = function ()
    get(config.domain..'/javascripts/shopify_stats.js')
  end`)
	l.Call(0, 0)
	fmt.Println(steps[0].name)
	// Output: request_tracking_js
}
