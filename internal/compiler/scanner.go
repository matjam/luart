package compiler

import (
	"bytes"
	"fmt"
	"github.com/matjam/apogee/internal/luautf8"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/matjam/apogee/internal/bytecode"
)

const firstReserved = 257
const endOfStream = -1
const maxInt = int(^uint(0) >> 1)

const (
	tkAnd = iota + firstReserved
	tkBreak
	tkDo
	tkElse
	tkElseif
	tkEnd
	tkFalse
	tkFor
	tkFunction
	tkGoto
	tkIf
	tkIn
	tkLocal
	tkNil
	tkNot
	tkOr
	tkRepeat
	tkReturn
	tkThen
	tkTrue
	tkUntil
	tkWhile
	tkConcat
	tkDots
	tkEq
	tkGE
	tkLE
	tkNE
	tkDoubleColon
	tkIDiv
	tkShl
	tkShr
	tkEOS
	tkNumber
	tkName
	tkString
	reservedCount = tkWhile - firstReserved + 1
)

var tokens []string = []string{
	"and", "break", "do", "else", "elseif",
	"end", "false", "for", "function", "goto", "if",
	"in", "local", "nil", "not", "or", "repeat",
	"return", "then", "true", "until", "while",
	"..", "...", "==", ">=", "<=", "~=", "::", "//", "<<", ">>", "<eof>",
	"<number>", "<name>", "<string>",
}

type token struct {
	t rune
	n bytecode.Number
	s string
}

type scanner struct {
	depth                int // nested Go calls and parser levels, as MaxCallCount counts
	buffer               bytes.Buffer
	r                    io.ByteReader
	current              rune
	lineNumber, lastLine int
	source               string
	lookAheadToken       token
	token
}

func (s *scanner) assert(cond bool) {
	if !cond {
		panic(syntaxError("assertion failure"))
	}
}
func (s *scanner) syntaxError(message string) { s.scanError(message, s.t) }
func (s *scanner) errorExpected(t rune)       { s.syntaxError(s.tokenToString(t) + " expected") }
func (s *scanner) numberError()               { s.scanError("malformed number", tkNumber) }
func isNewLine(c rune) bool                   { return c == '\n' || c == '\r' }
func isDecimal(c rune) bool                   { return '0' <= c && c <= '9' }

// Characters classify as in the C locale, as in Lua: names are ASCII.
func isNameStart(c rune) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_' }
func isSpace(c rune) bool     { return c == ' ' || '\t' <= c && c <= '\r' }

// tokenToString describes a token as llex.c's luaX_token2str does: a
// symbol or reserved word quoted, any other token by its kind, such as
// <name>.
func (s *scanner) tokenToString(t rune) string {
	switch {
	case t < firstReserved && ' ' <= t && t < 0x7f:
		return fmt.Sprintf("'%c'", t)
	case t < firstReserved: // a control character, as Lua 5.5 shows it
		return fmt.Sprintf("'<\\%d>'", t)
	case t < tkEOS:
		return fmt.Sprintf("'%s'", tokens[t-firstReserved])
	}
	return tokens[t-firstReserved]
}

// txtToken describes a token for an error message as llex.c's txtToken
// does: a name, string or numeral by its text, which is in the buffer, the
// text of the token being scanned or the last one scanned.
func (s *scanner) txtToken(t rune) string {
	switch t {
	case tkName, tkString, tkNumber:
		return fmt.Sprintf("'%s'", s.buffer.String())
	}
	return s.tokenToString(t)
}

func (s *scanner) scanError(message string, token rune) {
	buff := ChunkID(s.source)
	if token != 0 {
		message = fmt.Sprintf("%s:%d: %s near %s", buff, s.lineNumber, message, s.txtToken(token))
	} else {
		message = fmt.Sprintf("%s:%d: %s", buff, s.lineNumber, message)
	}
	panic(syntaxError(message))
}

func (s *scanner) incrementLineNumber() {
	old := s.current
	s.assert(isNewLine(old))
	if s.advance(); isNewLine(s.current) && s.current != old {
		s.advance()
	}
	if s.lineNumber++; s.lineNumber >= maxInt {
		s.syntaxError("chunk has too many lines")
	}
}

func (s *scanner) advance() {
	if c, err := s.r.ReadByte(); err != nil {
		s.current = endOfStream
	} else {
		s.current = rune(c)
	}
}

func (s *scanner) saveAndAdvance() {
	s.save(s.current)
	s.advance()
}

func (s *scanner) advanceAndSave(c rune) {
	s.advance()
	s.save(c)
}

func (s *scanner) save(c rune) {
	if err := s.buffer.WriteByte(byte(c)); err != nil {
		s.scanError("lexical element too long", 0)
	}
}

func (s *scanner) checkNext(str string) bool {
	if s.current == 0 || !strings.ContainsRune(str, s.current) {
		return false
	}
	s.saveAndAdvance()
	return true
}

func (s *scanner) skipSeparator() int { // TODO is this the right name?
	i, c := 0, s.current
	s.assert(c == '[' || c == ']')
	for s.saveAndAdvance(); s.current == '='; i++ {
		s.saveAndAdvance()
	}
	if s.current == c {
		return i
	}
	return -i - 1
}

func (s *scanner) readMultiLine(comment bool, sep int) (str string) {
	if s.saveAndAdvance(); isNewLine(s.current) {
		s.incrementLineNumber()
	}
	for {
		switch s.current {
		case endOfStream:
			if comment {
				s.scanError("unfinished long comment", tkEOS)
			} else {
				s.scanError("unfinished long string", tkEOS)
			}
		case ']':
			if s.skipSeparator() == sep {
				s.saveAndAdvance()
				if comment {
					s.buffer.Reset()
				} else {
					str = s.buffer.String()
					str = str[2+sep : len(str)-(2+sep)]
				}
				return
			}
		case '\r', '\n':
			s.save('\n')
			s.incrementLineNumber()
		default:
			if !comment {
				s.save(s.current)
			}
			s.advance()
		}
	}
}

func isHexadecimal(c rune) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// readNumber reads a numeral as llex.c's read_numeral does, greedily:
// digits, hexadecimal digits, '.'s and an exponent with its sign. The text
// converts as a whole, so 3f and 1..2 are malformed numbers.
func (s *scanner) readNumber() token {
	expo := "Ee"
	first := s.current
	s.assert(isDecimal(first))
	s.saveAndAdvance()
	if first == '0' && s.checkNext("Xx") {
		expo = "Pp"
	}
	for {
		if s.checkNext(expo) {
			s.checkNext("+-")
		} else if isHexadecimal(s.current) || s.current == '.' {
			s.saveAndAdvance()
		} else {
			break
		}
	}
	if isNameStart(s.current) { // a numeral touching a letter, as 1print: an error, as llex.c
		s.saveAndAdvance()
	}
	n, ok := bytecode.Numeral(s.buffer.String())
	if !ok {
		s.numberError()
	}
	return token{t: tkNumber, n: n}
}

var escapes map[rune]rune = map[rune]rune{
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\', '"': '"', '\'': '\'',
}

// escapeError raises message near the string read so far, its escape
// and the character after it, as llex.c's esccheck shows them.
func (s *scanner) escapeError(c []rune, message string) {
	s.save('\\')
	for _, r := range c {
		if r == endOfStream {
			break
		}
		s.save(r)
	}
	s.scanError(message, tkString)
}

func (s *scanner) readHexEscape() (r rune) {
	s.advance()
	for i, c, b := 1, s.current, [3]rune{'x'}; i < len(b); i, c, r = i+1, s.current, r<<4+c {
		switch b[i] = c; {
		case '0' <= c && c <= '9':
			c = c - '0'
		case 'a' <= c && c <= 'f':
			c = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			c = c - 'A' + 10
		default:
			s.escapeError(b[:i+1], "hexadecimal digit expected")
		}
		s.advance()
	}
	return
}

// readUTF8Escape reads \u{XXX}, a value up to 2^31-1, as llex.c's
// readutf8esc.
func (s *scanner) readUTF8Escape() uint32 {
	seen := []rune{'u'}
	fail := func(message string) {
		if s.current != endOfStream {
			seen = append(seen, s.current)
		}
		s.escapeError(seen, message)
	}
	hex := func(c rune) (uint32, bool) {
		switch {
		case '0' <= c && c <= '9':
			return uint32(c - '0'), true
		case 'a' <= c && c <= 'f':
			return uint32(c-'a') + 10, true
		case 'A' <= c && c <= 'F':
			return uint32(c-'A') + 10, true
		}
		return 0, false
	}
	s.advance() // skip 'u'
	if s.current != '{' {
		fail("missing '{' in \\u{xxxx}")
	}
	seen = append(seen, '{')
	s.advance()
	r, ok := hex(s.current)
	if !ok {
		fail("hexadecimal digit expected")
	}
	for {
		seen = append(seen, s.current)
		s.advance()
		d, ok := hex(s.current)
		if !ok {
			break
		}
		if r > luautf8.MaxUTF>>4 {
			fail("UTF-8 value too large")
		}
		r = r<<4 + d
	}
	if s.current != '}' {
		fail("missing '}' in \\u{xxxx}")
	}
	s.advance() // skip '}'
	return r
}

func (s *scanner) readDecimalEscape() (r rune) {
	b := [3]rune{}
	for c, i := s.current, 0; i < len(b) && isDecimal(c); i, c = i+1, s.current {
		b[i], r = c, 10*r+c-'0'
		s.advance()
	}
	if r > math.MaxUint8 {
		s.escapeError(append(b[:], s.current), "decimal escape too large")
	}
	return
}

func (s *scanner) readString() token {
	delimiter := s.current
	for s.saveAndAdvance(); s.current != delimiter; {
		switch s.current {
		case endOfStream:
			s.scanError("unfinished string", tkEOS)
		case '\n', '\r':
			s.scanError("unfinished string", tkString)
		case '\\':
			s.advance()
			c := s.current
			switch esc, ok := escapes[c]; {
			case ok:
				s.advanceAndSave(esc)
			case isNewLine(c):
				s.incrementLineNumber()
				s.save('\n')
			case c == endOfStream: // do nothing
			case c == 'x':
				s.save(s.readHexEscape())
			case c == 'u':
				for _, b := range luautf8.Encode(s.readUTF8Escape()) {
					s.save(rune(b))
				}
			case c == 'z':
				for s.advance(); isSpace(s.current); {
					if isNewLine(s.current) {
						s.incrementLineNumber()
					} else {
						s.advance()
					}
				}
			default:
				if !isDecimal(c) {
					s.escapeError([]rune{c}, "invalid escape sequence")
				}
				s.save(s.readDecimalEscape())
			}
		default:
			s.saveAndAdvance()
		}
	}
	s.saveAndAdvance()
	str := s.buffer.String()
	return token{t: tkString, s: str[1 : len(str)-1]}
}

func isReserved(s string) bool {
	return slices.Contains(tokens[:reservedCount], s)
}

func (s *scanner) reservedOrName() token {
	str := s.buffer.String()
	for i, reserved := range tokens[:reservedCount] {
		if str == reserved {
			return token{t: rune(i + firstReserved), s: reserved}
		}
	}
	return token{t: tkName, s: str}
}

// scan reads the next token. Its text stays in the buffer until the next
// scan, for error messages.
func (s *scanner) scan() token {
	const comment, str = true, false
	s.buffer.Reset()
	for {
		switch c := s.current; c {
		case '\n', '\r':
			s.incrementLineNumber()
		case ' ', '\f', '\t', '\v':
			s.advance()
		case '-':
			if s.advance(); s.current != '-' {
				return token{t: '-'}
			}
			if s.advance(); s.current == '[' {
				if sep := s.skipSeparator(); sep >= 0 {
					_ = s.readMultiLine(comment, sep)
					break
				}
				s.buffer.Reset()
			}
			for !isNewLine(s.current) && s.current != endOfStream {
				s.advance()
			}
		case '[':
			if sep := s.skipSeparator(); sep >= 0 {
				return token{t: tkString, s: s.readMultiLine(str, sep)}
			} else if sep == -1 {
				return token{t: '['}
			}
			s.scanError("invalid long string delimiter", tkString)
		case '=':
			if s.advance(); s.current != '=' {
				return token{t: '='}
			}
			s.advance()
			return token{t: tkEq}
		case '<':
			switch s.advance(); s.current {
			case '=':
				s.advance()
				return token{t: tkLE}
			case '<':
				s.advance()
				return token{t: tkShl}
			}
			return token{t: '<'}
		case '>':
			switch s.advance(); s.current {
			case '=':
				s.advance()
				return token{t: tkGE}
			case '>':
				s.advance()
				return token{t: tkShr}
			}
			return token{t: '>'}
		case '/':
			if s.advance(); s.current != '/' {
				return token{t: '/'}
			}
			s.advance()
			return token{t: tkIDiv}
		case '~':
			if s.advance(); s.current != '=' {
				return token{t: '~'}
			}
			s.advance()
			return token{t: tkNE}
		case ':':
			if s.advance(); s.current != ':' {
				return token{t: ':'}
			}
			s.advance()
			return token{t: tkDoubleColon}
		case '"', '\'':
			return s.readString()
		case endOfStream:
			return token{t: tkEOS}
		case '.':
			if s.saveAndAdvance(); s.checkNext(".") {
				if s.checkNext(".") {
					s.buffer.Reset()
					return token{t: tkDots}
				}
				s.buffer.Reset()
				return token{t: tkConcat}
			} else if !isDecimal(s.current) {
				s.buffer.Reset()
				return token{t: '.'}
			} else {
				return s.readNumber()
			}
		case 0:
			s.advance()
		default:
			if isDecimal(c) {
				return s.readNumber()
			} else if isNameStart(c) {
				for ; isNameStart(c) || isDecimal(c); c = s.current {
					s.saveAndAdvance()
				}
				return s.reservedOrName()
			}
			s.advance()
			return token{t: c}
		}
	}
}

func (s *scanner) next() {
	s.lastLine = s.lineNumber
	if s.lookAheadToken.t != tkEOS {
		s.token = s.lookAheadToken
		s.lookAheadToken.t = tkEOS
	} else {
		s.token = s.scan()
	}
}

func (s *scanner) lookAhead() rune {
	s.assert(s.lookAheadToken.t == tkEOS)
	s.lookAheadToken = s.scan()
	return s.lookAheadToken.t
}

func (s *scanner) testNext(t rune) (r bool) {
	if r = s.t == t; r {
		s.next()
	}
	return
}

func (s *scanner) check(t rune) {
	if s.t != t {
		s.errorExpected(t)
	}
}

func (s *scanner) checkMatch(what, who rune, where int) {
	if !s.testNext(what) {
		if where == s.lineNumber {
			s.errorExpected(what)
		} else {
			s.syntaxError(fmt.Sprintf("%s expected (to close %s at line %d)", s.tokenToString(what), s.tokenToString(who), where))
		}
	}
}

// IDSize bounds the length of a chunk's name in messages, as ChunkID
// shortens it.
const IDSize = 60

// ChunkID returns the name of a chunk with source source as messages show
// it, as lobject.c's luaO_chunkid does, within IDSize: the text after '=',
// the end of a file name after '@', or [string "..."] with the source's
// first line, marked "..." when it is cut.
func ChunkID(source string) string {
	const pre, rets, pos = `[string "`, "...", `"]`
	if source != "" && (source[0] == '=' || source[0] == '@') {
		if len(source) <= IDSize { // small enough
			return source[1:]
		} else if source[0] == '=' { // truncate it
			return source[1:IDSize]
		}
		return rets + source[len(source)-(IDSize-len(rets)-1):] // the end of the file name
	}
	room := IDSize - len(pre+rets+pos) - 1
	nl := strings.IndexByte(source, '\n')
	if len(source) < room && nl < 0 { // a small one-line source
		return pre + source + pos
	}
	if nl >= 0 {
		source = source[:nl] // stop at the first new line
	}
	if len(source) > room {
		source = source[:room]
	}
	return pre + source + rets + pos
}
