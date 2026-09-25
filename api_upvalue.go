package luart

// UpValue returns the name of the upvalue at index away from function,
// where index cannot be greater than the number of upvalues.
//
// Returns an empty string and false if the index is greater than the number
// of upvalues.
func (l *State) UpValue(function, index int) (name string, ok bool) {
	if c := l.indexToValue(function).closure(); c != nil {
		if ok = 1 <= index && index <= c.upValueCount(); ok {
			if c, isLua := c.(*luaClosure); isLua {
				name = c.prototype.upValues[index-1].name
			}
			l.apiPush(c.upValue(index - 1))
		}
	}
	return
}

// SetUpValue sets the value of a closure's upvalue. It assigns the value at
// the top of the stack to the upvalue and returns its name.  It also pops a
// value from the stack. function and index are as in UpValue.
//
// Returns an empty string and false if the index is greater than the number
// of upvalues.
//
// http://www.lua.org/manual/5.2/manual.html#lua_setupvalue
func (l *State) SetUpValue(function, index int) (name string, ok bool) {
	if c := l.indexToValue(function).closure(); c != nil {
		if ok = 1 <= index && index <= c.upValueCount(); ok {
			if c, isLua := c.(*luaClosure); isLua {
				name = c.prototype.upValues[index-1].name
			}
			l.top--
			c.setUpValue(index-1, l.stack[l.top])
		}
	}
	return
}

func (l *State) upValue(f, n int) **upValue {
	return &l.indexToValue(f).luaClosure().upValues[n-1]
}

// UpValueID returns a unique identifier for the upvalue numbered n from the
// closure at index f. Parameters f and n are as in UpValue (but n cannot be
// greater than the number of upvalues).
//
// These unique identifiers allow a program to check whether different
// closures share upvalues. Lua closures that share an upvalue (that is, that
// access a same external local variable) will return identical ids for those
// upvalue indices.
func (l *State) UpValueID(f, n int) any {
	v := l.indexToValue(f)
	if v.luaClosure() != nil {
		return *l.upValue(f, n)
	} else if c := v.goClosure(); c != nil {
		return &c.upValues[n-1]
	}
	panic("closure expected")
}

// UpValueJoin makes the n1-th upvalue of the Lua closure at index f1 refer to
// the n2-th upvalue of the Lua closure at index f2.
func (l *State) UpValueJoin(f1, n1, f2, n2 int) {
	u1 := l.upValue(f1, n1)
	u2 := l.upValue(f2, n2)
	*u1 = *u2
}
