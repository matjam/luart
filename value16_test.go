package luart

import "testing"

// The two-word value compares numbers by bits and strings by address, so
// equality and table keys must go through rawEqual and hashKey.
func TestTwoWordValueSemantics(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"zero equals negative zero", `
			local z = 0
			assert(z == -z and not (z ~= -z) and rawequal(z, -z))`},
		{"NaN is not equal to itself", `
			local n = 0/0
			assert(n ~= n and not rawequal(n, n))`},
		{"negative zero is the same hash key", `
			local t, z = {}, 0
			t[1.5] = "a"
			t[-z + 0.0] = "zero"
			assert(t[0] == "zero" and t[1.5] == "a")
			local h = {[{}] = 1}
			h[-z] = "neg"
			assert(h[0] == "neg")`},
		{"runtime strings equal constants", `
			local a = "hel" .. "lo"
			local b = string.rep("l", 2)
			assert(a == "hello" and ("he" .. b .. "o") == a and rawequal(a, "hello"))`},
		{"runtime strings index shapes", `
			local t = {hello = 1}
			assert(t["hel" .. "lo"] == 1)
			local k = string.char(104, 105)
			t[k] = 2
			assert(t.hi == 2)`},
		{"empty strings", `
			local e = ("x"):sub(2)
			assert(e == "" and #e == 0 and ({[""] = 1})[e] == 1)`},
		{"booleans and nil", `
			assert(true == true and false ~= true and nil == nil)
			local t = {[true] = 1, [false] = 2}
			assert(t[true] == 1 and t[false] == 2)`},
		{"tables compare by identity", `
			local a, b = {}, {}
			assert(a ~= b and a == a and rawequal(a, a))`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewState()
			OpenLibraries(l)
			if err := l.DoString(tt.src); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLightUserData(t *testing.T) {
	type key struct{ n int }
	l := NewState()
	OpenLibraries(l)

	l.PushLightUserData(key{1})
	l.PushLightUserData(key{1})
	if !l.RawEqual(-1, -2) {
		t.Fatal("equal Go values are not equal light userdata")
	}
	l.PushLightUserData(key{2})
	if l.RawEqual(-1, -2) {
		t.Fatal("different Go values are equal light userdata")
	}
	if got := l.TypeOf(-1); got != TypeLightUserData {
		t.Fatalf("TypeOf = %v, want TypeLightUserData", got)
	}
	if got := l.ToValue(-1); got != (key{2}) {
		t.Fatalf("ToValue = %v, want key{2}", got)
	}
	l.SetTop(0)

	// Light userdata as registry keys.
	l.NewTable()
	l.PushLightUserData(key{7})
	l.PushString("seven")
	l.RawSet(-3)
	l.RawGetValue(-1, key{7})
	if s, _ := l.ToString(-1); s != "seven" {
		t.Fatalf("RawGetValue = %q, want seven", s)
	}

	// Values Go cannot compare are distinct light userdata.
	l.PushLightUserData([]int{1})
	l.PushLightUserData([]int{1})
	if l.RawEqual(-1, -2) {
		t.Fatal("uncomparable Go values compared equal")
	}
}
