//go:build (darwin || linux) && (arm64 || amd64)

package lua

import (
	"math"
	"math/bits"

	"github.com/matjam/apogee/internal/bytecode"
)

// Integer % and // by a constant, without a divide instruction. A 64-bit
// divide costs a dozen or more cycles; compiled code divides by a constant
// d with a multiply as compilers do (Hacker's Delight, chapter 10):
//
//   - d a power of two, 2^k: n // d is n >> k, arithmetic, and n % d is
//     n & (d-1), both floored as Lua's are.
//   - otherwise: the truncated quotient q is the high half of n*magic,
//     corrected by n and shifted, plus one for a negative quotient; the
//     remainder is n - q*d; and both move one step toward minus infinity
//     when the remainder's sign differs from d's.
//
// divRef computes what the compiled code does, step for step, for tests.

// A divPlan is how compiled code divides by the constant d.
type divPlan struct {
	d     int64
	pow2  bool  // d is 2^shift
	magic int64 // the multiplier, when !pow2
	shift uint  // the shift after the multiply, or log2 d
	addN  bool  // add n to the high half: magic < 0 < d
	subN  bool  // subtract n from it: d < 0 < magic
}

// planDivide returns how to divide by d, or false where the divide
// instruction stays: d is 0, 1, -1 or math.MinInt64, or does not fit a
// 32-bit immediate, which the code multiplies the quotient by.
func planDivide(d int64) (divPlan, bool) {
	if d == 0 || d == 1 || d == -1 || d == math.MinInt64 || d != int64(int32(d)) {
		return divPlan{}, false
	}
	if d > 0 && d&(d-1) == 0 {
		return divPlan{d: d, pow2: true, shift: uint(bits.TrailingZeros64(uint64(d)))}, true
	}
	magic, shift := magicSigned(d)
	return divPlan{d: d, magic: magic, shift: shift, addN: d > 0 && magic < 0, subN: d < 0 && magic > 0}, true
}

// constantDivisor returns the plan for an RK field of % or // that is an
// integer constant, or false.
func constantDivisor(p *prototype, field int) (divPlan, bool) {
	if !bytecode.IsConstant(field) {
		return divPlan{}, false
	}
	k := p.Constants[bytecode.ConstantIndex(field)]
	if !k.isInteger() {
		return divPlan{}, false
	}
	return planDivide(k.i())
}

// magicSigned is Hacker's Delight's magic for signed division by d, with
// 2 <= |d| < 2^63: the multiplier and the shift.
func magicSigned(d int64) (int64, uint) {
	const two63 = uint64(1) << 63
	ad := uint64(d)
	if d < 0 {
		ad = -ad
	}
	t := two63 + uint64(d)>>63
	anc := t - 1 - t%ad // |nc|
	p := 63
	q1, r1 := two63/anc, two63%anc // 2^p / |nc| and its remainder
	q2, r2 := two63/ad, two63%ad   // 2^p / |d| and its remainder
	for {
		p++
		q1, r1 = 2*q1, 2*r1
		if r1 >= anc {
			q1, r1 = q1+1, r1-anc
		}
		q2, r2 = 2*q2, 2*r2
		if r2 >= ad {
			q2, r2 = q2+1, r2-ad
		}
		if delta := ad - r2; q1 > delta || q1 == delta && r1 != 0 {
			break
		}
	}
	m := int64(q2 + 1)
	if d < 0 {
		m = -m
	}
	return m, uint(p - 64)
}

// divRef returns n // d and n % d as the compiled code computes them.
func divRef(n int64, p divPlan) (quo, rem int64) {
	if p.pow2 {
		return n >> p.shift, n & (p.d - 1)
	}
	hi, _ := bits.Mul64(uint64(n), uint64(p.magic))
	q := int64(hi) // the signed high half: correct the unsigned one
	if n < 0 {
		q -= p.magic
	}
	if p.magic < 0 {
		q -= n
	}
	if p.addN {
		q += n
	}
	if p.subN {
		q -= n
	}
	q >>= p.shift
	if p.d > 0 {
		q += int64(uint64(n) >> 63)
	} else {
		q += int64(uint64(q) >> 63)
	}
	r := n - q*p.d
	if r != 0 && (r^p.d) < 0 {
		r += p.d
		q--
	}
	return q, r
}
