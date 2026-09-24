package lua

import (
	"math"
)

// table is a Lua table. Integer keys from 1 to len(array) live in array.
// Other string keys live in strs, which uses Go's fast string map, and all
// remaining keys live in hash. Both maps are allocated on first write.
type table struct {
	array         []value
	strs          map[string]value
	hash          map[value]value
	metaTable     *table
	flags         byte
	iterationKeys []value // snapshot of map keys for next
	iterationNext int     // index in iterationKeys after the key next last returned
}

func newTable() *table                     { return &table{} }
func (t *table) invalidateTagMethodCache() { t.flags = 0 }
func (t *table) atString(k string) value   { return t.strs[k] }

func newTableWithSize(arraySize, hashSize int) *table {
	t := new(table)
	if arraySize > 0 {
		t.array = make([]value, arraySize)
	}
	if hashSize > 0 {
		t.strs = make(map[string]value, hashSize)
	}
	return t
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

// hasKey reports whether key k, which is not in the array part, is present.
func (t *table) hasKey(k value) bool {
	if s, ok := k.str(); ok {
		_, ok = t.strs[s]
		return ok
	}
	_, ok := t.hash[k]
	return ok
}

// addOrInsert stores a non-nil v at a key outside the array part.
func (t *table) addOrInsert(k, v value) {
	if s, ok := k.str(); ok {
		if t.strs == nil {
			t.strs = make(map[string]value)
		}
		if _, ok := t.strs[s]; !ok {
			t.iterationKeys = nil // invalidate iterations when adding an entry
		}
		t.strs[s] = v
		return
	}
	if t.hash == nil {
		t.hash = make(map[value]value)
	}
	if _, ok := t.hash[k]; !ok {
		t.iterationKeys = nil
	}
	t.hash[k] = v
}

func (t *table) remove(k value) {
	if s, ok := k.str(); ok {
		delete(t.strs, s)
	} else {
		delete(t.hash, k)
	}
}

func (t *table) putAtInt(k int, v value) {
	if 0 < k && k <= len(t.array) {
		t.array[k-1] = v
	} else if k > 0 && !v.isNil() && t.maybeResizeArray(k) {
		t.array[k-1] = v
	} else if v.isNil() {
		delete(t.hash, numberValue(float64(k)))
	} else {
		t.addOrInsert(numberValue(float64(k)), v)
	}
}

func (t *table) at(k value) value {
	switch o := k.o.(type) {
	case nil:
		return nilValue
	case string:
		return t.strs[o]
	case *numberTag:
		if i := int(k.n); float64(i) == k.n && 0 < i && i <= len(t.array) { // OPT: Inlined copy of atInt.
			return t.array[i-1]
		}
	}
	return t.hash[k]
}

func (t *table) put(l *State, k, v value) {
	switch k.o.(type) {
	case nil:
		l.runtimeError("table index is nil")
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
		t.remove(k)
	} else {
		t.addOrInsert(k, v)
	}
}

// OPT: tryPut is an optimized variant of the at/put pair used by setTableAt to avoid hashing the key twice.
func (t *table) tryPut(l *State, k, v value) bool {
	switch o := k.o.(type) {
	case nil:
		return false
	case string:
		if old, ok := t.strs[o]; ok && !old.isNil() && !v.isNil() {
			t.strs[o] = v
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
	if old, ok := t.hash[k]; ok && !old.isNil() && !v.isNil() {
		t.hash[k] = v
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
// and reports whether there was one. Map keys come from a snapshot taken when
// iteration leaves the array part, so fields cleared during a traversal are
// still found, as Lua allows.
func (l *State) next(t *table, key int) bool {
	i, k := 0, l.stack[key]
	inArray := k.isNil()
	if !inArray {
		if i = arrayIndex(k); 0 < i && i <= len(t.array) {
			inArray = true
		}
	}
	if inArray {
		for ; i < len(t.array); i++ {
			if !t.array[i].isNil() {
				l.stack[key] = numberValue(float64(i + 1))
				l.stack[key+1] = t.array[i]
				return true
			}
		}
		if t.iterationKeys == nil {
			t.snapshotKeys()
		}
		return l.nextMapKey(t, key, 0)
	}
	if t.iterationKeys == nil {
		if !t.hasKey(k) {
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
	keys := make([]value, 0, len(t.strs)+len(t.hash))
	for s := range t.strs {
		keys = append(keys, stringValue(s))
	}
	for hk := range t.hash {
		keys = append(keys, hk)
	}
	t.iterationKeys, t.iterationNext = keys, 0
}

// nextMapKey pushes the first key from iterationKeys[from:] still present.
func (l *State) nextMapKey(t *table, key, from int) bool {
	for j := from; j < len(t.iterationKeys); j++ {
		hk := t.iterationKeys[j]
		if v := t.at(hk); !v.isNil() {
			l.stack[key], l.stack[key+1] = hk, v
			t.iterationNext = j + 1
			return true
		}
	}
	return false // no more elements
}
