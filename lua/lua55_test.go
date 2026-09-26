package lua

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// suite55 is the official Lua 5.5.1 test suite, in ../lua-5.5-tests,
// unmodified. Each file runs as all.lua runs it: with _soft, _port and
// _nomsg set, through string.dump and load, and checking what it returns.
//
// pending lists the files luart does not pass yet, with the reason; the
// list shrinks as luart approaches 5.5. LUART_SUITE_PROGRESS=1 runs them
// anyway and logs where each stops, without failing.
var suite55 = []struct {
	name    string
	returns string // the Lua expression the file's result must equal, or ""
	strip   bool   // dump with debug information stripped, as all.lua does
	wrapped bool   // runs in a coroutine, yielding 'b' and returning 'a', with _soft unset (all.lua skips it when soft)
	pending string // why the file cannot pass yet, or ""
	skip    string // why it never runs here, or ""
}{
	{name: "api", skip: "needs C Lua's internal test library (T)"},
	{name: "attrib", returns: "27"},
	{name: "big", wrapped: true},
	{name: "bitwise", pending: "string.pack"},
	{name: "bwcoercion", skip: "a module bitwise.lua loads"},
	{name: "calls", returns: "deep", pending: "string.pack"},
	{name: "closure", pending: "debug.upvalueid fails for a bad index (5.4)"},
	{name: "code", skip: "needs C Lua's internal test library (T)"},
	{name: "constructs"},
	{name: "coroutine", pending: "coroutine.isyieldable"},
	{name: "cstack"},
	{name: "db"},
	{name: "errors", pending: "error(nil) gives \"<no error object>\" (5.5)"},
	{name: "events", returns: "12", pending: "__eq from either operand (5.4)"},
	{name: "files", pending: "os.date and os.time checks; /dev/full, which macOS lacks"},
	{name: "gc", pending: "collectgarbage returns the previous mode"},
	{name: "gengc", pending: "collectgarbage returns the previous mode"},
	{name: "goto", strip: true},
	{name: "heavy", skip: "not in all.lua: needs gigabytes of memory"},
	{name: "literals", pending: "\\u escapes"},
	{name: "locals", returns: "5"},
	{name: "main", skip: "tests the standalone interpreter, lua.c"},
	{name: "math", pending: "string.pack"},
	{name: "memerr", skip: "needs C Lua's internal test library (T) to fail allocations"},
	{name: "nextvar", pending: "table.create"},
	{name: "pm", pending: "utf8 library"},
	{name: "sort", strip: true, pending: "table.create"},
	{name: "strings", pending: "\\u escapes"},
	{name: "tpack", pending: "string.pack"},
	{name: "tracegc", skip: "a module gc.lua loads"},
	{name: "utf8", pending: "\\u escapes, utf8 library"},
	{name: "vararg"},
	{name: "verybig", returns: "10", strip: true},
}

func TestLua55(t *testing.T) {
	progress := os.Getenv("LUART_SUITE_PROGRESS") == "1"
	dir, err := filepath.Abs("../lua-5.5-tests")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range suite55 {
		t.Run(f.name, func(t *testing.T) {
			switch {
			case f.skip != "":
				t.Skip(f.skip)
			case f.pending != "" && !progress:
				t.Skip("pending: " + f.pending)
			}
			err := runSuiteFile(t, dir, f.name, f.returns, f.strip, f.wrapped)
			switch {
			case f.pending != "" && err != nil:
				t.Logf("pending (%s): %v", f.pending, err)
			case f.pending != "":
				t.Logf("passes: take it off the pending list")
			case err != nil:
				t.Fatal(err)
			}
		})
	}
}

// runSuiteFile runs name.lua from dir in a new state, as all.lua would.
func runSuiteFile(t *testing.T, dir, name, returns string, strip, wrapped bool) error {
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer os.Chdir(cwd)
	l := NewState()
	l.global.goName = "C" // the suite expects C Lua's names, as LUART_GO_AS_C=1 gives
	openLibraries(l)
	for _, g := range []string{"_soft", "_port", "_nomsg"} {
		l.PushBoolean(g != "_soft" || !wrapped)
		l.SetGlobal(g)
	}
	check := "true"
	if returns != "" {
		check = fmt.Sprintf("r == %s", returns)
	}
	driver := fmt.Sprintf(`
		local f = assert(loadfile(%q))
		f = assert(load(string.dump(f, %v)))
		local r = f()
		assert(%s, "%s.lua returned " .. tostring(r))`, name+".lua", strip, check, name)
	if wrapped {
		driver = fmt.Sprintf(`
			local f = coroutine.wrap(assert(loadfile(%q)))
			assert(f() == 'b')
			assert(f() == 'a')`, name+".lua")
	}
	if err := l.LoadString(driver); err != nil {
		msg, _ := l.ToString(-1)
		return fmt.Errorf("%v: %s", err, msg)
	}
	l.Global("debug")
	l.Field(-1, "traceback")
	l.Remove(-2)
	l.Insert(-2)
	if err := l.ProtectedCall(0, 0, -2); err != nil {
		msg, _ := l.ToString(-1)
		return fmt.Errorf("%s", strings.TrimSpace(msg))
	}
	return nil
}
