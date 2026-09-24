# Embedded scripting runtimes for EncomPlayer visualisers

Which pure-Go scripting runtime should run user-written visualisers in
EncomPlayer, and how much can a script do per frame? The build must stay
`CGO_ENABLED=0`.

## Summary

- The pure-Go Lua runtimes are 17–27× slower than native Go on a per-pixel
  workload, and within 1.6× of each other.
- The interpreter's own work sets the cost. Removing the per-pixel call into
  Go made no difference outside the noise.
- A full-screen per-pixel effect costs 5.7–9.0 ms per frame in Lua. That is
  17–27% of a 30 fps frame budget, for a single visualiser.
- Visualisers that draw tens to hundreds of shapes per frame cost well
  under 1 ms on any runtime.
- gopher-lua allocates 1.8 MiB per frame, arnodel/golua 68 bytes. At 30 fps
  that is about 55 MiB/s of garbage against almost none.
- Recommendation: write per-pixel visualisers in Go, and use Lua for the
  lighter ones.

## Setup

| | |
|---|---|
| Machine | Apple M1 Pro, macOS (darwin/arm64) |
| Go | 1.27.1, `CGO_ENABLED=0` |
| Method | `go test -bench . -benchmem -benchtime 1s -count 6`, summarised with benchstat |
| Date | 2026-09-24 |

| Runtime | Version | Language |
|---|---|---|
| [yuin/gopher-lua](https://github.com/yuin/gopher-lua) | v1.1.2 | Lua 5.1 |
| [Shopify/go-lua](https://github.com/Shopify/go-lua) | v0.0.0-20250718183320-1e37f32ad7d0 | Lua 5.2 |
| [arnodel/golua](https://github.com/arnodel/golua) | v0.3.0 | Lua 5.4 |
| [dop251/goja](https://github.com/dop251/goja) | v0.0.0-20260917113740-793a2a65c13b | JavaScript, for comparison |

## Workload

One frame of a full-screen plasma effect. The canvas is 200×100 half-block
pixels, about what a large terminal shows. Each pixel sums three `sin` calls
and calls a Go function `set(x, y, v)` to store the result:

```lua
local sin = math.sin
function frame(t)
  for y = 0, 99 do
    for x = 0, 199 do
      set(x, y, sin(x*0.1+t) + sin(y*0.07+t) + sin((x+y)*0.05+t))
    end
  end
end
```

The "no call" variant runs the same maths but stores each result in a Lua
table, so it measures the interpreter without the calls into Go.

## Results

| Runtime | Time per frame | vs Go | Memory per frame | Allocations per frame |
|---|---|---|---|---|
| Native Go | 0.33 ms ± 1% | 1× | 0 | 0 |
| Shopify/go-lua | 5.74 ms ± 3% | 17× | 2.14 MiB | 280,000 |
| gopher-lua | 6.80 ms ± 2% | 21× | 1.84 MiB | 65,700 |
| arnodel/golua | 8.97 ms ± 1% | 27× | 68 B | 1 |
| goja | 17.9 ms ± 9% | 55× | 4.99 MiB | 474,500 |

| Runtime, no call into Go | Time per frame | Change from above |
|---|---|---|
| gopher-lua | 6.57 ms ± 4% | −3%, within noise |
| Shopify/go-lua | 6.17 ms ± 10% | +7%, within noise |

The raw output is in [`results.txt`](results.txt). `BenchmarkLuart` and
`BenchmarkLuartNoCall` run the same workload against this repository through
a `replace` directive.

## Findings

### The interpreter's work dominates

Removing 20,000 calls into Go per frame changed neither Lua runtime beyond
the noise. The cost lies in running bytecode, so batching canvas writes or
passing buffers would not bring a per-pixel script near Go's speed.

### The spread between pure-Go Lua runtimes is small

The fastest (Shopify) and the slowest (arnodel) are 1.6× apart. None of them
changes which effects are practical in Lua.

### Allocation behaviour differs widely

arnodel/golua allocates almost nothing per frame. gopher-lua and Shopify
allocate hundreds of thousands of objects per frame, probably from boxing
float values. The cause is inferred from the numbers, not profiled. At 30 fps
gopher-lua produces about 55 MiB/s of garbage. Go's collector handles that
with short pauses, but it costs CPU time alongside audio decoding.

### goja is not competitive here

goja pays for reflection when calling a plain Go function from JavaScript.
It would do better with its native `FunctionCall` signature, but it is also a
different language from the one requested.

## Implications for EncomPlayer

| Kind of visualiser | Work per frame | Where it belongs |
|---|---|---|
| Per-pixel: plasma, fire, tunnel, feedback trails, waterfall, cellular automata | 10,000–20,000 pixels | Go |
| Every-sample scopes: oscilloscope, vectorscope | 2,048 samples per channel | Go |
| Shape-based: bars, VU needles, radial spectrum, wireframes, starfields, particles | tens to hundreds of shapes | Lua, well under 1 ms |

### Choosing a Lua runtime

Speed does not decide this, because the Lua visualisers are nowhere near the
budget on any of the three runtimes.

| Runtime | For | Against |
|---|---|---|
| gopher-lua | Most widely used; cancels a script through its context, which gives a per-frame time limit | Lua 5.1; heavy allocation |
| arnodel/golua | Lua 5.4 (integers, bitwise operators); CPU and memory quotas built in; almost no allocation | Slowest of the three; smaller user base |
| Shopify/go-lua | Fastest | Lua 5.2; little recent development; heaviest allocation |

gopher-lua is the conservative choice. arnodel/golua is the stronger choice
if hard per-script memory limits and low garbage matter more than a larger
user base.

## Not measured

C Lua compiled to WebAssembly and run on
[wazero](https://github.com/wazero/wazero) v1.12.0 would keep
`CGO_ENABLED=0`. wazero compiles WebAssembly to native code on amd64 and
arm64, so this is the only route likely to make per-pixel Lua scripts
practical. It would need:

- a `lua.wasm` built with a C toolchain, checked in or built in CI
- C glue exposing the canvas API to Lua
- compiling the module when the player starts
- wazero's much slower interpreter on other CPU architectures

Measuring it first would need a prototype.

## Reproducing

From this directory:

```sh
CGO_ENABLED=0 go test -bench . -benchmem -benchtime 1s -count 6 | tee results.txt
go run golang.org/x/perf/cmd/benchstat@latest results.txt
```
