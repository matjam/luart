package luart

import (
	"strings"
	"testing"
)

// Errors and hooks report source lines from the VM's saved program counter.
func TestErrorPositions(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"arithmetic on nil", "local a\nlocal b = 1\nreturn b + a", `:3: attempt to perform arithmetic on local 'a' (a nil value)`},
		{"index nil", "local t = {}\n\nreturn t.x.y", `:3: attempt to index field 'x' (a nil value)`},
		{"index nil among nil locals", "local a, b\nreturn b.x", `:2: attempt to index local 'b' (a nil value)`},
		{"index nil global", "return missing.x", `:1: attempt to index global 'missing' (a nil value)`},
		{"index nil upvalue", "local u\nlocal function f() return u.x end\nf()", `:2: attempt to index upvalue 'u' (a nil value)`},
		{"arithmetic on second operand", "local a, b = 1\nreturn a + b", `:2: attempt to perform arithmetic on local 'b' (a nil value)`},
		{"call nil", "local f\nf()", `:2: attempt to call local 'f' (a nil value)`},
		{"call nil field", "local t = {}\nt.go()", `:2: attempt to call field 'go' (a nil value)`},
		{"error from Go", "local x = 1\n\nstring.rep()", `:3: bad argument #1 to 'rep'`},
		{"error from global Go function", "setmetatable(1, {})", `bad argument #1 to 'setmetatable'`},
		{"error in method", "local s = 'x'\nreturn s:rep()", `:2: bad argument #1 to 'rep'`},
		{"error in callee", "local function f()\n  error('boom')\nend\nf()", `:2: boom`},
		{"compare", "local a, b = {}, 1\nreturn a < b", `:2: attempt to compare`},
		{"concat", "local a = {}\nreturn 'x' .. a", `:2: attempt to concatenate local 'a' (a table value)`},
		{"for limit", "for i = 1, {} do end", `:1: 'for' limit must be a number`},
		{"after loop", "for i = 1, 3 do\n  local _ = i * 2\nend\nlocal t = nil\nreturn t[1]", `:5: attempt to index local 't' (a nil value)`},
		{"after number call", "local s = math.sin(1)\nlocal t\nreturn t.x", `:3: attempt to index local 't' (a nil value)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewState()
			openLibraries(l)
			err := l.DoString(tt.src)
			if err == nil {
				t.Fatal("no error")
			}
			if msg, _ := l.ToString(-1); !strings.Contains(msg, tt.want) {
				t.Fatalf("error %q does not contain %q", msg, tt.want)
			}
		})
	}
}

func TestLineHook(t *testing.T) {
	l := NewState()
	openLibraries(l)
	var lines []int
	l.SetHook(func(l *State, ar Debug) {
		lines = append(lines, ar.CurrentLine)
	}, MaskLine, 0)
	src := "local x = 1\nfor i = 1, 2 do\n  x = x + math.abs(i)\nend\nreturn x"
	if err := l.DoString(src); err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3, 2, 3, 2, 5}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
	}
}

// The Lua call and return fast paths must step aside when hooks are set.
func TestCallAndReturnHooksSeeLuaCalls(t *testing.T) {
	l := NewState()
	openLibraries(l)
	calls, returns := 0, 0
	l.SetHook(func(l *State, ar Debug) {
		switch ar.Event {
		case HookCall:
			calls++
		case HookReturn:
			returns++
		}
	}, MaskCall|MaskReturn, 0)
	src := "local function f(n) if n == 0 then return 0 end return f(n - 1) + 1 end\nlocal r = f(5)"
	if err := l.DoString(src); err != nil {
		t.Fatal(err)
	}
	// The chunk and six calls of f.
	if calls != 7 || returns != 7 {
		t.Fatalf("calls = %d, returns = %d, want 7 and 7", calls, returns)
	}
}

func TestTraceback(t *testing.T) {
	l := NewState()
	openLibraries(l)
	src := "local function inner()\n  error('deep')\nend\nlocal function outer()\n  inner()\nend\nouter()"
	l.Global("debug")
	l.Field(-1, "traceback")
	if err := l.LoadString(src); err != nil {
		t.Fatal(err)
	}
	if err := l.ProtectedCall(0, 0, -2); err == nil {
		t.Fatal("no error")
	}
	msg, _ := l.ToString(-1)
	for _, want := range []string{":2: deep", ":2: in function", ":5: in function", ":7: in main chunk"} {
		if !strings.Contains(msg, want) {
			t.Errorf("traceback missing %q:\n%s", want, msg)
		}
	}
}
