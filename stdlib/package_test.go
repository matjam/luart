package stdlib_test

import (
	"os"
	"path/filepath"
	"testing"
)

// The package library has 5.2's fields and four searchers. C modules are
// found on package.cpath but cannot load: luart has no dynamic libraries.
func TestPackage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cmod.so"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lmod.lua"), []byte("return {name = ..., file = select(2, ...)}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUART_TEST_DIR", dir)
	run(t, `
		local dir = os.getenv("LUART_TEST_DIR")
		assert(type(package.path) == "string" and type(package.cpath) == "string")
		assert(package.cpath:find("./?.so", 1, true))
		assert(#package.searchers == 4)
		assert(package.config:sub(3, 3) == ";")

		package.path = dir .. "/?.lua"
		package.cpath = dir .. "/?.so"
		local m = require("lmod")
		assert(m.name == "lmod" and m.file == dir .. "/lmod.lua")

		local ok, err = pcall(require, "nomod")
		assert(not ok and err:find("module 'nomod' not found:\n\tno field package.preload['nomod']", 1, true), err)
		assert(err:find("\n\tno file '" .. dir .. "/nomod.lua'", 1, true), err)
		assert(err:find("\n\tno file '" .. dir .. "/nomod.so'", 1, true), err)

		ok, err = pcall(require, "cmod")
		assert(not ok and err:find("error loading module 'cmod' from file '" .. dir .. "/cmod.so':\n\t", 1, true), err)
		assert(err:find("dynamic libraries not enabled", 1, true), err)
		ok, err = pcall(require, "cmod.sub") -- the root's file, by the all-in-one searcher
		assert(not ok and err:find("from file '" .. dir .. "/cmod.so'", 1, true), err)

		package.path = ""
		ok = pcall(require, "nomod") -- no templates: not found, not stdin
		assert(not ok)

		local f, msg = package.searchpath("x.y", "a:b/?.lua;./?.lua")
		assert(f == nil and msg == "\n\tno file 'a:b/x/y.lua'\n\tno file './x/y.lua'", msg)
		assert(package.searchpath("lmod", "nowhere/?.lua;" .. dir .. "/?.lua") == dir .. "/lmod.lua")
	`)
}

// LUA_PATH_5_2 sets package.path, with ;; standing for the default.
func TestPackagePathEnv(t *testing.T) {
	t.Setenv("LUA_PATH_5_2", "first/?.lua;;")
	t.Setenv("LUA_CPATH", "c/?.so")
	run(t, `
		assert(package.path:find("^first/%?%.lua;.+%./%?%.lua;$"), package.path)
		assert(package.cpath == "c/?.so", package.cpath)
	`)
}
