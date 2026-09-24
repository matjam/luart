package lua

import (
	"fmt"
	"strings"
)

func (l *State) arith(rb, rc value, op tm) value {
	if b, ok := l.toNumber(rb); ok {
		if c, ok := l.toNumber(rc); ok {
			return numberValue(arith(Operator(op-tmAdd)+OpAdd, b, c))
		}
	}
	if result, ok := l.callBinaryTagMethod(rb, rc, op); ok {
		return result
	}
	l.arithError(rb, rc)
	return nilValue
}

func isCallable(v value) bool {
	switch v.o.(type) {
	case closure, *goFunction:
		return true
	}
	return false
}

func (l *State) tableAt(t value, key value) value {
	for range maxTagLoop {
		var tm value
		if table, ok := t.o.(*table); ok {
			if result := table.at(key); !result.isNil() {
				return result
			} else if tm = l.fastTagMethod(table.metaTable, tmIndex); tm.isNil() {
				return nilValue
			}
		} else if tm = l.tagMethodByObject(t, tmIndex); tm.isNil() {
			l.typeError(t, "index")
		}
		if isCallable(tm) {
			return l.callTagMethod(tm, t, key)
		}
		t = tm
	}
	l.runtimeError("loop in table")
	return nilValue
}

func (l *State) setTableAt(t value, key value, val value) {
	for range maxTagLoop {
		var tm value
		if table, ok := t.o.(*table); ok {
			if table.tryPut(l, key, val) {
				// previous non-nil value ==> metamethod irrelevant
				table.invalidateTagMethodCache()
				return
			} else if tm = l.fastTagMethod(table.metaTable, tmNewIndex); tm.isNil() {
				// no metamethod
				table.put(l, key, val)
				table.invalidateTagMethodCache()
				return
			}
		} else if tm = l.tagMethodByObject(t, tmNewIndex); tm.isNil() {
			l.typeError(t, "index")
		}
		if isCallable(tm) {
			l.callTagMethodV(tm, t, key, val)
			return
		}
		t = tm
	}
	l.runtimeError("loop in setTable")
}

func (l *State) objectLength(v value) value {
	var tm value
	switch o := v.o.(type) {
	case *table:
		if tm = l.fastTagMethod(o.metaTable, tmLen); tm.isNil() {
			return numberValue(float64(o.length()))
		}
	case string:
		return numberValue(float64(len(o)))
	default:
		if tm = l.tagMethodByObject(v, tmLen); tm.isNil() {
			l.typeError(v, "get length of")
		}
	}
	return l.callTagMethod(tm, v, v)
}

func (l *State) equalTagMethod(mt1, mt2 *table, event tm) value {
	if tm1 := l.fastTagMethod(mt1, event); tm1.isNil() { // no metamethod
	} else if mt1 == mt2 { // same metatables => same metamethods
		return tm1
	} else if tm2 := l.fastTagMethod(mt2, event); tm2.isNil() { // no metamethod
	} else if tm1 == tm2 { // same metamethods
		return tm1
	}
	return nilValue
}

func (l *State) equalObjects(t1, t2 value) bool {
	var tm value
	switch o1 := t1.o.(type) {
	case *numberTag:
		n2, ok := t2.number()
		return ok && t1.n == n2
	case *userData:
		if t1 == t2 {
			return true
		} else if o2, ok := t2.o.(*userData); ok {
			tm = l.equalTagMethod(o1.metaTable, o2.metaTable, tmEq)
		}
	case *table:
		if t1 == t2 {
			return true
		} else if o2, ok := t2.o.(*table); ok {
			tm = l.equalTagMethod(o1.metaTable, o2.metaTable, tmEq)
		}
	default:
		return t1 == t2
	}
	return !tm.isNil() && !isFalse(l.callTagMethod(tm, t1, t2))
}

func (l *State) callBinaryTagMethod(p1, p2 value, event tm) (value, bool) {
	tm := l.tagMethodByObject(p1, event)
	if tm.isNil() {
		tm = l.tagMethodByObject(p2, event)
	}
	if tm.isNil() {
		return nilValue, false
	}
	return l.callTagMethod(tm, p1, p2), true
}

func (l *State) callOrderTagMethod(left, right value, event tm) (bool, bool) {
	result, ok := l.callBinaryTagMethod(left, right, event)
	return !isFalse(result), ok
}

func (l *State) lessThan(left, right value) bool {
	if lf, ok := left.number(); ok {
		if rf, ok := right.number(); ok {
			return lf < rf
		}
	} else if ls, ok := left.str(); ok {
		if rs, ok := right.str(); ok {
			return ls < rs
		}
	}
	if result, ok := l.callOrderTagMethod(left, right, tmLT); ok {
		return result
	}
	l.orderError(left, right)
	return false
}

func (l *State) lessOrEqual(left, right value) bool {
	if lf, ok := left.number(); ok {
		if rf, ok := right.number(); ok {
			return lf <= rf
		}
	} else if ls, ok := left.str(); ok {
		if rs, ok := right.str(); ok {
			return ls <= rs
		}
	}
	if result, ok := l.callOrderTagMethod(left, right, tmLE); ok {
		return result
	} else if result, ok := l.callOrderTagMethod(right, left, tmLT); ok {
		return !result
	}
	l.orderError(left, right)
	return false
}

func (l *State) concat(total int) {
	t := func(i int) value { return l.stack[l.top-i] }
	put := func(i int, v value) { l.stack[l.top-i] = v }
	concatTagMethod := func() {
		if v, ok := l.callBinaryTagMethod(t(2), t(1), tmConcat); !ok {
			l.concatError(t(2), t(1))
		} else {
			put(2, v)
		}
	}
	l.assert(total >= 2)
	for total > 1 {
		n := 2 // # of elements handled in this pass (at least 2)
		s2, ok := t(2).str()
		if !ok {
			ok = t(2).isNumber()
		}
		if !ok {
			concatTagMethod()
		} else if s1, ok := l.toString(l.top - 1); !ok {
			concatTagMethod()
		} else if len(s1) == 0 {
			v, _ := l.toString(l.top - 2)
			put(2, stringValue(v))
		} else if s2, ok = t(2).str(); ok && len(s2) == 0 {
			put(2, t(1))
		} else {
			// at least 2 non-empty strings; scarf as many as possible
			ss := []string{s1}
			for ; n <= total; n++ {
				if s, ok := l.toString(l.top - n); ok {
					ss = append(ss, s)
				} else {
					break
				}
			}
			n-- // last increment wasn't valid
			for i, j := 0, len(ss)-1; i < j; i, j = i+1, j-1 {
				ss[i], ss[j] = ss[j], ss[i]
			}
			put(len(ss), stringValue(strings.Join(ss, "")))
		}
		total -= n - 1 // created 1 new string from `n` strings
		l.top -= n - 1 // popped `n` strings and pushed 1
	}
}

func (l *State) traceExecution() {
	callInfo := l.callInfo
	mask := l.hookMask
	countHook := mask&MaskCount != 0 && l.hookCount == 0
	if countHook {
		l.resetHookCount()
	}
	if callInfo.isCallStatus(callStatusHookYielded) {
		callInfo.clearCallStatus(callStatusHookYielded)
		return
	}
	if countHook {
		l.hook(HookCount, -1)
	}
	if mask&MaskLine != 0 {
		p := l.prototype(callInfo)
		npc := callInfo.savedPC - 1
		newline := p.lineInfo[npc]
		if npc == 0 || callInfo.savedPC <= l.oldPC || newline != p.lineInfo[l.oldPC-1] {
			l.hook(HookLine, int(newline))
		}
	}
	l.oldPC = callInfo.savedPC
	if l.shouldYield {
		if countHook {
			l.hookCount = 1
		}
		callInfo.savedPC--
		callInfo.setCallStatus(callStatusHookYielded)
		callInfo.function = l.top - 1
		panic("Not implemented - use goroutines to emulate yield")
	}
}

func (l *State) execute() { l.executeSwitch() }

// callLua starts a call from register a of ci to f, a Lua function that
// takes a fixed number of parameters, with argCount arguments. It is
// preCall's Lua branch without the dispatch, varargs and call hook, and
// returns the new frame.
func (l *State) callLua(ci *callInfo, f *luaClosure, a, argCount, resultCount int) *callInfo {
	p := f.prototype
	function := ci.stackIndex(a)
	l.top = function + 1 + argCount
	l.checkStack(p.maxStackSize)
	if argCount < p.parameterCount {
		clear(l.stack[l.top : function+1+p.parameterCount])
	}
	nci := l.pushLuaFrame(function, function+1, resultCount, f)
	nci.setCallStatus(callStatusReentry)
	return nci
}

// numberResult stores the results of a frameless number function call at
// register a, as postCall would for a Go function returning results values.
func (l *State) numberResult(ci *callInfo, a, wanted, results int, r float64) {
	frame := ci.frame
	if wanted == MultipleReturns {
		if results == 1 {
			frame[a] = numberValue(r)
		}
		l.top = ci.stackIndex(a + results)
		return
	}
	if wanted > 0 {
		if results == 1 {
			frame[a] = numberValue(r)
		} else {
			frame[a] = nilValue
		}
		if wanted > 1 {
			clear(frame[a+1 : a+wanted])
		}
	}
	l.top = ci.top
}

func k(field int, constants []value, frame []value) value {
	if 0 != field&bitRK { // OPT: Inline isConstant(field).
		return constants[field & ^bitRK] // OPT: Inline constantIndex(field).
	}
	return frame[field]
}

func newFrame(l *State, ci *callInfo) (frame []value, closure *luaClosure, constants []value) {
	// TODO l.assert(ci == l.callInfo)
	frame, closure = ci.frame, ci.closure
	constants = closure.prototype.constants
	return
}

func expectOp(i instruction, expected opCode) instruction {
	if op := i.opCode(); op != expected {
		panic(fmt.Sprintf("expected opcode %s, got %s", opNames[expected], opNames[op]))
	}
	return i
}

// jumpFrom runs jump instruction j, which precedes ip, and returns the new ip.
func (l *State) jumpFrom(ci *callInfo, j instruction, ip pc) pc {
	if a := j.a(); a > 0 {
		l.close(ci.stackIndex(a - 1))
	}
	return ip + pc(j.sbx())
}

// executeSwitch runs Lua functions from l.callInfo until it returns to its
// caller. It keeps the instruction pointer in ip and stores it in
// ci.savedPC as each instruction starts, where errors and hooks read it.
// After a call or return changes ci, ip reloads from ci.savedPC.
func (l *State) executeSwitch() {
	ci := l.callInfo
	frame, closure, constants := newFrame(l, ci)
	code, ip := closure.prototype.execCode(), ci.savedPC
	for {
		i := code[ip]
		ip++
		ci.savedPC = ip
		if l.hookMask&(MaskLine|MaskCount) != 0 {
			if l.hookCount--; l.hookCount == 0 || l.hookMask&MaskLine != 0 {
				l.traceExecution()
				frame = ci.frame
			}
		}
		switch i.opCode() {
		case opMove:
			frame[i.a()] = frame[i.b()]
		case opLoadConstant:
			frame[i.a()] = constants[i.bx()]
		case opLoadConstantEx:
			frame[i.a()] = constants[expectOp(code[ip], opExtraArg).ax()]
			ip++
		case opLoadBool:
			frame[i.a()] = boolValue(i.b() != 0)
			if i.c() != 0 {
				ip++
			}
		case opLoadNil:
			a, b := i.a(), i.b()
			clear(frame[a : a+b+1])
		case opGetUpValue:
			frame[i.a()] = closure.upValue(i.b())
		case opGetTableUp:
			tmp := l.tableAt(closure.upValue(i.b()), k(i.c(), constants, frame))
			frame = ci.frame
			frame[i.a()] = tmp
		case opGetTable:
			tmp := l.tableAt(frame[i.b()], k(i.c(), constants, frame))
			frame = ci.frame
			frame[i.a()] = tmp
		case opSetTableUp:
			l.setTableAt(closure.upValue(i.a()), k(i.b(), constants, frame), k(i.c(), constants, frame))
			frame = ci.frame
		case opSetUpValue:
			closure.setUpValue(i.b(), frame[i.a()])
		case opSetTable:
			l.setTableAt(frame[i.a()], k(i.b(), constants, frame), k(i.c(), constants, frame))
			frame = ci.frame
		case opNewTable:
			a := i.a()
			b, c := float8(i.b()), float8(i.c())
			frame[a] = objectValue(newTableAt(&closure.prototype.fields[ip-1], intFromFloat8(b), intFromFloat8(c)))
			clear(frame[a+1:])
		case opSelf:
			a, t := i.a(), frame[i.b()]
			tmp := l.tableAt(t, k(i.c(), constants, frame))
			frame = ci.frame
			frame[a+1], frame[a] = t, tmp
		case opGetField:
			t, key := frame[i.b()], constants[i.c()]
			if v, ok := getField(t, key, &closure.prototype.fields[ip-1]); ok {
				frame[i.a()] = v
				break
			}
			tmp := l.tableAt(t, key)
			frame = ci.frame
			frame[i.a()] = tmp
		case opGetFieldUp:
			t, key := closure.upValue(i.b()), constants[i.c()]
			if v, ok := getField(t, key, &closure.prototype.fields[ip-1]); ok {
				frame[i.a()] = v
				break
			}
			tmp := l.tableAt(t, key)
			frame = ci.frame
			frame[i.a()] = tmp
		case opSelfField:
			a, t, key := i.a(), frame[i.b()], constants[i.c()]
			if v, ok := getField(t, key, &closure.prototype.fields[ip-1]); ok {
				frame[a+1], frame[a] = t, v
				break
			}
			tmp := l.tableAt(t, key)
			frame = ci.frame
			frame[a+1], frame[a] = t, tmp
		case opSetField:
			t, key, v := frame[i.a()], constants[i.b()], k(i.c(), constants, frame)
			if !setField(t, key, v, &closure.prototype.fields[ip-1]) {
				l.setTableAt(t, key, v)
				frame = ci.frame
			}
		case opSetFieldUp:
			t, key, v := closure.upValue(i.a()), constants[i.b()], k(i.c(), constants, frame)
			if !setField(t, key, v, &closure.prototype.fields[ip-1]) {
				l.setTableAt(t, key, v)
				frame = ci.frame
			}
		case opAdd:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			if b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n + c.n)
				break
			}
			tmp := l.arith(b, c, tmAdd)
			frame = ci.frame
			frame[i.a()] = tmp
		case opSub:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			if b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n - c.n)
				break
			}
			tmp := l.arith(b, c, tmSub)
			frame = ci.frame
			frame[i.a()] = tmp
		case opMul:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			if b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n * c.n)
				break
			}
			tmp := l.arith(b, c, tmMul)
			frame = ci.frame
			frame[i.a()] = tmp
		case opDiv:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			if b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n / c.n)
				break
			}
			tmp := l.arith(b, c, tmDiv)
			frame = ci.frame
			frame[i.a()] = tmp
		case opMod:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			if b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(arith(OpMod, b.n, c.n))
				break
			}
			tmp := l.arith(b, c, tmMod)
			frame = ci.frame
			frame[i.a()] = tmp
		case opPow:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			if b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(arith(OpPow, b.n, c.n))
				break
			}
			tmp := l.arith(b, c, tmPow)
			frame = ci.frame
			frame[i.a()] = tmp
		case opAddRR:
			if b, c := frame[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n + c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmAdd)
			}
		case opAddRK:
			if b, c := frame[i.b()], constants[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n + c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmAdd)
			}
		case opAddKR:
			if b, c := constants[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n + c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmAdd)
			}
		case opMulAddRKR:
			if b, c := frame[i.b()], constants[i.c()]; b.isNumber() && c.isNumber() {
				r := b.n * c.n
				frame[i.a()] = numberValue(r)
				if j := code[ip]; l.hookMask&(MaskLine|MaskCount) == 0 {
					if d := frame[j.c()]; d.isNumber() {
						frame[j.a()] = numberValue(r + d.n)
						ip++
					}
				}
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmMul)
			}
		case opSubRR:
			if b, c := frame[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n - c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmSub)
			}
		case opSubRK:
			if b, c := frame[i.b()], constants[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n - c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmSub)
			}
		case opSubKR:
			if b, c := constants[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n - c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmSub)
			}
		case opMulRR:
			if b, c := frame[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n * c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmMul)
			}
		case opMulRK:
			if b, c := frame[i.b()], constants[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n * c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmMul)
			}
		case opMulKR:
			if b, c := constants[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n * c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmMul)
			}
		case opDivRR:
			if b, c := frame[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n / c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmDiv)
			}
		case opDivRK:
			if b, c := frame[i.b()], constants[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n / c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmDiv)
			}
		case opDivKR:
			if b, c := constants[i.b()], frame[i.c()]; b.isNumber() && c.isNumber() {
				frame[i.a()] = numberValue(b.n / c.n)
			} else {
				frame = l.arithInto(ci, i.a(), b, c, tmDiv)
			}
		case opUnaryMinus:
			if b := frame[i.b()]; b.isNumber() {
				frame[i.a()] = numberValue(-b.n)
			} else {
				tmp := l.arith(b, b, tmUnaryMinus)
				frame = ci.frame
				frame[i.a()] = tmp
			}
		case opNot:
			frame[i.a()] = boolValue(isFalse(frame[i.b()]))
		case opLength:
			tmp := l.objectLength(frame[i.b()])
			frame = ci.frame
			frame[i.a()] = tmp
		case opConcat:
			a, b, c := i.a(), i.b(), i.c()
			l.top = ci.stackIndex(c + 1) // mark the end of concat operands
			l.concat(c - b + 1)
			frame = ci.frame
			frame[a] = frame[b]
			if a >= b { // limit of live values
				clear(frame[a+1:])
			} else {
				clear(frame[b:])
			}
		case opJump:
			ip = l.jumpFrom(ci, i, ip)
		case opEqual:
			if l.equalObjects(k(i.b(), constants, frame), k(i.c(), constants, frame)) == (i.a() != 0) {
				ip = l.jumpFrom(ci, code[ip], ip+1)
			} else {
				ip++
			}
			frame = ci.frame
		case opLessThan:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			var less bool
			if b.isNumber() && c.isNumber() {
				less = b.n < c.n
			} else {
				less = l.lessThan(b, c)
				frame = ci.frame
			}
			if less == (i.a() != 0) {
				ip = l.jumpFrom(ci, code[ip], ip+1)
			} else {
				ip++
			}
		case opLessOrEqual:
			b, c := k(i.b(), constants, frame), k(i.c(), constants, frame)
			var lessOrEqual bool
			if b.isNumber() && c.isNumber() {
				lessOrEqual = b.n <= c.n
			} else {
				lessOrEqual = l.lessOrEqual(b, c)
				frame = ci.frame
			}
			if lessOrEqual == (i.a() != 0) {
				ip = l.jumpFrom(ci, code[ip], ip+1)
			} else {
				ip++
			}
		case opTest:
			if isFalse(frame[i.a()]) == (i.c() == 0) {
				ip = l.jumpFrom(ci, code[ip], ip+1)
			} else {
				ip++
			}
		case opTestSet:
			if b := frame[i.b()]; isFalse(b) == (i.c() == 0) {
				frame[i.a()] = b
				ip = l.jumpFrom(ci, code[ip], ip+1)
			} else {
				ip++
			}
		case opCall:
			a, b, c := i.a(), i.b(), i.c()
			if b > 1 && l.hookMask&(MaskCall|MaskReturn) == 0 {
				if f, ok := frame[a].o.(*goFunction); ok && f.number != nil {
					if nf := f.number; nf.unary != nil && b == 2 && c == 2 && frame[a+1].isNumber() {
						frame[a] = numberValue(nf.unary(frame[a+1].n))
						l.top = ci.top
						break
					}
					if r, ok := f.number.tryCall(frame[a+1 : a+b]); ok {
						l.numberResult(ci, a, c-1, f.number.results, r)
						break
					}
				}
			}
			if b != 0 && l.hookMask&MaskCall == 0 {
				switch f := frame[a].o.(type) {
				case *luaClosure:
					if !f.prototype.isVarArg {
						ci = l.callLua(ci, f, a, b-1, c-1)
						frame, closure, constants = ci.frame, f, f.prototype.constants
						code, ip = f.prototype.execCode(), 0
						continue
					}
				case *goFunction, *goClosure:
					l.top = ci.stackIndex(a + b)
					l.callGo(frame[a], ci.stackIndex(a), c-1)
					if c != 0 {
						l.top = ci.top // adjust results
					}
					frame = ci.frame
					continue
				}
			}
			if b != 0 {
				l.top = ci.stackIndex(a + b)
			} // else previous instruction set top
			if n := c - 1; l.preCall(ci.stackIndex(a), n) { // go function
				if n >= 0 {
					l.top = ci.top // adjust results
				}
				frame = ci.frame
			} else { // lua function
				ci = l.callInfo
				ci.setCallStatus(callStatusReentry)
				frame, closure, constants = newFrame(l, ci)
				code, ip = closure.prototype.execCode(), ci.savedPC
			}
		case opTailCall:
			a, b := i.a(), i.b()
			if b != 0 {
				l.top = ci.stackIndex(a + b)
			} // else previous instruction set top
			// TODO l.assert(i.c()-1 == MultipleReturns)
			if l.preCall(ci.stackIndex(a), MultipleReturns) { // go function
				frame = ci.frame
			} else {
				// tail call: put called frame (n) in place of caller one (o)
				nci := l.callInfo                      // called frame
				oci := nci.previous                    // caller frame
				nfn, ofn := nci.function, oci.function // called & caller function
				// last stack slot filled by 'precall'
				lim := nci.base() + l.stack[nfn].o.(*luaClosure).prototype.parameterCount
				if len(closure.prototype.prototypes) > 0 { // close all upvalues from previous call
					l.close(oci.base())
				}
				// move new frame into old one
				for i := 0; nfn+i < lim; i++ {
					l.stack[ofn+i] = l.stack[nfn+i]
				}
				base := ofn + (nci.base() - nfn) // correct base
				oci.setTop(ofn + (l.top - nfn))  // correct top
				oci.frame = l.stack[base:oci.top]
				oci.savedPC, oci.code, oci.closure = nci.savedPC, nci.code, nci.closure // correct code (savedPC indexes nci->code)
				oci.setCallStatus(callStatusTail)                                       // function was tail called
				l.top, l.callInfo, ci = oci.top, oci, oci
				// TODO l.assert(l.top == oci.base()+l.stack[ofn].(*luaClosure).prototype.maxStackSize)
				// TODO l.assert(&oci.frame[0] == &l.stack[oci.base()] && len(oci.frame) == oci.top-oci.base())
				frame, closure, constants = newFrame(l, ci)
				code, ip = closure.prototype.execCode(), ci.savedPC
			}
		case opReturn:
			a := i.a()
			if b, wanted := i.b(), ci.resultCount; b != 0 && wanted >= 0 && l.hookMask&(MaskReturn|MaskLine) == 0 && ci.isCallStatus(callStatusReentry) {
				// Fixed results into a Lua caller that wants a fixed count.
				if len(closure.prototype.prototypes) > 0 {
					l.close(ci.base())
				}
				res, results := l.stack[ci.function:ci.function+wanted], frame[a:a+b-1]
				n := copy(res, results)
				clear(res[n:])
				ci = ci.previous
				l.callInfo, l.top = ci, ci.top
				frame, closure, constants = newFrame(l, ci)
				code, ip = closure.prototype.execCode(), ci.savedPC
				break
			}
			if b := i.b(); b != 0 {
				l.top = ci.stackIndex(a + b - 1)
			}
			if len(closure.prototype.prototypes) > 0 {
				l.close(ci.base())
			}
			n := l.postCall(ci.stackIndex(a))
			if !ci.isCallStatus(callStatusReentry) { // ci still the called one?
				return // external invocation: return
			}
			ci = l.callInfo
			if n {
				l.top = ci.top
			}
			// TODO l.assert(ci.code[ci.savedPC-1].opCode() == opCall)
			frame, closure, constants = newFrame(l, ci)
			code, ip = closure.prototype.execCode(), ci.savedPC
		case opForLoop:
			a := i.a()
			index, limit, step := frame[a+0].n, frame[a+1].n, frame[a+2].n
			if index += step; (0 < step && index <= limit) || (step <= 0 && limit <= index) {
				ip += pc(i.sbx())
				frame[a+0] = numberValue(index) // update internal index...
				frame[a+3] = numberValue(index) // ... and external index
			}
		case opForPrep:
			a := i.a()
			if init, ok := l.toNumber(frame[a+0]); !ok {
				l.runtimeError("'for' initial value must be a number")
			} else if limit, ok := l.toNumber(frame[a+1]); !ok {
				l.runtimeError("'for' limit must be a number")
			} else if step, ok := l.toNumber(frame[a+2]); !ok {
				l.runtimeError("'for' step must be a number")
			} else {
				frame[a+0], frame[a+1], frame[a+2] = numberValue(init-step), numberValue(limit), numberValue(step)
				ip += pc(i.sbx())
			}
		case opTForCall:
			a := i.a()
			callBase := a + 3
			copy(frame[callBase:callBase+3], frame[a:a+3])
			callBase += ci.base()
			l.top = callBase + 3 // function + 2 args (state and index)
			l.call(callBase, i.c(), true)
			frame, l.top = ci.frame, ci.top
			i = expectOp(code[ip], opTForLoop) // go to next instruction
			ip++
			ci.savedPC = ip
			fallthrough
		case opTForLoop:
			if a := i.a(); !frame[a+1].isNil() { // continue loop?
				frame[a] = frame[a+1] // save control variable
				ip += pc(i.sbx())     // jump back
			}
		case opSetList:
			a, n, c := i.a(), i.b(), i.c()
			if n == 0 {
				n = l.top - ci.stackIndex(a) - 1
			}
			if c == 0 {
				c = expectOp(code[ip], opExtraArg).ax()
				ip++
			}
			h := frame[a].o.(*table)
			start := (c - 1) * listItemsPerFlush
			last := start + n
			if last > len(h.array) {
				h.extendArray(last)
			}
			copy(h.array[start:last], frame[a+1:a+1+n])
			l.top = ci.top
		case opClosure:
			a, p := i.a(), &closure.prototype.prototypes[i.bx()]
			if ncl := cached(p, closure.upValues, ci.base()); ncl == nil { // no match?
				frame[a] = l.newClosure(p, closure.upValues, ci.base()) // create a new one
			} else {
				frame[a] = objectValue(ncl)
			}
			clear(frame[a+1:])
		case opVarArg:
			a, b := i.a(), i.b()-1
			n := ci.base() - ci.function - closure.prototype.parameterCount - 1
			if b < 0 {
				b = n // get all var arguments
				l.checkStack(n)
				l.top = ci.base() + a + n
				if ci.top < l.top {
					ci.setTop(l.top)
					ci.frame = l.stack[ci.base():ci.top]
				}
				frame = ci.frame
			}
			for j := range b {
				if j < n {
					frame[a+j] = l.stack[ci.base()-n+j]
				} else {
					frame[a+j] = nilValue
				}
			}
		case opExtraArg:
			panic(fmt.Sprintf("unexpected opExtraArg instruction, '%s'", i.String()))
		}
	}
}
