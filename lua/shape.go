package lua

// A shape maps a table's string keys to slots in the table's slots slice.
//
// Tables that gain the same string keys in the same order share one shape,
// so instructions can cache a (shape, slot) pair and read a field with one
// slice access. Shared shapes never change: adding a key moves the table to
// the child shape for that key. Removing a key leaves its slot nil.
//
// A table with many keys, or whose shape has too many children, gets a
// private dictionary shape instead, which it changes in place. Renumbering
// a dictionary's slots replaces its shape, which invalidates cached slots.
type shape struct {
	slots map[string]int32 // key to slot
	keys  []value          // slot to key, as a string value
	next  map[string]*shape
	dict  bool
}

const (
	maxSharedShapeKeys = 32 // keys beyond this make a table a dictionary
	maxShapeChildren   = 64 // children beyond this make new tables dictionaries
)

func newRootShape() *shape { return &shape{} }

func (s *shape) slot(k string) (int32, bool) {
	i, ok := s.slots[k]
	return i, ok
}

// with returns the shape for s plus key k, whose value will live in slot
// len(s.keys). Dictionary shapes change in place.
func (s *shape) with(k value, key string) *shape {
	if s.dict {
		s.slots[key] = int32(len(s.keys))
		s.keys = append(s.keys, k)
		return s
	}
	if c, ok := s.next[key]; ok {
		return c
	}
	if len(s.keys) >= maxSharedShapeKeys || len(s.next) >= maxShapeChildren {
		d := s.dictionary()
		return d.with(k, key)
	}
	c := &shape{slots: make(map[string]int32, len(s.keys)+1), keys: make([]value, len(s.keys), len(s.keys)+1)}
	for kk, i := range s.slots {
		c.slots[kk] = i
	}
	copy(c.keys, s.keys)
	c.slots[key] = int32(len(s.keys))
	c.keys = append(c.keys, k)
	if s.next == nil {
		s.next = make(map[string]*shape)
	}
	s.next[key] = c
	return c
}

// dictionary returns a private, mutable copy of s.
func (s *shape) dictionary() *shape {
	d := &shape{slots: make(map[string]int32, len(s.keys)+1), keys: make([]value, len(s.keys)), dict: true}
	for k, i := range s.slots {
		d.slots[k] = i
	}
	copy(d.keys, s.keys)
	return d
}
