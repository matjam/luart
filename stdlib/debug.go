package stdlib

import (
	"reflect"
	"strings"

	"github.com/matjam/luart/lua"
)

func upValueHelper(f func(*lua.State, int, int) (string, bool), returnValueCount int) lua.Function {
	return func(l *lua.State) int {
		l.CheckType(1, lua.TypeFunction)
		name, ok := f(l, 1, l.CheckInteger(2))
		if !ok {
			return 0
		}
		l.PushString(name)
		l.Insert(-returnValueCount)
		return returnValueCount
	}
}

func checkUpValue(l *lua.State, f, upValueCount int) int {
	n := l.CheckInteger(upValueCount)
	l.CheckType(f, lua.TypeFunction)
	l.PushValue(f)
	debug, _ := l.Info(">u", lua.Frame{})
	l.ArgumentCheck(1 <= n && n <= debug.UpValueCount, upValueCount, "invalue upvalue index")
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

var debugLibrary = []lua.RegistryFunction{
	// {"debug", db_debug},
	{Name: "getuservalue", Function: func(l *lua.State) int {
		if l.TypeOf(1) != lua.TypeUserData {
			l.PushNil()
		} else {
			l.UserValue(1)
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
	// {"getinfo", db_getinfo},
	// {"getlocal", db_getlocal},
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
	{Name: "upvalueid", Function: func(l *lua.State) int { l.PushLightUserData(l.UpValueID(1, checkUpValue(l, 1, 2))); return 1 }},
	{Name: "setuservalue", Function: func(l *lua.State) int {
		if l.TypeOf(1) == lua.TypeLightUserData {
			l.ArgumentError(1, "full userdata expected, got light userdata")
		}
		l.CheckType(1, lua.TypeUserData)
		if !l.IsNoneOrNil(2) {
			l.CheckType(2, lua.TypeTable)
		}
		l.SetTop(2)
		l.SetUserValue(1)
		return 1
	}},
	{Name: "sethook", Function: func(l *lua.State) int {
		var hook lua.Hook
		var mask byte
		var count int
		i, l1 := threadArg(l)
		if l.IsNoneOrNil(i + 1) {
			l.SetTop(i + 1)
		} else {
			s := l.CheckString(i + 2)
			l.CheckType(i+1, lua.TypeFunction)
			count = l.OptInteger(i+3, 0)
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
	// {"setlocal", db_setlocal},
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
			l.Traceback(l, s, l.OptInteger(i+2, 1))
		} else {
			l.Traceback(l1, s, l.OptInteger(i+2, 0))
		}
		return 1
	}},
}

// OpenDebug opens the debug library. Usually passed to Require.
func OpenDebug(l *lua.State) int {
	l.NewLibrary(debugLibrary)
	return 1
}
