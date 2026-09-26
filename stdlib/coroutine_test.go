package stdlib_test

import "testing"

func TestCoroutineBasics(t *testing.T) {
	run(t, `
		local co = coroutine.create(function(a, b)
			local c = coroutine.yield(a + b)
			local d, e = coroutine.yield(c * 2)
			return d + e
		end)
		assert(coroutine.status(co) == "suspended")
		local ok, v = coroutine.resume(co, 1, 2)
		assert(ok and v == 3 and coroutine.status(co) == "suspended")
		ok, v = coroutine.resume(co, 10)
		assert(ok and v == 20)
		ok, v = coroutine.resume(co, 3, 4)
		assert(ok and v == 7 and coroutine.status(co) == "dead")
		ok, v = coroutine.resume(co)
		assert(not ok and v == "cannot resume dead coroutine")

		local gen = coroutine.wrap(function(n) for i = 1, n do coroutine.yield(i) end return "done" end)
		assert(gen(3) == 1 and gen() == 2 and gen() == 3 and gen() == "done")

		local main, ismain = coroutine.running()
		assert(type(main) == "thread" and ismain)
		co = coroutine.create(function()
			local me, ismain = coroutine.running()
			assert(not ismain and coroutine.status(me) == "running")
			local inner = coroutine.create(function() return coroutine.status(me) end)
			return select(2, coroutine.resume(inner))
		end)
		assert(select(2, coroutine.resume(co)) == "normal")

		ok, v = pcall(coroutine.yield, 1)
		assert(not ok and v:find("attempt to yield from outside a coroutine", 1, true))
		co = coroutine.create(function() return coroutine.resume(coroutine.running()) end)
		local _, ok2, v2 = coroutine.resume(co)
		assert(not ok2 and v2 == "cannot resume non-suspended coroutine")
	`)
}

// A coroutine can yield across pcall, metamethods and iterators, but not
// across a Go function without a continuation.
func TestCoroutineYieldAcross(t *testing.T) {
	run(t, `
		local function collect(f, ...)
			local co, out = coroutine.create(f), {}
			local args = table.pack(...)
			while true do
				local r = table.pack(coroutine.resume(co, table.unpack(args, 1, args.n)))
				assert(r[1], r[2])
				if coroutine.status(co) == "dead" then out.result = r[2]; return out end
				out[#out + 1] = r[2]
				args = {n = 0}
			end
		end

		-- pcall and xpcall, with an error after the yield
		local out = collect(function()
			local ok, e = pcall(function() coroutine.yield("in pcall"); error("oops", 0) end)
			local ok2, e2 = xpcall(function() coroutine.yield("in xpcall"); error("again", 0) end,
				function(m) return "handled " .. m end)
			return tostring(ok) .. e .. tostring(ok2) .. e2
		end)
		assert(out[1] == "in pcall" and out[2] == "in xpcall" and out.result == "falseoopsfalsehandled again")

		-- metamethods
		local y = coroutine.yield
		local mt = {
			__index = function(t, k) y("index"); return k .. "!" end,
			__newindex = function(t, k, v) y("newindex"); rawset(t, k, v * 2) end,
			__add = function(a, b) y("add"); return 42 end,
			__concat = function(a, b) y("concat"); return "cat" end,
			__lt = function(a, b) y("lt"); return true end,
			__le = function(a, b) y("le"); return false end,
			__eq = function(a, b) y("eq"); return true end,
			__len = function(a) y("len"); return 7 end,
			__call = function(self, x) y("call"); return x + 1 end,
		}
		out = collect(function()
			local a, b = setmetatable({}, mt), setmetatable({}, mt)
			local r = {}
			r[1] = a.foo
			a.bar = 5
			r[2] = rawget(a, "bar")
			r[3] = a + 1
			r[4] = "x" .. a .. "y"
			r[5] = a < b
			r[6] = a <= b
			r[7] = a == b
			r[8] = #a
			r[9] = a(9)
			local s = a.foo2
			return table.concat({r[1], r[2], r[3], r[4], tostring(r[5]), tostring(r[6]), tostring(r[7]), r[8], r[9]}, ",")
		end)
		assert(out.result == "foo!,10,42,xcat,true,false,true,7,10", out.result) -- a .. "y" first
		assert(table.concat(out, ",") == "index,newindex,add,concat,lt,le,eq,len,call,index", table.concat(out, ","))

		-- a generic for's iterator
		out = collect(function()
			local n = 0
			for i in function(_, i) i = (i or 0) + 1; y(i); if i <= 3 then return i end end do n = n + i end
			return n
		end)
		assert(out.result == 6 and #out == 4)

		-- not across table.sort, a Go function without a continuation
		local co = coroutine.create(function() table.sort({3, 2, 1}, function(a, b) y(); return a < b end) end)
		local ok, e = coroutine.resume(co)
		assert(not ok and e:find("attempt to yield across a C-call boundary", 1, true), e)
	`)
}

// Errors end a coroutine; wrap raises them with a position.
func TestCoroutineErrors(t *testing.T) {
	run(t, `
		local co = coroutine.create(function() coroutine.yield(); error({code = 1}) end)
		coroutine.resume(co)
		local ok, e = coroutine.resume(co)
		assert(not ok and type(e) == "table" and e.code == 1 and coroutine.status(co) == "dead")
		local w = coroutine.wrap(function() error("bad") end)
		ok, e = pcall(w)
		assert(not ok and e:find("bad", 1, true), e)
		ok, e = pcall(coroutine.resume, 1)
		assert(not ok and e:find("coroutine expected", 1, true), e)
		ok, e = pcall(coroutine.create, 1)
		assert(not ok)
	`)
}

// Compiled code yields and resumes: a hot loop in a coroutine.
func TestCoroutineCompiled(t *testing.T) {
	run(t, `
		local function gen(n)
			return coroutine.wrap(function()
				local x = 0
				for i = 1, n do x = x + i * 0.5; coroutine.yield(i) end
				return x
			end)
		end
		local g, sum = gen(5000), 0
		for _ = 1, 5000 do sum = sum + g() end
		assert(sum == 5000 * 5001 / 2 and g() == 5000 * 5001 / 4)

		-- yields from deep recursion
		local function deep(n) if n == 0 then coroutine.yield("bottom") return 0 end return 1 + deep(n - 1) end
		local co = coroutine.wrap(function() return deep(150) end)
		assert(co() == "bottom" and co() == 150)
	`)
}
