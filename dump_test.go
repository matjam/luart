package luart

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUndumpThenDumpReturnsTheSameFunction(t *testing.T) {
	_, err := exec.LookPath("luac")
	if err != nil {
		t.Skipf("testing dump requires luac: %s", err)
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

	var out bytes.Buffer
	err = l.Dump(&out)
	if err != nil {
		t.Error("unexpected error", err, "with testing dump")
	}

	expectedBinary, err := os.ReadFile(binary)
	if err != nil {
		t.Error("error reading file", err)
	}
	actualBinary, err := io.ReadAll(&out)
	if err != nil {
		t.Error("error reading out bugger", err)
	}
	if !bytes.Equal(expectedBinary, actualBinary) {
		t.Errorf("binary chunks are not the same: %v %v", expectedBinary, actualBinary)
	}
}

func TestDumpThenUndumpReturnsTheSameFunction(t *testing.T) {
	_, err := exec.LookPath("luac")
	if err != nil {
		t.Skipf("testing dump requires luac: %s", err)
	}
	source := filepath.Join("lua-tests", "checktable.lua")
	l := NewState()
	err = l.LoadFile(source, "")
	if err != nil {
		t.Error("unexpected error", err, "with loading file", source)
	}

	var out bytes.Buffer
	f := l.stack[l.top-1].luaClosure()
	err = l.Dump(&out)
	if err != nil {
		t.Error("unexpected error", err, "with testing dump")
	}

	if err := l.Load(&out, "test", "b"); err != nil {
		t.Fatal("unexpected error", err)
	}
	undumpedPrototype := l.stack[l.top-1].luaClosure().prototype

	// comparePrototypes compares constants with rawEqual; reflect.DeepEqual
	// would compare string constants by address.
	comparePrototypes(t, f.prototype, undumpedPrototype)
}
