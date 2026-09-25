package lua

import (
	"errors"

	"github.com/matjam/luart/internal/bytecode"
)

// Coroutines work as C Lua 5.2's do (ldo.c's lua_resume and lua_yieldk,
// lvm.c's luaV_finishOp). A yield unwinds the Go stack back to Resume by
// panicking, as an error does; the coroutine's Lua frames stay on its own
// stack. Resume then finishes the instruction the yield interrupted and
// runs the frames on, and a Go function between them continues through the
// continuation it passed to CallWithContinuation or
// ProtectedCallWithContinuation. A Go function without one cannot be
// yielded across: "attempt to yield across a C-call boundary".

// A ThreadStatus is a thread's status, as Status reports it.
type ThreadStatus int

// Valid ThreadStatus values.
const (
	ThreadOK    ThreadStatus = iota // running, not started, or finished
	ThreadYield                     // suspended in a yield
	ThreadError                     // stopped by an error, and dead
)

var (
	errYield     = errors.New("lua: yield")         // unwinds a yield to Resume
	errBadResume = errors.New("lua: cannot resume") // Resume's own errors
)

// NewThread creates a thread, pushes it onto the stack, and returns it.
// The thread shares l's global state and hook, with its own stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_newthread
func (l *State) NewThread() *State {
	l1 := &State{global: l.global, allowHook: true, nonYieldableCallCount: 1}
	l1.initializeStack()
	l1.hooker, l1.hookMask, l1.baseHookCount = l.hooker, l.hookMask, l.baseHookCount
	l1.resetHookCount()
	l.apiPush(objectValue(l1))
	return l1
}

// Status returns the status of thread l.
//
// http://www.lua.org/manual/5.2/manual.html#lua_status
func (l *State) Status() ThreadStatus { return l.status }

// Resume starts or continues the coroutine l. from is the thread resuming
// it, or nil. To start it, push its function and then argCount arguments
// onto l's stack; to continue it after a yield, push argCount values for
// its yield to return.
//
// Resume returns true if the coroutine yielded, with the values it yielded
// on l's stack; false if it finished, with its results there instead; or
// an error, with the error value on l's stack. After an error raised in
// the coroutine, it is dead. An error resuming it at all, such as resuming
// a dead coroutine, leaves it as it was.
//
// http://www.lua.org/manual/5.2/manual.html#lua_resume
func (l *State) Resume(from *State, argCount int) (yielded bool, err error) {
	oldNonYieldable := l.nonYieldableCallCount
	l.nestedGoCallCount = 1
	if from != nil {
		l.nestedGoCallCount = from.nestedGoCallCount + 1
	}
	l.nonYieldableCallCount = 0 // allow yields
	if l.status == ThreadOK {
		l.checkElementCount(argCount + 1)
	} else {
		l.checkElementCount(argCount)
	}
	firstArg := l.top - argCount
	err = l.protect(func() { l.resume(firstArg) })
	if err == errBadResume {
		msg, _ := l.stack[l.top-1].str()
		err = RuntimeError(msg)
	} else {
		for err != nil && err != errYield { // an error: can a pcall take it?
			if !l.recover(err) {
				l.status = ThreadError // dead
				l.setErrorObject(err, l.top)
				l.callInfo.setTop(l.top)
				break
			}
			err = l.protect(l.unroll)
		}
	}
	l.nonYieldableCallCount = oldNonYieldable
	l.nestedGoCallCount--
	if err == errYield {
		return true, nil
	}
	return false, err
}

// resume is ldo.c's resume, which Resume runs protected.
func (l *State) resume(firstArg int) {
	ci := l.callInfo
	if l.nestedGoCallCount >= maxCallCount {
		l.resumeError("C stack overflow", firstArg)
	}
	switch l.status {
	case ThreadOK: // starting the coroutine
		if ci != &l.baseCallInfo {
			l.resumeError("cannot resume non-suspended coroutine", firstArg)
		}
		if !l.preCall(firstArg-1, MultipleReturns) { // a Lua function
			if !l.global.jit || !l.callJIT() {
				l.execute()
			}
		}
	case ThreadYield: // resuming from a yield
		l.status = ThreadOK
		ci.function = ci.extra
		if ci.isLua() { // yielded inside a hook
			l.execute()
		} else {
			if ci.continuation != nil {
				ci.shouldYield = true // a yield, not an error
				ci.setCallStatus(callStatusYielded)
				n := ci.continuation(l)
				apiCheckStackSpace(l, n)
				firstArg = l.top - n // its results are the yield's
			}
			l.postCall(firstArg)
		}
		l.unroll()
	default:
		l.resumeError("cannot resume dead coroutine", firstArg)
	}
}

// resumeError replaces Resume's arguments with msg, and raises it.
func (l *State) resumeError(msg string, firstArg int) {
	l.top = firstArg
	l.push(stringValue(msg))
	l.throw(errBadResume)
}

// unroll runs the frames that a yield or a recovered error interrupted,
// down to the base: a Go function through its continuation, a Lua
// function from its interrupted instruction.
func (l *State) unroll() {
	for l.callInfo != &l.baseCallInfo {
		if !l.callInfo.isLua() {
			l.finishGoCall()
		} else {
			l.finishOp()
			l.execute()
		}
	}
}

// finishGoCall finishes the Go function in l.callInfo, which was calling
// when a yield or error interrupted it, by calling its continuation.
func (l *State) finishGoCall() {
	ci := l.callInfo
	l.assert(ci.continuation != nil && l.nonYieldableCallCount == 0)
	if ci.isCallStatus(callStatusYieldableProtected) { // was inside a pcall
		ci.clearCallStatus(callStatusYieldableProtected)
		l.errorFunction = ci.oldErrorFunction
	}
	l.adjustResults(ci.resultCount) // finish the call or pcall
	if !ci.isCallStatus(callStatusError) {
		ci.shouldYield = true // a yield, not an error
	}
	ci.clearCallStatus(callStatusError)
	ci.setCallStatus(callStatusYielded)
	n := ci.continuation(l)
	apiCheckStackSpace(l, n)
	l.postCall(l.top - n)
}

// findProtectedCall returns the innermost yieldable pcall, or nil.
func (l *State) findProtectedCall() *callInfo {
	for ci := l.callInfo; ci != nil; ci = ci.previous {
		if ci.isCallStatus(callStatusYieldableProtected) {
			return ci
		}
	}
	return nil
}

// recover finishes a yieldable pcall that err interrupted, leaving its
// continuation to run with the error status. It reports whether there was
// one.
func (l *State) recover(err error) bool {
	ci := l.findProtectedCall()
	if ci == nil {
		return false
	}
	oldTop := ci.extra
	l.close(oldTop)
	l.setErrorObject(err, oldTop)
	l.callInfo = ci
	l.allowHook = ci.oldAllowHook
	l.nonYieldableCallCount = 0 // yieldable again
	l.errorFunction = ci.oldErrorFunction
	ci.setCallStatus(callStatusError)
	ci.shouldYield, ci.error = false, err
	return true
}

// Yield yields the coroutine running l, which must not be the main
// thread, with resultCount values from the top of the stack, which its
// Resume returns. A Go function yields with
//
//	return l.Yield(n)
//
// Yield does not return, except inside a hook: see YieldWithContinuation.
//
// http://www.lua.org/manual/5.2/manual.html#lua_yield
func (l *State) Yield(resultCount int) int {
	return l.YieldWithContinuation(resultCount, 0, nil)
}

// YieldWithContinuation yields as Yield does. When the coroutine resumes,
// continuation runs in place of the Go function that yielded, if it is not
// nil, and its results are that function's; Context returns context there.
// Without a continuation, the values passed to Resume are the function's
// results.
//
// A hook may yield without results or a continuation. Yield then returns
// to the hook, which must return at once; the coroutine resumes at the
// instruction the hook was called for.
//
// http://www.lua.org/manual/5.2/manual.html#lua_yieldk
func (l *State) YieldWithContinuation(resultCount, context int, continuation Function) int {
	ci := l.callInfo
	l.checkElementCount(resultCount)
	if l.nonYieldableCallCount > 0 {
		if l != l.global.mainThread {
			l.runtimeError("attempt to yield across a C-call boundary")
		}
		l.runtimeError("attempt to yield from outside a coroutine")
	}
	l.status = ThreadYield
	ci.extra = ci.function // saved; the yield moves function
	if ci.isLua() {        // inside a hook: traceExecution yields after it
		return 0
	}
	if ci.continuation = continuation; continuation != nil {
		ci.context = context
	}
	ci.function = l.top - resultCount - 1 // protect the stack below the results
	l.throw(errYield)
	return 0
}

// finishOp finishes the instruction a yield interrupted in the Lua
// function in l.callInfo, after lvm.c's luaV_finishOp: the metamethod or
// function it called has returned, its result on the top of the stack. It
// reads the original code, whose positions the specialised and compiled
// copies keep.
func (l *State) finishOp() {
	ci := l.callInfo
	frame, p := ci.frame, ci.closure.prototype
	i := p.Code[ci.savedPC-1]
	switch op := i.OpCode(); op {
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod,
		bytecode.OpPow, bytecode.OpUnaryMinus, bytecode.OpLength, bytecode.OpGetTableUp, bytecode.OpGetTable:
		l.top--
		frame[i.A()] = l.stack[l.top]
	case bytecode.OpSelf:
		l.top--
		frame[i.A()+1] = frame[i.B()] // luart stores self after the lookup
		frame[i.A()] = l.stack[l.top]
	case bytecode.OpLessOrEqual, bytecode.OpLessThan, bytecode.OpEqual:
		result := !isFalse(l.stack[l.top-1])
		l.top--
		if op == bytecode.OpLessOrEqual && l.tagMethodByObject(k(i.B(), p.Constants, frame), tmLE).isNil() {
			result = !result // "<=" ran as "not <"
		}
		if result != (i.A() != 0) {
			ci.savedPC++ // skip the jump
		}
	case bytecode.OpConcat:
		top := l.top - 1 // the top when the metamethod was called
		b := i.B()
		total := top - 1 - ci.stackIndex(b) // elements yet to concatenate
		l.stack[top-2] = l.stack[top]       // the metamethod's result in place
		if total > 1 {
			l.top = top - 1
			l.concat(total)
		}
		a := i.A()
		frame[a] = l.stack[l.top-1]
		if a >= b { // limit of live values, as the VM clears them
			clear(frame[a+1:])
		} else {
			clear(frame[b:])
		}
		l.top = ci.top
	case bytecode.OpTForCall:
		l.top = ci.top
	case bytecode.OpCall:
		if i.C()-1 >= 0 { // fixed results
			l.top = ci.top
		}
	case bytecode.OpTailCall, bytecode.OpSetTableUp, bytecode.OpSetTable:
	default:
		l.assert(false)
	}
}
