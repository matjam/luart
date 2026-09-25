package luart

import "sort"

// SortArray sorts t[1..n] in place, where t is the table at index, reading
// and writing its elements raw. If comparator is 0 it orders them with
// Lua's < operator, metamethods included; otherwise it calls the function
// at stack index comparator with two elements and treats a true result as
// "less than". It raises "invalid order function for sorting" when the
// order it finds is inconsistent. This is table.sort.
func (l *State) SortArray(index, n, comparator int) {
	h := arraySorter{l: l, t: l.indexToValue(index).table(), n: n}
	if comparator != 0 {
		h.function, h.hasFunction = l.indexToValue(comparator), true
		l.checkStack(3)
	}
	sort.Sort(h)
	if n > 0 && h.Less(n-1, 0) {
		l.Errorf("invalid order function for sorting")
	}
}

// arraySorter sorts for SortArray with the table and comparator resolved
// once: sort calls Less and Swap n log n times, and going through the stack
// API for each was most of the cost.
type arraySorter struct {
	l           *State
	t           *table
	n           int
	function    value // the comparator, when hasFunction
	hasFunction bool
}

func (h arraySorter) Len() int { return h.n }

// Swap and Less convert Go's indices to Lua's.
func (h arraySorter) Swap(i, j int) {
	i++
	j++
	vi, vj := h.t.atInt(i), h.t.atInt(j)
	h.t.putAtInt(i, vj)
	h.t.putAtInt(j, vi)
}

func (h arraySorter) Less(i, j int) bool {
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
