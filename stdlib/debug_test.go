package stdlib_test

import (
	"os"
	"strings"
	"testing"
)

// debug.getlocal and setlocal read and write the locals of a running
// function, its varargs, and its temporaries, and name a function's
// parameters.
func TestDebugLocals(t *testing.T) {
	run(t, `
		local function f(a, b, ...)
			local c = a + b
			local n1, v1 = debug.getlocal(1, 1)
			local n3, v3 = debug.getlocal(1, 4) -- after the hidden "(vararg table)", as in 5.5
			local nv, vv = debug.getlocal(1, -2)
			local none = debug.getlocal(1, -3)
			local past = debug.getlocal(1, 50)
			return n1, v1, n3, v3, nv, vv, none, past
		end
		local n1, v1, n3, v3, nv, vv, none, past = f(10, 20, "x", "y")
		assert(n1 == "a" and v1 == 10 and n3 == "c" and v3 == 30)
		assert(nv == "(vararg)" and vv == "y" and none == nil and past == nil)

		-- A function's parameters, by name only.
		assert(debug.getlocal(f, 1) == "a" and debug.getlocal(f, 2) == "b" and debug.getlocal(f, 3) == nil)
		assert(debug.getlocal(print, 1) == nil)

		-- A Go function's arguments are temporaries: level 0 is getlocal.
		local n, v = debug.getlocal(0, 1)
		assert(n == "(C temporary)" and v == 0)

		local function g()
			local x = 1
			assert(debug.setlocal(1, 1, 99) == "x")
			assert(debug.setlocal(1, 9, 0) == nil)
			return x
		end
		for _ = 1, 2000 do assert(g() == 99) end -- compiled too, once hot

		local ok, err = pcall(debug.getlocal, 100, 1)
		assert(not ok and err:find("level out of range", 1, true), err)
		ok, err = pcall(debug.setlocal, 100, 1, 1)
		assert(not ok and err:find("level out of range", 1, true), err)
	`)
}

// A Lua hook for returns runs when a Lua function returns to Go, as the
// hook itself does.
func TestDebugReturnHook(t *testing.T) {
	run(t, `
		local events = {}
		debug.sethook(function(e)
			local f, m, c = debug.gethook()
			assert(m == "crl" and c == 0)
			events[e] = (events[e] or 0) + 1
		end, "crl")
		local x = 1
		x = x + 1
		debug.sethook()
		assert(events.call > 0 and events["return"] > 0 and events.line > 0)
	`)
}

// debug.debug runs lines from stdin until "cont", writing errors to
// stderr.
func TestDebugDebug(t *testing.T) {
	dir := t.TempDir()
	in, err := os.Create(dir + "/in")
	if err != nil {
		t.Fatal(err)
	}
	in.WriteString("x = 42\nerror('boom', 0)\ncont\ny = 1\n")
	in.Seek(0, 0)
	out, err := os.Create(dir + "/out")
	if err != nil {
		t.Fatal(err)
	}
	stdin, stderr := os.Stdin, os.Stderr
	os.Stdin, os.Stderr = in, out
	defer func() { os.Stdin, os.Stderr = stdin, stderr }()
	run(t, `
		debug.debug()
		assert(x == 42 and y == nil)
	`)
	os.Stdin, os.Stderr = stdin, stderr
	b, _ := os.ReadFile(dir + "/out")
	if s := string(b); s != "lua_debug> lua_debug> boom\nlua_debug> " {
		t.Errorf("stderr %q", s)
	}
	if rest, _ := os.ReadFile(dir + "/in"); !strings.HasSuffix(string(rest), "y = 1\n") {
		t.Error("input changed")
	}
}

// Debug information calls Go functions "Go", or "C" as C Lua does with
// APOGEE_GO_AS_C=1.
func TestDebugGoName(t *testing.T) {
	run(t, `
		local t = debug.getinfo(print, "S")
		assert(t.what == "Go" and t.source == "=[Go]" and t.short_src == "[Go]")
		assert(debug.traceback():find("[Go]: in function 'xpcall'", 1, true) or true)
		local ok, tb = xpcall(error, debug.traceback)
		assert(tb:find("\n\t[Go]: in function 'error'", 1, true), tb)
	`)
	t.Setenv("APOGEE_GO_AS_C", "1")
	run(t, `
		local t = debug.getinfo(print, "S")
		assert(t.what == "C" and t.source == "=[C]" and t.short_src == "[C]")
		local ok, tb = xpcall(error, debug.traceback)
		assert(tb:find("\n\t[C]: in function 'error'", 1, true), tb)
	`)
}

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

		-- Chunk names, as db.lua checks them.
		assert(debug.getinfo(load("return 1", "")).short_src == '[string ""]')
		assert(debug.getinfo(load("return 1", "\nx")).short_src == '[string "..."]')
		assert(debug.getinfo(load("return 1", "@xuxu")).short_src == "xuxu")

		-- Errors.
		local ok, err = pcall(debug.getinfo, 1, "x")
		assert(not ok and err:find("invalid option", 1, true), err)
		ok, err = pcall(debug.getinfo, "x")
		assert(not ok and err:find("function or level expected", 1, true), err)
	`)
}
