package lua

import (
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"unsafe"
)

// Go's collector frees luart's memory. What Lua defines beyond freeing
// memory, weak tables and __gc finalizers, a Lua collection provides, after
// lgc.c's atomic phase: it marks every object reachable from Lua's roots,
// clears weak entries whose objects it did not reach, and runs the
// finalizers of unreached objects, which they resurrect.
//
// A Lua collection runs on collectgarbage "collect" and "step", and
// automatically, once a metatable with __mode or __gc has been set, as C
// Lua paces its collector: when the state has allocated (pause - 100)
// percent (setpause, 200 by default) of the heap the last one kept. It
// checks where C Lua steps its collector: when Lua is called or resumed
// from Go, and at NEWTABLE, CONCAT and CLOSURE.
//
// Differences from C Lua:
//
//   - Objects referenced only from Go memory, outside the registry and the
//     stacks, count as unreachable.
//   - __mode and __gc are read when the metatable is set, as 5.2 reads
//     __gc; a Lua collection also reads __mode as it marks.
//   - The collector is not incremental: a Lua collection marks the whole
//     Lua heap at once.
//   - Finalizers run on a thread of their own, so that a collection never
//     moves the running thread's stack.

// goGCCycles counts Go's collections, so states notice them cheaply.
var goGCCycles atomic.Uint64

type gcSentinel struct{ _ *int } // has a pointer, so it is not a tiny allocation

func init() { armGCSentinel() }

func armGCSentinel() {
	runtime.SetFinalizer(&gcSentinel{}, func(*gcSentinel) {
		goGCCycles.Add(1)
		armGCSentinel()
	})
}

// A GCOption is an operation for GC.
type GCOption int

// Valid GCOption values, as lua_gc's options.
const (
	GCStop         GCOption = iota // stop automatic Lua collections
	GCRestart                      // restart them
	GCCollect                      // run a full collection
	GCCount                        // the Go heap in use, in kilobytes
	GCCountBytes                   // the remainder of GCCount, in bytes
	GCStep                         // do data kilobytes' worth of work; returns 1 if that finished a collection
	GCSetPause                     // set the pause percentage; returns the old
	GCSetStepMul                   // set the step multiplier, which luart does not use; returns the old
	GCSetMajorInc                  // set the major increment, which luart does not use; returns the old
	GCIsRunning                    // 1 if automatic collections are on
	GCGenerational                 // no effect in luart
	GCIncremental                  // no effect in luart
)

// GC controls the collector, as lua_gc does; see the gc.go comment for
// what a collection does in luart. It returns -1 for an invalid option.
//
// http://www.lua.org/manual/5.2/manual.html#lua_gc
func (l *State) GC(what GCOption, data int) int {
	g := l.global
	switch what {
	case GCStop:
		g.gcStopped = true
		g.gcCountBase, g.gcCountAllocatedBase = heapNow()
	case GCRestart:
		g.gcStopped = false
	case GCCollect:
		l.fullCollect()
	case GCStep: // as much work as data kilobytes allocated would pay for
		g.gcStepWork += uint64(max(data, 1)) * 1024 * uint64(max(g.gcStepMul, 0)) / 100
		if g.gcStepWork < heapObjects() {
			return 0
		}
		g.gcStepWork = 0
		l.fullCollect()
		return 1 // a cycle finished
	case GCCount, GCCountBytes:
		n, allocated := heapNow()
		if g.gcStopped { // memory grows until the collector runs again
			n = g.gcCountBase + allocated - g.gcCountAllocatedBase
		}
		if what == GCCount {
			return int(n >> 10)
		}
		return int(n & 0x3ff)
	case GCSetPause:
		old := g.gcPause
		g.gcPause = data
		return old
	case GCSetStepMul:
		old := g.gcStepMul
		g.gcStepMul = data
		return old
	case GCSetMajorInc:
		old := g.gcMajorInc
		g.gcMajorInc = data
		return old
	case GCIsRunning:
		if !g.gcStopped {
			return 1
		}
	case GCGenerational, GCIncremental:
	default:
		return -1
	}
	return 0
}

// fullCollect runs a Lua collection and then Go's, which frees what the
// Lua collection and its finalizers left.
func (l *State) fullCollect() {
	g := l.global
	l.collect()
	runtime.GC()
	g.gcCountBase, g.gcCountAllocatedBase = heapNow()
}

// heapNow returns the bytes of Go's heap in objects, garbage included, and
// the bytes Go has ever allocated, for collectgarbage "count". It reads
// them exactly, stopping the world briefly: runtime/metrics counts small
// objects a span at a time, so hundreds of small allocations can show as
// none, and a count taken after a collection could exceed the one before.
func heapNow() (inUse, allocated uint64) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc, ms.TotalAlloc
}

// readMetric returns a uint64 runtime metric, or 0.
func readMetric(name string) uint64 {
	sample := []metrics.Sample{{Name: name}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

// heapObjects returns the bytes of Go's heap in objects, garbage included.
func heapObjects() uint64 { return readMetric("/memory/classes/heap/objects:bytes") }

// allocatedNow returns the bytes Go has ever allocated.
func allocatedNow() uint64 { return readMetric("/gc/heap/allocs:bytes") }

// readHeap returns the bytes of Go's heap its last collection kept, and
// the bytes it has ever allocated.
func readHeap() (live, allocated uint64) {
	return readMetric("/gc/heap/live:bytes"), allocatedNow()
}

// gcMayBeDue reports, cheaply enough to inline where tables and closures
// are made, whether checkGC has anything to look at: Go has collected
// since it last looked, in a state that uses weak tables or finalizers.
func (g *globalState) gcMayBeDue() bool {
	return g.gcWatch && goGCCycles.Load() != g.gcCycles
}

// newTableAt makes the table for a NEWTABLE instruction whose cache is
// site, after running a Lua collection if one is due, as C Lua checks
// there. A collection does not move the running thread's stack, since
// finalizers run on their own thread, so a caller's frame stays valid.
func (l *State) newTableAt(site *fieldCache, arraySize, hashSize int) *table {
	if l.global.gcMayBeDue() {
		l.checkGC()
	}
	return newTableAt(site, arraySize, hashSize)
}

// checkGC runs a Lua collection if one is due: the state uses weak tables
// or finalizers, and, as C Lua paces its collector, it has allocated
// (pause - 100) percent of the heap the last collection kept. It looks at
// the heap only after Go has collected, so checking costs little.
func (l *State) checkGC() {
	g := l.global
	if !g.gcWatch || g.gcStopped || g.gcBusy {
		return
	}
	if cycles := goGCCycles.Load(); cycles != g.gcCycles {
		g.gcCycles = cycles
		_, allocated := readHeap()
		due := g.gcHeapBase / 100 * uint64(max(g.gcPause-100, 0)) << g.gcBackoff
		if allocated-g.gcAllocatedBase >= due {
			l.collect()
		}
	}
}

// maxGCBackoff bounds how far automatic collections back off: to one in
// 2^maxGCBackoff of their pace.
const maxGCBackoff = 6

// noteMetaTable records what a new metatable mt for v asks of the
// collector: v's finalizer, if mt has __gc, and automatic collections, if
// it has __mode or __gc.
func (l *State) noteMetaTable(v value, mt *table) {
	if mt == nil {
		return
	}
	g := l.global
	if !mt.atString("__mode").isNil() {
		g.gcWatch, g.gcBackoff = true, 0
	}
	if mt.atString("__gc").isNil() {
		return
	}
	g.gcWatch = true
	if t := v.table(); t != nil && !t.finalizable {
		t.finalizable = true
		g.finalizable = append(g.finalizable, v)
		g.gcBackoff = 0
	} else if d := v.userData(); d != nil && !d.finalizable {
		d.finalizable = true
		g.finalizable = append(g.finalizable, v)
		g.gcBackoff = 0
	}
}

// collect runs a Lua collection, and then the finalizers it found due.
func (l *State) collect() {
	g := l.global
	if g.gcBusy {
		return
	}
	g.gcBusy = true
	defer func() { g.gcBusy = false }()
	c := collector{marked: map[unsafe.Pointer]struct{}{}}
	c.mark(objectValue(g.registry))
	for _, mt := range g.metaTables {
		if mt != nil {
			c.mark(objectValue(mt))
		}
	}
	c.mark(threadValue(g.mainThread))
	c.mark(threadValue(l))
	for _, o := range g.toFinalize { // still due, after an error stopped them
		c.mark(o)
	}
	c.propagate()
	c.convergeEphemerons()
	c.clearValues(l) // before finalizers resurrect anything

	// Objects with finalizers that nothing reached are resurrected, with
	// what they reach, to be finalized: newest first, as in C.
	kept := make([]value, 0, len(g.finalizable))
	var dying []value
	for _, o := range g.finalizable {
		if c.isMarked(o) {
			kept = append(kept, o)
		} else {
			dying = append(dying, o)
		}
	}
	g.finalizable = kept
	for _, o := range dying {
		c.mark(o)
	}
	c.propagate()
	c.convergeEphemerons()
	c.clearKeys(l) // resurrected keys stay until they are freed
	c.clearValues(l)
	g.gcHeapBase, g.gcAllocatedBase = readHeap()
	// A collection that finds nothing to do, such as one whose only
	// finalizable objects are the standard files, makes the next wait
	// longer. One that does work, or a new weak table, resets the pace.
	if len(dying) == 0 && c.cleared == 0 && len(c.weakValues)+len(c.ephemerons)+len(c.allWeak) == 0 {
		g.gcBackoff = min(g.gcBackoff+1, maxGCBackoff)
	} else {
		g.gcBackoff = 0
	}
	for i := len(dying) - 1; i >= 0; i-- {
		g.toFinalize = append(g.toFinalize, dying[i])
	}
	g.gcBusy = false
	l.runFinalizers()
}

// runFinalizers calls the __gc metamethods of the objects due, in order.
// An error in one stops them: the rest wait for the next collection.
func (l *State) runFinalizers() {
	g := l.global
	for len(g.toFinalize) > 0 {
		o := g.toFinalize[0]
		g.toFinalize = g.toFinalize[1:]
		l.callFinalizer(o)
	}
}

// callFinalizer calls o's __gc metamethod, if it still has one, without
// hooks, after lgc.c's GCTM. It runs on the state's finalizer thread, not
// on l, so that a collection never moves l's stack: the interpreter keeps
// its frame across NEWTABLE and CLOSURE. An error in it is raised in l.
func (l *State) callFinalizer(o value) {
	tm := l.tagMethodByObject(o, tmGC)
	if !tm.isFunction() {
		return
	}
	g := l.global
	f := g.finalizerThread
	if f == nil {
		f = &State{global: g, nonYieldableCallCount: 1}
		f.initializeStack()
		g.finalizerThread = f
	}
	busy := g.gcBusy
	g.gcBusy = true // no collections
	f.nestedGoCallCount = l.nestedGoCallCount
	base := f.top
	f.push(tm)
	f.push(o)
	err := f.protectedCall(func() { f.call(base, 0, false) }, base, 0)
	g.gcBusy = busy
	msg, ok := f.stack[f.top-1].str()
	clear(f.stack[base:f.top])
	f.top = base
	if err != nil {
		if !ok {
			msg = "no message"
		}
		msg = "error in __gc metamethod (" + msg + ")"
		l.checkStack(1)
		l.push(stringValue(msg))
		l.throw(RuntimeError(msg))
	}
}

// A collector marks the objects a Lua collection reaches.
type collector struct {
	marked     map[unsafe.Pointer]struct{}
	cleared    int // weak entries removed
	gray       []value
	weakValues []*table // reached tables with weak values only
	ephemerons []*table // with weak keys only
	allWeak    []*table // with weak keys and values
}

// collectable reports whether v is an object the collector tracks: not a
// number, boolean, string, light userdata or Go function, which C Lua
// never removes from weak tables either.
func collectable(v value) bool {
	switch v.kind() {
	case vkTable, vkLuaClosure, vkGoClosure, vkUserData, vkThread:
		return true
	}
	return false
}

func (c *collector) isMarked(v value) bool {
	if !collectable(v) {
		return true
	}
	_, ok := c.marked[v.p]
	return ok
}

func (c *collector) mark(v value) {
	if !c.isMarked(v) {
		c.marked[v.p] = struct{}{}
		c.gray = append(c.gray, v)
	}
}

// propagate traverses the objects marked but not yet traversed.
func (c *collector) propagate() {
	for len(c.gray) > 0 {
		v := c.gray[len(c.gray)-1]
		c.gray = c.gray[:len(c.gray)-1]
		switch v.kind() {
		case vkTable:
			c.traverseTable(v.table())
		case vkLuaClosure:
			for _, uv := range v.luaClosure().upValues {
				if uv != nil {
					c.mark(uv.value())
				}
			}
		case vkGoClosure:
			for _, u := range v.goClosure().upValues {
				c.mark(u)
			}
		case vkUserData:
			d := v.userData()
			if d.metaTable != nil {
				c.mark(objectValue(d.metaTable))
			}
			if d.env != nil {
				c.mark(objectValue(d.env))
			}
		case vkThread:
			th := v.thread()
			for _, s := range th.stack[:th.top] {
				c.mark(s)
			}
		}
	}
}

// traverseTable marks t's metatable and what its weakness lets it keep.
func (c *collector) traverseTable(t *table) {
	weakKeys, weakValues := false, false
	if mt := t.metaTable; mt != nil {
		c.mark(objectValue(mt))
		if mode, ok := mt.atString("__mode").str(); ok {
			weakKeys, weakValues = strings.Contains(mode, "k"), strings.Contains(mode, "v")
		}
	}
	switch {
	case weakKeys && weakValues:
		c.allWeak = append(c.allWeak, t)
	case weakValues:
		c.weakValues = append(c.weakValues, t)
		t.forEach(func(k, v value) { c.mark(k) })
	case weakKeys: // an ephemeron table: a value lives while its key does
		c.ephemerons = append(c.ephemerons, t)
		t.forEach(func(k, v value) {
			if c.isMarked(k) {
				c.mark(v)
			}
		})
	default:
		t.forEach(func(k, v value) {
			c.mark(k)
			c.mark(v)
		})
	}
}

// convergeEphemerons marks the values of ephemeron tables whose keys are
// marked, until marking them reaches no more keys.
func (c *collector) convergeEphemerons() {
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(c.ephemerons); i++ { // propagate may add some
			c.ephemerons[i].forEach(func(k, v value) {
				if c.isMarked(k) && !c.isMarked(v) {
					c.mark(v)
					changed = true
				}
			})
			c.propagate()
		}
	}
}

// clearValues removes the entries of weak-valued tables whose values the
// collection did not reach.
func (c *collector) clearValues(l *State) {
	for _, tables := range [][]*table{c.weakValues, c.allWeak} {
		for _, t := range tables {
			c.cleared += t.removeWhere(l, func(k, v value) bool { return !c.isMarked(v) })
		}
	}
}

// clearKeys removes the entries of weak-keyed tables whose keys the
// collection did not reach.
func (c *collector) clearKeys(l *State) {
	for _, tables := range [][]*table{c.ephemerons, c.allWeak} {
		for _, t := range tables {
			c.cleared += t.removeWhere(l, func(k, v value) bool { return !c.isMarked(k) })
		}
	}
}

// forEach calls f with each of t's entries.
func (t *table) forEach(f func(k, v value)) {
	for i, v := range t.array {
		if !v.isNil() {
			f(numberValue(float64(i+1)), v)
		}
	}
	if t.shape != nil {
		for i, v := range t.slots {
			if !v.isNil() {
				f(t.shape.keys[i], v)
			}
		}
	}
	for k, v := range t.hash {
		f(k.value(), v)
	}
}

// removeWhere removes t's entries for which dead reports true, and returns
// how many it removed.
func (t *table) removeWhere(l *State, dead func(k, v value) bool) int {
	var keys []value
	t.forEach(func(k, v value) {
		if dead(k, v) {
			keys = append(keys, k)
		}
	})
	for _, k := range keys {
		t.put(l, k, nilValue)
	}
	return len(keys)
}
