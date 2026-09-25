package chunk

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/matjam/luart/internal/bytecode"
)

// Dump writes p, and the functions nested in it, to w as a binary chunk.
func Dump(w io.Writer, p *bytecode.Proto) error {
	d := dumpState{out: w, order: endianness()}
	d.write(header)
	d.dumpFunction(p)
	return d.err
}

type dumpState struct {
	out   io.Writer
	order binary.ByteOrder
	err   error
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
		d.dumpFunction(o)
	}
}

func (d *dumpState) writeUpvalues(p *bytecode.Proto) {
	d.writeInt(len(p.UpValues))

	for _, u := range p.UpValues {
		d.writeBool(u.IsLocal)
		d.writeByte(byte(u.Index))
	}
}

func (d *dumpState) writeString(s string) {
	size := len(s)
	if size > 0 {
		size++ // accounts for the 0 byte at the end
	}
	switch header.PointerSize {
	case 8:
		d.write(uint64(size))
	case 4:
		d.write(uint32(size))
	default:
		panic(fmt.Sprintf("unsupported pointer size (%d)", header.PointerSize))
	}
	if size > 0 {
		d.write([]byte(s))
		d.writeByte(0)
	}
}

func (d *dumpState) writeLocalVariables(p *bytecode.Proto) {
	d.writeInt(len(p.LocalVariables))

	for _, lv := range p.LocalVariables {
		d.writeString(lv.Name)
		d.writeInt(lv.StartPC)
		d.writeInt(lv.EndPC)
	}
}

func (d *dumpState) writeDebug(p *bytecode.Proto) {
	d.writeString(p.Source)
	d.writeInt(len(p.LineInfo))
	d.write(p.LineInfo)
	d.writeLocalVariables(p)

	d.writeInt(len(p.UpValues))

	for _, uv := range p.UpValues {
		d.writeString(uv.Name)
	}
}

func (d *dumpState) dumpFunction(p *bytecode.Proto) {
	d.writeInt(p.LineDefined)
	d.writeInt(p.LastLineDefined)
	d.writeByte(byte(p.ParameterCount))
	d.writeBool(p.IsVarArg)
	d.writeByte(byte(p.MaxStackSize))
	d.writeCode(p)
	d.writeConstants(p)
	d.writePrototypes(p)
	d.writeUpvalues(p)
	d.writeDebug(p)
}
