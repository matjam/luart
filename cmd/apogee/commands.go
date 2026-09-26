package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/matjam/apogee/lua"
)

// commandHelp is what /help prints, a command a line.
var commandHelp = [][2]string{
	{"/help", "show this"},
	{"/load file.lua", "run a file in this state"},
	{"/reset", "start again with a new state"},
	{"/jit on|off", "start again with a new state that compiles, or only interprets"},
	{"/clear", "clear the screen"},
	{"/quit", "leave (or ctrl+d)"},
}

var keyHelp = [][2]string{
	{"enter", "run, or a new line if the statement is unfinished"},
	{"alt+enter", "a new line"},
	{"tab", "complete a name; again for the next"},
	{"up / down", "history, from the first or last line"},
	{"esc / ctrl+c", "interrupt a running evaluation; ctrl+c also clears the input"},
}

// command runs a slash command line.
func (m *model) command(line string) tea.Cmd {
	name, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)
	th := m.th
	echo := []string{th.faint.Render("❯ ") + th.text.Render(line)}
	switch name {
	case "/help":
		lines := echo
		for _, h := range [][][2]string{commandHelp, keyHelp} {
			for _, e := range h {
				lines = append(lines, "  "+lipgloss.NewStyle().Foreground(th.accent).Render(e[0])+th.faint.Render(strings.Repeat(" ", max(1, 16-len(e[0])))+e[1]))
			}
			lines = append(lines, "")
		}
		return m.printLines(lines...)
	case "/load":
		if arg == "" {
			return m.printLines(append(echo, errorLines(th, "/load needs a file name")...)...)
		}
		return m.run(echo, func(l *lua.State) error { return l.LoadFile(arg, "") })
	case "/reset":
		return m.restart(echo, m.c.noJIT)
	case "/jit":
		switch arg {
		case "on":
			return m.restart(echo, false)
		case "off":
			return m.restart(echo, true)
		}
		return m.printLines(append(echo, errorLines(th, "/jit takes on or off")...)...)
	case "/clear":
		return tea.ClearScreen
	case "/quit", "/exit":
		return tea.Quit
	}
	return m.printLines(append(echo, errorLines(th, "unknown command "+name+"; /help lists them")...)...)
}

// restart replaces the state with a new one.
func (m *model) restart(echo []string, noJIT bool) tea.Cmd {
	m.c.noJIT = noJIT
	var opts []lua.Option
	if noJIT {
		opts = append(opts, lua.WithoutJIT())
	}
	m.c.l = newState(opts...)
	return m.printLines(append(echo, m.th.faint.Render("  new state: "+m.describe()), "")...)
}
