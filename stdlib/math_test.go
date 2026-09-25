package stdlib_test

import "testing"

// math.huge is infinity, and infinities and NaNs print as C's printf
// prints them.
func TestInfinityAndNaN(t *testing.T) {
	run(t, `
		assert(math.huge > 1.7976931348623157e308 and math.huge * 2 == math.huge)
		assert(-math.huge < -1.7976931348623157e308)
		assert(tostring(math.huge) == "inf" and tostring(-math.huge) == "-inf")
		local nan = 0/0
		assert(nan ~= nan)
		local s, ns = tostring(nan), tostring(-nan)
		assert((s == "nan" or s == "-nan") and ns ~= s and (ns == "nan" or ns == "-nan"), s .. " " .. ns)
		assert(tostring(-0.0) == "-0.0" and tostring(1e15) == "1e+15" and tostring(2^53) == "9007199254740992.0") -- 5.5 reads back exactly

		assert(string.format("%f", math.huge) == "inf")
		assert(string.format("%.3f", -math.huge) == "-inf")
		assert(string.format("%5.1f", math.huge) == "  inf")
		assert(string.format("%-5g|", math.huge) == "inf  |")
		assert(string.format("%+e", math.huge) == "+inf")
		assert(string.format("%G", math.huge) == "INF")
		assert(string.format("%E", -math.huge) == "-INF")
		local f = string.format("%f", nan)
		assert(f == "nan" or f == "-nan", f)
	`)
}

// Integers and floats behave as C Lua 5.5's do; each case was diffed
// against it.
func TestIntegerSemantics(t *testing.T) {
	run(t, `
		local function err(f, ...) local ok, e = pcall(f, ...) assert(not ok) return (e:gsub("^.-:%d+: ", "")) end
		assert(math.type(1) == "integer" and math.type(1.0) == "float" and math.type("1") == nil)
		assert(7 // 2 == 3 and math.type(7 // 2) == "integer" and 7 // 2.0 == 3.0 and -7 // 2 == -4)
		assert(-7 % 3 == 2 and 7 % -3 == -2 and math.type(7 % 3.0) == "float" and 5.5 % -2 == -0.5)
		assert(math.maxinteger + 1 == math.mininteger and math.mininteger // -1 == math.mininteger)
		assert(1 << 63 == math.mininteger and 1 << 64 == 0 and -1 >> 63 == 1 and 3 | 0.0 == 3)
		assert(1 == 1.0 and math.maxinteger + 0.0 ~= math.maxinteger and math.maxinteger < 2^63)
		assert(tostring(3) == "3" and tostring(3.0) == "3.0" and tostring(-0.0) == "-0.0")
		assert(tostring(2^63) == "9.2233720368547758e+18" and tostring(1e15) == "1e+15")
		assert(tostring(2^53 // 3) == "3.00239975158033e+15") -- %.15g reads back
		assert(err(function(a) return a // 0 end, 1) == "attempt to divide by zero")
		assert(err(function(a) return a % 0 end, 1) == "attempt to perform 'n%0'")
		assert(err(function(a, b) return a & b end, 1, 0.5) == "number (local 'b') has no integer representation")
		assert(err(function(a, b) return a | b end, 1, "2") == "attempt to perform bitwise operation on a string value (local 'b')")
		local i, f = math.modf(3.5) assert(math.type(i) == "integer" and i == 3 and f == 0.5)
		i, f = math.modf(-0.0) assert(math.type(i) == "integer" and f == 0.0)
		assert(math.type(math.floor(2.5)) == "integer" and math.type(math.floor(2^70)) == "float")
		assert(err(math.min) == "bad argument #1 to 'math.min' (value expected)")
		assert(err(math.random, 3, 1) == "bad argument #1 to 'math.random' (interval is empty)")
		math.randomseed(42)
		assert(math.random(100) == 50 and math.random(1, 1000000) == 154966) -- C Lua 5.5's values
		assert(math.type(os.time()) == "integer" and math.type(os.time{year = 2020, month = 1, day = 1}) == "integer")
		local t = {} t[1.0] = "a" t[2^53] = "b"
		assert(t[1] == "a" and math.type(next(t)) == "integer" and t[2^53 | 0] == "b")
		local s = 0 for i = math.maxinteger - 2, math.maxinteger do s = s + 1 end assert(s == 3)
		s = 0 for i = 1, 3.5 do s = s + i end assert(s == 6 and math.type(s) == "integer")
		assert(err(function() for i = 1, 10, 0 do end end) == "'for' step is zero")
	`)
}
