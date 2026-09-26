# Standard benchmarks

Programs that other language implementations are measured with, so that
apogee's results can be set beside theirs. `standard_test.go` runs each one
in apogee with and without the JIT, in go-lua, and in C Lua 5.4 or LuaJIT
when built with their tag (see [../README.md](../README.md)).

## Are We Fast Yet

[`awfy/`](awfy) holds the Lua port of the 14 benchmarks of
[Are We Fast Yet](https://github.com/smarr/are-we-fast-yet) (Marr, Daloze
and Mössenböck, *Cross-Language Compiler Benchmarking: Are We Fast Yet?*,
DLS 2016), unchanged from commit 74306fe. Their harness is not included:
Go's testing package times them. The suite was designed to compare
language implementations on the same object-oriented code, and covers
method calls, closures, table access, strings and allocation:

| Benchmark | What it does |
|---|---|
| bounce | balls bouncing in a box; field access and method calls |
| cd | aircraft collision detection with a red-black tree |
| deltablue | a constraint solver |
| havlak | loop recognition in a large control-flow graph; allocation |
| json | parsing a JSON document |
| list | building and walking linked lists |
| mandelbrot | the Mandelbrot set; floating-point arithmetic |
| nbody | the solar system's planets; floating-point arithmetic on fields |
| permute | generating permutations recursively |
| queens | the eight queens problem |
| richards | an operating system's task scheduler |
| sieve | the sieve of Eratosthenes |
| storage | building a tree of arrays; allocation |
| towers | the towers of Hanoi |

Each runs once per iteration with the benchmark's own inner-iteration
count, which is the problem size for some: cd with 10 aircraft, deltablue
with chains of 1000 variables, havlak with 1 dummy loop (its smallest
size, which still runs for seconds), mandelbrot at 500×500, and nbody for
1000 steps. The benchmarks check their results, but nbody knows
the result only after 1 or 250,000 steps, so for it the test checks that
every interpreter computes the same energy.

The files keep their license headers: most are MIT, havlak is Apache 2.0,
richards and deltablue carry Mario Wolczko's terms, and nbody comes from
the Benchmarks Game. See [`awfy/LICENSE.md`](awfy/LICENSE.md) and
[`awfy/AUTORS.md`](awfy/AUTORS.md).

## The Computer Language Benchmarks Game

[`clbg/`](clbg) holds three of the
[Benchmarks Game](https://benchmarksgame-team.pages.debian.net/benchmarksgame/)'s
Lua programs by Mike Pall, from
[gligneul/Lua-Benchmarks](https://github.com/gligneul/Lua-Benchmarks) at
e1b3f24. Are We Fast Yet already has the Game's n-body and mandelbrot.
Each file is changed only to return a function that returns its result
instead of printing it. They are under the Game's revised BSD license,
[`clbg/LICENSE`](clbg/LICENSE).

| Benchmark | What it does |
|---|---|
| binary-trees | allocating and walking binary trees, depth 12; the garbage collector |
| fannkuch-redux | permutations of 9 elements in arrays |
| spectral-norm | the spectral norm of a 200×200 matrix; calls and arithmetic |
