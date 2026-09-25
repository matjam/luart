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
`-ldflags=-funcalign=64`, medians, 2026-09-25, at commit 1e9cece. Lua 5.4.9
and LuaJIT 2.1.1788460057 from Arch Linux's packages. Raw output:
[`suite-results-amd64.txt`](suite-results-amd64.txt).

### The suite

![Each interpreter's time on each workload divided by native Go's, on amd64](suite-amd64.svg)

Each cell is the median time, and in brackets that time divided by native
Go's.

<!-- suite-table amd64 -->
| Workload | Native Go | Luart (JIT) | Luart (no JIT) | go-lua | Lua 5.4 | LuaJIT |
|---|---:|---:|---:|---:|---:|---:|
| fib(25), recursive calls | 0.21 ms | 1.57 ms (7.3×) | 5.64 ms (26×) | 9.19 ms (43×) | 2.34 ms (11×) | 0.28 ms (1.3×) |
| numeric loop, 1M iterations | 0.79 ms | 0.95 ms (1.2×) | 8.89 ms (11×) | 185 ms (234×) | 4.53 ms (5.7×) | 0.78 ms (0.99×) |
| array fill and sum, 100k | 0.46 ms | 1.15 ms (2.5×) | 2.60 ms (5.6×) | 6.20 ms (13×) | 0.76 ms (1.7×) | 0.20 ms (0.43×) |
| records, 10k tables | 0.09 ms | 0.62 ms (6.9×) | 0.88 ms (9.8×) | 2.85 ms (32×) | 0.87 ms (9.8×) | 0.28 ms (3.1×) |
| closures, 100k | 0.22 ms | 4.63 ms (21×) | 5.27 ms (24×) | 9.62 ms (43×) | 6.91 ms (31×) | 3.42 ms (15×) |
| sort 10k with comparator | 1.28 ms | 3.60 ms (2.8×) | 3.74 ms (2.9×) | 11.9 ms (9.3×) | 3.27 ms (2.6×) | 3.36 ms (2.6×) |
| string build, 10k pieces | 0.37 ms | 0.58 ms (1.6×) | 0.70 ms (1.9×) | 77.3 ms (209×) | 0.96 ms (2.6×) | 0.31 ms (0.83×) |
| calls into Go, 100k | 0.22 ms | 1.30 ms (5.8×) | 1.77 ms (7.9×) | 5.37 ms (24×) | 1.09 ms (4.8×) | 0.70 ms (3.1×) |
| plasma frame | 0.29 ms | 0.75 ms (2.5×) | 1.39 ms (4.7×) | 4.19 ms (14×) | 1.46 ms (4.9×) | 0.54 ms (1.8×) |
| particles frame | 0.005 ms | 0.09 ms (16×) | 0.26 ms (51×) | 1.21 ms (232×) | 0.15 ms (28×) | 0.03 ms (5.6×) |
| **geometric mean** |  | **4.5×** | **9.1×** | **44×** | **6.5×** | **2.1×** |
<!-- /suite-table -->

### The standard benchmarks

![Each interpreter's time on each standard benchmark divided by C Lua 5.4's, on amd64](standard-amd64.svg)

Each cell is the median time, and in brackets that time divided by C Lua
5.4's.

<!-- suite-table standard-amd64 -->
| Benchmark | Lua 5.4 | Luart (JIT) | Luart (no JIT) | go-lua | LuaJIT |
|---|---:|---:|---:|---:|---:|
| Bounce | 0.29 ms | 0.16 ms (0.56×) | 0.53 ms (1.8×) | 2.11 ms (7.4×) | 0.03 ms (0.10×) |
| CD | 36.7 ms | 41.3 ms (1.1×) | 48.4 ms (1.3×) | 156 ms (4.2×) | 14.3 ms (0.39×) |
| DeltaBlue | 20.9 ms | 32.1 ms (1.5×) | 39.0 ms (1.9×) | 1387 ms (66×) | 9.54 ms (0.46×) |
| Havlak | 1719 ms | 2037 ms (1.2×) | 2303 ms (1.3×) | – | 1042 ms (0.61×) |
| Json | 4.43 ms | 7.73 ms (1.7×) | 9.31 ms (2.1×) | 20.9 ms (4.7×) | 0.91 ms (0.21×) |
| List | 0.21 ms | 0.25 ms (1.2×) | 0.49 ms (2.3×) | 1.06 ms (5.0×) | 0.06 ms (0.30×) |
| Mandelbrot | 129 ms | 232 ms (1.8×) | 212 ms (1.6×) | 677 ms (5.3×) | 23.7 ms (0.18×) |
| NBody | 1.18 ms | 0.70 ms (0.59×) | 2.16 ms (1.8×) | 11.1 ms (9.4×) | 0.08 ms (0.07×) |
| Permute | 0.36 ms | 0.24 ms (0.67×) | 0.99 ms (2.7×) | 2.43 ms (6.7×) | 0.01 ms (0.04×) |
| Queens | 0.29 ms | 0.18 ms (0.62×) | 0.65 ms (2.2×) | 1.36 ms (4.7×) | 0.03 ms (0.12×) |
| Richards | 15.9 ms | 45.9 ms (2.9×) | 47.9 ms (3.0×) | 100 ms (6.3×) | 5.39 ms (0.34×) |
| Sieve | 0.12 ms | 0.09 ms (0.79×) | 0.30 ms (2.5×) | 0.61 ms (5.2×) | 0.02 ms (0.15×) |
| Storage | 0.81 ms | 0.62 ms (0.76×) | 0.88 ms (1.1×) | 3.25 ms (4.0×) | 0.33 ms (0.41×) |
| Towers | 0.78 ms | 0.72 ms (0.92×) | 1.57 ms (2.0×) | 4.16 ms (5.3×) | 0.09 ms (0.11×) |
| binary-trees | 119 ms | 129 ms (1.1×) | 159 ms (1.3×) | 217 ms (1.8×) | 35.4 ms (0.30×) |
| fannkuch-redux | 69.2 ms | 63.5 ms (0.92×) | 182 ms (2.6×) | 319 ms (4.6×) | 17.5 ms (0.25×) |
| spectral-norm | 35.0 ms | 20.7 ms (0.59×) | 66.2 ms (1.9×) | 169 ms (4.8×) | 1.29 ms (0.04×) |
| **geometric mean** |  | **1.0×** | **1.9×** | **5.9×** | **0.18×** |
<!-- /suite-table -->

- luart with the JIT runs as fast as C Lua 5.4 on the geometric mean. It
  is faster on nine of the 17, most on numeric code and method calls
  (spectral-norm, NBody, Bounce and Queens), and slower on the rest, most
  on Richards, Mandelbrot, Json and DeltaBlue. On Mandelbrot the JIT is
  slower than luart's own interpreter.
- Without the JIT, luart takes about twice as long as C Lua 5.4.
- LuaJIT is 5.5 times faster than C Lua 5.4 on the geometric mean.
- go-lua takes about six times as long as C Lua 5.4, and 66 times on
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
