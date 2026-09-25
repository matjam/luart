//go:build (darwin || linux) && (arm64 || amd64)

package lua

import "github.com/matjam/luart/internal/bytecode"

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

// kernelPlan is a loop that qualifies as a kernel.
type kernelPlan struct {
	start, latch int             // the body's first pc, and the FORLOOP's
	intLoop      bool            // an integer loop, or a float one
	types        map[int]numKind // each Lua register's type
	regs         map[int]int     // Lua register to its kernel register, from 0 in its class
	liveIn       []int           // registers read before written, checked on entry
	written      []int           // registers written back when the loop ends
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
// reports whether generated code can reach constant k.
func planKernel(p *prototype, latch int, intLoop bool, maxFloats, maxInts int, constOK func(k int) bool) *kernelPlan {
	code := p.Code
	fl := code[latch]
	start := latch + 1 + fl.SBx()
	if start > latch {
		return nil
	}
	k := &kernelPlan{start: start, latch: latch, intLoop: intLoop, types: map[int]numKind{}, regs: map[int]int{}}
	base := fl.A()
	var order []int // registers in order of first use
	use := func(r int) {
		if _, ok := k.types[r]; !ok {
			k.types[r] = kindAny
			order = append(order, r)
		}
	}
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
		default:
			return nil
		}
	}
	if !k.inferTypes(p) {
		return nil
	}
	floats, ints := 0, 0
	for _, r := range order {
		if k.types[r] == kindInt {
			k.regs[r], ints = ints, ints+1
		} else {
			k.regs[r], floats = floats, floats+1
		}
	}
	if floats > maxFloats || ints > maxInts {
		return nil
	}
	return k
}

// kind returns the type of RK field in k.
func (k *kernelPlan) kind(p *prototype, field int) numKind {
	if bytecode.IsConstant(field) {
		return constKind(p.Constants[bytecode.ConstantIndex(field)])
	}
	return k.types[field]
}

// result returns the type the body instruction i gives its register A,
// kindAny while an operand's type is unknown, and false if it cannot run
// in a kernel with these types.
func (k *kernelPlan) result(p *prototype, i bytecode.Instruction) (numKind, bool) {
	switch i.OpCode() {
	case bytecode.OpMove, bytecode.OpUnaryMinus:
		return k.types[i.B()], true
	case bytecode.OpLoadConstant:
		return constKind(p.Constants[i.Bx()]), true
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul:
		b, c := k.kind(p, i.B()), k.kind(p, i.C())
		switch {
		case b == kindFloat || c == kindFloat:
			return kindFloat, true
		case b == kindInt && c == kindInt:
			return kindInt, true
		}
		return kindAny, true
	case bytecode.OpDiv:
		return kindFloat, true
	case bytecode.OpMod, bytecode.OpIDiv:
		b := k.types[i.B()]
		return b, b != kindFloat // float % is fmod, which Go computes
	}
	return kindAny, true
}

// inferTypes gives every register in k one type, or reports false when
// the body needs a register to hold both. Registers nothing decides only
// combine with themselves and integers, so are integers, as a counter
// starting at 0 is.
func (k *kernelPlan) inferTypes(p *prototype) bool {
	code := p.Code
	propagate := func() bool {
		for changed := true; changed; {
			changed = false
			for ip := k.start; ip < k.latch; ip++ {
				i := code[ip]
				switch i.OpCode() {
				case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
					ip++
					continue
				case bytecode.OpJump:
					continue
				}
				t, ok := k.result(p, i)
				if !ok {
					return false
				}
				switch have := k.types[i.A()]; {
				case t == kindAny:
				case have == kindAny:
					k.types[i.A()], changed = t, true
				case have != t:
					return false
				}
			}
		}
		return true
	}
	for {
		if !propagate() {
			return false
		}
		undecided := -1
		for r, t := range k.types {
			if t == kindAny && (undecided < 0 || r < undecided) {
				undecided = r
			}
		}
		if undecided < 0 {
			break
		}
		k.types[undecided] = kindInt
	}
	// Comparisons: two of a type, or a float and an integer constant that
	// converts exactly.
	for ip := k.start; ip < k.latch; ip++ {
		switch i := code[ip]; i.OpCode() {
		case bytecode.OpEqual, bytecode.OpLessThan, bytecode.OpLessOrEqual:
			if b, c := k.kind(p, i.B()), k.kind(p, i.C()); b != c && !k.exactConstant(p, i.B()) && !k.exactConstant(p, i.C()) {
				return false
			}
			ip++
		}
	}
	return true
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
