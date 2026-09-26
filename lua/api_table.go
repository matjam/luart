package lua

// Global pushes onto the stack the value of the global name, and returns
// its type.
//
// https://www.lua.org/manual/5.5/manual.html#lua_getglobal
func (l *State) Global(name string) Type {
	l.field(l.global.registry.atInt(RegistryIndexGlobals), name, false)
	return l.valueToType(l.stack[l.top-1])
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
// table on the stack at the given index, and returns its type. This call
// may trigger a metamethod for the __index event.
//
// https://www.lua.org/manual/5.5/manual.html#lua_getfield
func (l *State) Field(index int, name string) Type {
	l.field(l.indexToValue(index), name, true)
	return l.valueToType(l.stack[l.top-1])
}

// FieldInt pushes t[i], where t is the value at index, and returns its
// type. As in Lua, this may trigger a metamethod for the "index" event.
//
// http://www.lua.org/manual/5.5/manual.html#lua_geti
func (l *State) FieldInt[T Integer](index int, i T) Type {
	t, k := l.indexToValue(index), int64(i)
	var v value
	if tt := t.table(); tt != nil && int64(int(k)) == k {
		// A raw read first, as the library's loops over sequences mostly
		// find their values.
		if v = tt.atInt(int(k)); v.isNil() && tt.metaTable != nil {
			v = l.tableAt(t, integerValue(k))
		}
	} else {
		v = l.tableAt(t, integerValue(k))
	}
	l.apiPush(v)
	return l.valueToType(v)
}

// SetFieldInt does t[i] = v, where t is the value at index and v the value
// on top of the stack, which it pops. As in Lua, this may trigger a
// metamethod for the "newindex" event.
//
// http://www.lua.org/manual/5.5/manual.html#lua_seti
func (l *State) SetFieldInt[T Integer](index int, i T) {
	l.checkElementCount(1)
	t := l.indexToValue(index)
	l.setTableAt(t, integerValue(int64(i)), l.stack[l.top-1])
	l.top--
}

// RawGet is similar to Table, but does a raw access (without metamethods).
// It returns the type of the value.
//
// https://www.lua.org/manual/5.5/manual.html#lua_rawget
func (l *State) RawGet(index int) Type {
	t := l.indexToValue(index).table()
	v := t.at(l.stack[l.top-1])
	l.stack[l.top-1] = v
	return l.valueToType(v)
}

// RawGetInt pushes onto the stack the value table[key] where table is the
// value at index on the stack. The access is raw, as it doesn't invoke
// metamethods. It returns the type of the value.
//
// https://www.lua.org/manual/5.5/manual.html#lua_rawgeti
func (l *State) RawGetInt[T Integer](index int, key T) Type {
	t := l.indexToValue(index).table()
	v := t.at(integerValue(int64(key)))
	l.apiPush(v)
	return l.valueToType(v)
}

// RawGetValue pushes onto the stack value table[p] where table is the
// value at index on the stack, and p is a light userdata.  The access is
// raw, as it doesn't invoke metamethods. It returns the type of the value.
//
// https://www.lua.org/manual/5.5/manual.html#lua_rawgetp
func (l *State) RawGetValue(index int, p any) Type {
	t := l.indexToValue(index).table()
	v := t.at(l.valueOf(p))
	l.apiPush(v)
	return l.valueToType(v)
}

// CreateTable creates a new empty table and pushes it onto the stack.
// arrayCount is a hint for how many elements the table will have as a
// sequence; recordCount is a hint for how many other elements the table
// will have.  Lua may use these hints to preallocate memory for the the new
// table.  This pre-allocation is useful for performance when you know in
// advance how many elements the table will have.  Otherwise, you can use the
// function NewTable.
//
// https://www.lua.org/manual/5.5/manual.html#lua_createtable
func (l *State) CreateTable(arrayCount, recordCount int) {
	l.chargeTable(arrayCount, recordCount)
	l.apiPush(objectValue(newTableWithSize(arrayCount, recordCount)))
}

// NewTable creates a new empty table and pushes it onto the stack. It is
// equivalent to l.CreateTable(0, 0).
//
// https://www.lua.org/manual/5.5/manual.html#lua_newtable
func (l *State) NewTable() { l.CreateTable(0, 0) }

// MetaTable pushes onto the stack the metatable of the value at index.  If
// the value at index does not have a metatable, the function returns
// false and nothing is put onto the stack.
//
// https://www.lua.org/manual/5.5/manual.html#lua_getmetatable
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

// UserValue pushes user value n of the full userdata at index and returns
// its type, or pushes nil and returns TypeNone if the userdata has no such
// value.
//
// http://www.lua.org/manual/5.5/manual.html#lua_getiuservalue
func (l *State) UserValue(index, n int) Type {
	d := l.indexToValue(index).userData()
	if d == nil || n < 1 || n > len(d.userValues) {
		l.apiPush(nilValue)
		return TypeNone
	}
	v := d.userValues[n-1]
	l.apiPush(v)
	return l.valueToType(v)
}

// SetGlobal pops a value from the stack and sets it as the new value of
// global name.
//
// https://www.lua.org/manual/5.5/manual.html#lua_setglobal
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
// https://www.lua.org/manual/5.5/manual.html#lua_setfield
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
// https://www.lua.org/manual/5.5/manual.html#lua_settable
func (l *State) SetTable(index int) {
	l.checkElementCount(2)
	l.setTableAt(l.indexToValue(index), l.stack[l.top-2], l.stack[l.top-1])
	l.top -= 2
}

// RawSet is similar to SetTable, but does a raw assignment (without
// metamethods).
//
// https://www.lua.org/manual/5.5/manual.html#lua_rawset
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
// http://www.lua.org/manual/5.5/manual.html#lua_rawseti
func (l *State) RawSetInt[T Integer](index int, key T) {
	l.checkElementCount(1)
	t := l.indexToValue(index).table()
	t.put(l, integerValue(int64(key)), l.stack[l.top-1])
	l.top--
}

// SetUserValue pops a value from the stack and sets it as user value n of
// the full userdata at index. It reports false, popping the value anyway,
// if the userdata has no such value.
//
// http://www.lua.org/manual/5.5/manual.html#lua_setiuservalue
func (l *State) SetUserValue(index, n int) bool {
	l.checkElementCount(1)
	d := l.indexToValue(index).userData()
	ok := d != nil && 1 <= n && n <= len(d.userValues)
	if ok {
		d.userValues[n-1] = l.stack[l.top-1]
	}
	l.top--
	return ok
}

// SetMetaTable pops a table from the stack and sets it as the new metatable
// for the value at index.
//
// https://www.lua.org/manual/5.5/manual.html#lua_setmetatable
func (l *State) SetMetaTable(index int) {
	l.checkElementCount(1)
	mt := l.stack[l.top-1].table()
	if apiCheck && mt == nil && !l.stack[l.top-1].isNil() {
		panic("table expected")
	}
	v := l.indexToValue(index)
	if t := v.table(); t != nil {
		t.metaTable = mt
		l.noteMetaTable(v, mt)
	} else if d := v.userData(); d != nil {
		d.metaTable = mt
		l.noteMetaTable(v, mt)
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
//		key := l.CheckString(-2)
//		val := l.CheckString(-1)
//		l.Pop(1) // Remove val, but need key for the next iter.
//	}
//
// https://www.lua.org/manual/5.5/manual.html#lua_next
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
// https://www.lua.org/manual/5.5/manual.html#lua_register
func (l *State) Register(name string, f Function) {
	l.PushGoFunction(f)
	l.SetGlobal(name)
}

// Table pushes onto the stack the value table[top], where table is the
// value at index, and top is the value at the top of the stack. This
// function pops the key from the stack, putting the resulting value in its
// place.  As in Lua, this function may trigger a metamethod for the __index
// event. It returns the type of the value.
//
// https://www.lua.org/manual/5.5/manual.html#lua_gettable
func (l *State) Table(index int) Type {
	v := l.tableAt(l.indexToValue(index), l.stack[l.top-1])
	l.stack[l.top-1] = v
	return l.valueToType(v)
}
