package lua

import (
	"fmt"
	"strings"
)

// PushString pushes a string onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushstring
func (l *State) PushString(s string) string { // TODO is it useful to return the argument?
	l.apiPush(stringValue(s))
	return s
}

// PushFString pushes onto the stack a formatted string and returns that
// string.  It is similar to fmt.Sprintf, but has some differences: the
// conversion specifiers are quite restricted.  There are no flags, widths,
// or precisions.  The conversion specifiers can only be %% (inserts a %
// in the string), %s, %f (a Lua number), %p (a pointer as a hexadecimal
// numeral), %d and %c (an integer as a byte).
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushfstring
func (l *State) PushFString(format string, args ...any) string {
	n, i := 0, 0
	for {
		e := strings.IndexRune(format, '%')
		if e < 0 {
			break
		}
		l.checkStack(2) // format + item
		l.push(stringValue(format[:e]))
		switch format[e+1] {
		case 's':
			if args[i] == nil {
				l.push(stringValue("(null)"))
			} else {
				l.push(stringValue(args[i].(string)))
			}
			i++
		case 'c':
			l.push(stringValue(string(args[i].(rune))))
			i++
		case 'd':
			l.push(numberValue(float64(args[i].(int))))
			i++
		case 'f':
			l.push(numberValue(args[i].(float64)))
			i++
		case 'p':
			l.push(stringValue(fmt.Sprintf("%p", args[i])))
			i++
		case '%':
			l.push(stringValue("%"))
		default:
			l.runtimeError("invalid option " + format[e:e+2] + " to 'lua_pushfstring'")
		}
		n += 2
		format = format[e+2:]
	}
	l.checkStack(1)
	l.push(stringValue(format))
	if n > 0 {
		l.concat(n + 1)
	}
	s, _ := l.stack[l.top-1].str()
	return s
}

// PushGoClosure pushes a new Go closure onto the stack.
//
// When a Go function is created, it is possible to associate some values with
// it, thus creating a Go closure; these values are then accessible to the
// function whenever it is called.  To associate values with a Go function,
// first these values should be pushed onto the stack (when there are multiple
// values, the first value is pushed first).  Then PushGoClosure is called to
// create and push the Go function onto the stack, with the argument upValueCount
// telling how many values should be associated with the function.  Calling
// PushGoClosure also pops these values from the stack.
//
// When upValueCount is 0, this function creates a light Go function, which is just a
// Go function.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushcclosure
func (l *State) PushGoClosure(function Function, upValueCount uint8) {
	if upValueCount == 0 {
		l.apiPush(objectValue(&goFunction{Function: function}))
	} else {
		n := int(upValueCount)

		l.checkElementCount(n)
		cl := &goClosure{function: function, upValues: make([]value, upValueCount)}
		l.top -= n
		copy(cl.upValues, l.stack[l.top:l.top+n])
		l.apiPush(objectValue(cl))
	}
}

// PushGoFunction pushes a Function implemented in Go onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushcfunction
func (l *State) PushGoFunction(f Function) { l.PushGoClosure(f, 0) }

// PushThread pushes the thread l onto the stack.  It returns true if l is
// the main thread of its state.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushthread
func (l *State) PushThread() bool {
	l.apiPush(objectValue(l))
	return l.global.mainThread == l
}

// PushNil pushes a nil value onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushnil
func (l *State) PushNil() { l.apiPush(nilValue) }

// PushNumber pushes a number onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushnumber
func (l *State) PushNumber(n float64) { l.apiPush(numberValue(n)) }

// PushInteger pushes n onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushinteger
func (l *State) PushInteger(n int) { l.apiPush(numberValue(float64(n))) }

// PushUnsigned pushes n onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushunsigned
func (l *State) PushUnsigned(n uint) { l.apiPush(numberValue(float64(n))) }

// PushBoolean pushes a boolean value with value b onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushboolean
func (l *State) PushBoolean(b bool) { l.apiPush(boolValue(b)) }

// PushLightUserData pushes a light user data onto the stack. Userdata
// represents Go values in Lua. A light userdata is any Go value. Its
// equality matches the Go rules (http://golang.org/ref/spec#Comparison_operators).
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushlightuserdata
func (l *State) PushLightUserData(d any) { l.apiPush(l.valueOf(d)) }

// PushUserData is similar to PushLightUserData, but pushes a full userdata
// onto the stack.
func (l *State) PushUserData(d any) { l.apiPush(objectValue(&userData{data: d})) }

// PushGlobalTable pushes the global environment onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pushglobaltable
func (l *State) PushGlobalTable() { l.RawGetInt(RegistryIndex, RegistryIndexGlobals) }
