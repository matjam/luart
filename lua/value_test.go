package lua_test

import (
	"testing"

	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

func TestValueSemantics(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"modulo floors like Lua", `
			local a, b = -5, 3
			assert(a % b == 1, "-5 % 3")
			a, b = 5, -3
			assert(a % b == -1, "5 % -3")
			a, b = 5.5, 2
			assert(a % b == 1.5, "5.5 % 2")`},
		{"runtime power of ten is exact", `
			local ten, e = 10, 33
			assert(ten ^ e == 1e33)`},
		{"zero and negative zero are one key", `
			local t, z = {}, 0
			t[-z] = "x"
			assert(t[0] == "x")`},
		{"built strings match constant keys", `
			local t = {abc = 1}
			local k = "a" .. "b" .. string.char(99)
			assert(t[k] == 1)
			t[k] = 2
			assert(t.abc == 2)`},
		{"booleans and numbers are distinct keys", `
			local t = {}
			t[true], t[1], t["1"] = "b", "n", "s"
			assert(t[true] == "b" and t[1] == "n" and t["1"] == "s")
			assert(t[false] == nil)`},
		{"integral float keys use the array part", `
			local t = {}
			for i = 1, 10 do t[i] = i end
			assert(#t == 10 and t[5.0] == 5)`},
		{"closed upvalues keep their value", `
			local fs = {}
			for i = 1, 3 do fs[i] = function() return i end end
			assert(fs[1]() == 1 and fs[3]() == 3)`},
		{"pairs visits every key kind once", `
			local t = {10, 20, x = 1, y = 2, [true] = 3, [2.5] = 4}
			local seen, n = {}, 0
			for k, v in pairs(t) do
			  assert(seen[k] == nil)
			  seen[k], n = v, n + 1
			end
			assert(n == 6 and seen[1] == 10 and seen.x == 1 and seen[true] == 3 and seen[2.5] == 4)`},
		{"pairs allows clearing visited fields", `
			local t = {a = 1, b = 2, c = 3, [4.5] = 4}
			for k in pairs(t) do t[k] = nil end
			assert(next(t) == nil)`},
		{"number functions coerce strings on the slow path", `
			assert(math.floor("2.5") == 2 and math.max(1, 3) == 3)
			assert(select('#', math.sin(0)) == 1)
			local a, b = math.sin(0)
			assert(a == 0 and b == nil)`},
		{"fused multiply-add", `
			local function f(x, t) return x * 0.5 + t end
			assert(f(4, 1) == 3 and f(-2, 0.25) == -0.75)`},
		{"fused multiply-add falls back to __add", `
			local V = setmetatable({}, {__add = function(a, b) return "added" end})
			local function f(x, t) return x * 2 + t end
			assert(f(1, V) == "added")`},
		{"fused multiply-add falls back to __mul", `
			local V = setmetatable({}, {__mul = function(a, b) return 10 end})
			local function f(x, t) return x * 2 + t end
			assert(f(V, 1) == 11)`},
		{"nil and false are falsy, zero is truthy", `
			assert(not nil and not false and 0 and "")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			stdlib.Open(l)
			if err := l.DoString(tt.src); err != nil {
				t.Fatal(err)
			}
		})
	}
}
