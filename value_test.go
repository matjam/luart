package lua

import "testing"

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
		{"nil and false are falsy, zero is truthy", `
			assert(not nil and not false and 0 and "")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewState()
			OpenLibraries(l)
			if err := DoString(l, tt.src); err != nil {
				t.Fatal(err)
			}
		})
	}
}
