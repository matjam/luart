package lua

// NumberFunction lists the plain Go function types that PushNumberFunction
// accepts: up to four float64 arguments, returning one float64 or nothing.
type NumberFunction interface {
	func(float64) float64 |
		func(float64, float64) float64 |
		func(float64, float64, float64) float64 |
		func(float64, float64, float64, float64) float64 |
		func(float64) |
		func(float64, float64) |
		func(float64, float64, float64) |
		func(float64, float64, float64, float64)
}

const maxNumberArgs = 4

// numberFunction is a NumberFunction the VM can call without a call frame.
type numberFunction struct {
	arity, results int
	unary          func(float64) float64 // set for func(float64) float64
	call           func([maxNumberArgs]float64) float64
}

func newNumberFunction(f any) *numberFunction {
	switch f := f.(type) {
	case func(float64) float64:
		return &numberFunction{arity: 1, results: 1, unary: f, call: func(a [maxNumberArgs]float64) float64 { return f(a[0]) }}
	case func(float64, float64) float64:
		return &numberFunction{arity: 2, results: 1, call: func(a [maxNumberArgs]float64) float64 { return f(a[0], a[1]) }}
	case func(float64, float64, float64) float64:
		return &numberFunction{arity: 3, results: 1, call: func(a [maxNumberArgs]float64) float64 { return f(a[0], a[1], a[2]) }}
	case func(float64, float64, float64, float64) float64:
		return &numberFunction{arity: 4, results: 1, call: func(a [maxNumberArgs]float64) float64 { return f(a[0], a[1], a[2], a[3]) }}
	case func(float64):
		return &numberFunction{arity: 1, call: func(a [maxNumberArgs]float64) float64 { f(a[0]); return 0 }}
	case func(float64, float64):
		return &numberFunction{arity: 2, call: func(a [maxNumberArgs]float64) float64 { f(a[0], a[1]); return 0 }}
	case func(float64, float64, float64):
		return &numberFunction{arity: 3, call: func(a [maxNumberArgs]float64) float64 { f(a[0], a[1], a[2]); return 0 }}
	case func(float64, float64, float64, float64):
		return &numberFunction{arity: 4, call: func(a [maxNumberArgs]float64) float64 { f(a[0], a[1], a[2], a[3]); return 0 }}
	}
	panic("unsupported NumberFunction type")
}

// tryCall calls f directly when args are exactly its number arguments.
func (f *numberFunction) tryCall(args []value) (float64, bool) {
	if len(args) != f.arity {
		return 0, false
	}
	if f.unary != nil {
		if !args[0].isNumber() {
			return 0, false
		}
		return f.unary(args[0].f()), true
	}
	var a [maxNumberArgs]float64
	for i, v := range args {
		if !v.isNumber() {
			return 0, false
		}
		a[i] = v.f()
	}
	return f.call(a), true
}

// function is the ordinary Go function form, used when the fast path does
// not apply. It converts arguments like CheckNumber.
func (f *numberFunction) function(l *State) int {
	var a [maxNumberArgs]float64
	for i := range f.arity {
		a[i] = CheckNumber(l, i+1)
	}
	r := f.call(a)
	if f.results == 1 {
		l.PushNumber(r)
	}
	return f.results
}

// PushNumberFunction pushes f as a Lua function. When a script calls it with
// exactly its number of arguments, all numbers, and no call or return hook
// is set, the VM calls f directly without a call frame. Otherwise the
// arguments convert as with CheckNumber.
func (l *State) PushNumberFunction[F NumberFunction](f F) {
	nf := newNumberFunction(f)
	l.apiPush(objectValue(&goFunction{Function: nf.function, number: nf}))
}

// RegisterNumberFunction sets f as the new value of global name. See
// PushNumberFunction.
func (l *State) RegisterNumberFunction[F NumberFunction](name string, f F) {
	l.PushNumberFunction(f)
	l.SetGlobal(name)
}
