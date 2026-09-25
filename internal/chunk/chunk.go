// Package chunk reads and writes luart's binary chunks, the precompiled
// functions string.dump returns. The format is Lua 5.2's with integer
// constants added, marked as version 0x55 and format 2 (luart's). C Lua's
// chunks, of any version, do not load: luart runs its own instruction set.
package chunk

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/matjam/luart/internal/bytecode"
)

// Signature is the mark that starts a binary chunk ('<esc>Lua').
const Signature = "\033Lua"

// The type tags of constants. A float is 5.2's number; an integer has
// Lua 5.4's tag for integers, a variant of number.
const (
	tagNil     = 0
	tagBoolean = 1
	tagNumber  = 3
	tagString  = 4
	tagInteger = 0x13
)

// header is the header of the chunks this platform reads and writes.
var header struct {
	Signature                            [4]byte
	Version, Format, Endianness, IntSize byte
	PointerSize, InstructionSize         byte
	NumberSize, IntegralNumber           byte
	Tail                                 [6]byte
}

func init() {
	copy(header.Signature[:], Signature)
	header.Version = 0x55
	header.Format = 2
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
