package lua_test

import (
	"strings"
	"testing"
	"time"

	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

// Interrupt, from another goroutine, stops every kind of loop, interpreted
// and compiled, and leaves the state usable.
func TestInterrupt(t *testing.T) {
	loops := map[string]string{
		"while":            `while true do end`,
		"repeat":           `repeat until false`,
		"numeric for":      `for i = 1, math.huge do end`,
		"kernel":           `local s = 0; for i = 1, math.huge do s = s + i * 0.5 end`,
		"generic for":      `for _ in function() return 1 end do end`,
		"tail calls":       `local function f() return f() end; return f()`,
		"Lua calls":        `local function f(x) return x + 1 end; local n = 0; while true do n = f(n) end`,
		"calls into Go":    `local s = 0; while true do s = s + math.abs(-1) + #tostring(s) end`,
		"interpreted step": `local s; while true do s = "x" .. "y" end`,
	}
	for name, src := range loops {
		for _, jit := range []bool{true, false} {
			t.Run(name+map[bool]string{true: "/jit", false: "/interpreted"}[jit], func(t *testing.T) {
				var opts []lua.Option
				if !jit {
					opts = append(opts, lua.WithoutJIT())
				}
				l := lua.NewState(opts...)
				stdlib.Open(l)
				done := make(chan error)
				go func() { done <- l.DoString(src) }()
				time.Sleep(20 * time.Millisecond)
				l.Interrupt()
				select {
				case err := <-done:
					if err == nil || !strings.Contains(err.Error(), "interrupted!") {
						t.Fatalf("got %v, want interrupted!", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("not interrupted")
				}
				l.SetTop(0)
				if err := l.DoString(`local s = 0; for i = 1, 3000 do s = s + i end; assert(s == 4501500)`); err != nil {
					t.Fatalf("after the interrupt: %v", err)
				}
			})
		}
	}
}

// An interrupt that arrives when nothing is running is dropped once the
// next call from Go returns, rather than stopping the call after it.
func TestInterruptWhileIdle(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	l.Interrupt()
	for range 2 {
		if err := l.DoString(`local s = 0; for i = 1, 10 do s = s + i end; return s`); err != nil && !strings.Contains(err.Error(), "interrupted!") {
			t.Fatal(err)
		}
		l.SetTop(0)
	}
	if err := l.DoString(`for i = 1, 10 do end`); err != nil {
		t.Fatalf("a dropped interrupt stopped a later call: %v", err)
	}
}
