package compiler

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/matjam/luart/internal/bytecode"
)

func TestParseNumber(t *testing.T) {
	for _, tt := range []struct {
		s  string
		v  float64
		ok bool
	}{
		{"10", 10, true},
		{"  -2.5e3  ", -2500, true},
		{"+0x1p4", 16, true},
		{"0x10", 16, true},
		{"", 0, false},
		{"0x", 0, false}, // a malformed numeral: a syntax error inside
		{"1e", 0, false},
		{"1 2", 0, false},
		{"abc", 0, false},
		{"1e999", 0, false}, // infinite
	} {
		v, ok := ParseNumber(tt.s)
		if ok != tt.ok || ok && v != tt.v {
			t.Errorf("ParseNumber(%q) = %v, %v; want %v, %v", tt.s, v, ok, tt.v, tt.ok)
		}
	}
}

func TestChunkID(t *testing.T) {
	for _, tt := range []struct{ source, want string }{
		{"=stdin", "stdin"},
		{"@file.lua", "file.lua"},
		{"return 1\nreturn 2", `[string "return 1"]`},
		{"", `[string ""]`},
		{"@" + strings.Repeat("x", 100), "..." + strings.Repeat("x", IDSize-4)},
	} {
		if got := ChunkID(tt.source); got != tt.want {
			t.Errorf("ChunkID(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	p, err := Parse(strings.NewReader("local x = 1 + 2 return x, 'a', true, nil"), "=test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Code) == 0 || !p.IsVarArg || p.Source != "=test" {
		t.Errorf("main function: %d instructions, vararg %v, source %q", len(p.Code), p.IsVarArg, p.Source)
	}
	for _, k := range p.Constants {
		switch k.(type) {
		case nil, bool, float64, string:
		default:
			t.Errorf("constant %v of type %T", k, k)
		}
	}
	if !containsConstant(p.Constants, 3.0) { // 1 + 2, folded
		t.Errorf("constants %v do not hold the folded 3", p.Constants)
	}

	_, err = Parse(strings.NewReader("x = = 1"), "=test", 0)
	if err == nil || err.Error() != "test:1: unexpected symbol near =" {
		t.Errorf("syntax error: %v", err)
	}
	_, err = Parse(strings.NewReader("return 1"), "=test", 1000)
	if err == nil || !strings.Contains(err.Error(), "Go levels") {
		t.Errorf("nesting beyond MaxCallCount: %v", err)
	}
}

// A constant whose index does not fit an instruction's Bx loads with
// LOADKX and the index in the EXTRAARG word after it.
func TestLoadConstantEx(t *testing.T) {
	var b strings.Builder
	b.WriteString("local t = {0")
	for i := 1; i <= bytecode.MaxArgBx+2; i++ {
		fmt.Fprintf(&b, ";%d", i)
	}
	b.WriteString("}")
	p, err := Parse(strings.NewReader(b.String()), "=test", 0)
	if err != nil {
		t.Fatal(err)
	}
	extra := 0
	for pc, i := range p.Code {
		if i.OpCode() != bytecode.OpExtraArg || p.Code[pc-1].OpCode() == bytecode.OpSetList {
			continue
		}
		extra++
		if op := p.Code[pc-1].OpCode(); op != bytecode.OpLoadConstantEx {
			t.Fatalf("EXTRAARG %d at %d follows %v, want LOADKX", i.Ax(), pc, op)
		}
	}
	if extra == 0 {
		t.Fatal("no constant loaded with EXTRAARG")
	}
}

func containsConstant(ks []any, v float64) bool {
	for _, k := range ks {
		if f, ok := k.(float64); ok && (f == v || math.IsNaN(f) && math.IsNaN(v)) {
			return true
		}
	}
	return false
}
