package main

import (
	"slices"
	"strings"

	"github.com/matjam/apogee/lua"
)

// completions returns the names that could complete the identifier chain
// before the cursor, such as "str" or "string.fo" or "s:up", and the
// length of the part they replace. It reads tables without calling
// metamethods, following __index tables but not functions, so completing
// never runs Lua code.
func completions(l *lua.State, before string) (candidates []string, replace int) {
	chain := trailingChain(before)
	if chain == "" {
		return nil, 0
	}
	cut := strings.LastIndexAny(chain, ".:")
	prefix := chain[cut+1:]
	top := l.Top()
	defer l.SetTop(top)
	if cut < 0 {
		l.PushGlobalTable()
		candidates = names(l, -1, prefix)
		for k := range keywords {
			if strings.HasPrefix(k, prefix) {
				candidates = append(candidates, k)
			}
		}
	} else {
		if !resolve(l, chain[:cut]) {
			return nil, 0
		}
		method := chain[cut] == ':'
		candidates = names(l, -1, prefix)
		if method {
			candidates = slices.DeleteFunc(candidates, func(name string) bool {
				return !fieldIsFunction(l, -1, name)
			})
		}
	}
	slices.Sort(candidates)
	return slices.Compact(candidates), len(prefix)
}

// trailingChain returns the names and separators at the end of s: the
// "a.b:c" in "print(a.b:c".
func trailingChain(s string) string {
	i := len(s)
	for i > 0 {
		c := s[i-1]
		if c == '.' || c == ':' || c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
			i--
			continue
		}
		break
	}
	chain := s[i:]
	if chain != "" && '0' <= chain[0] && chain[0] <= '9' {
		return "" // a number
	}
	if i > 0 && (s[i-1] == '"' || s[i-1] == '\'') {
		return "" // inside a string
	}
	return chain
}

// resolve pushes the value of the dotted path, a sequence of names, from
// the globals, or reports false.
func resolve(l *lua.State, path string) bool {
	l.PushGlobalTable()
	for _, name := range strings.FieldsFunc(path, func(r rune) bool { return r == '.' || r == ':' }) {
		if !rawField(l, -1, name) {
			return false
		}
		l.Remove(-2)
	}
	return true
}

// rawField pushes t[name] for the value at index without calling
// metamethods, looking through __index tables. A string's fields come
// from the string library, as its metatable's __index says.
func rawField(l *lua.State, index int, name string) bool {
	index = l.AbsIndex(index)
	for range 8 { // bound the chain of __index tables
		if l.TypeOf(index) == lua.TypeTable {
			l.PushString(name)
			l.RawGet(index)
			if !l.IsNil(-1) {
				return true
			}
			l.Pop(1)
		}
		if !l.MetaTable(index) {
			return false
		}
		l.PushString("__index")
		l.RawGet(-2)
		l.Remove(-2)
		if l.TypeOf(-1) != lua.TypeTable {
			l.Pop(1)
			return false
		}
		index = l.AbsIndex(-1)
	}
	return false
}

// names returns the string keys, usable as names, of the value at index
// and the __index tables behind it that start with prefix.
func names(l *lua.State, index int, prefix string) []string {
	var out []string
	top := l.Top()
	defer l.SetTop(top)
	index = l.AbsIndex(index)
	for range 8 {
		if l.TypeOf(index) == lua.TypeTable {
			l.PushNil()
			for l.Next(index) {
				if l.TypeOf(-2) == lua.TypeString {
					if k, _ := l.ToString(-2); isName(k) && strings.HasPrefix(k, prefix) {
						out = append(out, k)
					}
				}
				l.Pop(1)
			}
		}
		if !l.MetaTable(index) {
			break
		}
		l.PushString("__index")
		l.RawGet(-2)
		if l.TypeOf(-1) != lua.TypeTable {
			break
		}
		index = l.AbsIndex(-1)
	}
	return out
}

func fieldIsFunction(l *lua.State, index int, name string) bool {
	top := l.Top()
	defer l.SetTop(top)
	return rawField(l, index, name) && l.IsFunction(-1)
}

// commonPrefix returns the longest prefix all of names share.
func commonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	p := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}
