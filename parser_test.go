package luart

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

func load(l *State, t *testing.T, fileName string) *luaClosure {
	if err := l.LoadFile(fileName, "bt"); err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal(err, msg)
	}
	return l.ToValue(-1).(*luaClosure)
}

func TestParser(t *testing.T) {
	l := NewState()
	openLibraries(l)
	bin := load(l, t, "fixtures/fib.bin")
	l.Pop(1)
	closure := load(l, t, "fixtures/fib.lua")
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

func TestParserExhaustively(t *testing.T) {
	_, err := exec.LookPath("luac")
	if err != nil {
		t.Skipf("exhaustively testing the parser requires luac: %s", err)
	}
	l := NewState()
	matches, err := filepath.Glob(filepath.Join("lua-tests", "*.lua"))
	if err != nil {
		t.Fatal(err)
	}
	blackList := map[string]bool{"math.lua": true}
	for _, source := range matches {
		if _, ok := blackList[filepath.Base(source)]; ok {
			continue
		}
		protectedTestParser(l, t, source)
	}
}

func protectedTestParser(l *State, t *testing.T, source string) {
	defer func() {
		if x := recover(); x != nil {
			t.Error(x)
			t.Log(string(debug.Stack()))
		}
	}()
	t.Log("Compiling " + source)
	binary := strings.TrimSuffix(source, ".lua") + ".bin"
	if err := exec.Command("luac", "-o", binary, source).Run(); err != nil {
		t.Fatalf("luac failed to compile %s: %s", source, err)
	}
	t.Log("Parsing " + source)
	bin := load(l, t, binary)
	l.Pop(1)
	src := load(l, t, source)
	l.Pop(1)
	t.Log(source)
	compareClosures(t, src, bin)
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
