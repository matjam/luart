package luart

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUndump(t *testing.T) {
	_, err := exec.LookPath("luac")
	if err != nil {
		t.Skipf("testing undump requires luac: %s", err)
	}
	source := filepath.Join("lua-tests", "checktable.lua")
	binary := filepath.Join("lua-tests", "checktable.bin")
	if err := exec.Command("luac", "-o", binary, source).Run(); err != nil {
		t.Fatalf("luac failed to compile %s: %s", source, err)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal("couldn't open checktable.bin")
	}
	l := NewState()
	if err := l.Load(file, "test", "b"); err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal("unexpected error", err, msg)
	}
	p := l.stack[l.top-1].luaClosure().prototype
	validate("@lua-tests/checktable.lua", p.Source, "as source file name", t)
	validate(23, len(p.Code), "instructions", t)
	validate(8, len(p.Constants), "constants", t)
	validate(4, len(p.Prototypes), "prototypes", t)
	validate(1, len(p.UpValues), "upvalues", t)
	validate(0, len(p.LocalVariables), "local variables", t)
	validate(0, p.ParameterCount, "parameters", t)
	validate(4, p.MaxStackSize, "stack slots", t)
	if !p.IsVarArg {
		t.Error("expected main function to be var arg, but wasn't")
	}
}

func validate(expected, actual any, description string, t *testing.T) {
	if expected != actual {
		t.Errorf("expected %v %s in main function but found %v", expected, description, actual)
	}
}
