package luart

import "testing"

// Each case runs the same instructions repeatedly so their field caches fill,
// then changes the tables in a way a stale cache would get wrong.
func TestFieldCacheInvalidation(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"cleared field", `
			local t = {x = 1}
			local function get() return t.x end
			for _ = 1, 3 do assert(get() == 1) end
			t.x = nil
			assert(get() == nil)
			t.x = 2
			assert(get() == 2)`},
		{"cleared field falls back to __index", `
			local t = setmetatable({x = 1}, {__index = {x = "meta"}})
			local function get() return t.x end
			for _ = 1, 3 do assert(get() == 1) end
			t.x = nil
			assert(get() == "meta")`},
		{"alternating shapes at one site", `
			local a, b = {x = 1, y = 2}, {y = 3, x = 4}
			local function get(t) return t.x end
			for _ = 1, 3 do assert(get(a) == 1 and get(b) == 4) end`},
		{"method from a replaced __index table", `
			local A = {f = function() return "a" end}
			local B = {f = function() return "b" end}
			local mt = {__index = A}
			local o = setmetatable({}, mt)
			o.pad = true
			local function call() return o:f() end
			for _ = 1, 3 do assert(call() == "a") end
			mt.__index = B
			assert(call() == "b")`},
		{"method redefined in its class", `
			local C = {}
			C.__index = C
			function C.f() return 1 end
			local o = setmetatable({pad = true}, C)
			local function call() return o:f() end
			for _ = 1, 3 do assert(call() == 1) end
			function C.f() return 2 end
			assert(call() == 2)`},
		{"method shadowed on the instance", `
			local C = {}
			C.__index = C
			function C.f() return "class" end
			local o = setmetatable({pad = true}, C)
			local function call() return o.f() end
			for _ = 1, 3 do assert(call() == "class") end
			o.f = function() return "own" end
			assert(call() == "own")`},
		{"__index becomes a function", `
			local mt = {__index = {x = 1}}
			local o = setmetatable({pad = true}, mt)
			local function get() return o.x end
			for _ = 1, 3 do assert(get() == 1) end
			mt.__index = function() return 2 end
			assert(get() == 2)`},
		{"metatable replaced", `
			local o = setmetatable({pad = true}, {__index = {x = 1}})
			local function get() return o.x end
			for _ = 1, 3 do assert(get() == 1) end
			setmetatable(o, {__index = {x = 2}})
			assert(get() == 2)
			setmetatable(o, nil)
			assert(get() == nil)`},
		{"dictionary gains a key", `
			local d = setmetatable({}, {__index = {late = "meta"}})
			for i = 1, 40 do d["k" .. i] = i end
			local function get() return d.late end
			for _ = 1, 3 do assert(get() == "meta") end
			d.late = "own"
			assert(get() == "own")`},
		{"dictionary compaction", `
			local d = {}
			for i = 1, 100 do d["k" .. i] = i end
			local function get() return d.k100 end
			for _ = 1, 3 do assert(get() == 100) end
			for i = 1, 90 do d["k" .. i] = nil end
			d.fresh = true
			assert(get() == 100 and d.k95 == 95 and d.k1 == nil)`},
		{"set on a cleared field calls __newindex", `
			local log = {}
			local o = setmetatable({x = 1}, {__newindex = function(t, k, v) log[#log + 1] = k; rawset(t, k, v) end})
			local function set(v) o.x = v end
			for i = 1, 3 do set(i) end
			assert(#log == 0 and o.x == 3)
			o.x = nil
			set(4)
			assert(#log == 1 and o.x == 4)`},
		{"global replaced", `
			g = 1
			local function get() return g end
			for _ = 1, 3 do assert(get() == 1) end
			g = nil
			assert(get() == nil)
			rawset(_G, "g", 2)
			assert(get() == 2)`},
		{"tag method cache sees fields set through the cache", `
			local mt = {__index = function() return "fn" end}
			local o = setmetatable({}, mt)
			assert(o.missing == "fn")
			local function set(v) mt.__index = v end
			for _ = 1, 3 do set(function() return "fn2" end) end
			assert(o.missing == "fn2")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewState()
			openLibraries(l)
			if err := l.DoString(tt.src); err != nil {
				t.Fatal(err)
			}
		})
	}
}
