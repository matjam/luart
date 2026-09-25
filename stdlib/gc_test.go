package stdlib_test

import "testing"

// While the collector is stopped, "count" grows by what the script
// allocates, even a few small objects, and a collection lowers it.
func TestCountWhileStopped(t *testing.T) {
	run(t, `
		collectgarbage("stop")
		for round = 1, 20 do
		  collectgarbage()
		  local before = collectgarbage("count")
		  local t = {}
		  for i = 1, 100 do t[i] = {} end
		  local after = collectgarbage("count")
		  assert(after > before, round)
		  t = nil
		  repeat until collectgarbage("step", 0)
		  assert(collectgarbage("count") < after, round)
		end
		collectgarbage("restart")
	`)
}

// Weak tables lose the entries whose objects a collection does not reach:
// weak values, weak keys, and ephemerons, whose values live while their
// keys do. Strings and numbers are values, never removed.
func TestWeakTables(t *testing.T) {
	run(t, `
		local keep = {}
		local wv = setmetatable({}, {__mode = "v"})
		wv[1], wv[2], wv.s, wv.n = {}, keep, "str", 42
		local wk = setmetatable({}, {__mode = "k"})
		local k1 = {}
		wk[k1], wk[{}], wk.s = 1, 2, 3
		local e = {}
		wk[e] = {e} -- the value refers to its own key
		collectgarbage()
		assert(wv[1] == nil and wv[2] == keep and wv.s == "str" and wv.n == 42)
		local n = 0
		for _ in pairs(wk) do n = n + 1 end
		assert(n == 3 and wk[k1] == 1 and wk[e][1] == e)
		e = nil
		collectgarbage()
		n = 0
		for _ in pairs(wk) do n = n + 1 end
		assert(n == 2, n) -- the ephemeron went with its key

		-- __mode read from a metatable changed after it was set
		local mt = {}
		local late = setmetatable({}, mt)
		late[1] = {}
		mt.__mode = "v"
		collectgarbage()
		assert(late[1] == nil)
	`)
}

// Finalizers run once for an unreachable object, which they resurrect,
// newest first, cycles included; errors in them are reported.
func TestFinalizers(t *testing.T) {
	run(t, `
		local log = {}
		local mt = {__gc = function(o) log[#log + 1] = o.name; saved = o end}
		do
			setmetatable({name = "a"}, mt)
			setmetatable({name = "b"}, mt)
			local c = setmetatable({name = "c"}, mt)
			c.self = c -- a cycle
		end
		collectgarbage()
		assert(table.concat(log, ",") == "c,b,a", table.concat(log, ","))
		assert(saved.name == "a") -- resurrected
		saved = nil
		collectgarbage()
		assert(#log == 3) -- only once

		-- __gc is read when the metatable is set, as in 5.2
		local late = {}
		setmetatable({}, late)
		late.__gc = function() log[#log + 1] = "late" end
		collectgarbage()
		assert(#log == 3)

		setmetatable({}, {__gc = function() error("boom") end})
		local ok, e = pcall(collectgarbage)
		assert(not ok and e:find("error in __gc metamethod (", 1, true) and e:find("boom", 1, true), e)
	`)
}

// Collections run automatically as the state allocates, once a metatable
// with __gc or __mode exists, unless stopped.
func TestAutomaticCollection(t *testing.T) {
	run(t, `
		local done = false
		setmetatable({}, {__gc = function() done = true end})
		local n = 0
		repeat n = n + 1; local t = {} until done or n > 5e7
		assert(done, "no automatic collection")

		assert(collectgarbage("isrunning"))
		collectgarbage("stop")
		assert(not collectgarbage("isrunning"))
		done = false
		setmetatable({}, {__gc = function() done = true end})
		for i = 1, 2e5 do local t = {} end
		assert(not done, "collected while stopped")
		local x = collectgarbage("count")
		for i = 1, 2e4 do local t = {} end
		assert(collectgarbage("count") > x) -- memory grows while stopped
		collectgarbage("restart")
		collectgarbage()
		assert(done)
		assert(collectgarbage("step", 1e6) == true)
	`)
}
