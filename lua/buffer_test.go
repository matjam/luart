package lua_test

import (
	"math"
	"strings"
	"testing"

	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

// Buffers share their slice's memory with Lua, count from 0, read nil
// outside the slice and refuse to be written there, and convert what they
// store to their element type; compiled and interpreted code agree.
func TestBuffer(t *testing.T) {
	forBoth(t, func(t *testing.T, jit bool) {
		var opts []lua.Option
		if !jit {
			opts = append(opts, lua.WithoutJIT())
		}
		l := lua.NewState(opts...)
		stdlib.Open(l)
		f64 := []float64{1.5, 2.5, 3.5}
		f32 := make([]float32, 4)
		i32 := make([]int32, 4)
		u8 := make([]uint8, 4)
		for name, push := range map[string]func(){
			"f64": func() { l.PushBuffer(f64) },
			"f32": func() { l.PushBuffer(f32) },
			"i32": func() { l.PushBuffer(i32) },
			"u8":  func() { l.PushBuffer(u8) },
		} {
			push()
			l.SetGlobal(name)
		}
		err := l.DoString(`
			for round = 1, 200 do -- long enough to compile
				assert(f64[0] == 1.5 and f64[2] == 3.5 and #f64 == 3)
				assert(f64[3] == nil and f64[-1] == nil and f64[0.5] == nil and f64.x == nil and f64[0.0] == 1.5)
				assert(type(f64) == "userdata" and getmetatable(f64) == nil)
				f64[1] = round
				f32[0] = 0.1
				f32[1] = 3
				i32[0] = 2^40 + 5
				i32[1] = -7.0
				i32[2] = math.maxinteger
				u8[0] = 300
				u8[1] = -1
				u8[2] = 255
			end
			assert(f32[0] == 0.10000000149011612 and math.type(f32[1]) == "float")
			assert(i32[0] == 5 and i32[1] == -7 and i32[2] == -1 and math.type(i32[0]) == "integer")
			assert(u8[0] == 44 and u8[1] == 255 and u8[2] == 255)
			local n = 0
			for i, v in ipairs(f64) do n = n + i end
			assert(n == 1 + 2)
			for _, bad in ipairs{
				{function() f64[3] = 1 end, "buffer index 3 out of range [0, 3)"},
				{function() f64[-1] = 1 end, "out of range"},
				{function() f64.x = 1 end, "buffer index is a string"},
				{function() f64[0] = "1" end, "attempt to store a string value in a buffer"},
				{function() i32[0] = 1.5 end, "number has no integer representation"},
				{function() u8[0] = nil end, "attempt to store a nil value"},
			} do
				local ok, e = pcall(bad[1])
				assert(not ok and e:find(bad[2], 1, true), e)
			end`)
		if err != nil {
			t.Fatal(err)
		}
		if f64[1] != 200 || f32[0] != float32(0.1) || i32[0] != 5 || u8[0] != 44 {
			t.Fatalf("Go sees %v %v %v %v", f64, f32, i32, u8)
		}
		// Lua sees what Go writes.
		f64[2] = 42
		if err := l.DoString(`assert(f64[2] == 42)`); err != nil {
			t.Fatal(err)
		}
		l.Global("f64")
		if s, ok := l.ToBuffer[float64](-1); !ok || &s[0] != &f64[0] {
			t.Fatal("ToBuffer did not return the slice")
		}
		if _, ok := l.ToBuffer[float32](-1); ok {
			t.Fatal("ToBuffer converted the element type")
		}
	})
}

// A buffer of no elements has length 0 and no elements.
func TestEmptyBuffer(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	l.PushBuffer([]float64(nil))
	l.SetGlobal("b")
	if err := l.DoString(`assert(#b == 0 and b[0] == nil); b[0] = 1`); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("got %v", err)
	}
	_ = math.Pi
}
