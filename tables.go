package lua

import (
	"math"
)

type table struct {
	array         []value
	hash          map[value]value
	metaTable     *table
	flags         byte
	iterationKeys []value
}

func newTable() *table                     { return &table{hash: make(map[value]value)} }
func (t *table) invalidateTagMethodCache() { t.flags = 0 }
func (t *table) atString(k string) value   { return t.hash[stringValue(k)] }

func newTableWithSize(arraySize, hashSize int) *table {
	t := new(table)
	if arraySize > 0 {
		t.array = make([]value, arraySize)
	}
	t.hash = make(map[value]value, hashSize)
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

func (t *table) addOrInsertHash(k, v value) {
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
	switch k.o.(type) {
	case nil:
		return nilValue
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
		delete(t.hash, k)
	} else {
		t.addOrInsertHash(k, v)
	}
}

// OPT: tryPut is an optimized variant of the at/put pair used by setTableAt to avoid hashing the key twice.
func (t *table) tryPut(l *State, k, v value) bool {
	switch k.o.(type) {
	case nil:
		return false
	case *numberTag:
		if i := int(k.n); float64(i) == k.n && 0 < i && i <= len(t.array) && !t.array[i-1].isNil() {
			t.array[i-1] = v
			return true
		} else if math.IsNaN(k.n) {
			return false
		}
	}
	if !v.isNil() && !t.hash[k].isNil() {
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

func (l *State) next(t *table, key int) bool {
	i, k := 0, l.stack[key]
	if k.isNil() { // first iteration
	} else if i = arrayIndex(k); 0 < i && i <= len(t.array) {
		k = nilValue
	} else if _, ok := t.hash[k]; !ok {
		l.runtimeError("invalid key to 'next'") // key not found
	} else {
		i = len(t.array)
	}
	for ; i < len(t.array); i++ {
		if !t.array[i].isNil() {
			l.stack[key] = numberValue(float64(i + 1))
			l.stack[key+1] = t.array[i]
			return true
		}
	}
	if t.iterationKeys == nil {
		j, keys := 0, make([]value, len(t.hash))
		for hk := range t.hash {
			keys[j] = hk
			j++
		}
		t.iterationKeys = keys
	}
	found := k.isNil()
	for i, hk := range t.iterationKeys {
		if hk.isNil() { // skip deleted key
		} else if _, present := t.hash[hk]; !present {
			t.iterationKeys[i] = nilValue // mark key as deleted
		} else if found {
			l.stack[key] = hk
			l.stack[key+1] = t.hash[hk]
			return true
		} else if l.equalObjects(hk, k) {
			found = true
		}
	}
	return false // no more elements
}
