package lua

import (
	"os"
	"testing"
)

// With LUART_JIT_TEST=1 every state compiles every function it runs, so
// the whole suite checks compiled code against the interpreter's results.
// Without it states compile hot functions, as they do by default, and
// LUART_JIT=off runs the suite interpreted.
func TestMain(m *testing.M) {
	if os.Getenv("LUART_JIT_TEST") == "1" {
		jitThreshold, jitMinRun = 0, 0
	}
	os.Exit(m.Run())
}
