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

// A new State compiles hot Lua functions to machine code on platforms that
// support it: linux and darwin on arm64 and amd64. Elsewhere it
// interprets them. Compiled code runs only while no debug hook is set.
// Setting the environment variable LUART_JIT=off disables compilation for
// every State.

// WithJIT turns the JIT on. It is on by default; WithJIT remains for code
// written when it was not.
func WithJIT() Option {
	return func(l *State) { l.global.jit = jitSupported && !jitDisabled }
}

// WithoutJIT turns the JIT off, so the State only interprets.
func WithoutJIT() Option {
	return func(l *State) { l.global.jit = false }
}

var jitDisabled = os.Getenv("LUART_JIT") == "off"

// jitDefault is whether a new State compiles.
var jitDefault = true

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
	reason    uint64         // why the last run exited, set by runJIT
	state     unsafe.Pointer // the *State, for calls and returns
	callee    unsafe.Pointer // the *goFunction or *goClosure a jitExitCallGo calls
}

// A jitExitCallGo exit leaves only the callee's object in the context, and
// runJIT calls its Function without knowing which kind it is, so the
// Function must come first in both.
// Each array length below overflows otherwise.
var (
	_ [0 - unsafe.Offsetof(goFunction{}.Function)]struct{}
	_ [0 - unsafe.Offsetof(goClosure{}.function)]struct{}
)

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
	jitExitCallGo             // the CALL at exitPC calls a Go function or Go closure
	jitExitCallNumber         // the CALL at exitPC calls a number function
)

// jitCode is a prototype's compiled code.
type jitCode struct {
	mem     *execmem.Code
	offsets []int32 // native offset of each pc's code, or -1
	base    uintptr // address of the code
	entry   uintptr // address of pc 0's code
	kernels int     // numeric loop kernels compiled, for tests
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
// advance the program and call or return into other functions. It returns
// the original instruction the interpreter must run next in l.callInfo,
// and the ip after that instruction. The interpreter reloads its frame from
// l.callInfo after every call.
//
// It stays out of line: its body inside the interpreter loop measured 10%
// slower on arithmetic.
//
//go:noinline
func (l *State) jitInstruction(i instruction, ip pc) (instruction, pc) {
	ci := l.callInfo
	if i.opCode() == opJITCount {
		l.countJIT(ci.closure.prototype)
	} else if l.hookMask == 0 {
		l.runJIT(ci, ip-1, nil)
		ci = l.callInfo
		ip = ci.savedPC + 1
		ci.savedPC = ip
		// A Go function compiled code called may have set a hook, which the
		// interpreter would check before this instruction.
		if l.hookMask&(MaskLine|MaskCount) != 0 {
			if l.hookCount--; l.hookCount == 0 || l.hookMask&MaskLine != 0 {
				l.traceExecution()
			}
		}
	}
	return ci.closure.prototype.jitOrig[ip-1], ip
}

// countJIT counts one call or loop iteration of p and compiles it once it
// is hot.
func (l *State) countJIT(p *prototype) {
	if p.hot++; p.hot <= jitThreshold || p.jit != nil {
		return
	}
	copy(p.exec, p.jitOrig) // remove the counters
	code, offsets, entries, kernels := compileJIT(p)
	if code == nil {
		return
	}
	mem, err := execmem.Load(code)
	if err != nil {
		return
	}
	p.jit = &jitCode{mem: mem, offsets: offsets, base: mem.Addr(0), entry: mem.Addr(int(offsets[0])), kernels: kernels}
	for _, ip := range entries {
		p.exec[ip] = patched(p.exec[ip], opJITEnter)
	}
}

// jitMinRun is how many instructions compiled code must run from an entry
// before its first unconditional exit for entering it to pay: a round trip
// between the interpreter and compiled code costs about as much as
// interpreting a few instructions.
const jitMinRun = 4

// jitEntries lists the pcs where the interpreter should hand over to
// compiled code: function entry, loop latches, jump targets, and the
// instruction after each pc where exits is true, since compiled code exits
// there. It leaves out entries that would reach an instruction always
// exits at, other than a call or return runJIT handles, too soon.
func jitEntries(p *prototype, exits, always []bool) []int {
	n := len(p.code)
	entry := make([]bool, n)
	mark := func(ip int) {
		if 0 <= ip && ip < n && !isConsumed(p.code, ip) && worthEntering(p.code, always, ip) {
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

// worthEntering reports whether compiled code entered at ip runs at least
// jitMinRun instructions, or reaches a loop, before an instruction the
// interpreter must run.
func worthEntering(code []instruction, always []bool, ip int) bool {
	for run := 0; ip < len(code); ip++ {
		if isExtraArg(code, ip) {
			continue
		}
		op := code[ip].opCode()
		if always[ip] && op != opCall && op != opReturn && !jitSteps(op) {
			return false
		}
		if run++; run >= jitMinRun || op == opForLoop || op == opJump && code[ip].sbx() < 0 {
			return true
		}
	}
	return false
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

// runJIT runs compiled code from ip in ci's function. It handles the calls
// and returns compiled code exits at itself, moving between compiled
// functions without the interpreter, and stops at the first instruction
// the interpreter must run: that instruction's pc is left in the savedPC
// of l.callInfo, which may now be another frame.
//
// bottom, when not nil, is a frame that Go called through l.call. runJIT
// returns from it to Go itself, as the interpreter would, and then reports
// true.
//
// c and p follow ci, and are reloaded only when ci changes: an exit for a
// Go call comes back to the frame it left, and the chain of loads from ci
// to its prototype would otherwise be repeated on every crossing.
func (l *State) runJIT(ci *callInfo, ip pc, bottom *callInfo) bool {
	c := ci.closure
	p := c.prototype
	for {
		jc := p.jit
		if jc == nil || l.hookMask != 0 {
			break
		}
		off := jc.offsets[ip]
		if off < 0 {
			break
		}
		l.enterJIT(ci, c, p, jc, off)
		if nci := l.callInfo; nci != ci { // compiled calls and returns move between frames
			ci, c = nci, nci.closure
			p = c.prototype
		}
		ip = pc(l.jitCtx.exitPC)
		switch l.jitCtx.reason {
		case jitExitBudget:
			runtime.Gosched()
			continue
		case jitExitCallGo:
			l.jitCallGoFunction(ci, p.jitOrig[ip], ip)
			ip++
			continue
		case jitExitCallNumber:
			l.jitCallNumber(ci, p.jitOrig[ip], ip)
			ip++
			continue
		}
		if l.hookMask != 0 { // a Go function set a hook
			break
		}
		switch i := p.jitOrig[ip]; i.opCode() {
		case opCall:
			if nci, ok := l.jitCall(ci, i, ip); ok {
				if nci == ci {
					ip++
				} else {
					ci, ip, c = nci, 0, nci.closure
					p = c.prototype
				}
				continue
			}
		case opReturn:
			if ci == bottom {
				if l.jitReturnToGo(ci, p, i) {
					return true
				}
			} else if l.jitReturn(ci, i) {
				ci = l.callInfo
				ip, c = ci.savedPC, ci.closure
				p = c.prototype
				continue
			}
		case opJump: // one that closes upvalues, which compiled code leaves to Go
			ci.savedPC = ip + 1
			ip = l.jumpFrom(ci, i, ip+1)
			continue
		default:
			if jitSteps(i.opCode()) {
				ci.savedPC = ip + 1
				l.jitStep(ci, i, ip)
				ip++
				continue
			}
		}
		break
	}
	ci.savedPC = ip
	return false
}

// callJIT runs the Lua function that preCall has just entered for l.call,
// if it is compiled. It reports whether the function has returned;
// otherwise the interpreter goes on from the savedPC of l.callInfo.
func (l *State) callJIT() bool {
	ci := l.callInfo
	if ci.closure.prototype.jit == nil || l.hookMask != 0 {
		return false
	}
	return l.runJIT(ci, 0, ci)
}

// jitReturnToGo runs the RETURN i that compiled code exited at in ci, a
// frame Go called running p, as the interpreter's general RETURN does. It
// reports false, having changed nothing, when the results run up to
// l.top, which compiled code does not track.
func (l *State) jitReturnToGo(ci *callInfo, p *prototype, i instruction) bool {
	a, b := i.a(), i.b()
	if b == 0 {
		return false
	}
	l.top = ci.stackIndex(a + b - 1)
	if len(p.prototypes) > 0 {
		l.close(ci.base())
	}
	l.postCall(ci.stackIndex(a))
	return true
}

// jitSteps reports whether runJIT runs op itself when compiled code exits
// at it, and goes on in compiled code after it.
func jitSteps(op opCode) bool {
	switch op {
	case opJump, opNewTable, opClosure, opLength, opGetTable, opGetTableUp, opSelf, opSetTable, opSetTableUp,
		opGetField, opGetFieldUp, opSelfField, opSetField, opSetFieldUp:
		return true
	}
	return false
}

// jitStep runs i, the exec instruction at ip, as the interpreter would.
// Each case is a copy of the interpreter's.
func (l *State) jitStep(ci *callInfo, i instruction, ip pc) {
	closure, frame := ci.closure, ci.frame
	constants := closure.prototype.constants
	switch i.opCode() {
	case opNewTable:
		a := i.a()
		b, c := float8(i.b()), float8(i.c())
		frame[a] = objectValue(newTableAt(&closure.prototype.fields[ip], intFromFloat8(b), intFromFloat8(c)))
		clear(frame[a+1:])
	case opClosure:
		a, p := i.a(), &closure.prototype.prototypes[i.bx()]
		if ncl := cached(p, closure.upValues, ci.base()); ncl == nil {
			frame[a] = l.newClosure(p, closure.upValues, ci.base())
		} else {
			frame[a] = objectValue(ncl)
		}
		clear(frame[a+1:])
	case opLength:
		tmp := l.objectLength(frame[i.b()])
		ci.frame[i.a()] = tmp
	case opGetTableUp:
		tmp := l.tableAt(closure.upValue(i.b()), k(i.c(), constants, frame))
		ci.frame[i.a()] = tmp
	case opGetTable:
		tmp := l.tableAt(frame[i.b()], k(i.c(), constants, frame))
		ci.frame[i.a()] = tmp
	case opSetTableUp:
		l.setTableAt(closure.upValue(i.a()), k(i.b(), constants, frame), k(i.c(), constants, frame))
	case opSetTable:
		l.setTableAt(frame[i.a()], k(i.b(), constants, frame), k(i.c(), constants, frame))
	case opSelf:
		a, t := i.a(), frame[i.b()]
		tmp := l.tableAt(t, k(i.c(), constants, frame))
		frame = ci.frame
		frame[a+1], frame[a] = t, tmp
	case opGetField, opGetFieldUp, opSelfField:
		var t value
		if i.opCode() == opGetFieldUp {
			t = closure.upValue(i.b())
		} else {
			t = frame[i.b()]
		}
		key := constants[i.c()]
		v, ok := getField(t, key, &closure.prototype.fields[ip])
		if !ok {
			v = l.tableAt(t, key)
			frame = ci.frame
		}
		if i.opCode() == opSelfField {
			frame[i.a()+1] = t
		}
		frame[i.a()] = v
	case opSetField, opSetFieldUp:
		var t value
		if i.opCode() == opSetFieldUp {
			t = closure.upValue(i.a())
		} else {
			t = frame[i.a()]
		}
		key, v := constants[i.b()], k(i.c(), constants, frame)
		if !setField(t, key, v, &closure.prototype.fields[ip]) {
			l.setTableAt(t, key, v)
		}
	}
}

// enterJIT runs ci's compiled code from native offset off until it exits,
// leaving the exit's pc and reason in l.jitCtx.
func (l *State) enterJIT(ci *callInfo, c *luaClosure, p *prototype, jc *jitCode, off int32) {
	ctx := &l.jitCtx
	// Compiled code reads constants and upvalues only when the function
	// has them, so an empty slice's data pointer, whatever it is, does.
	frame := ci.frame
	ctx.frame = unsafe.Pointer(unsafe.SliceData(frame))
	ctx.constants = unsafe.Pointer(unsafe.SliceData(p.constants))
	ctx.upValues = unsafe.Pointer(unsafe.SliceData(c.upValues))
	ctx.barrier = 0
	if writeBarrier.enabled {
		ctx.barrier = 1
		l.jitBarrierRuns++
	}
	ctx.target = jc.base + uintptr(off)
	ctx.budget = jitBudget
	ctx.state = unsafe.Pointer(l)
	ctx.reason = call.Call(jc.base, unsafe.Pointer(ctx))
	l.jitRuns++
	runtime.KeepAlive(frame)
	runtime.KeepAlive(c)
}

// jitCall runs the CALL i at ip that compiled code exited at, when the
// callee is a Go function, or a compiled Lua function it can enter
// directly. It returns ci when the call is done and compiled code can go
// on after it, or the callee's new frame. It reports false, having changed
// nothing, when the interpreter must make the call.
func (l *State) jitCall(ci *callInfo, i instruction, ip pc) (*callInfo, bool) {
	a, b, c := i.a(), i.b(), i.c()
	if b == 0 { // arguments up to l.top, which compiled code does not track
		return nil, false
	}
	switch fv := ci.frame[a]; fv.kind() {
	case vkGoFunction, vkGoClosure:
		if c == 0 {
			return nil, false
		}
		l.jitCallGo(ci, i, ip)
		return ci, true
	case vkLuaClosure:
		f := fv.luaClosure()
		if f.prototype.isVarArg || f.prototype.jit == nil {
			return nil, false
		}
		ci.savedPC = ip + 1
		return l.callLua(ci, f, a, b-1, c-1), true
	}
	return nil, false
}

// jitCallGo runs the CALL i at ip, which has fixed arguments and results
// and calls the Go function or Go closure in its register A.
func (l *State) jitCallGo(ci *callInfo, i instruction, ip pc) {
	a, b, c := i.a(), i.b(), i.c()
	frame := ci.frame
	fv := frame[a]
	ci.savedPC = ip + 1
	if f := fv.goFunction(); f != nil && f.number != nil && b > 1 {
		if r, ok := f.number.tryCall(frame[a+1 : a+b]); ok {
			l.numberResult(ci, a, c-1, f.number.results, r)
			return
		}
	}
	l.top = ci.stackIndex(a + b)
	l.callGo(fv, ci.stackIndex(a), c-1)
	l.top = ci.top
}

// jitCallGoFunction runs the CALL i at ip that compiled code exited at
// with jitExitCallGo: fixed arguments and results, to the Go function or
// Go closure whose object compiled code left in l.jitCtx.callee, with the
// frame's address in l.jitCtx.frame. It does what callGo does, without
// reloading the callee from the frame, and copies the results itself
// unless a hook is set. No hook is set on entry, as compiled code runs
// only without one.
func (l *State) jitCallGoFunction(ci *callInfo, i instruction, ip pc) {
	ctx := &l.jitCtx
	f := *(*Function)(ctx.callee)
	ctx.callee = nil
	a, b, wanted := i.a(), i.b(), i.c()-1
	base := int((uintptr(ctx.frame) - uintptr(unsafe.Pointer(unsafe.SliceData(l.stack)))) / unsafe.Sizeof(value{}))
	function := base + a
	ci.savedPC = ip + 1
	l.top = function + b
	l.checkStack(MinStack)
	l.pushGoFrame(function, wanted)
	n := f(l)
	apiCheckStackSpace(l, n)
	if l.hookMask != 0 { // the function set one
		l.postCall(l.top - n)
	} else {
		// A loop, not copy: copy of a few values is a runtime call.
		l.callInfo = ci
		first, k := l.top-n, 0
		for ; k < wanted && k < n; k++ {
			l.stack[function+k] = l.stack[first+k]
		}
		for ; k < wanted; k++ {
			l.stack[function+k] = nilValue
		}
	}
	l.top = ci.top
}

// jitCallNumber runs the CALL i at ip that compiled code exited at with
// jitExitCallNumber, leaving the number function's *goFunction in
// l.jitCtx.callee and the frame's address in l.jitCtx.frame. It calls the
// function without a frame, as the interpreter does, when the arguments
// fit it, and otherwise as an ordinary Go function.
func (l *State) jitCallNumber(ci *callInfo, i instruction, ip pc) {
	ctx := &l.jitCtx
	nf := (*goFunction)(ctx.callee).number
	a, b, wanted := i.a(), i.b(), i.c()-1
	function := int((uintptr(ctx.frame)-uintptr(unsafe.Pointer(unsafe.SliceData(l.stack))))/unsafe.Sizeof(value{})) + a
	r, ok := nf.tryCall(l.stack[function+1 : function+b])
	if !ok {
		l.jitCallGoFunction(ci, i, ip)
		return
	}
	ctx.callee = nil
	ci.savedPC = ip + 1
	for k := range wanted { // a loop, not clear: see jitCallGoFunction
		l.stack[function+k] = nilValue
	}
	if wanted > 0 && nf.results == 1 {
		l.stack[function] = numberValue(r)
	}
	l.top = ci.top
}

// jitReturn runs the RETURN i that compiled code exited at, when it returns
// a fixed number of results to a Lua caller in the same interpreter loop
// that wants a fixed number. It reports false, having changed nothing,
// when the interpreter must return.
func (l *State) jitReturn(ci *callInfo, i instruction) bool {
	a, b, wanted := i.a(), i.b(), ci.resultCount
	if b == 0 || wanted < 0 || !ci.isCallStatus(callStatusReentry) {
		return false
	}
	if len(ci.closure.prototype.prototypes) > 0 {
		l.close(ci.base())
	}
	res, results := l.stack[ci.function:ci.function+wanted], ci.frame[a:a+b-1]
	n := copy(res, results)
	clear(res[n:])
	ci = ci.previous
	l.callInfo, l.top = ci, ci.top
	return true
}
