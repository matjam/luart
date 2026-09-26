package lua

import (
	"errors"
	"fmt"
)

// To-be-closed variables, as Lua 5.4's lfunc.c has them. TBC puts a
// variable's stack index on its thread's list, l.tbc; leaving the
// variable's scope, by a closing JMP, a RETURN or an error, calls its
// __close metamethod, innermost first. Each leaves the list before its
// call, so a yield or an error inside one never closes it again: an
// interrupted JMP or RETURN runs again after a yield (finishOp) to close
// the rest.

// newTBC runs TBC for register a of ci: the value there, unless nil or
// false, must have a __close metamethod.
func (l *State) newTBC(ci *callInfo, a int) {
	level := ci.stackIndex(a)
	if isFalse(l.stack[level]) {
		return
	}
	if l.tagMethodByObject(l.stack[level], tmClose).isNil() {
		name, ok := ci.closure.prototype.localName(a+1, ci.savedPC-1)
		if !ok {
			name = "?"
		}
		l.runtimeError(fmt.Sprintf("variable '%s' got a non-closable value", name))
	}
	l.tbc = append(l.tbc, level)
}

// hasTBC reports whether a variable at or above stack index level is to
// be closed.
func (l *State) hasTBC(level int) bool {
	n := len(l.tbc)
	return n > 0 && l.tbc[n-1] >= level
}

// closeTBC closes the variables at or above level. After an error (hasErr)
// each __close gets errObj too. Calls start at top, above the live values,
// or with a negative top just above the variable, where nothing is live
// any more.
func (l *State) closeTBC(level, top int, errObj value, hasErr, yieldable bool) {
	for l.hasTBC(level) {
		n := len(l.tbc)
		index := l.tbc[n-1]
		l.tbc = l.tbc[:n-1]
		v := l.stack[index]
		tm := l.tagMethodByObject(v, tmClose)
		if l.top = top; top < 0 {
			l.top = index + 1
		}
		l.checkStack(3)
		if !tm.isFunction() && l.tagMethodByObject(tm, tmCall).isNil() {
			l.runtimeError(fmt.Sprintf("attempt to call a %s value (metamethod 'close')", l.valueToType(tm)))
		}
		l.push(tm)
		l.push(v)
		args := 1
		if hasErr {
			l.push(errObj)
			args = 2
		}
		l.call(l.top-1-args, 0, yieldable)
	}
}

// errorValue is the Lua value of err, as setErrorObject stores it: a
// raised error leaves its value on top of the stack.
func (l *State) errorValue(err error) value {
	switch err {
	case ErrMemory:
		return stringValue(l.global.memoryErrorMessage)
	case ErrErrorHandler:
		return stringValue("error in error handling")
	}
	return l.stack[l.top-1]
}

// closeProtected closes the upvalues and to-be-closed variables at or
// above level after err, running the __close metamethods protected and
// unable to yield. An error in one replaces err for the rest, as in
// ldo.c's luaD_closeprotected. It returns the last error and its value.
func (l *State) closeProtected(level int, err error) (error, value) {
	ci, allowHook, nonYieldable := l.callInfo, l.allowHook, l.nonYieldableCallCount
	errObj := l.errorValue(err)
	for {
		e := l.protect(func() {
			l.close(level)
			l.closeTBC(level, -1, errObj, true, false)
		})
		if e == nil {
			return err, errObj
		}
		err, errObj = e, l.errorValue(e)
		l.callInfo, l.allowHook, l.nonYieldableCallCount = ci, allowHook, nonYieldable
	}
}

// CloseThread closes the to-be-closed variables of l, a coroutine that is
// suspended or dead, as lua_closethread does, and leaves it dead. from is
// the thread closing it, whose nested calls count against l's limit, or
// nil. It returns the error that killed l, or one a __close metamethod
// raised since, with the error value on l's stack; or nil.
func (l *State) CloseThread(from *State) error {
	l.nestedGoCallCount = 0
	if from != nil {
		l.nestedGoCallCount = from.nestedGoCallCount
	}
	var err error
	if l.status == ThreadError && l.deathError != nil {
		err = l.deathError
		l.stack[l.top] = l.deathValue // its value, which Resume's caller took
		l.top++
	}
	l.deathError = nil // closed once: a second close finds nothing
	l.callInfo = &l.baseCallInfo
	l.status = ThreadOK // so that __close metamethods can run
	l.nonYieldableCallCount = 1
	if err == nil && !l.hasTBC(1) {
		l.top = l.baseCallInfo.function + 1
		return nil
	}
	if err == nil { // a suspended coroutine: no error object
		e := l.protect(func() { l.close(1); l.closeTBC(1, -1, nilValue, false, false) })
		if e == nil {
			l.top = l.baseCallInfo.function + 1
			return nil
		}
		err = e
		l.callInfo, l.nonYieldableCallCount = &l.baseCallInfo, 1
	}
	err, errObj := l.closeProtected(1, err)
	l.stack[1] = errObj
	l.top = 2
	l.status = ThreadError
	return err
}

// errClosed unwinds a coroutine that closed itself to its Resume.
var errClosed = errors.New("lua: coroutine closed")

// CloseRunning closes l, the running coroutine, as coroutine.close() does
// in Lua 5.5: its to-be-closed variables close and its Resume returns, as
// if it had finished without results, or with the error one raised.
func (l *State) CloseRunning() {
	l.closedError = l.CloseThread(l)
	l.throw(errClosed)
}
