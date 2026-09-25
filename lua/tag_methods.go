package lua

import "github.com/matjam/luart/internal/bytecode"

type tm uint

// The events, in Lua 5.4's order: tmAdd plus an ArithOp is that operator's
// event. Tables cache the absence of the events up to tmEq in their flags.
const (
	tmIndex tm = iota
	tmNewIndex
	tmGC
	tmMode
	tmLen
	tmEq
	tmAdd
	tmSub
	tmMul
	tmMod
	tmPow
	tmDiv
	tmIDiv
	tmBAnd
	tmBOr
	tmBXor
	tmShl
	tmShr
	tmUnaryMinus
	tmBNot
	tmLT
	tmLE
	tmConcat
	tmCall
	tmClose
	tmCount // number of tag methods
)

// arithEvent is the event of an arithmetic or bitwise operator.
func arithEvent(op bytecode.ArithOp) tm { return tmAdd + tm(op) }

var eventNames = []string{
	"__index",
	"__newindex",
	"__gc",
	"__mode",
	"__len",
	"__eq",
	"__add",
	"__sub",
	"__mul",
	"__mod",
	"__pow",
	"__div",
	"__idiv",
	"__band",
	"__bor",
	"__bxor",
	"__shl",
	"__shr",
	"__unm",
	"__bnot",
	"__lt",
	"__le",
	"__concat",
	"__call",
	"__close",
}

var typeNames = []string{
	"no value",
	"nil",
	"boolean",
	"userdata",
	"number",
	"string",
	"table",
	"function",
	"userdata",
	"thread",
	"proto", // these last two cases are used for tests only
	"upval",
}

func (events *table) tagMethod(event tm, name string) value {
	tm := events.atString(name)
	//l.assert(event <= tmEq)
	if tm.isNil() {
		events.flags |= 1 << event
	}
	return tm
}

func (l *State) tagMethodByObject(o value, event tm) value {
	var mt *table
	if t := o.table(); t != nil {
		mt = t.metaTable
	} else if d := o.userData(); d != nil {
		mt = d.metaTable
	} else {
		mt = l.global.metaTable(o)
	}
	if mt == nil {
		return nilValue
	}
	return mt.atString(l.global.tagMethodNames[event])
}

func (l *State) callTagMethod(f, p1, p2 value) value {
	l.push(f)
	l.push(p1)
	l.push(p2)
	l.call(l.top-3, 1, l.callInfo.isLua())
	return l.pop()
}

func (l *State) callTagMethodV(f, p1, p2, p3 value) {
	l.push(f)
	l.push(p1)
	l.push(p2)
	l.push(p3)
	l.call(l.top-4, 0, l.callInfo.isLua())
}
