package lua

import (
	"strings"

	"github.com/matjam/luart/internal/bytecode"
	"github.com/matjam/luart/internal/compiler"
)

// A Frame identifies an activation record. It is returned by State.Frame and
// passed to State.Info; its zero value identifies none.
type Frame struct{ ci *callInfo }

func (l *State) resetHookCount() { l.hookCount = l.baseHookCount }

func (l *State) prototype(ci *callInfo) *prototype {
	return l.stack[ci.function].luaClosure().prototype
}

func (l *State) currentLine(ci *callInfo) int {
	return int(l.prototype(ci).LineInfo[ci.savedPC-1])
}

// SetHook sets the debugging hook function.
//
// f is the hook function. mask specifies on which events the hook will be
// called: it is formed by a bitwise or of the constants MaskCall, MaskReturn,
// MaskLine, and MaskCount. The count argument is only meaningful when the
// mask includes MaskCount. For each event, the hook is called as explained
// below:
//
// Call hook is called when the interpreter calls a function. The hook is
// called just after Lua enters the new function, before the function gets
// its arguments.
//
// Return hook is called when the interpreter returns from a function. The
// hook is called just before Lua leaves the function. There is no standard
// way to access the values to be returned by the function.
//
// Line hook is called when the interpreter is about to start the execution
// of a new line of code, or when it jumps back in the code (even to the same
// line). (This event only happens while Lua is executing a Lua function.)
//
// Count hook is called after the interpreter executes every count
// instructions. (This event only happens while Lua is executing a Lua
// function.)
//
// A hook is disabled by setting mask to zero.
func (l *State) SetHook(f Hook, mask byte, count int) {
	if f == nil || mask == 0 {
		f, mask = nil, 0
	}
	if ci := l.callInfo; ci.isLua() {
		l.oldPC = ci.savedPC
	}
	l.hooker, l.baseHookCount = f, count
	l.resetHookCount()
	l.hookMask = mask
}

// Hook returns the current hook function.
func (l *State) Hook() Hook { return l.hooker }

// HookMask returns the current hook mask.
func (l *State) HookMask() byte { return l.hookMask }

// HookCount returns the hook count SetHook set.
//
// http://www.lua.org/manual/5.2/manual.html#lua_gethookcount
func (l *State) HookCount() int { return l.baseHookCount }

// Frame gets information about the interpreter runtime stack.
//
// It returns a Frame identifying the activation record of the
// function executing at a given level. Level 0 is the current running
// function, whereas level n+1 is the function that has called level n (except
// for tail calls, which do not count on the stack). When there are no errors,
// Stack returns true; when called with a level greater than the stack depth,
// it returns false.
func (l *State) Frame(level int) (f Frame, ok bool) {
	if level < 0 {
		return // invalid (negative) level
	}
	callInfo := l.callInfo
	for ; level > 0 && callInfo != &l.baseCallInfo; level, callInfo = level-1, callInfo.previous {
	}
	if level == 0 && callInfo != &l.baseCallInfo { // level found?
		f, ok = Frame{callInfo}, true
	}
	return
}

func (l *State) functionInfo(p Debug, f closure) (d Debug) {
	d = p
	if lc, ok := f.(*luaClosure); !ok {
		d.Source = "=[" + l.global.goName + "]"
		d.LineDefined, d.LastLineDefined = -1, -1
		d.What = l.global.goName
	} else {
		p := lc.prototype
		d.Source = p.Source // "=?" for a stripped binary chunk; see internal/chunk
		d.LineDefined, d.LastLineDefined = p.LineDefined, p.LastLineDefined
		d.What = "Lua"
		if d.LineDefined == 0 {
			d.What = "main"
		}
	}
	d.ShortSource = compiler.ChunkID(d.Source)
	return
}

func (l *State) functionName(ci *callInfo) (name, kind string) {
	if ci == &l.baseCallInfo {
		return
	}
	var tm tm
	p := l.prototype(ci)
	pc := ci.savedPC - 1 // the calling instruction
	switch i := p.Code[pc]; i.OpCode() {
	case bytecode.OpCall, bytecode.OpTailCall:
		return p.objectName(i.A(), pc)
	case bytecode.OpTForCall:
		return "for iterator", "for iterator"
	case bytecode.OpSelf, bytecode.OpGetTableUp, bytecode.OpGetTable:
		tm = tmIndex
	case bytecode.OpSetTableUp, bytecode.OpSetTable:
		tm = tmNewIndex
	case bytecode.OpEqual:
		tm = tmEq
	case bytecode.OpAdd:
		tm = tmAdd
	case bytecode.OpSub:
		tm = tmSub
	case bytecode.OpMul:
		tm = tmMul
	case bytecode.OpDiv:
		tm = tmDiv
	case bytecode.OpMod:
		tm = tmMod
	case bytecode.OpPow:
		tm = tmPow
	case bytecode.OpUnaryMinus:
		tm = tmUnaryMinus
	case bytecode.OpLength:
		tm = tmLen
	case bytecode.OpLessThan:
		tm = tmLT
	case bytecode.OpLessOrEqual:
		tm = tmLE
	case bytecode.OpConcat:
		tm = tmConcat
	default:
		return
	}
	return eventNames[tm], "metamethod"
}

// findLocal is ldebug.c's findlocal: the name and stack index of local n
// of the function running in ci, or "" if there is none. Negative n are
// the varargs of a Lua function; slots without a name are temporaries.
func (l *State) findLocal(ci *callInfo, n int) (string, int) {
	var name string
	var base int
	if ci.isLua() {
		if n < 0 {
			return l.findVarArg(ci, -n)
		}
		base = ci.base()
		name, _ = l.prototype(ci).localName(n, ci.savedPC-1)
	} else {
		base = ci.function + 1
	}
	if name == "" {
		limit := l.top
		if ci != l.callInfo {
			limit = ci.next.function
		}
		if n <= 0 || limit-base < n { // outside the function's slots
			return "", 0
		}
		name = "(*temporary)"
	}
	return name, base + n - 1
}

// findVarArg is ldebug.c's findvararg: vararg n of the Lua function in ci.
func (l *State) findVarArg(ci *callInfo, n int) (string, int) {
	parameters := l.prototype(ci).ParameterCount
	if n >= ci.base()-ci.function-parameters { // no such vararg
		return "", 0
	}
	return "(*vararg)", ci.function + parameters + n
}

// Local gets a local variable of the function running in frame: it pushes
// the variable's value and returns its name. Parameter 1 is the first
// local, then the other locals in the order they are declared, while they
// are active. Negative n are the varargs, named "(*vararg)", and the
// function's other slots are named "(*temporary)".
//
// With the zero Frame, Local instead returns the name of parameter n of
// the Lua function on the top of the stack, and pushes nothing.
//
// Local returns false, pushing nothing, if there is no such variable.
//
// http://www.lua.org/manual/5.2/manual.html#lua_getlocal
func (l *State) Local(frame Frame, n int) (string, bool) {
	if frame.ci == nil {
		if c := l.stack[l.top-1].luaClosure(); c != nil {
			return c.prototype.localName(n, 0) // the live variables at the start
		}
		return "", false
	}
	name, pos := l.findLocal(frame.ci, n)
	if name == "" {
		return "", false
	}
	l.apiPush(l.stack[pos])
	return name, true
}

// SetLocal sets a local variable of the function running in frame, as
// Local numbers them, to the value on the top of the stack, and returns
// its name. It pops the value, and returns false if there is no such
// variable.
//
// http://www.lua.org/manual/5.2/manual.html#lua_setlocal
func (l *State) SetLocal(frame Frame, n int) (string, bool) {
	l.checkElementCount(1)
	name, pos := l.findLocal(frame.ci, n)
	if name != "" {
		l.stack[pos] = l.stack[l.top-1]
	}
	l.top--
	return name, name != ""
}

func (l *State) collectValidLines(f closure) {
	if lc, ok := f.(*luaClosure); !ok {
		l.apiPush(nilValue)
	} else {
		t := newTable()
		l.apiPush(objectValue(t))
		for _, i := range lc.prototype.LineInfo {
			t.putAtInt(int(i), trueValue)
		}
	}
}

// Info gets information about a specific function or function invocation.
//
// To get information about a function invocation, the parameter where must
// be a valid activation record that was filled by a previous call to Stack
// or given as an argument to a hook (see Hook).
//
// To get information about a function you push it onto the stack and start
// the what string with the character '>'. (In that case, Info pops the
// function from the top of the stack.) For instance, to know in which line
// a function f was defined, you can write the following code:
//
//	l.Global("f") // Get global 'f'.
//	d, _ := l.Info(">S", Frame{})
//	fmt.Printf("%d\n", d.LineDefined)
//
// Each character in the string what selects some fields of the Debug struct
// to be filled or a value to be pushed on the stack:
//
//	'n': fills in the field Name and NameKind
//	'S': fills in the fields Source, ShortSource, LineDefined, LastLineDefined, and What
//	'l': fills in the field CurrentLine
//	't': fills in the field IsTailCall
//	'u': fills in the fields UpValueCount, ParameterCount, and IsVarArg
//	'f': pushes onto the stack the function that is running at the given level
//	'L': pushes onto the stack a table whose indices are the numbers of the lines that are valid on the function
//
// (A valid line is a line with some associated code, that is, a line where you
// can put a break point. Non-valid lines include empty lines and comments.)
//
// This function returns false on error (for instance, an invalid option in what).
func (l *State) Info(what string, frame Frame) (d Debug, ok bool) {
	where := frame.ci
	var f closure
	var fun value
	if strings.HasPrefix(what, ">") {
		where = nil
		fun = l.stack[l.top-1]
		if !fun.isFunction() {
			panic("function expected")
		}
		f = fun.closure()
		what = what[1:] // skip the '>'
		l.top--         // pop function
	} else {
		fun = l.stack[where.function]
		l.assert(fun.isFunction())
		f = fun.closure()
	}
	ok, hasL, hasF := true, false, false
	d.callInfo = where
	ci := d.callInfo
	for _, r := range what {
		switch r {
		case 'S':
			d = l.functionInfo(d, f)
		case 'l':
			d.CurrentLine = -1
			if where != nil && ci.isLua() {
				d.CurrentLine = l.currentLine(where)
			}
		case 'u':
			if f == nil {
				d.UpValueCount = 0
			} else {
				d.UpValueCount = f.upValueCount()
			}
			if lf, ok := f.(*luaClosure); !ok {
				d.IsVarArg = true
				d.ParameterCount = 0
			} else {
				d.IsVarArg = lf.prototype.IsVarArg
				d.ParameterCount = lf.prototype.ParameterCount
			}
		case 't':
			d.IsTailCall = where != nil && ci.isCallStatus(callStatusTail)
		case 'n':
			// calling function is a known Lua function?
			if where != nil && !ci.isCallStatus(callStatusTail) && where.previous.isLua() {
				d.Name, d.NameKind = l.functionName(where.previous)
			} else {
				d.NameKind = ""
			}
			if d.NameKind == "" {
				d.NameKind = "" // not found
				d.Name = ""
			}
		case 'L':
			hasL = true
		case 'f':
			hasF = true
		default:
			ok = false
		}
	}
	if hasF {
		l.apiPush(fun)
	}
	if hasL {
		l.collectValidLines(f)
	}
	return d, ok
}
