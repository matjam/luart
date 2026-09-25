package lua

import (
	"strings"
)

// xmove moves the top n values of from's stack to to's, as lua_xmove does.
// Moving within one state leaves the stack as it is.
func xmove(from, to *State, n int) {
	if from == to {
		return
	}
	for _, v := range from.stack[from.top-n : from.top] {
		to.apiPush(v)
	}
	from.top -= n
}

func upValueHelper(f func(*State, int, int) (string, bool), returnValueCount int) Function {
	return func(l *State) int {
		CheckType(l, 1, TypeFunction)
		name, ok := f(l, 1, CheckInteger(l, 2))
		if !ok {
			return 0
		}
		l.PushString(name)
		l.Insert(-returnValueCount)
		return returnValueCount
	}
}

func (l *State) checkUpValue(f, upValueCount int) int {
	n := CheckInteger(l, upValueCount)
	CheckType(l, f, TypeFunction)
	l.PushValue(f)
	debug, _ := Info(l, ">u", nil)
	ArgumentCheck(l, 1 <= n && n <= debug.UpValueCount, upValueCount, "invalue upvalue index")
	return n
}

func threadArg(l *State) (int, *State) {
	if l.IsThread(1) {
		return 1, l.ToThread(1)
	}
	return 0, l
}

func hookTable(l *State) bool { return SubTable(l, RegistryIndex, "_HKEY") }

func internalHook(l *State, d Debug) {
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
		_, ok := Info(l, "lS", d.callInfo)
		l.assert(ok)
		l.Call(2, 0)
	}
}

func maskToString(mask byte) (s string) {
	if mask&MaskCall != 0 {
		s += "c"
	}
	if mask&MaskReturn != 0 {
		s += "r"
	}
	if mask&MaskLine != 0 {
		s += "l"
	}
	return
}

func stringToMask(s string, maskCount bool) (mask byte) {
	for r, b := range map[rune]byte{'c': MaskCall, 'r': MaskReturn, 'l': MaskLine} {
		if strings.ContainsRune(s, r) {
			mask |= b
		}
	}
	if maskCount {
		mask |= MaskCount
	}
	return
}

var debugLibrary = []RegistryFunction{
	// {"debug", db_debug},
	{"getuservalue", func(l *State) int {
		if l.TypeOf(1) != TypeUserData {
			l.PushNil()
		} else {
			l.UserValue(1)
		}
		return 1
	}},
	{"gethook", func(l *State) int {
		_, l1 := threadArg(l)
		hooker, mask := DebugHook(l1), DebugHookMask(l1)
		if hooker != nil && !l.internalHook {
			l.PushString("external hook")
		} else {
			hookTable(l)
			l1.PushThread()
			xmove(l1, l, 1)
			l.RawGet(-2)
			l.Remove(-2)
		}
		l.PushString(maskToString(mask))
		l.PushInteger(DebugHookCount(l1))
		return 3
	}},
	// {"getinfo", db_getinfo},
	// {"getlocal", db_getlocal},
	{"getregistry", func(l *State) int { l.PushValue(RegistryIndex); return 1 }},
	{"getmetatable", func(l *State) int {
		CheckAny(l, 1)
		if !l.MetaTable(1) {
			l.PushNil()
		}
		return 1
	}},
	{"getupvalue", upValueHelper(UpValue, 2)},
	{"upvaluejoin", func(l *State) int {
		n1 := l.checkUpValue(1, 2)
		n2 := l.checkUpValue(3, 4)
		ArgumentCheck(l, !l.IsGoFunction(1), 1, "Lua function expected")
		ArgumentCheck(l, !l.IsGoFunction(3), 3, "Lua function expected")
		UpValueJoin(l, 1, n1, 3, n2)
		return 0
	}},
	{"upvalueid", func(l *State) int { l.PushLightUserData(UpValueId(l, 1, l.checkUpValue(1, 2))); return 1 }},
	{"setuservalue", func(l *State) int {
		if l.TypeOf(1) == TypeLightUserData {
			ArgumentError(l, 1, "full userdata expected, got light userdata")
		}
		CheckType(l, 1, TypeUserData)
		if !l.IsNoneOrNil(2) {
			CheckType(l, 2, TypeTable)
		}
		l.SetTop(2)
		l.SetUserValue(1)
		return 1
	}},
	{"sethook", func(l *State) int {
		var hook Hook
		var mask byte
		var count int
		i, l1 := threadArg(l)
		if l.IsNoneOrNil(i + 1) {
			l.SetTop(i + 1)
		} else {
			s := CheckString(l, i+2)
			CheckType(l, i+1, TypeFunction)
			count = OptInteger(l, i+3, 0)
			hook, mask = internalHook, stringToMask(s, count > 0)
		}
		if !hookTable(l) {
			l.PushString("k")
			l.SetField(-2, "__mode")
			l.PushValue(-1)
			l.SetMetaTable(-2)
		}
		l1.PushThread()
		xmove(l1, l, 1)
		l.PushValue(i + 1)
		l.RawSet(-3)
		SetDebugHook(l1, hook, mask, count)
		l1.internalHook = true
		return 0
	}},
	// {"setlocal", db_setlocal},
	{"setmetatable", func(l *State) int {
		t := l.TypeOf(2)
		ArgumentCheck(l, t == TypeNil || t == TypeTable, 2, "nil or table expected")
		l.SetTop(2)
		l.SetMetaTable(1)
		return 1
	}},
	{"setupvalue", upValueHelper(SetUpValue, 1)},
	{"traceback", func(l *State) int {
		i, l1 := threadArg(l)
		if s, ok := l.ToString(i + 1); !ok && !l.IsNoneOrNil(i+1) {
			l.PushValue(i + 1)
		} else if l == l1 {
			Traceback(l, l, s, OptInteger(l, i+2, 1))
		} else {
			Traceback(l, l1, s, OptInteger(l, i+2, 0))
		}
		return 1
	}},
}

// DebugOpen opens the debug library. Usually passed to Require.
func DebugOpen(l *State) int {
	NewLibrary(l, debugLibrary)
	return 1
}
