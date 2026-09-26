//go:build (darwin || linux) && (arm64 || amd64)

package execmem_test

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/matjam/apogee/internal/jit/call"
	"github.com/matjam/apogee/internal/jit/execmem"
)

// returnArgPlus41 is code that returns its ctx argument plus 41.
func returnArgPlus41() []byte {
	switch runtime.GOARCH {
	case "arm64":
		var b []byte
		for _, w := range []uint32{
			0x9100a400, // ADD X0, X0, #41
			0xd65f03c0, // RET
		} {
			b = binary.LittleEndian.AppendUint32(b, w)
		}
		return b
	case "amd64":
		return []byte{
			0x48, 0x8d, 0x47, 0x29, // LEA RAX, [RDI+41]
			0xc3, // RET
		}
	}
	return nil
}

func TestLoadAndCall(t *testing.T) {
	code, err := execmem.Load(returnArgPlus41())
	if err != nil {
		t.Fatal(err)
	}
	var buf [3]byte
	for i := range buf {
		p := unsafe.Pointer(&buf[i])
		if got, want := call.Call(code.Addr(0), p), uint64(uintptr(p))+41; got != want {
			t.Fatalf("Call(%p) = %#x, want %#x", p, got, want)
		}
	}
	runtime.KeepAlive(code)
}

func TestManyMappingsAreReleased(t *testing.T) {
	for range 2000 {
		if _, err := execmem.Load(returnArgPlus41()); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.GC()
}
