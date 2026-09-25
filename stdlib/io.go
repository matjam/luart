package stdlib

import (
	"fmt"
	"io"

	"os"

	"github.com/matjam/luart/lua"
)

const fileHandle = "FILE*"
const input = "_IO_input"
const output = "_IO_output"

type stream struct {
	f     *os.File
	close lua.Function
}

func toStream(l *lua.State) *stream { return l.CheckUserData[*stream](1, fileHandle) }

func toFile(l *lua.State) *os.File {
	s := toStream(l)
	if s.close == nil {
		l.Errorf("attempt to use a closed file")
	}
	if s.f == nil {
		l.Errorf("file handle has no file")
	}
	return s.f
}

func newStream(l *lua.State, f *os.File, close lua.Function) *stream {
	s := &stream{f: f, close: close}
	l.PushUserData(s)
	l.SetMetaTableNamed(fileHandle)
	return s
}

func newFile(l *lua.State) *stream {
	return newStream(l, nil, func(l *lua.State) int { return l.FileResult(toStream(l).f.Close(), "") })
}

func ioFile(l *lua.State, name string) *os.File {
	l.Field(lua.RegistryIndex, name)
	s := l.ToUserData(-1).(*stream)
	if s.close == nil {
		l.Errorf(fmt.Sprintf("standard %s file is closed", name[len("_IO_"):]))
	}
	return s.f
}

func forceOpen(l *lua.State, name, mode string) {
	s := newFile(l)
	flags, err := flags(mode)
	if err == nil {
		s.f, err = os.OpenFile(name, flags, 0666)
	}
	if err != nil {
		l.Errorf(fmt.Sprintf("cannot open file '%s' (%s)", name, err.Error()))
	}
}

func ioFileHelper(name, mode string) lua.Function {
	return func(l *lua.State) int {
		if !l.IsNoneOrNil(1) {
			if name, ok := l.ToString(1); ok {
				forceOpen(l, name, mode)
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

func write(l *lua.State, f *os.File, argIndex int) int {
	var err error
	for argCount := l.Top(); argIndex < argCount && err == nil; argIndex++ {
		if l.TypeOf(argIndex) == lua.TypeNumber { // not numeric strings, as C Lua
			s, _ := l.ToString(argIndex) // formats as Lua does
			_, err = f.WriteString(s)
		} else {
			_, err = f.WriteString(l.CheckString(argIndex))
		}
	}
	if err == nil {
		return 1
	}
	return l.FileResult(err, "")
}

func readNumber(l *lua.State, f *os.File) (err error) {
	var n float64
	if _, err = fmt.Fscanf(f, "%f", &n); err == nil {
		l.PushNumber(n)
	} else {
		l.PushNil()
	}
	return
}

func read(l *lua.State, f *os.File, argIndex int) int {
	resultCount := 0
	var err error
	if argCount := l.Top() - 1; argCount == 0 {
		//		err = readLineHelper(l, f, true)
		resultCount = argIndex + 1
	} else {
		// TODO
	}
	if err != nil {
		return l.FileResult(err, "")
	}
	if err == io.EOF {
		l.Pop(1)
		l.PushNil()
	}
	return resultCount - argIndex
}

func readLine(l *lua.State) int {
	s := l.ToUserData(lua.UpValueIndex(1)).(*stream)
	argCount, _ := l.ToInteger(lua.UpValueIndex(2))
	if s.close == nil {
		l.Errorf("file is already closed")
	}
	l.SetTop(1)
	for i := 1; i <= argCount; i++ {
		l.PushValue(lua.UpValueIndex(3 + i))
	}
	resultCount := read(l, s.f, 2)
	if resultCount <= 0 {
		l.Errorf("read returned no results")
	}
	if !l.IsNil(-resultCount) {
		return resultCount
	}
	if resultCount > 1 {
		m, _ := l.ToString(-resultCount + 1)
		l.Errorf(m)
	}
	if l.ToBoolean(lua.UpValueIndex(3)) {
		l.SetTop(0)
		l.PushValue(lua.UpValueIndex(1))
		closeHelper(l)
	}
	return 0
}

func lines(l *lua.State, shouldClose bool) {
	argCount := l.Top() - 1
	l.ArgumentCheck(argCount <= lua.MinStack-3, lua.MinStack-3, "too many options")
	l.PushValue(1)
	l.PushInteger(argCount)
	l.PushBoolean(shouldClose)
	for i := 1; i <= argCount; i++ {
		l.PushValue(i + 1)
	}
	l.PushGoClosure(readLine, uint8(3+argCount))
}

func flags(m string) (f int, err error) {
	if len(m) > 0 && m[len(m)-1] == 'b' {
		m = m[:len(m)-1]
	}
	switch m {
	case "r":
		f = os.O_RDONLY
	case "r+":
		f = os.O_RDWR
	case "w":
		f = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "w+":
		f = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	case "a":
		f = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case "a+":
		f = os.O_RDWR | os.O_CREATE | os.O_APPEND
	default:
		err = os.ErrInvalid
	}
	return
}

var ioLibrary = []lua.RegistryFunction{
	{Name: "close", Function: ioClose},
	{Name: "flush", Function: func(l *lua.State) int { return l.FileResult(ioFile(l, output).Sync(), "") }},
	{Name: "input", Function: ioFileHelper(input, "r")},
	{Name: "lines", Function: func(l *lua.State) int {
		if l.IsNone(1) {
			l.PushNil()
		}
		if l.IsNil(1) { // No file name.
			l.Field(lua.RegistryIndex, input)
			l.Replace(1)
			toFile(l)
			lines(l, false)
		} else {
			forceOpen(l, l.CheckString(1), "r")
			l.Replace(1)
			lines(l, true)
		}
		return 1
	}},
	{Name: "open", Function: func(l *lua.State) int {
		name := l.CheckString(1)
		flags, err := flags(l.OptString(2, "r"))
		s := newFile(l)
		l.ArgumentCheck(err == nil, 2, "invalid mode")
		s.f, err = os.OpenFile(name, flags, 0666)
		if err == nil {
			return 1
		}
		return l.FileResult(err, name)
	}},
	{Name: "output", Function: ioFileHelper(output, "w")},
	{Name: "popen", Function: func(l *lua.State) int { l.Errorf("'popen' not supported"); panic("unreachable") }},
	{Name: "read", Function: func(l *lua.State) int { return read(l, ioFile(l, input), 1) }},
	{Name: "tmpfile", Function: func(l *lua.State) int {
		s := newFile(l)
		f, err := os.CreateTemp("", "")
		if err == nil {
			s.f = f
			return 1
		}
		return l.FileResult(err, "")
	}},
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
	{Name: "flush", Function: func(l *lua.State) int { return l.FileResult(toFile(l).Sync(), "") }},
	{Name: "lines", Function: func(l *lua.State) int { toFile(l); lines(l, false); return 1 }},
	{Name: "read", Function: func(l *lua.State) int { return read(l, toFile(l), 2) }},
	{Name: "seek", Function: func(l *lua.State) int {
		whence := []int{os.SEEK_SET, os.SEEK_CUR, os.SEEK_END}
		f := toFile(l)
		op := l.CheckOption(2, "cur", []string{"set", "cur", "end"})
		p3 := l.OptNumber(3, 0)
		offset := int64(p3)
		l.ArgumentCheck(float64(offset) == p3, 3, "not an integer in proper range")
		ret, err := f.Seek(offset, whence[op])
		if err != nil {
			return l.FileResult(err, "")
		}
		l.PushNumber(float64(ret))
		return 1
	}},
	{Name: "setvbuf", Function: func(l *lua.State) int { // Files are unbuffered in Go. Fake support for now.
		//		f := toFile(l)
		//		op := CheckOption(l, 2, "", []string{"no", "full", "line"})
		//		size := OptInteger(l, 3, 1024)
		// TODO err := setvbuf(f, nil, mode[op], size)
		return l.FileResult(nil, "")
	}},
	{Name: "write", Function: func(l *lua.State) int { l.PushValue(1); return write(l, toFile(l), 2) }},
	//	{"__gc", },
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
