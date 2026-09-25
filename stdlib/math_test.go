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
		assert(tostring(-0.0) == "-0" and tostring(1e15) == "1e+15" and tostring(2^53) == "9.007199254741e+15")

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
