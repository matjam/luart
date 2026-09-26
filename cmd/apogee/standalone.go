package main

// The command line, after Lua 5.2's lua.c: its options, LUA_INIT, the arg
// table, error reports with a traceback, and the plain REPL used when
// stdin or stdout is not a terminal.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

const progName = "apogee"

// A cli is one run of the command line.
type cli struct {
	l        *lua.State
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	progName string // "" in the REPL, where messages carry no prefix
	terminal bool   // stdin and stdout are terminals: the REPL is the TUI
	capture  *capture
	noJIT    bool // the state only interprets (/jit off)
}

// run runs the command line args, args[0] being the program name, and
// returns the exit status.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, terminal bool) int {
	c := &cli{stdin: stdin, stdout: stdout, stderr: stderr, progName: progName, terminal: terminal}
	if len(args) > 0 && args[0] != "" {
		c.progName = args[0]
	}
	script, interactive, version, execute, noEnv := collectArgs(args)
	if script < 0 {
		c.usage(args[-script])
		return 1
	}
	if terminal {
		// Output from Lua must reach the TUI's transcript, so it goes
		// through a pipe from the start, and so does ours, to keep order.
		if cp, err := startCapture(); err == nil {
			c.capture = cp
			c.stdout, c.stderr = os.Stdout, os.Stderr
			defer cp.stop()
		}
	}
	if version {
		c.printVersion()
	}
	c.l = newState()
	defer c.l.Close() // runs pending finalizers and flushes files, as lua.c's lua_close
	if !noEnv && !c.report(c.handleInit()) {
		return 1
	}
	end := len(args)
	if script > 0 {
		end = script
	}
	if !c.runArgs(args, end) {
		return 1
	}
	if script > 0 && !c.report(c.handleScript(args, script)) {
		return 1
	}
	switch {
	case interactive:
		c.repl()
	case script == 0 && !execute && !version:
		if c.terminal || isTerminal(stdin) {
			if !c.terminal {
				c.printVersion()
			}
			c.repl()
		} else if !c.report(c.doFile("")) {
			return 1
		}
	}
	return 0
}

// newState makes a state with the standard libraries.
func newState(opts ...lua.Option) *lua.State {
	l := lua.NewState(opts...)
	stdlib.Open(l)
	return l
}

// collectArgs scans args as lua.c's collectargs does. script is the index
// of the script, 0 for none, or minus the index of a bad option.
func collectArgs(args []string) (script int, interactive, version, execute, noEnv bool) {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "" || a[0] != '-' {
			return i, interactive, version, execute, noEnv
		}
		if len(a) == 1 { // "-": the script is stdin
			return i, interactive, version, execute, noEnv
		}
		switch a[1] {
		case '-':
			if len(a) != 2 {
				return -i, interactive, version, execute, noEnv
			}
			if i+1 < len(args) {
				return i + 1, interactive, version, execute, noEnv
			}
			return 0, interactive, version, execute, noEnv
		case 'E':
			if len(a) != 2 {
				return -i, interactive, version, execute, noEnv
			}
			noEnv = true
		case 'W':
			if len(a) != 2 {
				return -i, interactive, version, execute, noEnv
			}
		case 'i', 'v':
			if len(a) != 2 {
				return -i, interactive, version, execute, noEnv
			}
			interactive = interactive || a[1] == 'i'
			version = true
		case 'e', 'l':
			execute = execute || a[1] == 'e'
			if len(a) == 2 {
				if i++; i >= len(args) || strings.HasPrefix(args[i], "-") {
					return -(i - 1), interactive, version, execute, noEnv
				}
			}
		default:
			return -i, interactive, version, execute, noEnv
		}
	}
	return 0, interactive, version, execute, noEnv
}

func (c *cli) usage(bad string) {
	if bad[1] == 'e' || bad[1] == 'l' {
		fmt.Fprintf(c.stderr, "%s: '%s' needs argument\n", c.progName, bad)
	} else {
		fmt.Fprintf(c.stderr, "%s: unrecognized option '%s'\n", c.progName, bad)
	}
	fmt.Fprintf(c.stderr, `usage: %s [options] [script [args]]
Available options are:
  -e stat   execute string 'stat'
  -i        enter interactive mode after executing 'script'
  -l mod    require library 'mod' into global 'mod'
  -l g=mod  require library 'mod' into global 'g'
  -v        show version information
  -E        ignore environment variables
  -W        turn warnings on
  --        stop handling options
  -         stop handling options and execute stdin
`, c.progName)
}

func (c *cli) printVersion() {
	fmt.Fprintln(c.stdout, versionLine())
}

// message writes msg to stderr, after the program name outside the REPL.
func (c *cli) message(msg string) {
	if c.progName != "" {
		fmt.Fprintf(c.stderr, "%s: ", c.progName)
	}
	fmt.Fprintln(c.stderr, msg)
}

// report writes the error message on the top of the stack if err is not
// nil, and reports whether err is nil.
func (c *cli) report(err error) bool {
	if err != nil && !c.l.IsNil(-1) {
		msg, ok := c.l.ToString(-1)
		if !ok {
			msg = "(error object is not a string)"
		}
		c.message(msg)
		c.l.Pop(1)
	}
	return err == nil
}

// traceback is the message handler docall runs errors through: it adds a
// traceback to a string message, and turns another error object into its
// __tostring, as lua.c's does.
func traceback(l *lua.State) int {
	if msg, ok := l.ToString(1); ok {
		l.Traceback(l, msg, 1)
	} else if !l.IsNoneOrNil(1) && !l.CallMeta(1, "__tostring") {
		l.PushString("(no error message)")
	}
	return 1
}

// docall calls the function below its argCount arguments with traceback
// as its message handler. SIGINT interrupts it, as it does lua.c.
func (c *cli) docall(argCount, resultCount int) error {
	l := c.l
	base := l.Top() - argCount
	l.PushGoFunction(traceback)
	l.Insert(base)
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigint:
			l.Interrupt()
		case <-done:
		}
	}()
	err := l.ProtectedCall(argCount, resultCount, base)
	close(done)
	signal.Stop(sigint)
	l.Remove(base)
	return err
}

func (c *cli) doFile(name string) error {
	if err := c.l.LoadFile(name, ""); err != nil {
		return err
	}
	return c.docall(0, 0)
}

func (c *cli) doString(s, name string) error {
	if err := c.l.LoadBuffer(s, name, ""); err != nil {
		return err
	}
	return c.docall(0, 0)
}

func (c *cli) doLibrary(name string) error {
	// As lua.c's dolibrary: -l g=mod sets g to require("mod"); -l mod
	// sets mod, less any suffix from a '-'.
	global, module, ok := strings.Cut(name, "=")
	if !ok {
		module = name
		global, _, _ = strings.Cut(name, "-")
	}
	c.l.Global("require")
	c.l.PushString(module)
	if err := c.docall(1, 1); err != nil {
		return err
	}
	c.l.SetGlobal(global)
	return nil
}

// handleInit runs LUA_INIT_5_5, or LUA_INIT: a file after @, else code.
func (c *cli) handleInit() error {
	name := "LUA_INIT_5_5"
	init, ok := os.LookupEnv(name)
	if !ok {
		name = "LUA_INIT"
		if init, ok = os.LookupEnv(name); !ok {
			return nil
		}
	}
	if file, ok := strings.CutPrefix(init, "@"); ok {
		return c.doFile(file)
	}
	return c.doString(init, "="+name)
}

// runArgs runs the -e and -l options in args[1:end].
func (c *cli) runArgs(args []string, end int) bool {
	for i := 1; i < end; i++ {
		a := args[i]
		if a == "-W" {
			c.l.Warning("@on", false) // turn warnings on
			continue
		}
		if len(a) < 2 || a[0] != '-' || (a[1] != 'e' && a[1] != 'l') {
			continue
		}
		value := a[2:]
		if value == "" {
			i++
			value = args[i]
		}
		var err error
		if a[1] == 'e' {
			err = c.doString(value, "=(command line)")
		} else {
			err = c.doLibrary(value)
		}
		if !c.report(err) {
			return false
		}
	}
	return true
}

// handleScript sets arg and runs the script at args[script] with the
// arguments after it.
func (c *cli) handleScript(args []string, script int) error {
	l := c.l
	l.CreateTable(len(args)-script-1, script+1)
	for i, a := range args {
		l.PushString(a)
		l.RawSetInt(-2, i-script)
	}
	l.SetGlobal("arg")
	name := args[script]
	if name == "-" && args[script-1] != "--" {
		name = "" // stdin
	}
	if err := l.LoadFile(name, ""); err != nil {
		return err
	}
	rest := args[script+1:]
	for _, a := range rest {
		l.PushString(a)
	}
	return c.docall(len(rest), lua.MultipleReturns)
}

// repl runs the TUI on a terminal, and otherwise the plain REPL.
func (c *cli) repl() {
	if c.terminal {
		if err := runTUI(c); err != nil {
			c.message(err.Error())
		}
		return
	}
	c.plainREPL()
}

// plainREPL is lua.c's dotty: read a statement, which may take several
// lines, run it, and print its results. An expression's value is printed
// as Lua 5.3 prints it, by trying the line as "return <line>" first.
func (c *cli) plainREPL() {
	saved := c.progName
	c.progName = ""
	in := bufio.NewReader(c.stdin)
	for {
		err, ok := c.loadLine(in)
		if !ok {
			break
		}
		if err == nil {
			err = c.docall(0, lua.MultipleReturns)
		}
		c.report(err)
		if err == nil && c.l.Top() > 0 {
			c.l.Global("print")
			c.l.Insert(1)
			if err := c.l.ProtectedCall(c.l.Top()-1, 0, 0); err != nil {
				msg, _ := c.l.ToString(-1)
				c.message(fmt.Sprintf("error calling 'print' (%s)", msg))
			}
		}
		c.l.SetTop(0)
	}
	fmt.Fprintln(c.stdout)
	c.progName = saved
}

// loadLine reads and compiles one statement, prompting for more lines while
// it is incomplete. ok is false at the end of input.
func (c *cli) loadLine(in *bufio.Reader) (err error, ok bool) {
	c.l.SetTop(0)
	line, ok := c.readLine(in, true)
	if !ok {
		return nil, false
	}
	code := line
	for {
		err := compile(c.l, code)
		if !incomplete(c.l, err) {
			return err, true // the function, or the error, is on the stack
		}
		more, ok := c.readLine(in, false)
		if !ok {
			return err, true
		}
		c.l.Pop(1)
		code += "\n" + more
	}
}

func (c *cli) readLine(in *bufio.Reader, first bool) (string, bool) {
	c.prompt(first)
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), true
}

// prompt writes _PROMPT or _PROMPT2 if the script set them, as lua.c does.
func (c *cli) prompt(first bool) {
	name, prompt := "_PROMPT2", ">> "
	if first {
		name, prompt = "_PROMPT", "> "
	}
	c.l.Global(name)
	if s, ok := c.l.ToString(-1); ok {
		prompt = s
	}
	c.l.Pop(1)
	fmt.Fprint(c.stdout, prompt)
}
