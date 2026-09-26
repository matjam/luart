package lua

import (
	"math"

	"github.com/matjam/luart/internal/bytecode"
)

// Lua 5.5's named vararg tables, function f(...t). When t is only
// indexed, its register holds varArgView, and indexing reads the extra
// arguments (luaT_getvararg); otherwise it is a table {n = #extras, ...},
// and ... reads it (luaT_getvarargs).

// varArgTable makes the vararg table of the extra arguments extras.
func (l *State) varArgTable(extras []value) value {
	t := newTableWithSize(len(extras), 1)
	for i, v := range extras {
		t.putAtInt(i+1, v)
	}
	t.put(l, stringValue("n"), integerValue(int64(len(extras))))
	return objectValue(t)
}

// varArgAt is t[key] for the view of ci's extra arguments: extra argument
// key, their count for "n", or nil.
func varArgAt(l *State, ci *callInfo, key value) value {
	n := ci.base() - ci.function - l.prototype(ci).ParameterCount - 1
	if i, ok := key.integer(); ok {
		if 1 <= i && i <= int64(n) {
			return l.stack[ci.base()-n+int(i)-1]
		}
	} else if s, ok := key.str(); ok && s == "n" {
		return integerValue(int64(n))
	}
	return nilValue
}

// varArgsFromTable runs VARARG i of ci's function, whose named vararg
// table holds its extra arguments: t[1] to t[t.n], which must be a count.
// It returns the frame, which it may move.
func (l *State) varArgsFromTable(ci *callInfo, i bytecode.Instruction) []value {
	p := l.prototype(ci)
	t := ci.frame[p.ParameterCount].table()
	nv := t.at(stringValue("n"))
	if !nv.isInteger() || nv.i() < 0 || nv.i() > math.MaxInt32/2 {
		l.runtimeError("vararg table has no proper 'n'")
	}
	a, n, wanted := i.A(), int(nv.i()), i.B()-1
	if wanted < 0 {
		wanted = n
		l.checkStack(n)
		l.top = ci.base() + a + n
		if ci.top < l.top {
			ci.setTop(l.top)
			ci.frame = l.stack[ci.base():ci.top]
		}
	}
	frame := ci.frame
	for j := range wanted {
		if j < n {
			frame[a+j] = t.atInt(j + 1)
		} else {
			frame[a+j] = nilValue
		}
	}
	return frame
}

// varArgs runs VARARG i of ci's function: its extra arguments, or with a
// named vararg table, the table's. It returns the frame, which it may
// move.
func (l *State) varArgs(ci *callInfo, i bytecode.Instruction) []value {
	p := l.prototype(ci)
	if p.VarArgKind == bytecode.VarArgTable {
		return l.varArgsFromTable(ci, i)
	}
	a, b := i.A(), i.B()-1
	n := ci.base() - ci.function - p.ParameterCount - 1
	if b < 0 {
		b = n // get all var arguments
		l.checkStack(n)
		l.top = ci.base() + a + n
		if ci.top < l.top {
			ci.setTop(l.top)
			ci.frame = l.stack[ci.base():ci.top]
		}
	}
	frame := ci.frame
	for j := range b {
		if j < n {
			frame[a+j] = l.stack[ci.base()-n+j]
		} else {
			frame[a+j] = nilValue
		}
	}
	return frame
}
