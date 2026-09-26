package stdlib

import (
	"math"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/matjam/apogee/lua"
)

// field is loslib.c's getfield: the integer field key of the table on top
// of the stack, less delta, which must fit a C int as struct tm's fields
// do; def when it is absent, or an error when def < 0.
func field(l *lua.State, key string, def, delta int) int {
	t := l.Field(-1, key)
	r, ok := l.ToInteger(-1)
	if !ok {
		if t != lua.TypeNil {
			l.Errorf("field '%s' is not an integer", key)
		} else if def < 0 {
			l.Errorf("field '%s' missing in date table", key)
		}
		r = int64(def)
	} else {
		if r >= 0 && r-int64(delta) > math.MaxInt32 || r < 0 && r < math.MinInt32+int64(delta) {
			l.Errorf("field '%s' is out-of-bound", key)
		}
		r -= int64(delta)
	}
	l.Pop(1)
	return int(r)
}

// setDateFields is loslib.c's setallfields: t's fields, into the table on
// top of the stack.
func setDateFields(l *lua.State, t time.Time) {
	for _, f := range []struct {
		name  string
		value int
	}{
		{"year", t.Year()}, {"month", int(t.Month())}, {"day", t.Day()},
		{"hour", t.Hour()}, {"min", t.Minute()}, {"sec", t.Second()},
		{"yday", t.YearDay()}, {"wday", int(t.Weekday()) + 1},
	} {
		l.PushInteger(f.value)
		l.SetField(-2, f.name)
	}
	l.PushBoolean(t.IsDST())
	l.SetField(-2, "isdst")
}

// representable reports whether t's year fits struct tm's int tm_year.
func representable(t time.Time) bool {
	y := int64(t.Year()) - 1900
	return math.MinInt32 <= y && y <= math.MaxInt32
}

// shellCommand runs c with the shell, as C's system and popen do.
func shellCommand(c string) *exec.Cmd { return exec.Command("sh", "-c", c) }

// execResult returns a finished command's results, as lauxlib.c's
// luaL_execresult does: true or nil, then "exit" and the exit status, or
// "signal" and the signal that ended it.
func execResult(l *lua.State, err error) int {
	reason, status := "exit", 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); !ok {
			status = -1 // not run, or no status, as system() reports it
		} else if ws, ok := e.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			reason, status = "signal", int(ws.Signal())
		} else {
			status = e.ExitCode()
		}
	}
	if err == nil {
		l.PushBoolean(true)
	} else {
		l.PushNil()
	}
	l.PushString(reason)
	l.PushInteger(status)
	return 3
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

		cmd := shellCommand(c)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return execResult(l, cmd.Run())
	}},
	{Name: "exit", Function: func(l *lua.State) int {
		var status int
		if l.IsBoolean(1) {
			if !l.ToBoolean(1) {
				status = 1
			}
		} else {
			status = optInt(l, 1, status)
		}
		if l.ToBoolean(2) {
			l.Close()
		}
		flushAll() // as C's exit flushes every FILE
		os.Exit(status)
		panic("unreachable")
	}},
	{Name: "getenv", Function: func(l *lua.State) int { l.PushString(os.Getenv(l.CheckString(1))); return 1 }},
	{Name: "remove", Function: func(l *lua.State) int { name := l.CheckString(1); return l.FileResult(os.Remove(name), name) }},
	{Name: "rename", Function: func(l *lua.State) int { return l.FileResult(os.Rename(l.CheckString(1), l.CheckString(2)), "") }},
	{Name: "setlocale", Function: osSetlocale},
	{Name: "time", Function: func(l *lua.State) int {
		if l.IsNoneOrNil(1) {
			l.PushInteger(time.Now().Unix())
		} else {
			l.CheckType(1, lua.TypeTable)
			l.SetTop(1)
			// In loslib.c's order, which decides which missing field an
			// error names. Out-of-range fields normalise, as with mktime,
			// and the table gets the normalised fields; isdst is not used:
			// Go resolves the offset from the zone.
			year := field(l, "year", -1, 1900)
			month := field(l, "month", -1, 1)
			day := field(l, "day", -1, 0)
			hour := field(l, "hour", 12, 0)
			min := field(l, "min", 0, 0)
			sec := field(l, "sec", 0, 0)
			t := time.Date(year+1900, time.Month(month+1), day, hour, min, sec, 0, time.Local)
			if !representable(t) {
				l.Errorf("time result cannot be represented in this installation")
			}
			setDateFields(l, t)
			l.PushInteger(t.Unix())
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
