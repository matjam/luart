//go:build (darwin || linux) && arm64

package call_test

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/matjam/apogee/internal/jit/call"
	"github.com/matjam/apogee/internal/jit/execmem"
)

func BenchmarkCallRet(b *testing.B) {
	code := binary.LittleEndian.AppendUint32(nil, 0xd65f03c0) // ret
	mem, err := execmem.Load(code)
	if err != nil {
		b.Fatal(err)
	}
	var x uint64
	for b.Loop() {
		call.Call(mem.Addr(0), unsafe.Pointer(&x))
	}
}
