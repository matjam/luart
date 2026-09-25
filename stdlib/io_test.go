package stdlib_test

import (
	"os/exec"
	"testing"
)

// read and lines take liolib.c's formats, through a buffer that seeks and
// writes see past.
func TestIORead(t *testing.T) {
	run(t, `
		local f = io.tmpfile()
		assert(f:write("line1\n", 42, " 0x10\nline3") == f)
		assert(f:seek("set") == 0)
		assert(f:read() == "line1")
		assert(f:read("*n") == 42 and f:read("*n") == 16)
		assert(f:read("*L") == "\n")
		assert(f:read("*l") == "line3")
		assert(f:read() == nil and f:read("*a") == "" and f:read(0) == nil)

		f:seek("set")
		assert(f:read(3) == "lin" and f:read(0) == "")
		assert(f:seek() == 3) -- where the reads reached, not the buffer
		local a, b, c = f:read("*l", "*n", 100)
		assert(a == "e1" and b == 42 and c == " 0x10\nline3")
		assert(select("#", f:read("*l", "*l")) == 1) -- stops at the first failure

		f:seek("set")
		f:read(2)
		f:write("X") -- after reads, at their position
		f:seek("set")
		assert(f:read("*a") == "liXe1\n42 0x10\nline3")

		f:seek("set", 5)
		assert(f:read("*n") == 42) -- "\n42": skips space, then reads 42
		assert(f:read("*n") == 16 and f:read("*n") == nil) -- "line3" is no number
		f:close()

		local ok, err = pcall(f.read, f)
		assert(not ok and err:find("attempt to use a closed file", 1, true))
		assert(not pcall(io.read, "x") and not pcall(io.read, "*x"))

		-- Lines, of a named file closed at its end, with formats.
		local name = os.tmpname()
		local w = assert(io.open(name, "w"))
		w:write("1 2\n3 4\n")
		assert(w:close())
		local got = {}
		for x, y in io.lines(name, "*n", "*n") do got[#got + 1] = x + y end
		assert(#got == 2 and got[1] == 3 and got[2] == 7)
		local it = io.lines(name)
		assert(it() == "1 2" and it() == "3 4" and it() == nil)
		assert(not pcall(it)) -- the file is closed at its end

		-- The default input and output.
		io.output(name)
		io.write("a\nb\n")
		io.close()
		io.input(name)
		assert(io.read() == "a" and io.read("*a") == "b\n")
		io.input():close()
		io.input(io.stdin)
		io.output(io.stdout)

		-- Reading a file opened only for writing is an error, not an exception.
		w = io.open(name, "w")
		local r, msg, errno = w:read()
		assert(r == nil and type(msg) == "string" and errno ~= 0, msg)
		w:close()
		os.remove(name)
	`)
}

// io.open takes C's modes and reports failures as C does.
func TestIOOpen(t *testing.T) {
	run(t, `
		local name = os.tmpname()
		for _, mode in ipairs{"r", "rb", "r+", "r+b", "w", "wb", "w+", "a", "a+b", "rbb"} do
			local f = assert(io.open(name, mode), mode)
			f:close()
		end
		for _, mode in ipairs{"", "x", "rw", "r+r", "b"} do
			local ok, err = pcall(io.open, name, mode)
			assert(not ok and err:find("invalid mode", 1, true), mode)
		end
		os.remove(name)
		local f, msg, errno = io.open(name)
		assert(f == nil and msg == name .. ": No such file or directory" and errno == 2, msg)
		local ok, err = pcall(io.lines, name)
		assert(not ok and err:find("cannot open file '" .. name .. "' (No such file or directory)", 1, true), err)

		local t = io.tmpfile()
		assert(t:setvbuf("no") == true and t:setvbuf("full", 1024) == true and t:flush() == true)
		assert(not pcall(t.setvbuf, t, "bogus"))
		t:close()
		assert(io.flush() == true)
	`)
}

// io.popen reads a command's output or writes its input, and closing it
// returns the command's status, as os.execute does.
func TestIOPopen(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("io.popen needs sh")
	}
	run(t, `
		local f = assert(io.popen("echo hello; echo world"))
		assert(io.type(f) == "file")
		assert(f:read("*l") == "hello" and f:read("*l") == "world" and f:read("*l") == nil)
		local ok, how, code = f:close()
		assert(ok == true and how == "exit" and code == 0)
		assert(io.type(f) == "closed file")

		f = assert(io.popen("exit 3"))
		f:read("*a")
		ok, how, code = f:close()
		assert(ok == nil and how == "exit" and code == 3, tostring(code))

		local out = os.tmpname()
		f = assert(io.popen("cat > " .. out, "w"))
		f:write("piped")
		assert(f:close())
		local g = assert(io.open(out))
		assert(g:read("*a") == "piped")
		g:close()
		os.remove(out)

		local ok, err = pcall(io.popen, "true", "rw")
		assert(not ok and err:find("invalid mode", 1, true), err)

		assert(os.execute() == true)
		ok, how, code = os.execute("exit 4")
		assert(ok == nil and how == "exit" and code == 4)
		assert(os.execute("true") == true)
	`)
}
