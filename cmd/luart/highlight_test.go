package main

import "testing"

// global is a keyword where it starts a declaration, and a name elsewhere.
func TestGlobalDeclarationHighlight(t *testing.T) {
	for _, tt := range []struct {
		code string
		want bool
	}{
		{"global x, y", true},
		{"global function f() end", true},
		{"global<const> *", true},
		{"global *", true},
		{"global = 1", false},
		{"print(global)", false},
	} {
		it, err := luaLexer.Tokenise(nil, tt.code)
		if err != nil {
			t.Fatal(err)
		}
		toks := it.Tokens()
		got := false
		for i := range toks {
			got = got || isGlobalDeclaration(toks, i)
		}
		if got != tt.want {
			t.Errorf("%q: declaration %v, want %v", tt.code, got, tt.want)
		}
	}
}
