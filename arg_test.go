package luart

import (
	"strings"
	"testing"
)

func TestArg(t *testing.T) {
	tests := []struct {
		name    string
		call    string
		want    string
		wantErr string
	}{
		{"float64", `f(1.5)`, "1.5", ""},
		{"float64 from a string", `f("2")`, "2", ""},
		{"int", `i(3.9)`, "3", ""},
		{"string", `s("x")`, "x", ""},
		{"string from a number", `s(4)`, "4", ""},
		{"bool", `b(nil)`, "false", ""},
		{"wrong type", `f({})`, "", "bad argument #1 to 'f' (number expected, got table)"},
		{"missing", `s()`, "", "bad argument #1 to 's' (string expected, got no value)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewState()
			OpenLibraries(l)
			var got string
			l.Register("f", func(l *State) int { got = numberToString(l.Arg[float64](1)); return 0 })
			l.Register("i", func(l *State) int { got = numberToString(float64(l.Arg[int](1))); return 0 })
			l.Register("s", func(l *State) int { got = l.Arg[string](1); return 0 })
			l.Register("b", func(l *State) int {
				if l.Arg[bool](1) {
					got = "true"
				} else {
					got = "false"
				}
				return 0
			})
			err := DoString(l, tt.call)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
