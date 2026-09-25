[![ci](https://github.com/matjam/luart/actions/workflows/ci.yml/badge.svg)](https://github.com/matjam/luart/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/matjam/luart/lua.svg)](https://pkg.go.dev/github.com/matjam/luart/lua)

# luart

luart ("Lua RT") is a Lua 5.2 VM in pure Go, built for real-time use such as
per-frame scripts in games, visualisers and audio tools. It is a fork of
[Shopify/go-lua](https://github.com/Shopify/go-lua).

## Why

I embed Lua in Go programs I write. go-lua is stable, but it no longer
receives updates, and it leaves a lot of performance on the table. luart is
also an experiment: I wanted to see how far I could push an interpreter with
generative AI doing the engineering, from modernising the code to writing a
JIT compiler. Quite far, it turns out.

## Goals

- **Real-time performance.** Fix the interpreter's obvious performance
  problems and avoid allocation wherever possible, so a script can run every
  frame without putting pressure on the garbage collector.
- **Modern Go.** Bring the code up to Go 1.27 idioms and use newer language
  features, such as generic methods, where they improve the API.

A newer Lua version may follow later. It comes second to performance and
modernisation.

## Status

| | |
|---|---|
| Lua version | 5.2, compatible with `luac` 5.2 binary chunks |
| Go | 1.27.1 or later, `CGO_ENABLED=0` |
| API | Methods on `*lua.State`, following Lua's C API and auxiliary library, in `github.com/matjam/luart/lua`; standard libraries in `github.com/matjam/luart/stdlib` |

Work so far:

- Go 1.27 modernisation (`any`, range-over-int, builtin `min`/`max`/`clear`)
- `switch` dispatch in place of go-lua's closure jump table
- Generic accessors `(*State).UserData[T]` and `(*State).CheckUserData[T]`
- Exact constant folding of `10^n` on current Go
- Two-word values: a pointer and a float64, so numbers and booleans never
  allocate and stacks and tables are a third smaller
- Lua's floored `%` on the interpreter's fast path (go-lua truncated)
- One allocation per small table, closure and captured upvalue; table
  constructors start in the shape their previous table reached
- Direct calls and returns between Lua functions, and direct calls into Go
- Generic argument accessor `(*State).Arg[T]`
- Number functions: `math.*` and `(*State).PushNumberFunction[F]` run
  without a call frame
- Table shapes: tables that gain the same string keys in the same order
  share a key-to-slot layout, and each table holds only a slice of values
- Inline caches: field reads, writes, method lookups through `__index`
  tables, and globals with constant names cache their slot per instruction
- Arithmetic specialised at load time for register and constant operands
- `pairs` is linear, visits string keys in insertion order, and allows
  clearing fields during traversal (go-lua was quadratic and raised
  "invalid key to 'next'")
- Error messages name the variable or function, as C Lua does (go-lua read
  the wrong instruction)
- Line and call hooks work (go-lua crashed)
- Assigning nil to an existing field no longer calls `__newindex`
- Lua patterns (`string.find`, `match`, `gmatch` and `gsub`), ported from
  Lua 5.2's lstrlib.c and checked by the Lua test suite's pm.lua, and
  `string.dump`
- Coroutines, as C Lua 5.2 implements them: yields across `pcall`,
  metamethods and iterators, from compiled code too, without a goroutine
  per coroutine
- Weak tables and `__gc` finalizers, checked by the test suite's gc.lua
- The rest of the standard library go-lua lacked: `io.read` and
  `io.lines`, `io.popen`, `os.date`, `debug.getinfo`, `getlocal` and
  `setlocal`, `package.cpath`

Differences from C Lua 5.2:

- There is only the C locale, and C modules cannot load: luart has no
  dynamic libraries.
- Debug information calls Go functions `Go`, not `C`, unless
  `LUART_GO_AS_C=1` is set.
- Go's collector frees memory. Weak tables and `__gc` finalizers come
  from a Lua collection that marks the Lua heap, clears weak entries and
  runs finalizers. It runs on `collectgarbage("collect")` or `"step"`,
  and automatically, paced as C Lua paces its collector, in states that
  use weak tables or finalizers. It is not incremental, and an object
  that only Go memory refers to, outside the registry and the stacks,
  counts as garbage. `collectgarbage` cannot stop or tune Go's
  collector, which serves the whole process; `"count"` reports the Go
  heap.

## Performance

[`bench/`](bench) runs the same Lua source in luart with and without the
JIT, Shopify/go-lua, C Lua 5.4 and LuaJIT, and checks that they all
compute the same results. AMD Ryzen 9 9900X3D, linux/amd64, Go 1.27.1,
medians of 6 runs; [`bench/README.md`](bench/README.md) has the details
and Apple M1 results.

### Standard benchmarks

The 14 benchmarks of [Are We Fast Yet](https://github.com/smarr/are-we-fast-yet)
and three from the Computer Language Benchmarks Game, compared with C
Lua 5.4. With the JIT, luart takes 0.80 times as long as C Lua 5.4 on the
geometric mean: it is faster on 12 of the 17, within 5% on three more,
and slower on Json and binary-trees. go-lua does not finish Havlak in ten
minutes.

![Each interpreter's time on each standard benchmark divided by C Lua 5.4's](bench/standard-amd64.svg)

<!-- suite-table standard-amd64 -->
| Benchmark | Lua 5.4 | Luart (JIT) | Luart (no JIT) | go-lua | LuaJIT |
|---|---:|---:|---:|---:|---:|
| Bounce | 0.29 ms | 0.16 ms (0.55×) | 0.53 ms (1.8×) | 2.16 ms (7.5×) | 0.03 ms (0.10×) |
| CD | 36.9 ms | 38.3 ms (1.0×) | 45.3 ms (1.2×) | 156 ms (4.2×) | 14.2 ms (0.38×) |
| DeltaBlue | 20.9 ms | 20.1 ms (0.96×) | 31.6 ms (1.5×) | 1459 ms (70×) | 9.58 ms (0.46×) |
| Havlak | 1728 ms | 1750 ms (1.0×) | 2105 ms (1.2×) | – | 1036 ms (0.60×) |
| Json | 4.31 ms | 5.80 ms (1.3×) | 7.74 ms (1.8×) | 20.8 ms (4.8×) | 0.88 ms (0.20×) |
| List | 0.22 ms | 0.17 ms (0.76×) | 0.44 ms (2.0×) | 1.07 ms (4.9×) | 0.06 ms (0.29×) |
| Mandelbrot | 129 ms | 110 ms (0.85×) | 212 ms (1.6×) | 679 ms (5.3×) | 23.7 ms (0.18×) |
| NBody | 1.17 ms | 0.70 ms (0.60×) | 2.20 ms (1.9×) | 11.4 ms (9.7×) | 0.08 ms (0.07×) |
| Permute | 0.38 ms | 0.24 ms (0.62×) | 0.98 ms (2.6×) | 2.47 ms (6.5×) | 0.02 ms (0.04×) |
| Queens | 0.29 ms | 0.17 ms (0.60×) | 0.64 ms (2.2×) | 1.36 ms (4.7×) | 0.04 ms (0.14×) |
| Richards | 16.0 ms | 16.0 ms (1.0×) | 26.8 ms (1.7×) | 99.0 ms (6.2×) | 5.47 ms (0.34×) |
| Sieve | 0.12 ms | 0.09 ms (0.76×) | 0.29 ms (2.5×) | 0.60 ms (5.2×) | 0.02 ms (0.15×) |
| Storage | 0.81 ms | 0.61 ms (0.75×) | 0.87 ms (1.1×) | 3.18 ms (3.9×) | 0.33 ms (0.41×) |
| Towers | 0.75 ms | 0.48 ms (0.64×) | 1.49 ms (2.0×) | 4.26 ms (5.7×) | 0.09 ms (0.12×) |
| binary-trees | 119 ms | 129 ms (1.1×) | 158 ms (1.3×) | 216 ms (1.8×) | 34.8 ms (0.29×) |
| fannkuch-redux | 68.6 ms | 63.6 ms (0.93×) | 182 ms (2.6×) | 316 ms (4.6×) | 17.8 ms (0.26×) |
| spectral-norm | 35.8 ms | 20.7 ms (0.58×) | 65.8 ms (1.8×) | 186 ms (5.2×) | 1.29 ms (0.04×) |
| **geometric mean** |  | **0.80×** | **1.8×** | **6.0×** | **0.18×** |
<!-- /suite-table -->

### Embedding workloads

Ten workloads chosen for what an embedded Lua does, each also written in
native Go, which the interpreters are compared with.

![Each interpreter's time on each workload divided by native Go's](bench/suite-amd64.svg)

<!-- suite-table amd64 -->
| Workload | Native Go | Luart (JIT) | Luart (no JIT) | go-lua | Lua 5.4 | LuaJIT |
|---|---:|---:|---:|---:|---:|---:|
| fib(25), recursive calls | 0.21 ms | 1.56 ms (7.3×) | 5.67 ms (27×) | 9.11 ms (43×) | 2.32 ms (11×) | 0.28 ms (1.3×) |
| numeric loop, 1M iterations | 0.79 ms | 0.95 ms (1.2×) | 8.65 ms (11×) | 186 ms (236×) | 4.52 ms (5.7×) | 0.78 ms (0.99×) |
| array fill and sum, 100k | 0.48 ms | 1.16 ms (2.4×) | 2.66 ms (5.5×) | 6.01 ms (13×) | 0.75 ms (1.6×) | 0.20 ms (0.41×) |
| records, 10k tables | 0.09 ms | 0.61 ms (6.8×) | 0.87 ms (9.6×) | 2.73 ms (30×) | 0.87 ms (9.7×) | 0.28 ms (3.1×) |
| closures, 100k | 0.22 ms | 4.57 ms (20×) | 5.25 ms (24×) | 9.79 ms (44×) | 6.77 ms (30×) | 3.46 ms (15×) |
| sort 10k with comparator | 1.28 ms | 3.61 ms (2.8×) | 3.71 ms (2.9×) | 11.6 ms (9.1×) | 3.28 ms (2.6×) | 3.38 ms (2.6×) |
| string build, 10k pieces | 0.37 ms | 0.57 ms (1.5×) | 0.89 ms (2.4×) | 77.6 ms (210×) | 0.96 ms (2.6×) | 0.27 ms (0.73×) |
| calls into Go, 100k | 0.22 ms | 1.29 ms (5.8×) | 1.79 ms (8.0×) | 5.31 ms (24×) | 1.11 ms (5.0×) | 0.69 ms (3.1×) |
| plasma frame | 0.30 ms | 0.75 ms (2.5×) | 1.39 ms (4.7×) | 4.17 ms (14×) | 1.48 ms (5.0×) | 0.54 ms (1.8×) |
| particles frame | 0.005 ms | 0.08 ms (16×) | 0.26 ms (50×) | 1.28 ms (246×) | 0.14 ms (27×) | 0.03 ms (5.6×) |
| **geometric mean** |  | **4.5×** | **9.3×** | **44×** | **6.5×** | **2.1×** |
<!-- /suite-table -->

- luart allocates nothing on fib, the numeric loop, calls into Go, plasma
  and particles, where go-lua allocates 25,000 to 4.9 million times per
  run. Its remaining allocations are the objects the script creates, one
  per table, closure or string. [`bench/README.md`](bench/README.md) has
  the full allocation table.
- go-lua's numeric loop is slow because its `%` calls `math.Mod`. Its
  string build is quadratic because its `table.concat` appends with
  `s += str`.
- The gap to Go is widest where Go inlines calls, as in particles and fib.
- Plasma is a 200×100 per-pixel effect with three `math.sin` calls and one
  call into Go per pixel. Particles moves 2,000 particle tables by a method
  and draws them.
- `TestNumericFrameDoesNotAllocate` keeps numeric code and calls into Go
  allocation-free.

## JIT

`lua.NewState()` compiles hot Lua functions to machine code.
`lua.NewState(lua.WithoutJIT())` makes a state that only interprets, and
the environment variable `LUART_JIT=off` does that for every state.

- Platforms: linux and darwin on arm64 and amd64. Elsewhere, Windows
  included, states interpret.
- It compiles arithmetic, comparisons and branches, loops, upvalues,
  table fields (through the interpreter's inline caches) and arrays, calls
  and returns between compiled Lua functions, and `math.floor`, `ceil`,
  `sqrt`, `abs`, `sin` and `cos` inline, bit for bit as Go computes them
  (on amd64, `floor` and `ceil` need SSE4.1, and `sin` and `cos` a
  GOAMD64 level below v3).
  Numeric loops run with their variables in registers.
- Compiled code shares the interpreter's stack frames. An instruction it
  cannot run, or a Go call, returns to Go and continues in compiled code
  after it, so every script runs correctly.
- It gains least where a script crosses into Go every few instructions,
  such as creating closures in a loop or a `table.sort` comparator; see
  [bench](bench/README.md).
- It pauses while a debug hook is set.
- CI runs the whole test suite with the JIT, with every function compiled
  (`LUART_JIT_TEST=1`) on linux/amd64, linux/arm64 and macOS, and
  interpreted (`LUART_JIT=off`).

## Usage

```sh
go get github.com/matjam/luart
```

```go
package main

import (
	"github.com/matjam/luart/lua"
	"github.com/matjam/luart/stdlib"
)

func main() {
	l := lua.NewState()
	stdlib.Open(l)
	if err := l.DoFile("hello.lua"); err != nil {
		panic(err)
	}
}
```

Functions over numbers can skip the call frame entirely. The VM calls them
directly when every argument is a number:

```go
l.RegisterNumberFunction("set", func(x, y, v float64) {
	canvas[int(y)*width+int(x)] = v
})
```

Go functions can read typed arguments, which raise a Lua error when an
argument does not convert:

```go
l.Register("rect", func(l *lua.State) int {
	x, y, label := l.Arg[float64](1), l.Arg[float64](2), l.Arg[string](3)
	draw(x, y, label)
	return 0
})
```

Userdata can be read back with its Go type:

```go
type point struct{ x, y float64 }

l.Register("norm", func(l *lua.State) int {
	p := l.CheckUserData[*point](1, "point")
	l.PushNumber(math.Hypot(p.x, p.y))
	return 1
})
```

## Development

```sh
git submodule update --init   # lua-tests
go test ./...
cd bench && go test -bench . -benchmem
```

The parser and dump tests compare against `luac` 5.2 and skip when it is not
installed. CI installs it.

## Licence

MIT, with Shopify's original go-lua copyright retained. See [LICENSE](LICENSE).
