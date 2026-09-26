package lua

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Loading a dumped chunk and dumping it again gives the same bytes.
func TestUndumpThenDumpReturnsTheSameFunction(t *testing.T) {
	source := filepath.Join("fixtures", "fib.lua")
	l := NewState()
	if err := l.LoadFile(source, ""); err != nil {
		t.Fatal(err)
	}
	var first bytes.Buffer
	if err := l.Dump(&first, false); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "sort.bin")
	if err := os.WriteFile(binary, first.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	l = NewState()
	if err := l.Load(file, "test", "b"); err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal("unexpected error", err, msg)
	}

	var out bytes.Buffer
	err = l.Dump(&out, false)
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
	source := filepath.Join("fixtures", "fib.lua")
	l := NewState()
	err := l.LoadFile(source, "")
	if err != nil {
		t.Error("unexpected error", err, "with loading file", source)
	}

	var out bytes.Buffer
	f := l.stack[l.top-1].luaClosure()
	err = l.Dump(&out, false)
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
