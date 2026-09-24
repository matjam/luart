package lua

// fieldCache remembers where an instruction with a constant string key last
// found its field.
//
// When slot >= 0 the field is in the receiver's slots. When slot < 0 the
// receiver's shared shape lacks the key, and the field was found in the
// table held by its metatable's __index field: mtSlot is that field's slot
// in the metatable, and index and indexSlot locate the key in that table.
// Every step is checked against the current tables, so the cache never
// returns a stale value.
type fieldCache struct {
	shape     *shape
	slot      int32
	mtShape   *shape
	mtSlot    int32
	index     *shape
	indexSlot int32
}

// getField returns t[key] for a constant string key when it can answer
// without metamethod calls, and false when the caller must take the generic
// path.
func getField(t value, key value, c *fieldCache) (value, bool) {
	tt, ok := t.o.(*table)
	if !ok {
		return nilValue, false
	}
	if s := tt.shape; s != nil && s == c.shape {
		if c.slot >= 0 {
			v := tt.slots[c.slot]
			return v, !v.isNil()
		}
		if v, ok := c.fromIndex(tt); ok {
			return v, true
		}
	}
	return c.fill(tt, key)
}

func (c *fieldCache) fromIndex(tt *table) (value, bool) {
	mt := tt.metaTable
	if mt == nil || mt.shape != c.mtShape {
		return nilValue, false
	}
	idx, ok := mt.slots[c.mtSlot].o.(*table)
	if !ok || idx.shape != c.index {
		return nilValue, false
	}
	v := idx.slots[c.indexSlot]
	return v, !v.isNil()
}

func (c *fieldCache) fill(tt *table, k value) (value, bool) {
	s := tt.shape
	if s == nil {
		return nilValue, false
	}
	key := k.o.(string)
	if i, ok := s.slot(key); ok {
		*c = fieldCache{shape: s, slot: i}
		v := tt.slots[i]
		return v, !v.isNil()
	}
	if s.dict { // a dictionary can gain the key without changing shape
		return nilValue, false
	}
	mt := tt.metaTable
	if mt == nil || mt.shape == nil {
		return nilValue, false
	}
	mi, ok := mt.shape.slot("__index")
	if !ok {
		return nilValue, false
	}
	idx, ok := mt.slots[mi].o.(*table)
	if !ok || idx.shape == nil {
		return nilValue, false
	}
	ii, ok := idx.shape.slot(key)
	if !ok {
		return nilValue, false
	}
	*c = fieldCache{shape: s, slot: -1, mtShape: mt.shape, mtSlot: mi, index: idx.shape, indexSlot: ii}
	v := idx.slots[ii]
	return v, !v.isNil()
}

// setField stores t[key] = v for a constant string key whose slot exists
// when no __newindex metamethod can apply: the field already holds a value,
// or the table has no metatable. It reports false when the caller must take
// the generic path.
func setField(t value, key value, v value, c *fieldCache) bool {
	tt, ok := t.o.(*table)
	if !ok || v.isNil() {
		return false
	}
	s := tt.shape
	if s == nil {
		return false
	}
	if s != c.shape || c.slot < 0 {
		i, ok := s.slot(key.o.(string))
		if !ok {
			return false
		}
		*c = fieldCache{shape: s, slot: i}
	}
	if tt.slots[c.slot].isNil() {
		if tt.metaTable != nil || s.dict {
			return false
		}
	}
	tt.slots[c.slot] = v
	tt.invalidateTagMethodCache()
	return true
}
