package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/matjam/luart/lua"
)

// The TUI is the REPL on a terminal. It runs inline, not full screen: the
// transcript goes into the terminal's own scrollback through Println, and
// the program draws only the input box and a status line under it.
//
// Lua runs in a goroutine while the event loop keeps drawing, so the state
// is touched only by whichever of the two is not waiting for the other:
// the event loop while idle, the evaluation while running. Everything the
// transcript shows is printed from goroutines through print, which blocks
// until the event loop takes the line, and so keeps its order.

// runTUI runs the REPL on c's state until the user leaves.
func runTUI(c *cli) error {
	m := newModel(c, newTheme(true))
	out := os.Stdout
	if c.capture != nil {
		out = c.capture.terminal
	}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(out))
	m.print = func(s string) { p.Println(s) }
	if c.capture != nil {
		c.capture.attach(m.print)
		defer c.capture.detach()
	}
	_, err := p.Run()
	return err
}

// evalDoneMsg ends an evaluation.
type evalDoneMsg struct{ elapsed time.Duration }

// bannerMsg prints the banner if the terminal has not said its background
// colour by then.
type bannerMsg struct{}

type model struct {
	c     *cli
	th    *theme
	input textarea.Model
	spin  spinner.Model
	hist  *history
	print func(string) // a transcript line; never from the event loop

	width   int
	running bool
	started time.Time
	last    time.Duration
	heap    uint64

	browse int    // the history entry shown, or len(hist.entries)
	draft  string // the input before browsing began

	candidates []string
	selected   int // the candidate Tab put in, or -1

	hint     string
	quitting bool // Ctrl+C was pressed on an empty input
	bannered bool
}

func newModel(c *cli, th *theme) *model {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.SetWidth(1 << 12) // the model wraps nothing: it draws the lines itself
	in.SetHeight(1 << 12)
	in.KeyMap.InsertNewline.SetKeys("alt+enter", "shift+enter", "ctrl+j")
	in.SetVirtualCursor(false) // View puts the terminal's cursor there
	in.Focus()
	h := loadHistory(historyPath())
	return &model{
		c:        c,
		th:       th,
		input:    in,
		spin:     spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(th.accent))),
		hist:     h,
		browse:   len(h.entries),
		selected: -1,
		width:    80,
		print:    func(string) {},
	}
}

// Init asks the terminal for its background colour, which picks the
// theme, and prints the banner once it answers, or after a moment.
func (m *model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return bannerMsg{} }))
}

func (m *model) banner() tea.Cmd {
	if m.bannered {
		return nil
	}
	m.bannered = true
	th := m.th
	title := lipgloss.NewStyle().Bold(true).Foreground(th.accent).Render("luart")
	banner := []string{
		title + th.faint.Render("  "+m.describe()),
		th.faint.Render("/help for commands · tab completes · ctrl+d exits"),
		"",
	}
	return m.printLines(banner...)
}

// describe is the language, JIT and platform, as the banner and status say.
func (m *model) describe() string {
	jit := "JIT"
	if !jitCompiles(m.c) {
		jit = "interpreted"
	}
	return fmt.Sprintf("%s · %s · %s/%s", lua.VersionString, jit, runtime.GOOS, runtime.GOARCH)
}

// jitCompiles reports whether the state compiles: it has the JIT and the
// platform runs it.
func jitCompiles(c *cli) bool {
	return !c.noJIT && os.Getenv("LUART_JIT") != "off" && jitPlatform()
}

func jitPlatform() bool {
	return (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")
}

// printLines prints lines to the transcript, in order, off the event loop.
func (m *model) printLines(lines ...string) tea.Cmd {
	print := m.print
	return func() tea.Msg {
		for _, line := range lines {
			print(line)
		}
		return nil
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.BackgroundColorMsg:
		if !m.bannered {
			m.th = newTheme(msg.IsDark())
			m.spin.Style = lipgloss.NewStyle().Foreground(m.th.accent)
		}
		return m, m.banner()
	case bannerMsg:
		return m, m.banner()
	case evalDoneMsg:
		m.running = false
		m.last = msg.elapsed
		m.heap = heapBytes()
		m.hint = ""
		return m, nil
	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k != "ctrl+c" {
		m.quitting = false
	}
	if k != "tab" {
		m.candidates, m.selected = nil, -1
	}
	if m.running {
		switch k {
		case "ctrl+c", "esc":
			m.c.l.Interrupt()
			m.hint = "interrupting…"
			return m, nil
		case "enter":
			return m, nil
		}
		return m.edit(msg)
	}
	m.hint = ""
	switch k {
	case "enter":
		return m.submit()
	case "ctrl+c":
		if m.input.Value() != "" {
			m.input.Reset()
			m.browse = len(m.hist.entries)
			return m, nil
		}
		if m.quitting {
			return m, tea.Quit
		}
		m.quitting = true
		m.hint = "press ctrl+c again to exit"
		return m, nil
	case "ctrl+d":
		if m.input.Value() == "" {
			return m, tea.Quit
		}
	case "ctrl+l":
		return m, tea.ClearScreen
	case "tab":
		m.complete()
		return m, nil
	case "up":
		if m.input.Line() == 0 && m.browse > 0 {
			if m.browse == len(m.hist.entries) {
				m.draft = m.input.Value()
			}
			m.browse--
			m.input.SetValue(m.hist.entries[m.browse])
			return m, nil
		}
	case "down":
		if m.input.Line() == m.input.LineCount()-1 && m.browse < len(m.hist.entries) {
			m.browse++
			if m.browse == len(m.hist.entries) {
				m.input.SetValue(m.draft)
			} else {
				m.input.SetValue(m.hist.entries[m.browse])
			}
			return m, nil
		}
	}
	return m.edit(msg)
}

func (m *model) edit(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// complete puts in the next name that completes the one before the cursor,
// or the names' common prefix when that adds anything.
func (m *model) complete() {
	before := m.textBeforeCursor()
	if m.selected >= 0 && len(m.candidates) > 0 {
		// Cycle: replace the candidate put in last with the next.
		prev := m.candidates[m.selected]
		m.selected = (m.selected + 1) % len(m.candidates)
		m.replaceBeforeCursor(len(prev), m.candidates[m.selected])
		return
	}
	cands, n := completions(m.c.l, before)
	if len(cands) == 0 {
		return
	}
	typed := before[len(before)-n:]
	if len(cands) == 1 {
		m.replaceBeforeCursor(n, cands[0])
		return
	}
	if p := commonPrefix(cands); len(p) > len(typed) {
		m.replaceBeforeCursor(n, p)
		m.candidates = cands
		return
	}
	m.candidates, m.selected = cands, 0
	m.replaceBeforeCursor(n, cands[0])
}

// textBeforeCursor is the input's current line up to the cursor.
func (m *model) textBeforeCursor() string {
	lines := strings.Split(m.input.Value(), "\n")
	line := []rune(lines[min(m.input.Line(), len(lines)-1)])
	return string(line[:min(m.input.Column(), len(line))])
}

// replaceBeforeCursor replaces the n bytes before the cursor with s.
func (m *model) replaceBeforeCursor(n int, s string) {
	for range []rune(m.textBeforeCursor()[len(m.textBeforeCursor())-n:]) {
		m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m.input.InsertString(s)
}

// submit runs the input if it is a complete statement or a command, and
// otherwise starts a new line of it.
func (m *model) submit() (tea.Model, tea.Cmd) {
	code := m.input.Value()
	if strings.TrimSpace(code) == "" {
		return m, nil
	}
	if strings.HasPrefix(code, "/") && !strings.Contains(code, "\n") {
		m.hist.add(code)
		m.input.Reset()
		m.browse = len(m.hist.entries)
		return m, m.command(code)
	}
	if !isComplete(m.c.l, code) {
		m.input.InsertString("\n")
		return m, nil
	}
	m.hist.add(code)
	m.input.Reset()
	m.browse = len(m.hist.entries)
	return m, m.run(m.echo(code), func(l *lua.State) error { return compile(l, code) })
}

// echo is the input as the transcript shows it.
func (m *model) echo(code string) []string {
	lines := m.th.highlight(code)
	prompt := lipgloss.NewStyle().Foreground(m.th.accent).Bold(true)
	for i := range lines {
		if i == 0 {
			lines[i] = prompt.Render("❯ ") + lines[i]
		} else {
			lines[i] = m.th.faint.Render("· ") + lines[i]
		}
	}
	return lines
}

// run evaluates the function load pushes, off the event loop, printing
// echo, the output, and then the results or the error.
func (m *model) run(echo []string, load func(*lua.State) error) tea.Cmd {
	m.running = true
	m.started = time.Now()
	l, print, th, width, capture := m.c.l, m.print, m.th, m.width, m.c.capture
	eval := func() tea.Msg {
		for _, line := range echo {
			print(line)
		}
		start := time.Now()
		lines := evaluate(l, load, th, width)
		elapsed := time.Since(start)
		if capture != nil {
			capture.Sync()
		}
		for _, line := range lines {
			print(line)
		}
		print("")
		return evalDoneMsg{elapsed: elapsed}
	}
	return tea.Batch(eval, m.spin.Tick)
}

// evaluate loads and runs a chunk on l, returning the transcript lines for
// its results or its error.
func evaluate(l *lua.State, load func(*lua.State) error, th *theme, width int) []string {
	l.SetTop(0)
	if err := load(l); err != nil {
		msg, _ := l.ToString(-1)
		l.SetTop(0)
		return errorLines(th, msg)
	}
	l.PushGoFunction(traceback)
	l.Insert(1)
	err := l.ProtectedCall(0, lua.MultipleReturns, 1)
	if err != nil {
		msg, _ := l.ToString(-1)
		l.SetTop(0)
		return errorLines(th, msg)
	}
	f := &formatter{l: l, p: th.values, maxDepth: 4, maxItems: 40}
	var lines []string
	for i := 2; i <= l.Top(); i++ {
		lines = append(lines, strings.Split(f.format(i, width-2), "\n")...)
	}
	l.SetTop(0)
	return lines
}

// errorLines shows an error: the message in the error colour, the
// traceback under it, faint.
func errorLines(th *theme, msg string) []string {
	head, trace, _ := strings.Cut(msg, "\nstack traceback:")
	lines := []string{th.errText.Render("✗ " + head)}
	if trace != "" {
		for _, t := range strings.Split("stack traceback:"+trace, "\n") {
			lines = append(lines, th.faint.Render("  "+strings.TrimLeft(t, "\t")))
		}
	}
	return lines
}

// heapBytes is the Go heap in use, as the status line shows it.
func heapBytes() uint64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

func (m *model) View() tea.View {
	th := m.th
	border := th.accent
	if m.running {
		border = th.muted
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(max(m.width, 20))

	value := m.input.Value()
	var lines []string
	prompt := lipgloss.NewStyle().Foreground(th.accent).Bold(true).Render("❯ ")
	if value == "" {
		lines = []string{prompt + th.faint.Render("Lua, or /help")}
	} else {
		for i, line := range th.highlight(value) {
			if i == 0 {
				lines = append(lines, prompt+line)
			} else {
				lines = append(lines, th.faint.Render("· ")+line)
			}
		}
	}
	parts := []string{box.Render(strings.Join(lines, "\n"))}
	if len(m.candidates) > 0 {
		parts = append(parts, m.candidateLine())
	}
	parts = append(parts, m.status())

	// The cursor: after the border, the padding and the prompt.
	v := tea.NewView(strings.Join(parts, "\n"))
	row, col := m.input.Line(), m.input.Column()
	x := 0
	if text := strings.Split(value, "\n"); row < len(text) {
		r := []rune(text[row])
		x = lipgloss.Width(string(r[:min(col, len(r))]))
	}
	v.Cursor = tea.NewCursor(1+1+2+x, 1+row)
	return v
}

// candidateLine lists completions under the box, the one put in marked.
func (m *model) candidateLine() string {
	th := m.th
	var out []string
	width := 2
	for i, c := range m.candidates {
		s := th.faint.Render(c)
		if i == m.selected {
			s = lipgloss.NewStyle().Foreground(th.accent).Bold(true).Render(c)
		}
		if width+len(c)+2 > m.width-8 {
			out = append(out, th.faint.Render(fmt.Sprintf("+%d", len(m.candidates)-i)))
			break
		}
		width += len(c) + 2
		out = append(out, s)
	}
	return "  " + strings.Join(out, "  ")
}

// status is the line under the box: what runs, and how the last run went.
func (m *model) status() string {
	th := m.th
	if m.running {
		elapsed := time.Since(m.started).Round(100 * time.Millisecond)
		line := m.spin.View() + th.faint.Render(fmt.Sprintf(" running %s · esc to interrupt", elapsed))
		if m.hint != "" {
			line += th.faint.Render(" · " + m.hint)
		}
		return " " + line
	}
	parts := []string{m.describe()}
	if m.last > 0 {
		parts = append(parts, formatDuration(m.last))
	}
	if m.heap > 0 {
		parts = append(parts, fmt.Sprintf("%.1f MB heap", float64(m.heap)/(1<<20)))
	}
	line := th.faint.Render(" " + strings.Join(parts, " · "))
	if m.hint != "" {
		line += "  " + lipgloss.NewStyle().Foreground(th.accent).Render(m.hint)
	}
	return line
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%d µs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}
