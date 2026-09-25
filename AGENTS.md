# AGENTS.md

Notes for anyone, human or agent, changing luart. The README describes the
project's goals and API; this file describes how the implementation works
today, the rules it depends on, and where performance work should go next.

## Working on the repository

- Go 1.27.1, `CGO_ENABLED=0`. CI pins every action to a full commit SHA.
- Go has generics: generic types, and since Go 1.27 generic methods, on
  generic types too. Reach for them before writing one function per
  numeric type (`PushInteger[T Integer]`, `Arg[T]`, `RawGetInt[T]`).
- Run everything CI runs before pushing:
  - `gofmt -l .` must print nothing.
  - `go generate ./...` must leave the tree unchanged; it rewrites
    `lua/vm_jit.go`.
  - `go vet ./...`.
  - `go test ./...`, which runs with the JIT at its normal threshold;
    again with `LUART_JIT_TEST=1`, which compiles every function on first
    use; again with `LUART_JIT=off`, which only interprets; and
    `-race -run JIT`.
  - `cd bench && LUART_JIT_TEST=1 go test -run 'TestSuiteAgrees|TestStandardAgrees' .`,
    and with `-tags clua54` or `-tags luajit` where those are installed.
- On an Apple silicon Mac, `GOARCH=amd64 go test ./...` runs the amd64 JIT
  under Rosetta. A `GOAMD64=v3` binary cannot run there; CI covers it on
  linux/amd64.
- On linux/amd64 with qemu-user's binfmt handler installed,
  `GOARCH=arm64 go test ./...` runs the arm64 JIT under emulation.
- luart targets Lua 5.5. The official Lua 5.5.1 suite, unmodified, is
  `lua-5.5-tests/`, run by
  `TestLua55` (lua/lua55_test.go). Its pending list says what each file
  still needs; `LUART_SUITE_PROGRESS=1` runs pending files and logs where
  they stop. Take a file off the list once it passes. The Lua 5.2 suite
  (the `lua-tests` submodule, `TestLua`) still runs the files whose
  semantics 5.5 kept; retire one, with a comment, when it tests 5.2
  behaviour 5.5 changed. C Lua 5.5 (`brew install lua`) is the reference
  for any behaviour the suite does not pin down: diff a script's output
  against it.
- Benchmarks: always pass `-ldflags=-funcalign=64`. Without it, unrelated
  changes move the interpreter loop's alignment and its timings by 5–10%.
  Compare back to back on an idle machine; on a CPU with more than one
  core complex, pin A/B runs to one (`taskset -c 0-5` on the 9900X3D).
  Every performance PR includes a full run of `bench/suite_test.go`, saved
  as the results file for its machine (`bench/suite-results-amd64.txt`,
  or `bench/suite-results.txt` for Apple M1). `bench/chart` redraws that
  machine's charts and rewrites its tables in bench/README.md, and
  `-summary` rewrites the root README's table of geometric means from
  both files (bench/README.md, Reproducing).

## Packages

- The root holds no Go package. Users import `lua` and `stdlib`.
- `lua` is the State, its API (api*.go, auxiliary.go, debug_api.go), the
  VM, the object types and the JIT. They read each other's unexported
  fields, and the JIT hard-codes their layout. File names elsewhere in
  this document are in `lua/` unless a path says otherwise. Its tests
  open `fixtures/` and `libs/` (which the Lua suite's attrib.lua uses)
  relative to `lua/`, and the suite as `../lua-tests`.
- `internal/bytecode` is the instruction format, the opcodes, the limits
  the compiler and VM share, `Number` and its arithmetic (`Arith`, so
  constant folding and the VM compute alike), numeral parsing
  (`ParseNumber`, `Numeral`) and float formatting (`FormatFloat`), and
  `Proto`, a compiled function with Go constants (nil, bool, int64,
  float64, string).
- `internal/compiler` compiles source to a `bytecode.Proto`; it knows
  nothing of the VM. `Parse` returns syntax errors as Go errors. The core
  builds its
  `prototype` from a Proto (`prototypeOf`, compile.go), keeping runtime
  state beside it: the specialised code, field caches and JIT state.
- `internal/chunk` reads and writes binary chunks as Protos (`Load`,
  `Dump`), in luart's own format, which C Lua's luac cannot read. `Load` returns malformed chunks as errors and bounds each
  allocation, but, like Lua, trusts the code of a well-formed chunk.
  `protoOf` (compile.go) converts a prototype back for `Dump`.
- `stdlib` holds the standard libraries and uses only `lua`'s public API.
  A library that needs something the API cannot do fast gets a public
  method in `lua`, as table.sort got `State.SortArray`.
- Tests that need only the public API are external (`package lua_test`)
  and call `stdlib.Open`. Tests that reach into internals stay in `lua`,
  which cannot import stdlib (it imports lua); they open the libraries
  with `openLibraries` from export_test.go, which libs_test.go, an
  external test file in the same binary, sets to `stdlib.Open`.

## The luart command

- `cmd/luart` is its own module, so the library's go.mod stays free of
  its dependencies (Bubble Tea v2, Bubbles, Lip Gloss, Chroma). It
  requires a released luart pseudo-version; `go install ...@latest`
  refuses replace directives. To use a newer library, bump it with
  `cd cmd/luart && go get github.com/matjam/luart@<commit on main>`.
- For local development against this checkout, make an untracked
  workspace: `go work init . ./cmd/luart` (go.work is gitignored; a
  committed one would put the root's `go test ./...` and bench/ in
  workspace mode).
- `standalone.go` ports lua.c: options, `LUA_INIT`, `arg`, `docall` with
  a traceback handler and SIGINT calling `State.Interrupt`, and the plain
  REPL. `tui.go` is the terminal REPL: an inline Bubble Tea program whose
  transcript goes to the scrollback through `Program.Println`.
- The TUI rules: Lua runs in a goroutine, and the state is touched only by
  the event loop while idle or by the evaluation while running. Every
  transcript line is printed from a goroutine (`print` blocks until the
  event loop takes it, which keeps order), never from `Update`.
  `capture` puts os.Stdout and os.Stderr through a pipe from startup, so
  print, io.write and child processes land in the transcript; each
  evaluation ends with `capture.Sync`, so no output is in flight when the
  program exits.
- Tests run the test binary as the command (`LUART_CLI_MAIN=1`) for lua.c
  behaviour, and drive the TUI model with key messages.

## Interpreter

- `value` (types.go) is 16 bytes: `p unsafe.Pointer` and `n float64`.
  Floats, integers, booleans and none have sentinel pointers in `p`; an
  integer keeps its int64's bits in `n`. The float and integer sentinels
  are adjacent bytes (`numberSentinels`), so `isNumber` is one subtract
  and compare, which compiled code repeats (`branchNumber`). Other kinds
  keep a kind tag in `n`'s top byte and the object in `p`; since an
  integer's bits can be any tag, a kind test must rule out both number
  sentinels first. Numbers and booleans never allocate. `value` is not
  comparable; use `rawEqual`, `hashKey` and `identical`. Table keys are
  normalised: a float with an integer value is stored as the integer.
- Numbers follow Lua 5.4: integers wrap around, `/` and `^` give floats,
  `//` and `%` floor, comparisons between integers and floats are exact
  (numbers.go), and a numeric for loop whose start and step are integers
  counts on integers (`forPrep`), never wrapping.
- `executeSwitch` (vm.go) is the interpreter loop. `prototype.exec`
  (specialise.go) holds a specialised copy of the bytecode with the same pc
  for every instruction: RR, RK and KR arithmetic, field instructions with
  constant string keys, and `opMulAddRKR`.
- Tables (tables.go, shape.go, field_cache.go) keep string keys in slots
  described by shared shapes (hidden classes). Each field instruction has a
  `fieldCache`, which can also cache a hit through a metatable's `__index`
  table. NEWTABLE sites remember the shape their last table reached.
- Number functions (number_function.go, `PushNumberFunction[F]`) are Go
  functions of float64s that CALL invokes without a Go frame. Any number
  argument converts to a float, and the result is a float, so only
  functions that always return floats (`math.sqrt`, `math.sin`) may be
  number functions.

## Coroutines

Coroutines work as C Lua 5.2's do (coroutine.go ports ldo.c's
`lua_resume`, `lua_yieldk`, `unroll` and `recover`, and lvm.c's
`luaV_finishOp`).

- **How a yield works.** A yield panics with `errYield`, unwinding the Go
  stack back to `Resume`'s `protect`. The coroutine's Lua frames stay on
  its own stack.
- **How a resume works.** `unroll` runs the frames on. A Go function
  continues through the continuation it gave `CallWithContinuation` or
  `ProtectedCallWithContinuation`. A Lua frame first finishes its
  interrupted instruction (`finishOp`), then `execute` carries on.
- **Errors inside a yieldable pcall** reach `Resume` like any error.
  `recover` then finds the pcall's frame and runs its continuation with the
  error status.
- **Why not goroutines.** A goroutine per coroutine would be simpler, but a
  switch would cost a scheduler hand-off, and a suspended coroutine that
  became garbage would leak its goroutine. This way a coroutine is only a
  stack, a switch is a panic and a recover, and the semantics are 5.2's:
  yields across pcall, metamethods and iterators, and "attempt to yield
  across a C-call boundary" for a Go function without a continuation, such
  as table.sort's comparator.

Rules any instruction that can call Lua must keep, in the interpreter and
in `jitStep`:

- `ci.savedPC` is past the instruction when it makes the call.
- A metamethod's result is on the top of the stack when it returns, as
  `callTagMethod` leaves it. `finishOp` moves it to its register.
- `finishOp` reads `prototype.Code`, the original bytecode. The specialised
  and patched copies must keep every instruction's position.
- SELF stores self in RA+1 after the lookup, not before as C does.
  `finishOp` stores it for a lookup a yield interrupted.

## Garbage collection

Go's collector frees luart's memory. A Lua collection (gc.go) adds what
Lua defines beyond freeing memory: weak tables and `__gc` finalizers. It
follows lgc.c's atomic phase.

- **What a collection does.** It marks from Lua's roots: the registry,
  the basic types' metatables, the main thread, the running thread and
  finalizers still due. Ephemeron tables are marked to convergence. It
  then clears weak values, moves unreached finalizable objects to
  `toFinalize` (resurrecting them), clears weak keys and weak values
  again, and runs the finalizers newest first. An error in one is raised
  as "error in __gc metamethod (...)".
- **When it runs.** On `collectgarbage("collect")` and `"step"`, and
  automatically once a metatable with `__mode` or `__gc` is set, since a
  state without either has nothing for it to do. The automatic pacing is
  C Lua's: collect once the state has allocated (pause − 100)% of the live
  heap the last collection kept. `checkGC` looks only after a Go
  collection, which a re-arming finalizer sentinel counts, so the check
  where C Lua steps its collector (NEWTABLE, CONCAT, CLOSURE, and calls
  and resumes from Go) is one load and compare. A collection that frees
  nothing doubles the wait, up to 64 times: every state's standard files
  have `__gc`, which would otherwise make every state collect.
- **Finalizers run on their own thread** (`finalizerThread`), not on the
  running one. A collection therefore never moves the running thread's
  stack, and the interpreter and `jitStep` keep their frame across
  NEWTABLE and CLOSURE without reloading it. A finalizer sees that thread
  as `coroutine.running()`.
- **Why not Go finalizers.** `runtime.SetFinalizer` runs on another
  goroutine at an unknown time, can't resurrect in Lua's order, and can't
  tell a weak table's entries from strong ones. A Lua-level mark over
  Lua's own roots gives 5.2's semantics: gc.lua passes.
- **Differences.** The collection is not incremental: it marks the whole
  Lua heap at once. An object only Go memory refers to, outside the
  registry and the stacks, counts as unreachable, so a Go embedder must
  keep such objects in the registry. `__mode` and `__gc` are noticed when
  the metatable is set, as 5.2 reads `__gc`. `setstepmul`, `setmajorinc`,
  `generational` and `incremental` are accepted and ignored.

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
  - `jitOrig` keeps the unpatched instructions. The counter sits at pc 0,
    each FORLOOP, and the head of every other loop (the target of a
    backward JMP or of TFORLOOP), so a function run once with a hot while
    loop compiles; after `jitThreshold` (1000) counts `compileJIT` runs,
    and `opJITEnter` goes at the entries `jitEntries` picks.
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
  - `jitEntries` leaves out entries from which compiled code would run
    fewer than `jitMinRun` instructions before an exit (`worthEntering`).
    A RETURN does not end that count: into a compiled caller it is a
    native return. Tests set `jitMinRun` to zero so that compiled code runs
    wherever it can.
  - When Go calls a compiled Lua function (`l.call`), `callJIT` runs it
    with `runJIT` straight from `preCall`, without the interpreter. A
    function that returns within `jitMinRun` instructions of pc 0
    (`returnsSoon`), such as a sort comparator, is interpreted instead,
    and pc 0 is not patched: its RETURN would go back to Go or to
    interpreted code, and on the M1 the round trip into compiled code and
    back costs about 10 ns, more than interpreting it. Compiled callers
    still call it natively. That
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
  - Compiled code handles floats only for now: an integer operand, and
    `%`, exit to the interpreter, and a numeric for loop compiles only on
    floats. Integers in compiled code are the next JIT work.
  - Moves, constants, arithmetic, comparisons, branches and numeric for
    loops. `<` and `<=` compare numbers; `==` compares any values, and
    exits only for two userdata, two tables whose first metatable is not
    known to lack `__eq`, or equal-length strings longer than
    `maxInlineCompare`.
  - Upvalues.
  - Fields through the field caches, including `__index` tables two
    deep, own fields holding nil, and string methods through the string
    metatable (whose cache `jitStep` fills), and array elements,
    including appends within capacity, of tables in registers or
    upvalues.
  - `#` of strings.
  - Native calls and returns between compiled fixed-parameter Lua
    functions.
  - `math.sqrt`, `sin` and `cos` inline. (`floor`, `ceil` and `abs`
    return integers now, and wait for integers in compiled code.)
- **Kernels:** an innermost float for loop whose body is only moves,
  float constants, arithmetic other than `%` and float comparisons keeps every Lua
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
- **Interrupt:** `State.Interrupt` sets an atomic flag from any goroutine.
  The interpreter checks it every `interruptPoll` (1024) loop iterations
  and tail calls (`pollInterrupt`: backward jumps, FORLOOP, TFORLOOP,
  TAILCALL); compiled code is checked at the budget exit and on every
  1024th entry (`enterJIT`), since a loop that leaves compiled code every
  iteration re-enters with a full budget. A new kind of loop must reach
  one of these. An atomic load on every iteration measured 6% slower on
  an interpreted numeric loop, and any check inside `runJIT`'s loop 12–18%
  slower on calls into Go; the paced checks cost 1–5% on interpreted loops
  and nothing measurable with the JIT.
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
results: AMD Ryzen 9 9900X3D (linux/amd64) and Apple M1 Pro (arm64). On
the standard benchmarks (Are We Fast Yet and three from the Benchmarks
Game) luart with the JIT takes 0.78 times as long as C Lua 5.4 on amd64
and 0.75 times on the M1, and 1.8 and 1.5 times without it.

To find where a workload leaves compiled code, count exits: log
`p.jitOrig[ip]` and `l.jitCtx.reason` after each `enterJIT` in `runJIT`
for one call of the workload. For field instructions, log the state of
`p.fields[ip]` too (own slot, `__index` table, chain length), which says
why the cache missed. An exit every iteration costs 10 ns or more; that
is how `GETTABUP` with an array index was found to exit on each of
particles' 2,000 iterations, and how every Are We Fast Yet class method
was found to miss the cache.

An interpreted result that moves by 10–25% after a change the
interpreter does not run is almost always `executeSwitch` moving: Json
and the string build swing that much with its address alone. Rebuild
both binaries with `-ldflags=-funcalign=128`, which moves it again, before
believing it.

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

- Exits are most of what is left. A call into Go costs about 12.5 ns, of
  which about 4.5 ns is compiled code around it, and the rest is the
  API's Go frame and the round trip. The other exits left on every
  iteration are allocations (`NEWTABLE`, `CLOSURE`), TAILCALL (every
  `return setmetatable(obj, mt)` constructor), GETTABLE and SETTABLE with
  keys that are not constant strings or array indices, LEN of a table,
  CONCAT, and the sort comparator's return to Go. Havlak exits 24 million
  times an iteration, most of them at NEWTABLE, CALL and TAILCALL.
- binary-trees, the one standard benchmark still slower than C Lua 5.4,
  is allocation: a NEWTABLE exit and Go's allocator for every node.
- Plasma takes about 37 ns a pixel against Go's 15: about 12 ns for the
  call to `set`, about 6 ns for each of three `sin`s (Go's `math.Sin` is
  about 4), and the rest in ordinary compiled code, which keeps every Lua
  register in memory.
- fib, records and particles are limited by ordinary compiled code:
  values in memory, a type check on every read and a barrier check on
  every store.
- Closures and records are limited by allocation, one object per
  iteration, as Lua requires.

### Done in the last rounds

Measured against the standard benchmarks, which found most of them:

- Loop heads count toward compiling (#70): a function run once with a
  hot `while` loop never compiled before (Mandelbrot −53%).
- `__index` chains in the field cache (#71), own fields holding nil and
  a second chain level in compiled reads (#74): class hierarchies built
  from metatables, as Are We Fast Yet's are, missed the cache at almost
  every method call (Richards −35%, List −38%, DeltaBlue −17%).
- `==` for every kind of value in compiled code (#72): comparisons with
  nil, strings and objects exited (Json −25%, Richards −15%).
- Field stores of nil, and into nil slots of tables whose metatable lacks
  `__newindex` (#73; Towers −28%).
- String length and string methods in compiled code (#76; Json −22%), and
  the API's argument fast paths (#77; string scan −14%).
- Before these: calls into Go through `jitExitCallGo` and
  `jitExitCallNumber` (16.6 to 12.5 ns), Go calling compiled Lua
  (`callJIT`, `jitReturnToGo`; sort 5.6 to 3.6 ms), `sin` and `cos`
  constants from memory, and array indexing of tables in upvalues.

### Next

In order of expected payoff for real-time scripts such as visualisers:

1. **Fewer, cheaper exits.** The exit itself is the cost: handling
   TAILCALL in `runJIT` instead of the interpreter measured no gain.
   Compiling a TAILCALL to a compiled Lua function as the frame
   replacement the interpreter does, and a CALL of `setmetatable` as an
   intrinsic, would remove most of the TAILCALL exits; allocating tables
   and closures from compiled code would remove the rest.
2. **Kernels with calls.** Let a kernel call an intrinsic, guarding the
   callee once at loop entry (nothing in a kernel can change the upvalue
   or global it comes from), and call a Go or number function by writing
   the kernel's registers back, exiting, and re-entering after the call.
   Plasma's inner loop would then keep its numbers in registers apart
   from the call to `set`; its body without `set` measured 23 ns a pixel
   as ordinary code and 1.4 ns as a kernel without `sin`.
3. **Registers across ordinary code.** Keep numbers in FP registers
   across straight-line code between exits, not only in kernels, with
   type checks at the first use. This is the lever for fib, records and
   particles.
4. **Short functions called from Go.** A comparator is now interpreted
   (above), which on the M1 made sort 13% faster with the JIT but only
   level with the interpreter. Compiled code could run the RETURN of the frame
   `callJIT` entered itself, with a call status bit on that frame cleared
   when the interpreter takes over, to make the crossing cheap enough to
   enter again. Replacing `jitReturnToGo`'s `postCall` with an inline
   copy measured no gain; the rest of the crossing is spread across
   `call`, `preCall`, `pushLuaFrame` and `enterJIT`.
5. **Closures and GC.** Shrink `luaClosure`, or create closures in
   compiled code from a pre-allocated pool.
6. **More native instructions:** TFORCALL/TFORLOOP with fast paths for
   `ipairs` and `pairs` over array parts; CONCAT into a reusable buffer;
   SETLIST; vararg; LEN of a table; GETTABLE and SETTABLE with string keys
   in registers.
7. **The barrier and budget in registers on amd64**, where they live in
   the context: unmeasured.
8. **The interpreter's placement.** Interpreted Json and string building
   vary by 20% with the address of `executeSwitch` alone; a layout that
   is good wherever the linker puts it would help every build without the
   JIT, Windows included.

Measured and not worth it for now: loop-invariant global loads (plasma
runs the same with `set` global or local), putting `sin` and `cos`
first in the intrinsic dispatch (0.9% on plasma, at a cost to every other
intrinsic), and running TAILCALL in `runJIT` rather than the interpreter
(no change: the exit is the cost).
