//go:build (darwin || linux) && amd64 && !amd64.v3

package lua

import (
	"math"

	. "github.com/matjam/luart/internal/jit/amd64"
)

// Below GOAMD64=v3 Go compiles math/sin.go without fused multiply-adds,
// so compiled sin and cos follow it with one rounding per operation, in
// the source's order. At v3 Go fuses some of them, and sin and cos are
// left to Go.

func (c *amd64Compiler) trigIntrinsics() []intrinsic {
	return []intrinsic{
		{funcValue(math.Sin), func() { c.trig(false) }},
		{funcValue(math.Cos), func() { c.trig(true) }},
	}
}

// fconst loads v into x.
func (c *amd64Compiler) fconst(x XReg, v float64) {
	c.a.MovImm(rTmp, math.Float64bits(v))
	c.a.MovqToX(x, rTmp)
}

// trig computes math.Sin, or math.Cos, of x0 into x0, exiting at the
// current instruction for arguments of 2^29 and more, which Go reduces
// with trigReduce, infinities and, for cos, NaN.
func (c *amd64Compiler) trig(cos bool) {
	a := &c.a
	exit := c.exit(c.ip)
	done := a.NewLabel()
	a.Ucomisd(0, 0)
	if cos {
		a.J(P, exit)
	} else {
		a.J(P, done) // sin(NaN) is its argument
		a.XorPD(7, 7)
		a.Ucomisd(0, 7)
		a.J(E, done) // and so is sin(±0)
		a.MovqFromX(rN, 0)
	}
	a.MovSD(1, 0)
	a.MovImm(rTmp, 1<<63-1)
	a.MovqToX(2, rTmp)
	a.AndPD(1, 2) // x = |x|
	c.fconst(2, 1<<29)
	a.Ucomisd(1, 2)
	a.J(AE, exit)
	// j = uint64(x * (4/Pi)); y = float64(j); if j is odd, j++ and y++.
	c.fconst(2, 4/math.Pi)
	a.MulSD(2, 1)
	a.Cvttsd2si(rIdx, 2)
	a.Cvtsi2sd(2, rIdx)
	even := a.NewLabel()
	a.Bt(rIdx, 0)
	a.J(AE, even)
	a.AddImm(rIdx, 1)
	c.fconst(3, 1)
	a.AddSD(2, 3)
	a.Bind(even)
	// z = ((x - y*PI4A) - y*PI4B) - y*PI4C
	for _, k := range []float64{trigPI4A, trigPI4B, trigPI4C} {
		c.fconst(4, k)
		a.MulSD(4, 2)
		a.SubSD(1, 4)
	}
	a.MovSD(2, 1)
	a.MulSD(2, 1) // zz
	// j is even: bit 2 is j&7 > 3, and bit 1 picks the other series.
	other, signs := a.NewLabel(), a.NewLabel()
	a.Bt(rIdx, 1)
	a.J(B, other)
	if cos {
		c.cosSeries()
	} else {
		c.sinSeries()
	}
	a.Jmp(signs)
	a.Bind(other)
	if cos {
		c.sinSeries()
	} else {
		c.cosSeries()
	}
	a.Bind(signs)
	flipped := a.NewLabel()
	a.Bt(rIdx, 2)
	a.J(AE, flipped)
	c.signMask(5)
	a.XorPD(0, 5)
	a.Bind(flipped)
	if cos {
		a.Bt(rIdx, 1)
	} else {
		a.Bt(rN, 63) // x was negative
	}
	a.J(AE, done)
	c.signMask(5)
	a.XorPD(0, 5)
	a.Bind(done)
}

// sinSeries computes x0 = z + z*zz*P(zz) from z in x1 and zz in x2.
func (c *amd64Compiler) sinSeries() {
	a := &c.a
	a.MovSD(3, 1)
	a.MulSD(3, 2)
	c.series(&trigSin)
	a.MulSD(3, 4)
	a.MovSD(0, 1)
	a.AddSD(0, 3)
}

// cosSeries computes x0 = 1 - 0.5*zz + zz*zz*Q(zz) from zz in x2.
func (c *amd64Compiler) cosSeries() {
	a := &c.a
	c.fconst(3, 0.5)
	a.MulSD(3, 2)
	c.fconst(0, 1)
	a.SubSD(0, 3)
	a.MovSD(3, 2)
	a.MulSD(3, 2)
	c.series(&trigCos)
	a.MulSD(3, 4)
	a.AddSD(0, 3)
}

// series evaluates the polynomial k at zz, in x2, by Horner's rule,
// leaving the result in x4.
func (c *amd64Compiler) series(k *[6]float64) {
	c.fconst(4, k[0])
	for _, v := range k[1:] {
		c.a.MulSD(4, 2)
		c.fconst(5, v)
		c.a.AddSD(4, 5)
	}
}
