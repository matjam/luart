package stdlib

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/matjam/luart/lua"
)

// The io library, after liolib.c.

const fileHandle = "FILE*"
const input = "_IO_input"
const output = "_IO_output"

// A stream is a Lua file handle: an os.File, read through a buffer as C's
// FILE is. Writes are not buffered unless a script asks with setvbuf, so
// by default a host that exits without closing its files loses no output.
type stream struct {
	f     *os.File
	close lua.Function  // closes f; nil while the stream is closed
	r     *bufio.Reader // created by the first read
	w     *bufio.Writer // set by setvbuf "full" or "line"
	line  bool          // flush w at each new line
}

// flush writes out what s has buffered.
func (s *stream) flush() error {
	if s.w == nil {
		return nil
	}
	return s.w.Flush()
}

// reader returns s's read buffer.
func (s *stream) reader() *bufio.Reader {
	if s.r == nil {
		s.r = bufio.NewReader(s.f)
	}
	return s.r
}

// unread gives back what s has read ahead, before a write or a seek, by
// moving the file back to the position its reads reached. A pipe cannot
// move back; C's FILE loses the same data.
func (s *stream) unread() {
	if s.r == nil {
		return
	}
	if n := s.r.Buffered(); n > 0 {
		s.f.Seek(-int64(n), io.SeekCurrent)
	}
	s.r.Reset(s.f)
}

func toStream(l *lua.State) *stream { return l.CheckUserData[*stream](1, fileHandle) }

// toFile is liolib.c's tofile: the stream at 1, which must be open.
func toFile(l *lua.State) *stream {
	s := toStream(l)
	if s.close == nil {
		l.Errorf("attempt to use a closed file")
	}
	return s
}

// newPreFile pushes a new stream, closed until a file is opened for it.
func newPreFile(l *lua.State) *stream {
	s := &stream{}
	l.PushUserData(s)
	l.SetMetaTableNamed(fileHandle)
	return s
}

// newStream pushes a stream for f, open, which close closes.
func newStream(l *lua.State, f *os.File, close lua.Function) *stream {
	s := newPreFile(l)
	s.f, s.close = f, close
	return s
}

func fileClose(l *lua.State) int {
	s := toStream(l)
	return l.FileResult(errors.Join(s.flush(), s.f.Close()), "")
}

// checkMode reports whether mode is one liolib.c's checkmode accepts: r,
// w or a, an optional +, and any number of b.
func checkMode(mode string) bool {
	if mode == "" || !strings.Contains("rwa", mode[:1]) {
		return false
	}
	return strings.Trim(strings.TrimPrefix(mode[1:], "+"), "b") == ""
}

// openFile opens name for s with mode, a mode checkMode accepts, as fopen
// does.
func openFile(s *stream, name, mode string) error {
	flags, plus := 0, strings.Contains(mode, "+")
	switch mode[0] {
	case 'r':
		flags = os.O_RDONLY
		if plus {
			flags = os.O_RDWR
		}
	case 'w':
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if plus {
			flags = os.O_RDWR | os.O_CREATE | os.O_TRUNC
		}
	case 'a':
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		if plus {
			flags = os.O_RDWR | os.O_CREATE | os.O_APPEND
		}
	}
	f, err := os.OpenFile(name, flags, 0666)
	if err != nil {
		return err
	}
	s.f, s.close = f, fileClose
	return nil
}

// openCheckFile pushes name opened with mode, or raises an error.
func openCheckFile(l *lua.State, name, mode string) {
	s := newPreFile(l)
	if err := openFile(s, name, mode); err != nil {
		l.FileResult(err, "")
		msg, _ := l.ToString(-2)
		l.Errorf("cannot open file '%s' (%s)", name, msg)
	}
}

// ioFile is liolib.c's getiofile: it pushes the default input or output.
func ioFile(l *lua.State, name string) *stream {
	l.Field(lua.RegistryIndex, name)
	s := l.ToUserData(-1).(*stream)
	if s.close == nil {
		l.Errorf("standard %s file is closed", name[len("_IO_"):])
	}
	return s
}

// ioFileHelper is io.input or io.output: it sets the default from a file
// name or handle, and returns it.
func ioFileHelper(name, mode string) lua.Function {
	return func(l *lua.State) int {
		if !l.IsNoneOrNil(1) {
			if fileName, ok := l.ToString(1); ok {
				openCheckFile(l, fileName, mode)
			} else {
				toFile(l)
				l.PushValue(1)
			}
			l.SetField(lua.RegistryIndex, name)
		}
		l.Field(lua.RegistryIndex, name)
		return 1
	}
}

func closeHelper(l *lua.State) int {
	s := toStream(l)
	close := s.close
	s.close = nil
	return close(l)
}

func ioClose(l *lua.State) int {
	if l.IsNone(1) {
		l.Field(lua.RegistryIndex, output)
	}
	toFile(l)
	return closeHelper(l)
}

// ioPopen is io.popen(prog [, mode]), after liolib.c's io_popen. The file
// reads the command's output, for mode "r", or writes its input, for "w";
// closing it waits for the command and returns its status as os.execute
// does.
func ioPopen(l *lua.State) int {
	prog, mode := l.CheckString(1), l.OptString(2, "r")
	l.ArgumentCheck(mode == "r" || mode == "w", 2, "invalid mode")
	r, w, err := os.Pipe()
	if err != nil {
		return l.FileResult(err, prog)
	}
	cmd := shellCommand(prog)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	ours, theirs := r, w
	if mode == "r" {
		cmd.Stdout = w
	} else {
		cmd.Stdin, ours, theirs = r, w, r
	}
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return l.FileResult(err, prog)
	}
	theirs.Close() // the command has its own copy
	newStream(l, ours, func(l *lua.State) int {
		s := toStream(l)
		s.flush()
		s.f.Close()
		return execResult(l, cmd.Wait())
	})
	return 1
}

// ioTmpfile is io.tmpfile: a file for reading and writing, removed when it
// is closed.
func ioTmpfile(l *lua.State) int {
	s := newPreFile(l)
	f, err := os.CreateTemp("", "lua_")
	if err != nil {
		return l.FileResult(err, "")
	}
	s.f, s.close = f, func(l *lua.State) int {
		err := errors.Join(s.flush(), f.Close())
		os.Remove(f.Name())
		return l.FileResult(err, "")
	}
	return 1
}

// write is liolib.c's g_write: it writes the arguments from arg on, and
// returns the file, which is on the stack's top.
func write(l *lua.State, s *stream, arg int) int {
	s.unread()
	var err error
	for n := l.Top() - arg; n > 0 && err == nil; n, arg = n-1, arg+1 {
		var str string
		if l.TypeOf(arg) == lua.TypeNumber { // not numeric strings, as in C
			str, _ = l.ToString(arg) // formats as Lua does
		} else {
			str = l.CheckString(arg)
		}
		if s.w == nil {
			_, err = s.f.WriteString(str)
		} else if _, err = s.w.WriteString(str); err == nil && s.line && strings.Contains(str, "\n") {
			err = s.w.Flush()
		}
	}
	if err != nil {
		return l.FileResult(err, "")
	}
	return 1
}

// read is liolib.c's g_read: it reads with each format from first on, or
// a line, until one fails, which gives nil. A read error gives nil, the
// error and its errno.
func read(l *lua.State, s *stream, first int) int {
	if err := s.flush(); err != nil { // C needs a flush between them; do it here
		return l.FileResult(err, "")
	}
	r := s.reader()
	nargs := l.Top() - 1
	var err error
	success, n := true, first
	if nargs == 0 {
		success, err = readLine(l, r, true)
		n = first + 1
	} else {
		l.CheckStackWithMessage(nargs+lua.MinStack, "too many arguments")
		for ; nargs > 0 && success && err == nil; nargs, n = nargs-1, n+1 {
			if l.TypeOf(n) == lua.TypeNumber {
				if k, _ := l.ToInteger(n); k == 0 {
					success, err = testEOF(l, r)
				} else {
					success, err = readChars(l, r, int(k))
				}
				continue
			}
			p := strings.TrimPrefix(l.CheckString(n), "*") // optional since 5.3
			switch p += "\x00"; p[0] {
			case 'n':
				success, err = readNumber(l, r)
			case 'l':
				success, err = readLine(l, r, true)
			case 'L':
				success, err = readLine(l, r, false)
			case 'a':
				err = readAll(l, r)
			default:
				l.ArgumentError(n, "invalid format")
			}
		}
	}
	if err != nil {
		return l.FileResult(err, "")
	}
	if !success {
		l.Pop(1)
		l.PushNil()
	}
	return n - first
}

// readLine reads a line, without its '\n' if chop. It fails at the end of
// the file.
func readLine(l *lua.State, r *bufio.Reader, chop bool) (bool, error) {
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	success := line != ""
	if chop {
		line = strings.TrimSuffix(line, "\n")
	}
	l.PushString(line)
	return success, nil
}

// readChars reads up to n bytes. It fails if there are none.
func readChars(l *lua.State, r *bufio.Reader, n int) (bool, error) {
	var b strings.Builder
	if _, err := io.CopyN(&b, r, int64(n)); err != nil && err != io.EOF {
		return false, err
	}
	l.PushString(b.String())
	return b.Len() > 0, nil
}

// testEOF pushes "" and reports whether the file has more to read.
func testEOF(l *lua.State, r *bufio.Reader) (bool, error) {
	l.PushString("")
	if _, err := r.Peek(1); err == io.EOF {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// readAll reads the rest of the file, "" at its end.
func readAll(l *lua.State, r *bufio.Reader) error {
	b, err := io.ReadAll(r)
	l.PushString(string(b))
	return err
}

// readNumber reads a numeral as fscanf's %lf does: after any space, a
// sign, then decimal digits with a fraction and an exponent, or 0x and
// hexadecimal ones with a binary exponent. It fails, pushing nil, if the
// text does not make a number.
func readNumber(l *lua.State, r *bufio.Reader) (bool, error) {
	var b []byte
	accept := func(set string) bool {
		c, err := r.ReadByte()
		if err != nil {
			return false
		}
		if strings.IndexByte(set, c) < 0 {
			r.UnreadByte()
			return false
		}
		b = append(b, c)
		return true
	}
	for accept(" \t\n\v\f\r") {
		b = b[:0]
	}
	accept("+-")
	digits, exponent := "0123456789", "eE"
	if accept("0") && accept("xX") {
		digits, exponent = "0123456789abcdefABCDEF", "pP"
	}
	for accept(digits) {
	}
	if accept(".") {
		for accept(digits) {
		}
	}
	if accept(exponent) {
		accept("+-")
		for accept("0123456789") {
		}
	}
	if !l.StringToNumber(string(b)) { // an integer numeral reads as an integer
		l.PushNil()
		return false, nil
	}
	return true, nil
}

// linesIterator is liolib.c's io_readline: it reads the file in upvalue
// 1 with the formats after upvalue 3, and closes it at its end if upvalue
// 3 is true.
func linesIterator(l *lua.State) int {
	s := l.ToUserData(lua.UpValueIndex(1)).(*stream)
	count, _ := l.ToInteger(lua.UpValueIndex(2))
	n := int(count)
	if s.close == nil {
		l.Errorf("file is already closed")
	}
	l.SetTop(1)
	for i := 1; i <= n; i++ {
		l.PushValue(lua.UpValueIndex(3 + i))
	}
	n = read(l, s, 2)
	if !l.IsNil(-n) { // read at least one value
		return n
	}
	if n > 1 { // an error
		m, _ := l.ToString(-n + 1)
		l.Errorf("%s", m)
	}
	if l.ToBoolean(lua.UpValueIndex(3)) {
		l.SetTop(0)
		l.PushValue(lua.UpValueIndex(1))
		closeHelper(l)
	}
	return 0
}

// lines pushes an iterator over the file at 1, reading with the formats
// after it.
func lines(l *lua.State, shouldClose bool) {
	n := l.Top() - 1
	l.ArgumentCheck(n <= lua.MinStack-3, lua.MinStack-3, "too many options")
	l.PushValue(1)
	l.PushInteger(n)
	l.PushBoolean(shouldClose)
	for i := 1; i <= n; i++ {
		l.PushValue(i + 1)
	}
	l.PushGoClosure(linesIterator, uint8(3+n))
}

var ioLibrary = []lua.RegistryFunction{
	{Name: "close", Function: ioClose},
	{Name: "flush", Function: func(l *lua.State) int { return l.FileResult(ioFile(l, output).flush(), "") }},
	{Name: "input", Function: ioFileHelper(input, "r")},
	{Name: "lines", Function: func(l *lua.State) int {
		if l.IsNone(1) {
			l.PushNil()
		}
		if l.IsNil(1) { // the default input, left open
			l.Field(lua.RegistryIndex, input)
			l.Replace(1)
			toFile(l)
			lines(l, false)
		} else { // a file, closed at its end
			openCheckFile(l, l.CheckString(1), "r")
			l.Replace(1)
			lines(l, true)
		}
		return 1
	}},
	{Name: "open", Function: func(l *lua.State) int {
		name, mode := l.CheckString(1), l.OptString(2, "r")
		s := newPreFile(l)
		l.ArgumentCheck(checkMode(mode), 2, "invalid mode")
		if err := openFile(s, name, mode); err != nil {
			return l.FileResult(err, name)
		}
		return 1
	}},
	{Name: "output", Function: ioFileHelper(output, "w")},
	{Name: "popen", Function: ioPopen},
	{Name: "read", Function: func(l *lua.State) int { return read(l, ioFile(l, input), 1) }},
	{Name: "tmpfile", Function: ioTmpfile},
	{Name: "type", Function: func(l *lua.State) int {
		l.CheckAny(1)
		if f, ok := l.TestUserData(1, fileHandle).(*stream); !ok {
			l.PushNil()
		} else if f.close == nil {
			l.PushString("closed file")
		} else {
			l.PushString("file")
		}
		return 1
	}},
	{Name: "write", Function: func(l *lua.State) int { return write(l, ioFile(l, output), 1) }},
}

var fileHandleMethods = []lua.RegistryFunction{
	{Name: "close", Function: ioClose},
	{Name: "flush", Function: func(l *lua.State) int { return l.FileResult(toFile(l).flush(), "") }},
	{Name: "lines", Function: func(l *lua.State) int { toFile(l); lines(l, false); return 1 }},
	{Name: "read", Function: func(l *lua.State) int { return read(l, toFile(l), 2) }},
	{Name: "seek", Function: func(l *lua.State) int {
		s := toFile(l)
		op := l.CheckOption(2, "cur", []string{"set", "cur", "end"})
		p3 := l.OptNumber(3, 0)
		offset := int64(p3)
		l.ArgumentCheck(float64(offset) == p3, 3, "not an integer in proper range")
		if err := s.flush(); err != nil {
			return l.FileResult(err, "")
		}
		s.unread()
		pos, err := s.f.Seek(offset, []int{io.SeekStart, io.SeekCurrent, io.SeekEnd}[op])
		if err != nil {
			return l.FileResult(err, "")
		}
		l.PushInteger(pos)
		return 1
	}},
	{Name: "setvbuf", Function: func(l *lua.State) int {
		s := toFile(l)
		mode := l.CheckOption(2, "", []string{"no", "full", "line"})
		size := optInt(l, 3, 1024)
		if err := s.flush(); err != nil {
			return l.FileResult(err, "")
		}
		s.w, s.line = nil, mode == 2
		if mode != 0 {
			s.w = bufio.NewWriterSize(s.f, size)
		}
		return l.FileResult(nil, "")
	}},
	{Name: "write", Function: func(l *lua.State) int {
		s := toFile(l)
		l.PushValue(1) // to return
		return write(l, s, 2)
	}},
	{Name: "__gc", Function: func(l *lua.State) int { // close an open file, ignoring errors
		if s := toStream(l); s.close != nil && s.f != nil {
			closeHelper(l)
		}
		return 0
	}},
	{Name: "__tostring", Function: func(l *lua.State) int {
		if s := toStream(l); s.close == nil {
			l.PushString("file (closed)")
		} else {
			l.PushString(fmt.Sprintf("file (%p)", s.f))
		}
		return 1
	}},
}

func dontClose(l *lua.State) int {
	toStream(l).close = dontClose
	l.PushNil()
	l.PushString("cannot close standard file")
	return 2
}

func registerStdFile(l *lua.State, f *os.File, reg, name string) {
	newStream(l, f, dontClose)
	if reg != "" {
		l.PushValue(-1)
		l.SetField(lua.RegistryIndex, reg)
	}
	l.SetField(-2, name)
}

// OpenIO opens the io library. Usually passed to Require.
func OpenIO(l *lua.State) int {
	l.NewLibrary(ioLibrary)

	l.NewMetaTable(fileHandle)
	l.PushValue(-1)
	l.SetField(-2, "__index")
	l.SetFunctions(fileHandleMethods, 0)
	l.Pop(1)

	registerStdFile(l, os.Stdin, input, "stdin")
	registerStdFile(l, os.Stdout, output, "stdout")
	registerStdFile(l, os.Stderr, "", "stderr")

	return 1
}
