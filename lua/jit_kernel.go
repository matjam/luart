//go:build (darwin || linux) && (arm64 || amd64)

package lua

import (
	"maps"
	"slices"

	"github.com/matjam/apogee/internal/bytecode"
)

// Numeric loop kernels.
//
// An innermost numeric for loop whose body only moves numbers, loads
// number constants, does arithmetic and compares numbers is also compiled
// as a kernel: each Lua register the loop uses lives in a machine register
// for the whole loop, a general-purpose one for an integer and a
// floating-point one for a float, so iterations run without loads, stores
// or type checks. Each register has one type throughout, which planKernel
// infers from the loop's kind (an integer or a float loop) and the body;
// a register nothing decides, such as an accumulator that only adds to
// itself or integer constants, is an integer. A loop may get two kernels, one for each
// kind. A kernel starts at the FORLOOP, after checking that the registers
// the body reads before writing hold numbers of their types, and writes
// the registers back when the loop ends or its budget runs out. When the
// check fails the loop runs in the ordinary compiled code, which comes
// back to the check at every iteration.

// A kernel may also call an intrinsic, and read and write buffers:
//
//   - GETUPVAL A, n followed, with only arithmetic between, by CALL A 2 2
//     of an intrinsic (sqrt, sin, cos) the upvalue holds when the function
//     compiles. The kernel checks on entry that the upvalue still holds it;
//     the GETUPVAL emits nothing, and the CALL computes the intrinsic on
//     registers.
//   - GETTABLE and SETTABLE of a buffer in a register the loop only reads,
//     at an integer key. The kernel checks on entry that the register holds
//     a buffer, of floats if the loop reads it.
//
// Those instructions can find what the kernel cannot handle: a key outside
// the buffer, or an argument sin or cos leave to Go. They leave the kernel
// there, a side exit: the kernel writes its registers back, as at the end
// of the loop, and the ordinary code runs the instruction and the rest of
// the iteration. Registers the iteration has yet to write hold stale
// numbers until it does.

// kernelPlan is a loop that qualifies as a kernel.
type kernelPlan struct {
	start, latch int             // the body's first pc, and the FORLOOP's
	intLoop      bool            // an integer loop, or a float one
	types        map[int]numKind // the loop's and live-in registers' types on entry
	liveIn       []int           // registers read before written, checked on entry
	written      []int           // registers written back when the loop ends
	calls        map[int]kernelCall
	virtual      map[int]bool // GETUPVAL pcs that emit nothing
	buffers      map[int]bool // registers holding buffers, true for those read

	// A register may hold an integer at one pc and a float at another, as
	// Lua reuses registers for temporaries: at gives each register's type
	// before each body pc and at the latch, results the type each body
	// instruction writes, and slots each register's machine register for
	// each type it takes.
	at      []map[int]numKind
	results map[int]numKind
	slots   map[kslot]int
}

// kernelCall is an intrinsic call in a kernel, by the CALL's pc: of fn,
// the intrinsic's Go function, which upvalue upValue must hold.
type kernelCall struct {
	upValue int
	fn      uint64
}

// kernelDivisor reports whether constant v may divide in a kernel: a
// nonzero integer small enough for an immediate.
func kernelDivisor(v value) bool {
	i, ok := v.integer()
	return ok && v.isInteger() && i != 0 && -(1<<31) <= i && i < 1<<31
}

// planKernel returns the kernel for the FORLOOP at latch in p, as an
// integer or a float loop, with at most maxFloats float and maxInts
// integer registers, or nil when its loop does not qualify. constOK
// reports whether generated code can reach constant k; intrinsic returns
// the intrinsic upvalue n holds, if it holds one compiled code computes.
func planKernel(p *prototype, latch int, intLoop bool, maxFloats, maxInts int, constOK func(k int) bool, intrinsic func(n int) (uint64, bool)) *kernelPlan {
	code := p.Code
	fl := code[latch]
	start := latch + 1 + fl.SBx()
	if start > latch {
		return nil
	}
	k := &kernelPlan{start: start, latch: latch, intLoop: intLoop, types: map[int]numKind{},
		calls: map[int]kernelCall{}, virtual: map[int]bool{}, buffers: map[int]bool{}}
	wrote := map[int]bool{} // registers the body writes, which cannot hold buffers
	used := map[int]bool{}  // registers holding numbers
	base := fl.A()
	use := func(r int) { used[r] = true }
	loopKind := kindFloat
	if intLoop {
		loopKind = kindInt
	}
	for r := base; r <= base+3; r++ {
		use(r)
		k.types[r] = loopKind
	}
	// FORLOOP defines the loop registers before the body; the index,
	// limit and step are checked on entry.
	defined := map[int]bool{base + 3: true}
	k.liveIn = []int{base, base + 1, base + 2}
	k.written = []int{base, base + 1, base + 3}
	if !intLoop {
		k.written = []int{base, base + 3} // a float loop's limit stays
	}
	seen := map[int]bool{base: true, base + 1: true, base + 2: true, base + 3: true}
	// skipped reports whether a jump from before ip lands after it, so
	// that a write at ip may not happen.
	skipped := func(ip int) bool {
		for j := start; j < ip; j++ {
			if t, ok := kernelJump(code, j, latch); ok && t > ip {
				return true
			}
		}
		return false
	}
	number := func(kk int) bool { return constOK(kk) && constKind(p.Constants[kk]) != kindAny }
	read := func(field int) bool {
		if bytecode.IsConstant(field) {
			return number(bytecode.ConstantIndex(field))
		}
		use(field)
		if !seen[field] {
			seen[field] = true
			if !defined[field] {
				k.liveIn = append(k.liveIn, field)
			}
		}
		return true
	}
	write := func(r, ip int) bool {
		wrote[r] = true
		use(r)
		if !seen[r] {
			seen[r] = true
			if skipped(ip) {
				k.liveIn = append(k.liveIn, r) // may keep its old value
			} else {
				defined[r] = true
			}
		}
		k.written = append(k.written, r)
		return true
	}
	for ip := start; ip < latch; ip++ {
		i := code[ip]
		switch i.OpCode() {
		case bytecode.OpMove:
			if !read(i.B()) || !write(i.A(), ip) {
				return nil
			}
		case bytecode.OpLoadConstant:
			if !number(i.Bx()) || !write(i.A(), ip) {
				return nil
			}
		case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv:
			if !read(i.B()) || !read(i.C()) || !write(i.A(), ip) {
				return nil
			}
		case bytecode.OpMod, bytecode.OpIDiv:
			// An integer divided by a constant: never zero, and fixed sign.
			if !bytecode.IsConstant(i.C()) || !read(i.C()) || !kernelDivisor(p.Constants[bytecode.ConstantIndex(i.C())]) {
				return nil
			}
			if !read(i.B()) || !write(i.A(), ip) {
				return nil
			}
		case bytecode.OpUnaryMinus:
			if !read(i.B()) || !write(i.A(), ip) {
				return nil
			}
		case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
			if !read(i.B()) || !read(i.C()) {
				return nil
			}
			if _, ok := kernelJump(code, ip, latch); !ok {
				return nil
			}
			ip++ // the JMP
		case bytecode.OpJump:
			if _, ok := kernelJump(code, ip, latch); !ok {
				return nil
			}
		case bytecode.OpGetUpValue: // an intrinsic for the CALL it feeds
			call, ok := intrinsicCall(code, ip, start, latch)
			fn, isIntrinsic := intrinsic(i.B())
			if !ok || !isIntrinsic {
				return nil
			}
			k.calls[call] = kernelCall{upValue: i.B(), fn: fn}
			k.virtual[ip] = true
		case bytecode.OpCall:
			if _, ok := k.calls[ip]; !ok || !read(i.A()+1) || !write(i.A(), ip) {
				return nil
			}
		case bytecode.OpGetTable: // a buffer's element, at a key checked below
			if !read(i.C()) || !write(i.A(), ip) {
				return nil
			}
			k.buffers[i.B()] = true
		case bytecode.OpSetTable:
			if !read(i.B()) || !read(i.C()) {
				return nil
			}
			if _, ok := k.buffers[i.A()]; !ok {
				k.buffers[i.A()] = false
			}
		default:
			return nil
		}
	}
	for r := range k.buffers {
		// A buffer register the loop never writes, and not a number.
		if used[r] || wrote[r] {
			return nil
		}
		// Nor one the function makes a table in: that register is surely
		// a table, and a kernel whose entry check fails costs the ordinary
		// loop the check each iteration.
		for _, i := range code {
			if i.OpCode() == bytecode.OpNewTable && i.A() == r {
				return nil
			}
		}
	}
	if len(k.calls) > 0 || len(k.buffers) > 0 {
		// A side exit leaves mid-iteration, where the enclosing function's
		// locals, below the loop's registers, that the body has yet to write
		// must hold the last iteration's values: an error from there may
		// close upvalues over them. So they are live in, and always held.
		// The body's own registers are dead until it writes them.
		for _, r := range k.writtenOnce() {
			if r < base && !slices.Contains(k.liveIn, r) {
				k.liveIn = append(k.liveIn, r)
			}
		}
	}
	for _, r := range k.liveIn {
		if _, ok := k.types[r]; !ok {
			k.types[r] = kindAny // decided by inferTypes
		}
	}
	if !k.inferTypes(p) {
		return nil
	}
	// A machine register for each type each register takes, in order of
	// first appearance.
	k.slots = map[kslot]int{}
	floats, ints := 0, 0
	assign := func(r int, t numKind) {
		s := kslot{r, t}
		if _, ok := k.slots[s]; ok {
			return
		}
		if t == kindInt {
			k.slots[s], ints = ints, ints+1
		} else {
			k.slots[s], floats = floats, floats+1
		}
	}
	for r := base; r <= base+3; r++ {
		assign(r, loopKind)
	}
	for _, r := range k.liveIn {
		assign(r, k.types[r])
	}
	for ip := start; ip < latch; ip++ {
		if t, ok := k.results[ip]; ok {
			assign(code[ip].A(), t)
		}
	}
	if floats > maxFloats || ints > maxInts {
		return nil
	}
	return k
}

// kslot is a register holding a type, which has a machine register.
type kslot struct {
	r int
	t numKind
}

// upValueIntrinsic returns the Go function of the intrinsic cl's upvalue n
// holds, one of fns, if it holds one.
func upValueIntrinsic(cl *luaClosure, n int, fns []uint64) (uint64, bool) {
	if cl == nil || n >= len(cl.upValues) || cl.upValues[n] == nil {
		return 0, false
	}
	f := cl.upValues[n].value().goFunction()
	if f == nil || f.number == nil || f.number.unary == nil {
		return 0, false
	}
	fn := funcValue(f.number.unary)
	return fn, slices.Contains(fns, fn)
}

// intrinsicCall returns the pc of the CALL A 2 2 that the GETUPVAL A at ip
// feeds, when only arithmetic that leaves A alone lies between them, and no
// jump in the body lands there: nothing between can leave the kernel while
// A holds the function only in the frame.
func intrinsicCall(code []bytecode.Instruction, ip, start, latch int) (int, bool) {
	a := code[ip].A()
	for j := ip + 1; j < latch; j++ {
		i := code[j]
		for from := start; from < latch; from++ {
			if t, ok := kernelJump(code, from, latch); ok && t == j {
				return 0, false
			}
		}
		switch i.OpCode() {
		case bytecode.OpCall:
			return j, i.A() == a && i.B() == 2 && i.C() == 2
		case bytecode.OpMove, bytecode.OpUnaryMinus:
			if i.A() == a || i.B() == a {
				return 0, false
			}
		case bytecode.OpLoadConstant:
			if i.A() == a {
				return 0, false
			}
		case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod, bytecode.OpIDiv:
			if i.A() == a || i.B() == a || i.C() == a {
				return 0, false
			}
		default:
			return 0, false
		}
	}
	return 0, false
}

// guardedUpValues returns the upvalues k's intrinsic calls come from, each
// once, in order.
func (k *kernelPlan) guardedUpValues() []int {
	var ns []int
	for _, kc := range k.calls {
		if !slices.Contains(ns, kc.upValue) {
			ns = append(ns, kc.upValue)
		}
	}
	slices.Sort(ns)
	return ns
}

// upValueFn returns the intrinsic upvalue n must hold.
func (k *kernelPlan) upValueFn(n int) uint64 {
	for _, kc := range k.calls {
		if kc.upValue == n {
			return kc.fn
		}
	}
	return 0
}

// bufferRegs returns the registers k indexes as buffers, in order.
func (k *kernelPlan) bufferRegs() []int {
	var rs []int
	for r := range k.buffers {
		rs = append(rs, r)
	}
	slices.Sort(rs)
	return rs
}

// kind returns the type of RK field before the body pc ip.
func (k *kernelPlan) kind(p *prototype, ip, field int) numKind {
	if bytecode.IsConstant(field) {
		return constKind(p.Constants[bytecode.ConstantIndex(field)])
	}
	return k.at[ip-k.start][field]
}

// result returns the type instruction i gives its register A when the
// registers have the types in state, kindAny while an operand's type is
// unknown, and false if it cannot run in a kernel with these types.
func result(p *prototype, i bytecode.Instruction, state map[int]numKind) (numKind, bool) {
	kind := func(field int) numKind {
		if bytecode.IsConstant(field) {
			return constKind(p.Constants[bytecode.ConstantIndex(field)])
		}
		return state[field]
	}
	switch i.OpCode() {
	case bytecode.OpMove, bytecode.OpUnaryMinus:
		return state[i.B()], true
	case bytecode.OpLoadConstant:
		return constKind(p.Constants[i.Bx()]), true
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul:
		b, c := kind(i.B()), kind(i.C())
		switch {
		case b == kindFloat || c == kindFloat:
			return kindFloat, true
		case b == kindInt && c == kindInt:
			return kindInt, true
		}
		return kindAny, true
	case bytecode.OpDiv, bytecode.OpCall, bytecode.OpGetTable: // intrinsics and buffers of floats
		return kindFloat, true
	case bytecode.OpMod, bytecode.OpIDiv:
		b := state[i.B()]
		return b, b != kindFloat // float % is fmod, which Go computes
	}
	return kindAny, true
}

// inferTypes types each register at each pc of the body, choosing the
// live-in registers' types on entry: a register the body writes carries the
// type it ends an iteration with into the next, and a register nothing
// decides takes the type guess gives it. It reports false when two paths
// reach a pc with a register of different types, or an instruction cannot
// run on them.
func (k *kernelPlan) inferTypes(p *prototype) bool {
	for {
		at, results, ok := k.flow(p)
		if !ok {
			return false
		}
		end := at[k.latch-k.start]
		changed := false
		for _, r := range k.liveIn {
			if t, ok := end[r]; ok && t != kindAny && k.types[r] == kindAny {
				k.types[r], changed = t, true
			}
		}
		if changed {
			continue
		}
		undecided := -1
		for _, r := range k.liveIn {
			if k.types[r] == kindAny && (undecided < 0 || r < undecided) {
				undecided = r
			}
		}
		if undecided >= 0 {
			k.types[undecided] = k.guess(p, at, undecided)
			continue
		}
		k.at, k.results = at, results
		return k.checkTypes(p)
	}
}

// guess returns the likelier type of live-in register r, which nothing in
// the body decides: a float if arithmetic and comparisons meet it with
// floats more often than with integers, as a time or scale parameter is,
// and otherwise an integer, as a counter or an enclosing loop's index is.
// A wrong guess costs only the kernel: its entry check fails and the
// ordinary code runs.
func (k *kernelPlan) guess(p *prototype, at []map[int]numKind, r int) numKind {
	floats, ints := 0, 0
	for ip := k.start; ip < k.latch; ip++ {
		i := p.Code[ip]
		switch i.OpCode() {
		case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
			bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		default:
			continue
		}
		var other int
		switch r {
		case i.B():
			other = i.C()
		case i.C():
			other = i.B()
		default:
			continue
		}
		t := at[ip-k.start][other]
		if bytecode.IsConstant(other) {
			t = constKind(p.Constants[bytecode.ConstantIndex(other)])
		}
		switch t {
		case kindFloat:
			floats++
		case kindInt:
			ints++
		}
	}
	if floats > ints {
		return kindFloat
	}
	return kindInt
}

// flow computes the registers' types before each body pc, and at the
// latch, from their types on entry, and the type each instruction writes.
// Jumps in a body only go forward, so one pass finds them. It reports
// false where two paths reach a pc with a register of different types.
func (k *kernelPlan) flow(p *prototype) ([]map[int]numKind, map[int]numKind, bool) {
	code := p.Code
	at := make([]map[int]numKind, k.latch-k.start+1)
	incoming := make([][]map[int]numKind, len(at))
	results := map[int]numKind{}
	cur := maps.Clone(k.types) // nil where no path falls through
	for ip := k.start; ip <= k.latch; ip++ {
		states := incoming[ip-k.start]
		if cur != nil {
			states = append(states, cur)
		}
		merged, ok := mergeTypes(states)
		if !ok {
			return nil, nil, false
		}
		at[ip-k.start] = merged
		if ip == k.latch {
			break
		}
		cur = maps.Clone(merged)
		i := code[ip]
		switch i.OpCode() {
		case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
			t, _ := kernelJump(code, ip, k.latch)
			incoming[t-k.start] = append(incoming[t-k.start], cur)
			at[ip+1-k.start] = cur // the JMP, which the test consumes
			ip++
		case bytecode.OpJump:
			t, _ := kernelJump(code, ip, k.latch)
			incoming[t-k.start] = append(incoming[t-k.start], cur)
			cur = nil
		case bytecode.OpGetUpValue, bytecode.OpSetTable: // no number written
		default:
			t, ok := result(p, i, cur)
			if !ok {
				return nil, nil, false
			}
			results[ip] = t
			cur[i.A()] = t
		}
	}
	return at, results, true
}

// mergeTypes merges the registers' types where paths meet: a register
// defined on only some of them is not defined, and one of two types is a
// conflict. An undecided type takes the other path's: it comes only from a
// live-in register inferTypes has yet to type, and so is optimistic, as
// the last pass, with every type decided, checks the merge exactly.
func mergeTypes(states []map[int]numKind) (map[int]numKind, bool) {
	if len(states) == 0 {
		return map[int]numKind{}, true // unreachable
	}
	out := maps.Clone(states[0])
	for _, s := range states[1:] {
		for r, t := range out {
			switch u, ok := s[r]; {
			case !ok:
				delete(out, r)
			case t == u || u == kindAny:
			case t == kindAny:
				out[r] = u
			default:
				return nil, false
			}
		}
	}
	return out, true
}

// checkTypes reports whether every instruction of the body can run on its
// operands' types: known, integer keys, comparisons of two of a type or a
// float and an integer constant that converts exactly, and a register the
// body writes that ends each iteration with the type it starts with.
func (k *kernelPlan) checkTypes(p *prototype) bool {
	code := p.Code
	end := k.at[k.latch-k.start]
	for _, r := range k.writtenOnce() {
		if t, ok := k.types[r]; ok && end[r] != t {
			return false
		}
	}
	for ip := k.start; ip < k.latch; ip++ {
		i := code[ip]
		known := func(fields ...int) bool {
			for _, f := range fields {
				if k.kind(p, ip, f) == kindAny {
					return false
				}
			}
			return true
		}
		switch i.OpCode() {
		case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
			b, c := k.kind(p, ip, i.B()), k.kind(p, ip, i.C())
			if !known(i.B(), i.C()) || b != c && !k.exactConstant(p, i.B()) && !k.exactConstant(p, i.C()) {
				return false
			}
			ip++
			continue
		case bytecode.OpJump, bytecode.OpGetUpValue:
			continue
		case bytecode.OpSetTable:
			if k.kind(p, ip, i.B()) != kindInt || !known(i.C()) {
				return false
			}
			continue
		case bytecode.OpGetTable:
			if k.kind(p, ip, i.C()) != kindInt {
				return false
			}
		case bytecode.OpCall:
			if !known(i.A() + 1) {
				return false
			}
		case bytecode.OpMove, bytecode.OpUnaryMinus:
			if !known(i.B()) {
				return false
			}
		case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod, bytecode.OpIDiv:
			if !known(i.B(), i.C()) {
				return false
			}
		}
		if k.results[ip] == kindAny {
			return false
		}
	}
	return true
}

// typeAt returns register r's type before the body pc ip.
func (k *kernelPlan) typeAt(ip, r int) numKind { return k.at[ip-k.start][r] }

// slot returns the index of register r's machine register for type t, in
// its class.
func (k *kernelPlan) slot(r int, t numKind) int {
	n, ok := k.slots[kslot{r, t}]
	if !ok {
		panic("kernel: register without a machine register for its type")
	}
	return n
}

// exactConstant reports whether RK field is an integer constant that
// converts to a float exactly.
func (k *kernelPlan) exactConstant(p *prototype, field int) bool {
	if !bytecode.IsConstant(field) {
		return false
	}
	v := p.Constants[bytecode.ConstantIndex(field)]
	return v.isInteger() && exactFloat(v.i())
}

// kernelJump returns where the jump at ip, or the JMP after the test at
// ip, goes, and false unless it is forward, closes no upvalues, and stays
// in the loop body or goes to its FORLOOP at latch.
func kernelJump(code []bytecode.Instruction, ip, latch int) (int, bool) {
	i := code[ip]
	switch i.OpCode() {
	case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
		ip++
		i = code[ip]
	case bytecode.OpJump:
	default:
		return 0, false
	}
	if i.OpCode() != bytecode.OpJump || i.A() != 0 {
		return 0, false
	}
	t := ip + 1 + i.SBx()
	return t, t > ip && t <= latch
}

// writtenOnce returns k's written registers without repeats.
func (k *kernelPlan) writtenOnce() []int {
	var out []int
	done := map[int]bool{}
	for _, r := range k.written {
		if !done[r] {
			done[r] = true
			out = append(out, r)
		}
	}
	return out
}

// A numKind is what compiling knows of an operand's number type: a
// constant's, or nothing, for a register.
type numKind uint8

const (
	kindAny numKind = iota
	kindFloat
	kindInt
)

// constKind is the number type of constant v, or kindAny if it is not a
// number.
func constKind(v value) numKind {
	switch {
	case v.isFloat():
		return kindFloat
	case v.isInteger():
		return kindInt
	}
	return kindAny
}

// exactFloat reports whether the integer i converts to a float exactly,
// so that comparing the float compares i.
func exactFloat(i int64) bool { return -(1<<53) <= i && i <= 1<<53 }
