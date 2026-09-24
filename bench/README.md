# Benchmarks

`suite_test.go` runs ten workloads four ways: native Go, luart, luart with
the JIT (`WithJIT`), and Shopify/go-lua, with the same Lua source for every
interpreter. `TestSuiteAgrees` checks that all of them compute the same
result. Raw output is in [`suite-results.txt`](suite-results.txt).

Apple M1 Pro, Go 1.27.1, `CGO_ENABLED=0`, `-count 6`,
`-ldflags=-funcalign=64`, medians, 2026-09-24:

| Workload | Native Go | luart | luart + JIT | Shopify/go-lua | JIT vs luart | JIT vs Go |
|---|---|---|---|---|---|---|
| fib(25), recursive calls | 0.25 ms | 8.11 ms | 2.71 ms | 13.8 ms | 3.0× faster | 11× slower |
| numeric loop, 1M iterations | 1.19 ms | 14.1 ms | 1.79 ms | 302 ms | 7.8× faster | 1.5× slower |
| array fill and sum, 100k | 0.48 ms | 4.10 ms | 1.64 ms | 8.56 ms | 2.5× faster | 3.4× slower |
| records, 10k tables | 0.13 ms | 1.34 ms | 1.16 ms | 4.40 ms | 1.2× faster | 8.7× slower |
| closures, 100k | 0.35 ms | 8.24 ms | 10.0 ms | 14.1 ms | 1.2× slower | 28× slower |
| sort 10k with comparator | 1.94 ms | 7.45 ms | 9.57 ms | 14.3 ms | 1.3× slower | 4.9× slower |
| string build, 10k pieces | 0.59 ms | 1.09 ms | 1.01 ms | 95.8 ms | 1.1× faster | 1.7× slower |
| calls into Go, 100k | 0.35 ms | 2.77 ms | 3.15 ms | 7.85 ms | 1.1× slower | 9.0× slower |
| plasma frame | 0.30 ms | 2.19 ms | 1.66 ms | 5.83 ms | 1.3× faster | 5.6× slower |
| particles frame | 0.006 ms | 0.36 ms | 0.23 ms | 1.61 ms | 1.6× faster | 38× slower |

The JIT allocates exactly what the interpreter does.

| Workload | luart allocations | Shopify allocations |
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

- Numbers, booleans and calls allocate nothing in luart. The remaining
  allocations are the Lua objects the script creates, one per table,
  closure or string.
- Shopify's numeric loop is slow because its `%` fast path calls
  `math.Mod`, which is far slower than Lua's `a - floor(a/b)*b`.
- Shopify's string build is quadratic: its `table.concat` appends with
  `s += str`. luart's uses a `strings.Builder`.
- The JIT is slower than the interpreter where scripts cross between
  compiled code and Go every few instructions: creating closures, Go
  calling Lua (the sort comparator), and calls into Go. It is fastest on
  numeric loops and Lua calls.
- `-ldflags=-funcalign=64` keeps the interpreter loop's alignment fixed;
  without it, unrelated changes move timings by 5–10%.
- The native Go versions keep Go's advantages, such as inlined method calls
  in particles, so the gap to Go is widest where Go inlines most.
- Plasma here registers `set` as an ordinary Go function. As a number
  function (`BenchmarkLuartNumberFunction`) it takes 2.1 ms.

## Reproducing

From this directory:

```sh
CGO_ENABLED=0 go test -run x -bench . -benchmem -count 6 -ldflags=-funcalign=64 | tee suite-results.txt
go run golang.org/x/perf/cmd/benchstat@latest suite-results.txt
```
