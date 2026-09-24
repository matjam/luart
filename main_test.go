package lua

import (
	"os"
	"testing"
)

// With LUART_JIT_TEST=1 every state compiles every function it runs, so
// the whole suite checks compiled code against the interpreter's results.
func TestMain(m *testing.M) {
	if os.Getenv("LUART_JIT_TEST") == "1" {
		jitDefault, jitThreshold = true, 0
	}
	os.Exit(m.Run())
}
