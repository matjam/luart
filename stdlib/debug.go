package stdlib

import (
	"reflect"
	"strings"

	"github.com/matjam/luart"
)

func upValueHelper(f func(*luart.State, int, int) (string, bool), returnValueCount int) luart.Function {
	return func(l *luart.State) int {
		l.CheckType(1, luart.TypeFunction)
		name, ok := f(l, 1, l.CheckInteger(2))
		if !ok {
			return 0
		}
		l.PushString(name)
		l.Insert(-returnValueCount)
		return returnValueCount
	}
}

func checkUpValue(l *luart.State, f, upValueCount int) int {
	n := l.CheckInteger(upValueCount)
	l.CheckType(f, luart.TypeFunction)
	l.PushValue(f)
	debug, _ := l.Info(">u", luart.Frame{})
	l.ArgumentCheck(1 <= n && n <= debug.UpValueCount, upValueCount, "invalue upvalue index")
	return n
}

func threadArg(l *luart.State) (int, *luart.State) {
	if l.IsThread(1) {
		return 1, l.ToThread(1)
	}
	return 0, l
}

func hookTable(l *luart.State) bool { return l.SubTable(luart.RegistryIndex, "_HKEY") }

func internalHook(l *luart.State, d luart.Debug) {
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
func isInternalHook(h luart.Hook) bool {
	return reflect.ValueOf(h).Pointer() == reflect.ValueOf(luart.Hook(internalHook)).Pointer()
}

func maskToString(mask byte) (s string) {
	if mask&luart.MaskCall != 0 {
		s += "c"
	}
	if mask&luart.MaskReturn != 0 {
		s += "r"
	}
	if mask&luart.MaskLine != 0 {
		s += "l"
	}
	return
}

func stringToMask(s string, maskCount bool) (mask byte) {
	for r, b := range map[rune]byte{'c': luart.MaskCall, 'r': luart.MaskReturn, 'l': luart.MaskLine} {
		if strings.ContainsRune(s, r) {
			mask |= b
		}
	}
	if maskCount {
		mask |= luart.MaskCount
	}
	return
}

var debugLibrary = []luart.RegistryFunction{
	// {"debug", db_debug},
	{Name: "getuservalue", Function: func(l *luart.State) int {
		if l.TypeOf(1) != luart.TypeUserData {
			l.PushNil()
		} else {
			l.UserValue(1)
		}
		return 1
	}},
	{Name: "gethook", Function: func(l *luart.State) int {
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
	{Name: "getregistry", Function: func(l *luart.State) int { l.PushValue(luart.RegistryIndex); return 1 }},
	{Name: "getmetatable", Function: func(l *luart.State) int {
		l.CheckAny(1)
		if !l.MetaTable(1) {
			l.PushNil()
		}
		return 1
	}},
	{Name: "getupvalue", Function: upValueHelper((*luart.State).UpValue, 2)},
	{Name: "upvaluejoin", Function: func(l *luart.State) int {
		n1 := checkUpValue(l, 1, 2)
		n2 := checkUpValue(l, 3, 4)
		l.ArgumentCheck(!l.IsGoFunction(1), 1, "Lua function expected")
		l.ArgumentCheck(!l.IsGoFunction(3), 3, "Lua function expected")
		l.UpValueJoin(1, n1, 3, n2)
		return 0
	}},
	{Name: "upvalueid", Function: func(l *luart.State) int { l.PushLightUserData(l.UpValueID(1, checkUpValue(l, 1, 2))); return 1 }},
	{Name: "setuservalue", Function: func(l *luart.State) int {
		if l.TypeOf(1) == luart.TypeLightUserData {
			l.ArgumentError(1, "full userdata expected, got light userdata")
		}
		l.CheckType(1, luart.TypeUserData)
		if !l.IsNoneOrNil(2) {
			l.CheckType(2, luart.TypeTable)
		}
		l.SetTop(2)
		l.SetUserValue(1)
		return 1
	}},
	{Name: "sethook", Function: func(l *luart.State) int {
		var hook luart.Hook
		var mask byte
		var count int
		i, l1 := threadArg(l)
		if l.IsNoneOrNil(i + 1) {
			l.SetTop(i + 1)
		} else {
			s := l.CheckString(i + 2)
			l.CheckType(i+1, luart.TypeFunction)
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
	{Name: "setmetatable", Function: func(l *luart.State) int {
		t := l.TypeOf(2)
		l.ArgumentCheck(t == luart.TypeNil || t == luart.TypeTable, 2, "nil or table expected")
		l.SetTop(2)
		l.SetMetaTable(1)
		return 1
	}},
	{Name: "setupvalue", Function: upValueHelper((*luart.State).SetUpValue, 1)},
	{Name: "traceback", Function: func(l *luart.State) int {
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
func OpenDebug(l *luart.State) int {
	l.NewLibrary(debugLibrary)
	return 1
}
