package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// plain is a palette without colour, so tests read the text.
var plain = palette{lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle()}

func TestCompile(t *testing.T) {
	l := newState()
	for _, tt := range []struct {
		code     string
		complete bool
	}{
		{"1 + 1", true},
		{"x = 1", true},
		{"= x", true},
		{"function f()", false},
		{"for i = 1, 2 do", false},
		{"x = ", false},
		{"'abc", false},
		{"x = )", true}, // an error, but not one more lines can fix
	} {
		if got := isComplete(l, tt.code); got != tt.complete {
			t.Errorf("isComplete(%q) = %v, want %v", tt.code, got, tt.complete)
		}
		if l.Top() != 0 {
			t.Fatalf("isComplete(%q) left %d values on the stack", tt.code, l.Top())
		}
	}
}

func TestFormat(t *testing.T) {
	l := newState()
	for _, tt := range []struct {
		src, want string
		width     int
	}{
		{`return 1, 2.5, "a\nb", true, nil`, "1|2.5|\"a\\nb\"|true|nil", 80},
		{`return {}`, "{}", 80},
		{`return {1, 2, 3}`, "{1, 2, 3}", 80},
		{`return {x = 1, ["not a name"] = 3, ["end"] = 4}`, `{x = 1, ["not a name"] = 3, ["end"] = 4}`, 80},
		{`return {[2.5] = true}`, `{[2.5] = true}`, 80},
		{`return {1, {2, {3, {4, {5}}}}}`, "{1, {2, {3, {4, {…}}}}}", 80},
		{`local t = {}; t.self = t; return t`, "{self = <cycle>}", 80},
		{`return setmetatable({}, {__tostring = function() return "custom" end})`, "custom", 80},
		{`return setmetatable({}, {__tostring = function() error("no") end})`, "<", 80},
		{`return {alpha = 1, beta = {1, 2}}`, "{\n  alpha = 1,\n  beta = {1, 2}\n}", 16},
	} {
		if err := l.DoString(tt.src); err != nil {
			t.Fatal(err)
		}
		f := &formatter{l: l, p: plain, maxDepth: 4, maxItems: 40}
		var got []string
		for i := 1; i <= l.Top(); i++ {
			got = append(got, f.format(i, tt.width))
		}
		l.SetTop(0)
		if s := strings.Join(got, "|"); !strings.HasPrefix(s, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.src, s, tt.want)
		}
	}
	if err := l.DoString(`local t = {}; for i = 1, 50 do t[i] = i end; return t`); err != nil {
		t.Fatal(err)
	}
	f := &formatter{l: l, p: plain, maxDepth: 4, maxItems: 40}
	if got := f.format(1, 1000); !strings.HasSuffix(got, "40, … 10 more}") {
		t.Errorf("long array: %q", got)
	}
}

func TestCompletions(t *testing.T) {
	l := newState()
	if err := l.DoString(`obj = setmetatable({own = 1}, {__index = {inherited = function() end, value = 2}})`); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		before  string
		want    []string
		replace int
	}{
		{"str", []string{"string"}, 3},
		{"print(string.fo", []string{"format"}, 2},
		{"string.", nil, 0}, // every name: checked by length below
		{"obj.", []string{"inherited", "own", "value"}, 0},
		{"obj:", []string{"inherited"}, 0},
		{`("x"):up`, nil, 0},
		{"wh", []string{"while"}, 2},
		{"x = 12", nil, 0},
		{"nope.x", nil, 0},
	} {
		got, n := completions(l, tt.before)
		if tt.before == "string." {
			if !slices.Contains(got, "rep") || !slices.Contains(got, "format") {
				t.Errorf("string.: %v", got)
			}
			continue
		}
		if !slices.Equal(got, tt.want) || n != tt.replace && len(tt.want) > 0 {
			t.Errorf("completions(%q) = %v, %d; want %v, %d", tt.before, got, n, tt.want, tt.replace)
		}
		if l.Top() != 0 {
			t.Fatalf("completions(%q) left the stack at %d", tt.before, l.Top())
		}
	}
	if got := commonPrefix([]string{"gmatch", "gsub"}); got != "g" {
		t.Errorf("commonPrefix = %q", got)
	}
}

func TestHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := loadHistory(path)
	h.add("print(1)")
	h.add("print(1)") // a repeat is not kept
	h.add("for i = 1, 2 do\n  print(i)\nend")
	for i := range maxHistory + 5 {
		h.add(fmt.Sprint(i))
	}
	again := loadHistory(path)
	if len(again.entries) != maxHistory {
		t.Fatalf("%d entries, want %d", len(again.entries), maxHistory)
	}
	h2 := loadHistory(filepath.Join(t.TempDir(), "h"))
	h2.add("for i = 1, 2 do\n  print(i)\nend")
	if got := loadHistory(h2.path).entries; len(got) != 1 || got[0] != "for i = 1, 2 do\n  print(i)\nend" {
		t.Fatalf("multi-line entry came back as %q", got)
	}
	loadHistory("").add("x") // in memory only: must not fail
}

// The capture forwards output in order, whole lines while attached, and
// the unfinished end of a line at a Sync.
func TestCapture(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	c, err := startCapture()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines []string
	c.attach(func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() })
	fmt.Fprint(os.Stdout, "one\ntw")
	fmt.Fprint(os.Stderr, "o\nthree")
	c.Sync()
	mu.Lock()
	got := slices.Clone(lines)
	mu.Unlock()
	if want := []string{"one", "two", "three"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	c.detach()
	c.stop()
	if os.Stdout != stdout {
		t.Fatal("stdout not restored")
	}
}

// tui drives a model as Bubble Tea would, collecting what it prints.
type tui struct {
	t     *testing.T
	m     *model
	mu    sync.Mutex
	lines []string
	quit  bool
}

func newTUI(t *testing.T) *tui {
	t.Setenv("LUART_HISTORY", "")
	c := &cli{l: newState(), progName: ""}
	u := &tui{t: t}
	u.m = newModel(c, newTheme(true))
	u.m.print = func(s string) { u.mu.Lock(); u.lines = append(u.lines, ansi.Strip(s)); u.mu.Unlock() }
	u.m.width = 80
	return u
}

// send delivers msg and runs the commands it returns, and theirs, until
// none are left, as the program's event loop would.
func (u *tui) send(msg tea.Msg) {
	_, cmd := u.m.Update(msg)
	cmds := []tea.Cmd{cmd}
	for len(cmds) > 0 {
		cmd, cmds = cmds[0], cmds[1:]
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			cmds = append(cmds, msg...)
		case tea.QuitMsg:
			u.quit = true
		default:
			_, next := u.m.Update(msg)
			cmds = append(cmds, next)
		}
	}
}

func (u *tui) typeText(s string) {
	for _, r := range s {
		u.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (u *tui) press(code rune, mod tea.KeyMod) { u.send(tea.KeyPressMsg{Code: code, Mod: mod}) }

func (u *tui) transcript() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return strings.Join(u.lines, "\n")
}

func TestTUIEvaluates(t *testing.T) {
	u := newTUI(t)
	u.typeText("1 + 1")
	u.press(tea.KeyEnter, 0)
	if got := u.transcript(); !strings.Contains(got, "❯ 1 + 1\n2") {
		t.Fatalf("transcript %q", got)
	}
	u.typeText("x = {a = 1}")
	u.press(tea.KeyEnter, 0)
	u.typeText("x")
	u.press(tea.KeyEnter, 0)
	if got := u.transcript(); !strings.Contains(got, "{a = 1}") {
		t.Fatalf("transcript %q", got)
	}
	u.typeText("error('boom')")
	u.press(tea.KeyEnter, 0)
	if got := u.transcript(); !strings.Contains(got, "✗ stdin:1: boom") || !strings.Contains(got, "stack traceback:") {
		t.Fatalf("transcript %q", got)
	}
}

func TestTUIContinuesUnfinishedStatements(t *testing.T) {
	u := newTUI(t)
	u.typeText("for i = 1, 2 do")
	u.press(tea.KeyEnter, 0)
	if got := u.m.input.Value(); got != "for i = 1, 2 do\n" {
		t.Fatalf("input %q, want a new line", got)
	}
	u.typeText("x = i end")
	u.press(tea.KeyEnter, 0)
	if u.m.input.Value() != "" || !strings.Contains(u.transcript(), "· x = i end") {
		t.Fatalf("input %q, transcript %q", u.m.input.Value(), u.transcript())
	}
}

func TestTUIHistoryAndCompletion(t *testing.T) {
	u := newTUI(t)
	u.typeText("print('a')")
	u.press(tea.KeyEnter, 0)
	u.press(tea.KeyUp, 0)
	if got := u.m.input.Value(); got != "print('a')" {
		t.Fatalf("up: %q", got)
	}
	u.press(tea.KeyDown, 0)
	if got := u.m.input.Value(); got != "" {
		t.Fatalf("down: %q", got)
	}
	u.typeText("string.for")
	u.press(tea.KeyTab, 0)
	if got := u.m.input.Value(); got != "string.format" {
		t.Fatalf("tab: %q", got)
	}
	u.m.input.Reset()
	u.typeText("string.g")
	u.press(tea.KeyTab, 0) // gmatch, gsub: nothing in common beyond "g"
	first := u.m.input.Value()
	u.press(tea.KeyTab, 0)
	if second := u.m.input.Value(); first == second || !strings.HasPrefix(second, "string.g") {
		t.Fatalf("tab cycled %q to %q", first, second)
	}
}

func TestTUICommandsAndQuit(t *testing.T) {
	u := newTUI(t)
	u.typeText("/help")
	u.press(tea.KeyEnter, 0)
	if !strings.Contains(u.transcript(), "/reset") {
		t.Fatalf("transcript %q", u.transcript())
	}
	u.typeText("/jit off")
	u.press(tea.KeyEnter, 0)
	if !u.m.c.noJIT || !strings.Contains(u.transcript(), "interpreted") {
		t.Fatalf("transcript %q", u.transcript())
	}
	u.typeText("/nonsense")
	u.press(tea.KeyEnter, 0)
	if !strings.Contains(u.transcript(), "unknown command /nonsense") {
		t.Fatalf("transcript %q", u.transcript())
	}
	u.press('c', tea.ModCtrl)
	if u.quit {
		t.Fatal("quit on the first ctrl+c")
	}
	u.press('c', tea.ModCtrl)
	if !u.quit {
		t.Fatal("did not quit on the second ctrl+c")
	}
}

// Esc interrupts an evaluation, which runs off the event loop.
func TestTUIInterrupts(t *testing.T) {
	u := newTUI(t)
	u.typeText("while true do end")
	_, cmd := u.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !u.m.running {
		t.Fatal("not running")
	}
	done := make(chan tea.Msg)
	go func() {
		for _, c := range cmd().(tea.BatchMsg) {
			if c == nil {
				continue
			}
			go func() {
				if msg, ok := c().(evalDoneMsg); ok {
					done <- msg
				}
			}()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	u.m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	select {
	case msg := <-done:
		u.m.Update(msg)
	case <-time.After(10 * time.Second):
		t.Fatal("not interrupted")
	}
	if u.m.running || !strings.Contains(u.transcript(), "interrupted!") {
		t.Fatalf("running %v, transcript %q", u.m.running, u.transcript())
	}
}
