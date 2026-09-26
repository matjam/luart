package chunk

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/matjam/luart/internal/bytecode"
)

// Dump writes p, and the functions nested in it, to w as a binary chunk.
// With strip set it leaves out the debug information: sources, line
// numbers, and local and upvalue names.
func Dump(w io.Writer, p *bytecode.Proto, strip bool) error {
	d := dumpState{out: w, order: endianness(), strip: strip}
	d.write(header)
	d.dumpFunction(p, nil)
	return d.err
}

type dumpState struct {
	out     io.Writer
	order   binary.ByteOrder
	err     error
	strip   bool
	strings map[string]int // strings written, by index
}

func (d *dumpState) write(data any) {
	if d.err == nil {
		d.err = binary.Write(d.out, d.order, data)
	}
}

func (d *dumpState) writeInt(i int) {
	d.write(int32(i))
}

func (d *dumpState) writeCode(p *bytecode.Proto) {
	d.writeInt(len(p.Code))
	d.write(p.Code)
}

func (d *dumpState) writeByte(b byte) {
	d.write(b)
}

func (d *dumpState) writeBool(b bool) {
	if b {
		d.writeByte(1)
	} else {
		d.writeByte(0)
	}
}

func (d *dumpState) writeNumber(f float64) {
	d.write(f)
}

func (d *dumpState) writeConstants(p *bytecode.Proto) {
	d.writeInt(len(p.Constants))

	for _, k := range p.Constants {
		switch k := k.(type) {
		case nil:
			d.writeByte(tagNil)
		case bool:
			d.writeByte(tagBoolean)
			d.writeBool(k)
		case float64:
			d.writeByte(tagNumber)
			d.writeNumber(k)
		case int64:
			d.writeByte(tagInteger)
			d.write(k)
		case string:
			d.writeByte(tagString)
			d.writeString(k)
		default:
			panic(fmt.Sprintf("constant of type %T", k))
		}
	}
}

func (d *dumpState) writePrototypes(p *bytecode.Proto) {
	d.writeInt(len(p.Prototypes))

	for _, o := range p.Prototypes {
		d.dumpFunction(o, &p.Source)
	}
}

func (d *dumpState) writeUpvalues(p *bytecode.Proto) {
	d.writeInt(len(p.UpValues))

	for _, u := range p.UpValues {
		d.writeBool(u.IsLocal)
		d.writeByte(byte(u.Index))
	}
}

// writeSize writes a string's size field, pointer-sized.
func (d *dumpState) writeSize(size uint64) {
	switch header.PointerSize {
	case 8:
		d.write(size)
	case 4:
		d.write(uint32(size))
	default:
		panic(fmt.Sprintf("unsupported pointer size (%d)", header.PointerSize))
	}
}

// reuseBit marks a size field that refers to a string already written,
// by its index, as Lua 5.5's dumps reuse strings.
func reuseBit() uint64 { return 1 << (8*uint(header.PointerSize) - 1) }

// writeAbsent writes a missing string, which loads as C's NULL.
func (d *dumpState) writeAbsent() { d.writeSize(0) }

// writeString writes s with its size, counting the 0 byte after it, as C
// Lua does; "" too, as size 0 means no string at all. A string written
// before is its index instead, with reuseBit set.
func (d *dumpState) writeString(s string) {
	if i, ok := d.strings[s]; ok {
		d.writeSize(reuseBit() | uint64(i))
		return
	}
	if d.strings == nil {
		d.strings = map[string]int{}
	}
	d.strings[s] = len(d.strings)
	d.writeSize(uint64(len(s) + 1))
	d.write([]byte(s))
	d.writeByte(0)
}

func (d *dumpState) writeLocalVariables(p *bytecode.Proto) {
	d.writeInt(len(p.LocalVariables))

	for _, lv := range p.LocalVariables {
		d.writeString(lv.Name)
		d.writeInt(lv.StartPC)
		d.writeInt(lv.EndPC)
	}
}

func (d *dumpState) writeDebug(p *bytecode.Proto, parentSource *string) {
	if d.strip { // no source (C's NULL), and no lines, locals or upvalue names
		d.writeAbsent()
		d.writeInt(0)
		d.writeInt(0)
		d.writeInt(0)
		return
	}
	if parentSource != nil && p.Source == *parentSource { // a nested function's: the loader takes its parent's
		d.writeAbsent()
	} else {
		d.writeString(p.Source)
	}
	d.writeInt(len(p.LineInfo))
	d.write(p.LineInfo)
	d.writeLocalVariables(p)

	d.writeInt(len(p.UpValues))

	for _, uv := range p.UpValues {
		d.writeString(uv.Name)
	}
}

func varArgByte(p *bytecode.Proto) byte {
	b := byte(p.VarArgKind) << 1
	if p.IsVarArg {
		b |= 1
	}
	return b
}

func (d *dumpState) dumpFunction(p *bytecode.Proto, parentSource *string) { // nil for the main function
	d.writeInt(p.LineDefined)
	d.writeInt(p.LastLineDefined)
	d.writeByte(byte(p.ParameterCount))
	d.writeByte(varArgByte(p)) // bit 0: vararg; above it, the named vararg table's kind
	d.writeByte(byte(p.MaxStackSize))
	d.writeCode(p)
	d.writeConstants(p)
	d.writePrototypes(p)
	d.writeUpvalues(p)
	d.writeDebug(p, parentSource)
}
