package luart

// Global pushes onto the stack the value of the global name.
//
// http://www.lua.org/manual/5.2/manual.html#lua_getglobal
func (l *State) Global(name string) {
	l.field(l.global.registry.atInt(RegistryIndexGlobals), name, false)
}

// field pushes t[name]. A raw hit skips boxing name, which would allocate.
func (l *State) field(t value, name string, check bool) {
	if tt := t.table(); tt != nil {
		if v := tt.atString(name); !v.isNil() {
			if check {
				l.apiPush(v)
			} else {
				l.push(v)
			}
			return
		}
	}
	if check {
		l.apiPush(stringValue(name))
	} else {
		l.push(stringValue(name))
	}
	l.stack[l.top-1] = l.tableAt(t, l.stack[l.top-1])
}

// Field pushes onto the stack the value table[name], where table is the
// table on the stack at the given index. This call may trigger a
// metamethod for the __index event.
//
// http://www.lua.org/manual/5.2/manual.html#lua_getfield
func (l *State) Field(index int, name string) {
	l.field(l.indexToValue(index), name, true)
}

// RawGet is similar to GetTable, but does a raw access (without metamethods).
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawget
func (l *State) RawGet(index int) {
	t := l.indexToValue(index).table()
	l.stack[l.top-1] = t.at(l.stack[l.top-1])
}

// RawGetInt pushes onto the stack the value table[key] where table is the
// value at index on the stack. The access is raw, as it doesn't invoke
// metamethods.
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawgeti
func (l *State) RawGetInt(index, key int) {
	t := l.indexToValue(index).table()
	l.apiPush(t.atInt(key))
}

// RawGetValue pushes onto the stack value table[p] where table is the
// value at index on the stack, and p is a light userdata.  The access is
// raw, as it doesn't invoke metamethods.
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawgetp
func (l *State) RawGetValue(index int, p any) {
	t := l.indexToValue(index).table()
	l.apiPush(t.at(l.valueOf(p)))
}

// CreateTable creates a new empty table and pushes it onto the stack.
// arrayCount is a hint for how many elements the table will have as a
// sequence; recordCount is a hint for how many other elements the table
// will have.  Lua may use these hints to preallocate memory for the the new
// table.  This pre-allocation is useful for performance when you know in
// advance how many elements the table will have.  Otherwise, you can use the
// function NewTable.
//
// http://www.lua.org/manual/5.2/manual.html#lua_createtable
func (l *State) CreateTable(arrayCount, recordCount int) {
	l.apiPush(objectValue(newTableWithSize(arrayCount, recordCount)))
}

// NewTable creates a new empty table and pushes it onto the stack. It is
// equivalent to l.CreateTable(0, 0).
//
// http://www.lua.org/manual/5.2/manual.html#lua_newtable
func (l *State) NewTable() { l.CreateTable(0, 0) }

// MetaTable pushes onto the stack the metatable of the value at index.  If
// the value at index does not have a metatable, the function returns
// false and nothing is put onto the stack.
//
// http://www.lua.org/manual/5.2/manual.html#lua_getmetatable
func (l *State) MetaTable(index int) bool {
	var mt *table
	v := l.indexToValue(index)
	if t := v.table(); t != nil {
		mt = t.metaTable
	} else if d := v.userData(); d != nil {
		mt = d.metaTable
	} else {
		mt = l.global.metaTable(v)
	}
	if mt == nil {
		return false
	}
	l.apiPush(objectValue(mt))
	return true
}

// UserValue pushes onto the stack the Lua value associated with the userdata
// at index.  This value must be a table or nil.
//
// http://www.lua.org/manual/5.2/manual.html#lua_getuservalue
func (l *State) UserValue(index int) {
	d := l.indexToValue(index).userData()
	if d.env == nil {
		l.apiPush(nilValue)
	} else {
		l.apiPush(objectValue(d.env))
	}
}

// SetGlobal pops a value from the stack and sets it as the new value of
// global name.
//
// http://www.lua.org/manual/5.2/manual.html#lua_setglobal
func (l *State) SetGlobal(name string) {
	l.checkElementCount(1)
	g := l.global.registry.atInt(RegistryIndexGlobals)
	l.push(stringValue(name))
	l.setTableAt(g, l.stack[l.top-1], l.stack[l.top-2])
	l.top -= 2 // pop value and key
}

// SetField does the equivalent of table[key]=v where table is the value at
// index and v is the value on top of the stack.
//
// This function pops the value from the stack. As in Lua, this function may
// trigger a metamethod for the __newindex event.
//
// http://www.lua.org/manual/5.2/manual.html#lua_setfield
func (l *State) SetField(index int, key string) {
	l.checkElementCount(1)
	t := l.indexToValue(index)
	k := stringValue(key)
	l.push(k)
	l.setTableAt(t, k, l.stack[l.top-2])
	l.top -= 2
}

// SetTable does the equivalent of table[key]=v, where table is the value
// at index, v is the value at the top of the stack and key is the value
// just below the top.
//
// The function pops both the key and the value from the stack.  As in Lua,
// this function may trigger a metamethod for the __newindex event.
//
// http://www.lua.org/manual/5.2/manual.html#lua_settable
func (l *State) SetTable(index int) {
	l.checkElementCount(2)
	l.setTableAt(l.indexToValue(index), l.stack[l.top-2], l.stack[l.top-1])
	l.top -= 2
}

// RawSet is similar to SetTable, but does a raw assignment (without
// metamethods).
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawset
func (l *State) RawSet(index int) {
	l.checkElementCount(2)
	t := l.indexToValue(index).table()
	t.put(l, l.stack[l.top-2], l.stack[l.top-1])
	t.invalidateTagMethodCache()
	l.top -= 2
}

// RawSetInt does the equivalent of table[n]=v where table is the table at
// index and v is the value at the top of the stack.
//
// This function pops the value from the stack.  The assignment is raw; it
// doesn't invoke metamethods.
//
// http://www.lua.org/manual/5.2/manual.html#lua_rawseti
func (l *State) RawSetInt(index, key int) {
	l.checkElementCount(1)
	t := l.indexToValue(index).table()
	t.putAtInt(key, l.stack[l.top-1])
	l.top--
}

// SetUserValue pops a table or nil from the stack and sets it as the new
// value associated to the userdata at index.
//
// http://www.lua.org/manual/5.2/manual.html#lua_setuservalue
func (l *State) SetUserValue(index int) {
	l.checkElementCount(1)
	d := l.indexToValue(index).userData()
	if l.stack[l.top-1].isNil() {
		d.env = nil
	} else {
		d.env = l.stack[l.top-1].table()
	}
	l.top--
}

// SetMetaTable pops a table from the stack and sets it as the new metatable
// for the value at index.
//
// http://www.lua.org/manual/5.2/manual.html#lua_setmetatable
func (l *State) SetMetaTable(index int) {
	l.checkElementCount(1)
	mt := l.stack[l.top-1].table()
	if apiCheck && mt == nil && !l.stack[l.top-1].isNil() {
		panic("table expected")
	}
	v := l.indexToValue(index)
	if t := v.table(); t != nil {
		t.metaTable = mt
	} else if d := v.userData(); d != nil {
		d.metaTable = mt
	} else {
		l.global.metaTables[l.TypeOf(index)] = mt
	}
	l.top--
}

// Next pops a key from the stack and pushes a key-value pair from the table
// at index, while the table has next elements.  If there are no more
// elements, nothing is pushed on the stack and Next returns false.
//
// A typical traversal looks like this:
//
//	// Table is on top of the stack (index -1).
//	l.PushNil() // Add nil entry on stack (need 2 free slots).
//	for l.Next(-2) {
//		key := lua.CheckString(l, -2)
//		val := lua.CheckString(l, -1)
//		l.Pop(1) // Remove val, but need key for the next iter.
//	}
//
// http://www.lua.org/manual/5.2/manual.html#lua_next
func (l *State) Next(index int) bool {
	t := l.indexToValue(index).table()
	if l.next(t, l.top-1) {
		l.apiIncrementTop()
		return true
	}
	// no more elements
	l.top-- // remove key
	return false
}

// Register sets the Go function f as the new value of global name. If
// name was already defined, it is overwritten.
//
// http://www.lua.org/manual/5.2/manual.html#lua_register
func (l *State) Register(name string, f Function) {
	l.PushGoFunction(f)
	l.SetGlobal(name)
}

// Table pushes onto the stack the value table[top], where table is the
// value at index, and top is the value at the top of the stack. This
// function pops the key from the stack, putting the resulting value in its
// place.  As in Lua, this function may trigger a metamethod for the __index
// event.
//
// http://www.lua.org/manual/5.2/manual.html#lua_gettable
func (l *State) Table(index int) {
	l.stack[l.top-1] = l.tableAt(l.indexToValue(index), l.stack[l.top-1])
}
