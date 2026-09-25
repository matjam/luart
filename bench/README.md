# Benchmarks

Two benchmarks run the same Lua source in every interpreter: luart without
the JIT (`WithoutJIT`), luart with it (the default), Shopify/go-lua, and C
Lua 5.4 or LuaJIT when built with their tag (see
[C interpreters](#c-interpreters)).

- **`BenchmarkSuite`** ([`suite_test.go`](suite_test.go)) runs ten
  workloads chosen for what an embedded Lua does: calls, loops, tables,
  closures, sorting, string building and calls into Go. Each is also
  written in native Go, which the interpreters are compared with.
- **`BenchmarkStandard`** ([`standard_test.go`](standard_test.go)) runs the
  14 benchmarks of Are We Fast Yet and three from the Computer Language
  Benchmarks Game, which other language implementations are measured with
  ([`standard/`](standard)). The interpreters are compared with C Lua 5.4.

`TestSuiteAgrees` and `TestStandardAgrees` check that every interpreter
computes the same results. The tables and charts come from the raw output
by [`chart`](chart) (see [Reproducing](#reproducing)).

## amd64

AMD Ryzen 9 9900X3D, linux, Go 1.27.1, `-count 6`,
`-ldflags=-funcalign=64`, medians, 2026-09-25, at commit 72f11fd. Lua 5.4.9
and LuaJIT 2.1.1788460057 from Arch Linux's packages. Raw output:
[`suite-results-amd64.txt`](suite-results-amd64.txt).

### The suite

![Each interpreter's time on each workload divided by native Go's, on amd64](suite-amd64.svg)

Each cell is the median time, and in brackets that time divided by native
Go's.

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

### The standard benchmarks

![Each interpreter's time on each standard benchmark divided by C Lua 5.4's, on amd64](standard-amd64.svg)

Each cell is the median time, and in brackets that time divided by C Lua
5.4's.

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

- luart with the JIT takes 0.80 times as long as C Lua 5.4 on the
  geometric mean. It is faster on 12 of the 17, most on numeric code and
  method calls (spectral-norm, Bounce, Queens and NBody), within 5% on CD,
  Havlak and Richards, and slower on Json (1.3 times) and binary-trees
  (1.1 times).
- Without the JIT, luart takes 1.8 times as long as C Lua 5.4.
- LuaJIT is 5.5 times faster than C Lua 5.4 on the geometric mean.
- go-lua takes six times as long as C Lua 5.4, and 70 times on
  DeltaBlue. It does not finish Havlak in ten minutes, so it has no result
  there, and its geometric mean is of the other 16.

## arm64

Apple M1 Pro, Go 1.27.1, `CGO_ENABLED=0`, `-count 6`,
`-ldflags=-funcalign=64`, medians, 2026-09-24, measured at commit 664f09d,
before the changes to calls between Go and compiled Lua, to `table.sort`,
and to `sin` and `cos` that the amd64 results include, and without the C
interpreters or the standard benchmarks. Raw output:
[`suite-results.txt`](suite-results.txt).

![Each interpreter's time on each workload divided by native Go's, on Apple M1 Pro](suite-arm64-m1.svg)

<!-- suite-table arm64-m1 -->
| Workload | Native Go | Luart (JIT) | Luart (no JIT) | go-lua |
|---|---:|---:|---:|---:|
| fib(25), recursive calls | 0.25 ms | 2.68 ms (11×) | 7.94 ms (32×) | 13.8 ms (55×) |
| numeric loop, 1M iterations | 1.20 ms | 1.83 ms (1.5×) | 14.0 ms (12×) | 294 ms (245×) |
| array fill and sum, 100k | 0.45 ms | 1.63 ms (3.6×) | 4.03 ms (8.9×) | 8.45 ms (19×) |
| records, 10k tables | 0.13 ms | 1.09 ms (8.7×) | 1.29 ms (10×) | 4.23 ms (34×) |
| closures, 100k | 0.35 ms | 9.55 ms (27×) | 7.80 ms (22×) | 13.6 ms (39×) |
| sort 10k with comparator | 1.94 ms | 9.52 ms (4.9×) | 7.41 ms (3.8×) | 14.0 ms (7.2×) |
| string build, 10k pieces | 0.57 ms | 0.99 ms (1.7×) | 1.04 ms (1.8×) | 93.4 ms (165×) |
| calls into Go, 100k | 0.35 ms | 3.21 ms (9.2×) | 2.76 ms (7.9×) | 7.66 ms (22×) |
| plasma frame | 0.30 ms | 1.66 ms (5.5×) | 2.19 ms (7.2×) | 5.70 ms (19×) |
| particles frame | 0.006 ms | 0.22 ms (38×) | 0.36 ms (61×) | 1.59 ms (270×) |
| **geometric mean** |  | **6.9×** | **11×** | **46×** |
<!-- /suite-table -->

## Allocations

The JIT allocates exactly what the interpreter does. Allocations are per
run of each suite workload.

| Workload | luart allocations | go-lua allocations |
|---|---|---|
| fib(25) | 0 | 318,000 |
| numeric loop | 0 | 4,857,000 |
| array fill and sum | 22 | 500,000 |
| records | 10,000 | 120,000 |
| closures | 100,000 | 900,000 |
| sort | 17 | 180,000 |
| string build | 10,000 | 80,000 |
| calls into Go | 0 | 400,000 |
| plasma frame | 0 | 280,000 |
| particles frame | 0 | 25,000 |

## Notes

- Numbers, booleans and calls allocate nothing in luart. The remaining
  allocations are the Lua objects the script creates, one per table,
  closure or string.
- go-lua's numeric loop is slow because its `%` fast path calls
  `math.Mod`, which is far slower than Lua's `a - floor(a/b)*b`.
- go-lua's string build is quadratic: its `table.concat` appends with
  `s += str`. luart's uses a `strings.Builder`.
- The JIT gains least where scripts cross between compiled code and Go
  every few instructions: creating closures, Go calling Lua (the sort
  comparator), and calls into Go. It is fastest on numeric loops and Lua
  calls.
- On amd64 most of plasma's time with the JIT is in compiled code, not in
  its calls to `set`: every Lua register lives in memory, and each store
  checks the write barrier flag in memory.
- `-ldflags=-funcalign=64` keeps the interpreter loop's alignment fixed;
  without it, unrelated changes move timings by 5–10%.
- The native Go versions keep Go's advantages, such as inlined method calls
  in particles, so the gap to Go is widest where Go inlines most.
- Plasma here registers `set` as an ordinary Go function. Registered as a
  number function, which compiled code calls without a Go frame, it takes
  0.71 ms with the JIT (`BenchmarkLuartNumberFunction`), against 0.76 ms
  (`BenchmarkLuart`).
- The C interpreters' calls into Go are calls to C functions, and their
  plasma draws into a C array; the suite's other workloads are the same
  Lua in every interpreter. Their results use integers where Lua 5.4 does,
  which the agreement tests compare as numbers.

## C interpreters

C Lua 5.4 and LuaJIT are linked with cgo, so they need a C compiler and
their development packages, found with pkg-config (`lua5.4` and
`luajit`). Both define the Lua C API, so a test binary can link only one.
Each has a build tag: `clua54` or `luajit`. Without either, the benchmarks
build with `CGO_ENABLED=0` and run luart and go-lua only.

## Reproducing

From this directory, on an idle machine, run luart, go-lua and native Go,
then each C interpreter, into one file:

```sh
CGO_ENABLED=0 go test -run x -bench . -benchmem -count 6 -timeout 3h -ldflags=-funcalign=64 > suite-results-amd64.txt
go test -tags clua54 -run x -bench '.*/.*/lua54$' -benchmem -count 6 -timeout 3h >> suite-results-amd64.txt
go test -tags luajit -run x -bench '.*/.*/luajit$' -benchmem -count 6 -timeout 3h >> suite-results-amd64.txt
go run ./chart -svg suite-amd64.svg -readme README.md,../README.md -name amd64 suite-results-amd64.txt
go run ./chart -suite standard -svg standard-amd64.svg -readme README.md,../README.md -name standard-amd64 suite-results-amd64.txt
```

From the median of each benchmark in the file, `-svg` redraws the chart
and `-readme` rewrites the table between `<!-- suite-table NAME -->` and
`<!-- /suite-table -->` in each file; `-table` prints it instead. Name the
file, charts and tables for the machine; `suite-results.txt`,
`suite-arm64-m1.svg` and `arm64-m1` are Apple M1.
