package luart

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/matjam/luart/internal/bytecode"
)

type loadState struct {
	in    io.Reader
	order binary.ByteOrder
}

var header struct {
	Signature                            [4]byte
	Version, Format, Endianness, IntSize byte
	PointerSize, InstructionSize         byte
	NumberSize, IntegralNumber           byte
	Tail                                 [6]byte
}

var (
	errUnknownConstantType = errors.New("lua: unknown constant type in lua binary")
	errNotPrecompiledChunk = errors.New("lua: is not a precompiled chunk")
	errVersionMismatch     = errors.New("lua: version mismatch in precompiled chunk")
	errIncompatible        = errors.New("lua: incompatible precompiled chunk")
	errCorrupted           = errors.New("lua: corrupted precompiled chunk")
)

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

func (state *loadState) readPC() (pc, error) {
	i, err := state.readInt()
	return pc(i), err
}

func (state *loadState) readByte() (b byte, err error) {
	err = state.read(&b)
	return
}

func (state *loadState) readBool() (bool, error) {
	b, err := state.readByte()
	return b != 0, err
}

func (state *loadState) readString() (s string, err error) {
	// Feel my pain
	maxUint := ^uint(0)
	var size uintptr
	var size64 uint64
	var size32 uint32
	if uint64(maxUint) == math.MaxUint64 {
		err = state.read(&size64)
		size = uintptr(size64)
	} else if maxUint == math.MaxUint32 {
		err = state.read(&size32)
		size = uintptr(size32)
	} else {
		panic(fmt.Sprintf("unsupported pointer size (%d)", maxUint))
	}
	if err != nil || size == 0 {
		return
	}
	ba := make([]byte, size)
	if err = state.read(ba); err == nil {
		s = string(ba[:len(ba)-1])
	}
	return
}

func (state *loadState) readCode() (code []bytecode.Instruction, err error) {
	n, err := state.readInt()
	if err != nil || n == 0 {
		return
	}
	code = make([]bytecode.Instruction, n)
	err = state.read(code)
	return
}

func (state *loadState) readUpValues() (u []bytecode.UpValueDesc, err error) {
	n, err := state.readInt()
	if err != nil || n == 0 {
		return
	}
	v := make([]struct{ IsLocal, Index byte }, n)
	err = state.read(v)
	if err != nil {
		return
	}
	u = make([]bytecode.UpValueDesc, n)
	for i := range v {
		u[i].IsLocal, u[i].Index = v[i].IsLocal != 0, int(v[i].Index)
	}
	return
}

func (state *loadState) readLocalVariables() (localVariables []bytecode.LocalVariable, err error) {
	var n int32
	if n, err = state.readInt(); err != nil || n == 0 {
		return
	}
	localVariables = make([]bytecode.LocalVariable, n)
	for i := range localVariables {
		if localVariables[i].Name, err = state.readString(); err != nil {
			return
		}
		var start, end pc
		if start, err = state.readPC(); err != nil {
			return
		}
		if end, err = state.readPC(); err != nil {
			return
		}
		localVariables[i].StartPC, localVariables[i].EndPC = int(start), int(end)
	}
	return
}

func (state *loadState) readLineInfo() (lineInfo []int32, err error) {
	var n int32
	if n, err = state.readInt(); err != nil || n == 0 {
		return
	}
	lineInfo = make([]int32, n)
	err = state.read(lineInfo)
	return
}

func (state *loadState) readDebug(p *prototype) (source string, lineInfo []int32, localVariables []bytecode.LocalVariable, names []string, err error) {
	var n int32
	if source, err = state.readString(); err != nil {
		return
	}
	if lineInfo, err = state.readLineInfo(); err != nil {
		return
	}
	if localVariables, err = state.readLocalVariables(); err != nil {
		return
	}
	if n, err = state.readInt(); err != nil {
		return
	}
	names = make([]string, n)
	for i := range names {
		if names[i], err = state.readString(); err != nil {
			return
		}
	}
	return
}

func (state *loadState) readConstants() (constants []value, prototypes []prototype, err error) {
	var n int32
	if n, err = state.readInt(); err != nil || n == 0 {
		return
	}

	constants = make([]value, n)
	for i := range constants {
		var t byte
		switch t, err = state.readByte(); {
		case err != nil:
			return
		case t == byte(TypeNil):
			constants[i] = nilValue
		case t == byte(TypeBoolean):
			var b bool
			b, err = state.readBool()
			constants[i] = boolValue(b)
		case t == byte(TypeNumber):
			var n float64
			n, err = state.readNumber()
			constants[i] = numberValue(n)
		case t == byte(TypeString):
			var s string
			s, err = state.readString()
			constants[i] = stringValue(s)
		default:
			err = errUnknownConstantType
		}
		if err != nil {
			return
		}
	}
	return
}

func (state *loadState) readPrototypes() (prototypes []prototype, err error) {
	var n int32
	if n, err = state.readInt(); err != nil || n == 0 {
		return
	}
	prototypes = make([]prototype, n)
	for i := range prototypes {
		if prototypes[i], err = state.readFunction(); err != nil {
			return
		}
	}
	return
}

func (state *loadState) readFunction() (p prototype, err error) {
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
	p.IsVarArg = b != 0
	if b, err = state.readByte(); err != nil {
		return
	}
	p.MaxStackSize = int(b)
	if p.Code, err = state.readCode(); err != nil {
		return
	}
	if p.Constants, p.Prototypes, err = state.readConstants(); err != nil {
		return
	}
	if p.Prototypes, err = state.readPrototypes(); err != nil {
		return
	}
	if p.UpValues, err = state.readUpValues(); err != nil {
		return
	}
	var names []string
	if p.Source, p.LineInfo, p.LocalVariables, names, err = state.readDebug(&p); err != nil {
		return
	}
	for i, name := range names {
		p.UpValues[i].Name = name
	}
	return
}

func init() {
	copy(header.Signature[:], Signature)
	header.Version = VersionMajor<<4 | VersionMinor
	header.Format = 0
	if endianness() == binary.LittleEndian {
		header.Endianness = 1
	} else {
		header.Endianness = 0
	}
	header.IntSize = 4
	header.PointerSize = byte(1+^uintptr(0)>>32&1) * 4
	header.InstructionSize = byte(1+^bytecode.Instruction(0)>>32&1) * 4
	header.NumberSize = 8
	header.IntegralNumber = 0
	tail := "\x19\x93\r\n\x1a\n"
	copy(header.Tail[:], tail)

	// The uintptr numeric type is implementation-specific
	uintptrBitCount := byte(0)
	for bits := ^uintptr(0); bits != 0; bits >>= 1 {
		uintptrBitCount++
	}
	if uintptrBitCount != header.PointerSize*8 {
		panic(fmt.Sprintf("invalid pointer size (%d)", uintptrBitCount))
	}
}

func endianness() binary.ByteOrder {
	if x := 1; *(*byte)(unsafe.Pointer(&x)) == 1 {
		return binary.LittleEndian
	}
	return binary.BigEndian
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

func (l *State) undump(in io.Reader, name string) (c *luaClosure, err error) {
	if name[0] == '@' || name[0] == '=' {
		name = name[1:]
	} else if name[0] == Signature[0] {
		name = "binary string"
	}
	// TODO assign name to p.source?
	s := &loadState{in, endianness()}
	var p prototype
	if err = s.checkHeader(); err != nil {
		return
	} else if p, err = s.readFunction(); err != nil {
		return
	}
	c = l.newLuaClosure(&p)
	l.push(objectValue(c))
	return
}
