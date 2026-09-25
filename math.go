package luart

import (
	"math"
	"math/rand"
)

const radiansPerDegree = math.Pi / 180.0

var mathUnaryFunctions = []struct {
	name string
	f    func(float64) float64
}{
	{"abs", math.Abs},
	{"acos", math.Acos},
	{"asin", math.Asin},
	{"atan", math.Atan},
	{"ceil", math.Ceil},
	{"cosh", math.Cosh},
	{"cos", math.Cos},
	{"deg", func(x float64) float64 { return x / radiansPerDegree }},
	{"exp", math.Exp},
	{"floor", math.Floor},
	{"rad", func(x float64) float64 { return x * radiansPerDegree }},
	{"sinh", math.Sinh},
	{"sin", math.Sin},
	{"sqrt", math.Sqrt},
	{"tanh", math.Tanh},
	{"tan", math.Tan},
}

var mathBinaryFunctions = []struct {
	name string
	f    func(float64, float64) float64
}{
	{"atan2", math.Atan2},
	{"fmod", math.Mod},
	{"pow", math.Pow},
}

func reduce(f func(float64, float64) float64) Function {
	return func(l *State) int {
		n := l.Top() // number of arguments
		v := CheckNumber(l, 1)
		for i := 2; i <= n; i++ {
			v = f(v, CheckNumber(l, i))
		}
		l.PushNumber(v)
		return 1
	}
}

var mathLibrary = []RegistryFunction{
	{"frexp", func(l *State) int {
		f, e := math.Frexp(CheckNumber(l, 1))
		l.PushNumber(f)
		l.PushInteger(e)
		return 2
	}},
	{"ldexp", func(l *State) int {
		x, e := CheckNumber(l, 1), CheckInteger(l, 2)
		l.PushNumber(math.Ldexp(x, e))
		return 1
	}},
	{"log", func(l *State) int {
		x := CheckNumber(l, 1)
		if l.IsNoneOrNil(2) {
			l.PushNumber(math.Log(x))
		} else if base := CheckNumber(l, 2); base == 10.0 {
			l.PushNumber(math.Log10(x))
		} else {
			l.PushNumber(math.Log(x) / math.Log(base))
		}
		return 1
	}},
	{"max", reduce(math.Max)},
	{"min", reduce(math.Min)},
	{"modf", func(l *State) int {
		i, f := math.Modf(CheckNumber(l, 1))
		l.PushNumber(i)
		l.PushNumber(f)
		return 2
	}},
	{"random", func(l *State) int {
		r := rand.Float64()
		switch l.Top() {
		case 0: // no arguments
			l.PushNumber(r)
		case 1: // upper limit only
			u := CheckNumber(l, 1)
			ArgumentCheck(l, 1.0 <= u, 1, "interval is empty")
			l.PushNumber(math.Floor(r*u) + 1.0) // [1, u]
		case 2: // lower and upper limits
			lo, u := CheckNumber(l, 1), CheckNumber(l, 2)
			ArgumentCheck(l, lo <= u, 2, "interval is empty")
			l.PushNumber(math.Floor(r*(u-lo+1)) + lo) // [lo, u]
		default:
			Errorf(l, "wrong number of arguments")
		}
		return 1
	}},
	{"randomseed", func(l *State) int {
		rand.Seed(int64(CheckUnsigned(l, 1)))
		rand.Float64() // discard first value to avoid undesirable correlations
		return 0
	}},
}

// MathOpen opens the math library. Usually passed to Require.
func MathOpen(l *State) int {
	l.CreateTable(0, len(mathLibrary)+len(mathUnaryFunctions)+len(mathBinaryFunctions)+2)
	SetFunctions(l, mathLibrary, 0)
	for _, f := range mathUnaryFunctions {
		l.PushNumberFunction(f.f)
		l.SetField(-2, f.name)
	}
	for _, f := range mathBinaryFunctions {
		l.PushNumberFunction(f.f)
		l.SetField(-2, f.name)
	}
	l.PushNumber(3.1415926535897932384626433832795) // TODO use math.Pi instead? Values differ.
	l.SetField(-2, "pi")
	l.PushNumber(math.MaxFloat64)
	l.SetField(-2, "huge")
	return 1
}
