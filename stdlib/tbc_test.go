package stdlib_test

import "testing"

// To-be-closed variables as Lua 5.5 closes them; each case was checked
// against C Lua 5.5.1.
func TestToBeClosed(t *testing.T) {
	run(t, `
		local log = {}
		local function obj(name)
			return setmetatable({}, {__close = function(_, e) log[#log + 1] = name .. ":" .. tostring(e) end})
		end
		local function take() local s = table.concat(log, " ") log = {} return s end

		do local a <close> = obj("a"); local b <close> = obj("b") end
		assert(take() == "b:nil a:nil")
		for i = 1, 2 do local x <close> = obj("l" .. i) if i == 2 then break end end
		assert(take() == "l1:nil l2:nil")
		local function f(...) local x <close> = obj("f"); return ... end
		assert(select("#", f(1, 2, 3)) == 3 and take() == "f:nil")
		assert(not pcall(function() local x <close> = obj("e"); error("boom", 0) end))
		assert(take() == "e:boom")

		-- An error in __close replaces the error for the variables below.
		local ok, err = pcall(function()
			local x <close> = obj("x")
			local y <close> = setmetatable({}, {__close = function(_, e) error("second", 0) end})
			error("first", 0)
		end)
		assert(not ok and err == "second" and take() == "x:second")

		local ok, err = pcall(function() local x <close> = {} end)
		assert(not ok and err:find("variable 'x' got a non%-closable value"))
		local ok, err = pcall(function()
			local x <close> = setmetatable({}, {__close = print})
			getmetatable(x).__close = nil
		end)
		assert(not ok and err:find("attempt to call a nil value %(metamethod 'close'%)"))
		do local x <close> = nil; local y <close> = false end
		assert(not load("local a <close>, b <close> = 1, 2"))
		assert(select(2, load("local a <close> = nil; a = 1")):find("attempt to assign to const variable 'a'"))

		-- A generic for closes its closing value, however it ends.
		local function it()
			local i = 0
			return function() i = i + 1; if i <= 2 then return i end end, nil, nil, obj("it")
		end
		for i in it() do end
		assert(take() == "it:nil")
		for i in it() do break end
		assert(take() == "it:nil")
		assert(not pcall(function() for i in it() do error("loop", 0) end end))
		assert(take() == "it:loop")

		-- Coroutines: close, wrap, and yields inside __close.
		local co = coroutine.create(function() local x <close> = obj("co"); coroutine.yield() end)
		coroutine.resume(co)
		assert(coroutine.close(co) == true and take() == "co:nil" and coroutine.status(co) == "dead")
		co = coroutine.create(function() local x <close> = obj("dead"); error("died", 0) end)
		coroutine.resume(co)
		local ok, err = coroutine.close(co)
		assert(not ok and err == "died" and take() == "dead:died" and coroutine.close(co) == true)
		assert(select(2, pcall(coroutine.close, coroutine.running())):find("cannot close main thread"))
		assert(not pcall(coroutine.wrap(function() local x <close> = obj("w"); error("werr", 0) end)))
		assert(take() == "w:werr")
		co = coroutine.wrap(function()
			do local x <close> = setmetatable({}, {__close = function() coroutine.yield("in close") end}) end
			return "done"
		end)
		assert(co() == "in close" and co() == "done")
		co = coroutine.wrap(function()
			return pcall(function()
				local x <close> = setmetatable({}, {__close = function() coroutine.yield("y") end})
				error("E", 0)
			end)
		end)
		assert(co() == "y")
		local ok, err = co()
		assert(not ok and err == "E")
		co = coroutine.create(function() local x <close> = obj("self"); coroutine.close() end)
		assert(coroutine.resume(co) == true and take() == "self:nil" and coroutine.status(co) == "dead")

		-- Files close as to-be-closed variables.
		local name = os.tmpname()
		do local f <close> = assert(io.open(name, "w")); f:write("x"); log.f = f end
		assert(io.type(log.f) == "closed file")
		os.remove(name)
	`)
}
