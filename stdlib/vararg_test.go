package stdlib_test

import "testing"

// Lua 5.5's named vararg tables; each case was checked against C Lua
// 5.5.1.
func TestNamedVarArgs(t *testing.T) {
	run(t, `
		local function f(a, ...t) return t.n, t[1], t[2], t[3], t[a] end
		local n, x, y, z, w = f(2, "p", "q")
		assert(n == 2 and x == "p" and y == "q" and z == nil and w == "q")
		assert(f(0) == 0)
		local function g(...t) return t end -- the table itself
		local t = g(1, nil, 3)
		assert(t.n == 3 and t[1] == 1 and t[2] == nil and t[3] == 3)
		local function h(...t) t[1] = t[1] + 10; return ... end -- ... reads the table
		assert(h(1, 2) == 11 and select("#", h(1, 2)) == 2)
		local function k(n, ...t) t.n = n; return ... end
		assert(select("#", k(3, 1)) == 3)
		assert(not pcall(k, -1) and select(2, pcall(k, 1.5)):find("no proper 'n'"))
		local function m(...t) return function() return t.n end end -- captured
		assert(m(1, 2)() == 2)
		local obj = {10, 20}
		function obj:get(...t) return self[t[1]] + t.n end -- a method: self is a parameter
		assert(obj:get(2) == 21)
		assert(select(2, load("return function(...t) t = 1 end")):find("const variable 't'"))
		local function e(..._ENV) global a = 10; return a end
		assert(e() == 10)
		-- Only indexed, the table is never made.
		local function view(...t) return t[1], t.n, t.x end
		view(1, 2)
		local before = collectgarbage("count")
		for _ = 1, 100 do view(1, 2) end
		assert(collectgarbage("count") == before)
		local function many(p1, p2, p3, p4, p5, p6, p7, p8, p9, p10, p11, p12, p13, p14, p15, p16, p17, p18, p19, p20, ...)
			local a1, a2, a3, a4, a5, a6, a7
		end
		many() -- the parameters move above the arguments: stack for both
	`)
}
