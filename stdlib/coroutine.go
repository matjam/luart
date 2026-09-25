package stdlib

import "github.com/matjam/luart/lua"

// The coroutine library, after lcorolib.c.

func toCoroutine(l *lua.State) *lua.State {
	co := l.ToThread(1)
	l.ArgumentCheck(co != nil, 1, "coroutine expected")
	return co
}

// auxResume resumes co with n arguments from l, moves its results or
// error to l, and returns how many results it moved, or -1 for an error.
func auxResume(l, co *lua.State, n int) int {
	if !co.CheckStack(n) {
		l.PushString("too many arguments to resume")
		return -1
	}
	if co.Status() == lua.ThreadOK && co.Top() == 0 {
		l.PushString("cannot resume dead coroutine")
		return -1
	}
	l.XMove(co, n)
	if _, err := co.Resume(l, n); err != nil {
		co.XMove(l, 1) // the error
		return -1
	}
	results := co.Top()
	if !l.CheckStack(results + 1) {
		co.Pop(results)
		l.PushString("too many results to resume")
		return -1
	}
	co.XMove(l, results)
	return results
}

func coroutineResume(l *lua.State) int {
	co := toCoroutine(l)
	r := auxResume(l, co, l.Top()-1)
	if r < 0 {
		l.PushBoolean(false)
		l.Insert(-2)
		return 2 // false and the error
	}
	l.PushBoolean(true)
	l.Insert(-(r + 1))
	return r + 1 // true and the results
}

func coroutineWrapped(l *lua.State) int {
	co := l.ToThread(lua.UpValueIndex(1))
	r := auxResume(l, co, l.Top())
	if r < 0 {
		if l.IsString(-1) { // add where the error was raised
			l.Where(1)
			l.Insert(-2)
			l.Concat(2)
		}
		l.Error()
	}
	return r
}

func coroutineCreate(l *lua.State) int {
	l.CheckType(1, lua.TypeFunction)
	co := l.NewThread()
	l.PushValue(1) // the function, on top
	l.XMove(co, 1)
	return 1
}

var coroutineLibrary = []lua.RegistryFunction{
	{Name: "create", Function: coroutineCreate},
	{Name: "resume", Function: coroutineResume},
	{Name: "running", Function: func(l *lua.State) int {
		l.PushBoolean(l.PushThread())
		return 2
	}},
	{Name: "status", Function: func(l *lua.State) int {
		co := toCoroutine(l)
		switch {
		case l == co:
			l.PushString("running")
		case co.Status() == lua.ThreadYield:
			l.PushString("suspended")
		case co.Status() == lua.ThreadError:
			l.PushString("dead")
		default:
			if _, ok := co.Frame(0); ok { // it has frames: it resumed another
				l.PushString("normal")
			} else if co.Top() == 0 {
				l.PushString("dead")
			} else {
				l.PushString("suspended") // not started
			}
		}
		return 1
	}},
	{Name: "wrap", Function: func(l *lua.State) int {
		coroutineCreate(l)
		l.PushGoClosure(coroutineWrapped, 1)
		return 1
	}},
	{Name: "yield", Function: func(l *lua.State) int { return l.Yield(l.Top()) }},
}

// OpenCoroutine opens the coroutine library. Usually passed to Require.
func OpenCoroutine(l *lua.State) int {
	l.NewLibrary(coroutineLibrary)
	return 1
}
