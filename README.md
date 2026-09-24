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
- String keys in their own `map[string]value`
- `pairs` is linear and allows clearing fields during traversal (go-lua was
  quadratic and raised "invalid key to 'next'")
- Line hooks work (go-lua indexed before the first instruction)

Inherited from go-lua:

- Most core libraries are implemented. The main gaps are regular
  expressions, coroutines and `string.dump`.
- Weak tables are not supported. Go's `weak` package (Go 1.24) could make
  them possible.

## Performance

[`bench/`](bench/README.md) runs one frame of a 200×100 per-pixel plasma
effect in several pure-Go scripting runtimes. On an Apple M1 Pro:

| Runtime | Time per frame | Memory per frame | Allocations per frame |
|---|---|---|---|
| Native Go | 0.33 ms | 0 | 0 |
| Shopify/go-lua | 5.8 ms | 2.1 MiB | 280,000 |
| luart | 2.8 ms | 5 B | 0 |
| luart, `set` as a number function | 2.7 ms | 0 | 0 |

`TestNumericFrameDoesNotAllocate` keeps numeric code and calls into Go
allocation-free. The remaining cost is instruction dispatch, about 4 ns for
each of the 20 bytecode instructions per pixel.

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
