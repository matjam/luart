// Command luart runs Lua 5.2 on luart. It takes lua.c's options, and on a
// terminal its REPL highlights and completes code, prints values as
// trees, and can be interrupted.
//
//	go install github.com/matjam/luart/cmd/luart@latest
//	luart [options] [script [args]]
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/charmbracelet/x/term"
	"github.com/matjam/luart/lua"
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

// luartVersion is the version of the luart module this binary was built
// with, or "(devel)".
func luartVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, m := range info.Deps {
			if m.Path == "github.com/matjam/luart" {
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
	return fmt.Sprintf("%s  Copyright (C) 1994-2026 Lua.org, PUC-Rio; luart %s %s/%s",
		lua.VersionString, luartVersion(), runtime.GOOS, runtime.GOARCH)
}
