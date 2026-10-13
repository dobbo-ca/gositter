# Benchmarks: gositter against C tree-sitter 0.25.0

Measured 2026-10-13 at commit 9b8f955. All ratios are gositter divided by the
other side, so below 1.00 means gositter is better. Ratios above 1.00 are bold.

## Result

Against a pure C program (no Go in the process, gcc -O2), on one thread,
geometric mean over the 21 grammars with the range in brackets:

| input | parse, cycles | parse + Child(i) walk, cycles | parse + TreeCursor walk, cycles | parse, instructions | peak RSS, steady state |
|---|---|---|---|---|---|
| real files, no error | 0.54 (0.46 to 0.81) | 0.68 (0.59 to 0.84) | 0.81 (0.71 to 0.96) | 0.69 (0.61 to **1.11**) | **2.15** (1.37 to 2.81) |
| real files with an error | 0.49 (0.31 to 0.62) | 0.61 (0.39 to 0.77) | 0.69 (0.39 to 0.85) | 0.59 (0.32 to 0.80) | **1.91** (1.48 to 2.23) |
| broken files | 0.43 (0.26 to 0.67) | 0.63 (0.36 to **1.16**) | 0.62 (0.31 to 0.97) | 0.52 (0.28 to 0.91) | **2.08** (1.62 to 2.51) |

On 16 threads the time ratios are within 0.03 of these. Peak RSS on 16 threads
is 0.97 (0.54 to **1.31**) on real files and 0.94 (0.51 to **1.52**) on broken files.

Against C through cgo (go-tree-sitter v0.25.0), parse on one thread costs 0.31
(0.25 to 0.46) of the cycles on real files and 0.27 on broken files.

What this says about the earlier claims:

- "Parse at 0.31 to 0.35 of the cgo cycles": confirmed, 0.31. But cgo itself
  costs 1.86 times the cycles of pure C on the same work (the binding routes
  every malloc through a Go callback and copies the source per read), so the
  honest ratio to C is 0.54.
- "Faster than C on deep error recovery": true for most inputs, not all. Of
  254 extreme inputs that both sides finished, gositter used fewer cycles on
  239. The worst case is 1.36 times C. gositter finished 6 inputs that C did
  not (time limit, memory cap, or a segfault in C); the reverse never happened.
- "Memory at or below C on the extreme inputs": refuted. gositter used less
  peak RSS on 131 of 254 extreme inputs and more on 123, up to 3.26 times. On
  ordinary files at one thread gositter peak RSS is above C in every row,
  about 2 times.

Trees are the same as C's: `oracle cmp` found 0 differing files in 60 of 61
lists (32,000 files), and node counts and a per-node checksum agree in all 244
walk configs. The one list not compared is scala real files, where the cgo
side of the oracle passed the 2 GB cap.

## Where gositter is slower or larger than pure C

- Peak RSS at one thread: above C in all 165 rows of the tables below, median
  1.93 times. Example: python real files, steady state, 55 MB against 24 MB (12 MB of that
  is the loaded source on both sides).
- Peak RSS at 16 threads: above C in 56 of 165 rows, median 0.92, worst 1.73
  (kotlin, broken, cursor).
- Traversal alone (walk pass minus parse pass): the TreeCursor walk costs 2.6
  times the cycles of C and Child(i) 2.3 times. Against C built by clang the
  cursor gap is 1.4 times. Parse plus walk stays below C only because the
  parse is that much cheaper.
- Parse plus Child(i) walk on broken files: tsx 1.16 times the cycles, rust
  1.06, typescript 1.03 at 16 threads. In instructions also php 1.17 and json
  1.05.
- hcl on real files: 1.11 times the instructions for parse, 1.13 with a walk.
  Cycles are still 0.81.
- Extreme inputs: listed one by one at the end of this file.
- A new process that parses one 2 KB file: about 2 times the cycles of C from
  exec to exit (bash: 4.4 M against 2.4 M, 0.89 ms against 0.46 ms). The first
  parse itself is within 8% of C for bash and cheaper for most grammars; the
  rest is the start of the Go runtime. Per-grammar numbers are under each
  table.

Wall time at 16 threads is above 1.00 in a few more rows (for example tsx
broken walk 1.43) where CPU time is not. In those rows the process got fewer
than 11 of 16 logical CPUs and the wall number is scheduling, not parsing.

## Independent check

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

Each slow case was profiled and a fix was prototyped in a scratch copy, then
rerun by a second agent that tried to refute it. None of this is in the
library yet. Every prototype kept node counts and checksums equal to C on
real, realerr and broken lists; none was run through the full `oracle cmp`.
Numbers are ratios to pure C, before and after.

| case | cause | prototype | before | after |
|---|---|---|---|---|
| TreeCursor walk alone | `Node` has 6 fields, so Go keeps it in memory and each copy stalls on store forwarding; the cursor is a line-by-line port of an iterator C inlines | pack `Node` into 4 fields, rewrite cursor movement as one loop | 2.6 cycles | 0.68 to 0.77 cycles; parse + cursor 0.81 to 0.55 |
| Child(i) walk alone | same `Node` copies | the 4-field `Node` | 2.3 cycles | 1.2 to 1.5 cycles; parse + walk stays below C |
| Child(i) on broken files | one or two UTF-16 mutants per list parse to one ERROR node with 10,000 to 38,000 children; Child(i) is quadratic there on both sides and gositter's step runs 1.5 times the instructions | leaner scan step in `Node.child` | tsx 1.18, rust 1.08 cycles | 0.97, 1.00 |
| hcl instructions | 0.56 tokens per byte, so tree balancing decides the ratio; `summarizeChildren` runs 695 instructions per call against 275 in C | in-place reduce before a trailing SHIFT_REPEAT, a straight-line summarize for two heap children, drop a redundant summarize in `compress` | 1.11 instructions, 0.81 cycles | 0.76 instructions, 0.51 cycles |
| extreme inputs | `summarizeChildren` again, plus a recovery loop that summarizes all children of the ERROR node on every recovery (C has the same quadratic) | split summarize loop, resume the ERROR summary from the node it replaces | 1.14 to 1.33 cycles (c, cpp, bash) | 0.18 to 0.22; all 15 slower inputs but one end below C with a warm parser |
| peak RSS, one thread | Go garbage and GC headroom, not the tree: the children slab is dropped and regrown, 18 KB of garbage per parse; nodes are already the size of C's | slabs outside the Go heap with mmap, external scanner reused across parses | 2.0 to 2.8 | 1.12 to 1.35 (bash real 1.70) |
| new process | Go runtime start: an empty Go main costs 1.9 M cycles against 0.5 M for an empty C main, 95% of the gap | none in the library; GOMAXPROCS=1 for one-shot tools | 2.16 | 1.68 |

Notes from the second pass:

- A checkpoint index for Child(i) on wide nodes took the broken-file walk to
  0.14 to 0.20 of C, but it was 1.8 times slower and larger than today on a
  tree with 12,000 wide nodes. Not recommended as written.
- The Go-heap-only version of the memory fix (never drop slabs) gets the same
  one-thread numbers but is 9% worse at 16 threads and brings back the
  behaviour that 7b4d2a8 removed.
- What stays after the memory fix is the Go runtime (3 to 4 MB) and the lookup
  table built at first use (1 to 3.5 MB per language).
- Callers that need a full walk today should use `Node.Preorder`: 0.89 to 0.93
  of the cycles of C's cursor walk with no change to the library.
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
  those 8 grammars is 0.54 against 0.53 with it (real files, one thread).
- C at -O3 uses 1.02 times the cycles of -O2 here. C built by clang 14 uses
  0.985 times.
- cgo adds -fPIC, which costs C 8% in cycles. The binding costs another 1.71
  times.
- "All cores" is 16 threads on 8 cores with SMT, the same for every side.

Not measured: arm64 (a run on an M4 Pro was stopped at 14% when the work moved
to a quieter machine), incremental reparse, queries, and trees kept past the
next parse.

## Machine

AMD Ryzen 7 9800X3D, 8 cores, 16 threads, about 5.2 GHz, 31 GB. Linux
7.0.0-30, in a container (Debian 12, glibc 2.36) on a Kubernetes node with
other light workloads; 1-minute load between 1.4 and 20 during the run, median
3.2. go1.27.2, gcc 12.2.0, clang 14.0.6. Run-to-run spread on one thread: 1%
for wall and cycles, under 0.4% for instructions.

## Reproduce

    sh bench/build.sh                 # binaries into $BENCH_DATA/bin
    python3 bench/run.py all          # cmp, matrix, allocs, mem, first, extreme
    python3 bench/run.py extreme --timeout 120
    python3 bench/report.py           # every number, about 9000 lines
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
| real | parse | 1 | 0.58 | 0.58 | 0.70 | 0.57 | **2.25** | **2.81** | 0.34 | 37 |
| real | walk | 1 | 0.71 | 0.71 | 0.80 | 0.71 | **2.23** |  | 0.31 | 27 |
| real | cursor | 1 | 0.82 | 0.81 | 0.89 | 0.81 | **2.27** |  | 0.36 | 23 |
| real | parse | all | 0.70 | 0.59 | 0.70 | 0.59 | **1.04** | **1.23** | 0.36 | 259 |
| real | walk | all | 0.78 | 0.68 | 0.79 | 0.70 | **1.06** |  | 0.31 | 195 |
| real | cursor | all | 0.83 | 0.78 | 0.89 | 0.80 | **1.11** |  | 0.35 | 161 |
| realerr | parse | 1 | 0.54 | 0.54 | 0.65 | 0.54 | **1.76** | **1.79** | 0.34 | 33 |
| realerr | walk | 1 | 0.67 | 0.67 | 0.74 | 0.67 | **1.72** |  | 0.31 | 25 |
| realerr | cursor | 1 | 0.75 | 0.76 | 0.82 | 0.75 | **1.76** |  | 0.36 | 21 |
| realerr | parse | all | 0.65 | 0.57 | 0.66 | 0.57 | **1.11** | 0.85 | 0.35 | 112 |
| realerr | walk | all | 0.76 | 0.67 | 0.75 | 0.67 | **1.11** |  | 0.32 | 84 |
| realerr | cursor | all | 0.80 | 0.74 | 0.83 | 0.75 | **1.15** |  | 0.35 | 78 |
| broken | parse | 1 | 0.36 | 0.36 | 0.43 | 0.37 | **2.06** | **2.30** | 0.25 | 21 |
| broken | walk | 1 | 0.49 | 0.49 | 0.55 | 0.49 | **2.82** |  | 0.25 | 14 |
| broken | cursor | 1 | 0.56 | 0.56 | 0.58 | 0.56 | **2.04** |  | 0.29 | 12 |
| broken | parse | all | 0.32 | 0.39 | 0.43 | 0.39 | 0.70 | 0.90 | 0.26 | 89 |
| broken | walk | all | 0.65 | 0.51 | 0.56 | 0.52 | 0.68 |  | 0.26 | 43 |
| broken | cursor | all | 0.76 | 0.56 | 0.58 | 0.57 | 0.79 |  | 0.29 | 37 |

Allocations per parse, warm parser: real gositter 1.4, C 728; broken gositter 1.4, C 2480.

New process, 2053 byte file: whole process 4.40 M cycles against 2.37 M for C (**1.86**), first parse 170 us against 167 us (**1.01**).

New process, 1444 byte file: whole process 3.93 M cycles against 1.81 M for C (**2.17**), first parse 122 us against 113 us (**1.08**).

Trees that differ from C (`oracle cmp`): real 0 of 638, realerr 0 of 43, broken 0 of 501.

### c

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.52 | 0.52 | 0.62 | 0.53 | **2.07** | **2.32** | 0.31 | 42 |
| real | walk | 1 | 0.71 | 0.72 | 0.75 | 0.72 | **2.13** |  | 0.31 | 27 |
| real | cursor | 1 | 0.80 | 0.82 | 0.84 | 0.81 | **2.05** |  | 0.35 | 23 |
| real | parse | all | 0.63 | 0.51 | 0.62 | 0.53 | **1.09** | 0.89 | 0.32 | 216 |
| real | walk | all | 0.98 | 0.66 | 0.75 | 0.68 | **1.04** |  | 0.30 | 112 |
| real | cursor | all | 0.95 | 0.74 | 0.83 | 0.76 | **1.11** |  | 0.33 | 119 |
| realerr | parse | 1 | 0.49 | 0.49 | 0.57 | 0.49 | **2.16** | **2.23** | 0.30 | 35 |
| realerr | walk | 1 | 0.64 | 0.64 | 0.67 | 0.64 | **2.12** |  | 0.30 | 24 |
| realerr | cursor | 1 | 0.74 | 0.74 | 0.75 | 0.74 | **2.11** |  | 0.34 | 20 |
| realerr | parse | all | 0.51 | 0.49 | 0.57 | 0.50 | 0.81 | 0.86 | 0.32 | 226 |
| realerr | walk | all | 0.75 | 0.60 | 0.67 | 0.62 | 0.83 |  | 0.29 | 141 |
| realerr | cursor | all | 0.74 | 0.70 | 0.75 | 0.70 | 0.89 |  | 0.32 | 138 |
| broken | parse | 1 | 0.38 | 0.37 | 0.43 | 0.37 | **1.93** | **2.29** | 0.24 | 26 |
| broken | walk | 1 | 0.48 | 0.49 | 0.52 | 0.49 | **2.10** |  | 0.24 | 18 |
| broken | cursor | 1 | 0.60 | 0.60 | 0.59 | 0.60 | **2.29** |  | 0.29 | 15 |
| broken | parse | all | 0.32 | 0.39 | 0.43 | 0.39 | 0.92 | **1.24** | 0.26 | 176 |
| broken | walk | all | 0.50 | 0.50 | 0.53 | 0.50 | 0.73 |  | 0.25 | 110 |
| broken | cursor | all | 0.65 | 0.57 | 0.59 | 0.58 | 0.83 |  | 0.28 | 86 |

Allocations per parse, warm parser: real gositter 0.1, C 1434; broken gositter 0.1, C 4092.

New process, 2050 byte file: whole process 4.07 M cycles against 2.01 M for C (**2.02**), first parse 125 us against 131 us (0.96).

New process, 4736 byte file: whole process 5.38 M cycles against 3.94 M for C (**1.36**), first parse 269 us against 328 us (0.82).

Trees that differ from C (`oracle cmp`): real 0 of 485, realerr 0 of 364, broken 0 of 505.

### cpp

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.53 | 0.53 | 0.64 | 0.53 | **1.77** | **2.21** | 0.32 | 27 |
| real | walk | 1 | 0.68 | 0.67 | 0.74 | 0.67 | **1.77** |  | 0.29 | 20 |
| real | cursor | 1 | 0.80 | 0.80 | 0.84 | 0.80 | **1.97** |  | 0.35 | 16 |
| real | parse | all | 0.57 | 0.54 | 0.64 | 0.55 | 0.90 | 0.89 | 0.34 | 215 |
| real | walk | all | 0.71 | 0.64 | 0.73 | 0.67 | 0.99 |  | 0.30 | 157 |
| real | cursor | all | 0.88 | 0.75 | 0.84 | 0.77 | 0.96 |  | 0.33 | 121 |
| realerr | parse | 1 | 0.47 | 0.47 | 0.55 | 0.47 | **2.15** | **2.20** | 0.30 | 24 |
| realerr | walk | 1 | 0.57 | 0.57 | 0.62 | 0.57 | **2.16** |  | 0.29 | 18 |
| realerr | cursor | 1 | 0.66 | 0.66 | 0.70 | 0.66 | **2.22** |  | 0.33 | 15 |
| realerr | parse | all | 0.49 | 0.49 | 0.55 | 0.49 | 0.94 | 0.94 | 0.32 | 179 |
| realerr | walk | all | 0.58 | 0.56 | 0.62 | 0.57 | 0.94 |  | 0.29 | 142 |
| realerr | cursor | all | 0.66 | 0.65 | 0.70 | 0.65 | 0.93 |  | 0.32 | 121 |
| broken | parse | 1 | 0.54 | 0.54 | 0.69 | 0.54 | **1.62** | **2.03** | 0.34 | 13 |
| broken | walk | 1 | 0.61 | 0.61 | 0.74 | 0.61 | **1.76** |  | 0.32 | 11 |
| broken | cursor | 1 | 0.67 | 0.66 | 0.78 | 0.67 | **1.81** |  | 0.35 | 10 |
| broken | parse | all | 0.84 | 0.54 | 0.69 | 0.55 | 0.77 | 0.78 | 0.34 | 29 |
| broken | walk | all | 0.90 | 0.61 | 0.75 | 0.62 | 0.75 |  | 0.32 | 27 |
| broken | cursor | all | 0.86 | 0.65 | 0.78 | 0.66 | 0.63 |  | 0.34 | 26 |

Allocations per parse, warm parser: real gositter 1.1, C 2598; broken gositter 1.1, C 10055.

New process, 2060 byte file: whole process 4.84 M cycles against 3.01 M for C (**1.61**), first parse 214 us against 230 us (0.93).

New process, 3845 byte file: whole process 6.80 M cycles against 5.46 M for C (**1.25**), first parse 452 us against 518 us (0.87).

Trees that differ from C (`oracle cmp`): real 0 of 318, realerr 0 of 371, broken 0 of 500.

### csharp

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.48 | 0.49 | 0.61 | 0.49 | **1.98** | **2.04** | 0.28 | 25 |
| real | walk | 1 | 0.59 | 0.59 | 0.67 | 0.59 | **1.98** |  | 0.27 | 20 |
| real | cursor | 1 | 0.70 | 0.71 | 0.78 | 0.71 | **2.11** |  | 0.33 | 16 |
| real | parse | all | 0.52 | 0.51 | 0.60 | 0.51 | 0.96 | 0.78 | 0.30 | 168 |
| real | walk | all | 0.60 | 0.59 | 0.67 | 0.60 | 0.90 |  | 0.28 | 136 |
| real | cursor | all | 0.71 | 0.70 | 0.77 | 0.70 | **1.07** |  | 0.33 | 110 |
| realerr | parse | 1 | 0.45 | 0.45 | 0.54 | 0.45 | **1.85** | **2.10** | 0.27 | 26 |
| realerr | walk | 1 | 0.54 | 0.54 | 0.60 | 0.54 | **1.86** |  | 0.27 | 20 |
| realerr | cursor | 1 | 0.64 | 0.64 | 0.69 | 0.64 | **1.92** |  | 0.32 | 17 |
| realerr | parse | all | 0.42 | 0.45 | 0.53 | 0.46 | 0.92 | **1.04** | 0.28 | 189 |
| realerr | walk | all | 0.63 | 0.53 | 0.60 | 0.55 | 0.96 |  | 0.27 | 139 |
| realerr | cursor | all | 0.67 | 0.61 | 0.69 | 0.63 | 0.99 |  | 0.31 | 112 |
| broken | parse | 1 | 0.36 | 0.36 | 0.41 | 0.36 | **1.90** | **2.15** | 0.24 | 19 |
| broken | walk | 1 | 0.42 | 0.42 | 0.46 | 0.42 | **1.86** |  | 0.24 | 16 |
| broken | cursor | 1 | 0.50 | 0.50 | 0.53 | 0.50 | **2.10** |  | 0.28 | 13 |
| broken | parse | all | 0.35 | 0.40 | 0.41 | 0.39 | **1.22** | **1.52** | 0.25 | 141 |
| broken | walk | all | 0.41 | 0.45 | 0.46 | 0.45 | **1.16** |  | 0.25 | 120 |
| broken | cursor | all | 0.49 | 0.52 | 0.53 | 0.52 | **1.26** |  | 0.28 | 98 |

Allocations per parse, warm parser: real gositter 1.1, C 2379; broken gositter 1.1, C 2936.

New process, 2048 byte file: whole process 5.61 M cycles against 3.93 M for C (**1.43**), first parse 323 us against 344 us (0.94).

New process, 2109 byte file: whole process 4.44 M cycles against 2.46 M for C (**1.81**), first parse 188 us against 186 us (**1.01**).

Trees that differ from C (`oracle cmp`): real 0 of 1437, realerr 0 of 63, broken 0 of 497.

### golang

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.54 | 0.54 | 0.69 | 0.54 | **2.05** | **2.32** | 0.32 | 35 |
| real | walk | 1 | 0.67 | 0.66 | 0.78 | 0.66 | **2.06** |  | 0.29 | 26 |
| real | cursor | 1 | 0.81 | 0.81 | 0.89 | 0.80 | **2.09** |  | 0.35 | 20 |
| real | parse | all | 0.54 | 0.56 | 0.69 | 0.56 | 0.91 | 0.80 | 0.34 | 265 |
| real | walk | all | 0.66 | 0.66 | 0.78 | 0.67 | 0.81 |  | 0.30 | 199 |
| real | cursor | all | 0.78 | 0.79 | 0.89 | 0.79 | 0.84 |  | 0.33 | 160 |
| realerr | parse | 1 | 0.54 | 0.54 | 0.68 | 0.54 | **2.09** | **2.19** | 0.31 | 26 |
| realerr | walk | 1 | 0.66 | 0.66 | 0.77 | 0.65 | **1.87** |  | 0.28 | 19 |
| realerr | cursor | 1 | 0.81 | 0.80 | 0.88 | 0.80 | **1.87** |  | 0.33 | 15 |
| realerr | parse | all | 0.54 | 0.53 | 0.68 | 0.54 | 0.83 | 0.90 | 0.31 | 103 |
| realerr | walk | all | 0.67 | 0.65 | 0.76 | 0.65 | 0.84 |  | 0.28 | 75 |
| realerr | cursor | all | 0.83 | 0.78 | 0.88 | 0.79 | 0.84 |  | 0.33 | 58 |
| broken | parse | 1 | 0.50 | 0.50 | 0.62 | 0.49 | **2.30** | **2.31** | 0.29 | 21 |
| broken | walk | 1 | 0.63 | 0.63 | 0.72 | 0.63 | **2.21** |  | 0.28 | 16 |
| broken | cursor | 1 | 0.73 | 0.73 | 0.80 | 0.73 | **2.27** |  | 0.33 | 13 |
| broken | parse | all | 0.56 | 0.52 | 0.62 | 0.52 | 0.86 | 0.85 | 0.32 | 105 |
| broken | walk | all | 0.64 | 0.63 | 0.72 | 0.64 | 0.70 |  | 0.29 | 80 |
| broken | cursor | all | 0.66 | 0.73 | 0.80 | 0.73 | 0.85 |  | 0.32 | 76 |

Allocations per parse, warm parser: real gositter 0.0, C 1986; broken gositter 0.1, C 3596.

New process, 2052 byte file: whole process 4.49 M cycles against 2.51 M for C (**1.79**), first parse 170 us against 174 us (0.98).

New process, 2374 byte file: whole process 4.96 M cycles against 3.04 M for C (**1.63**), first parse 232 us against 226 us (**1.03**).

Trees that differ from C (`oracle cmp`): real 0 of 1440, realerr 0 of 60, broken 0 of 501.

### hcl

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.81 | 0.81 | **1.11** | 0.81 | **1.75** | **2.26** | 0.46 | 9 |
| real | walk | 1 | 0.85 | 0.84 | **1.13** | 0.84 | **1.62** |  | 0.42 | 8 |
| real | cursor | 1 | 0.88 | 0.88 | **1.13** | 0.88 | **1.91** |  | 0.44 | 8 |
| real | parse | all | 0.88 | 0.86 | **1.11** | 0.86 | 0.84 | **1.16** | 0.49 | 72 |
| real | walk | all | 0.90 | 0.88 | **1.13** | 0.88 | 0.84 |  | 0.44 | 68 |
| real | cursor | all | 0.91 | 0.91 | **1.13** | 0.91 | **1.06** |  | 0.45 | 64 |
| broken | parse | 1 | 0.66 | 0.67 | 0.91 | 0.66 | **1.88** | **2.15** | 0.39 | 7 |
| broken | walk | 1 | 0.76 | 0.77 | **1.02** | 0.77 | **1.74** |  | 0.37 | 5 |
| broken | cursor | 1 | 0.72 | 0.72 | 0.94 | 0.72 | **1.77** |  | 0.39 | 6 |
| broken | parse | all | 0.67 | 0.70 | 0.91 | 0.70 | 0.82 | 0.94 | 0.41 | 55 |
| broken | walk | all | 0.90 | 0.80 | **1.02** | 0.80 | 0.80 |  | 0.41 | 34 |
| broken | cursor | all | 0.70 | 0.76 | 0.94 | 0.75 | 0.81 |  | 0.41 | 50 |

Allocations per parse, warm parser: real gositter 2.4, C 2599; broken gositter 5.9, C 3843.

New process, 2049 byte file: whole process 5.59 M cycles against 3.83 M for C (**1.46**), first parse 277 us against 294 us (0.94).

New process, 924 byte file: whole process 4.70 M cycles against 2.63 M for C (**1.79**), first parse 179 us against 172 us (**1.04**).

Trees that differ from C (`oracle cmp`): real 0 of 1500, broken 0 of 497.

### java

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.50 | 0.50 | 0.63 | 0.50 | **1.78** | **2.32** | 0.29 | 44 |
| real | walk | 1 | 0.64 | 0.64 | 0.72 | 0.63 | **1.61** |  | 0.27 | 31 |
| real | cursor | 1 | 0.78 | 0.78 | 0.84 | 0.78 | **2.04** |  | 0.33 | 24 |
| real | parse | all | 0.51 | 0.52 | 0.63 | 0.51 | **1.10** | **1.31** | 0.32 | 347 |
| real | walk | all | 0.63 | 0.61 | 0.72 | 0.61 | **1.12** |  | 0.28 | 260 |
| real | cursor | all | 0.75 | 0.75 | 0.84 | 0.74 | **1.25** |  | 0.32 | 206 |
| broken | parse | 1 | 0.35 | 0.35 | 0.40 | 0.35 | **1.78** | **2.18** | 0.23 | 21 |
| broken | walk | 1 | 0.45 | 0.45 | 0.48 | 0.45 | **1.92** |  | 0.23 | 15 |
| broken | cursor | 1 | 0.54 | 0.54 | 0.54 | 0.54 | **2.27** |  | 0.28 | 12 |
| broken | parse | all | 0.23 | 0.37 | 0.40 | 0.37 | 0.83 | 0.80 | 0.24 | 86 |
| broken | walk | all | 0.35 | 0.46 | 0.48 | 0.47 | 0.65 |  | 0.24 | 59 |
| broken | cursor | all | 0.46 | 0.53 | 0.54 | 0.54 | 0.77 |  | 0.28 | 42 |

Allocations per parse, warm parser: real gositter 0.0, C 1198; broken gositter 0.1, C 2767.

New process, 2048 byte file: whole process 3.85 M cycles against 1.78 M for C (**2.15**), first parse 97 us against 94 us (**1.03**).

New process, 2232 byte file: whole process 4.42 M cycles against 2.67 M for C (**1.66**), first parse 166 us against 188 us (0.88).

Trees that differ from C (`oracle cmp`): real 0 of 1498, realerr 0 of 2, broken 0 of 501.

### javascript

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.53 | 0.54 | 0.68 | 0.53 | **2.12** | **2.16** | 0.32 | 35 |
| real | walk | 1 | 0.70 | 0.70 | 0.78 | 0.70 | **2.00** |  | 0.30 | 24 |
| real | cursor | 1 | 0.85 | 0.84 | 0.89 | 0.84 | **1.91** |  | 0.36 | 19 |
| real | parse | all | 0.55 | 0.55 | 0.67 | 0.54 | 0.98 | 0.92 | 0.34 | 273 |
| real | walk | all | 0.68 | 0.67 | 0.78 | 0.68 | 0.96 |  | 0.30 | 200 |
| real | cursor | all | 0.81 | 0.80 | 0.89 | 0.80 | **1.02** |  | 0.34 | 158 |
| broken | parse | 1 | 0.50 | 0.51 | 0.67 | 0.51 | **1.94** | **2.04** | 0.28 | 20 |
| broken | walk | 1 | 0.70 | 0.70 | 0.88 | 0.70 | **1.85** |  | 0.27 | 12 |
| broken | cursor | 1 | 0.79 | 0.80 | 0.87 | 0.80 | **1.92** |  | 0.34 | 11 |
| broken | parse | all | 0.59 | 0.50 | 0.66 | 0.50 | 0.80 | 0.97 | 0.29 | 76 |
| broken | walk | all | 0.72 | 0.67 | 0.90 | 0.70 | 0.63 |  | 0.28 | 56 |
| broken | cursor | all | 0.82 | 0.75 | 0.87 | 0.77 | **1.01** |  | 0.32 | 47 |

Allocations per parse, warm parser: real gositter 0.1, C 2160; broken gositter 0.1, C 5055.

New process, 2047 byte file: whole process 4.27 M cycles against 2.39 M for C (**1.79**), first parse 163 us against 171 us (0.95).

New process, 2198 byte file: whole process 3.76 M cycles against 1.85 M for C (**2.04**), first parse 104 us against 104 us (1.00).

Trees that differ from C (`oracle cmp`): real 0 of 1344, realerr 0 of 9, broken 0 of 508.

### json

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.56 | 0.56 | 0.76 | 0.56 | **2.06** | **2.14** | 0.33 | 76 |
| real | walk | 1 | 0.81 | 0.81 | 0.89 | 0.81 | **2.02** |  | 0.29 | 46 |
| real | cursor | 1 | 0.96 | 0.96 | 0.98 | 0.96 | **1.89** |  | 0.35 | 36 |
| real | parse | all | 0.59 | 0.57 | 0.75 | 0.57 | **1.31** | **1.28** | 0.36 | 538 |
| real | walk | all | 0.77 | 0.76 | 0.88 | 0.77 | **1.27** |  | 0.29 | 354 |
| real | cursor | all | 0.92 | 0.88 | 0.98 | 0.89 | **1.38** |  | 0.32 | 281 |
| realerr | parse | 1 | 0.60 | 0.60 | 0.74 | 0.60 | **1.38** | **1.48** | 0.36 | 68 |
| realerr | walk | 1 | 0.77 | 0.77 | 0.90 | 0.76 | **1.09** |  | 0.31 | 45 |
| realerr | cursor | 1 | 0.85 | 0.85 | 0.89 | 0.85 | **1.08** |  | 0.36 | 42 |
| realerr | parse | all | 0.69 | 0.63 | 0.73 | 0.62 | **1.49** | **1.43** | 0.38 | 304 |
| realerr | walk | all | 0.90 | 0.75 | 0.90 | 0.75 | **1.30** |  | 0.34 | 215 |
| realerr | cursor | all | **1.01** | 0.83 | 0.89 | 0.82 | **1.44** |  | 0.37 | 180 |
| broken | parse | 1 | 0.57 | 0.57 | 0.76 | 0.58 | **1.99** | **2.10** | 0.31 | 57 |
| broken | walk | 1 | 0.89 | 0.87 | **1.05** | 0.87 | **2.17** |  | 0.29 | 27 |
| broken | cursor | 1 | 0.97 | 0.96 | 0.99 | 0.97 | **2.36** |  | 0.34 | 28 |
| broken | parse | all | 0.57 | 0.60 | 0.75 | 0.60 | 0.96 | **1.01** | 0.34 | 337 |
| broken | walk | all | **1.06** | 0.82 | **1.04** | 0.84 | 0.81 |  | 0.29 | 96 |
| broken | cursor | all | **1.08** | 0.91 | 0.99 | 0.92 | 0.91 |  | 0.32 | 150 |

Allocations per parse, warm parser: real gositter 0.1, C 1880; broken gositter 0.1, C 2746.

New process, 2035 byte file: whole process 4.61 M cycles against 2.11 M for C (**2.19**), first parse 166 us against 111 us (**1.49**).

New process, 1464 byte file: whole process 3.82 M cycles against 1.67 M for C (**2.29**), first parse 85 us against 69 us (**1.23**).

Trees that differ from C (`oracle cmp`): real 0 of 651, realerr 0 of 38, broken 0 of 498.

### julia

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.48 | 0.48 | 0.64 | 0.48 | **2.01** | **2.08** | 0.27 | 22 |
| real | walk | 1 | 0.62 | 0.63 | 0.73 | 0.62 | **2.07** |  | 0.27 | 16 |
| real | cursor | 1 | 0.77 | 0.77 | 0.86 | 0.77 | **2.12** |  | 0.34 | 12 |
| real | parse | all | 0.51 | 0.51 | 0.63 | 0.51 | 0.84 | 0.68 | 0.30 | 167 |
| real | walk | all | 0.64 | 0.62 | 0.72 | 0.63 | 0.84 |  | 0.28 | 126 |
| real | cursor | all | 0.75 | 0.76 | 0.86 | 0.77 | 0.92 |  | 0.34 | 99 |
| realerr | parse | 1 | 0.48 | 0.48 | 0.61 | 0.48 | **1.75** | **1.97** | 0.29 | 15 |
| realerr | walk | 1 | 0.59 | 0.60 | 0.67 | 0.60 | **1.80** |  | 0.29 | 12 |
| realerr | cursor | 1 | 0.70 | 0.70 | 0.77 | 0.70 | **1.80** |  | 0.34 | 9 |
| realerr | parse | all | 0.52 | 0.49 | 0.61 | 0.49 | 0.65 | 0.53 | 0.30 | 49 |
| realerr | walk | all | 0.62 | 0.59 | 0.68 | 0.59 | 0.62 |  | 0.28 | 36 |
| realerr | cursor | all | 0.76 | 0.69 | 0.78 | 0.70 | 0.60 |  | 0.34 | 28 |
| broken | parse | 1 | 0.46 | 0.46 | 0.58 | 0.46 | **1.99** | **1.97** | 0.28 | 15 |
| broken | walk | 1 | 0.57 | 0.57 | 0.66 | 0.57 | **1.98** |  | 0.28 | 12 |
| broken | cursor | 1 | 0.66 | 0.66 | 0.73 | 0.67 | **2.13** |  | 0.34 | 10 |
| broken | parse | all | 0.48 | 0.48 | 0.58 | 0.48 | 0.79 | 0.83 | 0.31 | 93 |
| broken | walk | all | 0.55 | 0.57 | 0.65 | 0.57 | 0.85 |  | 0.30 | 78 |
| broken | cursor | all | 0.62 | 0.66 | 0.73 | 0.66 | 0.90 |  | 0.34 | 67 |

Allocations per parse, warm parser: real gositter 0.2, C 5354; broken gositter 0.1, C 5648.

New process, 2045 byte file: whole process 5.54 M cycles against 4.14 M for C (**1.34**), first parse 312 us against 361 us (0.86).

New process, 3673 byte file: whole process 6.67 M cycles against 5.58 M for C (**1.20**), first parse 441 us against 509 us (0.87).

Trees that differ from C (`oracle cmp`): real 0 of 448, realerr 0 of 23, broken 0 of 493.

### kotlin

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.53 | 0.54 | 0.64 | 0.54 | **1.78** | **2.26** | 0.33 | 23 |
| real | walk | 1 | 0.63 | 0.63 | 0.70 | 0.63 | **1.82** |  | 0.31 | 18 |
| real | cursor | 1 | 0.74 | 0.74 | 0.79 | 0.74 | **2.17** |  | 0.36 | 15 |
| real | parse | all | 0.55 | 0.54 | 0.64 | 0.54 | **1.03** | **1.18** | 0.36 | 164 |
| real | walk | all | 0.63 | 0.61 | 0.70 | 0.61 | 1.00 |  | 0.32 | 139 |
| real | cursor | all | 0.70 | 0.69 | 0.79 | 0.69 | **1.33** |  | 0.36 | 117 |
| realerr | parse | 1 | 0.50 | 0.51 | 0.60 | 0.51 | **1.55** | **1.59** | 0.34 | 37 |
| realerr | walk | 1 | 0.58 | 0.59 | 0.64 | 0.58 | **1.61** |  | 0.31 | 30 |
| realerr | cursor | 1 | 0.68 | 0.67 | 0.72 | 0.67 | **1.61** |  | 0.36 | 25 |
| realerr | parse | all | 0.53 | 0.51 | 0.60 | 0.51 | **1.44** | **1.21** | 0.34 | 123 |
| realerr | walk | all | 0.59 | 0.59 | 0.65 | 0.59 | **1.36** |  | 0.33 | 104 |
| realerr | cursor | all | 0.68 | 0.68 | 0.72 | 0.66 | **1.29** |  | 0.36 | 90 |
| broken | parse | 1 | 0.53 | 0.53 | 0.61 | 0.52 | **1.69** | **1.71** | 0.34 | 26 |
| broken | walk | 1 | 0.59 | 0.59 | 0.66 | 0.59 | **1.66** |  | 0.32 | 22 |
| broken | cursor | 1 | 0.68 | 0.68 | 0.73 | 0.68 | **2.35** |  | 0.36 | 18 |
| broken | parse | all | 0.51 | 0.54 | 0.61 | 0.54 | **1.57** | **1.36** | 0.37 | 220 |
| broken | walk | all | 0.58 | 0.59 | 0.66 | 0.59 | **1.51** |  | 0.33 | 181 |
| broken | cursor | all | 0.67 | 0.66 | 0.74 | 0.66 | **1.73** |  | 0.36 | 152 |

Allocations per parse, warm parser: real gositter 0.0, C 1385; broken gositter 0.0, C 969.

New process, 2049 byte file: whole process 4.97 M cycles against 3.32 M for C (**1.50**), first parse 249 us against 275 us (0.91).

New process, 2051 byte file: whole process 5.99 M cycles against 4.17 M for C (**1.44**), first parse 376 us against 377 us (1.00).

Trees that differ from C (`oracle cmp`): real 0 of 1475, realerr 0 of 25, broken 0 of 493.

### lua

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.46 | 0.46 | 0.63 | 0.46 | **2.09** | **2.27** | 0.28 | 37 |
| real | walk | 1 | 0.61 | 0.61 | 0.74 | 0.61 | **1.97** |  | 0.27 | 26 |
| real | cursor | 1 | 0.76 | 0.76 | 0.87 | 0.76 | **2.04** |  | 0.33 | 19 |
| real | parse | all | 0.50 | 0.50 | 0.63 | 0.49 | 0.99 | **1.14** | 0.31 | 307 |
| real | walk | all | 0.62 | 0.62 | 0.74 | 0.62 | 0.97 |  | 0.28 | 227 |
| real | cursor | all | 0.77 | 0.76 | 0.86 | 0.76 | **1.32** |  | 0.33 | 174 |
| broken | parse | 1 | 0.45 | 0.46 | 0.57 | 0.46 | **1.97** | **2.51** | 0.32 | 14 |
| broken | walk | 1 | 0.54 | 0.54 | 0.63 | 0.54 | **2.10** |  | 0.29 | 11 |
| broken | cursor | 1 | 0.63 | 0.63 | 0.68 | 0.63 | **2.53** |  | 0.34 | 9 |
| broken | parse | all | 0.51 | 0.47 | 0.57 | 0.47 | 0.68 | 0.98 | 0.32 | 54 |
| broken | walk | all | 0.51 | 0.55 | 0.64 | 0.55 | 0.70 |  | 0.30 | 50 |
| broken | cursor | all | 0.56 | 0.61 | 0.68 | 0.61 | 0.85 |  | 0.33 | 46 |

Allocations per parse, warm parser: real gositter 1.0, C 2028; broken gositter 1.1, C 2597.

New process, 2045 byte file: whole process 5.06 M cycles against 2.99 M for C (**1.69**), first parse 189 us against 210 us (0.90).

New process, 2857 byte file: whole process 5.09 M cycles against 3.14 M for C (**1.62**), first parse 194 us against 225 us (0.86).

Trees that differ from C (`oracle cmp`): real 0 of 1415, realerr 0 of 1, broken 0 of 501.

### php

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.46 | 0.46 | 0.63 | 0.46 | **1.97** | **2.02** | 0.26 | 33 |
| real | walk | 1 | 0.62 | 0.62 | 0.73 | 0.62 | **1.97** |  | 0.26 | 23 |
| real | cursor | 1 | 0.81 | 0.80 | 0.88 | 0.80 | **1.96** |  | 0.34 | 16 |
| real | parse | all | 0.53 | 0.49 | 0.63 | 0.50 | 0.76 | 0.54 | 0.29 | 221 |
| real | walk | all | 0.64 | 0.62 | 0.72 | 0.63 | 0.70 |  | 0.27 | 159 |
| real | cursor | all | 0.81 | 0.78 | 0.88 | 0.79 | 0.74 |  | 0.33 | 116 |
| broken | parse | 1 | 0.40 | 0.40 | 0.49 | 0.40 | **1.99** | **2.42** | 0.24 | 26 |
| broken | walk | 1 | 0.92 | 0.92 | **1.17** | 0.92 | **1.92** |  | 0.28 | 5 |
| broken | cursor | 1 | 0.66 | 0.66 | 0.70 | 0.66 | **2.06** |  | 0.31 | 14 |
| broken | parse | all | 0.42 | 0.39 | 0.49 | 0.41 | 0.73 | 0.71 | 0.25 | 141 |
| broken | walk | all | **1.24** | 0.83 | **1.16** | 0.85 | 0.80 |  | 0.29 | 10 |
| broken | cursor | all | 0.74 | 0.62 | 0.69 | 0.63 | 0.65 |  | 0.30 | 72 |

Allocations per parse, warm parser: real gositter 3.5, C 2378; broken gositter 4.9, C 3894.

New process, 2046 byte file: whole process 4.99 M cycles against 3.00 M for C (**1.66**), first parse 246 us against 248 us (0.99).

New process, 2155 byte file: whole process 4.66 M cycles against 2.97 M for C (**1.57**), first parse 194 us against 228 us (0.85).

Trees that differ from C (`oracle cmp`): real 0 of 1499, realerr 0 of 1, broken 0 of 497.

### python

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.54 | 0.54 | 0.69 | 0.54 | **2.15** | **2.27** | 0.32 | 28 |
| real | walk | 1 | 0.67 | 0.66 | 0.77 | 0.66 | **2.10** |  | 0.30 | 21 |
| real | cursor | 1 | 0.80 | 0.79 | 0.88 | 0.79 | **2.01** |  | 0.36 | 17 |
| real | parse | all | 0.56 | 0.55 | 0.69 | 0.55 | 0.89 | 0.89 | 0.35 | 223 |
| real | walk | all | 0.67 | 0.66 | 0.77 | 0.66 | 0.88 |  | 0.31 | 174 |
| real | cursor | all | 0.79 | 0.77 | 0.88 | 0.77 | 0.97 |  | 0.34 | 139 |
| broken | parse | 1 | 0.43 | 0.43 | 0.52 | 0.43 | **1.92** | **1.99** | 0.27 | 19 |
| broken | walk | 1 | 0.76 | 0.76 | 0.99 | 0.76 | **1.93** |  | 0.28 | 7 |
| broken | cursor | 1 | 0.63 | 0.63 | 0.68 | 0.63 | **2.08** |  | 0.32 | 12 |
| broken | parse | all | 0.34 | 0.47 | 0.52 | 0.47 | 0.88 | **1.12** | 0.30 | 110 |
| broken | walk | all | 0.97 | 0.71 | 0.99 | 0.72 | 0.80 |  | 0.29 | 12 |
| broken | cursor | all | 0.48 | 0.64 | 0.68 | 0.64 | 0.96 |  | 0.32 | 70 |

Allocations per parse, warm parser: real gositter 3.4, C 3454; broken gositter 3.5, C 5141.

New process, 2052 byte file: whole process 5.29 M cycles against 3.48 M for C (**1.52**), first parse 263 us against 280 us (0.94).

New process, 4532 byte file: whole process 6.03 M cycles against 4.58 M for C (**1.32**), first parse 332 us against 394 us (0.84).

Trees that differ from C (`oracle cmp`): real 0 of 1022, realerr 0 of 3, broken 0 of 507.

### ruby

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.49 | 0.49 | 0.64 | 0.49 | **2.05** | **2.20** | 0.27 | 29 |
| real | walk | 1 | 0.63 | 0.63 | 0.74 | 0.63 | **2.05** |  | 0.28 | 21 |
| real | cursor | 1 | 0.76 | 0.76 | 0.86 | 0.76 | **2.16** |  | 0.35 | 17 |
| real | parse | all | 0.53 | 0.52 | 0.64 | 0.52 | 0.83 | 0.94 | 0.30 | 246 |
| real | walk | all | 0.63 | 0.62 | 0.74 | 0.63 | 0.82 |  | 0.29 | 189 |
| real | cursor | all | 0.75 | 0.75 | 0.86 | 0.75 | 0.99 |  | 0.35 | 149 |
| broken | parse | 1 | 0.27 | 0.27 | 0.32 | 0.27 | **1.65** | **2.06** | 0.19 | 14 |
| broken | walk | 1 | 0.35 | 0.35 | 0.40 | 0.36 | **1.80** |  | 0.21 | 10 |
| broken | cursor | 1 | 0.41 | 0.42 | 0.44 | 0.42 | **1.96** |  | 0.26 | 9 |
| broken | parse | all | 0.20 | 0.29 | 0.32 | 0.30 | 0.69 | 0.90 | 0.20 | 66 |
| broken | walk | all | 0.29 | 0.37 | 0.40 | 0.37 | 0.61 |  | 0.22 | 43 |
| broken | cursor | all | 0.45 | 0.43 | 0.44 | 0.43 | **1.12** |  | 0.26 | 30 |

Allocations per parse, warm parser: real gositter 2.2, C 2266; broken gositter 2.3, C 4633.

New process, 2047 byte file: whole process 4.47 M cycles against 2.64 M for C (**1.70**), first parse 168 us against 182 us (0.92).

New process, 1869 byte file: whole process 4.76 M cycles against 3.02 M for C (**1.58**), first parse 201 us against 231 us (0.87).

Trees that differ from C (`oracle cmp`): real 0 of 1500, broken 0 of 501.

### rust

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.52 | 0.52 | 0.69 | 0.52 | **1.67** | **2.20** | 0.30 | 33 |
| real | walk | 1 | 0.69 | 0.69 | 0.82 | 0.69 | **1.49** |  | 0.29 | 22 |
| real | cursor | 1 | 0.83 | 0.82 | 0.92 | 0.82 | **1.90** |  | 0.36 | 18 |
| real | parse | all | 0.55 | 0.55 | 0.69 | 0.54 | 0.93 | **1.13** | 0.33 | 264 |
| real | walk | all | 0.69 | 0.68 | 0.82 | 0.68 | 0.90 |  | 0.30 | 187 |
| real | cursor | all | 0.81 | 0.80 | 0.92 | 0.80 | **1.04** |  | 0.34 | 151 |
| broken | parse | 1 | 0.44 | 0.44 | 0.55 | 0.44 | **1.88** | **1.97** | 0.26 | 24 |
| broken | walk | 1 | **1.08** | **1.06** | **1.33** | **1.06** | **2.00** |  | 0.28 | 3 |
| broken | cursor | 1 | 0.71 | 0.71 | 0.74 | 0.71 | **1.93** |  | 0.32 | 13 |
| broken | parse | all | 0.34 | 0.48 | 0.55 | 0.48 | 0.73 | 0.96 | 0.29 | 142 |
| broken | walk | all | **1.21** | 0.97 | **1.33** | 0.98 | 0.72 |  | 0.28 | 4 |
| broken | cursor | all | 0.69 | 0.71 | 0.75 | 0.71 | 0.83 |  | 0.32 | 66 |

Allocations per parse, warm parser: real gositter 1.0, C 2566; broken gositter 1.1, C 4326.

New process, 2052 byte file: whole process 4.47 M cycles against 2.63 M for C (**1.70**), first parse 192 us against 201 us (0.96).

New process, 3520 byte file: whole process 5.03 M cycles against 3.33 M for C (**1.51**), first parse 243 us against 268 us (0.90).

Trees that differ from C (`oracle cmp`): real 0 of 1042, realerr 0 of 1, broken 0 of 501.

### scala

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.64 | 0.64 | 0.82 | 0.64 | **2.52** | **2.71** | 0.25 | 13 |
| real | walk | 1 | 0.70 | 0.71 | 0.86 | 0.71 | **2.45** |  | cgo killed | 11 |
| real | cursor | 1 | 0.78 | 0.79 | 0.91 | 0.78 | **2.53** |  | cgo killed | 10 |
| real | parse | all | 0.63 | 0.65 | 0.82 | 0.64 | 0.92 | **1.10** | cgo killed | 97 |
| real | walk | all | 0.70 | 0.70 | 0.86 | 0.70 | 0.95 |  | cgo killed | 87 |
| real | cursor | all | 0.77 | 0.76 | 0.91 | 0.76 | **1.17** |  | cgo killed | 76 |
| realerr | parse | 1 | 0.62 | 0.63 | 0.80 | 0.62 | **1.94** | **1.96** | 0.23 | 12 |
| realerr | walk | 1 | 0.69 | 0.69 | 0.84 | 0.69 | **1.77** |  | 0.23 | 10 |
| realerr | cursor | 1 | 0.75 | 0.75 | 0.90 | 0.75 | **1.88** |  | 0.26 | 9 |
| realerr | parse | all | 0.59 | 0.63 | 0.80 | 0.63 | **1.11** | 0.97 | cgo killed | 87 |
| realerr | walk | all | 0.73 | 0.69 | 0.84 | 0.70 | **1.04** |  | cgo killed | 77 |
| realerr | cursor | all | 0.81 | 0.74 | 0.90 | 0.75 | **1.11** |  | cgo killed | 66 |
| broken | parse | 1 | 0.61 | 0.61 | 0.76 | 0.60 | **1.66** | **1.71** | 0.28 | 12 |
| broken | walk | 1 | 0.80 | 0.80 | 0.98 | 0.80 | **1.59** |  | 0.29 | 7 |
| broken | cursor | 1 | 0.73 | 0.72 | 0.85 | 0.72 | **1.93** |  | 0.31 | 9 |
| broken | parse | all | 0.63 | 0.62 | 0.76 | 0.61 | 0.99 | 0.81 | 0.27 | 97 |
| broken | walk | all | **1.21** | 0.76 | 0.98 | 0.77 | 0.95 |  | 0.29 | 26 |
| broken | cursor | all | 0.72 | 0.72 | 0.85 | 0.72 | **1.02** |  | 0.29 | 78 |

Allocations per parse, warm parser: real gositter 2.0, C 3208; broken gositter 2.0, C 3105.

New process, 2049 byte file: whole process 5.32 M cycles against 3.66 M for C (**1.45**), first parse 280 us against 310 us (0.90).

New process, 2732 byte file: whole process 6.04 M cycles against 4.57 M for C (**1.32**), first parse 360 us against 412 us (0.87).

Trees that differ from C (`oracle cmp`): real not compared (KILL_RSS on the cgo side), realerr 0 of 84, broken 0 of 497.

### tsx

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.55 | 0.55 | 0.70 | 0.55 | **1.55** | **1.59** | 0.33 | 36 |
| real | walk | 1 | 0.69 | 0.69 | 0.79 | 0.69 | **1.45** |  | 0.30 | 26 |
| real | cursor | 1 | 0.83 | 0.83 | 0.91 | 0.83 | **2.04** |  | 0.36 | 20 |
| real | parse | all | 0.57 | 0.56 | 0.69 | 0.56 | **1.22** | **1.11** | 0.35 | 269 |
| real | walk | all | 0.69 | 0.68 | 0.79 | 0.68 | **1.20** |  | 0.30 | 203 |
| real | cursor | all | 0.85 | 0.78 | 0.91 | 0.79 | **1.38** |  | 0.35 | 159 |
| realerr | parse | 1 | 0.49 | 0.49 | 0.57 | 0.49 | **1.68** | **1.82** | 0.35 | 53 |
| realerr | walk | 1 | 0.59 | 0.61 | 0.64 | 0.60 | **1.53** |  | 0.33 | 42 |
| realerr | cursor | 1 | 0.64 | 0.64 | 0.69 | 0.64 | **1.58** |  | 0.36 | 37 |
| realerr | parse | all | 0.37 | 0.53 | 0.59 | 0.54 | **1.65** | **1.50** | 0.38 | 244 |
| realerr | walk | all | 0.37 | 0.62 | 0.66 | 0.62 | **1.62** |  | 0.35 | 237 |
| realerr | cursor | all | 0.39 | 0.66 | 0.71 | 0.66 | **1.57** |  | 0.36 | 214 |
| broken | parse | 1 | 0.44 | 0.43 | 0.48 | 0.44 | **1.31** | **1.62** | 0.29 | 25 |
| broken | walk | 1 | **1.20** | **1.16** | **1.34** | **1.16** | **1.28** |  | 0.32 | 3 |
| broken | cursor | 1 | 0.60 | 0.60 | 0.62 | 0.60 | **1.48** |  | 0.32 | 17 |
| broken | parse | all | 0.39 | 0.46 | 0.49 | 0.46 | 0.66 | 0.51 | 0.31 | 149 |
| broken | walk | all | **1.43** | **1.08** | **1.34** | **1.09** | 0.62 |  | 0.32 | 3 |
| broken | cursor | all | 0.46 | 0.60 | 0.62 | 0.60 | 0.72 |  | 0.31 | 120 |

Allocations per parse, warm parser: real gositter 0.0, C 1098; broken gositter 0.1, C 1854.

New process, 2041 byte file: whole process 4.72 M cycles against 2.76 M for C (**1.71**), first parse 248 us against 217 us (**1.14**).

New process, 975 byte file: whole process 4.30 M cycles against 2.30 M for C (**1.87**), first parse 164 us against 171 us (0.96).

Trees that differ from C (`oracle cmp`): real 0 of 578, realerr 0 of 45, broken 0 of 497.

### typescript

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.53 | 0.53 | 0.67 | 0.52 | **2.09** | **2.13** | 0.32 | 34 |
| real | walk | 1 | 0.67 | 0.67 | 0.77 | 0.67 | **2.03** |  | 0.30 | 25 |
| real | cursor | 1 | 0.82 | 0.82 | 0.89 | 0.82 | **2.11** |  | 0.36 | 19 |
| real | parse | all | 0.55 | 0.55 | 0.67 | 0.54 | 0.97 | 0.96 | 0.34 | 272 |
| real | walk | all | 0.67 | 0.66 | 0.77 | 0.66 | 0.97 |  | 0.30 | 205 |
| real | cursor | all | 0.81 | 0.79 | 0.89 | 0.80 | **1.06** |  | 0.34 | 161 |
| realerr | parse | 1 | 0.50 | 0.50 | 0.59 | 0.49 | **1.72** | **1.53** | 0.32 | 23 |
| realerr | walk | 1 | 0.62 | 0.61 | 0.66 | 0.61 | **1.48** |  | 0.30 | 18 |
| realerr | cursor | 1 | 0.72 | 0.74 | 0.76 | 0.74 | **1.55** |  | 0.36 | 14 |
| realerr | parse | all | 0.61 | 0.54 | 0.63 | 0.54 | **1.49** | **1.13** | 0.34 | 53 |
| realerr | walk | all | 0.72 | 0.62 | 0.69 | 0.62 | **1.29** |  | 0.31 | 40 |
| realerr | cursor | all | 0.88 | 0.74 | 0.79 | 0.72 | **1.40** |  | 0.35 | 31 |
| broken | parse | 1 | 0.39 | 0.39 | 0.46 | 0.39 | **1.94** | **2.24** | 0.25 | 19 |
| broken | walk | 1 | **1.12** | 0.99 | **1.34** | 0.99 | **2.04** |  | 0.30 | 2 |
| broken | cursor | 1 | 0.59 | 0.59 | 0.61 | 0.59 | **2.24** |  | 0.30 | 12 |
| broken | parse | all | 0.42 | 0.42 | 0.46 | 0.42 | 0.68 | 0.91 | 0.27 | 119 |
| broken | walk | all | **1.31** | **1.01** | **1.34** | **1.03** | 0.70 |  | 0.30 | 2 |
| broken | cursor | all | 0.79 | 0.58 | 0.63 | 0.59 | 0.70 |  | 0.30 | 57 |

Allocations per parse, warm parser: real gositter 0.1, C 1997; broken gositter 0.1, C 4319.

New process, 2046 byte file: whole process 4.62 M cycles against 2.74 M for C (**1.68**), first parse 217 us against 228 us (0.95).

New process, 1131 byte file: whole process 4.49 M cycles against 2.44 M for C (**1.84**), first parse 175 us against 180 us (0.97).

Trees that differ from C (`oracle cmp`): real 0 of 1431, realerr 0 of 69, broken 0 of 507.

### verilog

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.61 | 0.61 | 0.74 | 0.61 | **1.39** | **1.37** | 0.33 | 22 |
| real | walk | 1 | 0.72 | 0.73 | 0.82 | 0.73 | **1.38** |  | 0.29 | 17 |
| real | cursor | 1 | 0.86 | 0.85 | 0.92 | 0.85 | **1.45** |  | 0.34 | 13 |
| real | parse | all | 0.65 | 0.62 | 0.74 | 0.64 | **1.28** | **1.29** | 0.35 | 163 |
| real | walk | all | 0.71 | 0.71 | 0.82 | 0.73 | **1.34** |  | 0.29 | 131 |
| real | cursor | all | 0.89 | 0.82 | 0.92 | 0.83 | **1.39** |  | 0.31 | 103 |
| realerr | parse | 1 | 0.31 | 0.31 | 0.32 | 0.31 | **1.84** | **2.15** | 0.24 | 10 |
| realerr | walk | 1 | 0.39 | 0.39 | 0.41 | 0.39 | **1.85** |  | 0.24 | 8 |
| realerr | cursor | 1 | 0.39 | 0.39 | 0.38 | 0.39 | **1.92** |  | 0.26 | 8 |
| realerr | parse | all | 0.29 | 0.30 | 0.32 | 0.30 | 1.00 | 0.80 | 0.23 | 33 |
| realerr | walk | all | 0.41 | 0.36 | 0.42 | 0.36 | **1.03** |  | 0.23 | 21 |
| realerr | cursor | all | 0.32 | 0.37 | 0.38 | 0.36 | **1.02** |  | 0.24 | 29 |
| broken | parse | 1 | 0.26 | 0.26 | 0.28 | 0.26 | **2.02** | **2.20** | 0.21 | 7 |
| broken | walk | 1 | 0.36 | 0.37 | 0.43 | 0.37 | **2.01** |  | 0.23 | 5 |
| broken | cursor | 1 | 0.31 | 0.31 | 0.32 | 0.31 | **1.96** |  | 0.23 | 6 |
| broken | parse | all | 0.28 | 0.25 | 0.28 | 0.25 | 0.90 | **1.05** | 0.20 | 28 |
| broken | walk | all | 0.40 | 0.34 | 0.43 | 0.34 | 0.96 |  | 0.22 | 18 |
| broken | cursor | all | 0.31 | 0.30 | 0.32 | 0.30 | 0.92 |  | 0.22 | 25 |

Allocations per parse, warm parser: real gositter 0.1, C 1207; broken gositter 0.1, C 8348.

New process, 2048 byte file: whole process 6.96 M cycles against 5.32 M for C (**1.31**), first parse 523 us against 553 us (0.95).

New process, 1245 byte file: whole process 4.12 M cycles against 1.92 M for C (**2.14**), first parse 139 us against 140 us (1.00).

Trees that differ from C (`oracle cmp`): real 0 of 195, realerr 0 of 441, broken 0 of 497.

### zig

| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |
|---|---|---|---|---|---|---|---|---|---|---|
| real | parse | 1 | 0.48 | 0.48 | 0.65 | 0.48 | **1.88** | **1.88** | 0.26 | 40 |
| real | walk | 1 | 0.65 | 0.66 | 0.76 | 0.65 | **1.82** |  | 0.27 | 26 |
| real | cursor | 1 | 0.83 | 0.83 | 0.89 | 0.83 | **1.90** |  | 0.35 | 19 |
| real | parse | all | 0.50 | 0.51 | 0.64 | 0.51 | 0.87 | 0.76 | 0.29 | 298 |
| real | walk | all | 0.67 | 0.65 | 0.75 | 0.66 | 0.88 |  | 0.28 | 207 |
| real | cursor | all | 0.82 | 0.81 | 0.89 | 0.82 | 0.94 |  | 0.34 | 159 |
| realerr | parse | 1 | 0.49 | 0.50 | 0.66 | 0.49 | **1.93** | **2.12** | 0.26 | 40 |
| realerr | walk | 1 | 0.66 | 0.66 | 0.77 | 0.65 | **1.79** |  | 0.27 | 26 |
| realerr | cursor | 1 | 0.83 | 0.83 | 0.89 | 0.83 | **1.74** |  | 0.34 | 20 |
| realerr | parse | all | 0.54 | 0.50 | 0.63 | 0.50 | 0.71 | 0.80 | 0.28 | 113 |
| realerr | walk | all | 0.68 | 0.65 | 0.75 | 0.68 | 0.60 |  | 0.28 | 77 |
| realerr | cursor | all | 0.88 | 0.78 | 0.89 | 0.82 | 0.71 |  | 0.34 | 58 |
| broken | parse | 1 | 0.36 | 0.36 | 0.45 | 0.36 | **1.57** | **2.01** | 0.23 | 26 |
| broken | walk | 1 | 0.54 | 0.54 | 0.65 | 0.54 | **1.59** |  | 0.26 | 15 |
| broken | cursor | 1 | 0.56 | 0.57 | 0.61 | 0.57 | **1.85** |  | 0.30 | 15 |
| broken | parse | all | 0.25 | 0.42 | 0.45 | 0.41 | 0.79 | **1.05** | 0.26 | 189 |
| broken | walk | all | 0.54 | 0.57 | 0.65 | 0.56 | 0.83 |  | 0.27 | 76 |
| broken | cursor | all | 0.36 | 0.62 | 0.61 | 0.60 | 0.90 |  | 0.30 | 130 |

Allocations per parse, warm parser: real gositter 0.2, C 7998; broken gositter 0.1, C 7739.

New process, 2021 byte file: whole process 4.35 M cycles against 2.35 M for C (**1.85**), first parse 166 us against 165 us (1.00).

New process, 11749 byte file: whole process 8.89 M cycles against 9.49 M for C (0.94), first parse 640 us against 875 us (0.73).

Trees that differ from C (`oracle cmp`): real 0 of 423, realerr 0 of 15, broken 0 of 478.

## Extreme inputs

One process per file, one parse, one thread. Times are wall; a killed process shows the time and RSS at the kill.

| grammar | inputs | both finished | gositter fewer cycles | gositter less RSS | worst cycles ratio | worst RSS ratio | only gositter finished | only C finished | neither finished |
|---|---|---|---|---|---|---|---|---|---|
| bash | 12 | 11 | 8 | 7 | **1.14** | **2.27** | 1 | 0 | 0 |
| c | 14 | 13 | 12 | 7 | **1.32** | **1.90** | 0 | 0 | 1 |
| cpp | 13 | 12 | 10 | 7 | **1.27** | **1.77** | 0 | 0 | 1 |
| csharp | 14 | 13 | 13 | 6 | 0.99 | **1.73** | 0 | 0 | 1 |
| golang | 14 | 13 | 13 | 4 | 0.90 | **1.53** | 1 | 0 | 0 |
| hcl | 11 | 11 | 10 | 6 | **1.04** | **2.58** | 0 | 0 | 0 |
| java | 13 | 13 | 12 | 6 | **1.36** | **1.13** | 0 | 0 | 0 |
| javascript | 14 | 14 | 13 | 6 | **1.13** | **1.27** | 0 | 0 | 0 |
| json | 12 | 11 | 11 | 9 | 1.00 | **1.27** | 1 | 0 | 0 |
| julia | 16 | 16 | 16 | 12 | 0.82 | **1.55** | 0 | 0 | 0 |
| kotlin | 16 | 12 | 10 | 4 | **1.34** | **1.29** | 2 | 0 | 2 |
| lua | 10 | 10 | 10 | 4 | 0.81 | **1.72** | 0 | 0 | 0 |
| php | 11 | 11 | 11 | 5 | 0.97 | **1.37** | 0 | 0 | 0 |
| python | 12 | 12 | 12 | 6 | 0.81 | **1.20** | 0 | 0 | 0 |
| ruby | 12 | 12 | 12 | 6 | 0.80 | **3.26** | 0 | 0 | 0 |
| rust | 14 | 13 | 13 | 8 | 0.85 | **1.40** | 0 | 0 | 1 |
| scala | 14 | 14 | 12 | 5 | **1.31** | **1.63** | 0 | 0 | 0 |
| tsx | 11 | 10 | 10 | 6 | 0.93 | **1.33** | 0 | 0 | 1 |
| typescript | 12 | 12 | 11 | 4 | **1.02** | **1.43** | 0 | 0 | 0 |
| verilog | 11 | 10 | 9 | 4 | **1.31** | **2.51** | 1 | 0 | 0 |
| zig | 11 | 11 | 11 | 9 | 0.93 | **1.51** | 0 | 0 | 0 |
| all | 267 | 254 | 239 | 131 | | | 6 | 0 | 7 |

Every extreme input where gositter needs over 1.02 times the cycles or the peak RSS of C, or where a side did not finish:

| grammar | input | gositter | C | cycles ratio | RSS ratio |
|---|---|---|---|---|---|
| bash | `random_bracket_soup_7022a9__n100000.sh` | 0.01 s, 10 MB | 0.01 s, 4 MB | **1.09** | **2.27** |
| bash | `rep_6c2f79__n100000.sh` | 25.61 s, 22 MB | 22.76 s, 17 MB | **1.14** | **1.27** |
| bash | `unterminated_EOF_na_n_f319ea__n30000.sh` | 7.97 s, 62 MB | 17.98 s, 49 MB | 0.44 | **1.27** |
| bash | `advdeep_100000__sh_100000.sh` | 0.04 s, 54 MB | 0.06 s, 46 MB | 0.56 | **1.17** |
| bash | `rep_3c307f__n100000.sh` | 0.10 s, 62 MB | 0.10 s, 65 MB | **1.13** | 0.95 |
| bash | `random_bytes_697b59__n1000000.sh` | 0.19 s, 61 MB | rc=245_no_output at 0 s, 52 MB | n/a | n/a |
| c | `unterminated_define_A_c611ad__n10000.c` | 0.78 s, 7 MB | 0.60 s, 4 MB | **1.32** | **1.90** |
| c | `new_two_seq_A_n_2_V_B_n_2_define_A_n_232200__n30000.c` | 60.29 s, 28 MB | 62.06 s, 20 MB | 0.97 | **1.43** |
| c | `advdeep_100000__c_100000.c` | 0.02 s, 34 MB | 0.04 s, 26 MB | 0.61 | **1.32** |
| c | `advdeep_100000__cblk_100000.c` | 0.02 s, 32 MB | 0.03 s, 26 MB | 0.59 | **1.22** |
| c | `glr_valid_x_a_b_n_31270c__n100000.c` | 0.22 s, 111 MB | 0.29 s, 94 MB | 0.78 | **1.18** |
| c | `big5mb__big5mb.c` | 0.36 s, 313 MB | 0.64 s, 305 MB | 0.58 | **1.03** |
| c | `rep_n_aec841__n30000.c` | KILL_TIME at 120 s, 37 MB | KILL_TIME at 120 s, 22 MB | n/a | n/a |
| cpp | `unterminated_define_A_c611ad__n10000.cpp` | 0.77 s, 7 MB | 0.62 s, 4 MB | **1.27** | **1.77** |
| cpp | `x_n_x7_a_x5_26d2cc__n10000.cpp` | 6.78 s, 17 MB | 6.56 s, 10 MB | **1.03** | **1.73** |
| cpp | `advdeep_100000__cpp_100000.cpp` | 1.34 s, 722 MB | 1.69 s, 628 MB | 0.82 | **1.15** |
| cpp | `glr_valid_void_f_a_b_c_d_e_f_g_h_n_8e9ccf__n100000.cpp` | 2.85 s, 487 MB | 3.61 s, 439 MB | 0.79 | **1.11** |
| cpp | `big5mb__big5mb.cpp` | 0.32 s, 270 MB | 0.58 s, 245 MB | 0.57 | **1.10** |
| cpp | `rep_n_aec841__n30000.cpp` | KILL_TIME at 120 s, 40 MB | KILL_TIME at 120 s, 25 MB | n/a | n/a |
| csharp | `unterminated_a_ff4b90__n30000.cs` | 3.29 s, 14 MB | 3.59 s, 8 MB | 0.92 | **1.73** |
| csharp | `unterminated_a_adca5b__n30000.cs` | 4.07 s, 19 MB | 5.50 s, 12 MB | 0.74 | **1.57** |
| csharp | `unterminated_a_a452e7__n100000.cs` | 74.34 s, 26 MB | 76.21 s, 20 MB | 0.99 | **1.28** |
| csharp | `rep_n_8ed84f__n100000.cs` | 30.77 s, 57 MB | 38.87 s, 50 MB | 0.79 | **1.13** |
| csharp | `broken_stmt_var_x_new_A_1_n_82c71b__n100000.cs` | 1.06 s, 262 MB | 1.52 s, 243 MB | 0.70 | **1.08** |
| csharp | `glr_valid_x_a_b_c_n_003570__n100000.cs` | 0.59 s, 324 MB | 0.92 s, 302 MB | 0.65 | **1.07** |
| csharp | `random_bracket_soup_7022a9__n100000.cs` | 0.11 s, 21 MB | 0.26 s, 20 MB | 0.42 | **1.05** |
| csharp | `rep_n_aec841__n30000.cs` | KILL_TIME at 120 s, 44 MB | KILL_TIME at 120 s, 25 MB | n/a | n/a |
| golang | `rep_n_b5fbe6__n10000.go` | 25.55 s, 21 MB | 28.55 s, 14 MB | 0.89 | **1.53** |
| golang | `new_two_seq_A_n_2_V_B_n_2_n_a_bedb31__n30000.go` | 58.88 s, 34 MB | 65.77 s, 25 MB | 0.90 | **1.39** |
| golang | `advdeep_100000__go_100000.go` | 0.02 s, 33 MB | 0.04 s, 26 MB | 0.57 | **1.27** |
| golang | `x_n_x3_a_x5_7103f9__n80000.go` | 36.62 s, 30 MB | 44.11 s, 24 MB | 0.83 | **1.23** |
| golang | `unterminated_a_266e40__n30000.go` | 3.09 s, 22 MB | 10.34 s, 20 MB | 0.30 | **1.08** |
| golang | `broken_stmt_func_f_return_1_n_n_5b0a65__n100000.go` | 1.16 s, 145 MB | 1.88 s, 138 MB | 0.62 | **1.05** |
| golang | `big5mb__big5mb.go` | 0.51 s, 306 MB | 0.81 s, 298 MB | 0.65 | **1.03** |
| golang | `x_n_x3_a_x5_7103f9__n132000.go` | 103.10 s, 45 MB | KILL_TIME at 120 s, 36 MB | n/a | n/a |
| hcl | `rep_6c2f79__n100000.hcl` | 0.22 s, 416 MB | 0.42 s, 162 MB | 0.54 | **2.58** |
| hcl | `deep_x_1_n_b2e152__n100000.hcl` | 26.40 s, 499 MB | 32.14 s, 209 MB | 0.83 | **2.39** |
| hcl | `unterminated_if_a_f41bf6__n100000.hcl` | 0.49 s, 630 MB | 1.73 s, 288 MB | 0.29 | **2.19** |
| hcl | `unterminated_a_ac3879__n100000.hcl` | 0.43 s, 489 MB | 1.54 s, 238 MB | 0.29 | **2.06** |
| hcl | `unterminated_a_adca5b__n100000.hcl` | 54.03 s, 55 MB | 62.26 s, 44 MB | 0.87 | **1.23** |
| hcl | `random_printable_cb9992__n1000000.hcl` | 0.52 s, 293 MB | 0.53 s, 340 MB | **1.04** | 0.86 |
| java | `rep_n_b5fbe6__n100000.java` | 0.07 s, 44 MB | 0.05 s, 43 MB | **1.36** | **1.03** |
| java | `unterminated_a_266e40__n30000.java` | 3.15 s, 26 MB | 10.64 s, 23 MB | 0.30 | **1.13** |
| java | `advdeep_100000__java_100000.java` | 0.02 s, 39 MB | 0.04 s, 36 MB | 0.57 | **1.08** |
| java | `glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n100000.java` | 0.91 s, 526 MB | 1.35 s, 492 MB | 0.69 | **1.07** |
| java | `broken_stmt_class_A_void_f_return_1_n_eb23bc__n100000.java` | 0.53 s, 176 MB | 1.05 s, 169 MB | 0.51 | **1.04** |
| java | `big5mb__big5mb.java` | 0.34 s, 291 MB | 0.58 s, 279 MB | 0.60 | **1.04** |
| javascript | `unterminated_a_a452e7__n100000.js` | 48.17 s, 28 MB | 60.41 s, 22 MB | 0.80 | **1.27** |
| javascript | `block_class_A_n_31164b__n100000.js` | 47.44 s, 43 MB | 42.75 s, 34 MB | **1.13** | **1.27** |
| javascript | `rep_045310__n30000.js` | 7.16 s, 17 MB | 13.03 s, 13 MB | 0.55 | **1.25** |
| javascript | `random_bracket_soup_7022a9__n1000000.js` | 1.93 s, 218 MB | 2.10 s, 175 MB | 0.92 | **1.25** |
| javascript | `glr_valid_a_b_c_n_d20807__n100000.js` | 0.24 s, 135 MB | 0.31 s, 125 MB | 0.79 | **1.08** |
| javascript | `advdeep_100000__js_100000.js` | 0.03 s, 39 MB | 0.05 s, 37 MB | 0.57 | **1.07** |
| javascript | `big5mb__big5mb.js` | 0.47 s, 341 MB | 0.72 s, 334 MB | 0.66 | **1.02** |
| json | `advdeep_100000__json_100000.json` | 0.02 s, 33 MB | 0.03 s, 26 MB | 0.63 | **1.27** |
| json | `unterminated_a_b_ceeb36__n100000.json` | 106.34 s, 87 MB | KILL_TIME at 120 s, 62 MB | n/a | n/a |
| julia | `random_printable_cb9992__n100000.jl` | 0.09 s, 11 MB | 0.18 s, 7 MB | 0.50 | **1.55** |
| julia | `rep_b0b883__n100000.jl` | 23.30 s, 27 MB | 30.66 s, 21 MB | 0.76 | **1.27** |
| julia | `random_bracket_soup_7022a9__n100000.jl` | 0.85 s, 15 MB | 1.68 s, 13 MB | 0.50 | **1.17** |
| julia | `deep_x_1_n_be5de1__n100000.jl` | 0.09 s, 128 MB | 0.17 s, 115 MB | 0.49 | **1.11** |
| kotlin | `rep_n_93b953__n100000.kt` | 0.07 s, 55 MB | 0.06 s, 54 MB | **1.34** | **1.02** |
| kotlin | `unterminated_a_ac3879__n100000.kt` | 101.95 s, 41 MB | 91.40 s, 31 MB | **1.13** | **1.29** |
| kotlin | `random_printable_cb9992__n1000000.kt` | 0.01 s, 10 MB | 0.01 s, 8 MB | 0.89 | **1.28** |
| kotlin | `random_bytes_697b59__n1000000.kt` | 0.01 s, 7 MB | 0.01 s, 6 MB | 0.82 | **1.22** |
| kotlin | `glr_valid_val_x_a_b_c_n_837d02__n100000.kt` | 0.66 s, 259 MB | 0.89 s, 220 MB | 0.76 | **1.18** |
| kotlin | `random_bracket_soup_7022a9__n1000000.kt` | 0.01 s, 7 MB | 0.01 s, 6 MB | 0.50 | **1.16** |
| kotlin | `glr_valid_val_y_a_b_c_n_fa3a79__n100000.kt` | 0.67 s, 253 MB | 0.92 s, 221 MB | 0.74 | **1.14** |
| kotlin | `broken_stmt_val_x_f_1_n_46f8f5__n10000.kt` | 1.46 s, 23 MB | 3.91 s, 21 MB | 0.37 | **1.10** |
| kotlin | `015_stray.kt` | 16.98 s, 3836 MB | KILL_RSS at 28 s, 4098 MB | n/a | n/a |
| kotlin | `075_stray.kt` | 13.50 s, 3835 MB | KILL_RSS at 20 s, 4110 MB | n/a | n/a |
| kotlin | `079_stray.kt` | KILL_RSS at 25 s, 4100 MB | KILL_RSS at 43 s, 4098 MB | n/a | n/a |
| kotlin | `128_stray.kt` | KILL_TIME at 120 s, 3570 MB | KILL_TIME at 120 s, 1926 MB | n/a | n/a |
| lua | `rep_n_160caa__n10000.lua` | 0.98 s, 8 MB | 1.51 s, 4 MB | 0.65 | **1.72** |
| lua | `random_bracket_soup_7022a9__n1000000.lua` | 0.01 s, 8 MB | 0.01 s, 6 MB | 0.81 | **1.43** |
| lua | `unterminated_a_1609f6__n10000.lua` | 2.05 s, 9 MB | 3.40 s, 6 MB | 0.60 | **1.41** |
| lua | `random_printable_cb9992__n1000000.lua` | 0.04 s, 7 MB | 0.08 s, 5 MB | 0.49 | **1.38** |
| lua | `broken_stmt_function_f_return_1_end_n_a7c1b1__n3000.lua` | 0.26 s, 11 MB | 0.55 s, 8 MB | 0.48 | **1.33** |
| lua | `advdeep_100000__lua_100000.lua` | 0.02 s, 33 MB | 0.04 s, 26 MB | 0.55 | **1.29** |
| php | `random_printable_cb9992__n1000000.php` | 2.11 s, 55 MB | 2.59 s, 40 MB | 0.82 | **1.37** |
| php | `rep_EOT_fa7752__n100000.php` | 0.23 s, 81 MB | 0.32 s, 73 MB | 0.73 | **1.11** |
| php | `deep_x_a_1_n_0eb324__n100000.php` | 0.15 s, 201 MB | 0.30 s, 185 MB | 0.48 | **1.09** |
| php | `unterminated_a_266e40__n30000.php` | 3.13 s, 29 MB | 10.57 s, 27 MB | 0.30 | **1.08** |
| php | `random_bracket_soup_7022a9__n1000000.php` | 0.90 s, 182 MB | 0.94 s, 175 MB | 0.97 | **1.04** |
| python | `pair_0dd45c__n100000.py` | 48.65 s, 32 MB | 60.72 s, 27 MB | 0.81 | **1.20** |
| python | `unterminated_f_a_7130d5__n100000.py` | 0.28 s, 79 MB | 1.14 s, 69 MB | 0.24 | **1.14** |
| python | `advdeep_100000__py_100000.py` | 0.03 s, 40 MB | 0.06 s, 37 MB | 0.51 | **1.10** |
| python | `glr_valid_print_a_for_a_in_b_n_0a52da__n100000.py` | 0.39 s, 177 MB | 0.49 s, 172 MB | 0.81 | **1.03** |
| python | `big5mb__big5mb.py` | 0.41 s, 312 MB | 0.68 s, 304 MB | 0.61 | **1.03** |
| ruby | `rep_q_n_428283__n30000.rb` | 7.84 s, 217 MB | 17.47 s, 67 MB | 0.45 | **3.26** |
| ruby | `random_bracket_soup_7022a9__n1000000.rb` | 0.02 s, 12 MB | 0.05 s, 8 MB | 0.46 | **1.56** |
| ruby | `unterminated_a_1942ab__n100000.rb` | 25.57 s, 86 MB | 43.54 s, 77 MB | 0.59 | **1.12** |
| ruby | `unterminated_a_1942ab__n30000.rb` | 2.21 s, 29 MB | 4.05 s, 27 MB | 0.55 | **1.11** |
| ruby | `pair_0dd45c__n100000.rb` | 49.84 s, 31 MB | 64.34 s, 29 MB | 0.78 | **1.06** |
| rust | `unterminated_r_a_4676a0__n10000.rs` | 2.29 s, 10 MB | 3.50 s, 7 MB | 0.66 | **1.40** |
| rust | `random_printable_cb9992__n1000000.rs` | 0.09 s, 30 MB | 0.12 s, 26 MB | 0.76 | **1.13** |
| rust | `advdeep_100000__rs_100000.rs` | 0.03 s, 40 MB | 0.05 s, 37 MB | 0.57 | **1.08** |
| rust | `big5mb__big5mb.rs` | 0.32 s, 307 MB | 0.59 s, 297 MB | 0.56 | **1.03** |
| rust | `random_bytes_697b59__n1000000.rs` | 0.50 s, 101 MB | 0.97 s, 98 MB | 0.51 | **1.03** |
| rust | `unterminated_r_a_bcb8a3__n100000.rs` | KILL_TIME at 120 s, 51 MB | KILL_TIME at 120 s, 43 MB | n/a | n/a |
| scala | `random_printable_cb9992__n100000.scala` | 0.05 s, 25 MB | 0.10 s, 15 MB | 0.49 | **1.63** |
| scala | `rep_n_656002__n30000.scala` | 0.03 s, 32 MB | 0.03 s, 28 MB | **1.31** | **1.13** |
| scala | `glr_valid_val_x_a_b_c_n_af35b2__n10000.scala` | 0.04 s, 40 MB | 0.05 s, 31 MB | 0.77 | **1.28** |
| scala | `rep_n_7fd5aa__n100000.scala` | 0.11 s, 85 MB | 0.10 s, 84 MB | **1.26** | **1.01** |
| scala | `random_bracket_soup_7022a9__n10000.scala` | 0.01 s, 10 MB | 0.01 s, 8 MB | 0.80 | **1.24** |
| scala | `broken_stmt_val_x_f_1_n_46f8f5__n3000.scala` | 0.02 s, 13 MB | 0.02 s, 11 MB | 0.82 | **1.21** |
| scala | `random_bytes_697b59__n100000.scala` | 0.05 s, 15 MB | 0.08 s, 14 MB | 0.59 | **1.11** |
| scala | `rep_n_0ca2f1__n100000.scala` | 0.08 s, 70 MB | 0.08 s, 64 MB | 0.96 | **1.10** |
| scala | `rep_n_59d967__n100000.scala` | 0.05 s, 58 MB | 0.06 s, 53 MB | 0.87 | **1.10** |
| tsx | `unterminated_a_a452e7__n100000.tsx` | 47.99 s, 29 MB | 61.86 s, 22 MB | 0.78 | **1.33** |
| tsx | `random_printable_cb9992__n100000.tsx` | 0.03 s, 12 MB | 0.10 s, 9 MB | 0.33 | **1.27** |
| tsx | `rep_045310__n30000.tsx` | 4.49 s, 18 MB | 9.48 s, 14 MB | 0.47 | **1.24** |
| tsx | `random_bracket_soup_7022a9__n1000000.tsx` | 1.79 s, 216 MB | 1.95 s, 176 MB | 0.93 | **1.23** |
| tsx | `block_type_A_n_f29830__n100000.tsx` | KILL_TIME at 120 s, 77 MB | KILL_TIME at 120 s, 62 MB | n/a | n/a |
| typescript | `random_printable_cb9992__n100000.ts` | 0.03 s, 12 MB | 0.11 s, 9 MB | 0.31 | **1.43** |
| typescript | `unterminated_a_a452e7__n100000.ts` | 48.23 s, 29 MB | 61.15 s, 21 MB | 0.79 | **1.36** |
| typescript | `random_bracket_soup_7022a9__n1000000.ts` | 2.08 s, 227 MB | 2.15 s, 177 MB | 0.97 | **1.28** |
| typescript | `rep_n_e980df__n100000.ts` | 73.15 s, 25 MB | 72.84 s, 19 MB | **1.02** | **1.27** |
| typescript | `advdeep_100000__ts_100000.ts` | 0.03 s, 41 MB | 0.05 s, 37 MB | 0.59 | **1.10** |
| typescript | `rep_n_aec841__n30000.ts` | 22.69 s, 31 MB | 46.06 s, 29 MB | 0.49 | **1.06** |
| typescript | `glr_valid_x_a_b_c_d_e_f_g_h_n_f95d77__n100000.ts` | 0.88 s, 394 MB | 1.19 s, 382 MB | 0.75 | **1.03** |
| typescript | `big5mb__big5mb.ts` | 0.41 s, 335 MB | 0.67 s, 327 MB | 0.63 | **1.02** |
| verilog | `pair_ebd0a6__n100000.v` | 52.23 s, 24 MB | 40.50 s, 10 MB | **1.31** | **2.51** |
| verilog | `rep_module_n_afbc83__n100000.v` | 22.95 s, 29 MB | 27.24 s, 19 MB | 0.85 | **1.51** |
| verilog | `rep_module_n_afbc83__n30000.v` | 2.03 s, 12 MB | 2.50 s, 8 MB | 0.82 | **1.46** |
| verilog | `unterminated_a_7f7cb3__n100000.v` | 0.19 s, 67 MB | 0.85 s, 62 MB | 0.22 | **1.07** |
| verilog | `broken_stmt_module_m_assign_x_a_endmodul_5e9445__n30000.v` | 0.52 s, 87 MB | 1.80 s, 82 MB | 0.28 | **1.06** |
| verilog | `rep_n_aec841__n100000.v` | 30.88 s, 55 MB | 69.99 s, 53 MB | 0.44 | **1.03** |
| verilog | `block_always_posedge_c_begin_n_11163b__n100000.v` | 101.35 s, 200 MB | KILL_TIME at 120 s, 144 MB | n/a | n/a |
| zig | `unterminated_c_a_3f6b05__n100000.zig` | 58.01 s, 48 MB | 66.41 s, 32 MB | 0.88 | **1.51** |
| zig | `unterminated_a_ff4b90__n100000.zig` | 47.67 s, 28 MB | 65.15 s, 23 MB | 0.74 | **1.21** |

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
