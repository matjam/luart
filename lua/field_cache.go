package lua

// fieldCache remembers where an instruction with a constant string key last
// found its field.
//
// When slot >= 0 the field is in the receiver's slots; when slot < 0 the
// receiver's shared shape lacks the key. When the field is absent or nil,
// a read goes on through the metatable. If mtShape is nil, that answers
// only for a receiver without a metatable: the field is nil. Otherwise the
// key was looked up in the table held by the metatable's __index field:
// mtSlot is that field's slot in the metatable, and index is that table's
// shape. indexSlot is the key's slot there, or < 0 when the lookup went on
// through chain. Every step is checked against the current tables, so the
// cache never returns a stale value.
//
// Compiled code reads the fields up to indexSlot itself, and leaves the
// rest to Go.
type fieldCache struct {
	shape     *shape
	slot      int32
	mtShape   *shape
	mtSlot    int32
	index     *shape
	indexSlot int32
	chain     *fieldChain
}

// A fieldChain continues a fieldCache's lookup through the __index tables
// after the first, as class hierarchies built from metatables need.
type fieldChain struct {
	levels []chainLevel
	slot   int32 // the key's slot in the last level's table, or -1: no table has it
}

// A chainLevel is one step: the metatable of the table before, the slot
// of its __index field, and the shape of the table that field holds.
type chainLevel struct {
	mtShape *shape
	mtSlot  int32
	index   *shape
}

// maxChain is how many __index tables past the first a fieldChain follows.
const maxChain = 8

// getField returns t[key] for a constant string key when it can answer
// without metamethod calls, and false when the caller must take the generic
// path.
func getField(t value, key value, c *fieldCache) (value, bool) {
	tt := t.table()
	if tt == nil {
		return nilValue, false
	}
	if s := tt.shape; s != nil && s == c.shape {
		// lookup, written out: it does not inline, and this is every read.
		if c.slot >= 0 {
			if v := tt.slots[c.slot]; !v.isNil() {
				return v, true
			}
		}
		if v, ok := c.fromIndex(tt); ok {
			return v, true
		}
	}
	return c.fill(tt, key)
}

// lookup answers from the cache for tt, whose shape is c.shape: its own
// field, or when that is nil or absent, the metatable's.
func (c *fieldCache) lookup(tt *table) (value, bool) {
	if c.slot >= 0 {
		if v := tt.slots[c.slot]; !v.isNil() {
			return v, true
		}
	}
	return c.fromIndex(tt)
}

func (c *fieldCache) fromIndex(tt *table) (value, bool) {
	mt := tt.metaTable
	if mt == nil {
		return nilValue, c.mtShape == nil
	}
	if mt.shape != c.mtShape {
		return nilValue, false
	}
	idx := mt.slots[c.mtSlot].table()
	if idx == nil || idx.shape != c.index {
		return nilValue, false
	}
	if c.indexSlot >= 0 {
		v := idx.slots[c.indexSlot]
		return v, !v.isNil()
	}
	return c.chain.from(idx)
}

// from follows the chain from t, the first __index table.
func (ch *fieldChain) from(t *table) (value, bool) {
	for i := range ch.levels {
		l := &ch.levels[i]
		mt := t.metaTable
		if mt == nil || mt.shape != l.mtShape {
			return nilValue, false
		}
		if t = mt.slots[l.mtSlot].table(); t == nil || t.shape != l.index {
			return nilValue, false
		}
	}
	if ch.slot < 0 { // no table has the key: nil, if the chain ends here
		return nilValue, t.metaTable == nil
	}
	v := t.slots[ch.slot]
	return v, !v.isNil()
}

// indexTable returns the table in metatable mt's __index field, with the
// field's slot, or false if there is none or it has no shape.
func indexTable(mt *table) (*table, int32, bool) {
	if mt.shape == nil {
		return nil, 0, false
	}
	mi, ok := mt.shape.slot("__index")
	if !ok {
		return nil, 0, false
	}
	idx := mt.slots[mi].table()
	if idx == nil || idx.shape == nil {
		return nil, 0, false
	}
	return idx, mi, true
}

// fill caches where tt's shape finds key: in its own slots, and through its
// metatable, for when the key is absent there or holds nil. It then answers
// from the cache.
func (c *fieldCache) fill(tt *table, k value) (value, bool) {
	s := tt.shape
	if s == nil {
		return nilValue, false
	}
	key, _ := k.str()
	slot, own := s.slot(key)
	if !own {
		if s.dict { // a dictionary can gain the key without changing shape
			return nilValue, false
		}
		slot = -1
	}
	// Until fillIndex succeeds, mtShape is nil: nil only for a table
	// without a metatable.
	*c = fieldCache{shape: s, slot: slot, indexSlot: -1, chain: c.chain}
	if !c.fillIndex(tt.metaTable, key) && !own {
		*c = fieldCache{chain: c.chain}
		return nilValue, false
	}
	return c.lookup(tt)
}

// fillIndex caches where key is found through metatable mt's __index
// tables, or that no table there has it. It reports false, leaving the
// cache's metatable fields unset, when that cannot be cached: an __index
// that is not a table with a shape, a dictionary that may gain the key, or
// a chain longer than maxChain. With mt nil there is nothing to cache.
func (c *fieldCache) fillIndex(mt *table, key string) bool {
	if mt == nil {
		return true
	}
	idx, mi, ok := indexTable(mt)
	if !ok {
		return false
	}
	if ii, ok := idx.shape.slot(key); ok {
		c.mtShape, c.mtSlot, c.index, c.indexSlot = mt.shape, mi, idx.shape, ii
		return true
	}
	// Further along the chain, or nowhere. The chain is reused, so a site
	// that sees several kinds of object does not allocate as it switches;
	// nothing refers to it while it is rebuilt, as mtShape is nil.
	ch := c.chain
	if ch == nil {
		ch = &fieldChain{}
		c.chain = ch
	}
	ch.levels, ch.slot = ch.levels[:0], -1
	for t := idx; ; {
		if t.shape.dict {
			return false
		}
		tmt := t.metaTable
		if tmt == nil {
			break // the key is nowhere
		}
		next, nmi, ok := indexTable(tmt)
		if !ok || len(ch.levels) == maxChain {
			return false
		}
		ch.levels = append(ch.levels, chainLevel{tmt.shape, nmi, next.shape})
		t = next
		if i, ok := t.shape.slot(key); ok {
			ch.slot = i
			break
		}
	}
	c.mtShape, c.mtSlot, c.index, c.indexSlot = mt.shape, mi, idx.shape, -1
	return true
}

// setField stores t[key] = v for a constant string key whose slot exists
// when no __newindex metamethod can apply: the field already holds a value,
// or the table has no metatable, or one known to lack __newindex. A
// dictionary counts its nil slots, so storing nil in one, or filling one,
// is left to the generic path. It reports false when the caller must take
// the generic path.
func setField(t value, key value, v value, c *fieldCache) bool {
	tt := t.table()
	if tt == nil {
		return false
	}
	s := tt.shape
	if s == nil || s.dict && v.isNil() {
		return false
	}
	if s != c.shape || c.slot < 0 {
		k, _ := key.str()
		i, ok := s.slot(k)
		if !ok {
			return false
		}
		*c = fieldCache{shape: s, slot: i, indexSlot: -1, chain: c.chain}
	}
	if tt.slots[c.slot].isNil() {
		if s.dict || tt.metaTable != nil && tt.metaTable.flags&(1<<tmNewIndex) == 0 {
			return false
		}
	}
	tt.slots[c.slot] = v
	tt.invalidateTagMethodCache()
	return true
}
