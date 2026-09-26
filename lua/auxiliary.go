package lua

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

func functionName(l *State, d Debug) string {
	switch {
	case d.NameKind != "":
		return fmt.Sprintf("function '%s'", d.Name)
	case d.What == "main":
		return "main chunk"
	case d.What == l.global.goName:
		if pushGlobalFunctionName(l, Frame{d.callInfo}) {
			s, _ := l.ToString(-1)
			l.Pop(1)
			return fmt.Sprintf("function '%s'", s)
		}
		return "?"
	}
	return fmt.Sprintf("function <%s:%d>", d.ShortSource, d.LineDefined)
}

func countLevels(l *State) int {
	li, le := 1, 1
	for _, ok := l.Frame(le); ok; _, ok = l.Frame(le) {
		li = le
		le *= 2
	}
	for li < le {
		m := (li + le) / 2
		if _, ok := l.Frame(m); ok {
			li = m + 1
		} else {
			le = m
		}
	}
	return le - 1
}

// Traceback creates and pushes a traceback of the stack l1. If message is not
// nil it is appended at the beginning of the traceback. The level parameter
// tells at which level to start the traceback.
func (l *State) Traceback(l1 *State, message string, level int) {
	const levels1, levels2 = 12, 10
	levels := countLevels(l1)
	mark := 0
	if levels > levels1+levels2 {
		mark = levels1
	}
	buf := message
	if buf != "" {
		buf += "\n"
	}
	buf += "stack traceback:"
	for f, ok := l1.Frame(level); ok; f, ok = l1.Frame(level) {
		if level++; level == mark {
			buf += "\n\t..."
			level = levels - levels2
		} else {
			d, _ := l1.Info("Slnt", f)
			buf += "\n\t" + d.ShortSource + ":"
			if d.CurrentLine > 0 {
				buf += fmt.Sprintf("%d:", d.CurrentLine)
			}
			buf += " in " + functionName(l, d)
			if d.IsTailCall {
				buf += "\n\t(...tail calls...)"
			}
		}
	}
	l.PushString(buf)
}

// MetaField pushes onto the stack the field event from the metatable of the
// object at index. If the object does not have a metatable, or if the
// metatable does not have this field, returns false and pushes nothing.
func (l *State) MetaField(index int, event string) bool {
	if !l.MetaTable(index) {
		return false
	}
	l.PushString(event)
	l.RawGet(-2)
	if l.IsNil(-1) {
		l.Pop(2) // remove metatable and metafield
		return false
	}
	l.Remove(-2) // remove only metatable
	return true
}

// CallMeta calls a metamethod.
//
// If the object at index has a metatable and this metatable has a field event,
// this function calls this field passing the object as its only argument. In
// this case this function returns true and pushes onto the stack the value
// returned by the call. If there is no metatable or no metamethod, this
// function returns false (without pushing any value on the stack).
func (l *State) CallMeta(index int, event string) bool {
	index = l.AbsIndex(index)
	if !l.MetaField(index, event) {
		return false
	}
	l.PushValue(index)
	l.Call(1, 1)
	return true
}

// ArgumentError raises an error with a standard message that includes extraMessage as a comment.
//
// This function never returns. It is an idiom to use it in Go functions as
//
//	l.ArgumentError(args, "message")
//	panic("unreachable")
func (l *State) ArgumentError(argCount int, extraMessage string) {
	f, ok := l.Frame(0)
	if !ok { // no stack frame?
		l.Errorf("bad argument #%d (%s)", argCount, extraMessage)
		return
	}
	d, _ := l.Info("n", f)
	if d.NameKind == "method" {
		argCount--         // do not count 'self'
		if argCount == 0 { // error is in the self argument itself?
			l.Errorf("calling '%s' on bad self (%s)", d.Name, extraMessage)
			return
		}
	}
	if d.Name == "" {
		if pushGlobalFunctionName(l, f) {
			d.Name, _ = l.ToString(-1)
		} else {
			d.Name = "?"
		}
	}
	l.Errorf("bad argument #%d to '%s' (%s)", argCount, d.Name, extraMessage)
}

func findField(l *State, objectIndex, level int) bool {
	if level == 0 || !l.IsTable(-1) {
		return false
	}
	// Fields of the table itself first, so a global is found by its own
	// name before as a field of _G, whatever order the table visits keys.
	for l.PushNil(); l.Next(-2); l.Pop(1) { // for each pair in table
		if l.IsString(-2) && l.RawEqual(objectIndex, -1) { // found object?
			l.Pop(1) // remove value (but keep name)
			return true
		}
	}
	for l.PushNil(); l.Next(-2); l.Pop(1) {
		if l.IsString(-2) { // ignore non-string keys
			if findField(l, objectIndex, level-1) { // try recursively
				l.Remove(-2) // remove table (but keep name)
				l.PushString(".")
				l.Insert(-2) // place "." between the two names
				l.Concat(3)
				return true
			}
		}
	}
	return false
}

func pushGlobalFunctionName(l *State, f Frame) bool {
	top := l.Top()
	l.Info("f", f) // push function
	l.PushGlobalTable()
	if findField(l, top+1, 2) {
		l.Copy(-1, top+1) // move name to proper place
		l.Pop(2)          // remove pushed values
		return true
	}
	l.SetTop(top) // remove function and global table
	return false
}

func typeError(l *State, argCount int, typeName string) {
	l.ArgumentError(argCount, l.PushFString("%s expected, got %s", typeName, l.TypeName(argCount)))
}

func tagError(l *State, argCount int, tag Type) { typeError(l, argCount, tag.String()) }

// Where pushes onto the stack a string identifying the current position of
// the control at level in the call stack. Typically this string has the
// following format:
//
//	chunkname:currentline:
//
// Level 0 is the running function, level 1 is the function that called the
// running function, etc.
//
// This function is used to build a prefix for error messages.
func (l *State) Where(level int) {
	if f, ok := l.Frame(level); ok { // check function at level
		ar, _ := l.Info("Sl", f) // get info about it
		if ar.CurrentLine > 0 {  // is there info?
			l.PushString(fmt.Sprintf("%s:%d: ", ar.ShortSource, ar.CurrentLine))
			return
		}
	}
	l.PushString("") // else, no information available...
}

// Errorf raises an error. The error message format is given by format plus
// any extra arguments, following the same rules as PushFString. It also adds
// at the beginning of the message the file name and the line number where
// the error occurred, if this information is available.
//
// This function never returns. It is an idiom to use it in Go functions as:
//
//	l.Errorf(format, args...)
//	panic("unreachable")
func (l *State) Errorf(format string, a ...any) {
	l.Where(1)
	l.PushFString(format, a...)
	l.Concat(2)
	l.Error()
}

// ToStringMeta converts any Lua value at the given index to a Go string in a
// reasonable format. The resulting string is pushed onto the stack and also
// returned by the function.
//
// If the value has a metatable with a "__tostring" field, then ToStringMeta
// calls the corresponding metamethod with the value as argument, and uses
// the result of the call as its result.
func (l *State) ToStringMeta(index int) (string, bool) {
	if !l.CallMeta(index, "__tostring") {
		switch l.TypeOf(index) {
		case TypeNumber, TypeString:
			l.PushValue(index)
		case TypeBoolean:
			if l.ToBoolean(index) {
				l.PushString("true")
			} else {
				l.PushString("false")
			}
		case TypeNil:
			l.PushString("nil")
		default:
			l.PushFString("%s: %p", l.TypeName(index), l.ToValue(index))
		}
	}
	return l.ToString(-1)
}

// NewMetaTable returns false if the registry already has the key name. Otherwise,
// creates a new table to be used as a metatable for userdata, adds it to the
// registry with key name, and returns true.
//
// In both cases it pushes onto the stack the final value associated with name in
// the registry.
func (l *State) NewMetaTable(name string) bool {
	if l.MetaTableNamed(name); !l.IsNil(-1) {
		return false
	}
	l.Pop(1)
	l.NewTable()
	l.PushString(name)
	l.SetField(-2, "__name") // as Lua 5.3 on: error messages and tostring use it
	l.PushValue(-1)
	l.SetField(RegistryIndex, name)
	return true
}

func (l *State) MetaTableNamed(name string) {
	l.Field(RegistryIndex, name)
}

func (l *State) SetMetaTableNamed(name string) {
	l.MetaTableNamed(name)
	l.SetMetaTable(-2)
}

func (l *State) TestUserData(index int, name string) any {
	if d := l.ToUserData(index); d != nil {
		if l.MetaTable(index) {
			if l.MetaTableNamed(name); !l.RawEqual(-1, -2) {
				d = nil
			}
			l.Pop(2)
			return d
		}
	}
	return nil
}

// CheckUserData checks whether the function argument at index is a userdata
// of the type name (see NewMetaTable) holding a T, and returns it. It raises
// a Lua error otherwise.
func (l *State) CheckUserData[T any](index int, name string) T {
	if d, ok := l.TestUserData(index, name).(T); ok {
		return d
	}
	typeError(l, index, name)
	panic("unreachable")
}

// CheckType checks whether the function argument at index has type t. See Type for the encoding of types for t.
func (l *State) CheckType(index int, t Type) {
	if l.TypeOf(index) != t {
		tagError(l, index, t)
	}
}

// CheckAny checks whether the function has an argument of any type (including nil) at position index.
func (l *State) CheckAny(index int) {
	if l.TypeOf(index) == TypeNone {
		l.ArgumentError(index, "value expected")
	}
}

// ArgumentCheck checks whether cond is true. If not, raises an error with a standard message.
func (l *State) ArgumentCheck(cond bool, index int, extraMessage string) {
	if !cond {
		l.ArgumentError(index, extraMessage)
	}
}

// CheckString checks whether the function argument at index is a string and returns this string.
//
// This function uses ToString to get its result, so all conversions and caveats of that function apply here.
func (l *State) CheckString(index int) string {
	if s, ok := l.ToString(index); ok {
		return s
	}
	tagError(l, index, TypeString)
	panic("unreachable")
}

// OptString returns the string at index if it is a string. If this argument is
// absent or is nil, returns def. Otherwise, raises an error.
func (l *State) OptString(index int, def string) string {
	if l.IsNoneOrNil(index) {
		return def
	}
	return l.CheckString(index)
}

func (l *State) CheckNumber(index int) float64 {
	n, ok := l.ToNumber(index)
	if !ok {
		tagError(l, index, TypeNumber)
	}
	return n
}

func (l *State) OptNumber(index int, def float64) float64 {
	if l.IsNoneOrNil(index) {
		return def
	}
	return l.CheckNumber(index)
}

// CheckInteger returns the argument at index as an integer, raising an
// error if it is not a number, or has no integer representation.
//
// http://www.lua.org/manual/5.5/manual.html#luaL_checkinteger
func (l *State) CheckInteger(index int) int64 {
	i, ok := l.ToInteger(index)
	if !ok {
		if l.IsNumber(index) {
			l.ArgumentError(index, "number has no integer representation")
		}
		tagError(l, index, TypeNumber)
	}
	return i
}

// OptInteger is CheckInteger, returning def for an absent or nil argument.
func (l *State) OptInteger(index int, def int64) int64 {
	if l.IsNoneOrNil(index) {
		return def
	}
	return l.CheckInteger(index)
}

func (l *State) TypeName(index int) string { return l.TypeOf(index).String() }

func (l *State) SetFunctions(functions []RegistryFunction, upValueCount uint8) {
	uvCount := int(upValueCount)
	l.CheckStackWithMessage(uvCount, "too many upvalues")
	for _, r := range functions { // fill the table with given functions
		for range uvCount { // copy upvalues to the top
			l.PushValue(-uvCount)
		}
		l.PushGoClosure(r.Function, upValueCount) // closure with those upvalues
		l.SetField(-(uvCount + 2), r.Name)
	}
	l.Pop(uvCount) // remove upvalues
}

func (l *State) CheckStackWithMessage(space int, message string) {
	// keep some extra space to run error routines, if needed
	if !l.CheckStack(space + MinStack) {
		if message != "" {
			l.Errorf("stack overflow (%s)", message)
		} else {
			l.Errorf("stack overflow")
		}
	}
}

// CheckOption checks that the argument at index is a string in list, or
// is absent if def, its default, is not "", and returns its position in
// list. Otherwise it raises an error.
//
// http://www.lua.org/manual/5.2/manual.html#luaL_checkoption
func (l *State) CheckOption(index int, def string, list []string) int {
	var name string
	if def != "" {
		name = l.OptString(index, def)
	} else {
		name = l.CheckString(index)
	}
	for i, s := range list {
		if name == s {
			return i
		}
	}
	l.ArgumentError(index, l.PushFString("invalid option '%s'", name))
	panic("unreachable")
}

func (l *State) SubTable(index int, name string) bool {
	l.Field(index, name)
	if l.IsTable(-1) {
		return true // table already there
	}
	l.Pop(1) // remove previous result
	index = l.AbsIndex(index)
	l.NewTable()
	l.PushValue(-1)         // copy to be left at top
	l.SetField(index, name) // assign new table to field
	return false            // did not find table there
}

// Require calls function f with string name as an argument and sets the call
// result in package.loaded[name], as if that function had been called
// through require.
//
// If global is true, also stores the result into global name.
//
// Leaves a copy of that result on the stack.
func (l *State) Require(name string, f Function, global bool) {
	l.PushGoFunction(f)
	l.PushString(name) // argument to f
	l.Call(1, 1)       // open module
	l.SubTable(RegistryIndex, "_LOADED")
	l.PushValue(-2)      // make copy of module (call result)
	l.SetField(-2, name) // _LOADED[name] = module
	l.Pop(1)             // remove _LOADED table
	if global {
		l.PushValue(-1)   // copy of module
		l.SetGlobal(name) // _G[name] = module
	}
}

func (l *State) NewLibraryTable(functions []RegistryFunction) { l.CreateTable(0, len(functions)) }

func (l *State) NewLibrary(functions []RegistryFunction) {
	l.NewLibraryTable(functions)
	l.SetFunctions(functions, 0)
}

func skipComment(r *bufio.Reader) (bool, error) {
	bom := "\xEF\xBB\xBF"
	if ba, err := r.Peek(len(bom)); err != nil && err != io.EOF {
		return false, err
	} else if string(ba) == bom {
		_, _ = r.Read(ba)
	}
	if c, _, err := r.ReadRune(); err != nil {
		if err == io.EOF {
			err = nil
		}
		return false, err
	} else if c == '#' {
		_, err = r.ReadBytes('\n')
		if err == io.EOF {
			err = nil
		}
		return true, err
	}
	return false, r.UnreadRune()
}

func (l *State) LoadFile(fileName, mode string) error {
	var f *os.File
	fileNameIndex := l.Top() + 1
	fileError := func(what string, err error) error {
		fileName, _ := l.ToString(fileNameIndex)
		msg, _ := errorText(err)
		l.PushFString("cannot %s %s: %s", what, fileName[1:], msg)
		l.Remove(fileNameIndex)
		return ErrFile
	}
	if fileName == "" {
		l.PushString("=stdin")
		f = os.Stdin
	} else {
		l.PushString("@" + fileName)
		var err error
		if f, err = os.Open(fileName); err != nil {
			return fileError("open", err)
		}
	}
	r := bufio.NewReader(f)
	if skipped, err := skipComment(r); err != nil {
		l.SetTop(fileNameIndex)
		return fileError("read", err)
	} else if skipped {
		// A text chunk keeps its line numbers by reading the comment's
		// line as an empty one; a binary chunk starts right after it.
		if c, err := r.Peek(1); err != nil || c[0] != Signature[0] {
			r = bufio.NewReader(io.MultiReader(strings.NewReader("\n"), r))
		}
	}
	s, _ := l.ToString(-1)
	err := l.Load(r, s, mode)
	if f != os.Stdin {
		_ = f.Close()
	}
	switch err {
	case nil, ErrSyntax, ErrMemory: // do nothing
	default:
		l.SetTop(fileNameIndex)
		return fileError("read", err)
	}
	l.Remove(fileNameIndex)
	return err
}

func (l *State) LoadString(s string) error { return l.LoadBuffer(s, s, "") }

func (l *State) LoadBuffer(b, name, mode string) error {
	return l.Load(strings.NewReader(b), name, mode)
}

// Len returns the length of the value at index as an integer, as the #
// operator computes it, metamethods included.
//
// http://www.lua.org/manual/5.5/manual.html#luaL_len
func (l *State) Len(index int) int64 {
	l.Length(index)
	if length, ok := l.ToInteger(-1); ok {
		l.Pop(1)
		return length
	}
	l.Errorf("object length is not an integer")
	panic("unreachable")
}

// errorText returns err as C's strerror words its errno, and the errno;
// or, for an error without one, its own text and 0.
func errorText(err error) (string, int) {
	var e syscall.Errno
	if !errors.As(err, &e) {
		return err.Error(), 0
	}
	msg := e.Error()
	return strings.ToUpper(msg[:1]) + msg[1:], int(e)
}

// FileResult produces the return values for file-related functions in the standard
// library (io.open, os.rename, file:seek, etc.).
func (l *State) FileResult(err error, filename string) int {
	if err == nil {
		l.PushBoolean(true)
		return 1
	}
	msg, errno := errorText(err)
	if filename != "" {
		msg = filename + ": " + msg
	}
	l.PushNil()
	l.PushString(msg)
	l.PushInteger(errno)
	return 3
}

// DoFile loads and runs the given file.
func (l *State) DoFile(fileName string) error {
	if err := l.LoadFile(fileName, ""); err != nil {
		return err
	}
	return l.ProtectedCall(0, MultipleReturns, 0)
}

// DoString loads and runs the given string.
func (l *State) DoString(s string) error {
	if err := l.LoadString(s); err != nil {
		return err
	}
	return l.ProtectedCall(0, MultipleReturns, 0)
}
