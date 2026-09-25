package stdlib

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/matjam/luart/lua"
)

func field(l *lua.State, key string, def int) int {
	l.Field(-1, key)
	r, ok := l.ToInteger(-1)
	if !ok {
		if def < 0 {
			l.Errorf("field '%s' missing in date table", key)
		}
		r = def
	}
	l.Pop(1)
	return r
}

var osLibrary = []lua.RegistryFunction{
	{Name: "clock", Function: clock},
	{Name: "date", Function: osDate},
	{Name: "difftime", Function: func(l *lua.State) int {
		l.PushNumber(time.Unix(int64(l.CheckNumber(1)), 0).Sub(time.Unix(int64(l.OptNumber(2, 0)), 0)).Seconds())
		return 1
	}},

	// From the Lua manual:
	// "This function is equivalent to the ISO C function system"
	// https://www.lua.org/manual/5.2/manual.html#pdf-os.execute
	{Name: "execute", Function: func(l *lua.State) int {
		c := l.OptString(1, "")

		if c == "" {
			// Check whether "sh" is available on the system.
			err := exec.Command("sh").Run()
			l.PushBoolean(err == nil)
			return 1
		}

		terminatedSuccessfully := true
		terminationReason := "exit"
		terminationData := 0

		// Create the command.
		cmd := exec.Command("sh", "-c", c)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Run the command.
		if err := cmd.Run(); err != nil {
			terminatedSuccessfully = false
			terminationReason = "exit"
			terminationData = 1

			if exiterr, ok := err.(*exec.ExitError); ok {
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					if status.Signaled() {
						terminationReason = "signal"
						terminationData = int(status.Signal())
					} else {
						terminationData = status.ExitStatus()
					}
				} else {
					// Unsupported system?
				}
			} else {
				// From man 3 system:
				// "If a child process could not be created, or its
				// status could not be retrieved, the return value
				// is -1."
				terminationData = -1
			}
		}

		// Deal with the return values.
		if terminatedSuccessfully {
			l.PushBoolean(true)
		} else {
			l.PushNil()
		}

		l.PushString(terminationReason)
		l.PushInteger(terminationData)

		return 3
	}},
	{Name: "exit", Function: func(l *lua.State) int {
		var status int
		if l.IsBoolean(1) {
			if !l.ToBoolean(1) {
				status = 1
			}
		} else {
			status = l.OptInteger(1, status)
		}
		// if l.ToBoolean(2) {
		// 	Close(l)
		// }
		os.Exit(status)
		panic("unreachable")
	}},
	{Name: "getenv", Function: func(l *lua.State) int { l.PushString(os.Getenv(l.CheckString(1))); return 1 }},
	{Name: "remove", Function: func(l *lua.State) int { name := l.CheckString(1); return l.FileResult(os.Remove(name), name) }},
	{Name: "rename", Function: func(l *lua.State) int { return l.FileResult(os.Rename(l.CheckString(1), l.CheckString(2)), "") }},
	{Name: "setlocale", Function: osSetlocale},
	{Name: "time", Function: func(l *lua.State) int {
		if l.IsNoneOrNil(1) {
			l.PushNumber(float64(time.Now().Unix()))
		} else {
			l.CheckType(1, lua.TypeTable)
			l.SetTop(1)
			// In loslib.c's order, which decides which missing field an
			// error names. Out-of-range fields normalise, as with mktime;
			// isdst is not used: Go resolves the offset from the zone.
			sec := field(l, "sec", 0)
			min := field(l, "min", 0)
			hour := field(l, "hour", 12)
			day := field(l, "day", -1)
			month := field(l, "month", -1)
			year := field(l, "year", -1)
			l.PushNumber(float64(time.Date(year, time.Month(month), day, hour, min, sec, 0, time.Local).Unix()))
		}
		return 1
	}},
	{Name: "tmpname", Function: func(l *lua.State) int {
		f, err := os.CreateTemp("", "lua_")
		if err != nil {
			l.Errorf("unable to generate a unique filename")
		}
		defer f.Close()
		l.PushString(f.Name())
		return 1
	}},
}

// OpenOS opens the os library. Usually passed to Require.
func OpenOS(l *lua.State) int {
	l.NewLibrary(osLibrary)
	return 1
}
