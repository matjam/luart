package lua

import (
	"math"
)

// table is a Lua table. Integer keys from 1 to len(array) live in array.
// String keys live in slots, laid out by shape (see shape.go), and all
// remaining keys live in hash, which is allocated on first write.
type table struct {
	array         []value
	shape         *shape  // nil until the first string key
	slots         []value // len(slots) == len(shape.keys)
	dead          int     // nil slots in a dictionary shape
	hash          map[value]value
	metaTable     *table
	flags         byte
	iterationKeys []value // snapshot of hash keys for next
	iterationNext int     // index in iterationKeys after the key next last returned
}

const minCompactSlots = 8

func newTable() *table                     { return &table{} }
func (t *table) invalidateTagMethodCache() { t.flags = 0 }

func newTableWithSize(arraySize, hashSize int) *table {
	t := new(table)
	if arraySize > 0 {
		t.array = make([]value, arraySize)
	}
	if hashSize > 0 {
		t.slots = make([]value, 0, hashSize)
	}
	return t
}

func (t *table) atString(k string) value {
	if t.shape != nil {
		if i, ok := t.shape.slot(k); ok {
			return t.slots[i]
		}
	}
	return nilValue
}

// putString sets string key k, whose text is key, to v. root is the owning
// state's root shape.
func (t *table) putString(root *shape, k value, key string, v value) {
	if t.shape != nil {
		if i, ok := t.shape.slot(key); ok {
			old := t.slots[i]
			t.slots[i] = v
			if t.shape.dict {
				if old.isNil() && !v.isNil() {
					t.dead--
				} else if !old.isNil() && v.isNil() {
					t.dead++
				}
			}
			return
		}
	}
	if v.isNil() {
		return
	}
	s := t.shape
	if s == nil {
		s = root
	} else if s.dict && t.dead > minCompactSlots && t.dead > len(t.slots)/2 {
		t.compact()
		s = t.shape
	}
	t.shape = s.with(k, key)
	t.slots = append(t.slots, v)
	t.iterationKeys = nil // invalidate iterations when adding an entry
}

// compact drops the nil slots of a dictionary. The new shape invalidates
// slots that instructions cached for the old one.
func (t *table) compact() {
	live := len(t.slots) - t.dead
	d := &shape{slots: make(map[string]int32, live), keys: make([]value, 0, live), dict: true}
	slots := make([]value, 0, live)
	for i, v := range t.slots {
		if !v.isNil() {
			k := t.shape.keys[i]
			d.slots[k.o.(string)] = int32(len(slots))
			d.keys = append(d.keys, k)
			slots = append(slots, v)
		}
	}
	t.shape, t.slots, t.dead = d, slots, 0
}

func (l *State) fastTagMethod(table *table, event tm) value {
	if table == nil || table.flags&1<<event != 0 {
		return nilValue
	}
	return table.tagMethod(event, l.global.tagMethodNames[event])
}

// intKey reports whether k is a number with an integral value.
func intKey(k value) (int, bool) {
	if f, ok := k.number(); ok {
		if i := int(f); float64(i) == f {
			return i, true
		}
	}
	return 0, false
}

func (t *table) extendArray(last int) {
	t.array = append(t.array, make([]value, last-len(t.array))...)
	for k, v := range t.hash {
		if i, ok := intKey(k); ok && 0 < i && i <= len(t.array) {
			t.array[i-1] = v
			delete(t.hash, k)
		}
	}
}

func (t *table) atInt(k int) value {
	if 0 < k && k <= len(t.array) {
		return t.array[k-1]
	}
	return t.hash[numberValue(float64(k))]
}

func (t *table) maybeResizeArray(key int) bool {
	// Precondition: key > len(t.array).
	occupancy := 0
	for _, v := range t.array {
		if !v.isNil() {
			occupancy++
		}
	}
	for k, v := range t.hash {
		if i, ok := intKey(k); ok && i <= key && !v.isNil() {
			occupancy++
		}
	}
	if occupancy >= key>>1 {
		t.extendArray(max(occupancy*2, key)) // TODO Tune growth function.
		return true
	}
	return false
}

// addOrInsertHash stores a non-nil v at a key that is not a string and is
// outside the array part.
func (t *table) addOrInsertHash(k, v value) {
	if t.hash == nil {
		t.hash = make(map[value]value)
	}
	if _, ok := t.hash[k]; !ok {
		t.iterationKeys = nil // invalidate iterations when adding an entry
	}
	t.hash[k] = v
}

func (t *table) putAtInt(k int, v value) {
	if 0 < k && k <= len(t.array) {
		t.array[k-1] = v
	} else if k > 0 && !v.isNil() && t.maybeResizeArray(k) {
		t.array[k-1] = v
	} else if v.isNil() {
		delete(t.hash, numberValue(float64(k)))
	} else {
		t.addOrInsertHash(numberValue(float64(k)), v)
	}
}

func (t *table) at(k value) value {
	switch o := k.o.(type) {
	case nil:
		return nilValue
	case string:
		return t.atString(o)
	case *numberTag:
		if i := int(k.n); float64(i) == k.n && 0 < i && i <= len(t.array) { // OPT: Inlined copy of atInt.
			return t.array[i-1]
		}
	}
	return t.hash[k]
}

func (t *table) put(l *State, k, v value) {
	switch o := k.o.(type) {
	case nil:
		l.runtimeError("table index is nil")
		return
	case string:
		t.putString(l.global.rootShape, k, o, v)
		return
	case *numberTag:
		if i := int(k.n); float64(i) == k.n {
			t.putAtInt(i, v)
			return
		} else if math.IsNaN(k.n) {
			l.runtimeError("table index is NaN")
			return
		}
	}
	if v.isNil() {
		delete(t.hash, k)
	} else {
		t.addOrInsertHash(k, v)
	}
}

// tryPut stores v at k when k already holds a non-nil value, where Lua does
// a raw assignment without consulting __newindex. It reports whether it did.
func (t *table) tryPut(l *State, k, v value) bool {
	switch o := k.o.(type) {
	case nil:
		return false
	case string:
		if t.shape == nil {
			return false
		}
		if i, ok := t.shape.slot(o); ok && !t.slots[i].isNil() {
			if v.isNil() && t.shape.dict {
				t.dead++
			}
			t.slots[i] = v
			return true
		}
		return false
	case *numberTag:
		if i := int(k.n); float64(i) == k.n && 0 < i && i <= len(t.array) && !t.array[i-1].isNil() {
			t.array[i-1] = v
			return true
		} else if math.IsNaN(k.n) {
			return false
		}
	}
	if old, ok := t.hash[k]; ok && !old.isNil() {
		if v.isNil() {
			delete(t.hash, k)
		} else {
			t.hash[k] = v
		}
		return true
	}
	return false
}

func (t *table) unboundSearch(j int) int {
	i := j
	for j++; !t.atInt(j).isNil(); {
		i = j
		if j *= 2; j < 0 {
			for i = 1; !t.atInt(i).isNil(); i++ {
			}
			return i - 1
		}
	}
	for j-i > 1 {
		m := (i + j) / 2
		if t.atInt(m).isNil() {
			j = m
		} else {
			i = m
		}
	}
	return i
}

func (t *table) length() int {
	j := len(t.array)
	if j > 0 && t.array[j-1].isNil() {
		i := 0
		for j-i > 1 {
			m := (i + j) / 2
			if t.array[m-1].isNil() {
				j = m
			} else {
				i = m
			}
		}
		return i
	} else if t.hash == nil {
		return j
	}
	return t.unboundSearch(j)
}

func arrayIndex(k value) int {
	if i, ok := intKey(k); ok {
		return i
	}
	return -1
}

// next replaces the key at stack index key with the following key and value,
// and reports whether there was one. It visits the array part, then string
// keys in slot order, then other keys from a snapshot of hash. Fields
// cleared during a traversal are still found, as Lua allows.
func (l *State) next(t *table, key int) bool {
	k := l.stack[key]
	i, s := 0, 0 // array and slot positions to resume from
	if !k.isNil() {
		if i = arrayIndex(k); 0 < i && i <= len(t.array) {
		} else if str, ok := k.str(); ok {
			slot, ok := int32(0), false
			if t.shape != nil {
				slot, ok = t.shape.slot(str)
			}
			if !ok {
				l.runtimeError("invalid key to 'next'")
			}
			i, s = len(t.array), int(slot)+1
		} else {
			return l.nextHashKey(t, key, k)
		}
	}
	for ; i < len(t.array); i++ {
		if !t.array[i].isNil() {
			l.stack[key] = numberValue(float64(i + 1))
			l.stack[key+1] = t.array[i]
			return true
		}
	}
	for ; s < len(t.slots); s++ {
		if v := t.slots[s]; !v.isNil() {
			l.stack[key], l.stack[key+1] = t.shape.keys[s], v
			return true
		}
	}
	if t.iterationKeys == nil {
		t.snapshotKeys()
	}
	return l.nextMapKey(t, key, 0)
}

func (l *State) nextHashKey(t *table, key int, k value) bool {
	if t.iterationKeys == nil {
		if _, ok := t.hash[k]; !ok {
			l.runtimeError("invalid key to 'next'")
		}
		t.snapshotKeys()
	}
	if j := t.iterationNext - 1; 0 <= j && j < len(t.iterationKeys) && t.iterationKeys[j] == k {
		return l.nextMapKey(t, key, j+1)
	}
	for j, hk := range t.iterationKeys {
		if hk == k {
			return l.nextMapKey(t, key, j+1)
		}
	}
	l.runtimeError("invalid key to 'next'")
	return false
}

func (t *table) snapshotKeys() {
	keys := make([]value, 0, len(t.hash))
	for hk := range t.hash {
		keys = append(keys, hk)
	}
	t.iterationKeys, t.iterationNext = keys, 0
}

// nextMapKey pushes the first key from iterationKeys[from:] still present.
func (l *State) nextMapKey(t *table, key, from int) bool {
	for j := from; j < len(t.iterationKeys); j++ {
		hk := t.iterationKeys[j]
		if v := t.hash[hk]; !v.isNil() {
			l.stack[key], l.stack[key+1] = hk, v
			t.iterationNext = j + 1
			return true
		}
	}
	return false // no more elements
}
