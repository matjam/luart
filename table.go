package luart

import (
	"fmt"
	"sort"
	"strings"
)

// sortHelper sorts t[1..n] for table.sort. It works on the table and the
// stack directly, as RawGetInt, RawSetInt, Call and Compare would: sort
// calls Less and Swap n log n times, and resolving stack indices through
// the API each time was most of the cost.
type sortHelper struct {
	l           *State
	t           *table
	n           int
	function    value // the comparator, when hasFunction
	hasFunction bool
}

func (h sortHelper) Len() int { return h.n }

func (h sortHelper) Swap(i, j int) {
	// Convert Go to Lua indices
	i++
	j++
	vi, vj := h.t.atInt(i), h.t.atInt(j)
	h.t.putAtInt(i, vj)
	h.t.putAtInt(j, vi)
}

func (h sortHelper) Less(i, j int) bool {
	// Convert Go to Lua indices
	i++
	j++
	l := h.l
	a, b := h.t.atInt(i), h.t.atInt(j)
	if h.hasFunction {
		f := l.top
		l.stack[f], l.stack[f+1], l.stack[f+2] = h.function, a, b
		l.top = f + 3
		l.call(f, 1, false)
		l.top--
		return !isFalse(l.stack[l.top])
	}
	if a.isNil() || b.isNil() {
		return false
	}
	return l.lessThan(a, b)
}

var tableLibrary = []RegistryFunction{
	{"concat", func(l *State) int {
		CheckType(l, 1, TypeTable)
		sep := OptString(l, 2, "")
		i := OptInteger(l, 3, 1)
		var last int
		if l.IsNoneOrNil(4) {
			last = LengthEx(l, 1)
		} else {
			last = CheckInteger(l, 4)
		}
		var s strings.Builder
		addField := func() {
			l.RawGetInt(1, i)
			if str, ok := l.ToString(-1); ok {
				s.WriteString(str)
			} else {
				Errorf(l, fmt.Sprintf("invalid value (%s) at index %d in table for 'concat'", TypeNameOf(l, -1), i))
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
	{"insert", func(l *State) int {
		CheckType(l, 1, TypeTable)
		e := LengthEx(l, 1) + 1 // First empty element.
		switch l.Top() {
		case 2:
			l.RawSetInt(1, e) // Insert new element at the end.
		case 3:
			pos := CheckInteger(l, 2)
			ArgumentCheck(l, 1 <= pos && pos <= e, 2, "position out of bounds")
			for i := e; i > pos; i-- {
				l.RawGetInt(1, i-1)
				l.RawSetInt(1, i) // t[i] = t[i-1]
			}
			l.RawSetInt(1, pos) // t[pos] = v
		default:
			Errorf(l, "wrong number of arguments to 'insert'")
		}
		return 0
	}},
	{"pack", func(l *State) int {
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
	{"unpack", func(l *State) int {
		CheckType(l, 1, TypeTable)
		i := OptInteger(l, 2, 1)
		var e int
		if l.IsNoneOrNil(3) {
			e = LengthEx(l, 1)
		} else {
			e = CheckInteger(l, 3)
		}
		if i > e {
			return 0
		}
		n := e - i + 1
		if n <= 0 || !l.CheckStack(n) {
			Errorf(l, "too many results to unpack")
			panic("unreachable")
		}
		for l.RawGetInt(1, i); i < e; i++ {
			l.RawGetInt(1, i+1)
		}
		return n
	}},
	{"remove", func(l *State) int {
		CheckType(l, 1, TypeTable)
		size := LengthEx(l, 1)
		pos := OptInteger(l, 2, size)
		if pos != size {
			ArgumentCheck(l, 1 <= pos && pos <= size+1, 2, "position out of bounds")
		}
		for l.RawGetInt(1, pos); pos < size; pos++ {
			l.RawGetInt(1, pos+1)
			l.RawSetInt(1, pos) // t[pos] = t[pos+1]
		}
		l.PushNil()
		l.RawSetInt(1, pos) // t[pos] = nil
		return 1
	}},
	{"sort", func(l *State) int {
		CheckType(l, 1, TypeTable)
		n := LengthEx(l, 1)
		hasFunction := !l.IsNoneOrNil(2)
		if hasFunction {
			CheckType(l, 2, TypeFunction)
		}
		l.SetTop(2)
		h := sortHelper{l: l, t: l.indexToValue(1).table(), n: n, hasFunction: hasFunction}
		if hasFunction {
			h.function = l.indexToValue(2)
		}
		sort.Sort(h)
		// Check result is sorted.
		if n > 0 && h.Less(n-1, 0) {
			Errorf(l, "invalid order function for sorting")
		}
		return 0
	}},
}

// TableOpen opens the table library. Usually passed to Require.
func TableOpen(l *State) int {
	NewLibrary(l, tableLibrary)
	return 1
}
