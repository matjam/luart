package lua

import "log"

func (l *State) push(v value) {
	l.stack[l.top] = v
	l.top++
}

func (l *State) pop() value {
	l.top--
	return l.stack[l.top]
}

// upValue is open while state is non-nil, and then refers to
// state.stack[index]. Closing it copies that slot into closed. Open upvalues
// form a list through next, sorted by index from the top of the stack down,
// as in C Lua.
type upValue struct {
	state  *State
	index  int
	closed value
	next   *upValue
}

type closure interface {
	upValue(i int) value
	setUpValue(i int, v value)
	upValueCount() int
}

type luaClosure struct {
	prototype *prototype
	upValues  []*upValue
	inline    [2]*upValue // backs upValues for closures with up to two
	own       upValue     // storage for the first upvalue this closure opens
}

type goClosure struct {
	function Function
	upValues []value
}

// Function wrapper, to allow go functions as keys in maps. Explicitly not a closure.
type goFunction struct {
	Function
	number *numberFunction // non-nil when the VM may call it frameless
}

func (c *luaClosure) upValue(i int) value { return c.upValues[i].value() }

func (c *luaClosure) setUpValue(i int, v value) {
	if uv := c.upValues[i]; uv.state != nil {
		uv.state.stack[uv.index] = v
	} else {
		uv.closed = v
	}
}

func (c *luaClosure) upValueCount() int        { return len(c.upValues) }
func (c *goClosure) upValue(i int) value       { return c.upValues[i] }
func (c *goClosure) setUpValue(i int, v value) { c.upValues[i] = v }
func (c *goClosure) upValueCount() int         { return len(c.upValues) }
func (l *State) newUpValue() *upValue          { return &upValue{} }

func (uv *upValue) value() value {
	if uv.state != nil {
		return uv.state.stack[uv.index]
	}
	return uv.closed
}

func (uv *upValue) close() {
	if uv.state == nil {
		panic("attempt to close already-closed up value")
	}
	uv.closed, uv.state = uv.state.stack[uv.index], nil
}

func (uv *upValue) isInStackAt(level int) bool { return uv.state != nil && uv.index == level }

// sameHome reports whether uv and o refer to the same stack slot, or are
// both closed over equal values.
func (uv *upValue) sameHome(o *upValue) bool {
	if uv.state != nil || o.state != nil {
		return uv.state == o.state && uv.index == o.index
	}
	return uv.closed.identical(o.closed)
}

// close closes the open upvalues at or above stack index level, which head
// the sorted list.
func (l *State) close(level int) {
	for uv := l.upValues; uv != nil && uv.index >= level; uv = l.upValues {
		l.upValues, uv.next = uv.next, nil
		uv.close()
	}
}

// information about a call
type callInfo struct {
	function, top, resultCount int
	previous, next             *callInfo
	callStatus                 callStatus
	*luaCallInfo
	*goCallInfo
}

type luaCallInfo struct {
	frame   []value
	savedPC pc
	code    []instruction
	closure *luaClosure // the running function, as found at stack[function]
}

type goCallInfo struct {
	context, extra, oldErrorFunction int
	continuation                     Function
	oldAllowHook, shouldYield        bool
	error                            error
}

func (ci *callInfo) setCallStatus(flag callStatus)     { ci.callStatus |= flag }
func (ci *callInfo) clearCallStatus(flag callStatus)   { ci.callStatus &^= flag }
func (ci *callInfo) isCallStatus(flag callStatus) bool { return ci.callStatus&flag != 0 }
func (ci *callInfo) isLua() bool                       { return ci.callStatus&callStatusLua != 0 }

func (ci *callInfo) stackIndex(slot int) int { return ci.top - len(ci.frame) + slot }
func (ci *callInfo) base() int               { return ci.top - len(ci.frame) }
func (ci *callInfo) skip()                   { ci.savedPC++ }
func (ci *callInfo) jump(offset int)         { ci.savedPC += pc(offset) }

// hasFrame reports whether ci.frame tracks the stack: true for Lua frames
// and for the base frame. A Go frame keeps a stale luaCallInfo from an
// earlier Lua call so the slot can switch kinds without allocating.
func (ci *callInfo) hasFrame() bool {
	return ci.luaCallInfo != nil && (ci.isLua() || ci.goCallInfo == nil)
}

func (ci *callInfo) setTop(top int) {
	if ci.hasFrame() {
		diff := top - ci.top
		ci.frame = ci.frame[:len(ci.frame)+diff]
	}
	ci.top = top
}

func (ci *callInfo) frameIndex(stackSlot int) int {
	if stackSlot < ci.top-len(ci.frame) || ci.top <= stackSlot {
		panic("frameIndex called with out-of-range stackSlot")
	}
	return stackSlot - ci.top + len(ci.frame)
}

func (l *State) pushLuaFrame(function, base, resultCount int, c *luaClosure) *callInfo {
	p := c.prototype
	ci := l.callInfo.next
	if ci == nil {
		ci = &callInfo{previous: l.callInfo, luaCallInfo: &luaCallInfo{}}
		l.callInfo.next = ci
	} else if ci.luaCallInfo == nil {
		ci.luaCallInfo = &luaCallInfo{}
	}
	ci.savedPC, ci.code, ci.closure = 0, p.code, c
	ci.function = function
	ci.top = base + p.maxStackSize
	// TODO l.assert(ci.top <= l.stackLast)
	ci.resultCount = resultCount
	ci.callStatus = callStatusLua
	ci.frame = l.stack[base:ci.top]
	l.callInfo = ci
	l.top = ci.top
	return ci
}

func (l *State) pushGoFrame(function, resultCount int) {
	ci := l.callInfo.next
	if ci == nil {
		ci = &callInfo{previous: l.callInfo, goCallInfo: &goCallInfo{}}
		l.callInfo.next = ci
	} else if ci.goCallInfo == nil {
		ci.goCallInfo = &goCallInfo{}
	}
	ci.function = function
	ci.top = l.top + MinStack
	// TODO l.assert(ci.top <= l.stackLast)
	ci.resultCount = resultCount
	ci.callStatus = 0
	l.callInfo = ci
}

func (ci *luaCallInfo) step() instruction {
	i := ci.code[ci.savedPC]
	ci.savedPC++
	return i
}

func (l *State) newLuaClosure(p *prototype) *luaClosure {
	c := &luaClosure{prototype: p}
	if n := len(p.upValues); n <= len(c.inline) {
		c.upValues = c.inline[:n]
	} else {
		c.upValues = make([]*upValue, n)
	}
	return c
}

// findUpValue returns the open upvalue for stack index level, creating it
// in sorted position if there is none. A new upvalue uses storage when it
// is non-nil.
func (l *State) findUpValue(level int, storage *upValue) *upValue {
	link := &l.upValues
	for uv := *link; uv != nil && uv.index >= level; uv = *link {
		if uv.index == level {
			return uv
		}
		link = &uv.next
	}
	uv := storage
	if uv == nil {
		uv = new(upValue)
	}
	*uv = upValue{state: l, index: level, next: *link}
	*link = uv
	return uv
}

func (l *State) newClosure(p *prototype, upValues []*upValue, base int) value {
	c := l.newLuaClosure(p)
	p.cache = c
	storage := &c.own
	for i, uv := range p.upValues {
		if uv.isLocal { // upValue refers to local variable
			c.upValues[i] = l.findUpValue(base+uv.index, storage)
			if c.upValues[i] == storage {
				storage = nil // used
			}
		} else { // get upValue from enclosing function
			c.upValues[i] = upValues[uv.index]
		}
	}
	return objectValue(c)
}

func cached(p *prototype, upValues []*upValue, base int) *luaClosure {
	c := p.cache
	if c != nil {
		for i, uv := range p.upValues {
			if uv.isLocal && !c.upValues[i].isInStackAt(base+uv.index) {
				return nil
			} else if !uv.isLocal && !c.upValues[i].sameHome(upValues[uv.index]) {
				return nil
			}
		}
	}
	return c
}

func (l *State) callGo(f value, function int, resultCount int) {
	l.checkStack(MinStack)
	l.pushGoFrame(function, resultCount)
	if l.hookMask&MaskCall != 0 {
		l.hook(HookCall, -1)
	}
	var n int
	if c := f.goClosure(); c != nil {
		n = c.function(l)
	} else {
		n = f.goFunction().Function(l)
	}
	apiCheckStackSpace(l, n)
	l.postCall(l.top - n)
}

func (l *State) preCall(function int, resultCount int) bool {
	for {
		switch fv := l.stack[function]; fv.kind() {
		case vkGoClosure, vkGoFunction:
			l.callGo(fv, function, resultCount)
			return true
		case vkLuaClosure:
			f := fv.luaClosure()
			p := f.prototype
			l.checkStack(p.maxStackSize)
			argCount, parameterCount := l.top-function-1, p.parameterCount
			if argCount < parameterCount {
				extra := parameterCount - argCount
				args := l.stack[l.top : l.top+extra]
				clear(args)
				l.top += extra
				argCount += extra
			}
			base := function + 1
			if p.isVarArg {
				base = l.adjustVarArgs(p, argCount)
			}
			ci := l.pushLuaFrame(function, base, resultCount, f)
			if l.hookMask&MaskCall != 0 {
				l.callHook(ci)
			}
			return false
		default:
			tm := l.tagMethodByObject(l.stack[function], tmCall)
			if !tm.isFunction() {
				l.typeError(l.stack[function], "call")
			}
			// Slide the args + function up 1 slot and poke in the tag method
			for p := l.top; p > function; p-- {
				l.stack[p] = l.stack[p-1]
			}
			l.top++
			l.checkStack(0)
			l.stack[function] = tm
		}
	}
}

func (l *State) callHook(ci *callInfo) {
	ci.savedPC++ // hooks assume 'pc' is already incremented
	if pci := ci.previous; pci.isLua() && pci.code[pci.savedPC-1].opCode() == opTailCall {
		ci.setCallStatus(callStatusTail)
		l.hook(HookTailCall, -1)
	} else {
		l.hook(HookCall, -1)
	}
	ci.savedPC-- // correct 'pc'
}

func (l *State) adjustVarArgs(p *prototype, argCount int) int {
	fixedArgCount := p.parameterCount
	l.assert(argCount >= fixedArgCount)
	// move fixed parameters to final position
	fixed := l.top - argCount // first fixed argument
	base := l.top             // final position of first argument
	fixedArgs := l.stack[fixed : fixed+fixedArgCount]
	copy(l.stack[base:base+fixedArgCount], fixedArgs)
	clear(fixedArgs)
	return base
}

func (l *State) postCall(firstResult int) bool {
	ci := l.callInfo
	if l.hookMask&MaskReturn != 0 {
		l.hook(HookReturn, -1)
	}
	result, wanted, i := ci.function, ci.resultCount, 0
	l.callInfo = ci.previous // back to caller
	// TODO this is obscure - I don't fully understand the control flow, but it works
	for i = wanted; i != 0 && firstResult < l.top; i-- {
		l.stack[result] = l.stack[firstResult]
		result++
		firstResult++
	}
	for ; i > 0; i-- {
		l.stack[result] = nilValue
		result++
	}
	l.top = result
	if l.hookMask&(MaskReturn|MaskLine) != 0 {
		l.oldPC = l.callInfo.savedPC // oldPC for caller function
	}
	return wanted != MultipleReturns
}

// Call a Go or Lua function. The function to be called is at function.
// The arguments are on the stack, right after the function. On return, all the
// results are on the stack, starting at the original function position.
func (l *State) call(function int, resultCount int, allowYield bool) {
	if l.nestedGoCallCount++; l.nestedGoCallCount == maxCallCount {
		l.runtimeError("Go stack overflow")
	} else if l.nestedGoCallCount >= maxCallCount+maxCallCount>>3 {
		l.throw(ErrorError) // error while handling stack error
	}
	if !allowYield {
		l.nonYieldableCallCount++
	}
	if !l.preCall(function, resultCount) { // is a Lua function?
		l.execute() // call it
	}
	if !allowYield {
		l.nonYieldableCallCount--
	}
	l.nestedGoCallCount--
}

func (l *State) throw(errorCode error) {
	if l.protectFunction != nil {
		panic(errorCode)
	} else {
		l.error = errorCode
		if g := l.global.mainThread; g.protectFunction != nil {
			g.push(l.stack[l.top-1])
			g.throw(errorCode)
		} else {
			if l.global.panicFunction != nil {
				l.global.panicFunction(l)
			}
			log.Panicf("Uncaught Lua error: %v", errorCode)
		}
	}
}

func (l *State) protect(f func()) (err error) {
	nestedGoCallCount, protectFunction := l.nestedGoCallCount, l.protectFunction
	l.protectFunction = func() {
		if e := recover(); e != nil {
			err = e.(error)
			l.nestedGoCallCount, l.protectFunction = nestedGoCallCount, protectFunction
		}
	}
	defer l.protectFunction()
	f()
	l.nestedGoCallCount, l.protectFunction = nestedGoCallCount, protectFunction
	return err
}

func (l *State) hook(event, line int) {
	if l.hooker == nil || !l.allowHook {
		return
	}
	ci := l.callInfo
	top := l.top
	ciTop := ci.top
	ar := Debug{Event: event, CurrentLine: line, callInfo: ci}
	l.checkStack(MinStack)
	ci.setTop(l.top + MinStack)
	l.assert(ci.top <= l.stackLast)
	l.allowHook = false // can't hook calls inside a hook
	ci.setCallStatus(callStatusHooked)
	l.hooker(l, ar)
	l.assert(!l.allowHook)
	l.allowHook = true
	ci.setTop(ciTop)
	l.top = top
	ci.clearCallStatus(callStatusHooked)
}

func (l *State) initializeStack() {
	l.stack = make([]value, basicStackSize)
	l.stackLast = basicStackSize - extraStack
	l.top++
	l.baseCallInfo.luaCallInfo = &luaCallInfo{frame: l.stack[:0]}
	l.baseCallInfo.setTop(l.top + MinStack)
	l.callInfo = &l.baseCallInfo
}

func (l *State) checkStack(n int) {
	if l.stackLast-l.top <= n {
		l.growStack(n)
	}
}

func (l *State) reallocStack(newSize int) {
	l.assert(newSize <= maxStack || newSize == errorStackSize)
	l.assert(l.stackLast == len(l.stack)-extraStack)
	l.stack = append(l.stack, make([]value, newSize-len(l.stack))...)
	l.stackLast = len(l.stack) - extraStack
	l.callInfo.next = nil
	for ci := l.callInfo; ci != nil; ci = ci.previous {
		if ci.hasFrame() {
			top := ci.top
			ci.frame = l.stack[top-len(ci.frame) : top]
		} else if ci.luaCallInfo != nil {
			ci.frame = nil // stale; drop the old stack
		}
	}
}

func (l *State) growStack(n int) {
	if len(l.stack) > maxStack { // error after extra size?
		l.throw(ErrorError)
	} else {
		needed := l.top + n + extraStack
		newSize := 2 * len(l.stack)
		if newSize > maxStack {
			newSize = maxStack
		}
		if newSize < needed {
			newSize = needed
		}
		if newSize > maxStack { // stack overflow?
			l.reallocStack(errorStackSize)
			l.runtimeError("stack overflow")
		} else {
			l.reallocStack(newSize)
		}
	}
}
