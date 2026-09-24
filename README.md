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
- Unboxed values: numbers and booleans no longer allocate
- Lua's floored `%` on the interpreter's fast path (go-lua truncated)
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
Go, luart and Shopify/go-lua, with the same Lua source for both
interpreters. `TestSuiteAgrees` checks that all three compute the same
result. Apple M1 Pro, Go 1.27.1, `CGO_ENABLED=0`, medians of 6 runs:

| Workload | Native Go | luart | Shopify/go-lua | luart vs Shopify | luart vs Go |
|---|---|---|---|---|---|
| fib(25), recursive calls | 0.25 ms | 8.99 ms | 13.5 ms | 1.5× faster | 36× slower |
| numeric loop, 1M iterations | 1.17 ms | 14.7 ms | 290 ms | 20× faster | 13× slower |
| array fill and sum, 100k | 0.53 ms | 4.90 ms | 8.40 ms | 1.7× faster | 9× slower |
| records, 10k tables | 0.12 ms | 2.06 ms | 4.07 ms | 2.0× faster | 17× slower |
| closures, 100k | 0.34 ms | 9.54 ms | 13.3 ms | 1.4× faster | 28× slower |
| sort 10k with comparator | 1.92 ms | 7.43 ms | 13.4 ms | 1.8× faster | 3.9× slower |
| string build, 10k pieces | 0.57 ms | 2.09 ms | 97.7 ms | 47× faster | 3.7× slower |
| calls into Go, 100k | 0.34 ms | 3.19 ms | 7.53 ms | 2.4× faster | 9× slower |
| plasma frame | 0.29 ms | 2.56 ms | 5.69 ms | 2.2× faster | 9× slower |
| particles frame | 0.006 ms | 0.42 ms | 1.56 ms | 3.7× faster | 72× slower |

- luart allocates nothing on fib, the numeric loop, calls into Go, plasma
  and particles, where Shopify allocates 25,000 to 4.9 million times per
  run. Its remaining allocations are objects the script creates: tables,
  closures and strings. [`bench/README.md`](bench/README.md) has the full
  allocation table.
- Shopify's numeric loop is slow because its `%` calls `math.Mod`. Its
  string build is quadratic because its `table.concat` appends with
  `s += str`.
- The gap to Go is widest where Go inlines calls, as in particles and fib.
- Plasma is a 200×100 per-pixel effect with three `math.sin` calls and one
  call into Go per pixel. Particles moves 2,000 particle tables by a method
  and draws them. With `set` registered as a number function, plasma takes
  2.3 ms.
- `TestNumericFrameDoesNotAllocate` keeps numeric code and calls into Go
  allocation-free.

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
