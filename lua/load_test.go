package lua_test

import (
	"strings"
	"testing"

	"github.com/matjam/apogee/lua"
	"github.com/matjam/apogee/stdlib"
)

// A malformed binary chunk fails to load with a syntax error naming the
// problem, as a malformed text chunk does.
func TestLoadMalformedBinaryChunk(t *testing.T) {
	for _, tt := range []struct{ chunk, name, want string }{
		{"\x1bLua", "=bad", "bad: truncated precompiled chunk"},
		{"\x1bLuaR\x00", "@bad.luac", "bad.luac: truncated precompiled chunk"},
		{"\x1bLu\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00", "\x1bLu", "binary string: not a precompiled chunk"},
		{"\x1bLua\x99\x00\x01\x04\x08\x04\x08\x00\x19\x93\r\n\x1a\n", "=bad", "bad: version mismatch in precompiled chunk"},
	} {
		l := lua.NewState()
		err := l.Load(strings.NewReader(tt.chunk), tt.name, "")
		if err != lua.ErrSyntax {
			t.Errorf("Load(%q) = %v, want ErrSyntax", tt.chunk, err)
			continue
		}
		if msg, _ := l.ToString(-1); msg != tt.want {
			t.Errorf("Load(%q) message %q, want %q", tt.chunk, msg, tt.want)
		}
	}
}

// load reports a malformed binary chunk to Lua as nil and a message.
func TestLoadMalformedBinaryChunkFromLua(t *testing.T) {
	l := lua.NewState()
	stdlib.Open(l)
	if err := l.DoString(`
		local f, msg = load("\27Lua")
		assert(f == nil, "loaded a truncated chunk")
		assert(msg == "binary string: truncated precompiled chunk", msg)
	`); err != nil {
		msg, _ := l.ToString(-1)
		t.Fatal(err, msg)
	}
}
