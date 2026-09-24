# AGENTS.md

Notes for anyone, human or agent, changing luart. The README describes the
project's goals and API; this file describes how the implementation works
today, the rules it depends on, and where performance work should go next.

## Working on the repository

- Go 1.27.1, `CGO_ENABLED=0`. CI pins every action to a full commit SHA.
- Run everything CI runs before pushing:
  - `gofmt -l .` must print nothing.
  - `go generate ./...` must leave the tree unchanged; it rewrites
    `vm_jit.go`.
  - `go vet -unreachable=false ./...` (debug.go has two known
    unreachable-code reports).
  - `go test ./...`, again with `LUART_JIT_TEST=1`, which compiles every
    function on first use, and `-race -run JIT`.
  - `cd bench && LUART_JIT_TEST=1 go test -run TestSuiteAgrees .`
- On an Apple silicon Mac, `GOARCH=amd64 go test ./...` runs the amd64 JIT
  under Rosetta. A `GOAMD64=v3` binary cannot run there; CI covers it on
  linux/amd64.
- Tests that need `luac` 5.2 skip locally when it is missing. CI installs
  it, so wait for CI before merging.
- Benchmarks: always pass `-ldflags=-funcalign=64`. Without it, unrelated
  changes move the interpreter loop's alignment and its timings by 5–10%.
  Compare back to back on an idle machine. Every performance PR includes a
  full run of `bench/suite_test.go`, with `bench/README.md` and
  `bench/suite-results.txt` updated to match.

## Interpreter

- `value` (types.go) is 16 bytes: `p unsafe.Pointer` and `n float64`.
  Numbers, booleans and none have sentinel pointers in `p`; other kinds
  keep a kind tag in `n`'s top byte and the object in `p`. Numbers and
  booleans never allocate. `value` is not comparable; use `rawEqual`,
  `hashKey` and `identical`.
- `executeSwitch` (vm.go) is the interpreter loop. `prototype.exec`
  (specialise.go) holds a specialised copy of the bytecode with the same pc
  for every instruction: RR, RK and KR arithmetic, field instructions with
  constant string keys, and `opMulAddRKR`.
- Tables (tables.go, shape.go, field_cache.go) keep string keys in slots
  described by shared shapes (hidden classes). Each field instruction has a
  `fieldCache`, which can also cache a hit through a metatable's `__index`
  table. NEWTABLE sites remember the shape their last table reached.
- Number functions (number_function.go, `PushNumberFunction[F]`) are Go
  functions of float64s that CALL invokes without a Go frame.

## JIT

Opt-in with `NewState(WithJIT())`; `LUART_JIT=off` disables it. It runs
on linux and darwin, arm64 and amd64. Elsewhere `jit_none.go` makes it a
no-op.

- **Hand-over:**
  - A JIT state runs `executeSwitchJIT` (vm_jit.go), which
    internal/jitvm generates from `executeSwitch`. It adds one check for
    the patched opcodes `opJITCount` and `opJITEnter`, so states without
    the JIT run the original loop.
  - `jitOrig` keeps the unpatched instructions. The counter sits at pc 0
    and each FORLOOP; after `jitThreshold` (1000) counts `compileJIT`
    runs, and `opJITEnter` goes at the entries `jitEntries` picks.
  - Never patch an instruction its predecessor consumes: the JMP after a
    test, TFORLOOP after TFORCALL, or an EXTRAARG word.
- **Driver (`runJIT`, jit.go):**
  - It enters compiled code through `internal/jit/call` and handles exits
    in Go.
  - Exits at CALL to a Go function, and at RETURN, are run by the driver,
    as are CLOSURE, NEWTABLE, LEN, generic table access and
    upvalue-closing JMPs (`jitStep`). Compiled code then carries on after
    the instruction.
  - Anything else goes back to the interpreter. The interpreter reloads
    its frame from `l.callInfo` after every hand-over.
- **Compilers:**
  - `jit_arm64*.go` and `jit_amd64*.go` are separate, with the same
    structure.
  - Shared pieces: struct offsets (jit_layout.go), the numeric-loop
    analysis (jit_kernel.go) and the trig constants (jit_trig.go).
  - The encoders in internal/jit/arm64 and internal/jit/amd64 are checked
    against clang's output.
- **Coverage:**
  - Moves, constants, arithmetic, comparisons, branches and numeric for
    loops.
  - Upvalues.
  - Fields through the field caches, including `__index` tables, and
    array elements, including appends within capacity.
  - Native calls and returns between compiled fixed-parameter Lua
    functions.
  - `math.floor`, `ceil`, `sqrt`, `abs`, `sin` and `cos` inline.
- **Kernels:** an innermost numeric for loop whose body is only moves,
  number constants, arithmetic and number comparisons keeps every Lua
  register it uses in an FP register for the whole loop (`emitKernel`).

### Rules compiled code depends on

- **Check before write:** each instruction checks everything before it
  writes anything, so any failed check can exit at that instruction and
  let Go run it from the start.
- **Write barrier:** `runtime.writeBarrier` is read once on entry. It only
  changes while the world is stopped, and a goroutine in generated code
  cannot be stopped. While it is on, compiled code stores only nil,
  numbers and booleans over scalar values. Native calls, returns and
  kernels exit instead.
- **Budget:** loops and native calls spend `jitBudget` (65536). When it
  runs out, compiled code returns to Go, which calls `runtime.Gosched`, so
  the GC can stop the world. Any new backward branch or call path must
  spend budget.
- **No calls into Go:** generated code never calls Go and never uses the
  Go stack; Go's unwinder cannot see its frames. Anything needing Go
  exits.
- **Registers:** on arm64 generated code keeps R18, R26–R30 and SP; on
  amd64 SP, BP, R14 and X15 (internal/jit/call).
- **Bit-exact intrinsics:** they must match Go exactly. `sin` and `cos`
  copy the code Go 1.27 compiles for math/sin.go, including which
  multiply-adds it fuses on arm64. On amd64 that means no fusion below
  GOAMD64=v3; at v3 they are left to Go. `TestJITTrigMatchesGo` fails if a
  Go release changes this.

### Testing the JIT

- `runBoth` in jit_test.go runs a script with and without the JIT and
  compares the results; add cases there for new instructions.
- `TestJITKernels` asserts how many kernels a function compiles.
- To prove a test exercises generated code, break the generated code on
  purpose (for example, emit a subtract for ADD) and check the tests fail.

## Performance today

Apple M1 Pro, arm64, from bench/README.md:

| Workload | luart | luart + JIT | Native Go |
|---|---|---|---|
| numeric loop | 14.0 ms | 1.83 ms | 1.20 ms |
| fib(25) | 7.94 ms | 2.68 ms | 0.25 ms |
| array fill and sum | 4.03 ms | 1.63 ms | 0.45 ms |
| plasma frame | 2.19 ms | 1.66 ms | 0.31 ms |
| particles frame | 0.36 ms | 0.22 ms | 0.006 ms |
| closures | 7.80 ms | 9.55 ms | 0.35 ms |
| sort with comparator | 7.41 ms | 9.52 ms | 1.94 ms |
| calls into Go | 2.76 ms | 3.21 ms | 0.35 ms |

The JIT loses where a script crosses between compiled code and Go every
few instructions. A bare `call.Call` round trip costs about 2 ns
(`BenchmarkCallRet`). The rest of the 10–15 ns per crossing is the
driver's bookkeeping (`enterJIT`, `jitCall`) and dependent loads in the
compiled code around it.

## Recommended next steps

In order of expected payoff for real-time scripts such as visualisers:

1. **Cheaper calls into Go.**
   - *Why:* plasma spends most of its remaining time calling `set`, and
     calls into Go are the JIT's slowest case.
   - *Cost today:* re-entering after a Go call rewrites the whole
     `jitContext` and goes through `jitCall`'s generic checks.
   - *Fix:*
     - Keep the context fields that did not change.
     - Give number functions their own exit reason, so the driver calls
       `number.unary` or `number.call` straight from the argument
       registers.
     - Resume at a per-call-site continuation address.
   - *Measure:* the go-calls and plasma workloads.
2. **Go calling compiled Lua** (the `table.sort` comparator, callbacks
   from host code).
   - *Cost today:* each `l.Call` from Go enters the interpreter, reaches
     pc 0's `opJITEnter`, and returns through the interpreter's slow
     RETURN, because the frame is not a re-entry.
   - *Fix:* let `call` (stack.go) enter a compiled prototype directly,
     and give the driver a return path for frames called from Go.
3. **Loop-invariant global and upvalue loads.**
   - *Cost today:* GETTABUP of a global (`set`, `math`) re-walks upvalue →
     table → shape → cache → slot every iteration.
   - *Fix:* once per loop entry, guard the `_ENV` table's shape and the
     cached slot, then keep the value in a register. Stores to that
     table or calls invalidate the guard, so this suits loops whose calls
     are all to intrinsics or number functions.
4. **Wider kernels.**
   - Allow intrinsic calls inside kernels. The callee value is guarded
     once at loop entry, as the upvalue or global it comes from cannot
     change inside a kernel with no stores.
   - Allow number-function calls by writing back only the registers the
     call can observe.
   - This would make plasma's inner loop a kernel apart from `set`.
5. **Closures and GC.**
   - *Why:* the closures workload is GC-bound in both modes; the main
     goroutine gets about half the CPU.
   - *Fix:* shrink `luaClosure`, reuse closures for identical upvalues
     (Lua 5.2's closure cache, `cached`, only helps when upvalues match),
     or create closures in compiled code from a pre-allocated pool.
6. **More native instructions:**
   - TFORCALL/TFORLOOP, with fast paths for `ipairs` and `pairs` over
     array parts.
   - CONCAT of numbers and strings into a reusable buffer.
   - SETLIST.
   - Vararg calls and tail calls.
7. **amd64 on real hardware.** Its timings so far come from Rosetta.
   Benchmark on linux/amd64, and check whether keeping the budget and
   barrier in memory costs enough to justify spilling another register
   for them.
