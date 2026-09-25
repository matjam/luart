package luart

import (
	"encoding/binary"
	"fmt"
	"io"
)

type dumpState struct {
	l     *State
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

func (d *dumpState) writePC(p pc) {
	d.writeInt(int(p))
}

func (d *dumpState) writeCode(p *prototype) {
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

func (d *dumpState) writeConstants(p *prototype) {
	d.writeInt(len(p.Constants))

	for _, o := range p.Constants {
		d.writeByte(byte(d.l.valueToType(o)))

		switch o := o.toAny().(type) {
		case nil:
		case bool:
			d.writeBool(o)
		case float64:
			d.writeNumber(o)
		case string:
			d.writeString(o)
		default:
			d.l.assert(false)
		}
	}
}

func (d *dumpState) writePrototypes(p *prototype) {
	d.writeInt(len(p.Prototypes))

	for _, o := range p.Prototypes {
		d.dumpFunction(&o)
	}
}

func (d *dumpState) writeUpvalues(p *prototype) {
	d.writeInt(len(p.UpValues))

	for _, u := range p.UpValues {
		d.writeBool(u.IsLocal)
		d.writeByte(byte(u.Index))
	}
}

func (d *dumpState) writeString(s string) {
	ba := []byte(s)
	size := len(s)
	if size > 0 {
		size++ //accounts for 0 byte at the end
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
		d.write(ba)
		d.writeByte(0)
	}
}

func (d *dumpState) writeLocalVariables(p *prototype) {
	d.writeInt(len(p.LocalVariables))

	for _, lv := range p.LocalVariables {
		d.writeString(lv.Name)
		d.writePC(lv.StartPC)
		d.writePC(lv.EndPC)
	}
}

func (d *dumpState) writeDebug(p *prototype) {
	d.writeString(p.Source)
	d.writeInt(len(p.LineInfo))
	d.write(p.LineInfo)
	d.writeLocalVariables(p)

	d.writeInt(len(p.UpValues))

	for _, uv := range p.UpValues {
		d.writeString(uv.Name)
	}
}

func (d *dumpState) dumpFunction(p *prototype) {
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

func (d *dumpState) dumpHeader() {
	d.err = binary.Write(d.out, d.order, header)
}

func (l *State) dump(p *prototype, w io.Writer) error {
	d := dumpState{l: l, out: w, order: endianness()}
	d.dumpHeader()
	d.dumpFunction(p)

	return d.err
}
