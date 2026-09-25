package luart

import "testing"

// Pre-shaped tables and inline upvalues must not change what scripts see.
func TestAllocationSemantics(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"pre-shaped table hides unset keys", `
			local function make(full)
			  local t = {a = 1}
			  if full then t.b, t.c = 2, 3 end
			  return t
			end
			make(true)
			local t = make(false)
			local n = 0
			for k in pairs(t) do n = n + 1; assert(k == "a") end
			assert(n == 1 and t.b == nil and next(t, "a") == nil)`},
		{"pre-shaped table with a metatable consults __newindex", `
			local function make() return {} end
			local first = make()
			first.x = 1
			local log = {}
			local t = setmetatable(make(), {__newindex = function(t, k, v) log[#log + 1] = k; rawset(t, k, v) end})
			t.x = 2
			assert(#log == 1 and log[1] == "x" and t.x == 2)`},
		{"pre-shaped table falls back to __index", `
			local function make() return {} end
			make().x = 1
			local t = setmetatable(make(), {__index = {x = "meta"}})
			assert(t.x == "meta")`},
		{"constructors at one site with different keys", `
			local out = {}
			for i = 1, 4 do
			  local t = {}
			  if i % 2 == 0 then t.even = i else t.odd = i end
			  out[i] = t
			end
			assert(out[1].odd == 1 and out[1].even == nil)
			assert(out[2].even == 2 and out[2].odd == nil)
			assert(out[3].odd == 3 and out[4].even == 4)`},
		{"large constructor", `
			local function make(i)
			  local t = {}
			  for j = 1, 12 do t["f" .. j] = i + j end
			  return t
			end
			make(0)
			local t = make(100)
			local n = 0
			for k, v in pairs(t) do n = n + 1 end
			assert(n == 12 and t.f12 == 112)`},
		{"closures share an upvalue opened by another closure", `
			local get, set
			do
			  local x = 1
			  get = function() return x end
			  set = function(v) x = v end
			end
			set(5)
			assert(get() == 5)`},
		{"each iteration captures a fresh local", `
			local fs = {}
			for i = 1, 3 do
			  local j = i * 10
			  fs[i] = function() j = j + 1; return j end
			end
			assert(fs[1]() == 11 and fs[1]() == 12 and fs[2]() == 21 and fs[3]() == 31)`},
		{"nested closures close in order", `
			local function outer()
			  local a, b = 1, 2
			  local function mid()
			    local c = 3
			    return function() return a + b + c end
			  end
			  return mid()
			end
			assert(outer()() == 6)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewState()
			OpenLibraries(l)
			if err := l.DoString(tt.src); err != nil {
				t.Fatal(err)
			}
		})
	}
}
