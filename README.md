[![ci](https://github.com/matjam/luart/actions/workflows/ci.yml/badge.svg)](https://github.com/matjam/luart/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/matjam/luart.svg)](https://pkg.go.dev/github.com/matjam/luart)

# luart

luart ("Lua RT") is a Lua 5.2 VM in pure Go, built for real-time use such as
per-frame scripts in games, visualisers and audio tools. It is a fork of
[Shopify/go-lua](https://github.com/Shopify/go-lua).

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
| API | go-lua's API, under the module path `github.com/matjam/luart` |

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

Inherited from go-lua:

- Most core libraries are implemented. The main gaps are regular
  expressions, coroutines and `string.dump`.
- Weak tables are not supported. Go's `weak` package (Go 1.24) could make
  them possible.

## Performance

[`bench/suite_test.go`](bench/suite_test.go) runs ten workloads in native
Go, luart without and with the JIT, and Shopify/go-lua, with the same Lua
source for every interpreter. `TestSuiteAgrees` checks that all of them
compute the same result. AMD Ryzen 9 9900X3D, linux/amd64, Go 1.27.1,
`CGO_ENABLED=0`, medians of 6 runs; [`bench/README.md`](bench/README.md)
also has Apple M1 results.

![How many times slower than native Go each interpreter runs each workload](bench/suite-amd64.svg)

| Workload | Native Go | Luart (no JIT) | Luart (JIT) | go-lua | Luart (no JIT) vs Go | Luart (JIT) vs Go | go-lua vs Go |
|---|---|---|---|---|---|---|---|
| fib(25), recursive calls | 0.22 ms | 5.64 ms | 1.60 ms | 9.29 ms | 25× slower | 7.2× slower | 42× slower |
| numeric loop, 1M iterations | 0.78 ms | 8.47 ms | 0.99 ms | 189 ms | 11× slower | 1.3× slower | 242× slower |
| array fill and sum, 100k | 0.50 ms | 2.67 ms | 1.12 ms | 6.41 ms | 5.4× slower | 2.3× slower | 13× slower |
| records, 10k tables | 0.09 ms | 0.90 ms | 0.68 ms | 3.07 ms | 9.7× slower | 7.3× slower | 33× slower |
| closures, 100k | 0.22 ms | 5.33 ms | 4.62 ms | 9.59 ms | 24× slower | 21× slower | 43× slower |
| sort 10k with comparator | 1.28 ms | 3.63 ms | 3.55 ms | 9.73 ms | 2.8× slower | 2.8× slower | 7.6× slower |
| string build, 10k pieces | 0.37 ms | 0.72 ms | 0.60 ms | 59.4 ms | 1.9× slower | 1.6× slower | 161× slower |
| calls into Go, 100k | 0.22 ms | 1.71 ms | 1.26 ms | 5.42 ms | 7.7× slower | 5.7× slower | 25× slower |
| plasma frame | 0.29 ms | 1.35 ms | 0.74 ms | 4.15 ms | 4.6× slower | 2.5× slower | 14× slower |
| particles frame | 0.005 ms | 0.26 ms | 0.10 ms | 1.20 ms | 49× slower | 19× slower | 230× slower |

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
`WithJIT()` remains for code written when the JIT was opt-in.

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

import lua "github.com/matjam/luart"

func main() {
	l := lua.NewState()
	lua.OpenLibraries(l)
	if err := lua.DoFile(l, "hello.lua"); err != nil {
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
