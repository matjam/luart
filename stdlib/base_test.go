package stdlib_test

import "testing"

// pairs returns next and ipairs one iterator, the same function each time,
// and neither allocates it; metamethods still take over.
func TestPairsIterators(t *testing.T) {
	run(t, `
		assert(pairs({}) == next and ipairs({}) == ipairs({}))
		assert(type(ipairs({})) == "function")
		local mt = {__pairs = function(t) return "p" end, __ipairs = function(t) return "i" end}
		local t = setmetatable({}, mt)
		assert(pairs(t) == "p" and ipairs(t) == "i")
		local n = 0
		for i, v in ipairs({10, 20, 30}) do n = n + i * v end
		for k, v in pairs({a = 1}) do n = n + v end
		assert(n == 141)
	`)
}

// collectgarbage takes Lua 5.2's options and returns what C Lua returns.
func TestCollectGarbage(t *testing.T) {
	run(t, `
		assert(collectgarbage() == 0 and collectgarbage("collect") == 0)
		local k, b = collectgarbage("count")
		assert(k > 0 and b >= 0 and b < 1024 and k * 1024 == math.floor(k) * 1024 + b)
		assert(collectgarbage("step") == true)
		assert(collectgarbage("isrunning") == true)
		assert(collectgarbage("stop") == 0 and collectgarbage("isrunning") == false)
		assert(collectgarbage("restart") == 0 and collectgarbage("isrunning") == true)
		assert(collectgarbage("setpause", 100) == 200 and collectgarbage("setpause", 200) == 100)
		assert(collectgarbage("setstepmul", 400) == 200 and collectgarbage("setstepmul", 200) == 400)
		assert(collectgarbage("setmajorinc", 50) == 100)
		assert(collectgarbage("generational") == 0 and collectgarbage("incremental") == 0)
		local ok, err = pcall(collectgarbage, "bogus")
		assert(not ok and err:find("invalid option 'bogus'", 1, true), err)
	`)
}

// An option argument with a default may be omitted, as in file:seek().
func TestOptionDefault(t *testing.T) {
	run(t, `
		local f = io.tmpfile()
		f:write("abc")
		assert(f:seek() == 3 and f:seek("set") == 0 and f:seek("end") == 3)
		f:close()
	`)
}

// Runtime errors word and name things as Lua 5.2's do, as errors.lua
// checks.
func TestErrorMessages(t *testing.T) {
	run(t, `
		local function fails(msg, f)
			local ok, err = pcall(f)
			assert(not ok and err:find(msg, 1, true), "want " .. msg .. ", got " .. tostring(err))
		end
		fails("attempt to compare two function values", function() return print < print end)
		fails("attempt to compare table with number", function() return {} < 1 end)

		-- A name loaded by LOADK, past the 256 constants an instruction can
		-- name directly.
		local prog = {}
		for i = 1, 300 do prog[#prog + 1] = "a = x" .. i end
		prog[#prog + 1] = "aaa = bbb + 1"
		fails("global 'bbb'", load(table.concat(prog, "; ")))

		-- Files have __gc, which closes them, and checks its argument.
		fails("FILE* expected, got no value", function() getmetatable(io.stdin).__gc() end)
		local f = io.tmpfile()
		getmetatable(f).__gc(f)
		assert(io.type(f) == "closed file")
		getmetatable(f).__gc(f) -- already closed: nothing happens
	`)
}

// error raises any value, not only strings.
func TestErrorValues(t *testing.T) {
	run(t, `
		local ok, v = pcall(error)
		assert(not ok and v == nil and select('#', pcall(error)) == 2)
		local e = {}
		ok, v = pcall(error, e)
		assert(not ok and v == e)
		-- A number is a string to error; pcall, at level 1, has no position.
		ok, v = pcall(error, 42)
		assert(not ok and v == "42")
		ok, v = pcall(error, "msg", 0)
		assert(not ok and v == "msg")
		ok, v = pcall(function() error("msg") end)
		assert(not ok and v == "test:12: msg", v)
	`)
}

// xpcall calls its message handler with the error, and the chunk goes on.
func TestXpcall(t *testing.T) {
	run(t, `
		local calls = 0
		local ok, v = xpcall(function() error("x") end, function(m) calls = calls + 1; return "h:" .. m end)
		assert(not ok and v == "h:test:3: x" and calls == 1, v)
		ok, v = xpcall(function() error("x") end, debug.traceback)
		assert(not ok and v:find("^test:5: x\nstack traceback:\n"), v)
		ok, v = xpcall(function(a, b) return a + b end, print, 1, 2)
		assert(ok and v == 3)
		local function inner()
			return select(2, xpcall(error, function(m) return "inner " .. tostring(m) end, "e", 0))
		end
		assert(select(2, pcall(inner)) == "inner e")
		ok = xpcall(error, function() error("again") end)
		assert(not ok)
	`)
}

// loadfile skips a first line starting with #, before text or a binary
// chunk, and names the reason it cannot open a file.
func TestLoadfile(t *testing.T) {
	run(t, `
		local name = os.tmpname()
		local f = io.open(name, "wb")
		f:write("#!/usr/bin/lua\0\n", string.dump(function() return 20 end))
		f:close()
		assert(loadfile(name)() == 20)
		f = io.open(name, "w")
		f:write("# comment\nreturn debug.getinfo(1, 'l').currentline")
		f:close()
		assert(loadfile(name)() == 2) -- the comment keeps its line
		os.remove(name)
		local g, err = loadfile(name)
		assert(g == nil and err == "cannot open " .. name .. ": No such file or directory", err)
	`)
}

// load with a reader function ends the chunk at the first nil or empty
// piece, as the Lua suite's calls.lua checks.
func TestLoadReader(t *testing.T) {
	run(t, `
		local function reader(pieces)
			return function() return table.remove(pieces, 1) end
		end
		local f = assert(load(reader({nil, "return ", "3"})))
		assert(f() == nil)
		f = assert(load(reader({"return ", "", "3"})))
		assert(f() == nil)
		f = assert(load(reader({"return ", "3"})))
		assert(f() == 3)

		-- Many pieces do not grow the stack.
		local n = 0
		f = assert(load(function()
			n = n + 1
			if n <= 100000 then return "x = 1; " end
		end))
		f()
		assert(x == 1)
	`)
}
