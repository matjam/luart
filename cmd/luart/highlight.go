package main

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// A theme is the TUI's colours, taken from a Chroma style so that code and
// the rest of the screen agree.
type theme struct {
	style   *chroma.Style
	tokens  map[chroma.TokenType]lipgloss.Style
	accent  color.Color // the input box and prompt
	muted   color.Color // borders, hints and the status bar
	error   color.Color
	values  palette
	text    lipgloss.Style
	faint   lipgloss.Style
	errText lipgloss.Style
}

// newTheme makes the theme for a dark or light terminal.
func newTheme(dark bool) *theme {
	name := "catppuccin-mocha"
	if !dark {
		name = "catppuccin-latte"
	}
	s := styles.Get(name)
	t := &theme{style: s, tokens: map[chroma.TokenType]lipgloss.Style{}}
	fg := func(tt chroma.TokenType) color.Color { return hex(s.Get(tt).Colour) }
	t.accent = fg(chroma.Keyword)
	t.muted = fg(chroma.Comment)
	t.error = fg(chroma.GenericError)
	if s.Get(chroma.GenericError).Colour == s.Get(chroma.Text).Colour {
		t.error = lipgloss.Color("#f38ba8")
	}
	t.text = lipgloss.NewStyle().Foreground(fg(chroma.Text))
	t.faint = lipgloss.NewStyle().Foreground(t.muted)
	t.errText = lipgloss.NewStyle().Foreground(t.error)
	t.values = palette{
		number:  lipgloss.NewStyle().Foreground(fg(chroma.LiteralNumber)),
		str:     lipgloss.NewStyle().Foreground(fg(chroma.LiteralString)),
		keyword: lipgloss.NewStyle().Foreground(fg(chroma.KeywordConstant)),
		key:     lipgloss.NewStyle().Foreground(fg(chroma.NameVariable)),
		dim:     t.faint,
		punct:   lipgloss.NewStyle().Foreground(fg(chroma.Punctuation)),
	}
	return t
}

func hex(c chroma.Colour) color.Color {
	if !c.IsSet() {
		return nil
	}
	return lipgloss.Color(c.String())
}

// tokenStyle returns the style for tt, built once.
func (t *theme) tokenStyle(tt chroma.TokenType) lipgloss.Style {
	if st, ok := t.tokens[tt]; ok {
		return st
	}
	e := t.style.Get(tt)
	st := lipgloss.NewStyle()
	if c := hex(e.Colour); c != nil {
		st = st.Foreground(c)
	}
	if e.Bold == chroma.Yes {
		st = st.Bold(true)
	}
	if e.Italic == chroma.Yes {
		st = st.Italic(true)
	}
	t.tokens[tt] = st
	return st
}

var luaLexer = chroma.Coalesce(lexers.Get("lua"))

// isGlobalDeclaration reports whether toks[i] is the word global starting
// a Lua 5.5 declaration, which chroma's Lua 5.x lexer takes for a name:
// global followed by a name, function, an attribute or *.
func isGlobalDeclaration(toks []chroma.Token, i int) bool {
	if toks[i].Value != "global" || !toks[i].Type.InCategory(chroma.Name) {
		return false
	}
	for _, next := range toks[i+1:] {
		switch {
		case next.Type.InCategory(chroma.Text) && strings.TrimSpace(next.Value) == "":
			continue
		case next.Value == "function", next.Value == "<", next.Value == "*":
			return true
		default:
			return next.Type.InCategory(chroma.Name)
		}
	}
	return false
}

// highlight returns code's lines, coloured as Lua. Code that does not
// tokenise comes back plain.
func (t *theme) highlight(code string) []string {
	it, err := luaLexer.Tokenise(nil, code)
	if err != nil {
		return strings.Split(code, "\n")
	}
	toks := it.Tokens()
	lines := []string{""}
	for i, tok := range toks {
		if isGlobalDeclaration(toks, i) {
			tok.Type = chroma.Keyword
		}
		st := t.tokenStyle(tok.Type)
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				lines = append(lines, "")
			}
			if part != "" {
				lines[len(lines)-1] += st.Render(part)
			}
		}
	}
	// Chroma ends the text with a newline of its own.
	if n := len(lines); n > 1 && lines[n-1] == "" && !strings.HasSuffix(code, "\n") {
		lines = lines[:n-1]
	}
	return lines
}
