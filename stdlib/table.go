package stdlib

import (
	"fmt"
	"strings"

	"github.com/matjam/luart/lua"
)

var tableLibrary = []lua.RegistryFunction{
	{Name: "concat", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		sep := l.OptString(2, "")
		i := l.OptInteger(3, 1)
		var last int
		if l.IsNoneOrNil(4) {
			last = l.Len(1)
		} else {
			last = l.CheckInteger(4)
		}
		var s strings.Builder
		addField := func() {
			l.RawGetInt(1, i)
			if str, ok := l.ToString(-1); ok {
				s.WriteString(str)
			} else {
				l.Errorf(fmt.Sprintf("invalid value (%s) at index %d in table for 'concat'", l.TypeName(-1), i))
			}
			l.Pop(1)
		}
		for ; i < last; i++ {
			addField()
			s.WriteString(sep)
		}
		if i == last {
			addField()
		}
		l.PushString(s.String())
		return 1
	}},
	{Name: "insert", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		e := l.Len(1) + 1 // First empty element.
		switch l.Top() {
		case 2:
			l.RawSetInt(1, e) // Insert new element at the end.
		case 3:
			pos := l.CheckInteger(2)
			l.ArgumentCheck(1 <= pos && pos <= e, 2, "position out of bounds")
			for i := e; i > pos; i-- {
				l.RawGetInt(1, i-1)
				l.RawSetInt(1, i) // t[i] = t[i-1]
			}
			l.RawSetInt(1, pos) // t[pos] = v
		default:
			l.Errorf("wrong number of arguments to 'insert'")
		}
		return 0
	}},
	{Name: "pack", Function: func(l *lua.State) int {
		n := l.Top()
		l.CreateTable(n, 1)
		l.PushInteger(n)
		l.SetField(-2, "n")
		if n > 0 {
			l.PushValue(1)
			l.RawSetInt(-2, 1)
			l.Replace(1)
			for i := n; i >= 2; i-- {
				l.RawSetInt(1, i)
			}
		}
		return 1
	}},
	{Name: "unpack", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		i := l.OptInteger(2, 1)
		var e int
		if l.IsNoneOrNil(3) {
			e = l.Len(1)
		} else {
			e = l.CheckInteger(3)
		}
		if i > e {
			return 0
		}
		n := e - i + 1
		if n <= 0 || !l.CheckStack(n) {
			l.Errorf("too many results to unpack")
			panic("unreachable")
		}
		for l.RawGetInt(1, i); i < e; i++ {
			l.RawGetInt(1, i+1)
		}
		return n
	}},
	{Name: "remove", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		size := l.Len(1)
		pos := l.OptInteger(2, size)
		if pos != size {
			l.ArgumentCheck(1 <= pos && pos <= size+1, 2, "position out of bounds")
		}
		for l.RawGetInt(1, pos); pos < size; pos++ {
			l.RawGetInt(1, pos+1)
			l.RawSetInt(1, pos) // t[pos] = t[pos+1]
		}
		l.PushNil()
		l.RawSetInt(1, pos) // t[pos] = nil
		return 1
	}},
	{Name: "sort", Function: func(l *lua.State) int {
		l.CheckType(1, lua.TypeTable)
		n := l.Len(1)
		hasFunction := !l.IsNoneOrNil(2)
		if hasFunction {
			l.CheckType(2, lua.TypeFunction)
		}
		l.SetTop(2)
		comparator := 0
		if hasFunction {
			comparator = 2
		}
		l.SortArray(1, n, comparator)
		return 0
	}},
}

// OpenTable opens the table library. Usually passed to Require.
func OpenTable(l *lua.State) int {
	l.NewLibrary(tableLibrary)
	return 1
}
