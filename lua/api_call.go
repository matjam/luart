package lua

import (
	"io"

	"github.com/matjam/luart/internal/chunk"
)

// Context is called by a continuation function to retrieve the status of the
// thread and context information. When called in the origin function, it
// will always return (0, false, nil). When called inside a continuation function,
// it will return (ctx, shouldYield, err), where ctx is the value that was
// passed to the callee together with the continuation function.
//
// http://www.lua.org/manual/5.2/manual.html#lua_getctx
func (l *State) Context() (int, bool, error) {
	if l.callInfo.isCallStatus(callStatusYielded) {
		return l.callInfo.context, l.callInfo.shouldYield, l.callInfo.error
	}
	return 0, false, nil
}

// CallWithContinuation is exactly like Call, but allows the called function to
// yield.
//
// http://www.lua.org/manual/5.2/manual.html#lua_callk
func (l *State) CallWithContinuation(argCount, resultCount, context int, continuation Function) {
	if apiCheck && continuation != nil && l.callInfo.isLua() {
		panic("cannot use continuations inside hooks")
	}
	l.checkElementCount(argCount + 1)
	if apiCheck && l.shouldYield {
		panic("cannot do calls on non-normal thread")
	}
	l.checkResults(argCount, resultCount)
	f := l.top - (argCount + 1)
	if continuation != nil && l.nonYieldableCallCount == 0 { // need to prepare continuation?
		l.callInfo.continuation = continuation
		l.callInfo.context = context
		l.call(f, resultCount, true) // just do the call
	} else { // no continuation or not yieldable
		l.call(f, resultCount, false) // just do the call
	}
	l.adjustResults(resultCount)
}

// Call calls a function. To do so, use the following protocol: first, the
// function to be called is pushed onto the stack; then, the arguments to the
// function are pushed in direct order - that is, the first argument is pushed
// first. Finally, call Call. argCount is the number of arguments that you
// pushed onto the stack. All arguments and the function value are popped
// from the stack when the function is called.
//
// The results are pushed onto the stack when the function returns. The
// number of results is adjusted to resultCount, unless resultCount is
// MultipleReturns. In this case, all results from the function are pushed.
// Lua takes care that the returned values fit into the stack space. The
// function results are pushed onto the stack in direct order (the first
// result is pushed first), so that after the call the last result is on the
// top of the stack.
//
// Any error inside the called function provokes a call to panic().
//
// The following example shows how the host program can do the equivalent to
// this Lua code:
//
//	a = f("how", t.x, 14)
//
// Here it is in Go:
//
//	l.Global("f")       // Function to be called.
//	l.PushString("how") // 1st argument.
//	l.Global("t")       // Table to be indexed.
//	l.Field(-1, "x")    // Push result of t.x (2nd arg).
//	l.Remove(-2)        // Remove t from the stack.
//	l.PushInteger(14)   // 3rd argument.
//	l.Call(3, 1)        // Call f with 3 arguments and 1 result.
//	l.SetGlobal("a")    // Set global a.
//
// Note that the code above is "balanced": at its end, the stack is back to
// its original configuration. This is considered good programming practice.
//
// http://www.lua.org/manual/5.2/manual.html#lua_call
func (l *State) Call(argCount, resultCount int) {
	l.CallWithContinuation(argCount, resultCount, 0, nil)
}

// ProtectedCall calls a function in protected mode. Both argCount and
// resultCount have the same meaning as in Call. If there are no errors
// during the call, ProtectedCall behaves exactly like Call.
//
// However, if there is any error, ProtectedCall catches it, pushes a single
// value on the stack (the error message), and returns an error. Like Call,
// ProtectedCall always removes the function and its arguments from the stack.
//
// If errorFunction is 0, then the error message returned on the stack is
// exactly the original error message. Otherwise, errorFunction is the stack
// index of an error handler (in the Lua C, message handler). This cannot be
// a pseudo-index in the current implementation. In case of runtime errors,
// this function will be called with the error message and its return value
// will be the message returned on the stack by ProtectedCall.
//
// Typically, the error handler is used to add more debug information to the
// error message, such as a stack traceback. Such information cannot be
// gathered after the return of ProtectedCall, since by then, the stack has
// unwound.
//
// The possible errors are the following:
//
//	RuntimeError     a runtime error
//	ErrMemory        allocating memory, the error handler is not called
//	ErrErrorHandler  running the error handler
//
// http://www.lua.org/manual/5.2/manual.html#lua_pcall
func (l *State) ProtectedCall(argCount, resultCount, errorFunction int) error {
	return l.ProtectedCallWithContinuation(argCount, resultCount, errorFunction, 0, nil)
}

// ProtectedCallWithContinuation behaves exactly like ProtectedCall, but
// allows the called function to yield.
//
// http://www.lua.org/manual/5.2/manual.html#lua_pcallk
func (l *State) ProtectedCallWithContinuation(argCount, resultCount, errorFunction, context int, continuation Function) (err error) {
	if apiCheck && continuation != nil && l.callInfo.isLua() {
		panic("cannot use continuations inside hooks")
	}
	l.checkElementCount(argCount + 1)
	if apiCheck && l.shouldYield {
		panic("cannot do calls on non-normal thread")
	}
	l.checkResults(argCount, resultCount)
	if errorFunction != 0 {
		apiCheckStackIndex(errorFunction, l.indexToValue(errorFunction))
		errorFunction = l.AbsIndex(errorFunction)
	}

	f := l.top - (argCount + 1)

	if continuation == nil || l.nonYieldableCallCount > 0 {
		err = l.protectedCall(func() { l.call(f, resultCount, false) }, f, errorFunction)
	} else {
		c := l.callInfo
		c.continuation, c.context, c.extra, c.oldAllowHook, c.oldErrorFunction = continuation, context, f, l.allowHook, l.errorFunction
		l.errorFunction = errorFunction
		l.callInfo.setCallStatus(callStatusYieldableProtected)
		l.call(f, resultCount, true)
		l.callInfo.clearCallStatus(callStatusYieldableProtected)
		l.errorFunction = c.oldErrorFunction
	}
	l.adjustResults(resultCount)
	return
}

// Load loads a Lua chunk, without running it. If there are no errors, it
// pushes the compiled chunk as a Lua function on top of the stack.
// Otherwise, it pushes an error message.
//
// http://www.lua.org/manual/5.2/manual.html#lua_load
func (l *State) Load(r io.Reader, chunkName string, mode string) error {
	if chunkName == "" {
		chunkName = "?"
	}

	if err := protectedParser(l, r, chunkName, mode); err != nil {
		return err
	}

	f := l.stack[l.top-1].luaClosure()
	if f.upValueCount() == 1 {
		f.setUpValue(0, l.global.registry.atInt(RegistryIndexGlobals))
	}
	if l.global.jit {
		markJIT(f.prototype)
	}
	return nil
}

// Dump dumps a function as a binary chunk. It receives a Lua function on
// the top of the stack and produces a binary chunk that, if loaded again,
// results in a function equivalent to the one dumped.
//
// http://www.lua.org/manual/5.3/manual.html#lua_dump
func (l *State) Dump(w io.Writer) error {
	l.checkElementCount(1)
	if f := l.stack[l.top-1].luaClosure(); f != nil {
		return chunk.Dump(w, protoOf(f.prototype))
	}
	panic("closure expected")
}

// Error generates a Lua error.  The error message must be on the stack top.
// The error can be any of any Lua type. This function will panic().
//
// http://www.lua.org/manual/5.2/manual.html#lua_error
func (l *State) Error() {
	l.checkElementCount(1)
	l.errorMessage()
}

func (l *State) setErrorObject(err error, oldTop int) {
	switch err {
	case ErrMemory:
		l.stack[oldTop] = stringValue(l.global.memoryErrorMessage)
	case ErrErrorHandler:
		l.stack[oldTop] = stringValue("error in error handling")
	default:
		l.stack[oldTop] = l.stack[l.top-1]
	}
	l.top = oldTop + 1
}

func (l *State) protectedCall(f func(), oldTop, errorFunc int) error {
	callInfo, allowHook, nonYieldableCallCount, errorFunction := l.callInfo, l.allowHook, l.nonYieldableCallCount, l.errorFunction
	l.errorFunction = errorFunc
	err := l.protect(f)
	if err != nil {
		l.close(oldTop)
		l.setErrorObject(err, oldTop)
		l.callInfo, l.allowHook, l.nonYieldableCallCount = callInfo, allowHook, nonYieldableCallCount
		// TODO l.shrinkStack()
	}
	l.errorFunction = errorFunction
	return err
}
