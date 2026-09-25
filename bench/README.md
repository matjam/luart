# Benchmarks

Two benchmarks run the same Lua source in every interpreter: luart without
the JIT (`WithoutJIT`), luart with it (the default), Shopify/go-lua, and C
Lua 5.4 or LuaJIT when built with their tag (see
[C interpreters](#c-interpreters)).

- **`BenchmarkSuite`** ([`suite_test.go`](suite_test.go)) runs eleven
  workloads chosen for what an embedded Lua does: calls, loops, tables,
  closures, sorting, building and scanning strings, and calls into Go.
  Each is also written in native Go, which the interpreters are compared
  with.
- **`BenchmarkStandard`** ([`standard_test.go`](standard_test.go)) runs the
  14 benchmarks of Are We Fast Yet and three from the Computer Language
  Benchmarks Game, which other language implementations are measured with
  ([`standard/`](standard)). The interpreters are compared with C Lua 5.4.

`TestSuiteAgrees` and `TestStandardAgrees` check that every interpreter
computes the same results. The tables and charts come from the raw output
by [`chart`](chart) (see [Reproducing](#reproducing)).

## amd64

AMD Ryzen 9 9900X3D, linux, Go 1.27.1, `-count 6`,
`-ldflags=-funcalign=64`, medians, 2026-09-25, at commit e5ed0e4. Lua 5.4.9
and LuaJIT 2.1.1788460057 from Arch Linux's packages. Raw output:
[`suite-results-amd64.txt`](suite-results-amd64.txt).

### The suite

![Each interpreter's time on each workload divided by native Go's, on amd64](suite-amd64.svg)

Each cell is the median time, and in brackets that time divided by native
Go's.

<!-- suite-table amd64 -->
| Workload | Native Go | Luart (JIT) | Luart (no JIT) | go-lua | Lua 5.4 | LuaJIT |
|---|---:|---:|---:|---:|---:|---:|
| fib(25), recursive calls | 0.21 ms | 1.58 ms (7.4×) | 5.66 ms (27×) | 9.16 ms (43×) | 2.34 ms (11×) | 0.28 ms (1.3×) |
| numeric loop, 1M iterations | 0.79 ms | 0.95 ms (1.2×) | 9.22 ms (12×) | 185 ms (235×) | 4.52 ms (5.7×) | 0.78 ms (0.99×) |
| array fill and sum, 100k | 0.47 ms | 1.03 ms (2.2×) | 2.59 ms (5.5×) | 5.92 ms (12×) | 0.77 ms (1.6×) | 0.20 ms (0.42×) |
| records, 10k tables | 0.09 ms | 0.62 ms (6.9×) | 0.88 ms (9.8×) | 2.82 ms (31×) | 0.87 ms (9.7×) | 0.28 ms (3.1×) |
| closures, 100k | 0.22 ms | 4.74 ms (21×) | 5.31 ms (24×) | 9.65 ms (43×) | 6.75 ms (30×) | 3.44 ms (15×) |
| sort 10k with comparator | 1.29 ms | 3.59 ms (2.8×) | 3.71 ms (2.9×) | 11.1 ms (8.6×) | 3.28 ms (2.5×) | 3.36 ms (2.6×) |
| string build, 10k pieces | 0.37 ms | 0.56 ms (1.5×) | 0.69 ms (1.9×) | 78.8 ms (212×) | 0.95 ms (2.5×) | 0.37 ms (0.99×) |
| string scan, 11k characters | 0.007 ms | 0.57 ms (84×) | 1.51 ms (224×) | 2.76 ms (409×) | 0.71 ms (105×) | 0.06 ms (8.9×) |
| calls into Go, 100k | 0.22 ms | 1.30 ms (5.8×) | 1.79 ms (8.0×) | 5.43 ms (24×) | 1.09 ms (4.9×) | 0.71 ms (3.2×) |
| plasma frame | 0.30 ms | 0.75 ms (2.5×) | 1.37 ms (4.6×) | 4.20 ms (14×) | 1.48 ms (5.0×) | 0.54 ms (1.8×) |
| particles frame | 0.005 ms | 0.08 ms (16×) | 0.26 ms (50×) | 1.25 ms (239×) | 0.14 ms (27×) | 0.03 ms (5.6×) |
| **geometric mean** |  | **5.8×** | **12×** | **54×** | **8.3×** | **2.4×** |
<!-- /suite-table -->

### The standard benchmarks

![Each interpreter's time on each standard benchmark divided by C Lua 5.4's, on amd64](standard-amd64.svg)

Each cell is the median time, and in brackets that time divided by C Lua
5.4's.

<!-- suite-table standard-amd64 -->
| Benchmark | Lua 5.4 | Luart (JIT) | Luart (no JIT) | go-lua | LuaJIT |
|---|---:|---:|---:|---:|---:|
| Bounce | 0.28 ms | 0.16 ms (0.57×) | 0.53 ms (1.9×) | 2.16 ms (7.6×) | 0.03 ms (0.10×) |
| CD | 36.9 ms | 37.0 ms (1.0×) | 45.7 ms (1.2×) | 156 ms (4.2×) | 14.3 ms (0.39×) |
| DeltaBlue | 20.8 ms | 19.9 ms (0.96×) | 32.5 ms (1.6×) | 1414 ms (68×) | 9.55 ms (0.46×) |
| Havlak | 1727 ms | 1744 ms (1.0×) | 2184 ms (1.3×) | – | 1037 ms (0.60×) |
| Json | 4.31 ms | 4.32 ms (1.0×) | 7.78 ms (1.8×) | 20.8 ms (4.8×) | 0.91 ms (0.21×) |
| List | 0.23 ms | 0.17 ms (0.72×) | 0.43 ms (1.9×) | 1.07 ms (4.7×) | 0.07 ms (0.29×) |
| Mandelbrot | 128 ms | 110 ms (0.85×) | 211 ms (1.6×) | 683 ms (5.3×) | 23.7 ms (0.18×) |
| NBody | 1.18 ms | 0.70 ms (0.59×) | 2.19 ms (1.9×) | 11.2 ms (9.4×) | 0.08 ms (0.07×) |
| Permute | 0.39 ms | 0.23 ms (0.61×) | 1.07 ms (2.8×) | 2.44 ms (6.3×) | 0.01 ms (0.04×) |
| Queens | 0.30 ms | 0.17 ms (0.59×) | 0.64 ms (2.2×) | 1.36 ms (4.6×) | 0.03 ms (0.12×) |
| Richards | 16.3 ms | 16.0 ms (0.98×) | 27.2 ms (1.7×) | 99.8 ms (6.1×) | 5.65 ms (0.35×) |
| Sieve | 0.12 ms | 0.09 ms (0.78×) | 0.30 ms (2.5×) | 0.60 ms (5.2×) | 0.02 ms (0.15×) |
| Storage | 0.81 ms | 0.61 ms (0.75×) | 0.90 ms (1.1×) | 3.17 ms (3.9×) | 0.33 ms (0.41×) |
| Towers | 0.75 ms | 0.48 ms (0.64×) | 1.54 ms (2.0×) | 4.17 ms (5.6×) | 0.09 ms (0.12×) |
| binary-trees | 118 ms | 130 ms (1.1×) | 160 ms (1.3×) | 218 ms (1.8×) | 34.9 ms (0.29×) |
| fannkuch-redux | 69.2 ms | 63.5 ms (0.92×) | 182 ms (2.6×) | 318 ms (4.6×) | 18.0 ms (0.26×) |
| spectral-norm | 35.7 ms | 20.6 ms (0.58×) | 65.9 ms (1.8×) | 169 ms (4.7×) | 1.29 ms (0.04×) |
| **geometric mean** |  | **0.78×** | **1.8×** | **5.9×** | **0.18×** |
<!-- /suite-table -->

- luart with the JIT takes 0.78 times as long as C Lua 5.4 on the
  geometric mean. It is faster on 13 of the 17, most on numeric code and
  method calls (spectral-norm, Bounce, Queens and NBody), within 1% on
  CD, Havlak and Json, and 1.1 times as long on binary-trees, which
  allocates most.
- Without the JIT, luart takes 1.8 times as long as C Lua 5.4.
- LuaJIT is 5.5 times faster than C Lua 5.4 on the geometric mean.
- go-lua takes six times as long as C Lua 5.4, and 70 times on
  DeltaBlue. It does not finish Havlak in ten minutes, so it has no result
  there, and its geometric mean is of the other 16.

## arm64

Apple M1 Pro, macOS, Go 1.27.1, `-count 6`, `-ldflags=-funcalign=64`,
medians, 2026-09-25, at commit 26f3589. Lua 5.4.9 and LuaJIT 2.1.1788856981
from Homebrew (`lua@5.4`, `luajit`). The machine was not idle, so single
results vary more than on amd64. Raw output:
[`suite-results.txt`](suite-results.txt).

### The suite

![Each interpreter's time on each workload divided by native Go's, on Apple M1 Pro](suite-arm64-m1.svg)

Each cell is the median time, and in brackets that time divided by native
Go's.

<!-- suite-table arm64-m1 -->
| Workload | Native Go | Luart (JIT) | Luart (no JIT) | go-lua | Lua 5.4 | LuaJIT |
|---|---:|---:|---:|---:|---:|---:|
| fib(25), recursive calls | 0.25 ms | 2.75 ms (11×) | 8.03 ms (32×) | 13.7 ms (54×) | 4.28 ms (17×) | 0.50 ms (2.0×) |
| numeric loop, 1M iterations | 1.17 ms | 1.77 ms (1.5×) | 13.9 ms (12×) | 290 ms (247×) | 11.6 ms (9.9×) | 1.10 ms (0.94×) |
| array fill and sum, 100k | 0.57 ms | 1.91 ms (3.3×) | 4.26 ms (7.5×) | 9.36 ms (16×) | 1.22 ms (2.1×) | 0.41 ms (0.71×) |
| records, 10k tables | 0.14 ms | 1.17 ms (8.3×) | 1.49 ms (11×) | 5.06 ms (36×) | 1.34 ms (9.4×) | 0.43 ms (3.1×) |
| closures, 100k | 0.37 ms | 9.91 ms (27×) | 9.99 ms (27×) | 15.0 ms (40×) | 9.73 ms (26×) | 5.40 ms (14×) |
| sort 10k with comparator | 2.07 ms | 6.90 ms (3.3×) | 5.76 ms (2.8×) | 15.3 ms (7.4×) | 5.56 ms (2.7×) | 4.01 ms (1.9×) |
| string build, 10k pieces | 0.61 ms | 0.91 ms (1.5×) | 1.10 ms (1.8×) | 127 ms (207×) | 1.79 ms (2.9×) | 0.49 ms (0.81×) |
| string scan, 11k characters | 0.02 ms | 0.88 ms (40×) | 2.38 ms (108×) | 3.72 ms (168×) | 1.10 ms (50×) | 0.10 ms (4.7×) |
| calls into Go, 100k | 0.41 ms | 2.30 ms (5.6×) | 2.87 ms (7.0×) | 8.36 ms (20×) | 1.99 ms (4.8×) | 1.20 ms (2.9×) |
| plasma frame | 0.32 ms | 1.53 ms (4.8×) | 2.35 ms (7.4×) | 6.10 ms (19×) | 2.37 ms (7.4×) | 0.55 ms (1.7×) |
| particles frame | 0.006 ms | 0.19 ms (32×) | 0.40 ms (65×) | 1.77 ms (291×) | 0.29 ms (48×) | 0.06 ms (10×) |
| **geometric mean** |  | **7.0×** | **13×** | **53×** | **9.5×** | **2.5×** |
<!-- /suite-table -->

- Sort with a comparator takes longer with the JIT than without it on
  the M1 (6.90 ms against 5.76 ms), where amd64 is a little faster with
  it. Why is not yet measured.

### The standard benchmarks

![Each interpreter's time on each standard benchmark divided by C Lua 5.4's, on Apple M1 Pro](standard-arm64-m1.svg)

Each cell is the median time, and in brackets that time divided by C Lua
5.4's.

<!-- suite-table standard-arm64-m1 -->
| Benchmark | Lua 5.4 | Luart (JIT) | Luart (no JIT) | go-lua | LuaJIT |
|---|---:|---:|---:|---:|---:|
| Bounce | 0.49 ms | 0.35 ms (0.70×) | 0.79 ms (1.6×) | 3.02 ms (6.1×) | 0.10 ms (0.19×) |
| CD | 57.8 ms | 57.1 ms (0.99×) | 63.8 ms (1.1×) | 236 ms (4.1×) | 23.8 ms (0.41×) |
| DeltaBlue | 40.5 ms | 32.8 ms (0.81×) | 47.7 ms (1.2×) | 2473 ms (61×) | 15.4 ms (0.38×) |
| Havlak | 3436 ms | 2701 ms (0.79×) | 3142 ms (0.91×) | – | 1892 ms (0.55×) |
| Json | 8.80 ms | 6.18 ms (0.70×) | 10.2 ms (1.2×) | 28.2 ms (3.2×) | 1.24 ms (0.14×) |
| List | 0.48 ms | 0.28 ms (0.59×) | 0.62 ms (1.3×) | 1.48 ms (3.1×) | 0.08 ms (0.16×) |
| Mandelbrot | 303 ms | 175 ms (0.58×) | 346 ms (1.1×) | 1145 ms (3.8×) | 40.8 ms (0.13×) |
| NBody | 2.68 ms | 1.44 ms (0.54×) | 3.37 ms (1.3×) | 14.9 ms (5.6×) | 0.13 ms (0.05×) |
| Permute | 0.77 ms | 0.46 ms (0.60×) | 1.43 ms (1.9×) | 3.43 ms (4.5×) | 0.02 ms (0.03×) |
| Queens | 0.55 ms | 0.28 ms (0.51×) | 0.88 ms (1.6×) | 1.99 ms (3.6×) | 0.05 ms (0.10×) |
| Richards | 29.7 ms | 23.6 ms (0.79×) | 38.3 ms (1.3×) | 131 ms (4.4×) | 7.41 ms (0.25×) |
| Sieve | 0.18 ms | 0.14 ms (0.76×) | 0.44 ms (2.4×) | 0.87 ms (4.7×) | 0.02 ms (0.11×) |
| Storage | 1.51 ms | 1.02 ms (0.68×) | 1.26 ms (0.83×) | 4.20 ms (2.8×) | 0.49 ms (0.33×) |
| Towers | 1.34 ms | 0.97 ms (0.73×) | 2.08 ms (1.6×) | 6.11 ms (4.6×) | 0.08 ms (0.06×) |
| binary-trees | 195 ms | 231 ms (1.2×) | 269 ms (1.4×) | 316 ms (1.6×) | 52.3 ms (0.27×) |
| fannkuch-redux | 136 ms | 109 ms (0.80×) | 262 ms (1.9×) | 457 ms (3.4×) | 17.2 ms (0.13×) |
| spectral-norm | 65.5 ms | 32.6 ms (0.50×) | 93.7 ms (1.4×) | 247 ms (3.8×) | 2.04 ms (0.03×) |
| **geometric mean** |  | **0.70×** | **1.4×** | **4.5×** | **0.14×** |
<!-- /suite-table -->

- luart with the JIT takes 0.70 times as long as C Lua 5.4 on the
  geometric mean, and is faster on 16 of the 17; binary-trees, which
  allocates most, takes 1.2 times as long.
- Without the JIT, luart takes 1.4 times as long as C Lua 5.4.
- Relative to C Lua 5.4, both luart and LuaJIT do better here than on
  amd64. The two machines' C Lua 5.4 are different builds (Homebrew and
  Arch Linux), so compare their ratios with care.

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
| string scan | 0 | 71,000 |
| calls into Go | 0 | 400,000 |
| plasma frame | 0 | 280,000 |
| particles frame | 0 | 25,000 |

## Notes

- Numbers, booleans and calls allocate nothing in luart. The remaining
  allocations are the Lua objects the script creates, one per table,
  closure or string.
- go-lua's numeric loop is slow because its `%` fast path calls
  `math.Mod`, which is far slower than Lua's `a - floor(a/b)*b`.
- The string scan walks 11,000 characters with `s:byte(i)` and
  `s:sub(i, i)`: two calls into Go a character, which the native Go
  version replaces with indexing its compiler folds into a tight loop. It
  shows what calls into Go cost; luart with the JIT runs it faster than C
  Lua 5.4. It is the suite's newest workload, so its geometric means are
  not comparable with those of earlier runs.
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
build with `CGO_ENABLED=0` and run luart and go-lua only. On macOS,
Homebrew's `lua` is now 5.5; install `lua@5.4`, which is keg-only, and
set `PKG_CONFIG_PATH=$(brew --prefix lua@5.4)/lib/pkgconfig`.

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
file, charts and tables for the machine. On Apple M1 the file is
`suite-results.txt`, and the charts and tables are only in this README:

```sh
go run ./chart -svg suite-arm64-m1.svg -readme README.md -name arm64-m1 suite-results.txt
go run ./chart -suite standard -svg standard-arm64-m1.svg -readme README.md -name standard-arm64-m1 suite-results.txt
```
