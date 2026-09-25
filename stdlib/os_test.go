package stdlib_test

import "testing"

// os.date formats as C's strftime does in the C locale; "!" is UTC.
func TestOSDate(t *testing.T) {
	run(t, `
		local function eq(want, got) assert(got == want, "got '" .. tostring(got) .. "', want '" .. want .. "'") end
		eq("1970-01-01 00:00:00", os.date("!%Y-%m-%d %H:%M:%S", 0))
		eq("Thu Jan  1 00:00:00 1970", os.date("!%c", 0))
		-- 1970-01-04 13:05:09 UTC, a Sunday.
		local t = 3 * 86400 + 13 * 3600 + 5 * 60 + 9
		eq("01/04/70 13:05:09 PM 004", os.date("!%x %X %p %j", t))
		eq("Sun Sunday Jan January 70 19", os.date("!%a %A %b %B %y %C", t))
		eq(" 4 01/04/70 1970-01-04 13:05 13:05:09", os.date("!%e %D %F %R %T", t))
		eq("7 0 01 00 01 1970 70 01 Jan", os.date("!%u %w %U %W %V %G %g %I %h", t))
		eq("01:05:09 PM|\n|\t|%|+0000", os.date("!%r|%n|%t|%%|%z", t))
		eq("13 70 1970", os.date("!%OH %Ey %EY", t)) -- E and O modifiers change nothing in C
		eq("plain text", os.date("!plain text", t))

		local d = os.date("!*t", 0)
		assert(d.year == 1970 and d.month == 1 and d.day == 1 and d.hour == 0 and d.min == 0 and d.sec == 0)
		assert(d.wday == 5 and d.yday == 1 and d.isdst == false)
		assert(type(os.date()) == "string")

		local ok, err = pcall(os.date, "%Ez")
		assert(not ok and err:find("invalid conversion specifier '%Ez'", 1, true), err)
		ok, err = pcall(os.date, "%q")
		assert(not ok and err:find("invalid conversion specifier '%q'", 1, true), err)
	`)
}

// os.time converts a date table in local time, and os.date back.
func TestOSTime(t *testing.T) {
	run(t, `
		local t = os.time{year = 2026, month = 9, day = 25, hour = 10, min = 5, sec = 7}
		local d = os.date("*t", t)
		assert(d.year == 2026 and d.month == 9 and d.day == 25 and d.hour == 10 and d.min == 5 and d.sec == 7)
		assert(os.time{year = 2026, month = 9, day = 25} == os.time{year = 2026, month = 9, day = 25, hour = 12})
		-- Fields out of range normalise, as mktime does.
		assert(os.date("*t", os.time{year = 2026, month = 13, day = 1}).year == 2027)
		local ok, err = pcall(os.time, {year = 2026, month = 1})
		assert(not ok and err:find("field 'day' missing in date table", 1, true), err)
		assert(math.abs(os.time() - os.time(os.date("*t"))) <= 1)
	`)
}

// Only the C locale exists: os.setlocale reports it and refuses others.
func TestOSSetlocale(t *testing.T) {
	run(t, `
		assert(os.setlocale() == "C" and os.setlocale("C") == "C" and os.setlocale("POSIX") == "C")
		assert(os.setlocale("") == "C" and os.setlocale(nil, "numeric") == "C")
		assert(os.setlocale("en_US.UTF-8") == nil)
		local ok, err = pcall(os.setlocale, "C", "bogus")
		assert(not ok and err:find("invalid option 'bogus'", 1, true), err)
	`)
}
