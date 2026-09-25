# Benchmarks

`suite_test.go` runs ten workloads four ways: native Go, luart without the
JIT (`WithoutJIT`), luart with it (the default), and Shopify/go-lua, with
the same Lua source for every interpreter. `TestSuiteAgrees` checks that
all of them compute the same result. Each interpreter is compared with
native Go; the tables and charts come from the raw output by
[`chart`](chart) (see [Reproducing](#reproducing)).

## amd64

AMD Ryzen 9 9900X3D, linux, Go 1.27.1, `CGO_ENABLED=0`, `-count 6`,
`-ldflags=-funcalign=64`, medians, 2026-09-24. Raw output:
[`suite-results-amd64.txt`](suite-results-amd64.txt).

![How many times slower than native Go each interpreter runs each workload, on amd64](suite-amd64.svg)

<!-- suite-table amd64 -->
| Workload | Native Go | Luart (no JIT) | Luart (JIT) | go-lua | Luart (no JIT) vs Go | Luart (JIT) vs Go | go-lua vs Go |
|---|---|---|---|---|---|---|---|
| fib(25), recursive calls | 0.22 ms | 5.54 ms | 1.58 ms | 9.19 ms | 26× slower | 7.3× slower | 42× slower |
| numeric loop, 1M iterations | 0.78 ms | 8.51 ms | 0.96 ms | 187 ms | 11× slower | 1.2× slower | 239× slower |
| array fill and sum, 100k | 0.46 ms | 2.76 ms | 1.19 ms | 6.39 ms | 6.0× slower | 2.6× slower | 14× slower |
| records, 10k tables | 0.09 ms | 0.88 ms | 0.65 ms | 3.02 ms | 9.8× slower | 7.2× slower | 34× slower |
| closures, 100k | 0.22 ms | 5.48 ms | 4.63 ms | 9.72 ms | 25× slower | 21× slower | 44× slower |
| sort 10k with comparator | 1.28 ms | 3.63 ms | 3.56 ms | 10.2 ms | 2.8× slower | 2.8× slower | 7.9× slower |
| string build, 10k pieces | 0.37 ms | 0.71 ms | 0.60 ms | 60.2 ms | 1.9× slower | 1.6× slower | 163× slower |
| calls into Go, 100k | 0.22 ms | 1.72 ms | 1.25 ms | 5.50 ms | 7.8× slower | 5.7× slower | 25× slower |
| plasma frame | 0.29 ms | 1.35 ms | 0.74 ms | 4.23 ms | 4.6× slower | 2.5× slower | 15× slower |
| particles frame | 0.005 ms | 0.25 ms | 0.08 ms | 1.23 ms | 49× slower | 16× slower | 236× slower |
<!-- /suite-table -->

The numeric loop with the JIT varies by about 20% between runs on this
part, which has two core complexes with different caches and clocks; the
other workloads vary by a few percent.

## arm64

Apple M1 Pro, Go 1.27.1, `CGO_ENABLED=0`, `-count 6`,
`-ldflags=-funcalign=64`, medians, 2026-09-24, measured at commit 664f09d,
before the changes to calls between Go and compiled Lua, to `table.sort`,
and to `sin` and `cos` that the amd64 results include. Raw output:
[`suite-results.txt`](suite-results.txt).

![How many times slower than native Go each interpreter runs each workload, on Apple M1 Pro](suite-arm64-m1.svg)

<!-- suite-table arm64-m1 -->
| Workload | Native Go | Luart (no JIT) | Luart (JIT) | go-lua | Luart (no JIT) vs Go | Luart (JIT) vs Go | go-lua vs Go |
|---|---|---|---|---|---|---|---|
| fib(25), recursive calls | 0.25 ms | 7.94 ms | 2.68 ms | 13.8 ms | 32× slower | 11× slower | 55× slower |
| numeric loop, 1M iterations | 1.20 ms | 14.0 ms | 1.83 ms | 294 ms | 12× slower | 1.5× slower | 245× slower |
| array fill and sum, 100k | 0.45 ms | 4.03 ms | 1.63 ms | 8.45 ms | 8.9× slower | 3.6× slower | 19× slower |
| records, 10k tables | 0.13 ms | 1.29 ms | 1.09 ms | 4.23 ms | 10× slower | 8.7× slower | 34× slower |
| closures, 100k | 0.35 ms | 7.80 ms | 9.55 ms | 13.6 ms | 22× slower | 27× slower | 39× slower |
| sort 10k with comparator | 1.94 ms | 7.41 ms | 9.52 ms | 14.0 ms | 3.8× slower | 4.9× slower | 7.2× slower |
| string build, 10k pieces | 0.57 ms | 1.04 ms | 0.99 ms | 93.4 ms | 1.8× slower | 1.7× slower | 165× slower |
| calls into Go, 100k | 0.35 ms | 2.76 ms | 3.21 ms | 7.66 ms | 7.9× slower | 9.2× slower | 22× slower |
| plasma frame | 0.30 ms | 2.19 ms | 1.66 ms | 5.70 ms | 7.2× slower | 5.5× slower | 19× slower |
| particles frame | 0.006 ms | 0.36 ms | 0.22 ms | 1.59 ms | 61× slower | 38× slower | 270× slower |
<!-- /suite-table -->

## Allocations

The JIT allocates exactly what the interpreter does.

| Workload | luart allocations | go-lua allocations |
|---|---|---|
| fib(25) | 0 | 318,000 |
| numeric loop | 0 | 4,857,000 |
| array fill and sum | 19 | 500,000 |
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

## Reproducing

From this directory, on an idle machine:

```sh
CGO_ENABLED=0 go test -run x -bench . -benchmem -count 6 -ldflags=-funcalign=64 | tee suite-results-amd64.txt
go run ./chart -svg suite-amd64.svg -readme README.md,../README.md -name amd64 suite-results-amd64.txt
```

From the median of each benchmark in the file, `-svg` redraws the chart
and `-readme` rewrites the table between `<!-- suite-table NAME -->` and
`<!-- /suite-table -->` in each file; `-table` prints it instead. Name the
file, chart and table for the machine; `suite-results.txt`,
`suite-arm64-m1.svg` and `arm64-m1` are Apple M1.
