package stdlib

import (
	"fmt"
	"os"
	"strings"

	"github.com/matjam/apogee/lua"
)

// The package library, after loadlib.c. apogee cannot load dynamic
// libraries: package.loadlib and the C searchers report that, as C Lua
// built without them does.

const (
	pathSep    = ";" // separates templates
	pathMark   = "?" // stands for the module's name
	dirSep     = "/"
	noDynamic  = "dynamic libraries not enabled; check your Lua installation"
	root       = "/usr/local/"
	versionDir = "5.5"  // luaconf.h's LUA_VDIR
	envSuffix  = "_5_5" // after LUA_PATH and LUA_CPATH, lua.h's LUA_VERSUFFIX
	luaDir     = root + "share/lua/" + versionDir + "/"
	cDir       = root + "lib/lua/" + versionDir + "/"
	defaultLua = luaDir + "?.lua;" + luaDir + "?/init.lua;" + cDir + "?.lua;" + cDir + "?/init.lua;./?.lua;./?/init.lua"
	defaultC   = cDir + "?.so;" + cDir + "loadall.so;./?.so"
)

func findLoader(l *lua.State, name string) {
	if l.Field(lua.UpValueIndex(1), "searchers"); !l.IsTable(3) {
		l.Errorf("'package.searchers' must be a table")
	}
	var msg strings.Builder
	for i := 1; ; i++ {
		if l.RawGetInt(3, i); l.IsNil(-1) { // no more searchers
			l.Pop(1)
			l.Errorf("module '%s' not found:%s", name, msg.String())
		}
		l.PushString(name)
		if l.Call(1, 2); l.IsFunction(-2) { // found a loader
			return
		} else if s, ok := l.ToString(-2); ok { // the searcher's reason
			msg.WriteString(s)
		}
		l.Pop(2)
	}
}

// findFile searches package[field] for name, pushing why it failed.
func findFile(l *lua.State, name, field string) (string, bool) {
	l.Field(lua.UpValueIndex(1), field)
	path, ok := l.ToString(-1)
	if !ok {
		l.Errorf("'package.%s' must be a string", field)
	}
	filename, tried := searchPath(name, path, ".", dirSep)
	if filename == "" {
		l.PushString(tried)
		return "", false
	}
	return filename, true
}

func checkLoad(l *lua.State, loaded bool, fileName string) int {
	if loaded { // Module loaded successfully?
		l.PushString(fileName) // Second argument to module.
		return 2               // Return open function & file name.
	}
	m, _ := l.ToString(1)
	e, _ := l.ToString(-1)
	l.Errorf("error loading module '%s' from file '%s':\n\t%s", m, fileName, e)
	panic("unreachable")
}

func searcherPreload(l *lua.State) int {
	name := l.CheckString(1)
	l.Field(lua.RegistryIndex, "_PRELOAD")
	l.Field(-1, name)
	if l.IsNil(-1) {
		l.PushString(fmt.Sprintf("\n\tno field package.preload['%s']", name))
	}
	return 1
}

func searcherLua(l *lua.State) int {
	name := l.CheckString(1)
	filename, ok := findFile(l, name, "path")
	if !ok {
		return 1 // not found in this path
	}
	return checkLoad(l, l.LoadFile(filename, "") == nil, filename)
}

// searcherC finds a C library for name, which cannot load.
func searcherC(l *lua.State) int {
	name := l.CheckString(1)
	filename, ok := findFile(l, name, "cpath")
	if !ok {
		return 1 // not found in this path
	}
	l.PushString(noDynamic)
	return checkLoad(l, false, filename)
}

// searcherCroot finds the C library of the root of a name such as a.b.c,
// which cannot load either.
func searcherCroot(l *lua.State) int {
	name := l.CheckString(1)
	p := strings.IndexByte(name, '.')
	if p < 0 {
		return 0 // a root itself
	}
	filename, ok := findFile(l, name[:p], "cpath")
	if !ok {
		return 1 // root not found
	}
	l.PushString(noDynamic)
	return checkLoad(l, false, filename)
}

func createSearchersTable(l *lua.State) {
	searchers := []lua.Function{searcherPreload, searcherLua, searcherC, searcherCroot}
	l.CreateTable(len(searchers), 0)
	for i, s := range searchers {
		l.PushValue(-2)
		l.PushGoClosure(s, 1)
		l.RawSetInt(-2, i+1)
	}
}

func readable(filename string) bool {
	f, err := os.Open(filename)
	if f != nil {
		f.Close()
	}
	return err == nil
}

// searchPath looks for name in path's templates, with sep in name
// replaced by dirSep, as loadlib.c's searchpath does. It returns the first
// readable file, or "" and the list of files it tried.
func searchPath(name, path, sep, dirSep string) (string, string) {
	if sep != "" {
		name = strings.ReplaceAll(name, sep, dirSep)
	}
	var msg strings.Builder
	for _, template := range strings.Split(path, pathSep) {
		if template == "" {
			continue
		}
		filename := strings.ReplaceAll(template, pathMark, name)
		if readable(filename) {
			return filename, ""
		}
		fmt.Fprintf(&msg, "\n\tno file '%s'", filename)
	}
	return "", msg.String()
}

func noEnv(l *lua.State) bool {
	l.Field(lua.RegistryIndex, "LUA_NOENV")
	b := l.ToBoolean(-1)
	l.Pop(1)
	return b
}

// setPath sets package[field] from the environment variable env with
// _5_5, or without it, or to def. The variable's first ";;" stands for
// def, as in loadlib.c's setpath.
func setPath(l *lua.State, field, env, def string) {
	path, ok := os.LookupEnv(env + envSuffix)
	if !ok {
		path, ok = os.LookupEnv(env)
	}
	if !ok || noEnv(l) {
		l.PushString(def)
	} else if prefix, suffix, found := strings.Cut(path, pathSep+pathSep); !found {
		l.PushString(path)
	} else {
		if prefix != "" {
			def = prefix + pathSep + def
		}
		if suffix != "" {
			def += pathSep + suffix
		}
		l.PushString(def)
	}
	l.SetField(-2, field)
}

var packageLibrary = []lua.RegistryFunction{
	{Name: "loadlib", Function: func(l *lua.State) int {
		l.CheckString(1) // path
		l.CheckString(2) // init
		l.PushNil()
		l.PushString(noDynamic)
		l.PushString("absent")
		return 3 // Return nil, error message, and where.
	}},
	{Name: "searchpath", Function: func(l *lua.State) int {
		name := l.CheckString(1)
		path := l.CheckString(2)
		sep := l.OptString(3, ".")
		dirSep := l.OptString(4, dirSep)
		f, tried := searchPath(name, path, sep, dirSep)
		if f == "" {
			l.PushNil()
			l.PushString(tried)
			return 2
		}
		l.PushString(f)
		return 1
	}},
}

// OpenPackage opens the package library. Usually passed to Require.
func OpenPackage(l *lua.State) int {
	l.NewLibrary(packageLibrary)
	createSearchersTable(l)
	l.SetField(-2, "searchers")
	setPath(l, "path", "LUA_PATH", defaultLua)
	setPath(l, "cpath", "LUA_CPATH", defaultC)
	l.PushString(dirSep + "\n" + pathSep + "\n" + pathMark + "\n!\n-\n")
	l.SetField(-2, "config")
	l.SubTable(lua.RegistryIndex, "_LOADED")
	l.SetField(-2, "loaded")
	l.SubTable(lua.RegistryIndex, "_PRELOAD")
	l.SetField(-2, "preload")
	l.PushGlobalTable()
	l.PushValue(-2)
	l.SetFunctions([]lua.RegistryFunction{{Name: "require", Function: func(l *lua.State) int {
		name := l.CheckString(1)
		l.SetTop(1)
		l.Field(lua.RegistryIndex, "_LOADED")
		l.Field(2, name)
		if l.ToBoolean(-1) {
			return 1
		}
		l.Pop(1)
		findLoader(l, name)
		l.PushString(name)
		l.Insert(-2)
		l.Call(2, 1)
		if !l.IsNil(-1) {
			l.SetField(2, name)
		}
		l.Field(2, name)
		if l.IsNil(-1) {
			l.PushBoolean(true)
			l.PushValue(-1)
			l.SetField(2, name)
		}
		return 1
	}}}, 1)
	l.Pop(1)
	return 1
}
