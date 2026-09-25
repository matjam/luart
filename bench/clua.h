// The C side of the C Lua interpreters the benchmarks compare with: the
// same Go functions the suite registers in luart and go-lua, written in C,
// and calls that cgo can make where the Lua API has macros.

#include <stdlib.h>
#include <lua.h>
#include <lualib.h>
#include <lauxlib.h>

static double clua_canvas[200 * 100]; // plasma's canvas, as common_test.go's

static int clua_gofn(lua_State *L) {
	lua_pushnumber(L, lua_tonumber(L, 1));
	return 1;
}

static int clua_set(lua_State *L) {
	int x = (int)lua_tonumber(L, 1), y = (int)lua_tonumber(L, 2);
	if (x >= 0 && x < 200 && y >= 0 && y < 100)
		clua_canvas[y * 200 + x] = lua_tonumber(L, 3);
	return 0;
}

static lua_State *clua_new(void) {
	lua_State *L = luaL_newstate();
	luaL_openlibs(L);
	lua_register(L, "gofn", clua_gofn);
	lua_register(L, "set", clua_set);
	return L;
}

// clua_do runs src, returning NULL or the error message, which stays on
// the stack.
static const char *clua_do(lua_State *L, const char *src) {
	if (luaL_loadstring(L, src) != 0 || lua_pcall(L, 0, 0, 0) != 0)
		return lua_tostring(L, -1);
	return NULL;
}

// clua_run calls the global run, storing its result in *out; it returns
// NULL or the error message, as clua_do does.
static const char *clua_run(lua_State *L, double *out) {
	lua_getglobal(L, "run");
	if (lua_pcall(L, 0, 1, 0) != 0)
		return lua_tostring(L, -1);
	*out = lua_tonumber(L, -1);
	lua_pop(L, 1);
	return NULL;
}
