package lua

import (
	"fmt"
	"unsafe"
)

// Buffers give Lua direct access to a Go slice of numbers, as LuaJIT's FFI
// gives it C arrays: a pixel canvas, audio samples, vertex data. The
// slice's memory is shared, so Lua sees what Go writes to it and Go sees
// what Lua writes, without a call into Go, and compiled code reads and
// writes the elements inline.
//
// A buffer is a userdata without a metatable. buf[i] is element i, counting
// from 0 as Go and LuaJIT's FFI arrays do, and #buf is the slice's length.
// Reading outside the slice, or with a key that is not an integer, gives
// nil, as a table does, so ipairs stops at the end; writing there raises an
// error, as the slice cannot grow. A float buffer stores any number, a
// float32 buffer rounding it; an integer buffer stores integers, wrapping
// them to its width as Go's conversions do, and floats with an integer
// value.

// BufferElement is the element type of a buffer.
type BufferElement interface {
	float64 | float32 | int32 | uint8
}

type bufferKind uint8

const (
	bufferFloat64 bufferKind = iota
	bufferFloat32
	bufferInt32
	bufferUint8
)

// buffer is a buffer's memory, which compiled code reads at fixed offsets.
type buffer struct {
	ptr  unsafe.Pointer // the first element, which keeps the slice's memory alive
	len  int
	kind bufferKind
}

var bufferBytes = int(unsafe.Sizeof(buffer{}))

func bufferKindOf[T BufferElement]() bufferKind {
	var zero T
	switch any(zero).(type) {
	case float32:
		return bufferFloat32
	case int32:
		return bufferInt32
	case uint8:
		return bufferUint8
	}
	return bufferFloat64
}

// PushBuffer pushes a buffer onto the stack: a userdata through which Lua
// reads and writes the elements of s, sharing its memory. See buffer.go for
// how it behaves.
//
//	canvas := make([]float64, width*height)
//	l.PushBuffer(canvas)
//	l.SetGlobal("canvas") // Lua: canvas[y*width + x] = v
func (l *State) PushBuffer[T BufferElement](s []T) {
	l.charge(userDataBytes + bufferBytes)
	b := &buffer{ptr: unsafe.Pointer(unsafe.SliceData(s)), len: len(s), kind: bufferKindOf[T]()}
	l.apiPush(objectValue(&userData{data: s, buf: b}))
}

// ToBuffer returns the slice of the buffer at index, if the value there is
// a buffer of elements of type T.
func (l *State) ToBuffer[T BufferElement](index int) ([]T, bool) {
	u := l.indexToValue(index).userData()
	if u == nil || u.buf == nil {
		return nil, false
	}
	s, ok := u.data.([]T)
	return s, ok
}

// element returns the address of element i, which must be in range.
func (b *buffer) element(i int64) unsafe.Pointer {
	size := [...]int64{bufferFloat64: 8, bufferFloat32: 4, bufferInt32: 4, bufferUint8: 1}[b.kind]
	return unsafe.Add(b.ptr, i*size)
}

// at returns b[key]: an element, or nil for a key that is not an integer
// or is out of range.
func (b *buffer) at(key value) value {
	i, ok := key.integer()
	if !ok || uint64(i) >= uint64(b.len) {
		return nilValue
	}
	p := b.element(i)
	switch b.kind {
	case bufferFloat32:
		return numberValue(float64(*(*float32)(p)))
	case bufferInt32:
		return integerValue(int64(*(*int32)(p)))
	case bufferUint8:
		return integerValue(int64(*(*uint8)(p)))
	}
	return numberValue(*(*float64)(p))
}

// putBuffer stores v at b[key], raising an error for a key out of range or
// a value that does not fit.
func (l *State) putBuffer(b *buffer, key, v value) {
	i, ok := key.integer()
	if !ok {
		l.runtimeError(fmt.Sprintf("buffer index is a %s, not an integer", objectTypeName(key)))
	}
	if uint64(i) >= uint64(b.len) {
		l.runtimeError(fmt.Sprintf("buffer index %d out of range [0, %d)", i, b.len))
	}
	if !v.isNumber() {
		l.runtimeError(fmt.Sprintf("attempt to store a %s value in a buffer", objectTypeName(v)))
	}
	p := b.element(i)
	switch b.kind {
	case bufferFloat64:
		*(*float64)(p) = v.toFloat()
	case bufferFloat32:
		*(*float32)(p) = float32(v.toFloat())
	default:
		n, ok := v.integer()
		if !ok {
			l.runtimeError("number has no integer representation")
		}
		if b.kind == bufferInt32 {
			*(*int32)(p) = int32(n)
		} else {
			*(*uint8)(p) = uint8(n)
		}
	}
}
