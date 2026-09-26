package stdlib

import (
	"io"
	"os"
	"strings"

	"github.com/matjam/luart/lua"
)

func baseNext(l *lua.State) int {
	l.CheckType(1, lua.TypeTable)
	l.SetTop(2)
	if l.Next(1) {
		return 2
	}
	l.PushNil()
	return 1
}

// pairs is a Go closure whose upvalue is next, which it returns, so it
// returns the same function each time, unless __pairs says otherwise.
func pairs(l *lua.State) int {
	l.CheckAny(1)
	if l.MetaField(1, "__pairs") {
		l.PushValue(1) // argument 'self' to metamethod
		l.Call(1, 3)   // get 3 values from metamethod
		return 3
	}
	l.PushValue(lua.UpValueIndex(1))
	l.PushValue(1)
	l.PushNil()
	return 3
}

// ipairs returns its iterator, an upvalue so that it is the same function
// each time, which indexes the value with metamethods, as Lua 5.4's does.
func ipairs(l *lua.State) int {
	l.CheckAny(1)
	l.PushValue(lua.UpValueIndex(1))
	l.PushValue(1)
	l.PushInteger(0)
	return 3
}

var gcOptions = []string{"stop", "restart", "collect", "count", "step", "setpause", "setstepmul", "setmajorinc", "isrunning", "generational", "incremental"}

// gcOptionValues are the GC options for gcOptions, in order.
var gcOptionValues = []lua.GCOption{lua.GCStop, lua.GCRestart, lua.GCCollect, lua.GCCount, lua.GCStep,
	lua.GCSetPause, lua.GCSetStepMul, lua.GCSetMajorInc, lua.GCIsRunning, lua.GCGenerational, lua.GCIncremental}

// collectGarbage is collectgarbage, after lbaselib.c's luaB_collectgarbage.
// See State.GC for what each option does in luart.
func collectGarbage(l *lua.State) int {
	o := gcOptionValues[l.CheckOption(1, "collect", gcOptions)]
	res := l.GC(o, int(l.OptInteger(2, 0)))
	switch o {
	case lua.GCCount:
		b := l.GC(lua.GCCountBytes, 0)
		l.PushNumber(float64(res) + float64(b)/1024) // kilobytes, with the remainder
		return 1
	case lua.GCStep, lua.GCIsRunning:
		l.PushBoolean(res != 0)
	default:
		l.PushInteger(res)
	}
	return 1
}

// intPairs is ipairs' iterator: the next index, and the value there,
// through __index, until that is nil.
func intPairs(l *lua.State) int {
	i := l.CheckInteger(2) + 1
	l.PushInteger(i)
	l.PushInteger(i)
	l.Table(1)
	if l.IsNil(-1) {
		return 1
	}
	return 2
}

func finishProtectedCall(l *lua.State, status bool) int {
	if !l.CheckStack(1) {
		l.SetTop(0) // create space for return values
		l.PushBoolean(false)
		l.PushString("stack overflow")
		return 2 // return false, message
	}
	l.PushBoolean(status) // first result (status)
	l.Replace(1)          // put first result in the first slot
	return l.Top()
}

func protectedCallContinuation(l *lua.State) int {
	_, shouldYield, _ := l.Context()
	return finishProtectedCall(l, shouldYield)
}

func loadHelper(l *lua.State, s error, e int) int {
	if s == nil {
		if e != 0 {
			l.PushValue(e)
			if _, ok := l.SetUpValue(-2, 1); !ok {
				l.Pop(1)
			}
		}
		return 1
	}
	l.PushNil()
	l.Insert(-2)
	return 2
}

type genericReader struct {
	l *lua.State
	r *strings.Reader
	e error
}

func (r *genericReader) Read(b []byte) (n int, err error) {
	if r.e != nil {
		return 0, r.e
	}
	if l := r.l; r.r == nil {
		// The chunk ends at the first nil or empty piece. r.e keeps it
		// ended, as bufio reads again after an io.EOF.
		l.CheckStackWithMessage(2, "too many nested functions")
		l.PushValue(1)
		l.Call(0, 1)
		if !l.IsNil(-1) && !l.IsString(-1) {
			l.Errorf("reader function must return a string")
		}
		s, _ := l.ToString(-1)
		l.Pop(1)
		if s == "" {
			r.e = io.EOF
			return 0, io.EOF
		}
		r.r = strings.NewReader(s)
	}
	if n, err = r.r.Read(b); err == io.EOF {
		r.r, err = nil, nil
	} else if err != nil {
		r.e = err
	}
	return
}

var baseLibrary = []lua.RegistryFunction{
	{Name: "assert", Function: func(l *lua.State) int {
		if !l.ToBoolean(1) {
			l.Errorf("%s", l.OptString(2, "assertion failed!"))
			panic("unreachable")
		}
		return l.Top()
	}},
	{Name: "collectgarbage", Function: collectGarbage},
	{Name: "dofile", Function: func(l *lua.State) int {
		f := l.OptString(1, "")
		if l.SetTop(1); l.LoadFile(f, "") != nil {
			l.Error()
			panic("unreachable")
		}
		continuation := func(l *lua.State) int { return l.Top() - 1 }
		l.CallWithContinuation(0, lua.MultipleReturns, 0, continuation)
		return continuation(l)
	}},
	{Name: "error", Function: func(l *lua.State) int {
		level := l.OptInteger(2, 1)
		l.SetTop(1)
		if l.IsString(1) && level > 0 {
			l.Where(int(level))
			l.PushValue(1)
			l.Concat(2)
		}
		l.Error()
		panic("unreachable")
	}},
	{Name: "getmetatable", Function: func(l *lua.State) int {
		l.CheckAny(1)
		if !l.MetaTable(1) {
			l.PushNil()
			return 1
		}
		l.MetaField(1, "__metatable")
		return 1
	}},
	{Name: "loadfile", Function: func(l *lua.State) int {
		f, m, e := l.OptString(1, ""), l.OptString(2, ""), 3
		if l.IsNone(e) {
			e = 0
		}
		return loadHelper(l, l.LoadFile(f, m), e)
	}},
	{Name: "load", Function: func(l *lua.State) int {
		m, e := l.OptString(3, "bt"), 4
		l.ArgumentCheck(!strings.Contains(m, "B"), 3, "invalid mode") // C's fixed buffers
		if l.IsNone(e) {
			e = 0
		}
		var err error
		if s, ok := l.ToString(1); ok {
			err = l.LoadBuffer(s, l.OptString(2, s), m)
		} else {
			chunkName := l.OptString(2, "=(load)")
			l.CheckType(1, lua.TypeFunction)
			err = l.Load(&genericReader{l: l}, chunkName, m)
		}
		return loadHelper(l, err, e)
	}},
	{Name: "next", Function: baseNext},
	{Name: "pcall", Function: func(l *lua.State) int {
		l.CheckAny(1)
		l.PushNil()
		l.Insert(1) // create space for status result
		return finishProtectedCall(l, nil == l.ProtectedCallWithContinuation(l.Top()-2, lua.MultipleReturns, 0, 0, protectedCallContinuation))
	}},
	{Name: "print", Function: func(l *lua.State) int {
		n := l.Top()
		l.Global("tostring")
		for i := 1; i <= n; i++ {
			l.PushValue(-1) // function to be called
			l.PushValue(i)  // value to print
			l.Call(1, 1)
			s, ok := l.ToString(-1)
			if !ok {
				l.Errorf("'tostring' must return a string to 'print'")
				panic("unreachable")
			}
			if i > 1 {
				os.Stdout.WriteString("\t")
			}
			os.Stdout.WriteString(s)
			l.Pop(1) // pop result
		}
		os.Stdout.WriteString("\n")
		os.Stdout.Sync()
		return 0
	}},
	{Name: "rawequal", Function: func(l *lua.State) int {
		l.CheckAny(1)
		l.CheckAny(2)
		l.PushBoolean(l.RawEqual(1, 2))
		return 1
	}},
	{Name: "rawlen", Function: func(l *lua.State) int {
		t := l.TypeOf(1)
		l.ArgumentCheck(t == lua.TypeTable || t == lua.TypeString, 1, "table or string expected")
		l.PushInteger(l.RawLength(1))
		return 1
	}},
	{Name: "rawget", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		l.CheckAny(2)
		l.SetTop(2)
		l.RawGet(1)
		return 1
	}},
	{Name: "rawset", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		l.CheckAny(2)
		l.CheckAny(3)
		l.SetTop(3)
		l.RawSet(1)
		return 1
	}},
	{Name: "select", Function: func(l *lua.State) int {
		n := int64(l.Top())
		if l.TypeOf(1) == lua.TypeString {
			if s, _ := l.ToString(1); s[0] == '#' {
				l.PushInteger(n - 1)
				return 1
			}
		}
		i := l.CheckInteger(1)
		if i < 0 {
			i = n + i
		} else if i > n {
			i = n
		}
		l.ArgumentCheck(1 <= i, 1, "index out of range")
		return int(n - i)
	}},
	{Name: "setmetatable", Function: func(l *lua.State) int {
		t := l.TypeOf(2)
		l.CheckType(1, lua.TypeTable)
		l.ArgumentCheck(t == lua.TypeNil || t == lua.TypeTable, 2, "nil or table expected")
		if l.MetaField(1, "__metatable") {
			l.Errorf("cannot change a protected metatable")
		}
		l.SetTop(2)
		l.SetMetaTable(1)
		return 1
	}},
	{Name: "tonumber", Function: func(l *lua.State) int {
		if l.IsNoneOrNil(2) { // standard conversion
			if l.TypeOf(1) == lua.TypeNumber {
				l.SetTop(1)
				return 1
			}
			if s, ok := l.ToString(1); ok && l.StringToNumber(s) {
				return 1
			}
			l.CheckAny(1)
		} else {
			base := l.CheckInteger(2)
			l.CheckType(1, lua.TypeString)
			s, _ := l.ToString(1)
			l.ArgumentCheck(2 <= base && base <= 36, 2, "base out of range")
			if n, ok := stringToIntBase(s, base); ok {
				l.PushInteger(n)
				return 1
			}
		}
		l.PushNil()
		return 1
	}},
	{Name: "tostring", Function: func(l *lua.State) int {
		l.CheckAny(1)
		l.ToStringMeta(1)
		return 1
	}},
	{Name: "type", Function: func(l *lua.State) int {
		l.CheckAny(1)
		l.PushString(l.TypeName(1))
		return 1
	}},
	{Name: "xpcall", Function: func(l *lua.State) int {
		n := l.Top()
		l.ArgumentCheck(n >= 2, 2, "value expected")
		l.PushValue(1) // exchange function and error handler
		l.Copy(2, 1)
		l.Replace(2)
		return finishProtectedCall(l, nil == l.ProtectedCallWithContinuation(n-2, lua.MultipleReturns, 1, 0, protectedCallContinuation))
	}},
}

// OpenBase opens the basic library. Usually passed to Require.
func OpenBase(l *lua.State) int {
	l.PushGlobalTable()
	l.PushGlobalTable()
	l.SetField(-2, "_G")
	l.SetFunctions(baseLibrary, 0)
	// pairs returns next, and ipairs one iterator, as C Lua's do.
	l.Field(-1, "next")
	l.PushGoClosure(pairs, 1)
	l.SetField(-2, "pairs")
	l.PushGoFunction(intPairs)
	l.PushGoClosure(ipairs, 1)
	l.SetField(-2, "ipairs")
	l.PushString(lua.VersionString)
	l.SetField(-2, "_VERSION")
	return 1
}

// stringToIntBase converts s, an integer in base with optional space
// around it and a minus sign, as lbaselib.c's b_str2int does, wrapping
// around on overflow.
func stringToIntBase(s string, base int64) (int64, bool) {
	s = strings.Trim(s, " \f\n\r\t\v")
	negative := strings.HasPrefix(s, "-")
	if negative || strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d int64
		switch {
		case '0' <= c && c <= '9':
			d = int64(c - '0')
		case 'a' <= c && c <= 'z':
			d = int64(c-'a') + 10
		case 'A' <= c && c <= 'Z':
			d = int64(c-'A') + 10
		default:
			return 0, false
		}
		if d >= base {
			return 0, false
		}
		n = n*uint64(base) + uint64(d)
	}
	if negative {
		n = -n
	}
	return int64(n), true
}
