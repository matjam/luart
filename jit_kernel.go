//go:build (darwin || linux) && (arm64 || amd64)

package luart

// Numeric loop kernels.
//
// An innermost numeric for loop whose body only moves numbers, loads
// number constants, does arithmetic and compares numbers is also compiled
// as a kernel: each Lua register the loop uses lives in its own
// floating-point register for the whole loop, so iterations run without
// loads, stores or type checks. The kernel starts at the FORLOOP, after
// checking that the registers the body reads before writing hold numbers,
// and writes the registers back when the loop ends or its budget runs
// out. When the check fails the loop runs in the ordinary compiled code,
// which comes back to the check at every iteration.

// kernelPlan is a loop that qualifies as a kernel.
type kernelPlan struct {
	start, latch int         // the body's first pc, and the FORLOOP's
	regs         map[int]int // Lua register to its kernel register, from 0
	liveIn       []int       // registers read before written, checked on entry
	written      []int       // registers written back when the loop ends
}

// planKernel returns the kernel for the FORLOOP at latch in p, with at
// most maxRegs registers, or nil when its loop does not qualify. constOK
// reports whether generated code can reach constant k.
func planKernel(p *prototype, latch, maxRegs int, constOK func(k int) bool) *kernelPlan {
	code := p.code
	fl := code[latch]
	start := latch + 1 + fl.sbx()
	if start > latch {
		return nil
	}
	k := &kernelPlan{start: start, latch: latch, regs: map[int]int{}}
	base := fl.a()
	use := func(r int) bool {
		if _, ok := k.regs[r]; !ok {
			if len(k.regs) == maxRegs {
				return false
			}
			k.regs[r] = len(k.regs)
		}
		return true
	}
	for r := base; r <= base+3; r++ {
		if !use(r) {
			return nil
		}
	}
	// FORLOOP defines the loop registers before the body; the index,
	// limit and step are checked on entry.
	defined := map[int]bool{base + 3: true}
	k.liveIn = []int{base, base + 1, base + 2}
	k.written = []int{base, base + 3}
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
	number := func(kk int) bool { return constOK(kk) && p.constants[kk].isNumber() }
	read := func(field int) bool {
		if isConstant(field) {
			return number(constantIndex(field))
		}
		if !use(field) {
			return false
		}
		if !seen[field] {
			seen[field] = true
			if !defined[field] {
				k.liveIn = append(k.liveIn, field)
			}
		}
		return true
	}
	write := func(r, ip int) bool {
		if !use(r) {
			return false
		}
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
		switch i.opCode() {
		case opMove:
			if !read(i.b()) || !write(i.a(), ip) {
				return nil
			}
		case opLoadConstant:
			if !number(i.bx()) || !write(i.a(), ip) {
				return nil
			}
		case opAdd, opSub, opMul, opDiv, opMod:
			if !read(i.b()) || !read(i.c()) || !write(i.a(), ip) {
				return nil
			}
		case opUnaryMinus:
			if !read(i.b()) || !write(i.a(), ip) {
				return nil
			}
		case opEqual, opLessThan, opLessOrEqual:
			if !read(i.b()) || !read(i.c()) {
				return nil
			}
			if _, ok := kernelJump(code, ip, latch); !ok {
				return nil
			}
			ip++ // the JMP
		case opJump:
			if _, ok := kernelJump(code, ip, latch); !ok {
				return nil
			}
		default:
			return nil
		}
	}
	return k
}

// kernelJump returns where the jump at ip, or the JMP after the test at
// ip, goes, and false unless it is forward, closes no upvalues, and stays
// in the loop body or goes to its FORLOOP at latch.
func kernelJump(code []instruction, ip, latch int) (int, bool) {
	i := code[ip]
	switch i.opCode() {
	case opEqual, opLessThan, opLessOrEqual:
		ip++
		i = code[ip]
	case opJump:
	default:
		return 0, false
	}
	if i.opCode() != opJump || i.a() != 0 {
		return 0, false
	}
	t := ip + 1 + i.sbx()
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
