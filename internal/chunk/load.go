package chunk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/matjam/luart/internal/bytecode"
)

var (
	errTruncated           = errors.New("truncated precompiled chunk")
	errNotPrecompiledChunk = errors.New("not a precompiled chunk")
	errVersionMismatch     = errors.New("version mismatch in precompiled chunk")
	errIncompatible        = errors.New("incompatible precompiled chunk")
	errCorrupted           = errors.New("corrupted precompiled chunk")
)

// batchSize bounds each allocation made for a count read from the chunk,
// so a corrupted count fails on the missing data instead of allocating
// for it.
const batchSize = 4096

// Load reads a binary chunk from r and returns its main function. name is
// the chunk's name, for error messages.
//
// Load checks the chunk's structure, not its code: like Lua's, it trusts
// the instructions of a well-formed chunk.
func Load(r io.Reader, name string) (*bytecode.Proto, error) {
	s := &loadState{in: r, order: endianness()}
	err := s.checkHeader()
	var p *bytecode.Proto
	if err == nil {
		if p, err = s.readFunction(); err == nil {
			s.inheritSources(p, "=?")
		}
	}
	if err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			err = errTruncated
		}
		return nil, fmt.Errorf("%s: %w", displayName(name), err)
	}
	return p, nil
}

// displayName is how errors name a chunk: its source without the '@' or
// '=' mark, or "binary string" when the chunk is its own name.
func displayName(name string) string {
	switch {
	case name == "":
		return "?"
	case name[0] == '@' || name[0] == '=':
		return name[1:]
	case name[0] == Signature[0]:
		return "binary string"
	}
	return name
}

type loadState struct {
	in       io.Reader
	order    binary.ByteOrder
	absent   bool                     // the last string read was absent, not empty
	strings  []string                 // strings read, which later ones may refer to
	noSource map[*bytecode.Proto]bool // functions whose source was absent: their parent's, or "=?"

}

func (state *loadState) read(data any) error {
	return binary.Read(state.in, state.order, data)
}

func (state *loadState) readNumber() (f float64, err error) {
	err = state.read(&f)
	return
}

func (state *loadState) readInt() (i int32, err error) {
	err = state.read(&i)
	return
}

// readCount reads the length of a list.
func (state *loadState) readCount() (int, error) {
	n, err := state.readInt()
	if err == nil && n < 0 {
		err = errCorrupted
	}
	return int(n), err
}

func (state *loadState) readByte() (b byte, err error) {
	err = state.read(&b)
	return
}

func (state *loadState) readBool() (bool, error) {
	b, err := state.readByte()
	return b != 0, err
}

func (state *loadState) readString() (string, error) {
	var size uint64
	switch header.PointerSize {
	case 8:
		if err := state.read(&size); err != nil {
			return "", err
		}
	case 4:
		var size32 uint32
		if err := state.read(&size32); err != nil {
			return "", err
		}
		size = uint64(size32)
	default:
		panic(fmt.Sprintf("unsupported pointer size (%d)", header.PointerSize))
	}
	if state.absent = size == 0; state.absent { // no string: C's NULL
		return "", nil
	}
	if size&reuseBit() != 0 { // a string read before, by index
		i := size &^ reuseBit()
		if i >= uint64(len(state.strings)) {
			return "", errCorrupted
		}
		return state.strings[i], nil
	}
	if size > 1<<62 {
		return "", errCorrupted
	}
	// Copying grows the buffer only as the data arrives.
	var b bytes.Buffer
	if _, err := io.CopyN(&b, state.in, int64(size)); err != nil {
		return "", err
	}
	s := string(b.Bytes()[:size-1]) // without the 0 byte at the end
	state.strings = append(state.strings, s)
	return s, nil
}

// readList reads a count and that many fixed-size elements.
func readList[T any](state *loadState) ([]T, error) {
	n, err := state.readCount()
	if err != nil || n == 0 {
		return nil, err
	}
	list := make([]T, 0, min(n, batchSize))
	for len(list) < n {
		k := min(n-len(list), batchSize)
		list = slices.Grow(list, k)[:len(list)+k]
		if err := state.read(list[len(list)-k:]); err != nil {
			return nil, err
		}
	}
	return list, nil
}

func (state *loadState) readUpValues() ([]bytecode.UpValueDesc, error) {
	v, err := readList[struct{ IsLocal, Index byte }](state)
	if err != nil || len(v) == 0 {
		return nil, err
	}
	u := make([]bytecode.UpValueDesc, len(v))
	for i := range v {
		u[i].IsLocal, u[i].Index = v[i].IsLocal != 0, int(v[i].Index)
	}
	return u, nil
}

func (state *loadState) readLocalVariables() (localVariables []bytecode.LocalVariable, err error) {
	var n int
	if n, err = state.readCount(); err != nil || n == 0 {
		return
	}
	localVariables = make([]bytecode.LocalVariable, 0, min(n, batchSize))
	for range n {
		var lv bytecode.LocalVariable
		var start, end int32
		if lv.Name, err = state.readString(); err != nil {
			return
		}
		if start, err = state.readInt(); err != nil {
			return
		}
		if end, err = state.readInt(); err != nil {
			return
		}
		lv.StartPC, lv.EndPC = int(start), int(end)
		localVariables = append(localVariables, lv)
	}
	return
}

// readDebug reads p's debug information: its source, line numbers, local
// variables and upvalue names.
func (state *loadState) readDebug(p *bytecode.Proto) (err error) {
	if p.Source, err = state.readString(); err != nil {
		return
	} else if p.Source == "" && state.absent {
		if state.noSource == nil {
			state.noSource = map[*bytecode.Proto]bool{}
		}
		state.noSource[p] = true // filled in by inheritSources
	}
	if p.LineInfo, err = readList[int32](state); err != nil {
		return
	}
	if p.LocalVariables, err = state.readLocalVariables(); err != nil {
		return
	}
	var n int
	if n, err = state.readCount(); err != nil {
		return
	}
	if n > len(p.UpValues) {
		return errCorrupted
	}
	for i := range n {
		if p.UpValues[i].Name, err = state.readString(); err != nil {
			return
		}
	}
	return
}

func (state *loadState) readConstants() (constants []any, err error) {
	var n int
	if n, err = state.readCount(); err != nil || n == 0 {
		return
	}
	constants = make([]any, 0, min(n, batchSize))
	for range n {
		var t byte
		var k any
		switch t, err = state.readByte(); {
		case err != nil:
		case t == tagNil:
		case t == tagBoolean:
			k, err = state.readBool()
		case t == tagNumber:
			k, err = state.readNumber()
		case t == tagInteger:
			var i int64
			err = state.read(&i)
			k = i
		case t == tagString:
			k, err = state.readString()
		default:
			err = errCorrupted
		}
		if err != nil {
			return
		}
		constants = append(constants, k)
	}
	return
}

func (state *loadState) readPrototypes() (prototypes []*bytecode.Proto, err error) {
	var n int
	if n, err = state.readCount(); err != nil || n == 0 {
		return
	}
	prototypes = make([]*bytecode.Proto, 0, min(n, batchSize))
	for range n {
		var p *bytecode.Proto
		if p, err = state.readFunction(); err != nil {
			return
		}
		prototypes = append(prototypes, p)
	}
	return
}

func (state *loadState) readFunction() (p *bytecode.Proto, err error) {
	p = new(bytecode.Proto)
	var n int32
	if n, err = state.readInt(); err != nil {
		return
	}
	p.LineDefined = int(n)
	if n, err = state.readInt(); err != nil {
		return
	}
	p.LastLineDefined = int(n)
	var b byte
	if b, err = state.readByte(); err != nil {
		return
	}
	p.ParameterCount = int(b)
	if b, err = state.readByte(); err != nil {
		return
	}
	p.IsVarArg, p.VarArgKind = b&1 != 0, bytecode.VarArgKind(b>>1)
	if b, err = state.readByte(); err != nil {
		return
	}
	p.MaxStackSize = int(b)
	if p.Code, err = readList[bytecode.Instruction](state); err != nil {
		return
	}
	if p.Constants, err = state.readConstants(); err != nil {
		return
	}
	if p.Prototypes, err = state.readPrototypes(); err != nil {
		return
	}
	if p.UpValues, err = state.readUpValues(); err != nil {
		return
	}
	err = state.readDebug(p)
	return
}

// inheritSources gives each function loaded without a source its
// parent's, or "=?" (stripped, as lua_getinfo names a NULL source) for
// the main function, as lundump.c's loadFunction does. A function's
// source comes after its nested functions', so this runs once all are
// read.
func (state *loadState) inheritSources(p *bytecode.Proto, parent string) {
	if state.noSource[p] {
		p.Source = parent
	}
	for _, c := range p.Prototypes {
		state.inheritSources(c, p.Source)
	}
}

func (state *loadState) checkHeader() error {
	h := header
	if err := state.read(&h); err != nil {
		return err
	} else if h == header {
		return nil
	} else if string(h.Signature[:]) != Signature {
		return errNotPrecompiledChunk
	} else if h.Version != header.Version || h.Format != header.Format {
		return errVersionMismatch
	} else if h.Tail != header.Tail {
		return errCorrupted
	}
	return errIncompatible
}
