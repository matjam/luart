package stdlib_test

import (
	"strings"
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// run runs script as the chunk "=test" in a state with the standard
// libraries.
func run(t *testing.T, script string) {
	t.Helper()
	l := lua.NewState()
	stdlib.Open(l)
	err := l.Load(strings.NewReader(script), "=test", "t")
	if err == nil {
		err = l.ProtectedCall(0, 0, 0)
	}
	if err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal(err, msg)
	}
}

func TestStringFind(t *testing.T) {
	run(t, `
		local function eq(want, ...)
			local got = table.concat({...}, ",")
			assert(got == want, "got " .. got .. ", want " .. want)
		end
		eq("2,2", string.find("a.b", ".", 1, true))
		eq("1,1", string.find("a.b", "."))
		eq("3,3", string.find("a.b", "b"))
		eq("2,3,bc", string.find("abcd", "(b%a)"))
		eq("", string.find("abc", "^b") or "")
		eq("2,3", string.find("abc", "b.", -2))
		eq("", string.find("abc", "x", 10) or "")
	`)
}

func TestStringMatch(t *testing.T) {
	run(t, `
		assert(string.match("key = value", "(%w+)%s*=%s*(%w+)") == "key")
		local k, v = string.match("key = value", "(%w+)%s*=%s*(%w+)")
		assert(k == "key" and v == "value")
		assert(string.match("hello", "()ll()") == 3)
		assert(select(2, string.match("hello", "()ll()")) == 5)
		assert(string.match("  trim  ", "^%s*(.-)%s*$") == "trim")
		assert(string.match("f(a(b)c)d", "%b()") == "(a(b)c)")
		assert(string.match("THE (quick) fox", "%f[%a]%a+%f[%A]") == "THE")
		assert(string.match("abab", "(ab)%1") == "ab")
		assert(string.match("x = 10", "%d+$") == "10")
		assert(string.match("a$b", "a$b") == "a$b")
		assert(string.match("[]", "[]]") == "]")
		assert(string.match("a-b", "[%-]") == "-")
		assert(string.match("zeta", "[a-f]") == "e")
		assert(string.match("abc", "[^ab]") == "c")
		assert(string.match("abc", "a*?") == nil) -- a literal '?' after a*
		assert(string.match("aaa", "a-") == "")
		assert(string.match("aaab", "a-b") == "aaab")
		assert(string.match("", ".?") == "")
		assert(string.match("abc", "^(a)(b)(c)$") == "a")
	`)
}

func TestStringGmatch(t *testing.T) {
	run(t, `
		local words = {}
		for w in string.gmatch("one two  three", "%a+") do words[#words+1] = w end
		assert(table.concat(words, ",") == "one,two,three")
		local t = {}
		for k, v in string.gmatch("a=1, b=2", "(%w+)=(%w+)") do t[k] = v end
		assert(t.a == "1" and t.b == "2")
		local n = 0
		for _ in string.gmatch("abc", "") do n = n + 1 end
		assert(n == 4, n)
	`)
}

func TestStringGsub(t *testing.T) {
	run(t, `
		local function eq(want, got, gotn, wantn)
			assert(got == want, "got " .. tostring(got) .. ", want " .. want)
			if wantn then assert(gotn == wantn, "count " .. gotn) end
		end
		eq("hello world", string.gsub("hello world", "o", "o"))
		eq("hellX wXrld", string.gsub("hello world", "o", "X"))
		eq("hellX world", string.gsub("hello world", "o", "X", 1))
		eq("world hello", string.gsub("hello world", "(%w+) (%w+)", "%2 %1"))
		eq("<hello> <world>", string.gsub("hello world", "%w+", "<%0>"))
		eq("x = 1", string.gsub("$name = $value", "%$(%w+)", {name = "x", value = 1}))
		eq("HELLO world", string.gsub("hello world", "^%w+", string.upper))
		eq("hello world", string.gsub("hello world", "%w+", function() return nil end))
		eq("-a-b-c-", string.gsub("abc", "", "-"))
		eq("100%", string.gsub("100", "$", "%%"))
		local s, n = string.gsub("abc", "%w", "%0%0")
		eq("aabbcc", s, n, 3)
	`)
}

func TestStringPatternErrors(t *testing.T) {
	run(t, `
		local function fails(msg, f, ...)
			local ok, err = pcall(f, ...)
			assert(not ok and err:find(msg, 1, true), "want " .. msg .. ", got " .. tostring(err))
		end
		fails("malformed pattern (ends with '%')", string.find, "a", "a%")
		fails("malformed pattern (missing ']')", string.find, "a", "[a")
		fails("malformed pattern (missing arguments to '%b')", string.find, "a", "%b")
		fails("missing '[' after '%f' in pattern", string.find, "a", "%fa")
		fails("invalid pattern capture", string.match, "a", "a)")
		fails("invalid capture index %1", string.find, "a", "%1")
		fails("unfinished capture", string.match, "a", "(a")
		fails("invalid use of '%' in replacement string", string.gsub, "a", "a", "%x")
		fails("invalid capture index", string.gsub, "a", "a", "%2")
		fails("invalid replacement value (a table)", string.gsub, "a", "a", function() return {} end)
		fails("string/function/table expected", string.gsub, "a", "a", true)
		fails("too many captures", string.find, "a", string.rep("()", 33))
		fails("pattern too complex", string.find, string.rep("a", 300), string.rep("a?", 300) .. string.rep("a", 300))
	`)
}

func TestStringDump(t *testing.T) {
	run(t, `
		local f = load(string.dump(function(a, b) return a * b + 1 end))
		assert(f(6, 7) == 43)
		local ok, err = pcall(string.dump, print)
		assert(not ok and err:find("unable to dump given function", 1, true), err)
		assert(not pcall(string.dump, 1))
	`)
}

// string.reverse, lower and upper work on bytes, as Lua's do in the C
// locale: only ASCII letters change case, and bytes that are not UTF-8
// survive.
func TestStringBytes(t *testing.T) {
	run(t, `
		assert(string.reverse("ab\255\0c") == "c\0\255ba")
		assert(string.upper("é\255a") == "é\255A")
		assert(string.lower("É\255A") == "É\255a")
	`)
}
