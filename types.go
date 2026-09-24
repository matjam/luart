package lua

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"strings"
)

// value is a Lua value. Numbers and booleans keep their payload in n and a
// zero-size tag type in o, so they never allocate. Every other value lives
// in o, and n is zero. Values compare with == and work as map keys: numbers
// compare as floats, so 0 and -0 are the same key, and strings compare by
// content.
type value struct {
	o any
	n float64
}

type (
	numberTag struct{}
	boolTag   struct{}
	noneTag   struct{}
)

var (
	nilValue   = value{}
	none       = value{o: (*noneTag)(nil)}
	trueValue  = value{o: (*boolTag)(nil), n: 1}
	falseValue = value{o: (*boolTag)(nil)}
)

func numberValue(f float64) value { return value{o: (*numberTag)(nil), n: f} }
func stringValue(s string) value  { return value{o: s} }

func boolValue(b bool) value {
	if b {
		return trueValue
	}
	return falseValue
}

// objectValue wraps a table, closure, userdata, thread or light userdata.
func objectValue(x any) value { return value{o: x} }

// valueOf converts a Go value into a Lua value. float64 becomes a number,
// bool a boolean and string a string; anything else is stored as is.
func valueOf(x any) value {
	switch x := x.(type) {
	case float64:
		return numberValue(x)
	case bool:
		return boolValue(x)
	case value:
		return x
	}
	return value{o: x}
}

func (v value) isNil() bool    { return v.o == nil }
func (v value) isNumber() bool { _, ok := v.o.(*numberTag); return ok }

func (v value) number() (float64, bool) {
	_, ok := v.o.(*numberTag)
	return v.n, ok
}

func (v value) boolean() (b, ok bool) {
	_, ok = v.o.(*boolTag)
	return v.n != 0, ok
}

func (v value) str() (string, bool) {
	s, ok := v.o.(string)
	return s, ok
}

func (v value) table() *table {
	t, _ := v.o.(*table)
	return t
}

// toAny converts a Lua value into the Go value the public API exposes:
// float64, bool, string, nil, or the object itself.
func (v value) toAny() any {
	switch v.o.(type) {
	case *numberTag:
		return v.n
	case *boolTag:
		return v.n != 0
	case *noneTag:
		return nil
	}
	return v.o
}

type float8 int

func debugValue(v value) string {
	switch v := v.toAny().(type) {
	case *table:
		entry := func(x value) string {
			if t, ok := x.o.(*table); ok {
				return fmt.Sprintf("table %#v", t)
			}
			return debugValue(x)
		}
		var s strings.Builder
		s.WriteString(fmt.Sprintf("table %#v {[", v))
		for _, x := range v.array {
			s.WriteString(entry(x) + ", ")
		}
		s.WriteString("], {")
		for k, x := range v.strs {
			s.WriteString("'" + k + "': " + entry(x) + ", ")
		}
		for k, x := range v.hash {
			s.WriteString(entry(k) + ": " + entry(x) + ", ")
		}
		return s.String() + "}}"
	case string:
		return "'" + v + "'"
	case float64:
		return fmt.Sprintf("%f", v)
	case *luaClosure:
		return fmt.Sprintf("closure %s:%d %v", v.prototype.source, v.prototype.lineDefined, v)
	case *goClosure:
		return fmt.Sprintf("go closure %#v", v)
	case *goFunction:
		pc := reflect.ValueOf(v.Function).Pointer()
		f := runtime.FuncForPC(pc)
		file, line := f.FileLine(pc)
		return fmt.Sprintf("go function %s %s:%d", f.Name(), file, line)
	case *userData:
		return fmt.Sprintf("userdata %#v", v)
	case nil:
		return "nil"
	case bool:
		return fmt.Sprintf("%#v", v)
	}
	return fmt.Sprintf("unknown %#v %s", v, reflect.TypeOf(v).Name())
}

func stack(s []value) string {
	r := fmt.Sprintf("stack (len: %d, cap: %d):\n", len(s), cap(s))
	for i, v := range s {
		r = fmt.Sprintf("%s %d: %s\n", r, i, debugValue(v))
	}
	return r
}

func isFalse(s value) bool {
	switch s.o.(type) {
	case nil, *noneTag:
		return true
	case *boolTag:
		return s.n == 0
	}
	return false
}

type localVariable struct {
	name           string
	startPC, endPC pc
}

type userData struct {
	metaTable, env *table
	data           any
}

type upValueDesc struct {
	name    string
	isLocal bool
	index   int
}

type prototype struct {
	constants                    []value
	code                         []instruction
	prototypes                   []prototype
	lineInfo                     []int32
	localVariables               []localVariable
	upValues                     []upValueDesc
	cache                        *luaClosure
	source                       string
	lineDefined, lastLineDefined int
	parameterCount, maxStackSize int
	isVarArg                     bool
}

func (p *prototype) upValueName(index int) string {
	if s := p.upValues[index].name; s != "" {
		return s
	}
	return "?"
}

func (p *prototype) lastLoad(reg int, lastPC pc) (loadPC pc, found bool) {
	var ip, jumpTarget pc
	for ; ip < lastPC; ip++ {
		i, maybe := p.code[ip], false
		switch i.opCode() {
		case opLoadNil:
			maybe = i.a() <= reg && reg <= i.a()+i.b()
		case opTForCall:
			maybe = reg >= i.a()+2
		case opCall, opTailCall:
			maybe = reg >= i.a()
		case opJump:
			if dest := ip + 1 + pc(i.sbx()); ip < dest && dest <= lastPC && dest > jumpTarget {
				jumpTarget = dest
			}
		case opTest:
			maybe = reg == i.a()
		default:
			maybe = testAMode(i.opCode()) && reg == i.a()
		}
		if maybe {
			if ip < jumpTarget { // Can't know loading instruction because code is conditional.
				found = false
			} else {
				loadPC, found = ip, true
			}
		}
	}
	return
}

func (p *prototype) objectName(reg int, lastPC pc) (name, kind string) {
	if name, isLocal := p.localName(reg+1, lastPC); isLocal {
		return name, "local"
	}
	if pc, found := p.lastLoad(reg, lastPC); found {
		i := p.code[pc]
		switch op := i.opCode(); op {
		case opMove:
			if b := i.b(); b < i.a() {
				return p.objectName(b, pc)
			}
		case opGetTableUp:
			name, kind = p.constantName(i.c(), pc), "field"
			if p.upValueName(i.b()) == "_ENV" {
				kind = "global"
			}
			return
		case opGetTable:
			name, kind = p.constantName(i.c(), pc), "field"
			if v, ok := p.localName(i.b()+1, pc); ok && v == "_ENV" {
				kind = "global"
			}
			return
		case opGetUpValue:
			return p.upValueName(i.b()), "upvalue"
		case opLoadConstant:
			if s, ok := p.constants[i.bx()].str(); ok {
				return s, "constant"
			}
		case opLoadConstantEx:
			if s, ok := p.constants[p.code[pc+1].ax()].str(); ok {
				return s, "constant"
			}
		case opSelf:
			return p.constantName(i.c(), pc), "method"
		}
	}
	return
}

func (p *prototype) constantName(k int, pc pc) string {
	if isConstant(k) {
		if s, ok := p.constants[constantIndex(k)].str(); ok {
			return s
		}
	} else if name, kind := p.objectName(k, pc); kind == "c" {
		return name
	}
	return "?"
}

func (p *prototype) localName(index int, pc pc) (string, bool) {
	for i := 0; i < len(p.localVariables) && p.localVariables[i].startPC <= pc; i++ {
		if pc < p.localVariables[i].endPC {
			if index--; index == 0 {
				return p.localVariables[i].name, true
			}
		}
	}
	return "", false
}

// Converts an integer to a "floating point byte", represented as
// (eeeeexxx), where the real value is (1xxx) * 2^(eeeee - 1) if
// eeeee != 0 and (xxx) otherwise.
func float8FromInt(x int) float8 {
	if x < 8 {
		return float8(x)
	}
	e := 0
	for ; x >= 0x10; e++ {
		x = (x + 1) >> 1
	}
	return float8(((e + 1) << 3) | (x - 8))
}

func intFromFloat8(x float8) int {
	e := x >> 3 & 0x1f
	if e == 0 {
		return int(x)
	}
	return int(x&7+8) << uint(e-1)
}

const minPow10, maxPow10 = -323, 308

// pow10 holds correctly rounded powers of ten from 1e-323 to 1e308.
var pow10 = func() (t [maxPow10 - minPow10 + 1]float64) {
	for i := range t {
		t[i], _ = strconv.ParseFloat("1e"+strconv.Itoa(i+minPow10), 64)
	}
	return
}()

func arith(op Operator, v1, v2 float64) float64 {
	switch op {
	case OpAdd:
		return v1 + v2
	case OpSub:
		return v1 - v2
	case OpMul:
		return v1 * v2
	case OpDiv:
		return v1 / v2
	case OpMod:
		return v1 - math.Floor(v1/v2)*v2
	case OpPow:
		// math.Pow and math.Pow10 can be 1 ulp off for powers of ten, which
		// luac folds exactly.
		if v1 == 10.0 && minPow10 <= v2 && v2 <= maxPow10 && math.Trunc(v2) == v2 {
			return pow10[int(v2)-minPow10]
		}
		return math.Pow(v1, v2)
	case OpUnaryMinus:
		return -v1
	}
	panic(fmt.Sprintf("not an arithmetic op code (%d)", op))
}

func (l *State) parseNumber(s string) (v float64, ok bool) { // TODO this is f*cking ugly - scanner.readNumber should be refactored.
	if len(strings.Fields(s)) != 1 || strings.ContainsRune(s, 0) {
		return
	}
	scanner := scanner{l: l, r: strings.NewReader(s)}
	t := scanner.scan()
	if t.t == '-' {
		if t := scanner.scan(); t.t == tkNumber {
			v, ok = -t.n, true
		}
	} else if t.t == tkNumber {
		v, ok = t.n, true
	} else if t.t == '+' {
		if t := scanner.scan(); t.t == tkNumber {
			v, ok = t.n, true
		}
	}
	if ok && scanner.scan().t != tkEOS {
		ok = false
	} else if math.IsInf(v, 0) || math.IsNaN(v) {
		ok = false
	}
	return
}

func (l *State) toNumber(r value) (v float64, ok bool) {
	if v, ok = r.number(); ok {
		return
	}
	var s string
	if s, ok = r.str(); ok {
		if err := l.protectedCall(func() { v, ok = l.parseNumber(strings.TrimSpace(s)) }, l.top, l.errorFunction); err != nil {
			l.pop() // Remove error message from the stack.
			ok = false
		}
	}
	return
}

func (l *State) toString(index int) (s string, ok bool) {
	if s, ok = toString(l.stack[index]); ok {
		l.stack[index] = stringValue(s)
	}
	return
}

func numberToString(f float64) string {
	return fmt.Sprintf("%.14g", f)
}

func toString(r value) (string, bool) {
	switch o := r.o.(type) {
	case string:
		return o, true
	case *numberTag:
		return numberToString(r.n), true
	}
	return "", false
}

func pairAsNumbers(p1, p2 value) (f1, f2 float64, ok bool) {
	if f1, ok = p1.number(); !ok {
		return
	}
	f2, ok = p2.number()
	return
}

func pairAsStrings(p1, p2 value) (s1, s2 string, ok bool) {
	if s1, ok = p1.str(); !ok {
		return
	}
	s2, ok = p2.str()
	return
}
