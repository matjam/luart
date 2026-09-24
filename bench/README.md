# Benchmarks

`suite_test.go` runs ten workloads three ways: native Go, luart, and
Shopify/go-lua, with the same Lua source for both interpreters.
`TestSuiteAgrees` checks that all three compute the same result. Raw output
is in [`suite-results.txt`](suite-results.txt).

Apple M1 Pro, Go 1.27.1, `CGO_ENABLED=0`, `-count 6`, medians from
benchstat, 2026-09-24:

| Workload | Native Go | luart | Shopify/go-lua | luart vs Shopify | luart vs Go |
|---|---|---|---|---|---|
| fib(25), recursive calls | 0.26 ms | 8.02 ms | 14.0 ms | 1.7× faster | 31× slower |
| numeric loop, 1M iterations | 1.20 ms | 14.2 ms | 300 ms | 21× faster | 12× slower |
| array fill and sum, 100k | 0.49 ms | 4.18 ms | 8.78 ms | 2.1× faster | 8.5× slower |
| records, 10k tables | 0.13 ms | 1.36 ms | 4.47 ms | 3.3× faster | 10× slower |
| closures, 100k | 0.36 ms | 8.25 ms | 14.7 ms | 1.8× faster | 23× slower |
| sort 10k with comparator | 1.99 ms | 7.67 ms | 15.1 ms | 2.0× faster | 3.9× slower |
| string build, 10k pieces | 0.62 ms | 1.08 ms | 105 ms | 97× faster | 1.7× slower |
| calls into Go, 100k | 0.35 ms | 2.82 ms | 7.87 ms | 2.8× faster | 7.9× slower |
| plasma frame | 0.34 ms | 2.15 ms | 5.93 ms | 2.8× faster | 6.4× slower |
| particles frame | 0.006 ms | 0.38 ms | 1.67 ms | 4.4× faster | 63× slower |

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
- The native Go versions keep Go's advantages, such as inlined method calls
  in particles, so the gap to Go is widest where Go inlines most.
- Plasma here registers `set` as an ordinary Go function. As a number
  function (`BenchmarkLuartNumberFunction`) it takes 2.1 ms.

## Reproducing

From this directory:

```sh
CGO_ENABLED=0 go test -run x -bench 'Suite|GoParticles' -benchmem -count 6 | tee suite-results.txt
go run golang.org/x/perf/cmd/benchstat@latest suite-results.txt
```
