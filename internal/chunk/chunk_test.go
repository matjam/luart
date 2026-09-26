package chunk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/matjam/luart/internal/compiler"
)

const source = `
local t, s = {1, 2.5, true, nil}, "string"
local function f(a, b, ...)
	local c = a + b
	return function(...) return c, s, ... end
end
return f(1, 2)
`

func compile(t *testing.T) []byte {
	t.Helper()
	p, err := compiler.Parse(strings.NewReader(source), "@test.lua", 0)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := Dump(&b, p, false); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDumpThenLoad(t *testing.T) {
	want, err := compiler.Parse(strings.NewReader(source), "@test.lua", 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(bytes.NewReader(compile(t)), "=test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %+v\nwant %+v", got, want)
	}
}

// An empty string is written as C Lua writes it, size 1 and its NUL: size
// 0 means no string at all, which C reads back as NULL. A function without
// a source, as luac -s strips it, loads with the source "=?".
func TestEmptyStrings(t *testing.T) {
	sized := func(n uint64) []byte { // a string's size field
		var b bytes.Buffer
		if header.PointerSize == 8 {
			binary.Write(&b, endianness(), n)
		} else {
			binary.Write(&b, endianness(), uint32(n))
		}
		return b.Bytes()
	}
	dump := func(source string) []byte {
		p, err := compiler.Parse(strings.NewReader("return true"), source, 0)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := Dump(&b, p, false); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}

	// The source "" is the chunk's only empty string.
	b := dump("")
	empty := append(sized(1), 0)
	if bytes.Count(b, empty) != 1 {
		t.Fatalf("want one string of size 1 in % x", b)
	}
	if got, err := Load(bytes.NewReader(b), "=test"); err != nil {
		t.Error(err)
	} else if got.Source != "" {
		t.Errorf("source %q, want \"\"", got.Source)
	}

	// Stripped, with size 0 for the source, it loads as "=?".
	stripped := bytes.Replace(b, empty, sized(0), 1)
	if got, err := Load(bytes.NewReader(stripped), "=test"); err != nil {
		t.Error(err)
	} else if got.Source != "=?" {
		t.Errorf("stripped source %q, want =?", got.Source)
	}
}

// Every proper prefix of a chunk is truncated.
func TestLoadTruncated(t *testing.T) {
	b := compile(t)
	for n := range len(b) {
		_, err := Load(bytes.NewReader(b[:n]), "=test")
		if !errors.Is(err, errTruncated) {
			t.Fatalf("%d of %d bytes: %v", n, len(b), err)
		}
	}
}

// A corrupted count fails on the missing data, not on allocating for it.
func TestLoadCorruptedCount(t *testing.T) {
	for _, count := range []int32{-1, 1 << 30} {
		var b bytes.Buffer
		binary.Write(&b, endianness(), header)
		binary.Write(&b, endianness(), []int32{0, 0})
		b.Write([]byte{0, 1, 2})
		binary.Write(&b, endianness(), count) // instructions
		_, err := Load(&b, "=test")
		if !errors.Is(err, errCorrupted) && !errors.Is(err, errTruncated) {
			t.Errorf("count %d: %v", count, err)
		}
	}
}

func TestLoadNames(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"=stdin", "stdin: truncated precompiled chunk"},
		{"@x.luac", "x.luac: truncated precompiled chunk"},
		{"\x1bLua", "binary string: truncated precompiled chunk"},
		{"chunk", "chunk: truncated precompiled chunk"},
		{"", "?: truncated precompiled chunk"},
	} {
		_, err := Load(strings.NewReader(Signature), tt.name)
		if err == nil || err.Error() != tt.want {
			t.Errorf("Load named %q: %v, want %s", tt.name, err, tt.want)
		}
	}
}

func TestLoadHeader(t *testing.T) {
	// The header's bytes: the signature, version, format, endianness, int,
	// pointer, instruction and number sizes, integral flag and tail.
	for _, tt := range []struct {
		name   string
		change func(h []byte)
		want   error
	}{
		{"no function", func([]byte) {}, errTruncated},
		{"wrong signature", func(h []byte) { h[1] = 'X' }, errNotPrecompiledChunk},
		{"wrong version", func(h []byte) { h[4]++ }, errVersionMismatch},
		{"wrong endianness", func(h []byte) { h[6] ^= 1 }, errIncompatible},
		{"wrong number size", func(h []byte) { h[10] /= 2 }, errIncompatible},
		{"corrupt tail", func(h []byte) { h[15]++ }, errCorrupted},
	} {
		var b bytes.Buffer
		binary.Write(&b, endianness(), header)
		h := b.Bytes()
		tt.change(h)
		if _, err := Load(bytes.NewReader(h), "=test"); !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.want)
		}
	}
}
