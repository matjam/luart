// Command apogee runs Lua 5.5 on apogee. It takes lua.c's options, and on a
// terminal its REPL highlights and completes code, prints values as
// trees, and can be interrupted.
//
//	go install github.com/matjam/apogee/cmd/apogee@latest
//	apogee [options] [script [args]]
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/charmbracelet/x/term"
	"github.com/matjam/apogee/lua"
)

func main() {
	terminal := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr, terminal))
}

// isTerminal reports whether f is a terminal.
func isTerminal(f any) bool {
	file, ok := f.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

// apogeeVersion is the version of the apogee module this binary was built
// with, or "(devel)".
func apogeeVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, m := range info.Deps {
			if m.Path == "github.com/matjam/apogee" {
				if m.Replace != nil {
					return "(devel)"
				}
				return m.Version
			}
		}
	}
	return "(devel)"
}

// versionLine is what -v prints.
func versionLine() string {
	return fmt.Sprintf("%s  Copyright (C) 1994-2026 Lua.org, PUC-Rio; apogee %s %s/%s",
		lua.VersionString, apogeeVersion(), runtime.GOOS, runtime.GOARCH)
}
