package stdlib_test

import "testing"

func TestDebugGetinfo(t *testing.T) {
	run(t, `
		local function f(a, b, ...)
			return debug.getinfo(1)
		end
		local t = f(1, 2)
		assert(t.func == f)
		assert(t.what == "Lua" and t.source == "=test" and t.short_src == "test", t.short_src)
		assert(t.linedefined == 2 and t.lastlinedefined == 4)
		assert(t.currentline == 3, t.currentline)
		assert(t.nups == 1 and t.nparams == 2 and t.isvararg == true) -- _ENV, for debug
		assert(t.name == "f" and t.namewhat == "local", tostring(t.name))
		assert(t.istailcall == false)
		assert(t.activelines == nil) -- 'L' is not a default option

		-- A function rather than a level.
		t = debug.getinfo(print)
		assert(t.func == print and t.what == "Go" and t.short_src == "[Go]")
		assert(t.currentline == -1 and t.nparams == 0 and t.isvararg == true)
		assert(t.name == nil and t.namewhat == "")

		-- Chosen options only.
		t = debug.getinfo(1, "S")
		assert(t.what == "main" and t.currentline == nil and t.func == nil)
		t = debug.getinfo(1, "")
		assert(next(t) == nil)

		-- Active lines.
		local lines = debug.getinfo(f, "L").activelines
		assert(lines[3] and lines[4] and not lines[1] and not lines[5])

		-- Level 0 is getinfo itself; past the stack is nil.
		assert(debug.getinfo(0, "f").func == debug.getinfo)
		assert(debug.getinfo(100) == nil)
		assert(debug.getinfo(-1) == nil)

		-- Tail calls.
		local function g() return debug.getinfo(1, "t") end
		local function h() return g() end
		assert(h().istailcall == true)

		-- Errors.
		local ok, err = pcall(debug.getinfo, 1, "x")
		assert(not ok and err:find("invalid option", 1, true), err)
		ok, err = pcall(debug.getinfo, "x")
		assert(not ok and err:find("function or level expected", 1, true), err)
	`)
}
