package lua

// Interrupt stops the Lua code running in l's state, on whichever of its
// threads is running, with the error "interrupted!" at its next loop
// iteration or tail call, as the standalone lua does on Ctrl-C. Unlike
// every other method, it may be called from any goroutine. A Go function
// the code called runs to completion first, and an interrupt that arrives
// once the outermost call from Go has returned is dropped.
func (l *State) Interrupt() { l.global.interrupt.Store(true) }

// interrupted raises the error Interrupt asked for. The interpreter and
// the JIT driver call it where they find the flag set.
//
//go:noinline
func (l *State) interrupted() {
	if l.global.interrupt.CompareAndSwap(true, false) {
		l.runtimeError("interrupted!")
	}
}

// interruptPoll is how many loop iterations the interpreter runs between
// checks for an interrupt. An atomic load on every iteration measured 6%
// slower on an interpreted numeric loop.
const interruptPoll = 1024

// pollInterrupt counts one loop iteration or tail call, and checks for an
// interrupt every interruptPoll of them.
func (l *State) pollInterrupt() {
	if l.interruptPoll--; l.interruptPoll <= 0 {
		l.checkInterrupt()
	}
}

//go:noinline
func (l *State) checkInterrupt() {
	l.interruptPoll = interruptPoll
	if l.global.interrupt.Load() {
		l.interrupted()
	}
}

// dropInterrupt clears an interrupt that arrived after the code it was for
// had finished.
func (l *State) dropInterrupt() {
	if l.global.interrupt.Load() {
		l.global.interrupt.Store(false)
	}
}
