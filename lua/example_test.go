package lua_test

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

// A host registers Go functions, loads a script, and calls the script's
// functions from Go, here once a frame.
func Example() {
	l := lua.NewState()
	stdlib.Open(l)

	// A Go function the script can call.
	l.Register("greet", func(l *lua.State) int {
		l.PushString("hello, " + l.Arg[string](1))
		return 1
	})

	if err := l.DoString(`
		count = 0
		function frame(t)
		  count = count + 1
		  return greet("frame " .. count), t * 2
		end`); err != nil {
		log.Fatal(err)
	}

	for t := range 3 {
		l.Global("frame")
		l.PushNumber(float64(t))
		if err := l.ProtectedCall(1, 2, 0); err != nil {
			log.Fatal(err)
		}
		message, _ := l.ToString(-2)
		doubled, _ := l.ToNumber(-1)
		fmt.Println(message, doubled)
		l.Pop(2)
	}
	// Output:
	// hello, frame 1 0
	// hello, frame 2 2
	// hello, frame 3 4
}

// An error in Lua reaches Go as the error ProtectedCall returns, with the
// chunk name and line. The message is also left on the stack.
func ExampleState_ProtectedCall() {
	l := lua.NewState()
	stdlib.Open(l)
	if err := l.LoadBuffer(`
		function update(dt)
		  local speed
		  return speed * dt
		end`, "=game.lua", ""); err != nil {
		log.Fatal(err)
	}
	if err := l.ProtectedCall(0, 0, 0); err != nil {
		log.Fatal(err)
	}

	l.Global("update")
	l.PushNumber(0.016)
	if err := l.ProtectedCall(1, 0, 0); err != nil {
		fmt.Println(err)
		l.Pop(1)
	}
	// Output: runtime error: game.lua:4: attempt to perform arithmetic on a nil value (local 'speed')
}

// A Go function of numbers is called without a Go call frame when every
// argument is a number, which suits functions a script calls per pixel.
func ExampleState_RegisterNumberFunction() {
	const width, height = 4, 2
	var canvas [width * height]float64

	l := lua.NewState()
	stdlib.Open(l)
	l.RegisterNumberFunction("set", func(x, y, v float64) {
		canvas[int(y)*width+int(x)] = v
	})
	if err := l.DoString(`
		for y = 0, 1 do
		  for x = 0, 3 do set(x, y, x + y * 10) end
		end`); err != nil {
		log.Fatal(err)
	}
	fmt.Println(canvas)
	// Output: [0 1 2 3 10 11 12 13]
}

// A Go value becomes a Lua object with methods through userdata and a
// metatable, and is read back with its Go type.
func ExampleState_CheckUserData() {
	type point struct{ x, y float64 }

	l := lua.NewState()
	stdlib.Open(l)

	// The metatable of every point: its __index table holds the methods.
	l.NewMetaTable("point")
	l.NewTable()
	l.SetFunctions([]lua.RegistryFunction{{Name: "norm", Function: func(l *lua.State) int {
		p := l.CheckUserData[*point](1, "point")
		l.PushNumber(math.Hypot(p.x, p.y))
		return 1
	}}}, 0)
	l.SetField(-2, "__index")
	l.Pop(1)

	l.Register("point", func(l *lua.State) int {
		l.PushUserData(&point{l.Arg[float64](1), l.Arg[float64](2)})
		l.SetMetaTableNamed("point")
		return 1
	})

	if err := l.DoString(`print(point(3, 4):norm())`); err != nil {
		log.Fatal(err)
	}
	// Output: 5.0
}

// Interrupt, from another goroutine, stops a script that runs too long.
func ExampleState_Interrupt() {
	l := lua.NewState()
	stdlib.Open(l)
	time.AfterFunc(10*time.Millisecond, l.Interrupt)
	if err := l.DoString(`while true do end`); err != nil {
		fmt.Println(err)
		l.Pop(1)
	}
	// Output: runtime error: [string "while true do end"]:1: interrupted!
}

// A host can open only the libraries a script should have. Here the script
// gets the basic functions, string and math, but not io, os or package,
// and the basic functions that read files are removed.
func ExampleState_Require() {
	l := lua.NewState()
	for _, lib := range []lua.RegistryFunction{
		{Name: "_G", Function: stdlib.OpenBase},
		{Name: "string", Function: stdlib.OpenString},
		{Name: "math", Function: stdlib.OpenMath},
	} {
		l.Require(lib.Name, lib.Function, true)
		l.Pop(1)
	}
	for _, name := range []string{"dofile", "loadfile"} {
		l.PushNil()
		l.SetGlobal(name)
	}

	if err := l.DoString(`print(string.format("%.2f", math.pi))`); err != nil {
		log.Fatal(err)
	}
	if err := l.DoString(`io.open("secrets.txt")`); err != nil {
		fmt.Println(err)
		l.Pop(1)
	}
	// Output:
	// 3.14
	// runtime error: [string "io.open("secrets.txt")"]:1: attempt to index a nil value (global 'io')
}
