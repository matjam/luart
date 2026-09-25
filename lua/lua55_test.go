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
	{name: "attrib", returns: "27", pending: "integers, global declarations"},
	{name: "big", wrapped: true, pending: "tracebacks name metamethods ('newindex')"},
	{name: "bitwise", pending: "integers, bitwise operators"},
	{name: "bwcoercion", skip: "a module bitwise.lua loads"},
	{name: "calls", returns: "deep", pending: "integers, global declarations"},
	{name: "closure", pending: "global declarations"},
	{name: "code", skip: "needs C Lua's internal test library (T)"},
	{name: "constructs", pending: "integers"},
	{name: "coroutine", pending: "integers, coroutine.close, to-be-closed variables"},
	{name: "cstack", pending: "integers"},
	{name: "db", pending: "integers"},
	{name: "errors", pending: "integers, global declarations"},
	{name: "events", returns: "12", pending: "integers, bitwise metamethods"},
	{name: "files", pending: "integers, global declarations"},
	{name: "gc", pending: "integers"},
	{name: "gengc", pending: "integers"},
	{name: "goto", strip: true, pending: "global declarations"},
	{name: "heavy", skip: "not in all.lua: needs gigabytes of memory"},
	{name: "literals", pending: "integers, global declarations"},
	{name: "locals", returns: "5", pending: "integers, to-be-closed variables, global declarations"},
	{name: "main", skip: "tests the standalone interpreter, lua.c"},
	{name: "math", pending: "integers"},
	{name: "memerr", skip: "needs C Lua's internal test library (T) to fail allocations"},
	{name: "nextvar", pending: "integers, global declarations"},
	{name: "pm", pending: "integers, global declarations"},
	{name: "sort", strip: true, pending: "integers, table.move"},
	{name: "strings", pending: "integers"},
	{name: "tpack", pending: "string.pack"},
	{name: "tracegc", skip: "a module gc.lua loads"},
	{name: "utf8", pending: "utf8 library, global declarations"},
	{name: "vararg", pending: "integers, named varargs"},
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
