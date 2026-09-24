package lua

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// value is a Lua value in two words. p is nil for nil, a sentinel address
// for numbers, booleans, none and the empty string, or else a pointer: to a
// string's bytes or to an object. n holds a number, or for other values the
// bits returned by x: the value's kind in the top byte, with a boolean or a
// string's length below it. n is a float64 so arithmetic loads and stores it
// in floating-point registers without moves.
//
// The zero value is nil, so cleared registers need no initialisation.
// Values must not be compared with ==, which compares a number's bits and a
// string's address: use rawEqual. Values used as keys of table.hash are
// normalised by hashKey.
type value struct {
	_ [0]func() // not comparable; see rawEqual. First, so it adds no padding.
	p unsafe.Pointer
	n float64
}

// x returns v's second word as bits.
func (v value) x() uint64 { return math.Float64bits(v.n) }

func tagged(p unsafe.Pointer, bits uint64) value {
	return value{p: p, n: math.Float64frombits(bits)}
}

// valueKind is what a value holds. Its constants start with vk, because
// the parser's expression kinds already start with kind.
type valueKind uint8

const (
	vkNil valueKind = iota
	vkNone
	vkBool
	vkNumber
	vkString
	vkTable
	vkLuaClosure
	vkGoClosure
	vkGoFunction
	vkUserData
	vkThread
	vkLightUserData // p points to a boxed Go value; see lightUserData
)

const kindShift = 56

func tagOf(k valueKind) uint64 { return uint64(k) << kindShift }

// Sentinels give scalar values a non-nil p. They are bytes so that each
// has its own address.
var numberSentinel, boolSentinel, noneSentinel, emptyStringSentinel byte

// The sentinel addresses are functions, not variables, so each check
// compiles to a compare with an address constant instead of a load.
func numberPtr() unsafe.Pointer      { return unsafe.Pointer(&numberSentinel) }
func boolPtr() unsafe.Pointer        { return unsafe.Pointer(&boolSentinel) }
func nonePtr() unsafe.Pointer        { return unsafe.Pointer(&noneSentinel) }
func emptyStringPtr() unsafe.Pointer { return unsafe.Pointer(&emptyStringSentinel) }

var (
	nilValue   = value{}
	none       = tagged(nonePtr(), tagOf(vkNone))
	trueValue  = tagged(boolPtr(), tagOf(vkBool)|1)
	falseValue = tagged(boolPtr(), tagOf(vkBool))
)

func numberValue(f float64) value { return value{p: numberPtr(), n: f} }

func stringValue(s string) value {
	if len(s) == 0 {
		return tagged(emptyStringPtr(), tagOf(vkString))
	}
	return tagged(unsafe.Pointer(unsafe.StringData(s)), tagOf(vkString)|uint64(len(s)))
}

func boolValue(b bool) value {
	if b {
		return trueValue
	}
	return falseValue
}

func tableValue(t *table) value           { return tagged(unsafe.Pointer(t), tagOf(vkTable)) }
func luaClosureValue(c *luaClosure) value { return tagged(unsafe.Pointer(c), tagOf(vkLuaClosure)) }
func goClosureValue(c *goClosure) value   { return tagged(unsafe.Pointer(c), tagOf(vkGoClosure)) }
func goFunctionValue(f *goFunction) value { return tagged(unsafe.Pointer(f), tagOf(vkGoFunction)) }
func userDataValue(d *userData) value     { return tagged(unsafe.Pointer(d), tagOf(vkUserData)) }
func threadValue(l *State) value          { return tagged(unsafe.Pointer(l), tagOf(vkThread)) }
func lightUserDataValue(box *lightUserData) value {
	return tagged(unsafe.Pointer(box), tagOf(vkLightUserData))
}

// lightUserData boxes a Go value pushed as light userdata. A state interns
// boxes for comparable values, so equal Go values share a box and compare
// equal, as Go's == would.
type lightUserData struct{ v any }

// objectValue wraps a table, closure, userdata or thread.
func objectValue(x any) value {
	switch x := x.(type) {
	case *table:
		return tableValue(x)
	case *luaClosure:
		return luaClosureValue(x)
	case *goClosure:
		return goClosureValue(x)
	case *goFunction:
		return goFunctionValue(x)
	case *userData:
		return userDataValue(x)
	case *State:
		return threadValue(x)
	case closure:
		panic(fmt.Sprintf("unknown closure type %T", x))
	}
	panic(fmt.Sprintf("objectValue(%T)", x))
}

// valueOf converts a Go value into a Lua value. nil, float64, bool and
// string become the matching Lua values, luart's own objects are stored as
// themselves, and any other value becomes light userdata.
func (l *State) valueOf(x any) value {
	switch x := x.(type) {
	case nil:
		return nilValue
	case float64:
		return numberValue(x)
	case bool:
		return boolValue(x)
	case string:
		return stringValue(x)
	case *table, *luaClosure, *goClosure, *goFunction, *userData, *State:
		return objectValue(x)
	}
	return lightUserDataValue(l.global.lightUserData(x))
}

func (g *globalState) lightUserData(x any) *lightUserData {
	if !reflect.ValueOf(x).Comparable() {
		return &lightUserData{x}
	}
	if g.lightBoxes == nil {
		g.lightBoxes = make(map[any]*lightUserData)
	}
	box, ok := g.lightBoxes[x]
	if !ok {
		box = &lightUserData{x}
		g.lightBoxes[x] = box
	}
	return box
}

func (v value) isNil() bool    { return v.p == nil }
func (v value) isNumber() bool { return v.p == numberPtr() }

// f returns the number in v, which must be a number.
func (v value) f() float64 { return v.n }

func (v value) number() (float64, bool) {
	if v.p == numberPtr() {
		return v.n, true
	}
	return 0, false
}

func (v value) kind() valueKind {
	switch v.p {
	case numberPtr():
		return vkNumber
	case nil:
		return vkNil
	}
	return valueKind(v.x() >> kindShift)
}

// is reports whether v is an object of kind k, which must not be a scalar
// kind. nil fails the tag test, since its bits are zero.
func (v value) is(k valueKind) bool { return v.x() == tagOf(k) && v.p != numberPtr() }

func (v value) boolean() (b, ok bool) {
	if v.p == boolPtr() {
		return v.x()&1 != 0, true
	}
	return false, false
}

func (v value) isString() bool {
	return v.p != numberPtr() && v.p != nil && v.x()>>kindShift == uint64(vkString)
}

func (v value) str() (string, bool) {
	if !v.isString() {
		return "", false
	}
	if v.p == emptyStringPtr() {
		return "", true
	}
	return unsafe.String((*byte)(v.p), int(v.x()&(1<<kindShift-1))), true
}

func (v value) table() *table {
	if v.is(vkTable) {
		return (*table)(v.p)
	}
	return nil
}

func (v value) luaClosure() *luaClosure {
	if v.is(vkLuaClosure) {
		return (*luaClosure)(v.p)
	}
	return nil
}

func (v value) goClosure() *goClosure {
	if v.is(vkGoClosure) {
		return (*goClosure)(v.p)
	}
	return nil
}

func (v value) goFunction() *goFunction {
	if v.is(vkGoFunction) {
		return (*goFunction)(v.p)
	}
	return nil
}

func (v value) userData() *userData {
	if v.is(vkUserData) {
		return (*userData)(v.p)
	}
	return nil
}

func (v value) thread() *State {
	if v.is(vkThread) {
		return (*State)(v.p)
	}
	return nil
}

// closure returns v as a closure, or nil when v is not a Lua or Go closure.
func (v value) closure() closure {
	switch v.kind() {
	case vkLuaClosure:
		return (*luaClosure)(v.p)
	case vkGoClosure:
		return (*goClosure)(v.p)
	}
	return nil
}

// isFunction reports whether v can be called without a __call metamethod.
func (v value) isFunction() bool {
	switch v.kind() {
	case vkLuaClosure, vkGoClosure, vkGoFunction:
		return true
	}
	return false
}

// obj returns v as a Go value for switching on its type: nil, float64,
// bool, string, a luart object, or *lightUserData. Strings allocate, so hot
// paths use the typed accessors.
func (v value) obj() any {
	switch v.kind() {
	case vkNil, vkNone:
		return nil
	case vkNumber:
		return v.f()
	case vkBool:
		return v.x()&1 != 0
	case vkString:
		s, _ := v.str()
		return s
	case vkTable:
		return (*table)(v.p)
	case vkLuaClosure:
		return (*luaClosure)(v.p)
	case vkGoClosure:
		return (*goClosure)(v.p)
	case vkGoFunction:
		return (*goFunction)(v.p)
	case vkUserData:
		return (*userData)(v.p)
	case vkThread:
		return (*State)(v.p)
	case vkLightUserData:
		return (*lightUserData)(v.p)
	}
	return nil
}

// toAny converts a Lua value into the Go value the public API exposes:
// float64, bool, string, nil, the object itself, or a light userdata's
// Go value.
func (v value) toAny() any {
	if b, ok := v.obj().(*lightUserData); ok {
		return b.v
	}
	return v.obj()
}

// identical reports whether v and w are the same bits: the same object, or
// the same scalar encoding.
func (v value) identical(w value) bool { return v.p == w.p && v.x() == w.x() }

// rawEqual reports whether a and b are equal without metamethods: numbers
// by value, strings by content, everything else by identity.
func rawEqual(a, b value) bool {
	if a.p == numberPtr() {
		return b.p == numberPtr() && a.n == b.n
	}
	if a.isString() {
		if !b.isString() || a.x() != b.x() {
			return false
		}
		as, _ := a.str()
		bs, _ := b.str()
		return as == bs
	}
	return a.identical(b)
}

// hashKey is the table.hash key for k, which is neither nil, NaN nor a
// string: -0 becomes 0, so the two are one key.
func hashKey(k value) hashValue {
	if k.p == numberPtr() && k.n == 0 {
		return hashValue{p: numberPtr()}
	}
	return hashValue{p: k.p, x: k.x()}
}

// hashValue is a comparable copy of a value, for table.hash keys.
type hashValue struct {
	p unsafe.Pointer
	x uint64
}

func (h hashValue) value() value { return tagged(h.p, h.x) }

type float8 int

func debugValue(v value) string {
	switch v := v.toAny().(type) {
	case *table:
		entry := func(x value) string {
			if t := x.table(); t != nil {
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
		for i, x := range v.slots {
			s.WriteString(entry(v.shape.keys[i]) + ": " + entry(x) + ", ")
		}
		for k, x := range v.hash {
			s.WriteString(entry(k.value()) + ": " + entry(x) + ", ")
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
	switch s.p {
	case nil, nonePtr():
		return true
	case boolPtr():
		return s.x()&1 == 0
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
	exec                         []instruction // see execCode
	fields                       []fieldCache  // by pc, for exec's field instructions
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

// numberToString formats f as Lua's "%.14g" does. Integers below 1e14 take a
// fast path that skips fmt.
func numberToString(f float64) string {
	if i := int64(f); float64(i) == f && -1e14 < f && f < 1e14 && !(f == 0 && math.Signbit(f)) {
		return strconv.FormatInt(i, 10)
	}
	return strconv.FormatFloat(f, 'g', 14, 64)
}

func toString(r value) (string, bool) {
	if s, ok := r.str(); ok {
		return s, true
	}
	if f, ok := r.number(); ok {
		return numberToString(f), true
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
