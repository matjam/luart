package lua

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func load(l *State, t *testing.T, fileName string) *luaClosure {
	if err := l.LoadFile(fileName, "bt"); err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal(err, msg)
	}
	return l.ToValue(-1).(*luaClosure)
}

// roundTrip dumps the function on the top of the stack and loads it back,
// leaving the stack as it was.
func roundTrip(l *State, t *testing.T) *luaClosure {
	t.Helper()
	var b bytes.Buffer
	if err := l.Dump(&b); err != nil {
		t.Fatal(err)
	}
	if err := l.Load(&b, "=dumped", "b"); err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal(err, msg)
	}
	c := l.ToValue(-1).(*luaClosure)
	l.Pop(1)
	return c
}

func TestParser(t *testing.T) {
	l := NewState()
	openLibraries(l)
	closure := load(l, t, "fixtures/fib.lua")
	bin := roundTrip(l, t)
	p := closure.prototype
	if p == nil {
		t.Fatal("prototype was nil")
	}
	validate("@fixtures/fib.lua", p.Source, "as source file name", t)
	if !p.IsVarArg {
		t.Error("expected main function to be var arg, but wasn't")
	}
	if len(closure.upValues) != len(closure.prototype.UpValues) {
		t.Error("upvalue count doesn't match", len(closure.upValues), "!=", len(closure.prototype.UpValues))
	}
	compareClosures(t, bin, closure)
	l.Call(0, 0)
}

func TestEmptyString(t *testing.T) {
	l := NewState()
	if err := l.LoadString(""); err != nil {
		t.Fatal(err.Error())
	}
	l.Call(0, 0)
}

// Every file of the Lua 5.5 test suite that luart compiles survives a dump
// and load unchanged.
func TestDumpRoundTripsTheSuite(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("../lua-5.5-tests", "*.lua"))
	if err != nil || len(matches) == 0 {
		t.Fatal("no suite files", err)
	}
	l := NewState()
	for _, source := range matches {
		if err := l.LoadFile(source, "t"); err != nil {
			l.Pop(1) // not compilable yet: the suite's own test says why
			continue
		}
		src := l.ToValue(-1).(*luaClosure)
		bin := roundTrip(l, t)
		l.Pop(1)
		compareClosures(t, src, bin)
	}
}

func expectEqual(t *testing.T, x, y any, m string) {
	if x != y {
		t.Errorf("%s doesn't match: %v, %v\n", m, x, y)
	}
}

func expectDeepEqual(t *testing.T, x, y any, m string) bool {
	if reflect.DeepEqual(x, y) {
		return true
	}
	if reflect.TypeOf(x).Kind() == reflect.Slice && reflect.ValueOf(y).Len() == 0 && reflect.ValueOf(x).Len() == 0 {
		return true
	}
	t.Errorf("%s doesn't match: %v, %v\n", m, x, y)
	return false
}

func compareClosures(t *testing.T, a, b *luaClosure) {
	expectEqual(t, a.upValueCount(), b.upValueCount(), "upvalue count")
	comparePrototypes(t, a.prototype, b.prototype)
}

func comparePrototypes(t *testing.T, a, b *prototype) {
	expectEqual(t, a.IsVarArg, b.IsVarArg, "var arg")
	expectEqual(t, a.LineDefined, b.LineDefined, "line defined")
	expectEqual(t, a.LastLineDefined, b.LastLineDefined, "last line defined")
	expectEqual(t, a.ParameterCount, b.ParameterCount, "parameter count")
	expectEqual(t, a.MaxStackSize, b.MaxStackSize, "max stack size")
	expectEqual(t, a.Source, b.Source, "source")
	expectEqual(t, len(a.Code), len(b.Code), "code length")
	if !expectDeepEqual(t, a.Code, b.Code, "code") {
		for i := range a.Code {
			if a.Code[i] != b.Code[i] {
				t.Errorf("%d: %v != %v\n", a.LineInfo[i], a.Code[i], b.Code[i])
			}
		}
		for _, i := range []int{3, 197, 198, 199, 200, 201} {
			t.Errorf("%d: %#v, %#v\n", i, a.Constants[i], b.Constants[i])
		}
		for _, i := range []int{202, 203, 204} {
			t.Errorf("%d: %#v\n", i, b.Constants[i])
		}
	}
	if len(a.Constants) != len(b.Constants) {
		t.Errorf("constants doesn't match: %d constants, %d constants\n", len(a.Constants), len(b.Constants))
	} else {
		for i := range a.Constants {
			if !rawEqual(a.Constants[i], b.Constants[i]) {
				t.Errorf("%d: %s != %s\n", i, debugValue(a.Constants[i]), debugValue(b.Constants[i]))
			}
		}
	}
	expectDeepEqual(t, a.LineInfo, b.LineInfo, "line info")
	expectDeepEqual(t, a.UpValues, b.UpValues, "upvalues")
	expectDeepEqual(t, a.LocalVariables, b.LocalVariables, "local variables")
	expectEqual(t, len(a.Prototypes), len(b.Prototypes), "prototypes length")
	for i := range a.Prototypes {
		comparePrototypes(t, &a.Prototypes[i], &b.Prototypes[i])
	}
}

func validate(expected, actual any, description string, t *testing.T) {
	if expected != actual {
		t.Errorf("expected %v %s in main function but found %v", expected, description, actual)
	}
}
