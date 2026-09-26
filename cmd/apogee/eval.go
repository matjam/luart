package main

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/matjam/apogee/lua"
)

// compile loads code typed at the REPL as the chunk "=stdin", leaving the
// function, or the error message, on the stack. It tries "return <code>"
// first, so that an expression prints its value as Lua 5.3's REPL does,
// and takes 5.2's "=expr" too.
func compile(l *lua.State, code string) error {
	if l.LoadBuffer("return "+code, "=stdin", "t") == nil {
		return nil
	}
	l.Pop(1)
	if expr, ok := strings.CutPrefix(code, "="); ok {
		code = "return " + expr
	}
	return l.LoadBuffer(code, "=stdin", "t")
}

// incomplete reports whether err, with its message on the top of the
// stack, says the code stopped mid-statement: lua.c's test.
func incomplete(l *lua.State, err error) bool {
	if err != lua.ErrSyntax {
		return false
	}
	msg, _ := l.ToString(-1)
	return strings.HasSuffix(msg, "<eof>")
}

// isComplete reports whether code compiles, or fails for a reason other
// than ending mid-statement, leaving the stack as it was.
func isComplete(l *lua.State, code string) bool {
	top := l.Top()
	defer l.SetTop(top)
	return !incomplete(l, compile(l, code))
}

// palette is the colours values print in.
type palette struct {
	number, str, keyword, key, dim, punct lipgloss.Style
}

func (p palette) render(s lipgloss.Style, text string) string { return s.Render(text) }

// A pnode is a value laid out for printing: a leaf's text, or a table's
// entries.
type pnode struct {
	text    string // a leaf, styled
	entries []pentry
	more    int  // entries left out
	table   bool // entries, not text, hold the value
}

type pentry struct {
	key string // styled, with " = ", or "" for an array element
	val *pnode
}

// formatter prints Lua values as trees.
type formatter struct {
	l        *lua.State
	p        palette
	maxDepth int
	maxItems int
}

// format returns the value at index, laid out in width columns.
func (f *formatter) format(index, width int) string {
	index = f.l.AbsIndex(index)
	n := f.node(index, 0, map[any]bool{})
	return strings.Join(layout(n, "", width), "\n")
}

func (f *formatter) node(index, depth int, seen map[any]bool) *pnode {
	l := f.l
	switch l.TypeOf(index) {
	case lua.TypeNil:
		return &pnode{text: f.p.keyword.Render("nil")}
	case lua.TypeBoolean:
		return &pnode{text: f.p.keyword.Render(strconv.FormatBool(l.ToBoolean(index)))}
	case lua.TypeNumber:
		l.PushValue(index)
		s, _ := l.ToString(-1)
		l.Pop(1)
		return &pnode{text: f.p.number.Render(s)}
	case lua.TypeString:
		s, _ := l.ToString(index)
		return &pnode{text: f.p.str.Render(quote(s))}
	case lua.TypeTable:
		return f.table(index, depth, seen)
	}
	return &pnode{text: f.p.dim.Render(f.tostring(index))}
}

// tostring is tostring(v), protected: a failing __tostring prints as
// what it raised.
func (f *formatter) tostring(index int) string {
	l := f.l
	l.Global("tostring")
	l.PushValue(index)
	if err := l.ProtectedCall(1, 1, 0); err != nil {
		msg, _ := l.ToString(-1)
		l.Pop(1)
		return "<" + msg + ">"
	}
	s, _ := l.ToString(-1)
	l.Pop(1)
	return s
}

func (f *formatter) table(index, depth int, seen map[any]bool) *pnode {
	l := f.l
	if l.MetaTable(index) {
		l.Field(-1, "__tostring")
		has := !l.IsNil(-1)
		l.Pop(2)
		if has {
			return &pnode{text: f.p.dim.Render(f.tostring(index))}
		}
	}
	id := l.ToValue(index)
	if seen[id] {
		return &pnode{text: f.p.dim.Render("<cycle>")}
	}
	if depth >= f.maxDepth {
		return &pnode{text: f.p.punct.Render("{…}")}
	}
	seen[id] = true
	defer delete(seen, id)
	n := &pnode{table: true}
	add := func(key string) bool {
		if len(n.entries) == f.maxItems {
			n.more++
			return false
		}
		n.entries = append(n.entries, pentry{key, f.node(l.Top(), depth+1, seen)})
		return true
	}
	length := l.RawLength(index)
	for i := 1; i <= length; i++ {
		l.RawGetInt(index, i)
		add("")
		l.Pop(1)
	}
	l.PushNil()
	for l.Next(index) {
		if k, ok := l.ToInteger(-2); ok && l.TypeOf(-2) == lua.TypeNumber && 1 <= k && k <= int64(length) {
			l.Pop(1)
			continue
		}
		add(f.key(l.Top() - 1))
		l.Pop(1)
	}
	return n
}

// key formats the table key at index, followed by " = ".
func (f *formatter) key(index int) string {
	l := f.l
	if l.TypeOf(index) == lua.TypeString {
		if s, _ := l.ToString(index); isName(s) {
			return f.p.key.Render(s) + f.p.punct.Render(" = ")
		}
	}
	// A number key would become a string if converted in place.
	return f.p.punct.Render("[") + f.node(index, f.maxDepth, nil).text + f.p.punct.Render("] = ")
}

// layout lays n out in width columns after indent: on one line if it fits,
// otherwise an entry a line.
func layout(n *pnode, indent string, width int) []string {
	if !n.table {
		return []string{n.text}
	}
	if line := oneLine(n); lipgloss.Width(indent)+lipgloss.Width(line) <= width {
		return []string{line}
	}
	lines := []string{"{"}
	inner := indent + "  "
	for i, e := range n.entries {
		sub := layout(e.val, inner+strings.Repeat(" ", lipgloss.Width(e.key)), width)
		sub[0] = inner + e.key + sub[0]
		if i < len(n.entries)-1 || n.more > 0 {
			sub[len(sub)-1] += ","
		}
		lines = append(lines, sub...)
	}
	if n.more > 0 {
		lines = append(lines, fmt.Sprintf("%s… %d more", inner, n.more))
	}
	return append(lines, indent+"}")
}

func oneLine(n *pnode) string {
	if !n.table {
		return n.text
	}
	if len(n.entries) == 0 && n.more == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(n.entries)+1)
	for _, e := range n.entries {
		parts = append(parts, e.key+oneLine(e.val))
	}
	if n.more > 0 {
		parts = append(parts, fmt.Sprintf("… %d more", n.more))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// quote quotes s as a Lua string literal.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if c < ' ' || c == 0x7f {
				fmt.Fprintf(&b, `\%d`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

var keywords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true,
	"false": true, "for": true, "function": true, "goto": true, "if": true, "in": true,
	"local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true,
	"then": true, "true": true, "until": true, "while": true,
}

// isName reports whether s is a Lua name, which a field access can use.
func isName(s string) bool {
	if s == "" || keywords[s] {
		return false
	}
	for i, c := range s {
		if !(c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || i > 0 && '0' <= c && c <= '9') {
			return false
		}
	}
	return true
}
