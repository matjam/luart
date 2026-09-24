package lua

import (
	"os"
	"runtime"
	"unsafe"

	"github.com/matjam/luart/internal/jit/call"
	"github.com/matjam/luart/internal/jit/execmem"
)

// An Option configures a State created by NewState.
type Option func(*State)

// WithJIT compiles hot Lua functions to machine code on platforms that
// support it: linux and darwin on arm64 and amd64. Elsewhere it does
// nothing. Compiled code runs only while no debug hook is set. Setting the
// environment variable LUART_JIT=off disables it.
func WithJIT() Option {
	return func(l *State) { l.global.jit = jitSupported && !jitDisabled }
}

var jitDisabled = os.Getenv("LUART_JIT") == "off"

// jitDefault turns the JIT on for every new state. The tests set it to run
// the whole suite compiled.
var jitDefault = false

// jitThreshold is how many calls and loop iterations a function runs in
// the interpreter before it is compiled. Tests set it to zero.
var jitThreshold int32 = 1000

// jitBudget is how many loop back-edges compiled code runs before it
// returns to Go, so the goroutine can be preempted.
const jitBudget = 1 << 16

// jitContext is shared with generated code, which reads and writes it at
// fixed offsets. Keep the field order in sync with the compilers.
type jitContext struct {
	frame     unsafe.Pointer // &frame[0]
	constants unsafe.Pointer // &constants[0], or nil
	target    uintptr        // address to start at
	exitPC    uint64         // pc to resume the interpreter at
	budget    int64          // back-edges left
	upValues  unsafe.Pointer // &closure.upValues[0], or nil
	barrier   uint64         // nonzero while the GC write barrier is on
}

// writeBarrier is the runtime's flag that Go's own compiled code tests
// before storing a pointer. The runtime changes it only while the world is
// stopped, and a goroutine running generated code is never stopped, so a
// value read on entry holds for the whole run. While it is off, generated
// code stores any value directly, as Go does; while it is on, it stores
// only between nil and scalar values and exits otherwise.
//
//go:linkname writeBarrier runtime.writeBarrier
var writeBarrier struct {
	enabled bool
	pad     [3]byte
	alignme uint64
}

// Reasons generated code returns.
const (
	jitExitInstruction = iota // the interpreter must run the instruction at exitPC
	jitExitBudget             // the back-edge budget ran out
)

// jitCode is a prototype's compiled code.
type jitCode struct {
	mem     *execmem.Code
	offsets []int32 // native offset of each pc's code, or -1
}

// The interpreter reaches compiled code through instructions patched into
// a prototype's exec code, so a state without the JIT runs unchanged code
// and pays nothing. prototype.jitOrig keeps the unpatched instructions.
//
// Until p is compiled, opJITCount sits at its entry and at each FORLOOP.
// Once it is hot and compiled, those revert and opJITEnter goes at each
// pc the compiler lists as an entry: function entry, loop latches, jump
// targets, and the instruction after each exit, so the interpreter hands
// back to compiled code as soon as it has run the instruction that exited.

// markJIT marks p and the prototypes nested in it for compilation.
func markJIT(p *prototype) {
	p.jitOn = true
	for i := range p.prototypes {
		markJIT(&p.prototypes[i])
	}
}

// patchJITCounters puts opJITCount at p's entry and loop latches. It runs
// when p's exec code is built.
func (p *prototype) patchJITCounters() {
	p.jitOrig = append([]instruction(nil), p.exec...)
	for ip, i := range p.jitOrig {
		if ip == 0 || p.code[ip].opCode() == opForLoop && !isExtraArg(p.code, ip) {
			p.exec[ip] = patched(i, opJITCount)
		}
	}
}

// jitInstruction handles i, a patched instruction the interpreter fetched
// before ip. opJITCount counts; opJITEnter runs compiled code, which may
// advance the program. It returns the original instruction the
// interpreter must run next, and the ip after that instruction.
//
// It stays out of line: its body inside the interpreter loop measured 10%
// slower on arithmetic.
//
//go:noinline
func (l *State) jitInstruction(c *luaClosure, frame, constants []value, i instruction, ip pc) (instruction, pc) {
	p := c.prototype
	if i.opCode() == opJITCount {
		l.countJIT(p)
	} else if l.hookMask == 0 {
		ip = l.runJIT(c, frame, constants, ip-1) + 1
		l.callInfo.savedPC = ip
	}
	return p.jitOrig[ip-1], ip
}

// countJIT counts one call or loop iteration of p and compiles it once it
// is hot.
func (l *State) countJIT(p *prototype) {
	if p.hot++; p.hot <= jitThreshold || p.jit != nil {
		return
	}
	copy(p.exec, p.jitOrig) // remove the counters
	code, offsets, entries := compileJIT(p)
	if code == nil {
		return
	}
	mem, err := execmem.Load(code)
	if err != nil {
		return
	}
	p.jit = &jitCode{mem: mem, offsets: offsets}
	for _, ip := range entries {
		p.exec[ip] = patched(p.exec[ip], opJITEnter)
	}
}

// jitEntries lists the pcs where the interpreter should hand over to
// compiled code: function entry, loop latches, jump targets, and the
// instruction after each pc where exits is true, since compiled code exits
// there.
func jitEntries(p *prototype, exits []bool) []int {
	n := len(p.code)
	entry := make([]bool, n)
	mark := func(ip int) {
		if 0 <= ip && ip < n && !isConsumed(p.code, ip) {
			entry[ip] = true
		}
	}
	mark(0)
	for ip, i := range p.code {
		if isExtraArg(p.code, ip) {
			continue
		}
		switch i.opCode() {
		case opJump, opForPrep, opForLoop, opTForLoop:
			mark(ip + 1 + i.sbx())
		}
		if i.opCode() == opForLoop {
			mark(ip)
		}
		if exits[ip] {
			next := ip + 1
			if isExtraArg(p.code, next) {
				next++
			}
			mark(next)
		}
	}
	var entries []int
	for ip, ok := range entry {
		if ok {
			entries = append(entries, ip)
		}
	}
	return entries
}

func patched(i instruction, op opCode) instruction {
	i.setOpCode(op)
	return i
}

// isConsumed reports whether the instruction before code[ip] reads it as
// part of itself: an extra-argument word, the JMP after a test, or the
// TFORLOOP after TFORCALL. Such instructions are never patched.
func isConsumed(code []instruction, ip int) bool {
	if ip == 0 {
		return false
	}
	switch code[ip-1].opCode() {
	case opEqual, opLessThan, opLessOrEqual, opTest, opTestSet, opTForCall:
		return true
	}
	return isExtraArg(code, ip)
}

// isExtraArg reports whether code[ip] is the extra-argument word of the
// instruction before it rather than an instruction.
func isExtraArg(code []instruction, ip int) bool {
	if ip == 0 {
		return false
	}
	switch prev := code[ip-1]; prev.opCode() {
	case opLoadConstantEx:
		return true
	case opSetList:
		return prev.c() == 0
	}
	return false
}

// runJIT runs compiled code from pc in frame and returns the pc the
// interpreter continues at.
func (l *State) runJIT(c *luaClosure, frame, constants []value, ip pc) pc {
	jc := c.prototype.jit
	off := jc.offsets[ip]
	if off < 0 {
		return ip
	}
	ctx := &l.jitCtx
	ctx.frame = unsafe.Pointer(&frame[0])
	ctx.constants = nil
	if len(constants) > 0 {
		ctx.constants = unsafe.Pointer(&constants[0])
	}
	ctx.upValues = nil
	if len(c.upValues) > 0 {
		ctx.upValues = unsafe.Pointer(&c.upValues[0])
	}
	ctx.barrier = 0
	if writeBarrier.enabled {
		ctx.barrier = 1
		l.jitBarrierRuns++
	}
	ctx.target = jc.mem.Addr(int(off))
	ctx.budget = jitBudget
	reason := call.Call(jc.mem.Addr(0), unsafe.Pointer(ctx))
	l.jitRuns++
	runtime.KeepAlive(frame)
	runtime.KeepAlive(constants)
	runtime.KeepAlive(c)
	if reason == jitExitBudget {
		runtime.Gosched()
	}
	return pc(ctx.exitPC)
}
