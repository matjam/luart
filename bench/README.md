# Benchmarks

`suite_test.go` runs ten workloads three ways: native Go, luart, and
Shopify/go-lua, with the same Lua source for both interpreters.
`TestSuiteAgrees` checks that all three compute the same result. Raw output
is in [`suite-results.txt`](suite-results.txt).

Apple M1 Pro, Go 1.27.1, `CGO_ENABLED=0`, `-count 6`, medians from
benchstat, 2026-09-24:

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

| Workload | luart allocations | Shopify allocations |
|---|---|---|
| fib(25) | 0 | 318,000 |
| numeric loop | 0 | 4,857,000 |
| array fill and sum | 19 | 500,000 |
| records | 20,000 | 120,000 |
| closures | 400,000 | 900,000 |
| sort | 17 | 180,000 |
| string build | 40,000 | 80,000 |
| calls into Go | 0 | 400,000 |
| plasma frame | 0 | 280,000 |
| particles frame | 0 | 25,000 |

- Numbers, booleans and calls allocate nothing in luart. The remaining
  allocations are Lua objects the script creates: tables, closures and
  strings.
- Shopify's numeric loop is slow because its `%` fast path calls
  `math.Mod`, which is far slower than Lua's `a - floor(a/b)*b`.
- Shopify's string build is quadratic: its `table.concat` appends with
  `s += str`. luart's uses a `strings.Builder`.
- The native Go versions keep Go's advantages, such as inlined method calls
  in particles, so the gap to Go is widest where Go inlines most.
- Plasma here registers `set` as an ordinary Go function. As a number
  function (`BenchmarkLuartNumberFunction`) it takes 2.3 ms.

## Reproducing

From this directory:

```sh
CGO_ENABLED=0 go test -run x -bench 'Suite|GoParticles' -benchmem -count 6 | tee suite-results.txt
go run golang.org/x/perf/cmd/benchstat@latest suite-results.txt
```
