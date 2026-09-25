package lua_test

import (
	"testing"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// A Go host drives a coroutine with NewThread and Resume, and a Go
// function yields with a continuation that finishes it on resumption.
func TestResumeAndYieldFromGo(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	var resumedWith string
	l.Register("ask", func(l *lua.State) int {
		l.PushString("question")
		return l.YieldWithContinuation(1, 7, func(l *lua.State) int {
			ctx, yielded, _ := l.Context()
			if ctx != 7 || !yielded {
				t.Errorf("Context() = %d, %v", ctx, yielded)
			}
			resumedWith, _ = l.ToString(-1)
			l.PushString("answered " + resumedWith)
			return 1
		})
	})
	if err := l.DoString(`function body(x) local r = ask(); return x .. ": " .. r end`); err != nil {
		t.Fatal(err)
	}

	co := l.NewThread()
	if co.Status() != lua.ThreadOK {
		t.Fatalf("new thread status %v", co.Status())
	}
	co.Global("body")
	co.PushString("start")
	yielded, err := co.Resume(l, 1)
	if err != nil || !yielded || co.Status() != lua.ThreadYield {
		t.Fatalf("first Resume = %v, %v; status %v", yielded, err, co.Status())
	}
	if q, _ := co.ToString(-1); q != "question" {
		t.Errorf("yielded %q", q)
	}
	co.Pop(1)
	co.PushString("42")
	yielded, err = co.Resume(l, 1)
	if err != nil || yielded || co.Status() != lua.ThreadOK {
		t.Fatalf("second Resume = %v, %v; status %v", yielded, err, co.Status())
	}
	if r, _ := co.ToString(-1); r != "start: answered 42" || resumedWith != "42" {
		t.Errorf("result %q, resumed with %q", r, resumedWith)
	}

	// Resuming a finished coroutine is an error that leaves it as it was.
	co.SetTop(0)
	co.Global("error")
	co.PushString("boom")
	if _, err := co.Resume(l, 1); err == nil || co.Status() != lua.ThreadError {
		t.Errorf("error Resume = %v; status %v", err, co.Status())
	}
	if _, err := co.Resume(l, 0); err == nil || err.Error() != "runtime error: cannot resume dead coroutine" {
		t.Errorf("dead Resume = %v", err)
	}
}
