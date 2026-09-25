package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// With LUART_CLI_MAIN=1 the test binary is the command, so the tests below
// run it as a user would.
func TestMain(m *testing.M) {
	if os.Getenv("LUART_CLI_MAIN") == "1" {
		os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr, false))
	}
	os.Exit(m.Run())
}

type result struct {
	stdout, stderr string
	status         int
}

// luart runs the command with args, stdin and extra environment.
func luart(t *testing.T, stdin string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LUA_INIT") { // an empty LUA_INIT_5_2 still hides LUA_INIT
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, append([]string{"LUART_CLI_MAIN=1"}, env...)...)
	cmd.Args[0] = "luart"
	cmd.Stdin = strings.NewReader(stdin)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	status := 0
	if exit, ok := err.(*exec.ExitError); ok {
		status = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{out.String(), errs.String(), status}
}

func TestCommandLine(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "script.lua")
	os.WriteFile(script, []byte(`print("script", arg[0], arg[-1] ~= nil, arg[1], arg[2], select("#", ...))`), 0o644)
	init := filepath.Join(dir, "init.lua")
	os.WriteFile(init, []byte(`initialised = "from file"`), 0o644)

	tests := []struct {
		name   string
		stdin  string
		env    []string
		args   []string
		stdout string // a substring of stdout, or "" to check nothing
		stderr string // a substring of stderr, or ""
		status int
	}{
		{"version", "", nil, []string{"-v"}, "Lua 5.2  Copyright (C) 1994-2015 Lua.org, PUC-Rio; luart", "", 0},
		{"execute", "", nil, []string{"-e", "print(1 + 1)"}, "2\n", "", 0},
		{"execute joined", "", nil, []string{"-eprint('joined')"}, "joined\n", "", 0},
		{"executes in order", "", nil, []string{"-e", "x = 2", "-e", "print(x * 3)"}, "6\n", "", 0},
		{"script and arg", "", nil, []string{script, "one", "two"}, "script\t" + script + "\ttrue\tone\ttwo\t2\n", "", 0},
		{"stdin script", `print("stdin", ...)`, nil, []string{"-", "a"}, "stdin\ta\n", "", 0},
		{"stdin without a tty", `print("piped")`, nil, nil, "piped\n", "", 0},
		{"error", "", nil, []string{"-e", "error('bad')"}, "", "luart: (command line):1: bad\nstack traceback:", 1},
		{"syntax error", "", nil, []string{"-e", "x = )"}, "", "luart: (command line):1: unexpected symbol near ')'", 1},
		{"error object", "", nil, []string{"-e", "error(setmetatable({}, {__tostring = function() return 'custom' end}))"}, "", "luart: custom", 1},
		{"missing script", "", nil, []string{filepath.Join(dir, "none.lua")}, "", "cannot open", 1},
		{"bad option", "", nil, []string{"-z"}, "", "luart: unrecognized option '-z'\nusage: luart [options]", 1},
		{"option needs argument", "", nil, []string{"-e"}, "", "luart: '-e' needs argument", 1},
		{"require", "", nil, []string{"-l", "string", "-e", "print(type(string.rep))"}, "function\n", "", 0},
		{"LUA_INIT", "", []string{"LUA_INIT=print('init')"}, []string{"-e", "print('after')"}, "init\nafter\n", "", 0},
		{"LUA_INIT file", "", []string{"LUA_INIT=@" + init}, []string{"-e", "print(initialised)"}, "from file\n", "", 0},
		{"LUA_INIT_5_2 first", "", []string{"LUA_INIT=print('no')", "LUA_INIT_5_2=print('yes')"}, []string{"-e", ""}, "yes\n", "", 0},
		{"-E ignores LUA_INIT", "", []string{"LUA_INIT=print('init')"}, []string{"-E", "-e", "print('after')"}, "after\n", "", 0},
		{"--", "", nil, []string{"--", script}, "script", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := luart(t, tt.stdin, tt.env, tt.args...)
			if r.status != tt.status {
				t.Errorf("status %d, want %d; stderr %q", r.status, tt.status, r.stderr)
			}
			if !strings.Contains(r.stdout, tt.stdout) {
				t.Errorf("stdout %q, want it to contain %q", r.stdout, tt.stdout)
			}
			if !strings.Contains(r.stderr, tt.stderr) {
				t.Errorf("stderr %q, want it to contain %q", r.stderr, tt.stderr)
			}
			if tt.name == "LUA_INIT" || tt.name == "-E ignores LUA_INIT" || tt.name == "LUA_INIT_5_2 first" {
				if r.stdout != tt.stdout {
					t.Errorf("stdout %q, want exactly %q", r.stdout, tt.stdout)
				}
			}
		})
	}
}

// The plain REPL, which -i gives when stdin is not a terminal, runs
// statements over several lines and prints expressions' values.
func TestPlainREPL(t *testing.T) {
	stdin := strings.Join([]string{
		"1 + 1",
		"x = 10 +",
		"5",
		"= x",
		"function double(n)",
		"  return n * 2",
		"end",
		"double(21)",
		"error('boom')",
		`_PROMPT = "lua$ "`,
		"'still running'",
	}, "\n") + "\n"
	r := luart(t, stdin, nil, "-i")
	for _, want := range []string{"> 2\n", ">> > 15\n", ">> >> > 42\n", "lua$ still running\n"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout %q, want it to contain %q", r.stdout, want)
		}
	}
	if !strings.Contains(r.stderr, "stdin:1: boom\nstack traceback:") || strings.Contains(r.stderr, "luart:") {
		t.Errorf("stderr %q, want the error without the program name", r.stderr)
	}
	if r.status != 0 {
		t.Errorf("status %d", r.status)
	}
}

// SIGINT interrupts a running script, as it does in lua.c.
func TestInterruptScript(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-e", "io.write('running\\n'); io.flush(); while true do end")
	cmd.Env = append(os.Environ(), "LUART_CLI_MAIN=1")
	var errs bytes.Buffer
	cmd.Stderr = &errs
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Signal only once the loop runs, and the handler is installed.
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "running\n" {
		t.Fatalf("read %q, %v", line, err)
	}
	cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("not interrupted")
	}
	if !strings.Contains(errs.String(), "interrupted!") {
		t.Fatalf("stderr %q, want interrupted!", errs.String())
	}
}
