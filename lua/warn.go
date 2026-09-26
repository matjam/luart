package lua

import (
	"fmt"
	"io"
)

// A WarnFunction receives warnings, as lua_WarnFunction does: a message
// comes in pieces, each but the last with toBeContinued set.
//
// https://www.lua.org/manual/5.5/manual.html#lua_WarnFunction
type WarnFunction func(message string, toBeContinued bool)

// SetWarnFunction sets the function that receives warnings, or nil for
// none: warnings are then dropped.
//
// https://www.lua.org/manual/5.5/manual.html#lua_setwarnf
func (l *State) SetWarnFunction(f WarnFunction) { l.global.warn = f }

// Warning emits a warning, or a piece of one when toBeContinued is set.
//
// https://www.lua.org/manual/5.5/manual.html#lua_warning
func (l *State) Warning(message string, toBeContinued bool) {
	if f := l.global.warn; f != nil {
		f(message, toBeContinued)
	}
}

// warnError warns of the error value on top of the stack, raised in where,
// as lstate.c's luaE_warnerror does.
func (l *State) warnError(where string) {
	msg, ok := l.stack[l.top-1].str()
	if !ok {
		msg = "error object is not a string"
	}
	l.Warning("error in ", true)
	l.Warning(where, true)
	l.Warning(" (", true)
	l.Warning(msg, true)
	l.Warning(")", false)
}

// StderrWarnings returns lauxlib.c's warning function: off at first,
// "@on" and "@off" messages turn it on and off, and it writes warnings to
// w, standard error for NewState's states, each on a line of its own
// after "Lua warning: ".
func StderrWarnings(w io.Writer) WarnFunction {
	on, continuing := false, false
	return func(message string, toBeContinued bool) {
		if !continuing && !toBeContinued && len(message) > 0 && message[0] == '@' { // a control message
			switch message[1:] {
			case "off":
				on = false
			case "on":
				on = true
			}
			return
		}
		if !on {
			return
		}
		if !continuing {
			fmt.Fprint(w, "Lua warning: ")
		}
		fmt.Fprint(w, message)
		if continuing = toBeContinued; !continuing {
			fmt.Fprintln(w)
		}
	}
}
