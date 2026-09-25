package stdlib_test

import "testing"

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
