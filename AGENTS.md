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
  - `go vet ./...`.
  - `go test ./...`, which runs with the JIT at its normal threshold;
    again with `LUART_JIT_TEST=1`, which compiles every function on first
    use; again with `LUART_JIT=off`, which only interprets; and
    `-race -run JIT`.
  - `cd bench && LUART_JIT_TEST=1 go test -run TestSuiteAgrees .`
- On an Apple silicon Mac, `GOARCH=amd64 go test ./...` runs the amd64 JIT
  under Rosetta. A `GOAMD64=v3` binary cannot run there; CI covers it on
  linux/amd64.
- On linux/amd64 with qemu-user's binfmt handler installed,
  `GOARCH=arm64 go test ./...` runs the arm64 JIT under emulation.
- Tests that need `luac` 5.2 skip locally when it is missing, but fail
  when `luac` is another version; skip them with `-skip
  'TestParserExhaustively|TestUndump|TestDumpThenUndump'`. CI installs 5.2, so wait for CI
  before merging. The Lua test suite is the `lua-tests` submodule.
- Benchmarks: always pass `-ldflags=-funcalign=64`. Without it, unrelated
  changes move the interpreter loop's alignment and its timings by 5–10%.
  Compare back to back on an idle machine; on a CPU with more than one
  core complex, pin A/B runs to one (`taskset -c 0-5` on the 9900X3D).
  Every performance PR includes a full run of `bench/suite_test.go`, saved
  as the results file for its machine (`bench/suite-results-amd64.txt`,
  or `bench/suite-results.txt` for Apple M1). `cd bench && go run ./chart
  -svg suite-amd64.svg -readme README.md,../README.md -name amd64
  suite-results-amd64.txt` redraws the chart and rewrites the READMEs'
  tables from it (bench/README.md, Reproducing).

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

On by default; `NewState(WithoutJIT())` or `LUART_JIT=off` turns it off.
It runs on linux and darwin, arm64 and amd64. Elsewhere `jit_none.go`
makes it a no-op. JIT tests call `skipWithoutJIT`, which skips them where
nothing compiles.

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
  - Compiled code exits with `jitExitCallGo` at a CALL whose callee is a
    Go closure, or a Go function that is neither an inline intrinsic nor a
    number function, and with `jitExitCallNumber` for a number function. The exit stub leaves the callee's object in
    `jitContext.callee` and the frame register in `jitContext.frame`
    (native calls move the frame without updating the context), and the
    driver calls it (`jitCallGoFunction`) and re-enters after it. The
    check sits out of line, after the Lua closure check, so Lua-to-Lua
    calls neither run it nor have it in their code path.
  - Other exits at CALL and at RETURN are run by the driver, as are
    CLOSURE, NEWTABLE, LEN, generic table access and upvalue-closing JMPs
    (`jitStep`). Compiled code then carries on after the instruction.
  - `runJIT` reloads the closure and prototype only when `l.callInfo`
    changes. The chain of loads from a callInfo to its prototype is most
    of a crossing's cost otherwise.
  - Anything else goes back to the interpreter. The interpreter reloads
    its frame from `l.callInfo` after every hand-over.
  - When Go calls a compiled Lua function (`l.call`), `callJIT` runs it
    with `runJIT` straight from `preCall`, without the interpreter. That
    frame is `runJIT`'s `bottom`: at its RETURN, `jitReturnToGo` does the
    interpreter's general return and `runJIT` reports that the call is
    done. If compiled code stops anywhere else, `execute` carries on from
    `l.callInfo`.
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
    array elements, including appends within capacity, of tables in
    registers or upvalues.
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
  Go release changes this. Their constants live in `trigTable`
  (jit_trig.go), which compiled code reads as memory operands on amd64
  and with one LDR each on arm64; where a constant comes from does not
  change the rounding.

### Testing the JIT

- `runBoth` in jit_test.go runs a script with and without the JIT and
  compares the results; add cases there for new instructions.
- `TestJITKernels` asserts how many kernels a function compiles.
- To prove a test exercises generated code, break the generated code on
  purpose (for example, emit a subtract for ADD) and check the tests fail.

## Performance today

bench/README.md has the current tables and charts, generated from the raw
results: AMD Ryzen 9 9900X3D (linux/amd64), and Apple M1 Pro (arm64) as
of commit 664f09d.

To find where a workload leaves compiled code, count exits: log
`p.jitOrig[ip]` and `l.jitCtx.reason` after each `enterJIT` in `runJIT`
for one call of the workload. An exit every iteration costs 10 ns or more;
that is how `GETTABUP` with an array index was found to exit on each of
particles' 2,000 iterations.

The JIT gains least where a script crosses between compiled code and Go
every few instructions. A bare `call.Call` round trip costs about 2 ns
(`BenchmarkCallRet`). On the 9900X3D a call into Go from compiled code
now costs about 12.5 ns in all, against 17.3 ns interpreted; about 4.5
ns of that is compiled code around the call.

On amd64 most of plasma's JIT time (about 80%) is in compiled code, not
in calls to `set`: every Lua register lives in memory, each store
re-reads the barrier flag from the context, and an intrinsic call
compares the callee against each intrinsic in turn (`sin` is fifth).

## Recommended next steps

### Where the time goes now

Measured on the 9900X3D with the JIT on (bench/README.md):

- Crossings between compiled code and Go are close to their floor. A
  call into Go costs about 12.5 ns, of which about 4.5 ns is compiled
  code around it, and the rest is the API's Go frame and the round trip.
  Within a workload, the only exits left on every iteration are calls
  into Go, allocations (`CLOSURE`, `NEWTABLE`) and the sort comparator's
  return to Go.
- Plasma takes about 37 ns a pixel against Go's 15: about 12 ns for the
  call to `set`, about 6 ns for each of three `sin`s (Go's `math.Sin` is
  about 4), and the rest in ordinary compiled code, which keeps every Lua
  register in memory.
- fib, records and particles are limited by ordinary compiled code:
  values in memory, a type check on every read and a barrier check on
  every store.
- Closures and records are limited by allocation, one object per
  iteration, as Lua requires.

### Done in this round

- Calls into Go: `jitExitCallGo` and `jitExitCallNumber`, and a driver
  path that makes the call from what the exit leaves in the context (16.6
  to 12.5 ns). `numberFunction.call` takes float64 arguments, not an
  array: an array went through memory, and its 16-byte copies of 8-byte
  stores stalled on store forwarding.
- Go calling compiled Lua: `callJIT` and `jitReturnToGo`; `table.sort`
  on the table and stack directly (sort 5.6 to 3.6 ms).
- `sin` and `cos` constants from memory (−12% on `sin`-heavy loops).
- Array indexing of tables in upvalues (particles −15%).

### Next

In order of expected payoff for real-time scripts such as visualisers:

1. **Kernels with calls.** Let a kernel call an intrinsic, guarding the
   callee once at loop entry (nothing in a kernel can change the upvalue
   or global it comes from), and call a Go or number function by writing
   the kernel's registers back, exiting, and re-entering after the call.
   Plasma's inner loop would then keep its numbers in registers apart
   from the call to `set`; its body without `set` measured 23 ns a pixel
   as ordinary code and 1.4 ns as a kernel without `sin`.
2. **Registers across ordinary code.** Keep numbers in FP registers
   across straight-line code between exits, not only in kernels, with
   type checks at the first use. This is the lever for fib, records and
   particles.
3. **The comparator's return to Go.** Compiled code could run the RETURN
   of the frame `callJIT` entered itself, with a call status bit on that
   frame cleared when the interpreter takes over. Replacing
   `jitReturnToGo`'s `postCall` with an inline copy measured no gain; the
   remaining cost of each comparison is spread across `call`, `preCall`,
   `pushLuaFrame` and `enterJIT`.
4. **Closures and GC.** Shrink `luaClosure`, or create closures in
   compiled code from a pre-allocated pool.
5. **More native instructions:** TFORCALL/TFORLOOP with fast paths for
   `ipairs` and `pairs` over array parts; CONCAT into a reusable buffer;
   SETLIST; vararg and tail calls.
6. **The barrier and budget in registers on amd64**, where they live in
   the context: unmeasured.

Measured and not worth it for now: loop-invariant global loads (plasma
runs the same with `set` global or local), and putting `sin` and `cos`
first in the intrinsic dispatch (0.9% on plasma, at a cost to every other
intrinsic).
