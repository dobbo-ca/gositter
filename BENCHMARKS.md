# Benchmarks: gositter against C tree-sitter 0.25.0

Measured 2026-10-16 at commit dd759fe. All ratios are gositter divided by the
other side, so below 1.00 means gositter is better. Ratios above 1.00 are bold.
The first published run (2026-10-13, commit 9b8f955) is quoted as the earlier
run where numbers moved.

## Result

Against a pure C program (no Go in the process, gcc -O2), on one thread,
geometric mean over the 21 grammars with the range in brackets:

| input | parse, cycles | parse + Child(i) walk, cycles | parse + TreeCursor walk, cycles | parse, instructions | peak RSS, steady state |
|---|---|---|---|---|---|
| real files, no error | 0.49 (0.44 to 0.57) | 0.54 (0.49 to 0.61) | 0.52 (0.48 to 0.60) | 0.64 (0.57 to 0.75) | **1.35** (1.14 to 1.77) |
| real files with an error | 0.45 (0.26 to 0.55) | 0.50 (0.29 to 0.58) | 0.48 (0.27 to 0.55) | 0.55 (0.25 to 0.70) | **1.46** (1.19 to 1.92) |
| broken files | 0.40 (0.18 to 0.54) | 0.50 (0.24 to 0.76) | 0.43 (0.20 to 0.56) | 0.48 (0.19 to 0.69) | **1.37** (1.07 to 1.93) |

On 16 threads the time ratios are within 0.05 of these. Peak RSS on 16 threads
is 0.64 (0.30 to **1.24**) on real files and 0.51 (0.21 to **1.39**) on broken files.

Against C through cgo (go-tree-sitter v0.25.0), parse on one thread costs 0.28
(0.21 to 0.32) of the cycles on real files and 0.25 on broken files.

What this says about the earlier claims:

- "Parse at 0.31 to 0.35 of the cgo cycles": confirmed, 0.28. But cgo itself
  costs 1.74 times the cycles of pure C on the same work (real files, parse,
  one thread; the binding routes every malloc through a Go callback and copies
  the source per read), so the honest ratio to C is 0.49 (0.28 x 1.74).
- "Faster than C on deep error recovery": true for most inputs, not all. Of
  254 extreme inputs that both sides finished, gositter used fewer cycles on
  247. The worst case is 1.16 times C. gositter finished 9 inputs that C did
  not (6 time limit, 2 memory cap, 1 segfault in C); the reverse never happened.
- "Memory at or below C on the extreme inputs": refuted. gositter used less
  peak RSS on 130 of 254 extreme inputs and more on 124, up to 3.51 times (3.26
  in the earlier run). On ordinary files at one thread gositter peak RSS is
  above C in 164 of 165 rows, median 1.42 times (1.93 in the earlier run).

Trees are the same as C's: `oracle cmp` found 0 differing files in all 61
lists (33,000 files), and node counts and a per-node checksum agree in all 244
walk configs. The scala real list, which the earlier run could not compare
under a 2 GB cap, was compared with an 8 GB cap. The cgo side did not finish 5
configs (scala at 16 threads under the 8 GB cap, cpp broken at 16 threads under
2 GB); gositter and C finished them. In the scala table, the cycles-vs-cgo
cells for real parse and real cursor at all threads use 2 of the 3 cgo rounds,
because round 1 of each was killed at the 8 GB cap.

## Where gositter is slower or larger than pure C

- Peak RSS at one thread: above C in 164 of the 165 rows of the tables below,
  median 1.42 times (the exception is json with an error, cursor, 0.99).
  Example: python real files, steady state, 30 MB against 24 MB (12 MB of that
  is the loaded source on both sides).
- Peak RSS at 16 threads: above C in 35 of 165 rows, median 0.73, worst 1.74
  (kotlin, broken, cursor).
- Traversal alone (walk pass minus parse pass), real files, one thread: the
  Child(i) walk costs 1.07 times the cycles of C (0.76 to 1.42) and is the only
  traversal above C. The TreeCursor walk costs 0.72 (0.43 to 0.94); against C
  built by clang it is 0.40. Parse plus Child(i) walk is below C by a wide
  margin only because the parse is that much cheaper.
- No row of the tables is above 1.00 in cycles, instructions or CPU time
  against pure C. The highest cycle ratio is 0.76 (tsx and rust, broken, Child(i)
  walk) and the highest instruction ratio is 0.79 (json, broken, Child(i) walk).
- Extreme inputs: gositter used more cycles than C on 7 of the 254 inputs
  that both finished, worst 1.16 (scala), then 1.14 (kotlin), 1.11 (scala),
  1.06 (cpp), 1.04 (java), 1.01 (json) and 1.00 (bash).
  Listed one by one at the end of this file.
- A new process that parses one 2 KB file: about 1.6 times the cycles of C from
  exec to exit across the 21 grammars (bash: 4.2 M against 2.3 M, 0.87 ms against
  0.45 ms, 1.81). The first parse itself is within 8% of C for bash, at most
  1.26 times for any grammar, and cheaper than C for 31 of 42 grammar and file
  pairs; the rest is the start of the Go runtime. Per-grammar numbers are under
  each table.

Wall time at 16 threads is below 1.00 in every row (highest 0.91, tsx broken
walk). On this shared node 612 of 984 side-configs at more than one thread got
fewer than 0.7 of the logical CPUs they asked for, so the 16-thread wall
numbers include scheduling; the cycle and CPU columns are the better guide
there.

## Independent check

This check was made on the binaries of the earlier run (commit 9b8f955), before
the changes under "Closing the gaps", and was not repeated.

A separate agent wrote its own three programs (pure C, gositter, cgo) without
seeing this harness or its results, and measured whole processes from outside
with `perf stat`, taking the per-pass cost from the difference between an
8-pass and a 2-pass run. It covered 12 grammars. Its numbers:

| claim | its result |
|---|---|
| fewer cycles than cgo, one thread | 0.23 to 0.35 on real files, 0.19 to 0.36 on broken |
| at or below pure C on time, CPU, cycles | holds in all 112 configs: parse 0.45 to 0.63, parse + cursor walk 0.73 to 0.95 |
| at or below pure C on instructions | fails for json real + walk, 1.01 |
| faster than C on extreme inputs | 59 of 64 faster, 5 slower, up to 1.28 |
| memory at or below pure C | fails: 1.04 to 3.0 times C in 111 of 112 configs |
| first parse in a new process | the parse 0.49 to 0.78 of C; the whole process 1.07 to 1.47 times the cycles |

It also built C with -O3, with clang, without the optimisation pragma, and
with a per-thread free-list allocator. The best variant for C moved the parse
ratio by 0.08 at most (0.52 to 0.57 for c, with the allocator). No variant
changed a verdict.

## Closing the gaps

Each slow case was profiled and fixed in a pull request, then rerun by a
second agent that tried to refute it. All of them are merged into main (#4 to
#8) and were measured in this run, where `oracle cmp` found 0 differing files
in all 61 lists. Numbers are ratios to pure C: before from the earlier run,
after from this one, real files at one thread unless the row says otherwise.

| case | cause | fix | before | after |
|---|---|---|---|---|
| TreeCursor walk alone | `Node` has 6 fields, so Go keeps it in memory and each copy stalls on store forwarding; the cursor is a line-by-line port of an iterator C inlines | pack `Node` into 4 fields (#4), rewrite cursor movement as one loop (#6) | 2.6 cycles | 0.72 cycles (0.43 to 0.94); parse + cursor 0.81 to 0.52 |
| Child(i) walk alone | same `Node` copies | the 4-field `Node` (#4) | 2.2 cycles | 1.07 cycles (0.76 to 1.42); parse + walk 0.68 to 0.54 |
| Child(i) on broken files | one or two UTF-16 mutants per list parse to one ERROR node with 10,000 to 38,000 children; Child(i) is quadratic there on both sides and gositter's step ran 1.5 times the instructions | leaner scan step in `Node.child` (#7) | tsx 1.16, rust 1.06 cycles | 0.76, 0.76 |
| hcl instructions | 0.56 tokens per byte, so tree balancing decides the ratio; `summarizeChildren` ran 695 instructions per call against 275 in C | in-place reduce before a trailing SHIFT_REPEAT, a straight-line summarize for two heap children, drop a redundant summarize in `compress` (#5) | 1.11 instructions, 0.81 cycles | 0.75 instructions, 0.51 cycles |
| extreme inputs | `summarizeChildren` again, plus a recovery loop that summarizes all children of the ERROR node on every recovery (C has the same quadratic) | split summarize loop, resume the ERROR summary from the node it replaces (#5) | 1.14 to 1.32 cycles (c, cpp, bash); 15 inputs above C | 0.14 to 0.18 on those three; 7 inputs above C, worst 1.16 |
| peak RSS, one thread | Go garbage and GC headroom, not the tree: the children slab was dropped and regrown, 18 KB of garbage per parse; nodes are already the size of C's | shared weak free lists for children and stack slabs, slabs given back after a huge input, external scanner reused across parses (#8) | 2.15 steady, 1.94 after 6 passes | 1.35 steady, 1.33 after 6 passes (bash real 1.77) |
| new process | Go runtime start: an empty Go main costs 1.9 M cycles against 0.5 M for an empty C main, 95% of the gap | none in the library; GOMAXPROCS=1 for one-shot tools | 1.86 (bash, 2053 B file) | 1.81 |

Against the earlier run gositter peak RSS is 0.70 of what it was in the
steady state at one thread and 0.66 at 16 threads (geometric mean over 61
lists, same C side within 2%). By set at one thread: 0.63 on real files (the
1.35 and 2.15 above), 0.84 with an error, 0.67 on broken files.

What the memory fix costs: a caller that makes its own garbage (1 MB per file
in the skeptic's driver) runs 1.5 to 3 times the GCs. The GC count follows the
caller's allocation divided by the marked heap, and fix 5 keeps a smaller
marked heap by holding the stack slab weakly (`bench/notes/r3-gc.md`). Measured
by its skeptic (`bench/notes/r3-skeptic.md`): python broken +2.6% wall at one
thread, +8.1% at 8 and +10.6% at 16; cpp broken +1.2% to +2.3%. Holding the
slab strongly brings the GC count back but puts golang broken one-thread cursor
at +8.5% peak RSS (`bench/notes/r3-gc.md`). The owner took parity with C on
memory over that speed loss. The parse and Child(i) walk rows add almost no
garbage of their own (0.6 to 1.8 KB per parse on broken files at one thread for
ruby, javascript, bash and rust), so this cost is not in them. The TreeCursor
rows do: the harness calls `Walk()` for every file, which allocates a new
`TreeCursor` whose stack grows by append, 264 KB per parse for ruby, 257 for
javascript, 172 for bash and 112 for rust on broken files at one thread
(`go_alloc_bytes` per parse, this run). The fix 5 GC cost may therefore already
show in the cursor rows (inference, not tested). `bench/notes/r3-gc.md` finds the cursor
walk driving the GC phase in the golang broken cursor row.

Not fixed or worse than the earlier run:

- Peak RSS at one thread is still above C, 1.35 steady on real files. What
  stays is the Go runtime (3.4 to 3.7 MiB at start) and the lookup table built
  at first use (0.2 to 2.1 MiB in the five grammars measured), from
  `bench/notes/mem-rootcause-budget.md`.
- The Child(i) walk alone is still above C, 1.07.
- TreeCursor walk alone is above the 0.80 target for ruby (0.94), php (0.88),
  scala (0.87), verilog (0.86), bash (0.82) and julia (0.81). python is 0.73.
  These are medians of the passes, as in the Method section.
- Extreme inputs: 7 of 254 still use more cycles than C, worst scala 1.16.
- Peak RSS of the TreeCursor walk on broken files at one thread went up
  against the earlier run for six grammars while parse and Child(i) walk rows
  of the same grammars fell (median of the three rounds): ruby 170 to 220 MB,
  typescript 103 to 118, rust 98 to 113, javascript 144 to 156, bash 139 to
  150, java 111 to 115. C is unchanged to within 1%. The cause is not
  diagnosed. These cursor rows allocate 110 to 264 KB of Go garbage per parse
  (see above), which may be linked to the fix 5 GC behaviour; that is an
  inference, not tested, and python allocates 123 KB per parse and fell.
- Peak RSS on some extreme inputs rose against the earlier run while C stayed
  flat: hcl `unterminated_a_ac3879__n100000` 489 to 576 MiB (+18%), java
  `random_bracket_soup_7022a9__n1000000` 55 to 63 (+15%, Go allocation 59 to
  76 MB), ruby `pair_0dd45c__n100000` 31 to 34 (+13%), ruby
  `rep_q_n_428283__n30000` 217 to 233 (+7%), hcl `rep_6c2f79__n100000` 416 to
  445 (+7%). Each is one parse. Instructions moved by under 2% on three of
  them, hcl `rep_6c2f79__n100000` ran 4.9% fewer and ruby `pair_0dd45c__n100000`
  5.8 times fewer. The Go malloc counts went up (hcl 506 to 609). Over all 254 extreme inputs both
  sides finished the change is mixed: 8 rose by more than 3%, 13 fell by more
  than 3%, geometric mean 0.998. The worst ratio to C went from 3.26 to 3.51
  (ruby `rep_q_n_428283__n30000`). The cause is not diagnosed.
- A new process still costs 1.6 times the cycles of C across grammars; the
  fix is outside the library.

Notes from the second pass:

- A checkpoint index for Child(i) on wide nodes (patch `walk-broken.diff`) took
  the broken-file walk to 0.14 to 0.20 of C, but it was 1.8 times slower and
  larger than today on a tree with 12,000 wide nodes. Those numbers are from
  the earlier session, no result note was kept, and they were not rerun. It was
  not merged.
- The Go-heap-only version of the memory fix (never drop slabs) gets close to
  the one-thread numbers (`bench/notes/mem-prototype-bounded-retention.md`) but
  at 16 threads peak RSS is +20% on python real and +45% on cpp broken
  (`bench/notes/mem-rootcause-budget.md`). It also keeps the huge children
  slabs that the drop rule from commit 7b4d2a8 releases after a huge input.
- `Node.Preorder` was 0.89 to 0.93 of the cycles of C's cursor walk in the
  earlier run. It was not measured in this run.
- Cycle counts moved by up to 3% between byte-identical binaries in these
  runs, so the small gains are stated in instructions.

## Method

Three sides, one process each, same work:

- gositter: `bench/main.go` + `side_gs.go`, built with CGO_ENABLED=0.
- cgo: `bench/side_cgo.go`, go-tree-sitter v0.25.0 and the grammar bindings at
  the versions of `grammars/grammars.lock`, CGO_CFLAGS `-O2 -g`.
- pure C: `bench/purec/cbench.c` linked with `lib.c` of tree-sitter 0.25.0 and
  the same `parser.c` and `scanner.c` files (sha256 checked against
  `grammars.lock` at build), `gcc -O2 -std=c11`, pthreads, default glibc
  malloc.

Each process reads the whole file list into memory, makes one parser per
thread, runs one warm-up pass and five timed passes. A worker takes the next
file from an atomic counter, parses it, and frees the previous tree at its
next parse. `walk` adds a recursive Child(i) walk that reads kind, start byte
and end byte of every node. `cursor` does the same visit with the TreeCursor,
which is the usual full walk in C. Each config ran in three rounds with the
side order rotated, so 15 timed passes per side.

- Wall is the fastest pass. CPU, instructions and cycles are the median pass,
  process-wide over all threads, so Go GC threads count. Instructions and
  cycles are `perf_event_open` user-mode counters; they agree with
  `perf stat` within 0.04%.
- Peak RSS is VmHWM. "Steady" is one process of 41 passes, because gositter's
  peak at 16 threads still grows after 6 passes.
- Allocations: Go mallocs for gositter, calls through `ts_set_allocator` for
  C, counted in a separate run.
- New process: 15 fresh processes per side and file under `perf stat`, user
  and kernel, exec to exit.
- Extreme inputs: one process per file, one parse, 4 GB cap, 30 s limit, then
  a second pass at 120 s for the files that ran out of time.
- One process at a time under a lock, `nice -n 19`, a watchdog on RSS and time.
- The scala lists ran again for cmp, matrix and mem with a cap of 8192 MB
  instead of 2048, because cgo needs more than 2 GB on them. A later row of the
  same key replaces an earlier one.

Inputs:

- `real`: files from shallow clones of the repos listed below, at most 1500
  files and 12 MB per grammar, picked by even stride. Only files that parse
  without an error.
- `realerr`: the real files whose tree has an error (most of the c, cpp and
  verilog corpus: preprocessor and macro heavy code). Lists under 100 KB are
  left out.
- `broken`: about 500 files per grammar. Mutants of real files (truncated,
  byte runs deleted, a stray bracket or quote) plus adversarial files from
  earlier test sessions.
- extreme: 267 generated pathological inputs, 10 to 16 per grammar (repeated
  tokens, unterminated strings and comments, 100,000 deep nesting, random
  bytes, bracket soup, GLR-heavy valid code, 5 MB files). The set keeps the
  inputs that were worst for gositter in earlier sessions.

What favours which side:

- A gositter tree lives in slabs owned by its parser and dies at the next
  Parse. C pays malloc and free per subtree so that a tree can outlive the
  parser. The harness keeps one tree per parser, which is gositter's only
  mode and its best case.
- The pinned `parser.c` of bash, c, cpp, csharp, julia, kotlin, ruby and
  verilog carries `#pragma GCC optimize ("O0")`, so upstream ships their lexer
  unoptimised at any -O level. With that line removed, the parse ratio for
  those 8 grammars is 0.51 against 0.50 with it (real files, one thread).
- C at -O3 uses 1.01 times the cycles of -O2 here. C built by clang 14 uses
  0.98 times.
- cgo adds -fPIC, which costs C 8% in cycles (1.08 over all configs). The
  binding costs another 1.58 times on real files, parse, one thread (about 1.7
  over all configs).
- "All cores" is 16 threads on 8 cores with SMT, the same for every side.

Not measured: arm64 (a run on an M4 Pro was stopped at 14% when the work moved
to a quieter machine), incremental reparse, queries, and trees kept past the
next parse.

## Machine

AMD Ryzen 7 9800X3D, 8 cores, 16 threads, about 5.2 GHz. Linux
7.0.0-38, in a container (Debian 12, glibc 2.36) on the Kubernetes node
gullfaxi, shared with other pods; the earlier run used the node hrimfaxi with
the same CPU. The pod has 12 GiB of memory as request and limit. 1-minute load
between 1.4 and 11.2 during the matrix run, median 3.7. go1.27.2, gcc 12.2.0,
clang 14.0.6. Run-to-run spread on one thread, as the range over the three
rounds (median over configs): 1.5% for wall, 2.9% for cycles, under 0.01% for
instructions; the earlier run had 0.9% and 1.0%. Pure C moved 0.8% in cycles
between the two runs (geometric mean of median cycles over the 366 configs
both runs have; 1.1% at one thread, 0.5% at 16), so the two nodes run at
the same speed to within that.

## Reproduce

    sh bench/build.sh                 # binaries into $BENCH_DATA/bin
    python3 bench/run.py all          # cmp, matrix, allocs, mem, first, extreme
    python3 bench/run.py extreme --timeout 120
    python3 bench/run.py cmp --grammars scala --cap 8192   # and matrix, mem
    python3 bench/report.py           # every number, about 7600 lines
    python3 bench/compact.py $BENCH_DATA/results   # the tables below

`bench/mklists.py`, `mkbroken.py`, `mkextreme.py` and `splitreal.py` build the
file lists. `bench/k8s/` holds the pod manifest and the scripts used to run in
a container. The full report of this run is `bench/results/report-linux-amd64.md`.

## Per grammar

Columns are gositter divided by pure C, except `cycles vs cgo`. `peak RSS` is
the largest of the three processes of that row after 6 passes; `steady RSS` is
after 41 passes, parse only.

### bash

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.50 | 0.50 | 0.65 | 0.50 | **1.70** | **1.77** | 0.30 | 42 |
| real | walk | 1 | 0.56 | 0.57 | 0.71 | 0.57 | **1.67** |  | 0.25 | 35 |
| real | cursor | 1 | 0.53 | 0.54 | 0.68 | 0.54 | **2.09** |  | 0.24 | 34 |
| real | parse | all | 0.49 | 0.52 | 0.64 | 0.53 | 0.91 | 0.77 | 0.32 | 295 |
| real | walk | all | 0.57 | 0.57 | 0.71 | 0.58 | 0.89 |  | 0.26 | 232 |
| real | cursor | all | 0.49 | 0.55 | 0.68 | 0.57 | **1.04** |  | 0.25 | 251 |
| realerr | parse | 1 | 0.48 | 0.47 | 0.60 | 0.47 | **1.68** | **1.71** | 0.30 | 36 |
| realerr | walk | 1 | 0.53 | 0.52 | 0.66 | 0.52 | **1.69** |  | 0.25 | 31 |
| realerr | cursor | 1 | 0.51 | 0.51 | 0.63 | 0.51 | **1.71** |  | 0.24 | 30 |
| realerr | parse | all | 0.53 | 0.50 | 0.61 | 0.50 | **1.13** | 0.85 | 0.32 | 126 |
| realerr | walk | all | 0.58 | 0.55 | 0.67 | 0.55 | **1.01** |  | 0.26 | 108 |
| realerr | cursor | all | 0.58 | 0.54 | 0.65 | 0.53 | **1.01** |  | 0.25 | 106 |
| broken | parse | 1 | 0.33 | 0.33 | 0.41 | 0.33 | **1.27** | **1.28** | 0.22 | 22 |
| broken | walk | 1 | 0.39 | 0.41 | 0.49 | 0.41 | **1.56** |  | 0.21 | 18 |
| broken | cursor | 1 | 0.37 | 0.38 | 0.45 | 0.38 | **2.23** |  | 0.20 | 18 |
| broken | parse | all | 0.29 | 0.35 | 0.41 | 0.36 | 0.47 | 0.43 | 0.24 | 94 |
| broken | walk | all | 0.52 | 0.42 | 0.50 | 0.44 | 0.41 |  | 0.23 | 53 |
| broken | cursor | all | 0.45 | 0.40 | 0.46 | 0.41 | 0.64 |  | 0.21 | 60 |

Allocations per parse, warm parser: real gositter 0.0, C 728; broken gositter 0.1, C 2480.

New process, 2053 byte file: whole process 4.21 M cycles against 2.33 M for C (**1.81**), first parse 158 us against 154 us (**1.03**).

New process, 1444 byte file: whole process 3.76 M cycles against 1.78 M for C (**2.11**), first parse 110 us against 102 us (**1.08**).

Trees that differ from C (`oracle cmp`): real 0 of 638, realerr 0 of 43, broken 0 of 501.

### c

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.47 | 0.47 | 0.57 | 0.47 | **1.35** | **1.31** | 0.29 | 46 |
| real | walk | 1 | 0.53 | 0.53 | 0.65 | 0.54 | **1.36** |  | 0.22 | 37 |
| real | cursor | 1 | 0.50 | 0.49 | 0.62 | 0.50 | **1.70** |  | 0.21 | 37 |
| real | parse | all | 0.52 | 0.46 | 0.57 | 0.47 | 0.84 | 0.55 | 0.29 | 251 |
| real | walk | all | 0.59 | 0.52 | 0.65 | 0.54 | 0.85 |  | 0.24 | 176 |
| real | cursor | all | 0.51 | 0.50 | 0.62 | 0.51 | **1.11** |  | 0.22 | 204 |
| realerr | parse | 1 | 0.45 | 0.45 | 0.53 | 0.45 | **1.27** | **1.24** | 0.28 | 38 |
| realerr | walk | 1 | 0.50 | 0.51 | 0.59 | 0.51 | **1.27** |  | 0.23 | 31 |
| realerr | cursor | 1 | 0.48 | 0.48 | 0.57 | 0.48 | **1.53** |  | 0.22 | 31 |
| realerr | parse | all | 0.46 | 0.46 | 0.53 | 0.46 | 0.65 | 0.57 | 0.29 | 234 |
| realerr | walk | all | 0.51 | 0.50 | 0.59 | 0.50 | 0.60 |  | 0.24 | 197 |
| realerr | cursor | all | 0.52 | 0.49 | 0.57 | 0.49 | 0.63 |  | 0.23 | 188 |
| broken | parse | 1 | 0.35 | 0.35 | 0.40 | 0.35 | **1.35** | **1.35** | 0.23 | 28 |
| broken | walk | 1 | 0.40 | 0.40 | 0.47 | 0.40 | **1.71** |  | 0.19 | 22 |
| broken | cursor | 1 | 0.39 | 0.40 | 0.45 | 0.40 | **2.20** |  | 0.19 | 22 |
| broken | parse | all | 0.30 | 0.37 | 0.40 | 0.37 | 0.61 | 0.51 | 0.24 | 183 |
| broken | walk | all | 0.43 | 0.42 | 0.49 | 0.42 | 0.55 |  | 0.21 | 124 |
| broken | cursor | all | 0.39 | 0.41 | 0.46 | 0.41 | 0.72 |  | 0.20 | 132 |

Allocations per parse, warm parser: real gositter 0.0, C 1434; broken gositter 0.1, C 4092.

New process, 2050 byte file: whole process 3.90 M cycles against 1.91 M for C (**2.04**), first parse 119 us against 117 us (**1.02**).

New process, 4736 byte file: whole process 5.11 M cycles against 3.79 M for C (**1.35**), first parse 257 us against 312 us (0.82).

Trees that differ from C (`oracle cmp`): real 0 of 485, realerr 0 of 364, broken 0 of 505.

### cpp

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.50 | 0.50 | 0.61 | 0.50 | **1.35** | **1.33** | 0.30 | 29 |
| real | walk | 1 | 0.54 | 0.54 | 0.67 | 0.54 | **1.34** |  | 0.24 | 24 |
| real | cursor | 1 | 0.53 | 0.52 | 0.65 | 0.52 | **1.70** |  | 0.23 | 24 |
| real | parse | all | 0.53 | 0.50 | 0.61 | 0.51 | 0.79 | 0.54 | 0.32 | 217 |
| real | walk | all | 0.57 | 0.56 | 0.67 | 0.56 | 0.76 |  | 0.25 | 181 |
| real | cursor | all | 0.55 | 0.55 | 0.65 | 0.55 | 0.81 |  | 0.24 | 180 |
| realerr | parse | 1 | 0.45 | 0.45 | 0.52 | 0.45 | **1.24** | **1.27** | 0.29 | 25 |
| realerr | walk | 1 | 0.48 | 0.48 | 0.57 | 0.48 | **1.25** |  | 0.24 | 21 |
| realerr | cursor | 1 | 0.47 | 0.47 | 0.56 | 0.47 | **1.54** |  | 0.24 | 21 |
| realerr | parse | all | 0.48 | 0.46 | 0.52 | 0.46 | 0.59 | 0.58 | 0.30 | 178 |
| realerr | walk | all | 0.52 | 0.50 | 0.57 | 0.50 | 0.67 |  | 0.26 | 151 |
| realerr | cursor | all | 0.50 | 0.49 | 0.56 | 0.49 | 0.69 |  | 0.24 | 154 |
| broken | parse | 1 | 0.53 | 0.54 | 0.68 | 0.54 | **1.34** | **1.30** | 0.34 | 13 |
| broken | walk | 1 | 0.56 | 0.56 | 0.71 | 0.56 | **1.33** |  | 0.30 | 12 |
| broken | cursor | 1 | 0.55 | 0.56 | 0.69 | 0.56 | **1.71** |  | 0.30 | 12 |
| broken | parse | all | 0.81 | 0.52 | 0.68 | 0.53 | 0.39 | 0.21 | 0.33 | 28 |
| broken | walk | all | 0.85 | 0.55 | 0.71 | 0.56 | 0.41 |  | 0.30 | 27 |
| broken | cursor | all | 0.80 | 0.54 | 0.69 | 0.55 | 0.45 |  | 0.29 | 26 |

Allocations per parse, warm parser: real gositter 0.1, C 2598; broken gositter 0.3, C 10055.

New process, 2060 byte file: whole process 4.57 M cycles against 2.94 M for C (**1.56**), first parse 199 us against 221 us (0.90).

New process, 3845 byte file: whole process 6.32 M cycles against 5.30 M for C (**1.19**), first parse 429 us against 476 us (0.90).

Trees that differ from C (`oracle cmp`): real 0 of 318, realerr 0 of 371, broken 0 of 500.

### csharp

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.49 | 0.50 | 0.59 | 0.50 | **1.29** | **1.42** | 0.28 | 25 |
| real | walk | 1 | 0.52 | 0.52 | 0.64 | 0.52 | **1.28** |  | 0.24 | 22 |
| real | cursor | 1 | 0.51 | 0.51 | 0.62 | 0.51 | **1.93** |  | 0.24 | 22 |
| real | parse | all | 0.50 | 0.50 | 0.59 | 0.50 | 0.73 | 0.56 | 0.29 | 167 |
| real | walk | all | 0.54 | 0.54 | 0.63 | 0.54 | 0.66 |  | 0.26 | 145 |
| real | cursor | all | 0.54 | 0.53 | 0.62 | 0.54 | 0.97 |  | 0.25 | 141 |
| realerr | parse | 1 | 0.44 | 0.44 | 0.52 | 0.44 | **1.38** | **1.42** | 0.26 | 26 |
| realerr | walk | 1 | 0.48 | 0.49 | 0.56 | 0.49 | **1.38** |  | 0.24 | 23 |
| realerr | cursor | 1 | 0.48 | 0.48 | 0.56 | 0.48 | **1.54** |  | 0.23 | 22 |
| realerr | parse | all | 0.48 | 0.45 | 0.52 | 0.46 | 0.89 | 0.62 | 0.27 | 166 |
| realerr | walk | all | 0.50 | 0.48 | 0.56 | 0.50 | 0.89 |  | 0.24 | 146 |
| realerr | cursor | all | 0.54 | 0.48 | 0.56 | 0.49 | 0.91 |  | 0.24 | 139 |
| broken | parse | 1 | 0.36 | 0.36 | 0.40 | 0.36 | **1.53** | **1.54** | 0.24 | 19 |
| broken | walk | 1 | 0.38 | 0.38 | 0.43 | 0.38 | **1.53** |  | 0.22 | 17 |
| broken | cursor | 1 | 0.38 | 0.38 | 0.44 | 0.38 | **1.93** |  | 0.22 | 17 |
| broken | parse | all | 0.35 | 0.39 | 0.40 | 0.38 | 0.99 | 0.98 | 0.25 | 135 |
| broken | walk | all | 0.37 | 0.41 | 0.43 | 0.41 | 0.98 |  | 0.23 | 122 |
| broken | cursor | all | 0.40 | 0.42 | 0.44 | 0.41 | **1.08** |  | 0.23 | 117 |

Allocations per parse, warm parser: real gositter 1.1, C 2379; broken gositter 1.1, C 2936.

New process, 2048 byte file: whole process 5.45 M cycles against 3.92 M for C (**1.39**), first parse 302 us against 343 us (0.88).

New process, 2109 byte file: whole process 4.34 M cycles against 2.44 M for C (**1.78**), first parse 182 us against 192 us (0.95).

Trees that differ from C (`oracle cmp`): real 0 of 1437, realerr 0 of 63, broken 0 of 497.

### golang

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.52 | 0.54 | 0.66 | 0.54 | **1.22** | **1.22** | 0.32 | 36 |
| real | walk | 1 | 0.56 | 0.56 | 0.72 | 0.56 | **1.14** |  | 0.24 | 31 |
| real | cursor | 1 | 0.55 | 0.54 | 0.69 | 0.54 | **1.68** |  | 0.23 | 30 |
| real | parse | all | 0.50 | 0.54 | 0.66 | 0.53 | 0.60 | 0.48 | 0.33 | 269 |
| real | walk | all | 0.55 | 0.59 | 0.72 | 0.58 | 0.62 |  | 0.26 | 227 |
| real | cursor | all | 0.54 | 0.57 | 0.69 | 0.57 | 0.71 |  | 0.24 | 217 |
| realerr | parse | 1 | 0.52 | 0.51 | 0.66 | 0.51 | **1.31** | **1.37** | 0.30 | 27 |
| realerr | walk | 1 | 0.57 | 0.56 | 0.71 | 0.56 | **1.17** |  | 0.24 | 23 |
| realerr | cursor | 1 | 0.54 | 0.54 | 0.69 | 0.54 | **1.32** |  | 0.23 | 22 |
| realerr | parse | all | 0.57 | 0.53 | 0.65 | 0.52 | 0.73 | 0.54 | 0.30 | 104 |
| realerr | walk | all | 0.56 | 0.55 | 0.71 | 0.56 | 0.71 |  | 0.24 | 88 |
| realerr | cursor | all | 0.55 | 0.55 | 0.69 | 0.56 | 0.68 |  | 0.23 | 87 |
| broken | parse | 1 | 0.47 | 0.48 | 0.59 | 0.48 | **1.43** | **1.42** | 0.29 | 23 |
| broken | walk | 1 | 0.52 | 0.52 | 0.66 | 0.52 | **1.70** |  | 0.23 | 19 |
| broken | cursor | 1 | 0.50 | 0.51 | 0.63 | 0.51 | **2.33** |  | 0.23 | 19 |
| broken | parse | all | 0.48 | 0.48 | 0.59 | 0.49 | 0.60 | 0.42 | 0.30 | 110 |
| broken | walk | all | 0.52 | 0.54 | 0.66 | 0.55 | 0.49 |  | 0.25 | 96 |
| broken | cursor | all | 0.50 | 0.53 | 0.63 | 0.53 | 0.72 |  | 0.24 | 97 |

Allocations per parse, warm parser: real gositter 0.0, C 1986; broken gositter 0.1, C 3596.

New process, 2052 byte file: whole process 4.20 M cycles against 2.44 M for C (**1.72**), first parse 162 us against 169 us (0.96).

New process, 2374 byte file: whole process 4.70 M cycles against 2.96 M for C (**1.59**), first parse 226 us against 218 us (**1.03**).

Trees that differ from C (`oracle cmp`): real 0 of 1440, realerr 0 of 60, broken 0 of 501.

### hcl

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.51 | 0.51 | 0.75 | 0.51 | **1.33** | **1.35** | 0.28 | 14 |
| real | walk | 1 | 0.52 | 0.52 | 0.76 | 0.52 | **1.27** |  | 0.25 | 14 |
| real | cursor | 1 | 0.50 | 0.50 | 0.74 | 0.50 | **1.88** |  | 0.25 | 13 |
| real | parse | all | 0.56 | 0.55 | 0.75 | 0.55 | 0.77 | 0.72 | 0.31 | 108 |
| real | walk | all | 0.57 | 0.56 | 0.76 | 0.56 | 0.72 |  | 0.28 | 103 |
| real | cursor | all | 0.56 | 0.55 | 0.74 | 0.55 | 0.92 |  | 0.27 | 101 |
| broken | parse | 1 | 0.47 | 0.47 | 0.69 | 0.47 | **1.33** | **1.93** | 0.28 | 10 |
| broken | walk | 1 | 0.51 | 0.50 | 0.71 | 0.50 | **1.21** |  | 0.24 | 8 |
| broken | cursor | 1 | 0.47 | 0.48 | 0.69 | 0.48 | **1.53** |  | 0.25 | 10 |
| broken | parse | all | 0.50 | 0.52 | 0.69 | 0.52 | 0.71 | 0.72 | 0.30 | 70 |
| broken | walk | all | 0.58 | 0.56 | 0.71 | 0.56 | 0.68 |  | 0.29 | 49 |
| broken | cursor | all | 0.51 | 0.53 | 0.69 | 0.53 | 0.75 |  | 0.28 | 66 |

Allocations per parse, warm parser: real gositter 0.0, C 2599; broken gositter 3.0, C 3843.

New process, 2049 byte file: whole process 4.93 M cycles against 3.77 M for C (**1.31**), first parse 236 us against 290 us (0.81).

New process, 924 byte file: whole process 4.25 M cycles against 2.55 M for C (**1.67**), first parse 147 us against 169 us (0.87).

Trees that differ from C (`oracle cmp`): real 0 of 1500, broken 0 of 497.

### java

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.48 | 0.48 | 0.60 | 0.48 | **1.40** | **1.39** | 0.29 | 45 |
| real | walk | 1 | 0.53 | 0.53 | 0.67 | 0.53 | **1.26** |  | 0.22 | 38 |
| real | cursor | 1 | 0.51 | 0.51 | 0.65 | 0.51 | **2.02** |  | 0.22 | 36 |
| real | parse | all | 0.48 | 0.48 | 0.60 | 0.47 | 0.93 | 0.84 | 0.30 | 352 |
| real | walk | all | 0.53 | 0.53 | 0.67 | 0.52 | 0.90 |  | 0.23 | 290 |
| real | cursor | all | 0.52 | 0.53 | 0.64 | 0.52 | **1.23** |  | 0.22 | 283 |
| broken | parse | 1 | 0.34 | 0.35 | 0.38 | 0.35 | **1.41** | **1.41** | 0.22 | 21 |
| broken | walk | 1 | 0.38 | 0.37 | 0.44 | 0.37 | **1.59** |  | 0.19 | 18 |
| broken | cursor | 1 | 0.37 | 0.37 | 0.43 | 0.37 | **2.35** |  | 0.19 | 18 |
| broken | parse | all | 0.23 | 0.35 | 0.38 | 0.36 | 0.47 | 0.45 | 0.23 | 87 |
| broken | walk | all | 0.28 | 0.40 | 0.45 | 0.41 | 0.51 |  | 0.21 | 69 |
| broken | cursor | all | 0.27 | 0.39 | 0.43 | 0.40 | 0.72 |  | 0.20 | 72 |

Allocations per parse, warm parser: real gositter 0.0, C 1198; broken gositter 0.2, C 2767.

New process, 2048 byte file: whole process 3.56 M cycles against 1.70 M for C (**2.09**), first parse 90 us against 90 us (0.99).

New process, 2232 byte file: whole process 4.14 M cycles against 2.57 M for C (**1.61**), first parse 153 us against 182 us (0.84).

Trees that differ from C (`oracle cmp`): real 0 of 1498, realerr 0 of 2, broken 0 of 501.

### javascript

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.49 | 0.48 | 0.64 | 0.48 | **1.20** | **1.19** | 0.29 | 38 |
| real | walk | 1 | 0.54 | 0.54 | 0.71 | 0.54 | **1.17** |  | 0.23 | 31 |
| real | cursor | 1 | 0.53 | 0.53 | 0.68 | 0.53 | **1.82** |  | 0.23 | 30 |
| real | parse | all | 0.51 | 0.51 | 0.64 | 0.50 | 0.62 | 0.57 | 0.31 | 278 |
| real | walk | all | 0.57 | 0.56 | 0.70 | 0.55 | 0.65 |  | 0.24 | 231 |
| real | cursor | all | 0.56 | 0.55 | 0.68 | 0.54 | 0.78 |  | 0.23 | 218 |
| broken | parse | 1 | 0.49 | 0.49 | 0.64 | 0.50 | **1.27** | **1.27** | 0.27 | 20 |
| broken | walk | 1 | 0.57 | 0.56 | 0.72 | 0.57 | **1.49** |  | 0.21 | 15 |
| broken | cursor | 1 | 0.53 | 0.54 | 0.68 | 0.54 | **2.14** |  | 0.22 | 16 |
| broken | parse | all | 0.59 | 0.47 | 0.64 | 0.48 | 0.62 | 0.46 | 0.28 | 72 |
| broken | walk | all | 0.67 | 0.56 | 0.74 | 0.57 | 0.51 |  | 0.24 | 60 |
| broken | cursor | all | 0.62 | 0.52 | 0.68 | 0.53 | 0.77 |  | 0.23 | 61 |

Allocations per parse, warm parser: real gositter 0.0, C 2160; broken gositter 0.1, C 5055.

New process, 2047 byte file: whole process 4.20 M cycles against 2.34 M for C (**1.79**), first parse 158 us against 161 us (0.98).

New process, 2198 byte file: whole process 3.51 M cycles against 1.76 M for C (**2.00**), first parse 97 us against 103 us (0.94).

Trees that differ from C (`oracle cmp`): real 0 of 1344, realerr 0 of 9, broken 0 of 508.

### json

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.50 | 0.51 | 0.68 | 0.51 | **1.18** | **1.18** | 0.31 | 86 |
| real | walk | 1 | 0.57 | 0.56 | 0.76 | 0.56 | **1.11** |  | 0.20 | 65 |
| real | cursor | 1 | 0.54 | 0.56 | 0.71 | 0.55 | **1.52** |  | 0.20 | 64 |
| real | parse | all | 0.52 | 0.52 | 0.67 | 0.51 | 0.84 | 0.78 | 0.32 | 572 |
| real | walk | all | 0.58 | 0.57 | 0.75 | 0.57 | 0.83 |  | 0.21 | 439 |
| real | cursor | all | 0.56 | 0.56 | 0.71 | 0.55 | 0.88 |  | 0.20 | 433 |
| realerr | parse | 1 | 0.55 | 0.55 | 0.70 | 0.55 | **1.30** | **1.45** | 0.34 | 73 |
| realerr | walk | 1 | 0.60 | 0.56 | 0.75 | 0.56 | **1.06** |  | 0.24 | 57 |
| realerr | cursor | 1 | 0.57 | 0.55 | 0.72 | 0.55 | 0.99 |  | 0.24 | 63 |
| realerr | parse | all | 0.60 | 0.59 | 0.70 | 0.59 | **1.58** | **1.44** | 0.36 | 366 |
| realerr | walk | all | 0.65 | 0.60 | 0.75 | 0.58 | **1.35** |  | 0.26 | 278 |
| realerr | cursor | all | 0.64 | 0.62 | 0.72 | 0.61 | **1.39** |  | 0.27 | 284 |
| broken | parse | 1 | 0.52 | 0.52 | 0.69 | 0.52 | **1.34** | **1.33** | 0.28 | 64 |
| broken | walk | 1 | 0.62 | 0.62 | 0.79 | 0.62 | **1.62** |  | 0.20 | 38 |
| broken | cursor | 1 | 0.55 | 0.55 | 0.73 | 0.55 | **2.38** |  | 0.19 | 49 |
| broken | parse | all | 0.48 | 0.52 | 0.68 | 0.53 | 0.59 | 0.44 | 0.31 | 404 |
| broken | walk | all | 0.77 | 0.62 | 0.78 | 0.63 | 0.68 |  | 0.22 | 131 |
| broken | cursor | all | 0.60 | 0.56 | 0.72 | 0.57 | 0.81 |  | 0.20 | 252 |

Allocations per parse, warm parser: real gositter 0.0, C 1880; broken gositter 0.1, C 2746.

New process, 2035 byte file: whole process 3.95 M cycles against 2.08 M for C (**1.90**), first parse 114 us against 110 us (**1.04**).

New process, 1464 byte file: whole process 3.40 M cycles against 1.60 M for C (**2.12**), first parse 75 us against 66 us (**1.13**).

Trees that differ from C (`oracle cmp`): real 0 of 651, realerr 0 of 38, broken 0 of 498.

### julia

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.45 | 0.45 | 0.60 | 0.45 | **1.18** | **1.18** | 0.25 | 24 |
| real | walk | 1 | 0.50 | 0.49 | 0.66 | 0.49 | **1.17** |  | 0.21 | 20 |
| real | cursor | 1 | 0.50 | 0.50 | 0.65 | 0.50 | **1.52** |  | 0.22 | 19 |
| real | parse | all | 0.48 | 0.47 | 0.60 | 0.48 | 0.49 | 0.44 | 0.28 | 172 |
| real | walk | all | 0.53 | 0.52 | 0.65 | 0.52 | 0.50 |  | 0.23 | 144 |
| real | cursor | all | 0.53 | 0.53 | 0.65 | 0.53 | 0.58 |  | 0.23 | 136 |
| realerr | parse | 1 | 0.45 | 0.45 | 0.58 | 0.45 | **1.19** | **1.19** | 0.27 | 16 |
| realerr | walk | 1 | 0.49 | 0.49 | 0.62 | 0.49 | **1.17** |  | 0.24 | 14 |
| realerr | cursor | 1 | 0.49 | 0.49 | 0.62 | 0.49 | **1.22** |  | 0.24 | 13 |
| realerr | parse | all | 0.46 | 0.47 | 0.59 | 0.46 | 0.53 | 0.39 | 0.28 | 53 |
| realerr | walk | all | 0.50 | 0.49 | 0.63 | 0.50 | 0.50 |  | 0.25 | 44 |
| realerr | cursor | all | 0.50 | 0.50 | 0.63 | 0.51 | 0.50 |  | 0.25 | 42 |
| broken | parse | 1 | 0.44 | 0.44 | 0.56 | 0.44 | **1.20** | **1.20** | 0.27 | 16 |
| broken | walk | 1 | 0.47 | 0.47 | 0.60 | 0.47 | **1.21** |  | 0.24 | 14 |
| broken | cursor | 1 | 0.48 | 0.47 | 0.59 | 0.47 | **1.54** |  | 0.24 | 14 |
| broken | parse | all | 0.46 | 0.46 | 0.56 | 0.46 | 0.57 | 0.53 | 0.29 | 93 |
| broken | walk | all | 0.48 | 0.49 | 0.60 | 0.49 | 0.50 |  | 0.26 | 85 |
| broken | cursor | all | 0.49 | 0.50 | 0.59 | 0.50 | 0.58 |  | 0.25 | 81 |

Allocations per parse, warm parser: real gositter 0.0, C 5354; broken gositter 0.0, C 5648.

New process, 2045 byte file: whole process 5.19 M cycles against 4.11 M for C (**1.26**), first parse 278 us against 362 us (0.77).

New process, 3673 byte file: whole process 6.31 M cycles against 5.51 M for C (**1.15**), first parse 405 us against 489 us (0.83).

Trees that differ from C (`oracle cmp`): real 0 of 448, realerr 0 of 23, broken 0 of 493.

### kotlin

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.52 | 0.52 | 0.62 | 0.52 | **1.43** | **1.45** | 0.32 | 24 |
| real | walk | 1 | 0.55 | 0.55 | 0.65 | 0.55 | **1.41** |  | 0.27 | 21 |
| real | cursor | 1 | 0.54 | 0.54 | 0.64 | 0.54 | **2.15** |  | 0.27 | 20 |
| real | parse | all | 0.54 | 0.51 | 0.62 | 0.51 | 0.88 | 0.78 | 0.34 | 160 |
| real | walk | all | 0.57 | 0.55 | 0.65 | 0.54 | 0.94 |  | 0.29 | 146 |
| real | cursor | all | 0.55 | 0.54 | 0.64 | 0.54 | **1.31** |  | 0.27 | 141 |
| realerr | parse | 1 | 0.49 | 0.49 | 0.58 | 0.49 | **1.57** | **1.61** | 0.32 | 38 |
| realerr | walk | 1 | 0.51 | 0.49 | 0.60 | 0.49 | **1.56** |  | 0.27 | 35 |
| realerr | cursor | 1 | 0.51 | 0.50 | 0.60 | 0.50 | **1.60** |  | 0.26 | 34 |
| realerr | parse | all | 0.50 | 0.49 | 0.58 | 0.49 | **1.29** | **1.19** | 0.34 | 128 |
| realerr | walk | all | 0.54 | 0.51 | 0.61 | 0.52 | **1.38** |  | 0.29 | 115 |
| realerr | cursor | all | 0.53 | 0.52 | 0.61 | 0.52 | **1.22** |  | 0.28 | 114 |
| broken | parse | 1 | 0.50 | 0.50 | 0.59 | 0.50 | **1.66** | **1.67** | 0.32 | 27 |
| broken | walk | 1 | 0.53 | 0.54 | 0.61 | 0.54 | **1.60** |  | 0.28 | 25 |
| broken | cursor | 1 | 0.52 | 0.52 | 0.61 | 0.52 | **2.34** |  | 0.27 | 24 |
| broken | parse | all | 0.51 | 0.53 | 0.59 | 0.52 | **1.49** | **1.39** | 0.35 | 207 |
| broken | walk | all | 0.53 | 0.53 | 0.62 | 0.53 | **1.56** |  | 0.29 | 192 |
| broken | cursor | all | 0.55 | 0.55 | 0.62 | 0.54 | **1.74** |  | 0.29 | 181 |

Allocations per parse, warm parser: real gositter 0.0, C 1385; broken gositter 0.0, C 969.

New process, 2049 byte file: whole process 4.90 M cycles against 3.22 M for C (**1.52**), first parse 243 us against 262 us (0.93).

New process, 2051 byte file: whole process 5.69 M cycles against 4.06 M for C (**1.40**), first parse 359 us against 356 us (**1.01**).

Trees that differ from C (`oracle cmp`): real 0 of 1475, realerr 0 of 25, broken 0 of 493.

### lua

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.44 | 0.44 | 0.61 | 0.44 | **1.23** | **1.26** | 0.27 | 39 |
| real | walk | 1 | 0.49 | 0.49 | 0.68 | 0.49 | **1.16** |  | 0.21 | 33 |
| real | cursor | 1 | 0.47 | 0.48 | 0.65 | 0.48 | **1.99** |  | 0.21 | 31 |
| real | parse | all | 0.48 | 0.47 | 0.61 | 0.47 | 0.73 | 0.76 | 0.29 | 310 |
| real | walk | all | 0.54 | 0.54 | 0.68 | 0.52 | 0.79 |  | 0.23 | 254 |
| real | cursor | all | 0.53 | 0.53 | 0.65 | 0.52 | **1.06** |  | 0.22 | 241 |
| broken | parse | 1 | 0.45 | 0.44 | 0.55 | 0.44 | **1.59** | **1.58** | 0.30 | 14 |
| broken | walk | 1 | 0.49 | 0.50 | 0.60 | 0.50 | **1.77** |  | 0.26 | 12 |
| broken | cursor | 1 | 0.48 | 0.49 | 0.58 | 0.49 | **2.55** |  | 0.26 | 12 |
| broken | parse | all | 0.50 | 0.45 | 0.55 | 0.46 | 0.53 | 0.36 | 0.31 | 53 |
| broken | walk | all | 0.51 | 0.48 | 0.61 | 0.49 | 0.52 |  | 0.27 | 51 |
| broken | cursor | all | 0.55 | 0.48 | 0.58 | 0.48 | 0.78 |  | 0.26 | 50 |

Allocations per parse, warm parser: real gositter 1.0, C 2028; broken gositter 1.2, C 2597.

New process, 2045 byte file: whole process 4.91 M cycles against 2.90 M for C (**1.69**), first parse 181 us against 207 us (0.88).

New process, 2857 byte file: whole process 4.96 M cycles against 3.05 M for C (**1.63**), first parse 190 us against 225 us (0.84).

Trees that differ from C (`oracle cmp`): real 0 of 1415, realerr 0 of 1, broken 0 of 501.

### php

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.45 | 0.45 | 0.60 | 0.45 | **1.33** | **1.32** | 0.25 | 34 |
| real | walk | 1 | 0.50 | 0.50 | 0.66 | 0.50 | **1.25** |  | 0.21 | 29 |
| real | cursor | 1 | 0.50 | 0.51 | 0.65 | 0.51 | **1.88** |  | 0.21 | 26 |
| real | parse | all | 0.47 | 0.47 | 0.60 | 0.47 | 0.47 | 0.30 | 0.28 | 224 |
| real | walk | all | 0.54 | 0.52 | 0.66 | 0.52 | 0.45 |  | 0.22 | 186 |
| real | cursor | all | 0.54 | 0.53 | 0.65 | 0.53 | 0.64 |  | 0.22 | 170 |
| broken | parse | 1 | 0.38 | 0.38 | 0.47 | 0.38 | **1.38** | **1.44** | 0.23 | 28 |
| broken | walk | 1 | 0.64 | 0.62 | 0.69 | 0.62 | **1.35** |  | 0.20 | 8 |
| broken | cursor | 1 | 0.43 | 0.41 | 0.53 | 0.41 | **1.55** |  | 0.20 | 22 |
| broken | parse | all | 0.41 | 0.38 | 0.47 | 0.39 | 0.46 | 0.47 | 0.24 | 143 |
| broken | walk | all | 0.81 | 0.59 | 0.69 | 0.60 | 0.50 |  | 0.21 | 15 |
| broken | cursor | all | 0.46 | 0.43 | 0.53 | 0.43 | 0.55 |  | 0.21 | 109 |

Allocations per parse, warm parser: real gositter 3.4, C 2378; broken gositter 4.9, C 3894.

New process, 2046 byte file: whole process 4.68 M cycles against 2.89 M for C (**1.62**), first parse 235 us against 239 us (0.99).

New process, 2155 byte file: whole process 4.38 M cycles against 2.88 M for C (**1.52**), first parse 176 us against 216 us (0.82).

Trees that differ from C (`oracle cmp`): real 0 of 1499, realerr 0 of 1, broken 0 of 497.

### python

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.51 | 0.50 | 0.66 | 0.50 | **1.30** | **1.26** | 0.30 | 30 |
| real | walk | 1 | 0.55 | 0.55 | 0.71 | 0.55 | **1.19** |  | 0.25 | 26 |
| real | cursor | 1 | 0.54 | 0.53 | 0.69 | 0.53 | **1.74** |  | 0.24 | 25 |
| real | parse | all | 0.53 | 0.53 | 0.66 | 0.53 | 0.55 | 0.54 | 0.33 | 226 |
| real | walk | all | 0.57 | 0.57 | 0.71 | 0.57 | 0.56 |  | 0.26 | 194 |
| real | cursor | all | 0.57 | 0.56 | 0.69 | 0.56 | 0.69 |  | 0.25 | 188 |
| broken | parse | 1 | 0.40 | 0.41 | 0.49 | 0.41 | **1.33** | **1.34** | 0.26 | 21 |
| broken | walk | 1 | 0.58 | 0.56 | 0.65 | 0.56 | **1.60** |  | 0.21 | 10 |
| broken | cursor | 1 | 0.44 | 0.45 | 0.53 | 0.44 | **2.05** |  | 0.22 | 17 |
| broken | parse | all | 0.30 | 0.43 | 0.49 | 0.43 | 0.56 | 0.52 | 0.28 | 116 |
| broken | walk | all | 0.70 | 0.56 | 0.65 | 0.56 | 0.49 |  | 0.23 | 17 |
| broken | cursor | all | 0.31 | 0.48 | 0.54 | 0.47 | 0.82 |  | 0.23 | 105 |

Allocations per parse, warm parser: real gositter 0.0, C 3454; broken gositter 0.1, C 5141.

New process, 2052 byte file: whole process 4.98 M cycles against 3.43 M for C (**1.45**), first parse 252 us against 262 us (0.96).

New process, 4532 byte file: whole process 5.54 M cycles against 4.49 M for C (**1.23**), first parse 304 us against 382 us (0.80).

Trees that differ from C (`oracle cmp`): real 0 of 1022, realerr 0 of 3, broken 0 of 507.

### ruby

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.46 | 0.46 | 0.62 | 0.46 | **1.29** | **1.49** | 0.25 | 31 |
| real | walk | 1 | 0.51 | 0.50 | 0.68 | 0.50 | **1.31** |  | 0.22 | 26 |
| real | cursor | 1 | 0.50 | 0.51 | 0.66 | 0.51 | **2.05** |  | 0.23 | 26 |
| real | parse | all | 0.50 | 0.49 | 0.62 | 0.49 | 0.66 | 0.59 | 0.28 | 252 |
| real | walk | all | 0.55 | 0.54 | 0.68 | 0.54 | 0.61 |  | 0.25 | 210 |
| real | cursor | all | 0.55 | 0.54 | 0.66 | 0.54 | **1.01** |  | 0.25 | 199 |
| broken | parse | 1 | 0.25 | 0.25 | 0.31 | 0.26 | **1.17** | **1.20** | 0.18 | 15 |
| broken | walk | 1 | 0.29 | 0.29 | 0.36 | 0.30 | **1.38** |  | 0.17 | 12 |
| broken | cursor | 1 | 0.29 | 0.30 | 0.35 | 0.30 | **2.54** |  | 0.18 | 12 |
| broken | parse | all | 0.19 | 0.28 | 0.31 | 0.28 | 0.45 | 0.43 | 0.20 | 68 |
| broken | walk | all | 0.24 | 0.32 | 0.36 | 0.32 | 0.47 |  | 0.19 | 50 |
| broken | cursor | all | 0.27 | 0.31 | 0.35 | 0.31 | **1.05** |  | 0.19 | 47 |

Allocations per parse, warm parser: real gositter 2.2, C 2266; broken gositter 2.4, C 4633.

New process, 2047 byte file: whole process 4.09 M cycles against 2.60 M for C (**1.57**), first parse 152 us against 181 us (0.84).

New process, 1869 byte file: whole process 4.34 M cycles against 2.95 M for C (**1.47**), first parse 184 us against 223 us (0.83).

Trees that differ from C (`oracle cmp`): real 0 of 1500, broken 0 of 501.

### rust

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.47 | 0.47 | 0.63 | 0.47 | **1.27** | **1.26** | 0.27 | 37 |
| real | walk | 1 | 0.53 | 0.52 | 0.70 | 0.52 | **1.21** |  | 0.22 | 29 |
| real | cursor | 1 | 0.51 | 0.50 | 0.67 | 0.50 | **1.90** |  | 0.22 | 29 |
| real | parse | all | 0.49 | 0.49 | 0.63 | 0.49 | 0.76 | 0.77 | 0.30 | 283 |
| real | walk | all | 0.55 | 0.55 | 0.70 | 0.54 | 0.73 |  | 0.24 | 223 |
| real | cursor | all | 0.54 | 0.54 | 0.67 | 0.53 | 0.98 |  | 0.23 | 218 |
| broken | parse | 1 | 0.41 | 0.40 | 0.50 | 0.41 | **1.26** | **1.26** | 0.24 | 26 |
| broken | walk | 1 | 0.76 | 0.76 | 0.75 | 0.76 | **1.52** |  | 0.20 | 4 |
| broken | cursor | 1 | 0.45 | 0.46 | 0.56 | 0.45 | **2.26** |  | 0.20 | 21 |
| broken | parse | all | 0.34 | 0.44 | 0.50 | 0.44 | 0.49 | 0.43 | 0.27 | 142 |
| broken | walk | all | 0.87 | 0.69 | 0.75 | 0.71 | 0.53 |  | 0.20 | 5 |
| broken | cursor | all | 0.47 | 0.48 | 0.56 | 0.48 | 0.70 |  | 0.22 | 103 |

Allocations per parse, warm parser: real gositter 1.0, C 2566; broken gositter 1.1, C 4326.

New process, 2052 byte file: whole process 4.11 M cycles against 2.58 M for C (**1.59**), first parse 170 us against 195 us (0.87).

New process, 3520 byte file: whole process 4.56 M cycles against 3.22 M for C (**1.42**), first parse 211 us against 256 us (0.82).

Trees that differ from C (`oracle cmp`): real 0 of 1042, realerr 0 of 1, broken 0 of 501.

### scala

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.53 | 0.53 | 0.67 | 0.53 | **1.68** | **1.72** | 0.21 | 15 |
| real | walk | 1 | 0.56 | 0.57 | 0.70 | 0.57 | **1.62** |  | 0.21 | 14 |
| real | cursor | 1 | 0.56 | 0.56 | 0.69 | 0.56 | **2.53** |  | 0.20 | 14 |
| real | parse | all | 0.53 | 0.53 | 0.67 | 0.53 | 0.79 | 0.73 | 0.20 | 114 |
| real | walk | all | 0.56 | 0.55 | 0.70 | 0.56 | 0.84 |  | 0.19 | 103 |
| real | cursor | all | 0.56 | 0.55 | 0.69 | 0.55 | 0.98 |  | 0.19 | 101 |
| realerr | parse | 1 | 0.53 | 0.53 | 0.66 | 0.53 | **1.85** | **1.92** | 0.19 | 14 |
| realerr | walk | 1 | 0.55 | 0.58 | 0.69 | 0.58 | **1.74** |  | 0.19 | 13 |
| realerr | cursor | 1 | 0.54 | 0.54 | 0.68 | 0.54 | **1.89** |  | 0.19 | 13 |
| realerr | parse | all | 0.57 | 0.52 | 0.66 | 0.53 | **1.06** | 0.94 | 0.16 | 90 |
| realerr | walk | all | 0.56 | 0.54 | 0.69 | 0.55 | **1.04** |  | 0.16 | 80 |
| realerr | cursor | all | 0.68 | 0.54 | 0.68 | 0.55 | **1.01** |  | 0.16 | 78 |
| broken | parse | 1 | 0.51 | 0.52 | 0.62 | 0.52 | **1.66** | **1.65** | 0.24 | 14 |
| broken | walk | 1 | 0.60 | 0.59 | 0.69 | 0.59 | **1.55** |  | 0.22 | 9 |
| broken | cursor | 1 | 0.53 | 0.53 | 0.64 | 0.53 | **1.90** |  | 0.22 | 13 |
| broken | parse | all | 0.52 | 0.52 | 0.62 | 0.51 | 0.90 | 0.74 | 0.22 | 113 |
| broken | walk | all | 0.77 | 0.59 | 0.69 | 0.59 | 0.96 |  | 0.22 | 35 |
| broken | cursor | all | 0.54 | 0.53 | 0.64 | 0.53 | 0.99 |  | 0.21 | 102 |

Allocations per parse, warm parser: real gositter 2.0, C 3208; broken gositter 2.0, C 3105.

New process, 2049 byte file: whole process 4.85 M cycles against 3.49 M for C (**1.39**), first parse 254 us against 287 us (0.89).

New process, 2732 byte file: whole process 5.59 M cycles against 4.44 M for C (**1.26**), first parse 327 us against 391 us (0.84).

Trees that differ from C (`oracle cmp`): real 0 of 773, realerr 0 of 84, broken 0 of 497.

### tsx

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.51 | 0.51 | 0.66 | 0.51 | **1.53** | **1.58** | 0.30 | 38 |
| real | walk | 1 | 0.56 | 0.57 | 0.73 | 0.57 | **1.42** |  | 0.24 | 32 |
| real | cursor | 1 | 0.54 | 0.56 | 0.70 | 0.55 | **1.93** |  | 0.24 | 31 |
| real | parse | all | 0.52 | 0.53 | 0.66 | 0.52 | **1.18** | **1.10** | 0.33 | 274 |
| real | walk | all | 0.57 | 0.58 | 0.72 | 0.58 | **1.18** |  | 0.26 | 228 |
| real | cursor | all | 0.57 | 0.58 | 0.70 | 0.57 | **1.31** |  | 0.24 | 217 |
| realerr | parse | 1 | 0.44 | 0.43 | 0.52 | 0.43 | **1.69** | **1.66** | 0.31 | 59 |
| realerr | walk | 1 | 0.48 | 0.47 | 0.57 | 0.47 | **1.39** |  | 0.26 | 52 |
| realerr | cursor | 1 | 0.46 | 0.47 | 0.55 | 0.47 | **1.48** |  | 0.26 | 52 |
| realerr | parse | all | 0.33 | 0.48 | 0.54 | 0.48 | **1.62** | **1.54** | 0.34 | 270 |
| realerr | walk | all | 0.37 | 0.53 | 0.59 | 0.53 | **1.54** |  | 0.30 | 239 |
| realerr | cursor | all | 0.36 | 0.52 | 0.57 | 0.51 | **1.59** |  | 0.28 | 236 |
| broken | parse | 1 | 0.41 | 0.41 | 0.46 | 0.41 | **1.08** | **1.07** | 0.27 | 26 |
| broken | walk | 1 | 0.79 | 0.76 | 0.74 | 0.76 | **1.03** |  | 0.21 | 4 |
| broken | cursor | 1 | 0.44 | 0.44 | 0.50 | 0.44 | **1.33** |  | 0.23 | 23 |
| broken | parse | all | 0.38 | 0.43 | 0.47 | 0.43 | 0.48 | 0.37 | 0.29 | 148 |
| broken | walk | all | 0.91 | 0.71 | 0.74 | 0.72 | 0.53 |  | 0.21 | 5 |
| broken | cursor | all | 0.41 | 0.45 | 0.51 | 0.46 | 0.54 |  | 0.24 | 134 |

Allocations per parse, warm parser: real gositter 0.0, C 1098; broken gositter 0.1, C 1854.

New process, 2041 byte file: whole process 4.46 M cycles against 2.55 M for C (**1.75**), first parse 234 us against 186 us (**1.26**).

New process, 975 byte file: whole process 4.02 M cycles against 2.15 M for C (**1.87**), first parse 143 us against 141 us (**1.01**).

Trees that differ from C (`oracle cmp`): real 0 of 578, realerr 0 of 45, broken 0 of 497.

### typescript

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.50 | 0.50 | 0.64 | 0.50 | **1.26** | **1.26** | 0.31 | 36 |
| real | walk | 1 | 0.54 | 0.54 | 0.71 | 0.54 | **1.20** |  | 0.24 | 30 |
| real | cursor | 1 | 0.54 | 0.54 | 0.68 | 0.54 | **1.88** |  | 0.23 | 29 |
| real | parse | all | 0.52 | 0.52 | 0.64 | 0.51 | 0.64 | 0.57 | 0.33 | 278 |
| real | walk | all | 0.57 | 0.57 | 0.71 | 0.56 | 0.63 |  | 0.25 | 230 |
| real | cursor | all | 0.56 | 0.56 | 0.68 | 0.55 | 0.78 |  | 0.24 | 221 |
| realerr | parse | 1 | 0.48 | 0.47 | 0.57 | 0.47 | **1.59** | **1.74** | 0.30 | 24 |
| realerr | walk | 1 | 0.52 | 0.52 | 0.62 | 0.51 | **1.46** |  | 0.25 | 21 |
| realerr | cursor | 1 | 0.52 | 0.52 | 0.61 | 0.51 | **1.45** |  | 0.24 | 20 |
| realerr | parse | all | 0.55 | 0.50 | 0.60 | 0.51 | **1.21** | **1.16** | 0.32 | 56 |
| realerr | walk | all | 0.61 | 0.57 | 0.65 | 0.57 | **1.34** |  | 0.29 | 48 |
| realerr | cursor | all | 0.60 | 0.55 | 0.64 | 0.54 | **1.33** |  | 0.26 | 44 |
| broken | parse | 1 | 0.36 | 0.37 | 0.43 | 0.37 | **1.27** | **1.27** | 0.24 | 21 |
| broken | walk | 1 | 0.75 | 0.68 | 0.74 | 0.68 | **1.52** |  | 0.20 | 3 |
| broken | cursor | 1 | 0.39 | 0.40 | 0.48 | 0.40 | **2.27** |  | 0.21 | 17 |
| broken | parse | all | 0.39 | 0.39 | 0.44 | 0.39 | 0.47 | 0.43 | 0.26 | 121 |
| broken | walk | all | 0.89 | 0.70 | 0.74 | 0.71 | 0.51 |  | 0.21 | 3 |
| broken | cursor | all | 0.44 | 0.42 | 0.48 | 0.43 | 0.72 |  | 0.22 | 102 |

Allocations per parse, warm parser: real gositter 0.0, C 1997; broken gositter 0.1, C 4319.

New process, 2046 byte file: whole process 4.30 M cycles against 2.56 M for C (**1.68**), first parse 183 us against 194 us (0.94).

New process, 1131 byte file: whole process 4.07 M cycles against 2.24 M for C (**1.82**), first parse 147 us against 152 us (0.97).

Trees that differ from C (`oracle cmp`): real 0 of 1431, realerr 0 of 69, broken 0 of 507.

### verilog

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.58 | 0.57 | 0.70 | 0.57 | **1.35** | **1.39** | 0.32 | 23 |
| real | walk | 1 | 0.61 | 0.61 | 0.75 | 0.61 | **1.35** |  | 0.25 | 20 |
| real | cursor | 1 | 0.61 | 0.60 | 0.73 | 0.60 | **1.41** |  | 0.23 | 19 |
| real | parse | all | 0.61 | 0.59 | 0.70 | 0.61 | **1.32** | **1.24** | 0.33 | 171 |
| real | walk | all | 0.64 | 0.64 | 0.75 | 0.66 | **1.31** |  | 0.26 | 151 |
| real | cursor | all | 0.62 | 0.65 | 0.73 | 0.64 | **1.36** |  | 0.24 | 146 |
| realerr | parse | 1 | 0.26 | 0.26 | 0.25 | 0.26 | **1.33** | **1.34** | 0.20 | 12 |
| realerr | walk | 1 | 0.30 | 0.29 | 0.30 | 0.29 | **1.35** |  | 0.18 | 10 |
| realerr | cursor | 1 | 0.28 | 0.27 | 0.28 | 0.27 | **1.52** |  | 0.18 | 11 |
| realerr | parse | all | 0.17 | 0.27 | 0.26 | 0.26 | 0.75 | 0.53 | 0.20 | 56 |
| realerr | walk | all | 0.23 | 0.30 | 0.30 | 0.29 | 0.75 |  | 0.19 | 37 |
| realerr | cursor | all | 0.17 | 0.29 | 0.28 | 0.28 | 0.83 |  | 0.18 | 54 |
| broken | parse | 1 | 0.19 | 0.18 | 0.19 | 0.18 | **1.39** | **1.36** | 0.15 | 10 |
| broken | walk | 1 | 0.25 | 0.24 | 0.26 | 0.24 | **1.38** |  | 0.16 | 7 |
| broken | cursor | 1 | 0.19 | 0.20 | 0.20 | 0.20 | **1.57** |  | 0.15 | 9 |
| broken | parse | all | 0.15 | 0.20 | 0.19 | 0.19 | 0.72 | 0.71 | 0.16 | 49 |
| broken | walk | all | 0.22 | 0.25 | 0.26 | 0.24 | 0.75 |  | 0.16 | 28 |
| broken | cursor | all | 0.16 | 0.22 | 0.20 | 0.21 | 0.77 |  | 0.15 | 44 |

Allocations per parse, warm parser: real gositter 0.1, C 1207; broken gositter 0.0, C 8348.

New process, 2048 byte file: whole process 6.34 M cycles against 4.64 M for C (**1.37**), first parse 428 us against 428 us (1.00).

New process, 1245 byte file: whole process 3.64 M cycles against 1.68 M for C (**2.17**), first parse 103 us against 95 us (**1.08**).

Trees that differ from C (`oracle cmp`): real 0 of 195, realerr 0 of 441, broken 0 of 497.

### zig

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.46 | 0.48 | 0.62 | 0.48 | **1.12** | **1.14** | 0.26 | 40 |
| real | walk | 1 | 0.53 | 0.52 | 0.69 | 0.52 | **1.07** |  | 0.21 | 31 |
| real | cursor | 1 | 0.50 | 0.50 | 0.67 | 0.50 | **1.45** |  | 0.21 | 30 |
| real | parse | all | 0.48 | 0.48 | 0.62 | 0.48 | 0.58 | 0.45 | 0.27 | 290 |
| real | walk | all | 0.55 | 0.55 | 0.69 | 0.54 | 0.63 |  | 0.23 | 230 |
| real | cursor | all | 0.55 | 0.54 | 0.66 | 0.54 | 0.64 |  | 0.22 | 215 |
| realerr | parse | 1 | 0.46 | 0.41 | 0.63 | 0.41 | **1.26** | **1.30** | 0.24 | 42 |
| realerr | walk | 1 | 0.52 | 0.57 | 0.70 | 0.57 | **1.13** |  | 0.22 | 33 |
| realerr | cursor | 1 | 0.50 | 0.52 | 0.66 | 0.52 | **1.14** |  | 0.21 | 32 |
| realerr | parse | all | 0.45 | 0.44 | 0.61 | 0.46 | 0.64 | 0.50 | 0.26 | 106 |
| realerr | walk | all | 0.51 | 0.51 | 0.68 | 0.52 | 0.64 |  | 0.21 | 88 |
| realerr | cursor | all | 0.52 | 0.52 | 0.67 | 0.53 | 0.68 |  | 0.22 | 87 |
| broken | parse | 1 | 0.33 | 0.36 | 0.43 | 0.36 | **1.18** | **1.23** | 0.23 | 27 |
| broken | walk | 1 | 0.42 | 0.45 | 0.52 | 0.45 | **1.14** |  | 0.20 | 19 |
| broken | cursor | 1 | 0.38 | 0.38 | 0.47 | 0.38 | **1.57** |  | 0.20 | 23 |
| broken | parse | all | 0.24 | 0.39 | 0.43 | 0.38 | 0.75 | 0.69 | 0.25 | 185 |
| broken | walk | all | 0.38 | 0.48 | 0.52 | 0.47 | 0.79 |  | 0.22 | 104 |
| broken | cursor | all | 0.27 | 0.45 | 0.47 | 0.43 | 0.82 |  | 0.22 | 169 |

Allocations per parse, warm parser: real gositter 0.0, C 7998; broken gositter 0.0, C 7739.

New process, 2021 byte file: whole process 4.06 M cycles against 2.26 M for C (**1.79**), first parse 148 us against 142 us (**1.05**).

New process, 11749 byte file: whole process 8.56 M cycles against 9.21 M for C (0.93), first parse 603 us against 843 us (0.72).

Trees that differ from C (`oracle cmp`): real 0 of 423, realerr 0 of 15, broken 0 of 478.

## Extreme inputs

One process per file, one parse, one thread. Times are wall; a killed process shows the time and RSS at the kill.

| grammar | inputs | both finished | gositter fewer cycles | gositter less RSS | worst cycles ratio | worst RSS ratio | only gositter finished | only C finished | neither finished |
|---|---|---|---|---|---|---|---|---|---|
| bash | 12 | 11 | 10 | 7 | 1.00 | **2.31** | 1 | 0 | 0 |
| c | 14 | 13 | 13 | 7 | 0.98 | **1.62** | 0 | 0 | 1 |
| cpp | 13 | 12 | 11 | 7 | **1.06** | **1.62** | 0 | 0 | 1 |
| csharp | 14 | 13 | 13 | 6 | 0.78 | **1.96** | 0 | 0 | 1 |
| golang | 14 | 13 | 13 | 4 | 0.92 | **1.52** | 1 | 0 | 0 |
| hcl | 11 | 11 | 11 | 6 | 0.62 | **2.76** | 0 | 0 | 0 |
| java | 13 | 13 | 12 | 6 | **1.04** | **1.14** | 0 | 0 | 0 |
| javascript | 14 | 14 | 14 | 7 | 0.91 | **1.28** | 0 | 0 | 0 |
| json | 12 | 11 | 10 | 9 | **1.01** | **1.25** | 1 | 0 | 0 |
| julia | 16 | 16 | 16 | 11 | 0.71 | **1.50** | 0 | 0 | 0 |
| kotlin | 16 | 12 | 11 | 4 | **1.14** | **1.29** | 3 | 0 | 1 |
| lua | 10 | 10 | 10 | 4 | 0.73 | **1.51** | 0 | 0 | 0 |
| php | 11 | 11 | 11 | 5 | 0.94 | **1.36** | 0 | 0 | 0 |
| python | 12 | 12 | 12 | 6 | 0.80 | **1.23** | 0 | 0 | 0 |
| ruby | 12 | 12 | 12 | 5 | 0.70 | **3.51** | 0 | 0 | 0 |
| rust | 14 | 13 | 13 | 8 | 0.78 | **1.51** | 1 | 0 | 0 |
| scala | 14 | 14 | 12 | 5 | **1.16** | **1.74** | 0 | 0 | 0 |
| tsx | 11 | 10 | 10 | 6 | 0.94 | **1.29** | 1 | 0 | 0 |
| typescript | 12 | 12 | 12 | 4 | 0.98 | **1.49** | 0 | 0 | 0 |
| verilog | 11 | 10 | 10 | 4 | 0.91 | **2.59** | 1 | 0 | 0 |
| zig | 11 | 11 | 11 | 9 | 0.95 | **1.50** | 0 | 0 | 0 |
| all | 267 | 254 | 247 | 130 | | | 9 | 0 | 4 |

Every extreme input where gositter needs over 1.02 times the cycles or the peak RSS of C, or where a side did not finish:

| grammar | input | gositter | C | cycles ratio | RSS ratio |
|---|---|---|---|---|---|
| bash | `random_bracket_soup_7022a9__n100000.sh` | 0.01 s, 10 MB | 0.01 s, 4 MB | 1.00 | **2.31** |
| bash | `unterminated_EOF_na_n_f319ea__n30000.sh` | 7.94 s, 62 MB | 18.14 s, 49 MB | 0.44 | **1.27** |
| bash | `rep_6c2f79__n100000.sh` | 4.04 s, 21 MB | 22.80 s, 18 MB | 0.18 | **1.20** |
| bash | `advdeep_100000__sh_100000.sh` | 0.03 s, 54 MB | 0.06 s, 46 MB | 0.57 | **1.16** |
| bash | `random_bytes_697b59__n1000000.sh` | 0.16 s, 62 MB | rc=245_no_output at 0 s, 51 MB | n/a | n/a |
| c | `unterminated_define_A_c611ad__n10000.c` | 0.09 s, 7 MB | 0.62 s, 4 MB | 0.15 | **1.62** |
| c | `new_two_seq_A_n_2_V_B_n_2_define_A_n_232200__n30000.c` | 60.65 s, 28 MB | 61.89 s, 20 MB | 0.98 | **1.40** |
| c | `advdeep_100000__c_100000.c` | 0.02 s, 34 MB | 0.04 s, 26 MB | 0.55 | **1.31** |
| c | `advdeep_100000__cblk_100000.c` | 0.02 s, 32 MB | 0.03 s, 26 MB | 0.60 | **1.21** |
| c | `glr_valid_x_a_b_n_31270c__n100000.c` | 0.21 s, 106 MB | 0.28 s, 95 MB | 0.77 | **1.12** |
| c | `big5mb__big5mb.c` | 0.35 s, 313 MB | 0.63 s, 305 MB | 0.55 | **1.03** |
| c | `rep_n_aec841__n30000.c` | KILL_TIME at 120 s, 37 MB | KILL_TIME at 120 s, 23 MB | n/a | n/a |
| cpp | `x_n_x7_a_x5_26d2cc__n10000.cpp` | 6.96 s, 17 MB | 6.55 s, 11 MB | **1.06** | **1.62** |
| cpp | `unterminated_define_A_c611ad__n10000.cpp` | 0.08 s, 6 MB | 0.61 s, 4 MB | 0.14 | **1.45** |
| cpp | `advdeep_100000__cpp_100000.cpp` | 1.36 s, 722 MB | 1.69 s, 628 MB | 0.83 | **1.15** |
| cpp | `glr_valid_void_f_a_b_c_d_e_f_g_h_n_8e9ccf__n100000.cpp` | 3.03 s, 487 MB | 3.63 s, 439 MB | 0.84 | **1.11** |
| cpp | `big5mb__big5mb.cpp` | 0.32 s, 271 MB | 0.57 s, 246 MB | 0.55 | **1.10** |
| cpp | `rep_n_aec841__n30000.cpp` | KILL_TIME at 120 s, 40 MB | KILL_TIME at 120 s, 25 MB | n/a | n/a |
| csharp | `unterminated_a_ff4b90__n30000.cs` | 0.55 s, 14 MB | 3.64 s, 7 MB | 0.15 | **1.96** |
| csharp | `unterminated_a_adca5b__n30000.cs` | 0.87 s, 19 MB | 5.46 s, 11 MB | 0.16 | **1.66** |
| csharp | `unterminated_a_a452e7__n100000.cs` | 12.78 s, 26 MB | 76.57 s, 20 MB | 0.17 | **1.33** |
| csharp | `rep_n_8ed84f__n100000.cs` | 30.88 s, 56 MB | 39.37 s, 49 MB | 0.78 | **1.15** |
| csharp | `broken_stmt_var_x_new_A_1_n_82c71b__n100000.cs` | 1.09 s, 262 MB | 1.53 s, 242 MB | 0.71 | **1.08** |
| csharp | `glr_valid_x_a_b_c_n_003570__n100000.cs` | 0.59 s, 324 MB | 0.92 s, 301 MB | 0.65 | **1.07** |
| csharp | `random_bracket_soup_7022a9__n100000.cs` | 0.11 s, 21 MB | 0.26 s, 20 MB | 0.42 | **1.07** |
| csharp | `rep_n_aec841__n30000.cs` | KILL_TIME at 120 s, 44 MB | KILL_TIME at 120 s, 26 MB | n/a | n/a |
| golang | `rep_n_b5fbe6__n10000.go` | 26.23 s, 21 MB | 28.54 s, 14 MB | 0.92 | **1.52** |
| golang | `new_two_seq_A_n_2_V_B_n_2_n_a_bedb31__n30000.go` | 59.82 s, 34 MB | 65.48 s, 25 MB | 0.91 | **1.39** |
| golang | `advdeep_100000__go_100000.go` | 0.02 s, 33 MB | 0.04 s, 26 MB | 0.51 | **1.25** |
| golang | `x_n_x3_a_x5_7103f9__n80000.go` | 37.41 s, 30 MB | 44.21 s, 24 MB | 0.85 | **1.23** |
| golang | `unterminated_a_266e40__n30000.go` | 3.15 s, 22 MB | 10.61 s, 20 MB | 0.30 | **1.08** |
| golang | `broken_stmt_func_f_return_1_n_n_5b0a65__n100000.go` | 1.19 s, 144 MB | 1.88 s, 139 MB | 0.63 | **1.04** |
| golang | `big5mb__big5mb.go` | 0.50 s, 306 MB | 0.81 s, 298 MB | 0.62 | **1.03** |
| golang | `x_n_x3_a_x5_7103f9__n132000.go` | 105.26 s, 45 MB | KILL_TIME at 120 s, 37 MB | n/a | n/a |
| hcl | `rep_6c2f79__n100000.hcl` | 0.21 s, 445 MB | 0.43 s, 162 MB | 0.52 | **2.76** |
| hcl | `unterminated_a_ac3879__n100000.hcl` | 0.44 s, 576 MB | 1.55 s, 237 MB | 0.29 | **2.43** |
| hcl | `unterminated_if_a_f41bf6__n100000.hcl` | 0.47 s, 627 MB | 1.75 s, 288 MB | 0.28 | **2.18** |
| hcl | `deep_x_1_n_b2e152__n100000.hcl` | 6.86 s, 376 MB | 32.50 s, 209 MB | 0.21 | **1.80** |
| hcl | `unterminated_a_adca5b__n100000.hcl` | 12.70 s, 54 MB | 63.21 s, 44 MB | 0.20 | **1.23** |
| java | `random_bracket_soup_7022a9__n1000000.java` | 0.31 s, 63 MB | 0.59 s, 56 MB | 0.53 | **1.14** |
| java | `advdeep_100000__java_100000.java` | 0.02 s, 39 MB | 0.04 s, 36 MB | 0.56 | **1.08** |
| java | `unterminated_a_266e40__n30000.java` | 3.16 s, 25 MB | 10.70 s, 24 MB | 0.30 | **1.07** |
| java | `glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n100000.java` | 0.91 s, 528 MB | 1.37 s, 492 MB | 0.67 | **1.07** |
| java | `big5mb__big5mb.java` | 0.34 s, 291 MB | 0.59 s, 279 MB | 0.59 | **1.04** |
| java | `rep_n_b5fbe6__n100000.java` | 0.05 s, 44 MB | 0.05 s, 43 MB | **1.04** | **1.02** |
| javascript | `unterminated_a_a452e7__n100000.js` | 11.63 s, 28 MB | 60.84 s, 22 MB | 0.19 | **1.28** |
| javascript | `block_class_A_n_31164b__n100000.js` | 8.40 s, 43 MB | 43.92 s, 34 MB | 0.19 | **1.28** |
| javascript | `random_bracket_soup_7022a9__n1000000.js` | 1.92 s, 218 MB | 2.13 s, 175 MB | 0.91 | **1.25** |
| javascript | `rep_045310__n30000.js` | 7.21 s, 17 MB | 13.13 s, 14 MB | 0.55 | **1.23** |
| javascript | `glr_valid_a_b_c_n_d20807__n100000.js` | 0.23 s, 135 MB | 0.30 s, 125 MB | 0.81 | **1.08** |
| javascript | `advdeep_100000__js_100000.js` | 0.03 s, 39 MB | 0.05 s, 37 MB | 0.54 | **1.07** |
| javascript | `big5mb__big5mb.js` | 0.46 s, 341 MB | 0.75 s, 333 MB | 0.61 | **1.02** |
| json | `advdeep_100000__json_100000.json` | 0.02 s, 32 MB | 0.03 s, 26 MB | 0.62 | **1.25** |
| json | `unterminated_a_b_ceeb36__n100000.json` | 29.07 s, 87 MB | KILL_TIME at 120 s, 62 MB | n/a | n/a |
| julia | `random_printable_cb9992__n100000.jl` | 0.09 s, 11 MB | 0.18 s, 7 MB | 0.48 | **1.50** |
| julia | `rep_b0b883__n100000.jl` | 5.83 s, 27 MB | 30.18 s, 20 MB | 0.19 | **1.34** |
| julia | `random_bracket_soup_7022a9__n100000.jl` | 0.84 s, 15 MB | 1.70 s, 13 MB | 0.50 | **1.15** |
| julia | `deep_x_1_n_be5de1__n100000.jl` | 0.08 s, 128 MB | 0.17 s, 116 MB | 0.49 | **1.11** |
| julia | `unterminated_a_716541__n10000.jl` | 1.75 s, 20 MB | 4.05 s, 20 MB | 0.43 | **1.02** |
| kotlin | `unterminated_a_ac3879__n100000.kt` | 17.94 s, 40 MB | 91.11 s, 31 MB | 0.20 | **1.29** |
| kotlin | `glr_valid_val_x_a_b_c_n_837d02__n100000.kt` | 0.67 s, 259 MB | 0.89 s, 220 MB | 0.75 | **1.18** |
| kotlin | `random_printable_cb9992__n1000000.kt` | 0.01 s, 10 MB | 0.01 s, 8 MB | 0.77 | **1.18** |
| kotlin | `random_bracket_soup_7022a9__n1000000.kt` | 0.01 s, 7 MB | 0.01 s, 6 MB | 0.50 | **1.16** |
| kotlin | `glr_valid_val_y_a_b_c_n_fa3a79__n100000.kt` | 0.67 s, 253 MB | 0.92 s, 221 MB | 0.73 | **1.15** |
| kotlin | `rep_n_93b953__n100000.kt` | 0.07 s, 55 MB | 0.06 s, 54 MB | **1.14** | **1.02** |
| kotlin | `random_bytes_697b59__n1000000.kt` | 0.01 s, 7 MB | 0.01 s, 6 MB | 0.77 | **1.12** |
| kotlin | `broken_stmt_val_x_f_1_n_46f8f5__n10000.kt` | 1.47 s, 23 MB | 3.94 s, 21 MB | 0.37 | **1.10** |
| kotlin | `015_stray.kt` | 16.55 s, 3835 MB | KILL_RSS at 29 s, 4098 MB | n/a | n/a |
| kotlin | `075_stray.kt` | 13.08 s, 3833 MB | KILL_RSS at 21 s, 4096 MB | n/a | n/a |
| kotlin | `079_stray.kt` | KILL_RSS at 25 s, 4098 MB | KILL_RSS at 43 s, 4103 MB | n/a | n/a |
| kotlin | `128_stray.kt` | 119.89 s, 3580 MB | KILL_TIME at 120 s, 1913 MB | n/a | n/a |
| lua | `rep_n_160caa__n10000.lua` | 0.50 s, 8 MB | 1.52 s, 5 MB | 0.33 | **1.51** |
| lua | `random_bracket_soup_7022a9__n1000000.lua` | 0.01 s, 8 MB | 0.01 s, 6 MB | 0.73 | **1.38** |
| lua | `unterminated_a_1609f6__n10000.lua` | 1.30 s, 9 MB | 3.39 s, 7 MB | 0.38 | **1.37** |
| lua | `broken_stmt_function_f_return_1_end_n_a7c1b1__n3000.lua` | 0.26 s, 11 MB | 0.55 s, 8 MB | 0.48 | **1.31** |
| lua | `random_printable_cb9992__n1000000.lua` | 0.04 s, 7 MB | 0.08 s, 5 MB | 0.51 | **1.31** |
| lua | `advdeep_100000__lua_100000.lua` | 0.02 s, 33 MB | 0.04 s, 26 MB | 0.53 | **1.27** |
| php | `random_printable_cb9992__n1000000.php` | 2.14 s, 54 MB | 2.61 s, 40 MB | 0.82 | **1.36** |
| php | `rep_EOT_fa7752__n100000.php` | 0.23 s, 81 MB | 0.32 s, 73 MB | 0.74 | **1.11** |
| php | `deep_x_a_1_n_0eb324__n100000.php` | 0.15 s, 201 MB | 0.30 s, 186 MB | 0.49 | **1.08** |
| php | `unterminated_a_266e40__n30000.php` | 3.17 s, 29 MB | 10.77 s, 27 MB | 0.29 | **1.06** |
| php | `random_bracket_soup_7022a9__n1000000.php` | 0.88 s, 182 MB | 0.94 s, 176 MB | 0.94 | **1.04** |
| python | `pair_0dd45c__n100000.py` | 12.31 s, 33 MB | 60.50 s, 26 MB | 0.20 | **1.23** |
| python | `unterminated_f_a_7130d5__n100000.py` | 0.28 s, 79 MB | 1.13 s, 69 MB | 0.24 | **1.15** |
| python | `advdeep_100000__py_100000.py` | 0.03 s, 40 MB | 0.05 s, 37 MB | 0.51 | **1.08** |
| python | `glr_valid_print_a_for_a_in_b_n_0a52da__n100000.py` | 0.37 s, 179 MB | 0.51 s, 172 MB | 0.77 | **1.04** |
| python | `big5mb__big5mb.py` | 0.39 s, 312 MB | 0.70 s, 304 MB | 0.57 | **1.03** |
| ruby | `rep_q_n_428283__n30000.rb` | 7.82 s, 233 MB | 17.71 s, 66 MB | 0.44 | **3.51** |
| ruby | `random_bracket_soup_7022a9__n1000000.rb` | 0.02 s, 12 MB | 0.05 s, 8 MB | 0.44 | **1.53** |
| ruby | `pair_0dd45c__n100000.rb` | 12.43 s, 34 MB | 64.25 s, 29 MB | 0.19 | **1.19** |
| ruby | `unterminated_a_1942ab__n30000.rb` | 0.55 s, 30 MB | 4.03 s, 26 MB | 0.14 | **1.14** |
| ruby | `unterminated_a_1942ab__n100000.rb` | 6.55 s, 87 MB | 42.71 s, 76 MB | 0.15 | **1.14** |
| rust | `unterminated_r_a_4676a0__n10000.rs` | 0.92 s, 11 MB | 3.48 s, 7 MB | 0.27 | **1.51** |
| rust | `random_printable_cb9992__n1000000.rs` | 0.09 s, 30 MB | 0.13 s, 26 MB | 0.72 | **1.14** |
| rust | `advdeep_100000__rs_100000.rs` | 0.03 s, 40 MB | 0.05 s, 37 MB | 0.55 | **1.08** |
| rust | `big5mb__big5mb.rs` | 0.33 s, 307 MB | 0.59 s, 297 MB | 0.55 | **1.03** |
| rust | `random_bytes_697b59__n1000000.rs` | 0.50 s, 102 MB | 0.99 s, 99 MB | 0.50 | **1.03** |
| rust | `unterminated_r_a_bcb8a3__n100000.rs` | 55.19 s, 60 MB | KILL_TIME at 120 s, 43 MB | n/a | n/a |
| scala | `random_printable_cb9992__n100000.scala` | 0.05 s, 26 MB | 0.11 s, 15 MB | 0.47 | **1.74** |
| scala | `random_bracket_soup_7022a9__n10000.scala` | 0.01 s, 10 MB | 0.01 s, 8 MB | 0.80 | **1.27** |
| scala | `glr_valid_val_x_a_b_c_n_af35b2__n10000.scala` | 0.03 s, 40 MB | 0.05 s, 33 MB | 0.74 | **1.21** |
| scala | `broken_stmt_val_x_f_1_n_46f8f5__n3000.scala` | 0.02 s, 13 MB | 0.02 s, 11 MB | 0.84 | **1.17** |
| scala | `rep_n_656002__n30000.scala` | 0.03 s, 32 MB | 0.03 s, 29 MB | **1.16** | **1.09** |
| scala | `rep_n_0ca2f1__n100000.scala` | 0.06 s, 70 MB | 0.08 s, 63 MB | 0.71 | **1.11** |
| scala | `rep_n_7fd5aa__n100000.scala` | 0.10 s, 84 MB | 0.10 s, 84 MB | **1.11** | 1.00 |
| scala | `random_bytes_697b59__n100000.scala` | 0.05 s, 16 MB | 0.08 s, 14 MB | 0.59 | **1.09** |
| scala | `rep_n_59d967__n100000.scala` | 0.04 s, 58 MB | 0.06 s, 53 MB | 0.71 | **1.08** |
| tsx | `unterminated_a_a452e7__n100000.tsx` | 11.68 s, 29 MB | 61.27 s, 22 MB | 0.19 | **1.29** |
| tsx | `random_printable_cb9992__n100000.tsx` | 0.03 s, 12 MB | 0.10 s, 10 MB | 0.31 | **1.24** |
| tsx | `random_bracket_soup_7022a9__n1000000.tsx` | 1.83 s, 217 MB | 1.97 s, 176 MB | 0.94 | **1.23** |
| tsx | `rep_045310__n30000.tsx` | 4.54 s, 18 MB | 9.66 s, 15 MB | 0.47 | **1.21** |
| tsx | `block_type_A_n_f29830__n100000.tsx` | 56.37 s, 98 MB | KILL_TIME at 120 s, 62 MB | n/a | n/a |
| typescript | `random_printable_cb9992__n100000.ts` | 0.03 s, 12 MB | 0.12 s, 8 MB | 0.30 | **1.49** |
| typescript | `unterminated_a_a452e7__n100000.ts` | 10.96 s, 29 MB | 62.46 s, 22 MB | 0.18 | **1.33** |
| typescript | `random_bracket_soup_7022a9__n1000000.ts` | 2.09 s, 227 MB | 2.15 s, 178 MB | 0.98 | **1.28** |
| typescript | `rep_n_e980df__n100000.ts` | 12.63 s, 25 MB | 73.31 s, 21 MB | 0.17 | **1.20** |
| typescript | `advdeep_100000__ts_100000.ts` | 0.03 s, 41 MB | 0.05 s, 38 MB | 0.58 | **1.07** |
| typescript | `glr_valid_x_a_b_c_d_e_f_g_h_n_f95d77__n100000.ts` | 0.77 s, 393 MB | 1.21 s, 383 MB | 0.64 | **1.03** |
| typescript | `big5mb__big5mb.ts` | 0.43 s, 335 MB | 0.67 s, 328 MB | 0.65 | **1.02** |
| verilog | `pair_ebd0a6__n100000.v` | 6.38 s, 24 MB | 40.19 s, 9 MB | 0.16 | **2.59** |
| verilog | `rep_module_n_afbc83__n100000.v` | 5.47 s, 29 MB | 27.08 s, 20 MB | 0.20 | **1.48** |
| verilog | `rep_module_n_afbc83__n30000.v` | 0.51 s, 12 MB | 2.50 s, 9 MB | 0.21 | **1.37** |
| verilog | `unterminated_a_7f7cb3__n100000.v` | 0.19 s, 67 MB | 0.85 s, 63 MB | 0.22 | **1.06** |
| verilog | `broken_stmt_module_m_assign_x_a_endmodul_5e9445__n30000.v` | 0.51 s, 85 MB | 1.81 s, 82 MB | 0.28 | **1.03** |
| verilog | `block_always_posedge_c_begin_n_11163b__n100000.v` | 28.01 s, 200 MB | KILL_TIME at 120 s, 144 MB | n/a | n/a |
| zig | `unterminated_c_a_3f6b05__n100000.zig` | 12.55 s, 48 MB | 66.19 s, 32 MB | 0.19 | **1.50** |
| zig | `unterminated_a_ff4b90__n100000.zig` | 11.66 s, 28 MB | 64.25 s, 24 MB | 0.18 | **1.20** |

## Corpora

| corpus | commit | grammars (files in the real lists before the split) |
|---|---|---|
| [ripgrep](https://github.com/BurntSushi/ripgrep) | 3fce3b5bb0 | bash:2,lua:1,rust:110 |
| [tokio](https://github.com/tokio-rs/tokio) | 1989b46f5a | rust:810 |
| [newtonsoft](https://github.com/JamesNK/Newtonsoft.Json) | 52fa3aef1f | csharp:849,json:4 |
| [dapper](https://github.com/DapperLib/Dapper) | eb47546a40 | csharp:140,json:1 |
| [automapper](https://github.com/AutoMapper/AutoMapper) | 6e8697bc44 | bash:1,csharp:459,python:1 |
| [kong](https://github.com/Kong/kong) | 8599eea045 | bash:18,golang:1,hcl:16,json:27,lua:1312,python:9,rust:8 |
| [telescope](https://github.com/nvim-telescope/telescope.nvim) | 40aedd8a68 | lua:77 |
| [rails](https://github.com/rails/rails) | 6ed67f823c | bash:1,javascript:43,json:2,ruby:1424 |
| [sinatra](https://github.com/sinatra/sinatra) | cb22afd790 | ruby:61 |
| [laravel](https://github.com/laravel/framework) | 84744cdc2f | bash:4,javascript:2,json:9,php:1256 |
| [composer](https://github.com/composer/composer) | 43f7451702 | json:18,php:241 |
| [play](https://github.com/playframework/playframework) | b38332af11 | java:334,javascript:17,json:2,kotlin:1,scala:852 |
| [okhttp](https://github.com/square/okhttp) | 5a4b597d11 | bash:4,java:30,json:2,kotlin:478 |
| [coroutines](https://github.com/Kotlin/kotlinx.coroutines) | 113ee1f3c0 | bash:1,java:9,kotlin:838,python:1 |
| [dataframes](https://github.com/JuliaData/DataFrames.jl) | e867c34551 | bash:2,julia:73 |
| [flux](https://github.com/FluxML/Flux.jl) | 394703b1a6 | julia:100 |
| [plots](https://github.com/JuliaPlots/Plots.jl) | 1c6a7f66b2 | bash:1,json:1,julia:145 |
| [tigerbeetle](https://github.com/tigerbeetle/tigerbeetle) | 6e39a21fc9 | bash:2,c:2,csharp:19,golang:1,java:24,javascript:5,json:2,lua:5,python:11,ruby:9,rust:18,typescript:1,zig:248 |
| [zls](https://github.com/zigtools/zls) | eab2be0fd7 | json:1,zig:90 |
| [picorv32](https://github.com/YosysHQ/picorv32) | ef203c2b0a | bash:16,c:7,cpp:1,python:7,verilog:41 |
| [ibex](https://github.com/lowRISC/ibex) | af456e2557 | bash:12,c:4,cpp:7,json:4,python:36,rust:5,verilog:197 |
| [serv](https://github.com/olofk/serv) | f200eb2ed7 | c:3,cpp:1,python:4,verilog:78 |
| [cva6](https://github.com/openhwgroup/cva6) | 81245a47fa | bash:86,c:21,cpp:4,python:44,verilog:319 |
| [eks](https://github.com/terraform-aws-modules/terraform-aws-eks) | 91b8a8d944 | bash:6,hcl:57 |
| [vpc](https://github.com/terraform-aws-modules/terraform-aws-vpc) | b3abd6df2e | hcl:49 |
| [components](https://github.com/cloudposse/terraform-aws-components) | 1f74a966a0 | bash:17,hcl:1378,json:1,python:1 |
| [junit5](https://github.com/junit-team/junit5) | 1ef55a6a9a | bash:2,java:755,javascript:2,json:1,kotlin:180 |
| [commons-lang](https://github.com/apache/commons-lang) | 64375a58ee | java:264,json:1 |
| [ohmyzsh](https://github.com/ohmyzsh/ohmyzsh) | 083c339e06 | bash:17,python:7 |
| [nvm](https://github.com/nvm-sh/nvm) | b1b3a0038c | bash:8,javascript:1,json:1 |
| [eslint](https://github.com/eslint/eslint) | 48d5cc47e4 | javascript:1034 |
| [express](https://github.com/expressjs/express) | a419aa7350 | javascript:101 |
| [curl](https://github.com/curl/curl) | 2874cc647b | bash:30,c:317,cpp:1,python:46 |
| [k8s](https://github.com/kubernetes/kubernetes) | ab4fb775cf | bash:282,c:1,golang:1083,json:221,python:7 |
| [opencv](https://github.com/opencv/opencv) | 73a26a4231 | bash:15,c:263,cpp:660,csharp:13,java:81,javascript:31,json:9,kotlin:3,python:329,scala:4 |
| [python](https://github.com/Graphify-Labs/graphify) | 6478eb7123 | bash:1,c:1,cpp:2,csharp:6,java:1,javascript:1,json:2,julia:1,php:2,python:415,ruby:1,rust:3,scala:1,tsx:1,verilog:1,zig:1 |
| [redis](https://github.com/redis/redis) | 940a4d72fc | bash:83,c:230,cpp:8,javascript:1,json:84,lua:20,python:45,ruby:3 |
| [typescript](https://github.com/microsoft/TypeScript) | c0ca4744c9 | golang:415,javascript:5,json:37,tsx:351,typescript:711 |
| [vscode](https://github.com/microsoft/vscode) | 636f81174f | bash:70,cpp:5,csharp:14,java:2,javascript:110,json:259,julia:1,lua:1,php:1,python:62,ruby:2,rust:89,tsx:271,typescript:788 |
