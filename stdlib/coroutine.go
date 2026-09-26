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
		if co.Status() == lua.ThreadError { // it died: close its variables, as auxwrap does
			if err := co.CloseThread(l); err != nil {
				l.Pop(1)
				co.XMove(l, 1) // the error, perhaps from a __close
			}
		}
		if l.IsString(-1) { // add where the error was raised
			l.Where(1)
			l.Insert(-2)
			l.Concat(2)
		}
		l.Error()
	}
	return r
}

// coroutineStatus is co's status, as coroutine.status names it, seen from
// l.
func coroutineStatus(l, co *lua.State) string {
	switch {
	case l == co:
		return "running"
	case co.Status() == lua.ThreadYield:
		return "suspended"
	case co.Status() == lua.ThreadError:
		return "dead"
	}
	if _, ok := co.Frame(0); ok { // it has frames: it resumed another
		return "normal"
	} else if co.Top() == 0 {
		return "dead"
	}
	return "suspended" // not started
}

func coroutineCreate(l *lua.State) int {
	l.CheckType(1, lua.TypeFunction)
	co := l.NewThread()
	l.PushValue(1) // the function, on top
	l.XMove(co, 1)
	return 1
}

var coroutineLibrary = []lua.RegistryFunction{
	{Name: "close", Function: func(l *lua.State) int {
		co := l
		if !l.IsNone(1) {
			co = toCoroutine(l)
		}
		switch status := coroutineStatus(l, co); status {
		case "dead", "suspended":
			if err := co.CloseThread(l); err != nil {
				l.PushBoolean(false)
				co.XMove(l, 1)
				return 2
			}
			l.PushBoolean(true)
			return 1
		case "running":
			if l.PushThread() { // the main thread
				l.Errorf("cannot close main thread")
			}
			l.CloseRunning()
			return 0
		default:
			l.Errorf("cannot close a %s coroutine", status)
			return 0
		}
	}},
	{Name: "create", Function: coroutineCreate},
	{Name: "resume", Function: coroutineResume},
	{Name: "running", Function: func(l *lua.State) int {
		l.PushBoolean(l.PushThread())
		return 2
	}},
	{Name: "status", Function: func(l *lua.State) int {
		l.PushString(coroutineStatus(l, toCoroutine(l)))
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
