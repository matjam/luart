package stdlib

import (
	"os"
	"reflect"
	"strings"

	"github.com/matjam/apogee/lua"
)

func upValueHelper(f func(*lua.State, int, int) (string, bool), returnValueCount int) lua.Function {
	return func(l *lua.State) int {
		l.CheckType(1, lua.TypeFunction)
		name, ok := f(l, 1, checkInt(l, 2))
		if !ok {
			return 0
		}
		l.PushString(name)
		l.Insert(-returnValueCount)
		return returnValueCount
	}
}

func checkUpValue(l *lua.State, f, upValueCount int) int {
	n := checkInt(l, upValueCount)
	l.CheckType(f, lua.TypeFunction)
	l.PushValue(f)
	debug, _ := l.Info(">u", lua.Frame{})
	l.ArgumentCheck(1 <= n && n <= debug.UpValueCount, upValueCount, "invalid upvalue index")
	return n
}

func threadArg(l *lua.State) (int, *lua.State) {
	if l.IsThread(1) {
		return 1, l.ToThread(1)
	}
	return 0, l
}

func hookTable(l *lua.State) bool { return l.SubTable(lua.RegistryIndex, "_HKEY") }

func internalHook(l *lua.State, d lua.Debug) {
	hookNames := []string{"call", "return", "line", "count", "tail call"}
	hookTable(l)
	l.PushThread()
	l.RawGet(-2)
	if l.IsFunction(-1) {
		l.PushString(hookNames[d.Event])
		if d.CurrentLine >= 0 {
			l.PushInteger(d.CurrentLine)
		} else {
			l.PushNil()
		}
		l.Call(2, 0)
	}
}

// isInternalHook reports whether h is internalHook, the hook debug.sethook
// installs to call a Lua function.
func isInternalHook(h lua.Hook) bool {
	return reflect.ValueOf(h).Pointer() == reflect.ValueOf(lua.Hook(internalHook)).Pointer()
}

func maskToString(mask byte) (s string) {
	if mask&lua.MaskCall != 0 {
		s += "c"
	}
	if mask&lua.MaskReturn != 0 {
		s += "r"
	}
	if mask&lua.MaskLine != 0 {
		s += "l"
	}
	return
}

func stringToMask(s string, maskCount bool) (mask byte) {
	for r, b := range map[rune]byte{'c': lua.MaskCall, 'r': lua.MaskReturn, 'l': lua.MaskLine} {
		if strings.ContainsRune(s, r) {
			mask |= b
		}
	}
	if maskCount {
		mask |= lua.MaskCount
	}
	return
}

// getInfo is debug.getinfo([thread,] f [, what]), after ldblib.c's
// db_getinfo. f is a function or a stack level.
func getInfo(l *lua.State) int {
	arg, l1 := threadArg(l)
	options := l.OptString(arg+2, "flnStu")
	l.ArgumentCheck(!strings.HasPrefix(options, ">"), arg+2, "invalid option '>'")
	var frame lua.Frame
	if l.IsNumber(arg + 1) {
		level, _ := l.ToInteger(arg + 1)
		var ok bool
		if frame, ok = l1.Frame(int(level)); !ok {
			l.PushNil() // level out of range
			return 1
		}
	} else if l.IsFunction(arg + 1) {
		options = ">" + options
		l.PushValue(arg + 1)
		l.XMove(l1, 1)
	} else {
		l.ArgumentError(arg+1, "function or level expected")
	}
	d, ok := l1.Info(options, frame)
	if !ok {
		l.ArgumentError(arg+2, "invalid option")
	}
	has := func(o byte) bool { return strings.IndexByte(options, o) >= 0 }
	setString := func(k, v string) { l.PushString(v); l.SetField(-2, k) }
	setInteger := func(k string, v int) { l.PushInteger(v); l.SetField(-2, k) }
	setBoolean := func(k string, v bool) { l.PushBoolean(v); l.SetField(-2, k) }
	l.CreateTable(0, 2)
	if has('S') {
		setString("source", d.Source)
		setString("short_src", d.ShortSource)
		setInteger("linedefined", d.LineDefined)
		setInteger("lastlinedefined", d.LastLineDefined)
		setString("what", d.What)
	}
	if has('l') {
		setInteger("currentline", d.CurrentLine)
	}
	if has('u') {
		setInteger("nups", d.UpValueCount)
		setInteger("nparams", d.ParameterCount)
		setBoolean("isvararg", d.IsVarArg)
	}
	if has('n') {
		if d.NameKind != "" { // no name found leaves name nil, as in C
			setString("name", d.Name)
		}
		setString("namewhat", d.NameKind)
	}
	if has('t') {
		setBoolean("istailcall", d.IsTailCall)
		setInteger("extraargs", d.ExtraArgs)
	}
	if has('r') {
		setInteger("ftransfer", d.FirstTransfer)
		setInteger("ntransfer", d.TransferCount)
	}
	// Info pushed the function for 'f' and then the lines for 'L'.
	if has('L') {
		moveStackOption(l, l1, "activelines")
	}
	if has('f') {
		moveStackOption(l, l1, "func")
	}
	return 1
}

// moveStackOption moves a value Info pushed, below the result table on l
// or on top of l1's stack, to the table's field name.
func moveStackOption(l, l1 *lua.State, name string) {
	if l == l1 {
		l.PushValue(-2)
		l.Remove(-3)
	} else {
		l1.XMove(l, 1)
	}
	l.SetField(-2, name)
}

// getLocal is debug.getlocal([thread,] f, n), after ldblib.c's
// db_getlocal. f is a stack level, whose local n's name and value it
// returns, or a function, whose parameter n's name it returns.
func getLocal(l *lua.State) int {
	arg, l1 := threadArg(l)
	n := checkInt(l, arg+2)
	if l.IsFunction(arg + 1) {
		l.PushValue(arg + 1)
		if name, ok := l.Local(lua.Frame{}, n); ok {
			l.PushString(name)
		} else {
			l.PushNil()
		}
		return 1
	}
	frame, ok := l1.Frame(checkInt(l, arg+1))
	if !ok {
		l.ArgumentError(arg+1, "level out of range")
	}
	name, ok := l1.Local(frame, n)
	if !ok {
		l.PushNil() // no name, nor value
		return 1
	}
	l1.XMove(l, 1)
	l.PushString(name)
	l.PushValue(-2)
	return 2
}

// setLocal is debug.setlocal([thread,] level, n, value), after ldblib.c's
// db_setlocal: it returns the local's name, or nil if there is none.
func setLocal(l *lua.State) int {
	arg, l1 := threadArg(l)
	frame, ok := l1.Frame(checkInt(l, arg+1))
	if !ok {
		l.ArgumentError(arg+1, "level out of range")
	}
	n := checkInt(l, arg+2)
	l.CheckAny(arg + 3)
	l.SetTop(arg + 3)
	l.XMove(l1, 1)
	if name, ok := l1.SetLocal(frame, n); ok {
		l.PushString(name)
	} else {
		l.PushNil()
	}
	return 1
}

// debugPrompt is debug.debug, after ldblib.c's db_debug: it runs each line
// read from stdin until "cont" or the end of the input, prompting on
// stderr and writing errors there.
func debugPrompt(l *lua.State) int {
	for {
		os.Stderr.WriteString("lua_debug> ")
		line, ok := readStdinLine()
		if !ok || line == "cont\n" {
			return 0
		}
		err := l.LoadBuffer(line, "=(debug command)", "")
		if err == nil {
			err = l.ProtectedCall(0, 0, 0)
		}
		if err != nil {
			msg, _ := l.ToString(-1)
			os.Stderr.WriteString(msg + "\n")
		}
		l.SetTop(0)
	}
}

// readStdinLine reads a line from stdin a byte at a time, so it reads no
// further than the line, and returns false at the end of the input.
func readStdinLine() (string, bool) {
	var line []byte
	b := make([]byte, 1)
	for {
		if n, err := os.Stdin.Read(b); n == 0 || err != nil {
			return string(line), len(line) > 0
		}
		if line = append(line, b[0]); b[0] == '\n' {
			return string(line), true
		}
	}
}

var debugLibrary = []lua.RegistryFunction{
	{Name: "debug", Function: debugPrompt},
	{Name: "getuservalue", Function: func(l *lua.State) int {
		n := int(l.OptInteger(2, 1))
		if l.TypeOf(1) != lua.TypeUserData {
			l.PushNil()
		} else if l.UserValue(1, n) != lua.TypeNone {
			l.PushBoolean(true)
			return 2
		}
		return 1
	}},
	{Name: "gethook", Function: func(l *lua.State) int {
		_, l1 := threadArg(l)
		hooker, mask := l1.Hook(), l1.HookMask()
		if hooker != nil && !isInternalHook(hooker) {
			l.PushString("external hook")
		} else {
			hookTable(l)
			l1.PushThread()
			l1.XMove(l, 1)
			l.RawGet(-2)
			l.Remove(-2)
		}
		l.PushString(maskToString(mask))
		l.PushInteger(l1.HookCount())
		return 3
	}},
	{Name: "getinfo", Function: getInfo},
	{Name: "getlocal", Function: getLocal},
	{Name: "getregistry", Function: func(l *lua.State) int { l.PushValue(lua.RegistryIndex); return 1 }},
	{Name: "getmetatable", Function: func(l *lua.State) int {
		l.CheckAny(1)
		if !l.MetaTable(1) {
			l.PushNil()
		}
		return 1
	}},
	{Name: "getupvalue", Function: upValueHelper((*lua.State).UpValue, 2)},
	{Name: "upvaluejoin", Function: func(l *lua.State) int {
		n1 := checkUpValue(l, 1, 2)
		n2 := checkUpValue(l, 3, 4)
		l.ArgumentCheck(!l.IsGoFunction(1), 1, "Lua function expected")
		l.ArgumentCheck(!l.IsGoFunction(3), 3, "Lua function expected")
		l.UpValueJoin(1, n1, 3, n2)
		return 0
	}},
	{Name: "upvalueid", Function: func(l *lua.State) int {
		n := checkInt(l, 2)
		l.CheckType(1, lua.TypeFunction)
		if id := l.UpValueID(1, n); id != nil {
			l.PushLightUserData(id)
		} else {
			l.PushNil() // fail
		}
		return 1
	}},
	{Name: "setuservalue", Function: func(l *lua.State) int {
		n := int(l.OptInteger(3, 1))
		if l.TypeOf(1) == lua.TypeLightUserData {
			l.ArgumentError(1, "full userdata expected, got light userdata")
		}
		l.CheckType(1, lua.TypeUserData)
		l.CheckAny(2)
		l.SetTop(2)
		if !l.SetUserValue(1, n) {
			l.PushNil() // fail
		}
		return 1
	}},
	{Name: "sethook", Function: func(l *lua.State) int {
		var hook lua.Hook
		var mask byte
		var count int
		if !l.SubTable(lua.RegistryIndex, "_HOOKKEY") { // as ldblib.c keeps hooks: a weak-keyed table
			l.PushString("k")
			l.SetField(-2, "__mode")
			l.PushValue(-1)
			l.SetMetaTable(-2)
		}
		l.Pop(1)
		i, l1 := threadArg(l)
		if l.IsNoneOrNil(i + 1) {
			l.SetTop(i + 1)
		} else {
			s := l.CheckString(i + 2)
			l.CheckType(i+1, lua.TypeFunction)
			count = optInt(l, i+3, 0)
			hook, mask = internalHook, stringToMask(s, count > 0)
		}
		if !hookTable(l) {
			l.PushString("k")
			l.SetField(-2, "__mode")
			l.PushValue(-1)
			l.SetMetaTable(-2)
		}
		l1.PushThread()
		l1.XMove(l, 1)
		l.PushValue(i + 1)
		l.RawSet(-3)
		l1.SetHook(hook, mask, count)
		return 0
	}},
	{Name: "setlocal", Function: setLocal},
	{Name: "setmetatable", Function: func(l *lua.State) int {
		t := l.TypeOf(2)
		l.ArgumentCheck(t == lua.TypeNil || t == lua.TypeTable, 2, "nil or table expected")
		l.SetTop(2)
		l.SetMetaTable(1)
		return 1
	}},
	{Name: "setupvalue", Function: upValueHelper((*lua.State).SetUpValue, 1)},
	{Name: "traceback", Function: func(l *lua.State) int {
		i, l1 := threadArg(l)
		if s, ok := l.ToString(i + 1); !ok && !l.IsNoneOrNil(i+1) {
			l.PushValue(i + 1)
		} else if l == l1 {
			l.Traceback(l, s, optInt(l, i+2, 1))
		} else {
			l.Traceback(l1, s, optInt(l, i+2, 0))
		}
		return 1
	}},
}

// OpenDebug opens the debug library. Usually passed to Require.
func OpenDebug(l *lua.State) int {
	l.NewLibrary(debugLibrary)
	return 1
}
