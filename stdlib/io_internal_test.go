package stdlib

import (
	"os"
	"testing"
)

// After a failed flush a stream takes writes again, as C's FILE does, where
// bufio would refuse them: files.lua writes to /dev/full.
func TestWriteAfterFailedFlush(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close() // writes fail
	defer w.Close()
	s := &stream{f: w}
	s.setBuffer(fileBufferSize, false)
	defer s.setBuffer(0, false)
	if _, err := s.w.WriteString("abcd"); err != nil {
		t.Fatal(err)
	}
	if s.flush() == nil {
		t.Fatal("flush to a closed pipe succeeded")
	}
	if _, err := s.w.WriteString("abcd"); err != nil {
		t.Fatalf("write after a failed flush: %v", err)
	}
	if s.flush() == nil {
		t.Fatal("second flush succeeded")
	}
}
