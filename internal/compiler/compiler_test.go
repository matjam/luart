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
	// As lobject.c's luaO_chunkid, with LUA_IDSIZE 60.
	for _, tt := range []struct{ source, want string }{
		{"=stdin", "stdin"},
		{"=" + strings.Repeat("x", 100), strings.Repeat("x", 59)},
		{"@file.lua", "file.lua"},
		{"@a" + strings.Repeat("x", 99), "..." + strings.Repeat("x", 56)}, // the end of a long name
		{"", `[string ""]`},
		{"?", `[string "?"]`},
		{"return 1", `[string "return 1"]`},
		{"return 1\nreturn 2", `[string "return 1..."]`},
		{"\nreturn 1", `[string "..."]`},
		{strings.Repeat("y", 44), `[string "` + strings.Repeat("y", 44) + `"]`},
		{strings.Repeat("y", 45), `[string "` + strings.Repeat("y", 45) + `..."]`},
		{strings.Repeat("y", 100), `[string "` + strings.Repeat("y", 45) + `..."]`},
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
	if err == nil || err.Error() != "test:1: unexpected symbol near '='" {
		t.Errorf("syntax error: %v", err)
	}
	_, err = Parse(strings.NewReader("return 1"), "=test", 1000)
	if err == nil || !strings.Contains(err.Error(), "Go levels") {
		t.Errorf("nesting beyond MaxCallCount: %v", err)
	}
}

// Syntax errors name the token as llex.c's txtToken does: symbols and
// reserved words quoted, names, strings and numerals as their source text
// quoted, and the end of the chunk as <eof>.
func TestSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ source, want string }{
		{"x = = 1", "unexpected symbol near '='"},
		{"x = 1 end", "<eof> expected near 'end'"},
		{"local = 1", "<name> expected near '='"},
		{"x = 3 3", "unexpected symbol near '3'"},
		{"x = 0x10 y", "syntax error near <eof>"},
		{"x = 'abc' 'd'", "unexpected symbol near ''d''"},
		{"x =", "unexpected symbol near <eof>"},
		{"x = \x01", "unexpected symbol near char(1)"},
		{`x = "abc\x"`, `hexadecimal digit expected near '\x"'`},
		{`x = "abc\q"`, `invalid escape sequence near '\q'`},
		{`x = "abc\300"`, `decimal escape too large near '\300'`},
		{"x = \"abc\ny\"", `unfinished string near '"abc'`},
		{"x = 3f", "malformed number near '3f'"},
		{"x = 0x", "malformed number near '0x'"},
		{"x = 1e", "malformed number near '1e'"},
		{"x = 1..2", "malformed number near '1..2'"},
		// Names are ASCII letters, digits and '_', as in the C locale.
		{"\xe9 = 1", "unexpected symbol near char(233)"},
		{"a\xe91 = 1", "syntax error near char(233)"},
	} {
		_, err := Parse(strings.NewReader(tt.source), "=test", 0)
		if want := "test:1: " + tt.want; err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %s", tt.source, err, want)
		}
	}
}

// Numerals convert as Lua's do, hexadecimal fractions and exponents
// included; one too large to represent is infinite.
func TestNumerals(t *testing.T) {
	for _, tt := range []struct {
		source string
		want   float64
	}{
		{"3", 3}, {"3.", 3}, {".5", 0.5}, {"3e2", 300}, {"3E-2", 0.03}, {"0012", 12},
		{"0x10", 16}, {"0xA.8p1", 21}, {"0x.8", 0.5}, {"0x1P-2", 0.25},
		{"1e999", math.Inf(1)},
	} {
		p, err := Parse(strings.NewReader("return "+tt.source), "=test", 0)
		if err != nil {
			t.Errorf("%s: %v", tt.source, err)
			continue
		}
		if !containsConstant(p.Constants, tt.want) {
			t.Errorf("%s: constants %v, want %v", tt.source, p.Constants, tt.want)
		}
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
