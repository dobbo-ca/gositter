# gositter vs C tree-sitter 0.25.0

Ratios are gositter divided by the other side: below 1.00 means gositter is better. `min` is the fastest timed pass over all rounds, `med` the median pass. The headline time columns are min-pass wall and user+sys CPU; median wall is secondary because it moves with the load on the machine. `CPU/wall` (sum of user+sys over sum of wall) is the number of logical CPUs the process got: a j=all cell under 0.7 x j is marked `*`, its wall is load noise and only CPU, instructions and cycles count there. Wall and CPU are per pass over the whole list; CPU, instructions and cycles are process-wide and count all threads, including the Go GC. Peak columns are the maximum over the processes of that config. A matrix process runs 6 passes (warm-up included) and the gositter peak at j=all still grows at that point, so the matrix peaks are labelled `after 6 passes`; the memory numbers for the steady state are the `steady` ones, from one process of 41 passes per grammar, set, j and side (`run.py mem`). 3 significant digits.

### Machine

From `bin/BUILDINFO.txt`:

    built: 2026-10-16T16:43:24Z
    host: Linux 7.0.0-38-generic x86_64, AMD Ryzen 7 9800X3D 8-Core Processor, 16 cpus
    glibc: glibc 2.36
    topology: 1 socket x 8 cores x 2 threads, smt active 1
    governor: powersave
    cpufreq driver: amd-pstate-epp, epp balance_performance
    cgroup cpu.max: max 100000
    perf_event_paranoid: 4
    go: go version go1.27.2 linux/amd64
    cc: gcc = gcc (Debian 12.2.0-14+deb12u1) 12.2.0
    cc default pic/pie macros: #define __pie__ 2 #define __pic__ 2 #define __PIC__ 2 #define __PIE__ 2 
    tree-sitter runtime: go-tree-sitter v0.25.0 src/lib.c, sha256 of src/*.c src/*.h concatenated: dbe269413058b1685fd699115a4f45af29fead6950466c4f1a0c8f0e1be7c19b

Linux measures. Instructions and cycles are perf_event_open hardware counters, user mode only, of all threads (one inherited event per thread that exists when counting starts). Counting starts at the first line of main (C) or at package init (Go), not at exec; a 0 means perf_event_open was refused. That is right for a timed pass but not for a whole process: it leaves out the loader, the Go runtime start and all kernel work, and what is left out differs per side. So the cost of a first-parse process is in the `exec to exit` rows: each of these processes runs under `perf stat`, which counts user and kernel mode from exec to exit, and has its own counters switched off (BENCH_NOPERF), because the two sets together need more hardware counters than are free and perf would then scale its counts. Results from before that change have the in-process counts, as `user instr after main/init` and `user cycles after main/init`. `start to main` is not measured (n/a). `peak RSS` and `peak footprint` are both VmHWM of /proc/self/status, the peak resident set, so the two columns are equal here. ru_maxrss is not used: on Linux it keeps the peak of the process image before exec (the watchdog's python, about 10 MB), which is a floor under every small process. `RSS above loaded` is VmHWM minus VmRSS after the files were read. All Go sides are built without cgo for the counters; gsbench is the CGO_ENABLED=0 build.

RSS on Linux and on macOS is not the same measure: the two kernels count shared, file-backed and compressed pages differently, the allocators differ (glibc malloc against the macOS zone allocator) and so does the page size (4 KB on amd64, 16 KB on Apple silicon). Compare memory only between sides on one machine, never across the two.

C is `cbench` at -O2 -std=c11, the best stock C build. These are not all the flags cgo uses here: on Linux cgo adds -fPIC (and -m64 -pthread) to every C file, while `cbench` is the gcc default, PIE, and with gcc -fPIC stops the inlining of exported functions in lib.c. C-pic is `cbench` with -fPIC on lib.c and the grammars (one round, parse and cursor only): C-pic against C is the cost of that flag, cgo against C-pic the cost of the binding. Both are printed under the summary. C-O3 is the same sources at -O3, the default of the upstream tree-sitter Makefile; it runs parse and cursor only. C-nopragma is C with one source line removed, see below. C-clang, where it ran, is C built by clang instead of the compiler cgo uses (one round, parse and cursor only): it shows how much of the result is the C compiler. Exact flags: `bin/BUILDINFO.txt`, `bin/BUILDINFO-O3.txt` and `bin/BUILDINFO-clang.txt`.

`walk` and `cursor` rows time the parse plus the traversal of each tree. The `walk minus parse` and `cursor minus parse` rows are the traversal alone: min pass of the walk or cursor process minus min pass of the parse process of the same grammar, set, j and side. They are a difference of two processes, so small ones are noisy; a difference at or below zero prints n/a.

### What each side pays

Pure C (`C`, `C-O3`) is the baseline for the speed of the runtime. The cgo side measures the go-tree-sitter binding, not the C runtime.

- Optimisation pragma. The pinned parser.c of 8 grammars (bash, c, cpp, csharp, julia, kotlin, ruby, verilog) carries the line `#pragma clang optimize off` (for GCC: `#pragma GCC optimize ("O0")`), so the compiler builds the lexer and the tables of these grammars without optimisation at any -O level. That holds for `C`, `C-O3` and cgo alike, and it is how upstream ships these grammars, so the stock build stays the baseline. `C-nopragma` is `C` (-O2, same lib.o, same scanner objects) with only that line removed from these 8 parser.c; it runs for these grammars only, for the others it would be `C`. gositter has no such switch: its tables and lexers are compiled like the rest of the Go code.
- Tree lifetime. A gositter tree lives in slabs owned by its parser and is valid only until the next Parse, Reset or SetLanguage on that parser; `Tree.Close` is a no-op. A C tree is owned by the caller and the runtime pays malloc and free per subtree for that. This harness keeps one live tree per parser and frees it at the next parse, which is the only lifetime gositter supports and its best case. C has no such reuse, by design of the library.
- cgo binding. go-tree-sitter v0.25.0 routes every C malloc, calloc, realloc and free through a Go callback (its `init` calls `ts_set_allocator`), `Kind()` copies the name with `C.GoString`, `Child()` and the cursor's `Node()` allocate a `*Node` on the Go heap, and `Parser.Parse` reads the source through a callback that copies the rest of the source (`C.CString(text[i:])`) at each call and frees the copies when the parse ends. One 100 KB scala file needs about 950 MB that way against 11 MB in pure C, so a cgo config can end in KILL_RSS. That is a memory result of the cgo side, not a missing measurement: its cells print KILL_RSS, the config is listed under `Configs a side did not finish` with the RSS of the other sides, and it is absent from the cgo ratios (the summary prints the n of each cell).
  With glibc the freed copies stay in the malloc arena of the thread that made them, so at j above 1 the cgo RSS grows over the passes as a large file moves between threads. Measured here on one 45 KB scala file alone, j=16: 335 MB after 2 passes, 652 MB after 6, killed above 1024 MB before 21; 375 MB after 41 passes with `MALLOC_ARENA_MAX=1`, which no run of this report sets. The largest scala file needs 806 MB alone, so the cgo scala cells at j=all can be KILL_RSS at a cap well above that.
- Child(i) walk. `ts_node_child` scans from the first child on each call, so the `walk` rows are not the usual C traversal; `cursor` (TreeCursor) is.

Malloc* and GO* variables seen by the benchmark processes: `{'GOTOOLCHAIN': 'local', 'GOPATH': '/go', 'GOLANG_VERSION': '1.27.2'}`.

1-minute load average at the end of each matrix process: min 1.41, median 3.68, max 11.2.

## Inputs

`error files` is the number of files whose root has_error is set. `real` holds only the real files that parse without error; `splitreal.py` moved the others to `realerr` (counts per grammar in `lists/REALSPLIT.tsv`), the real files whose tree has an error. A grammar with no such file has no `realerr` rows. `broken` is mutated and adversarial input.

| grammar | set | files | MB | error files | % error files | sides agree | in summary |
|---|---|---|---|---|---|---|---|
| bash | broken | 501 | 2.79 | 323 | 64.5 | yes | yes |
| bash | real | 638 | 2.39 | 0 | 0 | yes | yes |
| bash | realerr | 43 | 0.69 | 43 | 100 | yes | yes |
| c | broken | 505 | 4.91 | 428 | 84.8 | yes | yes |
| c | real | 485 | 3.37 | 0 | 0 | yes | yes |
| c | realerr | 364 | 7.69 | 364 | 100 | yes | yes |
| cpp | broken | 500 | 7.46 | 472 | 94.4 | yes | yes |
| cpp | real | 318 | 2.66 | 0 | 0 | yes | yes |
| cpp | realerr | 371 | 9.69 | 371 | 100 | yes | yes |
| csharp | broken | 497 | 2.79 | 431 | 86.7 | yes | yes |
| csharp | real | 1437 | 7.89 | 0 | 0 | yes | yes |
| csharp | realerr | 63 | 1.96 | 63 | 100 | yes | yes |
| golang | broken | 501 | 3.99 | 393 | 78.4 | yes | yes |
| golang | real | 1440 | 11.5 | 0 | 0 | yes | yes |
| golang | realerr | 60 | 0.988 | 60 | 100 | yes | yes |
| hcl | broken | 497 | 1.6 | 446 | 89.7 | yes | yes |
| hcl | real | 1500 | 4.62 | 0 | 0 | yes | yes |
| java | broken | 501 | 2.45 | 413 | 82.4 | yes | yes |
| java | real | 1498 | 8.53 | 0 | 0 | yes | yes |
| java | realerr | 2 | 0.000907 | 2 | 100 | yes | no, too small |
| javascript | broken | 508 | 4.44 | 415 | 81.7 | yes | yes |
| javascript | real | 1344 | 11.9 | 0 | 0 | yes | yes |
| javascript | realerr | 9 | 0.0019 | 9 | 100 | yes | no, too small |
| json | broken | 498 | 8.09 | 476 | 95.6 | yes | yes |
| json | real | 651 | 12 | 0 | 0 | yes | yes |
| json | realerr | 38 | 0.187 | 38 | 100 | yes | yes |
| julia | broken | 493 | 4.77 | 410 | 83.2 | yes | yes |
| julia | real | 448 | 4.42 | 0 | 0 | yes | yes |
| julia | realerr | 23 | 0.806 | 23 | 100 | yes | yes |
| kotlin | broken | 493 | 1.95 | 416 | 84.4 | yes | yes |
| kotlin | real | 1475 | 6.48 | 0 | 0 | yes | yes |
| kotlin | realerr | 25 | 0.544 | 25 | 100 | yes | yes |
| lua | broken | 501 | 2.48 | 459 | 91.6 | yes | yes |
| lua | real | 1415 | 11.1 | 0 | 0 | yes | yes |
| lua | realerr | 1 | 0.000291 | 1 | 100 | yes | no, too small |
| php | broken | 497 | 3.94 | 409 | 82.3 | yes | yes |
| php | real | 1499 | 9.28 | 0 | 0 | yes | yes |
| php | realerr | 1 | 0 | 1 | 100 | yes | no, too small |
| python | broken | 507 | 5.3 | 407 | 80.3 | yes | yes |
| python | real | 1022 | 11.8 | 0 | 0 | yes | yes |
| python | realerr | 3 | 0.000982 | 3 | 100 | yes | no, too small |
| ruby | broken | 501 | 2.73 | 425 | 84.8 | yes | yes |
| ruby | real | 1500 | 8.33 | 0 | 0 | yes | yes |
| rust | broken | 501 | 4.37 | 443 | 88.4 | yes | yes |
| rust | real | 1042 | 9.05 | 0 | 0 | yes | yes |
| rust | realerr | 1 | 0.000257 | 1 | 100 | yes | no, too small |
| scala | broken | 497 | 2.31 | 429 | 86.3 | yes | yes |
| scala | real | 773 | 4.22 | 0 | 0 | yes | yes |
| scala | realerr | 84 | 0.698 | 84 | 100 | yes | yes |
| tsx | broken | 497 | 2.58 | 432 | 86.9 | yes | yes |
| tsx | real | 578 | 2.98 | 0 | 0 | yes | yes |
| tsx | realerr | 45 | 0.492 | 45 | 100 | yes | yes |
| typescript | broken | 507 | 3.92 | 420 | 82.8 | yes | yes |
| typescript | real | 1431 | 11.7 | 0 | 0 | yes | yes |
| typescript | realerr | 69 | 0.168 | 69 | 100 | yes | yes |
| verilog | broken | 497 | 4.38 | 458 | 92.2 | yes | yes |
| verilog | real | 195 | 0.495 | 0 | 0 | yes | yes |
| verilog | realerr | 441 | 5.23 | 441 | 100 | yes | yes |
| zig | broken | 478 | 8.31 | 444 | 92.9 | yes | yes |
| zig | real | 423 | 10.2 | 0 | 0 | yes | yes |
| zig | realerr | 15 | 0.729 | 15 | 100 | yes | yes |

## Cross-side agreement

For every walk and cursor config the node count of each pass and the checksum of the last pass (per node: first byte of the kind + start byte + end byte, summed over the list) of each side that finished are compared with gositter. Parse configs visit no node; their error-file counts are in the table above.

Nodes and checksum agree: yes, in all 244 walk and cursor configs (cgo in 244, C in 244, C-nopragma in 92, C-O3 in 122, C-clang in 122, C-pic in 122 compared with gositter).

## Configs a side did not finish: 5

A process killed by the watchdog at its memory cap (KILL_RSS) or time limit (KILL_TIME). This is a result of that side, not a missing measurement: with `side` cgo the go-tree-sitter binding needed more memory than the cap for a list that gositter and pure C parsed with the peak RSS in the last two columns. `cmp` is the oracle, which holds the C tree through go-tree-sitter next to the gositter tree: a KILL_RSS there is cgo-side memory too and says nothing about the trees, which stay uncompared for that list. The per-grammar tables print the status in the cell of that side, and the config is absent from its ratios.

| run | grammar | set | what | j | side | status | cap MB | limit s | wall s at the kill | RSS MB at the kill | gs peak RSS MB | C peak RSS MB |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| matrix | scala | real | parse | all | cgo | KILL_RSS | 8192 | 900 | 2.42 | 8429 | 63.7 | 80.4 |
| matrix | scala | real | cursor | all | cgo | KILL_RSS | 8192 | 900 | 2.4 | 8397 | 79.3 | 80 |
| mem | cpp | broken | parse | all | cgo | KILL_RSS | 2048 | 900 | 25.45 | 2100 | 398 | 1900 |
| mem | scala | real | parse | all | cgo | KILL_RSS | 8192 | 900 | 2.6 | 8404 | 71.8 | 98.3 |
| mem | scala | realerr | parse | all | cgo | KILL_RSS | 8192 | 900 | 4.41 | 8353 | 61.5 | 65.2 |

## Summary across grammars

Each cell: geometric mean (min-max) of the per-grammar ratio, and n = the number of grammars in that cell (a grammar whose process was killed on one side is missing from the cell, see `Configs a side did not finish`; the C-nopragma cells hold only the 8 grammars with the pragma). CPU, instructions and cycles use the median pass. `walk` and `cursor` include the parse; the second table of each side is the traversal alone, from min passes.

A list under 100000 bytes is left out of every cell here: one pass over it takes well under a millisecond, so its numbers, above all at j=all, are thread wake-up and scheduler cost and not parsing. Its per-grammar tables are still printed, with the set marked `too small`. Left out: java realerr (907 B), javascript realerr (1898 B), lua realerr (291 B), php realerr (25 B), python realerr (982 B), rust realerr (257 B).

612 of 984 side-configs at j above 1 got under 0.7 x j logical CPUs (CPU/wall): the wall columns of the j=all rows are valid only where that count is 0, CPU and cycles are the j=all numbers otherwise.

j=all is 16 threads on 1 socket x 8 cores x 2 threads, smt active 1, the same for every side. With SMT that is two threads per physical core, so a j=all wall is not a one-thread-per-core number. In this run the j=1 min-pass wall is 6.31x (1.21-9.4) the j=all one and a j=all pass costs 1.48x (1.03-1.77) the cycles of a j=1 pass: median (range) over 894 side-configs.

### gositter / pure C -O2

| set | j | what | wall min pass | CPU user+sys | wall median | instructions | cycles | peak RSS after 6 passes | peak footprint after 6 passes |
|---|---|---|---|---|---|---|---|---|---|
| real | 1 | parse | 0.491 (0.443-0.577) n=21 | 0.493 (0.445-0.57) n=21 | 0.492 (0.444-0.569) n=21 | 0.637 (0.572-0.749) n=21 | 0.493 (0.444-0.569) n=21 | 1.33 (1.12-1.7) n=21 | 1.33 (1.12-1.7) n=21 |
| real | 1 | walk | 0.537 (0.487-0.615) n=21 | 0.537 (0.488-0.61) n=21 | 0.536 (0.487-0.608) n=21 | 0.694 (0.636-0.762) n=21 | 0.536 (0.487-0.608) n=21 | 1.28 (1.07-1.67) n=21 | 1.28 (1.07-1.67) n=21 |
| real | 1 | cursor | 0.523 (0.472-0.607) n=21 | 0.525 (0.484-0.601) n=21 | 0.524 (0.483-0.601) n=21 | 0.672 (0.62-0.739) n=21 | 0.524 (0.484-0.6) n=21 | 1.83 (1.41-2.53) n=21 | 1.83 (1.41-2.53) n=21 |
| real | all | parse | 0.512 (0.47-0.61) n=21 | 0.507 (0.455-0.588) n=21 | 0.519 (0.461-0.647) n=21 | 0.634 (0.57-0.752) n=21 | 0.508 (0.467-0.611) n=21 | 0.74 (0.471-1.32) n=21 | 0.74 (0.471-1.32) n=21 |
| real | all | walk | 0.562 (0.527-0.642) n=21 | 0.554 (0.518-0.638) n=21 | 0.564 (0.523-0.647) n=21 | 0.692 (0.631-0.764) n=21 | 0.554 (0.517-0.662) n=21 | 0.738 (0.447-1.31) n=21 | 0.738 (0.447-1.31) n=21 |
| real | all | cursor | 0.547 (0.49-0.618) n=21 | 0.549 (0.5-0.649) n=21 | 0.556 (0.52-0.67) n=21 | 0.67 (0.617-0.741) n=21 | 0.547 (0.512-0.643) n=21 | 0.914 (0.581-1.36) n=21 | 0.914 (0.581-1.36) n=21 |
| realerr | 1 | parse | 0.456 (0.262-0.548) n=13 | 0.448 (0.26-0.548) n=13 | 0.447 (0.26-0.545) n=13 | 0.55 (0.255-0.699) n=13 | 0.447 (0.26-0.547) n=13 | 1.42 (1.19-1.85) n=13 | 1.42 (1.19-1.85) n=13 |
| realerr | 1 | walk | 0.497 (0.298-0.6) n=13 | 0.496 (0.295-0.579) n=13 | 0.496 (0.294-0.578) n=13 | 0.6 (0.3-0.749) n=13 | 0.496 (0.294-0.578) n=13 | 1.34 (1.06-1.74) n=13 | 1.34 (1.06-1.75) n=13 |
| realerr | 1 | cursor | 0.483 (0.28-0.57) n=13 | 0.481 (0.273-0.55) n=13 | 0.48 (0.273-0.548) n=13 | 0.583 (0.276-0.723) n=13 | 0.48 (0.273-0.55) n=13 | 1.44 (0.993-1.89) n=13 | 1.44 (0.995-1.89) n=13 |
| realerr | all | parse | 0.453 (0.165-0.601) n=13 | 0.465 (0.267-0.595) n=13 | 0.455 (0.164-0.649) n=13 | 0.554 (0.257-0.697) n=13 | 0.469 (0.26-0.586) n=13 | 0.912 (0.526-1.62) n=13 | 0.912 (0.526-1.62) n=13 |
| realerr | all | walk | 0.496 (0.228-0.647) n=13 | 0.504 (0.298-0.596) n=13 | 0.503 (0.23-0.636) n=13 | 0.603 (0.303-0.748) n=13 | 0.506 (0.291-0.584) n=13 | 0.899 (0.499-1.54) n=13 | 0.899 (0.499-1.54) n=13 |
| realerr | all | cursor | 0.491 (0.173-0.683) n=13 | 0.501 (0.286-0.618) n=13 | 0.484 (0.174-0.63) n=13 | 0.588 (0.278-0.721) n=13 | 0.502 (0.278-0.607) n=13 | 0.904 (0.496-1.59) n=13 | 0.905 (0.496-1.59) n=13 |
| broken | 1 | parse | 0.393 (0.188-0.535) n=21 | 0.396 (0.185-0.538) n=21 | 0.395 (0.184-0.537) n=21 | 0.48 (0.187-0.69) n=21 | 0.397 (0.184-0.541) n=21 | 1.35 (1.08-1.66) n=21 | 1.35 (1.08-1.66) n=21 |
| broken | 1 | walk | 0.499 (0.249-0.786) n=21 | 0.496 (0.244-0.76) n=21 | 0.495 (0.243-0.759) n=21 | 0.581 (0.26-0.787) n=21 | 0.497 (0.243-0.76) n=21 | 1.45 (1.03-1.77) n=21 | 1.45 (1.03-1.77) n=21 |
| broken | 1 | cursor | 0.425 (0.192-0.553) n=21 | 0.429 (0.2-0.563) n=21 | 0.427 (0.2-0.562) n=21 | 0.517 (0.202-0.725) n=21 | 0.428 (0.2-0.565) n=21 | 1.98 (1.33-2.55) n=21 | 1.98 (1.33-2.55) n=21 |
| broken | all | parse | 0.372 (0.153-0.809) n=21 | 0.413 (0.201-0.526) n=21 | 0.373 (0.155-0.737) n=21 | 0.481 (0.189-0.686) n=21 | 0.414 (0.194-0.527) n=21 | 0.601 (0.386-1.49) n=21 | 0.601 (0.386-1.49) n=21 |
| broken | all | walk | 0.54 (0.224-0.913) n=21 | 0.503 (0.249-0.707) n=21 | 0.533 (0.227-0.878) n=21 | 0.585 (0.262-0.784) n=21 | 0.506 (0.24-0.721) n=21 | 0.597 (0.405-1.56) n=21 | 0.597 (0.405-1.56) n=21 |
| broken | all | cursor | 0.425 (0.157-0.802) n=21 | 0.448 (0.216-0.562) n=21 | 0.426 (0.163-0.761) n=21 | 0.518 (0.204-0.721) n=21 | 0.447 (0.206-0.568) n=21 | 0.762 (0.45-1.74) n=21 | 0.762 (0.45-1.74) n=21 |

### gositter / pure C -O2: traversal alone

| set | j | what | instructions min | cycles min | CPU user+sys min |
|---|---|---|---|---|---|
| real | 1 | walk minus parse | 1.3 (1.11-1.41) n=21 | 1.06 (0.945-1.24) n=21 | 1.07 (0.943-1.25) n=21 |
| real | 1 | cursor minus parse | 0.855 (0.601-0.917) n=21 | 0.722 (0.417-0.842) n=21 | 0.727 (0.421-0.86) n=21 |
| real | all | walk minus parse | 1.3 (1.17-1.42) n=21 | 1.12 (0.761-2.15) n=21 | 1.11 (0.74-2.04) n=21 |
| real | all | cursor minus parse | 0.858 (0.602-0.918) n=21 | 0.811 (0.519-1.07) n=21 | 0.813 (0.504-1.06) n=21 |
| realerr | 1 | walk minus parse | 1.25 (0.926-1.4) n=13 | 1.05 (0.781-1.21) n=13 | 1.05 (0.759-1.22) n=13 |
| realerr | 1 | cursor minus parse | 0.89 (0.818-0.928) n=13 | 0.766 (0.671-0.92) n=13 | 0.767 (0.671-0.929) n=13 |
| realerr | all | walk minus parse | 1.25 (0.923-1.4) n=13 | 1.09 (0.496-2.38) n=13 | 1.12 (0.561-4.06) n=13 |
| realerr | all | cursor minus parse | 0.907 (0.835-0.964) n=13 | 0.794 (0.428-1.37) n=13 | 0.881 (0.538-1.67) n=13 |
| broken | 1 | walk minus parse | 1.09 (0.804-1.92) n=21 | 1.05 (0.747-1.79) n=21 | 1.05 (0.749-1.83) n=21 |
| broken | 1 | cursor minus parse | 0.893 (0.787-0.935) n=21 | 0.807 (0.227-4.06) n=21 | 0.822 (0.225-4.5) n=21 |
| broken | all | walk minus parse | 1.11 (0.804-2.14) n=21 | 0.881 (0.324-1.55) n=21 | 0.867 (0.329-1.49) n=21 |
| broken | all | cursor minus parse | 0.884 (0.75-0.925) n=21 | 0.744 (0.469-1.7) n=21 | 0.737 (0.455-2.13) n=21 |

### gositter / pure C -O2: memory in the steady state (41 passes)

| set | j | what | steady peak RSS | steady peak footprint | steady RSS above loaded |
|---|---|---|---|---|---|
| real | 1 | parse | 1.35 (1.14-1.77) n=21 | 1.35 (1.14-1.77) n=21 | 1.33 (1.06-1.86) n=21 |
| real | all | parse | 0.639 (0.297-1.24) n=21 | 0.639 (0.297-1.24) n=21 | 0.575 (0.261-1.2) n=21 |
| realerr | 1 | parse | 1.46 (1.19-1.92) n=13 | 1.46 (1.19-1.92) n=13 | 1.53 (1.17-2.08) n=13 |
| realerr | all | parse | 0.761 (0.391-1.54) n=13 | 0.761 (0.391-1.54) n=13 | 0.718 (0.374-1.5) n=13 |
| broken | 1 | parse | 1.37 (1.07-1.93) n=21 | 1.37 (1.07-1.93) n=21 | 1.38 (0.919-2.26) n=21 |
| broken | all | parse | 0.513 (0.209-1.39) n=21 | 0.513 (0.209-1.39) n=21 | 0.488 (0.204-1.34) n=21 |

### gositter / pure C -O2 without the pragma

| set | j | what | wall min pass | CPU user+sys | wall median | instructions | cycles |
|---|---|---|---|---|---|---|---|
| real | 1 | parse | 0.507 (0.468-0.604) n=8 | 0.509 (0.463-0.603) n=8 | 0.508 (0.462-0.602) n=8 | 0.65 (0.609-0.738) n=8 | 0.509 (0.461-0.603) n=8 |
| real | 1 | walk | 0.55 (0.509-0.641) n=8 | 0.551 (0.507-0.637) n=8 | 0.55 (0.506-0.636) n=8 | 0.705 (0.67-0.782) n=8 | 0.55 (0.506-0.636) n=8 |
| real | 1 | cursor | 0.537 (0.505-0.624) n=8 | 0.538 (0.511-0.614) n=8 | 0.538 (0.511-0.613) n=8 | 0.684 (0.653-0.76) n=8 | 0.538 (0.511-0.617) n=8 |
| real | all | parse | 0.528 (0.496-0.63) n=8 | 0.518 (0.464-0.624) n=8 | 0.543 (0.501-0.685) n=8 | 0.648 (0.607-0.736) n=8 | 0.521 (0.481-0.633) n=8 |
| real | all | walk | 0.578 (0.534-0.655) n=8 | 0.563 (0.524-0.668) n=8 | 0.583 (0.538-0.679) n=8 | 0.703 (0.665-0.781) n=8 | 0.565 (0.528-0.683) n=8 |
| real | all | cursor | 0.567 (0.516-0.65) n=8 | 0.558 (0.513-0.657) n=8 | 0.573 (0.54-0.682) n=8 | 0.682 (0.651-0.758) n=8 | 0.557 (0.518-0.655) n=8 |
| realerr | 1 | parse | 0.43 (0.264-0.496) n=7 | 0.428 (0.263-0.493) n=7 | 0.428 (0.263-0.492) n=7 | 0.517 (0.258-0.62) n=7 | 0.428 (0.263-0.492) n=7 |
| realerr | 1 | walk | 0.471 (0.299-0.545) n=7 | 0.473 (0.295-0.549) n=7 | 0.472 (0.295-0.549) n=7 | 0.565 (0.304-0.68) n=7 | 0.473 (0.295-0.549) n=7 |
| realerr | 1 | cursor | 0.459 (0.281-0.526) n=7 | 0.456 (0.276-0.526) n=7 | 0.455 (0.276-0.525) n=7 | 0.55 (0.279-0.652) n=7 | 0.455 (0.275-0.525) n=7 |
| realerr | all | parse | 0.419 (0.166-0.528) n=7 | 0.437 (0.266-0.507) n=7 | 0.419 (0.166-0.543) n=7 | 0.52 (0.261-0.633) n=7 | 0.436 (0.257-0.513) n=7 |
| realerr | all | walk | 0.468 (0.234-0.588) n=7 | 0.478 (0.301-0.564) n=7 | 0.475 (0.231-0.58) n=7 | 0.569 (0.307-0.694) n=7 | 0.475 (0.293-0.553) n=7 |
| realerr | all | cursor | 0.452 (0.174-0.579) n=7 | 0.468 (0.288-0.542) n=7 | 0.445 (0.173-0.569) n=7 | 0.553 (0.282-0.663) n=7 | 0.468 (0.28-0.543) n=7 |
| broken | 1 | parse | 0.355 (0.187-0.546) n=8 | 0.354 (0.185-0.551) n=8 | 0.354 (0.185-0.55) n=8 | 0.423 (0.188-0.701) n=8 | 0.355 (0.185-0.555) n=8 |
| broken | 1 | walk | 0.401 (0.25-0.569) n=8 | 0.401 (0.244-0.574) n=8 | 0.4 (0.243-0.573) n=8 | 0.481 (0.262-0.731) n=8 | 0.402 (0.243-0.578) n=8 |
| broken | 1 | cursor | 0.382 (0.193-0.556) n=8 | 0.387 (0.2-0.571) n=8 | 0.385 (0.2-0.569) n=8 | 0.457 (0.203-0.714) n=8 | 0.387 (0.2-0.573) n=8 |
| broken | all | parse | 0.343 (0.154-0.862) n=8 | 0.374 (0.203-0.531) n=8 | 0.346 (0.156-0.771) n=8 | 0.425 (0.191-0.702) n=8 | 0.371 (0.195-0.536) n=8 |
| broken | all | walk | 0.426 (0.224-0.867) n=8 | 0.417 (0.251-0.56) n=8 | 0.425 (0.226-0.802) n=8 | 0.487 (0.264-0.734) n=8 | 0.415 (0.241-0.567) n=8 |
| broken | all | cursor | 0.397 (0.158-0.802) n=8 | 0.406 (0.218-0.552) n=8 | 0.402 (0.163-0.795) n=8 | 0.459 (0.205-0.716) n=8 | 0.402 (0.207-0.559) n=8 |

### gositter / pure C -O2 without the pragma: traversal alone

| set | j | what | instructions min | cycles min | CPU user+sys min |
|---|---|---|---|---|---|
| real | 1 | walk minus parse | 1.3 (1.21-1.38) n=8 | 1.07 (0.984-1.12) n=8 | 1.07 (0.987-1.11) n=8 |
| real | 1 | cursor minus parse | 0.873 (0.845-0.917) n=8 | 0.747 (0.685-0.815) n=8 | 0.749 (0.685-0.822) n=8 |
| real | all | walk minus parse | 1.31 (1.2-1.4) n=8 | 1 (0.777-1.37) n=8 | 0.974 (0.741-1.31) n=8 |
| real | all | cursor minus parse | 0.875 (0.85-0.912) n=8 | 0.833 (0.704-1.26) n=8 | 0.876 (0.677-1.63) n=8 |
| realerr | 1 | walk minus parse | 1.23 (0.926-1.34) n=7 | 1.15 (0.927-1.66) n=7 | 1.15 (0.919-1.68) n=7 |
| realerr | 1 | cursor minus parse | 0.899 (0.862-0.928) n=7 | 0.812 (0.693-0.942) n=7 | 0.819 (0.697-0.954) n=7 |
| realerr | all | walk minus parse | 1.24 (0.924-1.37) n=7 | 1.21 (0.809-1.66) n=7 | 1.31 (0.964-1.89) n=7 |
| realerr | all | cursor minus parse | 0.888 (0.847-0.928) n=7 | 1.02 (0.646-1.84) n=7 | 1.11 (0.66-2.36) n=7 |
| broken | 1 | walk minus parse | 1.27 (0.856-1.92) n=8 | 1.12 (0.96-1.63) n=8 | 1.11 (0.967-1.57) n=8 |
| broken | 1 | cursor minus parse | 0.906 (0.869-0.935) n=8 | 0.695 (0.243-0.954) n=8 | 0.692 (0.242-0.951) n=8 |
| broken | all | walk minus parse | 1.33 (0.857-2.19) n=8 | 1.01 (0.607-1.5) n=8 | 1.01 (0.611-1.76) n=8 |
| broken | all | cursor minus parse | 0.897 (0.85-0.926) n=8 | 0.824 (0.402-1.12) n=8 | 0.846 (0.448-1.11) n=8 |

### gositter / pure C -O3

| set | j | what | wall min pass | CPU user+sys | wall median | instructions | cycles |
|---|---|---|---|---|---|---|---|
| real | 1 | parse | 0.479 (0.425-0.575) n=21 | 0.473 (0.422-0.576) n=21 | 0.473 (0.422-0.576) n=21 | 0.652 (0.584-0.761) n=21 | 0.473 (0.423-0.576) n=21 |
| real | 1 | cursor | 0.504 (0.46-0.587) n=21 | 0.501 (0.471-0.582) n=21 | 0.501 (0.47-0.581) n=21 | 0.683 (0.627-0.749) n=21 | 0.501 (0.469-0.58) n=21 |
| real | all | parse | 0.519 (0.467-0.613) n=21 | 0.512 (0.456-0.602) n=21 | 0.521 (0.46-0.676) n=21 | 0.649 (0.581-0.763) n=21 | 0.512 (0.467-0.628) n=21 |
| real | all | cursor | 0.547 (0.491-0.622) n=21 | 0.547 (0.503-0.642) n=21 | 0.553 (0.508-0.676) n=21 | 0.681 (0.626-0.751) n=21 | 0.545 (0.506-0.641) n=21 |
| realerr | 1 | parse | 0.449 (0.262-0.569) n=13 | 0.444 (0.26-0.567) n=13 | 0.443 (0.26-0.564) n=13 | 0.568 (0.262-0.778) n=13 | 0.443 (0.26-0.566) n=13 |
| realerr | 1 | cursor | 0.477 (0.279-0.61) n=13 | 0.471 (0.274-0.59) n=13 | 0.47 (0.273-0.587) n=13 | 0.597 (0.283-0.789) n=13 | 0.47 (0.273-0.592) n=13 |
| realerr | all | parse | 0.442 (0.168-0.609) n=13 | 0.468 (0.268-0.638) n=13 | 0.446 (0.169-0.61) n=13 | 0.572 (0.264-0.777) n=13 | 0.469 (0.259-0.632) n=13 |
| realerr | all | cursor | 0.478 (0.174-0.643) n=13 | 0.498 (0.285-0.647) n=13 | 0.477 (0.175-0.648) n=13 | 0.603 (0.285-0.787) n=13 | 0.499 (0.277-0.638) n=13 |
| broken | 1 | parse | 0.39 (0.188-0.549) n=21 | 0.388 (0.186-0.557) n=21 | 0.388 (0.185-0.557) n=21 | 0.492 (0.191-0.722) n=21 | 0.389 (0.186-0.561) n=21 |
| broken | 1 | cursor | 0.418 (0.196-0.544) n=21 | 0.42 (0.197-0.563) n=21 | 0.418 (0.197-0.562) n=21 | 0.527 (0.206-0.748) n=21 | 0.419 (0.197-0.565) n=21 |
| broken | all | parse | 0.378 (0.155-0.905) n=21 | 0.416 (0.201-0.536) n=21 | 0.377 (0.157-0.804) n=21 | 0.493 (0.194-0.714) n=21 | 0.415 (0.193-0.543) n=21 |
| broken | all | cursor | 0.422 (0.157-0.871) n=21 | 0.448 (0.216-0.58) n=21 | 0.428 (0.162-0.832) n=21 | 0.529 (0.208-0.743) n=21 | 0.447 (0.204-0.583) n=21 |

### gositter / pure C -O3: traversal alone

| set | j | what | instructions min | cycles min | CPU user+sys min |
|---|---|---|---|---|---|
| real | 1 | cursor minus parse | 0.833 (0.594-0.896) n=21 | 0.649 (0.427-0.939) n=21 | 0.652 (0.43-0.942) n=21 |
| real | all | cursor minus parse | 0.836 (0.592-0.898) n=21 | 0.825 (0.605-1.22) n=21 | 0.838 (0.605-1.86) n=21 |
| realerr | 1 | cursor minus parse | 0.866 (0.798-0.906) n=13 | 0.776 (0.6-1.06) n=13 | 0.777 (0.608-1.02) n=13 |
| realerr | all | cursor minus parse | 0.881 (0.854-0.926) n=13 | 0.66 (0.483-1.1) n=13 | 0.636 (0.469-0.96) n=13 |
| broken | 1 | cursor minus parse | 0.868 (0.777-0.909) n=21 | 0.705 (0.287-1.94) n=21 | 0.718 (0.286-1.99) n=21 |
| broken | all | cursor minus parse | 0.862 (0.736-0.904) n=21 | 0.753 (0.395-1.05) n=21 | 0.749 (0.414-1.13) n=21 |

### gositter / pure C -O2 built by clang

| set | j | what | wall min pass | CPU user+sys | wall median | instructions | cycles |
|---|---|---|---|---|---|---|---|
| real | 1 | parse | 0.489 (0.442-0.562) n=21 | 0.494 (0.445-0.59) n=21 | 0.494 (0.445-0.59) n=21 | 0.642 (0.553-0.731) n=21 | 0.494 (0.443-0.59) n=21 |
| real | 1 | cursor | 0.471 (0.429-0.534) n=21 | 0.476 (0.432-0.531) n=21 | 0.476 (0.432-0.53) n=21 | 0.666 (0.592-0.717) n=21 | 0.476 (0.432-0.53) n=21 |
| real | all | parse | 0.526 (0.46-0.612) n=21 | 0.533 (0.453-0.612) n=21 | 0.534 (0.47-0.673) n=21 | 0.639 (0.551-0.733) n=21 | 0.53 (0.465-0.622) n=21 |
| real | all | cursor | 0.523 (0.438-0.587) n=21 | 0.539 (0.465-0.626) n=21 | 0.536 (0.48-0.645) n=21 | 0.664 (0.591-0.718) n=21 | 0.532 (0.471-0.616) n=21 |
| realerr | 1 | parse | 0.472 (0.301-0.577) n=13 | 0.473 (0.302-0.586) n=13 | 0.472 (0.302-0.583) n=13 | 0.56 (0.267-0.767) n=13 | 0.472 (0.302-0.584) n=13 |
| realerr | 1 | cursor | 0.462 (0.309-0.535) n=13 | 0.467 (0.308-0.555) n=13 | 0.466 (0.308-0.554) n=13 | 0.585 (0.287-0.77) n=13 | 0.466 (0.308-0.554) n=13 |
| realerr | all | parse | 0.465 (0.174-0.57) n=13 | 0.498 (0.31-0.636) n=13 | 0.475 (0.176-0.615) n=13 | 0.564 (0.269-0.762) n=13 | 0.501 (0.303-0.634) n=13 |
| realerr | all | cursor | 0.462 (0.179-0.596) n=13 | 0.5 (0.326-0.631) n=13 | 0.476 (0.184-0.652) n=13 | 0.591 (0.29-0.77) n=13 | 0.499 (0.318-0.627) n=13 |
| broken | 1 | parse | 0.424 (0.216-0.567) n=21 | 0.429 (0.213-0.574) n=21 | 0.428 (0.213-0.573) n=21 | 0.496 (0.196-0.687) n=21 | 0.43 (0.213-0.578) n=21 |
| broken | 1 | cursor | 0.423 (0.215-0.551) n=21 | 0.43 (0.223-0.568) n=21 | 0.428 (0.223-0.567) n=21 | 0.527 (0.211-0.705) n=21 | 0.43 (0.223-0.57) n=21 |
| broken | all | parse | 0.421 (0.164-0.77) n=21 | 0.453 (0.236-0.557) n=21 | 0.429 (0.165-0.777) n=21 | 0.497 (0.198-0.68) n=21 | 0.455 (0.228-0.552) n=21 |
| broken | all | cursor | 0.446 (0.167-0.78) n=21 | 0.468 (0.248-0.561) n=21 | 0.458 (0.173-0.776) n=21 | 0.528 (0.213-0.702) n=21 | 0.468 (0.237-0.56) n=21 |

### gositter / pure C -O2 built by clang: traversal alone

| set | j | what | instructions min | cycles min | CPU user+sys min |
|---|---|---|---|---|---|
| real | 1 | cursor minus parse | 0.776 (0.551-0.831) n=21 | 0.401 (0.283-0.469) n=21 | 0.403 (0.286-0.474) n=21 |
| real | all | cursor minus parse | 0.777 (0.537-0.821) n=21 | 0.54 (0.413-0.699) n=21 | 0.567 (0.441-0.805) n=21 |
| realerr | 1 | cursor minus parse | 0.805 (0.749-0.841) n=13 | 0.422 (0.35-0.615) n=13 | 0.423 (0.352-0.624) n=13 |
| realerr | all | cursor minus parse | 0.817 (0.771-0.883) n=13 | 0.516 (0.351-1.13) n=13 | 0.546 (0.314-1.86) n=13 |
| broken | 1 | cursor minus parse | 0.806 (0.702-0.852) n=21 | 0.419 (0.21-0.612) n=21 | 0.424 (0.208-0.613) n=21 |
| broken | all | cursor minus parse | 0.804 (0.679-0.864) n=21 | 0.57 (0.444-0.907) n=21 | 0.596 (0.443-0.864) n=21 |

### gositter / cgo

| set | j | what | wall min pass | CPU user+sys | wall median | instructions | cycles | peak RSS after 6 passes | peak footprint after 6 passes |
|---|---|---|---|---|---|---|---|---|---|
| real | 1 | parse | 0.282 (0.22-0.322) n=21 | 0.281 (0.191-0.322) n=21 | 0.284 (0.218-0.324) n=21 | 0.39 (0.348-0.451) n=21 | 0.284 (0.212-0.324) n=21 | 0.65 (0.0252-1.01) n=21 | 0.651 (0.0252-1.01) n=21 |
| real | 1 | walk | 0.232 (0.206-0.274) n=21 | 0.227 (0.182-0.266) n=21 | 0.23 (0.202-0.268) n=21 | 0.31 (0.28-0.385) n=21 | 0.23 (0.202-0.268) n=21 | 0.303 (0.00598-0.83) n=21 | 0.303 (0.00598-0.83) n=21 |
| real | 1 | cursor | 0.226 (0.196-0.265) n=21 | 0.223 (0.182-0.265) n=21 | 0.225 (0.2-0.266) n=21 | 0.288 (0.259-0.37) n=21 | 0.225 (0.2-0.266) n=21 | 0.44 (0.0136-0.858) n=21 | 0.44 (0.0136-0.858) n=21 |
| real | all | parse | 0.283 (0.109-0.338) n=21 | 0.294 (0.173-0.339) n=21 | 0.285 (0.0952-0.351) n=21 | 0.388 (0.327-0.452) n=21 | 0.301 (0.2-0.343) n=21 | 0.446 (0.00858-0.979) n=21 | 0.446 (0.00858-0.979) n=21 |
| real | all | walk | 0.219 (0.0991-0.277) n=21 | 0.237 (0.164-0.282) n=21 | 0.221 (0.099-0.278) n=21 | 0.307 (0.279-0.383) n=21 | 0.243 (0.19-0.286) n=21 | 0.406 (0.00842-0.885) n=21 | 0.406 (0.00842-0.885) n=21 |
| real | all | cursor | 0.21 (0.0991-0.268) n=21 | 0.229 (0.171-0.271) n=21 | 0.214 (0.0987-0.272) n=21 | 0.286 (0.258-0.369) n=21 | 0.234 (0.193-0.275) n=21 | 0.483 (0.0104-0.939) n=21 | 0.483 (0.0104-0.939) n=21 |
| realerr | 1 | parse | 0.277 (0.198-0.336) n=13 | 0.27 (0.174-0.341) n=13 | 0.275 (0.198-0.34) n=13 | 0.361 (0.206-0.453) n=13 | 0.273 (0.19-0.341) n=13 | 0.66 (0.0318-1.01) n=13 | 0.661 (0.0318-1.01) n=13 |
| realerr | 1 | walk | 0.236 (0.184-0.277) n=13 | 0.228 (0.161-0.267) n=13 | 0.234 (0.185-0.27) n=13 | 0.304 (0.211-0.355) n=13 | 0.235 (0.185-0.271) n=13 | 0.305 (0.0152-0.704) n=13 | 0.305 (0.0152-0.705) n=13 |
| realerr | 1 | cursor | 0.233 (0.183-0.27) n=13 | 0.222 (0.156-0.262) n=13 | 0.227 (0.181-0.264) n=13 | 0.283 (0.191-0.326) n=13 | 0.228 (0.181-0.264) n=13 | 0.33 (0.0132-0.692) n=13 | 0.33 (0.0132-0.693) n=13 |
| realerr | all | parse | 0.263 (0.0713-0.379) n=13 | 0.273 (0.124-0.362) n=13 | 0.261 (0.0602-0.372) n=13 | 0.361 (0.208-0.454) n=13 | 0.284 (0.163-0.364) n=13 | 0.526 (0.0197-0.952) n=13 | 0.526 (0.0197-0.952) n=13 |
| realerr | all | walk | 0.216 (0.0712-0.311) n=13 | 0.232 (0.122-0.295) n=13 | 0.22 (0.0616-0.315) n=13 | 0.303 (0.212-0.355) n=13 | 0.242 (0.161-0.299) n=13 | 0.472 (0.0196-0.901) n=13 | 0.472 (0.0196-0.901) n=13 |
| realerr | all | cursor | 0.211 (0.0738-0.297) n=13 | 0.227 (0.125-0.277) n=13 | 0.212 (0.0633-0.288) n=13 | 0.283 (0.192-0.328) n=13 | 0.236 (0.162-0.281) n=13 | 0.485 (0.0199-0.895) n=13 | 0.485 (0.0199-0.896) n=13 |
| broken | 1 | parse | 0.248 (0.153-0.334) n=21 | 0.247 (0.151-0.336) n=21 | 0.249 (0.152-0.338) n=21 | 0.325 (0.16-0.435) n=21 | 0.249 (0.151-0.338) n=21 | 0.897 (0.141-1.25) n=21 | 0.897 (0.141-1.25) n=21 |
| broken | 1 | walk | 0.214 (0.159-0.292) n=21 | 0.211 (0.157-0.292) n=21 | 0.215 (0.158-0.294) n=21 | 0.31 (0.195-0.392) n=21 | 0.215 (0.158-0.297) n=21 | 0.35 (0.0538-0.631) n=21 | 0.35 (0.0538-0.631) n=21 |
| broken | 1 | cursor | 0.214 (0.141-0.289) n=21 | 0.213 (0.146-0.292) n=21 | 0.214 (0.147-0.293) n=21 | 0.267 (0.155-0.379) n=21 | 0.215 (0.147-0.296) n=21 | 0.47 (0.0634-0.915) n=21 | 0.47 (0.0634-0.915) n=21 |
| broken | all | parse | 0.239 (0.127-0.425) n=21 | 0.258 (0.161-0.348) n=21 | 0.242 (0.129-0.423) n=21 | 0.325 (0.162-0.435) n=21 | 0.263 (0.157-0.351) n=21 | 0.425 (0.0583-0.842) n=21 | 0.425 (0.0583-0.842) n=21 |
| broken | all | walk | 0.216 (0.145-0.405) n=21 | 0.222 (0.164-0.289) n=21 | 0.22 (0.149-0.411) n=21 | 0.31 (0.197-0.391) n=21 | 0.227 (0.16-0.295) n=21 | 0.448 (0.0572-0.835) n=21 | 0.448 (0.0572-0.835) n=21 |
| broken | all | cursor | 0.209 (0.122-0.398) n=21 | 0.22 (0.153-0.285) n=21 | 0.215 (0.126-0.403) n=21 | 0.266 (0.157-0.377) n=21 | 0.224 (0.149-0.288) n=21 | 0.476 (0.0614-0.961) n=21 | 0.476 (0.0614-0.962) n=21 |

### gositter / cgo: traversal alone

| set | j | what | instructions min | cycles min | CPU user+sys min |
|---|---|---|---|---|---|
| real | 1 | walk minus parse | 0.149 (0.121-0.196) n=21 | 0.117 (0.0947-0.141) n=21 | 0.117 (0.0935-0.16) n=21 |
| real | 1 | cursor minus parse | 0.139 (0.111-0.187) n=21 | 0.119 (0.0941-0.152) n=21 | 0.119 (0.0937-0.153) n=21 |
| real | all | walk minus parse | 0.146 (0.116-0.195) n=21 | 0.117 (0.0846-0.156) n=21 | 0.112 (0.0816-0.154) n=21 |
| real | all | cursor minus parse | 0.137 (0.111-0.185) n=21 | 0.12 (0.0826-0.166) n=21 | 0.12 (0.0808-0.165) n=21 |
| realerr | 1 | walk minus parse | 0.162 (0.129-0.232) n=13 | 0.13 (0.111-0.158) n=13 | 0.127 (0.106-0.143) n=13 |
| realerr | 1 | cursor minus parse | 0.137 (0.116-0.166) n=13 | 0.124 (0.103-0.153) n=13 | 0.121 (0.102-0.149) n=13 |
| realerr | all | walk minus parse | 0.159 (0.126-0.23) n=13 | 0.126 (0.094-0.203) n=13 | 0.122 (0.0871-0.204) n=13 |
| realerr | all | cursor minus parse | 0.135 (0.115-0.164) n=13 | 0.109 (0.08-0.132) n=13 | 0.109 (0.061-0.144) n=13 |
| broken | 1 | walk minus parse | 0.234 (0.13-0.402) n=21 | 0.15 (0.117-0.198) n=21 | 0.146 (0.114-0.191) n=21 |
| broken | 1 | cursor minus parse | 0.14 (0.114-0.188) n=21 | 0.123 (0.0916-0.194) n=21 | 0.122 (0.0908-0.196) n=21 |
| broken | all | walk minus parse | 0.235 (0.128-0.401) n=21 | 0.153 (0.082-0.225) n=21 | 0.149 (0.0829-0.238) n=21 |
| broken | all | cursor minus parse | 0.137 (0.113-0.182) n=21 | 0.124 (0.0949-0.175) n=21 | 0.125 (0.0947-0.196) n=21 |

### gositter / cgo: memory in the steady state (41 passes)

| set | j | what | steady peak RSS | steady peak footprint | steady RSS above loaded |
|---|---|---|---|---|---|
| real | 1 | parse | 0.586 (0.0152-0.985) n=21 | 0.587 (0.0152-0.985) n=21 | 0.462 (0.0108-1.05) n=21 |
| real | all | parse | 0.41 (0.256-0.812) n=20 | 0.41 (0.256-0.812) n=20 | 0.367 (0.227-0.797) n=20 |
| realerr | 1 | parse | 0.586 (0.0225-1.01) n=13 | 0.586 (0.0225-1.01) n=13 | 0.552 (0.0184-1.07) n=13 |
| realerr | all | parse | 0.471 (0.3-0.771) n=12 | 0.471 (0.3-0.771) n=12 | 0.442 (0.273-0.739) n=12 |
| broken | 1 | parse | 0.773 (0.0919-1.07) n=21 | 0.773 (0.092-1.07) n=21 | 0.756 (0.0704-1.13) n=21 |
| broken | all | parse | 0.287 (0.0468-0.686) n=20 | 0.287 (0.0468-0.686) n=20 | 0.272 (0.0425-0.64) n=20 |

C -O3 divided by C -O2, geometric mean (min-max) over the configs that ran both: wall 1.01 (0.89-1.15, n=220); CPU user+sys 1.01 (0.931-1.13, n=220); instructions 0.978 (0.897-0.991, n=220); cycles 1.01 (0.926-1.13, n=220).

C -O2 built by clang divided by C -O2, geometric mean (min-max) over the configs that ran both: wall 0.992 (0.76-1.25, n=220); CPU user+sys 0.98 (0.831-1.22, n=220); instructions 0.988 (0.912-1.06, n=220); cycles 0.981 (0.829-1.23, n=220).

C-pic divided by C (the -fPIC flag), geometric mean (min-max) over the configs that ran both: wall 1.1 (0.892-1.3, n=220); CPU user+sys 1.08 (1-1.21, n=220); instructions 1.04 (1.01-1.06, n=220); cycles 1.08 (1.03-1.21, n=220).

cgo divided by C-pic (the binding), geometric mean (min-max) over the configs that ran both: wall 1.75 (1.09-7.2, n=220); CPU user+sys 1.77 (1.18-4.19, n=220); instructions 1.74 (1.15-2.64, n=220); cycles 1.74 (1.16-3.26, n=220).

cgo divided by C (both), geometric mean (min-max) over the configs that ran both: wall 1.93 (1.17-9.26, n=220); CPU user+sys 1.92 (1.22-4.3, n=220); instructions 1.81 (1.16-2.81, n=220); cycles 1.88 (1.22-3.41, n=220).

## bash

### bash: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 152 | 790 | 410 | 403 | 411 | 389 | 439 | 0.192 | 0.37 | 0.376 | 0.369 | 0.389 | 0.345 | 157 | 808 | 417 | 406 | 391 | 441 | 0.194 | 0.376 | 0.386 | 0.401 | 0.355 | 152-178 | 410-436 | 1.01 | 1.01 | 1 | 18.4 |
| broken | cursor | all | 46.3 | 250 | 103 | 105 | 106 | 103 | 111 | 0.185 | 0.45 | 0.44 | 0.438 | 0.45 | 0.416 | 48.8 | 260 | 108 | 112 | 104 | 113 | 0.188 | 0.45 | 0.436 | 0.467 | 0.43 | 46.3-54 | 103-114 | 4.84 * | 4.49 * | 5.47 * | 60.2 |
| broken | parse | 1 | 125 | 552 | 377 | 372 | 376 | 327 | 404 | 0.226 | 0.331 | 0.335 | 0.331 | 0.381 | 0.308 | 125 | 571 | 386 | 381 | 331 | 406 | 0.22 | 0.325 | 0.329 | 0.379 | 0.309 | 125-126 | 377-404 | 1 | 1.01 | 1 | 22.4 |
| broken | parse | all | 29.8 | 122 | 103 | 101 | 100 | 79.1 | 106 | 0.244 | 0.291 | 0.296 | 0.297 | 0.377 | 0.282 | 32.5 | 133 | 108 | 106 | 82.5 | 109 | 0.245 | 0.302 | 0.306 | 0.394 | 0.297 | 29.8-36 | 103-116 | 5.84 * | 6.06 * | 5.01 * | 93.6 |
| broken | walk | 1 | 158 | 760 | 400 | 393 | n/a | n/a | n/a | 0.208 | 0.394 | 0.401 | n/a | n/a | n/a | 164 | 779 | 404 | 396 | n/a | n/a | 0.211 | 0.406 | 0.414 | n/a | n/a | 158-169 | 400-418 | 1 | 1.02 | 1 | 17.7 |
| broken | walk | all | 52.6 | 246 | 101 | 106 | n/a | n/a | n/a | 0.214 | 0.521 | 0.497 | n/a | n/a | n/a | 55.4 | 254 | 109 | 111 | n/a | n/a | 0.218 | 0.51 | 0.501 | n/a | n/a | 52.6-58.6 | 101-117 | 4.46 * | 4.5 * | 5.35 * | 53.1 |
| real | cursor | 1 | 69.3 | 287 | 130 | 125 | 135 | 140 | 142 | 0.241 | 0.531 | 0.553 | 0.513 | 0.495 | 0.488 | 71 | 294 | 131 | 127 | 141 | 143 | 0.242 | 0.542 | 0.561 | 0.503 | 0.496 | 69.3-74.7 | 130-137 | 1 | 1.01 | 1 | 34.4 |
| real | cursor | all | 9.51 | 46.8 | 19.4 | 15.5 | 19 | 21.7 | 19.8 | 0.203 | 0.49 | 0.614 | 0.5 | 0.438 | 0.48 | 11.2 | 48.6 | 20.1 | 19.9 | 22.3 | 20.7 | 0.23 | 0.557 | 0.563 | 0.501 | 0.541 | 9.51-14.2 | 19.4-22.2 | 10.7 * | 10.8 * | 10.9 * | 251 |
| real | parse | 1 | 57.1 | 190 | 113 | 109 | 115 | 111 | 125 | 0.3 | 0.504 | 0.523 | 0.496 | 0.514 | 0.457 | 58.2 | 194 | 116 | 111 | 111 | 126 | 0.301 | 0.504 | 0.523 | 0.523 | 0.462 | 57.1-60.3 | 113-120 | 1 | 1.01 | 1 | 41.8 |
| real | parse | all | 8.09 | 27.5 | 16.6 | 16.3 | 14.8 | 14.9 | 14.8 | 0.294 | 0.488 | 0.496 | 0.547 | 0.542 | 0.547 | 9.48 | 29.2 | 17.4 | 17.4 | 16.5 | 18.5 | 0.325 | 0.546 | 0.544 | 0.574 | 0.512 | 8.09-11.4 | 16.6-19.1 | 10.6 * | 11.2 * | 11.1 * | 295 |
| real | walk | 1 | 68.4 | 278 | 123 | 119 | n/a | n/a | n/a | 0.246 | 0.557 | 0.574 | n/a | n/a | n/a | 70.5 | 283 | 125 | 121 | n/a | n/a | 0.249 | 0.566 | 0.583 | n/a | n/a | 68.4-76.3 | 123-125 | 1 | 1.01 | 1 | 34.9 |
| real | walk | all | 10.3 | 43.4 | 17.9 | 17.8 | n/a | n/a | n/a | 0.237 | 0.573 | 0.578 | n/a | n/a | n/a | 11 | 45.1 | 19.1 | 18.8 | n/a | n/a | 0.243 | 0.576 | 0.585 | n/a | n/a | 10.3-14.6 | 17.9-22.8 | 10.5 * | 10.9 * | 11 * | 232 |
| realerr | cursor | 1 | 22.6 | 90.8 | 44 | 43 | 44.8 | 46.7 | 48.1 | 0.249 | 0.515 | 0.526 | 0.505 | 0.485 | 0.471 | 22.9 | 94.8 | 45.3 | 43.7 | 46.9 | 48.6 | 0.242 | 0.506 | 0.525 | 0.489 | 0.471 | 22.6-26.6 | 44-47.3 | 1 | 1.01 | 1 | 30.5 |
| realerr | cursor | all | 6.48 | 24.7 | 11.2 | 11.2 | 11.6 | 13.1 | 12.6 | 0.263 | 0.581 | 0.579 | 0.56 | 0.496 | 0.516 | 6.94 | 26.4 | 12.2 | 12.2 | 13.3 | 13 | 0.263 | 0.569 | 0.569 | 0.52 | 0.532 | 6.48-7.72 | 11.2-14.6 | 4.88 * | 5.23 * | 5.07 * | 106 |
| realerr | parse | 1 | 18.9 | 60.8 | 39.6 | 38.2 | 39.6 | 37.9 | 43.9 | 0.311 | 0.479 | 0.496 | 0.478 | 0.5 | 0.432 | 19.2 | 63 | 40.8 | 40.1 | 38.1 | 47.1 | 0.304 | 0.47 | 0.477 | 0.504 | 0.407 | 18.9-22.6 | 39.6-45.4 | 1 | 1.01 | 1 | 36.4 |
| realerr | parse | all | 5.46 | 14.5 | 10.3 | 10.4 | 10.6 | 10.3 | 11.5 | 0.378 | 0.532 | 0.528 | 0.513 | 0.531 | 0.474 | 5.93 | 16.4 | 11 | 10.9 | 10.8 | 11.9 | 0.362 | 0.54 | 0.543 | 0.547 | 0.496 | 5.46-6.71 | 10.3-12 | 4.85 * | 5.51 * | 5.21 * | 126 |
| realerr | walk | 1 | 22.5 | 88.2 | 42.5 | 41.3 | n/a | n/a | n/a | 0.255 | 0.53 | 0.545 | n/a | n/a | n/a | 23 | 91.3 | 43.9 | 41.8 | n/a | n/a | 0.252 | 0.523 | 0.549 | n/a | n/a | 22.5-25.1 | 42.5-46.2 | 1 | 1.01 | 1 | 30.6 |
| realerr | walk | all | 6.4 | 23.4 | 11.1 | 10.9 | n/a | n/a | n/a | 0.273 | 0.579 | 0.588 | n/a | n/a | n/a | 6.91 | 24.8 | 11.9 | 11.9 | n/a | n/a | 0.278 | 0.581 | 0.58 | n/a | n/a | 6.4-8.89 | 11.1-13.2 | 4.79 * | 5.2 * | 5.12 * | 108 |

### bash: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 153 | 798 | 410 | 403 | 411 | 389 | 439 | 0.192 | 0.373 | 0.38 | 0.372 | 0.393 | 0.348 | 159 | 816 | 417 | 406 | 391 | 441 | 0.195 | 0.383 | 0.393 | 0.408 | 0.362 | 153-179 | 410-436 |
| broken | cursor | all | 230 | 1150 | 576 | 562 | 584 | 557 | 605 | 0.199 | 0.399 | 0.408 | 0.393 | 0.413 | 0.38 | 236 | 1160 | 589 | 593 | 563 | 634 | 0.202 | 0.401 | 0.398 | 0.419 | 0.372 | 230-253 | 576-611 |
| broken | parse | 1 | 125 | 555 | 377 | 372 | 376 | 327 | 404 | 0.225 | 0.331 | 0.336 | 0.332 | 0.381 | 0.308 | 126 | 574 | 386 | 381 | 331 | 406 | 0.219 | 0.326 | 0.329 | 0.38 | 0.309 | 125-126 | 377-403 |
| broken | parse | all | 181 | 778 | 518 | 519 | 516 | 501 | 563 | 0.233 | 0.35 | 0.349 | 0.351 | 0.362 | 0.322 | 189 | 810 | 538 | 537 | 509 | 568 | 0.233 | 0.351 | 0.351 | 0.371 | 0.332 | 181-198 | 518-566 |
| broken | walk | 1 | 158 | 780 | 400 | 393 | n/a | n/a | n/a | 0.202 | 0.394 | 0.402 | n/a | n/a | n/a | 164 | 793 | 404 | 396 | n/a | n/a | 0.207 | 0.407 | 0.415 | n/a | n/a | 158-169 | 400-418 |
| broken | walk | all | 242 | 1120 | 559 | 561 | n/a | n/a | n/a | 0.216 | 0.433 | 0.431 | n/a | n/a | n/a | 247 | 1150 | 584 | 585 | n/a | n/a | 0.215 | 0.424 | 0.423 | n/a | n/a | 242-255 | 559-597 |
| real | cursor | 1 | 69.4 | 291 | 130 | 125 | 135 | 140 | 142 | 0.239 | 0.532 | 0.554 | 0.514 | 0.495 | 0.488 | 71.1 | 298 | 131 | 127 | 141 | 143 | 0.238 | 0.543 | 0.562 | 0.503 | 0.497 | 69.4-75.3 | 130-137 |
| real | cursor | all | 120 | 521 | 217 | 203 | 213 | 233 | 225 | 0.231 | 0.555 | 0.594 | 0.566 | 0.517 | 0.536 | 123 | 529 | 221 | 218 | 234 | 236 | 0.232 | 0.554 | 0.562 | 0.524 | 0.52 | 120-129 | 217-229 |
| real | parse | 1 | 57.1 | 192 | 113 | 109 | 115 | 111 | 125 | 0.298 | 0.505 | 0.524 | 0.496 | 0.515 | 0.457 | 58.3 | 195 | 116 | 111 | 111 | 126 | 0.299 | 0.504 | 0.524 | 0.524 | 0.463 | 57.1-60.3 | 113-120 |
| real | parse | all | 96.9 | 318 | 191 | 188 | 186 | 184 | 202 | 0.305 | 0.508 | 0.515 | 0.521 | 0.527 | 0.479 | 101 | 327 | 193 | 194 | 185 | 205 | 0.31 | 0.524 | 0.522 | 0.547 | 0.496 | 96.9-104 | 191-204 |
| real | walk | 1 | 68.5 | 282 | 123 | 119 | n/a | n/a | n/a | 0.243 | 0.557 | 0.574 | n/a | n/a | n/a | 70.6 | 286 | 124 | 121 | n/a | n/a | 0.246 | 0.567 | 0.584 | n/a | n/a | 68.5-76.4 | 123-125 |
| real | walk | all | 111 | 476 | 207 | 202 | n/a | n/a | n/a | 0.234 | 0.539 | 0.551 | n/a | n/a | n/a | 120 | 490 | 212 | 209 | n/a | n/a | 0.246 | 0.569 | 0.575 | n/a | n/a | 111-126 | 207-216 |
| realerr | cursor | 1 | 22.7 | 91.9 | 44 | 43 | 44.8 | 46.7 | 48.1 | 0.247 | 0.516 | 0.527 | 0.507 | 0.486 | 0.472 | 23 | 95.7 | 45.3 | 43.7 | 46.9 | 48.6 | 0.24 | 0.507 | 0.526 | 0.49 | 0.472 | 22.7-26.6 | 44-47.3 |
| realerr | cursor | all | 32.4 | 133 | 58.3 | 60.1 | 59.5 | 64.1 | 63.2 | 0.243 | 0.556 | 0.54 | 0.545 | 0.505 | 0.513 | 33.7 | 138 | 62.9 | 62.3 | 64.9 | 67.1 | 0.244 | 0.536 | 0.542 | 0.52 | 0.503 | 32.4-37.3 | 58.3-69.7 |
| realerr | parse | 1 | 19 | 61.6 | 39.6 | 38.2 | 39.6 | 37.9 | 43.9 | 0.308 | 0.48 | 0.497 | 0.479 | 0.501 | 0.432 | 19.2 | 63.6 | 40.8 | 40.1 | 38.1 | 47 | 0.302 | 0.471 | 0.478 | 0.504 | 0.408 | 19-22.6 | 39.6-45.4 |
| realerr | parse | all | 27.5 | 85.7 | 55.4 | 54.4 | 54 | 52.1 | 60 | 0.321 | 0.496 | 0.505 | 0.509 | 0.527 | 0.457 | 28.7 | 90 | 57.6 | 56.5 | 54.2 | 60.8 | 0.318 | 0.497 | 0.507 | 0.529 | 0.471 | 27.5-31.6 | 55.4-58.9 |
| realerr | walk | 1 | 22.6 | 89.2 | 42.5 | 41.3 | n/a | n/a | n/a | 0.253 | 0.531 | 0.546 | n/a | n/a | n/a | 23 | 92.3 | 43.9 | 41.8 | n/a | n/a | 0.249 | 0.523 | 0.549 | n/a | n/a | 22.6-25.1 | 42.5-46.1 |
| realerr | walk | all | 32.2 | 128 | 59.3 | 58.1 | n/a | n/a | n/a | 0.253 | 0.544 | 0.555 | n/a | n/a | n/a | 33.9 | 131 | 61.1 | 60.1 | n/a | n/a | 0.259 | 0.555 | 0.564 | n/a | n/a | 32.2-36.3 | 59.3-62.6 |

### bash: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 3520 | 14800 | 7770 | 7680 | 7670 | 7690 | 7980 | 0.238 | 0.453 | 0.459 | 0.459 | 0.458 | 0.442 | 3530 | 14800 | 7770 | 7680 | 7690 | 7980 | 0.238 | 0.454 | 0.459 | 0.458 | 0.442 | 3520-3530 | 7770-7770 |
| broken | cursor | all | 3540 | 14800 | 7780 | 7680 | 7680 | 7700 | 7990 | 0.238 | 0.455 | 0.461 | 0.461 | 0.46 | 0.443 | 3560 | 14900 | 7790 | 7690 | 7710 | 8000 | 0.239 | 0.457 | 0.463 | 0.462 | 0.445 | 3540-3630 | 7780-7790 |
| broken | parse | 1 | 2880 | 9780 | 7070 | 6970 | 6950 | 6920 | 7240 | 0.295 | 0.408 | 0.413 | 0.415 | 0.417 | 0.398 | 2880 | 9780 | 7070 | 6980 | 6920 | 7240 | 0.295 | 0.408 | 0.413 | 0.417 | 0.398 | 2880-2880 | 7070-7070 |
| broken | parse | all | 2900 | 9750 | 7070 | 6980 | 6960 | 6930 | 7240 | 0.297 | 0.41 | 0.415 | 0.416 | 0.418 | 0.4 | 2910 | 9780 | 7080 | 6990 | 6930 | 7250 | 0.298 | 0.411 | 0.417 | 0.42 | 0.402 | 2900-2930 | 7070-7110 |
| broken | walk | 1 | 3610 | 13800 | 7450 | 7360 | n/a | n/a | n/a | 0.262 | 0.485 | 0.491 | n/a | n/a | n/a | 3640 | 13900 | 7450 | 7360 | n/a | n/a | 0.261 | 0.488 | 0.495 | n/a | n/a | 3610-3660 | 7450-7450 |
| broken | walk | all | 3720 | 14000 | 7460 | 7360 | n/a | n/a | n/a | 0.266 | 0.499 | 0.506 | n/a | n/a | n/a | 3750 | 14100 | 7460 | 7370 | n/a | n/a | 0.267 | 0.503 | 0.51 | n/a | n/a | 3720-3780 | 7460-7470 |
| real | cursor | 1 | 1500 | 5060 | 2210 | 2130 | 2180 | 2270 | 2310 | 0.296 | 0.678 | 0.701 | 0.685 | 0.658 | 0.647 | 1500 | 5060 | 2210 | 2140 | 2270 | 2310 | 0.295 | 0.677 | 0.7 | 0.658 | 0.646 | 1500-1500 | 2210-2210 |
| real | cursor | all | 1500 | 5090 | 2200 | 2130 | 2180 | 2280 | 2310 | 0.294 | 0.68 | 0.702 | 0.687 | 0.657 | 0.647 | 1500 | 5120 | 2210 | 2140 | 2280 | 2310 | 0.292 | 0.677 | 0.7 | 0.657 | 0.647 | 1500-1550 | 2200-2220 |
| real | parse | 1 | 1220 | 2980 | 1880 | 1810 | 1850 | 1910 | 1970 | 0.408 | 0.647 | 0.674 | 0.659 | 0.637 | 0.619 | 1220 | 2980 | 1880 | 1810 | 1910 | 1970 | 0.408 | 0.646 | 0.672 | 0.636 | 0.617 | 1220-1220 | 1880-1890 |
| real | parse | all | 1220 | 2940 | 1880 | 1810 | 1850 | 1920 | 1970 | 0.415 | 0.647 | 0.674 | 0.658 | 0.635 | 0.618 | 1220 | 2990 | 1890 | 1820 | 1920 | 1980 | 0.407 | 0.644 | 0.671 | 0.634 | 0.616 | 1220-1220 | 1880-1900 |
| real | walk | 1 | 1470 | 4520 | 2060 | 1990 | n/a | n/a | n/a | 0.325 | 0.712 | 0.738 | n/a | n/a | n/a | 1470 | 4530 | 2070 | 1990 | n/a | n/a | 0.324 | 0.71 | 0.736 | n/a | n/a | 1470-1470 | 2060-2070 |
| real | walk | all | 1470 | 4560 | 2070 | 1990 | n/a | n/a | n/a | 0.322 | 0.71 | 0.739 | n/a | n/a | n/a | 1470 | 4590 | 2080 | 2000 | n/a | n/a | 0.32 | 0.708 | 0.734 | n/a | n/a | 1470-1490 | 2070-2080 |
| realerr | cursor | 1 | 464 | 1580 | 733 | 712 | 724 | 750 | 766 | 0.293 | 0.633 | 0.652 | 0.641 | 0.619 | 0.606 | 464 | 1590 | 734 | 713 | 750 | 767 | 0.293 | 0.633 | 0.652 | 0.619 | 0.606 | 464-465 | 733-735 |
| realerr | cursor | all | 470 | 1590 | 726 | 712 | 716 | 748 | 764 | 0.296 | 0.648 | 0.66 | 0.657 | 0.628 | 0.615 | 473 | 1590 | 733 | 714 | 751 | 767 | 0.297 | 0.645 | 0.663 | 0.63 | 0.616 | 470-488 | 726-743 |
| realerr | parse | 1 | 383 | 959 | 639 | 617 | 628 | 645 | 667 | 0.399 | 0.6 | 0.62 | 0.61 | 0.594 | 0.574 | 383 | 960 | 639 | 618 | 645 | 667 | 0.399 | 0.599 | 0.62 | 0.593 | 0.574 | 383-383 | 639-640 |
| realerr | parse | all | 388 | 956 | 636 | 615 | 624 | 642 | 665 | 0.406 | 0.61 | 0.631 | 0.622 | 0.604 | 0.583 | 392 | 961 | 638 | 619 | 644 | 668 | 0.408 | 0.614 | 0.633 | 0.608 | 0.586 | 388-395 | 636-644 |
| realerr | walk | 1 | 460 | 1430 | 698 | 676 | n/a | n/a | n/a | 0.321 | 0.66 | 0.68 | n/a | n/a | n/a | 460 | 1440 | 698 | 677 | n/a | n/a | 0.32 | 0.659 | 0.68 | n/a | n/a | 460-460 | 698-699 |
| realerr | walk | all | 465 | 1430 | 691 | 672 | n/a | n/a | n/a | 0.324 | 0.674 | 0.693 | n/a | n/a | n/a | 469 | 1440 | 700 | 676 | n/a | n/a | 0.326 | 0.671 | 0.694 | n/a | n/a | 465-472 | 691-706 |

### bash: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 793 | 4080 | 2110 | 2070 | 2120 | 2010 | 2270 | 0.195 | 0.376 | 0.382 | 0.375 | 0.396 | 0.35 | 816 | 4160 | 2140 | 2090 | 2010 | 2270 | 0.196 | 0.381 | 0.391 | 0.406 | 0.359 | 793-914 | 2110-2230 |
| broken | cursor | all | 1120 | 5420 | 2760 | 2700 | 2770 | 2650 | 2900 | 0.207 | 0.407 | 0.416 | 0.404 | 0.423 | 0.387 | 1150 | 5470 | 2800 | 2810 | 2650 | 3010 | 0.21 | 0.409 | 0.408 | 0.432 | 0.381 | 1120-1220 | 2760-2890 |
| broken | parse | 1 | 651 | 2860 | 1940 | 1920 | 1940 | 1680 | 2080 | 0.228 | 0.335 | 0.339 | 0.336 | 0.387 | 0.312 | 654 | 2950 | 1990 | 1970 | 1700 | 2090 | 0.222 | 0.329 | 0.333 | 0.384 | 0.312 | 651-659 | 1940-2070 |
| broken | parse | all | 891 | 3650 | 2490 | 2490 | 2470 | 2390 | 2700 | 0.244 | 0.358 | 0.358 | 0.36 | 0.372 | 0.329 | 929 | 3840 | 2590 | 2580 | 2400 | 2750 | 0.242 | 0.358 | 0.361 | 0.386 | 0.338 | 891-971 | 2490-2710 |
| broken | walk | 1 | 823 | 3970 | 2070 | 2020 | n/a | n/a | n/a | 0.207 | 0.399 | 0.407 | n/a | n/a | n/a | 852 | 4020 | 2090 | 2050 | n/a | n/a | 0.212 | 0.409 | 0.417 | n/a | n/a | 823-881 | 2070-2150 |
| broken | walk | all | 1170 | 5220 | 2670 | 2700 | n/a | n/a | n/a | 0.225 | 0.439 | 0.434 | n/a | n/a | n/a | 1210 | 5350 | 2750 | 2780 | n/a | n/a | 0.225 | 0.438 | 0.433 | n/a | n/a | 1170-1240 | 2670-2840 |
| real | cursor | 1 | 361 | 1500 | 681 | 654 | 705 | 731 | 742 | 0.241 | 0.53 | 0.553 | 0.512 | 0.494 | 0.487 | 370 | 1530 | 683 | 660 | 737 | 747 | 0.241 | 0.541 | 0.56 | 0.502 | 0.495 | 361-391 | 681-712 |
| real | cursor | all | 566 | 2340 | 1020 | 971 | 993 | 1110 | 1050 | 0.242 | 0.556 | 0.583 | 0.57 | 0.511 | 0.538 | 585 | 2370 | 1030 | 1020 | 1120 | 1100 | 0.247 | 0.567 | 0.572 | 0.521 | 0.534 | 566-593 | 1020-1060 |
| real | parse | 1 | 298 | 993 | 591 | 570 | 601 | 579 | 652 | 0.3 | 0.505 | 0.523 | 0.496 | 0.515 | 0.457 | 304 | 1010 | 603 | 581 | 580 | 657 | 0.3 | 0.504 | 0.523 | 0.524 | 0.463 | 298-315 | 591-625 |
| real | parse | all | 462 | 1450 | 892 | 889 | 858 | 863 | 938 | 0.318 | 0.518 | 0.52 | 0.538 | 0.535 | 0.492 | 484 | 1490 | 905 | 909 | 874 | 954 | 0.324 | 0.535 | 0.532 | 0.554 | 0.507 | 462-491 | 892-942 |
| real | walk | 1 | 357 | 1450 | 642 | 623 | n/a | n/a | n/a | 0.246 | 0.557 | 0.574 | n/a | n/a | n/a | 368 | 1480 | 650 | 631 | n/a | n/a | 0.249 | 0.567 | 0.583 | n/a | n/a | 357-396 | 642-654 |
| real | walk | all | 549 | 2170 | 964 | 954 | n/a | n/a | n/a | 0.253 | 0.569 | 0.575 | n/a | n/a | n/a | 570 | 2200 | 989 | 987 | n/a | n/a | 0.26 | 0.577 | 0.578 | n/a | n/a | 549-583 | 964-1010 |
| realerr | cursor | 1 | 118 | 476 | 229 | 224 | 233 | 243 | 251 | 0.248 | 0.515 | 0.526 | 0.506 | 0.485 | 0.471 | 120 | 494 | 237 | 228 | 245 | 254 | 0.242 | 0.506 | 0.525 | 0.489 | 0.472 | 118-138 | 229-247 |
| realerr | cursor | all | 165 | 657 | 301 | 309 | 301 | 318 | 325 | 0.251 | 0.548 | 0.535 | 0.548 | 0.519 | 0.508 | 171 | 676 | 321 | 315 | 330 | 338 | 0.253 | 0.534 | 0.543 | 0.518 | 0.506 | 165-184 | 301-338 |
| realerr | parse | 1 | 98.9 | 318 | 206 | 199 | 206 | 198 | 229 | 0.311 | 0.479 | 0.496 | 0.479 | 0.5 | 0.432 | 99.8 | 329 | 213 | 209 | 198 | 245 | 0.303 | 0.469 | 0.477 | 0.503 | 0.407 | 98.9-118 | 206-236 |
| realerr | parse | all | 135 | 430 | 278 | 278 | 274 | 263 | 302 | 0.315 | 0.487 | 0.487 | 0.494 | 0.514 | 0.448 | 146 | 449 | 290 | 284 | 268 | 305 | 0.324 | 0.501 | 0.513 | 0.543 | 0.478 | 135-152 | 278-300 |
| realerr | walk | 1 | 118 | 459 | 222 | 216 | n/a | n/a | n/a | 0.256 | 0.53 | 0.545 | n/a | n/a | n/a | 120 | 474 | 229 | 218 | n/a | n/a | 0.253 | 0.523 | 0.549 | n/a | n/a | 118-131 | 222-238 |
| realerr | walk | all | 163 | 628 | 300 | 295 | n/a | n/a | n/a | 0.26 | 0.543 | 0.553 | n/a | n/a | n/a | 169 | 643 | 309 | 305 | n/a | n/a | 0.263 | 0.547 | 0.553 | n/a | n/a | 163-182 | 300-319 |

### bash: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 641 | 5020 | 702 | 702 | 722 | 774 | 742 | 0.128 | 0.913 | 0.913 | 0.888 | 0.827 | 0.864 |
| broken | cursor minus parse | 1 | cycles M | 143 | 1220 | 167 | 156 | 180 | 322 | 181 | 0.117 | 0.854 | 0.915 | 0.794 | 0.442 | 0.786 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 28.2 | 243 | 33.3 | 31 | 34.9 | 62.1 | 35 | 0.116 | 0.849 | 0.91 | 0.808 | 0.455 | 0.807 |
| broken | cursor minus parse | all | instructions M | 643 | 5090 | 710 | 702 | 722 | 772 | 744 | 0.126 | 0.906 | 0.916 | 0.891 | 0.834 | 0.865 |
| broken | cursor minus parse | all | cycles M | 230 | 1770 | 265 | 205 | 300 | 253 | 193 | 0.129 | 0.867 | 1.12 | 0.767 | 0.907 | 1.19 |
| broken | cursor minus parse | all | CPU user+sys ms | 48.5 | 377 | 57.6 | 43.7 | 67.8 | 56.2 | 42.4 | 0.129 | 0.842 | 1.11 | 0.716 | 0.864 | 1.14 |
| broken | walk minus parse | 1 | instructions M | 732 | 4030 | 381 | 381 | n/a | n/a | n/a | 0.182 | 1.92 | 1.92 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 172 | 1110 | 122 | 106 | n/a | n/a | n/a | 0.155 | 1.41 | 1.63 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 33.2 | 225 | 23.9 | 21.2 | n/a | n/a | n/a | 0.147 | 1.39 | 1.57 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 828 | 4240 | 387 | 378 | n/a | n/a | n/a | 0.195 | 2.14 | 2.19 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 283 | 1570 | 183 | 212 | n/a | n/a | n/a | 0.18 | 1.55 | 1.33 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 60.7 | 341 | 40.6 | 42.5 | n/a | n/a | n/a | 0.178 | 1.49 | 1.43 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 279 | 2080 | 329 | 329 | 335 | 363 | 346 | 0.134 | 0.85 | 0.85 | 0.833 | 0.769 | 0.808 |
| real | cursor minus parse | 1 | cycles M | 63.1 | 507 | 90.5 | 84.1 | 104 | 152 | 89.6 | 0.125 | 0.697 | 0.751 | 0.607 | 0.415 | 0.705 |
| real | cursor minus parse | 1 | CPU user+sys ms | 12.2 | 98.7 | 17.3 | 16.1 | 19.9 | 29.1 | 17.2 | 0.124 | 0.707 | 0.759 | 0.615 | 0.42 | 0.713 |
| real | cursor minus parse | all | instructions M | 279 | 2160 | 318 | 325 | 330 | 360 | 344 | 0.129 | 0.879 | 0.86 | 0.847 | 0.777 | 0.812 |
| real | cursor minus parse | all | cycles M | 104 | 886 | 125 | 82.2 | 134 | 244 | 112 | 0.117 | 0.828 | 1.26 | 0.772 | 0.426 | 0.924 |
| real | cursor minus parse | all | CPU user+sys ms | 23.5 | 203 | 26.1 | 14.4 | 26.8 | 49 | 22.4 | 0.116 | 0.898 | 1.63 | 0.874 | 0.479 | 1.05 |
| real | walk minus parse | 1 | instructions M | 252 | 1540 | 185 | 185 | n/a | n/a | n/a | 0.163 | 1.37 | 1.37 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 59.3 | 460 | 51.2 | 53.2 | n/a | n/a | n/a | 0.129 | 1.16 | 1.12 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 11.4 | 89.7 | 9.77 | 10.2 | n/a | n/a | n/a | 0.127 | 1.16 | 1.11 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 252 | 1630 | 186 | 180 | n/a | n/a | n/a | 0.155 | 1.35 | 1.4 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 86.6 | 713 | 71.1 | 65.2 | n/a | n/a | n/a | 0.121 | 1.22 | 1.33 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 14.5 | 158 | 16.2 | 13.9 | n/a | n/a | n/a | 0.0921 | 0.9 | 1.04 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 81.6 | 625 | 94.6 | 94.6 | 96.7 | 105 | 99.8 | 0.13 | 0.862 | 0.862 | 0.843 | 0.776 | 0.817 |
| realerr | cursor minus parse | 1 | cycles M | 19.2 | 158 | 23.1 | 25.2 | 27.2 | 45.9 | 22.1 | 0.121 | 0.83 | 0.763 | 0.705 | 0.419 | 0.868 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 3.71 | 30.3 | 4.41 | 4.81 | 5.14 | 8.8 | 4.23 | 0.123 | 0.842 | 0.771 | 0.722 | 0.422 | 0.878 |
| realerr | cursor minus parse | all | instructions M | 82.1 | 633 | 90.2 | 96.7 | 91.9 | 106 | 98.7 | 0.13 | 0.91 | 0.849 | 0.893 | 0.771 | 0.831 |
| realerr | cursor minus parse | all | cycles M | 29.7 | 226 | 22.9 | 30.7 | 27.1 | 54.8 | 22.7 | 0.131 | 1.3 | 0.967 | 1.1 | 0.542 | 1.31 |
| realerr | cursor minus parse | all | CPU user+sys ms | 4.95 | 47.5 | 2.96 | 5.65 | 5.52 | 12.1 | 3.17 | 0.104 | 1.67 | 0.876 | 0.896 | 0.41 | 1.56 |
| realerr | walk minus parse | 1 | instructions M | 77.4 | 475 | 59 | 59 | n/a | n/a | n/a | 0.163 | 1.31 | 1.31 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 18.7 | 141 | 15.4 | 16.4 | n/a | n/a | n/a | 0.133 | 1.21 | 1.14 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 3.58 | 27.6 | 2.93 | 3.13 | n/a | n/a | n/a | 0.13 | 1.22 | 1.14 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 77.5 | 478 | 55.3 | 57.2 | n/a | n/a | n/a | 0.162 | 1.4 | 1.36 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 27.6 | 198 | 21.9 | 16.6 | n/a | n/a | n/a | 0.14 | 1.26 | 1.66 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 4.77 | 41.9 | 3.91 | 3.67 | n/a | n/a | n/a | 0.114 | 1.22 | 1.3 | n/a | n/a | n/a |

### bash: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 155 | 190 | 69.7 | 0.818 | 2.23 | 155 | 190 | 69.7 | 0.818 | 2.23 | 148 | 181 | 64.8 | 0.817 | 2.28 |
| broken | cursor | all | 227 | 438 | 356 | 0.518 | 0.637 | 227 | 438 | 356 | 0.518 | 0.637 | 219 | 430 | 351 | 0.509 | 0.623 |
| broken | parse | 1 | 62.9 | 80.7 | 49.5 | 0.78 | 1.27 | 62.9 | 80.7 | 49.5 | 0.78 | 1.27 | 55.3 | 71.8 | 44.6 | 0.77 | 1.24 |
| broken | parse | all | 122 | 266 | 261 | 0.457 | 0.466 | 122 | 266 | 261 | 0.457 | 0.466 | 113 | 257 | 256 | 0.441 | 0.443 |
| broken | walk | 1 | 127 | 285 | 81.4 | 0.445 | 1.56 | 127 | 285 | 81.4 | 0.445 | 1.56 | 119 | 276 | 76.5 | 0.432 | 1.56 |
| broken | walk | all | 199 | 384 | 486 | 0.518 | 0.409 | 199 | 384 | 486 | 0.518 | 0.409 | 190 | 375 | 481 | 0.508 | 0.396 |
| real | cursor | 1 | 22.2 | 44.9 | 10.6 | 0.493 | 2.09 | 22.2 | 44.9 | 10.6 | 0.494 | 2.09 | 14.7 | 36.2 | 6.06 | 0.405 | 2.42 |
| real | cursor | all | 51 | 79 | 49.2 | 0.646 | 1.04 | 51.1 | 79 | 49.2 | 0.646 | 1.04 | 42.8 | 70.2 | 44.6 | 0.61 | 0.959 |
| real | parse | 1 | 18.1 | 18.3 | 10.7 | 0.99 | 1.7 | 18.1 | 18.3 | 10.7 | 0.99 | 1.7 | 10.4 | 9.58 | 6.13 | 1.09 | 1.7 |
| real | parse | all | 46.4 | 76.5 | 51.1 | 0.607 | 0.909 | 46.4 | 76.5 | 51.1 | 0.607 | 0.909 | 38.2 | 67.9 | 46.2 | 0.563 | 0.827 |
| real | walk | 1 | 17.8 | 44.8 | 10.7 | 0.398 | 1.67 | 17.8 | 44.8 | 10.7 | 0.398 | 1.67 | 10.3 | 36.2 | 6.09 | 0.285 | 1.69 |
| real | walk | all | 46.3 | 78.6 | 52.3 | 0.589 | 0.886 | 46.3 | 78.6 | 52.3 | 0.589 | 0.886 | 38 | 69.9 | 47.6 | 0.544 | 0.799 |
| realerr | cursor | 1 | 14.5 | 34.1 | 8.48 | 0.426 | 1.71 | 14.5 | 34.1 | 8.48 | 0.426 | 1.71 | 10.4 | 28.5 | 5.54 | 0.366 | 1.88 |
| realerr | cursor | all | 39 | 53.1 | 38.6 | 0.735 | 1.01 | 39 | 53.1 | 38.6 | 0.735 | 1.01 | 33.9 | 47.4 | 35.6 | 0.715 | 0.954 |
| realerr | parse | 1 | 14.3 | 15.1 | 8.52 | 0.946 | 1.68 | 14.3 | 15.1 | 8.52 | 0.946 | 1.68 | 10.2 | 9.59 | 5.55 | 1.07 | 1.84 |
| realerr | parse | all | 38.8 | 47.9 | 34.2 | 0.81 | 1.13 | 38.8 | 47.9 | 34.2 | 0.81 | 1.13 | 33.6 | 42.4 | 31.1 | 0.792 | 1.08 |
| realerr | walk | 1 | 14.2 | 27.8 | 8.39 | 0.51 | 1.69 | 14.2 | 27.8 | 8.39 | 0.51 | 1.69 | 10.2 | 22.2 | 5.47 | 0.461 | 1.87 |
| realerr | walk | all | 38.2 | 52 | 37.6 | 0.734 | 1.01 | 38.2 | 52 | 37.6 | 0.734 | 1.01 | 33.1 | 46.5 | 34.5 | 0.712 | 0.959 |

### bash: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 63.1 | 85 | 49.3 | 0.742 | 1.28 | 63.1 | 85 | 49.3 | 0.742 | 1.28 | 55.3 | 76.1 | 44.2 | 0.727 | 1.25 |
| broken | all | 156 | 687 | 364 | 0.227 | 0.429 | 156 | 687 | 364 | 0.227 | 0.429 | 148 | 679 | 359 | 0.218 | 0.411 |
| real | 1 | 18 | 22.9 | 10.2 | 0.787 | 1.77 | 18 | 22.9 | 10.2 | 0.787 | 1.77 | 10.5 | 14 | 5.64 | 0.75 | 1.86 |
| real | all | 52.3 | 120 | 68.4 | 0.438 | 0.766 | 52.4 | 120 | 68.4 | 0.438 | 0.766 | 44.2 | 111 | 63.6 | 0.399 | 0.694 |
| realerr | 1 | 14.5 | 15.4 | 8.48 | 0.942 | 1.71 | 14.5 | 15.4 | 8.48 | 0.942 | 1.71 | 10.5 | 9.89 | 5.52 | 1.07 | 1.91 |
| realerr | all | 48.6 | 107 | 57.3 | 0.453 | 0.848 | 48.6 | 107 | 57.3 | 0.453 | 0.848 | 43.5 | 101 | 54.3 | 0.429 | 0.801 |

### bash: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0282 (real), 0.419 (realerr), 0.106 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.414 | 14200 | 7.66 | 728 | 68200 | 728 | 68200 | 0.000568 |
| real | steady | 0.0282 | 33.7 | 7.67 | 728 | 67900 | 728 | 67900 | 0 |
| realerr | cold | 19.6 | 181000 | 8.81 | 3270 | 316000 | 3270 | 316000 | 0.006 |
| realerr | steady | 0.419 | 86.5 | 10.5 | 3270 | 314000 | 3270 | 314000 | 0.000128 |
| broken | cold | 4.59 | 139000 | 8.31 | 2480 | 290000 | 2480 | 290000 | 0.00185 |
| broken | steady | 0.106 | 8340 | 8.29 | 2480 | 290000 | 2480 | 290000 | 0 |

### bash: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2053 B | init ms | 0.00891 | 0.0123 | 0.00387 | 0.724 | 2.3 | 15 |
| #0 2053 B | parse1 ms | 0.158 | 0.21 | 0.154 | 0.753 | 1.03 | 15 |
| #0 2053 B | init+parse1 ms | 0.168 | 0.223 | 0.158 | 0.756 | 1.06 | 15 |
| #0 2053 B | parse2 ms | 0.0592 | 0.146 | 0.0901 | 0.406 | 0.658 | 15 |
| #0 2053 B | process user+sys ms | 0.932 | 1.38 | 0.54 | 0.677 | 1.73 | 15 |
| #0 2053 B | exec to exit instr user+kernel M | 5.96 | 8.89 | 4.1 | 0.67 | 1.45 | 15 |
| #0 2053 B | exec to exit instr user M | 3.19 | 5.15 | 3.09 | 0.619 | 1.03 | 15 |
| #0 2053 B | exec to exit instr kernel M | 2.77 | 3.73 | 1.01 | 0.744 | 2.75 | 15 |
| #0 2053 B | exec to exit cycles user+kernel M | 4.21 | 6.49 | 2.33 | 0.649 | 1.81 | 15 |
| #0 2053 B | exec to exit task-clock ms | 0.87 | 1.3 | 0.45 | 0.669 | 1.93 | 15 |
| #0 2053 B | peak RSS MB | 4.88 | 5.97 | 3.5 | 0.817 | 1.39 | 15 |
| #0 2053 B | peak footprint MB | 4.91 | 6 | 3.47 | 0.818 | 1.41 | 15 |
| #1 1444 B | init ms | 0.00837 | 0.0124 | 0.0037 | 0.673 | 2.26 | 15 |
| #1 1444 B | parse1 ms | 0.11 | 0.131 | 0.102 | 0.841 | 1.08 | 15 |
| #1 1444 B | init+parse1 ms | 0.123 | 0.144 | 0.106 | 0.854 | 1.17 | 15 |
| #1 1444 B | parse2 ms | 0.0303 | 0.0768 | 0.048 | 0.394 | 0.631 | 15 |
| #1 1444 B | process user+sys ms | 0.833 | 1.19 | 0.432 | 0.7 | 1.93 | 15 |
| #1 1444 B | exec to exit instr user+kernel M | 4.53 | 6.77 | 2.69 | 0.669 | 1.68 | 15 |
| #1 1444 B | exec to exit instr user M | 1.8 | 3.14 | 1.7 | 0.572 | 1.06 | 15 |
| #1 1444 B | exec to exit instr kernel M | 2.74 | 3.61 | 0.988 | 0.761 | 2.78 | 15 |
| #1 1444 B | exec to exit cycles user+kernel M | 3.76 | 5.45 | 1.78 | 0.689 | 2.11 | 15 |
| #1 1444 B | exec to exit task-clock ms | 0.77 | 1.11 | 0.34 | 0.694 | 2.26 | 15 |
| #1 1444 B | peak RSS MB | 4.73 | 5.83 | 3.7 | 0.812 | 1.28 | 15 |
| #1 1444 B | peak footprint MB | 4.76 | 5.86 | 3.64 | 0.813 | 1.31 | 15 |

### bash: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 638 | 410142 | 0 | 0 | ok |
| realerr | 43 | 123934 | 43 | 0 | ok |
| broken | 501 | 987084 | 323 | 0 | ok |

## c

### c: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 221 | 1160 | 565 | 562 | 580 | 580 | 616 | 0.19 | 0.391 | 0.393 | 0.381 | 0.381 | 0.359 | 225 | 1200 | 569 | 572 | 582 | 617 | 0.188 | 0.395 | 0.393 | 0.386 | 0.365 | 221-233 | 565-635 | 1.01 | 1.01 | 1 | 22.2 |
| broken | cursor | all | 37.2 | 196 | 94.2 | 94.2 | 97.2 | 82.5 | 100 | 0.19 | 0.395 | 0.395 | 0.382 | 0.45 | 0.371 | 39.6 | 202 | 97.3 | 97.9 | 85.2 | 104 | 0.196 | 0.407 | 0.404 | 0.464 | 0.379 | 37.2-42.7 | 94.2-107 | 10.3 * | 10.3 * | 10.2 * | 132 |
| broken | parse | 1 | 179 | 797 | 514 | 507 | 519 | 480 | 577 | 0.224 | 0.348 | 0.352 | 0.344 | 0.372 | 0.31 | 182 | 808 | 517 | 523 | 481 | 597 | 0.225 | 0.352 | 0.348 | 0.378 | 0.305 | 179-210 | 514-534 | 1 | 1.01 | 1 | 27.5 |
| broken | parse | all | 26.9 | 122 | 88.7 | 88.8 | 91.3 | 76.8 | 98 | 0.22 | 0.303 | 0.303 | 0.295 | 0.351 | 0.275 | 28.4 | 127 | 93.9 | 93 | 78.5 | 98.7 | 0.224 | 0.302 | 0.305 | 0.362 | 0.287 | 26.9-32.5 | 88.7-103 | 11.6 | 11 * | 9.47 * | 183 |
| broken | walk | 1 | 219 | 1130 | 546 | 544 | n/a | n/a | n/a | 0.194 | 0.4 | 0.402 | n/a | n/a | n/a | 220 | 1150 | 557 | 564 | n/a | n/a | 0.192 | 0.396 | 0.391 | n/a | n/a | 219-232 | 546-574 | 1 | 1.01 | 1 | 22.5 |
| broken | walk | all | 39.6 | 190 | 93.3 | 88.1 | n/a | n/a | n/a | 0.208 | 0.425 | 0.45 | n/a | n/a | n/a | 42 | 197 | 98.2 | 96.1 | n/a | n/a | 0.213 | 0.427 | 0.437 | n/a | n/a | 39.6-46.9 | 93.3-118 | 9.63 * | 10.1 * | 9.62 * | 124 |
| real | cursor | 1 | 90.1 | 413 | 180 | 178 | 186 | 206 | 198 | 0.218 | 0.5 | 0.507 | 0.485 | 0.437 | 0.455 | 91.8 | 428 | 186 | 179 | 208 | 203 | 0.214 | 0.494 | 0.514 | 0.442 | 0.453 | 90.1-99.4 | 180-208 | 1 | 1.01 | 1 | 37.4 |
| real | cursor | all | 16.5 | 83.1 | 32.2 | 32 | 33.6 | 37.5 | 36.2 | 0.199 | 0.512 | 0.516 | 0.491 | 0.44 | 0.456 | 18.4 | 86.5 | 34 | 33.2 | 38.3 | 36.4 | 0.213 | 0.541 | 0.554 | 0.48 | 0.506 | 16.5-19.5 | 32.2-35.8 | 8.4 * | 8.65 * | 9.09 * | 204 |
| real | parse | 1 | 73.2 | 260 | 155 | 153 | 164 | 160 | 175 | 0.282 | 0.472 | 0.479 | 0.447 | 0.456 | 0.418 | 75.6 | 264 | 162 | 155 | 162 | 177 | 0.286 | 0.468 | 0.487 | 0.468 | 0.428 | 73.2-85.7 | 155-176 | 1 | 1.01 | 1 | 46 |
| real | parse | all | 13.4 | 46.1 | 25.8 | 26.6 | 26 | 29.1 | 30.7 | 0.29 | 0.518 | 0.503 | 0.516 | 0.46 | 0.437 | 14.3 | 48 | 27.8 | 28 | 30.1 | 31.5 | 0.298 | 0.514 | 0.509 | 0.474 | 0.454 | 13.4-17.2 | 25.8-29.8 | 8.39 * | 9.23 * | 9.56 * | 251 |
| real | walk | 1 | 91.2 | 404 | 172 | 170 | n/a | n/a | n/a | 0.226 | 0.532 | 0.538 | n/a | n/a | n/a | 92 | 414 | 173 | 172 | n/a | n/a | 0.222 | 0.533 | 0.535 | n/a | n/a | 91.2-96.1 | 172-181 | 1 | 1.01 | 1 | 36.9 |
| real | walk | all | 19.1 | 84.9 | 32.7 | 32.4 | n/a | n/a | n/a | 0.225 | 0.585 | 0.591 | n/a | n/a | n/a | 19.8 | 88.8 | 33.8 | 33.5 | n/a | n/a | 0.224 | 0.587 | 0.593 | n/a | n/a | 19.1-21.5 | 32.7-34.7 | 7.56 * | 7.87 * | 8.72 * | 176 |
| realerr | cursor | 1 | 247 | 1100 | 510 | 503 | 523 | 557 | 575 | 0.223 | 0.484 | 0.49 | 0.472 | 0.442 | 0.429 | 247 | 1130 | 518 | 509 | 564 | 596 | 0.219 | 0.478 | 0.486 | 0.439 | 0.415 | 247-260 | 510-584 | 1 | 1.01 | 1 | 31.2 |
| realerr | cursor | all | 40.8 | 182 | 78.6 | 78 | 82.1 | 85 | 89.8 | 0.224 | 0.519 | 0.523 | 0.497 | 0.48 | 0.454 | 41.8 | 187 | 82.5 | 81.6 | 87 | 91 | 0.224 | 0.506 | 0.512 | 0.48 | 0.459 | 40.8-45 | 78.6-84.6 | 10.5 * | 10.5 * | 10.9 * | 188 |
| realerr | parse | 1 | 202 | 740 | 451 | 448 | 464 | 454 | 518 | 0.273 | 0.449 | 0.452 | 0.436 | 0.445 | 0.39 | 207 | 750 | 460 | 458 | 456 | 525 | 0.276 | 0.451 | 0.452 | 0.455 | 0.395 | 202-214 | 451-521 | 1 | 1.01 | 1 | 38 |
| realerr | parse | all | 32.8 | 113 | 71.2 | 69.3 | 71 | 71.4 | 78.6 | 0.289 | 0.461 | 0.473 | 0.462 | 0.459 | 0.417 | 34.2 | 117 | 74.4 | 72 | 73.2 | 80.5 | 0.291 | 0.46 | 0.475 | 0.467 | 0.425 | 32.8-39.9 | 71.2-77.8 | 10.5 * | 10.8 * | 10.8 * | 234 |
| realerr | walk | 1 | 248 | 1070 | 491 | 487 | n/a | n/a | n/a | 0.232 | 0.505 | 0.509 | n/a | n/a | n/a | 252 | 1080 | 496 | 498 | n/a | n/a | 0.233 | 0.508 | 0.506 | n/a | n/a | 248-263 | 491-502 | 1 | 1.01 | 1 | 31 |
| realerr | walk | all | 39.1 | 171 | 76.5 | 75.9 | n/a | n/a | n/a | 0.229 | 0.511 | 0.515 | n/a | n/a | n/a | 41.7 | 176 | 78.5 | 79 | n/a | n/a | 0.237 | 0.531 | 0.528 | n/a | n/a | 39.1-43.9 | 76.5-83.1 | 10.4 * | 10.5 * | 11.1 * | 197 |

### c: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 222 | 1170 | 565 | 562 | 579 | 580 | 616 | 0.189 | 0.393 | 0.395 | 0.383 | 0.383 | 0.361 | 226 | 1210 | 569 | 572 | 582 | 617 | 0.188 | 0.398 | 0.396 | 0.389 | 0.367 | 222-234 | 565-635 |
| broken | cursor | all | 394 | 2020 | 984 | 968 | 994 | 969 | 1070 | 0.195 | 0.401 | 0.407 | 0.397 | 0.407 | 0.37 | 406 | 2070 | 995 | 989 | 979 | 1080 | 0.196 | 0.408 | 0.411 | 0.415 | 0.377 | 394-416 | 984-1020 |
| broken | parse | 1 | 179 | 800 | 513 | 507 | 519 | 480 | 576 | 0.223 | 0.348 | 0.352 | 0.345 | 0.372 | 0.31 | 182 | 812 | 517 | 523 | 481 | 597 | 0.224 | 0.352 | 0.348 | 0.378 | 0.305 | 179-211 | 513-534 |
| broken | parse | all | 309 | 1360 | 871 | 864 | 881 | 836 | 967 | 0.227 | 0.354 | 0.357 | 0.35 | 0.369 | 0.319 | 335 | 1390 | 900 | 892 | 853 | 980 | 0.241 | 0.372 | 0.375 | 0.392 | 0.341 | 309-341 | 871-910 |
| broken | walk | 1 | 219 | 1150 | 546 | 544 | n/a | n/a | n/a | 0.191 | 0.4 | 0.402 | n/a | n/a | n/a | 221 | 1160 | 556 | 564 | n/a | n/a | 0.19 | 0.396 | 0.391 | n/a | n/a | 219-233 | 546-574 |
| broken | walk | all | 381 | 1950 | 947 | 939 | n/a | n/a | n/a | 0.195 | 0.402 | 0.406 | n/a | n/a | n/a | 405 | 1980 | 968 | 960 | n/a | n/a | 0.204 | 0.419 | 0.422 | n/a | n/a | 381-418 | 947-985 |
| real | cursor | 1 | 90.2 | 417 | 180 | 178 | 186 | 206 | 198 | 0.216 | 0.501 | 0.508 | 0.485 | 0.438 | 0.456 | 91.9 | 433 | 186 | 178 | 208 | 203 | 0.213 | 0.495 | 0.515 | 0.443 | 0.454 | 90.2-99.5 | 180-208 |
| real | cursor | all | 145 | 734 | 294 | 290 | 298 | 330 | 325 | 0.197 | 0.492 | 0.499 | 0.486 | 0.439 | 0.445 | 155 | 745 | 309 | 301 | 332 | 328 | 0.208 | 0.5 | 0.513 | 0.465 | 0.471 | 145-157 | 294-315 |
| real | parse | 1 | 73.2 | 261 | 155 | 153 | 164 | 160 | 175 | 0.281 | 0.473 | 0.479 | 0.448 | 0.456 | 0.418 | 75.7 | 265 | 162 | 155 | 162 | 177 | 0.285 | 0.469 | 0.488 | 0.469 | 0.429 | 73.2-85.8 | 155-176 |
| real | parse | all | 116 | 433 | 258 | 247 | 259 | 264 | 281 | 0.267 | 0.449 | 0.469 | 0.447 | 0.438 | 0.412 | 121 | 440 | 266 | 261 | 267 | 287 | 0.275 | 0.455 | 0.464 | 0.453 | 0.422 | 116-131 | 258-273 |
| real | walk | 1 | 91.2 | 408 | 172 | 170 | n/a | n/a | n/a | 0.224 | 0.532 | 0.538 | n/a | n/a | n/a | 92.1 | 418 | 173 | 172 | n/a | n/a | 0.22 | 0.534 | 0.536 | n/a | n/a | 91.2-96.2 | 172-181 |
| real | walk | all | 142 | 690 | 285 | 282 | n/a | n/a | n/a | 0.205 | 0.497 | 0.502 | n/a | n/a | n/a | 152 | 697 | 294 | 290 | n/a | n/a | 0.218 | 0.518 | 0.524 | n/a | n/a | 142-157 | 285-301 |
| realerr | cursor | 1 | 247 | 1110 | 510 | 503 | 523 | 557 | 575 | 0.222 | 0.484 | 0.491 | 0.472 | 0.443 | 0.429 | 248 | 1140 | 518 | 509 | 564 | 596 | 0.218 | 0.478 | 0.487 | 0.439 | 0.415 | 247-260 | 510-584 |
| realerr | cursor | all | 427 | 1940 | 871 | 848 | 905 | 933 | 961 | 0.22 | 0.491 | 0.504 | 0.472 | 0.458 | 0.445 | 444 | 1960 | 902 | 889 | 942 | 971 | 0.227 | 0.493 | 0.499 | 0.472 | 0.457 | 427-454 | 871-911 |
| realerr | parse | 1 | 202 | 743 | 451 | 448 | 464 | 454 | 518 | 0.272 | 0.449 | 0.452 | 0.437 | 0.446 | 0.391 | 208 | 754 | 460 | 458 | 456 | 525 | 0.275 | 0.451 | 0.453 | 0.455 | 0.395 | 202-214 | 451-521 |
| realerr | parse | all | 347 | 1240 | 775 | 764 | 779 | 764 | 859 | 0.28 | 0.448 | 0.455 | 0.446 | 0.455 | 0.404 | 363 | 1260 | 796 | 793 | 783 | 874 | 0.288 | 0.455 | 0.457 | 0.463 | 0.415 | 347-373 | 775-815 |
| realerr | walk | 1 | 248 | 1080 | 491 | 487 | n/a | n/a | n/a | 0.23 | 0.505 | 0.509 | n/a | n/a | n/a | 252 | 1090 | 496 | 498 | n/a | n/a | 0.231 | 0.509 | 0.506 | n/a | n/a | 248-263 | 491-502 |
| realerr | walk | all | 414 | 1800 | 842 | 833 | n/a | n/a | n/a | 0.229 | 0.492 | 0.497 | n/a | n/a | n/a | 435 | 1840 | 872 | 858 | n/a | n/a | 0.236 | 0.499 | 0.508 | n/a | n/a | 414-444 | 842-882 |

### c: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 5040 | 22200 | 11100 | 10800 | 10900 | 11100 | 11500 | 0.227 | 0.454 | 0.467 | 0.461 | 0.454 | 0.439 | 5040 | 22200 | 11100 | 10800 | 11100 | 11500 | 0.227 | 0.454 | 0.467 | 0.453 | 0.438 | 5040-5040 | 11100-11100 |
| broken | cursor | all | 5050 | 22200 | 11100 | 10800 | 11000 | 11200 | 11500 | 0.227 | 0.454 | 0.466 | 0.461 | 0.453 | 0.438 | 5070 | 22300 | 11100 | 10800 | 11200 | 11500 | 0.228 | 0.456 | 0.469 | 0.455 | 0.44 | 5050-5160 | 11100-11100 |
| broken | parse | 1 | 4030 | 14200 | 9990 | 9680 | 9790 | 9880 | 10300 | 0.284 | 0.403 | 0.416 | 0.411 | 0.408 | 0.391 | 4030 | 14200 | 9990 | 9680 | 9880 | 10300 | 0.284 | 0.403 | 0.416 | 0.408 | 0.39 | 4030-4030 | 9990-9990 |
| broken | parse | all | 4040 | 14200 | 9960 | 9690 | 9760 | 9900 | 10300 | 0.285 | 0.405 | 0.416 | 0.414 | 0.408 | 0.391 | 4050 | 14200 | 10000 | 9700 | 9900 | 10300 | 0.286 | 0.405 | 0.418 | 0.409 | 0.392 | 4040-4190 | 9960-10000 |
| broken | walk | 1 | 4970 | 20400 | 10700 | 10400 | n/a | n/a | n/a | 0.243 | 0.466 | 0.479 | n/a | n/a | n/a | 4980 | 20500 | 10700 | 10400 | n/a | n/a | 0.243 | 0.467 | 0.481 | n/a | n/a | 4970-5000 | 10700-10700 |
| broken | walk | all | 5160 | 20700 | 10700 | 10400 | n/a | n/a | n/a | 0.249 | 0.483 | 0.497 | n/a | n/a | n/a | 5190 | 20800 | 10700 | 10400 | n/a | n/a | 0.249 | 0.485 | 0.5 | n/a | n/a | 5160-5240 | 10700-10700 |
| real | cursor | 1 | 2070 | 7730 | 3340 | 3170 | 3300 | 3500 | 3500 | 0.268 | 0.621 | 0.654 | 0.629 | 0.594 | 0.593 | 2070 | 7740 | 3350 | 3180 | 3500 | 3510 | 0.268 | 0.62 | 0.653 | 0.592 | 0.591 | 2070-2080 | 3340-3350 |
| real | cursor | all | 2070 | 7760 | 3350 | 3180 | 3310 | 3500 | 3520 | 0.267 | 0.619 | 0.652 | 0.627 | 0.592 | 0.59 | 2080 | 7790 | 3360 | 3190 | 3510 | 3520 | 0.266 | 0.617 | 0.651 | 0.591 | 0.59 | 2070-2080 | 3350-3360 |
| real | parse | 1 | 1630 | 4360 | 2840 | 2670 | 2780 | 2940 | 2970 | 0.373 | 0.573 | 0.61 | 0.585 | 0.554 | 0.548 | 1630 | 4360 | 2840 | 2670 | 2940 | 2970 | 0.373 | 0.572 | 0.609 | 0.553 | 0.547 | 1630-1630 | 2840-2840 |
| real | parse | all | 1630 | 4350 | 2840 | 2670 | 2790 | 2950 | 2980 | 0.373 | 0.573 | 0.609 | 0.584 | 0.552 | 0.545 | 1630 | 4370 | 2850 | 2680 | 2950 | 2990 | 0.372 | 0.57 | 0.607 | 0.551 | 0.544 | 1630-1630 | 2840-2860 |
| real | walk | 1 | 2070 | 6990 | 3190 | 3020 | n/a | n/a | n/a | 0.297 | 0.65 | 0.687 | n/a | n/a | n/a | 2070 | 7000 | 3190 | 3020 | n/a | n/a | 0.296 | 0.649 | 0.686 | n/a | n/a | 2070-2070 | 3190-3200 |
| real | walk | all | 2070 | 7020 | 3200 | 3020 | n/a | n/a | n/a | 0.296 | 0.649 | 0.687 | n/a | n/a | n/a | 2070 | 7060 | 3210 | 3030 | n/a | n/a | 0.294 | 0.647 | 0.684 | n/a | n/a | 2070-2080 | 3200-3210 |
| realerr | cursor | 1 | 5370 | 19900 | 9340 | 8870 | 9190 | 9680 | 9750 | 0.27 | 0.575 | 0.605 | 0.584 | 0.555 | 0.551 | 5370 | 19900 | 9340 | 8880 | 9680 | 9760 | 0.27 | 0.575 | 0.605 | 0.554 | 0.55 | 5370-5370 | 9340-9350 |
| realerr | cursor | all | 5380 | 19900 | 9360 | 8890 | 9220 | 9710 | 9780 | 0.27 | 0.574 | 0.604 | 0.583 | 0.554 | 0.549 | 5380 | 19900 | 9370 | 8900 | 9710 | 9790 | 0.27 | 0.574 | 0.604 | 0.554 | 0.55 | 5380-5470 | 9360-9390 |
| realerr | parse | 1 | 4330 | 12100 | 8170 | 7700 | 7990 | 8380 | 8520 | 0.358 | 0.53 | 0.562 | 0.542 | 0.516 | 0.508 | 4330 | 12100 | 8170 | 7710 | 8390 | 8520 | 0.358 | 0.529 | 0.562 | 0.516 | 0.508 | 4330-4330 | 8170-8180 |
| realerr | parse | all | 4330 | 12100 | 8180 | 7720 | 8010 | 8400 | 8540 | 0.359 | 0.53 | 0.561 | 0.541 | 0.515 | 0.507 | 4340 | 12100 | 8200 | 7740 | 8410 | 8550 | 0.359 | 0.529 | 0.561 | 0.516 | 0.507 | 4330-4350 | 8180-8210 |
| realerr | walk | 1 | 5300 | 18000 | 8920 | 8450 | n/a | n/a | n/a | 0.294 | 0.594 | 0.627 | n/a | n/a | n/a | 5300 | 18000 | 8920 | 8460 | n/a | n/a | 0.294 | 0.594 | 0.627 | n/a | n/a | 5300-5300 | 8920-8930 |
| realerr | walk | all | 5300 | 18100 | 8930 | 8460 | n/a | n/a | n/a | 0.294 | 0.594 | 0.627 | n/a | n/a | n/a | 5310 | 18100 | 8950 | 8480 | n/a | n/a | 0.293 | 0.594 | 0.626 | n/a | n/a | 5300-5510 | 8930-8960 |

### c: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1160 | 6060 | 2940 | 2920 | 3010 | 3010 | 3200 | 0.191 | 0.394 | 0.396 | 0.384 | 0.384 | 0.362 | 1180 | 6200 | 2960 | 2960 | 3020 | 3200 | 0.19 | 0.397 | 0.397 | 0.389 | 0.367 | 1160-1210 | 2940-3300 |
| broken | cursor | all | 1790 | 9160 | 4490 | 4430 | 4590 | 4400 | 4890 | 0.196 | 0.4 | 0.405 | 0.391 | 0.408 | 0.367 | 1860 | 9300 | 4540 | 4530 | 4470 | 4930 | 0.2 | 0.409 | 0.41 | 0.415 | 0.377 | 1790-1880 | 4490-4610 |
| broken | parse | 1 | 932 | 4150 | 2670 | 2640 | 2700 | 2500 | 2990 | 0.225 | 0.349 | 0.353 | 0.346 | 0.373 | 0.311 | 947 | 4200 | 2690 | 2720 | 2500 | 3100 | 0.225 | 0.352 | 0.348 | 0.379 | 0.305 | 932-1100 | 2670-2780 |
| broken | parse | all | 1420 | 6210 | 4020 | 4000 | 4070 | 3820 | 4450 | 0.229 | 0.354 | 0.355 | 0.349 | 0.372 | 0.319 | 1520 | 6280 | 4120 | 4100 | 3870 | 4500 | 0.242 | 0.37 | 0.371 | 0.393 | 0.338 | 1420-1550 | 4020-4170 |
| broken | walk | 1 | 1140 | 5910 | 2840 | 2830 | n/a | n/a | n/a | 0.193 | 0.402 | 0.403 | n/a | n/a | n/a | 1150 | 5970 | 2890 | 2920 | n/a | n/a | 0.193 | 0.398 | 0.395 | n/a | n/a | 1140-1200 | 2840-2980 |
| broken | walk | all | 1770 | 8830 | 4350 | 4280 | n/a | n/a | n/a | 0.2 | 0.407 | 0.413 | n/a | n/a | n/a | 1870 | 8920 | 4420 | 4420 | n/a | n/a | 0.209 | 0.423 | 0.422 | n/a | n/a | 1770-1890 | 4350-4500 |
| real | cursor | 1 | 470 | 2160 | 936 | 923 | 967 | 1070 | 1030 | 0.218 | 0.503 | 0.509 | 0.486 | 0.439 | 0.456 | 478 | 2240 | 965 | 928 | 1080 | 1050 | 0.214 | 0.496 | 0.515 | 0.443 | 0.455 | 470-518 | 936-1070 |
| real | cursor | all | 705 | 3320 | 1360 | 1360 | 1380 | 1550 | 1510 | 0.212 | 0.516 | 0.516 | 0.51 | 0.453 | 0.465 | 734 | 3360 | 1430 | 1420 | 1560 | 1540 | 0.219 | 0.512 | 0.518 | 0.471 | 0.478 | 705-757 | 1360-1460 |
| real | parse | 1 | 382 | 1350 | 805 | 794 | 850 | 833 | 911 | 0.283 | 0.474 | 0.481 | 0.449 | 0.458 | 0.419 | 395 | 1370 | 836 | 805 | 839 | 918 | 0.288 | 0.472 | 0.491 | 0.471 | 0.43 | 382-447 | 805-903 |
| real | parse | all | 576 | 1970 | 1210 | 1180 | 1210 | 1240 | 1330 | 0.292 | 0.476 | 0.488 | 0.476 | 0.463 | 0.435 | 591 | 2010 | 1250 | 1230 | 1270 | 1350 | 0.294 | 0.473 | 0.481 | 0.465 | 0.438 | 576-611 | 1210-1270 |
| real | walk | 1 | 476 | 2100 | 892 | 883 | n/a | n/a | n/a | 0.226 | 0.534 | 0.54 | n/a | n/a | n/a | 480 | 2150 | 896 | 894 | n/a | n/a | 0.223 | 0.536 | 0.537 | n/a | n/a | 476-501 | 892-938 |
| real | walk | all | 691 | 3130 | 1340 | 1330 | n/a | n/a | n/a | 0.221 | 0.518 | 0.52 | n/a | n/a | n/a | 742 | 3160 | 1360 | 1370 | n/a | n/a | 0.235 | 0.544 | 0.54 | n/a | n/a | 691-758 | 1340-1390 |
| realerr | cursor | 1 | 1290 | 5750 | 2650 | 2620 | 2720 | 2900 | 3000 | 0.224 | 0.485 | 0.492 | 0.473 | 0.443 | 0.43 | 1290 | 5880 | 2700 | 2650 | 2930 | 3100 | 0.22 | 0.479 | 0.487 | 0.44 | 0.416 | 1290-1350 | 2650-3040 |
| realerr | cursor | all | 1960 | 8710 | 3990 | 3920 | 4160 | 4310 | 4400 | 0.225 | 0.492 | 0.501 | 0.472 | 0.456 | 0.446 | 2030 | 8800 | 4100 | 4080 | 4310 | 4430 | 0.23 | 0.494 | 0.497 | 0.47 | 0.458 | 1960-2070 | 3990-4140 |
| realerr | parse | 1 | 1060 | 3860 | 2350 | 2330 | 2410 | 2360 | 2700 | 0.274 | 0.45 | 0.453 | 0.438 | 0.447 | 0.392 | 1080 | 3910 | 2390 | 2380 | 2370 | 2730 | 0.277 | 0.452 | 0.454 | 0.457 | 0.396 | 1060-1120 | 2350-2710 |
| realerr | parse | all | 1600 | 5650 | 3550 | 3540 | 3580 | 3530 | 3960 | 0.283 | 0.45 | 0.452 | 0.447 | 0.453 | 0.404 | 1670 | 5720 | 3650 | 3640 | 3600 | 4020 | 0.291 | 0.457 | 0.457 | 0.463 | 0.415 | 1600-1700 | 3550-3690 |
| realerr | walk | 1 | 1290 | 5580 | 2550 | 2540 | n/a | n/a | n/a | 0.232 | 0.507 | 0.51 | n/a | n/a | n/a | 1310 | 5640 | 2580 | 2590 | n/a | n/a | 0.233 | 0.51 | 0.508 | n/a | n/a | 1290-1360 | 2550-2610 |
| realerr | walk | all | 1920 | 8110 | 3870 | 3840 | n/a | n/a | n/a | 0.237 | 0.497 | 0.5 | n/a | n/a | n/a | 2000 | 8260 | 3970 | 3950 | n/a | n/a | 0.242 | 0.504 | 0.506 | n/a | n/a | 1920-2030 | 3870-4020 |

### c: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1010 | 8010 | 1100 | 1100 | 1140 | 1230 | 1170 | 0.126 | 0.917 | 0.917 | 0.889 | 0.821 | 0.864 |
| broken | cursor minus parse | 1 | cycles M | 225 | 1910 | 267 | 279 | 314 | 517 | 201 | 0.118 | 0.841 | 0.804 | 0.715 | 0.435 | 1.12 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 43.3 | 373 | 51.8 | 54.8 | 60.9 | 99.8 | 39.4 | 0.116 | 0.837 | 0.791 | 0.711 | 0.434 | 1.1 |
| broken | cursor minus parse | all | instructions M | 1010 | 8080 | 1160 | 1130 | 1200 | 1260 | 1200 | 0.125 | 0.871 | 0.894 | 0.847 | 0.804 | 0.843 |
| broken | cursor minus parse | all | cycles M | 374 | 2950 | 469 | 429 | 524 | 582 | 446 | 0.127 | 0.797 | 0.873 | 0.714 | 0.643 | 0.839 |
| broken | cursor minus parse | all | CPU user+sys ms | 85.6 | 663 | 113 | 104 | 113 | 133 | 99.9 | 0.129 | 0.757 | 0.821 | 0.759 | 0.642 | 0.857 |
| broken | walk minus parse | 1 | instructions M | 942 | 6250 | 685 | 685 | n/a | n/a | n/a | 0.151 | 1.38 | 1.38 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 209 | 1760 | 172 | 191 | n/a | n/a | n/a | 0.119 | 1.22 | 1.09 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 40.1 | 345 | 32.8 | 37.1 | n/a | n/a | n/a | 0.116 | 1.22 | 1.08 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 1120 | 6580 | 724 | 684 | n/a | n/a | n/a | 0.171 | 1.55 | 1.64 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 349 | 2620 | 334 | 280 | n/a | n/a | n/a | 0.133 | 1.05 | 1.24 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 72.3 | 594 | 75.7 | 75.1 | n/a | n/a | n/a | 0.122 | 0.955 | 0.964 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 449 | 3370 | 505 | 505 | 519 | 559 | 533 | 0.133 | 0.889 | 0.889 | 0.864 | 0.802 | 0.842 |
| real | cursor minus parse | 1 | cycles M | 88.3 | 807 | 130 | 129 | 117 | 237 | 120 | 0.11 | 0.678 | 0.685 | 0.754 | 0.373 | 0.737 |
| real | cursor minus parse | 1 | CPU user+sys ms | 16.9 | 156 | 25.2 | 24.7 | 22.3 | 45.5 | 22.8 | 0.108 | 0.672 | 0.685 | 0.759 | 0.372 | 0.743 |
| real | cursor minus parse | all | instructions M | 448 | 3410 | 514 | 513 | 524 | 555 | 533 | 0.132 | 0.873 | 0.874 | 0.857 | 0.809 | 0.841 |
| real | cursor minus parse | all | cycles M | 129 | 1340 | 155 | 183 | 172 | 311 | 189 | 0.0957 | 0.831 | 0.704 | 0.746 | 0.413 | 0.679 |
| real | cursor minus parse | all | CPU user+sys ms | 29.1 | 302 | 36.2 | 42.9 | 38.5 | 65.9 | 44.1 | 0.0963 | 0.803 | 0.677 | 0.756 | 0.441 | 0.659 |
| real | walk minus parse | 1 | instructions M | 448 | 2620 | 353 | 353 | n/a | n/a | n/a | 0.171 | 1.27 | 1.27 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 94.4 | 754 | 86.3 | 88.2 | n/a | n/a | n/a | 0.125 | 1.09 | 1.07 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 18 | 147 | 16.6 | 16.7 | n/a | n/a | n/a | 0.123 | 1.08 | 1.08 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 448 | 2660 | 355 | 350 | n/a | n/a | n/a | 0.168 | 1.26 | 1.28 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 115 | 1160 | 125 | 149 | n/a | n/a | n/a | 0.0996 | 0.923 | 0.777 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 26 | 257 | 27 | 35 | n/a | n/a | n/a | 0.101 | 0.961 | 0.741 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 1040 | 7800 | 1170 | 1170 | 1200 | 1300 | 1230 | 0.134 | 0.892 | 0.892 | 0.866 | 0.805 | 0.844 |
| realerr | cursor minus parse | 1 | cycles M | 230 | 1900 | 307 | 287 | 307 | 539 | 298 | 0.121 | 0.751 | 0.802 | 0.75 | 0.427 | 0.772 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 44.4 | 369 | 58.8 | 55 | 58.9 | 103 | 57.1 | 0.12 | 0.755 | 0.806 | 0.753 | 0.431 | 0.777 |
| realerr | cursor minus parse | all | instructions M | 1040 | 7860 | 1180 | 1180 | 1210 | 1300 | 1250 | 0.133 | 0.88 | 0.887 | 0.865 | 0.801 | 0.837 |
| realerr | cursor minus parse | all | cycles M | 363 | 3070 | 435 | 380 | 574 | 778 | 444 | 0.119 | 0.836 | 0.955 | 0.633 | 0.467 | 0.818 |
| realerr | cursor minus parse | all | CPU user+sys ms | 80.2 | 703 | 96 | 84.2 | 126 | 170 | 102 | 0.114 | 0.835 | 0.952 | 0.638 | 0.473 | 0.784 |
| realerr | walk minus parse | 1 | instructions M | 971 | 5940 | 750 | 750 | n/a | n/a | n/a | 0.163 | 1.29 | 1.29 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 238 | 1720 | 207 | 205 | n/a | n/a | n/a | 0.138 | 1.15 | 1.16 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 45.6 | 333 | 40 | 39.4 | n/a | n/a | n/a | 0.137 | 1.14 | 1.16 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 971 | 6000 | 754 | 744 | n/a | n/a | n/a | 0.162 | 1.29 | 1.31 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 324 | 2460 | 314 | 307 | n/a | n/a | n/a | 0.132 | 1.03 | 1.06 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 66.9 | 564 | 67.2 | 69.3 | n/a | n/a | n/a | 0.119 | 0.996 | 0.966 | n/a | n/a | n/a |

### c: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 95 | 211 | 43.2 | 0.45 | 2.2 | 95 | 211 | 43.2 | 0.45 | 2.2 | 85.5 | 200 | 35.9 | 0.427 | 2.38 |
| broken | cursor | all | 216 | 368 | 300 | 0.587 | 0.72 | 216 | 368 | 300 | 0.587 | 0.72 | 206 | 357 | 292 | 0.576 | 0.703 |
| broken | parse | 1 | 42.2 | 41.3 | 31.2 | 1.02 | 1.35 | 42.2 | 41.3 | 31.2 | 1.02 | 1.35 | 32.8 | 30.5 | 23.9 | 1.07 | 1.37 |
| broken | parse | all | 144 | 283 | 237 | 0.509 | 0.608 | 144 | 283 | 237 | 0.509 | 0.608 | 134 | 272 | 230 | 0.492 | 0.583 |
| broken | walk | 1 | 71.7 | 189 | 41.9 | 0.379 | 1.71 | 71.7 | 189 | 41.9 | 0.379 | 1.71 | 62.2 | 178 | 34.6 | 0.349 | 1.8 |
| broken | walk | all | 201 | 330 | 363 | 0.61 | 0.555 | 201 | 330 | 363 | 0.61 | 0.555 | 191 | 319 | 356 | 0.599 | 0.538 |
| real | cursor | 1 | 25.9 | 41.5 | 15.3 | 0.625 | 1.7 | 25.9 | 41.5 | 15.3 | 0.625 | 1.7 | 17.7 | 32.1 | 9.65 | 0.553 | 1.84 |
| real | cursor | all | 52.7 | 83.9 | 47.5 | 0.628 | 1.11 | 52.7 | 83.9 | 47.5 | 0.629 | 1.11 | 43.9 | 74.4 | 41.7 | 0.589 | 1.05 |
| real | parse | 1 | 20 | 23.6 | 14.9 | 0.851 | 1.35 | 20 | 23.6 | 14.9 | 0.851 | 1.35 | 11.8 | 14 | 9.3 | 0.848 | 1.27 |
| real | parse | all | 47.8 | 78.4 | 57.2 | 0.609 | 0.836 | 47.8 | 78.4 | 57.2 | 0.609 | 0.836 | 38.6 | 69 | 51.4 | 0.56 | 0.752 |
| real | walk | 1 | 20.2 | 48.5 | 14.9 | 0.417 | 1.36 | 20.2 | 48.5 | 14.9 | 0.417 | 1.36 | 11.9 | 39 | 9.16 | 0.304 | 1.29 |
| real | walk | all | 48.3 | 84.9 | 56.6 | 0.569 | 0.853 | 48.3 | 84.9 | 56.6 | 0.569 | 0.853 | 39.6 | 75.3 | 50.7 | 0.526 | 0.781 |
| realerr | cursor | 1 | 34.3 | 77.2 | 22.4 | 0.444 | 1.53 | 34.3 | 77.2 | 22.4 | 0.444 | 1.53 | 21.7 | 63.4 | 12.6 | 0.342 | 1.73 |
| realerr | cursor | all | 79.6 | 176 | 127 | 0.452 | 0.629 | 79.6 | 176 | 127 | 0.453 | 0.629 | 67 | 162 | 117 | 0.413 | 0.574 |
| realerr | parse | 1 | 28.2 | 35.9 | 22.2 | 0.788 | 1.27 | 28.2 | 35.9 | 22.2 | 0.788 | 1.27 | 15.8 | 22 | 12.5 | 0.718 | 1.26 |
| realerr | parse | all | 76.6 | 158 | 118 | 0.486 | 0.649 | 76.6 | 158 | 118 | 0.486 | 0.649 | 63.9 | 144 | 108 | 0.444 | 0.59 |
| realerr | walk | 1 | 28.5 | 82.9 | 22.5 | 0.344 | 1.27 | 28.6 | 82.9 | 22.5 | 0.344 | 1.27 | 16 | 69.2 | 12.6 | 0.231 | 1.27 |
| realerr | walk | all | 78.8 | 179 | 131 | 0.441 | 0.603 | 78.8 | 179 | 131 | 0.441 | 0.603 | 66.1 | 165 | 121 | 0.402 | 0.547 |

### c: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 41.8 | 41.9 | 31 | 0.997 | 1.35 | 41.8 | 41.9 | 31 | 0.997 | 1.35 | 32.3 | 31 | 23.8 | 1.04 | 1.35 |
| broken | all | 155 | 518 | 303 | 0.299 | 0.512 | 155 | 518 | 303 | 0.299 | 0.512 | 145 | 508 | 295 | 0.285 | 0.49 |
| real | 1 | 20 | 32.6 | 15.3 | 0.614 | 1.31 | 20 | 32.6 | 15.3 | 0.614 | 1.31 | 11.9 | 23.1 | 9.7 | 0.513 | 1.22 |
| real | all | 56.7 | 151 | 102 | 0.375 | 0.554 | 56.7 | 151 | 102 | 0.375 | 0.554 | 48 | 142 | 96.6 | 0.338 | 0.497 |
| realerr | 1 | 27.4 | 39.8 | 22.1 | 0.689 | 1.24 | 27.4 | 39.8 | 22.1 | 0.69 | 1.24 | 15 | 26.1 | 12.4 | 0.575 | 1.21 |
| realerr | all | 82.1 | 268 | 144 | 0.306 | 0.571 | 82.1 | 268 | 144 | 0.306 | 0.571 | 69.5 | 254 | 134 | 0.273 | 0.519 |

### c: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0474 (real), 0.0604 (realerr), 0.0812 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.47 | 25700 | 7.06 | 1430 | 136000 | 1430 | 136000 | 0.000328 |
| real | steady | 0.0474 | 35.7 | 7.04 | 1430 | 136000 | 1430 | 136000 | 0 |
| realerr | cold | 3.49 | 51700 | 7.35 | 5040 | 478000 | 5040 | 478000 | 0.000693 |
| realerr | steady | 0.0604 | 58 | 7.32 | 5040 | 478000 | 5040 | 478000 | 0 |
| broken | cold | 3.49 | 82100 | 7.9 | 4090 | 475000 | 4090 | 475000 | 0.000852 |
| broken | steady | 0.0812 | 7600 | 7.87 | 4090 | 475000 | 4090 | 475000 | 0 |

### c: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2050 B | init ms | 0.00829 | 0.0123 | 0.00558 | 0.677 | 1.49 | 15 |
| #0 2050 B | parse1 ms | 0.119 | 0.144 | 0.117 | 0.828 | 1.02 | 15 |
| #0 2050 B | init+parse1 ms | 0.127 | 0.156 | 0.123 | 0.813 | 1.04 | 15 |
| #0 2050 B | parse2 ms | 0.0367 | 0.095 | 0.0592 | 0.387 | 0.621 | 15 |
| #0 2050 B | process user+sys ms | 0.852 | 1.25 | 0.474 | 0.682 | 1.8 | 15 |
| #0 2050 B | exec to exit instr user+kernel M | 4.71 | 7.42 | 3.17 | 0.634 | 1.48 | 15 |
| #0 2050 B | exec to exit instr user M | 2.03 | 3.79 | 2.19 | 0.537 | 0.928 | 15 |
| #0 2050 B | exec to exit instr kernel M | 2.72 | 3.61 | 0.983 | 0.752 | 2.76 | 15 |
| #0 2050 B | exec to exit cycles user+kernel M | 3.9 | 5.67 | 1.91 | 0.687 | 2.04 | 15 |
| #0 2050 B | exec to exit task-clock ms | 0.79 | 1.18 | 0.38 | 0.669 | 2.08 | 15 |
| #0 2050 B | peak RSS MB | 4.39 | 5.3 | 3.22 | 0.828 | 1.37 | 15 |
| #0 2050 B | peak footprint MB | 4.43 | 5.34 | 3.12 | 0.829 | 1.42 | 15 |
| #1 4736 B | init ms | 0.00882 | 0.0124 | 0.00544 | 0.712 | 1.62 | 15 |
| #1 4736 B | parse1 ms | 0.257 | 0.408 | 0.312 | 0.63 | 0.823 | 15 |
| #1 4736 B | init+parse1 ms | 0.265 | 0.42 | 0.318 | 0.63 | 0.833 | 15 |
| #1 4736 B | parse2 ms | 0.131 | 0.367 | 0.221 | 0.356 | 0.592 | 15 |
| #1 4736 B | process user+sys ms | 1.1 | 1.79 | 0.823 | 0.618 | 1.34 | 15 |
| #1 4736 B | exec to exit instr user+kernel M | 9.16 | 15.8 | 9.01 | 0.578 | 1.02 | 15 |
| #1 4736 B | exec to exit instr user M | 6.37 | 12 | 7.95 | 0.531 | 0.801 | 15 |
| #1 4736 B | exec to exit instr kernel M | 2.78 | 3.8 | 1.06 | 0.732 | 2.62 | 15 |
| #1 4736 B | exec to exit cycles user+kernel M | 5.11 | 8.49 | 3.79 | 0.602 | 1.35 | 15 |
| #1 4736 B | exec to exit task-clock ms | 1.03 | 1.72 | 0.73 | 0.599 | 1.41 | 15 |
| #1 4736 B | peak RSS MB | 4.38 | 5.4 | 3.24 | 0.811 | 1.35 | 15 |
| #1 4736 B | peak footprint MB | 4.41 | 5.43 | 3.15 | 0.812 | 1.4 | 15 |

### c: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 485 | 669198 | 0 | 0 | ok |
| realerr | 364 | 1550405 | 364 | 0 | ok |
| broken | 505 | 1608580 | 428 | 0 | ok |

## cpp

### cpp: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 632 | 2190 | 1150 | 1140 | 1160 | 1150 | 1260 | 0.289 | 0.548 | 0.556 | 0.544 | 0.551 | 0.501 | 658 | 2240 | 1170 | 1160 | 1160 | 1280 | 0.293 | 0.562 | 0.569 | 0.567 | 0.514 | 632-677 | 1150-1240 | 1 | 1.01 | 1 | 11.8 |
| broken | cursor | all | 282 | 708 | 352 | 352 | 324 | 362 | 374 | 0.398 | 0.802 | 0.802 | 0.871 | 0.78 | 0.754 | 289 | 717 | 380 | 363 | 373 | 396 | 0.403 | 0.761 | 0.795 | 0.776 | 0.729 | 282-299 | 352-393 | 3.47 * | 5.04 * | 4.96 * | 26.5 |
| broken | parse | 1 | 586 | 1750 | 1100 | 1070 | 1070 | 1030 | 1180 | 0.334 | 0.535 | 0.546 | 0.549 | 0.567 | 0.496 | 600 | 1780 | 1120 | 1090 | 1050 | 1200 | 0.338 | 0.537 | 0.55 | 0.573 | 0.501 | 586-620 | 1100-1160 | 1 | 1 | 1 | 12.7 |
| broken | parse | all | 265 | 623 | 328 | 308 | 293 | 345 | 377 | 0.425 | 0.809 | 0.862 | 0.905 | 0.77 | 0.703 | 271 | 639 | 367 | 351 | 348 | 391 | 0.423 | 0.737 | 0.771 | 0.777 | 0.693 | 265-300 | 328-391 | 3.3 * | 4.35 * | 4.76 * | 28.1 |
| broken | walk | 1 | 634 | 2170 | 1140 | 1120 | n/a | n/a | n/a | 0.292 | 0.557 | 0.569 | n/a | n/a | n/a | 649 | 2210 | 1160 | 1130 | n/a | n/a | 0.294 | 0.558 | 0.573 | n/a | n/a | 634-667 | 1140-1230 | 1 | 1.01 | 1 | 11.8 |
| broken | walk | all | 274 | 677 | 323 | 316 | n/a | n/a | n/a | 0.405 | 0.849 | 0.867 | n/a | n/a | n/a | 290 | 705 | 373 | 361 | n/a | n/a | 0.411 | 0.777 | 0.802 | n/a | n/a | 274-316 | 323-396 | 3.44 * | 4.96 * | 4.93 * | 27.2 |
| real | cursor | 1 | 113 | 483 | 211 | 206 | 220 | 246 | 232 | 0.233 | 0.533 | 0.548 | 0.513 | 0.459 | 0.485 | 113 | 495 | 217 | 210 | 247 | 238 | 0.229 | 0.523 | 0.54 | 0.459 | 0.477 | 113-117 | 211-227 | 1 | 1.01 | 1 | 23.6 |
| real | cursor | all | 14.8 | 70.8 | 26.8 | 26.1 | 27 | 29.1 | 28.7 | 0.209 | 0.553 | 0.567 | 0.547 | 0.509 | 0.516 | 15.8 | 76.6 | 28 | 26.9 | 30.3 | 30.8 | 0.207 | 0.565 | 0.588 | 0.523 | 0.513 | 14.8-18.7 | 26.8-32.8 | 13 | 11.9 | 13.3 | 180 |
| real | parse | 1 | 92.1 | 306 | 185 | 180 | 191 | 196 | 203 | 0.301 | 0.497 | 0.513 | 0.482 | 0.47 | 0.455 | 92.6 | 313 | 187 | 181 | 198 | 206 | 0.296 | 0.496 | 0.511 | 0.469 | 0.449 | 92.1-99.2 | 185-190 | 1 | 1.01 | 1 | 28.9 |
| real | parse | all | 12.3 | 38.6 | 23 | 22.1 | 23 | 23.6 | 24.6 | 0.318 | 0.534 | 0.555 | 0.533 | 0.52 | 0.5 | 13.2 | 41.5 | 24.1 | 23.3 | 27.2 | 25.9 | 0.319 | 0.55 | 0.566 | 0.486 | 0.51 | 12.3-14.7 | 23-29.4 | 12.8 | 13.1 | 13.5 | 217 |
| real | walk | 1 | 110 | 459 | 204 | 196 | n/a | n/a | n/a | 0.24 | 0.539 | 0.56 | n/a | n/a | n/a | 111 | 471 | 206 | 200 | n/a | n/a | 0.237 | 0.541 | 0.557 | n/a | n/a | 110-116 | 204-214 | 1 | 1.01 | 1 | 24.2 |
| real | walk | all | 14.7 | 66 | 25.8 | 24.4 | n/a | n/a | n/a | 0.223 | 0.569 | 0.602 | n/a | n/a | n/a | 15.2 | 69.7 | 26.4 | 25.3 | n/a | n/a | 0.218 | 0.576 | 0.599 | n/a | n/a | 14.7-17.3 | 25.8-28.3 | 13 | 11.9 | 13.5 | 181 |
| realerr | cursor | 1 | 458 | 1920 | 965 | 941 | 975 | 1010 | 1060 | 0.238 | 0.475 | 0.487 | 0.47 | 0.452 | 0.432 | 462 | 1950 | 974 | 961 | 1010 | 1070 | 0.237 | 0.474 | 0.48 | 0.455 | 0.432 | 458-509 | 965-1010 | 1 | 1.01 | 1 | 21.1 |
| realerr | cursor | all | 62.9 | 273 | 126 | 125 | 129 | 131 | 137 | 0.23 | 0.5 | 0.505 | 0.487 | 0.479 | 0.459 | 65.7 | 278 | 129 | 129 | 133 | 138 | 0.236 | 0.508 | 0.509 | 0.494 | 0.476 | 62.9-74.9 | 126-132 | 12.7 | 12.4 | 13.3 | 154 |
| realerr | parse | 1 | 395 | 1380 | 883 | 866 | 890 | 867 | 974 | 0.287 | 0.447 | 0.456 | 0.444 | 0.455 | 0.405 | 404 | 1390 | 893 | 877 | 869 | 980 | 0.29 | 0.452 | 0.46 | 0.465 | 0.412 | 395-415 | 883-938 | 1 | 1.01 | 1 | 24.5 |
| realerr | parse | all | 54.6 | 183 | 115 | 112 | 114 | 115 | 125 | 0.298 | 0.476 | 0.488 | 0.479 | 0.476 | 0.437 | 56.1 | 186 | 118 | 115 | 117 | 127 | 0.302 | 0.475 | 0.489 | 0.478 | 0.442 | 54.6-65.7 | 115-127 | 12.7 | 12.9 | 13.3 | 178 |
| realerr | walk | 1 | 453 | 1850 | 936 | 911 | n/a | n/a | n/a | 0.245 | 0.484 | 0.497 | n/a | n/a | n/a | 458 | 1880 | 953 | 924 | n/a | n/a | 0.243 | 0.48 | 0.496 | n/a | n/a | 453-499 | 936-973 | 1 | 1.01 | 1 | 21.4 |
| realerr | walk | all | 64.4 | 258 | 123 | 119 | n/a | n/a | n/a | 0.25 | 0.522 | 0.539 | n/a | n/a | n/a | 65.9 | 261 | 125 | 123 | n/a | n/a | 0.253 | 0.525 | 0.535 | n/a | n/a | 64.4-73.2 | 123-132 | 12.4 | 12.6 | 13.3 | 151 |

### cpp: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 633 | 2200 | 1150 | 1140 | 1160 | 1150 | 1260 | 0.288 | 0.548 | 0.557 | 0.545 | 0.552 | 0.501 | 660 | 2260 | 1170 | 1160 | 1160 | 1280 | 0.292 | 0.563 | 0.571 | 0.568 | 0.515 | 633-679 | 1150-1240 |
| broken | cursor | all | 980 | 3610 | 1840 | 1750 | 1810 | 1800 | 1920 | 0.271 | 0.534 | 0.56 | 0.543 | 0.544 | 0.51 | 1010 | 3620 | 1870 | 1830 | 1840 | 1940 | 0.279 | 0.539 | 0.552 | 0.55 | 0.52 | 980-1020 | 1840-1910 |
| broken | parse | 1 | 586 | 1760 | 1100 | 1070 | 1070 | 1030 | 1180 | 0.332 | 0.535 | 0.547 | 0.55 | 0.567 | 0.497 | 601 | 1790 | 1120 | 1090 | 1050 | 1200 | 0.336 | 0.538 | 0.551 | 0.574 | 0.501 | 586-621 | 1100-1160 |
| broken | parse | all | 879 | 2740 | 1700 | 1650 | 1650 | 1650 | 1860 | 0.321 | 0.518 | 0.532 | 0.533 | 0.533 | 0.474 | 901 | 2790 | 1750 | 1710 | 1670 | 1870 | 0.323 | 0.516 | 0.528 | 0.541 | 0.482 | 879-926 | 1700-1790 |
| broken | walk | 1 | 635 | 2190 | 1140 | 1120 | n/a | n/a | n/a | 0.29 | 0.557 | 0.569 | n/a | n/a | n/a | 650 | 2230 | 1160 | 1130 | n/a | n/a | 0.292 | 0.559 | 0.574 | n/a | n/a | 635-669 | 1140-1230 |
| broken | walk | all | 958 | 3450 | 1760 | 1750 | n/a | n/a | n/a | 0.278 | 0.546 | 0.547 | n/a | n/a | n/a | 999 | 3490 | 1830 | 1780 | n/a | n/a | 0.286 | 0.547 | 0.56 | n/a | n/a | 958-1030 | 1760-1860 |
| real | cursor | 1 | 113 | 488 | 211 | 206 | 220 | 246 | 232 | 0.231 | 0.534 | 0.548 | 0.513 | 0.459 | 0.486 | 113 | 502 | 217 | 210 | 247 | 238 | 0.226 | 0.523 | 0.541 | 0.46 | 0.477 | 113-117 | 211-227 |
| real | cursor | all | 202 | 892 | 368 | 360 | 381 | 405 | 384 | 0.226 | 0.548 | 0.562 | 0.53 | 0.499 | 0.527 | 211 | 901 | 383 | 373 | 419 | 400 | 0.235 | 0.552 | 0.566 | 0.504 | 0.528 | 202-214 | 368-390 |
| real | parse | 1 | 92.2 | 308 | 185 | 180 | 191 | 196 | 203 | 0.299 | 0.497 | 0.513 | 0.482 | 0.47 | 0.455 | 92.7 | 314 | 187 | 181 | 198 | 206 | 0.295 | 0.496 | 0.512 | 0.469 | 0.449 | 92.2-99.3 | 185-190 |
| real | parse | all | 164 | 524 | 322 | 313 | 319 | 335 | 344 | 0.312 | 0.508 | 0.523 | 0.513 | 0.488 | 0.475 | 169 | 550 | 337 | 326 | 340 | 351 | 0.308 | 0.504 | 0.52 | 0.498 | 0.482 | 164-177 | 322-345 |
| real | walk | 1 | 110 | 464 | 204 | 196 | n/a | n/a | n/a | 0.237 | 0.54 | 0.561 | n/a | n/a | n/a | 112 | 476 | 206 | 200 | n/a | n/a | 0.235 | 0.541 | 0.557 | n/a | n/a | 110-116 | 204-214 |
| real | walk | all | 191 | 817 | 344 | 339 | n/a | n/a | n/a | 0.234 | 0.556 | 0.565 | n/a | n/a | n/a | 202 | 829 | 361 | 351 | n/a | n/a | 0.244 | 0.56 | 0.577 | n/a | n/a | 191-205 | 344-371 |
| realerr | cursor | 1 | 459 | 1930 | 965 | 941 | 975 | 1010 | 1060 | 0.237 | 0.475 | 0.487 | 0.47 | 0.452 | 0.432 | 462 | 1960 | 973 | 961 | 1010 | 1070 | 0.236 | 0.475 | 0.481 | 0.456 | 0.432 | 459-510 | 965-1010 |
| realerr | cursor | all | 813 | 3480 | 1700 | 1650 | 1730 | 1730 | 1810 | 0.234 | 0.48 | 0.492 | 0.469 | 0.471 | 0.45 | 846 | 3510 | 1720 | 1690 | 1770 | 1820 | 0.241 | 0.491 | 0.501 | 0.478 | 0.465 | 813-855 | 1700-1730 |
| realerr | parse | 1 | 395 | 1380 | 883 | 866 | 890 | 867 | 974 | 0.286 | 0.447 | 0.456 | 0.444 | 0.456 | 0.406 | 404 | 1400 | 893 | 877 | 869 | 980 | 0.289 | 0.453 | 0.461 | 0.465 | 0.412 | 395-416 | 883-938 |
| realerr | parse | all | 692 | 2380 | 1510 | 1510 | 1520 | 1540 | 1650 | 0.291 | 0.457 | 0.458 | 0.454 | 0.45 | 0.42 | 725 | 2420 | 1570 | 1530 | 1560 | 1660 | 0.299 | 0.461 | 0.474 | 0.466 | 0.437 | 692-735 | 1510-1590 |
| realerr | walk | 1 | 453 | 1860 | 936 | 911 | n/a | n/a | n/a | 0.244 | 0.484 | 0.497 | n/a | n/a | n/a | 458 | 1890 | 953 | 924 | n/a | n/a | 0.242 | 0.481 | 0.496 | n/a | n/a | 453-499 | 936-973 |
| realerr | walk | all | 794 | 3240 | 1660 | 1590 | n/a | n/a | n/a | 0.245 | 0.478 | 0.5 | n/a | n/a | n/a | 833 | 3290 | 1670 | 1620 | n/a | n/a | 0.253 | 0.497 | 0.512 | n/a | n/a | 794-850 | 1660-1700 |

### cpp: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 13500 | 37500 | 19600 | 18900 | 18800 | 19700 | 20400 | 0.361 | 0.691 | 0.714 | 0.719 | 0.686 | 0.664 | 13500 | 37500 | 19600 | 18900 | 19700 | 20400 | 0.36 | 0.691 | 0.714 | 0.686 | 0.664 | 13500-13500 | 19600-19600 |
| broken | cursor | all | 13500 | 37500 | 19600 | 19000 | 18800 | 19700 | 20400 | 0.361 | 0.692 | 0.715 | 0.72 | 0.686 | 0.665 | 13600 | 37600 | 19600 | 19000 | 19800 | 20400 | 0.361 | 0.692 | 0.716 | 0.687 | 0.666 | 13500-13800 | 19600-19600 |
| broken | parse | 1 | 12400 | 28400 | 18300 | 17600 | 17400 | 18300 | 19000 | 0.435 | 0.676 | 0.701 | 0.708 | 0.676 | 0.651 | 12400 | 28400 | 18300 | 17600 | 18300 | 19000 | 0.435 | 0.676 | 0.701 | 0.676 | 0.651 | 12400-12400 | 18300-18300 |
| broken | parse | all | 12400 | 28400 | 18200 | 17600 | 17400 | 18300 | 19000 | 0.436 | 0.679 | 0.704 | 0.711 | 0.678 | 0.652 | 12400 | 28400 | 18300 | 17600 | 18300 | 19000 | 0.435 | 0.678 | 0.702 | 0.678 | 0.653 | 12400-12400 | 18200-18300 |
| broken | walk | 1 | 13500 | 35400 | 19100 | 18500 | n/a | n/a | n/a | 0.382 | 0.706 | 0.73 | n/a | n/a | n/a | 13500 | 35500 | 19200 | 18500 | n/a | n/a | 0.381 | 0.706 | 0.731 | n/a | n/a | 13500-13500 | 19100-19200 |
| broken | walk | all | 13600 | 35600 | 19100 | 18500 | n/a | n/a | n/a | 0.382 | 0.71 | 0.735 | n/a | n/a | n/a | 13600 | 35600 | 19200 | 18500 | n/a | n/a | 0.381 | 0.709 | 0.734 | n/a | n/a | 13600-13600 | 19100-19200 |
| real | cursor | 1 | 2450 | 8800 | 3780 | 3560 | 3730 | 3990 | 3970 | 0.278 | 0.647 | 0.688 | 0.656 | 0.613 | 0.616 | 2450 | 8810 | 3780 | 3560 | 3990 | 3980 | 0.278 | 0.647 | 0.688 | 0.613 | 0.616 | 2450-2450 | 3780-3790 |
| real | cursor | all | 2450 | 8850 | 3790 | 3550 | 3730 | 4000 | 3980 | 0.277 | 0.646 | 0.689 | 0.656 | 0.612 | 0.615 | 2450 | 8890 | 3790 | 3570 | 4000 | 3990 | 0.275 | 0.645 | 0.686 | 0.611 | 0.614 | 2450-2450 | 3790-3800 |
| real | parse | 1 | 1950 | 5020 | 3200 | 2970 | 3130 | 3350 | 3360 | 0.389 | 0.611 | 0.657 | 0.625 | 0.584 | 0.582 | 1950 | 5020 | 3220 | 2990 | 3360 | 3380 | 0.389 | 0.608 | 0.653 | 0.581 | 0.579 | 1950-1950 | 3200-3220 |
| real | parse | all | 1950 | 4970 | 3220 | 2990 | 3150 | 3360 | 3380 | 0.394 | 0.608 | 0.654 | 0.62 | 0.581 | 0.578 | 1950 | 5030 | 3230 | 3000 | 3370 | 3400 | 0.389 | 0.606 | 0.651 | 0.58 | 0.576 | 1950-1990 | 3220-3250 |
| real | walk | 1 | 2360 | 7800 | 3510 | 3280 | n/a | n/a | n/a | 0.303 | 0.674 | 0.72 | n/a | n/a | n/a | 2370 | 7810 | 3530 | 3300 | n/a | n/a | 0.303 | 0.67 | 0.716 | n/a | n/a | 2360-2370 | 3510-3530 |
| real | walk | all | 2370 | 7820 | 3530 | 3310 | n/a | n/a | n/a | 0.302 | 0.67 | 0.715 | n/a | n/a | n/a | 2370 | 7910 | 3540 | 3310 | n/a | n/a | 0.299 | 0.667 | 0.714 | n/a | n/a | 2370-2370 | 3530-3560 |
| realerr | cursor | 1 | 9410 | 33700 | 16800 | 16000 | 16500 | 17300 | 17500 | 0.279 | 0.56 | 0.59 | 0.571 | 0.544 | 0.537 | 9410 | 33700 | 16800 | 16000 | 17300 | 17500 | 0.279 | 0.56 | 0.589 | 0.544 | 0.537 | 9410-9410 | 16800-16800 |
| realerr | cursor | all | 9420 | 33800 | 16800 | 16000 | 16500 | 17300 | 17500 | 0.279 | 0.561 | 0.59 | 0.573 | 0.545 | 0.538 | 9430 | 33800 | 16800 | 16000 | 17300 | 17500 | 0.279 | 0.561 | 0.59 | 0.545 | 0.538 | 9420-9450 | 16800-16800 |
| realerr | parse | 1 | 7880 | 22100 | 15100 | 14200 | 14700 | 15400 | 15700 | 0.357 | 0.523 | 0.553 | 0.536 | 0.512 | 0.502 | 7880 | 22100 | 15100 | 14300 | 15400 | 15700 | 0.357 | 0.522 | 0.553 | 0.512 | 0.502 | 7880-7880 | 15100-15100 |
| realerr | parse | all | 7890 | 22000 | 15100 | 14200 | 14700 | 15400 | 15700 | 0.359 | 0.524 | 0.554 | 0.537 | 0.514 | 0.503 | 7900 | 22100 | 15100 | 14300 | 15400 | 15700 | 0.358 | 0.524 | 0.554 | 0.513 | 0.503 | 7890-7920 | 15100-15100 |
| realerr | walk | 1 | 9210 | 30700 | 16100 | 15300 | n/a | n/a | n/a | 0.3 | 0.572 | 0.603 | n/a | n/a | n/a | 9210 | 30700 | 16100 | 15300 | n/a | n/a | 0.3 | 0.572 | 0.603 | n/a | n/a | 9210-9210 | 16100-16100 |
| realerr | walk | all | 9220 | 30700 | 16100 | 15200 | n/a | n/a | n/a | 0.3 | 0.574 | 0.605 | n/a | n/a | n/a | 9230 | 30800 | 16100 | 15300 | n/a | n/a | 0.299 | 0.573 | 0.604 | n/a | n/a | 9220-9250 | 16100-16100 |

### cpp: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 3300 | 11400 | 5990 | 5890 | 6020 | 5940 | 6550 | 0.289 | 0.551 | 0.56 | 0.548 | 0.556 | 0.504 | 3430 | 11600 | 6070 | 5990 | 6020 | 6650 | 0.296 | 0.565 | 0.573 | 0.57 | 0.516 | 3300-3530 | 5990-6430 |
| broken | cursor | all | 4610 | 16300 | 8530 | 8220 | 8320 | 8300 | 8900 | 0.282 | 0.54 | 0.561 | 0.554 | 0.555 | 0.518 | 4720 | 16400 | 8580 | 8450 | 8450 | 8950 | 0.287 | 0.551 | 0.559 | 0.559 | 0.528 | 4610-4760 | 8530-8710 |
| broken | parse | 1 | 3060 | 9160 | 5670 | 5550 | 5520 | 5340 | 6110 | 0.334 | 0.539 | 0.551 | 0.554 | 0.572 | 0.5 | 3130 | 9280 | 5790 | 5650 | 5420 | 6210 | 0.338 | 0.541 | 0.555 | 0.578 | 0.504 | 3060-3240 | 5670-5990 |
| broken | parse | all | 4170 | 12600 | 7920 | 7750 | 7690 | 7590 | 8580 | 0.331 | 0.527 | 0.538 | 0.543 | 0.55 | 0.487 | 4240 | 12800 | 8040 | 7910 | 7670 | 8600 | 0.331 | 0.527 | 0.536 | 0.552 | 0.493 | 4170-4360 | 7920-8210 |
| broken | walk | 1 | 3310 | 11300 | 5910 | 5780 | n/a | n/a | n/a | 0.294 | 0.561 | 0.573 | n/a | n/a | n/a | 3390 | 11400 | 6030 | 5860 | n/a | n/a | 0.297 | 0.562 | 0.578 | n/a | n/a | 3310-3480 | 5910-6350 |
| broken | walk | all | 4510 | 15800 | 8180 | 8190 | n/a | n/a | n/a | 0.285 | 0.551 | 0.55 | n/a | n/a | n/a | 4690 | 15900 | 8400 | 8270 | n/a | n/a | 0.295 | 0.558 | 0.567 | n/a | n/a | 4510-4860 | 8180-8500 |
| real | cursor | 1 | 588 | 2530 | 1100 | 1070 | 1150 | 1280 | 1210 | 0.233 | 0.533 | 0.548 | 0.512 | 0.458 | 0.485 | 591 | 2580 | 1130 | 1090 | 1290 | 1240 | 0.229 | 0.522 | 0.54 | 0.459 | 0.476 | 588-610 | 1100-1190 |
| real | cursor | all | 920 | 3970 | 1680 | 1650 | 1730 | 1870 | 1760 | 0.232 | 0.547 | 0.558 | 0.531 | 0.493 | 0.523 | 952 | 4000 | 1720 | 1700 | 1910 | 1810 | 0.238 | 0.553 | 0.561 | 0.499 | 0.527 | 920-963 | 1680-1750 |
| real | parse | 1 | 481 | 1600 | 967 | 937 | 997 | 1020 | 1060 | 0.3 | 0.497 | 0.513 | 0.482 | 0.47 | 0.455 | 484 | 1630 | 976 | 946 | 1030 | 1080 | 0.296 | 0.496 | 0.511 | 0.469 | 0.449 | 481-516 | 967-987 |
| real | parse | all | 745 | 2350 | 1470 | 1430 | 1470 | 1550 | 1560 | 0.317 | 0.507 | 0.52 | 0.508 | 0.482 | 0.477 | 778 | 2450 | 1520 | 1490 | 1560 | 1590 | 0.317 | 0.513 | 0.523 | 0.498 | 0.489 | 745-798 | 1470-1560 |
| real | walk | 1 | 574 | 2400 | 1060 | 1030 | n/a | n/a | n/a | 0.239 | 0.539 | 0.56 | n/a | n/a | n/a | 581 | 2450 | 1080 | 1050 | n/a | n/a | 0.237 | 0.54 | 0.556 | n/a | n/a | 574-607 | 1060-1120 |
| real | walk | all | 884 | 3650 | 1550 | 1570 | n/a | n/a | n/a | 0.242 | 0.568 | 0.564 | n/a | n/a | n/a | 921 | 3690 | 1630 | 1600 | n/a | n/a | 0.25 | 0.564 | 0.576 | n/a | n/a | 884-938 | 1550-1660 |
| realerr | cursor | 1 | 2390 | 10000 | 5030 | 4910 | 5090 | 5290 | 5530 | 0.238 | 0.475 | 0.487 | 0.469 | 0.452 | 0.432 | 2410 | 10200 | 5080 | 5010 | 5290 | 5580 | 0.237 | 0.475 | 0.481 | 0.455 | 0.432 | 2390-2660 | 5030-5280 |
| realerr | cursor | all | 3680 | 15500 | 7650 | 7510 | 7830 | 7940 | 8220 | 0.238 | 0.481 | 0.49 | 0.469 | 0.463 | 0.448 | 3810 | 15600 | 7760 | 7640 | 8090 | 8260 | 0.244 | 0.491 | 0.498 | 0.47 | 0.461 | 3680-3850 | 7650-7790 |
| realerr | parse | 1 | 2060 | 7180 | 4600 | 4520 | 4640 | 4520 | 5080 | 0.287 | 0.447 | 0.456 | 0.444 | 0.456 | 0.405 | 2110 | 7270 | 4660 | 4570 | 4530 | 5110 | 0.29 | 0.453 | 0.461 | 0.465 | 0.412 | 2060-2170 | 4600-4890 |
| realerr | parse | all | 3140 | 10700 | 6910 | 6930 | 6960 | 7030 | 7500 | 0.293 | 0.454 | 0.453 | 0.451 | 0.447 | 0.418 | 3270 | 10900 | 7130 | 7020 | 7080 | 7530 | 0.301 | 0.459 | 0.466 | 0.462 | 0.435 | 3140-3310 | 6910-7220 |
| realerr | walk | 1 | 2360 | 9650 | 4880 | 4750 | n/a | n/a | n/a | 0.245 | 0.484 | 0.497 | n/a | n/a | n/a | 2390 | 9820 | 4970 | 4820 | n/a | n/a | 0.243 | 0.481 | 0.496 | n/a | n/a | 2360-2600 | 4880-5080 |
| realerr | walk | all | 3610 | 14500 | 7500 | 7300 | n/a | n/a | n/a | 0.249 | 0.481 | 0.494 | n/a | n/a | n/a | 3760 | 14600 | 7570 | 7430 | n/a | n/a | 0.257 | 0.497 | 0.506 | n/a | n/a | 3610-3790 | 7500-7640 |

### cpp: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1180 | 9080 | 1320 | 1320 | 1360 | 1460 | 1400 | 0.13 | 0.891 | 0.891 | 0.866 | 0.806 | 0.844 |
| broken | cursor minus parse | 1 | cycles M | 242 | 2270 | 311 | 336 | 502 | 595 | 441 | 0.107 | 0.778 | 0.721 | 0.482 | 0.407 | 0.55 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 46.8 | 437 | 59.1 | 64.2 | 96.4 | 114 | 83.4 | 0.107 | 0.793 | 0.729 | 0.486 | 0.411 | 0.561 |
| broken | cursor minus parse | all | instructions M | 1180 | 9130 | 1340 | 1380 | 1400 | 1490 | 1410 | 0.129 | 0.88 | 0.85 | 0.838 | 0.792 | 0.836 |
| broken | cursor minus parse | all | cycles M | 435 | 3720 | 608 | 470 | 633 | 705 | 319 | 0.117 | 0.715 | 0.925 | 0.688 | 0.617 | 1.36 |
| broken | cursor minus parse | all | CPU user+sys ms | 101 | 871 | 140 | 98.7 | 156 | 150 | 64.1 | 0.116 | 0.721 | 1.02 | 0.645 | 0.671 | 1.57 |
| broken | walk minus parse | 1 | instructions M | 1170 | 7000 | 891 | 891 | n/a | n/a | n/a | 0.167 | 1.32 | 1.32 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 253 | 2100 | 231 | 225 | n/a | n/a | n/a | 0.121 | 1.09 | 1.13 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 48.8 | 423 | 44 | 42.9 | n/a | n/a | n/a | 0.115 | 1.11 | 1.14 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 1200 | 7160 | 887 | 907 | n/a | n/a | n/a | 0.167 | 1.35 | 1.32 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 333 | 3190 | 254 | 437 | n/a | n/a | n/a | 0.105 | 1.31 | 0.763 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 79.1 | 711 | 60.5 | 101 | n/a | n/a | n/a | 0.111 | 1.31 | 0.781 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 493 | 3780 | 583 | 583 | 599 | 643 | 615 | 0.13 | 0.845 | 0.845 | 0.823 | 0.767 | 0.802 |
| real | cursor minus parse | 1 | cycles M | 107 | 927 | 136 | 137 | 150 | 258 | 155 | 0.116 | 0.788 | 0.784 | 0.713 | 0.415 | 0.692 |
| real | cursor minus parse | 1 | CPU user+sys ms | 20.6 | 180 | 25.9 | 26.1 | 28.8 | 49.4 | 29.6 | 0.114 | 0.794 | 0.789 | 0.716 | 0.417 | 0.695 |
| real | cursor minus parse | all | instructions M | 493 | 3880 | 571 | 563 | 576 | 636 | 600 | 0.127 | 0.864 | 0.876 | 0.856 | 0.776 | 0.822 |
| real | cursor minus parse | all | cycles M | 174 | 1620 | 212 | 214 | 264 | 320 | 195 | 0.108 | 0.823 | 0.814 | 0.66 | 0.544 | 0.895 |
| real | cursor minus parse | all | CPU user+sys ms | 38.4 | 368 | 46.2 | 47.1 | 62.4 | 69.6 | 39.4 | 0.104 | 0.831 | 0.815 | 0.616 | 0.552 | 0.974 |
| real | walk minus parse | 1 | instructions M | 411 | 2780 | 312 | 312 | n/a | n/a | n/a | 0.148 | 1.32 | 1.32 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 93.8 | 800 | 98.2 | 88.7 | n/a | n/a | n/a | 0.117 | 0.955 | 1.06 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 17.9 | 156 | 18.7 | 16.8 | n/a | n/a | n/a | 0.115 | 0.957 | 1.07 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 411 | 2860 | 314 | 318 | n/a | n/a | n/a | 0.144 | 1.31 | 1.29 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 138 | 1300 | 85.2 | 133 | n/a | n/a | n/a | 0.106 | 1.62 | 1.04 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 27.6 | 293 | 21.5 | 26.1 | n/a | n/a | n/a | 0.0942 | 1.28 | 1.06 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 1530 | 11600 | 1710 | 1710 | 1760 | 1890 | 1810 | 0.131 | 0.894 | 0.894 | 0.868 | 0.809 | 0.846 |
| realerr | cursor minus parse | 1 | cycles M | 331 | 2850 | 427 | 392 | 453 | 767 | 456 | 0.116 | 0.775 | 0.845 | 0.731 | 0.431 | 0.726 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 63.6 | 551 | 81.7 | 75.2 | 85.9 | 147 | 87 | 0.115 | 0.778 | 0.846 | 0.741 | 0.433 | 0.732 |
| realerr | cursor minus parse | all | instructions M | 1530 | 11800 | 1720 | 1720 | 1760 | 1920 | 1830 | 0.13 | 0.887 | 0.888 | 0.87 | 0.796 | 0.835 |
| realerr | cursor minus parse | all | cycles M | 539 | 4740 | 740 | 578 | 877 | 914 | 714 | 0.114 | 0.728 | 0.933 | 0.614 | 0.59 | 0.755 |
| realerr | cursor minus parse | all | CPU user+sys ms | 121 | 1100 | 182 | 144 | 211 | 189 | 158 | 0.111 | 0.669 | 0.843 | 0.576 | 0.642 | 0.769 |
| realerr | walk minus parse | 1 | instructions M | 1330 | 8650 | 1020 | 1020 | n/a | n/a | n/a | 0.154 | 1.31 | 1.31 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 304 | 2470 | 278 | 236 | n/a | n/a | n/a | 0.123 | 1.09 | 1.29 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 58.2 | 477 | 52.9 | 45.4 | n/a | n/a | n/a | 0.122 | 1.1 | 1.28 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 1330 | 8690 | 1010 | 1010 | n/a | n/a | n/a | 0.153 | 1.32 | 1.32 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 467 | 3760 | 591 | 365 | n/a | n/a | n/a | 0.124 | 0.791 | 1.28 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 102 | 861 | 149 | 78.2 | n/a | n/a | n/a | 0.119 | 0.685 | 1.31 | n/a | n/a | n/a |

### cpp: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 244 | 443 | 143 | 0.55 | 1.71 | 244 | 443 | 143 | 0.55 | 1.71 | 232 | 430 | 133 | 0.539 | 1.74 |
| broken | cursor | all | 389 | 924 | 864 | 0.421 | 0.45 | 389 | 924 | 864 | 0.421 | 0.45 | 377 | 911 | 854 | 0.413 | 0.441 |
| broken | parse | 1 | 185 | 152 | 138 | 1.22 | 1.34 | 185 | 152 | 138 | 1.22 | 1.34 | 173 | 138 | 129 | 1.25 | 1.34 |
| broken | parse | all | 327 | 879 | 849 | 0.372 | 0.386 | 327 | 879 | 849 | 0.372 | 0.386 | 315 | 865 | 839 | 0.364 | 0.375 |
| broken | walk | 1 | 195 | 571 | 147 | 0.341 | 1.33 | 195 | 571 | 147 | 0.341 | 1.33 | 182 | 557 | 137 | 0.327 | 1.33 |
| broken | walk | all | 378 | 818 | 932 | 0.461 | 0.405 | 378 | 818 | 932 | 0.461 | 0.405 | 365 | 805 | 922 | 0.454 | 0.396 |
| real | cursor | 1 | 26.8 | 48.1 | 15.8 | 0.557 | 1.7 | 26.8 | 48.1 | 15.8 | 0.557 | 1.7 | 19.2 | 39.2 | 10.9 | 0.489 | 1.76 |
| real | cursor | all | 55.2 | 95.3 | 67.9 | 0.579 | 0.813 | 55.2 | 95.3 | 67.9 | 0.579 | 0.813 | 46.9 | 86.5 | 62.8 | 0.542 | 0.746 |
| real | parse | 1 | 21.3 | 29.7 | 15.8 | 0.716 | 1.35 | 21.3 | 29.7 | 15.8 | 0.716 | 1.35 | 13.7 | 21.1 | 10.6 | 0.648 | 1.28 |
| real | parse | all | 52.2 | 80 | 66.3 | 0.653 | 0.787 | 52.2 | 80 | 66.3 | 0.653 | 0.787 | 44.1 | 71.1 | 61.3 | 0.62 | 0.719 |
| real | walk | 1 | 21.1 | 48.7 | 15.8 | 0.434 | 1.34 | 21.1 | 48.7 | 15.8 | 0.434 | 1.34 | 13.6 | 39.8 | 10.8 | 0.341 | 1.26 |
| real | walk | all | 51.6 | 95.5 | 68.2 | 0.54 | 0.756 | 51.6 | 95.5 | 68.2 | 0.54 | 0.756 | 43.3 | 86.6 | 63.2 | 0.5 | 0.685 |
| realerr | cursor | 1 | 42.6 | 108 | 27.6 | 0.393 | 1.54 | 42.6 | 108 | 27.6 | 0.394 | 1.54 | 28.1 | 92.4 | 16 | 0.305 | 1.75 |
| realerr | cursor | all | 107 | 247 | 156 | 0.433 | 0.685 | 107 | 247 | 156 | 0.433 | 0.685 | 91.9 | 231 | 144 | 0.398 | 0.639 |
| realerr | parse | 1 | 34.7 | 42.9 | 28 | 0.808 | 1.24 | 34.7 | 42.9 | 28 | 0.808 | 1.24 | 20.2 | 27.2 | 16.3 | 0.744 | 1.24 |
| realerr | parse | all | 91.5 | 193 | 155 | 0.475 | 0.589 | 91.6 | 193 | 155 | 0.475 | 0.589 | 76.7 | 177 | 144 | 0.433 | 0.534 |
| realerr | walk | 1 | 35.1 | 130 | 28.1 | 0.27 | 1.25 | 35.1 | 130 | 28.1 | 0.27 | 1.25 | 20.5 | 114 | 16.3 | 0.18 | 1.26 |
| realerr | walk | all | 99.1 | 232 | 148 | 0.428 | 0.67 | 99.2 | 232 | 148 | 0.428 | 0.67 | 84.3 | 216 | 136 | 0.39 | 0.62 |

### cpp: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 179 | 276 | 138 | 0.649 | 1.3 | 179 | 276 | 138 | 0.649 | 1.3 | 167 | 262 | 129 | 0.636 | 1.3 |
| broken | all | 398 | KILL_RSS | 1900 | n/a | 0.209 | 398 | KILL_RSS | 1900 | n/a | 0.209 | 386 | KILL_RSS | 1890 | n/a | 0.204 |
| real | 1 | 21 | 23.9 | 15.8 | 0.882 | 1.33 | 21 | 23.9 | 15.8 | 0.882 | 1.33 | 13.6 | 14.9 | 10.8 | 0.912 | 1.26 |
| real | all | 56.9 | 140 | 105 | 0.406 | 0.541 | 57 | 140 | 105 | 0.406 | 0.541 | 48.3 | 131 | 99.9 | 0.368 | 0.484 |
| realerr | 1 | 35.1 | 43.6 | 27.8 | 0.805 | 1.27 | 35.1 | 43.6 | 27.8 | 0.805 | 1.27 | 20.6 | 27.9 | 15.9 | 0.741 | 1.3 |
| realerr | all | 102 | 274 | 175 | 0.372 | 0.583 | 102 | 274 | 175 | 0.372 | 0.584 | 87.3 | 259 | 163 | 0.338 | 0.535 |

### cpp: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0629 (real), 0.0458 (realerr), 0.31 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.67 | 35100 | 7.08 | 2600 | 244000 | 2600 | 244000 | 0.000258 |
| real | steady | 0.0629 | 75.8 | 7.07 | 2600 | 244000 | 2600 | 244000 | 0 |
| realerr | cold | 4.95 | 49700 | 7.29 | 9050 | 881000 | 9050 | 881000 | 0.000547 |
| realerr | steady | 0.0458 | 8.3 | 7.26 | 9050 | 881000 | 9050 | 881000 | 0 |
| broken | cold | 6.06 | 541000 | 7.68 | 10100 | 1090000 | 10100 | 1090000 | 0.000603 |
| broken | steady | 0.31 | 169000 | 7.66 | 10100 | 1090000 | 10100 | 1090000 | 0 |

### cpp: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2060 B | init ms | 0.00976 | 0.0123 | 0.00551 | 0.797 | 1.77 | 15 |
| #0 2060 B | parse1 ms | 0.199 | 0.32 | 0.221 | 0.621 | 0.899 | 15 |
| #0 2060 B | init+parse1 ms | 0.213 | 0.332 | 0.227 | 0.64 | 0.939 | 15 |
| #0 2060 B | parse2 ms | 0.103 | 0.279 | 0.148 | 0.368 | 0.695 | 15 |
| #0 2060 B | process user+sys ms | 0.997 | 1.59 | 0.653 | 0.626 | 1.53 | 15 |
| #0 2060 B | exec to exit instr user+kernel M | 8.23 | 13.1 | 6.8 | 0.629 | 1.21 | 15 |
| #0 2060 B | exec to exit instr user M | 5.43 | 9.26 | 5.69 | 0.586 | 0.954 | 15 |
| #0 2060 B | exec to exit instr kernel M | 2.78 | 3.83 | 1.11 | 0.726 | 2.5 | 15 |
| #0 2060 B | exec to exit cycles user+kernel M | 4.57 | 7.5 | 2.94 | 0.61 | 1.56 | 15 |
| #0 2060 B | exec to exit task-clock ms | 0.93 | 1.51 | 0.57 | 0.616 | 1.63 | 15 |
| #0 2060 B | peak RSS MB | 5.29 | 6.6 | 4.52 | 0.801 | 1.17 | 15 |
| #0 2060 B | peak footprint MB | 5.32 | 6.63 | 4.48 | 0.802 | 1.19 | 15 |
| #1 3845 B | init ms | 0.00847 | 0.0121 | 0.0053 | 0.701 | 1.6 | 15 |
| #1 3845 B | parse1 ms | 0.429 | 0.656 | 0.476 | 0.653 | 0.901 | 15 |
| #1 3845 B | init+parse1 ms | 0.438 | 0.67 | 0.484 | 0.654 | 0.907 | 15 |
| #1 3845 B | parse2 ms | 0.212 | 0.56 | 0.337 | 0.378 | 0.628 | 15 |
| #1 3845 B | process user+sys ms | 1.34 | 2.27 | 1.11 | 0.59 | 1.2 | 15 |
| #1 3845 B | exec to exit instr user+kernel M | 12.6 | 21 | 12.1 | 0.599 | 1.04 | 15 |
| #1 3845 B | exec to exit instr user M | 9.43 | 16.9 | 10.9 | 0.558 | 0.864 | 15 |
| #1 3845 B | exec to exit instr kernel M | 3.14 | 4.05 | 1.23 | 0.776 | 2.56 | 15 |
| #1 3845 B | exec to exit cycles user+kernel M | 6.32 | 11.1 | 5.3 | 0.572 | 1.19 | 15 |
| #1 3845 B | exec to exit task-clock ms | 1.26 | 2.22 | 1.02 | 0.568 | 1.24 | 15 |
| #1 3845 B | peak RSS MB | 6.78 | 7.61 | 5.48 | 0.891 | 1.24 | 15 |
| #1 3845 B | peak footprint MB | 6.8 | 7.64 | 5.41 | 0.891 | 1.26 | 15 |

### cpp: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 318 | 749571 | 0 | 0 | ok |
| realerr | 371 | 2306446 | 371 | 0 | ok |
| broken | 500 | 1803485 | 472 | 0 | ok |

## csharp

### csharp: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 163 | 753 | 425 | 427 | 433 | 390 | 457 | 0.216 | 0.383 | 0.381 | 0.376 | 0.417 | 0.356 | 165 | 767 | 435 | 433 | 391 | 480 | 0.215 | 0.38 | 0.382 | 0.422 | 0.344 | 163-174 | 425-455 | 1 | 1.01 | 1 | 17.1 |
| broken | cursor | all | 23.8 | 110 | 59.9 | 62.3 | 62.7 | 53 | 66.3 | 0.217 | 0.397 | 0.382 | 0.38 | 0.449 | 0.359 | 25.2 | 112 | 64.9 | 65 | 54.6 | 67.8 | 0.225 | 0.388 | 0.387 | 0.461 | 0.371 | 23.8-26.3 | 59.9-70.4 | 11.7 | 11.6 | 11 * | 117 |
| broken | parse | 1 | 143 | 602 | 401 | 400 | 406 | 342 | 441 | 0.238 | 0.358 | 0.359 | 0.354 | 0.419 | 0.325 | 146 | 612 | 406 | 407 | 342 | 448 | 0.238 | 0.358 | 0.358 | 0.425 | 0.325 | 143-150 | 401-427 | 1 | 1.01 | 1 | 19.4 |
| broken | parse | all | 20.7 | 81.4 | 59.4 | 60.5 | 59.3 | 49.5 | 63.9 | 0.254 | 0.348 | 0.342 | 0.348 | 0.418 | 0.323 | 22.3 | 84.5 | 61.8 | 62.9 | 51.4 | 65.3 | 0.264 | 0.361 | 0.354 | 0.434 | 0.341 | 20.7-24.7 | 59.4-65 | 11.5 | 11.8 | 10.6 * | 135 |
| broken | walk | 1 | 159 | 733 | 414 | 415 | n/a | n/a | n/a | 0.217 | 0.385 | 0.385 | n/a | n/a | n/a | 162 | 743 | 426 | 417 | n/a | n/a | 0.218 | 0.38 | 0.389 | n/a | n/a | 159-168 | 414-433 | 1 | 1.01 | 1 | 17.5 |
| broken | walk | all | 22.8 | 104 | 62.3 | 58.4 | n/a | n/a | n/a | 0.219 | 0.366 | 0.39 | n/a | n/a | n/a | 24.5 | 108 | 63.2 | 63.4 | n/a | n/a | 0.228 | 0.388 | 0.387 | n/a | n/a | 22.8-26.7 | 62.3-69.5 | 11.7 | 11.7 | 10.9 * | 122 |
| real | cursor | 1 | 365 | 1540 | 717 | 717 | 764 | 778 | 796 | 0.237 | 0.51 | 0.51 | 0.479 | 0.47 | 0.459 | 373 | 1570 | 730 | 729 | 784 | 806 | 0.237 | 0.511 | 0.512 | 0.476 | 0.463 | 365-383 | 717-752 | 1 | 1.01 | 1 | 21.6 |
| real | cursor | all | 56 | 237 | 103 | 105 | 104 | 110 | 112 | 0.236 | 0.543 | 0.535 | 0.538 | 0.511 | 0.498 | 58 | 241 | 106 | 107 | 112 | 115 | 0.241 | 0.545 | 0.54 | 0.518 | 0.505 | 56-60.2 | 103-111 | 11.6 | 11.6 | 11.9 | 141 |
| real | parse | 1 | 311 | 1130 | 640 | 639 | 670 | 641 | 728 | 0.275 | 0.486 | 0.486 | 0.464 | 0.485 | 0.427 | 325 | 1150 | 646 | 651 | 642 | 730 | 0.282 | 0.503 | 0.5 | 0.507 | 0.445 | 311-351 | 640-710 | 1 | 1 | 1 | 25.4 |
| real | parse | all | 47.4 | 166 | 94.3 | 94.5 | 94.8 | 94.7 | 103 | 0.285 | 0.503 | 0.501 | 0.5 | 0.5 | 0.459 | 49.2 | 170 | 96.8 | 97.2 | 96.2 | 106 | 0.29 | 0.508 | 0.506 | 0.511 | 0.465 | 47.4-52.2 | 94.3-100 | 11.5 | 11.6 | 11.7 | 167 |
| real | walk | 1 | 351 | 1460 | 679 | 677 | n/a | n/a | n/a | 0.24 | 0.517 | 0.518 | n/a | n/a | n/a | 358 | 1480 | 686 | 684 | n/a | n/a | 0.241 | 0.521 | 0.523 | n/a | n/a | 351-392 | 679-714 | 1 | 1.01 | 1 | 22.5 |
| real | walk | all | 54.2 | 222 | 99.9 | 102 | n/a | n/a | n/a | 0.244 | 0.543 | 0.534 | n/a | n/a | n/a | 55.7 | 227 | 102 | 104 | n/a | n/a | 0.245 | 0.545 | 0.538 | n/a | n/a | 54.2-60 | 99.9-106 | 11.5 | 11.5 | 11.8 | 145 |
| realerr | cursor | 1 | 87.8 | 372 | 185 | 184 | 192 | 192 | 200 | 0.236 | 0.475 | 0.477 | 0.458 | 0.459 | 0.439 | 89.3 | 386 | 186 | 187 | 193 | 204 | 0.232 | 0.479 | 0.477 | 0.464 | 0.439 | 87.8-93.3 | 185-193 | 1 | 1.01 | 1 | 22.4 |
| realerr | cursor | all | 14.1 | 58.3 | 26.3 | 25 | 27.6 | 29.9 | 30.6 | 0.242 | 0.536 | 0.562 | 0.51 | 0.47 | 0.46 | 14.9 | 62.9 | 29.9 | 29.3 | 30.8 | 32.6 | 0.236 | 0.497 | 0.507 | 0.483 | 0.456 | 14.1-18.4 | 26.3-31.5 | 9.77 * | 10.3 * | 10.3 * | 139 |
| realerr | parse | 1 | 75.3 | 283 | 169 | 171 | 175 | 165 | 187 | 0.266 | 0.444 | 0.44 | 0.431 | 0.456 | 0.402 | 75.9 | 287 | 171 | 175 | 167 | 188 | 0.264 | 0.443 | 0.434 | 0.455 | 0.403 | 75.3-76.6 | 169-174 | 1 | 1.01 | 1 | 26.1 |
| realerr | parse | all | 11.8 | 41 | 24.6 | 25.9 | 25.9 | 22.2 | 28.2 | 0.289 | 0.481 | 0.457 | 0.457 | 0.533 | 0.42 | 12.7 | 44.3 | 26.9 | 27.1 | 25.9 | 29.1 | 0.287 | 0.473 | 0.47 | 0.491 | 0.436 | 11.8-14.3 | 24.6-28.1 | 9.74 * | 10.7 * | 10.5 * | 166 |
| realerr | walk | 1 | 85.4 | 362 | 179 | 177 | n/a | n/a | n/a | 0.236 | 0.478 | 0.483 | n/a | n/a | n/a | 88.8 | 370 | 182 | 179 | n/a | n/a | 0.24 | 0.489 | 0.497 | n/a | n/a | 85.4-90.7 | 179-188 | 1 | 1.01 | 1 | 23 |
| realerr | walk | all | 13.5 | 56.8 | 26.9 | 27.9 | n/a | n/a | n/a | 0.237 | 0.501 | 0.483 | n/a | n/a | n/a | 14.9 | 60.5 | 28.5 | 29.1 | n/a | n/a | 0.245 | 0.521 | 0.51 | n/a | n/a | 13.5-15.9 | 26.9-35.5 | 9.75 * | 10.2 * | 10.2 * | 146 |

### csharp: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 163 | 759 | 425 | 427 | 433 | 390 | 457 | 0.215 | 0.383 | 0.382 | 0.376 | 0.418 | 0.356 | 165 | 773 | 435 | 433 | 391 | 480 | 0.214 | 0.38 | 0.382 | 0.422 | 0.344 | 163-174 | 425-454 |
| broken | cursor | all | 285 | 1280 | 695 | 686 | 699 | 658 | 740 | 0.224 | 0.41 | 0.415 | 0.408 | 0.433 | 0.385 | 296 | 1310 | 709 | 705 | 674 | 756 | 0.226 | 0.417 | 0.419 | 0.439 | 0.391 | 285-300 | 695-718 |
| broken | parse | 1 | 144 | 606 | 401 | 400 | 406 | 342 | 441 | 0.237 | 0.358 | 0.359 | 0.354 | 0.42 | 0.326 | 146 | 615 | 406 | 407 | 342 | 448 | 0.237 | 0.358 | 0.358 | 0.425 | 0.325 | 144-150 | 401-427 |
| broken | parse | all | 248 | 995 | 634 | 649 | 627 | 607 | 696 | 0.249 | 0.391 | 0.382 | 0.396 | 0.409 | 0.356 | 256 | 1020 | 659 | 667 | 622 | 698 | 0.252 | 0.388 | 0.384 | 0.411 | 0.367 | 248-269 | 634-669 |
| broken | walk | 1 | 160 | 739 | 414 | 415 | n/a | n/a | n/a | 0.216 | 0.385 | 0.385 | n/a | n/a | n/a | 162 | 749 | 426 | 417 | n/a | n/a | 0.217 | 0.381 | 0.389 | n/a | n/a | 160-168 | 414-433 |
| broken | walk | all | 271 | 1230 | 672 | 669 | n/a | n/a | n/a | 0.22 | 0.403 | 0.405 | n/a | n/a | n/a | 286 | 1250 | 692 | 693 | n/a | n/a | 0.229 | 0.413 | 0.413 | n/a | n/a | 271-293 | 672-703 |
| real | cursor | 1 | 366 | 1550 | 717 | 717 | 764 | 778 | 796 | 0.236 | 0.51 | 0.51 | 0.479 | 0.47 | 0.459 | 373 | 1580 | 730 | 729 | 784 | 806 | 0.236 | 0.512 | 0.513 | 0.477 | 0.463 | 366-384 | 717-752 |
| real | cursor | all | 655 | 2760 | 1240 | 1240 | 1270 | 1300 | 1330 | 0.237 | 0.527 | 0.527 | 0.516 | 0.504 | 0.49 | 680 | 2790 | 1270 | 1280 | 1300 | 1350 | 0.244 | 0.533 | 0.53 | 0.522 | 0.504 | 655-688 | 1240-1300 |
| real | parse | 1 | 311 | 1140 | 640 | 639 | 670 | 641 | 728 | 0.274 | 0.487 | 0.487 | 0.464 | 0.486 | 0.427 | 326 | 1160 | 646 | 651 | 642 | 730 | 0.282 | 0.504 | 0.5 | 0.508 | 0.446 | 311-352 | 640-710 |
| real | parse | all | 545 | 1960 | 1120 | 1100 | 1100 | 1140 | 1170 | 0.278 | 0.485 | 0.494 | 0.493 | 0.48 | 0.464 | 569 | 1970 | 1130 | 1150 | 1140 | 1200 | 0.289 | 0.502 | 0.497 | 0.501 | 0.475 | 545-574 | 1120-1140 |
| real | walk | 1 | 351 | 1470 | 679 | 677 | n/a | n/a | n/a | 0.239 | 0.517 | 0.519 | n/a | n/a | n/a | 358 | 1490 | 686 | 684 | n/a | n/a | 0.239 | 0.521 | 0.523 | n/a | n/a | 351-392 | 679-714 |
| real | walk | all | 629 | 2550 | 1200 | 1190 | n/a | n/a | n/a | 0.247 | 0.526 | 0.526 | n/a | n/a | n/a | 649 | 2590 | 1200 | 1220 | n/a | n/a | 0.251 | 0.538 | 0.533 | n/a | n/a | 629-653 | 1200-1220 |
| realerr | cursor | 1 | 87.9 | 375 | 185 | 184 | 192 | 192 | 200 | 0.234 | 0.476 | 0.477 | 0.459 | 0.459 | 0.439 | 89.5 | 389 | 186 | 187 | 193 | 204 | 0.23 | 0.48 | 0.478 | 0.465 | 0.44 | 87.9-93.8 | 185-193 |
| realerr | cursor | all | 140 | 627 | 298 | 285 | 303 | 299 | 325 | 0.223 | 0.468 | 0.49 | 0.46 | 0.467 | 0.429 | 148 | 647 | 306 | 311 | 314 | 329 | 0.229 | 0.483 | 0.475 | 0.471 | 0.45 | 140-154 | 298-315 |
| realerr | parse | 1 | 75.3 | 285 | 169 | 171 | 175 | 165 | 187 | 0.265 | 0.445 | 0.441 | 0.431 | 0.457 | 0.403 | 75.9 | 289 | 171 | 175 | 167 | 188 | 0.263 | 0.443 | 0.435 | 0.456 | 0.403 | 75.3-76.7 | 169-174 |
| realerr | parse | all | 118 | 466 | 275 | 275 | 270 | 266 | 287 | 0.253 | 0.429 | 0.429 | 0.437 | 0.443 | 0.41 | 126 | 477 | 284 | 285 | 271 | 299 | 0.265 | 0.445 | 0.443 | 0.467 | 0.422 | 118-131 | 275-296 |
| realerr | walk | 1 | 85.5 | 365 | 179 | 177 | n/a | n/a | n/a | 0.234 | 0.478 | 0.483 | n/a | n/a | n/a | 88.9 | 373 | 182 | 179 | n/a | n/a | 0.238 | 0.489 | 0.498 | n/a | n/a | 85.5-90.8 | 179-188 |
| realerr | walk | all | 134 | 599 | 279 | 285 | n/a | n/a | n/a | 0.223 | 0.479 | 0.468 | n/a | n/a | n/a | 143 | 615 | 296 | 301 | n/a | n/a | 0.232 | 0.482 | 0.475 | n/a | n/a | 134-150 | 279-305 |

### csharp: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 3410 | 13300 | 7800 | 7570 | 7620 | 7720 | 8000 | 0.256 | 0.437 | 0.45 | 0.448 | 0.442 | 0.426 | 3410 | 13300 | 7800 | 7570 | 7720 | 8000 | 0.256 | 0.437 | 0.45 | 0.442 | 0.426 | 3410-3410 | 7800-7810 |
| broken | cursor | all | 3420 | 13300 | 7840 | 7610 | 7660 | 7760 | 8050 | 0.257 | 0.437 | 0.45 | 0.447 | 0.441 | 0.425 | 3430 | 13400 | 7850 | 7620 | 7770 | 8050 | 0.257 | 0.437 | 0.45 | 0.442 | 0.426 | 3420-3460 | 7840-7860 |
| broken | parse | 1 | 2930 | 10300 | 7270 | 7040 | 7080 | 7140 | 7450 | 0.285 | 0.402 | 0.415 | 0.413 | 0.41 | 0.393 | 2930 | 10300 | 7280 | 7050 | 7140 | 7460 | 0.285 | 0.402 | 0.415 | 0.41 | 0.392 | 2930-2930 | 7270-7280 |
| broken | parse | all | 2940 | 10200 | 7310 | 7080 | 7120 | 7180 | 7500 | 0.289 | 0.402 | 0.415 | 0.413 | 0.41 | 0.392 | 2950 | 10300 | 7320 | 7090 | 7200 | 7500 | 0.288 | 0.403 | 0.416 | 0.41 | 0.393 | 2940-3030 | 7310-7330 |
| broken | walk | 1 | 3260 | 12400 | 7530 | 7300 | n/a | n/a | n/a | 0.263 | 0.433 | 0.447 | n/a | n/a | n/a | 3260 | 12400 | 7530 | 7300 | n/a | n/a | 0.262 | 0.433 | 0.447 | n/a | n/a | 3260-3260 | 7530-7540 |
| broken | walk | all | 3280 | 12400 | 7560 | 7340 | n/a | n/a | n/a | 0.264 | 0.434 | 0.447 | n/a | n/a | n/a | 3290 | 12500 | 7580 | 7350 | n/a | n/a | 0.263 | 0.434 | 0.447 | n/a | n/a | 3280-3330 | 7560-7590 |
| real | cursor | 1 | 7960 | 27100 | 12700 | 12100 | 12500 | 13200 | 13300 | 0.294 | 0.625 | 0.656 | 0.639 | 0.601 | 0.601 | 7960 | 27100 | 12800 | 12200 | 13300 | 13300 | 0.293 | 0.624 | 0.655 | 0.6 | 0.6 | 7960-7970 | 12700-12800 |
| real | cursor | all | 7960 | 27100 | 12800 | 12200 | 12500 | 13300 | 13300 | 0.293 | 0.622 | 0.652 | 0.636 | 0.598 | 0.598 | 7960 | 27200 | 12800 | 12200 | 13300 | 13300 | 0.293 | 0.622 | 0.652 | 0.598 | 0.598 | 7960-7960 | 12800-12800 |
| real | parse | 1 | 6600 | 18400 | 11100 | 10600 | 10800 | 11500 | 11600 | 0.358 | 0.592 | 0.625 | 0.609 | 0.574 | 0.569 | 6600 | 18400 | 11200 | 10600 | 11500 | 11600 | 0.358 | 0.592 | 0.625 | 0.573 | 0.569 | 6600-6600 | 11100-11200 |
| real | parse | all | 6600 | 18400 | 11200 | 10600 | 10900 | 11600 | 11700 | 0.359 | 0.59 | 0.622 | 0.606 | 0.57 | 0.565 | 6600 | 18400 | 11200 | 10600 | 11600 | 11700 | 0.358 | 0.588 | 0.621 | 0.569 | 0.565 | 6600-6600 | 11200-11300 |
| real | walk | 1 | 7530 | 24600 | 11800 | 11200 | n/a | n/a | n/a | 0.307 | 0.636 | 0.67 | n/a | n/a | n/a | 7530 | 24600 | 11800 | 11300 | n/a | n/a | 0.306 | 0.636 | 0.67 | n/a | n/a | 7530-7530 | 11800-11900 |
| real | walk | all | 7530 | 24600 | 11900 | 11300 | n/a | n/a | n/a | 0.306 | 0.633 | 0.668 | n/a | n/a | n/a | 7530 | 24700 | 11900 | 11300 | n/a | n/a | 0.305 | 0.631 | 0.665 | n/a | n/a | 7530-7540 | 11900-12000 |
| realerr | cursor | 1 | 1870 | 6550 | 3330 | 3170 | 3250 | 3430 | 3450 | 0.286 | 0.562 | 0.591 | 0.575 | 0.545 | 0.543 | 1870 | 6550 | 3330 | 3170 | 3440 | 3450 | 0.285 | 0.561 | 0.589 | 0.544 | 0.542 | 1870-1870 | 3330-3340 |
| realerr | cursor | all | 1880 | 6530 | 3350 | 3180 | 3270 | 3450 | 3470 | 0.287 | 0.56 | 0.589 | 0.574 | 0.543 | 0.541 | 1880 | 6590 | 3350 | 3190 | 3460 | 3480 | 0.285 | 0.56 | 0.588 | 0.543 | 0.541 | 1880-1890 | 3350-3370 |
| realerr | parse | 1 | 1570 | 4660 | 3000 | 2840 | 2920 | 3080 | 3110 | 0.337 | 0.523 | 0.553 | 0.538 | 0.51 | 0.505 | 1570 | 4660 | 3010 | 2850 | 3080 | 3110 | 0.337 | 0.522 | 0.552 | 0.51 | 0.505 | 1570-1570 | 3000-3020 |
| realerr | parse | all | 1570 | 4650 | 3020 | 2860 | 2930 | 3090 | 3120 | 0.339 | 0.522 | 0.551 | 0.537 | 0.509 | 0.504 | 1580 | 4670 | 3030 | 2870 | 3110 | 3130 | 0.338 | 0.521 | 0.55 | 0.508 | 0.504 | 1570-1590 | 3020-3040 |
| realerr | walk | 1 | 1780 | 6010 | 3160 | 3000 | n/a | n/a | n/a | 0.296 | 0.564 | 0.594 | n/a | n/a | n/a | 1780 | 6020 | 3170 | 3010 | n/a | n/a | 0.296 | 0.563 | 0.593 | n/a | n/a | 1780-1780 | 3160-3170 |
| realerr | walk | all | 1790 | 6000 | 3170 | 3010 | n/a | n/a | n/a | 0.297 | 0.563 | 0.593 | n/a | n/a | n/a | 1790 | 6050 | 3190 | 3030 | n/a | n/a | 0.296 | 0.561 | 0.591 | n/a | n/a | 1790-1800 | 3170-3190 |

### csharp: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 848 | 3930 | 2220 | 2220 | 2260 | 2030 | 2380 | 0.216 | 0.383 | 0.381 | 0.375 | 0.417 | 0.356 | 863 | 4000 | 2270 | 2260 | 2040 | 2500 | 0.215 | 0.381 | 0.382 | 0.423 | 0.345 | 848-904 | 2220-2370 |
| broken | cursor | all | 1310 | 5820 | 3230 | 3210 | 3240 | 3030 | 3450 | 0.225 | 0.405 | 0.407 | 0.404 | 0.432 | 0.379 | 1360 | 5910 | 3280 | 3300 | 3090 | 3510 | 0.229 | 0.413 | 0.411 | 0.439 | 0.386 | 1310-1380 | 3230-3330 |
| broken | parse | 1 | 749 | 3140 | 2090 | 2090 | 2120 | 1780 | 2300 | 0.238 | 0.358 | 0.359 | 0.354 | 0.42 | 0.325 | 760 | 3190 | 2120 | 2120 | 1790 | 2340 | 0.238 | 0.359 | 0.358 | 0.425 | 0.325 | 749-784 | 2090-2230 |
| broken | parse | all | 1140 | 4570 | 2980 | 3050 | 2970 | 2800 | 3250 | 0.249 | 0.383 | 0.373 | 0.383 | 0.406 | 0.351 | 1180 | 4640 | 3060 | 3120 | 2870 | 3260 | 0.253 | 0.384 | 0.377 | 0.411 | 0.361 | 1140-1230 | 2980-3110 |
| broken | walk | 1 | 832 | 3830 | 2160 | 2160 | n/a | n/a | n/a | 0.217 | 0.385 | 0.385 | n/a | n/a | n/a | 846 | 3880 | 2220 | 2170 | n/a | n/a | 0.218 | 0.38 | 0.389 | n/a | n/a | 832-875 | 2160-2260 |
| broken | walk | all | 1260 | 5590 | 3130 | 3150 | n/a | n/a | n/a | 0.226 | 0.403 | 0.401 | n/a | n/a | n/a | 1310 | 5680 | 3210 | 3230 | n/a | n/a | 0.231 | 0.408 | 0.405 | n/a | n/a | 1260-1340 | 3130-3270 |
| real | cursor | 1 | 1910 | 8050 | 3740 | 3740 | 3980 | 4050 | 4150 | 0.237 | 0.511 | 0.511 | 0.48 | 0.471 | 0.46 | 1950 | 8200 | 3800 | 3800 | 4080 | 4200 | 0.237 | 0.512 | 0.513 | 0.477 | 0.463 | 1910-2000 | 3740-3920 |
| real | cursor | all | 2970 | 12200 | 5610 | 5700 | 5720 | 5920 | 6040 | 0.243 | 0.53 | 0.521 | 0.52 | 0.502 | 0.492 | 3070 | 12400 | 5720 | 5820 | 5930 | 6080 | 0.248 | 0.536 | 0.527 | 0.517 | 0.505 | 2970-3110 | 5610-5790 |
| real | parse | 1 | 1620 | 5910 | 3330 | 3340 | 3490 | 3340 | 3800 | 0.275 | 0.487 | 0.487 | 0.464 | 0.486 | 0.427 | 1700 | 6010 | 3370 | 3390 | 3350 | 3810 | 0.282 | 0.503 | 0.5 | 0.507 | 0.445 | 1620-1830 | 3330-3700 |
| real | parse | all | 2490 | 8760 | 5090 | 5080 | 5040 | 5120 | 5340 | 0.285 | 0.49 | 0.49 | 0.495 | 0.487 | 0.467 | 2580 | 8810 | 5130 | 5240 | 5150 | 5450 | 0.293 | 0.503 | 0.493 | 0.502 | 0.474 | 2490-2600 | 5090-5150 |
| real | walk | 1 | 1830 | 7640 | 3540 | 3530 | n/a | n/a | n/a | 0.24 | 0.518 | 0.519 | n/a | n/a | n/a | 1870 | 7740 | 3580 | 3560 | n/a | n/a | 0.241 | 0.522 | 0.524 | n/a | n/a | 1830-2040 | 3540-3710 |
| real | walk | all | 2870 | 11400 | 5400 | 5490 | n/a | n/a | n/a | 0.251 | 0.531 | 0.522 | n/a | n/a | n/a | 2940 | 11500 | 5440 | 5560 | n/a | n/a | 0.255 | 0.54 | 0.528 | n/a | n/a | 2870-2960 | 5400-5480 |
| realerr | cursor | 1 | 458 | 1940 | 965 | 962 | 1000 | 1000 | 1050 | 0.236 | 0.475 | 0.476 | 0.458 | 0.458 | 0.438 | 465 | 2010 | 973 | 977 | 1010 | 1060 | 0.231 | 0.478 | 0.476 | 0.462 | 0.437 | 458-488 | 965-1010 |
| realerr | cursor | all | 659 | 2840 | 1390 | 1350 | 1410 | 1410 | 1520 | 0.232 | 0.474 | 0.487 | 0.468 | 0.469 | 0.434 | 704 | 2920 | 1420 | 1460 | 1450 | 1530 | 0.241 | 0.495 | 0.484 | 0.484 | 0.46 | 659-722 | 1390-1460 |
| realerr | parse | 1 | 393 | 1480 | 885 | 892 | 913 | 861 | 976 | 0.265 | 0.444 | 0.44 | 0.431 | 0.456 | 0.403 | 396 | 1500 | 894 | 912 | 870 | 984 | 0.264 | 0.443 | 0.434 | 0.455 | 0.402 | 393-400 | 885-909 |
| realerr | parse | all | 574 | 2140 | 1270 | 1300 | 1270 | 1250 | 1360 | 0.269 | 0.451 | 0.442 | 0.453 | 0.458 | 0.421 | 601 | 2190 | 1310 | 1340 | 1260 | 1400 | 0.275 | 0.46 | 0.449 | 0.478 | 0.43 | 574-623 | 1270-1360 |
| realerr | walk | 1 | 446 | 1890 | 933 | 924 | n/a | n/a | n/a | 0.236 | 0.478 | 0.482 | n/a | n/a | n/a | 463 | 1930 | 949 | 933 | n/a | n/a | 0.24 | 0.488 | 0.497 | n/a | n/a | 446-473 | 933-981 |
| realerr | walk | all | 642 | 2710 | 1300 | 1350 | n/a | n/a | n/a | 0.236 | 0.493 | 0.475 | n/a | n/a | n/a | 680 | 2790 | 1370 | 1420 | n/a | n/a | 0.244 | 0.496 | 0.479 | n/a | n/a | 642-699 | 1300-1410 |

### csharp: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 483 | 3040 | 524 | 524 | 539 | 577 | 548 | 0.159 | 0.921 | 0.921 | 0.896 | 0.837 | 0.882 |
| broken | cursor minus parse | 1 | cycles M | 99.2 | 790 | 126 | 138 | 143 | 249 | 81.5 | 0.126 | 0.787 | 0.72 | 0.693 | 0.399 | 1.22 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 19.4 | 153 | 24.4 | 27.3 | 27.6 | 47.9 | 16 | 0.127 | 0.792 | 0.709 | 0.701 | 0.404 | 1.21 |
| broken | cursor minus parse | all | instructions M | 483 | 3140 | 529 | 522 | 539 | 581 | 550 | 0.154 | 0.912 | 0.926 | 0.895 | 0.83 | 0.878 |
| broken | cursor minus parse | all | cycles M | 170 | 1250 | 258 | 160 | 268 | 227 | 208 | 0.137 | 0.661 | 1.06 | 0.636 | 0.752 | 0.818 |
| broken | cursor minus parse | all | CPU user+sys ms | 37.1 | 280 | 60.8 | 37.7 | 71.8 | 51.5 | 43.5 | 0.132 | 0.61 | 0.986 | 0.517 | 0.72 | 0.853 |
| broken | walk minus parse | 1 | instructions M | 338 | 2180 | 257 | 257 | n/a | n/a | n/a | 0.155 | 1.32 | 1.32 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 83 | 690 | 71.9 | 77 | n/a | n/a | n/a | 0.12 | 1.15 | 1.08 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 16 | 133 | 13.7 | 14.8 | n/a | n/a | n/a | 0.12 | 1.17 | 1.08 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 337 | 2240 | 250 | 254 | n/a | n/a | n/a | 0.15 | 1.35 | 1.33 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 124 | 1020 | 154 | 94.5 | n/a | n/a | n/a | 0.121 | 0.8 | 1.31 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 22.8 | 233 | 38.3 | 19.9 | n/a | n/a | n/a | 0.0977 | 0.594 | 1.14 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1360 | 8690 | 1590 | 1590 | 1630 | 1730 | 1650 | 0.157 | 0.859 | 0.859 | 0.837 | 0.786 | 0.824 |
| real | cursor minus parse | 1 | cycles M | 285 | 2150 | 403 | 400 | 485 | 715 | 349 | 0.133 | 0.708 | 0.712 | 0.588 | 0.399 | 0.817 |
| real | cursor minus parse | 1 | CPU user+sys ms | 54.6 | 415 | 77.2 | 77.5 | 93.7 | 137 | 67.9 | 0.131 | 0.706 | 0.704 | 0.582 | 0.398 | 0.804 |
| real | cursor minus parse | all | instructions M | 1360 | 8730 | 1620 | 1600 | 1620 | 1750 | 1640 | 0.156 | 0.844 | 0.85 | 0.84 | 0.78 | 0.829 |
| real | cursor minus parse | all | cycles M | 481 | 3490 | 518 | 619 | 680 | 801 | 699 | 0.138 | 0.928 | 0.777 | 0.707 | 0.6 | 0.688 |
| real | cursor minus parse | all | CPU user+sys ms | 110 | 804 | 117 | 138 | 164 | 164 | 160 | 0.137 | 0.935 | 0.792 | 0.669 | 0.669 | 0.684 |
| real | walk minus parse | 1 | instructions M | 936 | 6150 | 694 | 694 | n/a | n/a | n/a | 0.152 | 1.35 | 1.35 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 209 | 1730 | 205 | 193 | n/a | n/a | n/a | 0.121 | 1.02 | 1.08 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 39.9 | 333 | 39 | 37.5 | n/a | n/a | n/a | 0.12 | 1.02 | 1.06 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 936 | 6220 | 714 | 675 | n/a | n/a | n/a | 0.151 | 1.31 | 1.39 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 373 | 2670 | 304 | 411 | n/a | n/a | n/a | 0.14 | 1.23 | 0.907 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 83.9 | 594 | 71.3 | 91.2 | n/a | n/a | n/a | 0.141 | 1.18 | 0.92 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 300 | 1880 | 324 | 324 | 333 | 356 | 338 | 0.159 | 0.925 | 0.925 | 0.9 | 0.841 | 0.885 |
| realerr | cursor minus parse | 1 | cycles M | 65.2 | 461 | 80.7 | 69.1 | 87.9 | 139 | 69.1 | 0.141 | 0.808 | 0.942 | 0.742 | 0.468 | 0.943 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 12.6 | 90.6 | 15.4 | 13.2 | 16.9 | 26.7 | 13.2 | 0.139 | 0.815 | 0.954 | 0.744 | 0.471 | 0.955 |
| realerr | cursor minus parse | all | instructions M | 301 | 1880 | 333 | 327 | 338 | 359 | 348 | 0.16 | 0.904 | 0.922 | 0.89 | 0.838 | 0.866 |
| realerr | cursor minus parse | all | cycles M | 84.4 | 699 | 116 | 55.5 | 138 | 151 | 155 | 0.121 | 0.727 | 1.52 | 0.614 | 0.557 | 0.544 |
| realerr | cursor minus parse | all | CPU user+sys ms | 21.6 | 161 | 23.1 | 9.61 | 33.2 | 32.5 | 38 | 0.134 | 0.932 | 2.24 | 0.649 | 0.664 | 0.568 |
| realerr | walk minus parse | 1 | instructions M | 211 | 1350 | 158 | 158 | n/a | n/a | n/a | 0.156 | 1.34 | 1.34 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 53 | 412 | 48.5 | 31.9 | n/a | n/a | n/a | 0.129 | 1.09 | 1.66 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 10.2 | 80.7 | 9.28 | 6.05 | n/a | n/a | n/a | 0.126 | 1.1 | 1.68 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 212 | 1350 | 155 | 154 | n/a | n/a | n/a | 0.156 | 1.36 | 1.37 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 67.3 | 577 | 28.2 | 51.2 | n/a | n/a | n/a | 0.117 | 2.38 | 1.31 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 15.5 | 132 | 3.82 | 10.1 | n/a | n/a | n/a | 0.117 | 4.06 | 1.54 | n/a | n/a | n/a |

### csharp: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 28.5 | 49.9 | 14.8 | 0.572 | 1.93 | 28.6 | 49.9 | 14.8 | 0.572 | 1.93 | 20.9 | 40.9 | 9.7 | 0.51 | 2.15 |
| broken | cursor | all | 73.2 | 108 | 67.8 | 0.68 | 1.08 | 73.2 | 108 | 67.8 | 0.68 | 1.08 | 64.9 | 98.5 | 62.4 | 0.658 | 1.04 |
| broken | parse | 1 | 22.6 | 22.7 | 14.8 | 0.998 | 1.53 | 22.7 | 22.7 | 14.8 | 0.999 | 1.53 | 14.9 | 13.7 | 9.73 | 1.09 | 1.54 |
| broken | parse | all | 67.5 | 97.1 | 68.5 | 0.695 | 0.986 | 67.5 | 97.1 | 68.5 | 0.695 | 0.986 | 59.2 | 88.1 | 63.1 | 0.672 | 0.939 |
| broken | walk | 1 | 22.6 | 54.4 | 14.8 | 0.416 | 1.53 | 22.6 | 54.4 | 14.8 | 0.416 | 1.53 | 15.1 | 45.4 | 9.82 | 0.332 | 1.53 |
| broken | walk | all | 66.8 | 105 | 68 | 0.633 | 0.982 | 66.8 | 105 | 68 | 0.633 | 0.982 | 58.5 | 96.5 | 62.8 | 0.606 | 0.932 |
| real | cursor | 1 | 49.4 | 78.9 | 25.6 | 0.626 | 1.93 | 49.4 | 78.9 | 25.6 | 0.626 | 1.93 | 35.8 | 64 | 15.6 | 0.56 | 2.3 |
| real | cursor | all | 101 | 155 | 104 | 0.652 | 0.97 | 101 | 155 | 104 | 0.652 | 0.971 | 86.9 | 140 | 93.8 | 0.622 | 0.926 |
| real | parse | 1 | 33.2 | 39.2 | 25.7 | 0.848 | 1.29 | 33.2 | 39.2 | 25.7 | 0.848 | 1.29 | 19.6 | 24.4 | 15.8 | 0.805 | 1.24 |
| real | parse | all | 77.2 | 147 | 106 | 0.526 | 0.73 | 77.2 | 147 | 106 | 0.526 | 0.73 | 63.3 | 132 | 95.7 | 0.481 | 0.662 |
| real | walk | 1 | 33.1 | 85.8 | 25.9 | 0.386 | 1.28 | 33.1 | 85.8 | 25.9 | 0.386 | 1.28 | 19.5 | 71 | 15.9 | 0.275 | 1.23 |
| real | walk | all | 76.6 | 157 | 116 | 0.489 | 0.66 | 76.6 | 157 | 116 | 0.489 | 0.66 | 62.6 | 142 | 106 | 0.442 | 0.591 |
| realerr | cursor | 1 | 23.9 | 54.4 | 15.5 | 0.439 | 1.54 | 23.9 | 54.4 | 15.5 | 0.439 | 1.54 | 18.7 | 47.7 | 11.4 | 0.391 | 1.65 |
| realerr | cursor | all | 64.3 | 101 | 70.7 | 0.637 | 0.909 | 64.3 | 101 | 70.7 | 0.638 | 0.91 | 58 | 94.1 | 66.4 | 0.617 | 0.873 |
| realerr | parse | 1 | 21.7 | 21.9 | 15.8 | 0.994 | 1.38 | 21.7 | 21.9 | 15.8 | 0.994 | 1.38 | 16.5 | 15.1 | 11.5 | 1.09 | 1.44 |
| realerr | parse | all | 63.7 | 89.7 | 71.8 | 0.71 | 0.888 | 63.7 | 89.8 | 71.8 | 0.71 | 0.888 | 57.3 | 83 | 67.2 | 0.689 | 0.852 |
| realerr | walk | 1 | 21.8 | 56.6 | 15.9 | 0.385 | 1.38 | 21.8 | 56.6 | 15.9 | 0.386 | 1.38 | 16.7 | 49.7 | 11.5 | 0.335 | 1.44 |
| realerr | walk | all | 63.3 | 114 | 71 | 0.556 | 0.891 | 63.3 | 114 | 71 | 0.556 | 0.891 | 56.9 | 107 | 66.6 | 0.532 | 0.854 |

### csharp: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 22.5 | 25.8 | 14.6 | 0.872 | 1.54 | 22.5 | 25.8 | 14.6 | 0.872 | 1.54 | 14.8 | 16.9 | 9.41 | 0.875 | 1.57 |
| broken | all | 71.6 | 124 | 73.4 | 0.577 | 0.976 | 71.6 | 124 | 73.4 | 0.577 | 0.976 | 63.4 | 115 | 68 | 0.55 | 0.932 |
| real | 1 | 35.1 | 42.9 | 24.8 | 0.818 | 1.42 | 35.1 | 42.9 | 24.8 | 0.818 | 1.42 | 21.4 | 28 | 14.9 | 0.765 | 1.44 |
| real | all | 81.5 | 184 | 145 | 0.444 | 0.562 | 81.5 | 184 | 145 | 0.444 | 0.563 | 67.5 | 169 | 135 | 0.4 | 0.501 |
| realerr | 1 | 21.5 | 22.1 | 15.2 | 0.977 | 1.42 | 21.6 | 22.1 | 15.2 | 0.977 | 1.42 | 16.3 | 15.3 | 11.1 | 1.07 | 1.47 |
| realerr | all | 64.1 | 156 | 103 | 0.411 | 0.623 | 64.1 | 156 | 103 | 0.411 | 0.623 | 57.7 | 149 | 98.5 | 0.387 | 0.586 |

### csharp: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 1.05 (real), 1.38 (realerr), 1.07 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 1.22 | 12000 | 7.31 | 2380 | 218000 | 2380 | 218000 | 0.000512 |
| real | steady | 1.05 | 68.8 | 7.3 | 2380 | 218000 | 2380 | 218000 | 0.000443 |
| realerr | cold | 17.8 | 179000 | 8.76 | 12700 | 1180000 | 12700 | 1180000 | 0.0014 |
| realerr | steady | 1.38 | 371 | 7.76 | 12700 | 1180000 | 12700 | 1180000 | 0.000108 |
| broken | cold | 6.76 | 20000 | 7.79 | 2940 | 283000 | 2940 | 283000 | 0.0023 |
| broken | steady | 1.07 | 113 | 7.77 | 2940 | 282000 | 2940 | 282000 | 0.000366 |

### csharp: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2048 B | init ms | 0.00944 | 0.0121 | 0.00555 | 0.78 | 1.7 | 15 |
| #0 2048 B | parse1 ms | 0.302 | 0.461 | 0.343 | 0.655 | 0.88 | 15 |
| #0 2048 B | init+parse1 ms | 0.318 | 0.474 | 0.349 | 0.67 | 0.911 | 15 |
| #0 2048 B | parse2 ms | 0.137 | 0.377 | 0.211 | 0.362 | 0.648 | 15 |
| #0 2048 B | process user+sys ms | 1.17 | 1.83 | 0.834 | 0.641 | 1.4 | 15 |
| #0 2048 B | exec to exit instr user+kernel M | 9.32 | 16.3 | 8.75 | 0.571 | 1.06 | 15 |
| #0 2048 B | exec to exit instr user M | 6.23 | 12.3 | 7.37 | 0.506 | 0.845 | 15 |
| #0 2048 B | exec to exit instr kernel M | 3.11 | 3.96 | 1.38 | 0.784 | 2.25 | 15 |
| #0 2048 B | exec to exit cycles user+kernel M | 5.45 | 8.8 | 3.92 | 0.619 | 1.39 | 15 |
| #0 2048 B | exec to exit task-clock ms | 1.11 | 1.75 | 0.75 | 0.634 | 1.48 | 15 |
| #0 2048 B | peak RSS MB | 7.33 | 8.18 | 6.36 | 0.896 | 1.15 | 15 |
| #0 2048 B | peak footprint MB | 7.36 | 8.21 | 6.36 | 0.896 | 1.16 | 15 |
| #1 2109 B | init ms | 0.0103 | 0.0128 | 0.00557 | 0.805 | 1.84 | 15 |
| #1 2109 B | parse1 ms | 0.182 | 0.239 | 0.192 | 0.76 | 0.947 | 15 |
| #1 2109 B | init+parse1 ms | 0.196 | 0.252 | 0.199 | 0.775 | 0.984 | 15 |
| #1 2109 B | parse2 ms | 0.0492 | 0.141 | 0.0859 | 0.349 | 0.573 | 15 |
| #1 2109 B | process user+sys ms | 0.967 | 1.4 | 0.571 | 0.69 | 1.69 | 15 |
| #1 2109 B | exec to exit instr user+kernel M | 5.44 | 9.04 | 4.43 | 0.602 | 1.23 | 15 |
| #1 2109 B | exec to exit instr user M | 2.54 | 5.17 | 3.12 | 0.491 | 0.815 | 15 |
| #1 2109 B | exec to exit instr kernel M | 2.89 | 3.85 | 1.32 | 0.752 | 2.2 | 15 |
| #1 2109 B | exec to exit cycles user+kernel M | 4.34 | 6.38 | 2.44 | 0.68 | 1.78 | 15 |
| #1 2109 B | exec to exit task-clock ms | 0.9 | 1.34 | 0.47 | 0.672 | 1.91 | 15 |
| #1 2109 B | peak RSS MB | 6.81 | 7.74 | 6.23 | 0.879 | 1.09 | 15 |
| #1 2109 B | peak footprint MB | 6.84 | 7.78 | 6.17 | 0.879 | 1.11 | 15 |

### csharp: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1437 | 1664971 | 0 | 0 | ok |
| realerr | 63 | 358104 | 63 | 0 | ok |
| broken | 497 | 582105 | 431 | 0 | ok |

## golang

### golang: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 212 | 947 | 421 | 426 | 423 | 468 | 0.224 | 0.504 | 0.498 | 0.502 | 0.453 | 218 | 963 | 426 | 431 | 475 | 0.226 | 0.51 | 0.505 | 0.458 | 212-225 | 421-449 | 1.01 | 1.01 | 1 | 18.8 |
| broken | cursor | all | 41 | 185 | 81.5 | 82.1 | 75.5 | 88.8 | 0.221 | 0.503 | 0.5 | 0.544 | 0.462 | 42.6 | 189 | 87.2 | 81.4 | 90.6 | 0.226 | 0.489 | 0.523 | 0.471 | 41-48.1 | 81.5-96.8 | 8.58 * | 8.51 * | 8.04 * | 97.2 |
| broken | parse | 1 | 177 | 624 | 376 | 375 | 335 | 422 | 0.283 | 0.471 | 0.471 | 0.528 | 0.419 | 182 | 637 | 382 | 343 | 426 | 0.285 | 0.476 | 0.53 | 0.426 | 177-188 | 376-393 | 1 | 1 | 1 | 22.6 |
| broken | parse | all | 36.2 | 119 | 75.8 | 74.7 | 65.1 | 85.2 | 0.303 | 0.477 | 0.484 | 0.555 | 0.425 | 37.2 | 126 | 78.6 | 66 | 87.8 | 0.295 | 0.473 | 0.563 | 0.424 | 36.2-43.8 | 75.8-82.8 | 7.99 * | 8.28 * | 8.01 * | 110 |
| broken | walk | 1 | 211 | 907 | 402 | n/a | n/a | n/a | 0.233 | 0.525 | n/a | n/a | n/a | 213 | 921 | 408 | n/a | n/a | 0.232 | 0.523 | n/a | n/a | 211-226 | 402-431 | 1 | 1.01 | 1 | 18.9 |
| broken | walk | all | 41.7 | 174 | 80.8 | n/a | n/a | n/a | 0.24 | 0.516 | n/a | n/a | n/a | 43.6 | 185 | 84.6 | n/a | n/a | 0.235 | 0.515 | n/a | n/a | 41.7-47 | 80.8-104 | 8.4 * | 8.26 * | 7.68 * | 95.7 |
| real | cursor | 1 | 389 | 1640 | 710 | 717 | 736 | 776 | 0.237 | 0.548 | 0.543 | 0.529 | 0.502 | 391 | 1680 | 727 | 739 | 776 | 0.233 | 0.538 | 0.53 | 0.504 | 389-404 | 710-760 | 1 | 1.01 | 1 | 29.6 |
| real | cursor | all | 53.2 | 244 | 97.9 | 98 | 98.4 | 106 | 0.218 | 0.544 | 0.543 | 0.541 | 0.504 | 54.3 | 251 | 101 | 101 | 108 | 0.217 | 0.539 | 0.54 | 0.504 | 53.2-60.3 | 97.9-104 | 13 | 12.2 | 12.5 | 217 |
| real | parse | 1 | 321 | 1030 | 622 | 618 | 571 | 690 | 0.31 | 0.516 | 0.52 | 0.562 | 0.465 | 339 | 1050 | 632 | 574 | 697 | 0.324 | 0.536 | 0.59 | 0.486 | 321-368 | 622-718 | 1 | 1.01 | 1 | 35.9 |
| real | parse | all | 42.8 | 141 | 85.6 | 83.3 | 77.9 | 92.4 | 0.304 | 0.5 | 0.514 | 0.549 | 0.463 | 44.7 | 145 | 88.3 | 79.7 | 93.8 | 0.308 | 0.506 | 0.561 | 0.476 | 42.8-49.5 | 85.6-92.8 | 13 | 12.6 | 12.4 | 269 |
| real | walk | 1 | 376 | 1560 | 673 | n/a | n/a | n/a | 0.241 | 0.559 | n/a | n/a | n/a | 378 | 1580 | 682 | n/a | n/a | 0.239 | 0.555 | n/a | n/a | 376-411 | 673-766 | 1 | 1.01 | 1 | 30.7 |
| real | walk | all | 50.8 | 225 | 92.1 | n/a | n/a | n/a | 0.226 | 0.552 | n/a | n/a | n/a | 53.5 | 230 | 95.1 | n/a | n/a | 0.233 | 0.562 | n/a | n/a | 50.8-57.5 | 92.1-99.7 | 13 | 12 | 12.5 | 227 |
| realerr | cursor | 1 | 45.1 | 199 | 83.5 | 83.1 | 84.3 | 93.9 | 0.227 | 0.54 | 0.543 | 0.535 | 0.48 | 46.9 | 203 | 86.9 | 84.7 | 94.4 | 0.232 | 0.54 | 0.554 | 0.497 | 45.1-49 | 83.5-94.2 | 1 | 1.01 | 1 | 21.9 |
| realerr | cursor | all | 11.4 | 52.2 | 20.7 | 21.4 | 21 | 23.7 | 0.218 | 0.549 | 0.531 | 0.54 | 0.478 | 11.9 | 55.2 | 22.1 | 22.3 | 25.1 | 0.216 | 0.539 | 0.533 | 0.475 | 11.4-13.7 | 20.7-25.8 | 4.92 * | 5.08 * | 4.95 * | 86.9 |
| realerr | parse | 1 | 37.2 | 123 | 71.7 | 72.4 | 64.5 | 84.1 | 0.303 | 0.519 | 0.514 | 0.577 | 0.442 | 37.3 | 126 | 73.3 | 65.8 | 87.2 | 0.297 | 0.509 | 0.567 | 0.428 | 37.2-37.4 | 71.7-76.1 | 1 | 1.01 | 1 | 26.5 |
| realerr | parse | all | 9.54 | 28.7 | 16.7 | 18.2 | 16.7 | 20.5 | 0.333 | 0.57 | 0.523 | 0.57 | 0.466 | 10.4 | 31.7 | 18.8 | 17 | 21.5 | 0.33 | 0.556 | 0.615 | 0.485 | 9.54-13.2 | 16.7-21.2 | 4.81 * | 5.42 * | 5.1 * | 104 |
| realerr | walk | 1 | 43.9 | 184 | 77.3 | n/a | n/a | n/a | 0.238 | 0.568 | n/a | n/a | n/a | 44.9 | 190 | 79.9 | n/a | n/a | 0.236 | 0.561 | n/a | n/a | 43.9-46.6 | 77.3-83.7 | 1 | 1.01 | 1 | 22.5 |
| realerr | walk | all | 11.2 | 49 | 20.2 | n/a | n/a | n/a | 0.228 | 0.555 | n/a | n/a | n/a | 12.1 | 52.1 | 21.6 | n/a | n/a | 0.232 | 0.558 | n/a | n/a | 11.2-14.9 | 20.2-25.3 | 4.8 * | 4.93 * | 4.94 * | 88.3 |

### golang: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 213 | 956 | 421 | 426 | 423 | 468 | 0.223 | 0.506 | 0.5 | 0.504 | 0.455 | 218 | 972 | 426 | 431 | 475 | 0.225 | 0.513 | 0.507 | 0.46 | 213-226 | 421-449 |
| broken | cursor | all | 361 | 1600 | 650 | 688 | 654 | 739 | 0.226 | 0.556 | 0.525 | 0.553 | 0.489 | 374 | 1610 | 704 | 667 | 769 | 0.232 | 0.531 | 0.561 | 0.487 | 361-379 | 650-719 |
| broken | parse | 1 | 177 | 627 | 376 | 375 | 335 | 422 | 0.282 | 0.471 | 0.471 | 0.528 | 0.419 | 182 | 640 | 382 | 343 | 426 | 0.284 | 0.477 | 0.53 | 0.427 | 177-189 | 376-393 |
| broken | parse | all | 292 | 1020 | 617 | 603 | 550 | 663 | 0.286 | 0.474 | 0.485 | 0.532 | 0.441 | 306 | 1040 | 631 | 561 | 686 | 0.295 | 0.485 | 0.545 | 0.446 | 292-312 | 617-655 |
| broken | walk | 1 | 211 | 916 | 402 | n/a | n/a | n/a | 0.23 | 0.525 | n/a | n/a | n/a | 214 | 931 | 408 | n/a | n/a | 0.229 | 0.524 | n/a | n/a | 211-226 | 402-431 |
| broken | walk | all | 357 | 1510 | 664 | n/a | n/a | n/a | 0.237 | 0.537 | n/a | n/a | n/a | 369 | 1530 | 681 | n/a | n/a | 0.242 | 0.542 | n/a | n/a | 357-380 | 664-726 |
| real | cursor | 1 | 390 | 1650 | 710 | 717 | 736 | 776 | 0.236 | 0.549 | 0.543 | 0.529 | 0.503 | 392 | 1690 | 727 | 739 | 776 | 0.232 | 0.539 | 0.531 | 0.505 | 390-404 | 710-760 |
| real | cursor | all | 690 | 3020 | 1250 | 1240 | 1220 | 1360 | 0.229 | 0.55 | 0.559 | 0.564 | 0.509 | 724 | 3060 | 1260 | 1230 | 1360 | 0.237 | 0.574 | 0.587 | 0.533 | 690-738 | 1250-1270 |
| real | parse | 1 | 321 | 1040 | 622 | 618 | 571 | 690 | 0.309 | 0.517 | 0.52 | 0.562 | 0.466 | 339 | 1050 | 632 | 574 | 697 | 0.322 | 0.537 | 0.59 | 0.487 | 321-369 | 622-718 |
| real | parse | all | 563 | 1790 | 1060 | 1060 | 953 | 1140 | 0.315 | 0.533 | 0.53 | 0.59 | 0.493 | 592 | 1820 | 1100 | 992 | 1160 | 0.326 | 0.54 | 0.597 | 0.51 | 563-611 | 1060-1100 |
| real | walk | 1 | 376 | 1570 | 673 | n/a | n/a | n/a | 0.24 | 0.559 | n/a | n/a | n/a | 379 | 1590 | 682 | n/a | n/a | 0.238 | 0.555 | n/a | n/a | 376-411 | 673-766 |
| real | walk | all | 668 | 2710 | 1170 | n/a | n/a | n/a | 0.247 | 0.571 | n/a | n/a | n/a | 700 | 2760 | 1190 | n/a | n/a | 0.254 | 0.588 | n/a | n/a | 668-707 | 1170-1200 |
| realerr | cursor | 1 | 45.1 | 201 | 83.5 | 83.1 | 84.3 | 93.9 | 0.225 | 0.541 | 0.543 | 0.536 | 0.481 | 47 | 205 | 86.9 | 84.7 | 94.4 | 0.229 | 0.541 | 0.555 | 0.498 | 45.1-49 | 83.5-94.1 |
| realerr | cursor | all | 54.6 | 272 | 104 | 108 | 104 | 118 | 0.201 | 0.524 | 0.508 | 0.525 | 0.464 | 61.2 | 283 | 111 | 114 | 122 | 0.216 | 0.552 | 0.537 | 0.501 | 54.6-65.1 | 104-117 |
| realerr | parse | 1 | 37.2 | 124 | 71.7 | 72.4 | 64.5 | 84 | 0.301 | 0.519 | 0.514 | 0.577 | 0.443 | 37.3 | 126 | 73.3 | 65.8 | 87.1 | 0.296 | 0.509 | 0.567 | 0.428 | 37.2-37.4 | 71.7-76.1 |
| realerr | parse | all | 48.2 | 167 | 92.3 | 93.9 | 84.9 | 104 | 0.288 | 0.522 | 0.514 | 0.568 | 0.463 | 50.9 | 174 | 96.8 | 86.7 | 110 | 0.294 | 0.527 | 0.588 | 0.462 | 48.2-55.1 | 92.3-100 |
| realerr | walk | 1 | 43.9 | 187 | 77.3 | n/a | n/a | n/a | 0.235 | 0.568 | n/a | n/a | n/a | 44.9 | 192 | 79.9 | n/a | n/a | 0.233 | 0.562 | n/a | n/a | 43.9-46.6 | 77.3-83.7 |
| realerr | walk | all | 55.5 | 251 | 102 | n/a | n/a | n/a | 0.221 | 0.543 | n/a | n/a | n/a | 58.8 | 260 | 108 | n/a | n/a | 0.226 | 0.545 | n/a | n/a | 55.5-68.3 | 102-112 |

### golang: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 4680 | 16900 | 7430 | 7310 | 7280 | 7800 | 0.278 | 0.629 | 0.64 | 0.643 | 0.6 | 4680 | 16900 | 7440 | 7280 | 7800 | 0.277 | 0.629 | 0.643 | 0.6 | 4680-4680 | 7430-7440 |
| broken | cursor | all | 4680 | 16900 | 7440 | 7320 | 7290 | 7810 | 0.277 | 0.629 | 0.64 | 0.642 | 0.6 | 4700 | 17000 | 7460 | 7300 | 7820 | 0.277 | 0.63 | 0.644 | 0.6 | 4680-4760 | 7440-7460 |
| broken | parse | 1 | 3830 | 10200 | 6480 | 6330 | 6220 | 6790 | 0.377 | 0.591 | 0.605 | 0.616 | 0.564 | 3830 | 10200 | 6490 | 6220 | 6800 | 0.376 | 0.591 | 0.616 | 0.564 | 3830-3830 | 6480-6490 |
| broken | parse | all | 3840 | 10200 | 6490 | 6340 | 6230 | 6810 | 0.377 | 0.591 | 0.606 | 0.616 | 0.564 | 3840 | 10200 | 6500 | 6240 | 6810 | 0.377 | 0.591 | 0.616 | 0.564 | 3840-3890 | 6490-6510 |
| broken | walk | 1 | 4640 | 15300 | 7040 | n/a | n/a | n/a | 0.303 | 0.659 | n/a | n/a | n/a | 4650 | 15300 | 7040 | n/a | n/a | 0.303 | 0.66 | n/a | n/a | 4640-4670 | 7040-7050 |
| broken | walk | all | 4680 | 15500 | 7040 | n/a | n/a | n/a | 0.303 | 0.664 | n/a | n/a | n/a | 4690 | 15600 | 7060 | n/a | n/a | 0.302 | 0.665 | n/a | n/a | 4680-4790 | 7040-7070 |
| real | cursor | 1 | 8610 | 29900 | 12400 | 12200 | 12000 | 13100 | 0.288 | 0.696 | 0.708 | 0.715 | 0.659 | 8610 | 29900 | 12400 | 12100 | 13100 | 0.288 | 0.695 | 0.714 | 0.659 | 8610-8610 | 12400-12400 |
| real | cursor | all | 8610 | 29900 | 12400 | 12200 | 12100 | 13100 | 0.288 | 0.696 | 0.708 | 0.714 | 0.658 | 8610 | 30000 | 12400 | 12100 | 13100 | 0.287 | 0.694 | 0.713 | 0.657 | 8610-8610 | 12400-12400 |
| real | parse | 1 | 6940 | 16700 | 10500 | 10200 | 9930 | 11000 | 0.416 | 0.663 | 0.681 | 0.699 | 0.628 | 6940 | 16700 | 10500 | 9940 | 11100 | 0.416 | 0.663 | 0.699 | 0.628 | 6940-6940 | 10500-10500 |
| real | parse | all | 6940 | 16600 | 10400 | 10200 | 9920 | 11000 | 0.419 | 0.666 | 0.683 | 0.7 | 0.63 | 6940 | 16700 | 10500 | 9930 | 11000 | 0.416 | 0.663 | 0.699 | 0.629 | 6940-6950 | 10400-10500 |
| real | walk | 1 | 8230 | 26200 | 11500 | n/a | n/a | n/a | 0.315 | 0.719 | n/a | n/a | n/a | 8230 | 26200 | 11500 | n/a | n/a | 0.315 | 0.718 | n/a | n/a | 8230-8230 | 11500-11500 |
| real | walk | all | 8230 | 26100 | 11400 | n/a | n/a | n/a | 0.315 | 0.722 | n/a | n/a | n/a | 8230 | 26300 | 11500 | n/a | n/a | 0.314 | 0.719 | n/a | n/a | 8230-8230 | 11400-11500 |
| realerr | cursor | 1 | 1010 | 3530 | 1440 | 1420 | 1400 | 1530 | 0.285 | 0.697 | 0.71 | 0.717 | 0.66 | 1010 | 3530 | 1450 | 1410 | 1530 | 0.285 | 0.693 | 0.712 | 0.656 | 1010-1010 | 1440-1460 |
| realerr | cursor | all | 1010 | 3530 | 1440 | 1420 | 1410 | 1530 | 0.286 | 0.699 | 0.709 | 0.714 | 0.66 | 1010 | 3540 | 1460 | 1420 | 1530 | 0.285 | 0.693 | 0.712 | 0.659 | 1010-1010 | 1440-1470 |
| realerr | parse | 1 | 812 | 1940 | 1230 | 1200 | 1170 | 1300 | 0.418 | 0.658 | 0.676 | 0.694 | 0.624 | 812 | 1940 | 1230 | 1170 | 1300 | 0.418 | 0.657 | 0.693 | 0.623 | 812-812 | 1230-1240 |
| realerr | parse | all | 812 | 1930 | 1230 | 1200 | 1170 | 1300 | 0.422 | 0.661 | 0.678 | 0.692 | 0.626 | 814 | 1940 | 1240 | 1180 | 1310 | 0.419 | 0.655 | 0.69 | 0.622 | 812-818 | 1230-1250 |
| realerr | walk | 1 | 961 | 3070 | 1350 | n/a | n/a | n/a | 0.313 | 0.714 | n/a | n/a | n/a | 961 | 3080 | 1350 | n/a | n/a | 0.312 | 0.713 | n/a | n/a | 961-961 | 1350-1350 |
| realerr | walk | all | 962 | 3070 | 1340 | n/a | n/a | n/a | 0.313 | 0.717 | n/a | n/a | n/a | 963 | 3080 | 1350 | n/a | n/a | 0.312 | 0.713 | n/a | n/a | 962-971 | 1340-1360 |

### golang: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1110 | 4920 | 2190 | 2210 | 2200 | 2440 | 0.225 | 0.505 | 0.5 | 0.504 | 0.454 | 1130 | 5010 | 2210 | 2240 | 2470 | 0.226 | 0.513 | 0.506 | 0.459 | 1110-1170 | 2190-2340 |
| broken | cursor | all | 1660 | 7240 | 3020 | 3170 | 3020 | 3430 | 0.23 | 0.551 | 0.525 | 0.55 | 0.486 | 1730 | 7280 | 3240 | 3090 | 3510 | 0.237 | 0.533 | 0.56 | 0.491 | 1660-1750 | 3020-3290 |
| broken | parse | 1 | 923 | 3250 | 1950 | 1950 | 1740 | 2200 | 0.284 | 0.473 | 0.473 | 0.53 | 0.421 | 950 | 3320 | 1980 | 1780 | 2220 | 0.286 | 0.478 | 0.532 | 0.428 | 923-979 | 1950-2040 |
| broken | parse | all | 1370 | 4670 | 2850 | 2790 | 2570 | 3110 | 0.293 | 0.48 | 0.49 | 0.533 | 0.44 | 1430 | 4730 | 2920 | 2590 | 3170 | 0.301 | 0.489 | 0.55 | 0.45 | 1370-1450 | 2850-2970 |
| broken | walk | 1 | 1100 | 4710 | 2090 | n/a | n/a | n/a | 0.234 | 0.527 | n/a | n/a | n/a | 1110 | 4780 | 2120 | n/a | n/a | 0.233 | 0.525 | n/a | n/a | 1100-1180 | 2090-2230 |
| broken | walk | all | 1670 | 6840 | 3060 | n/a | n/a | n/a | 0.243 | 0.543 | n/a | n/a | n/a | 1710 | 6880 | 3130 | n/a | n/a | 0.249 | 0.547 | n/a | n/a | 1670-1760 | 3060-3370 |
| real | cursor | 1 | 2030 | 8590 | 3710 | 3750 | 3840 | 4050 | 0.237 | 0.548 | 0.543 | 0.529 | 0.502 | 2040 | 8780 | 3790 | 3860 | 4050 | 0.233 | 0.538 | 0.53 | 0.504 | 2030-2110 | 3710-3960 |
| real | cursor | all | 3110 | 13400 | 5690 | 5590 | 5590 | 6120 | 0.232 | 0.547 | 0.557 | 0.557 | 0.508 | 3240 | 13600 | 5710 | 5600 | 6150 | 0.239 | 0.568 | 0.579 | 0.528 | 3110-3270 | 5690-5730 |
| real | parse | 1 | 1680 | 5410 | 3250 | 3230 | 2980 | 3600 | 0.31 | 0.516 | 0.52 | 0.562 | 0.465 | 1770 | 5470 | 3290 | 3000 | 3640 | 0.324 | 0.538 | 0.59 | 0.486 | 1680-1910 | 3250-3750 |
| real | parse | all | 2550 | 8050 | 4840 | 4840 | 4360 | 5220 | 0.316 | 0.526 | 0.527 | 0.584 | 0.488 | 2650 | 8150 | 5000 | 4510 | 5300 | 0.325 | 0.531 | 0.588 | 0.501 | 2550-2740 | 4840-5040 |
| real | walk | 1 | 1960 | 8130 | 3510 | n/a | n/a | n/a | 0.241 | 0.559 | n/a | n/a | n/a | 1980 | 8250 | 3560 | n/a | n/a | 0.24 | 0.555 | n/a | n/a | 1960-2150 | 3510-4000 |
| real | walk | all | 3030 | 12100 | 5350 | n/a | n/a | n/a | 0.25 | 0.567 | n/a | n/a | n/a | 3140 | 12300 | 5430 | n/a | n/a | 0.256 | 0.579 | n/a | n/a | 3030-3160 | 5350-5450 |
| realerr | cursor | 1 | 235 | 1040 | 436 | 434 | 440 | 490 | 0.227 | 0.54 | 0.543 | 0.535 | 0.48 | 245 | 1050 | 453 | 442 | 493 | 0.233 | 0.54 | 0.554 | 0.497 | 235-253 | 436-491 |
| realerr | cursor | all | 282 | 1310 | 530 | 549 | 537 | 598 | 0.214 | 0.532 | 0.513 | 0.525 | 0.472 | 312 | 1360 | 561 | 574 | 618 | 0.229 | 0.556 | 0.543 | 0.505 | 282-331 | 530-588 |
| realerr | parse | 1 | 194 | 641 | 374 | 378 | 337 | 438 | 0.303 | 0.519 | 0.514 | 0.576 | 0.443 | 195 | 655 | 382 | 344 | 455 | 0.297 | 0.509 | 0.567 | 0.428 | 194-195 | 374-397 |
| realerr | parse | all | 242 | 817 | 471 | 473 | 424 | 539 | 0.296 | 0.514 | 0.512 | 0.571 | 0.45 | 256 | 852 | 490 | 437 | 550 | 0.3 | 0.522 | 0.585 | 0.464 | 242-280 | 471-506 |
| realerr | walk | 1 | 229 | 956 | 403 | n/a | n/a | n/a | 0.239 | 0.568 | n/a | n/a | n/a | 234 | 984 | 417 | n/a | n/a | 0.238 | 0.562 | n/a | n/a | 229-240 | 403-437 |
| realerr | walk | all | 279 | 1210 | 519 | n/a | n/a | n/a | 0.23 | 0.538 | n/a | n/a | n/a | 298 | 1240 | 537 | n/a | n/a | 0.24 | 0.555 | n/a | n/a | 279-339 | 519-555 |

### golang: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 848 | 6690 | 951 | 979 | 1060 | 1010 | 0.127 | 0.892 | 0.866 | 0.803 | 0.843 |
| broken | cursor minus parse | 1 | cycles M | 183 | 1670 | 235 | 260 | 453 | 241 | 0.11 | 0.778 | 0.703 | 0.404 | 0.76 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 36.2 | 329 | 45.7 | 50.5 | 87.8 | 46.1 | 0.11 | 0.79 | 0.716 | 0.412 | 0.784 |
| broken | cursor minus parse | all | instructions M | 845 | 6740 | 953 | 986 | 1060 | 1010 | 0.125 | 0.886 | 0.857 | 0.8 | 0.84 |
| broken | cursor minus parse | all | cycles M | 295 | 2560 | 173 | 377 | 454 | 319 | 0.115 | 1.7 | 0.783 | 0.65 | 0.925 |
| broken | cursor minus parse | all | CPU user+sys ms | 69 | 577 | 32.4 | 85.1 | 104 | 75.7 | 0.12 | 2.13 | 0.811 | 0.663 | 0.912 |
| broken | walk minus parse | 1 | instructions M | 813 | 5130 | 559 | n/a | n/a | n/a | 0.158 | 1.45 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 178 | 1460 | 138 | n/a | n/a | n/a | 0.122 | 1.29 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 34.1 | 289 | 26.1 | n/a | n/a | n/a | 0.118 | 1.31 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 840 | 5270 | 554 | n/a | n/a | n/a | 0.159 | 1.52 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 296 | 2170 | 215 | n/a | n/a | n/a | 0.136 | 1.38 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 64.4 | 485 | 46.7 | n/a | n/a | n/a | 0.133 | 1.38 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1670 | 13200 | 1910 | 1960 | 2110 | 2010 | 0.126 | 0.875 | 0.851 | 0.791 | 0.829 |
| real | cursor minus parse | 1 | cycles M | 358 | 3180 | 461 | 520 | 861 | 449 | 0.112 | 0.775 | 0.688 | 0.415 | 0.796 |
| real | cursor minus parse | 1 | CPU user+sys ms | 68.6 | 613 | 88.5 | 99.9 | 165 | 86 | 0.112 | 0.775 | 0.687 | 0.416 | 0.798 |
| real | cursor minus parse | all | instructions M | 1670 | 13300 | 1940 | 2000 | 2140 | 2080 | 0.125 | 0.86 | 0.836 | 0.779 | 0.804 |
| real | cursor minus parse | all | cycles M | 564 | 5360 | 842 | 751 | 1230 | 900 | 0.105 | 0.669 | 0.75 | 0.459 | 0.627 |
| real | cursor minus parse | all | CPU user+sys ms | 127 | 1230 | 198 | 174 | 270 | 216 | 0.104 | 0.644 | 0.734 | 0.471 | 0.591 |
| real | walk minus parse | 1 | instructions M | 1290 | 9480 | 986 | n/a | n/a | n/a | 0.136 | 1.31 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 288 | 2730 | 267 | n/a | n/a | n/a | 0.106 | 1.08 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 55.3 | 530 | 51.3 | n/a | n/a | n/a | 0.104 | 1.08 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1290 | 9580 | 982 | n/a | n/a | n/a | 0.135 | 1.32 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 481 | 4060 | 502 | n/a | n/a | n/a | 0.118 | 0.959 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 106 | 920 | 115 | n/a | n/a | n/a | 0.115 | 0.921 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 195 | 1590 | 211 | 218 | 235 | 224 | 0.123 | 0.925 | 0.897 | 0.831 | 0.873 |
| realerr | cursor minus parse | 1 | cycles M | 41.2 | 394 | 61.3 | 55.7 | 103 | 51.9 | 0.104 | 0.671 | 0.739 | 0.4 | 0.793 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 7.9 | 77.2 | 11.8 | 10.7 | 19.8 | 9.89 | 0.102 | 0.671 | 0.739 | 0.4 | 0.799 |
| realerr | cursor minus parse | all | instructions M | 195 | 1600 | 212 | 222 | 237 | 229 | 0.122 | 0.919 | 0.879 | 0.822 | 0.851 |
| realerr | cursor minus parse | all | cycles M | 39.8 | 497 | 59.2 | 76.6 | 114 | 59.1 | 0.08 | 0.672 | 0.519 | 0.351 | 0.674 |
| realerr | cursor minus parse | all | CPU user+sys ms | 6.41 | 105 | 11.9 | 13.7 | 19.1 | 13.5 | 0.061 | 0.538 | 0.469 | 0.336 | 0.474 |
| realerr | walk minus parse | 1 | instructions M | 149 | 1130 | 113 | n/a | n/a | n/a | 0.132 | 1.32 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 34.9 | 315 | 29 | n/a | n/a | n/a | 0.111 | 1.2 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 6.67 | 63.1 | 5.59 | n/a | n/a | n/a | 0.106 | 1.19 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 150 | 1150 | 113 | n/a | n/a | n/a | 0.131 | 1.33 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 37.3 | 397 | 48.3 | n/a | n/a | n/a | 0.094 | 0.773 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 7.31 | 83.8 | 9.92 | n/a | n/a | n/a | 0.0871 | 0.737 | n/a | n/a | n/a |

### golang: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 86.6 | 134 | 37.1 | 0.645 | 2.33 | 86.6 | 134 | 37.1 | 0.645 | 2.33 | 77.9 | 124 | 31 | 0.626 | 2.52 |
| broken | cursor | all | 166 | 300 | 231 | 0.552 | 0.715 | 166 | 300 | 231 | 0.552 | 0.715 | 156 | 290 | 225 | 0.539 | 0.694 |
| broken | parse | 1 | 42.5 | 39 | 29.7 | 1.09 | 1.43 | 42.5 | 39 | 29.7 | 1.09 | 1.43 | 33.9 | 29 | 23.5 | 1.17 | 1.44 |
| broken | parse | all | 111 | 236 | 184 | 0.47 | 0.602 | 111 | 236 | 184 | 0.47 | 0.602 | 101 | 226 | 178 | 0.448 | 0.569 |
| broken | walk | 1 | 71.4 | 138 | 41.9 | 0.515 | 1.7 | 71.4 | 138 | 41.9 | 0.515 | 1.7 | 62.7 | 128 | 35.6 | 0.488 | 1.76 |
| broken | walk | all | 144 | 255 | 292 | 0.565 | 0.494 | 144 | 255 | 292 | 0.565 | 0.494 | 135 | 246 | 285 | 0.55 | 0.473 |
| real | cursor | 1 | 43.4 | 103 | 25.8 | 0.423 | 1.68 | 43.4 | 103 | 25.8 | 0.423 | 1.68 | 26.4 | 84 | 12.4 | 0.314 | 2.13 |
| real | cursor | all | 98.2 | 196 | 138 | 0.502 | 0.71 | 98.2 | 196 | 138 | 0.502 | 0.71 | 80.8 | 177 | 125 | 0.457 | 0.647 |
| real | parse | 1 | 29.4 | 43.4 | 24.1 | 0.679 | 1.22 | 29.4 | 43.4 | 24.1 | 0.679 | 1.22 | 12.4 | 24.9 | 10.7 | 0.497 | 1.16 |
| real | parse | all | 81.2 | 171 | 135 | 0.476 | 0.601 | 81.2 | 171 | 135 | 0.476 | 0.601 | 64 | 152 | 122 | 0.421 | 0.526 |
| real | walk | 1 | 29.4 | 95.1 | 25.7 | 0.309 | 1.14 | 29.4 | 95.1 | 25.7 | 0.309 | 1.14 | 12.3 | 76.7 | 12.2 | 0.161 | 1.01 |
| real | walk | all | 83.3 | 198 | 135 | 0.421 | 0.615 | 83.3 | 198 | 135 | 0.421 | 0.615 | 65.9 | 179 | 122 | 0.368 | 0.541 |
| realerr | cursor | 1 | 15.8 | 56.1 | 12 | 0.282 | 1.32 | 15.9 | 56.1 | 12 | 0.283 | 1.32 | 11.5 | 50.4 | 8.66 | 0.228 | 1.33 |
| realerr | cursor | all | 54 | 97.5 | 79.9 | 0.554 | 0.676 | 54 | 97.5 | 79.9 | 0.554 | 0.676 | 48.7 | 91.4 | 76.3 | 0.532 | 0.638 |
| realerr | parse | 1 | 14.1 | 17.3 | 10.7 | 0.816 | 1.31 | 14.1 | 17.3 | 10.7 | 0.816 | 1.31 | 9.82 | 11.3 | 7.29 | 0.87 | 1.35 |
| realerr | parse | all | 52.8 | 90.6 | 72.1 | 0.583 | 0.732 | 52.8 | 90.6 | 72.1 | 0.583 | 0.732 | 47.3 | 84.8 | 68.7 | 0.558 | 0.689 |
| realerr | walk | 1 | 14.3 | 64.4 | 12.2 | 0.222 | 1.17 | 14.3 | 64.4 | 12.2 | 0.222 | 1.17 | 10 | 58.5 | 8.86 | 0.172 | 1.13 |
| realerr | walk | all | 55.3 | 97.8 | 77.4 | 0.566 | 0.715 | 55.3 | 97.8 | 77.4 | 0.566 | 0.715 | 49.9 | 92.1 | 74.1 | 0.542 | 0.674 |

### golang: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 42.1 | 39.5 | 29.6 | 1.07 | 1.42 | 42.1 | 39.5 | 29.6 | 1.07 | 1.42 | 33.3 | 29.6 | 23.5 | 1.13 | 1.41 |
| broken | all | 132 | 493 | 317 | 0.267 | 0.415 | 132 | 493 | 317 | 0.267 | 0.415 | 122 | 483 | 311 | 0.253 | 0.394 |
| real | 1 | 29.5 | 43.3 | 24.2 | 0.68 | 1.22 | 29.5 | 43.3 | 24.2 | 0.68 | 1.22 | 12.1 | 24.6 | 10.6 | 0.491 | 1.13 |
| real | all | 85.1 | 247 | 176 | 0.344 | 0.483 | 85.1 | 247 | 176 | 0.344 | 0.483 | 67.8 | 229 | 163 | 0.297 | 0.417 |
| realerr | 1 | 14.2 | 17.7 | 10.3 | 0.802 | 1.37 | 14.2 | 17.7 | 10.3 | 0.803 | 1.38 | 9.94 | 11.8 | 7.2 | 0.839 | 1.38 |
| realerr | all | 58.9 | 171 | 110 | 0.344 | 0.535 | 58.9 | 171 | 110 | 0.344 | 0.536 | 53.5 | 166 | 106 | 0.323 | 0.503 |

### golang: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.00903 (real), 0.3 (realerr), 0.102 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.168 | 9390 | 7.01 | 1990 | 186000 | 1990 | 186000 | 0 |
| real | steady | 0.00903 | 3.89 | 7.01 | 1990 | 186000 | 1990 | 186000 | 0 |
| realerr | cold | 6.77 | 142000 | 7.23 | 5570 | 532000 | 5570 | 532000 | 0.00122 |
| realerr | steady | 0.3 | 71.3 | 7.38 | 5560 | 531000 | 5560 | 531000 | 0 |
| broken | cold | 3.29 | 91100 | 7.52 | 3600 | 385000 | 3600 | 385000 | 0.000914 |
| broken | steady | 0.102 | 6370 | 7.5 | 3600 | 385000 | 3600 | 385000 | 0 |

### golang: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2052 B | init ms | 0.00906 | 0.012 | 0.00549 | 0.752 | 1.65 | 15 |
| #0 2052 B | parse1 ms | 0.162 | 0.226 | 0.169 | 0.719 | 0.963 | 15 |
| #0 2052 B | init+parse1 ms | 0.173 | 0.238 | 0.174 | 0.726 | 0.994 | 15 |
| #0 2052 B | parse2 ms | 0.0789 | 0.194 | 0.113 | 0.406 | 0.699 | 15 |
| #0 2052 B | process user+sys ms | 0.943 | 1.41 | 0.562 | 0.668 | 1.68 | 15 |
| #0 2052 B | exec to exit instr user+kernel M | 6.87 | 10.3 | 5.02 | 0.665 | 1.37 | 15 |
| #0 2052 B | exec to exit instr user M | 4.19 | 6.69 | 4.01 | 0.626 | 1.04 | 15 |
| #0 2052 B | exec to exit instr kernel M | 2.63 | 3.61 | 1 | 0.728 | 2.62 | 15 |
| #0 2052 B | exec to exit cycles user+kernel M | 4.2 | 6.48 | 2.44 | 0.648 | 1.72 | 15 |
| #0 2052 B | exec to exit task-clock ms | 0.85 | 1.35 | 0.47 | 0.63 | 1.81 | 15 |
| #0 2052 B | peak RSS MB | 3.83 | 5.02 | 2.73 | 0.763 | 1.4 | 15 |
| #0 2052 B | peak footprint MB | 3.86 | 5.05 | 2.67 | 0.764 | 1.45 | 15 |
| #1 2374 B | init ms | 0.00888 | 0.0119 | 0.00562 | 0.745 | 1.58 | 15 |
| #1 2374 B | parse1 ms | 0.226 | 0.296 | 0.218 | 0.762 | 1.03 | 15 |
| #1 2374 B | init+parse1 ms | 0.234 | 0.309 | 0.224 | 0.758 | 1.05 | 15 |
| #1 2374 B | parse2 ms | 0.115 | 0.252 | 0.146 | 0.455 | 0.784 | 15 |
| #1 2374 B | process user+sys ms | 1.05 | 1.55 | 0.672 | 0.673 | 1.56 | 15 |
| #1 2374 B | exec to exit instr user+kernel M | 7.97 | 11.9 | 5.83 | 0.671 | 1.37 | 15 |
| #1 2374 B | exec to exit instr user M | 5.22 | 8.19 | 4.81 | 0.638 | 1.08 | 15 |
| #1 2374 B | exec to exit instr kernel M | 2.73 | 3.63 | 1.01 | 0.751 | 2.7 | 15 |
| #1 2374 B | exec to exit cycles user+kernel M | 4.7 | 7.33 | 2.96 | 0.641 | 1.59 | 15 |
| #1 2374 B | exec to exit task-clock ms | 0.99 | 1.48 | 0.57 | 0.669 | 1.74 | 15 |
| #1 2374 B | peak RSS MB | 3.87 | 5.11 | 2.79 | 0.757 | 1.38 | 15 |
| #1 2374 B | peak footprint MB | 3.89 | 5.14 | 2.71 | 0.758 | 1.43 | 15 |

### golang: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1440 | 2614065 | 0 | 0 | ok |
| realerr | 60 | 313873 | 60 | 0 | ok |
| broken | 501 | 1325390 | 393 | 0 | ok |

## hcl

### hcl: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 165 | 638 | 348 | 353 | 384 | 382 | 0.259 | 0.474 | 0.468 | 0.429 | 0.432 | 166 | 655 | 349 | 391 | 387 | 0.254 | 0.476 | 0.425 | 0.429 | 165-178 | 348-363 | 1 | 1.01 | 1 | 9.67 |
| broken | cursor | all | 24.2 | 90.7 | 47.9 | 47.7 | 48.7 | 52.4 | 0.267 | 0.506 | 0.508 | 0.498 | 0.463 | 25.3 | 94.1 | 49.8 | 49.5 | 53.6 | 0.269 | 0.508 | 0.511 | 0.472 | 24.2-27 | 47.9-54 | 12.3 | 11.9 | 11.8 | 65.8 |
| broken | parse | 1 | 157 | 564 | 335 | 339 | 361 | 371 | 0.278 | 0.469 | 0.464 | 0.435 | 0.423 | 159 | 571 | 340 | 372 | 376 | 0.279 | 0.469 | 0.428 | 0.424 | 157-179 | 335-350 | 1 | 1.01 | 1 | 10.2 |
| broken | parse | all | 22.7 | 77.9 | 45.6 | 46.2 | 46.2 | 50.8 | 0.292 | 0.498 | 0.492 | 0.492 | 0.448 | 24.2 | 81.2 | 47.9 | 48 | 51.7 | 0.298 | 0.505 | 0.504 | 0.468 | 22.7-24.8 | 45.6-51.1 | 12.3 | 12.1 | 11.8 | 70.2 |
| broken | walk | 1 | 194 | 800 | 385 | n/a | n/a | n/a | 0.243 | 0.505 | n/a | n/a | n/a | 195 | 809 | 388 | n/a | n/a | 0.241 | 0.503 | n/a | n/a | 194-201 | 385-396 | 1 | 1.01 | 1 | 8.21 |
| broken | walk | all | 32.7 | 144 | 56.7 | n/a | n/a | n/a | 0.227 | 0.577 | n/a | n/a | n/a | 35.1 | 146 | 58.1 | n/a | n/a | 0.24 | 0.604 | n/a | n/a | 32.7-39 | 56.7-61.4 | 10.5 * | 8.82 * | 11.2 | 48.7 |
| real | cursor | 1 | 343 | 1350 | 685 | 697 | 800 | 742 | 0.253 | 0.501 | 0.492 | 0.429 | 0.463 | 346 | 1380 | 690 | 801 | 754 | 0.25 | 0.501 | 0.432 | 0.459 | 343-377 | 685-718 | 1 | 1.01 | 1 | 13.5 |
| real | cursor | all | 45.8 | 182 | 81.5 | 81.1 | 85.1 | 88.4 | 0.252 | 0.562 | 0.564 | 0.538 | 0.518 | 47.5 | 183 | 83.5 | 88.7 | 92.6 | 0.259 | 0.569 | 0.536 | 0.514 | 45.8-52.5 | 81.5-87.7 | 14 | 13.5 | 14.6 | 101 |
| real | parse | 1 | 320 | 1130 | 630 | 644 | 720 | 685 | 0.284 | 0.508 | 0.497 | 0.445 | 0.468 | 324 | 1150 | 640 | 722 | 696 | 0.282 | 0.506 | 0.449 | 0.466 | 320-346 | 630-677 | 1 | 1 | 1 | 14.4 |
| real | parse | all | 42.6 | 141 | 76.4 | 74.9 | 77.2 | 82.8 | 0.302 | 0.557 | 0.568 | 0.551 | 0.514 | 43.8 | 145 | 78.9 | 80.4 | 83.7 | 0.303 | 0.555 | 0.545 | 0.524 | 42.6-45.6 | 76.4-83 | 14.2 | 13.9 | 14.4 | 108 |
| real | walk | 1 | 338 | 1320 | 648 | n/a | n/a | n/a | 0.257 | 0.522 | n/a | n/a | n/a | 340 | 1350 | 656 | n/a | n/a | 0.251 | 0.518 | n/a | n/a | 338-356 | 648-678 | 1 | 1.01 | 1 | 13.7 |
| real | walk | all | 44.9 | 173 | 79.1 | n/a | n/a | n/a | 0.26 | 0.568 | n/a | n/a | n/a | 45.9 | 175 | 81.8 | n/a | n/a | 0.262 | 0.561 | n/a | n/a | 44.9-47.9 | 79.1-87 | 14.3 | 13.5 | 14.3 | 103 |

### hcl: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 165 | 644 | 348 | 353 | 384 | 382 | 0.257 | 0.475 | 0.468 | 0.43 | 0.432 | 166 | 660 | 349 | 391 | 387 | 0.252 | 0.476 | 0.426 | 0.429 | 165-178 | 348-363 |
| broken | cursor | all | 298 | 1100 | 564 | 564 | 580 | 603 | 0.271 | 0.528 | 0.528 | 0.513 | 0.494 | 314 | 1120 | 591 | 592 | 616 | 0.281 | 0.532 | 0.531 | 0.51 | 298-318 | 564-597 |
| broken | parse | 1 | 157 | 568 | 335 | 338 | 361 | 371 | 0.277 | 0.47 | 0.464 | 0.436 | 0.423 | 160 | 574 | 340 | 372 | 376 | 0.278 | 0.469 | 0.429 | 0.424 | 157-179 | 335-350 |
| broken | parse | all | 283 | 964 | 544 | 548 | 553 | 579 | 0.293 | 0.52 | 0.516 | 0.511 | 0.488 | 296 | 985 | 570 | 565 | 596 | 0.3 | 0.519 | 0.523 | 0.496 | 283-306 | 544-578 |
| broken | walk | 1 | 195 | 807 | 384 | n/a | n/a | n/a | 0.241 | 0.506 | n/a | n/a | n/a | 195 | 819 | 388 | n/a | n/a | 0.238 | 0.503 | n/a | n/a | 195-201 | 384-396 |
| broken | walk | all | 357 | 1280 | 641 | n/a | n/a | n/a | 0.28 | 0.557 | n/a | n/a | n/a | 370 | 1290 | 657 | n/a | n/a | 0.286 | 0.563 | n/a | n/a | 357-384 | 641-661 |
| real | cursor | 1 | 343 | 1360 | 685 | 697 | 800 | 742 | 0.252 | 0.501 | 0.492 | 0.429 | 0.463 | 346 | 1390 | 690 | 801 | 754 | 0.249 | 0.502 | 0.432 | 0.459 | 343-377 | 685-718 |
| real | cursor | all | 653 | 2450 | 1210 | 1140 | 1220 | 1280 | 0.267 | 0.538 | 0.572 | 0.537 | 0.51 | 670 | 2490 | 1220 | 1230 | 1300 | 0.27 | 0.548 | 0.545 | 0.515 | 653-684 | 1210-1240 |
| real | parse | 1 | 320 | 1130 | 630 | 644 | 720 | 685 | 0.283 | 0.508 | 0.497 | 0.445 | 0.468 | 324 | 1150 | 640 | 722 | 696 | 0.281 | 0.507 | 0.449 | 0.466 | 320-346 | 630-677 |
| real | parse | all | 594 | 1990 | 1100 | 1110 | 1110 | 1170 | 0.299 | 0.542 | 0.535 | 0.535 | 0.509 | 625 | 2030 | 1140 | 1130 | 1220 | 0.308 | 0.549 | 0.555 | 0.513 | 594-632 | 1100-1160 |
| real | walk | 1 | 338 | 1320 | 648 | n/a | n/a | n/a | 0.256 | 0.522 | n/a | n/a | n/a | 340 | 1360 | 656 | n/a | n/a | 0.25 | 0.518 | n/a | n/a | 338-356 | 648-678 |
| real | walk | all | 652 | 2360 | 1130 | n/a | n/a | n/a | 0.276 | 0.578 | n/a | n/a | n/a | 663 | 2380 | 1170 | n/a | n/a | 0.279 | 0.565 | n/a | n/a | 652-673 | 1130-1200 |

### hcl: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 4250 | 11100 | 6150 | 6070 | 6240 | 6450 | 0.382 | 0.692 | 0.7 | 0.681 | 0.659 | 4250 | 11200 | 6160 | 6250 | 6460 | 0.379 | 0.691 | 0.68 | 0.658 | 4250-4250 | 6150-6160 |
| broken | cursor | all | 4260 | 11200 | 6170 | 6100 | 6280 | 6490 | 0.379 | 0.689 | 0.697 | 0.678 | 0.656 | 4260 | 11300 | 6190 | 6280 | 6500 | 0.377 | 0.688 | 0.678 | 0.655 | 4260-4300 | 6170-6200 |
| broken | parse | 1 | 4060 | 9590 | 5900 | 5830 | 5970 | 6200 | 0.424 | 0.688 | 0.697 | 0.68 | 0.656 | 4060 | 9590 | 5910 | 5980 | 6210 | 0.424 | 0.688 | 0.68 | 0.655 | 4060-4060 | 5900-5910 |
| broken | parse | all | 4070 | 9660 | 5920 | 5850 | 6000 | 6210 | 0.421 | 0.687 | 0.695 | 0.678 | 0.654 | 4070 | 9710 | 5930 | 6010 | 6230 | 0.419 | 0.686 | 0.677 | 0.653 | 4070-4070 | 5920-5950 |
| broken | walk | 1 | 5060 | 12900 | 7120 | n/a | n/a | n/a | 0.393 | 0.711 | n/a | n/a | n/a | 5060 | 12900 | 7120 | n/a | n/a | 0.392 | 0.711 | n/a | n/a | 5060-5060 | 7120-7120 |
| broken | walk | all | 5060 | 12900 | 7120 | n/a | n/a | n/a | 0.392 | 0.711 | n/a | n/a | n/a | 5060 | 13000 | 7140 | n/a | n/a | 0.39 | 0.709 | n/a | n/a | 5060-5180 | 7120-7160 |
| real | cursor | 1 | 9000 | 24300 | 12200 | 12000 | 12500 | 12900 | 0.37 | 0.74 | 0.75 | 0.717 | 0.699 | 9000 | 24300 | 12200 | 12600 | 12900 | 0.37 | 0.739 | 0.717 | 0.699 | 9000-9010 | 12200-12200 |
| real | cursor | all | 9000 | 24400 | 12100 | 12000 | 12500 | 12900 | 0.369 | 0.742 | 0.752 | 0.72 | 0.7 | 9000 | 24400 | 12200 | 12500 | 12900 | 0.369 | 0.741 | 0.718 | 0.7 | 9000-9020 | 12100-12200 |
| real | parse | 1 | 8430 | 19500 | 11200 | 11000 | 11500 | 11900 | 0.431 | 0.751 | 0.763 | 0.732 | 0.709 | 8430 | 19500 | 11200 | 11500 | 11900 | 0.431 | 0.749 | 0.731 | 0.707 | 8430-8430 | 11200-11300 |
| real | parse | all | 8430 | 19500 | 11200 | 11000 | 11400 | 11900 | 0.433 | 0.754 | 0.766 | 0.737 | 0.711 | 8430 | 19600 | 11200 | 11500 | 11900 | 0.431 | 0.752 | 0.733 | 0.71 | 8430-8430 | 11200-11200 |
| real | walk | 1 | 8860 | 23000 | 11600 | n/a | n/a | n/a | 0.386 | 0.763 | n/a | n/a | n/a | 8860 | 23000 | 11600 | n/a | n/a | 0.385 | 0.762 | n/a | n/a | 8860-8860 | 11600-11700 |
| real | walk | all | 8860 | 23000 | 11600 | n/a | n/a | n/a | 0.385 | 0.767 | n/a | n/a | n/a | 8860 | 23100 | 11600 | n/a | n/a | 0.383 | 0.764 | n/a | n/a | 8860-8860 | 11600-11700 |

### hcl: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 862 | 3340 | 1820 | 1840 | 2000 | 2000 | 0.258 | 0.474 | 0.468 | 0.43 | 0.432 | 867 | 3410 | 1820 | 2040 | 2020 | 0.254 | 0.476 | 0.425 | 0.429 | 862-928 | 1820-1900 |
| broken | cursor | all | 1350 | 4950 | 2600 | 2590 | 2680 | 2800 | 0.274 | 0.521 | 0.523 | 0.505 | 0.484 | 1420 | 5010 | 2700 | 2720 | 2850 | 0.284 | 0.527 | 0.522 | 0.499 | 1350-1440 | 2600-2720 |
| broken | parse | 1 | 819 | 2950 | 1750 | 1770 | 1880 | 1940 | 0.278 | 0.469 | 0.464 | 0.435 | 0.423 | 831 | 2980 | 1770 | 1940 | 1960 | 0.279 | 0.469 | 0.429 | 0.423 | 819-933 | 1750-1830 |
| broken | parse | all | 1290 | 4320 | 2500 | 2520 | 2540 | 2690 | 0.298 | 0.513 | 0.509 | 0.505 | 0.478 | 1340 | 4400 | 2580 | 2570 | 2760 | 0.305 | 0.519 | 0.522 | 0.486 | 1290-1380 | 2500-2630 |
| broken | walk | 1 | 1010 | 4180 | 2010 | n/a | n/a | n/a | 0.242 | 0.505 | n/a | n/a | n/a | 1020 | 4220 | 2020 | n/a | n/a | 0.241 | 0.503 | n/a | n/a | 1010-1050 | 2010-2070 |
| broken | walk | all | 1620 | 5810 | 2950 | n/a | n/a | n/a | 0.279 | 0.55 | n/a | n/a | n/a | 1670 | 5850 | 3010 | n/a | n/a | 0.286 | 0.556 | n/a | n/a | 1620-1720 | 2950-3030 |
| real | cursor | 1 | 1790 | 7060 | 3570 | 3640 | 4170 | 3870 | 0.253 | 0.501 | 0.492 | 0.429 | 0.463 | 1800 | 7220 | 3600 | 4180 | 3930 | 0.25 | 0.501 | 0.432 | 0.458 | 1790-1970 | 3570-3740 |
| real | cursor | all | 2910 | 10800 | 5430 | 5180 | 5540 | 5750 | 0.268 | 0.536 | 0.562 | 0.525 | 0.506 | 2990 | 11000 | 5450 | 5590 | 5850 | 0.273 | 0.548 | 0.534 | 0.511 | 2910-3030 | 5430-5480 |
| real | parse | 1 | 1670 | 5880 | 3290 | 3360 | 3760 | 3570 | 0.284 | 0.509 | 0.498 | 0.445 | 0.468 | 1690 | 5990 | 3340 | 3760 | 3630 | 0.283 | 0.507 | 0.449 | 0.466 | 1670-1790 | 3290-3530 |
| real | parse | all | 2670 | 8860 | 4960 | 4980 | 5070 | 5290 | 0.301 | 0.537 | 0.536 | 0.525 | 0.504 | 2790 | 8960 | 5090 | 5110 | 5480 | 0.311 | 0.547 | 0.546 | 0.509 | 2670-2810 | 4960-5150 |
| real | walk | 1 | 1770 | 6870 | 3380 | n/a | n/a | n/a | 0.257 | 0.522 | n/a | n/a | n/a | 1770 | 7050 | 3420 | n/a | n/a | 0.252 | 0.518 | n/a | n/a | 1770-1860 | 3380-3530 |
| real | walk | all | 2910 | 10400 | 5130 | n/a | n/a | n/a | 0.279 | 0.568 | n/a | n/a | n/a | 2940 | 10500 | 5270 | n/a | n/a | 0.28 | 0.559 | n/a | n/a | 2910-2960 | 5130-5350 |

### hcl: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 189 | 1550 | 241 | 244 | 270 | 252 | 0.122 | 0.787 | 0.777 | 0.702 | 0.752 |
| broken | cursor minus parse | 1 | cycles M | 42.2 | 389 | 70.8 | 74.7 | 122 | 58.4 | 0.109 | 0.596 | 0.564 | 0.347 | 0.722 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 8.05 | 76.1 | 13.4 | 14.2 | 23.6 | 11.1 | 0.106 | 0.601 | 0.567 | 0.341 | 0.728 |
| broken | cursor minus parse | all | instructions M | 190 | 1570 | 253 | 258 | 280 | 274 | 0.121 | 0.75 | 0.736 | 0.679 | 0.692 |
| broken | cursor minus parse | all | cycles M | 68.3 | 625 | 91.6 | 65.1 | 134 | 109 | 0.109 | 0.745 | 1.05 | 0.509 | 0.628 |
| broken | cursor minus parse | all | CPU user+sys ms | 15.1 | 135 | 20.1 | 15.6 | 27.1 | 24 | 0.111 | 0.749 | 0.965 | 0.555 | 0.628 |
| broken | walk minus parse | 1 | instructions M | 996 | 3280 | 1210 | n/a | n/a | n/a | 0.304 | 0.823 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 195 | 1230 | 261 | n/a | n/a | n/a | 0.158 | 0.747 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 37.4 | 240 | 50 | n/a | n/a | n/a | 0.156 | 0.749 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 997 | 3260 | 1200 | n/a | n/a | n/a | 0.306 | 0.833 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 336 | 1490 | 444 | n/a | n/a | n/a | 0.225 | 0.755 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 74.3 | 312 | 97.3 | n/a | n/a | n/a | 0.238 | 0.764 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 572 | 4770 | 953 | 963 | 1040 | 988 | 0.12 | 0.601 | 0.594 | 0.551 | 0.579 |
| real | cursor minus parse | 1 | cycles M | 118 | 1180 | 284 | 277 | 419 | 296 | 0.1 | 0.417 | 0.427 | 0.283 | 0.4 |
| real | cursor minus parse | 1 | CPU user+sys ms | 22.8 | 230 | 54.3 | 53.1 | 79.9 | 56.8 | 0.0995 | 0.421 | 0.43 | 0.286 | 0.402 |
| real | cursor minus parse | all | instructions M | 572 | 4910 | 951 | 968 | 1070 | 1010 | 0.117 | 0.602 | 0.592 | 0.537 | 0.569 |
| real | cursor minus parse | all | cycles M | 243 | 1980 | 468 | 200 | 462 | 452 | 0.122 | 0.519 | 1.22 | 0.525 | 0.537 |
| real | cursor minus parse | all | CPU user+sys ms | 58.6 | 459 | 116 | 31.5 | 105 | 111 | 0.128 | 0.504 | 1.86 | 0.558 | 0.526 |
| real | walk minus parse | 1 | instructions M | 432 | 3430 | 388 | n/a | n/a | n/a | 0.126 | 1.11 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 93.7 | 989 | 93.1 | n/a | n/a | n/a | 0.0947 | 1.01 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 18 | 192 | 17.4 | n/a | n/a | n/a | 0.0935 | 1.03 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 432 | 3560 | 369 | n/a | n/a | n/a | 0.121 | 1.17 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 246 | 1570 | 167 | n/a | n/a | n/a | 0.156 | 1.47 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 57.8 | 375 | 31.9 | n/a | n/a | n/a | 0.154 | 1.81 | n/a | n/a | n/a |

### hcl: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 19.4 | 62.5 | 12.6 | 0.31 | 1.53 | 19.4 | 62.5 | 12.6 | 0.311 | 1.54 | 14.1 | 55.8 | 8.72 | 0.253 | 1.62 |
| broken | cursor | all | 69.5 | 154 | 93.2 | 0.451 | 0.746 | 69.5 | 154 | 93.2 | 0.451 | 0.746 | 62.8 | 147 | 89.2 | 0.427 | 0.704 |
| broken | parse | 1 | 15.4 | 17.9 | 11.6 | 0.861 | 1.33 | 15.4 | 17.9 | 11.6 | 0.861 | 1.33 | 10.1 | 11 | 7.54 | 0.922 | 1.34 |
| broken | parse | all | 65.5 | 130 | 92.5 | 0.504 | 0.708 | 65.5 | 130 | 92.5 | 0.504 | 0.708 | 59.1 | 123 | 88.6 | 0.48 | 0.666 |
| broken | walk | 1 | 15.3 | 58.1 | 12.6 | 0.263 | 1.21 | 15.3 | 58.1 | 12.6 | 0.263 | 1.21 | 9.97 | 51.1 | 8.66 | 0.195 | 1.15 |
| broken | walk | all | 65.5 | 145 | 96.2 | 0.451 | 0.681 | 65.5 | 145 | 96.2 | 0.451 | 0.681 | 58.8 | 138 | 92.2 | 0.425 | 0.638 |
| real | cursor | 1 | 26.9 | 49.6 | 14.3 | 0.543 | 1.88 | 26.9 | 49.6 | 14.3 | 0.543 | 1.88 | 16.8 | 38.2 | 7.45 | 0.44 | 2.25 |
| real | cursor | all | 69 | 111 | 75 | 0.62 | 0.92 | 69 | 111 | 75 | 0.62 | 0.92 | 58.7 | 99.8 | 68 | 0.588 | 0.863 |
| real | parse | 1 | 17.8 | 23 | 13.3 | 0.774 | 1.33 | 17.8 | 23 | 13.3 | 0.774 | 1.33 | 7.6 | 11.7 | 6.39 | 0.651 | 1.19 |
| real | parse | all | 59.9 | 99.7 | 77.8 | 0.601 | 0.77 | 59.9 | 99.7 | 77.8 | 0.601 | 0.77 | 49.7 | 88.5 | 70.8 | 0.562 | 0.702 |
| real | walk | 1 | 17.5 | 47.2 | 13.8 | 0.37 | 1.27 | 17.5 | 47.2 | 13.8 | 0.37 | 1.27 | 7.37 | 36 | 7.11 | 0.205 | 1.04 |
| real | walk | all | 60.2 | 107 | 84.1 | 0.561 | 0.716 | 60.2 | 107 | 84.1 | 0.561 | 0.716 | 49.9 | 96.1 | 77 | 0.519 | 0.648 |

### hcl: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 22.3 | 24.6 | 11.6 | 0.908 | 1.93 | 22.3 | 24.6 | 11.6 | 0.908 | 1.93 | 17 | 17.7 | 7.53 | 0.961 | 2.26 |
| broken | all | 80.8 | 178 | 112 | 0.455 | 0.72 | 80.9 | 178 | 112 | 0.455 | 0.72 | 74.3 | 171 | 108 | 0.435 | 0.688 |
| real | 1 | 17.5 | 23.6 | 13 | 0.742 | 1.35 | 17.5 | 23.6 | 13 | 0.742 | 1.35 | 7.48 | 12.3 | 6.36 | 0.609 | 1.17 |
| real | all | 62.5 | 119 | 86.7 | 0.526 | 0.721 | 62.5 | 119 | 86.7 | 0.527 | 0.721 | 52.1 | 107 | 79.7 | 0.486 | 0.653 |

### hcl: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0107 (real), 2.97 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.118 | 4980 | 7.02 | 2600 | 245000 | 2600 | 245000 | 0 |
| real | steady | 0.0107 | 2.04 | 7.02 | 2600 | 245000 | 2600 | 245000 | 0 |
| broken | cold | 4.16 | 17800 | 7.65 | 3840 | 389000 | 3840 | 389000 | 0.00108 |
| broken | steady | 2.97 | 434 | 7.52 | 3840 | 389000 | 3840 | 389000 | 0.000773 |

### hcl: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2049 B | init ms | 0.00887 | 0.0114 | 0.00544 | 0.777 | 1.63 | 15 |
| #0 2049 B | parse1 ms | 0.236 | 0.406 | 0.29 | 0.582 | 0.813 | 15 |
| #0 2049 B | init+parse1 ms | 0.245 | 0.417 | 0.296 | 0.588 | 0.828 | 15 |
| #0 2049 B | parse2 ms | 0.148 | 0.422 | 0.238 | 0.35 | 0.622 | 15 |
| #0 2049 B | process user+sys ms | 1.08 | 1.84 | 0.822 | 0.588 | 1.32 | 15 |
| #0 2049 B | exec to exit instr user+kernel M | 10.6 | 17.2 | 9.3 | 0.618 | 1.15 | 15 |
| #0 2049 B | exec to exit instr user M | 7.91 | 13.4 | 8.2 | 0.59 | 0.964 | 15 |
| #0 2049 B | exec to exit instr kernel M | 2.72 | 3.82 | 1.09 | 0.712 | 2.49 | 15 |
| #0 2049 B | exec to exit cycles user+kernel M | 4.93 | 8.86 | 3.77 | 0.557 | 1.31 | 15 |
| #0 2049 B | exec to exit task-clock ms | 1.01 | 1.78 | 0.72 | 0.567 | 1.4 | 15 |
| #0 2049 B | peak RSS MB | 3.77 | 4.93 | 2.68 | 0.765 | 1.41 | 15 |
| #0 2049 B | peak footprint MB | 3.8 | 4.96 | 2.68 | 0.766 | 1.42 | 15 |
| #1 924 B | init ms | 0.00896 | 0.0111 | 0.0057 | 0.809 | 1.57 | 15 |
| #1 924 B | parse1 ms | 0.147 | 0.236 | 0.169 | 0.623 | 0.868 | 15 |
| #1 924 B | init+parse1 ms | 0.159 | 0.246 | 0.175 | 0.643 | 0.906 | 15 |
| #1 924 B | parse2 ms | 0.0823 | 0.229 | 0.128 | 0.36 | 0.642 | 15 |
| #1 924 B | process user+sys ms | 0.939 | 1.45 | 0.579 | 0.647 | 1.62 | 15 |
| #1 924 B | exec to exit instr user+kernel M | 7.63 | 11.8 | 5.74 | 0.648 | 1.33 | 15 |
| #1 924 B | exec to exit instr user M | 4.94 | 8.09 | 4.71 | 0.611 | 1.05 | 15 |
| #1 924 B | exec to exit instr kernel M | 2.69 | 3.68 | 1.03 | 0.731 | 2.62 | 15 |
| #1 924 B | exec to exit cycles user+kernel M | 4.25 | 6.84 | 2.55 | 0.621 | 1.67 | 15 |
| #1 924 B | exec to exit task-clock ms | 0.86 | 1.37 | 0.49 | 0.628 | 1.76 | 15 |
| #1 924 B | peak RSS MB | 3.73 | 4.8 | 2.66 | 0.776 | 1.4 | 15 |
| #1 924 B | peak footprint MB | 3.76 | 4.84 | 2.6 | 0.777 | 1.44 | 15 |

### hcl: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1500 | 933142 | 0 | 0 | ok |
| broken | 497 | 294150 | 446 | 0 | ok |

## java

### java: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 136 | 715 | 367 | 374 | 347 | 398 | 0.19 | 0.37 | 0.363 | 0.391 | 0.341 | 140 | 728 | 380 | 351 | 420 | 0.193 | 0.369 | 0.4 | 0.334 | 136-144 | 367-432 | 1.01 | 1.01 | 1 | 18 |
| broken | cursor | all | 34 | 186 | 127 | 125 | 98.1 | 136 | 0.183 | 0.267 | 0.272 | 0.346 | 0.25 | 37 | 193 | 129 | 102 | 138 | 0.192 | 0.286 | 0.362 | 0.267 | 34-39.7 | 127-137 | 5.93 * | 5.9 * | 4.32 * | 72 |
| broken | parse | 1 | 114 | 518 | 336 | 341 | 292 | 364 | 0.22 | 0.339 | 0.334 | 0.389 | 0.313 | 117 | 527 | 339 | 296 | 368 | 0.223 | 0.346 | 0.397 | 0.319 | 114-121 | 336-357 | 1 | 1.01 | 1 | 21.5 |
| broken | parse | all | 28.2 | 160 | 124 | 126 | 96.8 | 129 | 0.176 | 0.227 | 0.224 | 0.291 | 0.218 | 29.8 | 167 | 128 | 98 | 136 | 0.179 | 0.232 | 0.304 | 0.219 | 28.2-33.2 | 124-135 | 5.97 * | 4.81 * | 3.98 * | 86.9 |
| broken | walk | 1 | 135 | 691 | 354 | n/a | n/a | n/a | 0.196 | 0.382 | n/a | n/a | n/a | 137 | 709 | 372 | n/a | n/a | 0.193 | 0.368 | n/a | n/a | 135-145 | 354-390 | 1 | 1.02 | 1 | 18.1 |
| broken | walk | all | 35.5 | 186 | 126 | n/a | n/a | n/a | 0.191 | 0.281 | n/a | n/a | n/a | 37.8 | 193 | 133 | n/a | n/a | 0.196 | 0.283 | n/a | n/a | 35.5-41.2 | 126-139 | 5.69 * | 5.58 * | 4.08 * | 69 |
| real | cursor | 1 | 234 | 1080 | 456 | 471 | 495 | 503 | 0.218 | 0.514 | 0.498 | 0.473 | 0.466 | 238 | 1100 | 465 | 496 | 505 | 0.217 | 0.512 | 0.48 | 0.471 | 234-242 | 456-503 | 1 | 1.01 | 1 | 36.4 |
| real | cursor | all | 30.2 | 146 | 57.6 | 57.6 | 56.4 | 62.3 | 0.207 | 0.524 | 0.524 | 0.535 | 0.485 | 31.1 | 149 | 59.8 | 58.4 | 64.2 | 0.208 | 0.52 | 0.533 | 0.485 | 30.2-33.6 | 57.6-62.3 | 14.4 | 13.5 | 14.3 | 283 |
| real | parse | 1 | 188 | 668 | 394 | 403 | 378 | 439 | 0.281 | 0.477 | 0.466 | 0.497 | 0.428 | 194 | 677 | 404 | 379 | 444 | 0.286 | 0.481 | 0.512 | 0.437 | 188-212 | 394-414 | 1 | 1.01 | 1 | 45.4 |
| real | parse | all | 24.3 | 85.7 | 50.4 | 49.4 | 46 | 53.7 | 0.283 | 0.482 | 0.491 | 0.528 | 0.452 | 25.1 | 87.1 | 51.9 | 47.1 | 55.7 | 0.288 | 0.484 | 0.533 | 0.451 | 24.3-28.4 | 50.4-54.2 | 13.9 | 13.8 | 14.3 | 352 |
| real | walk | 1 | 225 | 1010 | 426 | n/a | n/a | n/a | 0.224 | 0.529 | n/a | n/a | n/a | 228 | 1030 | 433 | n/a | n/a | 0.222 | 0.527 | n/a | n/a | 225-232 | 426-470 | 1 | 1.01 | 1 | 37.9 |
| real | walk | all | 29.5 | 134 | 55.1 | n/a | n/a | n/a | 0.22 | 0.535 | n/a | n/a | n/a | 30.5 | 136 | 56.8 | n/a | n/a | 0.223 | 0.536 | n/a | n/a | 29.5-32 | 55.1-59.7 | 14 | 13.6 | 14.3 | 290 |
| realerr (too small) | cursor | 1 | 0.0411 | 0.132 | 0.0628 | 0.0648 | 0.0666 | 0.0748 | 0.312 | 0.655 | 0.635 | 0.617 | 0.55 | 0.0483 | 0.181 | 0.0732 | 0.0693 | 0.0791 | 0.267 | 0.66 | 0.697 | 0.611 | 0.0411-0.104 | 0.0628-0.159 | 1.25 | 1.1 | 0.992 | 22 |
| realerr (too small) | cursor | all | 0.0433 | 0.0882 | 0.0836 | 0.0655 | 0.0752 | 0.0873 | 0.491 | 0.518 | 0.661 | 0.577 | 0.496 | 0.0772 | 0.113 | 0.111 | 0.108 | 0.124 | 0.683 | 0.697 | 0.715 | 0.62 | 0.0433-0.147 | 0.0836-0.188 | 1.86 * | 2.02 * | 2.41 * | 20.9 |
| realerr (too small) | parse | 1 | 0.0375 | 0.099 | 0.0564 | 0.056 | 0.0563 | 0.0671 | 0.379 | 0.664 | 0.669 | 0.666 | 0.558 | 0.0397 | 0.103 | 0.0592 | 0.0575 | 0.0685 | 0.384 | 0.67 | 0.69 | 0.579 | 0.0375-0.0783 | 0.0564-0.0786 | 1.26 | 1.13 | 0.998 | 24.2 |
| realerr (too small) | parse | all | 0.0307 | 0.0704 | 0.0669 | 0.0726 | 0.0737 | 0.0747 | 0.436 | 0.459 | 0.423 | 0.417 | 0.411 | 0.0504 | 0.0909 | 0.0881 | 0.0995 | 0.0896 | 0.555 | 0.573 | 0.507 | 0.563 | 0.0307-0.138 | 0.0669-0.114 | 2.07 * | 2.13 * | 2.62 * | 29.5 |
| realerr (too small) | walk | 1 | 0.0398 | 0.121 | 0.0568 | n/a | n/a | n/a | 0.33 | 0.7 | n/a | n/a | n/a | 0.0517 | 0.132 | 0.0697 | n/a | n/a | 0.393 | 0.742 | n/a | n/a | 0.0398-0.0997 | 0.0568-0.0985 | 1.25 | 1.11 | 1 | 22.8 |
| realerr (too small) | walk | all | 0.0353 | 0.0859 | 0.0651 | n/a | n/a | n/a | 0.411 | 0.542 | n/a | n/a | n/a | 0.0631 | 0.101 | 0.104 | n/a | n/a | 0.625 | 0.61 | n/a | n/a | 0.0353-0.0946 | 0.0651-0.163 | 2.12 * | 2.1 * | 2.47 * | 25.7 |

### java: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 137 | 722 | 367 | 374 | 347 | 398 | 0.189 | 0.372 | 0.365 | 0.394 | 0.343 | 141 | 735 | 380 | 351 | 420 | 0.192 | 0.371 | 0.402 | 0.336 | 137-146 | 367-432 |
| broken | cursor | all | 212 | 1120 | 550 | 535 | 499 | 596 | 0.19 | 0.385 | 0.396 | 0.424 | 0.355 | 221 | 1140 | 563 | 512 | 608 | 0.194 | 0.392 | 0.431 | 0.363 | 212-224 | 550-576 |
| broken | parse | 1 | 114 | 521 | 336 | 341 | 292 | 364 | 0.219 | 0.339 | 0.334 | 0.39 | 0.313 | 118 | 530 | 339 | 296 | 368 | 0.222 | 0.347 | 0.398 | 0.319 | 114-122 | 336-357 |
| broken | parse | all | 171 | 789 | 489 | 498 | 442 | 541 | 0.217 | 0.35 | 0.343 | 0.387 | 0.316 | 180 | 805 | 514 | 447 | 556 | 0.224 | 0.351 | 0.403 | 0.324 | 171-188 | 489-520 |
| broken | walk | 1 | 135 | 702 | 354 | n/a | n/a | n/a | 0.193 | 0.383 | n/a | n/a | n/a | 138 | 719 | 372 | n/a | n/a | 0.191 | 0.37 | n/a | n/a | 135-146 | 354-390 |
| broken | walk | all | 204 | 1070 | 528 | n/a | n/a | n/a | 0.191 | 0.387 | n/a | n/a | n/a | 216 | 1090 | 545 | n/a | n/a | 0.198 | 0.396 | n/a | n/a | 204-227 | 528-563 |
| real | cursor | 1 | 235 | 1080 | 456 | 471 | 495 | 502 | 0.217 | 0.515 | 0.498 | 0.474 | 0.467 | 238 | 1110 | 465 | 496 | 505 | 0.215 | 0.512 | 0.48 | 0.471 | 235-242 | 456-503 |
| real | cursor | all | 439 | 2000 | 827 | 815 | 813 | 895 | 0.219 | 0.53 | 0.538 | 0.54 | 0.49 | 453 | 2040 | 854 | 835 | 922 | 0.222 | 0.53 | 0.543 | 0.492 | 439-457 | 827-868 |
| real | parse | 1 | 188 | 671 | 394 | 403 | 378 | 439 | 0.28 | 0.477 | 0.467 | 0.498 | 0.428 | 194 | 681 | 404 | 379 | 444 | 0.285 | 0.481 | 0.513 | 0.438 | 188-212 | 394-414 |
| real | parse | all | 343 | 1180 | 723 | 700 | 638 | 758 | 0.291 | 0.474 | 0.491 | 0.538 | 0.453 | 360 | 1200 | 746 | 647 | 765 | 0.299 | 0.483 | 0.557 | 0.471 | 343-365 | 723-763 |
| real | walk | 1 | 225 | 1010 | 426 | n/a | n/a | n/a | 0.222 | 0.529 | n/a | n/a | n/a | 229 | 1030 | 433 | n/a | n/a | 0.221 | 0.527 | n/a | n/a | 225-232 | 426-470 |
| real | walk | all | 413 | 1840 | 783 | n/a | n/a | n/a | 0.224 | 0.527 | n/a | n/a | n/a | 428 | 1860 | 816 | n/a | n/a | 0.231 | 0.525 | n/a | n/a | 413-436 | 783-823 |
| realerr (too small) | cursor | 1 | 0.049 | 0.146 | 0.063 | 0.064 | 0.065 | 0.075 | 0.336 | 0.778 | 0.766 | 0.754 | 0.653 | 0.061 | 0.199 | 0.077 | 0.066 | 0.078 | 0.307 | 0.792 | 0.924 | 0.782 | 0.049-0.126 | 0.063-0.16 |
| realerr (too small) | cursor | all | 0.097 | 0.171 | 0.214 | 0.175 | 0.2 | 0.213 | 0.567 | 0.453 | 0.554 | 0.485 | 0.455 | 0.138 | 0.23 | 0.272 | 0.249 | 0.29 | 0.6 | 0.507 | 0.554 | 0.476 | 0.097-0.268 | 0.214-0.333 |
| realerr (too small) | parse | 1 | 0.046 | 0.109 | 0.056 | 0.056 | 0.055 | 0.067 | 0.422 | 0.821 | 0.821 | 0.836 | 0.687 | 0.052 | 0.115 | 0.059 | 0.057 | 0.068 | 0.452 | 0.881 | 0.912 | 0.765 | 0.046-0.095 | 0.056-0.08 |
| realerr (too small) | parse | all | 0.079 | 0.148 | 0.18 | 0.213 | 0.201 | 0.214 | 0.534 | 0.439 | 0.371 | 0.393 | 0.369 | 0.113 | 0.194 | 0.235 | 0.246 | 0.226 | 0.582 | 0.481 | 0.459 | 0.5 | 0.079-0.252 | 0.18-0.304 |
| realerr (too small) | walk | 1 | 0.05 | 0.131 | 0.057 | n/a | n/a | n/a | 0.382 | 0.877 | n/a | n/a | n/a | 0.068 | 0.148 | 0.071 | n/a | n/a | 0.459 | 0.958 | n/a | n/a | 0.05-0.114 | 0.057-0.099 |
| realerr (too small) | walk | all | 0.078 | 0.177 | 0.189 | n/a | n/a | n/a | 0.441 | 0.413 | n/a | n/a | n/a | 0.113 | 0.222 | 0.246 | n/a | n/a | 0.509 | 0.459 | n/a | n/a | 0.078-0.215 | 0.189-0.34 |

### java: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 3120 | 13500 | 7310 | 7170 | 6920 | 7530 | 0.23 | 0.427 | 0.435 | 0.451 | 0.414 | 3120 | 13600 | 7310 | 6920 | 7530 | 0.23 | 0.427 | 0.451 | 0.415 | 3120-3120 | 7310-7310 |
| broken | cursor | all | 3130 | 13600 | 7320 | 7170 | 6920 | 7540 | 0.23 | 0.427 | 0.436 | 0.452 | 0.415 | 3130 | 13600 | 7330 | 6930 | 7550 | 0.23 | 0.428 | 0.452 | 0.415 | 3130-3190 | 7320-7350 |
| broken | parse | 1 | 2560 | 9430 | 6690 | 6530 | 6230 | 6880 | 0.271 | 0.382 | 0.391 | 0.41 | 0.372 | 2560 | 9440 | 6700 | 6230 | 6880 | 0.271 | 0.382 | 0.41 | 0.372 | 2560-2560 | 6690-6700 |
| broken | parse | all | 2560 | 9420 | 6680 | 6520 | 6230 | 6880 | 0.272 | 0.384 | 0.393 | 0.411 | 0.373 | 2570 | 9440 | 6700 | 6230 | 6880 | 0.272 | 0.383 | 0.412 | 0.373 | 2560-2610 | 6680-6720 |
| broken | walk | 1 | 3100 | 12700 | 7040 | n/a | n/a | n/a | 0.245 | 0.441 | n/a | n/a | n/a | 3110 | 12700 | 7040 | n/a | n/a | 0.245 | 0.442 | n/a | n/a | 3100-3120 | 7040-7040 |
| broken | walk | all | 3130 | 12800 | 7030 | n/a | n/a | n/a | 0.245 | 0.445 | n/a | n/a | n/a | 3140 | 12900 | 7040 | n/a | n/a | 0.244 | 0.446 | n/a | n/a | 3130-3200 | 7030-7070 |
| real | cursor | 1 | 5470 | 20000 | 8450 | 8330 | 8070 | 8890 | 0.274 | 0.647 | 0.656 | 0.677 | 0.615 | 5470 | 20000 | 8460 | 8080 | 8900 | 0.274 | 0.646 | 0.676 | 0.614 | 5470-5470 | 8450-8470 |
| real | cursor | all | 5470 | 20000 | 8470 | 8360 | 8090 | 8900 | 0.273 | 0.646 | 0.654 | 0.675 | 0.614 | 5470 | 20000 | 8480 | 8100 | 8920 | 0.273 | 0.645 | 0.675 | 0.613 | 5470-5520 | 8470-8500 |
| real | parse | 1 | 4310 | 11100 | 7130 | 6980 | 6610 | 7500 | 0.389 | 0.605 | 0.618 | 0.653 | 0.575 | 4310 | 11100 | 7150 | 6630 | 7510 | 0.388 | 0.604 | 0.651 | 0.574 | 4310-4310 | 7130-7150 |
| real | parse | all | 4320 | 11100 | 7160 | 6990 | 6650 | 7530 | 0.388 | 0.603 | 0.617 | 0.649 | 0.573 | 4320 | 11100 | 7170 | 6650 | 7530 | 0.388 | 0.602 | 0.649 | 0.573 | 4320-4320 | 7160-7190 |
| real | walk | 1 | 5230 | 17500 | 7820 | n/a | n/a | n/a | 0.298 | 0.669 | n/a | n/a | n/a | 5230 | 17500 | 7830 | n/a | n/a | 0.298 | 0.668 | n/a | n/a | 5230-5230 | 7820-7840 |
| real | walk | all | 5230 | 17600 | 7840 | n/a | n/a | n/a | 0.297 | 0.667 | n/a | n/a | n/a | 5230 | 17600 | 7860 | n/a | n/a | 0.297 | 0.665 | n/a | n/a | 5230-5230 | 7840-7880 |
| realerr (too small) | cursor | 1 | 0.952 | 2.26 | 1.07 | 1.01 | 1.03 | 1.14 | 0.42 | 0.892 | 0.939 | 0.927 | 0.832 | 0.956 | 2.27 | 1.07 | 1.03 | 1.15 | 0.421 | 0.893 | 0.929 | 0.833 | 0.952-0.97 | 1.07-1.08 |
| realerr (too small) | cursor | all | 1.03 | 2.32 | 1.08 | 1.03 | 1.04 | 1.17 | 0.444 | 0.954 | 1 | 0.992 | 0.884 | 1.14 | 2.36 | 1.09 | 1.05 | 1.17 | 0.482 | 1.04 | 1.08 | 0.971 | 1.03-1.36 | 1.08-1.1 |
| realerr (too small) | parse | 1 | 0.875 | 1.66 | 0.983 | 0.926 | 0.932 | 1.05 | 0.528 | 0.89 | 0.945 | 0.939 | 0.83 | 0.877 | 1.66 | 0.983 | 0.932 | 1.05 | 0.528 | 0.892 | 0.941 | 0.831 | 0.875-0.883 | 0.983-0.983 |
| realerr (too small) | parse | all | 0.942 | 1.7 | 0.99 | 0.938 | 0.949 | 1.06 | 0.553 | 0.952 | 1 | 0.993 | 0.888 | 1.06 | 1.74 | 1.01 | 0.959 | 1.08 | 0.607 | 1.05 | 1.1 | 0.978 | 0.942-1.23 | 0.99-1.01 |
| realerr (too small) | walk | 1 | 0.932 | 2.08 | 1.02 | n/a | n/a | n/a | 0.448 | 0.91 | n/a | n/a | n/a | 0.935 | 2.08 | 1.02 | n/a | n/a | 0.449 | 0.912 | n/a | n/a | 0.932-0.947 | 1.02-1.03 |
| realerr (too small) | walk | all | 1.01 | 2.13 | 1.04 | n/a | n/a | n/a | 0.474 | 0.972 | n/a | n/a | n/a | 1.11 | 2.18 | 1.05 | n/a | n/a | 0.508 | 1.05 | n/a | n/a | 1.01-1.24 | 1.04-1.05 |

### java: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 711 | 3710 | 1900 | 1950 | 1800 | 2070 | 0.192 | 0.375 | 0.365 | 0.396 | 0.344 | 733 | 3760 | 1980 | 1810 | 2170 | 0.195 | 0.371 | 0.405 | 0.338 | 711-753 | 1900-2230 |
| broken | cursor | all | 1020 | 5180 | 2600 | 2550 | 2360 | 2840 | 0.197 | 0.393 | 0.401 | 0.433 | 0.361 | 1060 | 5270 | 2670 | 2400 | 2900 | 0.201 | 0.397 | 0.44 | 0.365 | 1020-1080 | 2600-2690 |
| broken | parse | 1 | 595 | 2690 | 1740 | 1760 | 1510 | 1880 | 0.221 | 0.343 | 0.338 | 0.393 | 0.316 | 610 | 2730 | 1750 | 1530 | 1910 | 0.223 | 0.348 | 0.399 | 0.32 | 595-634 | 1740-1840 |
| broken | parse | all | 844 | 3700 | 2340 | 2380 | 2080 | 2590 | 0.228 | 0.36 | 0.355 | 0.406 | 0.326 | 880 | 3760 | 2450 | 2110 | 2640 | 0.234 | 0.36 | 0.417 | 0.334 | 844-914 | 2340-2480 |
| broken | walk | 1 | 704 | 3610 | 1830 | n/a | n/a | n/a | 0.195 | 0.385 | n/a | n/a | n/a | 710 | 3670 | 1920 | n/a | n/a | 0.194 | 0.371 | n/a | n/a | 704-759 | 1830-2010 |
| broken | walk | all | 992 | 4980 | 2500 | n/a | n/a | n/a | 0.199 | 0.396 | n/a | n/a | n/a | 1050 | 5050 | 2580 | n/a | n/a | 0.207 | 0.405 | n/a | n/a | 992-1090 | 2500-2650 |
| real | cursor | 1 | 1220 | 5620 | 2380 | 2460 | 2590 | 2620 | 0.218 | 0.513 | 0.497 | 0.473 | 0.466 | 1240 | 5730 | 2430 | 2590 | 2640 | 0.216 | 0.511 | 0.479 | 0.47 | 1220-1260 | 2380-2620 |
| real | cursor | all | 1960 | 8880 | 3780 | 3720 | 3700 | 4050 | 0.22 | 0.518 | 0.526 | 0.529 | 0.483 | 2020 | 9040 | 3860 | 3800 | 4180 | 0.223 | 0.522 | 0.531 | 0.483 | 1960-2030 | 3780-3900 |
| real | parse | 1 | 982 | 3490 | 2060 | 2110 | 1970 | 2290 | 0.281 | 0.477 | 0.466 | 0.498 | 0.428 | 1010 | 3540 | 2110 | 1980 | 2320 | 0.286 | 0.481 | 0.512 | 0.437 | 982-1100 | 2060-2160 |
| real | parse | all | 1550 | 5310 | 3310 | 3220 | 2920 | 3470 | 0.292 | 0.468 | 0.481 | 0.53 | 0.446 | 1610 | 5410 | 3400 | 2960 | 3510 | 0.299 | 0.475 | 0.546 | 0.46 | 1550-1630 | 3310-3440 |
| real | walk | 1 | 1180 | 5260 | 2220 | n/a | n/a | n/a | 0.223 | 0.528 | n/a | n/a | n/a | 1190 | 5370 | 2260 | n/a | n/a | 0.222 | 0.527 | n/a | n/a | 1180-1210 | 2220-2450 |
| real | walk | all | 1860 | 8180 | 3580 | n/a | n/a | n/a | 0.227 | 0.519 | n/a | n/a | n/a | 1910 | 8240 | 3700 | n/a | n/a | 0.232 | 0.517 | n/a | n/a | 1860-1950 | 3580-3740 |
| realerr (too small) | cursor | 1 | 0.216 | 0.689 | 0.304 | 0.31 | 0.312 | 0.367 | 0.314 | 0.71 | 0.697 | 0.693 | 0.59 | 0.234 | 0.946 | 0.36 | 0.321 | 0.39 | 0.248 | 0.65 | 0.73 | 0.601 | 0.216-0.537 | 0.304-0.759 |
| realerr (too small) | cursor | all | 0.383 | 0.802 | 0.551 | 0.49 | 0.575 | 0.583 | 0.478 | 0.695 | 0.782 | 0.667 | 0.657 | 0.54 | 1.02 | 0.682 | 0.669 | 0.739 | 0.532 | 0.792 | 0.808 | 0.731 | 0.383-0.92 | 0.551-0.892 |
| realerr (too small) | parse | 1 | 0.2 | 0.525 | 0.271 | 0.275 | 0.267 | 0.327 | 0.381 | 0.739 | 0.729 | 0.75 | 0.612 | 0.215 | 0.543 | 0.277 | 0.269 | 0.333 | 0.395 | 0.775 | 0.797 | 0.644 | 0.2-0.414 | 0.271-0.359 |
| realerr (too small) | parse | all | 0.293 | 0.655 | 0.44 | 0.472 | 0.526 | 0.547 | 0.448 | 0.666 | 0.621 | 0.558 | 0.536 | 0.447 | 0.807 | 0.594 | 0.589 | 0.63 | 0.553 | 0.752 | 0.759 | 0.709 | 0.293-0.89 | 0.44-0.793 |
| realerr (too small) | walk | 1 | 0.214 | 0.635 | 0.279 | n/a | n/a | n/a | 0.337 | 0.768 | n/a | n/a | n/a | 0.265 | 0.696 | 0.342 | n/a | n/a | 0.381 | 0.776 | n/a | n/a | 0.214-0.49 | 0.279-0.481 |
| realerr (too small) | walk | all | 0.328 | 0.819 | 0.474 | n/a | n/a | n/a | 0.4 | 0.692 | n/a | n/a | n/a | 0.479 | 0.958 | 0.645 | n/a | n/a | 0.499 | 0.742 | n/a | n/a | 0.328-0.819 | 0.474-0.865 |

### java: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 564 | 4110 | 617 | 637 | 687 | 651 | 0.137 | 0.914 | 0.886 | 0.82 | 0.866 |
| broken | cursor minus parse | 1 | cycles M | 117 | 1030 | 163 | 185 | 282 | 188 | 0.114 | 0.716 | 0.63 | 0.413 | 0.619 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 22.6 | 201 | 30.9 | 32.6 | 54.4 | 34.3 | 0.112 | 0.731 | 0.693 | 0.415 | 0.659 |
| broken | cursor minus parse | all | instructions M | 562 | 4200 | 639 | 646 | 692 | 663 | 0.134 | 0.88 | 0.871 | 0.813 | 0.849 |
| broken | cursor minus parse | all | cycles M | 179 | 1480 | 256 | 172 | 280 | 249 | 0.121 | 0.698 | 1.04 | 0.638 | 0.718 |
| broken | cursor minus parse | all | CPU user+sys ms | 40.6 | 328 | 61.8 | 36.1 | 56.7 | 55.8 | 0.124 | 0.657 | 1.13 | 0.717 | 0.728 |
| broken | walk minus parse | 1 | instructions M | 544 | 3240 | 344 | n/a | n/a | n/a | 0.168 | 1.58 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 109 | 922 | 94.5 | n/a | n/a | n/a | 0.119 | 1.16 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 21.5 | 181 | 18.2 | n/a | n/a | n/a | 0.119 | 1.18 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 570 | 3380 | 355 | n/a | n/a | n/a | 0.169 | 1.6 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 148 | 1280 | 161 | n/a | n/a | n/a | 0.116 | 0.918 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 33.1 | 283 | 39 | n/a | n/a | n/a | 0.117 | 0.849 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1150 | 8860 | 1320 | 1360 | 1460 | 1390 | 0.13 | 0.874 | 0.849 | 0.789 | 0.829 |
| real | cursor minus parse | 1 | cycles M | 240 | 2120 | 321 | 354 | 613 | 330 | 0.113 | 0.748 | 0.678 | 0.392 | 0.728 |
| real | cursor minus parse | 1 | CPU user+sys ms | 46.5 | 412 | 61.2 | 67.8 | 117 | 63.1 | 0.113 | 0.76 | 0.685 | 0.396 | 0.737 |
| real | cursor minus parse | all | instructions M | 1150 | 8890 | 1310 | 1360 | 1450 | 1370 | 0.129 | 0.879 | 0.844 | 0.795 | 0.839 |
| real | cursor minus parse | all | cycles M | 409 | 3570 | 463 | 499 | 780 | 578 | 0.114 | 0.883 | 0.818 | 0.524 | 0.707 |
| real | cursor minus parse | all | CPU user+sys ms | 95.5 | 823 | 104 | 115 | 175 | 137 | 0.116 | 0.919 | 0.827 | 0.545 | 0.698 |
| real | walk minus parse | 1 | instructions M | 916 | 6430 | 687 | n/a | n/a | n/a | 0.142 | 1.33 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 194 | 1770 | 166 | n/a | n/a | n/a | 0.109 | 1.17 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 37.3 | 343 | 31.7 | n/a | n/a | n/a | 0.109 | 1.18 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 916 | 6480 | 686 | n/a | n/a | n/a | 0.141 | 1.33 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 308 | 2870 | 264 | n/a | n/a | n/a | 0.108 | 1.17 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 69.4 | 660 | 59.6 | n/a | n/a | n/a | 0.105 | 1.16 | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | 1 | instructions M | 0.0767 | 0.607 | 0.0849 | 0.0874 | 0.0941 | 0.0898 | 0.127 | 0.904 | 0.878 | 0.815 | 0.855 |
| realerr (too small) | cursor minus parse | 1 | cycles M | 0.016 | 0.163 | 0.0335 | 0.0358 | 0.0448 | 0.0394 | 0.0979 | 0.478 | 0.447 | 0.357 | 0.406 |
| realerr (too small) | cursor minus parse | 1 | CPU user+sys ms | 0.003 | 0.037 | 0.007 | 0.008 | 0.01 | 0.008 | 0.0811 | 0.429 | 0.375 | 0.3 | 0.375 |
| realerr (too small) | cursor minus parse | all | instructions M | 0.0889 | 0.616 | 0.0904 | 0.0881 | 0.0901 | 0.105 | 0.144 | 0.983 | 1.01 | 0.986 | 0.847 |
| realerr (too small) | cursor minus parse | all | cycles M | 0.0901 | 0.147 | 0.111 | 0.0182 | 0.0487 | 0.0362 | 0.611 | 0.81 | 4.96 | 1.85 | 2.49 |
| realerr (too small) | cursor minus parse | all | CPU user+sys ms | 0.018 | 0.023 | 0.034 | n/a | n/a | n/a | 0.783 | 0.529 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | instructions M | 0.0571 | 0.42 | 0.0421 | n/a | n/a | n/a | 0.136 | 1.35 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | cycles M | 0.0139 | 0.11 | 0.00792 | n/a | n/a | n/a | 0.126 | 1.75 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | CPU user+sys ms | 0.004 | 0.022 | 0.001 | n/a | n/a | n/a | 0.182 | 4 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | instructions M | 0.0658 | 0.424 | 0.047 | n/a | n/a | n/a | 0.155 | 1.4 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | cycles M | 0.0345 | 0.164 | 0.0337 | n/a | n/a | n/a | 0.211 | 1.02 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | CPU user+sys ms | n/a | 0.029 | 0.009 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

### java: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 116 | 201 | 49.1 | 0.576 | 2.35 | 116 | 201 | 49.1 | 0.576 | 2.35 | 108 | 192 | 44.3 | 0.562 | 2.44 |
| broken | cursor | all | 178 | 352 | 249 | 0.506 | 0.717 | 178 | 352 | 249 | 0.506 | 0.717 | 171 | 344 | 244 | 0.497 | 0.701 |
| broken | parse | 1 | 54.6 | 46.8 | 38.7 | 1.17 | 1.41 | 54.7 | 46.8 | 38.7 | 1.17 | 1.41 | 48.1 | 38.1 | 34.1 | 1.26 | 1.41 |
| broken | parse | all | 105 | 248 | 223 | 0.424 | 0.472 | 105 | 248 | 223 | 0.424 | 0.472 | 97.9 | 240 | 218 | 0.409 | 0.449 |
| broken | walk | 1 | 80.5 | 205 | 50.7 | 0.393 | 1.59 | 80.5 | 205 | 50.7 | 0.393 | 1.59 | 73.3 | 196 | 45.9 | 0.374 | 1.6 |
| broken | walk | all | 141 | 268 | 275 | 0.525 | 0.511 | 141 | 268 | 275 | 0.525 | 0.511 | 133 | 259 | 270 | 0.515 | 0.493 |
| real | cursor | 1 | 32.5 | 50.5 | 16.1 | 0.643 | 2.02 | 32.5 | 50.5 | 16.1 | 0.644 | 2.02 | 17.9 | 34.7 | 5.5 | 0.517 | 3.26 |
| real | cursor | all | 73.4 | 96.5 | 59.4 | 0.76 | 1.23 | 73.4 | 96.6 | 59.4 | 0.76 | 1.24 | 58.6 | 80.9 | 48.7 | 0.725 | 1.2 |
| real | parse | 1 | 20.9 | 30.2 | 14.9 | 0.69 | 1.4 | 20.9 | 30.2 | 14.9 | 0.69 | 1.4 | 6.27 | 14.3 | 4.3 | 0.438 | 1.46 |
| real | parse | all | 55.5 | 84.7 | 59.6 | 0.655 | 0.931 | 55.5 | 84.7 | 59.6 | 0.655 | 0.931 | 40.8 | 68.7 | 48.9 | 0.594 | 0.834 |
| real | walk | 1 | 20.7 | 47.1 | 16.4 | 0.439 | 1.26 | 20.7 | 47.1 | 16.4 | 0.439 | 1.26 | 6.25 | 31.4 | 5.85 | 0.199 | 1.07 |
| real | walk | all | 56.3 | 92.1 | 62.5 | 0.612 | 0.901 | 56.3 | 92.1 | 62.5 | 0.612 | 0.901 | 41.6 | 76.3 | 51.8 | 0.545 | 0.803 |
| realerr | cursor | 1 | 5 | 6.09 | 3.91 | 0.822 | 1.28 | 5.01 | 6.09 | 3.91 | 0.822 | 1.28 | 1.84 | 1.31 | 1.55 | 1.41 | 1.19 |
| realerr | cursor | all | 6.48 | 6.85 | 4.64 | 0.946 | 1.4 | 6.51 | 6.85 | 4.64 | 0.95 | 1.4 | 3.21 | 2.02 | 2.08 | 1.59 | 1.54 |
| realerr | parse | 1 | 5.02 | 5.86 | 2.95 | 0.857 | 1.7 | 5.02 | 5.86 | 2.95 | 0.857 | 1.7 | 1.77 | 0.957 | 0.633 | 1.84 | 2.79 |
| realerr | parse | all | 6.61 | 6.36 | 3.34 | 1.04 | 1.98 | 6.64 | 6.36 | 3.34 | 1.04 | 1.99 | 3.35 | 1.62 | 0.805 | 2.06 | 4.17 |
| realerr | walk | 1 | 5.09 | 6.04 | 3.86 | 0.843 | 1.32 | 5.1 | 6.05 | 3.86 | 0.842 | 1.32 | 1.83 | 1.27 | 1.46 | 1.44 | 1.25 |
| realerr | walk | all | 6.38 | 6.85 | 4.41 | 0.932 | 1.45 | 6.41 | 6.86 | 4.41 | 0.935 | 1.45 | 3.12 | 2.04 | 1.96 | 1.53 | 1.59 |

### java: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 54.6 | 51.4 | 38.8 | 1.06 | 1.41 | 54.6 | 51.4 | 38.8 | 1.06 | 1.41 | 47 | 42.9 | 34.2 | 1.1 | 1.37 |
| broken | all | 134 | 548 | 301 | 0.245 | 0.446 | 134 | 548 | 301 | 0.245 | 0.446 | 127 | 539 | 296 | 0.235 | 0.428 |
| real | 1 | 20.7 | 30 | 14.9 | 0.69 | 1.39 | 20.7 | 30 | 14.9 | 0.691 | 1.39 | 6.38 | 14.4 | 4.18 | 0.443 | 1.53 |
| real | all | 57.6 | 116 | 68.4 | 0.496 | 0.841 | 57.6 | 116 | 68.4 | 0.496 | 0.841 | 42.8 | 100 | 57.7 | 0.427 | 0.742 |
| realerr | 1 | 5.16 | 5.66 | 3.18 | 0.913 | 1.62 | 5.17 | 5.66 | 3.18 | 0.913 | 1.62 | 2.01 | 0.988 | 0.707 | 2.04 | 2.85 |
| realerr | all | 7.49 | 7.13 | 3.47 | 1.05 | 2.16 | 7.52 | 7.13 | 3.47 | 1.05 | 2.16 | 4.23 | 2.3 | 1 | 1.84 | 4.23 |

### java: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0134 (real), 4.5 (realerr), 0.192 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.109 | 3620 | 7.02 | 1200 | 112000 | 1200 | 112000 | 0 |
| real | steady | 0.0134 | 14.6 | 7.01 | 1200 | 112000 | 1200 | 112000 | 0 |
| realerr | cold | 31 | 86700 | 12.5 | 172 | 18700 | 172 | 18700 | 0.18 |
| realerr | steady | 4.5 | 104 | 11.5 | 152 | 13800 | 152 | 13800 | 0.0296 |
| broken | cold | 2.68 | 105000 | 7.9 | 2770 | 305000 | 2770 | 305000 | 0.000969 |
| broken | steady | 0.192 | 41600 | 7.88 | 2770 | 305000 | 2770 | 305000 | 0 |

### java: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2048 B | init ms | 0.00881 | 0.0118 | 0.006 | 0.745 | 1.47 | 15 |
| #0 2048 B | parse1 ms | 0.0895 | 0.114 | 0.0905 | 0.785 | 0.989 | 15 |
| #0 2048 B | init+parse1 ms | 0.099 | 0.127 | 0.097 | 0.778 | 1.02 | 15 |
| #0 2048 B | parse2 ms | 0.0233 | 0.0673 | 0.0443 | 0.346 | 0.527 | 15 |
| #0 2048 B | process user+sys ms | 0.802 | 1.18 | 0.425 | 0.682 | 1.89 | 15 |
| #0 2048 B | exec to exit instr user+kernel M | 4.21 | 6.64 | 2.7 | 0.635 | 1.56 | 15 |
| #0 2048 B | exec to exit instr user M | 1.55 | 2.98 | 1.73 | 0.519 | 0.895 | 15 |
| #0 2048 B | exec to exit instr kernel M | 2.63 | 3.62 | 0.969 | 0.726 | 2.71 | 15 |
| #0 2048 B | exec to exit cycles user+kernel M | 3.56 | 5.46 | 1.7 | 0.651 | 2.09 | 15 |
| #0 2048 B | exec to exit task-clock ms | 0.75 | 1.11 | 0.33 | 0.676 | 2.27 | 15 |
| #0 2048 B | peak RSS MB | 4.16 | 5.16 | 2.77 | 0.806 | 1.5 | 15 |
| #0 2048 B | peak footprint MB | 4.2 | 5.2 | 2.64 | 0.808 | 1.59 | 15 |
| #1 2232 B | init ms | 0.00841 | 0.0118 | 0.00612 | 0.716 | 1.37 | 15 |
| #1 2232 B | parse1 ms | 0.153 | 0.247 | 0.182 | 0.62 | 0.842 | 15 |
| #1 2232 B | init+parse1 ms | 0.163 | 0.26 | 0.187 | 0.628 | 0.873 | 15 |
| #1 2232 B | parse2 ms | 0.0713 | 0.215 | 0.124 | 0.332 | 0.576 | 15 |
| #1 2232 B | process user+sys ms | 0.929 | 1.44 | 0.589 | 0.643 | 1.58 | 15 |
| #1 2232 B | exec to exit instr user+kernel M | 6.79 | 10.9 | 5.54 | 0.622 | 1.22 | 15 |
| #1 2232 B | exec to exit instr user M | 4.05 | 7.23 | 4.53 | 0.56 | 0.895 | 15 |
| #1 2232 B | exec to exit instr kernel M | 2.7 | 3.68 | 1.02 | 0.734 | 2.66 | 15 |
| #1 2232 B | exec to exit cycles user+kernel M | 4.14 | 6.8 | 2.57 | 0.609 | 1.61 | 15 |
| #1 2232 B | exec to exit task-clock ms | 0.85 | 1.37 | 0.5 | 0.62 | 1.7 | 15 |
| #1 2232 B | peak RSS MB | 4.26 | 5.27 | 2.75 | 0.809 | 1.55 | 15 |
| #1 2232 B | peak footprint MB | 4.29 | 5.3 | 2.69 | 0.81 | 1.6 | 15 |

### java: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1498 | 1752742 | 0 | 0 | ok |
| realerr | 2 | 118 | 2 | 0 | ok |
| broken | 501 | 820664 | 413 | 0 | ok |

## javascript

### javascript: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 271 | 1220 | 507 | 518 | 560 | 556 | 0.221 | 0.534 | 0.523 | 0.483 | 0.487 | 275 | 1260 | 514 | 563 | 575 | 0.219 | 0.534 | 0.488 | 0.477 | 271-285 | 507-554 | 1.01 | 1.01 | 1 | 16.4 |
| broken | cursor | all | 73.1 | 269 | 118 | 119 | 127 | 130 | 0.271 | 0.618 | 0.612 | 0.574 | 0.563 | 75.6 | 289 | 126 | 133 | 134 | 0.262 | 0.599 | 0.568 | 0.564 | 73.1-79.6 | 118-134 | 6.08 * | 7.23 * | 7.01 * | 60.7 |
| broken | parse | 1 | 217 | 789 | 442 | 448 | 432 | 499 | 0.275 | 0.492 | 0.484 | 0.503 | 0.435 | 218 | 798 | 444 | 432 | 505 | 0.273 | 0.492 | 0.505 | 0.432 | 217-223 | 442-470 | 1 | 1 | 1 | 20.4 |
| broken | parse | all | 61.4 | 181 | 104 | 103 | 104 | 107 | 0.34 | 0.591 | 0.599 | 0.592 | 0.574 | 63.8 | 195 | 112 | 106 | 121 | 0.328 | 0.569 | 0.6 | 0.527 | 61.4-69 | 104-123 | 5.69 * | 6.78 * | 6.84 * | 72.3 |
| broken | walk | 1 | 306 | 1410 | 533 | n/a | n/a | n/a | 0.217 | 0.574 | n/a | n/a | n/a | 308 | 1440 | 547 | n/a | n/a | 0.214 | 0.563 | n/a | n/a | 306-373 | 533-578 | 1 | 1.02 | 1 | 14.5 |
| broken | walk | all | 74 | 275 | 111 | n/a | n/a | n/a | 0.269 | 0.666 | n/a | n/a | n/a | 77.9 | 286 | 121 | n/a | n/a | 0.272 | 0.642 | n/a | n/a | 74-82.3 | 111-138 | 6.68 * | 7.92 * | 7.57 * | 59.9 |
| real | cursor | 1 | 392 | 1750 | 743 | 803 | 817 | 805 | 0.225 | 0.528 | 0.488 | 0.48 | 0.487 | 402 | 1780 | 761 | 827 | 807 | 0.226 | 0.529 | 0.486 | 0.498 | 392-432 | 743-792 | 1 | 1.01 | 1 | 30.4 |
| real | cursor | all | 54.7 | 242 | 97.1 | 99.8 | 99 | 105 | 0.226 | 0.563 | 0.548 | 0.552 | 0.518 | 55.6 | 245 | 99.7 | 101 | 108 | 0.227 | 0.558 | 0.549 | 0.516 | 54.7-58.6 | 97.1-110 | 13.4 | 13.3 | 13.7 | 218 |
| real | parse | 1 | 313 | 1060 | 636 | 653 | 616 | 695 | 0.295 | 0.492 | 0.48 | 0.509 | 0.451 | 316 | 1070 | 653 | 637 | 697 | 0.294 | 0.483 | 0.496 | 0.453 | 313-324 | 636-673 | 1 | 1.01 | 1 | 38 |
| real | parse | all | 42.9 | 138 | 83.3 | 84.5 | 77.7 | 88.5 | 0.312 | 0.515 | 0.508 | 0.552 | 0.484 | 43.6 | 142 | 85.8 | 80.7 | 89.7 | 0.307 | 0.508 | 0.541 | 0.486 | 42.9-48.1 | 83.3-93 | 13.5 | 13.4 | 13.6 | 278 |
| real | walk | 1 | 380 | 1640 | 700 | n/a | n/a | n/a | 0.232 | 0.544 | n/a | n/a | n/a | 389 | 1660 | 721 | n/a | n/a | 0.234 | 0.54 | n/a | n/a | 380-416 | 700-730 | 1 | 1.01 | 1 | 31.3 |
| real | walk | all | 51.6 | 222 | 90.9 | n/a | n/a | n/a | 0.233 | 0.568 | n/a | n/a | n/a | 53.8 | 227 | 93.9 | n/a | n/a | 0.237 | 0.573 | n/a | n/a | 51.6-55.2 | 90.9-102 | 13.4 | 13.1 | 13.7 | 231 |
| realerr (too small) | cursor | 1 | 0.124 | 0.566 | 0.405 | 0.361 | 0.315 | 0.433 | 0.219 | 0.306 | 0.343 | 0.393 | 0.286 | 0.141 | 0.623 | 0.449 | 0.326 | 0.442 | 0.227 | 0.315 | 0.433 | 0.32 | 0.124-0.189 | 0.405-0.638 | 1.09 | 1.04 | 0.999 | 15.3 |
| realerr (too small) | cursor | all | 0.099 | 0.253 | 0.179 | 0.158 | 0.166 | 0.172 | 0.392 | 0.554 | 0.626 | 0.597 | 0.576 | 0.119 | 0.302 | 0.209 | 0.171 | 0.207 | 0.394 | 0.57 | 0.698 | 0.577 | 0.099-0.594 | 0.179-0.275 | 2.33 * | 3.18 * | 3.17 * | 19.2 |
| realerr (too small) | parse | 1 | 0.105 | 0.449 | 0.345 | 0.335 | 0.279 | 0.404 | 0.235 | 0.305 | 0.315 | 0.378 | 0.261 | 0.113 | 0.516 | 0.375 | 0.29 | 0.413 | 0.219 | 0.301 | 0.389 | 0.273 | 0.105-0.164 | 0.345-0.489 | 1.1 | 1.05 | 0.999 | 18 |
| realerr (too small) | parse | all | 0.0878 | 0.209 | 0.184 | 0.162 | 0.153 | 0.172 | 0.421 | 0.477 | 0.54 | 0.574 | 0.512 | 0.119 | 0.264 | 0.217 | 0.182 | 0.191 | 0.453 | 0.551 | 0.656 | 0.626 | 0.0878-0.705 | 0.184-0.308 | 2.5 * | 3.07 * | 2.91 * | 21.6 |
| realerr (too small) | walk | 1 | 0.114 | 0.55 | 0.39 | n/a | n/a | n/a | 0.206 | 0.291 | n/a | n/a | n/a | 0.127 | 0.583 | 0.418 | n/a | n/a | 0.218 | 0.304 | n/a | n/a | 0.114-0.189 | 0.39-0.502 | 1.11 | 1.05 | 0.999 | 16.7 |
| realerr (too small) | walk | all | 0.0823 | 0.228 | 0.177 | n/a | n/a | n/a | 0.36 | 0.465 | n/a | n/a | n/a | 0.122 | 0.296 | 0.199 | n/a | n/a | 0.412 | 0.613 | n/a | n/a | 0.0823-0.681 | 0.177-0.277 | 2.4 * | 3.32 * | 3.14 * | 23.1 |

### javascript: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 273 | 1230 | 507 | 518 | 560 | 556 | 0.221 | 0.538 | 0.527 | 0.487 | 0.491 | 277 | 1270 | 514 | 563 | 575 | 0.219 | 0.54 | 0.493 | 0.483 | 273-287 | 507-553 |
| broken | cursor | all | 448 | 2030 | 849 | 856 | 876 | 918 | 0.221 | 0.527 | 0.523 | 0.511 | 0.488 | 463 | 2070 | 889 | 893 | 935 | 0.223 | 0.521 | 0.519 | 0.495 | 448-471 | 849-924 |
| broken | parse | 1 | 217 | 792 | 442 | 448 | 432 | 499 | 0.274 | 0.492 | 0.485 | 0.503 | 0.436 | 219 | 802 | 444 | 432 | 505 | 0.273 | 0.492 | 0.506 | 0.432 | 217-223 | 442-470 |
| broken | parse | all | 352 | 1300 | 728 | 744 | 712 | 807 | 0.271 | 0.483 | 0.473 | 0.494 | 0.436 | 367 | 1320 | 775 | 718 | 841 | 0.277 | 0.473 | 0.511 | 0.436 | 352-381 | 728-808 |
| broken | walk | 1 | 306 | 1430 | 533 | n/a | n/a | n/a | 0.214 | 0.574 | n/a | n/a | n/a | 308 | 1460 | 547 | n/a | n/a | 0.21 | 0.564 | n/a | n/a | 306-374 | 533-578 |
| broken | walk | all | 500 | 2230 | 887 | n/a | n/a | n/a | 0.224 | 0.563 | n/a | n/a | n/a | 525 | 2270 | 935 | n/a | n/a | 0.232 | 0.561 | n/a | n/a | 500-552 | 887-954 |
| real | cursor | 1 | 393 | 1760 | 743 | 803 | 817 | 805 | 0.223 | 0.528 | 0.489 | 0.48 | 0.488 | 403 | 1800 | 761 | 827 | 807 | 0.224 | 0.529 | 0.487 | 0.499 | 393-434 | 743-792 |
| real | cursor | all | 723 | 3250 | 1370 | 1380 | 1350 | 1410 | 0.223 | 0.53 | 0.522 | 0.537 | 0.512 | 758 | 3260 | 1380 | 1380 | 1420 | 0.233 | 0.55 | 0.551 | 0.533 | 723-763 | 1370-1400 |
| real | parse | 1 | 314 | 1070 | 636 | 653 | 616 | 695 | 0.294 | 0.493 | 0.48 | 0.509 | 0.451 | 316 | 1080 | 653 | 637 | 697 | 0.293 | 0.484 | 0.496 | 0.453 | 314-325 | 636-673 |
| real | parse | all | 582 | 1890 | 1130 | 1160 | 1070 | 1250 | 0.308 | 0.514 | 0.503 | 0.545 | 0.467 | 599 | 1920 | 1180 | 1080 | 1250 | 0.312 | 0.506 | 0.555 | 0.477 | 582-609 | 1130-1190 |
| real | walk | 1 | 381 | 1650 | 700 | n/a | n/a | n/a | 0.23 | 0.544 | n/a | n/a | n/a | 390 | 1670 | 721 | n/a | n/a | 0.233 | 0.541 | n/a | n/a | 381-416 | 700-730 |
| real | walk | all | 684 | 2940 | 1270 | n/a | n/a | n/a | 0.232 | 0.538 | n/a | n/a | n/a | 722 | 2980 | 1300 | n/a | n/a | 0.242 | 0.556 | n/a | n/a | 684-732 | 1270-1310 |
| realerr (too small) | cursor | 1 | 0.134 | 0.588 | 0.405 | 0.36 | 0.315 | 0.432 | 0.228 | 0.331 | 0.372 | 0.425 | 0.31 | 0.156 | 0.648 | 0.449 | 0.326 | 0.442 | 0.241 | 0.347 | 0.479 | 0.353 | 0.134-0.208 | 0.405-0.633 |
| realerr (too small) | cursor | all | 0.289 | 0.822 | 0.567 | 0.568 | 0.541 | 0.57 | 0.352 | 0.51 | 0.509 | 0.534 | 0.507 | 0.418 | 0.978 | 0.658 | 0.568 | 0.605 | 0.427 | 0.635 | 0.736 | 0.691 | 0.289-0.822 | 0.567-0.797 |
| realerr (too small) | parse | 1 | 0.115 | 0.468 | 0.345 | 0.335 | 0.278 | 0.403 | 0.246 | 0.333 | 0.343 | 0.414 | 0.285 | 0.127 | 0.538 | 0.375 | 0.289 | 0.413 | 0.236 | 0.339 | 0.439 | 0.308 | 0.115-0.182 | 0.345-0.486 |
| realerr (too small) | parse | all | 0.275 | 0.672 | 0.544 | 0.513 | 0.532 | 0.555 | 0.409 | 0.506 | 0.536 | 0.517 | 0.495 | 0.38 | 0.842 | 0.632 | 0.574 | 0.597 | 0.451 | 0.601 | 0.662 | 0.637 | 0.275-1.68 | 0.544-0.853 |
| realerr (too small) | walk | 1 | 0.126 | 0.573 | 0.39 | n/a | n/a | n/a | 0.22 | 0.323 | n/a | n/a | n/a | 0.14 | 0.612 | 0.415 | n/a | n/a | 0.229 | 0.337 | n/a | n/a | 0.126-0.209 | 0.39-0.498 |
| realerr (too small) | walk | all | 0.27 | 0.755 | 0.546 | n/a | n/a | n/a | 0.358 | 0.495 | n/a | n/a | n/a | 0.365 | 0.958 | 0.634 | n/a | n/a | 0.381 | 0.576 | n/a | n/a | 0.27-1.63 | 0.546-0.824 |

### javascript: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 6540 | 23000 | 9620 | 9490 | 9540 | 10100 | 0.285 | 0.68 | 0.689 | 0.686 | 0.648 | 6540 | 23000 | 9620 | 9540 | 10100 | 0.284 | 0.68 | 0.686 | 0.647 | 6540-6540 | 9620-9630 |
| broken | cursor | all | 6550 | 23000 | 9620 | 9500 | 9540 | 10100 | 0.284 | 0.681 | 0.689 | 0.686 | 0.648 | 6560 | 23100 | 9640 | 9560 | 10100 | 0.284 | 0.681 | 0.686 | 0.648 | 6550-6610 | 9620-9660 |
| broken | parse | 1 | 5250 | 13400 | 8140 | 7980 | 7900 | 8550 | 0.39 | 0.645 | 0.658 | 0.665 | 0.614 | 5250 | 13400 | 8150 | 7910 | 8560 | 0.39 | 0.644 | 0.664 | 0.614 | 5250-5250 | 8140-8150 |
| broken | parse | all | 5260 | 13400 | 8150 | 8000 | 7930 | 8570 | 0.392 | 0.645 | 0.657 | 0.663 | 0.613 | 5260 | 13400 | 8190 | 7940 | 8590 | 0.391 | 0.642 | 0.663 | 0.612 | 5260-5280 | 8150-8240 |
| broken | walk | 1 | 7570 | 23700 | 10500 | n/a | n/a | n/a | 0.319 | 0.722 | n/a | n/a | n/a | 7580 | 23800 | 10500 | n/a | n/a | 0.318 | 0.722 | n/a | n/a | 7570-7600 | 10500-10500 |
| broken | walk | all | 7790 | 24100 | 10500 | n/a | n/a | n/a | 0.324 | 0.741 | n/a | n/a | n/a | 7820 | 24200 | 10500 | n/a | n/a | 0.324 | 0.743 | n/a | n/a | 7790-7860 | 10500-10600 |
| real | cursor | 1 | 9400 | 32600 | 13800 | 13700 | 13500 | 14500 | 0.288 | 0.682 | 0.688 | 0.699 | 0.65 | 9400 | 32700 | 13800 | 13500 | 14500 | 0.288 | 0.681 | 0.697 | 0.649 | 9400-9410 | 13800-13800 |
| real | cursor | all | 9410 | 32700 | 13800 | 13700 | 13500 | 14500 | 0.288 | 0.68 | 0.686 | 0.696 | 0.648 | 9410 | 32700 | 13900 | 13500 | 14500 | 0.287 | 0.678 | 0.695 | 0.647 | 9410-9460 | 13800-13900 |
| real | parse | 1 | 7390 | 17900 | 11500 | 11300 | 10900 | 12000 | 0.413 | 0.643 | 0.654 | 0.677 | 0.614 | 7390 | 17900 | 11500 | 10900 | 12100 | 0.413 | 0.642 | 0.676 | 0.613 | 7390-7390 | 11500-11600 |
| real | parse | all | 7390 | 17700 | 11500 | 11300 | 11000 | 12100 | 0.417 | 0.642 | 0.651 | 0.673 | 0.61 | 7390 | 17900 | 11600 | 11000 | 12100 | 0.413 | 0.638 | 0.671 | 0.609 | 7390-7390 | 11500-11600 |
| real | walk | 1 | 9010 | 28800 | 12800 | n/a | n/a | n/a | 0.313 | 0.707 | n/a | n/a | n/a | 9010 | 28800 | 12800 | n/a | n/a | 0.313 | 0.705 | n/a | n/a | 9010-9010 | 12800-12800 |
| real | walk | all | 9010 | 28900 | 12800 | n/a | n/a | n/a | 0.312 | 0.703 | n/a | n/a | n/a | 9010 | 28900 | 12800 | n/a | n/a | 0.312 | 0.702 | n/a | n/a | 9010-9010 | 12800-12900 |
| realerr (too small) | cursor | 1 | 2.58 | 10.3 | 6.56 | 6.47 | 6.23 | 6.7 | 0.251 | 0.393 | 0.398 | 0.414 | 0.385 | 2.58 | 10.4 | 6.56 | 6.23 | 6.7 | 0.25 | 0.394 | 0.415 | 0.385 | 2.58-2.59 | 6.56-6.56 |
| realerr (too small) | cursor | all | 2.78 | 10.2 | 6.49 | 6.41 | 6.16 | 6.6 | 0.272 | 0.428 | 0.433 | 0.451 | 0.42 | 3.25 | 10.6 | 6.55 | 6.18 | 6.69 | 0.306 | 0.496 | 0.526 | 0.486 | 2.78-8.87 | 6.49-6.61 |
| realerr (too small) | parse | 1 | 2.27 | 8.07 | 6.23 | 6.13 | 5.86 | 6.35 | 0.282 | 0.365 | 0.37 | 0.388 | 0.358 | 2.27 | 8.07 | 6.23 | 5.86 | 6.35 | 0.282 | 0.365 | 0.388 | 0.358 | 2.27-2.28 | 6.23-6.23 |
| realerr (too small) | parse | all | 2.51 | 7.95 | 6.17 | 6.03 | 5.81 | 6.26 | 0.316 | 0.408 | 0.417 | 0.432 | 0.401 | 2.81 | 8.32 | 6.22 | 5.84 | 6.34 | 0.338 | 0.452 | 0.482 | 0.444 | 2.51-13.4 | 6.17-6.24 |
| realerr (too small) | walk | 1 | 2.49 | 9.67 | 6.4 | n/a | n/a | n/a | 0.257 | 0.389 | n/a | n/a | n/a | 2.49 | 9.67 | 6.4 | n/a | n/a | 0.257 | 0.389 | n/a | n/a | 2.49-2.49 | 6.4-6.4 |
| realerr (too small) | walk | all | 2.73 | 9.59 | 6.33 | n/a | n/a | n/a | 0.285 | 0.432 | n/a | n/a | n/a | 3.06 | 9.98 | 6.38 | n/a | n/a | 0.307 | 0.48 | n/a | n/a | 2.73-13.5 | 6.33-6.45 |

### javascript: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1410 | 6380 | 2630 | 2680 | 2900 | 2880 | 0.221 | 0.537 | 0.526 | 0.487 | 0.49 | 1430 | 6490 | 2660 | 2910 | 2980 | 0.221 | 0.538 | 0.492 | 0.481 | 1410-1470 | 2630-2850 |
| broken | cursor | all | 2070 | 9240 | 3850 | 3990 | 4050 | 4210 | 0.224 | 0.538 | 0.52 | 0.512 | 0.492 | 2120 | 9350 | 4030 | 4090 | 4290 | 0.227 | 0.526 | 0.518 | 0.494 | 2070-2180 | 3850-4110 |
| broken | parse | 1 | 1130 | 4100 | 2280 | 2320 | 2230 | 2580 | 0.277 | 0.497 | 0.488 | 0.508 | 0.44 | 1140 | 4150 | 2290 | 2230 | 2610 | 0.275 | 0.497 | 0.51 | 0.436 | 1130-1160 | 2280-2420 |
| broken | parse | all | 1640 | 5950 | 3300 | 3370 | 3240 | 3720 | 0.276 | 0.497 | 0.487 | 0.507 | 0.442 | 1710 | 6000 | 3520 | 3280 | 3790 | 0.284 | 0.484 | 0.52 | 0.449 | 1640-1760 | 3300-3640 |
| broken | walk | 1 | 1600 | 7350 | 2760 | n/a | n/a | n/a | 0.217 | 0.578 | n/a | n/a | n/a | 1610 | 7500 | 2820 | n/a | n/a | 0.214 | 0.57 | n/a | n/a | 1600-1950 | 2760-2990 |
| broken | walk | all | 2300 | 10200 | 4080 | n/a | n/a | n/a | 0.226 | 0.564 | n/a | n/a | n/a | 2420 | 10300 | 4220 | n/a | n/a | 0.235 | 0.574 | n/a | n/a | 2300-2550 | 4080-4350 |
| real | cursor | 1 | 2040 | 9120 | 3880 | 4190 | 4270 | 4200 | 0.224 | 0.527 | 0.488 | 0.479 | 0.487 | 2100 | 9310 | 3970 | 4320 | 4210 | 0.225 | 0.528 | 0.486 | 0.498 | 2040-2250 | 3880-4120 |
| real | cursor | all | 3220 | 14400 | 6130 | 6240 | 6110 | 6370 | 0.224 | 0.526 | 0.517 | 0.528 | 0.506 | 3360 | 14400 | 6170 | 6210 | 6410 | 0.233 | 0.545 | 0.542 | 0.525 | 3220-3410 | 6130-6240 |
| real | parse | 1 | 1640 | 5540 | 3320 | 3410 | 3210 | 3630 | 0.295 | 0.493 | 0.48 | 0.509 | 0.451 | 1650 | 5600 | 3410 | 3320 | 3640 | 0.294 | 0.484 | 0.496 | 0.453 | 1640-1690 | 3320-3510 |
| real | parse | all | 2620 | 8450 | 5170 | 5250 | 4850 | 5630 | 0.31 | 0.507 | 0.499 | 0.541 | 0.465 | 2670 | 8560 | 5340 | 4880 | 5650 | 0.312 | 0.501 | 0.547 | 0.473 | 2620-2720 | 5170-5380 |
| real | walk | 1 | 1990 | 8580 | 3650 | n/a | n/a | n/a | 0.232 | 0.544 | n/a | n/a | n/a | 2030 | 8680 | 3760 | n/a | n/a | 0.234 | 0.54 | n/a | n/a | 1990-2160 | 3650-3810 |
| real | walk | all | 3100 | 13100 | 5730 | n/a | n/a | n/a | 0.237 | 0.54 | n/a | n/a | n/a | 3210 | 13200 | 5850 | n/a | n/a | 0.242 | 0.548 | n/a | n/a | 3100-3260 | 5730-5900 |
| realerr (too small) | cursor | 1 | 0.633 | 2.96 | 2.08 | 1.85 | 1.6 | 2.24 | 0.213 | 0.304 | 0.341 | 0.395 | 0.283 | 0.745 | 3.25 | 2.29 | 1.67 | 2.29 | 0.23 | 0.325 | 0.447 | 0.325 | 0.633-0.944 | 2.08-3.21 |
| realerr (too small) | cursor | all | 1.2 | 3.87 | 2.53 | 2.46 | 2.38 | 2.59 | 0.309 | 0.474 | 0.486 | 0.504 | 0.463 | 1.76 | 4.44 | 2.87 | 2.51 | 2.67 | 0.396 | 0.612 | 0.701 | 0.659 | 1.2-3.43 | 2.53-3.56 |
| realerr (too small) | parse | 1 | 0.554 | 2.35 | 1.78 | 1.72 | 1.43 | 2.08 | 0.236 | 0.312 | 0.322 | 0.387 | 0.266 | 0.593 | 2.69 | 1.9 | 1.48 | 2.13 | 0.22 | 0.312 | 0.4 | 0.279 | 0.554-0.836 | 1.78-2.45 |
| realerr (too small) | parse | all | 1.28 | 3.13 | 2.4 | 2.2 | 2.22 | 2.49 | 0.407 | 0.533 | 0.579 | 0.575 | 0.512 | 1.62 | 3.58 | 2.64 | 2.48 | 2.51 | 0.453 | 0.614 | 0.655 | 0.647 | 1.28-6.61 | 2.4-3.5 |
| realerr (too small) | walk | 1 | 0.599 | 2.88 | 2 | n/a | n/a | n/a | 0.208 | 0.299 | n/a | n/a | n/a | 0.662 | 3.04 | 2.14 | n/a | n/a | 0.217 | 0.309 | n/a | n/a | 0.599-0.925 | 2-2.53 |
| realerr (too small) | walk | all | 1.23 | 3.66 | 2.39 | n/a | n/a | n/a | 0.335 | 0.514 | n/a | n/a | n/a | 1.62 | 4.39 | 2.71 | n/a | n/a | 0.368 | 0.597 | n/a | n/a | 1.23-6.54 | 2.39-3.16 |

### javascript: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1290 | 9530 | 1470 | 1520 | 1640 | 1550 | 0.136 | 0.878 | 0.852 | 0.79 | 0.833 |
| broken | cursor minus parse | 1 | cycles M | 277 | 2280 | 347 | 362 | 669 | 300 | 0.122 | 0.799 | 0.767 | 0.414 | 0.924 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 55.5 | 440 | 65.7 | 69.4 | 129 | 57.5 | 0.126 | 0.845 | 0.8 | 0.432 | 0.965 |
| broken | cursor minus parse | all | instructions M | 1290 | 9600 | 1470 | 1500 | 1620 | 1520 | 0.134 | 0.877 | 0.859 | 0.797 | 0.845 |
| broken | cursor minus parse | all | cycles M | 430 | 3280 | 550 | 613 | 806 | 497 | 0.131 | 0.781 | 0.701 | 0.533 | 0.865 |
| broken | cursor minus parse | all | CPU user+sys ms | 96 | 730 | 121 | 112 | 164 | 110 | 0.132 | 0.793 | 0.858 | 0.585 | 0.871 |
| broken | walk minus parse | 1 | instructions M | 2320 | 10300 | 2350 | n/a | n/a | n/a | 0.226 | 0.99 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 464 | 3250 | 481 | n/a | n/a | n/a | 0.143 | 0.964 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 89 | 638 | 91.9 | n/a | n/a | n/a | 0.139 | 0.969 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 2540 | 10600 | 2370 | n/a | n/a | n/a | 0.238 | 1.07 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 658 | 4220 | 776 | n/a | n/a | n/a | 0.156 | 0.848 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 148 | 933 | 159 | n/a | n/a | n/a | 0.158 | 0.928 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 2010 | 14700 | 2300 | 2370 | 2550 | 2420 | 0.137 | 0.874 | 0.849 | 0.79 | 0.83 |
| real | cursor minus parse | 1 | cycles M | 409 | 3580 | 558 | 783 | 1050 | 574 | 0.114 | 0.733 | 0.522 | 0.388 | 0.712 |
| real | cursor minus parse | 1 | CPU user+sys ms | 78.9 | 691 | 107 | 150 | 201 | 110 | 0.114 | 0.741 | 0.527 | 0.392 | 0.717 |
| real | cursor minus parse | all | instructions M | 2010 | 15000 | 2310 | 2350 | 2540 | 2390 | 0.134 | 0.872 | 0.855 | 0.793 | 0.843 |
| real | cursor minus parse | all | cycles M | 605 | 5930 | 963 | 982 | 1260 | 743 | 0.102 | 0.628 | 0.616 | 0.479 | 0.814 |
| real | cursor minus parse | all | CPU user+sys ms | 141 | 1360 | 232 | 229 | 279 | 167 | 0.104 | 0.606 | 0.616 | 0.505 | 0.844 |
| real | walk minus parse | 1 | instructions M | 1620 | 10900 | 1260 | n/a | n/a | n/a | 0.149 | 1.29 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 350 | 3030 | 331 | n/a | n/a | n/a | 0.115 | 1.06 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 67.1 | 588 | 63.5 | n/a | n/a | n/a | 0.114 | 1.06 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1620 | 11200 | 1310 | n/a | n/a | n/a | 0.145 | 1.24 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 476 | 4620 | 563 | n/a | n/a | n/a | 0.103 | 0.844 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 102 | 1050 | 138 | n/a | n/a | n/a | 0.0969 | 0.74 | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | 1 | instructions M | 0.308 | 2.23 | 0.332 | 0.342 | 0.368 | 0.351 | 0.138 | 0.926 | 0.901 | 0.835 | 0.877 |
| realerr (too small) | cursor minus parse | 1 | cycles M | 0.0782 | 0.612 | 0.305 | 0.133 | 0.17 | 0.155 | 0.128 | 0.257 | 0.589 | 0.459 | 0.506 |
| realerr (too small) | cursor minus parse | 1 | CPU user+sys ms | 0.019 | 0.12 | 0.06 | 0.025 | 0.037 | 0.029 | 0.158 | 0.317 | 0.76 | 0.514 | 0.655 |
| realerr (too small) | cursor minus parse | all | instructions M | 0.264 | 2.27 | 0.328 | 0.376 | 0.349 | 0.34 | 0.116 | 0.805 | 0.702 | 0.756 | 0.777 |
| realerr (too small) | cursor minus parse | all | cycles M | n/a | 0.737 | 0.133 | 0.259 | 0.157 | 0.0923 | n/a | n/a | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | all | CPU user+sys ms | 0.014 | 0.15 | 0.023 | 0.055 | 0.009 | 0.015 | 0.0933 | 0.609 | 0.255 | 1.56 | 0.933 |
| realerr (too small) | walk minus parse | 1 | instructions M | 0.214 | 1.6 | 0.167 | n/a | n/a | n/a | 0.133 | 1.28 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | cycles M | 0.0449 | 0.533 | 0.225 | n/a | n/a | n/a | 0.0842 | 0.199 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | CPU user+sys ms | 0.011 | 0.105 | 0.045 | n/a | n/a | n/a | 0.105 | 0.244 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | instructions M | 0.221 | 1.64 | 0.16 | n/a | n/a | n/a | 0.135 | 1.39 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | cycles M | n/a | 0.527 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | CPU user+sys ms | n/a | 0.083 | 0.002 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

### javascript: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 159 | 297 | 74.1 | 0.533 | 2.14 | 159 | 297 | 74.1 | 0.533 | 2.14 | 149 | 287 | 67.4 | 0.521 | 2.22 |
| broken | cursor | all | 371 | 703 | 481 | 0.528 | 0.771 | 371 | 703 | 481 | 0.528 | 0.771 | 362 | 693 | 475 | 0.522 | 0.762 |
| broken | parse | 1 | 75.5 | 69.3 | 59.6 | 1.09 | 1.27 | 75.5 | 69.3 | 59.6 | 1.09 | 1.27 | 66.5 | 58.9 | 52.9 | 1.13 | 1.26 |
| broken | parse | all | 246 | 562 | 398 | 0.437 | 0.616 | 246 | 562 | 398 | 0.437 | 0.616 | 236 | 551 | 392 | 0.428 | 0.602 |
| broken | walk | 1 | 107 | 349 | 71.6 | 0.306 | 1.49 | 107 | 349 | 71.6 | 0.306 | 1.49 | 97.7 | 338 | 64.8 | 0.289 | 1.51 |
| broken | walk | all | 287 | 634 | 566 | 0.453 | 0.507 | 287 | 634 | 566 | 0.453 | 0.507 | 277 | 623 | 559 | 0.445 | 0.496 |
| real | cursor | 1 | 51.4 | 118 | 28.2 | 0.435 | 1.82 | 51.4 | 118 | 28.2 | 0.435 | 1.82 | 33.9 | 99.4 | 14.4 | 0.341 | 2.35 |
| real | cursor | all | 127 | 264 | 162 | 0.479 | 0.78 | 127 | 264 | 162 | 0.479 | 0.781 | 109 | 245 | 148 | 0.445 | 0.736 |
| real | parse | 1 | 32.2 | 44.6 | 26.8 | 0.72 | 1.2 | 32.2 | 44.6 | 26.8 | 0.721 | 1.2 | 14.7 | 25.9 | 12.9 | 0.566 | 1.14 |
| real | parse | all | 99.9 | 229 | 162 | 0.436 | 0.618 | 99.9 | 229 | 162 | 0.436 | 0.618 | 82.3 | 210 | 147 | 0.392 | 0.558 |
| real | walk | 1 | 32.5 | 108 | 27.8 | 0.3 | 1.17 | 32.5 | 108 | 27.8 | 0.3 | 1.17 | 15 | 89.7 | 14 | 0.167 | 1.07 |
| real | walk | all | 102 | 250 | 157 | 0.408 | 0.651 | 102 | 250 | 157 | 0.408 | 0.651 | 84.6 | 232 | 143 | 0.365 | 0.592 |
| realerr | cursor | 1 | 4.98 | 6.27 | 4.47 | 0.795 | 1.11 | 4.98 | 6.27 | 4.47 | 0.795 | 1.11 | 1.8 | 1.52 | 2.04 | 1.18 | 0.883 |
| realerr | cursor | all | 7.95 | 7.83 | 4.41 | 1.01 | 1.8 | 7.97 | 7.84 | 4.41 | 1.02 | 1.81 | 4.66 | 3.14 | 1.89 | 1.48 | 2.46 |
| realerr | parse | 1 | 5.06 | 6.07 | 3.05 | 0.834 | 1.66 | 5.07 | 6.07 | 3.05 | 0.835 | 1.66 | 1.75 | 1.16 | 0.641 | 1.51 | 2.74 |
| realerr | parse | all | 8.25 | 7.59 | 3.71 | 1.09 | 2.23 | 8.27 | 7.59 | 3.71 | 1.09 | 2.23 | 4.83 | 2.88 | 1.16 | 1.68 | 4.18 |
| realerr | walk | 1 | 5.06 | 6.3 | 3.92 | 0.802 | 1.29 | 5.07 | 6.31 | 3.92 | 0.803 | 1.29 | 1.76 | 1.52 | 1.57 | 1.16 | 1.12 |
| realerr | walk | all | 8.09 | 7.95 | 4.51 | 1.02 | 1.79 | 8.11 | 7.96 | 4.51 | 1.02 | 1.8 | 4.66 | 3.09 | 1.93 | 1.51 | 2.41 |

### javascript: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 75.4 | 104 | 59.5 | 0.728 | 1.27 | 75.4 | 104 | 59.5 | 0.728 | 1.27 | 66.5 | 93.5 | 52.9 | 0.711 | 1.26 |
| broken | all | 269 | 1130 | 589 | 0.238 | 0.457 | 269 | 1130 | 589 | 0.238 | 0.457 | 259 | 1120 | 582 | 0.232 | 0.445 |
| real | 1 | 31.9 | 58 | 26.9 | 0.551 | 1.19 | 32 | 58 | 26.9 | 0.551 | 1.19 | 14.5 | 39.1 | 12.9 | 0.371 | 1.13 |
| real | all | 105 | 341 | 185 | 0.307 | 0.567 | 105 | 341 | 185 | 0.307 | 0.567 | 87.2 | 323 | 171 | 0.27 | 0.51 |
| realerr | 1 | 5.64 | 5.93 | 3.24 | 0.951 | 1.74 | 5.65 | 5.93 | 3.24 | 0.952 | 1.74 | 2.32 | 1.27 | 0.801 | 1.83 | 2.9 |
| realerr | all | 9.3 | 8.94 | 4.21 | 1.04 | 2.21 | 9.33 | 8.94 | 4.21 | 1.04 | 2.22 | 5.89 | 4.23 | 1.66 | 1.39 | 3.55 |

### javascript: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0134 (real), 1 (realerr), 0.0591 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.183 | 11400 | 8.22 | 2160 | 203000 | 2160 | 203000 | 0 |
| real | steady | 0.0134 | 11.4 | 8.22 | 2160 | 203000 | 2160 | 203000 | 0 |
| realerr | cold | 18.2 | 20900 | 9.89 | 97.7 | 9360 | 97.7 | 9360 | 0.187 |
| realerr | steady | 1 | 23.1 | 9.78 | 95 | 8760 | 95 | 8760 | 0.0105 |
| broken | cold | 2.98 | 162000 | 8.57 | 5060 | 586000 | 5060 | 586000 | 0.00059 |
| broken | steady | 0.0591 | 3170 | 8.53 | 5060 | 585000 | 5060 | 585000 | 0 |

### javascript: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2047 B | init ms | 0.00952 | 0.0125 | 0.00584 | 0.763 | 1.63 | 15 |
| #0 2047 B | parse1 ms | 0.158 | 0.2 | 0.161 | 0.79 | 0.984 | 15 |
| #0 2047 B | init+parse1 ms | 0.168 | 0.213 | 0.166 | 0.79 | 1.01 | 15 |
| #0 2047 B | parse2 ms | 0.0585 | 0.148 | 0.094 | 0.394 | 0.623 | 15 |
| #0 2047 B | process user+sys ms | 0.938 | 1.34 | 0.556 | 0.701 | 1.69 | 15 |
| #0 2047 B | exec to exit instr user+kernel M | 5.64 | 8.77 | 4.11 | 0.643 | 1.37 | 15 |
| #0 2047 B | exec to exit instr user M | 2.97 | 5.11 | 3.11 | 0.581 | 0.954 | 15 |
| #0 2047 B | exec to exit instr kernel M | 2.68 | 3.65 | 0.998 | 0.733 | 2.68 | 15 |
| #0 2047 B | exec to exit cycles user+kernel M | 4.2 | 6.27 | 2.34 | 0.67 | 1.79 | 15 |
| #0 2047 B | exec to exit task-clock ms | 0.85 | 1.27 | 0.45 | 0.669 | 1.89 | 15 |
| #0 2047 B | peak RSS MB | 4.1 | 5.28 | 2.98 | 0.776 | 1.38 | 15 |
| #0 2047 B | peak footprint MB | 4.13 | 5.31 | 2.91 | 0.778 | 1.42 | 15 |
| #1 2198 B | init ms | 0.00852 | 0.0124 | 0.00592 | 0.688 | 1.44 | 15 |
| #1 2198 B | parse1 ms | 0.097 | 0.117 | 0.103 | 0.829 | 0.944 | 15 |
| #1 2198 B | init+parse1 ms | 0.105 | 0.129 | 0.109 | 0.815 | 0.97 | 15 |
| #1 2198 B | parse2 ms | 0.027 | 0.0717 | 0.0487 | 0.377 | 0.554 | 15 |
| #1 2198 B | process user+sys ms | 0.816 | 1.16 | 0.44 | 0.706 | 1.85 | 15 |
| #1 2198 B | exec to exit instr user+kernel M | 4.34 | 6.75 | 2.74 | 0.643 | 1.58 | 15 |
| #1 2198 B | exec to exit instr user M | 1.7 | 3.1 | 1.77 | 0.547 | 0.961 | 15 |
| #1 2198 B | exec to exit instr kernel M | 2.68 | 3.61 | 0.97 | 0.742 | 2.76 | 15 |
| #1 2198 B | exec to exit cycles user+kernel M | 3.51 | 5.2 | 1.76 | 0.674 | 2 | 15 |
| #1 2198 B | exec to exit task-clock ms | 0.75 | 1.09 | 0.34 | 0.688 | 2.21 | 15 |
| #1 2198 B | peak RSS MB | 4.04 | 5.21 | 2.9 | 0.774 | 1.39 | 15 |
| #1 2198 B | peak footprint MB | 4.07 | 5.25 | 2.84 | 0.775 | 1.43 | 15 |

### javascript: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1344 | 2929470 | 0 | 0 | ok |
| realerr | 9 | 443 | 9 | 0 | ok |
| broken | 508 | 1902242 | 415 | 0 | ok |

## json

### json: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 166 | 862 | 300 | 307 | 377 | 356 | 0.193 | 0.553 | 0.541 | 0.441 | 0.467 | 168 | 879 | 309 | 378 | 359 | 0.191 | 0.543 | 0.444 | 0.467 | 166-175 | 300-319 | 1.01 | 1.01 | 1 | 48.7 |
| broken | cursor | all | 32.1 | 175 | 53.9 | 55.2 | 66.7 | 67.1 | 0.184 | 0.596 | 0.582 | 0.482 | 0.479 | 35 | 181 | 59.6 | 71.6 | 67.8 | 0.194 | 0.588 | 0.489 | 0.517 | 32.1-42.8 | 53.9-66.3 | 8.42 * | 8.36 * | 8.91 * | 252 |
| broken | parse | 1 | 126 | 451 | 245 | 251 | 266 | 295 | 0.28 | 0.516 | 0.503 | 0.474 | 0.427 | 129 | 458 | 248 | 267 | 297 | 0.282 | 0.521 | 0.482 | 0.434 | 126-138 | 245-254 | 1 | 1.01 | 1 | 64 |
| broken | parse | all | 20 | 81.7 | 41.8 | 44.2 | 45 | 54.2 | 0.245 | 0.479 | 0.453 | 0.444 | 0.369 | 24.2 | 89.1 | 47.5 | 48.1 | 57 | 0.271 | 0.509 | 0.503 | 0.425 | 20-26.3 | 41.8-52 | 9.47 * | 8.43 * | 9.12 * | 404 |
| broken | walk | 1 | 214 | 1050 | 345 | n/a | n/a | n/a | 0.203 | 0.621 | n/a | n/a | n/a | 218 | 1080 | 353 | n/a | n/a | 0.203 | 0.618 | n/a | n/a | 214-240 | 345-394 | 1 | 1.01 | 1 | 37.8 |
| broken | walk | all | 61.7 | 290 | 80.1 | n/a | n/a | n/a | 0.213 | 0.771 | n/a | n/a | n/a | 62.8 | 297 | 88.4 | n/a | n/a | 0.211 | 0.71 | n/a | n/a | 61.7-85.2 | 80.1-99.2 | 5.56 * | 5.61 * | 6.49 * | 131 |
| real | cursor | 1 | 186 | 952 | 342 | 351 | 426 | 389 | 0.196 | 0.545 | 0.531 | 0.438 | 0.479 | 194 | 969 | 350 | 428 | 394 | 0.2 | 0.553 | 0.453 | 0.492 | 186-203 | 342-365 | 1 | 1.01 | 1 | 64.3 |
| real | cursor | all | 27.7 | 147 | 49.4 | 49.3 | 56.6 | 54.8 | 0.188 | 0.56 | 0.562 | 0.489 | 0.504 | 29.4 | 150 | 51.7 | 57.4 | 56.7 | 0.196 | 0.57 | 0.513 | 0.52 | 27.7-30.6 | 49.4-57.7 | 12.1 | 12 | 12.2 | 433 |
| real | parse | 1 | 140 | 465 | 280 | 283 | 298 | 322 | 0.301 | 0.501 | 0.495 | 0.47 | 0.435 | 145 | 474 | 287 | 303 | 324 | 0.306 | 0.506 | 0.479 | 0.449 | 140-150 | 280-328 | 1 | 1.01 | 1 | 85.5 |
| real | parse | all | 20.9 | 67.2 | 40.2 | 39.5 | 41 | 44.8 | 0.312 | 0.521 | 0.531 | 0.511 | 0.467 | 22.1 | 69.5 | 42 | 41.9 | 44.9 | 0.318 | 0.526 | 0.528 | 0.493 | 20.9-23.7 | 40.2-43.3 | 12 | 12.2 | 12.3 | 572 |
| real | walk | 1 | 183 | 888 | 319 | n/a | n/a | n/a | 0.206 | 0.574 | n/a | n/a | n/a | 185 | 913 | 328 | n/a | n/a | 0.202 | 0.563 | n/a | n/a | 183-193 | 319-337 | 1 | 1.01 | 1 | 65.4 |
| real | walk | all | 27.3 | 133 | 47.1 | n/a | n/a | n/a | 0.206 | 0.579 | n/a | n/a | n/a | 28.2 | 135 | 48 | n/a | n/a | 0.209 | 0.587 | n/a | n/a | 27.3-29.1 | 47.1-53.2 | 12 | 12 | 12.3 | 439 |
| realerr | cursor | 1 | 2.98 | 11.8 | 5.23 | 4.88 | 5.63 | 5.71 | 0.253 | 0.57 | 0.61 | 0.53 | 0.522 | 3.02 | 12.4 | 5.51 | 5.68 | 5.81 | 0.243 | 0.548 | 0.532 | 0.52 | 2.98-3.15 | 5.23-5.96 | 1.01 | 1.01 | 1 | 62.6 |
| realerr | cursor | all | 0.658 | 3.02 | 1.02 | 1.02 | 1.18 | 1.24 | 0.218 | 0.643 | 0.643 | 0.556 | 0.531 | 0.796 | 3.36 | 1.26 | 1.22 | 1.44 | 0.237 | 0.63 | 0.652 | 0.553 | 0.658-1.07 | 1.02-1.53 | 6.13 * | 5.24 * | 6.48 * | 284 |
| realerr | parse | 1 | 2.54 | 7.57 | 4.64 | 4.46 | 4.5 | 5.22 | 0.336 | 0.548 | 0.569 | 0.565 | 0.487 | 2.63 | 7.73 | 4.82 | 4.51 | 5.41 | 0.34 | 0.545 | 0.583 | 0.485 | 2.54-2.95 | 4.64-5.81 | 1.01 | 1.01 | 0.999 | 73.4 |
| realerr | parse | all | 0.51 | 1.35 | 0.849 | 0.838 | 0.934 | 0.97 | 0.379 | 0.601 | 0.609 | 0.546 | 0.526 | 0.606 | 1.73 | 0.933 | 1.01 | 1.09 | 0.351 | 0.649 | 0.6 | 0.555 | 0.51-0.777 | 0.849-1.26 | 6.81 * | 6.99 * | 7.16 * | 366 |
| realerr | walk | 1 | 3.27 | 13.3 | 5.46 | n/a | n/a | n/a | 0.246 | 0.6 | n/a | n/a | n/a | 3.31 | 13.6 | 5.97 | n/a | n/a | 0.243 | 0.555 | n/a | n/a | 3.27-3.86 | 5.46-7.66 | 1 | 1.02 | 1 | 57 |
| realerr | walk | all | 0.673 | 2.79 | 1.04 | n/a | n/a | n/a | 0.242 | 0.647 | n/a | n/a | n/a | 0.802 | 3.12 | 1.26 | n/a | n/a | 0.257 | 0.636 | n/a | n/a | 0.673-0.957 | 1.04-1.4 | 6.56 * | 6.2 * | 6.95 * | 278 |

### json: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 167 | 869 | 300 | 307 | 377 | 356 | 0.192 | 0.555 | 0.543 | 0.443 | 0.469 | 169 | 885 | 309 | 378 | 359 | 0.19 | 0.546 | 0.446 | 0.47 | 167-177 | 300-319 |
| broken | cursor | all | 288 | 1480 | 515 | 481 | 580 | 564 | 0.194 | 0.56 | 0.6 | 0.498 | 0.511 | 300 | 1530 | 533 | 584 | 589 | 0.197 | 0.562 | 0.514 | 0.509 | 288-315 | 515-548 |
| broken | parse | 1 | 126 | 454 | 245 | 251 | 266 | 295 | 0.279 | 0.517 | 0.503 | 0.475 | 0.428 | 129 | 460 | 248 | 267 | 297 | 0.281 | 0.521 | 0.483 | 0.434 | 126-138 | 245-254 |
| broken | parse | all | 218 | 742 | 414 | 407 | 421 | 482 | 0.294 | 0.527 | 0.536 | 0.518 | 0.452 | 227 | 753 | 436 | 433 | 491 | 0.302 | 0.521 | 0.525 | 0.463 | 218-238 | 414-447 |
| broken | walk | 1 | 214 | 1070 | 345 | n/a | n/a | n/a | 0.201 | 0.621 | n/a | n/a | n/a | 218 | 1090 | 353 | n/a | n/a | 0.2 | 0.618 | n/a | n/a | 214-240 | 345-394 |
| broken | walk | all | 348 | 1640 | 559 | n/a | n/a | n/a | 0.212 | 0.623 | n/a | n/a | n/a | 358 | 1670 | 582 | n/a | n/a | 0.214 | 0.616 | n/a | n/a | 348-381 | 559-597 |
| real | cursor | 1 | 186 | 960 | 342 | 351 | 425 | 389 | 0.194 | 0.545 | 0.531 | 0.438 | 0.48 | 195 | 976 | 350 | 428 | 394 | 0.199 | 0.556 | 0.454 | 0.493 | 186-204 | 342-365 |
| real | cursor | all | 339 | 1790 | 613 | 612 | 664 | 679 | 0.19 | 0.553 | 0.554 | 0.511 | 0.499 | 357 | 1810 | 639 | 674 | 698 | 0.197 | 0.558 | 0.529 | 0.511 | 339-365 | 613-654 |
| real | parse | 1 | 140 | 468 | 280 | 283 | 298 | 322 | 0.3 | 0.501 | 0.495 | 0.47 | 0.435 | 145 | 477 | 287 | 303 | 324 | 0.305 | 0.506 | 0.48 | 0.45 | 140-150 | 280-328 |
| real | parse | all | 261 | 819 | 504 | 483 | 506 | 552 | 0.318 | 0.518 | 0.54 | 0.515 | 0.472 | 266 | 846 | 516 | 513 | 568 | 0.315 | 0.515 | 0.518 | 0.468 | 261-270 | 504-530 |
| real | walk | 1 | 183 | 895 | 319 | n/a | n/a | n/a | 0.205 | 0.574 | n/a | n/a | n/a | 185 | 921 | 328 | n/a | n/a | 0.201 | 0.564 | n/a | n/a | 183-193 | 319-337 |
| real | walk | all | 324 | 1600 | 571 | n/a | n/a | n/a | 0.203 | 0.568 | n/a | n/a | n/a | 343 | 1620 | 601 | n/a | n/a | 0.211 | 0.57 | n/a | n/a | 324-348 | 571-622 |
| realerr | cursor | 1 | 2.99 | 11.8 | 5.23 | 4.89 | 5.62 | 5.71 | 0.254 | 0.571 | 0.611 | 0.531 | 0.523 | 3.03 | 13 | 5.51 | 5.68 | 5.81 | 0.233 | 0.55 | 0.534 | 0.522 | 2.99-3.25 | 5.23-5.96 |
| realerr | cursor | all | 4.46 | 17.2 | 7.46 | 6.77 | 7.58 | 8.16 | 0.26 | 0.598 | 0.659 | 0.589 | 0.546 | 4.92 | 18 | 7.96 | 7.79 | 8.44 | 0.273 | 0.618 | 0.631 | 0.583 | 4.46-5.65 | 7.46-8.89 |
| realerr | parse | 1 | 2.56 | 7.64 | 4.64 | 4.46 | 4.5 | 5.22 | 0.334 | 0.551 | 0.573 | 0.568 | 0.49 | 2.64 | 7.75 | 4.81 | 4.51 | 5.41 | 0.341 | 0.548 | 0.586 | 0.488 | 2.56-2.99 | 4.64-5.8 |
| realerr | parse | all | 3.9 | 11.1 | 6.53 | 5.95 | 6.47 | 7.5 | 0.352 | 0.597 | 0.655 | 0.603 | 0.52 | 4.29 | 11.9 | 7.21 | 6.74 | 7.6 | 0.362 | 0.595 | 0.636 | 0.564 | 3.9-4.62 | 6.53-7.83 |
| realerr | walk | 1 | 3.28 | 13.3 | 5.46 | n/a | n/a | n/a | 0.246 | 0.601 | n/a | n/a | n/a | 3.33 | 14 | 5.96 | n/a | n/a | 0.237 | 0.558 | n/a | n/a | 3.28-3.86 | 5.46-7.66 |
| realerr | walk | all | 4.78 | 18.5 | 7.97 | n/a | n/a | n/a | 0.258 | 0.6 | n/a | n/a | n/a | 5.12 | 19.5 | 8.58 | n/a | n/a | 0.263 | 0.596 | n/a | n/a | 4.78-5.7 | 7.97-9.01 |

### json: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 4430 | 17200 | 6090 | 5910 | 6270 | 6490 | 0.258 | 0.728 | 0.75 | 0.708 | 0.684 | 4430 | 17200 | 6110 | 6290 | 6510 | 0.258 | 0.725 | 0.705 | 0.681 | 4430-4440 | 6090-6130 |
| broken | cursor | all | 4430 | 17200 | 6140 | 5960 | 6320 | 6530 | 0.258 | 0.722 | 0.744 | 0.702 | 0.679 | 4440 | 17200 | 6160 | 6320 | 6550 | 0.258 | 0.721 | 0.702 | 0.678 | 4430-4470 | 6140-6170 |
| broken | parse | 1 | 3370 | 7820 | 4880 | 4660 | 4910 | 5200 | 0.431 | 0.69 | 0.723 | 0.687 | 0.649 | 3370 | 7820 | 4890 | 4910 | 5200 | 0.431 | 0.69 | 0.687 | 0.648 | 3370-3370 | 4880-4920 |
| broken | parse | all | 3370 | 7760 | 4920 | 4710 | 4950 | 5250 | 0.434 | 0.685 | 0.717 | 0.681 | 0.643 | 3370 | 7820 | 4940 | 4960 | 5260 | 0.432 | 0.683 | 0.68 | 0.642 | 3370-3370 | 4920-4960 |
| broken | walk | 1 | 5710 | 18000 | 7260 | n/a | n/a | n/a | 0.317 | 0.786 | n/a | n/a | n/a | 5720 | 18100 | 7260 | n/a | n/a | 0.316 | 0.787 | n/a | n/a | 5710-5730 | 7260-7290 |
| broken | walk | all | 5730 | 18100 | 7290 | n/a | n/a | n/a | 0.317 | 0.785 | n/a | n/a | n/a | 5740 | 18200 | 7310 | n/a | n/a | 0.316 | 0.784 | n/a | n/a | 5730-5750 | 7290-7330 |
| real | cursor | 1 | 5050 | 19500 | 7040 | 6800 | 7230 | 7500 | 0.259 | 0.717 | 0.742 | 0.698 | 0.673 | 5050 | 19500 | 7070 | 7260 | 7530 | 0.259 | 0.714 | 0.695 | 0.671 | 5050-5050 | 7040-7080 |
| real | cursor | all | 5050 | 19400 | 7070 | 6830 | 7270 | 7560 | 0.26 | 0.714 | 0.739 | 0.694 | 0.668 | 5050 | 19500 | 7100 | 7290 | 7560 | 0.258 | 0.711 | 0.693 | 0.668 | 5050-5050 | 7070-7120 |
| real | parse | 1 | 3820 | 8440 | 5580 | 5290 | 5590 | 5940 | 0.452 | 0.685 | 0.722 | 0.683 | 0.643 | 3820 | 8460 | 5580 | 5590 | 5940 | 0.451 | 0.685 | 0.683 | 0.643 | 3820-3820 | 5580-5680 |
| real | parse | all | 3820 | 8390 | 5640 | 5340 | 5660 | 6000 | 0.455 | 0.677 | 0.716 | 0.675 | 0.637 | 3820 | 8460 | 5670 | 5680 | 6010 | 0.452 | 0.673 | 0.672 | 0.635 | 3820-3820 | 5640-5690 |
| real | walk | 1 | 4950 | 16800 | 6500 | n/a | n/a | n/a | 0.295 | 0.761 | n/a | n/a | n/a | 4950 | 16800 | 6500 | n/a | n/a | 0.295 | 0.761 | n/a | n/a | 4950-4950 | 6500-6610 |
| real | walk | all | 4950 | 16800 | 6560 | n/a | n/a | n/a | 0.295 | 0.755 | n/a | n/a | n/a | 4950 | 16900 | 6570 | n/a | n/a | 0.294 | 0.753 | n/a | n/a | 4950-4950 | 6560-6620 |
| realerr | cursor | 1 | 76.1 | 238 | 105 | 96.3 | 98.7 | 111 | 0.319 | 0.723 | 0.789 | 0.77 | 0.688 | 76.1 | 239 | 105 | 98.7 | 111 | 0.319 | 0.723 | 0.77 | 0.688 | 76.1-76.1 | 105-105 |
| realerr | cursor | all | 76.4 | 236 | 106 | 96.7 | 99 | 111 | 0.324 | 0.724 | 0.79 | 0.772 | 0.689 | 76.5 | 241 | 106 | 99.4 | 112 | 0.318 | 0.721 | 0.77 | 0.686 | 76.4-79.8 | 106-107 |
| realerr | parse | 1 | 64.5 | 142 | 92.2 | 82.9 | 84 | 96.7 | 0.453 | 0.7 | 0.778 | 0.767 | 0.667 | 64.5 | 142 | 92.2 | 84.1 | 96.8 | 0.453 | 0.699 | 0.767 | 0.666 | 64.5-64.5 | 92.2-92.3 |
| realerr | parse | all | 64.8 | 142 | 92.3 | 83.1 | 84.7 | 97.2 | 0.455 | 0.702 | 0.78 | 0.765 | 0.666 | 64.9 | 143 | 93.1 | 85.1 | 97.7 | 0.454 | 0.697 | 0.762 | 0.664 | 64.8-65.2 | 92.3-93.6 |
| realerr | walk | 1 | 85.3 | 240 | 114 | n/a | n/a | n/a | 0.355 | 0.75 | n/a | n/a | n/a | 85.3 | 240 | 114 | n/a | n/a | 0.355 | 0.749 | n/a | n/a | 85.3-85.3 | 114-114 |
| realerr | walk | all | 85.6 | 240 | 114 | n/a | n/a | n/a | 0.357 | 0.751 | n/a | n/a | n/a | 85.8 | 242 | 115 | n/a | n/a | 0.355 | 0.748 | n/a | n/a | 85.6-88.6 | 114-115 |

### json: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 869 | 4490 | 1560 | 1590 | 1950 | 1840 | 0.194 | 0.559 | 0.548 | 0.446 | 0.472 | 878 | 4560 | 1600 | 1960 | 1860 | 0.193 | 0.549 | 0.447 | 0.473 | 869-906 | 1560-1650 |
| broken | cursor | all | 1330 | 6640 | 2340 | 2200 | 2660 | 2580 | 0.2 | 0.569 | 0.606 | 0.5 | 0.516 | 1380 | 6820 | 2430 | 2680 | 2660 | 0.202 | 0.568 | 0.515 | 0.518 | 1330-1430 | 2340-2460 |
| broken | parse | 1 | 660 | 2350 | 1270 | 1300 | 1380 | 1530 | 0.281 | 0.52 | 0.507 | 0.477 | 0.432 | 674 | 2380 | 1280 | 1390 | 1540 | 0.283 | 0.525 | 0.486 | 0.437 | 660-720 | 1270-1320 |
| broken | parse | all | 1000 | 3380 | 1900 | 1860 | 1930 | 2220 | 0.297 | 0.529 | 0.538 | 0.519 | 0.452 | 1050 | 3420 | 2000 | 1980 | 2240 | 0.308 | 0.527 | 0.531 | 0.469 | 1000-1090 | 1900-2030 |
| broken | walk | 1 | 1120 | 5500 | 1790 | n/a | n/a | n/a | 0.203 | 0.625 | n/a | n/a | n/a | 1140 | 5600 | 1830 | n/a | n/a | 0.203 | 0.623 | n/a | n/a | 1120-1250 | 1790-2040 |
| broken | walk | all | 1630 | 7530 | 2600 | n/a | n/a | n/a | 0.216 | 0.625 | n/a | n/a | n/a | 1680 | 7620 | 2680 | n/a | n/a | 0.22 | 0.626 | n/a | n/a | 1630-1800 | 2600-2770 |
| real | cursor | 1 | 972 | 4980 | 1790 | 1830 | 2220 | 2030 | 0.195 | 0.544 | 0.531 | 0.437 | 0.479 | 1010 | 5060 | 1830 | 2230 | 2060 | 0.2 | 0.554 | 0.453 | 0.492 | 972-1060 | 1790-1910 |
| real | cursor | all | 1530 | 7900 | 2760 | 2770 | 3070 | 3090 | 0.193 | 0.552 | 0.551 | 0.498 | 0.494 | 1590 | 8000 | 2880 | 3100 | 3160 | 0.199 | 0.553 | 0.513 | 0.503 | 1530-1620 | 2760-2900 |
| real | parse | 1 | 732 | 2430 | 1460 | 1480 | 1560 | 1680 | 0.301 | 0.501 | 0.495 | 0.47 | 0.435 | 758 | 2480 | 1500 | 1580 | 1690 | 0.306 | 0.506 | 0.479 | 0.449 | 732-781 | 1460-1710 |
| real | parse | all | 1180 | 3680 | 2290 | 2190 | 2310 | 2520 | 0.32 | 0.513 | 0.537 | 0.508 | 0.467 | 1200 | 3780 | 2340 | 2320 | 2590 | 0.317 | 0.512 | 0.516 | 0.462 | 1180-1220 | 2290-2400 |
| real | walk | 1 | 956 | 4650 | 1670 | n/a | n/a | n/a | 0.206 | 0.573 | n/a | n/a | n/a | 963 | 4770 | 1710 | n/a | n/a | 0.202 | 0.563 | n/a | n/a | 956-1010 | 1670-1760 |
| real | walk | all | 1470 | 7090 | 2590 | n/a | n/a | n/a | 0.207 | 0.565 | n/a | n/a | n/a | 1540 | 7190 | 2720 | n/a | n/a | 0.214 | 0.566 | n/a | n/a | 1470-1570 | 2590-2780 |
| realerr | cursor | 1 | 15.6 | 61.4 | 27.2 | 25.4 | 29.3 | 29.7 | 0.254 | 0.572 | 0.613 | 0.53 | 0.523 | 15.8 | 64.4 | 28.7 | 29.6 | 30.3 | 0.245 | 0.55 | 0.533 | 0.521 | 15.6-16.4 | 27.2-31 |
| realerr | cursor | all | 22.3 | 85.8 | 37.9 | 34.5 | 38.2 | 40.9 | 0.26 | 0.59 | 0.647 | 0.585 | 0.547 | 24.3 | 89 | 40 | 38.8 | 42.5 | 0.273 | 0.607 | 0.627 | 0.571 | 22.3-26.8 | 37.9-44.8 |
| realerr | parse | 1 | 13.3 | 39.5 | 24.1 | 23.2 | 23.4 | 27.2 | 0.336 | 0.55 | 0.572 | 0.567 | 0.488 | 13.7 | 40.2 | 25.1 | 23.5 | 28.2 | 0.341 | 0.547 | 0.584 | 0.486 | 13.3-15.4 | 24.1-30.1 |
| realerr | parse | all | 19.6 | 55.5 | 33.1 | 29.9 | 31.8 | 37.9 | 0.354 | 0.593 | 0.656 | 0.617 | 0.519 | 21.3 | 58.5 | 36.4 | 33.6 | 38.4 | 0.364 | 0.586 | 0.634 | 0.556 | 19.6-22.7 | 33.1-39.2 |
| realerr | walk | 1 | 17.1 | 69.1 | 28.4 | n/a | n/a | n/a | 0.247 | 0.602 | n/a | n/a | n/a | 17.3 | 70.8 | 31.1 | n/a | n/a | 0.244 | 0.556 | n/a | n/a | 17.1-20.1 | 28.4-39.9 |
| realerr | walk | all | 24.1 | 93.3 | 40.4 | n/a | n/a | n/a | 0.258 | 0.597 | n/a | n/a | n/a | 25.5 | 97.7 | 43.7 | n/a | n/a | 0.261 | 0.584 | n/a | n/a | 24.1-28.2 | 40.4-45.8 |

### json: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1060 | 9340 | 1210 | 1250 | 1360 | 1290 | 0.114 | 0.879 | 0.853 | 0.782 | 0.825 |
| broken | cursor minus parse | 1 | cycles M | 210 | 2140 | 287 | 287 | 568 | 314 | 0.0982 | 0.731 | 0.732 | 0.37 | 0.669 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 40.4 | 415 | 55.7 | 55.8 | 110 | 60.1 | 0.0972 | 0.725 | 0.723 | 0.367 | 0.671 |
| broken | cursor minus parse | all | instructions M | 1060 | 9430 | 1220 | 1250 | 1370 | 1290 | 0.113 | 0.871 | 0.849 | 0.775 | 0.825 |
| broken | cursor minus parse | all | cycles M | 328 | 3270 | 444 | 334 | 730 | 360 | 0.1 | 0.739 | 0.982 | 0.449 | 0.911 |
| broken | cursor minus parse | all | CPU user+sys ms | 70.4 | 743 | 101 | 73.9 | 159 | 82.2 | 0.0947 | 0.696 | 0.952 | 0.443 | 0.855 |
| broken | walk minus parse | 1 | instructions M | 2340 | 10200 | 2370 | n/a | n/a | n/a | 0.229 | 0.984 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 458 | 3150 | 521 | n/a | n/a | n/a | 0.145 | 0.88 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 87.9 | 614 | 100 | n/a | n/a | n/a | 0.143 | 0.875 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 2350 | 10300 | 2370 | n/a | n/a | n/a | 0.228 | 0.992 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 624 | 4150 | 707 | n/a | n/a | n/a | 0.15 | 0.883 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 130 | 902 | 145 | n/a | n/a | n/a | 0.144 | 0.897 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1230 | 11100 | 1460 | 1510 | 1640 | 1560 | 0.111 | 0.839 | 0.815 | 0.749 | 0.789 |
| real | cursor minus parse | 1 | cycles M | 240 | 2550 | 324 | 351 | 663 | 345 | 0.0941 | 0.739 | 0.683 | 0.362 | 0.696 |
| real | cursor minus parse | 1 | CPU user+sys ms | 46.2 | 493 | 62.2 | 67.6 | 127 | 66.1 | 0.0937 | 0.742 | 0.683 | 0.363 | 0.699 |
| real | cursor minus parse | all | instructions M | 1230 | 11100 | 1440 | 1500 | 1620 | 1560 | 0.111 | 0.855 | 0.821 | 0.76 | 0.789 |
| real | cursor minus parse | all | cycles M | 349 | 4220 | 471 | 577 | 751 | 571 | 0.0826 | 0.741 | 0.605 | 0.465 | 0.611 |
| real | cursor minus parse | all | CPU user+sys ms | 78.2 | 969 | 110 | 129 | 158 | 127 | 0.0808 | 0.714 | 0.605 | 0.496 | 0.617 |
| real | walk minus parse | 1 | instructions M | 1130 | 8350 | 926 | n/a | n/a | n/a | 0.136 | 1.22 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 224 | 2220 | 206 | n/a | n/a | n/a | 0.101 | 1.09 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 43.1 | 427 | 39.5 | n/a | n/a | n/a | 0.101 | 1.09 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1130 | 8400 | 919 | n/a | n/a | n/a | 0.135 | 1.23 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 289 | 3420 | 299 | n/a | n/a | n/a | 0.0846 | 0.967 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 63.5 | 778 | 67.2 | n/a | n/a | n/a | 0.0816 | 0.945 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 11.6 | 96 | 13.1 | 13.5 | 14.7 | 13.9 | 0.121 | 0.885 | 0.858 | 0.788 | 0.833 |
| realerr | cursor minus parse | 1 | cycles M | 2.28 | 21.9 | 3.08 | 2.16 | 5.9 | 2.54 | 0.104 | 0.741 | 1.06 | 0.387 | 0.9 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 0.432 | 4.14 | 0.593 | 0.423 | 1.12 | 0.493 | 0.104 | 0.728 | 1.02 | 0.384 | 0.876 |
| realerr | cursor minus parse | all | instructions M | 11.6 | 93.3 | 13.3 | 13.6 | 14.4 | 13.7 | 0.125 | 0.876 | 0.854 | 0.81 | 0.848 |
| realerr | cursor minus parse | all | cycles M | 2.71 | 30.3 | 4.75 | 4.57 | 6.36 | 3 | 0.0895 | 0.57 | 0.592 | 0.426 | 0.903 |
| realerr | cursor minus parse | all | CPU user+sys ms | 0.56 | 6.08 | 0.935 | 0.813 | 1.1 | 0.666 | 0.0922 | 0.599 | 0.689 | 0.507 | 0.841 |
| realerr | walk minus parse | 1 | instructions M | 20.8 | 97.7 | 21.6 | n/a | n/a | n/a | 0.213 | 0.963 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 3.81 | 29.7 | 4.29 | n/a | n/a | n/a | 0.129 | 0.889 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 0.725 | 5.68 | 0.821 | n/a | n/a | n/a | 0.128 | 0.883 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 20.8 | 97.6 | 21.8 | n/a | n/a | n/a | 0.213 | 0.958 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 4.48 | 37.8 | 7.25 | n/a | n/a | n/a | 0.118 | 0.618 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 0.881 | 7.42 | 1.44 | n/a | n/a | n/a | 0.119 | 0.612 | n/a | n/a | n/a |

### json: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 97 | 148 | 40.8 | 0.657 | 2.38 | 97 | 148 | 40.8 | 0.657 | 2.38 | 84.2 | 134 | 30.7 | 0.63 | 2.75 |
| broken | cursor | all | 193 | 301 | 238 | 0.642 | 0.814 | 193 | 301 | 238 | 0.642 | 0.814 | 180 | 287 | 227 | 0.629 | 0.794 |
| broken | parse | 1 | 44.9 | 47.1 | 33.5 | 0.952 | 1.34 | 44.9 | 47.1 | 33.5 | 0.952 | 1.34 | 32 | 32.9 | 23.5 | 0.971 | 1.36 |
| broken | parse | all | 116 | 248 | 195 | 0.466 | 0.593 | 116 | 248 | 195 | 0.466 | 0.593 | 103 | 234 | 185 | 0.439 | 0.556 |
| broken | walk | 1 | 73.4 | 148 | 45.3 | 0.495 | 1.62 | 73.4 | 148 | 45.3 | 0.495 | 1.62 | 60.5 | 134 | 35.2 | 0.451 | 1.72 |
| broken | walk | all | 148 | 256 | 219 | 0.578 | 0.677 | 148 | 256 | 219 | 0.578 | 0.677 | 135 | 242 | 209 | 0.559 | 0.648 |
| real | cursor | 1 | 31.3 | 71.1 | 20.6 | 0.439 | 1.52 | 31.3 | 71.1 | 20.6 | 0.439 | 1.52 | 14.6 | 53 | 6.66 | 0.275 | 2.19 |
| real | cursor | all | 80.5 | 151 | 91.3 | 0.532 | 0.882 | 80.5 | 151 | 91.3 | 0.532 | 0.882 | 63.6 | 133 | 77.3 | 0.478 | 0.823 |
| real | parse | 1 | 23.4 | 42.2 | 19.7 | 0.554 | 1.18 | 23.4 | 42.2 | 19.7 | 0.555 | 1.18 | 6.73 | 24.1 | 5.74 | 0.28 | 1.17 |
| real | parse | all | 74.7 | 123 | 88.9 | 0.607 | 0.84 | 74.7 | 123 | 88.9 | 0.607 | 0.84 | 57.4 | 105 | 74.9 | 0.547 | 0.766 |
| real | walk | 1 | 23.4 | 77.5 | 21 | 0.302 | 1.11 | 23.4 | 77.5 | 21 | 0.302 | 1.11 | 6.74 | 59.4 | 7.19 | 0.114 | 0.937 |
| real | walk | all | 76.2 | 147 | 92.1 | 0.519 | 0.828 | 76.3 | 147 | 92.1 | 0.519 | 0.828 | 59.3 | 129 | 77.9 | 0.461 | 0.761 |
| realerr | cursor | 1 | 5.02 | 11.5 | 5.05 | 0.438 | 0.993 | 5.03 | 11.5 | 5.05 | 0.438 | 0.995 | 1.56 | 6.56 | 2.46 | 0.237 | 0.634 |
| realerr | cursor | all | 13.3 | 17.4 | 9.58 | 0.766 | 1.39 | 13.3 | 17.4 | 9.58 | 0.766 | 1.39 | 8.93 | 12.3 | 6.84 | 0.727 | 1.3 |
| realerr | parse | 1 | 4.82 | 7.7 | 3.7 | 0.626 | 1.3 | 4.83 | 7.7 | 3.7 | 0.627 | 1.31 | 1.36 | 2.62 | 0.906 | 0.521 | 1.5 |
| realerr | parse | all | 12.1 | 15.3 | 7.66 | 0.793 | 1.58 | 12.1 | 15.3 | 7.66 | 0.792 | 1.58 | 7.44 | 10 | 5.09 | 0.741 | 1.46 |
| realerr | walk | 1 | 4.97 | 11.8 | 4.68 | 0.421 | 1.06 | 4.98 | 11.8 | 4.68 | 0.421 | 1.06 | 1.38 | 6.69 | 2.09 | 0.206 | 0.658 |
| realerr | walk | all | 12.1 | 17.3 | 8.96 | 0.697 | 1.35 | 12.1 | 17.3 | 8.96 | 0.697 | 1.35 | 7.91 | 12.3 | 6.22 | 0.644 | 1.27 |

### json: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 44.7 | 46.9 | 33.6 | 0.952 | 1.33 | 44.7 | 46.9 | 33.6 | 0.952 | 1.33 | 31.8 | 32.9 | 23.5 | 0.967 | 1.35 |
| broken | all | 134 | 421 | 308 | 0.318 | 0.436 | 134 | 421 | 308 | 0.318 | 0.436 | 121 | 407 | 297 | 0.297 | 0.407 |
| real | 1 | 23.3 | 48.9 | 19.7 | 0.476 | 1.18 | 23.3 | 48.9 | 19.7 | 0.476 | 1.18 | 6.25 | 30.6 | 5.81 | 0.204 | 1.08 |
| real | all | 76 | 174 | 97.7 | 0.436 | 0.778 | 76 | 174 | 97.7 | 0.436 | 0.778 | 58.9 | 156 | 83.6 | 0.377 | 0.704 |
| realerr | 1 | 4.98 | 10.9 | 3.44 | 0.459 | 1.45 | 4.99 | 10.9 | 3.44 | 0.46 | 1.45 | 1.39 | 5.8 | 0.887 | 0.241 | 1.57 |
| realerr | all | 14.5 | 23 | 10 | 0.629 | 1.44 | 14.5 | 23 | 10 | 0.63 | 1.45 | 9.86 | 17.8 | 7.36 | 0.552 | 1.34 |

### json: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0261 (real), 0.289 (realerr), 0.102 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.171 | 9630 | 7 | 1880 | 186000 | 1880 | 186000 | 0 |
| real | steady | 0.0261 | 4.73 | 6.99 | 1880 | 186000 | 1880 | 186000 | 0 |
| realerr | cold | 2.92 | 13600 | 8 | 681 | 86000 | 681 | 86000 | 0.00429 |
| realerr | steady | 0.289 | 59.8 | 7.92 | 680 | 85500 | 680 | 85500 | 0.000426 |
| broken | cold | 0.861 | 82900 | 7.36 | 2750 | 317000 | 2750 | 317000 | 0.000314 |
| broken | steady | 0.102 | 6280 | 7.36 | 2750 | 317000 | 2750 | 317000 | 0 |

### json: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2035 B | init ms | 0.0106 | 0.0124 | 0.00576 | 0.854 | 1.84 | 15 |
| #0 2035 B | parse1 ms | 0.114 | 0.159 | 0.11 | 0.719 | 1.04 | 15 |
| #0 2035 B | init+parse1 ms | 0.127 | 0.172 | 0.116 | 0.738 | 1.09 | 15 |
| #0 2035 B | parse2 ms | 0.0627 | 0.17 | 0.0916 | 0.368 | 0.684 | 15 |
| #0 2035 B | process user+sys ms | 0.889 | 1.28 | 0.502 | 0.696 | 1.77 | 15 |
| #0 2035 B | exec to exit instr user+kernel M | 6.67 | 10 | 5 | 0.664 | 1.33 | 15 |
| #0 2035 B | exec to exit instr user M | 4 | 6.45 | 4.01 | 0.62 | 0.998 | 15 |
| #0 2035 B | exec to exit instr kernel M | 2.65 | 3.57 | 0.998 | 0.74 | 2.65 | 15 |
| #0 2035 B | exec to exit cycles user+kernel M | 3.95 | 6.07 | 2.08 | 0.651 | 1.9 | 15 |
| #0 2035 B | exec to exit task-clock ms | 0.83 | 1.23 | 0.4 | 0.675 | 2.07 | 15 |
| #0 2035 B | peak RSS MB | 3.48 | 4.89 | 2.68 | 0.71 | 1.3 | 15 |
| #0 2035 B | peak footprint MB | 3.5 | 4.93 | 2.55 | 0.711 | 1.37 | 15 |
| #1 1464 B | init ms | 0.00921 | 0.0123 | 0.00554 | 0.751 | 1.66 | 15 |
| #1 1464 B | parse1 ms | 0.0749 | 0.0947 | 0.0662 | 0.791 | 1.13 | 15 |
| #1 1464 B | init+parse1 ms | 0.0831 | 0.108 | 0.072 | 0.771 | 1.15 | 15 |
| #1 1464 B | parse2 ms | 0.0278 | 0.0893 | 0.0488 | 0.312 | 0.571 | 15 |
| #1 1464 B | process user+sys ms | 0.783 | 1.15 | 0.397 | 0.681 | 1.97 | 15 |
| #1 1464 B | exec to exit instr user+kernel M | 4.93 | 7.4 | 3.09 | 0.666 | 1.6 | 15 |
| #1 1464 B | exec to exit instr user M | 2.2 | 3.83 | 2.11 | 0.575 | 1.04 | 15 |
| #1 1464 B | exec to exit instr kernel M | 2.66 | 3.57 | 0.976 | 0.746 | 2.73 | 15 |
| #1 1464 B | exec to exit cycles user+kernel M | 3.4 | 5.26 | 1.6 | 0.645 | 2.12 | 15 |
| #1 1464 B | exec to exit task-clock ms | 0.71 | 1.08 | 0.31 | 0.657 | 2.29 | 15 |
| #1 1464 B | peak RSS MB | 3.54 | 4.86 | 2.61 | 0.727 | 1.35 | 15 |
| #1 1464 B | peak footprint MB | 3.57 | 4.89 | 2.49 | 0.73 | 1.43 | 15 |

### json: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 651 | 2264663 | 0 | 0 | ok |
| realerr | 38 | 19609 | 38 | 0 | ok |
| broken | 498 | 1914794 | 476 | 0 | ok |

## julia

### julia: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 352 | 1460 | 736 | 716 | 747 | 773 | 788 | 0.241 | 0.479 | 0.492 | 0.471 | 0.456 | 0.447 | 355 | 1490 | 751 | 724 | 797 | 806 | 0.239 | 0.473 | 0.491 | 0.446 | 0.441 | 352-368 | 736-773 | 1 | 1.01 | 1 | 13.5 |
| broken | cursor | all | 58.7 | 238 | 119 | 117 | 118 | 123 | 127 | 0.247 | 0.494 | 0.501 | 0.499 | 0.478 | 0.46 | 61.7 | 242 | 123 | 120 | 124 | 130 | 0.255 | 0.501 | 0.513 | 0.496 | 0.475 | 58.7-66 | 119-127 | 10.5 * | 10.9 * | 10.8 * | 81.3 |
| broken | parse | 1 | 293 | 1100 | 671 | 654 | 671 | 652 | 725 | 0.266 | 0.437 | 0.448 | 0.437 | 0.45 | 0.405 | 302 | 1120 | 691 | 661 | 665 | 729 | 0.268 | 0.437 | 0.457 | 0.454 | 0.414 | 293-306 | 671-708 | 1 | 1.01 | 1 | 16.3 |
| broken | parse | all | 51.4 | 179 | 112 | 108 | 108 | 106 | 117 | 0.288 | 0.459 | 0.474 | 0.477 | 0.484 | 0.438 | 54 | 182 | 115 | 111 | 108 | 119 | 0.296 | 0.471 | 0.486 | 0.499 | 0.452 | 51.4-57.4 | 112-122 | 10.2 * | 10.5 * | 10.5 * | 92.8 |
| broken | walk | 1 | 337 | 1440 | 719 | 699 | n/a | n/a | n/a | 0.235 | 0.469 | 0.483 | n/a | n/a | n/a | 343 | 1460 | 726 | 711 | n/a | n/a | 0.236 | 0.473 | 0.483 | n/a | n/a | 337-358 | 719-758 | 1 | 1.01 | 1 | 14.1 |
| broken | walk | all | 56.1 | 232 | 117 | 114 | n/a | n/a | n/a | 0.242 | 0.479 | 0.492 | n/a | n/a | n/a | 60.4 | 237 | 121 | 117 | n/a | n/a | 0.255 | 0.498 | 0.517 | n/a | n/a | 56.1-63.7 | 117-128 | 10.6 * | 10.7 * | 10.6 * | 85 |
| real | cursor | 1 | 233 | 1070 | 467 | 453 | 486 | 530 | 519 | 0.217 | 0.499 | 0.513 | 0.478 | 0.439 | 0.448 | 237 | 1080 | 476 | 464 | 532 | 531 | 0.22 | 0.497 | 0.511 | 0.445 | 0.446 | 233-245 | 467-490 | 1 | 1.01 | 1 | 19 |
| real | cursor | all | 32.5 | 153 | 61.5 | 59.9 | 61.8 | 64.7 | 66.3 | 0.212 | 0.528 | 0.542 | 0.526 | 0.502 | 0.489 | 33.3 | 156 | 63.6 | 61.6 | 66.8 | 68.7 | 0.214 | 0.523 | 0.541 | 0.499 | 0.485 | 32.5-38.7 | 61.5-65.3 | 13.4 | 12.6 | 13.3 | 136 |
| real | parse | 1 | 186 | 730 | 408 | 396 | 436 | 420 | 456 | 0.254 | 0.454 | 0.468 | 0.425 | 0.442 | 0.407 | 189 | 749 | 417 | 409 | 425 | 464 | 0.253 | 0.453 | 0.462 | 0.445 | 0.408 | 186-196 | 408-445 | 1 | 1 | 1 | 23.8 |
| real | parse | all | 25.7 | 95.9 | 54 | 51.4 | 55 | 54.1 | 58.5 | 0.268 | 0.475 | 0.499 | 0.467 | 0.475 | 0.439 | 26.9 | 97.7 | 55.5 | 53.6 | 57.2 | 61 | 0.275 | 0.484 | 0.501 | 0.47 | 0.44 | 25.7-28.9 | 54-60.5 | 13 | 13.2 | 13.2 | 172 |
| real | walk | 1 | 219 | 1010 | 441 | 429 | n/a | n/a | n/a | 0.217 | 0.496 | 0.509 | n/a | n/a | n/a | 220 | 1030 | 446 | 434 | n/a | n/a | 0.213 | 0.493 | 0.506 | n/a | n/a | 219-222 | 441-460 | 1 | 1.01 | 1 | 20.2 |
| real | walk | all | 30.6 | 142 | 58.1 | 55.9 | n/a | n/a | n/a | 0.216 | 0.527 | 0.547 | n/a | n/a | n/a | 31.5 | 146 | 60.1 | 58.2 | n/a | n/a | 0.216 | 0.524 | 0.541 | n/a | n/a | 30.6-34.8 | 58.1-64.6 | 12.9 | 12.5 | 13.2 | 144 |
| realerr | cursor | 1 | 60.3 | 248 | 122 | 120 | 127 | 134 | 134 | 0.243 | 0.493 | 0.504 | 0.473 | 0.448 | 0.45 | 60.5 | 256 | 125 | 122 | 136 | 137 | 0.236 | 0.485 | 0.497 | 0.445 | 0.44 | 60.3-61.5 | 122-129 | 1 | 1.01 | 1 | 13.4 |
| realerr | cursor | all | 19.1 | 81.7 | 38.7 | 39.3 | 38.6 | 41.4 | 41.7 | 0.234 | 0.495 | 0.487 | 0.496 | 0.463 | 0.459 | 20.7 | 89 | 41.9 | 40.9 | 46.9 | 46 | 0.232 | 0.493 | 0.505 | 0.44 | 0.449 | 19.1-22.5 | 38.7-44 | 4.07 * | 4.12 * | 4.09 * | 42.1 |
| realerr | parse | 1 | 50.5 | 184 | 111 | 108 | 112 | 111 | 120 | 0.275 | 0.454 | 0.468 | 0.45 | 0.453 | 0.421 | 51.4 | 188 | 115 | 109 | 115 | 122 | 0.273 | 0.445 | 0.47 | 0.446 | 0.42 | 50.5-54.2 | 111-124 | 1 | 1.01 | 1 | 16 |
| realerr | parse | all | 15.3 | 55.6 | 33.5 | 31.3 | 35.8 | 32.6 | 38.2 | 0.276 | 0.458 | 0.49 | 0.428 | 0.471 | 0.402 | 16.3 | 59.4 | 37.4 | 36.8 | 37.5 | 40.4 | 0.274 | 0.435 | 0.443 | 0.435 | 0.403 | 15.3-20.7 | 33.5-40.6 | 4.29 * | 4.41 * | 4.15 * | 52.5 |
| realerr | walk | 1 | 57.9 | 239 | 118 | 115 | n/a | n/a | n/a | 0.242 | 0.492 | 0.505 | n/a | n/a | n/a | 58.4 | 247 | 119 | 115 | n/a | n/a | 0.236 | 0.491 | 0.507 | n/a | n/a | 57.9-61.7 | 118-120 | 1 | 1.01 | 1 | 13.9 |
| realerr | walk | all | 18.3 | 77.5 | 36.5 | 37 | n/a | n/a | n/a | 0.236 | 0.5 | 0.494 | n/a | n/a | n/a | 19.5 | 83.4 | 40.6 | 38.8 | n/a | n/a | 0.234 | 0.481 | 0.503 | n/a | n/a | 18.3-22.6 | 36.5-43.4 | 4.1 * | 4.09 * | 4.1 * | 44.1 |

### julia: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 353 | 1470 | 736 | 716 | 747 | 773 | 788 | 0.239 | 0.479 | 0.492 | 0.472 | 0.456 | 0.447 | 356 | 1500 | 751 | 724 | 797 | 806 | 0.237 | 0.473 | 0.491 | 0.446 | 0.441 | 353-368 | 736-772 |
| broken | cursor | all | 636 | 2610 | 1300 | 1290 | 1300 | 1310 | 1360 | 0.244 | 0.491 | 0.495 | 0.49 | 0.485 | 0.466 | 659 | 2640 | 1330 | 1290 | 1330 | 1390 | 0.25 | 0.497 | 0.509 | 0.496 | 0.473 | 636-671 | 1300-1340 |
| broken | parse | 1 | 293 | 1110 | 671 | 654 | 671 | 652 | 725 | 0.264 | 0.437 | 0.449 | 0.437 | 0.45 | 0.405 | 302 | 1130 | 691 | 661 | 665 | 729 | 0.267 | 0.437 | 0.457 | 0.454 | 0.414 | 293-306 | 671-708 |
| broken | parse | all | 530 | 1890 | 1160 | 1120 | 1160 | 1150 | 1240 | 0.28 | 0.457 | 0.474 | 0.458 | 0.461 | 0.427 | 553 | 1920 | 1210 | 1180 | 1160 | 1290 | 0.288 | 0.457 | 0.47 | 0.476 | 0.43 | 530-563 | 1160-1220 |
| broken | walk | 1 | 337 | 1450 | 719 | 699 | n/a | n/a | n/a | 0.233 | 0.47 | 0.483 | n/a | n/a | n/a | 344 | 1470 | 726 | 711 | n/a | n/a | 0.234 | 0.473 | 0.483 | n/a | n/a | 337-358 | 719-757 |
| broken | walk | all | 619 | 2510 | 1260 | 1250 | n/a | n/a | n/a | 0.247 | 0.49 | 0.497 | n/a | n/a | n/a | 637 | 2530 | 1290 | 1260 | n/a | n/a | 0.252 | 0.493 | 0.507 | n/a | n/a | 619-651 | 1260-1310 |
| real | cursor | 1 | 233 | 1080 | 467 | 453 | 486 | 530 | 519 | 0.216 | 0.499 | 0.514 | 0.479 | 0.439 | 0.449 | 237 | 1090 | 476 | 464 | 532 | 531 | 0.218 | 0.498 | 0.511 | 0.445 | 0.447 | 233-245 | 467-490 |
| real | cursor | all | 441 | 1910 | 815 | 807 | 812 | 884 | 910 | 0.231 | 0.541 | 0.547 | 0.544 | 0.499 | 0.485 | 449 | 1960 | 849 | 826 | 892 | 919 | 0.229 | 0.529 | 0.544 | 0.504 | 0.489 | 441-458 | 815-857 |
| real | parse | 1 | 186 | 733 | 408 | 396 | 436 | 420 | 456 | 0.253 | 0.455 | 0.469 | 0.426 | 0.442 | 0.407 | 189 | 754 | 417 | 409 | 425 | 463 | 0.251 | 0.454 | 0.463 | 0.445 | 0.409 | 186-196 | 408-445 |
| real | parse | all | 339 | 1270 | 719 | 669 | 725 | 691 | 789 | 0.267 | 0.472 | 0.507 | 0.467 | 0.49 | 0.43 | 352 | 1290 | 742 | 711 | 732 | 795 | 0.272 | 0.474 | 0.495 | 0.481 | 0.443 | 339-357 | 719-759 |
| real | walk | 1 | 219 | 1020 | 441 | 429 | n/a | n/a | n/a | 0.215 | 0.496 | 0.509 | n/a | n/a | n/a | 220 | 1040 | 446 | 434 | n/a | n/a | 0.211 | 0.494 | 0.507 | n/a | n/a | 219-222 | 441-460 |
| real | walk | all | 399 | 1780 | 770 | 739 | n/a | n/a | n/a | 0.225 | 0.518 | 0.54 | n/a | n/a | n/a | 413 | 1830 | 797 | 774 | n/a | n/a | 0.226 | 0.518 | 0.533 | n/a | n/a | 399-426 | 770-816 |
| realerr | cursor | 1 | 60.4 | 251 | 122 | 120 | 127 | 134 | 134 | 0.24 | 0.494 | 0.505 | 0.474 | 0.449 | 0.451 | 60.5 | 258 | 125 | 122 | 136 | 137 | 0.234 | 0.485 | 0.497 | 0.446 | 0.44 | 60.4-61.6 | 122-129 |
| realerr | cursor | all | 80.5 | 349 | 162 | 159 | 166 | 169 | 178 | 0.231 | 0.498 | 0.506 | 0.486 | 0.477 | 0.453 | 84.6 | 360 | 170 | 168 | 174 | 182 | 0.235 | 0.497 | 0.504 | 0.485 | 0.466 | 80.5-86.8 | 162-178 |
| realerr | parse | 1 | 50.5 | 185 | 111 | 108 | 112 | 111 | 120 | 0.273 | 0.455 | 0.468 | 0.45 | 0.454 | 0.422 | 51.4 | 190 | 115 | 109 | 115 | 122 | 0.271 | 0.446 | 0.471 | 0.447 | 0.421 | 50.5-54.3 | 111-124 |
| realerr | parse | all | 68.3 | 254 | 149 | 145 | 146 | 144 | 167 | 0.269 | 0.458 | 0.472 | 0.468 | 0.474 | 0.41 | 72.3 | 265 | 155 | 153 | 152 | 169 | 0.273 | 0.466 | 0.473 | 0.474 | 0.429 | 68.3-79.1 | 149-164 |
| realerr | walk | 1 | 58 | 242 | 118 | 115 | n/a | n/a | n/a | 0.24 | 0.493 | 0.505 | n/a | n/a | n/a | 58.4 | 250 | 119 | 115 | n/a | n/a | 0.234 | 0.492 | 0.508 | n/a | n/a | 58-61.7 | 118-120 |
| realerr | walk | all | 77.7 | 336 | 156 | 155 | n/a | n/a | n/a | 0.231 | 0.498 | 0.503 | n/a | n/a | n/a | 80.6 | 347 | 166 | 157 | n/a | n/a | 0.232 | 0.486 | 0.512 | n/a | n/a | 77.7-87 | 156-176 |

### julia: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 8150 | 26400 | 13700 | 13400 | 13400 | 14000 | 14100 | 0.309 | 0.594 | 0.609 | 0.607 | 0.583 | 0.577 | 8150 | 26400 | 13700 | 13400 | 14000 | 14100 | 0.309 | 0.594 | 0.609 | 0.583 | 0.577 | 8150-8150 | 13700-13700 |
| broken | cursor | all | 8170 | 26500 | 13800 | 13400 | 13500 | 14000 | 14200 | 0.309 | 0.593 | 0.608 | 0.606 | 0.582 | 0.577 | 8180 | 26500 | 13800 | 13500 | 14100 | 14200 | 0.308 | 0.593 | 0.607 | 0.581 | 0.576 | 8170-8230 | 13800-13800 |
| broken | parse | 1 | 6880 | 18700 | 12300 | 12000 | 12000 | 12500 | 12700 | 0.368 | 0.558 | 0.574 | 0.573 | 0.552 | 0.543 | 6880 | 18700 | 12300 | 12000 | 12500 | 12700 | 0.368 | 0.558 | 0.573 | 0.552 | 0.543 | 6880-6880 | 12300-12400 |
| broken | parse | all | 6900 | 18400 | 12400 | 12000 | 12100 | 12500 | 12700 | 0.376 | 0.557 | 0.572 | 0.571 | 0.55 | 0.542 | 6910 | 18700 | 12400 | 12100 | 12500 | 12800 | 0.369 | 0.557 | 0.571 | 0.551 | 0.541 | 6900-6940 | 12400-12500 |
| broken | walk | 1 | 7880 | 24600 | 13200 | 12900 | n/a | n/a | n/a | 0.32 | 0.597 | 0.613 | n/a | n/a | n/a | 7880 | 24600 | 13200 | 12900 | n/a | n/a | 0.32 | 0.597 | 0.612 | n/a | n/a | 7880-7880 | 13200-13200 |
| broken | walk | all | 7910 | 24800 | 13300 | 12900 | n/a | n/a | n/a | 0.319 | 0.595 | 0.611 | n/a | n/a | n/a | 7910 | 24800 | 13300 | 13000 | n/a | n/a | 0.319 | 0.595 | 0.611 | n/a | n/a | 7910-7960 | 13300-13300 |
| real | cursor | 1 | 5430 | 19200 | 8310 | 8020 | 8230 | 8650 | 8660 | 0.283 | 0.653 | 0.677 | 0.659 | 0.627 | 0.627 | 5430 | 19200 | 8320 | 8020 | 8660 | 8670 | 0.283 | 0.653 | 0.676 | 0.627 | 0.626 | 5430-5430 | 8310-8330 |
| real | cursor | all | 5430 | 19100 | 8320 | 8050 | 8250 | 8700 | 8730 | 0.284 | 0.652 | 0.674 | 0.658 | 0.624 | 0.622 | 5430 | 19300 | 8380 | 8090 | 8700 | 8740 | 0.281 | 0.648 | 0.671 | 0.624 | 0.621 | 5430-5430 | 8320-8420 |
| real | parse | 1 | 4250 | 12100 | 7030 | 6740 | 6920 | 7240 | 7330 | 0.35 | 0.605 | 0.631 | 0.615 | 0.588 | 0.58 | 4250 | 12100 | 7040 | 6750 | 7250 | 7340 | 0.35 | 0.604 | 0.63 | 0.587 | 0.579 | 4250-4250 | 7030-7070 |
| real | parse | all | 4250 | 12000 | 7040 | 6760 | 6940 | 7270 | 7390 | 0.355 | 0.604 | 0.629 | 0.613 | 0.585 | 0.576 | 4250 | 12100 | 7090 | 6800 | 7300 | 7400 | 0.351 | 0.6 | 0.626 | 0.583 | 0.575 | 4250-4250 | 7040-7100 |
| real | walk | 1 | 5010 | 17200 | 7630 | 7340 | n/a | n/a | n/a | 0.292 | 0.657 | 0.683 | n/a | n/a | n/a | 5010 | 17200 | 7650 | 7350 | n/a | n/a | 0.291 | 0.656 | 0.682 | n/a | n/a | 5010-5010 | 7630-7680 |
| real | walk | all | 5010 | 17100 | 7640 | 7360 | n/a | n/a | n/a | 0.293 | 0.656 | 0.681 | n/a | n/a | n/a | 5010 | 17300 | 7690 | 7390 | n/a | n/a | 0.29 | 0.652 | 0.679 | n/a | n/a | 5010-5010 | 7640-7710 |
| realerr | cursor | 1 | 1430 | 4540 | 2300 | 2240 | 2260 | 2370 | 2380 | 0.316 | 0.623 | 0.639 | 0.635 | 0.605 | 0.603 | 1430 | 4550 | 2310 | 2250 | 2370 | 2380 | 0.315 | 0.622 | 0.638 | 0.604 | 0.602 | 1430-1430 | 2300-2310 |
| realerr | cursor | all | 1440 | 4540 | 2270 | 2220 | 2230 | 2330 | 2350 | 0.316 | 0.632 | 0.646 | 0.644 | 0.617 | 0.612 | 1440 | 4570 | 2290 | 2240 | 2350 | 2350 | 0.315 | 0.629 | 0.643 | 0.611 | 0.612 | 1440-1440 | 2270-2310 |
| realerr | parse | 1 | 1200 | 3150 | 2050 | 2000 | 2000 | 2090 | 2120 | 0.382 | 0.586 | 0.603 | 0.601 | 0.575 | 0.568 | 1200 | 3150 | 2060 | 2000 | 2100 | 2120 | 0.382 | 0.585 | 0.602 | 0.574 | 0.568 | 1200-1200 | 2050-2070 |
| realerr | parse | all | 1210 | 3130 | 2030 | 1970 | 1970 | 2070 | 2100 | 0.385 | 0.593 | 0.611 | 0.612 | 0.583 | 0.573 | 1210 | 3150 | 2050 | 1990 | 2080 | 2110 | 0.383 | 0.59 | 0.606 | 0.582 | 0.573 | 1210-1210 | 2030-2070 |
| realerr | walk | 1 | 1350 | 4150 | 2170 | 2120 | n/a | n/a | n/a | 0.327 | 0.623 | 0.64 | n/a | n/a | n/a | 1350 | 4160 | 2180 | 2120 | n/a | n/a | 0.326 | 0.622 | 0.639 | n/a | n/a | 1350-1350 | 2170-2190 |
| realerr | walk | all | 1360 | 4140 | 2150 | 2100 | n/a | n/a | n/a | 0.327 | 0.632 | 0.647 | n/a | n/a | n/a | 1360 | 4180 | 2170 | 2100 | n/a | n/a | 0.325 | 0.626 | 0.647 | n/a | n/a | 1360-1360 | 2150-2180 |

### julia: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1840 | 7600 | 3830 | 3730 | 3890 | 4020 | 4100 | 0.242 | 0.479 | 0.493 | 0.472 | 0.457 | 0.448 | 1850 | 7760 | 3910 | 3770 | 4140 | 4190 | 0.239 | 0.474 | 0.492 | 0.447 | 0.442 | 1840-1910 | 3830-4010 |
| broken | cursor | all | 2890 | 11600 | 5880 | 5860 | 5850 | 5990 | 6250 | 0.248 | 0.492 | 0.493 | 0.494 | 0.482 | 0.463 | 2970 | 11700 | 6000 | 5890 | 6040 | 6310 | 0.254 | 0.495 | 0.505 | 0.493 | 0.471 | 2890-3020 | 5880-6030 |
| broken | parse | 1 | 1530 | 5740 | 3490 | 3400 | 3490 | 3390 | 3770 | 0.267 | 0.438 | 0.45 | 0.438 | 0.451 | 0.406 | 1580 | 5860 | 3600 | 3440 | 3460 | 3790 | 0.269 | 0.439 | 0.459 | 0.456 | 0.416 | 1530-1590 | 3490-3680 |
| broken | parse | all | 2430 | 8420 | 5290 | 5140 | 5280 | 5200 | 5670 | 0.289 | 0.46 | 0.473 | 0.46 | 0.468 | 0.429 | 2500 | 8540 | 5480 | 5370 | 5230 | 5840 | 0.293 | 0.456 | 0.466 | 0.478 | 0.428 | 2430-2550 | 5290-5550 |
| broken | walk | 1 | 1760 | 7470 | 3740 | 3640 | n/a | n/a | n/a | 0.236 | 0.47 | 0.484 | n/a | n/a | n/a | 1790 | 7590 | 3780 | 3700 | n/a | n/a | 0.236 | 0.474 | 0.485 | n/a | n/a | 1760-1870 | 3740-3930 |
| broken | walk | all | 2810 | 11200 | 5740 | 5660 | n/a | n/a | n/a | 0.251 | 0.489 | 0.496 | n/a | n/a | n/a | 2890 | 11200 | 5860 | 5750 | n/a | n/a | 0.257 | 0.492 | 0.502 | n/a | n/a | 2810-2920 | 5740-5890 |
| real | cursor | 1 | 1210 | 5570 | 2440 | 2370 | 2540 | 2770 | 2710 | 0.218 | 0.498 | 0.512 | 0.478 | 0.438 | 0.448 | 1240 | 5610 | 2490 | 2420 | 2780 | 2770 | 0.22 | 0.497 | 0.511 | 0.445 | 0.446 | 1210-1280 | 2440-2560 |
| real | cursor | all | 1980 | 8440 | 3680 | 3630 | 3650 | 3990 | 4090 | 0.235 | 0.539 | 0.546 | 0.542 | 0.496 | 0.484 | 2010 | 8650 | 3790 | 3710 | 4020 | 4100 | 0.233 | 0.531 | 0.542 | 0.501 | 0.49 | 1980-2040 | 3680-3820 |
| real | parse | 1 | 968 | 3810 | 2130 | 2070 | 2280 | 2190 | 2380 | 0.254 | 0.454 | 0.468 | 0.425 | 0.442 | 0.407 | 985 | 3910 | 2180 | 2140 | 2220 | 2420 | 0.252 | 0.452 | 0.461 | 0.443 | 0.407 | 968-1020 | 2130-2320 |
| real | parse | all | 1540 | 5630 | 3260 | 3050 | 3290 | 3150 | 3560 | 0.273 | 0.472 | 0.504 | 0.468 | 0.487 | 0.432 | 1590 | 5710 | 3330 | 3210 | 3290 | 3570 | 0.278 | 0.477 | 0.495 | 0.483 | 0.445 | 1540-1610 | 3260-3360 |
| real | walk | 1 | 1140 | 5250 | 2300 | 2240 | n/a | n/a | n/a | 0.217 | 0.496 | 0.509 | n/a | n/a | n/a | 1150 | 5380 | 2330 | 2270 | n/a | n/a | 0.213 | 0.494 | 0.506 | n/a | n/a | 1140-1160 | 2300-2400 |
| real | walk | all | 1810 | 7890 | 3470 | 3380 | n/a | n/a | n/a | 0.23 | 0.522 | 0.537 | n/a | n/a | n/a | 1860 | 8020 | 3590 | 3490 | n/a | n/a | 0.232 | 0.519 | 0.533 | n/a | n/a | 1810-1920 | 3470-3640 |
| realerr | cursor | 1 | 315 | 1290 | 639 | 625 | 665 | 702 | 700 | 0.243 | 0.493 | 0.504 | 0.473 | 0.448 | 0.45 | 315 | 1330 | 649 | 635 | 709 | 717 | 0.237 | 0.486 | 0.497 | 0.445 | 0.44 | 315-321 | 639-674 |
| realerr | cursor | all | 410 | 1680 | 815 | 796 | 826 | 851 | 895 | 0.245 | 0.503 | 0.516 | 0.497 | 0.483 | 0.459 | 428 | 1710 | 840 | 832 | 883 | 900 | 0.251 | 0.509 | 0.514 | 0.484 | 0.475 | 410-437 | 815-876 |
| realerr | parse | 1 | 264 | 958 | 580 | 563 | 586 | 582 | 625 | 0.275 | 0.454 | 0.468 | 0.45 | 0.453 | 0.421 | 268 | 985 | 603 | 570 | 601 | 638 | 0.272 | 0.445 | 0.471 | 0.446 | 0.42 | 264-283 | 580-641 |
| realerr | parse | all | 350 | 1220 | 747 | 715 | 735 | 732 | 816 | 0.287 | 0.468 | 0.489 | 0.476 | 0.478 | 0.428 | 359 | 1280 | 774 | 756 | 755 | 833 | 0.281 | 0.464 | 0.474 | 0.475 | 0.43 | 350-392 | 747-802 |
| realerr | walk | 1 | 302 | 1240 | 614 | 599 | n/a | n/a | n/a | 0.243 | 0.492 | 0.504 | n/a | n/a | n/a | 305 | 1280 | 620 | 600 | n/a | n/a | 0.238 | 0.491 | 0.508 | n/a | n/a | 302-322 | 614-626 |
| realerr | walk | all | 392 | 1610 | 766 | 767 | n/a | n/a | n/a | 0.243 | 0.511 | 0.511 | n/a | n/a | n/a | 407 | 1640 | 820 | 785 | n/a | n/a | 0.248 | 0.496 | 0.519 | n/a | n/a | 392-446 | 766-861 |

### julia: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1270 | 7650 | 1400 | 1400 | 1430 | 1540 | 1450 | 0.167 | 0.913 | 0.913 | 0.891 | 0.827 | 0.875 |
| broken | cursor minus parse | 1 | cycles M | 305 | 1860 | 337 | 325 | 396 | 631 | 332 | 0.164 | 0.904 | 0.94 | 0.771 | 0.484 | 0.919 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 59.1 | 363 | 64.5 | 62.1 | 76 | 120 | 63.4 | 0.163 | 0.916 | 0.951 | 0.777 | 0.49 | 0.933 |
| broken | cursor minus parse | all | instructions M | 1280 | 8090 | 1380 | 1390 | 1410 | 1510 | 1440 | 0.158 | 0.922 | 0.917 | 0.904 | 0.843 | 0.883 |
| broken | cursor minus parse | all | cycles M | 458 | 3210 | 590 | 723 | 564 | 796 | 578 | 0.143 | 0.775 | 0.632 | 0.811 | 0.575 | 0.791 |
| broken | cursor minus parse | all | CPU user+sys ms | 106 | 722 | 135 | 166 | 139 | 163 | 122 | 0.147 | 0.783 | 0.638 | 0.762 | 0.649 | 0.869 |
| broken | walk minus parse | 1 | instructions M | 1000 | 5910 | 877 | 877 | n/a | n/a | n/a | 0.17 | 1.14 | 1.14 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 229 | 1730 | 248 | 233 | n/a | n/a | n/a | 0.132 | 0.922 | 0.981 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 43.9 | 339 | 47.2 | 44.8 | n/a | n/a | n/a | 0.13 | 0.93 | 0.981 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 1010 | 6390 | 893 | 887 | n/a | n/a | n/a | 0.158 | 1.13 | 1.14 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 374 | 2750 | 448 | 521 | n/a | n/a | n/a | 0.136 | 0.835 | 0.718 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 88.7 | 616 | 102 | 127 | n/a | n/a | n/a | 0.144 | 0.866 | 0.697 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1170 | 7060 | 1280 | 1280 | 1310 | 1410 | 1330 | 0.166 | 0.917 | 0.917 | 0.896 | 0.831 | 0.88 |
| real | cursor minus parse | 1 | cycles M | 245 | 1760 | 304 | 301 | 261 | 578 | 328 | 0.139 | 0.806 | 0.815 | 0.939 | 0.424 | 0.747 |
| real | cursor minus parse | 1 | CPU user+sys ms | 47.2 | 346 | 58.3 | 57.4 | 50.1 | 111 | 62.8 | 0.136 | 0.809 | 0.822 | 0.942 | 0.427 | 0.751 |
| real | cursor minus parse | all | instructions M | 1170 | 7140 | 1280 | 1290 | 1310 | 1430 | 1340 | 0.164 | 0.917 | 0.912 | 0.898 | 0.821 | 0.875 |
| real | cursor minus parse | all | cycles M | 443 | 2810 | 417 | 579 | 364 | 840 | 535 | 0.158 | 1.06 | 0.764 | 1.22 | 0.527 | 0.828 |
| real | cursor minus parse | all | CPU user+sys ms | 102 | 642 | 96.8 | 138 | 86.5 | 193 | 121 | 0.159 | 1.06 | 0.74 | 1.18 | 0.531 | 0.843 |
| real | walk minus parse | 1 | instructions M | 759 | 5050 | 605 | 605 | n/a | n/a | n/a | 0.15 | 1.26 | 1.26 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 172 | 1450 | 169 | 175 | n/a | n/a | n/a | 0.119 | 1.02 | 0.984 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 32.9 | 283 | 32.3 | 33.4 | n/a | n/a | n/a | 0.116 | 1.02 | 0.987 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 759 | 5130 | 602 | 598 | n/a | n/a | n/a | 0.148 | 1.26 | 1.27 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 277 | 2250 | 214 | 331 | n/a | n/a | n/a | 0.123 | 1.3 | 0.837 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 60.2 | 505 | 51.7 | 69.9 | n/a | n/a | n/a | 0.119 | 1.16 | 0.861 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 231 | 1390 | 249 | 249 | 255 | 275 | 260 | 0.166 | 0.928 | 0.928 | 0.906 | 0.84 | 0.89 |
| realerr | cursor minus parse | 1 | cycles M | 51.1 | 335 | 58.4 | 61.5 | 79.6 | 121 | 74.3 | 0.153 | 0.875 | 0.831 | 0.642 | 0.424 | 0.688 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 9.83 | 66 | 11.2 | 11.7 | 15.2 | 23 | 14.2 | 0.149 | 0.878 | 0.839 | 0.646 | 0.426 | 0.692 |
| realerr | cursor minus parse | all | instructions M | 231 | 1410 | 243 | 249 | 260 | 262 | 244 | 0.164 | 0.95 | 0.928 | 0.889 | 0.883 | 0.947 |
| realerr | cursor minus parse | all | cycles M | 60.7 | 459 | 68.6 | 80.5 | 90.9 | 119 | 78.7 | 0.132 | 0.885 | 0.754 | 0.668 | 0.51 | 0.771 |
| realerr | cursor minus parse | all | CPU user+sys ms | 12.2 | 94.5 | 12.3 | 14.3 | 19.7 | 24.8 | 11.1 | 0.129 | 0.989 | 0.853 | 0.618 | 0.493 | 1.1 |
| realerr | walk minus parse | 1 | instructions M | 151 | 997 | 120 | 120 | n/a | n/a | n/a | 0.152 | 1.26 | 1.26 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 38.6 | 284 | 34.2 | 36 | n/a | n/a | n/a | 0.136 | 1.13 | 1.07 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 7.44 | 56.5 | 6.53 | 6.87 | n/a | n/a | n/a | 0.132 | 1.14 | 1.08 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 152 | 1010 | 117 | 123 | n/a | n/a | n/a | 0.15 | 1.29 | 1.23 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 41.8 | 393 | 19.3 | 51.7 | n/a | n/a | n/a | 0.106 | 2.16 | 0.809 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 9.4 | 82.4 | 6.7 | 9.75 | n/a | n/a | n/a | 0.114 | 1.4 | 0.964 | n/a | n/a | n/a |

### julia: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 40.2 | 150 | 26.1 | 0.269 | 1.54 | 40.2 | 150 | 26.1 | 0.269 | 1.54 | 30.8 | 139 | 19.2 | 0.222 | 1.61 |
| broken | cursor | all | 127 | 360 | 221 | 0.353 | 0.576 | 127 | 360 | 221 | 0.353 | 0.576 | 117 | 349 | 213 | 0.335 | 0.549 |
| broken | parse | 1 | 31.6 | 39.3 | 26.2 | 0.803 | 1.2 | 31.6 | 39.3 | 26.2 | 0.803 | 1.2 | 22.2 | 28.6 | 19.3 | 0.776 | 1.15 |
| broken | parse | all | 119 | 339 | 208 | 0.351 | 0.571 | 119 | 339 | 208 | 0.351 | 0.571 | 109 | 328 | 201 | 0.332 | 0.542 |
| broken | walk | 1 | 31.6 | 165 | 26 | 0.191 | 1.21 | 31.6 | 165 | 26 | 0.191 | 1.21 | 22.2 | 155 | 19.2 | 0.144 | 1.16 |
| broken | walk | all | 118 | 397 | 238 | 0.298 | 0.497 | 118 | 397 | 238 | 0.298 | 0.497 | 108 | 386 | 230 | 0.28 | 0.47 |
| real | cursor | 1 | 36.6 | 111 | 24 | 0.33 | 1.52 | 36.6 | 111 | 24 | 0.33 | 1.53 | 27.6 | 101 | 17.2 | 0.274 | 1.6 |
| real | cursor | all | 101 | 274 | 173 | 0.367 | 0.581 | 101 | 274 | 173 | 0.367 | 0.581 | 90.9 | 264 | 166 | 0.345 | 0.547 |
| real | parse | 1 | 28.4 | 33.3 | 24 | 0.853 | 1.18 | 28.4 | 33.3 | 24 | 0.853 | 1.18 | 19.3 | 22.9 | 17.2 | 0.843 | 1.12 |
| real | parse | all | 87.5 | 209 | 179 | 0.418 | 0.489 | 87.5 | 209 | 179 | 0.418 | 0.489 | 77.9 | 199 | 172 | 0.391 | 0.453 |
| real | walk | 1 | 28.2 | 116 | 24.1 | 0.243 | 1.17 | 28.2 | 116 | 24.1 | 0.243 | 1.17 | 19.2 | 106 | 17.3 | 0.182 | 1.11 |
| real | walk | all | 93.1 | 268 | 187 | 0.347 | 0.498 | 93.1 | 268 | 187 | 0.347 | 0.498 | 83.3 | 258 | 180 | 0.324 | 0.463 |
| realerr | cursor | 1 | 27.2 | 101 | 22.4 | 0.269 | 1.22 | 27.2 | 101 | 22.4 | 0.269 | 1.22 | 23.2 | 95.9 | 19.3 | 0.242 | 1.21 |
| realerr | cursor | all | 68.7 | 164 | 138 | 0.419 | 0.496 | 68.7 | 164 | 138 | 0.419 | 0.496 | 63.6 | 158 | 135 | 0.401 | 0.471 |
| realerr | parse | 1 | 26.8 | 30.3 | 22.6 | 0.886 | 1.19 | 26.8 | 30.3 | 22.6 | 0.886 | 1.19 | 22.8 | 24.6 | 19.3 | 0.927 | 1.18 |
| realerr | parse | all | 72.4 | 132 | 138 | 0.547 | 0.526 | 72.4 | 132 | 138 | 0.547 | 0.526 | 67.1 | 127 | 134 | 0.53 | 0.5 |
| realerr | walk | 1 | 26.6 | 114 | 22.6 | 0.232 | 1.17 | 26.6 | 114 | 22.6 | 0.232 | 1.17 | 22.5 | 109 | 19.3 | 0.207 | 1.17 |
| realerr | walk | all | 69.9 | 178 | 140 | 0.393 | 0.499 | 69.9 | 178 | 140 | 0.394 | 0.499 | 64.7 | 172 | 137 | 0.376 | 0.473 |

### julia: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 31.3 | 54.6 | 26 | 0.574 | 1.2 | 31.3 | 54.6 | 26 | 0.574 | 1.2 | 22 | 44 | 19.1 | 0.5 | 1.15 |
| broken | all | 128 | 519 | 244 | 0.247 | 0.526 | 128 | 519 | 244 | 0.247 | 0.526 | 118 | 508 | 237 | 0.232 | 0.499 |
| real | 1 | 28.4 | 44.6 | 24 | 0.636 | 1.18 | 28.4 | 44.6 | 24 | 0.636 | 1.18 | 19.3 | 34.2 | 17.2 | 0.564 | 1.12 |
| real | all | 93.7 | 282 | 213 | 0.333 | 0.44 | 93.7 | 282 | 213 | 0.333 | 0.44 | 84.1 | 271 | 206 | 0.31 | 0.407 |
| realerr | 1 | 26.6 | 30.7 | 22.4 | 0.868 | 1.19 | 26.6 | 30.7 | 22.4 | 0.868 | 1.19 | 22.5 | 25.1 | 19.2 | 0.898 | 1.17 |
| realerr | all | 89.5 | 189 | 229 | 0.475 | 0.391 | 89.5 | 189 | 229 | 0.475 | 0.391 | 84.3 | 183 | 226 | 0.461 | 0.374 |

### julia: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0446 (real), 0.826 (realerr), 0.0446 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.589 | 43000 | 7.11 | 5350 | 488000 | 5350 | 488000 | 0.00011 |
| real | steady | 0.0446 | 25.9 | 7.1 | 5350 | 488000 | 5350 | 488000 | 0 |
| realerr | cold | 33.4 | 893000 | 27.6 | 21500 | 1970000 | 21500 | 1970000 | 0.00155 |
| realerr | steady | 0.826 | 592 | 23.7 | 21500 | 1970000 | 21500 | 1970000 | 0 |
| broken | cold | 7.27 | 44300 | 18.7 | 5650 | 532000 | 5650 | 532000 | 0.00129 |
| broken | steady | 0.0446 | 71.2 | 18.7 | 5650 | 532000 | 5650 | 532000 | 0 |

### julia: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2045 B | init ms | 0.00894 | 0.0121 | 0.00592 | 0.739 | 1.51 | 15 |
| #0 2045 B | parse1 ms | 0.278 | 0.483 | 0.362 | 0.577 | 0.768 | 15 |
| #0 2045 B | init+parse1 ms | 0.288 | 0.495 | 0.37 | 0.581 | 0.778 | 15 |
| #0 2045 B | parse2 ms | 0.128 | 0.43 | 0.229 | 0.297 | 0.56 | 15 |
| #0 2045 B | process user+sys ms | 1.11 | 1.93 | 0.877 | 0.574 | 1.26 | 15 |
| #0 2045 B | exec to exit instr user+kernel M | 9.31 | 17.1 | 9.01 | 0.545 | 1.03 | 15 |
| #0 2045 B | exec to exit instr user M | 6.37 | 13.1 | 7.68 | 0.488 | 0.83 | 15 |
| #0 2045 B | exec to exit instr kernel M | 2.95 | 3.96 | 1.33 | 0.746 | 2.21 | 15 |
| #0 2045 B | exec to exit cycles user+kernel M | 5.19 | 9.36 | 4.11 | 0.555 | 1.26 | 15 |
| #0 2045 B | exec to exit task-clock ms | 1.04 | 1.87 | 0.79 | 0.556 | 1.32 | 15 |
| #0 2045 B | peak RSS MB | 5.96 | 7.23 | 4.96 | 0.825 | 1.2 | 15 |
| #0 2045 B | peak footprint MB | 5.99 | 7.26 | 4.9 | 0.826 | 1.22 | 15 |
| #1 3673 B | init ms | 0.00872 | 0.0117 | 0.00508 | 0.746 | 1.72 | 15 |
| #1 3673 B | parse1 ms | 0.405 | 0.68 | 0.489 | 0.595 | 0.828 | 15 |
| #1 3673 B | init+parse1 ms | 0.415 | 0.693 | 0.494 | 0.598 | 0.84 | 15 |
| #1 3673 B | parse2 ms | 0.204 | 0.622 | 0.349 | 0.327 | 0.584 | 15 |
| #1 3673 B | process user+sys ms | 1.33 | 2.34 | 1.15 | 0.569 | 1.16 | 15 |
| #1 3673 B | exec to exit instr user+kernel M | 12.5 | 23 | 13 | 0.544 | 0.964 | 15 |
| #1 3673 B | exec to exit instr user M | 9.41 | 18.8 | 11.6 | 0.5 | 0.813 | 15 |
| #1 3673 B | exec to exit instr kernel M | 3.06 | 4.09 | 1.39 | 0.748 | 2.21 | 15 |
| #1 3673 B | exec to exit cycles user+kernel M | 6.31 | 11.3 | 5.51 | 0.559 | 1.15 | 15 |
| #1 3673 B | exec to exit task-clock ms | 1.26 | 2.27 | 1.06 | 0.555 | 1.19 | 15 |
| #1 3673 B | peak RSS MB | 6.29 | 7.47 | 5.36 | 0.842 | 1.17 | 15 |
| #1 3673 B | peak footprint MB | 6.32 | 7.5 | 5.28 | 0.842 | 1.2 | 15 |

### julia: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 448 | 1341967 | 0 | 0 | ok |
| realerr | 23 | 266319 | 23 | 0 | ok |
| broken | 493 | 1460337 | 410 | 0 | ok |

## kotlin

### kotlin: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 80.1 | 293 | 154 | 154 | 157 | 170 | 167 | 0.273 | 0.52 | 0.521 | 0.509 | 0.472 | 0.481 | 81.1 | 302 | 157 | 158 | 171 | 169 | 0.269 | 0.517 | 0.512 | 0.475 | 0.48 | 80.1-87.1 | 154-164 | 1 | 1.01 | 1 | 24.3 |
| broken | cursor | all | 10.7 | 44.8 | 19.6 | 19.6 | 19.7 | 20.5 | 21.5 | 0.24 | 0.547 | 0.549 | 0.545 | 0.524 | 0.501 | 11.1 | 46.6 | 20.7 | 20.3 | 20.9 | 21.7 | 0.238 | 0.536 | 0.546 | 0.53 | 0.511 | 10.7-13.7 | 19.6-22.7 | 13.8 | 11.9 | 13.9 | 181 |
| broken | parse | 1 | 71.2 | 218 | 141 | 140 | 142 | 145 | 153 | 0.326 | 0.504 | 0.507 | 0.501 | 0.49 | 0.465 | 72.4 | 225 | 146 | 145 | 154 | 155 | 0.322 | 0.497 | 0.498 | 0.471 | 0.467 | 71.2-74.5 | 141-157 | 1 | 1.01 | 1 | 27.3 |
| broken | parse | all | 9.4 | 31.6 | 18.4 | 17.7 | 17.7 | 17.5 | 19.4 | 0.298 | 0.511 | 0.53 | 0.53 | 0.537 | 0.485 | 9.72 | 33 | 19.3 | 18.3 | 18.6 | 20.2 | 0.294 | 0.504 | 0.533 | 0.522 | 0.482 | 9.4-11.3 | 18.4-20.2 | 14 | 12 | 13.8 | 207 |
| broken | walk | 1 | 78.9 | 282 | 148 | 147 | n/a | n/a | n/a | 0.28 | 0.532 | 0.535 | n/a | n/a | n/a | 80 | 286 | 149 | 152 | n/a | n/a | 0.28 | 0.538 | 0.527 | n/a | n/a | 78.9-82.4 | 148-159 | 1 | 1.01 | 1 | 24.7 |
| broken | walk | all | 10.1 | 42.2 | 19.2 | 18.8 | n/a | n/a | n/a | 0.24 | 0.529 | 0.539 | n/a | n/a | n/a | 10.9 | 43.9 | 19.8 | 19.5 | n/a | n/a | 0.248 | 0.549 | 0.558 | n/a | n/a | 10.1-11.8 | 19.2-21.5 | 13.7 | 11.6 | 14 | 192 |
| real | cursor | 1 | 316 | 1200 | 583 | 579 | 596 | 643 | 653 | 0.265 | 0.542 | 0.546 | 0.531 | 0.492 | 0.485 | 323 | 1210 | 602 | 594 | 645 | 654 | 0.266 | 0.536 | 0.543 | 0.5 | 0.493 | 316-337 | 583-617 | 1 | 1.01 | 1 | 20.5 |
| real | cursor | all | 45.8 | 171 | 83 | 80 | 81 | 84.5 | 87.4 | 0.268 | 0.552 | 0.572 | 0.565 | 0.542 | 0.524 | 47.5 | 174 | 84.4 | 83.2 | 86.5 | 91.8 | 0.272 | 0.562 | 0.57 | 0.549 | 0.517 | 45.8-50.4 | 83-89.2 | 12.5 | 12.6 | 13 | 141 |
| real | parse | 1 | 275 | 854 | 524 | 522 | 531 | 541 | 588 | 0.322 | 0.523 | 0.526 | 0.517 | 0.507 | 0.467 | 277 | 868 | 535 | 531 | 545 | 598 | 0.319 | 0.518 | 0.522 | 0.508 | 0.463 | 275-292 | 524-580 | 1 | 1.01 | 1 | 23.6 |
| real | parse | all | 40.4 | 119 | 74.5 | 73.3 | 73 | 75 | 78.8 | 0.338 | 0.542 | 0.551 | 0.553 | 0.538 | 0.512 | 41.2 | 124 | 77.7 | 75.8 | 75.8 | 80.7 | 0.333 | 0.529 | 0.543 | 0.543 | 0.51 | 40.4-44.8 | 74.5-82.3 | 12.3 | 12.3 | 12.9 | 160 |
| real | walk | 1 | 307 | 1120 | 554 | 554 | n/a | n/a | n/a | 0.274 | 0.554 | 0.554 | n/a | n/a | n/a | 309 | 1150 | 561 | 558 | n/a | n/a | 0.268 | 0.551 | 0.554 | n/a | n/a | 307-322 | 554-635 | 1 | 1.01 | 1 | 21.1 |
| real | walk | all | 44.3 | 160 | 78.1 | 77.6 | n/a | n/a | n/a | 0.277 | 0.567 | 0.571 | n/a | n/a | n/a | 45.8 | 165 | 80.6 | 80.4 | n/a | n/a | 0.278 | 0.569 | 0.57 | n/a | n/a | 44.3-48.1 | 78.1-88.6 | 12.4 | 12.4 | 12.9 | 146 |
| realerr | cursor | 1 | 16.1 | 59.5 | 31.7 | 31.6 | 32.1 | 34.1 | 36.3 | 0.27 | 0.506 | 0.508 | 0.5 | 0.471 | 0.442 | 16.2 | 61.2 | 32.5 | 32 | 35.4 | 36.9 | 0.264 | 0.499 | 0.506 | 0.457 | 0.438 | 16.1-16.8 | 31.7-38.2 | 1 | 1.01 | 1 | 33.9 |
| realerr | cursor | all | 4.75 | 16 | 9 | 8.94 | 8.89 | 9.53 | 9.64 | 0.297 | 0.528 | 0.531 | 0.534 | 0.498 | 0.493 | 5.01 | 17.6 | 9.67 | 9.44 | 10.2 | 9.86 | 0.285 | 0.518 | 0.531 | 0.491 | 0.508 | 4.75-6.26 | 9-11 | 4.56 * | 4.74 * | 4.57 * | 114 |
| realerr | parse | 1 | 14.2 | 44.2 | 29.1 | 29 | 29.1 | 29.3 | 32.1 | 0.322 | 0.489 | 0.49 | 0.488 | 0.485 | 0.442 | 14.3 | 44.8 | 29.4 | 29.1 | 29.4 | 32.6 | 0.319 | 0.486 | 0.492 | 0.486 | 0.439 | 14.2-15 | 29.1-30.7 | 1 | 1.01 | 1 | 38.3 |
| realerr | parse | all | 4.25 | 12.1 | 8.43 | 8.57 | 8.17 | 8.82 | 9.39 | 0.352 | 0.504 | 0.496 | 0.52 | 0.482 | 0.453 | 4.65 | 12.5 | 9.07 | 8.92 | 9.4 | 9.68 | 0.372 | 0.513 | 0.522 | 0.495 | 0.481 | 4.25-5.17 | 8.43-9.85 | 4.42 * | 4.8 * | 4.62 * | 128 |
| realerr | walk | 1 | 15.6 | 56.3 | 30.8 | 30.4 | n/a | n/a | n/a | 0.277 | 0.505 | 0.512 | n/a | n/a | n/a | 15.8 | 58.3 | 32.3 | 30.6 | n/a | n/a | 0.27 | 0.488 | 0.514 | n/a | n/a | 15.6-19.1 | 30.8-33.6 | 1 | 1.01 | 1 | 34.9 |
| realerr | walk | all | 4.73 | 15.2 | 8.82 | 8.77 | n/a | n/a | n/a | 0.311 | 0.536 | 0.539 | n/a | n/a | n/a | 5.13 | 16.3 | 9.27 | 9.06 | n/a | n/a | 0.315 | 0.554 | 0.567 | n/a | n/a | 4.73-7.06 | 8.82-11.6 | 4.38 * | 4.73 * | 4.58 * | 115 |

### kotlin: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 80.2 | 297 | 154 | 154 | 157 | 170 | 167 | 0.27 | 0.521 | 0.522 | 0.51 | 0.473 | 0.481 | 81.3 | 305 | 157 | 158 | 171 | 169 | 0.266 | 0.519 | 0.514 | 0.477 | 0.482 | 80.2-87.1 | 154-164 |
| broken | cursor | all | 151 | 531 | 274 | 275 | 270 | 286 | 292 | 0.284 | 0.55 | 0.55 | 0.56 | 0.527 | 0.517 | 157 | 553 | 288 | 288 | 290 | 301 | 0.285 | 0.547 | 0.547 | 0.543 | 0.524 | 151-167 | 274-297 |
| broken | parse | 1 | 71.3 | 221 | 141 | 140 | 142 | 145 | 153 | 0.323 | 0.505 | 0.508 | 0.502 | 0.491 | 0.465 | 72.4 | 227 | 146 | 145 | 154 | 155 | 0.319 | 0.497 | 0.499 | 0.472 | 0.467 | 71.3-74.6 | 141-157 |
| broken | parse | all | 131 | 384 | 242 | 254 | 245 | 248 | 274 | 0.34 | 0.54 | 0.513 | 0.534 | 0.526 | 0.477 | 140 | 401 | 266 | 263 | 254 | 275 | 0.348 | 0.526 | 0.531 | 0.549 | 0.509 | 131-145 | 242-273 |
| broken | walk | 1 | 79 | 286 | 148 | 147 | n/a | n/a | n/a | 0.276 | 0.533 | 0.536 | n/a | n/a | n/a | 80.1 | 290 | 149 | 152 | n/a | n/a | 0.277 | 0.539 | 0.528 | n/a | n/a | 79-82.5 | 148-159 |
| broken | walk | all | 141 | 506 | 272 | 260 | n/a | n/a | n/a | 0.278 | 0.516 | 0.541 | n/a | n/a | n/a | 149 | 516 | 280 | 272 | n/a | n/a | 0.289 | 0.531 | 0.547 | n/a | n/a | 141-156 | 272-285 |
| real | cursor | 1 | 317 | 1200 | 583 | 579 | 596 | 643 | 653 | 0.263 | 0.543 | 0.547 | 0.532 | 0.493 | 0.485 | 323 | 1220 | 602 | 594 | 645 | 654 | 0.265 | 0.537 | 0.544 | 0.502 | 0.494 | 317-337 | 583-617 |
| real | cursor | all | 579 | 2170 | 1090 | 1050 | 1040 | 1090 | 1160 | 0.266 | 0.532 | 0.553 | 0.555 | 0.533 | 0.501 | 596 | 2200 | 1110 | 1070 | 1130 | 1160 | 0.271 | 0.537 | 0.556 | 0.526 | 0.513 | 579-604 | 1090-1110 |
| real | parse | 1 | 275 | 858 | 524 | 522 | 531 | 541 | 588 | 0.32 | 0.524 | 0.526 | 0.518 | 0.508 | 0.467 | 277 | 873 | 535 | 531 | 545 | 598 | 0.318 | 0.518 | 0.523 | 0.509 | 0.464 | 275-293 | 524-580 |
| real | parse | all | 496 | 1500 | 986 | 952 | 972 | 984 | 1010 | 0.33 | 0.503 | 0.521 | 0.51 | 0.504 | 0.492 | 513 | 1520 | 1000 | 974 | 996 | 1050 | 0.339 | 0.512 | 0.527 | 0.516 | 0.49 | 496-520 | 986-1010 |
| real | walk | 1 | 307 | 1130 | 554 | 554 | n/a | n/a | n/a | 0.272 | 0.555 | 0.555 | n/a | n/a | n/a | 310 | 1160 | 561 | 558 | n/a | n/a | 0.266 | 0.552 | 0.554 | n/a | n/a | 307-322 | 554-635 |
| real | walk | all | 548 | 2000 | 1010 | 996 | n/a | n/a | n/a | 0.274 | 0.542 | 0.55 | n/a | n/a | n/a | 573 | 2030 | 1050 | 1030 | n/a | n/a | 0.282 | 0.546 | 0.558 | n/a | n/a | 548-583 | 1010-1070 |
| realerr | cursor | 1 | 16.1 | 60.1 | 31.7 | 31.6 | 32.1 | 34.1 | 36.3 | 0.267 | 0.507 | 0.509 | 0.501 | 0.472 | 0.443 | 16.2 | 61.9 | 32.5 | 32 | 35.4 | 36.9 | 0.262 | 0.499 | 0.506 | 0.458 | 0.439 | 16.1-16.8 | 31.7-38.2 |
| realerr | cursor | all | 21.6 | 78.8 | 43.5 | 42.8 | 42.6 | 45.4 | 46.3 | 0.274 | 0.496 | 0.504 | 0.507 | 0.475 | 0.466 | 23.1 | 84.1 | 44.4 | 44.2 | 46.4 | 47.8 | 0.275 | 0.522 | 0.524 | 0.498 | 0.484 | 21.6-25.5 | 43.5-46.6 |
| realerr | parse | 1 | 14.2 | 44.4 | 29 | 29 | 29.1 | 29.3 | 32.1 | 0.321 | 0.49 | 0.491 | 0.489 | 0.486 | 0.443 | 14.3 | 45.4 | 29.4 | 29.1 | 29.4 | 32.6 | 0.316 | 0.487 | 0.493 | 0.488 | 0.44 | 14.2-15.1 | 29-30.7 |
| realerr | parse | all | 19.4 | 58.1 | 39.5 | 39.5 | 39.5 | 40.6 | 44.1 | 0.334 | 0.49 | 0.491 | 0.491 | 0.477 | 0.44 | 20.6 | 61.3 | 42.1 | 41.7 | 42.1 | 46.6 | 0.336 | 0.488 | 0.493 | 0.488 | 0.441 | 19.4-21.6 | 39.5-43.9 |
| realerr | walk | 1 | 15.6 | 56.8 | 30.8 | 30.4 | n/a | n/a | n/a | 0.275 | 0.506 | 0.512 | n/a | n/a | n/a | 15.8 | 59.1 | 32.3 | 30.6 | n/a | n/a | 0.267 | 0.489 | 0.515 | n/a | n/a | 15.6-19.1 | 30.8-33.6 |
| realerr | walk | all | 21.3 | 72.4 | 41.9 | 40.8 | n/a | n/a | n/a | 0.294 | 0.508 | 0.521 | n/a | n/a | n/a | 22.6 | 79 | 43.9 | 42.7 | n/a | n/a | 0.286 | 0.514 | 0.529 | n/a | n/a | 21.3-25.5 | 41.9-46.8 |

### kotlin: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1710 | 5270 | 2800 | 2710 | 2730 | 2850 | 2900 | 0.325 | 0.612 | 0.633 | 0.627 | 0.601 | 0.592 | 1710 | 5270 | 2800 | 2710 | 2850 | 2900 | 0.325 | 0.611 | 0.633 | 0.601 | 0.592 | 1710-1720 | 2800-2800 |
| broken | cursor | all | 1730 | 5320 | 2810 | 2710 | 2730 | 2860 | 2900 | 0.324 | 0.614 | 0.636 | 0.631 | 0.603 | 0.595 | 1730 | 5340 | 2810 | 2710 | 2860 | 2900 | 0.325 | 0.617 | 0.638 | 0.606 | 0.597 | 1730-1820 | 2810-2810 |
| broken | parse | 1 | 1500 | 3710 | 2560 | 2460 | 2480 | 2580 | 2640 | 0.404 | 0.587 | 0.609 | 0.605 | 0.581 | 0.569 | 1500 | 3710 | 2560 | 2460 | 2580 | 2640 | 0.404 | 0.587 | 0.609 | 0.581 | 0.569 | 1500-1500 | 2560-2560 |
| broken | parse | all | 1510 | 3740 | 2560 | 2460 | 2480 | 2580 | 2640 | 0.403 | 0.59 | 0.613 | 0.608 | 0.584 | 0.572 | 1520 | 3750 | 2560 | 2470 | 2580 | 2640 | 0.404 | 0.592 | 0.615 | 0.586 | 0.575 | 1510-1530 | 2560-2560 |
| broken | walk | 1 | 1640 | 4830 | 2680 | 2580 | n/a | n/a | n/a | 0.341 | 0.614 | 0.637 | n/a | n/a | n/a | 1640 | 4830 | 2680 | 2580 | n/a | n/a | 0.34 | 0.614 | 0.637 | n/a | n/a | 1640-1640 | 2680-2680 |
| broken | walk | all | 1650 | 4880 | 2680 | 2580 | n/a | n/a | n/a | 0.339 | 0.618 | 0.641 | n/a | n/a | n/a | 1660 | 4900 | 2680 | 2590 | n/a | n/a | 0.339 | 0.619 | 0.642 | n/a | n/a | 1650-1690 | 2680-2680 |
| real | cursor | 1 | 6870 | 21600 | 10700 | 10200 | 10400 | 11000 | 11100 | 0.319 | 0.644 | 0.671 | 0.658 | 0.625 | 0.62 | 6870 | 21600 | 10700 | 10200 | 11000 | 11100 | 0.318 | 0.644 | 0.67 | 0.625 | 0.62 | 6870-6870 | 10700-10700 |
| real | cursor | all | 6870 | 21600 | 10700 | 10200 | 10400 | 11000 | 11100 | 0.318 | 0.644 | 0.67 | 0.657 | 0.625 | 0.62 | 6870 | 21600 | 10700 | 10300 | 11000 | 11100 | 0.317 | 0.643 | 0.669 | 0.624 | 0.62 | 6870-6920 | 10700-10700 |
| real | parse | 1 | 5860 | 14400 | 9500 | 9070 | 9240 | 9700 | 9850 | 0.408 | 0.617 | 0.646 | 0.634 | 0.604 | 0.595 | 5860 | 14400 | 9500 | 9080 | 9700 | 9850 | 0.408 | 0.617 | 0.645 | 0.604 | 0.595 | 5860-5860 | 9500-9510 |
| real | parse | all | 5860 | 14400 | 9510 | 9080 | 9260 | 9710 | 9870 | 0.408 | 0.616 | 0.646 | 0.633 | 0.604 | 0.594 | 5860 | 14400 | 9520 | 9090 | 9720 | 9870 | 0.408 | 0.616 | 0.645 | 0.603 | 0.594 | 5860-5860 | 9510-9530 |
| real | walk | 1 | 6520 | 19500 | 10000 | 9620 | n/a | n/a | n/a | 0.335 | 0.649 | 0.678 | n/a | n/a | n/a | 6520 | 19500 | 10000 | 9620 | n/a | n/a | 0.335 | 0.649 | 0.678 | n/a | n/a | 6520-6520 | 10000-10100 |
| real | walk | all | 6520 | 19500 | 10100 | 9630 | n/a | n/a | n/a | 0.334 | 0.649 | 0.678 | n/a | n/a | n/a | 6520 | 19600 | 10100 | 9640 | n/a | n/a | 0.334 | 0.648 | 0.677 | n/a | n/a | 6520-6530 | 10100-10100 |
| realerr | cursor | 1 | 358 | 1100 | 593 | 574 | 578 | 604 | 611 | 0.326 | 0.604 | 0.623 | 0.619 | 0.593 | 0.586 | 358 | 1100 | 595 | 576 | 606 | 613 | 0.326 | 0.602 | 0.621 | 0.591 | 0.584 | 358-358 | 593-595 |
| realerr | cursor | all | 360 | 1100 | 594 | 576 | 577 | 605 | 613 | 0.327 | 0.605 | 0.625 | 0.624 | 0.594 | 0.587 | 362 | 1100 | 596 | 577 | 606 | 614 | 0.328 | 0.608 | 0.627 | 0.597 | 0.589 | 360-368 | 594-598 |
| realerr | parse | 1 | 313 | 773 | 543 | 524 | 526 | 548 | 558 | 0.405 | 0.576 | 0.597 | 0.594 | 0.57 | 0.56 | 313 | 773 | 543 | 524 | 549 | 559 | 0.405 | 0.576 | 0.596 | 0.57 | 0.56 | 313-313 | 543-544 |
| realerr | parse | all | 316 | 771 | 541 | 524 | 525 | 550 | 557 | 0.41 | 0.583 | 0.603 | 0.601 | 0.574 | 0.567 | 317 | 776 | 546 | 527 | 551 | 560 | 0.408 | 0.581 | 0.601 | 0.575 | 0.566 | 316-324 | 541-548 |
| realerr | walk | 1 | 342 | 1000 | 567 | 548 | n/a | n/a | n/a | 0.341 | 0.604 | 0.624 | n/a | n/a | n/a | 342 | 1000 | 568 | 549 | n/a | n/a | 0.341 | 0.603 | 0.624 | n/a | n/a | 342-342 | 567-569 |
| realerr | walk | all | 345 | 1010 | 567 | 547 | n/a | n/a | n/a | 0.343 | 0.609 | 0.631 | n/a | n/a | n/a | 346 | 1010 | 571 | 552 | n/a | n/a | 0.343 | 0.607 | 0.628 | n/a | n/a | 345-352 | 567-573 |

### kotlin: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 418 | 1530 | 803 | 803 | 821 | 886 | 870 | 0.272 | 0.52 | 0.52 | 0.509 | 0.472 | 0.48 | 422 | 1580 | 817 | 826 | 887 | 881 | 0.268 | 0.516 | 0.511 | 0.476 | 0.479 | 418-447 | 803-856 |
| broken | cursor | all | 682 | 2380 | 1260 | 1280 | 1240 | 1310 | 1340 | 0.286 | 0.541 | 0.534 | 0.549 | 0.519 | 0.509 | 709 | 2460 | 1310 | 1320 | 1340 | 1380 | 0.288 | 0.541 | 0.535 | 0.528 | 0.515 | 682-748 | 1260-1350 |
| broken | parse | 1 | 372 | 1140 | 737 | 732 | 742 | 758 | 799 | 0.325 | 0.505 | 0.508 | 0.501 | 0.491 | 0.465 | 378 | 1170 | 761 | 757 | 799 | 809 | 0.321 | 0.496 | 0.498 | 0.473 | 0.466 | 372-389 | 737-814 |
| broken | parse | all | 600 | 1740 | 1110 | 1180 | 1130 | 1140 | 1250 | 0.346 | 0.539 | 0.51 | 0.53 | 0.527 | 0.481 | 631 | 1800 | 1210 | 1210 | 1170 | 1270 | 0.351 | 0.519 | 0.52 | 0.538 | 0.498 | 600-645 | 1110-1250 |
| broken | walk | 1 | 412 | 1480 | 774 | 770 | n/a | n/a | n/a | 0.279 | 0.533 | 0.535 | n/a | n/a | n/a | 418 | 1500 | 776 | 792 | n/a | n/a | 0.279 | 0.538 | 0.527 | n/a | n/a | 412-430 | 774-828 |
| broken | walk | all | 644 | 2270 | 1250 | 1210 | n/a | n/a | n/a | 0.284 | 0.516 | 0.534 | n/a | n/a | n/a | 677 | 2300 | 1280 | 1260 | n/a | n/a | 0.294 | 0.531 | 0.539 | n/a | n/a | 644-705 | 1250-1300 |
| real | cursor | 1 | 1650 | 6230 | 3040 | 3020 | 3110 | 3350 | 3400 | 0.265 | 0.543 | 0.547 | 0.532 | 0.493 | 0.485 | 1680 | 6330 | 3140 | 3100 | 3360 | 3410 | 0.266 | 0.536 | 0.543 | 0.5 | 0.494 | 1650-1760 | 3040-3220 |
| real | cursor | all | 2610 | 9650 | 4920 | 4780 | 4750 | 4980 | 5200 | 0.271 | 0.531 | 0.547 | 0.55 | 0.524 | 0.503 | 2680 | 9760 | 5000 | 4880 | 5150 | 5250 | 0.275 | 0.536 | 0.549 | 0.52 | 0.511 | 2610-2710 | 4920-5020 |
| real | parse | 1 | 1430 | 4460 | 2740 | 2730 | 2770 | 2830 | 3070 | 0.322 | 0.524 | 0.526 | 0.518 | 0.507 | 0.467 | 1450 | 4530 | 2790 | 2770 | 2850 | 3120 | 0.32 | 0.518 | 0.522 | 0.508 | 0.464 | 1430-1530 | 2740-3030 |
| real | parse | all | 2270 | 6740 | 4480 | 4360 | 4420 | 4490 | 4600 | 0.337 | 0.507 | 0.521 | 0.514 | 0.506 | 0.494 | 2330 | 6780 | 4550 | 4440 | 4540 | 4760 | 0.343 | 0.512 | 0.524 | 0.513 | 0.489 | 2270-2360 | 4480-4590 |
| real | walk | 1 | 1600 | 5860 | 2890 | 2890 | n/a | n/a | n/a | 0.273 | 0.554 | 0.554 | n/a | n/a | n/a | 1610 | 6020 | 2930 | 2910 | n/a | n/a | 0.268 | 0.551 | 0.554 | n/a | n/a | 1600-1680 | 2890-3310 |
| real | walk | all | 2490 | 8900 | 4580 | 4570 | n/a | n/a | n/a | 0.28 | 0.543 | 0.545 | n/a | n/a | n/a | 2580 | 9020 | 4770 | 4690 | n/a | n/a | 0.286 | 0.541 | 0.549 | n/a | n/a | 2490-2600 | 4580-4820 |
| realerr | cursor | 1 | 83.8 | 310 | 165 | 165 | 168 | 178 | 189 | 0.27 | 0.506 | 0.508 | 0.499 | 0.471 | 0.443 | 84.4 | 320 | 169 | 167 | 185 | 192 | 0.264 | 0.498 | 0.506 | 0.457 | 0.441 | 83.8-87.5 | 165-199 |
| realerr | cursor | all | 111 | 398 | 218 | 217 | 218 | 231 | 232 | 0.278 | 0.507 | 0.509 | 0.508 | 0.479 | 0.476 | 116 | 418 | 225 | 222 | 235 | 241 | 0.278 | 0.515 | 0.522 | 0.494 | 0.482 | 111-129 | 218-234 |
| realerr | parse | 1 | 74.2 | 230 | 151 | 151 | 152 | 153 | 168 | 0.322 | 0.49 | 0.491 | 0.489 | 0.485 | 0.442 | 74.6 | 233 | 153 | 152 | 153 | 170 | 0.32 | 0.486 | 0.492 | 0.487 | 0.439 | 74.2-78.3 | 151-160 |
| realerr | parse | all | 98.6 | 289 | 200 | 199 | 198 | 207 | 222 | 0.342 | 0.493 | 0.497 | 0.498 | 0.476 | 0.445 | 104 | 306 | 213 | 208 | 208 | 226 | 0.339 | 0.488 | 0.497 | 0.498 | 0.458 | 98.6-108 | 200-223 |
| realerr | walk | 1 | 81.3 | 294 | 161 | 159 | n/a | n/a | n/a | 0.276 | 0.506 | 0.512 | n/a | n/a | n/a | 82.2 | 303 | 168 | 160 | n/a | n/a | 0.271 | 0.488 | 0.515 | n/a | n/a | 81.3-99.7 | 161-176 |
| realerr | walk | all | 109 | 364 | 209 | 208 | n/a | n/a | n/a | 0.299 | 0.52 | 0.523 | n/a | n/a | n/a | 114 | 391 | 220 | 216 | n/a | n/a | 0.292 | 0.518 | 0.529 | n/a | n/a | 109-130 | 209-234 |

### kotlin: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 216 | 1560 | 248 | 248 | 255 | 273 | 260 | 0.138 | 0.869 | 0.869 | 0.845 | 0.788 | 0.828 |
| broken | cursor minus parse | 1 | cycles M | 45.8 | 391 | 66.8 | 70 | 78.9 | 128 | 70.3 | 0.117 | 0.686 | 0.653 | 0.58 | 0.358 | 0.651 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 8.89 | 76.3 | 12.8 | 13.4 | 15.1 | 24.4 | 13.4 | 0.117 | 0.697 | 0.661 | 0.587 | 0.364 | 0.663 |
| broken | cursor minus parse | all | instructions M | 216 | 1580 | 253 | 253 | 254 | 275 | 262 | 0.137 | 0.856 | 0.856 | 0.854 | 0.787 | 0.828 |
| broken | cursor minus parse | all | cycles M | 81.6 | 647 | 147 | 100 | 109 | 174 | 91.1 | 0.126 | 0.556 | 0.816 | 0.747 | 0.469 | 0.896 |
| broken | cursor minus parse | all | CPU user+sys ms | 20.4 | 147 | 32.5 | 20.4 | 25 | 38.2 | 18.5 | 0.139 | 0.629 | 1 | 0.815 | 0.534 | 1.1 |
| broken | walk minus parse | 1 | instructions M | 145 | 1110 | 121 | 121 | n/a | n/a | n/a | 0.13 | 1.2 | 1.2 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 40.2 | 333 | 37.1 | 37.1 | n/a | n/a | n/a | 0.121 | 1.08 | 1.08 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 7.69 | 65.3 | 7.08 | 7.12 | n/a | n/a | n/a | 0.118 | 1.09 | 1.08 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 145 | 1140 | 121 | 122 | n/a | n/a | n/a | 0.128 | 1.2 | 1.19 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 43.9 | 535 | 135 | 29.2 | n/a | n/a | n/a | 0.082 | 0.324 | 1.5 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 10.1 | 121 | 30.7 | 5.71 | n/a | n/a | n/a | 0.0829 | 0.329 | 1.76 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1010 | 7190 | 1170 | 1170 | 1200 | 1280 | 1220 | 0.14 | 0.864 | 0.864 | 0.84 | 0.784 | 0.824 |
| real | cursor minus parse | 1 | cycles M | 219 | 1780 | 309 | 296 | 337 | 526 | 333 | 0.123 | 0.709 | 0.739 | 0.649 | 0.416 | 0.657 |
| real | cursor minus parse | 1 | CPU user+sys ms | 42 | 346 | 58.9 | 57.2 | 65 | 101 | 64.2 | 0.121 | 0.712 | 0.734 | 0.645 | 0.415 | 0.653 |
| real | cursor minus parse | all | instructions M | 1010 | 7240 | 1160 | 1170 | 1190 | 1280 | 1210 | 0.139 | 0.865 | 0.859 | 0.847 | 0.787 | 0.829 |
| real | cursor minus parse | all | cycles M | 341 | 2910 | 440 | 418 | 327 | 497 | 601 | 0.117 | 0.776 | 0.816 | 1.04 | 0.686 | 0.567 |
| real | cursor minus parse | all | CPU user+sys ms | 82.8 | 671 | 103 | 94.1 | 71.1 | 103 | 148 | 0.124 | 0.808 | 0.88 | 1.17 | 0.805 | 0.561 |
| real | walk minus parse | 1 | instructions M | 663 | 5100 | 546 | 546 | n/a | n/a | n/a | 0.13 | 1.21 | 1.21 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 167 | 1400 | 156 | 162 | n/a | n/a | n/a | 0.119 | 1.08 | 1.03 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 32.2 | 272 | 29.1 | 31.5 | n/a | n/a | n/a | 0.119 | 1.11 | 1.02 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 663 | 5180 | 551 | 553 | n/a | n/a | n/a | 0.128 | 1.2 | 1.2 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 217 | 2160 | 101 | 208 | n/a | n/a | n/a | 0.101 | 2.15 | 1.05 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 51.8 | 497 | 25.4 | 44.2 | n/a | n/a | n/a | 0.104 | 2.04 | 1.17 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 45.3 | 325 | 50.5 | 50.5 | 51.9 | 55.8 | 53 | 0.139 | 0.897 | 0.897 | 0.872 | 0.812 | 0.854 |
| realerr | cursor minus parse | 1 | cycles M | 9.63 | 80.1 | 14 | 13.9 | 16 | 24.9 | 21.3 | 0.12 | 0.689 | 0.693 | 0.6 | 0.386 | 0.453 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 1.85 | 15.8 | 2.69 | 2.66 | 3.04 | 4.79 | 4.2 | 0.117 | 0.69 | 0.697 | 0.608 | 0.386 | 0.441 |
| realerr | cursor minus parse | all | instructions M | 43.9 | 329 | 52.6 | 51.9 | 51.4 | 55.2 | 55.8 | 0.134 | 0.835 | 0.847 | 0.855 | 0.797 | 0.788 |
| realerr | cursor minus parse | all | cycles M | 12 | 109 | 18 | 18.6 | 19.7 | 23.9 | 10.8 | 0.11 | 0.671 | 0.646 | 0.61 | 0.504 | 1.12 |
| realerr | cursor minus parse | all | CPU user+sys ms | 2.19 | 20.7 | 4 | 3.31 | 3.13 | 4.82 | 2.28 | 0.105 | 0.547 | 0.66 | 0.699 | 0.453 | 0.959 |
| realerr | walk minus parse | 1 | instructions M | 29.8 | 231 | 24.7 | 24.7 | n/a | n/a | n/a | 0.129 | 1.21 | 1.21 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 7.2 | 64.4 | 9.22 | 7.7 | n/a | n/a | n/a | 0.112 | 0.781 | 0.935 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 1.36 | 12.4 | 1.79 | 1.46 | n/a | n/a | n/a | 0.109 | 0.759 | 0.929 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 29.6 | 234 | 26.1 | 23.7 | n/a | n/a | n/a | 0.126 | 1.13 | 1.25 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 10.3 | 75.8 | 9.1 | 9.7 | n/a | n/a | n/a | 0.135 | 1.13 | 1.06 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 1.89 | 14.3 | 2.32 | 1.35 | n/a | n/a | n/a | 0.132 | 0.813 | 1.4 | n/a | n/a | n/a |

### kotlin: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 21.5 | 23.5 | 9.18 | 0.915 | 2.34 | 21.5 | 23.5 | 9.18 | 0.915 | 2.34 | 15.8 | 16.3 | 4.75 | 0.971 | 3.33 |
| broken | cursor | all | 36.8 | 38.2 | 21.1 | 0.961 | 1.74 | 36.8 | 38.2 | 21.1 | 0.962 | 1.74 | 29.7 | 30.9 | 16.9 | 0.96 | 1.76 |
| broken | parse | 1 | 15.5 | 16.4 | 9.37 | 0.95 | 1.66 | 15.6 | 16.4 | 9.37 | 0.951 | 1.66 | 9.86 | 9.11 | 4.89 | 1.08 | 2.02 |
| broken | parse | all | 31.6 | 37.5 | 21.1 | 0.842 | 1.49 | 31.6 | 37.5 | 21.1 | 0.842 | 1.49 | 24.3 | 30.1 | 16.6 | 0.806 | 1.46 |
| broken | walk | 1 | 15.1 | 24 | 9.47 | 0.631 | 1.6 | 15.1 | 24 | 9.47 | 0.631 | 1.6 | 9.45 | 16.8 | 5.23 | 0.563 | 1.81 |
| broken | walk | all | 31.7 | 38 | 20.4 | 0.835 | 1.56 | 31.7 | 38 | 20.4 | 0.835 | 1.56 | 24.7 | 30.6 | 16 | 0.806 | 1.54 |
| real | cursor | 1 | 36.3 | 55.1 | 16.9 | 0.657 | 2.15 | 36.3 | 55.1 | 16.9 | 0.657 | 2.15 | 24.1 | 41.7 | 8.06 | 0.578 | 2.99 |
| real | cursor | all | 81.5 | 102 | 62.3 | 0.8 | 1.31 | 81.5 | 102 | 62.3 | 0.8 | 1.31 | 69.1 | 88.5 | 53.6 | 0.781 | 1.29 |
| real | parse | 1 | 24.5 | 30.6 | 17.1 | 0.802 | 1.43 | 24.5 | 30.6 | 17.1 | 0.802 | 1.43 | 12.4 | 17.3 | 8.43 | 0.719 | 1.47 |
| real | parse | all | 57.2 | 101 | 65.2 | 0.567 | 0.878 | 57.2 | 101 | 65.2 | 0.567 | 0.878 | 44.7 | 87.5 | 56.5 | 0.511 | 0.792 |
| real | walk | 1 | 24.4 | 55.6 | 17.3 | 0.439 | 1.41 | 24.4 | 55.6 | 17.3 | 0.439 | 1.41 | 12.3 | 42.1 | 8.56 | 0.291 | 1.43 |
| real | walk | all | 58.4 | 98.2 | 62 | 0.595 | 0.942 | 58.5 | 98.3 | 62 | 0.595 | 0.942 | 45.9 | 84.8 | 53.2 | 0.542 | 0.864 |
| realerr | cursor | 1 | 13.5 | 22.6 | 8.48 | 0.6 | 1.6 | 13.6 | 22.6 | 8.48 | 0.6 | 1.6 | 9.8 | 17.4 | 5.5 | 0.565 | 1.78 |
| realerr | cursor | all | 32.6 | 39.5 | 26.8 | 0.824 | 1.22 | 32.6 | 39.5 | 26.8 | 0.824 | 1.22 | 28 | 34.2 | 23.8 | 0.819 | 1.17 |
| realerr | parse | 1 | 13.1 | 15.3 | 8.37 | 0.861 | 1.57 | 13.2 | 15.3 | 8.37 | 0.863 | 1.57 | 9.37 | 9.93 | 5.53 | 0.944 | 1.7 |
| realerr | parse | all | 33.7 | 38.9 | 26 | 0.865 | 1.29 | 33.7 | 38.9 | 26 | 0.866 | 1.29 | 28.9 | 33.7 | 23 | 0.858 | 1.26 |
| realerr | walk | 1 | 13.1 | 19.2 | 8.37 | 0.682 | 1.56 | 13.1 | 19.2 | 8.37 | 0.683 | 1.57 | 9.37 | 13.9 | 5.42 | 0.676 | 1.73 |
| realerr | walk | all | 34.7 | 38.5 | 25.1 | 0.901 | 1.38 | 34.7 | 38.5 | 25.1 | 0.901 | 1.38 | 29.9 | 33.2 | 22.1 | 0.899 | 1.35 |

### kotlin: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 15.5 | 16.8 | 9.23 | 0.92 | 1.67 | 15.5 | 16.8 | 9.23 | 0.92 | 1.68 | 9.77 | 9.46 | 5.05 | 1.03 | 1.93 |
| broken | all | 35.1 | 51.1 | 25.3 | 0.686 | 1.39 | 35.1 | 51.1 | 25.3 | 0.686 | 1.39 | 28.1 | 43.8 | 20.9 | 0.64 | 1.34 |
| real | 1 | 24.6 | 33.1 | 17 | 0.742 | 1.45 | 24.6 | 33.1 | 17 | 0.742 | 1.45 | 12.5 | 19.8 | 8.4 | 0.63 | 1.48 |
| real | all | 62.5 | 123 | 79.7 | 0.508 | 0.785 | 62.5 | 123 | 79.7 | 0.508 | 0.785 | 50.1 | 110 | 70.7 | 0.456 | 0.708 |
| realerr | 1 | 13.3 | 17.8 | 8.27 | 0.748 | 1.61 | 13.4 | 17.8 | 8.27 | 0.749 | 1.61 | 9.46 | 12.5 | 5.42 | 0.758 | 1.75 |
| realerr | all | 41.3 | 69.9 | 34.6 | 0.591 | 1.19 | 41.3 | 69.9 | 34.6 | 0.591 | 1.19 | 36.5 | 64.5 | 31.3 | 0.565 | 1.16 |

### kotlin: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0108 (real), 0.8 (realerr), 0.0345 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.179 | 6670 | 11.2 | 1390 | 127000 | 1390 | 127000 | 0.000129 |
| real | steady | 0.0108 | 3.32 | 11.2 | 1390 | 127000 | 1390 | 127000 | 0 |
| realerr | cold | 22.1 | 227000 | 14.4 | 3780 | 352000 | 3780 | 352000 | 0.00584 |
| realerr | steady | 0.8 | 458 | 14.4 | 3780 | 352000 | 3780 | 352000 | 0.000212 |
| broken | cold | 3.24 | 10100 | 12.7 | 970 | 90800 | 970 | 90800 | 0.00334 |
| broken | steady | 0.0345 | 6.25 | 12.6 | 969 | 90800 | 969 | 90800 | 0 |

### kotlin: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2049 B | init ms | 0.00913 | 0.0115 | 0.00584 | 0.795 | 1.56 | 15 |
| #0 2049 B | parse1 ms | 0.243 | 0.348 | 0.262 | 0.697 | 0.927 | 15 |
| #0 2049 B | init+parse1 ms | 0.258 | 0.36 | 0.267 | 0.716 | 0.966 | 15 |
| #0 2049 B | parse2 ms | 0.106 | 0.28 | 0.162 | 0.378 | 0.652 | 15 |
| #0 2049 B | process user+sys ms | 1.07 | 1.65 | 0.712 | 0.648 | 1.5 | 15 |
| #0 2049 B | exec to exit instr user+kernel M | 7.97 | 13.3 | 6.93 | 0.601 | 1.15 | 15 |
| #0 2049 B | exec to exit instr user M | 5.07 | 9.42 | 5.81 | 0.538 | 0.874 | 15 |
| #0 2049 B | exec to exit instr kernel M | 2.89 | 3.85 | 1.12 | 0.749 | 2.58 | 15 |
| #0 2049 B | exec to exit cycles user+kernel M | 4.9 | 7.53 | 3.22 | 0.65 | 1.52 | 15 |
| #0 2049 B | exec to exit task-clock ms | 0.99 | 1.59 | 0.62 | 0.623 | 1.6 | 15 |
| #0 2049 B | peak RSS MB | 5.7 | 6.59 | 5.38 | 0.864 | 1.06 | 15 |
| #0 2049 B | peak footprint MB | 5.73 | 6.63 | 5.38 | 0.864 | 1.06 | 15 |
| #1 2051 B | init ms | 0.00944 | 0.0109 | 0.00561 | 0.866 | 1.68 | 15 |
| #1 2051 B | parse1 ms | 0.359 | 0.454 | 0.356 | 0.793 | 1.01 | 15 |
| #1 2051 B | init+parse1 ms | 0.368 | 0.464 | 0.362 | 0.793 | 1.02 | 15 |
| #1 2051 B | parse2 ms | 0.147 | 0.324 | 0.211 | 0.455 | 0.697 | 15 |
| #1 2051 B | process user+sys ms | 1.24 | 1.79 | 0.878 | 0.694 | 1.41 | 15 |
| #1 2051 B | exec to exit instr user+kernel M | 8.71 | 14.3 | 7.77 | 0.607 | 1.12 | 15 |
| #1 2051 B | exec to exit instr user M | 5.68 | 10.4 | 6.62 | 0.545 | 0.858 | 15 |
| #1 2051 B | exec to exit instr kernel M | 3.03 | 3.94 | 1.15 | 0.771 | 2.65 | 15 |
| #1 2051 B | exec to exit cycles user+kernel M | 5.69 | 8.53 | 4.06 | 0.667 | 1.4 | 15 |
| #1 2051 B | exec to exit task-clock ms | 1.16 | 1.72 | 0.79 | 0.674 | 1.47 | 15 |
| #1 2051 B | peak RSS MB | 6.47 | 7.48 | 5.62 | 0.865 | 1.15 | 15 |
| #1 2051 B | peak footprint MB | 6.49 | 7.51 | 5.55 | 0.865 | 1.17 | 15 |

### kotlin: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1475 | 1395888 | 0 | 0 | ok |
| realerr | 25 | 62774 | 25 | 0 | ok |
| broken | 493 | 300695 | 416 | 0 | ok |

## lua

### lua: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 204 | 793 | 428 | 425 | 410 | 461 | 0.257 | 0.477 | 0.481 | 0.497 | 0.442 | 208 | 812 | 430 | 413 | 464 | 0.257 | 0.485 | 0.505 | 0.449 | 204-222 | 428-443 | 1.01 | 1.01 | 1 | 12.1 |
| broken | cursor | all | 49.6 | 179 | 90 | 101 | 86.2 | 112 | 0.278 | 0.551 | 0.492 | 0.575 | 0.442 | 51.3 | 187 | 105 | 90.3 | 114 | 0.275 | 0.491 | 0.568 | 0.449 | 49.6-54.9 | 90-109 | 6.65 * | 7.29 * | 6.83 * | 50 |
| broken | parse | 1 | 176 | 575 | 392 | 390 | 364 | 431 | 0.307 | 0.45 | 0.452 | 0.485 | 0.409 | 178 | 589 | 404 | 367 | 435 | 0.303 | 0.442 | 0.487 | 0.411 | 176-194 | 392-424 | 1 | 1.01 | 1 | 14 |
| broken | parse | all | 46.9 | 137 | 94.1 | 93.8 | 81.9 | 107 | 0.342 | 0.498 | 0.5 | 0.573 | 0.436 | 47.9 | 140 | 101 | 83.7 | 110 | 0.341 | 0.472 | 0.572 | 0.433 | 46.9-51.1 | 94.1-111 | 6.05 * | 6.86 * | 6.3 * | 52.8 |
| broken | walk | 1 | 201 | 780 | 412 | n/a | n/a | n/a | 0.257 | 0.487 | n/a | n/a | n/a | 209 | 792 | 421 | n/a | n/a | 0.264 | 0.496 | n/a | n/a | 201-229 | 412-449 | 1 | 1.02 | 1 | 12.3 |
| broken | walk | all | 48.8 | 177 | 95 | n/a | n/a | n/a | 0.276 | 0.514 | n/a | n/a | n/a | 50.9 | 183 | 104 | n/a | n/a | 0.279 | 0.492 | n/a | n/a | 48.8-53.6 | 95-118 | 6.6 * | 7.16 * | 6.59 * | 50.7 |
| real | cursor | 1 | 353 | 1730 | 748 | 768 | 778 | 816 | 0.204 | 0.472 | 0.46 | 0.454 | 0.433 | 366 | 1750 | 758 | 803 | 824 | 0.209 | 0.483 | 0.456 | 0.444 | 353-378 | 748-785 | 1 | 1.01 | 1 | 31.3 |
| real | cursor | all | 45.8 | 226 | 86.3 | 86.5 | 85.4 | 97.1 | 0.202 | 0.531 | 0.529 | 0.536 | 0.472 | 47 | 228 | 88.3 | 85.7 | 99.4 | 0.206 | 0.532 | 0.548 | 0.472 | 45.8-51.6 | 86.3-92 | 14.9 | 14 | 15.1 | 241 |
| real | parse | 1 | 282 | 1050 | 636 | 655 | 586 | 718 | 0.267 | 0.443 | 0.431 | 0.481 | 0.393 | 288 | 1060 | 647 | 590 | 723 | 0.272 | 0.444 | 0.487 | 0.398 | 282-298 | 636-705 | 1 | 1 | 1 | 39.2 |
| real | parse | all | 35.7 | 127 | 74 | 73.5 | 68 | 79.2 | 0.28 | 0.482 | 0.486 | 0.525 | 0.451 | 36.7 | 129 | 74.8 | 70.8 | 84.3 | 0.284 | 0.49 | 0.518 | 0.435 | 35.7-45.4 | 74-80.1 | 14.4 | 13.9 | 15.2 | 310 |
| real | walk | 1 | 340 | 1610 | 698 | n/a | n/a | n/a | 0.212 | 0.487 | n/a | n/a | n/a | 347 | 1640 | 712 | n/a | n/a | 0.212 | 0.487 | n/a | n/a | 340-355 | 698-799 | 1 | 1.01 | 1 | 32.5 |
| real | walk | all | 43.6 | 203 | 81.3 | n/a | n/a | n/a | 0.214 | 0.536 | n/a | n/a | n/a | 44.3 | 207 | 82.9 | n/a | n/a | 0.214 | 0.535 | n/a | n/a | 43.6-48.4 | 81.3-88.3 | 14.9 | 14 | 15 | 254 |
| realerr (too small) | cursor | 1 | 0.0114 | 0.0513 | 0.0355 | 0.0362 | 0.0291 | 0.0438 | 0.223 | 0.322 | 0.315 | 0.392 | 0.26 | 0.0154 | 0.0566 | 0.0421 | 0.0338 | 0.047 | 0.272 | 0.366 | 0.456 | 0.328 | 0.0114-0.0564 | 0.0355-0.0488 | 1.59 | 1.23 | 0.978 | 25.5 |
| realerr (too small) | cursor | all | 0.0215 | 0.0625 | 0.0496 | 0.0611 | 0.0643 | 0.0801 | 0.344 | 0.433 | 0.352 | 0.334 | 0.269 | 0.0797 | 0.0983 | 0.0764 | 0.0787 | 0.0917 | 0.811 | 1.04 | 1.01 | 0.869 | 0.0215-0.199 | 0.0496-0.0895 | 1.41 * | 1.35 * | 2.19 * | 13.5 |
| realerr (too small) | parse | 1 | 0.0108 | 0.0426 | 0.0333 | 0.0352 | 0.0254 | 0.0509 | 0.252 | 0.323 | 0.305 | 0.424 | 0.211 | 0.0141 | 0.0477 | 0.0375 | 0.0281 | 0.059 | 0.295 | 0.376 | 0.502 | 0.239 | 0.0108-0.129 | 0.0333-0.0464 | 1.43 | 1.28 | 1.01 | 27 |
| realerr (too small) | parse | all | 0.015 | 0.053 | 0.0486 | 0.0515 | 0.0427 | 0.0704 | 0.284 | 0.31 | 0.292 | 0.352 | 0.214 | 0.0763 | 0.0848 | 0.0793 | 0.07 | 0.0837 | 0.899 | 0.962 | 1.09 | 0.911 | 0.015-0.166 | 0.0486-0.104 | 1.43 * | 1.36 * | 2.07 * | 19.3 |
| realerr (too small) | walk | 1 | 0.0112 | 0.0515 | 0.0337 | n/a | n/a | n/a | 0.218 | 0.333 | n/a | n/a | n/a | 0.0136 | 0.0575 | 0.038 | n/a | n/a | 0.237 | 0.358 | n/a | n/a | 0.0112-0.0313 | 0.0337-0.0474 | 1.74 | 1.22 | 0.996 | 25.9 |
| realerr (too small) | walk | all | 0.0227 | 0.063 | 0.0627 | n/a | n/a | n/a | 0.36 | 0.362 | n/a | n/a | n/a | 0.0696 | 0.0998 | 0.0778 | n/a | n/a | 0.698 | 0.895 | n/a | n/a | 0.0227-0.173 | 0.0627-0.12 | 1.4 * | 1.38 * | 2.08 * | 12.8 |

### lua: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 205 | 805 | 428 | 424 | 410 | 461 | 0.255 | 0.479 | 0.484 | 0.5 | 0.444 | 210 | 822 | 430 | 413 | 464 | 0.256 | 0.489 | 0.509 | 0.453 | 205-223 | 428-443 |
| broken | cursor | all | 332 | 1330 | 681 | 662 | 653 | 731 | 0.249 | 0.488 | 0.502 | 0.509 | 0.455 | 344 | 1360 | 713 | 666 | 755 | 0.254 | 0.483 | 0.517 | 0.455 | 332-354 | 681-731 |
| broken | parse | 1 | 177 | 581 | 392 | 390 | 364 | 431 | 0.304 | 0.451 | 0.453 | 0.485 | 0.41 | 179 | 595 | 404 | 367 | 435 | 0.3 | 0.442 | 0.487 | 0.411 | 177-194 | 392-424 |
| broken | parse | all | 280 | 954 | 574 | 596 | 571 | 672 | 0.293 | 0.488 | 0.47 | 0.491 | 0.416 | 294 | 964 | 647 | 577 | 679 | 0.305 | 0.454 | 0.509 | 0.433 | 280-304 | 574-668 |
| broken | walk | 1 | 201 | 793 | 412 | n/a | n/a | n/a | 0.253 | 0.488 | n/a | n/a | n/a | 210 | 807 | 421 | n/a | n/a | 0.26 | 0.499 | n/a | n/a | 201-230 | 412-449 |
| broken | walk | all | 322 | 1290 | 654 | n/a | n/a | n/a | 0.249 | 0.492 | n/a | n/a | n/a | 337 | 1300 | 696 | n/a | n/a | 0.259 | 0.485 | n/a | n/a | 322-345 | 654-707 |
| real | cursor | 1 | 354 | 1740 | 748 | 768 | 778 | 816 | 0.203 | 0.473 | 0.46 | 0.454 | 0.434 | 367 | 1760 | 757 | 803 | 824 | 0.208 | 0.484 | 0.457 | 0.445 | 354-379 | 748-785 |
| real | cursor | all | 686 | 3160 | 1300 | 1300 | 1320 | 1430 | 0.217 | 0.527 | 0.529 | 0.521 | 0.479 | 710 | 3200 | 1350 | 1320 | 1450 | 0.222 | 0.527 | 0.536 | 0.49 | 686-717 | 1300-1360 |
| real | parse | 1 | 282 | 1060 | 636 | 655 | 586 | 718 | 0.266 | 0.443 | 0.431 | 0.481 | 0.393 | 288 | 1060 | 647 | 590 | 723 | 0.271 | 0.445 | 0.488 | 0.398 | 282-298 | 636-705 |
| real | parse | all | 527 | 1810 | 1140 | 1100 | 1010 | 1140 | 0.292 | 0.464 | 0.481 | 0.521 | 0.464 | 547 | 1860 | 1150 | 1040 | 1200 | 0.295 | 0.474 | 0.528 | 0.456 | 527-556 | 1140-1160 |
| real | walk | 1 | 340 | 1620 | 698 | n/a | n/a | n/a | 0.211 | 0.488 | n/a | n/a | n/a | 347 | 1650 | 712 | n/a | n/a | 0.211 | 0.488 | n/a | n/a | 340-355 | 698-799 |
| real | walk | all | 620 | 2830 | 1240 | n/a | n/a | n/a | 0.219 | 0.498 | n/a | n/a | n/a | 675 | 2900 | 1260 | n/a | n/a | 0.233 | 0.535 | n/a | n/a | 620-683 | 1240-1270 |
| realerr (too small) | cursor | 1 | 0.018 | 0.062 | 0.035 | 0.036 | 0.028 | 0.04 | 0.29 | 0.514 | 0.5 | 0.643 | 0.45 | 0.025 | 0.067 | 0.039 | 0.029 | 0.041 | 0.373 | 0.641 | 0.862 | 0.61 | 0.018-0.073 | 0.035-0.051 |
| realerr (too small) | cursor | all | 0.049 | 0.081 | 0.121 | 0.145 | 0.148 | 0.155 | 0.605 | 0.405 | 0.338 | 0.331 | 0.316 | 0.106 | 0.137 | 0.159 | 0.169 | 0.174 | 0.774 | 0.667 | 0.627 | 0.609 | 0.049-0.261 | 0.121-0.178 |
| realerr (too small) | parse | 1 | 0.02 | 0.053 | 0.034 | 0.034 | 0.026 | 0.051 | 0.377 | 0.588 | 0.588 | 0.769 | 0.392 | 0.023 | 0.06 | 0.037 | 0.028 | 0.06 | 0.383 | 0.622 | 0.821 | 0.383 | 0.02-0.146 | 0.034-0.048 |
| realerr (too small) | parse | all | 0.03 | 0.068 | 0.116 | 0.117 | 0.114 | 0.14 | 0.441 | 0.259 | 0.256 | 0.263 | 0.214 | 0.098 | 0.114 | 0.158 | 0.158 | 0.165 | 0.86 | 0.62 | 0.62 | 0.594 | 0.03-0.211 | 0.116-0.206 |
| realerr (too small) | walk | 1 | 0.02 | 0.061 | 0.034 | n/a | n/a | n/a | 0.328 | 0.588 | n/a | n/a | n/a | 0.024 | 0.072 | 0.037 | n/a | n/a | 0.333 | 0.649 | n/a | n/a | 0.02-0.052 | 0.034-0.049 |
| realerr (too small) | walk | all | 0.047 | 0.082 | 0.145 | n/a | n/a | n/a | 0.573 | 0.324 | n/a | n/a | n/a | 0.095 | 0.13 | 0.169 | n/a | n/a | 0.731 | 0.562 | n/a | n/a | 0.047-0.213 | 0.145-0.203 |

### lua: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 4900 | 15000 | 8400 | 8220 | 8260 | 8620 | 0.326 | 0.583 | 0.596 | 0.593 | 0.568 | 4900 | 15100 | 8410 | 8260 | 8630 | 0.325 | 0.583 | 0.593 | 0.568 | 4900-4900 | 8400-8410 |
| broken | cursor | all | 4900 | 15200 | 8410 | 8230 | 8270 | 8630 | 0.323 | 0.582 | 0.595 | 0.592 | 0.568 | 4910 | 15200 | 8420 | 8270 | 8640 | 0.323 | 0.583 | 0.594 | 0.568 | 4900-4970 | 8410-8420 |
| broken | parse | 1 | 4290 | 10400 | 7730 | 7530 | 7510 | 7910 | 0.412 | 0.555 | 0.57 | 0.571 | 0.542 | 4290 | 10400 | 7730 | 7510 | 7910 | 0.412 | 0.555 | 0.571 | 0.542 | 4290-4290 | 7730-7730 |
| broken | parse | all | 4290 | 10400 | 7740 | 7530 | 7520 | 7920 | 0.411 | 0.555 | 0.57 | 0.571 | 0.542 | 4290 | 10500 | 7740 | 7520 | 7920 | 0.411 | 0.555 | 0.571 | 0.542 | 4290-4340 | 7740-7740 |
| broken | walk | 1 | 4890 | 14000 | 8100 | n/a | n/a | n/a | 0.35 | 0.604 | n/a | n/a | n/a | 4900 | 14000 | 8110 | n/a | n/a | 0.35 | 0.605 | n/a | n/a | 4890-4910 | 8100-8110 |
| broken | walk | all | 4920 | 14200 | 8110 | n/a | n/a | n/a | 0.346 | 0.607 | n/a | n/a | n/a | 4930 | 14300 | 8120 | n/a | n/a | 0.345 | 0.607 | n/a | n/a | 4920-4940 | 8110-8120 |
| real | cursor | 1 | 8670 | 31400 | 13200 | 13000 | 13100 | 13800 | 0.276 | 0.655 | 0.667 | 0.663 | 0.628 | 8670 | 31400 | 13200 | 13100 | 13800 | 0.276 | 0.654 | 0.662 | 0.627 | 8670-8670 | 13200-13300 |
| real | cursor | all | 8670 | 31400 | 13300 | 13000 | 13100 | 13900 | 0.276 | 0.653 | 0.665 | 0.66 | 0.625 | 8670 | 31500 | 13300 | 13100 | 13900 | 0.275 | 0.652 | 0.66 | 0.625 | 8670-8700 | 13300-13300 |
| real | parse | 1 | 6700 | 17300 | 11000 | 10700 | 10600 | 11500 | 0.388 | 0.609 | 0.626 | 0.63 | 0.584 | 6700 | 17300 | 11000 | 10700 | 11500 | 0.388 | 0.608 | 0.629 | 0.583 | 6700-6700 | 11000-11000 |
| real | parse | all | 6700 | 17200 | 11000 | 10800 | 10700 | 11500 | 0.39 | 0.607 | 0.624 | 0.628 | 0.583 | 6700 | 17300 | 11100 | 10700 | 11500 | 0.388 | 0.606 | 0.627 | 0.582 | 6700-6710 | 11000-11100 |
| real | walk | 1 | 8290 | 27500 | 12100 | n/a | n/a | n/a | 0.302 | 0.683 | n/a | n/a | n/a | 8290 | 27500 | 12200 | n/a | n/a | 0.302 | 0.682 | n/a | n/a | 8290-8290 | 12100-12200 |
| real | walk | all | 8300 | 27500 | 12200 | n/a | n/a | n/a | 0.301 | 0.682 | n/a | n/a | n/a | 8300 | 27600 | 12200 | n/a | n/a | 0.301 | 0.68 | n/a | n/a | 8300-8300 | 12200-12200 |
| realerr (too small) | cursor | 1 | 0.245 | 0.94 | 0.621 | 0.607 | 0.586 | 0.633 | 0.261 | 0.395 | 0.404 | 0.418 | 0.387 | 0.247 | 0.942 | 0.621 | 0.586 | 0.633 | 0.262 | 0.398 | 0.422 | 0.39 | 0.245-0.267 | 0.621-0.622 |
| realerr (too small) | cursor | all | 0.255 | 0.983 | 0.628 | 0.614 | 0.593 | 0.649 | 0.259 | 0.406 | 0.415 | 0.43 | 0.393 | 0.488 | 1.02 | 0.637 | 0.603 | 0.649 | 0.477 | 0.766 | 0.81 | 0.752 | 0.255-2.15 | 0.628-0.638 |
| realerr (too small) | parse | 1 | 0.23 | 0.814 | 0.606 | 0.592 | 0.57 | 0.617 | 0.282 | 0.379 | 0.388 | 0.403 | 0.372 | 0.232 | 0.816 | 0.606 | 0.57 | 0.617 | 0.284 | 0.383 | 0.407 | 0.376 | 0.23-0.258 | 0.606-0.607 |
| realerr (too small) | parse | all | 0.238 | 0.852 | 0.613 | 0.599 | 0.577 | 0.633 | 0.28 | 0.389 | 0.398 | 0.413 | 0.376 | 0.473 | 0.896 | 0.622 | 0.586 | 0.633 | 0.528 | 0.759 | 0.806 | 0.746 | 0.238-2.1 | 0.613-0.623 |
| realerr (too small) | walk | 1 | 0.24 | 0.896 | 0.613 | n/a | n/a | n/a | 0.268 | 0.391 | n/a | n/a | n/a | 0.243 | 0.899 | 0.613 | n/a | n/a | 0.27 | 0.396 | n/a | n/a | 0.24-0.267 | 0.613-0.614 |
| realerr (too small) | walk | all | 0.253 | 0.938 | 0.62 | n/a | n/a | n/a | 0.27 | 0.408 | n/a | n/a | n/a | 0.488 | 0.982 | 0.629 | n/a | n/a | 0.497 | 0.776 | n/a | n/a | 0.253-2.13 | 0.62-0.63 |

### lua: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1070 | 4130 | 2220 | 2200 | 2130 | 2390 | 0.259 | 0.481 | 0.485 | 0.502 | 0.447 | 1090 | 4230 | 2230 | 2140 | 2410 | 0.257 | 0.487 | 0.507 | 0.45 | 1070-1160 | 2220-2300 |
| broken | cursor | all | 1540 | 6080 | 3180 | 3090 | 3030 | 3420 | 0.253 | 0.484 | 0.498 | 0.507 | 0.449 | 1610 | 6150 | 3320 | 3070 | 3540 | 0.262 | 0.484 | 0.523 | 0.455 | 1540-1640 | 3180-3370 |
| broken | parse | 1 | 922 | 3000 | 2040 | 2030 | 1890 | 2240 | 0.307 | 0.452 | 0.455 | 0.488 | 0.411 | 932 | 3070 | 2100 | 1900 | 2260 | 0.303 | 0.445 | 0.49 | 0.413 | 922-1010 | 2040-2200 |
| broken | parse | all | 1320 | 4390 | 2740 | 2840 | 2650 | 3160 | 0.3 | 0.481 | 0.464 | 0.498 | 0.418 | 1390 | 4410 | 3050 | 2690 | 3180 | 0.314 | 0.456 | 0.516 | 0.437 | 1320-1430 | 2740-3130 |
| broken | walk | 1 | 1050 | 4060 | 2140 | n/a | n/a | n/a | 0.258 | 0.489 | n/a | n/a | n/a | 1090 | 4130 | 2190 | n/a | n/a | 0.264 | 0.498 | n/a | n/a | 1050-1200 | 2140-2330 |
| broken | walk | all | 1530 | 5860 | 3080 | n/a | n/a | n/a | 0.26 | 0.495 | n/a | n/a | n/a | 1590 | 5910 | 3230 | n/a | n/a | 0.269 | 0.491 | n/a | n/a | 1530-1630 | 3080-3290 |
| real | cursor | 1 | 1840 | 9030 | 3900 | 4010 | 4060 | 4250 | 0.204 | 0.473 | 0.46 | 0.454 | 0.434 | 1910 | 9140 | 3950 | 4190 | 4300 | 0.209 | 0.484 | 0.456 | 0.445 | 1840-1970 | 3900-4090 |
| real | cursor | all | 3040 | 14000 | 5860 | 5810 | 5920 | 6430 | 0.217 | 0.519 | 0.524 | 0.514 | 0.473 | 3120 | 14100 | 6030 | 5940 | 6470 | 0.221 | 0.517 | 0.524 | 0.482 | 3040-3140 | 5860-6060 |
| real | parse | 1 | 1470 | 5500 | 3320 | 3420 | 3060 | 3740 | 0.268 | 0.443 | 0.431 | 0.481 | 0.393 | 1500 | 5530 | 3380 | 3080 | 3770 | 0.272 | 0.444 | 0.488 | 0.399 | 1470-1560 | 3320-3680 |
| real | parse | all | 2340 | 8120 | 5100 | 4980 | 4580 | 5180 | 0.289 | 0.46 | 0.47 | 0.512 | 0.453 | 2430 | 8260 | 5200 | 4660 | 5430 | 0.294 | 0.467 | 0.521 | 0.447 | 2340-2460 | 5100-5220 |
| real | walk | 1 | 1780 | 8390 | 3640 | n/a | n/a | n/a | 0.212 | 0.487 | n/a | n/a | n/a | 1810 | 8540 | 3720 | n/a | n/a | 0.212 | 0.487 | n/a | n/a | 1780-1850 | 3640-4160 |
| real | walk | all | 2780 | 12600 | 5610 | n/a | n/a | n/a | 0.221 | 0.496 | n/a | n/a | n/a | 2970 | 12800 | 5660 | n/a | n/a | 0.233 | 0.525 | n/a | n/a | 2780-3010 | 5610-5700 |
| realerr (too small) | cursor | 1 | 0.0622 | 0.275 | 0.16 | 0.169 | 0.121 | 0.184 | 0.226 | 0.389 | 0.368 | 0.513 | 0.338 | 0.0792 | 0.289 | 0.178 | 0.127 | 0.19 | 0.274 | 0.444 | 0.623 | 0.416 | 0.0622-0.189 | 0.16-0.225 |
| realerr (too small) | cursor | all | 0.16 | 0.349 | 0.223 | 0.248 | 0.255 | 0.347 | 0.46 | 0.719 | 0.648 | 0.629 | 0.463 | 0.351 | 0.544 | 0.303 | 0.302 | 0.373 | 0.646 | 1.16 | 1.16 | 0.942 | 0.16-0.988 | 0.223-0.362 |
| realerr (too small) | parse | 1 | 0.0617 | 0.229 | 0.155 | 0.157 | 0.114 | 0.24 | 0.27 | 0.399 | 0.393 | 0.541 | 0.257 | 0.0765 | 0.25 | 0.17 | 0.12 | 0.261 | 0.306 | 0.451 | 0.637 | 0.293 | 0.0617-0.239 | 0.155-0.215 |
| realerr (too small) | parse | all | 0.103 | 0.28 | 0.232 | 0.243 | 0.201 | 0.298 | 0.368 | 0.443 | 0.425 | 0.514 | 0.346 | 0.345 | 0.42 | 0.329 | 0.284 | 0.36 | 0.822 | 1.05 | 1.22 | 0.959 | 0.103-0.798 | 0.232-0.439 |
| realerr (too small) | walk | 1 | 0.0643 | 0.271 | 0.156 | n/a | n/a | n/a | 0.237 | 0.412 | n/a | n/a | n/a | 0.0735 | 0.304 | 0.171 | n/a | n/a | 0.242 | 0.429 | n/a | n/a | 0.0643-0.146 | 0.156-0.219 |
| realerr (too small) | walk | all | 0.156 | 0.334 | 0.254 | n/a | n/a | n/a | 0.466 | 0.614 | n/a | n/a | n/a | 0.329 | 0.527 | 0.342 | n/a | n/a | 0.624 | 0.962 | n/a | n/a | 0.156-0.824 | 0.254-0.428 |

### lua: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 610 | 4640 | 675 | 696 | 749 | 714 | 0.131 | 0.903 | 0.876 | 0.813 | 0.854 |
| broken | cursor minus parse | 1 | cycles M | 147 | 1130 | 182 | 174 | 241 | 147 | 0.13 | 0.807 | 0.844 | 0.612 | 1 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 28.4 | 223 | 35.8 | 33.5 | 46.3 | 30 | 0.127 | 0.792 | 0.846 | 0.613 | 0.947 |
| broken | cursor minus parse | all | instructions M | 608 | 4720 | 678 | 704 | 754 | 707 | 0.129 | 0.897 | 0.864 | 0.805 | 0.86 |
| broken | cursor minus parse | all | cycles M | 219 | 1690 | 435 | 248 | 384 | 266 | 0.13 | 0.502 | 0.883 | 0.57 | 0.822 |
| broken | cursor minus parse | all | CPU user+sys ms | 52.2 | 379 | 106 | 65.6 | 82.5 | 58.5 | 0.138 | 0.49 | 0.796 | 0.633 | 0.892 |
| broken | walk minus parse | 1 | instructions M | 603 | 3580 | 373 | n/a | n/a | n/a | 0.169 | 1.62 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 124 | 1060 | 99.9 | n/a | n/a | n/a | 0.117 | 1.24 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 24.2 | 212 | 19.9 | n/a | n/a | n/a | 0.114 | 1.21 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 629 | 3780 | 375 | n/a | n/a | n/a | 0.167 | 1.68 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 208 | 1470 | 341 | n/a | n/a | n/a | 0.141 | 0.61 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 42.1 | 340 | 79.9 | n/a | n/a | n/a | 0.124 | 0.526 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1960 | 14100 | 2210 | 2270 | 2430 | 2320 | 0.139 | 0.888 | 0.863 | 0.807 | 0.844 |
| real | cursor minus parse | 1 | cycles M | 373 | 3530 | 578 | 591 | 1000 | 504 | 0.106 | 0.644 | 0.63 | 0.372 | 0.739 |
| real | cursor minus parse | 1 | CPU user+sys ms | 71.4 | 682 | 111 | 114 | 192 | 98 | 0.105 | 0.641 | 0.629 | 0.372 | 0.729 |
| real | cursor minus parse | all | instructions M | 1960 | 14200 | 2240 | 2290 | 2460 | 2360 | 0.138 | 0.877 | 0.857 | 0.796 | 0.83 |
| real | cursor minus parse | all | cycles M | 700 | 5890 | 762 | 830 | 1340 | 1260 | 0.119 | 0.918 | 0.843 | 0.522 | 0.557 |
| real | cursor minus parse | all | CPU user+sys ms | 159 | 1350 | 167 | 201 | 305 | 299 | 0.118 | 0.955 | 0.791 | 0.522 | 0.533 |
| real | walk minus parse | 1 | instructions M | 1590 | 10200 | 1130 | n/a | n/a | n/a | 0.156 | 1.41 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 303 | 2890 | 321 | n/a | n/a | n/a | 0.105 | 0.945 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 58.1 | 557 | 61.6 | n/a | n/a | n/a | 0.104 | 0.943 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1590 | 10300 | 1130 | n/a | n/a | n/a | 0.154 | 1.41 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 440 | 4480 | 512 | n/a | n/a | n/a | 0.0982 | 0.859 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 93.1 | 1020 | 109 | n/a | n/a | n/a | 0.0912 | 0.853 | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | 1 | instructions M | 0.0154 | 0.126 | 0.0148 | 0.0152 | 0.0164 | 0.0158 | 0.123 | 1.04 | 1.01 | 0.943 | 0.978 |
| realerr (too small) | cursor minus parse | 1 | cycles M | 0.00045 | 0.046 | 0.00493 | 0.0118 | 0.00718 | n/a | 0.00978 | 0.0913 | 0.0383 | 0.0626 | n/a |
| realerr (too small) | cursor minus parse | 1 | CPU user+sys ms | n/a | 0.009 | 0.001 | 0.002 | 0.002 | n/a | n/a | n/a | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | all | instructions M | 0.0166 | 0.131 | 0.0148 | 0.0155 | 0.0163 | 0.0156 | 0.127 | 1.12 | 1.07 | 1.01 | 1.06 |
| realerr (too small) | cursor minus parse | all | cycles M | 0.0575 | 0.0696 | n/a | 0.00497 | 0.0547 | 0.0491 | 0.825 | n/a | 11.6 | 1.05 | 1.17 |
| realerr (too small) | cursor minus parse | all | CPU user+sys ms | 0.019 | 0.013 | 0.005 | 0.028 | 0.034 | 0.015 | 1.46 | 3.8 | 0.679 | 0.559 | 1.27 |
| realerr (too small) | walk minus parse | 1 | instructions M | 0.0102 | 0.0823 | 0.00694 | n/a | n/a | n/a | 0.124 | 1.47 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | cycles M | 0.00259 | 0.0425 | 0.00139 | n/a | n/a | n/a | 0.061 | 1.87 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | CPU user+sys ms | n/a | 0.008 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | instructions M | 0.0149 | 0.0855 | 0.00692 | n/a | n/a | n/a | 0.174 | 2.15 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | cycles M | 0.0528 | 0.0546 | 0.0213 | n/a | n/a | n/a | 0.966 | 2.48 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | CPU user+sys ms | 0.017 | 0.014 | 0.029 | n/a | n/a | n/a | 1.21 | 0.586 | n/a | n/a | n/a |

### lua: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 91 | 154 | 35.6 | 0.589 | 2.55 | 91 | 154 | 35.6 | 0.589 | 2.55 | 83.5 | 146 | 30.9 | 0.573 | 2.71 |
| broken | cursor | all | 144 | 283 | 184 | 0.51 | 0.784 | 144 | 283 | 184 | 0.51 | 0.784 | 137 | 274 | 179 | 0.499 | 0.764 |
| broken | parse | 1 | 45.4 | 36.4 | 28.5 | 1.25 | 1.59 | 45.4 | 36.4 | 28.5 | 1.25 | 1.59 | 38.8 | 27.9 | 23.7 | 1.39 | 1.64 |
| broken | parse | all | 92.5 | 235 | 174 | 0.394 | 0.531 | 92.5 | 235 | 174 | 0.394 | 0.531 | 84.8 | 226 | 169 | 0.375 | 0.501 |
| broken | walk | 1 | 71.7 | 125 | 40.5 | 0.572 | 1.77 | 71.7 | 125 | 40.5 | 0.572 | 1.77 | 65 | 117 | 35.7 | 0.557 | 1.82 |
| broken | walk | all | 120 | 260 | 232 | 0.461 | 0.517 | 120 | 260 | 232 | 0.461 | 0.517 | 112 | 251 | 227 | 0.447 | 0.495 |
| real | cursor | 1 | 40.2 | 62.9 | 20.2 | 0.639 | 1.99 | 40.2 | 62.9 | 20.2 | 0.639 | 1.99 | 23.5 | 44.7 | 7.27 | 0.525 | 3.23 |
| real | cursor | all | 81.7 | 137 | 77.3 | 0.598 | 1.06 | 81.7 | 137 | 77.3 | 0.598 | 1.06 | 64.9 | 119 | 64.2 | 0.547 | 1.01 |
| real | parse | 1 | 23.7 | 35.6 | 19.3 | 0.664 | 1.23 | 23.7 | 35.6 | 19.3 | 0.664 | 1.23 | 6.89 | 17.6 | 6.31 | 0.39 | 1.09 |
| real | parse | all | 60.6 | 111 | 82.8 | 0.544 | 0.732 | 60.6 | 111 | 82.8 | 0.544 | 0.732 | 43.7 | 93.3 | 69.7 | 0.469 | 0.627 |
| real | walk | 1 | 23.7 | 66.5 | 20.4 | 0.357 | 1.16 | 23.7 | 66.5 | 20.4 | 0.357 | 1.16 | 6.95 | 48.4 | 7.15 | 0.144 | 0.972 |
| real | walk | all | 62.3 | 134 | 79.1 | 0.464 | 0.787 | 62.3 | 134 | 79.1 | 0.464 | 0.787 | 45.4 | 116 | 66 | 0.391 | 0.687 |
| realerr | cursor | 1 | 4.35 | 5.64 | 4.06 | 0.771 | 1.07 | 4.36 | 5.64 | 4.06 | 0.772 | 1.07 | 1.19 | 0.926 | 1.59 | 1.28 | 0.749 |
| realerr | cursor | all | 5.94 | 6.23 | 4.05 | 0.952 | 1.46 | 5.96 | 6.24 | 4.05 | 0.956 | 1.47 | 2.66 | 1.49 | 1.53 | 1.78 | 1.74 |
| realerr | parse | 1 | 4.48 | 5.5 | 2.81 | 0.814 | 1.6 | 4.48 | 5.51 | 2.81 | 0.814 | 1.6 | 1.22 | 0.703 | 0.543 | 1.73 | 2.24 |
| realerr | parse | all | 5.81 | 5.86 | 3.33 | 0.991 | 1.74 | 5.84 | 5.86 | 3.33 | 0.995 | 1.75 | 2.55 | 1.17 | 0.77 | 2.18 | 3.31 |
| realerr | walk | 1 | 4.34 | 5.76 | 3.82 | 0.753 | 1.13 | 4.34 | 5.77 | 3.82 | 0.753 | 1.14 | 1.18 | 0.961 | 1.66 | 1.23 | 0.709 |
| realerr | walk | all | 5.92 | 6.39 | 4.05 | 0.927 | 1.46 | 5.93 | 6.39 | 4.05 | 0.927 | 1.46 | 2.65 | 1.53 | 1.66 | 1.73 | 1.59 |

### lua: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 45.3 | 43.2 | 28.6 | 1.05 | 1.58 | 45.3 | 43.2 | 28.6 | 1.05 | 1.58 | 38.4 | 34.7 | 23.9 | 1.11 | 1.61 |
| broken | all | 114 | 512 | 319 | 0.222 | 0.356 | 114 | 512 | 319 | 0.222 | 0.356 | 106 | 503 | 314 | 0.211 | 0.338 |
| real | 1 | 23.8 | 36.3 | 18.9 | 0.654 | 1.26 | 23.8 | 36.3 | 18.9 | 0.654 | 1.26 | 7 | 18.3 | 5.84 | 0.383 | 1.2 |
| real | all | 64.1 | 141 | 84.1 | 0.454 | 0.762 | 64.1 | 141 | 84.1 | 0.454 | 0.762 | 47.3 | 123 | 70.9 | 0.384 | 0.667 |
| realerr | 1 | 4.44 | 5.35 | 3.05 | 0.829 | 1.45 | 4.44 | 5.36 | 3.05 | 0.829 | 1.46 | 1.28 | 0.672 | 0.492 | 1.9 | 2.6 |
| realerr | all | 6.16 | 6.32 | 3.21 | 0.974 | 1.91 | 6.17 | 6.32 | 3.21 | 0.975 | 1.92 | 2.78 | 1.55 | 0.801 | 1.79 | 3.47 |

### lua: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 1.01 (real), 11 (realerr), 1.19 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 1.09 | 4700 | 8 | 2030 | 190000 | 2030 | 190000 | 0.000536 |
| real | steady | 1.01 | 2.56 | 8 | 2030 | 190000 | 2030 | 190000 | 0.000497 |
| realerr | cold | 83 | 174000 | 22 | 100 | 14500 | 100 | 14500 | 0.83 |
| realerr | steady | 11 | 232 | 20 | 64 | 5850 | 64 | 5850 | 0.172 |
| broken | cold | 3.05 | 83200 | 23.3 | 2600 | 297000 | 2600 | 297000 | 0.00117 |
| broken | steady | 1.19 | 42200 | 23.3 | 2600 | 296000 | 2600 | 296000 | 0.000459 |

### lua: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2045 B | init ms | 0.00873 | 0.0122 | 0.00569 | 0.715 | 1.53 | 15 |
| #0 2045 B | parse1 ms | 0.181 | 0.271 | 0.207 | 0.668 | 0.877 | 15 |
| #0 2045 B | init+parse1 ms | 0.192 | 0.284 | 0.213 | 0.678 | 0.905 | 15 |
| #0 2045 B | parse2 ms | 0.165 | 0.269 | 0.16 | 0.612 | 1.03 | 15 |
| #0 2045 B | process user+sys ms | 1.06 | 1.59 | 0.652 | 0.663 | 1.62 | 15 |
| #0 2045 B | exec to exit instr user+kernel M | 9.27 | 12.2 | 6.42 | 0.76 | 1.45 | 15 |
| #0 2045 B | exec to exit instr user M | 6.43 | 8.54 | 5.39 | 0.753 | 1.19 | 15 |
| #0 2045 B | exec to exit instr kernel M | 2.85 | 3.67 | 1.03 | 0.778 | 2.78 | 15 |
| #0 2045 B | exec to exit cycles user+kernel M | 4.91 | 7.59 | 2.9 | 0.646 | 1.69 | 15 |
| #0 2045 B | exec to exit task-clock ms | 1 | 1.51 | 0.56 | 0.662 | 1.79 | 15 |
| #0 2045 B | peak RSS MB | 3.89 | 4.91 | 2.89 | 0.793 | 1.35 | 15 |
| #0 2045 B | peak footprint MB | 3.93 | 4.95 | 2.84 | 0.794 | 1.38 | 15 |
| #1 2857 B | init ms | 0.00819 | 0.011 | 0.00544 | 0.743 | 1.51 | 15 |
| #1 2857 B | parse1 ms | 0.19 | 0.292 | 0.225 | 0.651 | 0.844 | 15 |
| #1 2857 B | init+parse1 ms | 0.199 | 0.302 | 0.231 | 0.657 | 0.861 | 15 |
| #1 2857 B | parse2 ms | 0.175 | 0.287 | 0.173 | 0.61 | 1.01 | 15 |
| #1 2857 B | process user+sys ms | 1.08 | 1.51 | 0.681 | 0.712 | 1.58 | 15 |
| #1 2857 B | exec to exit instr user+kernel M | 9.61 | 12.9 | 6.9 | 0.745 | 1.39 | 15 |
| #1 2857 B | exec to exit instr user M | 6.78 | 9.24 | 5.86 | 0.733 | 1.16 | 15 |
| #1 2857 B | exec to exit instr kernel M | 2.85 | 3.64 | 1.03 | 0.783 | 2.76 | 15 |
| #1 2857 B | exec to exit cycles user+kernel M | 4.96 | 7.17 | 3.05 | 0.692 | 1.63 | 15 |
| #1 2857 B | exec to exit task-clock ms | 1.01 | 1.49 | 0.59 | 0.678 | 1.71 | 15 |
| #1 2857 B | peak RSS MB | 3.9 | 4.93 | 2.78 | 0.79 | 1.4 | 15 |
| #1 2857 B | peak footprint MB | 3.93 | 4.96 | 2.72 | 0.792 | 1.45 | 15 |

### lua: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1415 | 2770626 | 0 | 0 | ok |
| realerr | 1 | 24 | 1 | 0 | ok |
| broken | 501 | 921007 | 459 | 0 | ok |

## php

### php: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 178 | 902 | 418 | 442 | 433 | 455 | 0.198 | 0.426 | 0.404 | 0.412 | 0.392 | 180 | 910 | 436 | 436 | 457 | 0.198 | 0.413 | 0.413 | 0.394 | 178-193 | 418-452 | 1 | 1.01 | 1 | 22.1 |
| broken | cursor | all | 36.2 | 171 | 78.8 | 81.6 | 82.8 | 85.2 | 0.212 | 0.46 | 0.444 | 0.437 | 0.425 | 38 | 178 | 81.2 | 84.9 | 86.1 | 0.213 | 0.468 | 0.447 | 0.441 | 36.2-43.6 | 78.8-87.4 | 8 * | 8.6 * | 8.8 * | 109 |
| broken | parse | 1 | 143 | 627 | 373 | 395 | 349 | 412 | 0.228 | 0.383 | 0.362 | 0.41 | 0.347 | 145 | 631 | 379 | 351 | 417 | 0.23 | 0.383 | 0.414 | 0.348 | 143-156 | 373-419 | 1 | 1.01 | 1 | 27.6 |
| broken | parse | all | 27.5 | 109 | 67 | 68.6 | 64.4 | 75.9 | 0.253 | 0.41 | 0.4 | 0.426 | 0.362 | 29.5 | 115 | 71.4 | 66.6 | 78.3 | 0.257 | 0.413 | 0.443 | 0.377 | 27.5-33.5 | 67-75.2 | 8.29 * | 9.28 * | 9.05 * | 143 |
| broken | walk | 1 | 511 | 2610 | 796 | n/a | n/a | n/a | 0.196 | 0.642 | n/a | n/a | n/a | 519 | 2650 | 832 | n/a | n/a | 0.196 | 0.624 | n/a | n/a | 511-592 | 796-850 | 1 | 1.03 | 1 | 7.71 |
| broken | walk | all | 270 | 1390 | 334 | n/a | n/a | n/a | 0.194 | 0.808 | n/a | n/a | n/a | 279 | 1400 | 349 | n/a | n/a | 0.199 | 0.8 | n/a | n/a | 270-294 | 334-387 | 2.42 * | 2.33 * | 3.29 * | 14.6 |
| real | cursor | 1 | 352 | 1690 | 703 | 737 | 787 | 771 | 0.209 | 0.501 | 0.477 | 0.447 | 0.456 | 360 | 1710 | 709 | 804 | 778 | 0.21 | 0.508 | 0.448 | 0.463 | 352-376 | 703-748 | 1 | 1.01 | 1 | 26.4 |
| real | cursor | all | 54.4 | 250 | 101 | 102 | 106 | 109 | 0.218 | 0.541 | 0.536 | 0.512 | 0.498 | 56.1 | 257 | 105 | 111 | 113 | 0.218 | 0.537 | 0.503 | 0.498 | 54.4-57.7 | 101-116 | 11.9 | 12.1 | 12.1 | 170 |
| real | parse | 1 | 275 | 1080 | 604 | 630 | 595 | 670 | 0.253 | 0.455 | 0.436 | 0.462 | 0.41 | 279 | 1100 | 617 | 596 | 674 | 0.253 | 0.451 | 0.467 | 0.414 | 275-311 | 604-637 | 1 | 1 | 1 | 33.8 |
| real | parse | all | 41.4 | 149 | 88.1 | 86.3 | 82.2 | 96.5 | 0.278 | 0.47 | 0.48 | 0.504 | 0.429 | 42.5 | 153 | 92.2 | 87.2 | 97 | 0.277 | 0.461 | 0.487 | 0.438 | 41.4-43.3 | 88.1-96.3 | 12.1 | 12.4 | 11.9 | 224 |
| real | walk | 1 | 325 | 1570 | 656 | n/a | n/a | n/a | 0.207 | 0.495 | n/a | n/a | n/a | 331 | 1600 | 664 | n/a | n/a | 0.207 | 0.499 | n/a | n/a | 325-346 | 656-686 | 1 | 1.01 | 1 | 28.5 |
| real | walk | all | 49.8 | 232 | 92.1 | n/a | n/a | n/a | 0.214 | 0.541 | n/a | n/a | n/a | 51.3 | 238 | 98.1 | n/a | n/a | 0.216 | 0.523 | n/a | n/a | 49.8-58.5 | 92.1-101 | 11.8 | 11.8 | 12.1 | 186 |
| realerr (too small) | cursor | 1 | 0.00482 | 0.011 | 0.00834 | 0.00762 | 0.0127 | 0.00931 | 0.437 | 0.578 | 0.633 | 0.38 | 0.518 | 0.00671 | 0.012 | 0.00964 | 0.0159 | 0.011 | 0.561 | 0.696 | 0.423 | 0.608 | 0.00482-0.0275 | 0.00834-0.0147 | 2.1 | 1.87 | 1.03 | 5.19 |
| realerr (too small) | cursor | all | 0.021 | 0.0129 | 0.0273 | 0.0333 | 0.023 | 0.0177 | 1.62 | 0.767 | 0.629 | 0.913 | 1.19 | 0.0383 | 0.0485 | 0.044 | 0.037 | 0.039 | 0.789 | 0.871 | 1.03 | 0.982 | 0.021-0.0773 | 0.0273-0.0599 | 1.79 * | 1.76 * | 2.85 * | 1.19 |
| realerr (too small) | parse | 1 | 0.00426 | 0.00878 | 0.0076 | 0.0073 | 0.00838 | 0.00967 | 0.485 | 0.561 | 0.584 | 0.508 | 0.441 | 0.00567 | 0.00996 | 0.00969 | 0.00918 | 0.01 | 0.569 | 0.585 | 0.618 | 0.566 | 0.00426-0.0171 | 0.0076-0.0155 | 2.46 | 1.93 | 1.02 | 5.87 |
| realerr (too small) | parse | all | 0.00955 | 0.0141 | 0.0243 | 0.0189 | 0.018 | 0.0249 | 0.678 | 0.392 | 0.506 | 0.53 | 0.384 | 0.0255 | 0.0309 | 0.041 | 0.0196 | 0.0378 | 0.826 | 0.624 | 1.3 | 0.676 | 0.00955-0.0802 | 0.0243-0.055 | 1.79 * | 1.98 * | 3.24 * | 2.62 |
| realerr (too small) | walk | 1 | 0.00438 | 0.0102 | 0.0083 | n/a | n/a | n/a | 0.43 | 0.528 | n/a | n/a | n/a | 0.0057 | 0.0118 | 0.0105 | n/a | n/a | 0.481 | 0.543 | n/a | n/a | 0.00438-0.017 | 0.0083-0.0353 | 2.52 | 1.82 | 0.984 | 5.71 |
| realerr (too small) | walk | all | 0.0111 | 0.0144 | 0.0252 | n/a | n/a | n/a | 0.767 | 0.439 | n/a | n/a | n/a | 0.043 | 0.0442 | 0.0398 | n/a | n/a | 0.971 | 1.08 | n/a | n/a | 0.0111-0.0713 | 0.0252-0.0498 | 1.91 * | 1.59 * | 3.21 * | 2.26 |

### php: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 179 | 910 | 418 | 442 | 433 | 455 | 0.196 | 0.427 | 0.404 | 0.412 | 0.393 | 180 | 917 | 435 | 436 | 457 | 0.197 | 0.414 | 0.414 | 0.395 | 179-193 | 418-452 |
| broken | cursor | all | 296 | 1510 | 705 | 722 | 675 | 740 | 0.196 | 0.42 | 0.41 | 0.438 | 0.4 | 308 | 1530 | 722 | 693 | 779 | 0.201 | 0.426 | 0.444 | 0.395 | 296-318 | 705-736 |
| broken | parse | 1 | 143 | 630 | 373 | 395 | 349 | 412 | 0.227 | 0.383 | 0.362 | 0.41 | 0.347 | 146 | 635 | 379 | 351 | 417 | 0.229 | 0.384 | 0.415 | 0.349 | 143-156 | 373-419 |
| broken | parse | all | 238 | 1050 | 619 | 649 | 574 | 678 | 0.227 | 0.385 | 0.367 | 0.415 | 0.351 | 245 | 1070 | 646 | 580 | 688 | 0.23 | 0.379 | 0.422 | 0.356 | 238-253 | 619-662 |
| broken | walk | 1 | 511 | 2690 | 796 | n/a | n/a | n/a | 0.19 | 0.643 | n/a | n/a | n/a | 519 | 2730 | 832 | n/a | n/a | 0.191 | 0.624 | n/a | n/a | 511-593 | 796-850 |
| broken | walk | all | 653 | 3300 | 1120 | n/a | n/a | n/a | 0.198 | 0.583 | n/a | n/a | n/a | 675 | 3320 | 1150 | n/a | n/a | 0.203 | 0.588 | n/a | n/a | 653-686 | 1120-1190 |
| real | cursor | 1 | 352 | 1700 | 703 | 737 | 787 | 771 | 0.207 | 0.501 | 0.478 | 0.448 | 0.457 | 360 | 1720 | 709 | 804 | 778 | 0.209 | 0.508 | 0.448 | 0.463 | 352-376 | 703-748 |
| real | cursor | all | 651 | 3070 | 1250 | 1290 | 1260 | 1310 | 0.212 | 0.522 | 0.503 | 0.516 | 0.496 | 672 | 3100 | 1270 | 1300 | 1350 | 0.217 | 0.529 | 0.517 | 0.498 | 651-680 | 1250-1300 |
| real | parse | 1 | 275 | 1090 | 604 | 630 | 595 | 670 | 0.252 | 0.455 | 0.437 | 0.462 | 0.411 | 279 | 1110 | 617 | 596 | 674 | 0.252 | 0.452 | 0.468 | 0.414 | 275-311 | 604-637 |
| real | parse | all | 496 | 1870 | 1050 | 1060 | 1010 | 1120 | 0.265 | 0.472 | 0.467 | 0.493 | 0.443 | 516 | 1910 | 1090 | 1040 | 1160 | 0.271 | 0.472 | 0.496 | 0.444 | 496-524 | 1050-1110 |
| real | walk | 1 | 326 | 1580 | 656 | n/a | n/a | n/a | 0.205 | 0.496 | n/a | n/a | n/a | 332 | 1610 | 664 | n/a | n/a | 0.206 | 0.5 | n/a | n/a | 326-346 | 656-686 |
| real | walk | all | 587 | 2780 | 1170 | n/a | n/a | n/a | 0.211 | 0.501 | n/a | n/a | n/a | 618 | 2820 | 1190 | n/a | n/a | 0.219 | 0.521 | n/a | n/a | 587-624 | 1170-1200 |
| realerr (too small) | cursor | 1 | 0.014 | 0.02 | 0.008 | 0.007 | 0.013 | 0.009 | 0.7 | 1.75 | 2 | 1.08 | 1.56 | 0.018 | 0.023 | 0.009 | 0.017 | 0.01 | 0.783 | 2 | 1.06 | 1.8 | 0.014-0.039 | 0.008-0.017 |
| realerr (too small) | cursor | all | 0.032 | 0.018 | 0.108 | 0.109 | 0.093 | 0.089 | 1.78 | 0.296 | 0.294 | 0.344 | 0.36 | 0.071 | 0.084 | 0.125 | 0.121 | 0.124 | 0.845 | 0.568 | 0.587 | 0.573 | 0.032-0.131 | 0.108-0.149 |
| realerr (too small) | parse | 1 | 0.01 | 0.016 | 0.008 | 0.008 | 0.007 | 0.009 | 0.625 | 1.25 | 1.25 | 1.43 | 1.11 | 0.015 | 0.02 | 0.009 | 0.009 | 0.009 | 0.75 | 1.67 | 1.67 | 1.67 | 0.01-0.032 | 0.008-0.016 |
| realerr (too small) | parse | all | 0.016 | 0.018 | 0.094 | 0.094 | 0.09 | 0.111 | 0.889 | 0.17 | 0.17 | 0.178 | 0.144 | 0.05 | 0.053 | 0.124 | 0.11 | 0.119 | 0.943 | 0.403 | 0.455 | 0.42 | 0.016-0.12 | 0.094-0.16 |
| realerr (too small) | walk | 1 | 0.01 | 0.019 | 0.008 | n/a | n/a | n/a | 0.526 | 1.25 | n/a | n/a | n/a | 0.016 | 0.022 | 0.01 | n/a | n/a | 0.727 | 1.6 | n/a | n/a | 0.01-0.041 | 0.008-0.034 |
| realerr (too small) | walk | all | 0.018 | 0.018 | 0.098 | n/a | n/a | n/a | 1 | 0.184 | n/a | n/a | n/a | 0.078 | 0.071 | 0.12 | n/a | n/a | 1.1 | 0.65 | n/a | n/a | 0.018-0.133 | 0.098-0.144 |

### php: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 4120 | 16400 | 7800 | 7670 | 7550 | 8060 | 0.251 | 0.528 | 0.537 | 0.546 | 0.511 | 4120 | 16400 | 7800 | 7550 | 8060 | 0.251 | 0.528 | 0.546 | 0.511 | 4120-4120 | 7800-7810 |
| broken | cursor | all | 4140 | 16400 | 7850 | 7730 | 7610 | 8120 | 0.252 | 0.527 | 0.535 | 0.543 | 0.51 | 4150 | 16500 | 7880 | 7620 | 8130 | 0.252 | 0.526 | 0.544 | 0.51 | 4140-4180 | 7850-7900 |
| broken | parse | 1 | 3230 | 10700 | 6810 | 6650 | 6460 | 7020 | 0.301 | 0.474 | 0.485 | 0.5 | 0.46 | 3230 | 10700 | 6810 | 6460 | 7020 | 0.301 | 0.473 | 0.499 | 0.459 | 3230-3230 | 6810-6820 |
| broken | parse | all | 3240 | 10700 | 6880 | 6720 | 6540 | 7090 | 0.304 | 0.471 | 0.482 | 0.496 | 0.457 | 3250 | 10700 | 6890 | 6540 | 7090 | 0.304 | 0.472 | 0.497 | 0.459 | 3240-3360 | 6880-6900 |
| broken | walk | 1 | 13000 | 36800 | 18800 | n/a | n/a | n/a | 0.354 | 0.692 | n/a | n/a | n/a | 13000 | 36900 | 18800 | n/a | n/a | 0.353 | 0.692 | n/a | n/a | 13000-13000 | 18800-18800 |
| broken | walk | all | 13000 | 36900 | 18900 | n/a | n/a | n/a | 0.354 | 0.69 | n/a | n/a | n/a | 13100 | 37000 | 18900 | n/a | n/a | 0.353 | 0.691 | n/a | n/a | 13000-13200 | 18900-18900 |
| real | cursor | 1 | 8290 | 30900 | 12700 | 12500 | 12800 | 13200 | 0.268 | 0.654 | 0.661 | 0.65 | 0.627 | 8290 | 30900 | 12700 | 12800 | 13300 | 0.268 | 0.652 | 0.648 | 0.625 | 8290-8300 | 12700-12700 |
| real | cursor | all | 8290 | 30900 | 12600 | 12600 | 12800 | 13300 | 0.268 | 0.656 | 0.66 | 0.648 | 0.624 | 8290 | 31000 | 12700 | 12800 | 13300 | 0.268 | 0.65 | 0.648 | 0.623 | 8290-8380 | 12600-12800 |
| real | parse | 1 | 6300 | 18100 | 10400 | 10200 | 10300 | 10900 | 0.349 | 0.604 | 0.615 | 0.612 | 0.579 | 6300 | 18100 | 10400 | 10300 | 10900 | 0.349 | 0.603 | 0.611 | 0.579 | 6300-6300 | 10400-10500 |
| real | parse | all | 6300 | 17900 | 10500 | 10200 | 10300 | 10900 | 0.351 | 0.602 | 0.616 | 0.61 | 0.578 | 6300 | 18100 | 10500 | 10400 | 11000 | 0.349 | 0.6 | 0.608 | 0.575 | 6300-6300 | 10500-10600 |
| real | walk | 1 | 7590 | 27100 | 11400 | n/a | n/a | n/a | 0.28 | 0.664 | n/a | n/a | n/a | 7590 | 27100 | 11400 | n/a | n/a | 0.28 | 0.663 | n/a | n/a | 7590-7600 | 11400-11500 |
| real | walk | all | 7590 | 27200 | 11400 | n/a | n/a | n/a | 0.28 | 0.664 | n/a | n/a | n/a | 7590 | 27200 | 11500 | n/a | n/a | 0.279 | 0.66 | n/a | n/a | 7590-7590 | 11400-11500 |
| realerr (too small) | cursor | 1 | 0.0865 | 0.185 | 0.0727 | 0.0691 | 0.0649 | 0.0748 | 0.467 | 1.19 | 1.25 | 1.33 | 1.16 | 0.0877 | 0.188 | 0.0728 | 0.065 | 0.0749 | 0.467 | 1.2 | 1.35 | 1.17 | 0.0865-0.0997 | 0.0727-0.0736 |
| realerr (too small) | cursor | all | 0.145 | 0.207 | 0.08 | 0.0785 | 0.0743 | 0.0822 | 0.702 | 1.81 | 1.85 | 1.95 | 1.77 | 0.194 | 0.248 | 0.0823 | 0.0747 | 0.0846 | 0.784 | 2.36 | 2.6 | 2.3 | 0.145-0.266 | 0.08-0.0829 |
| realerr (too small) | parse | 1 | 0.081 | 0.145 | 0.068 | 0.0643 | 0.0597 | 0.0698 | 0.558 | 1.19 | 1.26 | 1.36 | 1.16 | 0.0824 | 0.148 | 0.0681 | 0.0598 | 0.0699 | 0.556 | 1.21 | 1.38 | 1.18 | 0.081-0.089 | 0.068-0.0689 |
| realerr (too small) | parse | all | 0.117 | 0.169 | 0.0753 | 0.0714 | 0.0668 | 0.0792 | 0.69 | 1.55 | 1.63 | 1.74 | 1.47 | 0.198 | 0.209 | 0.0774 | 0.067 | 0.0792 | 0.945 | 2.55 | 2.95 | 2.49 | 0.117-0.23 | 0.0753-0.0782 |
| realerr (too small) | walk | 1 | 0.0836 | 0.17 | 0.0701 | n/a | n/a | n/a | 0.49 | 1.19 | n/a | n/a | n/a | 0.0853 | 0.172 | 0.0702 | n/a | n/a | 0.495 | 1.22 | n/a | n/a | 0.0836-0.0929 | 0.0701-0.071 |
| realerr (too small) | walk | all | 0.116 | 0.193 | 0.0792 | n/a | n/a | n/a | 0.603 | 1.47 | n/a | n/a | n/a | 0.195 | 0.234 | 0.0795 | n/a | n/a | 0.833 | 2.46 | n/a | n/a | 0.116-0.261 | 0.0792-0.0803 |

### php: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 928 | 4670 | 2170 | 2300 | 2250 | 2360 | 0.199 | 0.427 | 0.404 | 0.412 | 0.393 | 937 | 4740 | 2260 | 2270 | 2380 | 0.198 | 0.414 | 0.413 | 0.394 | 928-1000 | 2170-2350 |
| broken | cursor | all | 1380 | 6850 | 3240 | 3360 | 3160 | 3460 | 0.202 | 0.427 | 0.411 | 0.438 | 0.4 | 1440 | 6930 | 3320 | 3190 | 3590 | 0.207 | 0.433 | 0.45 | 0.4 | 1380-1470 | 3240-3370 |
| broken | parse | 1 | 746 | 3270 | 1940 | 2050 | 1810 | 2140 | 0.228 | 0.384 | 0.363 | 0.412 | 0.348 | 755 | 3290 | 1970 | 1820 | 2170 | 0.23 | 0.384 | 0.415 | 0.349 | 746-815 | 1940-2180 |
| broken | parse | all | 1120 | 4790 | 2870 | 3030 | 2630 | 3150 | 0.235 | 0.391 | 0.371 | 0.427 | 0.357 | 1160 | 4860 | 2970 | 2680 | 3170 | 0.239 | 0.391 | 0.433 | 0.366 | 1120-1200 | 2870-3030 |
| broken | walk | 1 | 2670 | 13700 | 4150 | n/a | n/a | n/a | 0.195 | 0.643 | n/a | n/a | n/a | 2710 | 13900 | 4340 | n/a | n/a | 0.196 | 0.625 | n/a | n/a | 2670-3090 | 4150-4430 |
| broken | walk | all | 3230 | 15800 | 5370 | n/a | n/a | n/a | 0.204 | 0.602 | n/a | n/a | n/a | 3310 | 15900 | 5490 | n/a | n/a | 0.208 | 0.603 | n/a | n/a | 3230-3370 | 5370-5710 |
| real | cursor | 1 | 1830 | 8800 | 3670 | 3850 | 4110 | 4020 | 0.208 | 0.5 | 0.477 | 0.447 | 0.456 | 1880 | 8920 | 3690 | 4190 | 4060 | 0.21 | 0.508 | 0.448 | 0.462 | 1830-1960 | 3670-3890 |
| real | cursor | all | 2920 | 13600 | 5610 | 5830 | 5750 | 5950 | 0.215 | 0.521 | 0.501 | 0.508 | 0.49 | 3010 | 13700 | 5720 | 5890 | 6100 | 0.22 | 0.527 | 0.512 | 0.495 | 2920-3050 | 5610-5790 |
| real | parse | 1 | 1430 | 5650 | 3150 | 3280 | 3100 | 3490 | 0.254 | 0.456 | 0.438 | 0.463 | 0.411 | 1450 | 5730 | 3210 | 3100 | 3510 | 0.254 | 0.453 | 0.468 | 0.414 | 1430-1620 | 3150-3310 |
| real | parse | all | 2250 | 8380 | 4780 | 4850 | 4590 | 5110 | 0.269 | 0.471 | 0.465 | 0.491 | 0.441 | 2340 | 8480 | 4950 | 4710 | 5250 | 0.275 | 0.472 | 0.496 | 0.445 | 2250-2360 | 4780-4980 |
| real | walk | 1 | 1700 | 8210 | 3420 | n/a | n/a | n/a | 0.207 | 0.496 | n/a | n/a | n/a | 1730 | 8320 | 3450 | n/a | n/a | 0.208 | 0.501 | n/a | n/a | 1700-1800 | 3420-3570 |
| real | walk | all | 2650 | 12300 | 5310 | n/a | n/a | n/a | 0.216 | 0.5 | n/a | n/a | n/a | 2790 | 12400 | 5360 | n/a | n/a | 0.224 | 0.52 | n/a | n/a | 2650-2820 | 5310-5410 |
| realerr (too small) | cursor | 1 | 0.0303 | 0.0613 | 0.023 | 0.0211 | 0.0454 | 0.0252 | 0.494 | 1.32 | 1.44 | 0.667 | 1.2 | 0.0424 | 0.0674 | 0.0257 | 0.0484 | 0.0283 | 0.629 | 1.65 | 0.875 | 1.5 | 0.0303-0.108 | 0.023-0.0516 |
| realerr (too small) | cursor | all | 0.0962 | 0.075 | 0.0898 | 0.079 | 0.0788 | 0.0767 | 1.28 | 1.07 | 1.22 | 1.22 | 1.25 | 0.215 | 0.269 | 0.146 | 0.144 | 0.164 | 0.799 | 1.47 | 1.49 | 1.31 | 0.0962-0.375 | 0.0898-0.167 |
| realerr (too small) | parse | 1 | 0.0242 | 0.05 | 0.0212 | 0.0193 | 0.017 | 0.0232 | 0.484 | 1.14 | 1.25 | 1.43 | 1.04 | 0.0356 | 0.0562 | 0.0242 | 0.0203 | 0.0246 | 0.633 | 1.47 | 1.75 | 1.45 | 0.0242-0.0725 | 0.0212-0.0467 |
| realerr (too small) | parse | all | 0.0534 | 0.0768 | 0.0647 | 0.073 | 0.0591 | 0.0878 | 0.695 | 0.825 | 0.731 | 0.903 | 0.608 | 0.141 | 0.161 | 0.135 | 0.0741 | 0.139 | 0.878 | 1.05 | 1.9 | 1.01 | 0.0534-0.324 | 0.0647-0.16 |
| realerr (too small) | walk | 1 | 0.0251 | 0.0569 | 0.0222 | n/a | n/a | n/a | 0.441 | 1.13 | n/a | n/a | n/a | 0.0346 | 0.0617 | 0.0275 | n/a | n/a | 0.561 | 1.26 | n/a | n/a | 0.0251-0.0713 | 0.0222-0.104 |
| realerr (too small) | walk | all | 0.0547 | 0.0771 | 0.0736 | n/a | n/a | n/a | 0.709 | 0.742 | n/a | n/a | n/a | 0.213 | 0.241 | 0.127 | n/a | n/a | 0.885 | 1.67 | n/a | n/a | 0.0547-0.363 | 0.0736-0.162 |

### php: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 894 | 5700 | 994 | 1020 | 1090 | 1040 | 0.157 | 0.9 | 0.875 | 0.819 | 0.861 |
| broken | cursor minus parse | 1 | cycles M | 182 | 1410 | 233 | 243 | 439 | 220 | 0.13 | 0.781 | 0.751 | 0.415 | 0.83 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 35.5 | 279 | 45.1 | 46.8 | 84.5 | 42.9 | 0.127 | 0.787 | 0.759 | 0.42 | 0.827 |
| broken | cursor minus parse | all | instructions M | 894 | 5770 | 975 | 1010 | 1080 | 1030 | 0.155 | 0.917 | 0.886 | 0.829 | 0.871 |
| broken | cursor minus parse | all | cycles M | 260 | 2060 | 372 | 333 | 522 | 312 | 0.126 | 0.697 | 0.779 | 0.497 | 0.833 |
| broken | cursor minus parse | all | CPU user+sys ms | 57.8 | 461 | 86.2 | 73.4 | 102 | 61.9 | 0.125 | 0.67 | 0.788 | 0.569 | 0.933 |
| broken | walk minus parse | 1 | instructions M | 9800 | 26100 | 12000 | n/a | n/a | n/a | 0.375 | 0.816 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 1920 | 10400 | 2210 | n/a | n/a | n/a | 0.185 | 0.872 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 368 | 2060 | 423 | n/a | n/a | n/a | 0.179 | 0.872 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 9800 | 26200 | 12000 | n/a | n/a | n/a | 0.374 | 0.816 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 2110 | 11100 | 2500 | n/a | n/a | n/a | 0.191 | 0.844 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 415 | 2250 | 502 | n/a | n/a | n/a | 0.185 | 0.827 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1990 | 12800 | 2240 | 2310 | 2460 | 2340 | 0.155 | 0.888 | 0.863 | 0.809 | 0.849 |
| real | cursor minus parse | 1 | cycles M | 399 | 3150 | 520 | 567 | 1010 | 534 | 0.127 | 0.767 | 0.703 | 0.395 | 0.747 |
| real | cursor minus parse | 1 | CPU user+sys ms | 77.3 | 608 | 98.6 | 107 | 192 | 102 | 0.127 | 0.784 | 0.72 | 0.402 | 0.759 |
| real | cursor minus parse | all | instructions M | 1990 | 12900 | 2170 | 2340 | 2470 | 2370 | 0.154 | 0.918 | 0.85 | 0.807 | 0.84 |
| real | cursor minus parse | all | cycles M | 666 | 5220 | 826 | 983 | 1160 | 841 | 0.128 | 0.807 | 0.678 | 0.572 | 0.793 |
| real | cursor minus parse | all | CPU user+sys ms | 155 | 1200 | 196 | 230 | 255 | 193 | 0.129 | 0.788 | 0.671 | 0.606 | 0.799 |
| real | walk minus parse | 1 | instructions M | 1300 | 9030 | 1010 | n/a | n/a | n/a | 0.143 | 1.29 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 263 | 2560 | 272 | n/a | n/a | n/a | 0.102 | 0.968 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 50.5 | 494 | 51.9 | n/a | n/a | n/a | 0.102 | 0.973 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1300 | 9220 | 974 | n/a | n/a | n/a | 0.141 | 1.33 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 400 | 3920 | 526 | n/a | n/a | n/a | 0.102 | 0.761 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 90.9 | 915 | 122 | n/a | n/a | n/a | 0.0993 | 0.744 | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | 1 | instructions M | 0.00548 | 0.04 | 0.00471 | 0.00485 | 0.00525 | 0.00504 | 0.137 | 1.16 | 1.13 | 1.04 | 1.09 |
| realerr (too small) | cursor minus parse | 1 | cycles M | 0.00607 | 0.0113 | 0.00171 | 0.00174 | 0.0284 | 0.00198 | 0.539 | 3.54 | 3.49 | 0.214 | 3.06 |
| realerr (too small) | cursor minus parse | 1 | CPU user+sys ms | 0.004 | 0.004 | n/a | n/a | 0.006 | n/a | 1 | n/a | n/a | 0.667 | n/a |
| realerr (too small) | cursor minus parse | all | instructions M | 0.0287 | 0.0378 | 0.00467 | 0.00705 | 0.00754 | 0.00303 | 0.759 | 6.13 | 4.06 | 3.8 | 9.45 |
| realerr (too small) | cursor minus parse | all | cycles M | 0.0429 | n/a | 0.0251 | 0.00598 | 0.0197 | n/a | n/a | 1.71 | 7.16 | 2.18 | n/a |
| realerr (too small) | cursor minus parse | all | CPU user+sys ms | 0.016 | n/a | 0.014 | 0.015 | 0.003 | n/a | n/a | 1.14 | 1.07 | 5.33 | n/a |
| realerr (too small) | walk minus parse | 1 | instructions M | 0.00252 | 0.0251 | 0.00208 | n/a | n/a | n/a | 0.1 | 1.22 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | cycles M | 0.000895 | 0.00686 | 0.000929 | n/a | n/a | n/a | 0.13 | 0.963 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | CPU user+sys ms | n/a | 0.003 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | instructions M | n/a | 0.0236 | 0.00391 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | cycles M | 0.00128 | 0.000344 | 0.00895 | n/a | n/a | n/a | 3.74 | 0.144 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | CPU user+sys ms | 0.002 | n/a | 0.004 | n/a | n/a | n/a | n/a | 0.5 | n/a | n/a | n/a |

### php: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 47.6 | 148 | 30.7 | 0.321 | 1.55 | 47.6 | 148 | 30.7 | 0.321 | 1.55 | 38.9 | 138 | 24.6 | 0.282 | 1.58 |
| broken | cursor | all | 130 | 324 | 238 | 0.402 | 0.547 | 130 | 324 | 238 | 0.402 | 0.547 | 121 | 314 | 232 | 0.385 | 0.521 |
| broken | parse | 1 | 40.8 | 39.7 | 29.5 | 1.03 | 1.38 | 40.8 | 39.7 | 29.5 | 1.03 | 1.38 | 32.2 | 29.8 | 23.3 | 1.08 | 1.38 |
| broken | parse | all | 121 | 303 | 262 | 0.399 | 0.461 | 121 | 303 | 262 | 0.399 | 0.461 | 111 | 293 | 255 | 0.381 | 0.436 |
| broken | walk | 1 | 40.5 | 166 | 30.1 | 0.244 | 1.35 | 40.5 | 166 | 30.1 | 0.244 | 1.35 | 31.8 | 156 | 24 | 0.204 | 1.32 |
| broken | walk | all | 125 | 378 | 249 | 0.331 | 0.502 | 125 | 378 | 249 | 0.331 | 0.502 | 116 | 368 | 243 | 0.315 | 0.477 |
| real | cursor | 1 | 66.1 | 111 | 35.2 | 0.593 | 1.88 | 66.1 | 111 | 35.2 | 0.593 | 1.88 | 50.9 | 94.8 | 23.8 | 0.537 | 2.14 |
| real | cursor | all | 122 | 239 | 193 | 0.512 | 0.636 | 122 | 239 | 193 | 0.512 | 0.636 | 107 | 223 | 181 | 0.481 | 0.592 |
| real | parse | 1 | 45.8 | 49.7 | 34.5 | 0.921 | 1.33 | 45.8 | 49.7 | 34.5 | 0.921 | 1.33 | 30.8 | 33.2 | 23.4 | 0.927 | 1.32 |
| real | parse | all | 93.8 | 231 | 199 | 0.406 | 0.471 | 93.8 | 231 | 199 | 0.406 | 0.471 | 78.6 | 215 | 188 | 0.366 | 0.419 |
| real | walk | 1 | 43.7 | 119 | 35.1 | 0.367 | 1.25 | 43.7 | 119 | 35.1 | 0.367 | 1.25 | 28.6 | 103 | 23.9 | 0.279 | 1.2 |
| real | walk | all | 94 | 235 | 210 | 0.401 | 0.447 | 94 | 235 | 210 | 0.401 | 0.447 | 78.7 | 218 | 199 | 0.361 | 0.396 |
| realerr | cursor | 1 | 5.35 | 6.51 | 4 | 0.822 | 1.34 | 5.36 | 6.51 | 4 | 0.822 | 1.34 | 2.08 | 1.66 | 1.46 | 1.25 | 1.42 |
| realerr | cursor | all | 6.41 | 6.94 | 4.3 | 0.923 | 1.49 | 6.44 | 6.95 | 4.3 | 0.927 | 1.5 | 3.15 | 2.25 | 1.79 | 1.4 | 1.76 |
| realerr | parse | 1 | 5.23 | 6.12 | 3.75 | 0.855 | 1.39 | 5.23 | 6.12 | 3.75 | 0.855 | 1.39 | 1.99 | 1.45 | 1.46 | 1.38 | 1.37 |
| realerr | parse | all | 6.39 | 6.76 | 4.14 | 0.946 | 1.54 | 6.42 | 6.76 | 4.14 | 0.95 | 1.55 | 3.13 | 1.96 | 1.59 | 1.6 | 1.97 |
| realerr | walk | 1 | 5.3 | 6.52 | 3.98 | 0.813 | 1.33 | 5.3 | 6.53 | 3.98 | 0.813 | 1.33 | 2.06 | 1.67 | 1.72 | 1.23 | 1.2 |
| realerr | walk | all | 6.25 | 7.02 | 4.17 | 0.89 | 1.5 | 6.28 | 7.03 | 4.17 | 0.894 | 1.51 | 3 | 2.21 | 1.51 | 1.35 | 1.99 |

### php: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 42.1 | 49.4 | 29.3 | 0.853 | 1.44 | 42.1 | 49.4 | 29.3 | 0.853 | 1.44 | 33.1 | 39.6 | 23.2 | 0.836 | 1.43 |
| broken | all | 135 | 486 | 287 | 0.277 | 0.469 | 135 | 486 | 287 | 0.277 | 0.469 | 125 | 476 | 281 | 0.263 | 0.445 |
| real | 1 | 46.4 | 54.4 | 35 | 0.852 | 1.32 | 46.4 | 54.4 | 35 | 0.853 | 1.32 | 31.4 | 38.1 | 23.8 | 0.825 | 1.32 |
| real | all | 103 | 402 | 347 | 0.256 | 0.297 | 103 | 402 | 347 | 0.256 | 0.297 | 87.7 | 386 | 336 | 0.227 | 0.261 |
| realerr | 1 | 5.12 | 6.08 | 3.52 | 0.841 | 1.46 | 5.12 | 6.09 | 3.52 | 0.841 | 1.46 | 1.96 | 1.45 | 1.27 | 1.35 | 1.55 |
| realerr | all | 6.86 | 7.25 | 3.81 | 0.945 | 1.8 | 6.86 | 7.26 | 3.81 | 0.945 | 1.8 | 3.5 | 2.42 | 1.34 | 1.45 | 2.61 |

### php: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 3.42 (real), 10 (realerr), 4.91 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 3.61 | 18900 | 7.01 | 2380 | 217000 | 2380 | 217000 | 0.00152 |
| real | steady | 3.42 | 121 | 7.01 | 2380 | 217000 | 2380 | 217000 | 0.00144 |
| realerr | cold | 41 | 170000 | 17 | 29 | 3140 | 29 | 3140 | 1.41 |
| realerr | steady | 10 | 240 | 16 | 22 | 1690 | 22 | 1690 | 0.455 |
| broken | cold | 8.72 | 67300 | 7.66 | 3890 | 399000 | 3890 | 399000 | 0.00224 |
| broken | steady | 4.91 | 21900 | 7.67 | 3890 | 399000 | 3890 | 399000 | 0.00126 |

### php: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2046 B | init ms | 0.00909 | 0.0122 | 0.00576 | 0.743 | 1.58 | 15 |
| #0 2046 B | parse1 ms | 0.235 | 0.302 | 0.239 | 0.779 | 0.987 | 15 |
| #0 2046 B | init+parse1 ms | 0.246 | 0.314 | 0.244 | 0.783 | 1.01 | 15 |
| #0 2046 B | parse2 ms | 0.0844 | 0.23 | 0.132 | 0.367 | 0.641 | 15 |
| #0 2046 B | process user+sys ms | 1.03 | 1.49 | 0.642 | 0.688 | 1.6 | 15 |
| #0 2046 B | exec to exit instr user+kernel M | 7.02 | 11.3 | 5.5 | 0.622 | 1.28 | 15 |
| #0 2046 B | exec to exit instr user M | 4.2 | 7.56 | 4.37 | 0.555 | 0.959 | 15 |
| #0 2046 B | exec to exit instr kernel M | 2.83 | 3.71 | 1.13 | 0.762 | 2.51 | 15 |
| #0 2046 B | exec to exit cycles user+kernel M | 4.68 | 7.14 | 2.89 | 0.655 | 1.62 | 15 |
| #0 2046 B | exec to exit task-clock ms | 0.95 | 1.45 | 0.56 | 0.655 | 1.7 | 15 |
| #0 2046 B | peak RSS MB | 4.99 | 5.8 | 3.64 | 0.86 | 1.37 | 15 |
| #0 2046 B | peak footprint MB | 5.02 | 5.83 | 3.6 | 0.861 | 1.4 | 15 |
| #1 2155 B | init ms | 0.0093 | 0.0121 | 0.00556 | 0.771 | 1.67 | 15 |
| #1 2155 B | parse1 ms | 0.176 | 0.292 | 0.216 | 0.602 | 0.816 | 15 |
| #1 2155 B | init+parse1 ms | 0.189 | 0.304 | 0.221 | 0.622 | 0.855 | 15 |
| #1 2155 B | parse2 ms | 0.082 | 0.267 | 0.142 | 0.307 | 0.578 | 15 |
| #1 2155 B | process user+sys ms | 0.955 | 1.53 | 0.647 | 0.623 | 1.48 | 15 |
| #1 2155 B | exec to exit instr user+kernel M | 7.45 | 12.7 | 6.35 | 0.586 | 1.17 | 15 |
| #1 2155 B | exec to exit instr user M | 4.69 | 8.91 | 5.24 | 0.527 | 0.896 | 15 |
| #1 2155 B | exec to exit instr kernel M | 2.76 | 3.81 | 1.12 | 0.725 | 2.48 | 15 |
| #1 2155 B | exec to exit cycles user+kernel M | 4.38 | 7.3 | 2.88 | 0.6 | 1.52 | 15 |
| #1 2155 B | exec to exit task-clock ms | 0.9 | 1.45 | 0.55 | 0.621 | 1.64 | 15 |
| #1 2155 B | peak RSS MB | 4.6 | 5.67 | 3.46 | 0.811 | 1.33 | 15 |
| #1 2155 B | peak footprint MB | 4.63 | 5.7 | 3.4 | 0.812 | 1.36 | 15 |

### php: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1499 | 2466862 | 0 | 0 | ok |
| realerr | 1 | 7 | 1 | 0 | ok |
| broken | 497 | 1099098 | 409 | 0 | ok |

## python

### python: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 305 | 1390 | 696 | 726 | 694 | 755 | 0.219 | 0.438 | 0.42 | 0.439 | 0.404 | 312 | 1410 | 703 | 706 | 790 | 0.221 | 0.444 | 0.441 | 0.395 | 305-325 | 696-724 | 1 | 1.01 | 1 | 17.4 |
| broken | cursor | all | 50.6 | 253 | 161 | 162 | 139 | 171 | 0.2 | 0.314 | 0.313 | 0.363 | 0.295 | 53.1 | 261 | 167 | 142 | 174 | 0.203 | 0.319 | 0.375 | 0.305 | 50.6-56.5 | 161-173 | 10.6 * | 9.31 * | 7.05 * | 105 |
| broken | parse | 1 | 256 | 993 | 640 | 646 | 583 | 708 | 0.258 | 0.401 | 0.397 | 0.44 | 0.362 | 261 | 1020 | 645 | 592 | 723 | 0.257 | 0.405 | 0.44 | 0.361 | 256-280 | 640-673 | 1 | 1.01 | 1 | 20.7 |
| broken | parse | all | 45.9 | 214 | 154 | 155 | 130 | 169 | 0.214 | 0.298 | 0.297 | 0.353 | 0.272 | 47.1 | 221 | 162 | 135 | 169 | 0.213 | 0.292 | 0.35 | 0.278 | 45.9-50.4 | 154-175 | 9.86 * | 7.54 * | 6.57 * | 116 |
| broken | walk | 1 | 555 | 2640 | 958 | n/a | n/a | n/a | 0.21 | 0.579 | n/a | n/a | n/a | 564 | 2670 | 1000 | n/a | n/a | 0.211 | 0.564 | n/a | n/a | 555-583 | 958-1090 | 1 | 1.03 | 1 | 9.55 |
| broken | walk | all | 308 | 1530 | 443 | n/a | n/a | n/a | 0.201 | 0.697 | n/a | n/a | n/a | 313 | 1550 | 473 | n/a | n/a | 0.202 | 0.661 | n/a | n/a | 308-360 | 443-535 | 2.55 * | 2.34 * | 3.04 * | 17.2 |
| real | cursor | 1 | 473 | 1970 | 881 | 925 | 959 | 969 | 0.24 | 0.537 | 0.511 | 0.493 | 0.488 | 476 | 2000 | 900 | 969 | 970 | 0.237 | 0.529 | 0.491 | 0.491 | 473-534 | 881-963 | 1 | 1.01 | 1 | 24.9 |
| real | cursor | all | 62.8 | 273 | 111 | 112 | 113 | 121 | 0.23 | 0.566 | 0.561 | 0.554 | 0.521 | 64.2 | 277 | 114 | 115 | 123 | 0.232 | 0.563 | 0.557 | 0.521 | 62.8-67.5 | 111-122 | 13.9 | 13.1 | 13.8 | 188 |
| real | parse | 1 | 390 | 1280 | 770 | 789 | 757 | 850 | 0.303 | 0.506 | 0.494 | 0.514 | 0.458 | 393 | 1300 | 786 | 759 | 851 | 0.302 | 0.5 | 0.517 | 0.461 | 390-406 | 770-826 | 1 | 1 | 1 | 30.3 |
| real | parse | all | 52.2 | 167 | 97.7 | 96.5 | 94.9 | 107 | 0.313 | 0.534 | 0.54 | 0.55 | 0.486 | 54.3 | 169 | 99.4 | 96.6 | 109 | 0.322 | 0.547 | 0.562 | 0.497 | 52.2-58.7 | 97.7-104 | 13.5 | 13.4 | 13.9 | 226 |
| real | walk | 1 | 458 | 1850 | 831 | n/a | n/a | n/a | 0.248 | 0.551 | n/a | n/a | n/a | 462 | 1860 | 843 | n/a | n/a | 0.248 | 0.548 | n/a | n/a | 458-483 | 831-863 | 1 | 1.01 | 1 | 25.8 |
| real | walk | all | 60.8 | 250 | 107 | n/a | n/a | n/a | 0.244 | 0.57 | n/a | n/a | n/a | 62.1 | 254 | 111 | n/a | n/a | 0.244 | 0.56 | n/a | n/a | 60.8-67.4 | 107-114 | 13.7 | 13.1 | 13.6 | 194 |
| realerr (too small) | cursor | 1 | 0.0592 | 0.209 | 0.11 | 0.112 | 0.105 | 0.125 | 0.284 | 0.539 | 0.528 | 0.564 | 0.474 | 0.0649 | 0.22 | 0.122 | 0.118 | 0.13 | 0.295 | 0.533 | 0.551 | 0.499 | 0.0592-0.103 | 0.11-0.149 | 1.17 | 1.07 | 0.992 | 16.6 |
| realerr (too small) | cursor | all | 0.0644 | 0.132 | 0.1 | 0.0896 | 0.0988 | 0.123 | 0.489 | 0.642 | 0.719 | 0.652 | 0.525 | 0.116 | 0.158 | 0.133 | 0.141 | 0.137 | 0.733 | 0.871 | 0.821 | 0.846 | 0.0644-0.162 | 0.1-0.24 | 2.07 * | 2.17 * | 2.57 * | 15.2 |
| realerr (too small) | parse | 1 | 0.0536 | 0.16 | 0.101 | 0.107 | 0.104 | 0.112 | 0.336 | 0.53 | 0.499 | 0.514 | 0.48 | 0.0573 | 0.17 | 0.111 | 0.124 | 0.12 | 0.336 | 0.515 | 0.46 | 0.478 | 0.0536-0.131 | 0.101-0.138 | 1.18 | 1.09 | 0.998 | 18.3 |
| realerr (too small) | parse | all | 0.049 | 0.103 | 0.0937 | 0.0968 | 0.107 | 0.116 | 0.475 | 0.523 | 0.506 | 0.458 | 0.422 | 0.112 | 0.124 | 0.116 | 0.131 | 0.125 | 0.899 | 0.965 | 0.855 | 0.894 | 0.049-0.201 | 0.0937-0.214 | 2.05 * | 2.27 * | 2.61 * | 20 |
| realerr (too small) | walk | 1 | 0.0571 | 0.197 | 0.106 | n/a | n/a | n/a | 0.29 | 0.541 | n/a | n/a | n/a | 0.0611 | 0.209 | 0.119 | n/a | n/a | 0.292 | 0.515 | n/a | n/a | 0.0571-0.0992 | 0.106-0.14 | 1.17 | 1.08 | 0.999 | 17.2 |
| realerr (too small) | walk | all | 0.0535 | 0.12 | 0.0943 | n/a | n/a | n/a | 0.447 | 0.567 | n/a | n/a | n/a | 0.0934 | 0.149 | 0.131 | n/a | n/a | 0.625 | 0.714 | n/a | n/a | 0.0535-0.163 | 0.0943-0.173 | 2.09 * | 2.21 * | 2.51 * | 18.3 |

### python: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 306 | 1400 | 696 | 726 | 694 | 755 | 0.219 | 0.441 | 0.422 | 0.442 | 0.406 | 313 | 1420 | 703 | 706 | 790 | 0.221 | 0.445 | 0.443 | 0.396 | 306-327 | 696-724 |
| broken | cursor | all | 554 | 2390 | 1150 | 1170 | 1120 | 1220 | 0.232 | 0.48 | 0.475 | 0.493 | 0.453 | 561 | 2430 | 1180 | 1140 | 1240 | 0.231 | 0.477 | 0.494 | 0.452 | 554-570 | 1150-1190 |
| broken | parse | 1 | 256 | 998 | 640 | 646 | 583 | 708 | 0.257 | 0.401 | 0.397 | 0.44 | 0.362 | 261 | 1020 | 645 | 592 | 722 | 0.256 | 0.405 | 0.441 | 0.362 | 256-280 | 640-673 |
| broken | parse | all | 455 | 1630 | 1050 | 1030 | 932 | 1130 | 0.279 | 0.432 | 0.442 | 0.488 | 0.404 | 464 | 1670 | 1070 | 955 | 1140 | 0.278 | 0.435 | 0.486 | 0.407 | 455-484 | 1050-1080 |
| broken | walk | 1 | 555 | 2710 | 958 | n/a | n/a | n/a | 0.205 | 0.58 | n/a | n/a | n/a | 565 | 2750 | 1000 | n/a | n/a | 0.205 | 0.564 | n/a | n/a | 555-584 | 958-1090 |
| broken | walk | all | 778 | 3600 | 1390 | n/a | n/a | n/a | 0.216 | 0.558 | n/a | n/a | n/a | 806 | 3640 | 1440 | n/a | n/a | 0.221 | 0.56 | n/a | n/a | 778-850 | 1390-1520 |
| real | cursor | 1 | 474 | 1990 | 881 | 925 | 959 | 969 | 0.238 | 0.537 | 0.512 | 0.494 | 0.489 | 477 | 2020 | 900 | 969 | 970 | 0.236 | 0.53 | 0.492 | 0.491 | 474-534 | 881-963 |
| real | cursor | all | 884 | 3610 | 1550 | 1570 | 1600 | 1650 | 0.245 | 0.571 | 0.564 | 0.554 | 0.534 | 899 | 3640 | 1600 | 1610 | 1700 | 0.247 | 0.564 | 0.559 | 0.53 | 884-912 | 1550-1610 |
| real | parse | 1 | 390 | 1290 | 770 | 789 | 757 | 850 | 0.302 | 0.506 | 0.494 | 0.515 | 0.459 | 393 | 1310 | 786 | 759 | 851 | 0.301 | 0.5 | 0.518 | 0.462 | 390-407 | 770-826 |
| real | parse | all | 707 | 2240 | 1370 | 1380 | 1290 | 1440 | 0.316 | 0.517 | 0.513 | 0.548 | 0.491 | 742 | 2280 | 1400 | 1310 | 1460 | 0.326 | 0.531 | 0.565 | 0.508 | 707-753 | 1370-1410 |
| real | walk | 1 | 458 | 1860 | 831 | n/a | n/a | n/a | 0.247 | 0.551 | n/a | n/a | n/a | 463 | 1880 | 843 | n/a | n/a | 0.246 | 0.549 | n/a | n/a | 458-483 | 831-863 |
| real | walk | all | 841 | 3280 | 1470 | n/a | n/a | n/a | 0.257 | 0.57 | n/a | n/a | n/a | 858 | 3340 | 1500 | n/a | n/a | 0.257 | 0.57 | n/a | n/a | 841-870 | 1470-1550 |
| realerr (too small) | cursor | 1 | 0.07 | 0.222 | 0.11 | 0.112 | 0.105 | 0.121 | 0.315 | 0.636 | 0.625 | 0.667 | 0.579 | 0.078 | 0.233 | 0.121 | 0.117 | 0.129 | 0.335 | 0.645 | 0.667 | 0.605 | 0.07-0.126 | 0.11-0.151 |
| realerr (too small) | cursor | all | 0.146 | 0.276 | 0.276 | 0.264 | 0.253 | 0.314 | 0.529 | 0.529 | 0.553 | 0.577 | 0.465 | 0.244 | 0.349 | 0.339 | 0.32 | 0.349 | 0.699 | 0.72 | 0.762 | 0.699 | 0.146-0.337 | 0.276-0.531 |
| realerr (too small) | parse | 1 | 0.064 | 0.17 | 0.1 | 0.107 | 0.104 | 0.112 | 0.376 | 0.64 | 0.598 | 0.615 | 0.571 | 0.07 | 0.187 | 0.111 | 0.123 | 0.119 | 0.374 | 0.631 | 0.569 | 0.588 | 0.064-0.153 | 0.1-0.134 |
| realerr (too small) | parse | all | 0.124 | 0.219 | 0.269 | 0.256 | 0.255 | 0.294 | 0.566 | 0.461 | 0.484 | 0.486 | 0.422 | 0.23 | 0.266 | 0.309 | 0.339 | 0.325 | 0.865 | 0.744 | 0.678 | 0.708 | 0.124-0.4 | 0.269-0.504 |
| realerr (too small) | walk | 1 | 0.065 | 0.212 | 0.106 | n/a | n/a | n/a | 0.307 | 0.613 | n/a | n/a | n/a | 0.071 | 0.226 | 0.118 | n/a | n/a | 0.314 | 0.602 | n/a | n/a | 0.065-0.119 | 0.106-0.142 |
| realerr (too small) | walk | all | 0.124 | 0.25 | 0.251 | n/a | n/a | n/a | 0.496 | 0.494 | n/a | n/a | n/a | 0.18 | 0.33 | 0.344 | n/a | n/a | 0.545 | 0.523 | n/a | n/a | 0.124-0.343 | 0.251-0.387 |

### python: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 6700 | 24700 | 12500 | 12400 | 12400 | 12900 | 0.271 | 0.535 | 0.542 | 0.542 | 0.518 | 6700 | 24800 | 12600 | 12400 | 13000 | 0.27 | 0.534 | 0.54 | 0.516 | 6700-6700 | 12500-12600 |
| broken | cursor | all | 6710 | 24700 | 12500 | 12400 | 12400 | 13000 | 0.271 | 0.535 | 0.541 | 0.541 | 0.517 | 6730 | 24900 | 12600 | 12400 | 13000 | 0.271 | 0.535 | 0.542 | 0.518 | 6710-6770 | 12500-12600 |
| broken | parse | 1 | 5530 | 16500 | 11200 | 11000 | 10900 | 11600 | 0.335 | 0.493 | 0.502 | 0.506 | 0.478 | 5530 | 16500 | 11300 | 11000 | 11600 | 0.335 | 0.492 | 0.505 | 0.477 | 5530-5540 | 11200-11300 |
| broken | parse | all | 5550 | 16400 | 11300 | 11100 | 11000 | 11600 | 0.339 | 0.493 | 0.501 | 0.505 | 0.478 | 5560 | 16500 | 11300 | 11000 | 11700 | 0.337 | 0.492 | 0.505 | 0.477 | 5550-5690 | 11300-11300 |
| broken | walk | 1 | 13300 | 38900 | 20400 | n/a | n/a | n/a | 0.341 | 0.651 | n/a | n/a | n/a | 13300 | 39000 | 20400 | n/a | n/a | 0.341 | 0.651 | n/a | n/a | 13300-13300 | 20400-20400 |
| broken | walk | all | 13300 | 39000 | 20400 | n/a | n/a | n/a | 0.342 | 0.652 | n/a | n/a | n/a | 13400 | 39200 | 20500 | n/a | n/a | 0.341 | 0.652 | n/a | n/a | 13300-13400 | 20400-20500 |
| real | cursor | 1 | 10500 | 34700 | 15100 | 15000 | 15300 | 15800 | 0.302 | 0.691 | 0.699 | 0.683 | 0.661 | 10500 | 34700 | 15200 | 15300 | 15800 | 0.301 | 0.689 | 0.682 | 0.66 | 10500-10500 | 15100-15200 |
| real | cursor | all | 10500 | 34700 | 15200 | 15000 | 15300 | 15800 | 0.301 | 0.689 | 0.697 | 0.682 | 0.661 | 10500 | 34800 | 15200 | 15400 | 15900 | 0.301 | 0.688 | 0.68 | 0.659 | 10500-10500 | 15200-15200 |
| real | parse | 1 | 8470 | 20500 | 12900 | 12600 | 12800 | 13400 | 0.413 | 0.658 | 0.67 | 0.662 | 0.631 | 8470 | 20500 | 12900 | 12800 | 13400 | 0.413 | 0.658 | 0.662 | 0.631 | 8470-8470 | 12900-12900 |
| real | parse | all | 8470 | 20400 | 12800 | 12600 | 12800 | 13400 | 0.416 | 0.659 | 0.672 | 0.662 | 0.631 | 8470 | 20500 | 12900 | 12800 | 13500 | 0.413 | 0.656 | 0.659 | 0.629 | 8470-8470 | 12800-13000 |
| real | walk | 1 | 9970 | 30700 | 14000 | n/a | n/a | n/a | 0.325 | 0.713 | n/a | n/a | n/a | 9970 | 30700 | 14000 | n/a | n/a | 0.324 | 0.712 | n/a | n/a | 9970-9970 | 14000-14000 |
| real | walk | all | 9970 | 30800 | 14000 | n/a | n/a | n/a | 0.324 | 0.713 | n/a | n/a | n/a | 9970 | 30800 | 14000 | n/a | n/a | 0.324 | 0.71 | n/a | n/a | 9970-9970 | 14000-14100 |
| realerr (too small) | cursor | 1 | 1.33 | 3.58 | 1.89 | 1.85 | 1.85 | 1.95 | 0.37 | 0.704 | 0.716 | 0.717 | 0.68 | 1.33 | 3.59 | 1.9 | 1.86 | 1.96 | 0.37 | 0.7 | 0.713 | 0.676 | 1.33-1.34 | 1.89-1.91 |
| realerr (too small) | cursor | all | 1.42 | 3.48 | 1.88 | 1.85 | 1.85 | 1.95 | 0.408 | 0.752 | 0.766 | 0.766 | 0.727 | 2.63 | 3.61 | 1.89 | 1.85 | 1.95 | 0.73 | 1.39 | 1.42 | 1.35 | 1.42-2.85 | 1.88-1.91 |
| realerr (too small) | parse | 1 | 1.17 | 2.54 | 1.72 | 1.69 | 1.67 | 1.78 | 0.462 | 0.682 | 0.696 | 0.703 | 0.66 | 1.18 | 2.55 | 1.74 | 1.68 | 1.79 | 0.461 | 0.678 | 0.699 | 0.656 | 1.17-1.18 | 1.72-1.74 |
| realerr (too small) | parse | all | 1.28 | 2.48 | 1.72 | 1.68 | 1.67 | 1.78 | 0.517 | 0.745 | 0.763 | 0.769 | 0.72 | 1.73 | 2.64 | 1.73 | 1.67 | 1.78 | 0.655 | 1 | 1.03 | 0.971 | 1.28-2.64 | 1.72-1.75 |
| realerr (too small) | walk | 1 | 1.28 | 3.27 | 1.8 | n/a | n/a | n/a | 0.391 | 0.71 | n/a | n/a | n/a | 1.28 | 3.27 | 1.81 | n/a | n/a | 0.391 | 0.705 | n/a | n/a | 1.28-1.29 | 1.8-1.82 |
| realerr (too small) | walk | all | 1.36 | 3.14 | 1.8 | n/a | n/a | n/a | 0.434 | 0.758 | n/a | n/a | n/a | 1.8 | 3.26 | 1.8 | n/a | n/a | 0.551 | 0.997 | n/a | n/a | 1.36-2.74 | 1.8-1.82 |

### python: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1590 | 7220 | 3620 | 3780 | 3620 | 3940 | 0.221 | 0.439 | 0.421 | 0.44 | 0.404 | 1620 | 7350 | 3670 | 3670 | 4120 | 0.221 | 0.442 | 0.442 | 0.393 | 1590-1690 | 3620-3770 |
| broken | cursor | all | 2530 | 10800 | 5320 | 5360 | 5170 | 5680 | 0.234 | 0.476 | 0.472 | 0.489 | 0.445 | 2550 | 10900 | 5400 | 5190 | 5730 | 0.234 | 0.472 | 0.491 | 0.445 | 2530-2590 | 5320-5450 |
| broken | parse | 1 | 1340 | 5180 | 3330 | 3360 | 3030 | 3680 | 0.258 | 0.402 | 0.398 | 0.441 | 0.363 | 1360 | 5290 | 3350 | 3080 | 3750 | 0.257 | 0.406 | 0.442 | 0.363 | 1340-1460 | 3330-3490 |
| broken | parse | all | 2090 | 7470 | 4850 | 4790 | 4320 | 5250 | 0.279 | 0.43 | 0.436 | 0.483 | 0.397 | 2130 | 7580 | 4930 | 4420 | 5280 | 0.281 | 0.432 | 0.482 | 0.403 | 2090-2210 | 4850-4980 |
| broken | walk | 1 | 2900 | 13800 | 4990 | n/a | n/a | n/a | 0.21 | 0.581 | n/a | n/a | n/a | 2950 | 14000 | 5210 | n/a | n/a | 0.211 | 0.565 | n/a | n/a | 2900-3040 | 4990-5680 |
| broken | walk | all | 3730 | 17000 | 6660 | n/a | n/a | n/a | 0.219 | 0.561 | n/a | n/a | n/a | 3860 | 17200 | 6850 | n/a | n/a | 0.225 | 0.564 | n/a | n/a | 3730-4070 | 6660-7230 |
| real | cursor | 1 | 2470 | 10300 | 4600 | 4830 | 5010 | 5060 | 0.24 | 0.537 | 0.511 | 0.493 | 0.488 | 2490 | 10500 | 4700 | 5060 | 5060 | 0.238 | 0.529 | 0.491 | 0.491 | 2470-2780 | 4600-5030 |
| real | cursor | all | 3940 | 16000 | 7000 | 7050 | 7210 | 7490 | 0.247 | 0.564 | 0.559 | 0.547 | 0.527 | 4010 | 16100 | 7170 | 7260 | 7650 | 0.25 | 0.56 | 0.553 | 0.525 | 3940-4050 | 7000-7210 |
| real | parse | 1 | 2030 | 6700 | 4020 | 4120 | 3950 | 4440 | 0.303 | 0.506 | 0.494 | 0.515 | 0.458 | 2050 | 6790 | 4100 | 3960 | 4440 | 0.302 | 0.5 | 0.517 | 0.461 | 2030-2120 | 4020-4310 |
| real | parse | all | 3200 | 10000 | 6180 | 6230 | 5880 | 6540 | 0.319 | 0.518 | 0.514 | 0.544 | 0.489 | 3330 | 10100 | 6340 | 5930 | 6640 | 0.328 | 0.526 | 0.561 | 0.502 | 3200-3350 | 6180-6360 |
| real | walk | 1 | 2390 | 9640 | 4340 | n/a | n/a | n/a | 0.248 | 0.551 | n/a | n/a | n/a | 2410 | 9750 | 4400 | n/a | n/a | 0.248 | 0.549 | n/a | n/a | 2390-2520 | 4340-4500 |
| real | walk | all | 3810 | 14500 | 6690 | n/a | n/a | n/a | 0.262 | 0.569 | n/a | n/a | n/a | 3860 | 14800 | 6790 | n/a | n/a | 0.262 | 0.568 | n/a | n/a | 3810-3910 | 6690-6880 |
| realerr (too small) | cursor | 1 | 0.314 | 1.1 | 0.552 | 0.563 | 0.522 | 0.607 | 0.286 | 0.569 | 0.558 | 0.601 | 0.517 | 0.334 | 1.14 | 0.605 | 0.557 | 0.647 | 0.294 | 0.553 | 0.6 | 0.517 | 0.314-0.528 | 0.552-0.73 |
| realerr (too small) | cursor | all | 0.594 | 1.3 | 0.931 | 0.851 | 0.815 | 1.05 | 0.457 | 0.639 | 0.699 | 0.729 | 0.567 | 1.02 | 1.58 | 1.02 | 1.04 | 1.19 | 0.644 | 0.998 | 0.984 | 0.854 | 0.594-1.48 | 0.931-1.45 |
| realerr (too small) | parse | 1 | 0.283 | 0.835 | 0.505 | 0.538 | 0.518 | 0.558 | 0.339 | 0.561 | 0.526 | 0.547 | 0.508 | 0.305 | 0.896 | 0.54 | 0.617 | 0.596 | 0.34 | 0.564 | 0.494 | 0.511 | 0.283-0.674 | 0.505-0.658 |
| realerr (too small) | parse | all | 0.489 | 1.03 | 0.775 | 0.797 | 0.875 | 0.924 | 0.473 | 0.631 | 0.614 | 0.559 | 0.529 | 0.926 | 1.23 | 0.953 | 0.94 | 1.03 | 0.753 | 0.972 | 0.985 | 0.899 | 0.489-1.58 | 0.775-1.09 |
| realerr (too small) | walk | 1 | 0.305 | 1.03 | 0.528 | n/a | n/a | n/a | 0.295 | 0.577 | n/a | n/a | n/a | 0.322 | 1.09 | 0.59 | n/a | n/a | 0.295 | 0.546 | n/a | n/a | 0.305-0.506 | 0.528-0.701 |
| realerr (too small) | walk | all | 0.533 | 1.16 | 0.849 | n/a | n/a | n/a | 0.461 | 0.628 | n/a | n/a | n/a | 0.758 | 1.52 | 1.02 | n/a | n/a | 0.499 | 0.745 | n/a | n/a | 0.533-1.45 | 0.849-1.38 |

### python: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1160 | 8220 | 1280 | 1320 | 1420 | 1350 | 0.141 | 0.906 | 0.881 | 0.817 | 0.861 |
| broken | cursor minus parse | 1 | cycles M | 257 | 2050 | 299 | 420 | 592 | 258 | 0.126 | 0.86 | 0.612 | 0.434 | 0.997 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 50.1 | 402 | 56 | 79.8 | 111 | 46.7 | 0.124 | 0.894 | 0.627 | 0.452 | 1.07 |
| broken | cursor minus parse | all | instructions M | 1160 | 8330 | 1290 | 1330 | 1430 | 1370 | 0.139 | 0.902 | 0.873 | 0.813 | 0.845 |
| broken | cursor minus parse | all | cycles M | 446 | 3360 | 470 | 575 | 858 | 436 | 0.133 | 0.949 | 0.776 | 0.52 | 1.02 |
| broken | cursor minus parse | all | CPU user+sys ms | 99.3 | 762 | 99.8 | 137 | 192 | 96.9 | 0.13 | 0.995 | 0.725 | 0.518 | 1.03 |
| broken | walk minus parse | 1 | instructions M | 7760 | 22400 | 9170 | n/a | n/a | n/a | 0.346 | 0.846 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 1560 | 8600 | 1660 | n/a | n/a | n/a | 0.181 | 0.94 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 299 | 1710 | 318 | n/a | n/a | n/a | 0.175 | 0.94 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 7780 | 22600 | 9190 | n/a | n/a | n/a | 0.344 | 0.847 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 1650 | 9570 | 1810 | n/a | n/a | n/a | 0.172 | 0.911 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 323 | 1970 | 339 | n/a | n/a | n/a | 0.164 | 0.952 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1990 | 14200 | 2280 | 2330 | 2510 | 2390 | 0.14 | 0.872 | 0.851 | 0.792 | 0.831 |
| real | cursor minus parse | 1 | cycles M | 434 | 3600 | 580 | 713 | 1050 | 620 | 0.121 | 0.749 | 0.61 | 0.413 | 0.701 |
| real | cursor minus parse | 1 | CPU user+sys ms | 83.6 | 695 | 111 | 137 | 202 | 118 | 0.12 | 0.752 | 0.612 | 0.414 | 0.706 |
| real | cursor minus parse | all | instructions M | 1990 | 14400 | 2320 | 2390 | 2560 | 2390 | 0.138 | 0.854 | 0.832 | 0.777 | 0.832 |
| real | cursor minus parse | all | cycles M | 745 | 5940 | 817 | 824 | 1330 | 945 | 0.125 | 0.911 | 0.904 | 0.561 | 0.788 |
| real | cursor minus parse | all | CPU user+sys ms | 177 | 1380 | 180 | 189 | 304 | 214 | 0.129 | 0.984 | 0.937 | 0.581 | 0.826 |
| real | walk minus parse | 1 | instructions M | 1500 | 10200 | 1120 | n/a | n/a | n/a | 0.147 | 1.34 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 356 | 2930 | 320 | n/a | n/a | n/a | 0.121 | 1.11 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 68.4 | 568 | 61.2 | n/a | n/a | n/a | 0.12 | 1.12 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1500 | 10400 | 1140 | n/a | n/a | n/a | 0.145 | 1.32 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 606 | 4510 | 509 | n/a | n/a | n/a | 0.134 | 1.19 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 134 | 1040 | 106 | n/a | n/a | n/a | 0.128 | 1.25 | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | 1 | instructions M | 0.152 | 1.04 | 0.162 | 0.166 | 0.179 | 0.17 | 0.146 | 0.938 | 0.915 | 0.849 | 0.893 |
| realerr (too small) | cursor minus parse | 1 | cycles M | 0.0305 | 0.263 | 0.0464 | 0.0244 | 0.00457 | 0.0486 | 0.116 | 0.658 | 1.25 | 6.68 | 0.628 |
| realerr (too small) | cursor minus parse | 1 | CPU user+sys ms | 0.006 | 0.052 | 0.01 | 0.005 | 0.001 | 0.009 | 0.115 | 0.6 | 1.2 | 6 | 0.667 |
| realerr (too small) | cursor minus parse | all | instructions M | 0.135 | 0.998 | 0.162 | 0.17 | 0.183 | 0.17 | 0.136 | 0.834 | 0.797 | 0.739 | 0.795 |
| realerr (too small) | cursor minus parse | all | cycles M | 0.105 | 0.266 | 0.156 | 0.0538 | n/a | 0.123 | 0.395 | 0.675 | 1.95 | n/a | 0.856 |
| realerr (too small) | cursor minus parse | all | CPU user+sys ms | 0.022 | 0.057 | 0.007 | 0.008 | n/a | 0.02 | 0.386 | 3.14 | 2.75 | n/a | 1.1 |
| realerr (too small) | walk minus parse | 1 | instructions M | 0.103 | 0.726 | 0.0778 | n/a | n/a | n/a | 0.142 | 1.33 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | cycles M | 0.0213 | 0.196 | 0.0228 | n/a | n/a | n/a | 0.109 | 0.933 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | CPU user+sys ms | 0.001 | 0.042 | 0.006 | n/a | n/a | n/a | 0.0238 | 0.167 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | instructions M | 0.0822 | 0.664 | 0.0783 | n/a | n/a | n/a | 0.124 | 1.05 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | cycles M | 0.0443 | 0.125 | 0.0744 | n/a | n/a | n/a | 0.355 | 0.595 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | CPU user+sys ms | n/a | 0.031 | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

### python: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 118 | 221 | 57.4 | 0.532 | 2.05 | 118 | 221 | 57.4 | 0.532 | 2.05 | 108 | 209 | 49.8 | 0.514 | 2.16 |
| broken | cursor | all | 220 | 418 | 269 | 0.528 | 0.818 | 220 | 418 | 269 | 0.528 | 0.818 | 210 | 406 | 262 | 0.517 | 0.802 |
| broken | parse | 1 | 55.8 | 52.6 | 42.1 | 1.06 | 1.33 | 55.8 | 52.6 | 42.1 | 1.06 | 1.33 | 46 | 41.1 | 34.5 | 1.12 | 1.33 |
| broken | parse | all | 133 | 357 | 238 | 0.374 | 0.561 | 133 | 357 | 238 | 0.374 | 0.561 | 123 | 346 | 230 | 0.355 | 0.534 |
| broken | walk | 1 | 85.9 | 202 | 53.7 | 0.426 | 1.6 | 85.9 | 202 | 53.7 | 0.426 | 1.6 | 76 | 190 | 46.4 | 0.399 | 1.64 |
| broken | walk | all | 168 | 359 | 342 | 0.469 | 0.492 | 168 | 359 | 342 | 0.469 | 0.492 | 158 | 347 | 334 | 0.454 | 0.471 |
| real | cursor | 1 | 44.3 | 113 | 25.5 | 0.391 | 1.74 | 44.3 | 113 | 25.5 | 0.391 | 1.74 | 27.2 | 95 | 11.8 | 0.286 | 2.31 |
| real | cursor | all | 106 | 236 | 155 | 0.45 | 0.686 | 106 | 236 | 155 | 0.45 | 0.686 | 89.2 | 218 | 141 | 0.41 | 0.633 |
| real | parse | 1 | 31.4 | 42.7 | 24.2 | 0.734 | 1.3 | 31.4 | 42.7 | 24.2 | 0.734 | 1.3 | 14.2 | 24.3 | 10.6 | 0.583 | 1.34 |
| real | parse | all | 85 | 214 | 154 | 0.398 | 0.551 | 85 | 214 | 154 | 0.398 | 0.551 | 67.9 | 195 | 141 | 0.347 | 0.482 |
| real | walk | 1 | 30.4 | 108 | 25.5 | 0.281 | 1.19 | 30.4 | 108 | 25.5 | 0.281 | 1.19 | 13.3 | 89.7 | 11.9 | 0.148 | 1.12 |
| real | walk | all | 85.7 | 234 | 154 | 0.366 | 0.557 | 85.7 | 234 | 154 | 0.367 | 0.557 | 68.5 | 215 | 140 | 0.318 | 0.489 |
| realerr | cursor | 1 | 5.17 | 6.21 | 4.3 | 0.833 | 1.2 | 5.18 | 6.21 | 4.3 | 0.833 | 1.2 | 1.91 | 1.4 | 1.74 | 1.36 | 1.1 |
| realerr | cursor | all | 6.74 | 6.95 | 4.8 | 0.97 | 1.4 | 6.77 | 6.95 | 4.8 | 0.973 | 1.41 | 3.48 | 2.25 | 2.16 | 1.55 | 1.61 |
| realerr | parse | 1 | 5.05 | 5.8 | 3.48 | 0.869 | 1.45 | 5.05 | 5.81 | 3.48 | 0.87 | 1.45 | 1.89 | 1.01 | 0.906 | 1.88 | 2.09 |
| realerr | parse | all | 6.85 | 6.58 | 3.62 | 1.04 | 1.89 | 6.88 | 6.58 | 3.62 | 1.05 | 1.9 | 3.59 | 1.89 | 1.02 | 1.9 | 3.5 |
| realerr | walk | 1 | 5.18 | 6.15 | 4.52 | 0.843 | 1.15 | 5.19 | 6.16 | 4.52 | 0.843 | 1.15 | 1.91 | 1.39 | 1.97 | 1.38 | 0.97 |
| realerr | walk | all | 6.68 | 6.95 | 4.56 | 0.961 | 1.46 | 6.71 | 6.96 | 4.56 | 0.964 | 1.47 | 3.41 | 2.2 | 2.03 | 1.55 | 1.68 |

### python: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 55.8 | 53.2 | 41.7 | 1.05 | 1.34 | 55.8 | 53.2 | 41.7 | 1.05 | 1.34 | 45.9 | 42 | 34.4 | 1.09 | 1.34 |
| broken | all | 165 | 653 | 317 | 0.253 | 0.521 | 165 | 653 | 317 | 0.253 | 0.521 | 154 | 642 | 309 | 0.241 | 0.5 |
| real | 1 | 30.4 | 52.8 | 24.1 | 0.576 | 1.26 | 30.4 | 52.8 | 24.1 | 0.576 | 1.26 | 13.3 | 34.5 | 10.5 | 0.386 | 1.27 |
| real | all | 92.3 | 309 | 170 | 0.299 | 0.543 | 92.3 | 309 | 170 | 0.299 | 0.543 | 75.1 | 290 | 156 | 0.259 | 0.481 |
| realerr | 1 | 5.05 | 5.78 | 3.17 | 0.874 | 1.59 | 5.05 | 5.78 | 3.17 | 0.874 | 1.6 | 1.89 | 1.09 | 0.742 | 1.73 | 2.55 |
| realerr | all | 8.8 | 7.28 | 4.04 | 1.21 | 2.18 | 8.8 | 7.28 | 4.04 | 1.21 | 2.18 | 5.54 | 2.51 | 1.29 | 2.2 | 4.29 |

### python: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0147 (real), 3 (realerr), 0.0809 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.25 | 14000 | 9.22 | 3450 | 316000 | 3450 | 316000 | 0 |
| real | steady | 0.0147 | 50.6 | 9.21 | 3450 | 316000 | 3450 | 316000 | 0 |
| realerr | cold | 39 | 59400 | 11.3 | 137 | 13200 | 137 | 13200 | 0.284 |
| realerr | steady | 3 | 69.3 | 11 | 129 | 11500 | 129 | 11500 | 0.0233 |
| broken | cold | 4.84 | 109000 | 9.3 | 5140 | 580000 | 5140 | 580000 | 0.000942 |
| broken | steady | 0.0809 | 6040 | 9.28 | 5140 | 579000 | 5140 | 579000 | 0 |

### python: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2052 B | init ms | 0.00943 | 0.012 | 0.00544 | 0.787 | 1.73 | 15 |
| #0 2052 B | parse1 ms | 0.252 | 0.361 | 0.262 | 0.698 | 0.963 | 15 |
| #0 2052 B | init+parse1 ms | 0.262 | 0.373 | 0.268 | 0.705 | 0.979 | 15 |
| #0 2052 B | parse2 ms | 0.13 | 0.316 | 0.183 | 0.409 | 0.709 | 15 |
| #0 2052 B | process user+sys ms | 1.1 | 1.65 | 0.756 | 0.665 | 1.45 | 15 |
| #0 2052 B | exec to exit instr user+kernel M | 8.8 | 13.4 | 6.96 | 0.656 | 1.26 | 15 |
| #0 2052 B | exec to exit instr user M | 6.03 | 9.65 | 5.91 | 0.625 | 1.02 | 15 |
| #0 2052 B | exec to exit instr kernel M | 2.73 | 3.73 | 1.06 | 0.73 | 2.58 | 15 |
| #0 2052 B | exec to exit cycles user+kernel M | 4.98 | 7.89 | 3.43 | 0.632 | 1.45 | 15 |
| #0 2052 B | exec to exit task-clock ms | 1.02 | 1.58 | 0.66 | 0.646 | 1.55 | 15 |
| #0 2052 B | peak RSS MB | 4.25 | 5.34 | 2.97 | 0.797 | 1.43 | 15 |
| #0 2052 B | peak footprint MB | 4.28 | 5.37 | 2.88 | 0.797 | 1.49 | 15 |
| #1 4532 B | init ms | 0.00885 | 0.0126 | 0.00554 | 0.702 | 1.6 | 15 |
| #1 4532 B | parse1 ms | 0.304 | 0.523 | 0.382 | 0.582 | 0.796 | 15 |
| #1 4532 B | init+parse1 ms | 0.32 | 0.535 | 0.388 | 0.598 | 0.825 | 15 |
| #1 4532 B | parse2 ms | 0.186 | 0.503 | 0.286 | 0.37 | 0.651 | 15 |
| #1 4532 B | process user+sys ms | 1.21 | 2.11 | 0.96 | 0.57 | 1.26 | 15 |
| #1 4532 B | exec to exit instr user+kernel M | 12 | 19.1 | 10.8 | 0.629 | 1.12 | 15 |
| #1 4532 B | exec to exit instr user M | 9.18 | 15.2 | 9.65 | 0.605 | 0.951 | 15 |
| #1 4532 B | exec to exit instr kernel M | 2.83 | 3.92 | 1.11 | 0.722 | 2.55 | 15 |
| #1 4532 B | exec to exit cycles user+kernel M | 5.54 | 10.1 | 4.49 | 0.549 | 1.23 | 15 |
| #1 4532 B | exec to exit task-clock ms | 1.15 | 2.04 | 0.86 | 0.564 | 1.34 | 15 |
| #1 4532 B | peak RSS MB | 4.23 | 5.43 | 3.18 | 0.779 | 1.33 | 15 |
| #1 4532 B | peak footprint MB | 4.25 | 5.46 | 3.06 | 0.78 | 1.39 | 15 |

### python: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1022 | 2782347 | 0 | 0 | ok |
| realerr | 3 | 199 | 3 | 0 | ok |
| broken | 507 | 1626924 | 407 | 0 | ok |

## ruby

### ruby: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 222 | 1230 | 752 | 751 | 758 | 685 | 791 | 0.18 | 0.295 | 0.295 | 0.292 | 0.324 | 0.281 | 226 | 1260 | 769 | 770 | 686 | 819 | 0.178 | 0.294 | 0.293 | 0.329 | 0.276 | 222-235 | 752-817 | 1.01 | 1.01 | 1 | 12.3 |
| broken | cursor | all | 57.8 | 281 | 210 | 216 | 217 | 169 | 229 | 0.205 | 0.274 | 0.268 | 0.266 | 0.341 | 0.252 | 61.6 | 288 | 226 | 221 | 174 | 232 | 0.214 | 0.273 | 0.278 | 0.355 | 0.265 | 57.8-74.4 | 210-242 | 5.93 * | 7.04 * | 5.31 * | 47.2 |
| broken | parse | 1 | 181 | 1020 | 713 | 706 | 716 | 603 | 763 | 0.178 | 0.254 | 0.257 | 0.253 | 0.3 | 0.238 | 185 | 1030 | 727 | 712 | 604 | 797 | 0.18 | 0.254 | 0.259 | 0.305 | 0.232 | 181-198 | 713-777 | 1 | 1.01 | 1 | 15 |
| broken | parse | all | 40 | 249 | 212 | 212 | 210 | 163 | 229 | 0.16 | 0.188 | 0.189 | 0.191 | 0.245 | 0.174 | 44 | 264 | 221 | 218 | 168 | 239 | 0.166 | 0.199 | 0.201 | 0.262 | 0.184 | 40-47.2 | 212-228 | 7.14 * | 6.17 * | 5.01 * | 68.3 |
| broken | walk | 1 | 219 | 1270 | 746 | 740 | n/a | n/a | n/a | 0.172 | 0.293 | 0.296 | n/a | n/a | n/a | 221 | 1290 | 752 | 750 | n/a | n/a | 0.172 | 0.294 | 0.295 | n/a | n/a | 219-232 | 746-778 | 1 | 1.01 | 1 | 12.5 |
| broken | walk | all | 54.9 | 328 | 229 | 226 | n/a | n/a | n/a | 0.167 | 0.239 | 0.243 | n/a | n/a | n/a | 56.6 | 333 | 236 | 235 | n/a | n/a | 0.17 | 0.239 | 0.24 | n/a | n/a | 54.9-61.6 | 229-250 | 6.52 * | 6.06 * | 4.93 * | 49.7 |
| real | cursor | 1 | 326 | 1440 | 656 | 647 | 681 | 745 | 727 | 0.227 | 0.498 | 0.505 | 0.479 | 0.438 | 0.449 | 334 | 1460 | 662 | 651 | 748 | 744 | 0.229 | 0.505 | 0.513 | 0.447 | 0.449 | 326-353 | 656-680 | 1 | 1.01 | 1 | 25.5 |
| real | cursor | all | 41.8 | 184 | 76.6 | 75.7 | 75.9 | 80.4 | 84.9 | 0.227 | 0.545 | 0.552 | 0.551 | 0.519 | 0.492 | 43.2 | 187 | 78 | 77.7 | 81.6 | 86.3 | 0.231 | 0.554 | 0.556 | 0.53 | 0.501 | 41.8-44.8 | 76.6-81.2 | 14.9 | 14.2 | 15.3 | 199 |
| real | parse | 1 | 267 | 1050 | 576 | 567 | 594 | 588 | 632 | 0.255 | 0.463 | 0.471 | 0.449 | 0.453 | 0.422 | 272 | 1070 | 596 | 578 | 597 | 639 | 0.254 | 0.457 | 0.471 | 0.456 | 0.426 | 267-277 | 576-656 | 1 | 1 | 1 | 31.2 |
| real | parse | all | 33 | 127 | 66.7 | 65.9 | 66.3 | 66.8 | 71.3 | 0.261 | 0.495 | 0.501 | 0.498 | 0.495 | 0.463 | 34.5 | 128 | 67.9 | 67.5 | 67.9 | 73.5 | 0.269 | 0.508 | 0.511 | 0.508 | 0.469 | 33-36 | 66.7-70.6 | 14.8 | 14.5 | 15.3 | 252 |
| real | walk | 1 | 317 | 1400 | 624 | 612 | n/a | n/a | n/a | 0.226 | 0.507 | 0.517 | n/a | n/a | n/a | 319 | 1420 | 633 | 617 | n/a | n/a | 0.224 | 0.503 | 0.517 | n/a | n/a | 317-330 | 624-658 | 1 | 1.01 | 1 | 26.3 |
| real | walk | all | 39.6 | 176 | 71.8 | 71.5 | n/a | n/a | n/a | 0.225 | 0.551 | 0.554 | n/a | n/a | n/a | 41.5 | 180 | 72.9 | 72.6 | n/a | n/a | 0.231 | 0.569 | 0.572 | n/a | n/a | 39.6-44.3 | 71.8-76.1 | 14.7 | 13.9 | 15.4 | 210 |

### ruby: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 224 | 1240 | 752 | 751 | 758 | 685 | 790 | 0.181 | 0.298 | 0.298 | 0.295 | 0.327 | 0.283 | 229 | 1280 | 769 | 770 | 685 | 819 | 0.179 | 0.297 | 0.297 | 0.334 | 0.279 | 224-239 | 752-817 |
| broken | cursor | all | 356 | 2000 | 1160 | 1130 | 1160 | 1030 | 1230 | 0.178 | 0.308 | 0.316 | 0.307 | 0.344 | 0.29 | 379 | 2030 | 1210 | 1190 | 1080 | 1250 | 0.187 | 0.314 | 0.319 | 0.352 | 0.304 | 356-398 | 1160-1230 |
| broken | parse | 1 | 181 | 1020 | 713 | 706 | 716 | 603 | 763 | 0.177 | 0.255 | 0.257 | 0.253 | 0.301 | 0.238 | 185 | 1030 | 727 | 712 | 604 | 797 | 0.179 | 0.254 | 0.259 | 0.306 | 0.232 | 181-198 | 713-776 |
| broken | parse | all | 302 | 1570 | 1040 | 1070 | 1080 | 928 | 1200 | 0.192 | 0.291 | 0.281 | 0.28 | 0.325 | 0.252 | 314 | 1630 | 1110 | 1110 | 955 | 1200 | 0.193 | 0.282 | 0.282 | 0.329 | 0.262 | 302-323 | 1040-1130 |
| broken | walk | 1 | 219 | 1290 | 746 | 740 | n/a | n/a | n/a | 0.17 | 0.293 | 0.296 | n/a | n/a | n/a | 222 | 1310 | 752 | 749 | n/a | n/a | 0.17 | 0.294 | 0.296 | n/a | n/a | 219-232 | 746-778 |
| broken | walk | all | 363 | 1990 | 1100 | 1130 | n/a | n/a | n/a | 0.182 | 0.33 | 0.323 | n/a | n/a | n/a | 373 | 2030 | 1170 | 1160 | n/a | n/a | 0.184 | 0.317 | 0.321 | n/a | n/a | 363-381 | 1100-1200 |
| real | cursor | 1 | 327 | 1450 | 656 | 647 | 681 | 745 | 727 | 0.226 | 0.498 | 0.505 | 0.48 | 0.438 | 0.449 | 335 | 1470 | 662 | 651 | 748 | 744 | 0.228 | 0.506 | 0.515 | 0.448 | 0.451 | 327-354 | 656-680 |
| real | cursor | all | 624 | 2640 | 1180 | 1150 | 1160 | 1260 | 1220 | 0.237 | 0.53 | 0.544 | 0.536 | 0.496 | 0.51 | 647 | 2660 | 1200 | 1180 | 1260 | 1240 | 0.243 | 0.537 | 0.546 | 0.512 | 0.523 | 624-656 | 1180-1210 |
| real | parse | 1 | 267 | 1050 | 576 | 566 | 594 | 588 | 632 | 0.254 | 0.463 | 0.471 | 0.449 | 0.454 | 0.422 | 273 | 1080 | 596 | 578 | 597 | 639 | 0.253 | 0.457 | 0.472 | 0.456 | 0.426 | 267-278 | 576-656 |
| real | parse | all | 494 | 1850 | 1010 | 984 | 999 | 1010 | 1080 | 0.267 | 0.491 | 0.502 | 0.494 | 0.488 | 0.456 | 515 | 1860 | 1050 | 1020 | 1030 | 1110 | 0.277 | 0.489 | 0.505 | 0.502 | 0.463 | 494-519 | 1010-1060 |
| real | walk | 1 | 317 | 1410 | 624 | 612 | n/a | n/a | n/a | 0.225 | 0.508 | 0.518 | n/a | n/a | n/a | 319 | 1430 | 633 | 617 | n/a | n/a | 0.222 | 0.504 | 0.517 | n/a | n/a | 317-330 | 624-658 |
| real | walk | all | 572 | 2480 | 1100 | 1080 | n/a | n/a | n/a | 0.231 | 0.52 | 0.53 | n/a | n/a | n/a | 606 | 2500 | 1130 | 1110 | n/a | n/a | 0.242 | 0.539 | 0.545 | n/a | n/a | 572-619 | 1100-1150 |

### ruby: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 5050 | 22900 | 14500 | 14300 | 14200 | 14000 | 14700 | 0.221 | 0.349 | 0.352 | 0.356 | 0.36 | 0.344 | 5060 | 22900 | 14500 | 14400 | 14000 | 14700 | 0.22 | 0.35 | 0.352 | 0.36 | 0.344 | 5050-5060 | 14500-14500 |
| broken | cursor | all | 5070 | 23000 | 14500 | 14400 | 14200 | 14000 | 14700 | 0.221 | 0.351 | 0.353 | 0.356 | 0.362 | 0.345 | 5110 | 23000 | 14500 | 14400 | 14000 | 14700 | 0.222 | 0.352 | 0.355 | 0.363 | 0.346 | 5070-5200 | 14500-14500 |
| broken | parse | 1 | 4190 | 18300 | 13500 | 13400 | 13300 | 13000 | 13800 | 0.229 | 0.31 | 0.312 | 0.316 | 0.322 | 0.305 | 4190 | 18300 | 13500 | 13400 | 13000 | 13800 | 0.229 | 0.31 | 0.312 | 0.322 | 0.305 | 4190-4190 | 13500-13500 |
| broken | parse | all | 4210 | 18300 | 13500 | 13400 | 13200 | 13000 | 13800 | 0.231 | 0.311 | 0.313 | 0.318 | 0.323 | 0.306 | 4220 | 18300 | 13500 | 13400 | 13000 | 13800 | 0.231 | 0.312 | 0.314 | 0.324 | 0.307 | 4210-4260 | 13500-13600 |
| broken | walk | 1 | 5120 | 22400 | 14300 | 14200 | n/a | n/a | n/a | 0.229 | 0.359 | 0.362 | n/a | n/a | n/a | 5130 | 22400 | 14300 | 14200 | n/a | n/a | 0.229 | 0.359 | 0.362 | n/a | n/a | 5120-5130 | 14300-14300 |
| broken | walk | all | 5170 | 22500 | 14300 | 14200 | n/a | n/a | n/a | 0.229 | 0.362 | 0.365 | n/a | n/a | n/a | 5190 | 22600 | 14300 | 14200 | n/a | n/a | 0.23 | 0.364 | 0.366 | n/a | n/a | 5170-5230 | 14300-14300 |
| real | cursor | 1 | 7580 | 25500 | 11500 | 11200 | 11400 | 11900 | 12000 | 0.297 | 0.657 | 0.676 | 0.665 | 0.638 | 0.634 | 7590 | 25600 | 11600 | 11200 | 11900 | 12000 | 0.297 | 0.657 | 0.676 | 0.638 | 0.634 | 7580-7590 | 11500-11600 |
| real | cursor | all | 7580 | 25600 | 11600 | 11200 | 11400 | 11900 | 12000 | 0.297 | 0.657 | 0.675 | 0.664 | 0.636 | 0.633 | 7580 | 25600 | 11600 | 11200 | 11900 | 12000 | 0.296 | 0.655 | 0.674 | 0.636 | 0.632 | 7580-7660 | 11600-11600 |
| real | parse | 1 | 6050 | 17300 | 9820 | 9490 | 9640 | 10000 | 10200 | 0.35 | 0.616 | 0.637 | 0.627 | 0.604 | 0.594 | 6050 | 17300 | 9830 | 9500 | 10000 | 10200 | 0.35 | 0.615 | 0.636 | 0.603 | 0.594 | 6050-6050 | 9820-9830 |
| real | parse | all | 6050 | 17300 | 9800 | 9480 | 9600 | 9990 | 10200 | 0.35 | 0.617 | 0.638 | 0.63 | 0.605 | 0.595 | 6050 | 17300 | 9820 | 9500 | 10000 | 10200 | 0.35 | 0.616 | 0.637 | 0.604 | 0.594 | 6050-6050 | 9800-9840 |
| real | walk | 1 | 7250 | 23400 | 10700 | 10400 | n/a | n/a | n/a | 0.31 | 0.678 | 0.699 | n/a | n/a | n/a | 7250 | 23400 | 10700 | 10400 | n/a | n/a | 0.309 | 0.678 | 0.699 | n/a | n/a | 7250-7250 | 10700-10700 |
| real | walk | all | 7250 | 23400 | 10700 | 10300 | n/a | n/a | n/a | 0.309 | 0.681 | 0.701 | n/a | n/a | n/a | 7250 | 23500 | 10700 | 10400 | n/a | n/a | 0.309 | 0.678 | 0.7 | n/a | n/a | 7250-7290 | 10700-10700 |

### ruby: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1160 | 6370 | 3870 | 3870 | 3910 | 3520 | 4070 | 0.182 | 0.299 | 0.299 | 0.296 | 0.329 | 0.284 | 1180 | 6540 | 3960 | 3970 | 3520 | 4220 | 0.18 | 0.297 | 0.297 | 0.334 | 0.279 | 1160-1230 | 3870-4210 |
| broken | cursor | all | 1680 | 9190 | 5470 | 5320 | 5510 | 4820 | 5790 | 0.183 | 0.307 | 0.316 | 0.305 | 0.349 | 0.29 | 1760 | 9310 | 5600 | 5550 | 4980 | 5860 | 0.189 | 0.314 | 0.317 | 0.354 | 0.3 | 1680-1830 | 5470-5710 |
| broken | parse | 1 | 947 | 5280 | 3690 | 3650 | 3700 | 3110 | 3950 | 0.179 | 0.257 | 0.26 | 0.256 | 0.304 | 0.24 | 964 | 5320 | 3760 | 3680 | 3120 | 4120 | 0.181 | 0.256 | 0.262 | 0.309 | 0.234 | 947-1030 | 3690-4000 |
| broken | parse | all | 1420 | 7340 | 4920 | 5080 | 5110 | 4330 | 5580 | 0.194 | 0.289 | 0.28 | 0.278 | 0.328 | 0.255 | 1480 | 7570 | 5220 | 5240 | 4430 | 5660 | 0.195 | 0.283 | 0.282 | 0.333 | 0.261 | 1420-1500 | 4920-5350 |
| broken | walk | 1 | 1140 | 6570 | 3860 | 3820 | n/a | n/a | n/a | 0.174 | 0.296 | 0.299 | n/a | n/a | n/a | 1160 | 6660 | 3890 | 3880 | n/a | n/a | 0.174 | 0.297 | 0.298 | n/a | n/a | 1140-1210 | 3860-4020 |
| broken | walk | all | 1700 | 9260 | 5220 | 5360 | n/a | n/a | n/a | 0.184 | 0.326 | 0.318 | n/a | n/a | n/a | 1740 | 9310 | 5520 | 5430 | n/a | n/a | 0.187 | 0.316 | 0.321 | n/a | n/a | 1700-1770 | 5220-5650 |
| real | cursor | 1 | 1700 | 7520 | 3420 | 3380 | 3560 | 3890 | 3800 | 0.227 | 0.497 | 0.505 | 0.479 | 0.438 | 0.449 | 1750 | 7620 | 3460 | 3400 | 3900 | 3880 | 0.229 | 0.505 | 0.514 | 0.448 | 0.45 | 1700-1840 | 3420-3550 |
| real | cursor | all | 2780 | 11600 | 5240 | 5120 | 5190 | 5620 | 5460 | 0.24 | 0.53 | 0.543 | 0.535 | 0.494 | 0.508 | 2860 | 11700 | 5330 | 5270 | 5630 | 5540 | 0.245 | 0.536 | 0.543 | 0.508 | 0.516 | 2780-2880 | 5240-5380 |
| real | parse | 1 | 1390 | 5470 | 3010 | 2960 | 3100 | 3070 | 3300 | 0.255 | 0.463 | 0.471 | 0.449 | 0.454 | 0.422 | 1420 | 5590 | 3110 | 3010 | 3120 | 3340 | 0.254 | 0.457 | 0.471 | 0.456 | 0.426 | 1390-1450 | 3010-3420 |
| real | parse | all | 2200 | 8140 | 4530 | 4420 | 4470 | 4540 | 4850 | 0.271 | 0.486 | 0.499 | 0.493 | 0.485 | 0.454 | 2290 | 8190 | 4670 | 4590 | 4600 | 4980 | 0.279 | 0.49 | 0.499 | 0.497 | 0.46 | 2200-2300 | 4530-4700 |
| real | walk | 1 | 1650 | 7320 | 3260 | 3190 | n/a | n/a | n/a | 0.226 | 0.507 | 0.518 | n/a | n/a | n/a | 1660 | 7440 | 3300 | 3220 | n/a | n/a | 0.224 | 0.504 | 0.517 | n/a | n/a | 1650-1720 | 3260-3430 |
| real | walk | all | 2590 | 10900 | 4930 | 4860 | n/a | n/a | n/a | 0.238 | 0.525 | 0.532 | n/a | n/a | n/a | 2700 | 11000 | 5010 | 4960 | n/a | n/a | 0.246 | 0.54 | 0.545 | n/a | n/a | 2590-2750 | 4930-5050 |

### ruby: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 863 | 4580 | 922 | 922 | 949 | 1010 | 958 | 0.188 | 0.935 | 0.935 | 0.909 | 0.852 | 0.9 |
| broken | cursor minus parse | 1 | cycles M | 211 | 1090 | 189 | 221 | 209 | 409 | 125 | 0.194 | 1.12 | 0.954 | 1.01 | 0.516 | 1.69 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 42.5 | 216 | 39.3 | 45.7 | 42.5 | 82 | 27.4 | 0.196 | 1.08 | 0.93 | 1 | 0.518 | 1.55 |
| broken | cursor minus parse | all | instructions M | 859 | 4720 | 928 | 937 | 980 | 994 | 952 | 0.182 | 0.925 | 0.916 | 0.876 | 0.864 | 0.902 |
| broken | cursor minus parse | all | cycles M | 259 | 1850 | 552 | 243 | 403 | 484 | 208 | 0.14 | 0.469 | 1.07 | 0.643 | 0.535 | 1.25 |
| broken | cursor minus parse | all | CPU user+sys ms | 54 | 428 | 119 | 54.2 | 82.9 | 105 | 31.1 | 0.126 | 0.455 | 0.996 | 0.652 | 0.514 | 1.74 |
| broken | walk minus parse | 1 | instructions M | 927 | 4100 | 730 | 730 | n/a | n/a | n/a | 0.226 | 1.27 | 1.27 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 196 | 1290 | 171 | 177 | n/a | n/a | n/a | 0.152 | 1.14 | 1.1 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 37.5 | 262 | 33.6 | 33.9 | n/a | n/a | n/a | 0.143 | 1.12 | 1.11 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 961 | 4290 | 750 | 732 | n/a | n/a | n/a | 0.224 | 1.28 | 1.31 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 282 | 1920 | 307 | 280 | n/a | n/a | n/a | 0.147 | 0.92 | 1.01 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 61.6 | 420 | 61.5 | 52.2 | n/a | n/a | n/a | 0.147 | 1 | 1.18 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1540 | 8230 | 1720 | 1720 | 1770 | 1880 | 1790 | 0.187 | 0.892 | 0.892 | 0.87 | 0.819 | 0.86 |
| real | cursor minus parse | 1 | cycles M | 311 | 2050 | 418 | 419 | 457 | 821 | 500 | 0.152 | 0.745 | 0.744 | 0.682 | 0.379 | 0.623 |
| real | cursor minus parse | 1 | CPU user+sys ms | 59.9 | 396 | 79.9 | 80.1 | 87.6 | 157 | 95.7 | 0.151 | 0.749 | 0.748 | 0.684 | 0.381 | 0.626 |
| real | cursor minus parse | all | instructions M | 1540 | 8300 | 1750 | 1750 | 1820 | 1930 | 1830 | 0.185 | 0.877 | 0.878 | 0.846 | 0.797 | 0.842 |
| real | cursor minus parse | all | cycles M | 574 | 3450 | 705 | 701 | 723 | 1080 | 609 | 0.166 | 0.815 | 0.819 | 0.794 | 0.532 | 0.943 |
| real | cursor minus parse | all | CPU user+sys ms | 130 | 789 | 173 | 162 | 166 | 245 | 139 | 0.165 | 0.754 | 0.801 | 0.785 | 0.53 | 0.935 |
| real | walk minus parse | 1 | instructions M | 1210 | 6140 | 876 | 876 | n/a | n/a | n/a | 0.196 | 1.38 | 1.38 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 260 | 1850 | 249 | 235 | n/a | n/a | n/a | 0.141 | 1.04 | 1.11 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 50 | 357 | 48 | 45.2 | n/a | n/a | n/a | 0.14 | 1.04 | 1.11 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1210 | 6190 | 861 | 862 | n/a | n/a | n/a | 0.195 | 1.4 | 1.4 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 384 | 2740 | 396 | 446 | n/a | n/a | n/a | 0.14 | 0.969 | 0.86 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 78.3 | 629 | 95.8 | 96 | n/a | n/a | n/a | 0.125 | 0.818 | 0.816 | n/a | n/a | n/a |

### ruby: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 220 | 264 | 86.7 | 0.835 | 2.54 | 220 | 264 | 86.7 | 0.835 | 2.54 | 213 | 255 | 81.7 | 0.834 | 2.6 |
| broken | cursor | all | 313 | 539 | 299 | 0.581 | 1.05 | 313 | 539 | 299 | 0.581 | 1.05 | 305 | 530 | 294 | 0.575 | 1.04 |
| broken | parse | 1 | 80.5 | 76.7 | 68.5 | 1.05 | 1.17 | 80.5 | 76.7 | 68.5 | 1.05 | 1.17 | 72.6 | 67.7 | 63.4 | 1.07 | 1.15 |
| broken | parse | all | 142 | 356 | 318 | 0.399 | 0.445 | 142 | 356 | 318 | 0.399 | 0.445 | 134 | 347 | 313 | 0.385 | 0.426 |
| broken | walk | 1 | 110 | 242 | 79.3 | 0.453 | 1.38 | 110 | 242 | 79.3 | 0.453 | 1.39 | 102 | 233 | 74.2 | 0.438 | 1.38 |
| broken | walk | all | 177 | 391 | 380 | 0.453 | 0.467 | 177 | 391 | 380 | 0.453 | 0.467 | 169 | 382 | 375 | 0.443 | 0.452 |
| real | cursor | 1 | 42.9 | 78.3 | 20.9 | 0.547 | 2.05 | 42.9 | 78.3 | 20.9 | 0.547 | 2.05 | 28.8 | 62.7 | 10.5 | 0.46 | 2.73 |
| real | cursor | all | 111 | 165 | 110 | 0.674 | 1.01 | 111 | 165 | 110 | 0.674 | 1.01 | 96.9 | 150 | 99.9 | 0.648 | 0.97 |
| real | parse | 1 | 26.8 | 35.6 | 20.8 | 0.752 | 1.29 | 26.8 | 35.6 | 20.8 | 0.752 | 1.29 | 12.7 | 20 | 10.3 | 0.637 | 1.24 |
| real | parse | all | 67.1 | 146 | 101 | 0.46 | 0.662 | 67.1 | 146 | 101 | 0.46 | 0.662 | 52.7 | 130 | 90.8 | 0.405 | 0.58 |
| real | walk | 1 | 27.1 | 73.4 | 20.7 | 0.369 | 1.31 | 27.1 | 73.4 | 20.7 | 0.369 | 1.31 | 13.1 | 57.8 | 10.3 | 0.227 | 1.27 |
| real | walk | all | 69.7 | 162 | 114 | 0.43 | 0.612 | 69.7 | 162 | 114 | 0.43 | 0.612 | 55.3 | 146 | 103 | 0.378 | 0.535 |

### ruby: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 81.8 | 77.3 | 68.3 | 1.06 | 1.2 | 81.8 | 77.3 | 68.3 | 1.06 | 1.2 | 74.1 | 68.3 | 63.4 | 1.09 | 1.17 |
| broken | all | 169 | 780 | 393 | 0.217 | 0.432 | 169 | 780 | 393 | 0.217 | 0.432 | 161 | 771 | 387 | 0.209 | 0.417 |
| real | 1 | 31.2 | 35.5 | 21 | 0.879 | 1.49 | 31.3 | 35.5 | 21 | 0.88 | 1.49 | 17.2 | 20.1 | 10.5 | 0.857 | 1.64 |
| real | all | 74.6 | 183 | 127 | 0.408 | 0.589 | 74.6 | 183 | 127 | 0.408 | 0.589 | 60.2 | 167 | 116 | 0.361 | 0.518 |

### ruby: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 2.15 (real), 2.36 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 2.27 | 7660 | 9.01 | 2270 | 209000 | 2270 | 209000 | 0.001 |
| real | steady | 2.15 | 95.1 | 9.01 | 2270 | 209000 | 2270 | 209000 | 0.000951 |
| broken | cold | 8.95 | 170000 | 9.57 | 4630 | 516000 | 4630 | 516000 | 0.00193 |
| broken | steady | 2.36 | 8170 | 9.57 | 4630 | 516000 | 4630 | 516000 | 0.000508 |

### ruby: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2047 B | init ms | 0.00834 | 0.0133 | 0.00561 | 0.625 | 1.49 | 15 |
| #0 2047 B | parse1 ms | 0.152 | 0.26 | 0.181 | 0.585 | 0.841 | 15 |
| #0 2047 B | init+parse1 ms | 0.164 | 0.275 | 0.187 | 0.599 | 0.879 | 15 |
| #0 2047 B | parse2 ms | 0.07 | 0.251 | 0.124 | 0.279 | 0.566 | 15 |
| #0 2047 B | process user+sys ms | 0.907 | 1.51 | 0.594 | 0.601 | 1.53 | 15 |
| #0 2047 B | exec to exit instr user+kernel M | 6.76 | 12.2 | 5.74 | 0.553 | 1.18 | 15 |
| #0 2047 B | exec to exit instr user M | 4.01 | 8.47 | 4.6 | 0.473 | 0.871 | 15 |
| #0 2047 B | exec to exit instr kernel M | 2.81 | 3.75 | 1.14 | 0.749 | 2.46 | 15 |
| #0 2047 B | exec to exit cycles user+kernel M | 4.09 | 7.05 | 2.6 | 0.58 | 1.57 | 15 |
| #0 2047 B | exec to exit task-clock ms | 0.84 | 1.43 | 0.5 | 0.587 | 1.68 | 15 |
| #0 2047 B | peak RSS MB | 4.68 | 6 | 4.26 | 0.779 | 1.1 | 15 |
| #0 2047 B | peak footprint MB | 4.71 | 6.04 | 4.2 | 0.78 | 1.12 | 15 |
| #1 1869 B | init ms | 0.00759 | 0.0128 | 0.00546 | 0.593 | 1.39 | 15 |
| #1 1869 B | parse1 ms | 0.184 | 0.307 | 0.223 | 0.6 | 0.826 | 15 |
| #1 1869 B | init+parse1 ms | 0.196 | 0.32 | 0.228 | 0.611 | 0.857 | 15 |
| #1 1869 B | parse2 ms | 0.0824 | 0.276 | 0.146 | 0.298 | 0.566 | 15 |
| #1 1869 B | process user+sys ms | 0.976 | 1.57 | 0.66 | 0.62 | 1.48 | 15 |
| #1 1869 B | exec to exit instr user+kernel M | 7.2 | 12.8 | 6.27 | 0.564 | 1.15 | 15 |
| #1 1869 B | exec to exit instr user M | 4.41 | 8.96 | 5.1 | 0.492 | 0.865 | 15 |
| #1 1869 B | exec to exit instr kernel M | 2.83 | 3.8 | 1.17 | 0.745 | 2.41 | 15 |
| #1 1869 B | exec to exit cycles user+kernel M | 4.34 | 7.49 | 2.95 | 0.579 | 1.47 | 15 |
| #1 1869 B | exec to exit task-clock ms | 0.9 | 1.51 | 0.57 | 0.596 | 1.58 | 15 |
| #1 1869 B | peak RSS MB | 4.86 | 6.11 | 4.46 | 0.794 | 1.09 | 15 |
| #1 1869 B | peak footprint MB | 4.89 | 6.15 | 4.4 | 0.795 | 1.11 | 15 |

### ruby: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1500 | 1532901 | 0 | 0 | ok |
| broken | 501 | 869290 | 425 | 0 | ok |

## rust

### rust: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 212 | 1040 | 468 | 483 | 496 | 515 | 0.203 | 0.453 | 0.439 | 0.428 | 0.412 | 215 | 1070 | 472 | 501 | 522 | 0.201 | 0.454 | 0.428 | 0.411 | 212-226 | 468-504 | 1.01 | 1.01 | 1 | 20.6 |
| broken | cursor | all | 42.5 | 206 | 90.7 | 87.2 | 88.4 | 105 | 0.206 | 0.468 | 0.487 | 0.48 | 0.405 | 44.8 | 210 | 103 | 92.3 | 106 | 0.213 | 0.435 | 0.485 | 0.422 | 42.5-48.6 | 90.7-109 | 8.63 * | 8.61 * | 7.86 * | 103 |
| broken | parse | 1 | 170 | 696 | 418 | 429 | 397 | 463 | 0.245 | 0.407 | 0.396 | 0.428 | 0.368 | 171 | 706 | 424 | 399 | 477 | 0.242 | 0.404 | 0.429 | 0.359 | 170-179 | 418-439 | 1 | 1.01 | 1 | 25.7 |
| broken | parse | all | 30.7 | 138 | 91.4 | 81.4 | 69.5 | 97.8 | 0.223 | 0.336 | 0.377 | 0.442 | 0.314 | 31.6 | 143 | 97.3 | 78.1 | 99.5 | 0.221 | 0.325 | 0.405 | 0.318 | 30.7-39.9 | 91.4-109 | 9.61 * | 8.23 * | 7.11 * | 142 |
| broken | walk | 1 | 999 | 5020 | 1310 | n/a | n/a | n/a | 0.199 | 0.762 | n/a | n/a | n/a | 1030 | 5030 | 1350 | n/a | n/a | 0.204 | 0.758 | n/a | n/a | 999-1120 | 1310-1420 | 1 | 1.03 | 1 | 4.37 |
| broken | walk | all | 809 | 4150 | 934 | n/a | n/a | n/a | 0.195 | 0.867 | n/a | n/a | n/a | 818 | 4170 | 1010 | n/a | n/a | 0.196 | 0.81 | n/a | n/a | 809-881 | 934-1190 | 1.44 * | 1.41 * | 1.67 * | 5.4 |
| real | cursor | 1 | 312 | 1430 | 615 | 642 | 694 | 672 | 0.219 | 0.506 | 0.485 | 0.449 | 0.464 | 314 | 1450 | 633 | 695 | 673 | 0.216 | 0.496 | 0.452 | 0.467 | 312-326 | 615-644 | 1 | 1.01 | 1 | 29.1 |
| real | cursor | all | 41.4 | 195 | 77 | 77.9 | 78.8 | 81.7 | 0.212 | 0.538 | 0.532 | 0.526 | 0.507 | 42.9 | 200 | 78.8 | 80.2 | 85.9 | 0.215 | 0.544 | 0.535 | 0.5 | 41.4-45 | 77-88.1 | 14 | 13.4 | 14 | 218 |
| real | parse | 1 | 243 | 879 | 522 | 544 | 528 | 575 | 0.276 | 0.466 | 0.446 | 0.46 | 0.423 | 245 | 912 | 526 | 529 | 581 | 0.269 | 0.466 | 0.463 | 0.422 | 243-261 | 522-558 | 1 | 1 | 1 | 37.2 |
| real | parse | all | 32 | 111 | 65.3 | 64.7 | 62.3 | 69.6 | 0.288 | 0.491 | 0.495 | 0.514 | 0.46 | 32.9 | 115 | 67.2 | 62.4 | 71.9 | 0.286 | 0.489 | 0.527 | 0.457 | 32-35.2 | 65.3-69.9 | 14.1 | 13.6 | 14.1 | 283 |
| real | walk | 1 | 307 | 1380 | 584 | n/a | n/a | n/a | 0.222 | 0.526 | n/a | n/a | n/a | 310 | 1410 | 598 | n/a | n/a | 0.22 | 0.518 | n/a | n/a | 307-334 | 584-626 | 1 | 1.01 | 1 | 29.4 |
| real | walk | all | 40.5 | 183 | 73.4 | n/a | n/a | n/a | 0.222 | 0.552 | n/a | n/a | n/a | 41.7 | 185 | 77.3 | n/a | n/a | 0.225 | 0.539 | n/a | n/a | 40.5-45.3 | 73.4-81.8 | 14 | 13.5 | 13.8 | 223 |
| realerr (too small) | cursor | 1 | 0.0336 | 0.113 | 0.0527 | 0.0556 | 0.0545 | 0.0591 | 0.298 | 0.638 | 0.605 | 0.616 | 0.568 | 0.0353 | 0.118 | 0.0559 | 0.0551 | 0.0637 | 0.299 | 0.631 | 0.64 | 0.554 | 0.0336-0.0559 | 0.0527-0.0677 | 1.32 | 1.11 | 1.01 | 7.65 |
| realerr (too small) | cursor | all | 0.0508 | 0.123 | 0.0753 | 0.0681 | 0.0882 | 0.124 | 0.414 | 0.675 | 0.746 | 0.576 | 0.41 | 0.115 | 0.167 | 0.129 | 0.113 | 0.126 | 0.69 | 0.888 | 1.02 | 0.911 | 0.0508-0.153 | 0.0753-0.155 | 1.32 * | 1.27 * | 1.65 * | 5.06 |
| realerr (too small) | parse | 1 | 0.0301 | 0.0769 | 0.0486 | 0.0509 | 0.0445 | 0.053 | 0.391 | 0.62 | 0.592 | 0.676 | 0.567 | 0.0323 | 0.0819 | 0.0505 | 0.0458 | 0.0554 | 0.394 | 0.639 | 0.705 | 0.583 | 0.0301-0.0508 | 0.0486-0.0609 | 1.32 | 1.16 | 0.995 | 8.54 |
| realerr (too small) | parse | all | 0.031 | 0.0839 | 0.0763 | 0.0674 | 0.0863 | 0.0784 | 0.369 | 0.406 | 0.46 | 0.359 | 0.395 | 0.0982 | 0.137 | 0.112 | 0.101 | 0.117 | 0.718 | 0.88 | 0.971 | 0.842 | 0.031-0.156 | 0.0763-0.137 | 1.33 * | 1.3 * | 1.8 * | 8.3 |
| realerr (too small) | walk | 1 | 0.0326 | 0.104 | 0.0499 | n/a | n/a | n/a | 0.312 | 0.654 | n/a | n/a | n/a | 0.0345 | 0.109 | 0.0563 | n/a | n/a | 0.317 | 0.614 | n/a | n/a | 0.0326-0.0557 | 0.0499-0.0642 | 1.29 | 1.11 | 0.994 | 7.88 |
| realerr (too small) | walk | all | 0.0543 | 0.117 | 0.0717 | n/a | n/a | n/a | 0.464 | 0.757 | n/a | n/a | n/a | 0.102 | 0.16 | 0.11 | n/a | n/a | 0.637 | 0.924 | n/a | n/a | 0.0543-0.134 | 0.0717-0.142 | 1.31 * | 1.21 * | 1.8 * | 4.73 |

### rust: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 213 | 1050 | 468 | 483 | 496 | 515 | 0.202 | 0.455 | 0.441 | 0.43 | 0.414 | 216 | 1080 | 472 | 501 | 522 | 0.201 | 0.457 | 0.431 | 0.413 | 213-227 | 468-504 |
| broken | cursor | all | 375 | 1800 | 777 | 783 | 777 | 850 | 0.208 | 0.483 | 0.479 | 0.483 | 0.442 | 390 | 1820 | 807 | 783 | 862 | 0.215 | 0.484 | 0.498 | 0.453 | 375-399 | 777-823 |
| broken | parse | 1 | 170 | 699 | 418 | 429 | 397 | 463 | 0.244 | 0.407 | 0.397 | 0.429 | 0.368 | 171 | 710 | 424 | 399 | 477 | 0.241 | 0.404 | 0.429 | 0.359 | 170-179 | 418-438 |
| broken | parse | all | 296 | 1160 | 680 | 682 | 633 | 736 | 0.256 | 0.435 | 0.434 | 0.467 | 0.402 | 310 | 1180 | 712 | 640 | 756 | 0.263 | 0.436 | 0.485 | 0.411 | 296-316 | 680-725 |
| broken | walk | 1 | 1000 | 5200 | 1310 | n/a | n/a | n/a | 0.193 | 0.763 | n/a | n/a | n/a | 1030 | 5210 | 1350 | n/a | n/a | 0.197 | 0.759 | n/a | n/a | 1000-1120 | 1310-1420 |
| broken | walk | all | 1160 | 5880 | 1630 | n/a | n/a | n/a | 0.198 | 0.712 | n/a | n/a | n/a | 1180 | 5920 | 1700 | n/a | n/a | 0.199 | 0.694 | n/a | n/a | 1160-1240 | 1630-1860 |
| real | cursor | 1 | 312 | 1430 | 615 | 642 | 694 | 672 | 0.217 | 0.507 | 0.486 | 0.449 | 0.464 | 314 | 1460 | 633 | 695 | 673 | 0.215 | 0.497 | 0.452 | 0.467 | 312-327 | 615-644 |
| real | cursor | all | 589 | 2640 | 1090 | 1080 | 1090 | 1170 | 0.223 | 0.539 | 0.544 | 0.54 | 0.505 | 601 | 2680 | 1120 | 1110 | 1190 | 0.225 | 0.536 | 0.541 | 0.507 | 589-609 | 1090-1130 |
| real | parse | 1 | 243 | 883 | 522 | 544 | 528 | 575 | 0.275 | 0.466 | 0.447 | 0.46 | 0.423 | 245 | 916 | 526 | 529 | 581 | 0.268 | 0.466 | 0.464 | 0.422 | 243-261 | 522-558 |
| real | parse | all | 449 | 1550 | 924 | 909 | 877 | 982 | 0.289 | 0.486 | 0.494 | 0.512 | 0.457 | 466 | 1570 | 946 | 891 | 995 | 0.297 | 0.493 | 0.523 | 0.468 | 449-473 | 924-964 |
| real | walk | 1 | 308 | 1400 | 584 | n/a | n/a | n/a | 0.22 | 0.527 | n/a | n/a | n/a | 310 | 1420 | 598 | n/a | n/a | 0.218 | 0.519 | n/a | n/a | 308-335 | 584-626 |
| real | walk | all | 563 | 2450 | 1020 | n/a | n/a | n/a | 0.23 | 0.55 | n/a | n/a | n/a | 586 | 2500 | 1070 | n/a | n/a | 0.235 | 0.547 | n/a | n/a | 563-593 | 1020-1080 |
| realerr (too small) | cursor | 1 | 0.04 | 0.121 | 0.053 | 0.055 | 0.054 | 0.059 | 0.331 | 0.755 | 0.727 | 0.741 | 0.678 | 0.048 | 0.131 | 0.056 | 0.055 | 0.059 | 0.366 | 0.857 | 0.873 | 0.814 | 0.04-0.073 | 0.053-0.072 |
| realerr (too small) | cursor | all | 0.081 | 0.148 | 0.151 | 0.144 | 0.179 | 0.201 | 0.547 | 0.536 | 0.563 | 0.453 | 0.403 | 0.159 | 0.207 | 0.214 | 0.195 | 0.21 | 0.768 | 0.743 | 0.815 | 0.757 | 0.081-0.193 | 0.151-0.251 |
| realerr (too small) | parse | 1 | 0.037 | 0.088 | 0.048 | 0.05 | 0.044 | 0.053 | 0.42 | 0.771 | 0.74 | 0.841 | 0.698 | 0.044 | 0.093 | 0.05 | 0.045 | 0.055 | 0.473 | 0.88 | 0.978 | 0.8 | 0.037-0.07 | 0.048-0.062 |
| realerr (too small) | parse | all | 0.046 | 0.099 | 0.151 | 0.134 | 0.168 | 0.166 | 0.465 | 0.305 | 0.343 | 0.274 | 0.277 | 0.129 | 0.173 | 0.201 | 0.181 | 0.221 | 0.746 | 0.642 | 0.713 | 0.584 | 0.046-0.195 | 0.151-0.232 |
| realerr (too small) | walk | 1 | 0.038 | 0.114 | 0.05 | n/a | n/a | n/a | 0.333 | 0.76 | n/a | n/a | n/a | 0.044 | 0.12 | 0.054 | n/a | n/a | 0.367 | 0.815 | n/a | n/a | 0.038-0.076 | 0.05-0.067 |
| realerr (too small) | walk | all | 0.076 | 0.134 | 0.15 | n/a | n/a | n/a | 0.567 | 0.507 | n/a | n/a | n/a | 0.13 | 0.193 | 0.196 | n/a | n/a | 0.674 | 0.663 | n/a | n/a | 0.076-0.182 | 0.15-0.228 |

### rust: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 4880 | 19100 | 8800 | 8670 | 8470 | 9120 | 0.256 | 0.555 | 0.563 | 0.576 | 0.535 | 4880 | 19100 | 8800 | 8480 | 9120 | 0.255 | 0.555 | 0.576 | 0.535 | 4880-4890 | 8800-8800 |
| broken | cursor | all | 4900 | 19200 | 8810 | 8690 | 8500 | 9140 | 0.255 | 0.556 | 0.564 | 0.576 | 0.536 | 4910 | 19200 | 8830 | 8510 | 9150 | 0.256 | 0.557 | 0.578 | 0.537 | 4900-5030 | 8810-8850 |
| broken | parse | 1 | 3840 | 12000 | 7620 | 7460 | 7170 | 7890 | 0.321 | 0.505 | 0.515 | 0.536 | 0.487 | 3840 | 12000 | 7630 | 7180 | 7900 | 0.321 | 0.504 | 0.535 | 0.487 | 3840-3840 | 7620-7630 |
| broken | parse | all | 3860 | 11900 | 7650 | 7490 | 7220 | 7930 | 0.323 | 0.504 | 0.515 | 0.535 | 0.486 | 3870 | 12000 | 7670 | 7230 | 7940 | 0.323 | 0.505 | 0.535 | 0.487 | 3860-3950 | 7650-7700 |
| broken | walk | 1 | 26000 | 68300 | 34800 | n/a | n/a | n/a | 0.38 | 0.745 | n/a | n/a | n/a | 26000 | 68400 | 34900 | n/a | n/a | 0.38 | 0.745 | n/a | n/a | 26000-26000 | 34800-34900 |
| broken | walk | all | 26000 | 68500 | 34900 | n/a | n/a | n/a | 0.38 | 0.746 | n/a | n/a | n/a | 26000 | 68500 | 34900 | n/a | n/a | 0.38 | 0.746 | n/a | n/a | 26000-26100 | 34900-34900 |
| real | cursor | 1 | 7250 | 26200 | 10800 | 10700 | 10600 | 11300 | 0.277 | 0.671 | 0.679 | 0.685 | 0.642 | 7250 | 26200 | 10800 | 10600 | 11300 | 0.277 | 0.671 | 0.685 | 0.642 | 7250-7250 | 10800-10800 |
| real | cursor | all | 7250 | 26200 | 10800 | 10700 | 10600 | 11300 | 0.276 | 0.67 | 0.678 | 0.684 | 0.641 | 7250 | 26300 | 10800 | 10600 | 11300 | 0.276 | 0.67 | 0.683 | 0.64 | 7250-7360 | 10800-10800 |
| real | parse | 1 | 5590 | 14600 | 8930 | 8750 | 8520 | 9330 | 0.383 | 0.627 | 0.639 | 0.657 | 0.599 | 5590 | 14600 | 8930 | 8520 | 9340 | 0.383 | 0.626 | 0.656 | 0.599 | 5590-5590 | 8930-8940 |
| real | parse | all | 5590 | 14600 | 8900 | 8730 | 8510 | 9320 | 0.383 | 0.628 | 0.641 | 0.658 | 0.6 | 5590 | 14600 | 8920 | 8510 | 9330 | 0.383 | 0.627 | 0.657 | 0.599 | 5590-5590 | 8900-8930 |
| real | walk | 1 | 7090 | 23600 | 10100 | n/a | n/a | n/a | 0.301 | 0.7 | n/a | n/a | n/a | 7090 | 23600 | 10100 | n/a | n/a | 0.301 | 0.7 | n/a | n/a | 7090-7090 | 10100-10100 |
| real | walk | all | 7090 | 23600 | 10100 | n/a | n/a | n/a | 0.3 | 0.702 | n/a | n/a | n/a | 7090 | 23600 | 10100 | n/a | n/a | 0.3 | 0.701 | n/a | n/a | 7090-7090 | 10100-10100 |
| realerr (too small) | cursor | 1 | 0.724 | 2.08 | 0.986 | 0.97 | 0.934 | 1.03 | 0.348 | 0.734 | 0.746 | 0.775 | 0.701 | 0.727 | 2.08 | 0.986 | 0.934 | 1.03 | 0.349 | 0.737 | 0.778 | 0.704 | 0.724-0.749 | 0.986-0.986 |
| realerr (too small) | cursor | all | 0.778 | 2.03 | 0.986 | 0.97 | 0.935 | 1.03 | 0.382 | 0.788 | 0.802 | 0.832 | 0.753 | 1.24 | 2.13 | 0.986 | 0.935 | 1.03 | 0.58 | 1.25 | 1.32 | 1.2 | 0.778-1.27 | 0.986-0.987 |
| realerr (too small) | parse | 1 | 0.635 | 1.37 | 0.887 | 0.868 | 0.825 | 0.928 | 0.463 | 0.716 | 0.731 | 0.769 | 0.684 | 0.637 | 1.37 | 0.887 | 0.825 | 0.928 | 0.464 | 0.718 | 0.772 | 0.686 | 0.635-0.661 | 0.887-0.888 |
| realerr (too small) | parse | all | 0.674 | 1.32 | 0.887 | 0.868 | 0.826 | 0.928 | 0.509 | 0.76 | 0.776 | 0.817 | 0.726 | 1.12 | 1.35 | 0.888 | 0.826 | 0.928 | 0.832 | 1.26 | 1.36 | 1.21 | 0.674-1.23 | 0.887-0.894 |
| realerr (too small) | walk | 1 | 0.707 | 1.89 | 0.944 | n/a | n/a | n/a | 0.375 | 0.749 | n/a | n/a | n/a | 0.71 | 1.89 | 0.944 | n/a | n/a | 0.375 | 0.752 | n/a | n/a | 0.707-0.729 | 0.944-0.945 |
| realerr (too small) | walk | all | 0.768 | 1.84 | 0.945 | n/a | n/a | n/a | 0.417 | 0.813 | n/a | n/a | n/a | 1.22 | 1.87 | 0.945 | n/a | n/a | 0.654 | 1.29 | n/a | n/a | 0.768-1.29 | 0.945-0.951 |

### rust: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1110 | 5440 | 2450 | 2520 | 2590 | 2690 | 0.204 | 0.454 | 0.44 | 0.428 | 0.413 | 1120 | 5540 | 2470 | 2620 | 2720 | 0.202 | 0.454 | 0.429 | 0.411 | 1110-1170 | 2450-2630 |
| broken | cursor | all | 1720 | 8090 | 3600 | 3640 | 3560 | 3880 | 0.213 | 0.478 | 0.473 | 0.483 | 0.444 | 1790 | 8160 | 3700 | 3600 | 3950 | 0.219 | 0.484 | 0.498 | 0.453 | 1720-1820 | 3600-3730 |
| broken | parse | 1 | 887 | 3610 | 2170 | 2220 | 2060 | 2400 | 0.246 | 0.41 | 0.399 | 0.432 | 0.37 | 894 | 3660 | 2190 | 2060 | 2470 | 0.244 | 0.408 | 0.434 | 0.362 | 887-934 | 2170-2270 |
| broken | parse | all | 1370 | 5260 | 3160 | 3160 | 2900 | 3400 | 0.26 | 0.433 | 0.433 | 0.471 | 0.402 | 1430 | 5350 | 3250 | 2920 | 3470 | 0.267 | 0.439 | 0.489 | 0.411 | 1370-1450 | 3160-3320 |
| broken | walk | 1 | 5220 | 26300 | 6820 | n/a | n/a | n/a | 0.199 | 0.765 | n/a | n/a | n/a | 5350 | 26400 | 7040 | n/a | n/a | 0.203 | 0.76 | n/a | n/a | 5220-5870 | 6820-7390 |
| broken | walk | all | 5840 | 28800 | 8020 | n/a | n/a | n/a | 0.203 | 0.727 | n/a | n/a | n/a | 5920 | 29000 | 8390 | n/a | n/a | 0.204 | 0.705 | n/a | n/a | 5840-6240 | 8020-9270 |
| real | cursor | 1 | 1620 | 7450 | 3210 | 3350 | 3620 | 3510 | 0.218 | 0.506 | 0.485 | 0.448 | 0.463 | 1640 | 7600 | 3300 | 3630 | 3520 | 0.216 | 0.497 | 0.452 | 0.467 | 1620-1700 | 3210-3360 |
| real | cursor | all | 2630 | 11700 | 4910 | 4900 | 4980 | 5260 | 0.226 | 0.536 | 0.537 | 0.529 | 0.501 | 2680 | 11800 | 5010 | 5050 | 5350 | 0.227 | 0.534 | 0.53 | 0.501 | 2630-2720 | 4910-5040 |
| real | parse | 1 | 1270 | 4590 | 2720 | 2840 | 2760 | 3000 | 0.277 | 0.466 | 0.447 | 0.46 | 0.423 | 1280 | 4760 | 2750 | 2760 | 3030 | 0.269 | 0.466 | 0.463 | 0.422 | 1270-1360 | 2720-2910 |
| real | parse | all | 2030 | 6930 | 4180 | 4140 | 3980 | 4470 | 0.293 | 0.485 | 0.489 | 0.509 | 0.454 | 2090 | 6980 | 4280 | 4030 | 4500 | 0.299 | 0.488 | 0.519 | 0.464 | 2030-2110 | 4180-4340 |
| real | walk | 1 | 1600 | 7240 | 3050 | n/a | n/a | n/a | 0.222 | 0.526 | n/a | n/a | n/a | 1620 | 7360 | 3120 | n/a | n/a | 0.22 | 0.518 | n/a | n/a | 1600-1750 | 3050-3260 |
| real | walk | all | 2530 | 10900 | 4670 | n/a | n/a | n/a | 0.232 | 0.542 | n/a | n/a | n/a | 2610 | 11000 | 4840 | n/a | n/a | 0.237 | 0.54 | n/a | n/a | 2530-2640 | 4670-4870 |
| realerr (too small) | cursor | 1 | 0.179 | 0.59 | 0.254 | 0.263 | 0.257 | 0.283 | 0.303 | 0.703 | 0.68 | 0.696 | 0.631 | 0.184 | 0.612 | 0.26 | 0.261 | 0.286 | 0.301 | 0.711 | 0.707 | 0.645 | 0.179-0.279 | 0.254-0.313 |
| realerr (too small) | cursor | all | 0.304 | 0.656 | 0.342 | 0.318 | 0.371 | 0.557 | 0.463 | 0.888 | 0.954 | 0.818 | 0.545 | 0.581 | 0.898 | 0.53 | 0.472 | 0.581 | 0.647 | 1.1 | 1.23 | 0.999 | 0.304-0.709 | 0.342-0.711 |
| realerr (too small) | parse | 1 | 0.161 | 0.409 | 0.23 | 0.24 | 0.21 | 0.258 | 0.395 | 0.702 | 0.673 | 0.768 | 0.626 | 0.17 | 0.429 | 0.235 | 0.214 | 0.26 | 0.397 | 0.726 | 0.797 | 0.655 | 0.161-0.25 | 0.23-0.29 |
| realerr (too small) | parse | all | 0.181 | 0.453 | 0.385 | 0.337 | 0.356 | 0.347 | 0.4 | 0.471 | 0.539 | 0.509 | 0.523 | 0.5 | 0.699 | 0.474 | 0.444 | 0.498 | 0.715 | 1.05 | 1.13 | 1 | 0.181-0.694 | 0.385-0.634 |
| realerr (too small) | walk | 1 | 0.172 | 0.543 | 0.243 | n/a | n/a | n/a | 0.317 | 0.709 | n/a | n/a | n/a | 0.185 | 0.569 | 0.25 | n/a | n/a | 0.325 | 0.74 | n/a | n/a | 0.172-0.291 | 0.243-0.301 |
| realerr (too small) | walk | all | 0.303 | 0.607 | 0.338 | n/a | n/a | n/a | 0.5 | 0.899 | n/a | n/a | n/a | 0.475 | 0.825 | 0.497 | n/a | n/a | 0.576 | 0.956 | n/a | n/a | 0.303-0.688 | 0.338-0.633 |

### rust: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1040 | 7140 | 1180 | 1210 | 1300 | 1230 | 0.146 | 0.884 | 0.858 | 0.8 | 0.842 |
| broken | cursor minus parse | 1 | cycles M | 222 | 1820 | 280 | 302 | 534 | 292 | 0.122 | 0.792 | 0.734 | 0.416 | 0.761 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 42.9 | 354 | 50 | 54 | 98.7 | 52.6 | 0.121 | 0.858 | 0.794 | 0.434 | 0.815 |
| broken | cursor minus parse | all | instructions M | 1040 | 7250 | 1160 | 1190 | 1280 | 1210 | 0.143 | 0.899 | 0.872 | 0.81 | 0.862 |
| broken | cursor minus parse | all | cycles M | 355 | 2830 | 443 | 483 | 661 | 480 | 0.125 | 0.8 | 0.735 | 0.537 | 0.739 |
| broken | cursor minus parse | all | CPU user+sys ms | 79.1 | 646 | 97.3 | 101 | 144 | 114 | 0.122 | 0.813 | 0.784 | 0.55 | 0.694 |
| broken | walk minus parse | 1 | instructions M | 22100 | 56400 | 27200 | n/a | n/a | n/a | 0.393 | 0.813 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 4340 | 22700 | 4660 | n/a | n/a | n/a | 0.191 | 0.93 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 830 | 4500 | 892 | n/a | n/a | n/a | 0.185 | 0.93 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 22200 | 56500 | 27200 | n/a | n/a | n/a | 0.392 | 0.814 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 4470 | 23500 | 4860 | n/a | n/a | n/a | 0.19 | 0.919 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 866 | 4720 | 951 | n/a | n/a | n/a | 0.183 | 0.91 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1660 | 11600 | 1870 | 1930 | 2070 | 1970 | 0.143 | 0.885 | 0.859 | 0.802 | 0.843 |
| real | cursor minus parse | 1 | cycles M | 355 | 2860 | 489 | 510 | 866 | 507 | 0.124 | 0.726 | 0.696 | 0.41 | 0.699 |
| real | cursor minus parse | 1 | CPU user+sys ms | 68.6 | 552 | 93.5 | 97.3 | 166 | 97 | 0.124 | 0.733 | 0.705 | 0.413 | 0.707 |
| real | cursor minus parse | all | instructions M | 1660 | 11600 | 1910 | 1970 | 2090 | 2000 | 0.142 | 0.866 | 0.842 | 0.794 | 0.828 |
| real | cursor minus parse | all | cycles M | 606 | 4730 | 726 | 759 | 992 | 791 | 0.128 | 0.834 | 0.798 | 0.611 | 0.766 |
| real | cursor minus parse | all | CPU user+sys ms | 140 | 1090 | 170 | 174 | 215 | 184 | 0.129 | 0.827 | 0.806 | 0.654 | 0.761 |
| real | walk minus parse | 1 | instructions M | 1500 | 8940 | 1210 | n/a | n/a | n/a | 0.168 | 1.24 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 335 | 2650 | 326 | n/a | n/a | n/a | 0.126 | 1.03 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 64.4 | 513 | 62.2 | n/a | n/a | n/a | 0.126 | 1.04 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1500 | 9010 | 1210 | n/a | n/a | n/a | 0.166 | 1.24 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 506 | 3980 | 488 | n/a | n/a | n/a | 0.127 | 1.04 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 114 | 897 | 101 | n/a | n/a | n/a | 0.128 | 1.13 | n/a | n/a | n/a |
| realerr (too small) | cursor minus parse | 1 | instructions M | 0.089 | 0.709 | 0.0989 | 0.102 | 0.109 | 0.105 | 0.126 | 0.9 | 0.874 | 0.813 | 0.85 |
| realerr (too small) | cursor minus parse | 1 | cycles M | 0.0175 | 0.182 | 0.0243 | 0.0231 | 0.0468 | 0.0254 | 0.0962 | 0.717 | 0.757 | 0.373 | 0.689 |
| realerr (too small) | cursor minus parse | 1 | CPU user+sys ms | 0.003 | 0.033 | 0.005 | 0.005 | 0.01 | 0.006 | 0.0909 | 0.6 | 0.6 | 0.3 | 0.5 |
| realerr (too small) | cursor minus parse | all | instructions M | 0.104 | 0.71 | 0.099 | 0.102 | 0.109 | 0.105 | 0.146 | 1.05 | 1.02 | 0.946 | 0.989 |
| realerr (too small) | cursor minus parse | all | cycles M | 0.122 | 0.202 | n/a | n/a | 0.0148 | 0.21 | 0.604 | n/a | n/a | 8.26 | 0.583 |
| realerr (too small) | cursor minus parse | all | CPU user+sys ms | 0.035 | 0.049 | n/a | 0.01 | 0.011 | 0.035 | 0.714 | n/a | 3.5 | 3.18 | 1 |
| realerr (too small) | walk minus parse | 1 | instructions M | 0.0727 | 0.518 | 0.0573 | n/a | n/a | n/a | 0.14 | 1.27 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | cycles M | 0.0107 | 0.135 | 0.0128 | n/a | n/a | n/a | 0.0792 | 0.837 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | 1 | CPU user+sys ms | 0.001 | 0.026 | 0.002 | n/a | n/a | n/a | 0.0385 | 0.5 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | instructions M | 0.0937 | 0.515 | 0.0574 | n/a | n/a | n/a | 0.182 | 1.63 | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | cycles M | 0.122 | 0.153 | n/a | n/a | n/a | n/a | 0.794 | n/a | n/a | n/a | n/a |
| realerr (too small) | walk minus parse | all | CPU user+sys ms | 0.03 | 0.035 | n/a | n/a | n/a | n/a | 0.857 | n/a | n/a | n/a | n/a |

### rust: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 118 | 226 | 52.1 | 0.522 | 2.26 | 118 | 226 | 52.1 | 0.522 | 2.26 | 109 | 216 | 45.7 | 0.504 | 2.39 |
| broken | cursor | all | 204 | 450 | 293 | 0.453 | 0.697 | 204 | 450 | 293 | 0.453 | 0.697 | 194 | 440 | 286 | 0.442 | 0.68 |
| broken | parse | 1 | 52.8 | 51.5 | 42 | 1.03 | 1.26 | 52.8 | 51.5 | 42 | 1.03 | 1.26 | 43.8 | 41.3 | 35.5 | 1.06 | 1.23 |
| broken | parse | all | 124 | 295 | 255 | 0.421 | 0.487 | 124 | 295 | 255 | 0.421 | 0.487 | 115 | 285 | 249 | 0.402 | 0.461 |
| broken | walk | 1 | 81.5 | 169 | 53.7 | 0.483 | 1.52 | 81.5 | 169 | 53.7 | 0.483 | 1.52 | 72.5 | 158 | 47.3 | 0.458 | 1.53 |
| broken | walk | all | 163 | 335 | 308 | 0.489 | 0.531 | 163 | 335 | 308 | 0.489 | 0.531 | 154 | 324 | 301 | 0.474 | 0.51 |
| real | cursor | 1 | 40.2 | 75.2 | 21.1 | 0.535 | 1.9 | 40.2 | 75.2 | 21.1 | 0.535 | 1.9 | 25.7 | 59.1 | 9.97 | 0.435 | 2.58 |
| real | cursor | all | 81.3 | 134 | 83 | 0.608 | 0.98 | 81.4 | 134 | 83 | 0.608 | 0.98 | 66.6 | 118 | 71.9 | 0.565 | 0.926 |
| real | parse | 1 | 25.2 | 35 | 19.9 | 0.72 | 1.27 | 25.2 | 35 | 19.9 | 0.721 | 1.27 | 10.7 | 19.1 | 8.87 | 0.561 | 1.21 |
| real | parse | all | 64.9 | 114 | 85.9 | 0.572 | 0.756 | 64.9 | 114 | 85.9 | 0.572 | 0.756 | 50 | 97.4 | 74.8 | 0.513 | 0.669 |
| real | walk | 1 | 25.5 | 63.5 | 21.1 | 0.401 | 1.21 | 25.5 | 63.5 | 21.1 | 0.401 | 1.21 | 11 | 47.8 | 10.1 | 0.231 | 1.09 |
| real | walk | all | 63.9 | 133 | 87 | 0.482 | 0.734 | 63.9 | 133 | 87 | 0.482 | 0.735 | 49 | 117 | 75.9 | 0.42 | 0.646 |
| realerr | cursor | 1 | 5.27 | 6.74 | 4.91 | 0.781 | 1.07 | 5.27 | 6.74 | 4.91 | 0.782 | 1.07 | 2.03 | 1.91 | 2.55 | 1.06 | 0.794 |
| realerr | cursor | all | 6.4 | 7.23 | 4.58 | 0.885 | 1.4 | 6.43 | 7.23 | 4.58 | 0.888 | 1.4 | 3.14 | 2.43 | 2.2 | 1.29 | 1.43 |
| realerr | parse | 1 | 5.24 | 6.32 | 3.93 | 0.829 | 1.33 | 5.25 | 6.33 | 3.93 | 0.829 | 1.33 | 2 | 1.54 | 1.6 | 1.29 | 1.25 |
| realerr | parse | all | 6.68 | 6.89 | 4.18 | 0.969 | 1.6 | 6.7 | 6.89 | 4.18 | 0.973 | 1.61 | 3.42 | 2.15 | 1.6 | 1.59 | 2.13 |
| realerr | walk | 1 | 5.26 | 6.54 | 4.74 | 0.805 | 1.11 | 5.27 | 6.54 | 4.74 | 0.805 | 1.11 | 2.03 | 1.84 | 2.45 | 1.1 | 0.826 |
| realerr | walk | all | 6.52 | 7.14 | 5.06 | 0.913 | 1.29 | 6.53 | 7.15 | 5.06 | 0.914 | 1.29 | 3.27 | 2.35 | 2.6 | 1.39 | 1.26 |

### rust: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 52.7 | 51.9 | 41.8 | 1.01 | 1.26 | 52.7 | 51.9 | 41.8 | 1.01 | 1.26 | 43.6 | 41.6 | 35.2 | 1.05 | 1.24 |
| broken | all | 153 | 464 | 352 | 0.329 | 0.433 | 153 | 464 | 352 | 0.329 | 0.433 | 143 | 453 | 346 | 0.315 | 0.413 |
| real | 1 | 25.2 | 38.6 | 20 | 0.653 | 1.26 | 25.2 | 38.6 | 20 | 0.653 | 1.26 | 10.7 | 22.6 | 8.83 | 0.472 | 1.21 |
| real | all | 66.4 | 145 | 85.8 | 0.458 | 0.774 | 66.4 | 145 | 85.8 | 0.458 | 0.774 | 51.6 | 129 | 74.7 | 0.4 | 0.691 |
| realerr | 1 | 5.12 | 6.18 | 3.6 | 0.828 | 1.42 | 5.12 | 6.18 | 3.6 | 0.828 | 1.42 | 1.97 | 1.51 | 1.3 | 1.31 | 1.52 |
| realerr | all | 6.8 | 7.35 | 4.09 | 0.926 | 1.66 | 6.83 | 7.35 | 4.09 | 0.929 | 1.67 | 3.54 | 2.6 | 1.67 | 1.36 | 2.12 |

### rust: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 1.02 (real), 12 (realerr), 1.09 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 1.15 | 8880 | 8.02 | 2570 | 248000 | 2570 | 248000 | 0.00045 |
| real | steady | 1.02 | 4.95 | 8.01 | 2570 | 248000 | 2570 | 248000 | 0.000398 |
| realerr | cold | 62 | 171000 | 18 | 258 | 26100 | 258 | 26100 | 0.24 |
| realerr | steady | 12 | 240 | 16 | 237 | 21400 | 237 | 21400 | 0.0506 |
| broken | cold | 5.96 | 105000 | 8.43 | 4330 | 494000 | 4330 | 494000 | 0.00138 |
| broken | steady | 1.09 | 6280 | 8.41 | 4330 | 493000 | 4330 | 493000 | 0.000253 |

### rust: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2052 B | init ms | 0.0089 | 0.0124 | 0.00553 | 0.719 | 1.61 | 15 |
| #0 2052 B | parse1 ms | 0.17 | 0.248 | 0.195 | 0.686 | 0.873 | 15 |
| #0 2052 B | init+parse1 ms | 0.179 | 0.261 | 0.2 | 0.685 | 0.894 | 15 |
| #0 2052 B | parse2 ms | 0.0636 | 0.178 | 0.109 | 0.357 | 0.583 | 15 |
| #0 2052 B | process user+sys ms | 0.927 | 1.42 | 0.593 | 0.652 | 1.56 | 15 |
| #0 2052 B | exec to exit instr user+kernel M | 6.22 | 9.81 | 4.73 | 0.634 | 1.31 | 15 |
| #0 2052 B | exec to exit instr user M | 3.49 | 6.07 | 3.68 | 0.576 | 0.949 | 15 |
| #0 2052 B | exec to exit instr kernel M | 2.72 | 3.73 | 1.05 | 0.729 | 2.61 | 15 |
| #0 2052 B | exec to exit cycles user+kernel M | 4.11 | 6.64 | 2.58 | 0.619 | 1.59 | 15 |
| #0 2052 B | exec to exit task-clock ms | 0.85 | 1.34 | 0.5 | 0.634 | 1.7 | 15 |
| #0 2052 B | peak RSS MB | 4.82 | 5.92 | 3.76 | 0.815 | 1.28 | 15 |
| #0 2052 B | peak footprint MB | 4.86 | 5.95 | 3.7 | 0.816 | 1.31 | 15 |
| #1 3520 B | init ms | 0.00845 | 0.012 | 0.00611 | 0.705 | 1.38 | 15 |
| #1 3520 B | parse1 ms | 0.211 | 0.338 | 0.256 | 0.624 | 0.823 | 15 |
| #1 3520 B | init+parse1 ms | 0.227 | 0.35 | 0.263 | 0.647 | 0.864 | 15 |
| #1 3520 B | parse2 ms | 0.0971 | 0.287 | 0.168 | 0.338 | 0.578 | 15 |
| #1 3520 B | process user+sys ms | 1.01 | 1.61 | 0.715 | 0.628 | 1.42 | 15 |
| #1 3520 B | exec to exit instr user+kernel M | 7.82 | 13.1 | 6.87 | 0.597 | 1.14 | 15 |
| #1 3520 B | exec to exit instr user M | 5.04 | 9.31 | 5.79 | 0.542 | 0.872 | 15 |
| #1 3520 B | exec to exit instr kernel M | 2.78 | 3.78 | 1.09 | 0.736 | 2.55 | 15 |
| #1 3520 B | exec to exit cycles user+kernel M | 4.56 | 7.77 | 3.22 | 0.587 | 1.42 | 15 |
| #1 3520 B | exec to exit task-clock ms | 0.94 | 1.55 | 0.62 | 0.606 | 1.52 | 15 |
| #1 3520 B | peak RSS MB | 4.87 | 5.89 | 3.7 | 0.826 | 1.32 | 15 |
| #1 3520 B | peak footprint MB | 4.9 | 5.93 | 3.7 | 0.827 | 1.32 | 15 |

### rust: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1042 | 2269545 | 0 | 0 | ok |
| realerr | 1 | 141 | 1 | 0 | ok |
| broken | 501 | 1405274 | 443 | 0 | ok |

## scala

### scala: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 182 | 784 | 346 | 353 | 357 | 372 | 0.232 | 0.525 | 0.515 | 0.51 | 0.489 | 186 | 803 | 349 | 358 | 374 | 0.232 | 0.533 | 0.52 | 0.498 | 182-203 | 346-379 | 1 | 1.11 | 1 | 12.7 |
| broken | cursor | all | 22.6 | 182 | 42.2 | 41.4 | 40.8 | 44.8 | 0.124 | 0.536 | 0.546 | 0.554 | 0.505 | 24.8 | 189 | 44.2 | 41.3 | 45.2 | 0.131 | 0.56 | 0.599 | 0.548 | 22.6-26.2 | 42.2-46.3 | 14.3 | 9.45 * | 14.7 | 102 |
| broken | parse | 1 | 165 | 647 | 324 | 328 | 323 | 354 | 0.254 | 0.508 | 0.501 | 0.51 | 0.464 | 171 | 672 | 327 | 328 | 359 | 0.255 | 0.524 | 0.523 | 0.478 | 165-181 | 324-357 | 1 | 1.12 | 1 | 14 |
| broken | parse | all | 20.5 | 161 | 39.7 | 38.4 | 37.9 | 42.3 | 0.127 | 0.516 | 0.534 | 0.54 | 0.485 | 21.6 | 167 | 41.5 | 39 | 43.3 | 0.129 | 0.519 | 0.552 | 0.497 | 20.5-23.7 | 39.7-43.4 | 14.3 | 9.33 * | 14.5 | 113 |
| broken | walk | 1 | 243 | 1070 | 406 | n/a | n/a | n/a | 0.228 | 0.599 | n/a | n/a | n/a | 245 | 1090 | 413 | n/a | n/a | 0.225 | 0.593 | n/a | n/a | 243-280 | 406-431 | 1 | 1.08 | 1 | 9.49 |
| broken | walk | all | 66.3 | 360 | 86.5 | n/a | n/a | n/a | 0.184 | 0.766 | n/a | n/a | n/a | 68 | 374 | 91.3 | n/a | n/a | 0.182 | 0.745 | n/a | n/a | 66.3-79.5 | 86.5-98 | 6 * | 5.46 * | 7.77 * | 34.8 |
| real | cursor | 1 | 312 | 1480 | 561 | 574 | 596 | 630 | 0.211 | 0.555 | 0.543 | 0.523 | 0.495 | 316 | 1540 | 566 | 596 | 636 | 0.205 | 0.559 | 0.53 | 0.497 | 312-348 | 561-591 | 1 | 1.13 | 1 | 13.6 |
| real | cursor | all | 41.9 | 423 | 74.9 | 72.5 | 75.3 | 82.2 | 0.0991 | 0.56 | 0.578 | 0.557 | 0.51 | 43.5 | 441 | 76.7 | 76.3 | 83.5 | 0.0987 | 0.568 | 0.57 | 0.522 | 41.9-53.3 | 74.9-83.7 | 12.9 | 7.74 * | 13.5 | 101 |
| real | parse | 1 | 277 | 1260 | 518 | 525 | 523 | 576 | 0.22 | 0.535 | 0.528 | 0.53 | 0.481 | 280 | 1280 | 524 | 526 | 577 | 0.218 | 0.533 | 0.531 | 0.485 | 277-317 | 518-531 | 1 | 1.14 | 1 | 15.2 |
| real | parse | all | 36.9 | 339 | 69.4 | 66.9 | 66.6 | 73.5 | 0.109 | 0.533 | 0.552 | 0.554 | 0.503 | 38.4 | 403 | 70.8 | 66.9 | 75.1 | 0.0952 | 0.543 | 0.574 | 0.511 | 36.9-41.9 | 69.4-75.8 | 13.3 | 7.61 * | 13.4 | 114 |
| real | walk | 1 | 304 | 1420 | 540 | n/a | n/a | n/a | 0.214 | 0.562 | n/a | n/a | n/a | 310 | 1520 | 547 | n/a | n/a | 0.204 | 0.568 | n/a | n/a | 304-347 | 540-573 | 1 | 1.12 | 1 | 13.9 |
| real | walk | all | 40.8 | 412 | 72.5 | n/a | n/a | n/a | 0.0991 | 0.563 | n/a | n/a | n/a | 42.8 | 432 | 75.2 | n/a | n/a | 0.099 | 0.569 | n/a | n/a | 40.8-49.8 | 72.5-81.7 | 12.8 | 7.72 * | 13.2 | 103 |
| realerr | cursor | 1 | 54.3 | 262 | 100 | 103 | 107 | 109 | 0.207 | 0.543 | 0.529 | 0.507 | 0.499 | 55 | 302 | 102 | 110 | 110 | 0.182 | 0.538 | 0.502 | 0.5 | 54.3-59.8 | 100-112 | 1 | 1.18 | 1 | 12.8 |
| realerr | cursor | all | 8.98 | 122 | 13.1 | 15.1 | 15.1 | 16.9 | 0.0738 | 0.683 | 0.593 | 0.596 | 0.531 | 9.86 | 156 | 17.1 | 16.3 | 18 | 0.0633 | 0.578 | 0.606 | 0.549 | 8.98-10.2 | 13.1-18.5 | 9.26 * | 4.71 * | 9.77 * | 77.7 |
| realerr | parse | 1 | 48.6 | 223 | 92.2 | 96.9 | 91.1 | 103 | 0.218 | 0.527 | 0.502 | 0.534 | 0.474 | 48.9 | 231 | 92.8 | 91.6 | 105 | 0.211 | 0.527 | 0.534 | 0.464 | 48.6-50.3 | 92.2-107 | 1 | 1.22 | 1 | 14.3 |
| realerr | parse | all | 7.79 | 109 | 13.7 | 14.4 | 13.9 | 15.5 | 0.0713 | 0.568 | 0.54 | 0.56 | 0.502 | 8.6 | 143 | 15.4 | 15.8 | 16.7 | 0.0602 | 0.558 | 0.543 | 0.515 | 7.79-10.3 | 13.7-18.9 | 9.17 * | 4.6 * | 9.54 * | 89.5 |
| realerr | walk | 1 | 53.4 | 256 | 96.3 | n/a | n/a | n/a | 0.209 | 0.554 | n/a | n/a | n/a | 56.1 | 298 | 97 | n/a | n/a | 0.188 | 0.578 | n/a | n/a | 53.4-57.2 | 96.3-98.6 | 1 | 1.18 | 1 | 13.1 |
| realerr | walk | all | 8.74 | 123 | 15.5 | n/a | n/a | n/a | 0.0712 | 0.565 | n/a | n/a | n/a | 9.39 | 152 | 16.4 | n/a | n/a | 0.0616 | 0.573 | n/a | n/a | 8.74-10.2 | 15.5-19.5 | 9.19 * | 4.77 * | 9.68 * | 79.9 |

### scala: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 182 | 869 | 346 | 353 | 357 | 372 | 0.209 | 0.526 | 0.516 | 0.51 | 0.489 | 186 | 887 | 349 | 358 | 374 | 0.21 | 0.533 | 0.521 | 0.498 | 182-203 | 346-379 |
| broken | cursor | all | 340 | 1730 | 624 | 615 | 606 | 671 | 0.196 | 0.545 | 0.553 | 0.561 | 0.507 | 348 | 1790 | 653 | 620 | 687 | 0.194 | 0.532 | 0.561 | 0.506 | 340-354 | 624-662 |
| broken | parse | 1 | 165 | 729 | 324 | 328 | 323 | 354 | 0.226 | 0.509 | 0.502 | 0.511 | 0.465 | 172 | 752 | 327 | 328 | 359 | 0.228 | 0.524 | 0.523 | 0.478 | 165-182 | 324-357 |
| broken | parse | all | 298 | 1520 | 574 | 566 | 545 | 615 | 0.196 | 0.52 | 0.527 | 0.548 | 0.485 | 313 | 1560 | 601 | 561 | 626 | 0.2 | 0.521 | 0.557 | 0.5 | 298-319 | 574-615 |
| broken | walk | 1 | 243 | 1160 | 406 | n/a | n/a | n/a | 0.21 | 0.599 | n/a | n/a | n/a | 245 | 1180 | 413 | n/a | n/a | 0.208 | 0.593 | n/a | n/a | 243-280 | 406-431 |
| broken | walk | all | 404 | 2010 | 678 | n/a | n/a | n/a | 0.201 | 0.596 | n/a | n/a | n/a | 416 | 2050 | 708 | n/a | n/a | 0.203 | 0.587 | n/a | n/a | 404-425 | 678-724 |
| real | cursor | 1 | 312 | 1670 | 561 | 574 | 595 | 630 | 0.187 | 0.556 | 0.544 | 0.524 | 0.495 | 316 | 1730 | 565 | 596 | 636 | 0.182 | 0.559 | 0.53 | 0.497 | 312-349 | 561-591 |
| real | cursor | all | 557 | 3340 | 1020 | 970 | 1030 | 1090 | 0.167 | 0.545 | 0.574 | 0.541 | 0.511 | 580 | 3390 | 1050 | 1030 | 1100 | 0.171 | 0.554 | 0.563 | 0.528 | 557-592 | 1020-1070 |
| real | parse | 1 | 277 | 1440 | 518 | 525 | 523 | 576 | 0.192 | 0.535 | 0.529 | 0.531 | 0.482 | 280 | 1460 | 524 | 526 | 577 | 0.191 | 0.534 | 0.532 | 0.485 | 277-318 | 518-531 |
| real | parse | all | 493 | 2910 | 921 | 917 | 897 | 983 | 0.169 | 0.535 | 0.537 | 0.549 | 0.501 | 516 | 2990 | 967 | 909 | 1010 | 0.173 | 0.534 | 0.568 | 0.509 | 493-526 | 921-982 |
| real | walk | 1 | 304 | 1610 | 540 | n/a | n/a | n/a | 0.189 | 0.563 | n/a | n/a | n/a | 311 | 1710 | 547 | n/a | n/a | 0.182 | 0.568 | n/a | n/a | 304-347 | 540-573 |
| real | walk | all | 527 | 3200 | 955 | n/a | n/a | n/a | 0.165 | 0.552 | n/a | n/a | n/a | 551 | 3370 | 996 | n/a | n/a | 0.164 | 0.554 | n/a | n/a | 527-570 | 955-1020 |
| realerr | cursor | 1 | 54.4 | 316 | 100 | 103 | 107 | 109 | 0.172 | 0.543 | 0.53 | 0.507 | 0.499 | 55.5 | 355 | 102 | 110 | 110 | 0.156 | 0.543 | 0.506 | 0.504 | 54.4-59.8 | 100-112 |
| realerr | cursor | all | 84 | 663 | 156 | 157 | 158 | 167 | 0.127 | 0.539 | 0.535 | 0.531 | 0.502 | 90.3 | 722 | 168 | 160 | 172 | 0.125 | 0.538 | 0.566 | 0.524 | 84-91.8 | 156-173 |
| realerr | parse | 1 | 48.7 | 275 | 92.3 | 96.9 | 91.1 | 103 | 0.177 | 0.528 | 0.503 | 0.535 | 0.475 | 48.9 | 281 | 92.8 | 91.6 | 105 | 0.174 | 0.527 | 0.535 | 0.465 | 48.7-50.4 | 92.3-107 |
| realerr | parse | all | 77.3 | 603 | 145 | 144 | 142 | 156 | 0.128 | 0.533 | 0.536 | 0.545 | 0.497 | 79.7 | 645 | 153 | 142 | 162 | 0.124 | 0.521 | 0.562 | 0.492 | 77.3-86.1 | 145-156 |
| realerr | walk | 1 | 53.4 | 309 | 96.3 | n/a | n/a | n/a | 0.173 | 0.555 | n/a | n/a | n/a | 56.2 | 350 | 97 | n/a | n/a | 0.161 | 0.579 | n/a | n/a | 53.4-57.3 | 96.3-98.6 |
| realerr | walk | all | 82.7 | 663 | 155 | n/a | n/a | n/a | 0.125 | 0.535 | n/a | n/a | n/a | 87 | 715 | 161 | n/a | n/a | 0.122 | 0.539 | n/a | n/a | 82.7-89.4 | 155-164 |

### scala: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 3980 | 12800 | 6180 | 6010 | 6100 | 6400 | 0.312 | 0.644 | 0.662 | 0.653 | 0.622 | 3980 | 12800 | 6190 | 6110 | 6410 | 0.311 | 0.643 | 0.652 | 0.621 | 3980-3980 | 6180-6200 |
| broken | cursor | all | 3990 | 13300 | 6210 | 6040 | 6120 | 6430 | 0.301 | 0.643 | 0.662 | 0.653 | 0.621 | 4000 | 13300 | 6220 | 6130 | 6440 | 0.3 | 0.644 | 0.652 | 0.622 | 3990-4090 | 6210-6230 |
| broken | parse | 1 | 3580 | 10300 | 5740 | 5560 | 5610 | 5940 | 0.348 | 0.624 | 0.644 | 0.638 | 0.603 | 3580 | 10300 | 5740 | 5610 | 5940 | 0.347 | 0.623 | 0.638 | 0.603 | 3580-3580 | 5740-5750 |
| broken | parse | all | 3590 | 10700 | 5750 | 5580 | 5620 | 5950 | 0.335 | 0.625 | 0.644 | 0.639 | 0.604 | 3600 | 10800 | 5770 | 5630 | 5960 | 0.334 | 0.624 | 0.639 | 0.604 | 3590-3710 | 5750-5780 |
| broken | walk | 1 | 5440 | 15800 | 7910 | n/a | n/a | n/a | 0.344 | 0.688 | n/a | n/a | n/a | 5440 | 15800 | 7910 | n/a | n/a | 0.343 | 0.687 | n/a | n/a | 5440-5440 | 7910-7920 |
| broken | walk | all | 5450 | 16300 | 7930 | n/a | n/a | n/a | 0.335 | 0.688 | n/a | n/a | n/a | 5460 | 16400 | 7940 | n/a | n/a | 0.334 | 0.688 | n/a | n/a | 5450-5480 | 7930-7950 |
| real | cursor | 1 | 7090 | 22900 | 10300 | 9990 | 10300 | 10700 | 0.31 | 0.689 | 0.71 | 0.689 | 0.665 | 7090 | 22900 | 10300 | 10300 | 10700 | 0.31 | 0.689 | 0.688 | 0.665 | 7090-7100 | 10300-10300 |
| real | cursor | all | 7100 | 24200 | 10300 | 10000 | 10300 | 10700 | 0.294 | 0.689 | 0.71 | 0.687 | 0.663 | 7100 | 24300 | 10300 | 10300 | 10700 | 0.292 | 0.687 | 0.686 | 0.663 | 7100-7140 | 10300-10400 |
| real | parse | 1 | 6350 | 18200 | 9430 | 9110 | 9350 | 9770 | 0.348 | 0.674 | 0.697 | 0.679 | 0.65 | 6350 | 18200 | 9430 | 9360 | 9770 | 0.348 | 0.673 | 0.679 | 0.65 | 6350-6350 | 9430-9440 |
| real | parse | all | 6350 | 19300 | 9450 | 9130 | 9380 | 9800 | 0.329 | 0.672 | 0.696 | 0.677 | 0.648 | 6350 | 19400 | 9470 | 9390 | 9810 | 0.327 | 0.67 | 0.676 | 0.648 | 6350-6350 | 9450-9500 |
| real | walk | 1 | 6910 | 21600 | 9830 | n/a | n/a | n/a | 0.32 | 0.703 | n/a | n/a | n/a | 6910 | 21600 | 9830 | n/a | n/a | 0.32 | 0.703 | n/a | n/a | 6910-6910 | 9830-9850 |
| real | walk | all | 6910 | 22900 | 9850 | n/a | n/a | n/a | 0.301 | 0.702 | n/a | n/a | n/a | 6910 | 23000 | 9880 | n/a | n/a | 0.3 | 0.7 | n/a | n/a | 6910-6910 | 9850-9900 |
| realerr | cursor | 1 | 1160 | 4020 | 1710 | 1660 | 1700 | 1770 | 0.287 | 0.677 | 0.696 | 0.68 | 0.652 | 1160 | 4030 | 1710 | 1700 | 1770 | 0.287 | 0.677 | 0.68 | 0.652 | 1160-1160 | 1710-1710 |
| realerr | cursor | all | 1160 | 4500 | 1710 | 1670 | 1700 | 1780 | 0.258 | 0.678 | 0.696 | 0.681 | 0.653 | 1160 | 4580 | 1720 | 1710 | 1780 | 0.254 | 0.677 | 0.679 | 0.653 | 1160-1170 | 1710-1720 |
| realerr | parse | 1 | 1040 | 3240 | 1570 | 1520 | 1550 | 1630 | 0.32 | 0.66 | 0.681 | 0.669 | 0.636 | 1040 | 3260 | 1570 | 1550 | 1630 | 0.317 | 0.659 | 0.668 | 0.635 | 1040-1040 | 1570-1570 |
| realerr | parse | all | 1040 | 3700 | 1580 | 1530 | 1560 | 1640 | 0.281 | 0.657 | 0.68 | 0.666 | 0.635 | 1040 | 3770 | 1580 | 1560 | 1640 | 0.276 | 0.657 | 0.667 | 0.634 | 1040-1050 | 1580-1590 |
| realerr | walk | 1 | 1130 | 3800 | 1630 | n/a | n/a | n/a | 0.297 | 0.689 | n/a | n/a | n/a | 1130 | 3810 | 1640 | n/a | n/a | 0.296 | 0.689 | n/a | n/a | 1130-1130 | 1630-1640 |
| realerr | walk | all | 1130 | 4340 | 1650 | n/a | n/a | n/a | 0.261 | 0.686 | n/a | n/a | n/a | 1130 | 4370 | 1650 | n/a | n/a | 0.259 | 0.686 | n/a | n/a | 1130-1140 | 1650-1660 |

### scala: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 949 | 4300 | 1810 | 1840 | 1860 | 1940 | 0.221 | 0.525 | 0.515 | 0.509 | 0.489 | 968 | 4370 | 1820 | 1870 | 1950 | 0.221 | 0.531 | 0.518 | 0.496 | 949-1060 | 1810-1970 |
| broken | cursor | all | 1520 | 7150 | 2820 | 2780 | 2730 | 3040 | 0.212 | 0.538 | 0.545 | 0.556 | 0.499 | 1550 | 7290 | 2940 | 2790 | 3090 | 0.213 | 0.527 | 0.556 | 0.501 | 1520-1570 | 2820-2970 |
| broken | parse | 1 | 859 | 3620 | 1690 | 1710 | 1680 | 1850 | 0.237 | 0.508 | 0.501 | 0.51 | 0.464 | 895 | 3730 | 1710 | 1710 | 1870 | 0.24 | 0.524 | 0.525 | 0.478 | 859-943 | 1690-1860 |
| broken | parse | all | 1340 | 6160 | 2630 | 2580 | 2470 | 2800 | 0.218 | 0.512 | 0.522 | 0.544 | 0.481 | 1390 | 6240 | 2720 | 2520 | 2850 | 0.224 | 0.513 | 0.552 | 0.49 | 1340-1430 | 2630-2770 |
| broken | walk | 1 | 1270 | 5780 | 2120 | n/a | n/a | n/a | 0.22 | 0.599 | n/a | n/a | n/a | 1280 | 5870 | 2150 | n/a | n/a | 0.218 | 0.593 | n/a | n/a | 1270-1460 | 2120-2250 |
| broken | walk | all | 1880 | 8640 | 3160 | n/a | n/a | n/a | 0.218 | 0.596 | n/a | n/a | n/a | 1930 | 8730 | 3280 | n/a | n/a | 0.221 | 0.589 | n/a | n/a | 1880-1970 | 3160-3350 |
| real | cursor | 1 | 1630 | 8010 | 2920 | 2990 | 3110 | 3290 | 0.203 | 0.556 | 0.543 | 0.523 | 0.495 | 1650 | 8070 | 2950 | 3110 | 3320 | 0.204 | 0.558 | 0.529 | 0.497 | 1630-1810 | 2920-3080 |
| real | cursor | all | 2510 | 13300 | 4640 | 4410 | 4640 | 4940 | 0.189 | 0.542 | 0.57 | 0.542 | 0.509 | 2600 | 13500 | 4730 | 4650 | 4980 | 0.193 | 0.55 | 0.56 | 0.523 | 2510-2620 | 4640-4800 |
| real | parse | 1 | 1450 | 6750 | 2700 | 2740 | 2730 | 3000 | 0.214 | 0.535 | 0.528 | 0.531 | 0.482 | 1460 | 6890 | 2740 | 2750 | 3010 | 0.212 | 0.534 | 0.532 | 0.485 | 1450-1660 | 2700-2770 |
| real | parse | all | 2220 | 11500 | 4180 | 4150 | 4070 | 4470 | 0.193 | 0.531 | 0.535 | 0.546 | 0.497 | 2320 | 11600 | 4380 | 4130 | 4590 | 0.2 | 0.529 | 0.561 | 0.506 | 2220-2340 | 4180-4430 |
| real | walk | 1 | 1580 | 7810 | 2820 | n/a | n/a | n/a | 0.203 | 0.563 | n/a | n/a | n/a | 1620 | 7870 | 2850 | n/a | n/a | 0.206 | 0.568 | n/a | n/a | 1580-1810 | 2820-2990 |
| real | walk | all | 2390 | 12900 | 4370 | n/a | n/a | n/a | 0.186 | 0.547 | n/a | n/a | n/a | 2500 | 13100 | 4500 | n/a | n/a | 0.19 | 0.556 | n/a | n/a | 2390-2550 | 4370-4590 |
| realerr | cursor | 1 | 284 | 1520 | 522 | 535 | 559 | 568 | 0.187 | 0.543 | 0.53 | 0.507 | 0.499 | 287 | 1540 | 534 | 572 | 575 | 0.187 | 0.539 | 0.502 | 0.5 | 284-312 | 522-587 |
| realerr | cursor | all | 401 | 2640 | 762 | 755 | 762 | 809 | 0.152 | 0.526 | 0.531 | 0.526 | 0.496 | 435 | 2690 | 790 | 765 | 827 | 0.162 | 0.55 | 0.568 | 0.526 | 401-445 | 762-820 |
| realerr | parse | 1 | 254 | 1320 | 481 | 506 | 475 | 535 | 0.193 | 0.528 | 0.503 | 0.535 | 0.475 | 255 | 1340 | 484 | 478 | 550 | 0.19 | 0.527 | 0.534 | 0.464 | 254-262 | 481-556 |
| realerr | parse | all | 373 | 2300 | 697 | 697 | 687 | 758 | 0.162 | 0.535 | 0.535 | 0.543 | 0.492 | 388 | 2370 | 728 | 692 | 775 | 0.163 | 0.533 | 0.56 | 0.501 | 373-400 | 697-745 |
| realerr | walk | 1 | 279 | 1470 | 503 | n/a | n/a | n/a | 0.189 | 0.554 | n/a | n/a | n/a | 293 | 1510 | 506 | n/a | n/a | 0.194 | 0.578 | n/a | n/a | 279-298 | 503-515 |
| realerr | walk | all | 398 | 2560 | 747 | n/a | n/a | n/a | 0.155 | 0.532 | n/a | n/a | n/a | 424 | 2640 | 766 | n/a | n/a | 0.161 | 0.554 | n/a | n/a | 398-434 | 747-784 |

### scala: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 401 | 2490 | 444 | 454 | 490 | 463 | 0.161 | 0.903 | 0.883 | 0.819 | 0.866 |
| broken | cursor minus parse | 1 | cycles M | 89.8 | 674 | 118 | 128 | 179 | 92.2 | 0.133 | 0.764 | 0.7 | 0.501 | 0.974 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 17.3 | 141 | 22.5 | 24.7 | 34.3 | 17.7 | 0.123 | 0.769 | 0.7 | 0.505 | 0.98 |
| broken | cursor minus parse | all | instructions M | 402 | 2570 | 463 | 461 | 500 | 477 | 0.156 | 0.868 | 0.871 | 0.804 | 0.842 |
| broken | cursor minus parse | all | cycles M | 172 | 985 | 192 | 209 | 254 | 240 | 0.175 | 0.895 | 0.821 | 0.678 | 0.716 |
| broken | cursor minus parse | all | CPU user+sys ms | 41.7 | 213 | 50.3 | 48.5 | 61.2 | 55.3 | 0.196 | 0.829 | 0.86 | 0.681 | 0.755 |
| broken | walk minus parse | 1 | instructions M | 1860 | 5550 | 2170 | n/a | n/a | n/a | 0.335 | 0.857 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 410 | 2150 | 430 | n/a | n/a | n/a | 0.19 | 0.953 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 78.7 | 429 | 82.2 | n/a | n/a | n/a | 0.183 | 0.957 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 1860 | 5570 | 2180 | n/a | n/a | n/a | 0.334 | 0.853 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 538 | 2480 | 532 | n/a | n/a | n/a | 0.217 | 1.01 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 106 | 492 | 104 | n/a | n/a | n/a | 0.215 | 1.02 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 744 | 4630 | 865 | 885 | 950 | 901 | 0.161 | 0.86 | 0.841 | 0.783 | 0.826 |
| real | cursor minus parse | 1 | cycles M | 179 | 1260 | 221 | 257 | 381 | 281 | 0.142 | 0.809 | 0.696 | 0.469 | 0.635 |
| real | cursor minus parse | 1 | CPU user+sys ms | 34.6 | 226 | 42.9 | 49.1 | 72.9 | 53.7 | 0.153 | 0.807 | 0.704 | 0.474 | 0.643 |
| real | cursor minus parse | all | instructions M | 744 | 4840 | 850 | 868 | 950 | 903 | 0.154 | 0.876 | 0.857 | 0.783 | 0.825 |
| real | cursor minus parse | all | cycles M | 293 | 1810 | 460 | 258 | 569 | 475 | 0.162 | 0.637 | 1.13 | 0.516 | 0.617 |
| real | cursor minus parse | all | CPU user+sys ms | 64.7 | 429 | 101 | 53.3 | 133 | 107 | 0.151 | 0.638 | 1.21 | 0.487 | 0.608 |
| real | walk minus parse | 1 | instructions M | 561 | 3360 | 402 | n/a | n/a | n/a | 0.167 | 1.39 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 138 | 1060 | 113 | n/a | n/a | n/a | 0.13 | 1.22 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 26.5 | 165 | 21.7 | n/a | n/a | n/a | 0.16 | 1.22 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 561 | 3620 | 397 | n/a | n/a | n/a | 0.155 | 1.41 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 167 | 1380 | 190 | n/a | n/a | n/a | 0.121 | 0.882 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 34.8 | 288 | 34.1 | n/a | n/a | n/a | 0.121 | 1.02 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 121 | 784 | 138 | 141 | 152 | 144 | 0.154 | 0.876 | 0.856 | 0.797 | 0.84 |
| realerr | cursor minus parse | 1 | cycles M | 29.4 | 197 | 41 | 29.9 | 84.2 | 33.2 | 0.149 | 0.719 | 0.986 | 0.35 | 0.886 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 5.67 | 40.4 | 7.81 | 5.74 | 16.1 | 6.39 | 0.14 | 0.726 | 0.988 | 0.352 | 0.887 |
| realerr | cursor minus parse | all | instructions M | 121 | 799 | 132 | 139 | 144 | 141 | 0.152 | 0.92 | 0.871 | 0.84 | 0.857 |
| realerr | cursor minus parse | all | cycles M | 27.9 | 345 | 65.1 | 57.7 | 75.4 | 51.1 | 0.0809 | 0.428 | 0.483 | 0.37 | 0.545 |
| realerr | cursor minus parse | all | CPU user+sys ms | 6.71 | 60 | 11 | 12.8 | 16.2 | 11.8 | 0.112 | 0.609 | 0.522 | 0.415 | 0.569 |
| realerr | walk minus parse | 1 | instructions M | 91.4 | 558 | 65.2 | n/a | n/a | n/a | 0.164 | 1.4 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 24.4 | 155 | 21.5 | n/a | n/a | n/a | 0.158 | 1.14 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 4.71 | 33.2 | 4.03 | n/a | n/a | n/a | 0.142 | 1.17 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 91.8 | 641 | 66.8 | n/a | n/a | n/a | 0.143 | 1.37 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 24.6 | 260 | 49.5 | n/a | n/a | n/a | 0.0944 | 0.496 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 5.39 | 60.5 | 9.61 | n/a | n/a | n/a | 0.0891 | 0.561 | n/a | n/a | n/a |

### scala: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 31.4 | 496 | 16.5 | 0.0634 | 1.9 | 31.4 | 496 | 16.5 | 0.0634 | 1.9 | 24.8 | 488 | 12 | 0.0507 | 2.06 |
| broken | cursor | all | 72.7 | 1180 | 73.7 | 0.0614 | 0.985 | 72.7 | 1180 | 73.7 | 0.0614 | 0.986 | 65.2 | 1180 | 69.1 | 0.0554 | 0.943 |
| broken | parse | 1 | 26.1 | 185 | 15.7 | 0.141 | 1.66 | 26.1 | 185 | 15.7 | 0.141 | 1.66 | 19.4 | 178 | 10.9 | 0.109 | 1.78 |
| broken | parse | all | 64.7 | 1110 | 71.6 | 0.0583 | 0.904 | 64.7 | 1110 | 71.6 | 0.0583 | 0.904 | 57 | 1100 | 66.9 | 0.0517 | 0.852 |
| broken | walk | 1 | 25.6 | 475 | 16.5 | 0.0538 | 1.55 | 25.6 | 475 | 16.5 | 0.0538 | 1.55 | 18.9 | 467 | 11.9 | 0.0404 | 1.58 |
| broken | walk | all | 66 | 1150 | 68.7 | 0.0572 | 0.96 | 66 | 1150 | 68.7 | 0.0572 | 0.961 | 58.5 | 1150 | 63.8 | 0.0511 | 0.918 |
| real | cursor | 1 | 48 | 3540 | 19 | 0.0136 | 2.53 | 48 | 3540 | 19 | 0.0136 | 2.53 | 38.6 | 3530 | 12.6 | 0.0109 | 3.07 |
| real | cursor | all | 79.3 | 7640 | 80.8 | 0.0104 | 0.981 | 79.3 | 7640 | 80.8 | 0.0104 | 0.981 | 69.4 | 7630 | 74.1 | 0.00911 | 0.937 |
| real | parse | 1 | 30.9 | 1230 | 18.4 | 0.0252 | 1.68 | 30.9 | 1230 | 18.4 | 0.0252 | 1.68 | 21.3 | 1220 | 12 | 0.0175 | 1.78 |
| real | parse | all | 68.1 | 7940 | 86.6 | 0.00858 | 0.786 | 68.1 | 7940 | 86.6 | 0.00858 | 0.786 | 58.2 | 7930 | 80.1 | 0.00734 | 0.726 |
| real | walk | 1 | 30.7 | 5130 | 18.9 | 0.00598 | 1.62 | 30.7 | 5130 | 18.9 | 0.00598 | 1.62 | 21.3 | 5120 | 12.5 | 0.00416 | 1.7 |
| real | walk | all | 69 | 8200 | 81.9 | 0.00842 | 0.843 | 69 | 8200 | 81.9 | 0.00842 | 0.843 | 59 | 8190 | 75.3 | 0.00721 | 0.784 |
| realerr | cursor | 1 | 23.6 | 1780 | 12.5 | 0.0132 | 1.89 | 23.6 | 1780 | 12.5 | 0.0132 | 1.89 | 19.4 | 1780 | 9.55 | 0.0109 | 2.03 |
| realerr | cursor | all | 57.6 | 2900 | 57.1 | 0.0199 | 1.01 | 57.6 | 2900 | 57.1 | 0.0199 | 1.01 | 52.3 | 2890 | 53.8 | 0.0181 | 0.971 |
| realerr | parse | 1 | 22.5 | 707 | 12.1 | 0.0318 | 1.85 | 22.5 | 707 | 12.1 | 0.0318 | 1.85 | 18.3 | 701 | 8.96 | 0.0261 | 2.04 |
| realerr | parse | all | 56.9 | 2880 | 53.4 | 0.0197 | 1.06 | 56.9 | 2880 | 53.4 | 0.0197 | 1.06 | 51.9 | 2870 | 50.1 | 0.0181 | 1.04 |
| realerr | walk | 1 | 22.7 | 1490 | 13 | 0.0152 | 1.74 | 22.7 | 1490 | 13 | 0.0152 | 1.75 | 18.7 | 1490 | 9.79 | 0.0125 | 1.91 |
| realerr | walk | all | 57 | 2910 | 55 | 0.0196 | 1.04 | 57 | 2910 | 55 | 0.0196 | 1.04 | 51.6 | 2910 | 51.9 | 0.0178 | 0.996 |

### scala: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 25.9 | 282 | 15.7 | 0.0919 | 1.65 | 26 | 282 | 15.7 | 0.092 | 1.65 | 19.3 | 274 | 11.1 | 0.0704 | 1.74 |
| broken | all | 74.9 | 1600 | 101 | 0.0468 | 0.741 | 74.9 | 1600 | 101 | 0.0468 | 0.741 | 67.5 | 1590 | 96.4 | 0.0425 | 0.701 |
| real | 1 | 31.7 | 2090 | 18.4 | 0.0152 | 1.72 | 31.7 | 2090 | 18.4 | 0.0152 | 1.72 | 22.5 | 2080 | 12.1 | 0.0108 | 1.86 |
| real | all | 71.8 | KILL_RSS | 98.3 | n/a | 0.731 | 71.8 | KILL_RSS | 98.3 | n/a | 0.731 | 62 | KILL_RSS | 91.6 | n/a | 0.676 |
| realerr | 1 | 22.7 | 1010 | 11.9 | 0.0225 | 1.92 | 22.7 | 1010 | 11.9 | 0.0225 | 1.92 | 18.5 | 1010 | 8.87 | 0.0184 | 2.08 |
| realerr | all | 61.5 | KILL_RSS | 65.2 | n/a | 0.944 | 61.5 | KILL_RSS | 65.2 | n/a | 0.944 | 56.2 | KILL_RSS | 61.8 | n/a | 0.908 |

### scala: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 2 (real), 2.29 (realerr), 1.95 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 2.33 | 27200 | 445 | 3210 | 296000 | 3210 | 296000 | 0.000727 |
| real | steady | 2 | 70.7 | 445 | 3210 | 295000 | 3210 | 295000 | 0.000623 |
| realerr | cold | 10.7 | 184000 | 724 | 5060 | 471000 | 5060 | 471000 | 0.00211 |
| realerr | steady | 2.29 | 81.2 | 723 | 5050 | 471000 | 5050 | 471000 | 0.000452 |
| broken | cold | 6.66 | 34700 | 379 | 3100 | 310000 | 3100 | 310000 | 0.00214 |
| broken | steady | 1.95 | 46 | 379 | 3100 | 310000 | 3100 | 310000 | 0.000629 |

### scala: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2049 B | init ms | 0.00898 | 0.0115 | 0.00548 | 0.779 | 1.64 | 15 |
| #0 2049 B | parse1 ms | 0.254 | 0.476 | 0.287 | 0.534 | 0.885 | 15 |
| #0 2049 B | init+parse1 ms | 0.262 | 0.487 | 0.292 | 0.539 | 0.897 | 15 |
| #0 2049 B | parse2 ms | 0.122 | 0.394 | 0.188 | 0.309 | 0.648 | 15 |
| #0 2049 B | process user+sys ms | 1.07 | 1.92 | 0.766 | 0.557 | 1.4 | 15 |
| #0 2049 B | exec to exit instr user+kernel M | 9.13 | 16.4 | 8.25 | 0.557 | 1.11 | 15 |
| #0 2049 B | exec to exit instr user M | 6.18 | 11.8 | 7.03 | 0.522 | 0.879 | 15 |
| #0 2049 B | exec to exit instr kernel M | 2.91 | 4.53 | 1.22 | 0.643 | 2.4 | 15 |
| #0 2049 B | exec to exit cycles user+kernel M | 4.85 | 8.97 | 3.49 | 0.541 | 1.39 | 15 |
| #0 2049 B | exec to exit task-clock ms | 1.01 | 1.85 | 0.67 | 0.546 | 1.51 | 15 |
| #0 2049 B | peak RSS MB | 6.38 | 8.24 | 5.98 | 0.774 | 1.07 | 15 |
| #0 2049 B | peak footprint MB | 6.4 | 8.26 | 5.94 | 0.775 | 1.08 | 15 |
| #1 2732 B | init ms | 0.00857 | 0.0119 | 0.00579 | 0.721 | 1.48 | 15 |
| #1 2732 B | parse1 ms | 0.327 | 0.68 | 0.391 | 0.481 | 0.837 | 15 |
| #1 2732 B | init+parse1 ms | 0.336 | 0.692 | 0.397 | 0.486 | 0.848 | 15 |
| #1 2732 B | parse2 ms | 0.17 | 0.585 | 0.264 | 0.291 | 0.644 | 15 |
| #1 2732 B | process user+sys ms | 1.2 | 2.36 | 0.945 | 0.507 | 1.27 | 15 |
| #1 2732 B | exec to exit instr user+kernel M | 11.5 | 21.5 | 11 | 0.536 | 1.05 | 15 |
| #1 2732 B | exec to exit instr user M | 8.44 | 16.4 | 9.67 | 0.514 | 0.874 | 15 |
| #1 2732 B | exec to exit instr kernel M | 3.03 | 5.11 | 1.29 | 0.594 | 2.36 | 15 |
| #1 2732 B | exec to exit cycles user+kernel M | 5.59 | 11.4 | 4.44 | 0.489 | 1.26 | 15 |
| #1 2732 B | exec to exit task-clock ms | 1.13 | 2.28 | 0.85 | 0.496 | 1.33 | 15 |
| #1 2732 B | peak RSS MB | 6.66 | 8.98 | 6.18 | 0.741 | 1.08 | 15 |
| #1 2732 B | peak footprint MB | 6.69 | 9 | 6.17 | 0.743 | 1.08 | 15 |

### scala: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 773 | 886835 | 0 | 0 | ok |
| realerr | 84 | 146922 | 84 | 0 | ok |
| broken | 497 | 474890 | 429 | 0 | ok |

## tsx

### tsx: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 114 | 494 | 258 | 266 | 242 | 275 | 0.231 | 0.442 | 0.43 | 0.471 | 0.415 | 116 | 499 | 265 | 244 | 276 | 0.233 | 0.44 | 0.477 | 0.421 | 114-122 | 258-289 | 1 | 1.01 | 1 | 22.6 |
| broken | cursor | all | 19.2 | 83.3 | 46.9 | 47.9 | 42.4 | 47.8 | 0.231 | 0.41 | 0.401 | 0.454 | 0.402 | 20.2 | 84.7 | 49.8 | 43.6 | 52.4 | 0.238 | 0.405 | 0.463 | 0.385 | 19.2-22.2 | 46.9-55.6 | 9.85 * | 10.1 * | 8.81 * | 134 |
| broken | parse | 1 | 97.9 | 356 | 240 | 242 | 208 | 255 | 0.275 | 0.408 | 0.404 | 0.471 | 0.384 | 98.8 | 363 | 241 | 210 | 255 | 0.272 | 0.41 | 0.471 | 0.387 | 97.9-103 | 240-253 | 1 | 1.01 | 1 | 26.4 |
| broken | parse | all | 17.5 | 65.2 | 46.6 | 46.5 | 38.7 | 47.6 | 0.269 | 0.376 | 0.377 | 0.453 | 0.367 | 18.7 | 67.1 | 48 | 39.5 | 49.5 | 0.279 | 0.39 | 0.473 | 0.378 | 17.5-20.8 | 46.6-51.4 | 9.08 * | 8.86 * | 8.25 * | 148 |
| broken | walk | 1 | 636 | 3070 | 810 | n/a | n/a | n/a | 0.207 | 0.786 | n/a | n/a | n/a | 643 | 3090 | 846 | n/a | n/a | 0.208 | 0.759 | n/a | n/a | 636-711 | 810-930 | 1 | 1.04 | 1 | 4.06 |
| broken | walk | all | 499 | 2440 | 547 | n/a | n/a | n/a | 0.205 | 0.913 | n/a | n/a | n/a | 505 | 2450 | 586 | n/a | n/a | 0.206 | 0.861 | n/a | n/a | 499-590 | 547-659 | 1.45 * | 1.44 * | 1.75 * | 5.17 |
| real | cursor | 1 | 96.7 | 414 | 178 | 184 | 193 | 204 | 0.234 | 0.543 | 0.524 | 0.5 | 0.475 | 100 | 428 | 181 | 193 | 209 | 0.235 | 0.556 | 0.519 | 0.479 | 96.7-102 | 178-188 | 1 | 1.01 | 1 | 30.8 |
| real | cursor | all | 13.7 | 63.1 | 24.2 | 23.9 | 25.1 | 26.6 | 0.218 | 0.568 | 0.575 | 0.547 | 0.516 | 14.3 | 65.6 | 25.2 | 25.7 | 27.2 | 0.218 | 0.569 | 0.557 | 0.526 | 13.7-15.4 | 24.2-26.7 | 12.2 | 11.4 | 12.3 | 217 |
| real | parse | 1 | 77.6 | 254 | 152 | 155 | 148 | 175 | 0.305 | 0.511 | 0.501 | 0.523 | 0.445 | 78.3 | 257 | 153 | 149 | 175 | 0.304 | 0.512 | 0.526 | 0.447 | 77.6-81.9 | 152-158 | 1 | 1.01 | 1 | 38.4 |
| real | parse | all | 10.9 | 34.5 | 20.8 | 20.8 | 19.6 | 23.2 | 0.315 | 0.522 | 0.523 | 0.555 | 0.469 | 11.5 | 37.6 | 21.8 | 19.8 | 23.6 | 0.306 | 0.529 | 0.582 | 0.488 | 10.9-13.2 | 20.8-22.9 | 12 | 11.7 | 12.3 | 274 |
| real | walk | 1 | 94 | 386 | 167 | n/a | n/a | n/a | 0.243 | 0.562 | n/a | n/a | n/a | 97.2 | 398 | 171 | n/a | n/a | 0.244 | 0.57 | n/a | n/a | 94-108 | 167-176 | 1 | 1.01 | 1 | 31.7 |
| real | walk | all | 13.1 | 57.9 | 22.9 | n/a | n/a | n/a | 0.225 | 0.57 | n/a | n/a | n/a | 14 | 60.3 | 24 | n/a | n/a | 0.232 | 0.582 | n/a | n/a | 13.1-15.8 | 22.9-25 | 12.1 | 11.4 | 12.3 | 228 |
| realerr | cursor | 1 | 9.47 | 36.8 | 20.4 | 20.4 | 19.6 | 23.6 | 0.258 | 0.464 | 0.464 | 0.483 | 0.401 | 9.61 | 37.4 | 20.7 | 19.9 | 25.1 | 0.257 | 0.465 | 0.482 | 0.383 | 9.47-11 | 20.4-22.9 | 1 | 1.01 | 1 | 52 |
| realerr | cursor | all | 2.08 | 8.36 | 5.83 | 5.77 | 4.57 | 6.28 | 0.249 | 0.357 | 0.361 | 0.456 | 0.332 | 2.36 | 9.29 | 6.33 | 5.59 | 6.44 | 0.254 | 0.373 | 0.423 | 0.366 | 2.08-2.91 | 5.83-7.12 | 6.46 * | 5.78 * | 4.77 * | 236 |
| realerr | parse | 1 | 8.29 | 26.9 | 18.7 | 19.2 | 17.7 | 20.7 | 0.309 | 0.443 | 0.432 | 0.468 | 0.4 | 8.39 | 27.1 | 19.7 | 18.7 | 20.8 | 0.31 | 0.427 | 0.449 | 0.404 | 8.29-9.31 | 18.7-21 | 1 | 1.01 | 1 | 59.3 |
| realerr | parse | all | 1.82 | 7.31 | 5.59 | 5.73 | 4.51 | 5.94 | 0.249 | 0.326 | 0.318 | 0.404 | 0.307 | 2.02 | 8.11 | 6.14 | 4.73 | 6.12 | 0.25 | 0.329 | 0.428 | 0.331 | 1.82-2.48 | 5.59-8.3 | 6.52 * | 4.7 * | 4.45 * | 270 |
| realerr | walk | 1 | 9.54 | 35.5 | 19.8 | n/a | n/a | n/a | 0.269 | 0.481 | n/a | n/a | n/a | 9.65 | 36.5 | 20.4 | n/a | n/a | 0.264 | 0.473 | n/a | n/a | 9.54-11.3 | 19.8-21.9 | 1 | 1.01 | 1 | 51.6 |
| realerr | walk | all | 2.06 | 8.79 | 5.56 | n/a | n/a | n/a | 0.234 | 0.371 | n/a | n/a | n/a | 2.26 | 9.5 | 6.28 | n/a | n/a | 0.238 | 0.359 | n/a | n/a | 2.06-3.22 | 5.56-7.25 | 6.68 * | 5.54 * | 4.6 * | 239 |

### tsx: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 114 | 499 | 258 | 266 | 242 | 275 | 0.229 | 0.443 | 0.43 | 0.472 | 0.415 | 116 | 504 | 265 | 244 | 276 | 0.231 | 0.44 | 0.478 | 0.422 | 114-122 | 258-289 |
| broken | cursor | all | 192 | 855 | 429 | 420 | 372 | 449 | 0.225 | 0.448 | 0.458 | 0.517 | 0.429 | 200 | 862 | 440 | 395 | 459 | 0.233 | 0.455 | 0.508 | 0.437 | 192-205 | 429-460 |
| broken | parse | 1 | 98 | 358 | 240 | 242 | 208 | 255 | 0.274 | 0.408 | 0.405 | 0.472 | 0.384 | 98.9 | 365 | 241 | 210 | 255 | 0.271 | 0.41 | 0.471 | 0.387 | 98-103 | 240-253 |
| broken | parse | all | 166 | 588 | 381 | 385 | 336 | 414 | 0.283 | 0.437 | 0.432 | 0.496 | 0.402 | 171 | 601 | 398 | 342 | 422 | 0.285 | 0.431 | 0.5 | 0.406 | 166-179 | 381-418 |
| broken | walk | 1 | 637 | 3180 | 810 | n/a | n/a | n/a | 0.2 | 0.786 | n/a | n/a | n/a | 643 | 3210 | 846 | n/a | n/a | 0.201 | 0.76 | n/a | n/a | 637-712 | 810-930 |
| broken | walk | all | 714 | 3500 | 962 | n/a | n/a | n/a | 0.204 | 0.742 | n/a | n/a | n/a | 735 | 3530 | 1040 | n/a | n/a | 0.208 | 0.707 | n/a | n/a | 714-831 | 962-1110 |
| real | cursor | 1 | 96.8 | 418 | 178 | 184 | 193 | 204 | 0.231 | 0.544 | 0.525 | 0.501 | 0.475 | 100 | 432 | 181 | 193 | 209 | 0.232 | 0.556 | 0.519 | 0.48 | 96.8-102 | 178-187 |
| real | cursor | all | 171 | 731 | 296 | 299 | 297 | 330 | 0.234 | 0.576 | 0.571 | 0.574 | 0.518 | 178 | 749 | 310 | 308 | 334 | 0.238 | 0.575 | 0.579 | 0.534 | 171-181 | 296-321 |
| real | parse | 1 | 77.7 | 256 | 152 | 155 | 148 | 175 | 0.304 | 0.512 | 0.502 | 0.524 | 0.445 | 78.4 | 259 | 153 | 149 | 175 | 0.303 | 0.513 | 0.527 | 0.448 | 77.7-81.7 | 152-158 |
| real | parse | all | 135 | 428 | 251 | 261 | 239 | 284 | 0.316 | 0.538 | 0.517 | 0.565 | 0.475 | 141 | 437 | 269 | 246 | 290 | 0.323 | 0.526 | 0.574 | 0.487 | 135-146 | 251-274 |
| real | walk | 1 | 94.1 | 391 | 167 | n/a | n/a | n/a | 0.241 | 0.563 | n/a | n/a | n/a | 97.3 | 402 | 171 | n/a | n/a | 0.242 | 0.571 | n/a | n/a | 94.1-108 | 167-176 |
| real | walk | all | 166 | 673 | 287 | n/a | n/a | n/a | 0.247 | 0.581 | n/a | n/a | n/a | 171 | 684 | 296 | n/a | n/a | 0.25 | 0.578 | n/a | n/a | 166-174 | 287-302 |
| realerr | cursor | 1 | 9.48 | 37 | 20.4 | 20.4 | 19.6 | 23.6 | 0.256 | 0.465 | 0.465 | 0.484 | 0.402 | 9.63 | 37.8 | 20.7 | 19.9 | 25.1 | 0.255 | 0.466 | 0.483 | 0.384 | 9.48-11 | 20.4-22.9 |
| realerr | cursor | all | 14.3 | 53.3 | 29 | 28.2 | 26.6 | 30.5 | 0.268 | 0.494 | 0.508 | 0.537 | 0.469 | 15.5 | 56.1 | 29.9 | 28.9 | 31.1 | 0.277 | 0.519 | 0.538 | 0.5 | 14.3-17.9 | 29-32.4 |
| realerr | parse | 1 | 8.31 | 27 | 18.7 | 19.2 | 17.7 | 20.7 | 0.308 | 0.444 | 0.433 | 0.469 | 0.401 | 8.41 | 27.3 | 19.7 | 18.7 | 20.8 | 0.308 | 0.428 | 0.45 | 0.405 | 8.31-9.32 | 18.7-21 |
| realerr | parse | all | 12.2 | 36.5 | 26.9 | 26 | 24.4 | 27.8 | 0.334 | 0.454 | 0.47 | 0.499 | 0.439 | 13.3 | 39.2 | 28 | 24.8 | 28 | 0.34 | 0.476 | 0.536 | 0.475 | 12.2-14.8 | 26.9-29.9 |
| realerr | walk | 1 | 9.55 | 35.6 | 19.8 | n/a | n/a | n/a | 0.268 | 0.481 | n/a | n/a | n/a | 9.67 | 37 | 20.4 | n/a | n/a | 0.261 | 0.474 | n/a | n/a | 9.55-11.3 | 19.8-21.9 |
| realerr | walk | all | 14.9 | 49.7 | 28.1 | n/a | n/a | n/a | 0.3 | 0.53 | n/a | n/a | n/a | 15.6 | 52.9 | 29.3 | n/a | n/a | 0.295 | 0.532 | n/a | n/a | 14.9-16.8 | 28.1-31.2 |

### tsx: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 2310 | 8660 | 4610 | 4530 | 4310 | 4750 | 0.267 | 0.501 | 0.51 | 0.536 | 0.486 | 2310 | 8670 | 4610 | 4310 | 4750 | 0.266 | 0.501 | 0.536 | 0.486 | 2310-2310 | 4610-4610 |
| broken | cursor | all | 2320 | 8710 | 4610 | 4530 | 4310 | 4760 | 0.267 | 0.504 | 0.513 | 0.539 | 0.489 | 2340 | 8740 | 4620 | 4310 | 4760 | 0.267 | 0.506 | 0.541 | 0.491 | 2320-2430 | 4610-4620 |
| broken | parse | 1 | 1950 | 6000 | 4200 | 4110 | 3850 | 4320 | 0.325 | 0.465 | 0.475 | 0.507 | 0.452 | 1950 | 6000 | 4200 | 3850 | 4320 | 0.325 | 0.465 | 0.506 | 0.452 | 1950-1950 | 4200-4200 |
| broken | parse | all | 1970 | 5990 | 4200 | 4110 | 3860 | 4320 | 0.329 | 0.469 | 0.48 | 0.511 | 0.456 | 1980 | 6010 | 4210 | 3860 | 4330 | 0.33 | 0.471 | 0.514 | 0.458 | 1970-2090 | 4200-4210 |
| broken | walk | 1 | 15900 | 40800 | 21600 | n/a | n/a | n/a | 0.391 | 0.738 | n/a | n/a | n/a | 15900 | 40800 | 21600 | n/a | n/a | 0.39 | 0.738 | n/a | n/a | 15900-15900 | 21600-21600 |
| broken | walk | all | 15900 | 40800 | 21600 | n/a | n/a | n/a | 0.39 | 0.739 | n/a | n/a | n/a | 16000 | 40900 | 21600 | n/a | n/a | 0.391 | 0.74 | n/a | n/a | 15900-16100 | 21600-21600 |
| real | cursor | 1 | 2120 | 7320 | 3040 | 3010 | 2990 | 3190 | 0.29 | 0.698 | 0.706 | 0.711 | 0.666 | 2120 | 7330 | 3040 | 2990 | 3190 | 0.29 | 0.698 | 0.711 | 0.665 | 2120-2130 | 3040-3050 |
| real | cursor | all | 2130 | 7370 | 3050 | 3020 | 2990 | 3200 | 0.289 | 0.697 | 0.704 | 0.711 | 0.664 | 2130 | 7390 | 3060 | 3000 | 3210 | 0.287 | 0.695 | 0.707 | 0.662 | 2130-2150 | 3050-3060 |
| real | parse | 1 | 1680 | 3960 | 2540 | 2490 | 2430 | 2660 | 0.425 | 0.662 | 0.675 | 0.692 | 0.632 | 1680 | 3960 | 2540 | 2430 | 2670 | 0.425 | 0.662 | 0.691 | 0.631 | 1680-1680 | 2540-2550 |
| real | parse | all | 1680 | 3940 | 2550 | 2500 | 2440 | 2680 | 0.427 | 0.661 | 0.673 | 0.691 | 0.628 | 1680 | 3960 | 2560 | 2450 | 2680 | 0.425 | 0.659 | 0.688 | 0.628 | 1680-1690 | 2550-2560 |
| real | walk | 1 | 2040 | 6400 | 2810 | n/a | n/a | n/a | 0.318 | 0.727 | n/a | n/a | n/a | 2040 | 6410 | 2810 | n/a | n/a | 0.318 | 0.726 | n/a | n/a | 2040-2040 | 2810-2810 |
| real | walk | all | 2040 | 6440 | 2810 | n/a | n/a | n/a | 0.317 | 0.725 | n/a | n/a | n/a | 2040 | 6470 | 2820 | n/a | n/a | 0.315 | 0.723 | n/a | n/a | 2040-2040 | 2810-2830 |
| realerr | cursor | 1 | 202 | 660 | 364 | 355 | 354 | 376 | 0.305 | 0.554 | 0.567 | 0.569 | 0.536 | 202 | 662 | 364 | 354 | 376 | 0.304 | 0.554 | 0.569 | 0.536 | 202-202 | 364-365 |
| realerr | cursor | all | 206 | 663 | 363 | 355 | 355 | 376 | 0.311 | 0.568 | 0.58 | 0.581 | 0.548 | 208 | 665 | 365 | 355 | 377 | 0.313 | 0.571 | 0.587 | 0.553 | 206-223 | 363-366 |
| realerr | parse | 1 | 175 | 445 | 333 | 324 | 320 | 343 | 0.393 | 0.524 | 0.54 | 0.546 | 0.509 | 175 | 445 | 334 | 321 | 344 | 0.393 | 0.524 | 0.545 | 0.508 | 175-175 | 333-334 |
| realerr | parse | all | 178 | 438 | 333 | 324 | 321 | 344 | 0.405 | 0.534 | 0.547 | 0.553 | 0.516 | 182 | 446 | 335 | 322 | 344 | 0.408 | 0.544 | 0.567 | 0.529 | 178-186 | 333-336 |
| realerr | walk | 1 | 201 | 608 | 353 | n/a | n/a | n/a | 0.331 | 0.571 | n/a | n/a | n/a | 201 | 609 | 353 | n/a | n/a | 0.33 | 0.57 | n/a | n/a | 201-201 | 353-353 |
| realerr | walk | all | 206 | 610 | 353 | n/a | n/a | n/a | 0.337 | 0.583 | n/a | n/a | n/a | 209 | 614 | 354 | n/a | n/a | 0.341 | 0.59 | n/a | n/a | 206-213 | 353-356 |

### tsx: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 595 | 2570 | 1350 | 1390 | 1260 | 1440 | 0.231 | 0.441 | 0.429 | 0.471 | 0.414 | 607 | 2600 | 1380 | 1270 | 1440 | 0.233 | 0.439 | 0.477 | 0.421 | 595-633 | 1350-1510 |
| broken | cursor | all | 918 | 3880 | 2010 | 1970 | 1760 | 2120 | 0.237 | 0.456 | 0.467 | 0.523 | 0.434 | 945 | 3900 | 2060 | 1860 | 2180 | 0.242 | 0.459 | 0.509 | 0.434 | 918-961 | 2010-2150 |
| broken | parse | 1 | 511 | 1850 | 1250 | 1260 | 1080 | 1330 | 0.275 | 0.41 | 0.406 | 0.473 | 0.385 | 516 | 1900 | 1250 | 1090 | 1330 | 0.272 | 0.412 | 0.475 | 0.388 | 511-537 | 1250-1310 |
| broken | parse | all | 802 | 2730 | 1800 | 1820 | 1590 | 1970 | 0.294 | 0.444 | 0.44 | 0.503 | 0.408 | 816 | 2770 | 1880 | 1600 | 1990 | 0.295 | 0.434 | 0.509 | 0.41 | 802-841 | 1800-1950 |
| broken | walk | 1 | 3320 | 16100 | 4220 | n/a | n/a | n/a | 0.207 | 0.787 | n/a | n/a | n/a | 3350 | 16200 | 4410 | n/a | n/a | 0.207 | 0.76 | n/a | n/a | 3320-3710 | 4220-4840 |
| broken | walk | all | 3650 | 17300 | 4830 | n/a | n/a | n/a | 0.211 | 0.755 | n/a | n/a | n/a | 3740 | 17400 | 5190 | n/a | n/a | 0.215 | 0.721 | n/a | n/a | 3650-4210 | 4830-5570 |
| real | cursor | 1 | 505 | 2170 | 929 | 963 | 1010 | 1060 | 0.233 | 0.543 | 0.524 | 0.501 | 0.475 | 523 | 2220 | 942 | 1010 | 1090 | 0.236 | 0.554 | 0.518 | 0.479 | 505-532 | 929-976 |
| real | cursor | all | 784 | 3310 | 1380 | 1390 | 1390 | 1540 | 0.237 | 0.568 | 0.564 | 0.564 | 0.509 | 817 | 3360 | 1430 | 1430 | 1550 | 0.243 | 0.572 | 0.571 | 0.527 | 784-831 | 1380-1470 |
| real | parse | 1 | 405 | 1330 | 793 | 808 | 774 | 911 | 0.305 | 0.511 | 0.501 | 0.523 | 0.445 | 409 | 1340 | 798 | 777 | 914 | 0.304 | 0.512 | 0.526 | 0.447 | 405-426 | 793-823 |
| real | parse | all | 626 | 1950 | 1170 | 1220 | 1110 | 1320 | 0.32 | 0.533 | 0.514 | 0.563 | 0.475 | 653 | 1990 | 1250 | 1140 | 1350 | 0.328 | 0.524 | 0.571 | 0.484 | 626-675 | 1170-1270 |
| real | walk | 1 | 491 | 2020 | 873 | n/a | n/a | n/a | 0.243 | 0.562 | n/a | n/a | n/a | 507 | 2080 | 890 | n/a | n/a | 0.244 | 0.569 | n/a | n/a | 491-564 | 873-917 |
| real | walk | all | 769 | 3050 | 1330 | n/a | n/a | n/a | 0.252 | 0.578 | n/a | n/a | n/a | 789 | 3080 | 1370 | n/a | n/a | 0.256 | 0.576 | n/a | n/a | 769-800 | 1330-1410 |
| realerr | cursor | 1 | 49.4 | 192 | 106 | 106 | 102 | 123 | 0.257 | 0.464 | 0.464 | 0.483 | 0.401 | 50.1 | 195 | 108 | 104 | 131 | 0.257 | 0.465 | 0.482 | 0.383 | 49.4-57.3 | 106-119 |
| realerr | cursor | all | 72.6 | 267 | 148 | 145 | 137 | 153 | 0.272 | 0.489 | 0.501 | 0.528 | 0.475 | 78.4 | 279 | 152 | 147 | 157 | 0.281 | 0.515 | 0.533 | 0.499 | 72.6-85.3 | 148-157 |
| realerr | parse | 1 | 43.3 | 140 | 97.7 | 100 | 92.4 | 108 | 0.309 | 0.443 | 0.432 | 0.468 | 0.401 | 43.8 | 141 | 103 | 97.2 | 108 | 0.31 | 0.427 | 0.45 | 0.404 | 43.3-48.6 | 97.7-110 |
| realerr | parse | all | 62.7 | 183 | 132 | 133 | 125 | 142 | 0.343 | 0.477 | 0.471 | 0.503 | 0.441 | 67.5 | 196 | 139 | 125 | 144 | 0.344 | 0.484 | 0.539 | 0.469 | 62.7-73.4 | 132-151 |
| realerr | walk | 1 | 49.8 | 185 | 104 | n/a | n/a | n/a | 0.269 | 0.481 | n/a | n/a | n/a | 50.3 | 190 | 106 | n/a | n/a | 0.264 | 0.473 | n/a | n/a | 49.8-58.8 | 104-114 |
| realerr | walk | all | 76.3 | 250 | 145 | n/a | n/a | n/a | 0.305 | 0.528 | n/a | n/a | n/a | 79.2 | 265 | 150 | n/a | n/a | 0.299 | 0.528 | n/a | n/a | 76.3-84.9 | 145-157 |

### tsx: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 358 | 2660 | 413 | 425 | 456 | 435 | 0.134 | 0.866 | 0.841 | 0.783 | 0.823 |
| broken | cursor minus parse | 1 | cycles M | 84.2 | 717 | 102 | 129 | 184 | 110 | 0.117 | 0.828 | 0.655 | 0.458 | 0.767 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 16.3 | 142 | 18.1 | 23.5 | 34.4 | 20.1 | 0.115 | 0.899 | 0.69 | 0.472 | 0.809 |
| broken | cursor minus parse | all | instructions M | 355 | 2720 | 410 | 420 | 455 | 432 | 0.13 | 0.865 | 0.845 | 0.78 | 0.821 |
| broken | cursor minus parse | all | cycles M | 117 | 1150 | 208 | 143 | 163 | 152 | 0.101 | 0.56 | 0.813 | 0.717 | 0.769 |
| broken | cursor minus parse | all | CPU user+sys ms | 26 | 267 | 48.2 | 34.6 | 36.2 | 34.9 | 0.0976 | 0.54 | 0.752 | 0.719 | 0.745 |
| broken | walk minus parse | 1 | instructions M | 14000 | 34800 | 17400 | n/a | n/a | n/a | 0.402 | 0.804 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 2810 | 14200 | 2970 | n/a | n/a | n/a | 0.198 | 0.945 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 539 | 2820 | 570 | n/a | n/a | n/a | 0.191 | 0.946 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 14000 | 34900 | 17400 | n/a | n/a | n/a | 0.401 | 0.804 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 2850 | 14600 | 3030 | n/a | n/a | n/a | 0.195 | 0.941 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 547 | 2910 | 581 | n/a | n/a | n/a | 0.188 | 0.941 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 442 | 3370 | 502 | 516 | 555 | 529 | 0.131 | 0.881 | 0.856 | 0.796 | 0.835 |
| real | cursor minus parse | 1 | cycles M | 99.9 | 840 | 137 | 155 | 235 | 152 | 0.119 | 0.73 | 0.646 | 0.426 | 0.659 |
| real | cursor minus parse | 1 | CPU user+sys ms | 19.1 | 163 | 26.2 | 29.7 | 44.9 | 29.1 | 0.118 | 0.73 | 0.645 | 0.426 | 0.658 |
| real | cursor minus parse | all | instructions M | 442 | 3430 | 505 | 519 | 554 | 521 | 0.129 | 0.875 | 0.852 | 0.798 | 0.848 |
| real | cursor minus parse | all | cycles M | 158 | 1350 | 205 | 171 | 278 | 220 | 0.117 | 0.767 | 0.919 | 0.566 | 0.716 |
| real | cursor minus parse | all | CPU user+sys ms | 35.6 | 303 | 45.5 | 37.5 | 58.3 | 45.5 | 0.117 | 0.781 | 0.95 | 0.61 | 0.782 |
| real | walk minus parse | 1 | instructions M | 356 | 2450 | 266 | n/a | n/a | n/a | 0.145 | 1.34 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 85.7 | 692 | 80.6 | n/a | n/a | n/a | 0.124 | 1.06 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 16.4 | 135 | 15.5 | n/a | n/a | n/a | 0.122 | 1.06 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 357 | 2500 | 267 | n/a | n/a | n/a | 0.143 | 1.34 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 143 | 1090 | 157 | n/a | n/a | n/a | 0.131 | 0.91 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 31.3 | 246 | 35.7 | n/a | n/a | n/a | 0.128 | 0.878 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 26.8 | 215 | 30.5 | 31.4 | 33.9 | 32.3 | 0.124 | 0.878 | 0.853 | 0.79 | 0.83 |
| realerr | cursor minus parse | 1 | cycles M | 6.11 | 52.5 | 8.7 | 6.16 | 9.93 | 15.1 | 0.116 | 0.702 | 0.991 | 0.615 | 0.406 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 1.17 | 10.1 | 1.67 | 1.17 | 1.88 | 2.86 | 0.116 | 0.703 | 0.998 | 0.624 | 0.41 |
| realerr | cursor minus parse | all | instructions M | 28.5 | 225 | 30.4 | 30.8 | 33.7 | 32.2 | 0.127 | 0.939 | 0.926 | 0.847 | 0.885 |
| realerr | cursor minus parse | all | cycles M | 9.81 | 84 | 16.7 | 11.8 | 12.6 | 10.5 | 0.117 | 0.586 | 0.829 | 0.777 | 0.935 |
| realerr | cursor minus parse | all | CPU user+sys ms | 2.11 | 16.8 | 2.09 | 2.19 | 2.19 | 2.67 | 0.125 | 1.01 | 0.96 | 0.963 | 0.788 |
| realerr | walk minus parse | 1 | instructions M | 26.5 | 163 | 19.5 | n/a | n/a | n/a | 0.163 | 1.36 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 6.56 | 45.4 | 5.85 | n/a | n/a | n/a | 0.145 | 1.12 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 1.24 | 8.69 | 1.12 | n/a | n/a | n/a | 0.143 | 1.11 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 28.1 | 172 | 20.5 | n/a | n/a | n/a | 0.164 | 1.37 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 13.6 | 66.9 | 13 | n/a | n/a | n/a | 0.203 | 1.04 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 2.71 | 13.3 | 1.29 | n/a | n/a | n/a | 0.204 | 2.11 | n/a | n/a | n/a |

### tsx: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 27.7 | 92.8 | 20.8 | 0.299 | 1.33 | 27.7 | 92.8 | 20.8 | 0.299 | 1.33 | 20.2 | 84 | 16 | 0.24 | 1.26 |
| broken | cursor | all | 59.8 | 139 | 111 | 0.43 | 0.539 | 59.8 | 139 | 111 | 0.43 | 0.539 | 51.6 | 130 | 106 | 0.396 | 0.487 |
| broken | parse | 1 | 21.5 | 27.8 | 20 | 0.774 | 1.08 | 21.5 | 27.8 | 20 | 0.774 | 1.08 | 13.9 | 19 | 15.2 | 0.733 | 0.917 |
| broken | parse | all | 55.1 | 124 | 114 | 0.444 | 0.484 | 55.1 | 124 | 114 | 0.444 | 0.484 | 46.9 | 116 | 109 | 0.406 | 0.431 |
| broken | walk | 1 | 21.5 | 83.8 | 21 | 0.257 | 1.03 | 21.5 | 83.8 | 21 | 0.257 | 1.03 | 14 | 75.2 | 16.2 | 0.187 | 0.869 |
| broken | walk | all | 60.1 | 139 | 113 | 0.433 | 0.534 | 60.1 | 139 | 113 | 0.433 | 0.534 | 51.8 | 130 | 107 | 0.399 | 0.482 |
| real | cursor | 1 | 21 | 33.1 | 10.9 | 0.636 | 1.93 | 21 | 33.1 | 10.9 | 0.636 | 1.93 | 13 | 23.9 | 5.67 | 0.545 | 2.3 |
| real | cursor | all | 47.8 | 58.5 | 36.5 | 0.817 | 1.31 | 47.8 | 58.5 | 36.5 | 0.817 | 1.31 | 38.9 | 49.3 | 31.1 | 0.789 | 1.25 |
| real | parse | 1 | 15 | 20 | 9.78 | 0.749 | 1.53 | 15 | 20 | 9.78 | 0.749 | 1.53 | 7.02 | 10.8 | 4.54 | 0.651 | 1.55 |
| real | parse | all | 41.6 | 55 | 35.3 | 0.757 | 1.18 | 41.6 | 55 | 35.3 | 0.757 | 1.18 | 32.8 | 45.7 | 29.8 | 0.718 | 1.1 |
| real | walk | 1 | 15.1 | 30.8 | 10.6 | 0.489 | 1.42 | 15.1 | 30.8 | 10.6 | 0.489 | 1.42 | 7.08 | 21.8 | 5.54 | 0.325 | 1.28 |
| real | walk | all | 42.3 | 56.5 | 35.9 | 0.748 | 1.18 | 42.3 | 56.5 | 35.9 | 0.748 | 1.18 | 33.5 | 47.2 | 30.3 | 0.71 | 1.11 |
| realerr | cursor | 1 | 9.48 | 13.7 | 6.41 | 0.692 | 1.48 | 9.5 | 13.7 | 6.41 | 0.693 | 1.48 | 5.67 | 8.49 | 3.62 | 0.668 | 1.57 |
| realerr | cursor | all | 20.2 | 23.8 | 12.7 | 0.85 | 1.59 | 20.2 | 23.8 | 12.7 | 0.85 | 1.59 | 15.4 | 18.4 | 9.57 | 0.836 | 1.61 |
| realerr | parse | 1 | 9.19 | 11.8 | 5.45 | 0.777 | 1.69 | 9.2 | 11.8 | 5.45 | 0.778 | 1.69 | 5.33 | 6.52 | 2.58 | 0.817 | 2.07 |
| realerr | parse | all | 18.9 | 20.7 | 11.7 | 0.914 | 1.62 | 18.9 | 20.7 | 11.7 | 0.914 | 1.62 | 14.3 | 15.4 | 8.71 | 0.929 | 1.64 |
| realerr | walk | 1 | 9.23 | 13.1 | 6.65 | 0.704 | 1.39 | 9.24 | 13.1 | 6.65 | 0.705 | 1.39 | 5.41 | 7.75 | 3.86 | 0.698 | 1.4 |
| realerr | walk | all | 19.4 | 22.8 | 12.6 | 0.851 | 1.54 | 19.4 | 22.8 | 12.6 | 0.851 | 1.54 | 14.5 | 17.5 | 9.64 | 0.831 | 1.51 |

### tsx: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 21.5 | 41.4 | 20.1 | 0.521 | 1.07 | 21.5 | 41.4 | 20.1 | 0.521 | 1.07 | 14.1 | 32.6 | 15.3 | 0.431 | 0.919 |
| broken | all | 72.4 | 297 | 195 | 0.244 | 0.372 | 72.4 | 297 | 195 | 0.244 | 0.372 | 64.4 | 288 | 190 | 0.224 | 0.34 |
| real | 1 | 15 | 22.8 | 9.48 | 0.656 | 1.58 | 15 | 22.8 | 9.48 | 0.656 | 1.58 | 7.05 | 13.6 | 4.32 | 0.518 | 1.63 |
| real | all | 48.2 | 79.8 | 43.7 | 0.604 | 1.1 | 48.2 | 79.8 | 43.7 | 0.604 | 1.1 | 39.9 | 70.4 | 38.4 | 0.566 | 1.04 |
| realerr | 1 | 9.25 | 12.5 | 5.55 | 0.741 | 1.66 | 9.26 | 12.5 | 5.55 | 0.742 | 1.67 | 5.36 | 7.12 | 2.78 | 0.753 | 1.93 |
| realerr | all | 23.2 | 30.1 | 15.1 | 0.771 | 1.54 | 23.2 | 30.1 | 15.1 | 0.771 | 1.54 | 18.2 | 24.7 | 12.2 | 0.739 | 1.5 |

### tsx: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0329 (real), 0.422 (realerr), 0.0785 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.374 | 8700 | 8.19 | 1100 | 103000 | 1100 | 103000 | 0.00034 |
| real | steady | 0.0329 | 33.8 | 8.17 | 1100 | 103000 | 1100 | 103000 | 0 |
| realerr | cold | 14.7 | 62300 | 8.6 | 1190 | 122000 | 1190 | 122000 | 0.0123 |
| realerr | steady | 0.422 | 188 | 8.53 | 1190 | 122000 | 1190 | 122000 | 0.000354 |
| broken | cold | 4.19 | 28000 | 8.34 | 1850 | 223000 | 1850 | 223000 | 0.00226 |
| broken | steady | 0.0785 | 1580 | 8.34 | 1850 | 222000 | 1850 | 222000 | 0 |

### tsx: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2041 B | init ms | 0.00827 | 0.0115 | 0.00582 | 0.717 | 1.42 | 15 |
| #0 2041 B | parse1 ms | 0.234 | 0.216 | 0.186 | 1.08 | 1.26 | 15 |
| #0 2041 B | init+parse1 ms | 0.241 | 0.228 | 0.193 | 1.06 | 1.25 | 15 |
| #0 2041 B | parse2 ms | 0.0495 | 0.151 | 0.111 | 0.328 | 0.446 | 15 |
| #0 2041 B | process user+sys ms | 0.982 | 1.38 | 0.586 | 0.713 | 1.68 | 15 |
| #0 2041 B | exec to exit instr user+kernel M | 6.85 | 9.04 | 4.7 | 0.758 | 1.46 | 15 |
| #0 2041 B | exec to exit instr user M | 4.04 | 5.24 | 3.65 | 0.771 | 1.11 | 15 |
| #0 2041 B | exec to exit instr kernel M | 2.8 | 3.79 | 1.04 | 0.74 | 2.69 | 15 |
| #0 2041 B | exec to exit cycles user+kernel M | 4.46 | 6.33 | 2.55 | 0.704 | 1.75 | 15 |
| #0 2041 B | exec to exit task-clock ms | 0.91 | 1.29 | 0.49 | 0.705 | 1.86 | 15 |
| #0 2041 B | peak RSS MB | 4.92 | 6.06 | 3.99 | 0.812 | 1.23 | 15 |
| #0 2041 B | peak footprint MB | 4.95 | 6.09 | 3.93 | 0.812 | 1.26 | 15 |
| #1 975 B | init ms | 0.00873 | 0.0115 | 0.00618 | 0.756 | 1.41 | 15 |
| #1 975 B | parse1 ms | 0.143 | 0.184 | 0.141 | 0.775 | 1.01 | 15 |
| #1 975 B | init+parse1 ms | 0.155 | 0.196 | 0.146 | 0.794 | 1.06 | 15 |
| #1 975 B | parse2 ms | 0.0474 | 0.117 | 0.073 | 0.406 | 0.65 | 15 |
| #1 975 B | process user+sys ms | 0.906 | 1.31 | 0.507 | 0.693 | 1.79 | 15 |
| #1 975 B | exec to exit instr user+kernel M | 5.5 | 8.11 | 3.6 | 0.678 | 1.53 | 15 |
| #1 975 B | exec to exit instr user M | 2.69 | 4.38 | 2.55 | 0.614 | 1.05 | 15 |
| #1 975 B | exec to exit instr kernel M | 2.81 | 3.71 | 1.05 | 0.756 | 2.67 | 15 |
| #1 975 B | exec to exit cycles user+kernel M | 4.02 | 6.01 | 2.15 | 0.67 | 1.87 | 15 |
| #1 975 B | exec to exit task-clock ms | 0.84 | 1.23 | 0.42 | 0.683 | 2 | 15 |
| #1 975 B | peak RSS MB | 5.02 | 6.14 | 3.96 | 0.818 | 1.27 | 15 |
| #1 975 B | peak footprint MB | 5.05 | 6.17 | 3.93 | 0.819 | 1.29 | 15 |

### tsx: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 578 | 666301 | 0 | 0 | ok |
| realerr | 45 | 43241 | 45 | 0 | ok |
| broken | 497 | 527636 | 432 | 0 | ok |

## typescript

### typescript: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 226 | 1110 | 574 | 572 | 540 | 615 | 0.204 | 0.395 | 0.396 | 0.42 | 0.368 | 233 | 1120 | 584 | 543 | 618 | 0.208 | 0.399 | 0.43 | 0.378 | 226-245 | 574-599 | 1.01 | 1.01 | 1 | 17.3 |
| broken | cursor | all | 38.6 | 212 | 87.7 | 87.4 | 90.7 | 94.4 | 0.182 | 0.44 | 0.441 | 0.425 | 0.409 | 47 | 222 | 90.1 | 95.4 | 100 | 0.212 | 0.522 | 0.493 | 0.47 | 38.6-56.4 | 87.7-103 | 8.76 * | 8.82 * | 10.8 * | 102 |
| broken | parse | 1 | 189 | 797 | 522 | 521 | 451 | 554 | 0.237 | 0.362 | 0.363 | 0.419 | 0.341 | 196 | 804 | 532 | 458 | 560 | 0.244 | 0.369 | 0.429 | 0.35 | 189-200 | 522-576 | 1 | 1.01 | 1 | 20.7 |
| broken | parse | all | 32.5 | 123 | 83.2 | 82.6 | 69.8 | 90.8 | 0.264 | 0.39 | 0.393 | 0.465 | 0.358 | 34 | 129 | 85.9 | 73.6 | 91.5 | 0.263 | 0.396 | 0.462 | 0.372 | 32.5-36.1 | 83.2-95.5 | 10.2 * | 10.3 * | 10.3 * | 121 |
| broken | walk | 1 | 1450 | 7200 | 1940 | n/a | n/a | n/a | 0.202 | 0.748 | n/a | n/a | n/a | 1490 | 7260 | 2190 | n/a | n/a | 0.205 | 0.682 | n/a | n/a | 1450-1570 | 1940-2290 | 1 | 1.03 | 1 | 2.7 |
| broken | walk | all | 1140 | 5690 | 1270 | n/a | n/a | n/a | 0.2 | 0.895 | n/a | n/a | n/a | 1170 | 5730 | 1330 | n/a | n/a | 0.203 | 0.878 | n/a | n/a | 1140-1240 | 1270-1650 | 1.43 * | 1.42 * | 1.76 * | 3.44 |
| real | cursor | 1 | 403 | 1730 | 749 | 803 | 817 | 811 | 0.232 | 0.538 | 0.501 | 0.493 | 0.496 | 412 | 1760 | 767 | 825 | 828 | 0.234 | 0.537 | 0.499 | 0.497 | 403-426 | 749-798 | 1 | 1.01 | 1 | 29 |
| real | cursor | all | 52.9 | 238 | 95 | 93.3 | 94.6 | 101 | 0.222 | 0.557 | 0.567 | 0.559 | 0.524 | 53.8 | 243 | 96.4 | 96.9 | 105 | 0.222 | 0.558 | 0.555 | 0.513 | 52.9-56.4 | 95-101 | 14.2 | 13.4 | 14.1 | 221 |
| real | parse | 1 | 321 | 1050 | 644 | 651 | 629 | 705 | 0.306 | 0.498 | 0.493 | 0.51 | 0.455 | 325 | 1060 | 656 | 641 | 706 | 0.308 | 0.496 | 0.507 | 0.461 | 321-350 | 644-687 | 1 | 1 | 1 | 36.4 |
| real | parse | all | 42 | 134 | 80.8 | 80.1 | 76.7 | 92.1 | 0.313 | 0.519 | 0.524 | 0.547 | 0.456 | 43.2 | 138 | 84 | 77.9 | 92.7 | 0.314 | 0.515 | 0.555 | 0.466 | 42-45.2 | 80.8-87.5 | 14.1 | 13.6 | 14.1 | 278 |
| real | walk | 1 | 387 | 1610 | 711 | n/a | n/a | n/a | 0.241 | 0.545 | n/a | n/a | n/a | 392 | 1630 | 725 | n/a | n/a | 0.24 | 0.54 | n/a | n/a | 387-406 | 711-773 | 1 | 1.01 | 1 | 30.2 |
| real | walk | all | 50.8 | 217 | 88.6 | n/a | n/a | n/a | 0.234 | 0.573 | n/a | n/a | n/a | 51.6 | 222 | 89.8 | n/a | n/a | 0.233 | 0.574 | n/a | n/a | 50.8-55.7 | 88.6-99.3 | 13.9 | 13.2 | 14.2 | 230 |
| realerr | cursor | 1 | 8.21 | 33.1 | 15.9 | 16.3 | 16.4 | 17.3 | 0.248 | 0.516 | 0.503 | 0.502 | 0.476 | 8.42 | 34.6 | 16.3 | 16.6 | 18.1 | 0.243 | 0.517 | 0.508 | 0.465 | 8.21-9.08 | 15.9-17.5 | 1 | 1.01 | 1 | 20.5 |
| realerr | cursor | all | 3.84 | 13.9 | 6.38 | 6.43 | 7.08 | 7.14 | 0.276 | 0.602 | 0.596 | 0.542 | 0.538 | 4.22 | 14.6 | 7.05 | 7.18 | 7.58 | 0.288 | 0.599 | 0.588 | 0.557 | 3.84-4.69 | 6.38-8.11 | 2.93 * | 3.07 * | 3.17 * | 43.8 |
| realerr | parse | 1 | 6.89 | 22.4 | 14.3 | 14.8 | 13.2 | 15.5 | 0.307 | 0.481 | 0.466 | 0.521 | 0.443 | 6.95 | 23.4 | 15 | 13.8 | 15.8 | 0.297 | 0.465 | 0.502 | 0.441 | 6.89-8.33 | 14.3-17.1 | 1 | 1.01 | 1 | 24.4 |
| realerr | parse | all | 3.01 | 8.56 | 5.45 | 5.88 | 5.68 | 6.18 | 0.352 | 0.553 | 0.512 | 0.531 | 0.487 | 3.36 | 9.16 | 5.95 | 5.76 | 6.6 | 0.366 | 0.564 | 0.583 | 0.508 | 3.01-4.2 | 5.45-6.42 | 3.03 * | 3.46 * | 3.38 * | 55.8 |
| realerr | walk | 1 | 7.99 | 31.5 | 15.5 | n/a | n/a | n/a | 0.253 | 0.517 | n/a | n/a | n/a | 8.14 | 32.8 | 15.9 | n/a | n/a | 0.248 | 0.513 | n/a | n/a | 7.99-11.9 | 15.5-18 | 1 | 1.01 | 1 | 21 |
| realerr | walk | all | 3.53 | 13.3 | 5.74 | n/a | n/a | n/a | 0.265 | 0.614 | n/a | n/a | n/a | 4.08 | 13.9 | 6.56 | n/a | n/a | 0.294 | 0.621 | n/a | n/a | 3.53-4.82 | 5.74-7.4 | 2.94 * | 3.09 * | 3.27 * | 47.7 |

### typescript: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 227 | 1120 | 574 | 572 | 540 | 615 | 0.203 | 0.396 | 0.398 | 0.422 | 0.37 | 235 | 1130 | 584 | 543 | 618 | 0.207 | 0.401 | 0.432 | 0.38 | 227-248 | 574-599 |
| broken | cursor | all | 397 | 1930 | 964 | 965 | 888 | 1030 | 0.205 | 0.412 | 0.412 | 0.447 | 0.385 | 418 | 1970 | 986 | 906 | 1050 | 0.212 | 0.424 | 0.461 | 0.398 | 397-430 | 964-1010 |
| broken | parse | 1 | 189 | 801 | 522 | 521 | 451 | 554 | 0.236 | 0.362 | 0.363 | 0.42 | 0.342 | 196 | 809 | 532 | 458 | 560 | 0.243 | 0.369 | 0.429 | 0.35 | 189-200 | 522-576 |
| broken | parse | all | 333 | 1340 | 867 | 844 | 747 | 913 | 0.248 | 0.384 | 0.395 | 0.446 | 0.365 | 349 | 1360 | 892 | 755 | 949 | 0.256 | 0.392 | 0.462 | 0.368 | 333-357 | 867-910 |
| broken | walk | 1 | 1450 | 7410 | 1940 | n/a | n/a | n/a | 0.196 | 0.749 | n/a | n/a | n/a | 1490 | 7500 | 2190 | n/a | n/a | 0.199 | 0.682 | n/a | n/a | 1450-1570 | 1940-2290 |
| broken | walk | all | 1640 | 8180 | 2310 | n/a | n/a | n/a | 0.2 | 0.709 | n/a | n/a | n/a | 1670 | 8230 | 2360 | n/a | n/a | 0.202 | 0.705 | n/a | n/a | 1640-1770 | 2310-2680 |
| real | cursor | 1 | 403 | 1750 | 749 | 803 | 817 | 811 | 0.231 | 0.538 | 0.502 | 0.493 | 0.497 | 412 | 1770 | 767 | 825 | 828 | 0.232 | 0.537 | 0.499 | 0.498 | 403-427 | 749-797 |
| real | cursor | all | 743 | 3200 | 1320 | 1340 | 1320 | 1420 | 0.232 | 0.565 | 0.553 | 0.564 | 0.524 | 770 | 3250 | 1380 | 1370 | 1460 | 0.237 | 0.558 | 0.564 | 0.528 | 743-774 | 1320-1390 |
| real | parse | 1 | 321 | 1050 | 644 | 651 | 629 | 705 | 0.305 | 0.499 | 0.493 | 0.511 | 0.455 | 326 | 1060 | 656 | 641 | 706 | 0.307 | 0.497 | 0.508 | 0.461 | 321-350 | 644-687 |
| real | parse | all | 590 | 1860 | 1160 | 1140 | 1110 | 1230 | 0.317 | 0.509 | 0.519 | 0.534 | 0.481 | 612 | 1880 | 1180 | 1110 | 1230 | 0.326 | 0.517 | 0.553 | 0.498 | 590-619 | 1160-1200 |
| real | walk | 1 | 388 | 1620 | 711 | n/a | n/a | n/a | 0.239 | 0.545 | n/a | n/a | n/a | 392 | 1640 | 725 | n/a | n/a | 0.239 | 0.541 | n/a | n/a | 388-407 | 711-772 |
| real | walk | all | 694 | 2920 | 1240 | n/a | n/a | n/a | 0.238 | 0.561 | n/a | n/a | n/a | 733 | 2940 | 1300 | n/a | n/a | 0.25 | 0.566 | n/a | n/a | 694-741 | 1240-1300 |
| realerr | cursor | 1 | 8.23 | 33.2 | 15.9 | 16.3 | 16.4 | 17.3 | 0.248 | 0.516 | 0.503 | 0.503 | 0.477 | 8.44 | 35.1 | 16.3 | 16.6 | 18.1 | 0.24 | 0.519 | 0.51 | 0.466 | 8.23-9.11 | 15.9-17.5 |
| realerr | cursor | all | 11.5 | 42.9 | 20.2 | 21.9 | 21 | 23.3 | 0.267 | 0.568 | 0.524 | 0.545 | 0.493 | 12.2 | 45.9 | 22.1 | 21.3 | 23.5 | 0.267 | 0.554 | 0.576 | 0.521 | 11.5-13.3 | 20.2-23.8 |
| realerr | parse | 1 | 6.9 | 22.6 | 14.3 | 14.8 | 13.2 | 15.5 | 0.305 | 0.482 | 0.467 | 0.522 | 0.444 | 6.97 | 23.5 | 15 | 13.8 | 15.8 | 0.296 | 0.466 | 0.504 | 0.442 | 6.9-8.36 | 14.3-17.1 |
| realerr | parse | all | 9.48 | 29 | 18.5 | 19.1 | 17.2 | 20.8 | 0.327 | 0.512 | 0.495 | 0.55 | 0.456 | 10 | 32.3 | 20.3 | 18.3 | 22.2 | 0.311 | 0.495 | 0.55 | 0.451 | 9.48-12.3 | 18.5-22.6 |
| realerr | walk | 1 | 7.99 | 31.8 | 15.5 | n/a | n/a | n/a | 0.251 | 0.517 | n/a | n/a | n/a | 8.17 | 33.4 | 15.8 | n/a | n/a | 0.245 | 0.516 | n/a | n/a | 7.99-11.9 | 15.5-18 |
| realerr | walk | all | 11 | 39.3 | 20.2 | n/a | n/a | n/a | 0.281 | 0.547 | n/a | n/a | n/a | 12.1 | 42.5 | 21.1 | n/a | n/a | 0.285 | 0.572 | n/a | n/a | 11-13.8 | 20.2-23.9 |

### typescript: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 5060 | 20400 | 10600 | 10400 | 10000 | 10900 | 0.249 | 0.479 | 0.485 | 0.506 | 0.464 | 5060 | 20400 | 10600 | 10000 | 10900 | 0.248 | 0.479 | 0.506 | 0.464 | 5060-5060 | 10600-10600 |
| broken | cursor | all | 5080 | 20400 | 10600 | 10400 | 10000 | 10900 | 0.249 | 0.48 | 0.487 | 0.508 | 0.465 | 5090 | 20500 | 10600 | 10000 | 10900 | 0.249 | 0.481 | 0.508 | 0.466 | 5080-5170 | 10600-10600 |
| broken | parse | 1 | 4150 | 13800 | 9540 | 9370 | 8860 | 9820 | 0.3 | 0.434 | 0.443 | 0.468 | 0.422 | 4150 | 13800 | 9560 | 8870 | 9840 | 0.3 | 0.434 | 0.467 | 0.421 | 4150-4150 | 9540-9560 |
| broken | parse | all | 4160 | 13800 | 9560 | 9380 | 8880 | 9830 | 0.303 | 0.435 | 0.444 | 0.469 | 0.424 | 4170 | 13800 | 9570 | 8890 | 9860 | 0.302 | 0.436 | 0.469 | 0.423 | 4160-4200 | 9560-9590 |
| broken | walk | 1 | 37100 | 95900 | 50400 | n/a | n/a | n/a | 0.387 | 0.736 | n/a | n/a | n/a | 37100 | 96000 | 50500 | n/a | n/a | 0.387 | 0.736 | n/a | n/a | 37100-37200 | 50400-50500 |
| broken | walk | all | 37200 | 96100 | 50500 | n/a | n/a | n/a | 0.387 | 0.737 | n/a | n/a | n/a | 37200 | 96200 | 50500 | n/a | n/a | 0.387 | 0.737 | n/a | n/a | 37200-37200 | 50500-50500 |
| real | cursor | 1 | 9020 | 31500 | 13200 | 13100 | 12900 | 13900 | 0.287 | 0.682 | 0.688 | 0.7 | 0.651 | 9020 | 31500 | 13200 | 12900 | 13900 | 0.286 | 0.682 | 0.7 | 0.651 | 9020-9020 | 13200-13200 |
| real | cursor | all | 9020 | 31500 | 13200 | 13100 | 12800 | 13900 | 0.287 | 0.682 | 0.69 | 0.703 | 0.651 | 9020 | 31600 | 13200 | 12900 | 13900 | 0.286 | 0.681 | 0.7 | 0.65 | 9020-9020 | 13200-13300 |
| real | parse | 1 | 7090 | 17200 | 10900 | 10700 | 10300 | 11400 | 0.411 | 0.649 | 0.661 | 0.686 | 0.62 | 7090 | 17200 | 11000 | 10400 | 11500 | 0.411 | 0.645 | 0.681 | 0.616 | 7090-7090 | 10900-11000 |
| real | parse | all | 7090 | 17100 | 11000 | 10800 | 10400 | 11500 | 0.415 | 0.646 | 0.657 | 0.68 | 0.615 | 7090 | 17200 | 11000 | 10400 | 11600 | 0.412 | 0.642 | 0.679 | 0.614 | 7090-7090 | 11000-11100 |
| real | walk | 1 | 8580 | 27500 | 12000 | n/a | n/a | n/a | 0.312 | 0.713 | n/a | n/a | n/a | 8580 | 27500 | 12100 | n/a | n/a | 0.312 | 0.708 | n/a | n/a | 8580-8580 | 12000-12100 |
| real | walk | all | 8580 | 27400 | 12100 | n/a | n/a | n/a | 0.313 | 0.708 | n/a | n/a | n/a | 8580 | 27600 | 12100 | n/a | n/a | 0.311 | 0.706 | n/a | n/a | 8580-8580 | 12100-12200 |
| realerr | cursor | 1 | 167 | 584 | 274 | 270 | 264 | 286 | 0.286 | 0.609 | 0.618 | 0.633 | 0.584 | 167 | 588 | 276 | 266 | 288 | 0.284 | 0.606 | 0.629 | 0.581 | 167-167 | 274-276 |
| realerr | cursor | all | 173 | 586 | 270 | 267 | 263 | 286 | 0.295 | 0.639 | 0.648 | 0.657 | 0.605 | 175 | 589 | 275 | 265 | 287 | 0.297 | 0.637 | 0.661 | 0.611 | 173-180 | 270-280 |
| realerr | parse | 1 | 136 | 355 | 240 | 235 | 226 | 250 | 0.384 | 0.568 | 0.58 | 0.602 | 0.545 | 136 | 355 | 241 | 227 | 250 | 0.384 | 0.567 | 0.602 | 0.545 | 136-136 | 240-242 |
| realerr | parse | all | 142 | 325 | 237 | 231 | 223 | 248 | 0.436 | 0.6 | 0.614 | 0.636 | 0.572 | 143 | 356 | 241 | 228 | 251 | 0.403 | 0.596 | 0.629 | 0.571 | 142-150 | 237-245 |
| realerr | walk | 1 | 160 | 521 | 258 | n/a | n/a | n/a | 0.308 | 0.622 | n/a | n/a | n/a | 160 | 523 | 258 | n/a | n/a | 0.307 | 0.621 | n/a | n/a | 160-160 | 258-260 |
| realerr | walk | all | 166 | 500 | 257 | n/a | n/a | n/a | 0.331 | 0.645 | n/a | n/a | n/a | 168 | 524 | 259 | n/a | n/a | 0.32 | 0.647 | n/a | n/a | 166-173 | 257-262 |

### typescript: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1190 | 5760 | 2970 | 2970 | 2800 | 3200 | 0.206 | 0.399 | 0.399 | 0.423 | 0.371 | 1220 | 5830 | 3030 | 2820 | 3210 | 0.209 | 0.403 | 0.433 | 0.381 | 1190-1280 | 2970-3110 |
| broken | cursor | all | 1840 | 8720 | 4390 | 4430 | 4100 | 4750 | 0.212 | 0.42 | 0.417 | 0.449 | 0.389 | 1910 | 8830 | 4490 | 4120 | 4800 | 0.216 | 0.425 | 0.464 | 0.398 | 1840-1950 | 4390-4600 |
| broken | parse | 1 | 987 | 4140 | 2710 | 2700 | 2330 | 2870 | 0.238 | 0.365 | 0.365 | 0.423 | 0.344 | 1020 | 4190 | 2760 | 2370 | 2910 | 0.244 | 0.371 | 0.432 | 0.352 | 987-1040 | 2710-2990 |
| broken | parse | all | 1540 | 6140 | 4020 | 3880 | 3420 | 4270 | 0.251 | 0.383 | 0.397 | 0.45 | 0.361 | 1600 | 6200 | 4090 | 3500 | 4400 | 0.258 | 0.391 | 0.457 | 0.364 | 1540-1630 | 4020-4160 |
| broken | walk | 1 | 7580 | 37700 | 10100 | n/a | n/a | n/a | 0.201 | 0.749 | n/a | n/a | n/a | 7780 | 38000 | 11400 | n/a | n/a | 0.205 | 0.683 | n/a | n/a | 7580-8180 | 10100-12000 |
| broken | walk | all | 8240 | 40500 | 11500 | n/a | n/a | n/a | 0.204 | 0.72 | n/a | n/a | n/a | 8390 | 40600 | 11800 | n/a | n/a | 0.207 | 0.714 | n/a | n/a | 8240-8920 | 11500-13400 |
| real | cursor | 1 | 2100 | 9070 | 3910 | 4190 | 4260 | 4230 | 0.231 | 0.537 | 0.5 | 0.492 | 0.496 | 2150 | 9190 | 4000 | 4310 | 4320 | 0.233 | 0.536 | 0.498 | 0.496 | 2100-2220 | 3910-4150 |
| real | cursor | all | 3340 | 14200 | 5960 | 6040 | 5990 | 6430 | 0.235 | 0.56 | 0.553 | 0.557 | 0.519 | 3430 | 14300 | 6190 | 6200 | 6560 | 0.239 | 0.554 | 0.553 | 0.522 | 3340-3440 | 5960-6210 |
| real | parse | 1 | 1680 | 5470 | 3360 | 3400 | 3280 | 3680 | 0.306 | 0.498 | 0.493 | 0.51 | 0.455 | 1700 | 5520 | 3420 | 3350 | 3680 | 0.307 | 0.496 | 0.508 | 0.461 | 1680-1830 | 3360-3590 |
| real | parse | all | 2650 | 8310 | 5230 | 5150 | 5010 | 5560 | 0.319 | 0.507 | 0.515 | 0.529 | 0.477 | 2740 | 8390 | 5330 | 5040 | 5590 | 0.327 | 0.514 | 0.544 | 0.49 | 2650-2760 | 5230-5370 |
| real | walk | 1 | 2020 | 8390 | 3710 | n/a | n/a | n/a | 0.241 | 0.545 | n/a | n/a | n/a | 2050 | 8520 | 3780 | n/a | n/a | 0.24 | 0.541 | n/a | n/a | 2020-2120 | 3710-4030 |
| real | walk | all | 3140 | 12900 | 5630 | n/a | n/a | n/a | 0.243 | 0.558 | n/a | n/a | n/a | 3280 | 13000 | 5840 | n/a | n/a | 0.252 | 0.561 | n/a | n/a | 3140-3310 | 5630-5850 |
| realerr | cursor | 1 | 42.9 | 171 | 83.1 | 85.2 | 85.3 | 90 | 0.251 | 0.516 | 0.503 | 0.503 | 0.476 | 43.5 | 178 | 84.7 | 86.4 | 94.3 | 0.244 | 0.513 | 0.503 | 0.461 | 42.9-47.3 | 83.1-91.4 |
| realerr | cursor | all | 57.9 | 214 | 102 | 108 | 106 | 117 | 0.27 | 0.566 | 0.537 | 0.546 | 0.497 | 59.6 | 225 | 110 | 108 | 120 | 0.265 | 0.542 | 0.553 | 0.498 | 57.9-65.7 | 102-119 |
| realerr | parse | 1 | 36 | 117 | 74.6 | 77 | 69 | 81.1 | 0.308 | 0.482 | 0.467 | 0.522 | 0.444 | 36.3 | 122 | 77.9 | 72.1 | 82.2 | 0.298 | 0.466 | 0.503 | 0.441 | 36-43.5 | 74.6-89.4 |
| realerr | parse | all | 48.2 | 137 | 95 | 95.6 | 86.4 | 105 | 0.351 | 0.507 | 0.504 | 0.558 | 0.457 | 50.6 | 156 | 98.2 | 89 | 109 | 0.324 | 0.515 | 0.568 | 0.463 | 48.2-59.8 | 95-106 |
| realerr | walk | 1 | 41.7 | 164 | 80.6 | n/a | n/a | n/a | 0.254 | 0.518 | n/a | n/a | n/a | 42.4 | 171 | 82.5 | n/a | n/a | 0.248 | 0.515 | n/a | n/a | 41.7-61.9 | 80.6-93.9 |
| realerr | walk | all | 56 | 196 | 101 | n/a | n/a | n/a | 0.286 | 0.556 | n/a | n/a | n/a | 59.9 | 210 | 105 | n/a | n/a | 0.285 | 0.572 | n/a | n/a | 56-65.6 | 101-111 |

### typescript: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 916 | 6540 | 1030 | 1060 | 1140 | 1080 | 0.14 | 0.891 | 0.865 | 0.804 | 0.847 |
| broken | cursor minus parse | 1 | cycles M | 198 | 1610 | 267 | 268 | 469 | 325 | 0.123 | 0.744 | 0.74 | 0.423 | 0.61 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 38.3 | 317 | 52.1 | 51.2 | 88.9 | 61.2 | 0.121 | 0.735 | 0.749 | 0.431 | 0.626 |
| broken | cursor minus parse | all | instructions M | 916 | 6660 | 1010 | 1050 | 1120 | 1090 | 0.138 | 0.905 | 0.873 | 0.817 | 0.843 |
| broken | cursor minus parse | all | cycles M | 305 | 2580 | 370 | 547 | 682 | 480 | 0.118 | 0.824 | 0.557 | 0.447 | 0.634 |
| broken | cursor minus parse | all | CPU user+sys ms | 63.9 | 589 | 97.7 | 121 | 141 | 119 | 0.109 | 0.654 | 0.53 | 0.454 | 0.537 |
| broken | walk minus parse | 1 | instructions M | 33000 | 82100 | 40900 | n/a | n/a | n/a | 0.402 | 0.807 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 6600 | 33500 | 7410 | n/a | n/a | n/a | 0.197 | 0.89 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 1260 | 6610 | 1420 | n/a | n/a | n/a | 0.191 | 0.891 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 33000 | 82300 | 40900 | n/a | n/a | n/a | 0.401 | 0.807 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 6700 | 34300 | 7430 | n/a | n/a | n/a | 0.195 | 0.902 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 1300 | 6830 | 1440 | n/a | n/a | n/a | 0.191 | 0.904 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1930 | 14200 | 2310 | 2370 | 2540 | 2430 | 0.136 | 0.834 | 0.812 | 0.759 | 0.795 |
| real | cursor minus parse | 1 | cycles M | 423 | 3590 | 544 | 794 | 979 | 552 | 0.118 | 0.777 | 0.533 | 0.432 | 0.766 |
| real | cursor minus parse | 1 | CPU user+sys ms | 81.8 | 695 | 105 | 152 | 188 | 106 | 0.118 | 0.781 | 0.538 | 0.435 | 0.773 |
| real | cursor minus parse | all | instructions M | 1930 | 14400 | 2240 | 2290 | 2400 | 2320 | 0.134 | 0.859 | 0.842 | 0.802 | 0.83 |
| real | cursor minus parse | all | cycles M | 686 | 5870 | 724 | 887 | 982 | 874 | 0.117 | 0.948 | 0.774 | 0.699 | 0.786 |
| real | cursor minus parse | all | CPU user+sys ms | 153 | 1340 | 157 | 207 | 211 | 193 | 0.114 | 0.975 | 0.739 | 0.725 | 0.791 |
| real | walk minus parse | 1 | instructions M | 1490 | 10300 | 1120 | n/a | n/a | n/a | 0.145 | 1.33 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 348 | 2920 | 346 | n/a | n/a | n/a | 0.119 | 1 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 66.5 | 566 | 66.8 | n/a | n/a | n/a | 0.118 | 0.995 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1490 | 10300 | 1150 | n/a | n/a | n/a | 0.145 | 1.3 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 490 | 4590 | 392 | n/a | n/a | n/a | 0.107 | 1.25 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 104 | 1060 | 78.5 | n/a | n/a | n/a | 0.0981 | 1.32 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 30.7 | 229 | 34 | 35 | 37.7 | 35.9 | 0.134 | 0.902 | 0.876 | 0.814 | 0.855 |
| realerr | cursor minus parse | 1 | cycles M | 6.88 | 54 | 8.48 | 8.23 | 16.4 | 8.93 | 0.127 | 0.812 | 0.836 | 0.421 | 0.77 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 1.33 | 10.6 | 1.62 | 1.57 | 3.16 | 1.73 | 0.126 | 0.819 | 0.845 | 0.419 | 0.766 |
| realerr | cursor minus parse | all | instructions M | 30.9 | 261 | 33.7 | 35.5 | 40 | 37.5 | 0.118 | 0.916 | 0.871 | 0.771 | 0.824 |
| realerr | cursor minus parse | all | cycles M | 9.69 | 76.6 | 7.25 | 12.2 | 19.7 | 11.1 | 0.126 | 1.34 | 0.792 | 0.492 | 0.87 |
| realerr | cursor minus parse | all | CPU user+sys ms | 2 | 13.9 | 1.71 | 2.75 | 3.81 | 2.5 | 0.144 | 1.17 | 0.725 | 0.524 | 0.8 |
| realerr | walk minus parse | 1 | instructions M | 24 | 166 | 17.7 | n/a | n/a | n/a | 0.144 | 1.36 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 5.74 | 47.2 | 6 | n/a | n/a | n/a | 0.122 | 0.957 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 1.09 | 9.18 | 1.15 | n/a | n/a | n/a | 0.119 | 0.948 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 23.6 | 175 | 19.9 | n/a | n/a | n/a | 0.135 | 1.18 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 7.83 | 58.8 | 5.67 | n/a | n/a | n/a | 0.133 | 1.38 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 1.54 | 10.3 | 1.65 | n/a | n/a | n/a | 0.15 | 0.932 | n/a | n/a | n/a |

### typescript: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 118 | 222 | 51.9 | 0.532 | 2.27 | 118 | 222 | 51.9 | 0.532 | 2.27 | 109 | 212 | 45.9 | 0.516 | 2.39 |
| broken | cursor | all | 212 | 449 | 293 | 0.472 | 0.725 | 212 | 449 | 293 | 0.472 | 0.725 | 203 | 439 | 286 | 0.462 | 0.708 |
| broken | parse | 1 | 53 | 51.1 | 41.8 | 1.04 | 1.27 | 53 | 51.1 | 41.8 | 1.04 | 1.27 | 44.3 | 41.3 | 35.7 | 1.07 | 1.24 |
| broken | parse | all | 137 | 357 | 293 | 0.384 | 0.468 | 137 | 357 | 293 | 0.384 | 0.468 | 128 | 348 | 287 | 0.369 | 0.447 |
| broken | walk | 1 | 81.5 | 212 | 53.4 | 0.384 | 1.52 | 81.5 | 212 | 53.4 | 0.384 | 1.52 | 73 | 203 | 47.4 | 0.36 | 1.54 |
| broken | walk | all | 177 | 388 | 351 | 0.458 | 0.506 | 177 | 388 | 351 | 0.458 | 0.506 | 168 | 378 | 345 | 0.446 | 0.489 |
| real | cursor | 1 | 48 | 106 | 25.5 | 0.451 | 1.88 | 48 | 106 | 25.5 | 0.451 | 1.88 | 30.6 | 88 | 12 | 0.348 | 2.55 |
| real | cursor | all | 107 | 238 | 137 | 0.448 | 0.776 | 107 | 238 | 137 | 0.448 | 0.776 | 89.2 | 219 | 123 | 0.407 | 0.723 |
| real | parse | 1 | 30.7 | 43.3 | 24.3 | 0.709 | 1.26 | 30.7 | 43.3 | 24.3 | 0.71 | 1.27 | 13.5 | 24.6 | 10.7 | 0.547 | 1.26 |
| real | parse | all | 86.9 | 200 | 135 | 0.435 | 0.645 | 86.9 | 200 | 135 | 0.435 | 0.645 | 69.5 | 181 | 121 | 0.384 | 0.575 |
| real | walk | 1 | 30.6 | 108 | 25.5 | 0.283 | 1.2 | 30.6 | 108 | 25.5 | 0.283 | 1.2 | 13.4 | 89.5 | 12 | 0.149 | 1.11 |
| real | walk | all | 85.8 | 236 | 137 | 0.364 | 0.628 | 85.8 | 236 | 137 | 0.364 | 0.628 | 68.4 | 217 | 123 | 0.315 | 0.558 |
| realerr | cursor | 1 | 11.2 | 20.7 | 7.73 | 0.541 | 1.45 | 11.2 | 20.7 | 7.73 | 0.541 | 1.45 | 7.63 | 15.7 | 5.14 | 0.485 | 1.48 |
| realerr | cursor | all | 29 | 32.4 | 21.9 | 0.895 | 1.33 | 29 | 32.4 | 21.9 | 0.896 | 1.33 | 24.6 | 27.3 | 19.3 | 0.9 | 1.27 |
| realerr | parse | 1 | 10.5 | 11 | 6.62 | 0.956 | 1.59 | 10.5 | 11 | 6.62 | 0.957 | 1.59 | 7.06 | 5.9 | 4.09 | 1.2 | 1.73 |
| realerr | parse | all | 26.6 | 27.9 | 21.9 | 0.952 | 1.21 | 26.6 | 28 | 21.9 | 0.952 | 1.21 | 22 | 22.8 | 19.3 | 0.964 | 1.14 |
| realerr | walk | 1 | 11.1 | 21.1 | 7.56 | 0.525 | 1.46 | 11.1 | 21.1 | 7.56 | 0.526 | 1.47 | 7.48 | 16 | 5.04 | 0.468 | 1.48 |
| realerr | walk | all | 28.3 | 31.9 | 21.1 | 0.887 | 1.34 | 28.3 | 31.9 | 21.1 | 0.887 | 1.34 | 23.8 | 26.9 | 18.5 | 0.881 | 1.29 |

### typescript: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 53 | 51.6 | 41.8 | 1.03 | 1.27 | 53 | 51.6 | 41.8 | 1.03 | 1.27 | 44.5 | 41.7 | 35.5 | 1.07 | 1.25 |
| broken | all | 156 | 599 | 364 | 0.26 | 0.428 | 156 | 599 | 364 | 0.26 | 0.428 | 147 | 589 | 357 | 0.249 | 0.41 |
| real | 1 | 30.5 | 43.9 | 24.2 | 0.695 | 1.26 | 30.5 | 43.9 | 24.2 | 0.695 | 1.26 | 13.2 | 25.2 | 10.7 | 0.525 | 1.23 |
| real | all | 87.7 | 275 | 154 | 0.319 | 0.568 | 87.7 | 275 | 154 | 0.319 | 0.569 | 70.4 | 256 | 140 | 0.275 | 0.501 |
| realerr | 1 | 11.2 | 13.7 | 6.47 | 0.819 | 1.74 | 11.3 | 13.7 | 6.47 | 0.821 | 1.74 | 7.63 | 8.8 | 4.05 | 0.866 | 1.89 |
| realerr | all | 41.5 | 63.9 | 35.9 | 0.649 | 1.16 | 41.5 | 63.9 | 35.9 | 0.649 | 1.16 | 37 | 58.9 | 33.3 | 0.628 | 1.11 |

### typescript: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0161 (real), 0.319 (realerr), 0.108 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.154 | 9420 | 8.14 | 2000 | 187000 | 2000 | 187000 | 0 |
| real | steady | 0.0161 | 153 | 8.14 | 2000 | 187000 | 2000 | 187000 | 0 |
| realerr | cold | 11 | 70800 | 8.43 | 806 | 76400 | 806 | 76400 | 0.0136 |
| realerr | steady | 0.319 | 319 | 8.39 | 805 | 76100 | 805 | 76100 | 0.000396 |
| broken | cold | 4.32 | 108000 | 8.39 | 4320 | 520000 | 4320 | 520000 | 0.000999 |
| broken | steady | 0.108 | 6200 | 8.39 | 4320 | 519000 | 4320 | 519000 | 0 |

### typescript: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2046 B | init ms | 0.00936 | 0.0126 | 0.00587 | 0.745 | 1.59 | 15 |
| #0 2046 B | parse1 ms | 0.183 | 0.23 | 0.194 | 0.796 | 0.942 | 15 |
| #0 2046 B | init+parse1 ms | 0.192 | 0.243 | 0.201 | 0.791 | 0.956 | 15 |
| #0 2046 B | parse2 ms | 0.0604 | 0.163 | 0.103 | 0.371 | 0.584 | 15 |
| #0 2046 B | process user+sys ms | 0.975 | 1.43 | 0.589 | 0.682 | 1.66 | 15 |
| #0 2046 B | exec to exit instr user+kernel M | 5.87 | 9.29 | 4.43 | 0.632 | 1.33 | 15 |
| #0 2046 B | exec to exit instr user M | 3.08 | 5.58 | 3.38 | 0.551 | 0.911 | 15 |
| #0 2046 B | exec to exit instr kernel M | 2.8 | 3.73 | 1.05 | 0.75 | 2.66 | 15 |
| #0 2046 B | exec to exit cycles user+kernel M | 4.3 | 6.4 | 2.56 | 0.672 | 1.68 | 15 |
| #0 2046 B | exec to exit task-clock ms | 0.89 | 1.37 | 0.5 | 0.65 | 1.78 | 15 |
| #0 2046 B | peak RSS MB | 5.27 | 6.14 | 3.99 | 0.859 | 1.32 | 15 |
| #0 2046 B | peak footprint MB | 5.3 | 6.17 | 3.96 | 0.859 | 1.34 | 15 |
| #1 1131 B | init ms | 0.00905 | 0.0125 | 0.00582 | 0.725 | 1.55 | 15 |
| #1 1131 B | parse1 ms | 0.147 | 0.202 | 0.152 | 0.726 | 0.969 | 15 |
| #1 1131 B | init+parse1 ms | 0.157 | 0.215 | 0.158 | 0.73 | 0.994 | 15 |
| #1 1131 B | parse2 ms | 0.0532 | 0.156 | 0.0885 | 0.341 | 0.602 | 15 |
| #1 1131 B | process user+sys ms | 0.888 | 1.35 | 0.526 | 0.658 | 1.69 | 15 |
| #1 1131 B | exec to exit instr user+kernel M | 5.85 | 9.17 | 4.22 | 0.639 | 1.39 | 15 |
| #1 1131 B | exec to exit instr user M | 3.07 | 5.46 | 3.17 | 0.562 | 0.968 | 15 |
| #1 1131 B | exec to exit instr kernel M | 2.77 | 3.68 | 1.04 | 0.753 | 2.65 | 15 |
| #1 1131 B | exec to exit cycles user+kernel M | 4.07 | 6.22 | 2.24 | 0.655 | 1.82 | 15 |
| #1 1131 B | exec to exit task-clock ms | 0.83 | 1.27 | 0.43 | 0.654 | 1.93 | 15 |
| #1 1131 B | peak RSS MB | 4.94 | 6.21 | 4.03 | 0.795 | 1.22 | 15 |
| #1 1131 B | peak footprint MB | 4.97 | 6.25 | 4.03 | 0.795 | 1.23 | 15 |

### typescript: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 1431 | 2805704 | 0 | 0 | ok |
| realerr | 69 | 45414 | 69 | 0 | ok |
| broken | 507 | 1294921 | 420 | 0 | ok |

## verilog

### verilog: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 514 | 3650 | 2680 | 2670 | 2620 | 2390 | 2820 | 0.141 | 0.192 | 0.193 | 0.196 | 0.215 | 0.182 | 545 | 3710 | 2720 | 2730 | 2450 | 2930 | 0.147 | 0.2 | 0.2 | 0.223 | 0.186 | 514-584 | 2680-2880 | 1 | 1.01 | 1 | 8.51 |
| broken | cursor | all | 101 | 821 | 640 | 637 | 641 | 603 | 672 | 0.122 | 0.157 | 0.158 | 0.157 | 0.167 | 0.15 | 106 | 843 | 653 | 653 | 613 | 676 | 0.126 | 0.163 | 0.163 | 0.173 | 0.157 | 101-115 | 640-675 | 7.92 * | 6.6 * | 5.98 * | 43.5 |
| broken | parse | 1 | 449 | 2940 | 2390 | 2400 | 2390 | 2080 | 2540 | 0.153 | 0.188 | 0.187 | 0.188 | 0.216 | 0.177 | 453 | 2990 | 2460 | 2450 | 2130 | 2550 | 0.152 | 0.184 | 0.185 | 0.213 | 0.177 | 449-476 | 2390-2520 | 1 | 1.01 | 1 | 9.74 |
| broken | parse | all | 88.6 | 675 | 579 | 575 | 571 | 541 | 605 | 0.131 | 0.153 | 0.154 | 0.155 | 0.164 | 0.146 | 91 | 691 | 587 | 583 | 552 | 611 | 0.132 | 0.155 | 0.156 | 0.165 | 0.149 | 88.6-94.5 | 579-596 | 8.07 * | 6.62 * | 6.23 * | 49.4 |
| broken | walk | 1 | 654 | 4100 | 2620 | 2610 | n/a | n/a | n/a | 0.159 | 0.249 | 0.25 | n/a | n/a | n/a | 706 | 4470 | 2900 | 2900 | n/a | n/a | 0.158 | 0.243 | 0.243 | n/a | n/a | 654-744 | 2620-3020 | 1 | 1.01 | 1 | 6.69 |
| broken | walk | all | 158 | 1090 | 703 | 703 | n/a | n/a | n/a | 0.145 | 0.224 | 0.224 | n/a | n/a | n/a | 164 | 1100 | 725 | 727 | n/a | n/a | 0.149 | 0.227 | 0.226 | n/a | n/a | 158-168 | 703-750 | 6.33 * | 5.69 * | 5.7 * | 27.8 |
| real | cursor | 1 | 25.8 | 107 | 42.4 | 41.3 | 43.9 | 48.3 | 46 | 0.241 | 0.607 | 0.624 | 0.587 | 0.534 | 0.56 | 26 | 112 | 43.3 | 42.4 | 49.3 | 46.1 | 0.233 | 0.601 | 0.613 | 0.528 | 0.565 | 25.8-30.2 | 42.4-46.7 | 1 | 1.01 | 1 | 19.2 |
| real | cursor | all | 3.39 | 16.8 | 5.48 | 5.21 | 5.45 | 5.76 | 5.97 | 0.202 | 0.618 | 0.65 | 0.622 | 0.587 | 0.567 | 3.95 | 18.1 | 5.89 | 5.79 | 6.12 | 6.17 | 0.219 | 0.67 | 0.682 | 0.645 | 0.64 | 3.39-4.82 | 5.48-7.1 | 11.5 | 11.1 * | 12.2 | 146 |
| real | parse | 1 | 21.8 | 68.5 | 37.9 | 36.1 | 38 | 39.4 | 40.5 | 0.319 | 0.577 | 0.604 | 0.575 | 0.554 | 0.539 | 22 | 69.1 | 38.7 | 36.5 | 43 | 40.7 | 0.318 | 0.569 | 0.602 | 0.512 | 0.541 | 21.8-26.3 | 37.9-40.2 | 1 | 1.01 | 1 | 22.7 |
| real | parse | all | 2.9 | 8.9 | 4.75 | 4.6 | 4.72 | 4.73 | 5.07 | 0.325 | 0.61 | 0.63 | 0.613 | 0.612 | 0.572 | 3.34 | 9.5 | 5.16 | 4.88 | 4.96 | 5.43 | 0.351 | 0.647 | 0.685 | 0.673 | 0.615 | 2.9-4.03 | 4.75-5.83 | 11.4 | 12.3 | 12.2 | 171 |
| real | walk | 1 | 25 | 99.2 | 40.8 | 39.1 | n/a | n/a | n/a | 0.253 | 0.615 | 0.641 | n/a | n/a | n/a | 25.2 | 102 | 41.4 | 39.6 | n/a | n/a | 0.248 | 0.608 | 0.636 | n/a | n/a | 25-28.1 | 40.8-43.5 | 1 | 1.01 | 1 | 19.8 |
| real | walk | all | 3.28 | 15.7 | 5.11 | 5.02 | n/a | n/a | n/a | 0.21 | 0.642 | 0.655 | n/a | n/a | n/a | 3.63 | 16.8 | 5.62 | 5.35 | n/a | n/a | 0.216 | 0.647 | 0.679 | n/a | n/a | 3.28-5.04 | 5.11-6.32 | 11.9 | 11 * | 12.2 | 151 |
| realerr | cursor | 1 | 475 | 2590 | 1700 | 1690 | 1700 | 1540 | 1790 | 0.183 | 0.28 | 0.281 | 0.279 | 0.309 | 0.265 | 477 | 2630 | 1750 | 1730 | 1550 | 1820 | 0.181 | 0.273 | 0.276 | 0.308 | 0.262 | 475-500 | 1700-1800 | 1 | 1.01 | 1 | 11 |
| realerr | cursor | all | 97.6 | 730 | 564 | 562 | 561 | 545 | 588 | 0.134 | 0.173 | 0.174 | 0.174 | 0.179 | 0.166 | 101 | 747 | 579 | 585 | 550 | 601 | 0.135 | 0.174 | 0.173 | 0.184 | 0.168 | 97.6-103 | 564-606 | 8.15 * | 5.97 * | 4.98 * | 53.6 |
| realerr | parse | 1 | 432 | 2180 | 1650 | 1640 | 1650 | 1430 | 1730 | 0.198 | 0.262 | 0.264 | 0.262 | 0.301 | 0.25 | 439 | 2220 | 1690 | 1670 | 1450 | 1770 | 0.198 | 0.26 | 0.263 | 0.302 | 0.248 | 432-452 | 1650-1810 | 1 | 1.01 | 1 | 12.1 |
| realerr | parse | all | 93 | 663 | 563 | 560 | 553 | 535 | 579 | 0.14 | 0.165 | 0.166 | 0.168 | 0.174 | 0.161 | 95 | 674 | 578 | 572 | 540 | 583 | 0.141 | 0.164 | 0.166 | 0.176 | 0.163 | 93-98.8 | 563-598 | 7.8 * | 5.43 * | 4.82 * | 56.2 |
| realerr | walk | 1 | 516 | 2800 | 1730 | 1730 | n/a | n/a | n/a | 0.184 | 0.298 | 0.299 | n/a | n/a | n/a | 523 | 2820 | 1780 | 1770 | n/a | n/a | 0.185 | 0.294 | 0.295 | n/a | n/a | 516-588 | 1730-1860 | 1 | 1.01 | 1 | 10.1 |
| realerr | walk | all | 142 | 983 | 623 | 607 | n/a | n/a | n/a | 0.145 | 0.228 | 0.234 | n/a | n/a | n/a | 145 | 993 | 632 | 628 | n/a | n/a | 0.146 | 0.23 | 0.231 | n/a | n/a | 142-151 | 623-649 | 5.96 * | 4.6 * | 4.62 * | 36.8 |

### verilog: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 515 | 3680 | 2680 | 2670 | 2620 | 2390 | 2820 | 0.14 | 0.192 | 0.193 | 0.196 | 0.215 | 0.182 | 546 | 3740 | 2720 | 2730 | 2450 | 2930 | 0.146 | 0.2 | 0.2 | 0.223 | 0.186 | 515-585 | 2680-2880 |
| broken | cursor | all | 832 | 5500 | 3830 | 3810 | 3880 | 3330 | 4090 | 0.151 | 0.217 | 0.218 | 0.214 | 0.25 | 0.204 | 849 | 5550 | 3920 | 3900 | 3420 | 4120 | 0.153 | 0.216 | 0.218 | 0.248 | 0.206 | 832-878 | 3830-4000 |
| broken | parse | 1 | 450 | 2960 | 2390 | 2400 | 2390 | 2080 | 2540 | 0.152 | 0.188 | 0.187 | 0.188 | 0.216 | 0.177 | 453 | 3010 | 2450 | 2450 | 2130 | 2550 | 0.151 | 0.185 | 0.185 | 0.213 | 0.177 | 450-476 | 2390-2520 |
| broken | parse | all | 724 | 4490 | 3630 | 3570 | 3620 | 3120 | 3830 | 0.161 | 0.199 | 0.203 | 0.2 | 0.232 | 0.189 | 739 | 4570 | 3670 | 3640 | 3130 | 3870 | 0.161 | 0.201 | 0.203 | 0.236 | 0.191 | 724-749 | 3630-3690 |
| broken | walk | 1 | 655 | 4140 | 2620 | 2610 | n/a | n/a | n/a | 0.158 | 0.25 | 0.25 | n/a | n/a | n/a | 706 | 4510 | 2900 | 2900 | n/a | n/a | 0.157 | 0.244 | 0.244 | n/a | n/a | 655-745 | 2620-3020 |
| broken | walk | all | 1010 | 6220 | 4080 | 4040 | n/a | n/a | n/a | 0.163 | 0.249 | 0.251 | n/a | n/a | n/a | 1030 | 6270 | 4130 | 4110 | n/a | n/a | 0.164 | 0.249 | 0.251 | n/a | n/a | 1010-1060 | 4080-4220 |
| real | cursor | 1 | 25.8 | 108 | 42.4 | 41.3 | 43.9 | 48.3 | 46 | 0.238 | 0.608 | 0.624 | 0.587 | 0.534 | 0.561 | 26.1 | 113 | 43.3 | 42.4 | 49.3 | 46.1 | 0.231 | 0.601 | 0.614 | 0.528 | 0.566 | 25.8-30.2 | 42.4-46.7 |
| real | cursor | all | 42.5 | 194 | 69.2 | 66.2 | 67.8 | 73.8 | 72.4 | 0.219 | 0.614 | 0.642 | 0.626 | 0.575 | 0.587 | 46.8 | 202 | 72.2 | 71.3 | 74.8 | 75.1 | 0.232 | 0.649 | 0.657 | 0.626 | 0.623 | 42.5-52.1 | 69.2-75.5 |
| real | parse | 1 | 21.9 | 68.6 | 37.9 | 36.1 | 38 | 39.4 | 40.5 | 0.319 | 0.578 | 0.605 | 0.576 | 0.555 | 0.539 | 22 | 69.8 | 38.7 | 36.5 | 43 | 40.7 | 0.316 | 0.57 | 0.603 | 0.513 | 0.542 | 21.9-26.3 | 37.9-40.2 |
| real | parse | all | 34.6 | 115 | 60.6 | 57.7 | 56.4 | 61.1 | 62.3 | 0.301 | 0.571 | 0.599 | 0.613 | 0.566 | 0.555 | 37.7 | 119 | 64.2 | 60.5 | 61.7 | 67.5 | 0.316 | 0.588 | 0.624 | 0.612 | 0.558 | 34.6-44.4 | 60.6-66.7 |
| real | walk | 1 | 25.1 | 101 | 40.7 | 39.1 | n/a | n/a | n/a | 0.25 | 0.616 | 0.642 | n/a | n/a | n/a | 25.3 | 103 | 41.4 | 39.6 | n/a | n/a | 0.245 | 0.61 | 0.637 | n/a | n/a | 25.1-28.1 | 40.7-43.5 |
| real | walk | all | 41.8 | 180 | 65.1 | 63.2 | n/a | n/a | n/a | 0.232 | 0.642 | 0.66 | n/a | n/a | n/a | 44.3 | 183 | 69.4 | 66.3 | n/a | n/a | 0.242 | 0.638 | 0.668 | n/a | n/a | 41.8-50.4 | 65.1-71.6 |
| realerr | cursor | 1 | 475 | 2610 | 1700 | 1690 | 1700 | 1540 | 1790 | 0.182 | 0.28 | 0.282 | 0.279 | 0.309 | 0.265 | 477 | 2650 | 1750 | 1730 | 1550 | 1820 | 0.18 | 0.273 | 0.276 | 0.308 | 0.262 | 475-500 | 1700-1800 |
| realerr | cursor | all | 807 | 4420 | 2810 | 2770 | 2850 | 2430 | 3010 | 0.183 | 0.287 | 0.292 | 0.283 | 0.332 | 0.269 | 823 | 4460 | 2880 | 2860 | 2520 | 3040 | 0.185 | 0.286 | 0.288 | 0.326 | 0.271 | 807-837 | 2810-2950 |
| realerr | parse | 1 | 433 | 2190 | 1650 | 1640 | 1650 | 1430 | 1730 | 0.197 | 0.262 | 0.264 | 0.262 | 0.302 | 0.25 | 439 | 2230 | 1690 | 1670 | 1450 | 1770 | 0.197 | 0.26 | 0.263 | 0.302 | 0.248 | 433-453 | 1650-1810 |
| realerr | parse | all | 724 | 3620 | 2750 | 2730 | 2690 | 2390 | 2920 | 0.2 | 0.264 | 0.265 | 0.269 | 0.303 | 0.248 | 744 | 3670 | 2780 | 2790 | 2400 | 2950 | 0.202 | 0.267 | 0.266 | 0.31 | 0.252 | 724-756 | 2750-2830 |
| realerr | walk | 1 | 517 | 2820 | 1730 | 1730 | n/a | n/a | n/a | 0.183 | 0.298 | 0.299 | n/a | n/a | n/a | 523 | 2850 | 1780 | 1770 | n/a | n/a | 0.184 | 0.295 | 0.295 | n/a | n/a | 517-588 | 1730-1860 |
| realerr | walk | all | 842 | 4540 | 2860 | 2790 | n/a | n/a | n/a | 0.185 | 0.294 | 0.301 | n/a | n/a | n/a | 868 | 4580 | 2910 | 2890 | n/a | n/a | 0.19 | 0.298 | 0.301 | n/a | n/a | 842-880 | 2860-2990 |

### verilog: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 9860 | 63400 | 48900 | 48500 | 47800 | 46800 | 49500 | 0.156 | 0.202 | 0.203 | 0.206 | 0.211 | 0.199 | 9860 | 63400 | 48900 | 48500 | 46800 | 49500 | 0.155 | 0.202 | 0.203 | 0.211 | 0.199 | 9860-9860 | 48900-48900 |
| broken | cursor | all | 9920 | 63500 | 48900 | 48500 | 47800 | 46800 | 49500 | 0.156 | 0.203 | 0.205 | 0.208 | 0.212 | 0.2 | 9960 | 63600 | 48900 | 48500 | 46800 | 49500 | 0.157 | 0.204 | 0.205 | 0.213 | 0.201 | 9920-10200 | 48900-48900 |
| broken | parse | 1 | 8940 | 55700 | 47900 | 47400 | 46700 | 45700 | 48400 | 0.161 | 0.187 | 0.189 | 0.191 | 0.196 | 0.185 | 8940 | 55700 | 47900 | 47500 | 45700 | 48400 | 0.16 | 0.187 | 0.188 | 0.196 | 0.185 | 8940-8940 | 47900-47900 |
| broken | parse | all | 9000 | 55700 | 47900 | 47500 | 46700 | 45700 | 48400 | 0.162 | 0.188 | 0.19 | 0.193 | 0.197 | 0.186 | 9050 | 55700 | 47900 | 47500 | 45700 | 48400 | 0.162 | 0.189 | 0.191 | 0.198 | 0.187 | 9000-9150 | 47900-47900 |
| broken | walk | 1 | 13900 | 71500 | 53700 | 53300 | n/a | n/a | n/a | 0.195 | 0.26 | 0.262 | n/a | n/a | n/a | 13900 | 71500 | 53700 | 53300 | n/a | n/a | 0.195 | 0.26 | 0.262 | n/a | n/a | 13900-13900 | 53700-53700 |
| broken | walk | all | 14000 | 71500 | 53700 | 53300 | n/a | n/a | n/a | 0.196 | 0.261 | 0.263 | n/a | n/a | n/a | 14100 | 71600 | 53700 | 53300 | n/a | n/a | 0.197 | 0.262 | 0.264 | n/a | n/a | 14000-14200 | 53700-53700 |
| real | cursor | 1 | 550 | 1990 | 755 | 723 | 735 | 788 | 791 | 0.276 | 0.728 | 0.761 | 0.748 | 0.698 | 0.695 | 550 | 2000 | 756 | 723 | 788 | 791 | 0.275 | 0.727 | 0.76 | 0.697 | 0.695 | 550-550 | 755-757 |
| real | cursor | all | 550 | 2000 | 756 | 723 | 734 | 789 | 792 | 0.275 | 0.728 | 0.761 | 0.75 | 0.697 | 0.694 | 550 | 2030 | 758 | 726 | 791 | 793 | 0.271 | 0.726 | 0.758 | 0.695 | 0.694 | 550-583 | 756-760 |
| real | parse | 1 | 453 | 1130 | 644 | 611 | 621 | 666 | 673 | 0.399 | 0.703 | 0.74 | 0.729 | 0.679 | 0.673 | 453 | 1130 | 646 | 613 | 668 | 675 | 0.399 | 0.701 | 0.738 | 0.678 | 0.671 | 453-453 | 644-647 |
| real | parse | all | 453 | 1130 | 646 | 614 | 623 | 669 | 674 | 0.401 | 0.701 | 0.738 | 0.727 | 0.677 | 0.672 | 453 | 1140 | 649 | 615 | 671 | 676 | 0.398 | 0.698 | 0.736 | 0.676 | 0.67 | 453-513 | 646-651 |
| real | walk | 1 | 525 | 1730 | 702 | 669 | n/a | n/a | n/a | 0.303 | 0.748 | 0.784 | n/a | n/a | n/a | 525 | 1730 | 703 | 671 | n/a | n/a | 0.303 | 0.746 | 0.782 | n/a | n/a | 525-525 | 702-705 |
| real | walk | all | 526 | 1750 | 704 | 670 | n/a | n/a | n/a | 0.3 | 0.747 | 0.784 | n/a | n/a | n/a | 526 | 1770 | 706 | 673 | n/a | n/a | 0.297 | 0.745 | 0.781 | n/a | n/a | 526-601 | 704-708 |
| realerr | cursor | 1 | 9340 | 49000 | 33900 | 33400 | 33000 | 32500 | 34500 | 0.191 | 0.276 | 0.279 | 0.283 | 0.287 | 0.271 | 9340 | 49000 | 33900 | 33400 | 32500 | 34500 | 0.191 | 0.276 | 0.279 | 0.287 | 0.271 | 9340-9340 | 33900-33900 |
| realerr | cursor | all | 9390 | 49100 | 33900 | 33400 | 33000 | 32500 | 34500 | 0.191 | 0.277 | 0.281 | 0.284 | 0.289 | 0.272 | 9420 | 49100 | 33900 | 33500 | 32500 | 34500 | 0.192 | 0.278 | 0.282 | 0.29 | 0.273 | 9390-9530 | 33900-33900 |
| realerr | parse | 1 | 8340 | 40400 | 32800 | 32300 | 31900 | 31300 | 33300 | 0.207 | 0.255 | 0.258 | 0.262 | 0.267 | 0.251 | 8340 | 40400 | 32800 | 32300 | 31300 | 33300 | 0.206 | 0.255 | 0.258 | 0.267 | 0.251 | 8340-8340 | 32800-32800 |
| realerr | parse | all | 8390 | 40400 | 32800 | 32300 | 31900 | 31300 | 33300 | 0.208 | 0.256 | 0.26 | 0.263 | 0.268 | 0.252 | 8430 | 40400 | 32800 | 32300 | 31300 | 33300 | 0.208 | 0.257 | 0.261 | 0.269 | 0.253 | 8390-8520 | 32800-32800 |
| realerr | walk | 1 | 10600 | 50000 | 35200 | 34700 | n/a | n/a | n/a | 0.211 | 0.301 | 0.304 | n/a | n/a | n/a | 10600 | 50000 | 35200 | 34700 | n/a | n/a | 0.211 | 0.3 | 0.304 | n/a | n/a | 10600-10600 | 35200-35200 |
| realerr | walk | all | 10600 | 50100 | 35200 | 34700 | n/a | n/a | n/a | 0.212 | 0.302 | 0.306 | n/a | n/a | n/a | 10700 | 50100 | 35200 | 34700 | n/a | n/a | 0.212 | 0.303 | 0.307 | n/a | n/a | 10600-10800 | 35200-35200 |

### verilog: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-nopragma min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-nopragma | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-nopragma med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-nopragma | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 2680 | 19000 | 14000 | 13900 | 13700 | 12500 | 14700 | 0.141 | 0.192 | 0.193 | 0.196 | 0.215 | 0.182 | 2840 | 19400 | 14200 | 14200 | 12800 | 15300 | 0.147 | 0.2 | 0.2 | 0.223 | 0.186 | 2680-3040 | 14000-15000 |
| broken | cursor | all | 3770 | 25500 | 18200 | 18300 | 18500 | 15900 | 19500 | 0.148 | 0.207 | 0.206 | 0.204 | 0.237 | 0.194 | 3830 | 25700 | 18600 | 18500 | 16200 | 19600 | 0.149 | 0.206 | 0.207 | 0.237 | 0.195 | 3770-3960 | 18200-18900 |
| broken | parse | 1 | 2350 | 15400 | 12500 | 12500 | 12500 | 10900 | 13300 | 0.153 | 0.188 | 0.187 | 0.188 | 0.216 | 0.177 | 2360 | 15600 | 12800 | 12800 | 11100 | 13300 | 0.151 | 0.184 | 0.185 | 0.213 | 0.177 | 2350-2480 | 12500-13200 |
| broken | parse | all | 3390 | 21500 | 17500 | 17300 | 17500 | 15000 | 18500 | 0.158 | 0.194 | 0.196 | 0.194 | 0.226 | 0.183 | 3430 | 21800 | 17700 | 17600 | 15100 | 18800 | 0.157 | 0.194 | 0.195 | 0.228 | 0.183 | 3390-3480 | 17500-17900 |
| broken | walk | 1 | 3410 | 21400 | 13700 | 13700 | n/a | n/a | n/a | 0.159 | 0.249 | 0.25 | n/a | n/a | n/a | 3680 | 23300 | 15100 | 15100 | n/a | n/a | 0.158 | 0.243 | 0.243 | n/a | n/a | 3410-3880 | 13700-15700 |
| broken | walk | all | 4680 | 29400 | 19500 | 19500 | n/a | n/a | n/a | 0.159 | 0.24 | 0.241 | n/a | n/a | n/a | 4730 | 29600 | 19700 | 19700 | n/a | n/a | 0.16 | 0.24 | 0.241 | n/a | n/a | 4680-4900 | 19500-20100 |
| real | cursor | 1 | 134 | 557 | 221 | 216 | 229 | 252 | 240 | 0.241 | 0.607 | 0.624 | 0.587 | 0.534 | 0.56 | 136 | 581 | 226 | 220 | 257 | 241 | 0.233 | 0.6 | 0.617 | 0.527 | 0.564 | 134-156 | 221-243 |
| real | cursor | all | 208 | 883 | 327 | 320 | 322 | 346 | 348 | 0.236 | 0.636 | 0.651 | 0.646 | 0.602 | 0.598 | 218 | 909 | 338 | 332 | 353 | 355 | 0.239 | 0.643 | 0.655 | 0.616 | 0.613 | 208-237 | 327-350 |
| real | parse | 1 | 114 | 357 | 197 | 189 | 198 | 206 | 212 | 0.319 | 0.578 | 0.605 | 0.576 | 0.555 | 0.539 | 115 | 360 | 202 | 191 | 224 | 212 | 0.319 | 0.569 | 0.603 | 0.513 | 0.542 | 114-137 | 197-209 |
| real | parse | all | 172 | 529 | 286 | 275 | 277 | 287 | 310 | 0.326 | 0.603 | 0.626 | 0.622 | 0.6 | 0.555 | 183 | 552 | 299 | 289 | 294 | 313 | 0.332 | 0.611 | 0.633 | 0.622 | 0.585 | 172-206 | 286-308 |
| real | walk | 1 | 131 | 519 | 213 | 204 | n/a | n/a | n/a | 0.252 | 0.615 | 0.641 | n/a | n/a | n/a | 131 | 531 | 216 | 207 | n/a | n/a | 0.248 | 0.608 | 0.636 | n/a | n/a | 131-145 | 213-227 |
| real | walk | all | 207 | 810 | 316 | 300 | n/a | n/a | n/a | 0.255 | 0.654 | 0.688 | n/a | n/a | n/a | 213 | 825 | 322 | 313 | n/a | n/a | 0.259 | 0.662 | 0.683 | n/a | n/a | 207-230 | 316-331 |
| realerr | cursor | 1 | 2480 | 13500 | 8860 | 8810 | 8900 | 8030 | 9340 | 0.183 | 0.28 | 0.281 | 0.278 | 0.309 | 0.265 | 2490 | 13700 | 9120 | 9030 | 8080 | 9500 | 0.181 | 0.273 | 0.275 | 0.308 | 0.262 | 2480-2600 | 8860-9410 |
| realerr | cursor | all | 3720 | 20400 | 13300 | 13200 | 13500 | 11600 | 14300 | 0.182 | 0.279 | 0.283 | 0.275 | 0.321 | 0.259 | 3790 | 20500 | 13600 | 13500 | 11900 | 14400 | 0.184 | 0.278 | 0.28 | 0.318 | 0.263 | 3720-3840 | 13300-13800 |
| realerr | parse | 1 | 2260 | 11400 | 8620 | 8550 | 8630 | 7490 | 9030 | 0.198 | 0.262 | 0.264 | 0.261 | 0.301 | 0.25 | 2290 | 11600 | 8810 | 8710 | 7580 | 9250 | 0.198 | 0.26 | 0.263 | 0.302 | 0.248 | 2260-2360 | 8620-9470 |
| realerr | parse | all | 3350 | 17000 | 13100 | 13000 | 12900 | 11200 | 13900 | 0.197 | 0.257 | 0.259 | 0.26 | 0.298 | 0.241 | 3420 | 17200 | 13100 | 13300 | 11300 | 14000 | 0.199 | 0.26 | 0.257 | 0.303 | 0.244 | 3350-3480 | 13100-13400 |
| realerr | walk | 1 | 2700 | 14600 | 9060 | 9020 | n/a | n/a | n/a | 0.184 | 0.298 | 0.299 | n/a | n/a | n/a | 2730 | 14700 | 9270 | 9260 | n/a | n/a | 0.185 | 0.294 | 0.295 | n/a | n/a | 2700-3070 | 9060-9720 |
| realerr | walk | all | 3910 | 21200 | 13500 | 13300 | n/a | n/a | n/a | 0.185 | 0.289 | 0.293 | n/a | n/a | n/a | 4020 | 21300 | 13800 | 13700 | n/a | n/a | 0.188 | 0.291 | 0.293 | n/a | n/a | 3910-4060 | 13500-14200 |

### verilog: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-nopragma | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-nopragma | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 916 | 7710 | 1020 | 1020 | 1050 | 1120 | 1080 | 0.119 | 0.895 | 0.895 | 0.87 | 0.817 | 0.848 |
| broken | cursor minus parse | 1 | cycles M | 336 | 3660 | 1480 | 1380 | 1170 | 1600 | 1470 | 0.0916 | 0.227 | 0.243 | 0.287 | 0.21 | 0.229 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 65.1 | 717 | 289 | 269 | 228 | 312 | 285 | 0.0908 | 0.225 | 0.242 | 0.286 | 0.208 | 0.228 |
| broken | cursor minus parse | all | instructions M | 916 | 7800 | 1030 | 1020 | 1050 | 1120 | 1080 | 0.117 | 0.892 | 0.899 | 0.875 | 0.818 | 0.85 |
| broken | cursor minus parse | all | cycles M | 378 | 3980 | 689 | 939 | 956 | 850 | 913 | 0.0949 | 0.548 | 0.402 | 0.395 | 0.444 | 0.414 |
| broken | cursor minus parse | all | CPU user+sys ms | 108 | 1000 | 198 | 242 | 262 | 212 | 257 | 0.108 | 0.547 | 0.448 | 0.414 | 0.512 | 0.422 |
| broken | walk minus parse | 1 | instructions M | 5010 | 15800 | 5840 | 5840 | n/a | n/a | n/a | 0.318 | 0.856 | 0.856 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 1060 | 6070 | 1180 | 1110 | n/a | n/a | n/a | 0.175 | 0.906 | 0.96 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 205 | 1180 | 227 | 212 | n/a | n/a | n/a | 0.174 | 0.902 | 0.967 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 5000 | 15800 | 5850 | 5840 | n/a | n/a | n/a | 0.316 | 0.855 | 0.857 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 1290 | 7930 | 2010 | 2130 | n/a | n/a | n/a | 0.163 | 0.643 | 0.607 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 290 | 1730 | 446 | 475 | n/a | n/a | n/a | 0.168 | 0.65 | 0.611 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 97.1 | 861 | 111 | 111 | 115 | 122 | 118 | 0.113 | 0.872 | 0.872 | 0.848 | 0.799 | 0.825 |
| real | cursor minus parse | 1 | cycles M | 20.4 | 200 | 24.2 | 27 | 31 | 46.3 | 28.4 | 0.102 | 0.842 | 0.754 | 0.657 | 0.44 | 0.718 |
| real | cursor minus parse | 1 | CPU user+sys ms | 3.92 | 39.7 | 4.56 | 5.15 | 5.94 | 8.84 | 5.43 | 0.0986 | 0.86 | 0.761 | 0.66 | 0.444 | 0.721 |
| real | cursor minus parse | all | instructions M | 97.1 | 874 | 109 | 109 | 111 | 120 | 118 | 0.111 | 0.889 | 0.889 | 0.876 | 0.812 | 0.823 |
| real | cursor minus parse | all | cycles M | 35.8 | 355 | 41.1 | 44.4 | 44.9 | 58.5 | 37.3 | 0.101 | 0.871 | 0.806 | 0.797 | 0.612 | 0.959 |
| real | cursor minus parse | all | CPU user+sys ms | 7.86 | 79 | 8.56 | 8.43 | 11.4 | 12.8 | 10.1 | 0.0995 | 0.918 | 0.932 | 0.691 | 0.616 | 0.779 |
| real | walk minus parse | 1 | instructions M | 72.3 | 597 | 57.7 | 57.7 | n/a | n/a | n/a | 0.121 | 1.25 | 1.25 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 16.8 | 162 | 15.5 | 15.4 | n/a | n/a | n/a | 0.103 | 1.08 | 1.09 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 3.22 | 31.9 | 2.89 | 2.95 | n/a | n/a | n/a | 0.101 | 1.11 | 1.09 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 72.6 | 625 | 57.3 | 56.5 | n/a | n/a | n/a | 0.116 | 1.27 | 1.29 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 34.5 | 282 | 30.5 | 25.2 | n/a | n/a | n/a | 0.122 | 1.13 | 1.37 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 7.16 | 65.2 | 4.46 | 5.48 | n/a | n/a | n/a | 0.11 | 1.6 | 1.31 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 998 | 8590 | 1120 | 1120 | 1150 | 1220 | 1180 | 0.116 | 0.894 | 0.894 | 0.868 | 0.816 | 0.846 |
| realerr | cursor minus parse | 1 | cycles M | 221 | 2130 | 240 | 265 | 264 | 535 | 308 | 0.103 | 0.92 | 0.833 | 0.835 | 0.412 | 0.716 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 42.4 | 415 | 45.6 | 50.2 | 50.5 | 102 | 59.9 | 0.102 | 0.929 | 0.844 | 0.838 | 0.414 | 0.708 |
| realerr | cursor minus parse | all | instructions M | 996 | 8650 | 1110 | 1110 | 1140 | 1220 | 1180 | 0.115 | 0.897 | 0.895 | 0.872 | 0.819 | 0.847 |
| realerr | cursor minus parse | all | cycles M | 366 | 3440 | 268 | 199 | 614 | 323 | 437 | 0.106 | 1.37 | 1.84 | 0.596 | 1.13 | 0.838 |
| realerr | cursor minus parse | all | CPU user+sys ms | 83.3 | 794 | 66.2 | 35.4 | 161 | 44.8 | 87.3 | 0.105 | 1.26 | 2.36 | 0.517 | 1.86 | 0.954 |
| realerr | walk minus parse | 1 | instructions M | 2230 | 9590 | 2410 | 2410 | n/a | n/a | n/a | 0.232 | 0.926 | 0.926 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 439 | 3260 | 437 | 474 | n/a | n/a | n/a | 0.135 | 1.01 | 0.927 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 84.1 | 631 | 83.3 | 91.5 | n/a | n/a | n/a | 0.133 | 1.01 | 0.919 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 2220 | 9650 | 2410 | 2410 | n/a | n/a | n/a | 0.23 | 0.923 | 0.924 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 556 | 4160 | 480 | 378 | n/a | n/a | n/a | 0.134 | 1.16 | 1.47 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 118 | 920 | 113 | 62.3 | n/a | n/a | n/a | 0.128 | 1.05 | 1.89 | n/a | n/a | n/a |

### verilog: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 50.8 | 91.6 | 32.2 | 0.554 | 1.57 | 50.8 | 91.6 | 32.2 | 0.554 | 1.58 | 41.7 | 81.3 | 25.7 | 0.513 | 1.62 |
| broken | cursor | all | 113 | 162 | 147 | 0.7 | 0.774 | 113 | 162 | 147 | 0.7 | 0.774 | 104 | 152 | 140 | 0.684 | 0.742 |
| broken | parse | 1 | 44.9 | 42.1 | 32.3 | 1.07 | 1.39 | 44.9 | 42.1 | 32.3 | 1.07 | 1.39 | 35.9 | 31.9 | 25.9 | 1.13 | 1.39 |
| broken | parse | all | 107 | 142 | 148 | 0.756 | 0.723 | 107 | 142 | 148 | 0.756 | 0.723 | 97.5 | 131 | 141 | 0.743 | 0.689 |
| broken | walk | 1 | 45 | 91.3 | 32.7 | 0.493 | 1.38 | 45 | 91.3 | 32.7 | 0.493 | 1.38 | 36 | 81.1 | 26.1 | 0.444 | 1.38 |
| broken | walk | all | 113 | 168 | 151 | 0.674 | 0.746 | 113 | 168 | 151 | 0.674 | 0.747 | 103 | 157 | 144 | 0.657 | 0.715 |
| real | cursor | 1 | 25.7 | 29.9 | 18.3 | 0.858 | 1.41 | 25.7 | 29.9 | 18.3 | 0.858 | 1.41 | 21.7 | 24.4 | 15.4 | 0.892 | 1.41 |
| real | cursor | all | 37.9 | 40.4 | 27.9 | 0.939 | 1.36 | 37.9 | 40.4 | 27.9 | 0.939 | 1.36 | 32.8 | 34.8 | 24.9 | 0.942 | 1.32 |
| real | parse | 1 | 24.6 | 24.4 | 18.2 | 1.01 | 1.35 | 24.6 | 24.4 | 18.2 | 1.01 | 1.35 | 20.7 | 19.1 | 15.4 | 1.09 | 1.35 |
| real | parse | all | 36.7 | 37.5 | 27.8 | 0.979 | 1.32 | 36.7 | 37.5 | 27.8 | 0.979 | 1.32 | 31.4 | 31.9 | 24.7 | 0.984 | 1.27 |
| real | walk | 1 | 24.7 | 29.7 | 18.3 | 0.83 | 1.35 | 24.7 | 29.7 | 18.3 | 0.83 | 1.35 | 20.6 | 24.2 | 15.5 | 0.852 | 1.33 |
| real | walk | all | 36.4 | 41.1 | 27.8 | 0.885 | 1.31 | 36.4 | 41.1 | 27.8 | 0.885 | 1.31 | 31.3 | 35.7 | 24.8 | 0.878 | 1.26 |
| realerr | cursor | 1 | 51.4 | 75.2 | 33.9 | 0.684 | 1.52 | 51.5 | 75.2 | 33.9 | 0.684 | 1.52 | 41.4 | 63.6 | 26.6 | 0.651 | 1.56 |
| realerr | cursor | all | 95.9 | 142 | 116 | 0.678 | 0.829 | 96 | 142 | 116 | 0.678 | 0.829 | 85.4 | 130 | 108 | 0.658 | 0.789 |
| realerr | parse | 1 | 45.3 | 45 | 34 | 1.01 | 1.33 | 45.3 | 45 | 34 | 1.01 | 1.33 | 35.1 | 33.3 | 26.5 | 1.06 | 1.33 |
| realerr | parse | all | 87.9 | 124 | 118 | 0.709 | 0.746 | 87.9 | 124 | 118 | 0.709 | 0.747 | 77.4 | 112 | 110 | 0.688 | 0.702 |
| realerr | walk | 1 | 45.6 | 80.8 | 33.9 | 0.565 | 1.35 | 45.6 | 80.8 | 33.9 | 0.565 | 1.35 | 35.4 | 69.1 | 26.6 | 0.513 | 1.33 |
| realerr | walk | all | 87.7 | 142 | 117 | 0.618 | 0.751 | 87.7 | 142 | 117 | 0.618 | 0.751 | 77.1 | 130 | 109 | 0.592 | 0.705 |

### verilog: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 44.5 | 53 | 32.7 | 0.839 | 1.36 | 44.5 | 53 | 32.7 | 0.839 | 1.36 | 35.5 | 42.7 | 26 | 0.831 | 1.36 |
| broken | all | 120 | 184 | 170 | 0.653 | 0.706 | 120 | 184 | 170 | 0.653 | 0.707 | 110 | 173 | 163 | 0.636 | 0.677 |
| real | 1 | 24.5 | 24.9 | 17.7 | 0.985 | 1.39 | 24.5 | 24.9 | 17.7 | 0.985 | 1.39 | 20.5 | 19.4 | 14.7 | 1.05 | 1.39 |
| real | all | 40.7 | 50.2 | 33 | 0.812 | 1.24 | 40.7 | 50.2 | 33 | 0.812 | 1.24 | 35.7 | 44.8 | 29.8 | 0.797 | 1.2 |
| realerr | 1 | 45.5 | 45.1 | 34 | 1.01 | 1.34 | 45.5 | 45.1 | 34 | 1.01 | 1.34 | 35 | 33.4 | 26.4 | 1.05 | 1.32 |
| realerr | all | 95.4 | 155 | 178 | 0.615 | 0.535 | 95.4 | 155 | 178 | 0.615 | 0.535 | 84.9 | 144 | 171 | 0.591 | 0.497 |

### verilog: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0821 (real), 0.0635 (realerr), 0.0483 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 1.07 | 31700 | 7.08 | 1210 | 110000 | 1210 | 110000 | 0.000884 |
| real | steady | 0.0821 | 15.7 | 7.07 | 1210 | 110000 | 1210 | 110000 | 0 |
| realerr | cold | 6.62 | 52800 | 7.58 | 8940 | 1210000 | 8940 | 1210000 | 0.000741 |
| realerr | steady | 0.0635 | 112 | 7.57 | 8940 | 1210000 | 8940 | 1210000 | 0 |
| broken | cold | 6.01 | 49700 | 7.69 | 8350 | 1510000 | 8350 | 1510000 | 0.00072 |
| broken | steady | 0.0483 | 202 | 7.67 | 8350 | 1510000 | 8350 | 1510000 | 0 |

### verilog: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2048 B | init ms | 0.009 | 0.0116 | 0.00555 | 0.775 | 1.62 | 15 |
| #0 2048 B | parse1 ms | 0.428 | 0.597 | 0.428 | 0.716 | 0.999 | 15 |
| #0 2048 B | init+parse1 ms | 0.437 | 0.61 | 0.433 | 0.717 | 1.01 | 15 |
| #0 2048 B | parse2 ms | 0.184 | 0.495 | 0.264 | 0.372 | 0.698 | 15 |
| #0 2048 B | process user+sys ms | 1.33 | 2.11 | 0.982 | 0.631 | 1.36 | 15 |
| #0 2048 B | exec to exit instr user+kernel M | 11.6 | 19.9 | 10 | 0.586 | 1.16 | 15 |
| #0 2048 B | exec to exit instr user M | 8.33 | 15.7 | 8.7 | 0.532 | 0.957 | 15 |
| #0 2048 B | exec to exit instr kernel M | 3.31 | 4.18 | 1.35 | 0.792 | 2.46 | 15 |
| #0 2048 B | exec to exit cycles user+kernel M | 6.34 | 10.1 | 4.64 | 0.63 | 1.37 | 15 |
| #0 2048 B | exec to exit task-clock ms | 1.27 | 2.04 | 0.89 | 0.623 | 1.43 | 15 |
| #0 2048 B | peak RSS MB | 8.69 | 9.73 | 7.58 | 0.893 | 1.15 | 15 |
| #0 2048 B | peak footprint MB | 8.72 | 9.76 | 7.52 | 0.893 | 1.16 | 15 |
| #1 1245 B | init ms | 0.00888 | 0.0119 | 0.00519 | 0.744 | 1.71 | 15 |
| #1 1245 B | parse1 ms | 0.103 | 0.121 | 0.0951 | 0.852 | 1.08 | 15 |
| #1 1245 B | init+parse1 ms | 0.115 | 0.133 | 0.1 | 0.867 | 1.15 | 15 |
| #1 1245 B | parse2 ms | 0.0216 | 0.0636 | 0.0376 | 0.34 | 0.574 | 15 |
| #1 1245 B | process user+sys ms | 0.819 | 1.18 | 0.416 | 0.693 | 1.97 | 15 |
| #1 1245 B | exec to exit instr user+kernel M | 4.35 | 6.63 | 2.57 | 0.657 | 1.69 | 15 |
| #1 1245 B | exec to exit instr user M | 1.53 | 2.89 | 1.49 | 0.53 | 1.03 | 15 |
| #1 1245 B | exec to exit instr kernel M | 2.82 | 3.72 | 1.08 | 0.756 | 2.62 | 15 |
| #1 1245 B | exec to exit cycles user+kernel M | 3.64 | 5.42 | 1.68 | 0.671 | 2.17 | 15 |
| #1 1245 B | exec to exit task-clock ms | 0.75 | 1.11 | 0.32 | 0.676 | 2.34 | 15 |
| #1 1245 B | peak RSS MB | 5.61 | 6.62 | 4.92 | 0.846 | 1.14 | 15 |
| #1 1245 B | peak footprint MB | 5.63 | 6.66 | 4.79 | 0.846 | 1.18 | 15 |

### verilog: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 195 | 166214 | 0 | 0 | ok |
| realerr | 441 | 1659622 | 441 | 0 | ok |
| broken | 497 | 1488751 | 458 | 0 | ok |

## zig

### zig: wall per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max | CPU/wall gs | CPU/wall cgo | CPU/wall C | gs MB/s (min pass) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 363 | 1820 | 960 | 980 | 901 | 1050 | 0.199 | 0.379 | 0.371 | 0.403 | 0.345 | 369 | 1840 | 966 | 906 | 1060 | 0.2 | 0.381 | 0.407 | 0.349 | 363-381 | 960-974 | 1 | 1.01 | 1 | 22.9 |
| broken | cursor | all | 49.1 | 275 | 182 | 178 | 141 | 191 | 0.179 | 0.269 | 0.276 | 0.349 | 0.257 | 50.2 | 280 | 186 | 142 | 192 | 0.18 | 0.27 | 0.354 | 0.262 | 49.1-52.3 | 182-194 | 13.9 | 11.6 | 8.44 * | 169 |
| broken | parse | 1 | 311 | 1490 | 948 | 954 | 797 | 1030 | 0.209 | 0.328 | 0.326 | 0.39 | 0.303 | 346 | 1510 | 975 | 810 | 1030 | 0.229 | 0.355 | 0.427 | 0.337 | 311-388 | 948-1050 | 1 | 1.01 | 1 | 26.7 |
| broken | parse | all | 45 | 252 | 185 | 182 | 143 | 190 | 0.178 | 0.243 | 0.248 | 0.316 | 0.236 | 47.8 | 259 | 194 | 149 | 201 | 0.184 | 0.247 | 0.32 | 0.237 | 45-58 | 185-211 | 11.9 | 8.91 * | 7.43 * | 185 |
| broken | walk | 1 | 428 | 2160 | 1010 | n/a | n/a | n/a | 0.198 | 0.423 | n/a | n/a | n/a | 462 | 2330 | 1030 | n/a | n/a | 0.198 | 0.45 | n/a | n/a | 428-496 | 1010-1180 | 1 | 1.01 | 1 | 19.4 |
| broken | walk | all | 79.9 | 396 | 208 | n/a | n/a | n/a | 0.202 | 0.384 | n/a | n/a | n/a | 81.9 | 406 | 213 | n/a | n/a | 0.202 | 0.386 | n/a | n/a | 79.9-91.9 | 208-220 | 9.13 * | 8.42 * | 7.54 * | 104 |
| real | cursor | 1 | 337 | 1650 | 677 | 704 | 758 | 753 | 0.204 | 0.498 | 0.478 | 0.444 | 0.447 | 352 | 1710 | 700 | 804 | 783 | 0.205 | 0.504 | 0.438 | 0.45 | 337-399 | 677-741 | 1 | 1.01 | 1 | 30.3 |
| real | cursor | all | 47.5 | 221 | 85.6 | 85.7 | 91.6 | 90.5 | 0.215 | 0.555 | 0.554 | 0.518 | 0.524 | 49 | 228 | 89.8 | 92.8 | 95.8 | 0.215 | 0.546 | 0.528 | 0.511 | 47.5-52.2 | 85.6-95.1 | 12.8 | 12.7 | 12.9 | 215 |
| real | parse | 1 | 258 | 1060 | 566 | 587 | 580 | 645 | 0.244 | 0.456 | 0.439 | 0.445 | 0.4 | 287 | 1100 | 600 | 596 | 664 | 0.26 | 0.478 | 0.481 | 0.431 | 258-316 | 566-683 | 1 | 1 | 1 | 39.6 |
| real | parse | all | 35.2 | 130 | 72.8 | 72.1 | 71 | 77.6 | 0.27 | 0.483 | 0.488 | 0.496 | 0.453 | 36.8 | 136 | 76.2 | 72.6 | 80.3 | 0.271 | 0.482 | 0.506 | 0.458 | 35.2-41.7 | 72.8-81.1 | 12.8 | 12.9 | 13 | 290 |
| real | walk | 1 | 329 | 1560 | 623 | n/a | n/a | n/a | 0.211 | 0.528 | n/a | n/a | n/a | 348 | 1630 | 665 | n/a | n/a | 0.214 | 0.523 | n/a | n/a | 329-383 | 623-731 | 1 | 1.01 | 1 | 31 |
| real | walk | all | 44.4 | 205 | 80.4 | n/a | n/a | n/a | 0.217 | 0.553 | n/a | n/a | n/a | 46.2 | 212 | 83.1 | n/a | n/a | 0.218 | 0.556 | n/a | n/a | 44.4-49.2 | 80.4-86.3 | 12.9 | 12.6 | 13.1 | 230 |
| realerr | cursor | 1 | 22.9 | 113 | 45.6 | 48.2 | 52.8 | 56.1 | 0.203 | 0.502 | 0.474 | 0.433 | 0.408 | 25.4 | 123 | 49.2 | 53.4 | 57.2 | 0.206 | 0.517 | 0.476 | 0.444 | 22.9-28.1 | 45.6-56.6 | 1 | 1.01 | 1 | 31.8 |
| realerr | cursor | all | 8.41 | 38.4 | 16.3 | 17.2 | 19.1 | 18.7 | 0.219 | 0.515 | 0.49 | 0.441 | 0.449 | 10.4 | 41.6 | 18.5 | 20.2 | 19.8 | 0.249 | 0.561 | 0.514 | 0.525 | 8.41-12.2 | 16.3-23.8 | 3.13 * | 3.71 * | 3.29 * | 86.6 |
| realerr | parse | 1 | 17.4 | 71.9 | 37.6 | 39.1 | 38.5 | 43.5 | 0.241 | 0.462 | 0.443 | 0.451 | 0.399 | 18.3 | 75.7 | 44.5 | 40.2 | 46.9 | 0.241 | 0.41 | 0.455 | 0.39 | 17.4-22.3 | 37.6-47.5 | 1 | 1.01 | 1 | 42 |
| realerr | parse | all | 6.89 | 24.3 | 15.3 | 15.5 | 13.1 | 16.2 | 0.283 | 0.45 | 0.445 | 0.526 | 0.424 | 7.71 | 25.3 | 16.3 | 14 | 18.1 | 0.305 | 0.474 | 0.551 | 0.426 | 6.89-9.35 | 15.3-19.9 | 3.17 * | 3.9 * | 3.39 * | 106 |
| realerr | walk | 1 | 22.2 | 110 | 42.5 | n/a | n/a | n/a | 0.203 | 0.523 | n/a | n/a | n/a | 25.2 | 117 | 44.5 | n/a | n/a | 0.214 | 0.565 | n/a | n/a | 22.2-30.8 | 42.5-55.5 | 1 | 1.01 | 1 | 32.8 |
| realerr | walk | all | 8.27 | 36 | 16.3 | n/a | n/a | n/a | 0.23 | 0.506 | n/a | n/a | n/a | 9.76 | 39.4 | 17.3 | n/a | n/a | 0.248 | 0.564 | n/a | n/a | 8.27-12.3 | 16.3-21.2 | 3.14 * | 3.73 * | 3.39 * | 88.1 |

### zig: CPU user+sys per pass (ms)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 364 | 1830 | 960 | 980 | 901 | 1050 | 0.198 | 0.379 | 0.371 | 0.403 | 0.346 | 369 | 1850 | 966 | 905 | 1060 | 0.199 | 0.382 | 0.407 | 0.35 | 364-382 | 960-974 |
| broken | cursor | all | 685 | 3210 | 1510 | 1530 | 1460 | 1670 | 0.213 | 0.453 | 0.448 | 0.471 | 0.411 | 703 | 3240 | 1570 | 1460 | 1680 | 0.217 | 0.447 | 0.483 | 0.418 | 685-713 | 1510-1590 |
| broken | parse | 1 | 311 | 1500 | 948 | 954 | 797 | 1030 | 0.208 | 0.328 | 0.326 | 0.39 | 0.303 | 346 | 1520 | 975 | 810 | 1030 | 0.227 | 0.355 | 0.427 | 0.337 | 311-388 | 948-1050 |
| broken | parse | all | 565 | 2270 | 1400 | 1390 | 1210 | 1510 | 0.248 | 0.404 | 0.406 | 0.466 | 0.374 | 575 | 2310 | 1460 | 1260 | 1540 | 0.249 | 0.395 | 0.455 | 0.372 | 565-609 | 1400-1480 |
| broken | walk | 1 | 428 | 2180 | 1010 | n/a | n/a | n/a | 0.196 | 0.423 | n/a | n/a | n/a | 462 | 2350 | 1030 | n/a | n/a | 0.197 | 0.451 | n/a | n/a | 428-496 | 1010-1180 |
| broken | walk | all | 730 | 3430 | 1560 | n/a | n/a | n/a | 0.213 | 0.466 | n/a | n/a | n/a | 768 | 3460 | 1610 | n/a | n/a | 0.222 | 0.478 | n/a | n/a | 730-774 | 1560-1630 |
| real | cursor | 1 | 337 | 1660 | 677 | 704 | 758 | 753 | 0.203 | 0.498 | 0.479 | 0.445 | 0.448 | 353 | 1730 | 699 | 804 | 783 | 0.204 | 0.504 | 0.439 | 0.451 | 337-399 | 677-741 |
| real | cursor | all | 617 | 2860 | 1130 | 1130 | 1160 | 1210 | 0.216 | 0.544 | 0.544 | 0.531 | 0.509 | 631 | 2910 | 1160 | 1180 | 1250 | 0.217 | 0.543 | 0.534 | 0.506 | 617-639 | 1130-1190 |
| real | parse | 1 | 258 | 1060 | 566 | 587 | 580 | 645 | 0.244 | 0.456 | 0.439 | 0.445 | 0.4 | 287 | 1110 | 600 | 596 | 664 | 0.259 | 0.478 | 0.482 | 0.432 | 258-317 | 566-683 |
| real | parse | all | 461 | 1730 | 980 | 947 | 923 | 1010 | 0.266 | 0.47 | 0.487 | 0.499 | 0.456 | 479 | 1760 | 996 | 929 | 1050 | 0.272 | 0.481 | 0.516 | 0.457 | 461-488 | 980-1010 |
| real | walk | 1 | 330 | 1570 | 623 | n/a | n/a | n/a | 0.21 | 0.529 | n/a | n/a | n/a | 348 | 1640 | 665 | n/a | n/a | 0.213 | 0.524 | n/a | n/a | 330-383 | 623-731 |
| real | walk | all | 595 | 2630 | 1070 | n/a | n/a | n/a | 0.226 | 0.556 | n/a | n/a | n/a | 599 | 2670 | 1090 | n/a | n/a | 0.225 | 0.547 | n/a | n/a | 595-604 | 1070-1120 |
| realerr | cursor | 1 | 22.9 | 114 | 45.6 | 48.2 | 52.8 | 56.1 | 0.202 | 0.503 | 0.475 | 0.434 | 0.409 | 25.5 | 125 | 49.2 | 53.4 | 57.2 | 0.204 | 0.518 | 0.476 | 0.445 | 22.9-28.2 | 45.6-56.6 |
| realerr | cursor | all | 28.4 | 141 | 57.8 | 61.7 | 66 | 62.9 | 0.202 | 0.491 | 0.46 | 0.43 | 0.451 | 32.2 | 154 | 62.5 | 67.3 | 66.6 | 0.209 | 0.516 | 0.478 | 0.484 | 28.4-37.1 | 57.8-67.3 |
| realerr | parse | 1 | 17.4 | 72.2 | 37.5 | 39.1 | 38.5 | 43.5 | 0.241 | 0.463 | 0.444 | 0.451 | 0.399 | 18.3 | 76.2 | 44.5 | 40.2 | 46.9 | 0.24 | 0.411 | 0.455 | 0.39 | 17.4-22.3 | 37.5-47.5 |
| realerr | parse | all | 22.6 | 87.4 | 53.7 | 50.1 | 47.6 | 55.2 | 0.259 | 0.421 | 0.451 | 0.475 | 0.41 | 24.7 | 99.7 | 55.9 | 50.8 | 61.4 | 0.248 | 0.441 | 0.486 | 0.402 | 22.6-28.5 | 53.7-60.4 |
| realerr | walk | 1 | 22.2 | 111 | 42.5 | n/a | n/a | n/a | 0.201 | 0.524 | n/a | n/a | n/a | 25.2 | 119 | 44.5 | n/a | n/a | 0.213 | 0.566 | n/a | n/a | 22.2-30.8 | 42.5-55.5 |
| realerr | walk | all | 28.6 | 135 | 56.5 | n/a | n/a | n/a | 0.212 | 0.506 | n/a | n/a | n/a | 30.7 | 148 | 59.8 | n/a | n/a | 0.207 | 0.514 | n/a | n/a | 28.6-36 | 56.5-66.1 |

### zig: instructions per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 8450 | 33600 | 17800 | 17500 | 16800 | 18300 | 0.252 | 0.475 | 0.483 | 0.503 | 0.462 | 8450 | 33600 | 17800 | 16800 | 18300 | 0.251 | 0.474 | 0.502 | 0.461 | 8450-8450 | 17800-17800 |
| broken | cursor | all | 8450 | 33600 | 17800 | 17500 | 16800 | 18300 | 0.252 | 0.474 | 0.483 | 0.502 | 0.461 | 8460 | 33700 | 17800 | 16800 | 18300 | 0.251 | 0.474 | 0.502 | 0.461 | 8450-8520 | 17800-17900 |
| broken | parse | 1 | 6850 | 23500 | 16000 | 15700 | 14800 | 16400 | 0.292 | 0.428 | 0.438 | 0.462 | 0.417 | 6850 | 23500 | 16000 | 14900 | 16500 | 0.292 | 0.427 | 0.461 | 0.416 | 6850-6850 | 16000-16100 |
| broken | parse | all | 6860 | 23400 | 16000 | 15700 | 14900 | 16500 | 0.293 | 0.427 | 0.437 | 0.461 | 0.416 | 6870 | 23500 | 16100 | 14900 | 16500 | 0.293 | 0.427 | 0.461 | 0.416 | 6860-7040 | 16000-16100 |
| broken | walk | 1 | 10000 | 35500 | 19400 | n/a | n/a | n/a | 0.282 | 0.517 | n/a | n/a | n/a | 10000 | 35600 | 19400 | n/a | n/a | 0.281 | 0.516 | n/a | n/a | 10000-10000 | 19400-19400 |
| broken | walk | all | 10000 | 35600 | 19400 | n/a | n/a | n/a | 0.281 | 0.516 | n/a | n/a | n/a | 10000 | 35600 | 19400 | n/a | n/a | 0.281 | 0.516 | n/a | n/a | 10000-10000 | 19400-19400 |
| real | cursor | 1 | 7900 | 28800 | 11900 | 11700 | 11600 | 12400 | 0.274 | 0.665 | 0.674 | 0.679 | 0.637 | 7900 | 28900 | 11900 | 11600 | 12400 | 0.274 | 0.665 | 0.679 | 0.637 | 7900-7900 | 11900-11900 |
| real | cursor | all | 7900 | 28800 | 11900 | 11700 | 11700 | 12400 | 0.274 | 0.664 | 0.675 | 0.677 | 0.637 | 7900 | 28900 | 11900 | 11700 | 12400 | 0.273 | 0.663 | 0.677 | 0.635 | 7900-7900 | 11900-11900 |
| real | parse | 1 | 5990 | 16800 | 9620 | 9420 | 9170 | 10000 | 0.356 | 0.623 | 0.636 | 0.653 | 0.596 | 5990 | 16800 | 9630 | 9180 | 10100 | 0.356 | 0.622 | 0.652 | 0.595 | 5990-5990 | 9620-9630 |
| real | parse | all | 5990 | 16700 | 9700 | 9470 | 9270 | 10100 | 0.359 | 0.617 | 0.632 | 0.646 | 0.593 | 5990 | 16800 | 9730 | 9280 | 10100 | 0.356 | 0.615 | 0.646 | 0.591 | 5990-5990 | 9700-9790 |
| real | walk | 1 | 7420 | 25600 | 10700 | n/a | n/a | n/a | 0.29 | 0.696 | n/a | n/a | n/a | 7420 | 25600 | 10700 | n/a | n/a | 0.29 | 0.695 | n/a | n/a | 7420-7420 | 10700-10700 |
| real | walk | all | 7420 | 25500 | 10700 | n/a | n/a | n/a | 0.291 | 0.693 | n/a | n/a | n/a | 7420 | 25700 | 10800 | n/a | n/a | 0.289 | 0.688 | n/a | n/a | 7420-7420 | 10700-10800 |
| realerr | cursor | 1 | 555 | 2010 | 831 | 821 | 815 | 868 | 0.275 | 0.667 | 0.676 | 0.681 | 0.639 | 555 | 2020 | 835 | 818 | 871 | 0.274 | 0.664 | 0.678 | 0.637 | 555-555 | 831-840 |
| realerr | cursor | all | 555 | 1930 | 816 | 811 | 815 | 855 | 0.288 | 0.681 | 0.685 | 0.682 | 0.65 | 556 | 2000 | 834 | 817 | 871 | 0.278 | 0.667 | 0.68 | 0.638 | 555-560 | 816-858 |
| realerr | parse | 1 | 421 | 1190 | 668 | 654 | 636 | 698 | 0.355 | 0.63 | 0.644 | 0.662 | 0.603 | 421 | 1190 | 668 | 636 | 698 | 0.355 | 0.63 | 0.662 | 0.603 | 421-421 | 668-700 |
| realerr | parse | all | 422 | 1040 | 677 | 665 | 654 | 719 | 0.406 | 0.622 | 0.634 | 0.645 | 0.587 | 422 | 1170 | 690 | 663 | 721 | 0.362 | 0.612 | 0.637 | 0.586 | 422-426 | 677-715 |
| realerr | walk | 1 | 524 | 1790 | 744 | n/a | n/a | n/a | 0.292 | 0.705 | n/a | n/a | n/a | 524 | 1800 | 744 | n/a | n/a | 0.291 | 0.705 | n/a | n/a | 524-524 | 744-776 |
| realerr | walk | all | 525 | 1630 | 751 | n/a | n/a | n/a | 0.322 | 0.699 | n/a | n/a | n/a | 526 | 1790 | 770 | n/a | n/a | 0.293 | 0.682 | n/a | n/a | 525-530 | 751-786 |

### zig: cycles per pass (M)

| set | what | j | gs min | cgo min | C min | C-O3 min | C-clang min | C-pic min | min gs/cgo | min gs/C | min gs/C-O3 | min gs/C-clang | min gs/C-pic | gs med | cgo med | C med | C-clang med | C-pic med | med gs/cgo | med gs/C | med gs/C-clang | med gs/C-pic | gs min-max | C min-max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 1900 | 9510 | 5010 | 5120 | 4700 | 5490 | 0.199 | 0.378 | 0.371 | 0.403 | 0.345 | 1920 | 9610 | 5050 | 4730 | 5510 | 0.2 | 0.381 | 0.407 | 0.349 | 1900-1980 | 5010-5090 |
| broken | cursor | all | 3050 | 14300 | 6990 | 7070 | 6630 | 7630 | 0.213 | 0.436 | 0.431 | 0.46 | 0.399 | 3110 | 14400 | 7240 | 6640 | 7690 | 0.216 | 0.429 | 0.468 | 0.404 | 3050-3170 | 6990-7300 |
| broken | parse | 1 | 1620 | 7770 | 4940 | 4970 | 4150 | 5350 | 0.208 | 0.327 | 0.325 | 0.389 | 0.302 | 1800 | 7900 | 5080 | 4230 | 5360 | 0.228 | 0.355 | 0.427 | 0.337 | 1620-2020 | 4940-5500 |
| broken | parse | all | 2500 | 10100 | 6430 | 6390 | 5530 | 6910 | 0.247 | 0.389 | 0.391 | 0.452 | 0.362 | 2540 | 10300 | 6660 | 5720 | 7020 | 0.245 | 0.381 | 0.444 | 0.362 | 2500-2650 | 6430-6800 |
| broken | walk | 1 | 2230 | 11300 | 5290 | n/a | n/a | n/a | 0.198 | 0.422 | n/a | n/a | n/a | 2410 | 12200 | 5360 | n/a | n/a | 0.198 | 0.449 | n/a | n/a | 2230-2560 | 5290-6140 |
| broken | walk | all | 3320 | 15500 | 7270 | n/a | n/a | n/a | 0.214 | 0.457 | n/a | n/a | n/a | 3460 | 15600 | 7420 | n/a | n/a | 0.222 | 0.466 | n/a | n/a | 3320-3500 | 7270-7510 |
| real | cursor | 1 | 1740 | 8560 | 3530 | 3670 | 3950 | 3920 | 0.204 | 0.495 | 0.475 | 0.441 | 0.444 | 1830 | 8910 | 3650 | 4190 | 4050 | 0.206 | 0.503 | 0.437 | 0.452 | 1740-2070 | 3530-3860 |
| real | cursor | all | 2720 | 12400 | 4980 | 4990 | 5180 | 5350 | 0.22 | 0.546 | 0.545 | 0.524 | 0.508 | 2770 | 12600 | 5100 | 5220 | 5450 | 0.221 | 0.544 | 0.532 | 0.509 | 2720-2790 | 4980-5180 |
| real | parse | 1 | 1340 | 5500 | 2950 | 3060 | 3020 | 3360 | 0.243 | 0.453 | 0.437 | 0.443 | 0.398 | 1490 | 5750 | 3130 | 3100 | 3460 | 0.259 | 0.476 | 0.48 | 0.43 | 1340-1650 | 2950-3560 |
| real | parse | all | 2040 | 7570 | 4340 | 4220 | 4080 | 4470 | 0.27 | 0.47 | 0.484 | 0.501 | 0.457 | 2110 | 7680 | 4390 | 4090 | 4640 | 0.274 | 0.479 | 0.514 | 0.454 | 2040-2140 | 4340-4440 |
| real | walk | 1 | 1710 | 8130 | 3250 | n/a | n/a | n/a | 0.21 | 0.525 | n/a | n/a | n/a | 1810 | 8470 | 3470 | n/a | n/a | 0.213 | 0.521 | n/a | n/a | 1710-1990 | 3250-3810 |
| real | walk | all | 2620 | 11400 | 4740 | n/a | n/a | n/a | 0.229 | 0.552 | n/a | n/a | n/a | 2630 | 11600 | 4830 | n/a | n/a | 0.227 | 0.544 | n/a | n/a | 2620-2660 | 4740-4880 |
| realerr | cursor | 1 | 119 | 582 | 238 | 252 | 275 | 292 | 0.205 | 0.502 | 0.474 | 0.433 | 0.408 | 132 | 640 | 255 | 278 | 298 | 0.206 | 0.517 | 0.473 | 0.442 | 119-146 | 238-294 |
| realerr | cursor | all | 140 | 676 | 282 | 297 | 318 | 304 | 0.208 | 0.498 | 0.473 | 0.442 | 0.462 | 159 | 738 | 299 | 324 | 315 | 0.216 | 0.533 | 0.492 | 0.506 | 140-172 | 282-321 |
| realerr | parse | 1 | 89.6 | 374 | 196 | 204 | 201 | 227 | 0.239 | 0.458 | 0.439 | 0.446 | 0.395 | 94.8 | 394 | 232 | 210 | 245 | 0.24 | 0.408 | 0.452 | 0.388 | 89.6-116 | 196-248 |
| realerr | parse | all | 111 | 419 | 247 | 257 | 237 | 258 | 0.266 | 0.45 | 0.434 | 0.471 | 0.431 | 124 | 473 | 269 | 250 | 290 | 0.262 | 0.46 | 0.494 | 0.427 | 111-138 | 247-283 |
| realerr | walk | 1 | 116 | 571 | 221 | n/a | n/a | n/a | 0.203 | 0.522 | n/a | n/a | n/a | 131 | 603 | 232 | n/a | n/a | 0.218 | 0.566 | n/a | n/a | 116-159 | 221-289 |
| realerr | walk | all | 143 | 647 | 271 | n/a | n/a | n/a | 0.221 | 0.526 | n/a | n/a | n/a | 151 | 706 | 289 | n/a | n/a | 0.214 | 0.522 | n/a | n/a | 143-170 | 271-313 |

### zig: traversal alone (min pass minus min parse pass)

| set | what | j | metric | gs | cgo | C | C-O3 | C-clang | C-pic | gs/cgo | gs/C | gs/C-O3 | gs/C-clang | gs/C-pic |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor minus parse | 1 | instructions M | 1600 | 10100 | 1780 | 1830 | 1960 | 1860 | 0.158 | 0.899 | 0.875 | 0.815 | 0.861 |
| broken | cursor minus parse | 1 | cycles M | 280 | 1750 | 69 | 144 | 550 | 142 | 0.16 | 4.06 | 1.94 | 0.509 | 1.97 |
| broken | cursor minus parse | 1 | CPU user+sys ms | 52.5 | 337 | 11.7 | 26.5 | 104 | 25.1 | 0.156 | 4.5 | 1.99 | 0.503 | 2.09 |
| broken | cursor minus parse | all | instructions M | 1590 | 10200 | 1780 | 1810 | 1950 | 1860 | 0.157 | 0.894 | 0.88 | 0.816 | 0.856 |
| broken | cursor minus parse | all | cycles M | 548 | 4140 | 556 | 678 | 1100 | 729 | 0.132 | 0.985 | 0.808 | 0.5 | 0.751 |
| broken | cursor minus parse | all | CPU user+sys ms | 120 | 937 | 112 | 139 | 242 | 159 | 0.128 | 1.07 | 0.864 | 0.496 | 0.754 |
| broken | walk minus parse | 1 | instructions M | 3150 | 12100 | 3330 | n/a | n/a | n/a | 0.261 | 0.946 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | cycles M | 615 | 3520 | 343 | n/a | n/a | n/a | 0.175 | 1.79 | n/a | n/a | n/a |
| broken | walk minus parse | 1 | CPU user+sys ms | 117 | 684 | 63.8 | n/a | n/a | n/a | 0.171 | 1.83 | n/a | n/a | n/a |
| broken | walk minus parse | all | instructions M | 3150 | 12200 | 3330 | n/a | n/a | n/a | 0.259 | 0.947 | n/a | n/a | n/a |
| broken | walk minus parse | all | cycles M | 817 | 5380 | 833 | n/a | n/a | n/a | 0.152 | 0.981 | n/a | n/a | n/a |
| broken | walk minus parse | all | CPU user+sys ms | 165 | 1150 | 167 | n/a | n/a | n/a | 0.143 | 0.986 | n/a | n/a | n/a |
| real | cursor minus parse | 1 | instructions M | 1910 | 12000 | 2250 | 2310 | 2460 | 2340 | 0.159 | 0.849 | 0.827 | 0.774 | 0.814 |
| real | cursor minus parse | 1 | cycles M | 407 | 3060 | 577 | 608 | 934 | 563 | 0.133 | 0.705 | 0.67 | 0.436 | 0.723 |
| real | cursor minus parse | 1 | CPU user+sys ms | 79 | 599 | 111 | 116 | 178 | 108 | 0.132 | 0.713 | 0.679 | 0.443 | 0.734 |
| real | cursor minus parse | all | instructions M | 1910 | 12100 | 2180 | 2230 | 2380 | 2300 | 0.158 | 0.874 | 0.856 | 0.8 | 0.831 |
| real | cursor minus parse | all | cycles M | 675 | 4800 | 633 | 769 | 1100 | 877 | 0.141 | 1.07 | 0.877 | 0.612 | 0.769 |
| real | cursor minus parse | all | CPU user+sys ms | 156 | 1120 | 155 | 188 | 240 | 200 | 0.139 | 1.01 | 0.834 | 0.652 | 0.781 |
| real | walk minus parse | 1 | instructions M | 1430 | 8730 | 1050 | n/a | n/a | n/a | 0.164 | 1.37 | n/a | n/a | n/a |
| real | walk minus parse | 1 | cycles M | 369 | 2640 | 298 | n/a | n/a | n/a | 0.14 | 1.24 | n/a | n/a | n/a |
| real | walk minus parse | 1 | CPU user+sys ms | 71.4 | 512 | 57.2 | n/a | n/a | n/a | 0.139 | 1.25 | n/a | n/a | n/a |
| real | walk minus parse | all | instructions M | 1430 | 8820 | 1010 | n/a | n/a | n/a | 0.162 | 1.42 | n/a | n/a | n/a |
| real | walk minus parse | all | cycles M | 574 | 3870 | 396 | n/a | n/a | n/a | 0.148 | 1.45 | n/a | n/a | n/a |
| real | walk minus parse | all | CPU user+sys ms | 134 | 900 | 89.7 | n/a | n/a | n/a | 0.149 | 1.49 | n/a | n/a | n/a |
| realerr | cursor minus parse | 1 | instructions M | 134 | 829 | 163 | 167 | 178 | 170 | 0.161 | 0.818 | 0.798 | 0.749 | 0.787 |
| realerr | cursor minus parse | 1 | cycles M | 29.7 | 208 | 42 | 47.5 | 74.6 | 65.8 | 0.143 | 0.707 | 0.626 | 0.398 | 0.452 |
| realerr | cursor minus parse | 1 | CPU user+sys ms | 5.55 | 41.5 | 8.06 | 9.09 | 14.3 | 12.6 | 0.134 | 0.689 | 0.611 | 0.388 | 0.44 |
| realerr | cursor minus parse | all | instructions M | 134 | 886 | 139 | 146 | 161 | 136 | 0.151 | 0.964 | 0.917 | 0.833 | 0.982 |
| realerr | cursor minus parse | all | cycles M | 29.1 | 257 | 34.9 | 40 | 81.4 | 45.8 | 0.113 | 0.833 | 0.727 | 0.357 | 0.636 |
| realerr | cursor minus parse | all | CPU user+sys ms | 5.78 | 53.1 | 4.1 | 11.6 | 18.4 | 7.72 | 0.109 | 1.41 | 0.499 | 0.314 | 0.748 |
| realerr | walk minus parse | 1 | instructions M | 103 | 609 | 75.7 | n/a | n/a | n/a | 0.169 | 1.36 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | cycles M | 26.1 | 197 | 25.8 | n/a | n/a | n/a | 0.133 | 1.01 | n/a | n/a | n/a |
| realerr | walk minus parse | 1 | CPU user+sys ms | 4.87 | 38.5 | 4.93 | n/a | n/a | n/a | 0.127 | 0.988 | n/a | n/a | n/a |
| realerr | walk minus parse | all | instructions M | 103 | 593 | 74 | n/a | n/a | n/a | 0.174 | 1.4 | n/a | n/a | n/a |
| realerr | walk minus parse | all | cycles M | 31.4 | 228 | 24 | n/a | n/a | n/a | 0.138 | 1.31 | n/a | n/a | n/a |
| realerr | walk minus parse | all | CPU user+sys ms | 5.99 | 47.6 | 2.8 | n/a | n/a | n/a | 0.126 | 2.14 | n/a | n/a | n/a |

### zig: memory after 6 passes (matrix, max over processes)

| set | what | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | cursor | 1 | 31.3 | 67.2 | 19.9 | 0.465 | 1.57 | 31.3 | 67.2 | 19.9 | 0.465 | 1.57 | 18.1 | 52.8 | 9.64 | 0.344 | 1.88 |
| broken | cursor | all | 82.4 | 153 | 101 | 0.537 | 0.815 | 82.4 | 153 | 101 | 0.537 | 0.815 | 69.2 | 139 | 90.6 | 0.498 | 0.763 |
| broken | parse | 1 | 22.2 | 32.7 | 18.8 | 0.678 | 1.18 | 22.2 | 32.7 | 18.8 | 0.678 | 1.18 | 9.07 | 18.2 | 8.35 | 0.499 | 1.09 |
| broken | parse | all | 76.3 | 130 | 101 | 0.588 | 0.753 | 76.3 | 130 | 101 | 0.588 | 0.753 | 63.1 | 115 | 91 | 0.547 | 0.694 |
| broken | walk | 1 | 22.1 | 66 | 19.4 | 0.335 | 1.14 | 22.1 | 66 | 19.4 | 0.335 | 1.14 | 8.99 | 51.6 | 9.18 | 0.174 | 0.979 |
| broken | walk | all | 78.9 | 159 | 99.4 | 0.495 | 0.793 | 78.9 | 159 | 99.4 | 0.495 | 0.794 | 65.6 | 145 | 89 | 0.453 | 0.737 |
| real | cursor | 1 | 44.2 | 128 | 30.4 | 0.345 | 1.45 | 44.2 | 128 | 30.4 | 0.345 | 1.45 | 29.3 | 112 | 18.1 | 0.262 | 1.61 |
| real | cursor | all | 123 | 290 | 191 | 0.422 | 0.64 | 123 | 290 | 191 | 0.422 | 0.64 | 107 | 274 | 179 | 0.392 | 0.598 |
| real | parse | 1 | 32.7 | 45.5 | 29.3 | 0.719 | 1.12 | 32.7 | 45.5 | 29.3 | 0.719 | 1.12 | 17.8 | 29.4 | 17 | 0.604 | 1.04 |
| real | parse | all | 111 | 241 | 193 | 0.462 | 0.576 | 111 | 241 | 193 | 0.462 | 0.576 | 95.9 | 224 | 181 | 0.427 | 0.531 |
| real | walk | 1 | 32.9 | 132 | 30.7 | 0.249 | 1.07 | 32.9 | 132 | 30.7 | 0.249 | 1.07 | 18 | 116 | 18.7 | 0.155 | 0.965 |
| real | walk | all | 114 | 309 | 182 | 0.369 | 0.625 | 114 | 309 | 182 | 0.369 | 0.625 | 98.7 | 292 | 170 | 0.337 | 0.581 |
| realerr | cursor | 1 | 15.2 | 62 | 13.4 | 0.245 | 1.14 | 15.2 | 62 | 13.4 | 0.245 | 1.14 | 11.4 | 56.7 | 10.1 | 0.2 | 1.12 |
| realerr | cursor | all | 57.6 | 96.8 | 84.3 | 0.595 | 0.683 | 57.6 | 96.8 | 84.3 | 0.595 | 0.683 | 52.7 | 91.3 | 81.1 | 0.577 | 0.65 |
| realerr | parse | 1 | 15 | 18.3 | 11.9 | 0.819 | 1.26 | 15 | 18.3 | 11.9 | 0.82 | 1.26 | 11.1 | 12.9 | 8.8 | 0.862 | 1.26 |
| realerr | parse | all | 57.6 | 85.6 | 89.8 | 0.673 | 0.641 | 57.6 | 85.6 | 89.8 | 0.673 | 0.641 | 52.7 | 80 | 86.7 | 0.659 | 0.607 |
| realerr | walk | 1 | 15 | 64.5 | 13.3 | 0.232 | 1.13 | 15 | 64.5 | 13.3 | 0.233 | 1.13 | 11.2 | 59.1 | 10.1 | 0.189 | 1.1 |
| realerr | walk | all | 57.8 | 98.7 | 89.9 | 0.586 | 0.642 | 57.8 | 98.7 | 89.9 | 0.586 | 0.643 | 52.9 | 93.2 | 86.8 | 0.567 | 0.609 |

### zig: memory in the steady state (parse, one process of 41 passes per cell)

| set | j | RSS gs MB | cgo MB | C MB | gs/cgo | gs/C | footprint gs MB | cgo MB | C MB | gs/cgo | gs/C | RSS-loaded gs MB | cgo MB | C MB | gs/cgo | gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken | 1 | 22.4 | 32.5 | 18.1 | 0.688 | 1.23 | 22.4 | 32.5 | 18.1 | 0.689 | 1.23 | 9.29 | 18.1 | 7.92 | 0.513 | 1.17 |
| broken | all | 82.3 | 188 | 119 | 0.438 | 0.69 | 82.3 | 188 | 119 | 0.438 | 0.69 | 68.7 | 173 | 109 | 0.397 | 0.631 |
| real | 1 | 33 | 46 | 29.1 | 0.718 | 1.14 | 33 | 46 | 29.1 | 0.718 | 1.14 | 18.1 | 29.7 | 17 | 0.609 | 1.06 |
| real | all | 116 | 393 | 258 | 0.296 | 0.451 | 116 | 393 | 258 | 0.296 | 0.451 | 101 | 376 | 246 | 0.268 | 0.412 |
| realerr | 1 | 15.3 | 26.7 | 11.7 | 0.573 | 1.3 | 15.3 | 26.7 | 11.7 | 0.573 | 1.31 | 11.4 | 21.2 | 8.78 | 0.535 | 1.29 |
| realerr | all | 62.3 | 207 | 124 | 0.3 | 0.502 | 62.3 | 207 | 124 | 0.3 | 0.502 | 57.3 | 202 | 121 | 0.284 | 0.474 |

### zig: allocations per parse

`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady count is what a re-parse still allocates: 0.0449 (real), 1.4 (realerr), 0.0418 (broken) mallocs per parse here. A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum of the requested sizes. The two are not compared.

| set | parser | gs mallocs | gs new heap bytes | cgo Go mallocs | cgo C allocs | cgo C bytes | C allocs | C bytes | gs/C allocs |
|---|---|---|---|---|---|---|---|---|---|
| real | cold | 0.622 | 45200 | 7.06 | 8000 | 737000 | 8000 | 737000 | 0 |
| real | steady | 0.0449 | 51.5 | 7.06 | 8000 | 737000 | 8000 | 737000 | 0 |
| realerr | cold | 21.4 | 653000 | 8.6 | 15900 | 1470000 | 15900 | 1470000 | 0.00134 |
| realerr | steady | 1.4 | 1340 | 7.93 | 15900 | 1470000 | 15900 | 1470000 | 0 |
| broken | cold | 3.21 | 18300 | 7.64 | 7740 | 810000 | 7740 | 810000 | 0.000415 |
| broken | steady | 0.0418 | 34.6 | 7.64 | 7740 | 810000 | 7740 | 810000 | 0 |

### zig: first parse in a new process (median of K processes)

`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, `parse1` the first parse, `parse2` the second parse of the same file. `process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole process, user and kernel, both parses and the exit included: the cost of a new process. Any `after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until after parse2 and leaves out the loader, the runtime start and the kernel.

| file | metric | gs | cgo | C | gs/cgo | gs/C | K |
|---|---|---|---|---|---|---|---|
| #0 2021 B | init ms | 0.00779 | 0.0106 | 0.00535 | 0.736 | 1.46 | 15 |
| #0 2021 B | parse1 ms | 0.148 | 0.193 | 0.142 | 0.77 | 1.05 | 15 |
| #0 2021 B | init+parse1 ms | 0.156 | 0.207 | 0.147 | 0.751 | 1.06 | 15 |
| #0 2021 B | parse2 ms | 0.0549 | 0.163 | 0.0912 | 0.337 | 0.602 | 15 |
| #0 2021 B | process user+sys ms | 0.894 | 1.37 | 0.529 | 0.651 | 1.69 | 15 |
| #0 2021 B | exec to exit instr user+kernel M | 5.8 | 9.42 | 4.3 | 0.616 | 1.35 | 15 |
| #0 2021 B | exec to exit instr user M | 3.07 | 5.77 | 3.3 | 0.532 | 0.93 | 15 |
| #0 2021 B | exec to exit instr kernel M | 2.75 | 3.64 | 0.996 | 0.755 | 2.76 | 15 |
| #0 2021 B | exec to exit cycles user+kernel M | 4.06 | 6.32 | 2.26 | 0.642 | 1.79 | 15 |
| #0 2021 B | exec to exit task-clock ms | 0.83 | 1.31 | 0.44 | 0.634 | 1.89 | 15 |
| #0 2021 B | peak RSS MB | 4.32 | 5.35 | 3.2 | 0.807 | 1.35 | 15 |
| #0 2021 B | peak footprint MB | 4.34 | 5.38 | 3.14 | 0.808 | 1.38 | 15 |
| #1 11749 B | init ms | 0.00734 | 0.0107 | 0.00543 | 0.685 | 1.35 | 15 |
| #1 11749 B | parse1 ms | 0.603 | 1.28 | 0.843 | 0.47 | 0.715 | 15 |
| #1 11749 B | init+parse1 ms | 0.611 | 1.29 | 0.849 | 0.472 | 0.72 | 15 |
| #1 11749 B | parse2 ms | 0.421 | 1.46 | 0.727 | 0.288 | 0.578 | 15 |
| #1 11749 B | process user+sys ms | 1.78 | 3.88 | 1.85 | 0.458 | 0.958 | 15 |
| #1 11749 B | exec to exit instr user+kernel M | 24.3 | 49.8 | 28.8 | 0.489 | 0.846 | 15 |
| #1 11749 B | exec to exit instr user M | 21.1 | 45.3 | 27.2 | 0.465 | 0.773 | 15 |
| #1 11749 B | exec to exit instr kernel M | 3.24 | 4.49 | 1.51 | 0.722 | 2.15 | 15 |
| #1 11749 B | exec to exit cycles user+kernel M | 8.56 | 19.1 | 9.21 | 0.448 | 0.93 | 15 |
| #1 11749 B | exec to exit task-clock ms | 1.7 | 3.81 | 1.77 | 0.446 | 0.96 | 15 |
| #1 11749 B | peak RSS MB | 4.68 | 5.91 | 3.58 | 0.792 | 1.31 | 15 |
| #1 11749 B | peak footprint MB | 4.71 | 5.94 | 3.49 | 0.793 | 1.35 | 15 |

### zig: cmp (C tree vs gositter tree)

| set | files | nodes | trees_with_errors | mismatching_files | result |
|---|---|---|---|---|---|
| real | 423 | 2323014 | 0 | 0 | ok |
| realerr | 15 | 160172 | 15 | 0 | ok |
| broken | 478 | 1964020 | 444 | 0 | ok |

## Extreme inputs (one process each, parse, j=1, 1 pass, no warm-up)

Each process has a time cap 30/120 s, memory cap 4096 MB. An input that needs more on both sides is not compared on time or memory use; for those the last section lists the wall and the polled RSS peak at the kill. Sides run here: gs, C.

### bash

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| rep_n_1528d5__n100000.sh | 74.3 | 119 | 0.624 | 1540 | 1870 | 0.824 | 78.7 | 83.1 | 0.947 | 78.7 | 83.1 | 0.947 |
| rep_8227ed__n100000.sh | 12.7 | 21.5 | 0.588 | 282 | 316 | 0.893 | 27.7 | 39.2 | 0.705 | 27.7 | 39.2 | 0.705 |
| rep_3c307f__n100000.sh | 69.4 | 96.9 | 0.716 | 1490 | 1660 | 0.898 | 61.8 | 65.6 | 0.942 | 61.8 | 65.6 | 0.942 |
| unterminated_EOF_na_n_f319ea__n30000.sh | 7940 | 18100 | 0.438 | 275000 | 454000 | 0.606 | 62 | 48.8 | 1.27 | 62 | 48.8 | 1.27 |
| broken_stmt_echo_a_grep_x_1_n_3c5400__n100000.sh | 728 | 1170 | 0.625 | 15200 | 18000 | 0.848 | 269 | 304 | 0.883 | 269 | 304 | 0.883 |
| rep_6c2f79__n100000.sh | 4040 | 22800 | 0.177 | 89400 | 408000 | 0.219 | 21 | 17.5 | 1.2 | 21 | 17.5 | 1.2 |
| random_bracket_soup_7022a9__n100000.sh | 5.45 | 5.31 | 1.03 | 105 | 74.8 | 1.41 | 9.84 | 4.25 | 2.31 | 9.86 | 4.25 | 2.32 |
| random_bytes_697b59__n1000000.sh | 157 | rc=245_no_output | n/a | 2700 | rc=245_no_output | n/a | 62.1 | rc=245_no_output | n/a | 62.1 | rc=245_no_output | n/a |
| random_printable_cb9992__n1000000.sh | 62.1 | 143 | 0.433 | 1010 | 2060 | 0.488 | 24 | 30.2 | 0.796 | 24 | 30.2 | 0.796 |
| block_for_i_in_a_do_n_19a71c__n100000.sh | 77.5 | 147 | 0.527 | 1740 | 2210 | 0.784 | 110 | 169 | 0.65 | 110 | 169 | 0.65 |
| pair_ebd0a6__n100000.sh | 18.1 | 31.9 | 0.567 | 373 | 504 | 0.739 | 31.5 | 51.2 | 0.616 | 31.5 | 51.2 | 0.616 |
| advdeep_100000__sh_100000.sh | 34.8 | 61.7 | 0.564 | 791 | 1050 | 0.75 | 53.7 | 46.2 | 1.16 | 53.7 | 46.2 | 1.16 |

### c

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_void_f_a_b_c_d_e_f_g_n_3668c3__n100000.c | 759 | 1100 | 0.691 | 15300 | 18800 | 0.813 | 348 | 363 | 0.958 | 348 | 363 | 0.958 |
| glr_valid_x_a_b_n_31270c__n100000.c | 215 | 280 | 0.767 | 4250 | 5260 | 0.807 | 106 | 94.8 | 1.12 | 106 | 94.8 | 1.12 |
| random_bracket_soup_7022a9__n1000000.c | 479 | 1140 | 0.418 | 7760 | 19300 | 0.403 | 106 | 109 | 0.976 | 106 | 109 | 0.976 |
| unterminated_define_A_c611ad__n10000.c | 91.2 | 621 | 0.147 | 2180 | 15000 | 0.145 | 6.77 | 4.17 | 1.62 | 6.78 | 4.17 | 1.62 |
| rep_n_aec841__n30000.c | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a |
| random_bytes_697b59__n1000000.c | 198 | 509 | 0.388 | 3160 | 8300 | 0.381 | 149 | 218 | 0.686 | 149 | 218 | 0.686 |
| random_printable_cb9992__n1000000.c | 151 | 302 | 0.498 | 2630 | 4810 | 0.546 | 69 | 80.3 | 0.859 | 69 | 80.3 | 0.859 |
| block_void_f_n_7cafb5__n100000.c | 69.9 | 162 | 0.431 | 1460 | 2230 | 0.655 | 103 | 135 | 0.76 | 103 | 135 | 0.76 |
| broken_stmt_int_x_1_n_78b438__n100000.c | 512 | 1750 | 0.292 | 10900 | 29100 | 0.376 | 155 | 209 | 0.739 | 155 | 209 | 0.739 |
| pair_ebd0a6__n100000.c | 160 | 219 | 0.732 | 3340 | 4140 | 0.808 | 52.1 | 72.3 | 0.72 | 52.1 | 72.3 | 0.72 |
| new_two_seq_A_n_2_V_B_n_2_define_A_n_232200__n30000.c | 60600 | 61900 | 0.98 | 1300000 | 1070000 | 1.21 | 28.2 | 20.2 | 1.4 | 28.2 | 20.2 | 1.4 |
| big5mb__big5mb.c | 345 | 628 | 0.55 | 7000 | 10600 | 0.662 | 313 | 305 | 1.03 | 313 | 305 | 1.03 |
| advdeep_100000__c_100000.c | 20.5 | 37.5 | 0.546 | 475 | 656 | 0.724 | 34.5 | 26.3 | 1.31 | 34.5 | 26.3 | 1.31 |
| advdeep_100000__cblk_100000.c | 19.8 | 33.8 | 0.584 | 453 | 641 | 0.707 | 31.7 | 26.3 | 1.21 | 31.8 | 26.3 | 1.21 |

### cpp

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| broken_stmt_std_vector_std_map_int_x_1_n_b8b813__n100000.cpp | 1360 | 5230 | 0.26 | 29500 | 93200 | 0.317 | 449 | 980 | 0.458 | 449 | 980 | 0.458 |
| x_n_x7_a_x5_26d2cc__n10000.cpp | 6960 | 6550 | 1.06 | 155000 | 113000 | 1.37 | 17 | 10.5 | 1.62 | 17 | 10.5 | 1.62 |
| broken_stmt_std_vector_std_map_int_x_1_n_b8b813__n30000.cpp | 372 | 1510 | 0.247 | 8910 | 28000 | 0.319 | 113 | 298 | 0.38 | 113 | 298 | 0.38 |
| unterminated_define_A_c611ad__n10000.cpp | 84 | 611 | 0.137 | 2190 | 15200 | 0.144 | 6.41 | 4.42 | 1.45 | 6.41 | 4.42 | 1.45 |
| rep_n_aec841__n30000.cpp | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a |
| random_bracket_soup_7022a9__n1000000.cpp | 518 | 1380 | 0.374 | 8150 | 23000 | 0.355 | 108 | 112 | 0.97 | 108 | 112 | 0.97 |
| random_bytes_697b59__n1000000.cpp | 207 | 580 | 0.357 | 3390 | 10300 | 0.329 | 152 | 221 | 0.687 | 152 | 221 | 0.687 |
| random_printable_cb9992__n1000000.cpp | 123 | 208 | 0.592 | 2290 | 3250 | 0.707 | 66.7 | 79.9 | 0.835 | 66.7 | 79.9 | 0.835 |
| block_void_f_n_7cafb5__n100000.cpp | 74.2 | 191 | 0.389 | 1560 | 2400 | 0.652 | 112 | 145 | 0.768 | 112 | 145 | 0.768 |
| pair_ebd0a6__n100000.cpp | 87.5 | 321 | 0.273 | 1740 | 6460 | 0.27 | 38.4 | 57.2 | 0.671 | 38.4 | 57.2 | 0.671 |
| glr_valid_void_f_a_b_c_d_e_f_g_h_n_8e9ccf__n100000.cpp | 3030 | 3630 | 0.835 | 63500 | 62100 | 1.02 | 487 | 439 | 1.11 | 487 | 439 | 1.11 |
| big5mb__big5mb.cpp | 319 | 574 | 0.556 | 6320 | 9330 | 0.677 | 271 | 246 | 1.1 | 271 | 246 | 1.1 |
| advdeep_100000__cpp_100000.cpp | 1360 | 1690 | 0.804 | 26600 | 22400 | 1.19 | 722 | 628 | 1.15 | 722 | 628 | 1.15 |

### csharp

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_F_a_b_c_d_n_ff5b49__n100000.cs | 877 | 1340 | 0.656 | 17800 | 21300 | 0.839 | 363 | 380 | 0.957 | 363 | 380 | 0.957 |
| glr_valid_x_a_b_c_n_003570__n100000.cs | 593 | 922 | 0.643 | 12200 | 14800 | 0.819 | 324 | 301 | 1.07 | 324 | 301 | 1.07 |
| unterminated_a_adca5b__n30000.cs | 869 | 5460 | 0.159 | 17200 | 69800 | 0.246 | 18.7 | 11.3 | 1.66 | 18.7 | 11.3 | 1.66 |
| rep_n_aec841__n30000.cs | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a | KILL_TIME | KILL_TIME | n/a |
| unterminated_a_ff4b90__n30000.cs | 553 | 3640 | 0.152 | 13800 | 56200 | 0.245 | 14.3 | 7.27 | 1.96 | 14.3 | 7.27 | 1.97 |
| random_bracket_soup_7022a9__n100000.cs | 111 | 262 | 0.424 | 2020 | 4360 | 0.464 | 21.5 | 20 | 1.07 | 21.5 | 20 | 1.07 |
| random_bytes_697b59__n1000000.cs | 233 | 668 | 0.348 | 3810 | 11800 | 0.323 | 167 | 218 | 0.764 | 167 | 218 | 0.764 |
| random_printable_cb9992__n1000000.cs | 247 | 769 | 0.321 | 4230 | 12900 | 0.327 | 69.6 | 80.6 | 0.864 | 69.6 | 80.6 | 0.864 |
| block_using_x_n_6f50d3__n100000.cs | 121 | 241 | 0.503 | 2780 | 2950 | 0.94 | 146 | 199 | 0.732 | 146 | 199 | 0.732 |
| broken_stmt_var_x_new_A_1_n_82c71b__n100000.cs | 1090 | 1530 | 0.711 | 24900 | 26900 | 0.926 | 262 | 242 | 1.08 | 262 | 242 | 1.08 |
| pair_0dd45c__n100000.cs | 50.3 | 83.6 | 0.601 | 994 | 1430 | 0.695 | 61.4 | 64.5 | 0.951 | 61.4 | 64.5 | 0.951 |
| deep_A_int_x_n_dbd987__n100000.cs | 322 | 516 | 0.624 | 6120 | 6610 | 0.926 | 208 | 228 | 0.914 | 208 | 228 | 0.914 |
| rep_n_8ed84f__n100000.cs | 30900 | 39400 | 0.784 | 893000 | 687000 | 1.3 | 56.3 | 49 | 1.15 | 56.3 | 49 | 1.15 |
| unterminated_a_a452e7__n100000.cs | 12800 | 76600 | 0.167 | 294000 | 1150000 | 0.255 | 26.5 | 19.9 | 1.33 | 26.5 | 19.9 | 1.33 |

### golang

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| x_n_x3_a_x5_7103f9__n132000.go | 105000 | KILL_TIME | n/a | 2170000 | KILL_TIME | n/a | 44.9 | KILL_TIME | n/a | 44.9 | KILL_TIME | n/a |
| x_n_x3_a_x5_7103f9__n80000.go | 37400 | 44200 | 0.846 | 799000 | 743000 | 1.08 | 30 | 24.3 | 1.23 | 30 | 24.3 | 1.23 |
| rep_n_b5fbe6__n10000.go | 26200 | 28500 | 0.919 | 574000 | 497000 | 1.15 | 20.7 | 13.6 | 1.52 | 20.7 | 13.6 | 1.52 |
| unterminated_a_266e40__n30000.go | 3150 | 10600 | 0.297 | 119000 | 267000 | 0.445 | 21.8 | 20.1 | 1.08 | 21.9 | 20.1 | 1.09 |
| random_bracket_soup_7022a9__n1000000.go | 410 | 1070 | 0.383 | 6830 | 16600 | 0.41 | 75.5 | 96.3 | 0.784 | 75.5 | 96.3 | 0.784 |
| random_bytes_697b59__n1000000.go | 199 | 455 | 0.437 | 3280 | 6950 | 0.472 | 148 | 214 | 0.691 | 148 | 214 | 0.691 |
| random_printable_cb9992__n1000000.go | 260 | 734 | 0.354 | 3940 | 11600 | 0.339 | 42.2 | 42.1 | 1 | 42.2 | 42.1 | 1 |
| block_func_f_n_e8e87e__n100000.go | 172 | 322 | 0.533 | 3400 | 5770 | 0.589 | 79.5 | 131 | 0.608 | 79.5 | 131 | 0.608 |
| broken_stmt_func_f_return_1_n_n_5b0a65__n100000.go | 1190 | 1880 | 0.633 | 26600 | 34200 | 0.776 | 144 | 139 | 1.04 | 144 | 139 | 1.04 |
| pair_b94783__n100000.go | 228 | 920 | 0.247 | 5190 | 16400 | 0.317 | 38.3 | 62 | 0.618 | 38.3 | 62 | 0.618 |
| glr_valid_func_f_if_a_T_x_a_b_c_n_b85f6e__n100000.go | 420 | 673 | 0.624 | 8700 | 11300 | 0.769 | 350 | 346 | 1.01 | 350 | 346 | 1.01 |
| new_two_seq_A_n_2_V_B_n_2_n_a_bedb31__n30000.go | 59800 | 65500 | 0.914 | 1290000 | 1130000 | 1.15 | 34.2 | 24.7 | 1.39 | 34.2 | 24.7 | 1.39 |
| big5mb__big5mb.go | 503 | 809 | 0.622 | 10200 | 12600 | 0.803 | 306 | 298 | 1.03 | 306 | 298 | 1.03 |
| advdeep_100000__go_100000.go | 20.6 | 40.3 | 0.512 | 487 | 713 | 0.683 | 32.6 | 26.1 | 1.25 | 32.6 | 26.1 | 1.25 |

### hcl

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| random_printable_cb9992__n1000000.hcl | 320 | 533 | 0.6 | 6650 | 8350 | 0.797 | 293 | 341 | 0.861 | 293 | 341 | 0.861 |
| broken_stmt_a_b_c_n_e61895__n10000.hcl | 49.1 | 82.7 | 0.594 | 1010 | 1270 | 0.796 | 30.3 | 34.1 | 0.888 | 30.3 | 34.1 | 0.888 |
| pair_0dd45c__n100000.hcl | 18100 | 45600 | 0.397 | 538000 | 1030000 | 0.524 | 75.5 | 91.5 | 0.825 | 75.5 | 91.5 | 0.825 |
| unterminated_if_a_f41bf6__n100000.hcl | 473 | 1750 | 0.271 | 10600 | 28100 | 0.378 | 627 | 288 | 2.18 | 627 | 288 | 2.18 |
| unterminated_a_ac3879__n100000.hcl | 443 | 1550 | 0.286 | 9670 | 25400 | 0.381 | 576 | 237 | 2.43 | 576 | 237 | 2.43 |
| random_bracket_soup_7022a9__n1000000.hcl | 649 | 1950 | 0.333 | 17100 | 28800 | 0.592 | 394 | 751 | 0.525 | 394 | 751 | 0.525 |
| random_bytes_697b59__n1000000.hcl | 641 | 1900 | 0.337 | 17100 | 29000 | 0.591 | 253 | 753 | 0.337 | 253 | 753 | 0.337 |
| rep_6c2f79__n100000.hcl | 208 | 426 | 0.488 | 3740 | 6190 | 0.605 | 445 | 162 | 2.76 | 445 | 162 | 2.76 |
| block_resource_a_b_n_f29953__n100000.hcl | 166 | 456 | 0.364 | 3440 | 4010 | 0.858 | 193 | 261 | 0.74 | 193 | 261 | 0.74 |
| deep_x_1_n_b2e152__n100000.hcl | 6860 | 32500 | 0.211 | 117000 | 330000 | 0.353 | 376 | 209 | 1.8 | 376 | 209 | 1.8 |
| unterminated_a_adca5b__n100000.hcl | 12700 | 63200 | 0.201 | 225000 | 640000 | 0.352 | 54.3 | 44.3 | 1.23 | 54.3 | 44.3 | 1.23 |

### java

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n100000.java | 905 | 1370 | 0.66 | 18800 | 22500 | 0.836 | 528 | 492 | 1.07 | 528 | 492 | 1.07 |
| glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n10000.java | 91 | 140 | 0.649 | 1890 | 2250 | 0.839 | 52.3 | 51.8 | 1.01 | 52.3 | 51.8 | 1.01 |
| pair_b94783__n100000.java | 654 | 822 | 0.796 | 15400 | 14700 | 1.05 | 171 | 232 | 0.736 | 171 | 232 | 0.736 |
| unterminated_a_266e40__n30000.java | 3160 | 10700 | 0.296 | 120000 | 268000 | 0.446 | 25.4 | 23.7 | 1.07 | 25.4 | 23.7 | 1.07 |
| pair_b94783__n10000.java | 62.5 | 81.4 | 0.767 | 1550 | 1470 | 1.06 | 23.4 | 25.8 | 0.909 | 23.4 | 25.8 | 0.91 |
| random_bracket_soup_7022a9__n1000000.java | 312 | 586 | 0.533 | 5610 | 10200 | 0.549 | 63.4 | 55.7 | 1.14 | 63.4 | 55.7 | 1.14 |
| random_bytes_697b59__n1000000.java | 177 | 480 | 0.368 | 2860 | 8350 | 0.343 | 114 | 155 | 0.733 | 114 | 155 | 0.733 |
| random_printable_cb9992__n1000000.java | 281 | 799 | 0.351 | 4240 | 13600 | 0.313 | 43.7 | 46.1 | 0.949 | 43.7 | 46.1 | 0.949 |
| rep_n_b5fbe6__n100000.java | 53.3 | 54.9 | 0.97 | 1180 | 960 | 1.22 | 44 | 43 | 1.02 | 44.1 | 43 | 1.02 |
| block_for_n_eb9345__n100000.java | 107 | 164 | 0.656 | 2520 | 2320 | 1.09 | 114 | 195 | 0.581 | 114 | 195 | 0.581 |
| broken_stmt_class_A_void_f_return_1_n_eb23bc__n100000.java | 538 | 1050 | 0.511 | 11300 | 19200 | 0.585 | 160 | 169 | 0.947 | 160 | 169 | 0.947 |
| big5mb__big5mb.java | 342 | 586 | 0.583 | 6820 | 9790 | 0.697 | 291 | 279 | 1.04 | 291 | 279 | 1.04 |
| advdeep_100000__java_100000.java | 24.5 | 44.1 | 0.556 | 558 | 775 | 0.72 | 39.4 | 36.5 | 1.08 | 39.5 | 36.5 | 1.08 |

### javascript

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_x_a_b_c_d_e_f_g_h_n_f95d77__n100000.js | 691 | 1050 | 0.66 | 13900 | 17300 | 0.803 | 360 | 382 | 0.944 | 360 | 382 | 0.944 |
| random_bracket_soup_7022a9__n1000000.js | 1920 | 2130 | 0.904 | 50800 | 33700 | 1.5 | 218 | 175 | 1.25 | 218 | 175 | 1.25 |
| glr_valid_a_b_c_n_d20807__n100000.js | 232 | 296 | 0.784 | 4520 | 5200 | 0.871 | 135 | 125 | 1.08 | 135 | 125 | 1.08 |
| rep_045310__n30000.js | 7210 | 13100 | 0.549 | 205000 | 320000 | 0.639 | 16.8 | 13.7 | 1.23 | 16.8 | 13.7 | 1.23 |
| unterminated_a_a452e7__n100000.js | 11600 | 60800 | 0.191 | 226000 | 651000 | 0.347 | 27.9 | 21.9 | 1.28 | 27.9 | 21.9 | 1.28 |
| random_bytes_697b59__n1000000.js | 226 | 566 | 0.4 | 3730 | 8990 | 0.415 | 153 | 220 | 0.698 | 153 | 220 | 0.698 |
| random_printable_cb9992__n1000000.js | 253 | 840 | 0.301 | 3820 | 13900 | 0.275 | 43 | 45.4 | 0.947 | 43 | 45.4 | 0.947 |
| block_for_n_3794d8__n100000.js | 84.2 | 167 | 0.504 | 1990 | 2220 | 0.898 | 111 | 169 | 0.657 | 111 | 169 | 0.657 |
| broken_stmt_function_f_return_1_n_4bd8f0__n100000.js | 1000 | 1460 | 0.683 | 23100 | 23300 | 0.989 | 178 | 284 | 0.626 | 178 | 284 | 0.626 |
| pair_f0f6a0__n100000.js | 32.5 | 59.6 | 0.546 | 722 | 766 | 0.944 | 46.8 | 71.5 | 0.654 | 46.8 | 71.5 | 0.655 |
| block_class_A_n_31164b__n100000.js | 8400 | 43900 | 0.191 | 164000 | 682000 | 0.24 | 43.4 | 34.1 | 1.28 | 43.4 | 34.1 | 1.28 |
| big5mb__big5mb.js | 458 | 754 | 0.607 | 8540 | 11500 | 0.742 | 341 | 333 | 1.02 | 341 | 333 | 1.02 |
| advdeep_100000__js_100000.js | 25.7 | 48.7 | 0.528 | 611 | 850 | 0.719 | 39.3 | 36.8 | 1.07 | 39.3 | 36.8 | 1.07 |
| advdeep_100000__jsfn_100000.js | 83.9 | 161 | 0.522 | 1970 | 2590 | 0.761 | 97.7 | 108 | 0.904 | 97.7 | 108 | 0.904 |

### json

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| new_two_inter_rand_runs_A_B_V_a_b_95b842__n30000.json | 39 | 55.8 | 0.698 | 763 | 841 | 0.906 | 37.5 | 52 | 0.721 | 37.5 | 52 | 0.721 |
| new_rnd_A_B_C_D_a_b_efebf3__n30000.json | 104 | 106 | 0.983 | 1940 | 1770 | 1.09 | 25.7 | 30.5 | 0.842 | 25.7 | 30.5 | 0.842 |
| random_bytes_697b59__n1000000.json | 56 | 77.3 | 0.725 | 887 | 1130 | 0.788 | 43.1 | 57.3 | 0.753 | 43.1 | 57.3 | 0.753 |
| random_bracket_soup_7022a9__n1000000.json | 200 | 325 | 0.617 | 3370 | 3880 | 0.869 | 160 | 231 | 0.69 | 160 | 231 | 0.69 |
| broken_stmt_1_2_3_n_6f817b__n100000.json | 284 | 303 | 0.939 | 5640 | 5620 | 1 | 83.5 | 84.8 | 0.985 | 83.5 | 84.8 | 0.985 |
| random_printable_cb9992__n1000000.json | 55.7 | 88.2 | 0.632 | 941 | 1260 | 0.744 | 52.4 | 69.2 | 0.756 | 52.4 | 69.2 | 0.756 |
| rep_0dc390__n100000.json | 14.7 | 28 | 0.527 | 293 | 434 | 0.675 | 21.8 | 35.5 | 0.616 | 21.8 | 35.5 | 0.616 |
| unterminated_a_b_ceeb36__n100000.json | 29100 | KILL_TIME | n/a | 451000 | KILL_TIME | n/a | 87.2 | KILL_TIME | n/a | 87.2 | KILL_TIME | n/a |
| block_a_n_6700cf__n100000.json | 57.6 | 108 | 0.532 | 1380 | 1450 | 0.95 | 74.7 | 112 | 0.667 | 74.7 | 112 | 0.667 |
| pair_ebd0a6__n100000.json | 94.8 | 121 | 0.781 | 1790 | 1990 | 0.897 | 57.8 | 80.9 | 0.715 | 57.9 | 80.9 | 0.715 |
| big5mb__big5mb.json | 372 | 563 | 0.66 | 8040 | 9760 | 0.824 | 342 | 342 | 1 | 342 | 342 | 1 |
| advdeep_100000__json_100000.json | 19.8 | 32.6 | 0.607 | 462 | 611 | 0.756 | 32.3 | 25.9 | 1.25 | 32.3 | 25.9 | 1.25 |

### julia

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| random_bytes_697b59__n1000000.jl | 884 | 1620 | 0.546 | 21600 | 30300 | 0.711 | 115 | 205 | 0.558 | 115 | 205 | 0.558 |
| rep_n_6d5fcf__n100000.jl | 7.41 | 15.6 | 0.474 | 167 | 242 | 0.691 | 19 | 27.9 | 0.68 | 19 | 27.9 | 0.681 |
| rep_40a18c__n100000.jl | 9.84 | 17.8 | 0.553 | 253 | 285 | 0.888 | 19.3 | 32.9 | 0.588 | 19.3 | 32.9 | 0.588 |
| broken_stmt_x_f_1_n_586b17__n30000.jl | 9090 | 21100 | 0.43 | 302000 | 544000 | 0.555 | 109 | 231 | 0.47 | 109 | 231 | 0.47 |
| broken_stmt_function_f_x_end_n_92fd0a__n100000.jl | 195 | 455 | 0.429 | 3820 | 5260 | 0.727 | 203 | 272 | 0.748 | 203 | 272 | 0.748 |
| random_bracket_soup_7022a9__n100000.jl | 843 | 1700 | 0.496 | 19900 | 36100 | 0.553 | 15.1 | 13.1 | 1.15 | 15.1 | 13.1 | 1.15 |
| random_printable_cb9992__n100000.jl | 87.5 | 182 | 0.481 | 2300 | 4110 | 0.56 | 11.1 | 7.45 | 1.5 | 11.1 | 7.45 | 1.5 |
| unterminated_a_716541__n10000.jl | 1750 | 4050 | 0.433 | 60000 | 111000 | 0.542 | 20.4 | 19.9 | 1.02 | 20.4 | 19.9 | 1.02 |
| block_function_f_n_cb6fb3__n100000.jl | 102 | 222 | 0.459 | 2060 | 2610 | 0.788 | 114 | 156 | 0.736 | 114 | 156 | 0.736 |
| pair_ebd0a6__n100000.jl | 20.7 | 38.5 | 0.539 | 526 | 643 | 0.818 | 33.5 | 60.8 | 0.55 | 33.5 | 60.8 | 0.55 |
| deep_x_1_n_be5de1__n100000.jl | 84.7 | 173 | 0.49 | 1830 | 2490 | 0.734 | 128 | 116 | 1.11 | 128 | 116 | 1.11 |
| rep_b0b883__n100000.jl | 5830 | 30200 | 0.193 | 102000 | 296000 | 0.345 | 27.2 | 20.3 | 1.34 | 27.2 | 20.3 | 1.34 |
| 119_trunc.jl | 17300 | 101000 | 0.171 | 404000 | 2040000 | 0.198 | 1380 | 1450 | 0.947 | 1380 | 1450 | 0.947 |
| 121_trunc.jl | 17600 | 101000 | 0.173 | 408000 | 2040000 | 0.199 | 1380 | 1450 | 0.948 | 1380 | 1450 | 0.948 |
| 125_trunc.jl | 17700 | 102000 | 0.173 | 409000 | 2060000 | 0.198 | 1380 | 1450 | 0.947 | 1380 | 1450 | 0.947 |
| 127_trunc.jl | 17400 | 100000 | 0.173 | 403000 | 2040000 | 0.198 | 1380 | 1440 | 0.959 | 1380 | 1440 | 0.959 |

### kotlin

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_val_x_a_b_c_n_837d02__n100000.kt | 669 | 886 | 0.755 | 12900 | 13900 | 0.926 | 259 | 220 | 1.18 | 259 | 220 | 1.18 |
| block_class_A_n_31164b__n100000.kt | 32.4 | 62.7 | 0.516 | 731 | 1080 | 0.68 | 53.4 | 76.6 | 0.697 | 53.4 | 76.6 | 0.697 |
| glr_valid_val_y_a_b_c_n_fa3a79__n100000.kt | 671 | 918 | 0.731 | 13100 | 14500 | 0.903 | 253 | 221 | 1.15 | 253 | 221 | 1.15 |
| new_two_inter_ramp_A_k_B_k_for_i_in_a_n_3954d1__n30000.kt | 3400 | 9280 | 0.366 | 111000 | 208000 | 0.533 | 25.1 | 34.9 | 0.721 | 25.2 | 34.9 | 0.721 |
| unterminated_a_ac3879__n100000.kt | 17900 | 91100 | 0.197 | 358000 | 1650000 | 0.217 | 40.4 | 31.2 | 1.29 | 40.4 | 31.2 | 1.29 |
| random_bracket_soup_7022a9__n1000000.kt | 5 | 9.87 | 0.506 | 111 | 198 | 0.558 | 7.28 | 6.26 | 1.16 | 7.28 | 6.26 | 1.16 |
| random_bytes_697b59__n1000000.kt | 8.83 | 11.5 | 0.77 | 104 | 125 | 0.827 | 6.89 | 6.17 | 1.12 | 6.89 | 6.17 | 1.12 |
| random_printable_cb9992__n1000000.kt | 7.48 | 9.85 | 0.759 | 158 | 181 | 0.872 | 9.79 | 8.32 | 1.18 | 9.79 | 8.32 | 1.18 |
| rep_n_93b953__n100000.kt | 66.1 | 61.5 | 1.07 | 1320 | 1040 | 1.26 | 54.8 | 54 | 1.02 | 54.8 | 54 | 1.02 |
| broken_stmt_val_x_f_1_n_46f8f5__n10000.kt | 1470 | 3940 | 0.372 | 47200 | 87900 | 0.538 | 23.3 | 21.1 | 1.1 | 23.3 | 21.1 | 1.1 |
| pair_b94783__n100000.kt | 17.4 | 34.9 | 0.498 | 362 | 538 | 0.674 | 31.5 | 52.6 | 0.6 | 31.5 | 52.6 | 0.6 |
| deep_fun_f_if_x_n_b0fafb__n100000.kt | 79.1 | 181 | 0.437 | 1820 | 2710 | 0.671 | 111 | 139 | 0.798 | 111 | 139 | 0.798 |
| 015_stray.kt | 16500 | KILL_RSS | n/a | 401000 | KILL_RSS | n/a | 3840 | KILL_RSS | n/a | 3840 | KILL_RSS | n/a |
| 075_stray.kt | 13100 | KILL_RSS | n/a | 293000 | KILL_RSS | n/a | 3830 | KILL_RSS | n/a | 3830 | KILL_RSS | n/a |
| 079_stray.kt | KILL_RSS | KILL_RSS | n/a | KILL_RSS | KILL_RSS | n/a | KILL_RSS | KILL_RSS | n/a | KILL_RSS | KILL_RSS | n/a |
| 128_stray.kt | 120000 | KILL_TIME | n/a | 3440000 | KILL_TIME | n/a | 3580 | KILL_TIME | n/a | 3580 | KILL_TIME | n/a |

### lua

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| block_for_i_1_2_do_n_cd6c87__n100000.lua | 76.4 | 159 | 0.481 | 1900 | 2370 | 0.802 | 81.9 | 114 | 0.716 | 81.9 | 114 | 0.716 |
| block_x_function_n_370b99__n100000.lua | 80 | 199 | 0.402 | 1790 | 2250 | 0.796 | 92.6 | 148 | 0.624 | 92.6 | 148 | 0.624 |
| pair_ebd0a6__n30000.lua | 3170 | 6630 | 0.478 | 99900 | 189000 | 0.529 | 17.6 | 25.7 | 0.686 | 17.6 | 25.7 | 0.687 |
| unterminated_a_1609f6__n10000.lua | 1300 | 3390 | 0.384 | 37800 | 75400 | 0.501 | 8.96 | 6.53 | 1.37 | 9 | 6.53 | 1.38 |
| rep_n_160caa__n10000.lua | 500 | 1520 | 0.33 | 15100 | 32400 | 0.466 | 7.68 | 5.07 | 1.51 | 7.7 | 5.07 | 1.52 |
| random_bracket_soup_7022a9__n1000000.lua | 5.88 | 8.06 | 0.729 | 114 | 157 | 0.728 | 8.16 | 5.93 | 1.38 | 8.18 | 5.93 | 1.38 |
| random_bytes_697b59__n1000000.lua | 1230 | 2020 | 0.609 | 30100 | 40800 | 0.736 | 156 | 215 | 0.728 | 156 | 215 | 0.728 |
| random_printable_cb9992__n1000000.lua | 41.4 | 81.1 | 0.511 | 1030 | 1930 | 0.532 | 6.9 | 5.28 | 1.31 | 6.91 | 5.28 | 1.31 |
| broken_stmt_function_f_return_1_end_n_a7c1b1__n3000.lua | 262 | 549 | 0.478 | 7350 | 14000 | 0.525 | 10.6 | 8.09 | 1.31 | 10.6 | 8.09 | 1.31 |
| advdeep_100000__lua_100000.lua | 22.4 | 41 | 0.546 | 521 | 752 | 0.692 | 32.9 | 25.8 | 1.27 | 32.9 | 25.8 | 1.27 |

### php

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_x_fn_a_a_b_c_n_744123__n100000.php | 372 | 692 | 0.537 | 7500 | 10800 | 0.695 | 429 | 431 | 0.994 | 429 | 431 | 0.994 |
| unterminated_a_122a46__n100000.php | 544 | 6710 | 0.0811 | 12700 | 147000 | 0.0863 | 114 | 112 | 1.02 | 114 | 112 | 1.02 |
| broken_stmt_x_f_1_n_b979a9__n100000.php | 319 | 664 | 0.48 | 6410 | 9450 | 0.678 | 202 | 285 | 0.71 | 202 | 285 | 0.71 |
| unterminated_a_266e40__n30000.php | 3170 | 10800 | 0.295 | 120000 | 268000 | 0.447 | 28.9 | 27.2 | 1.06 | 29 | 27.2 | 1.07 |
| random_bracket_soup_7022a9__n1000000.php | 876 | 941 | 0.931 | 21800 | 16400 | 1.33 | 182 | 176 | 1.04 | 182 | 176 | 1.04 |
| random_bytes_697b59__n1000000.php | 339 | 1020 | 0.333 | 6610 | 17900 | 0.369 | 120 | 232 | 0.518 | 120 | 232 | 0.518 |
| random_printable_cb9992__n1000000.php | 2140 | 2610 | 0.819 | 56800 | 37100 | 1.53 | 54.4 | 39.9 | 1.36 | 54.4 | 39.9 | 1.36 |
| rep_EOT_fa7752__n100000.php | 231 | 317 | 0.73 | 4670 | 5770 | 0.809 | 80.5 | 72.8 | 1.11 | 80.5 | 72.8 | 1.11 |
| block_if_x_n_6d0298__n100000.php | 99.5 | 220 | 0.453 | 2220 | 2810 | 0.788 | 111 | 159 | 0.702 | 111 | 159 | 0.702 |
| pair_b94783__n100000.php | 335 | 500 | 0.67 | 7280 | 9300 | 0.783 | 38.9 | 63 | 0.617 | 38.9 | 63 | 0.617 |
| deep_x_a_1_n_0eb324__n100000.php | 146 | 296 | 0.493 | 2790 | 4280 | 0.653 | 201 | 186 | 1.08 | 201 | 186 | 1.08 |

### python

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| random_bracket_soup_7022a9__n1000000.py | 6050 | 7580 | 0.798 | 162000 | 110000 | 1.47 | 252 | 250 | 1.01 | 252 | 250 | 1.01 |
| glr_valid_print_a_for_a_in_b_n_0a52da__n100000.py | 374 | 506 | 0.738 | 7340 | 8410 | 0.873 | 179 | 172 | 1.04 | 179 | 172 | 1.04 |
| glr_valid_x_a_for_a_in_b_if_c_n_9d9fc0__n100000.py | 235 | 416 | 0.565 | 4800 | 6810 | 0.706 | 211 | 212 | 0.999 | 211 | 212 | 0.999 |
| rep_n_d0e171__n100000.py | 41400 | 56900 | 0.728 | 770000 | 1140000 | 0.678 | 24.4 | 36.5 | 0.667 | 24.4 | 36.5 | 0.667 |
| random_bytes_697b59__n1000000.py | 256 | 563 | 0.454 | 4370 | 8660 | 0.505 | 117 | 174 | 0.675 | 117 | 174 | 0.675 |
| random_printable_cb9992__n1000000.py | 163 | 469 | 0.348 | 2380 | 7390 | 0.321 | 29.6 | 39 | 0.76 | 29.6 | 39 | 0.76 |
| unterminated_f_a_7130d5__n100000.py | 278 | 1130 | 0.246 | 5880 | 22800 | 0.258 | 79.1 | 68.8 | 1.15 | 79.1 | 68.8 | 1.15 |
| block_with_a_n_0b75bc__n100000.py | 85.4 | 132 | 0.645 | 1600 | 2210 | 0.727 | 69.8 | 72.8 | 0.959 | 69.8 | 72.8 | 0.959 |
| broken_stmt_x_f_1_n_4e85f5__n100000.py | 395 | 1420 | 0.278 | 8700 | 21400 | 0.406 | 164 | 246 | 0.666 | 164 | 246 | 0.666 |
| pair_0dd45c__n100000.py | 12300 | 60500 | 0.203 | 229000 | 675000 | 0.339 | 32.6 | 26.5 | 1.23 | 32.6 | 26.5 | 1.23 |
| big5mb__big5mb.py | 394 | 705 | 0.559 | 7730 | 10800 | 0.717 | 312 | 304 | 1.03 | 312 | 304 | 1.03 |
| advdeep_100000__py_100000.py | 27.6 | 54.9 | 0.503 | 646 | 986 | 0.655 | 40.1 | 37.1 | 1.08 | 40.1 | 37.1 | 1.08 |

### ruby

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_f_a_b_do_c_d_end_n_e39762__n100000.rb | 293 | 554 | 0.529 | 5760 | 8450 | 0.681 | 345 | 339 | 1.02 | 345 | 339 | 1.02 |
| broken_stmt_x_f_1_n_586b17__n100000.rb | 545 | 981 | 0.556 | 11200 | 16800 | 0.669 | 243 | 289 | 0.841 | 243 | 289 | 0.841 |
| unterminated_a_1942ab__n100000.rb | 6550 | 42700 | 0.153 | 118000 | 378000 | 0.311 | 86.6 | 76.3 | 1.14 | 86.6 | 76.3 | 1.14 |
| rep_q_n_428283__n30000.rb | 7820 | 17700 | 0.442 | 264000 | 427000 | 0.619 | 233 | 66.3 | 3.51 | 233 | 66.3 | 3.51 |
| unterminated_a_1942ab__n30000.rb | 550 | 4030 | 0.136 | 11800 | 44200 | 0.268 | 30 | 26.2 | 1.14 | 30 | 26.2 | 1.15 |
| random_bracket_soup_7022a9__n1000000.rb | 21.5 | 48.3 | 0.445 | 414 | 868 | 0.477 | 11.5 | 7.55 | 1.53 | 11.5 | 7.55 | 1.53 |
| random_bytes_697b59__n1000000.rb | 425 | 864 | 0.491 | 6570 | 16400 | 0.401 | 108 | 140 | 0.77 | 108 | 140 | 0.77 |
| random_printable_cb9992__n1000000.rb | 40.6 | 107 | 0.379 | 736 | 1790 | 0.411 | 27.2 | 28 | 0.972 | 27.2 | 28 | 0.972 |
| block_a_do_b_n_97d512__n100000.rb | 120 | 274 | 0.438 | 2580 | 2900 | 0.891 | 127 | 187 | 0.683 | 127 | 187 | 0.683 |
| pair_ebd0a6__n100000.rb | 23.6 | 42.1 | 0.559 | 613 | 709 | 0.864 | 33.7 | 60.5 | 0.557 | 33.7 | 60.5 | 0.557 |
| pair_0dd45c__n100000.rb | 12400 | 64200 | 0.194 | 230000 | 692000 | 0.332 | 34.5 | 29 | 1.19 | 34.5 | 29 | 1.19 |
| advdeep_100000__rb_100000.rb | 44 | 81 | 0.543 | 934 | 1350 | 0.691 | 65.8 | 64.6 | 1.02 | 65.8 | 64.6 | 1.02 |

### rust

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| block_x_n_756d62__n100000.rs | 587 | 800 | 0.733 | 13500 | 12000 | 1.13 | 188 | 393 | 0.477 | 188 | 393 | 0.477 |
| new_alt_B_D_r_a_x_n_8ab5c8__n30000.rs | 120 | 300 | 0.401 | 2890 | 5930 | 0.487 | 27.8 | 43.4 | 0.641 | 27.9 | 43.4 | 0.641 |
| glr_valid_fn_f_if_a_b_c_n_be6ae4__n100000.rs | 259 | 424 | 0.611 | 4980 | 6850 | 0.727 | 248 | 251 | 0.988 | 248 | 251 | 0.988 |
| new_two_inter_ramp_A_k_B_k_r_a_x_n_639b25__n30000.rs | 2760 | 6740 | 0.409 | 80600 | 159000 | 0.506 | 35.5 | 68.4 | 0.52 | 35.5 | 68.4 | 0.52 |
| unterminated_r_a_4676a0__n10000.rs | 921 | 3480 | 0.265 | 23400 | 52900 | 0.442 | 10.6 | 7.04 | 1.51 | 10.6 | 7.04 | 1.51 |
| random_bracket_soup_7022a9__n1000000.rs | 393 | 571 | 0.688 | 7260 | 8630 | 0.842 | 173 | 208 | 0.832 | 173 | 208 | 0.832 |
| random_bytes_697b59__n1000000.rs | 499 | 992 | 0.503 | 10400 | 20200 | 0.517 | 102 | 98.8 | 1.03 | 102 | 98.8 | 1.03 |
| random_printable_cb9992__n1000000.rs | 90.3 | 126 | 0.718 | 1950 | 2190 | 0.89 | 30 | 26.4 | 1.14 | 30 | 26.4 | 1.14 |
| rep_n_b5fbe6__n100000.rs | 119 | 242 | 0.493 | 2510 | 4990 | 0.504 | 46.3 | 71.8 | 0.644 | 46.3 | 71.8 | 0.644 |
| broken_stmt_fn_f_i32_1_n_0138d8__n100000.rs | 466 | 603 | 0.772 | 9450 | 10500 | 0.904 | 148 | 157 | 0.944 | 148 | 157 | 0.944 |
| pair_ebd0a6__n100000.rs | 17.3 | 31.2 | 0.554 | 357 | 468 | 0.764 | 30.9 | 51.8 | 0.598 | 30.9 | 51.8 | 0.598 |
| unterminated_r_a_bcb8a3__n100000.rs | 55200 | KILL_TIME | n/a | 904000 | KILL_TIME | n/a | 59.5 | KILL_TIME | n/a | 59.5 | KILL_TIME | n/a |
| big5mb__big5mb.rs | 328 | 590 | 0.556 | 6450 | 9390 | 0.688 | 307 | 297 | 1.03 | 307 | 297 | 1.03 |
| advdeep_100000__rs_100000.rs | 25.5 | 47.4 | 0.538 | 595 | 827 | 0.72 | 40 | 37 | 1.08 | 40 | 37 | 1.08 |

### scala

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| rep_n_7fd5aa__n100000.scala | 97.3 | 97 | 1 | 1980 | 1530 | 1.29 | 84.2 | 83.9 | 1 | 84.2 | 83.9 | 1 |
| rep_n_656002__n30000.scala | 30 | 28.7 | 1.05 | 599 | 460 | 1.3 | 32.1 | 29.5 | 1.09 | 32.1 | 29.5 | 1.09 |
| rep_n_59d967__n100000.scala | 43.5 | 62.7 | 0.694 | 902 | 926 | 0.974 | 57.7 | 53.5 | 1.08 | 57.7 | 53.5 | 1.08 |
| rep_n_0ca2f1__n100000.scala | 59.7 | 83.2 | 0.718 | 1180 | 1330 | 0.891 | 70.1 | 63.1 | 1.11 | 70.1 | 63.1 | 1.11 |
| rep_n_63f874__n100000.scala | 71.4 | 180 | 0.397 | 1270 | 2830 | 0.448 | 74.3 | 100 | 0.742 | 74.3 | 100 | 0.742 |
| rep_n_6d5fcf__n100000.scala | 71.3 | 148 | 0.481 | 1540 | 2990 | 0.516 | 41.8 | 45.3 | 0.923 | 41.8 | 45.3 | 0.923 |
| random_bracket_soup_7022a9__n10000.scala | 6.52 | 8.13 | 0.802 | 115 | 119 | 0.961 | 9.6 | 7.58 | 1.27 | 9.61 | 7.58 | 1.27 |
| random_bytes_697b59__n100000.scala | 50.1 | 83.9 | 0.597 | 1220 | 1710 | 0.717 | 15.6 | 14.3 | 1.09 | 15.6 | 14.3 | 1.09 |
| random_printable_cb9992__n100000.scala | 51.2 | 107 | 0.479 | 885 | 1660 | 0.535 | 26.2 | 15 | 1.74 | 26.2 | 15 | 1.74 |
| unterminated_a_266e40__n100000.scala | 18.2 | 29 | 0.628 | 406 | 432 | 0.938 | 36.3 | 55.1 | 0.659 | 36.3 | 55.1 | 0.659 |
| block_if_x_n_412d56__n10000.scala | 31.6 | 39.4 | 0.802 | 724 | 647 | 1.12 | 22.3 | 39.9 | 0.559 | 22.3 | 39.9 | 0.559 |
| broken_stmt_val_x_f_1_n_46f8f5__n3000.scala | 19.4 | 23.5 | 0.828 | 419 | 452 | 0.927 | 13 | 11.1 | 1.17 | 13 | 11.1 | 1.17 |
| pair_b94783__n100000.scala | 17.7 | 31.1 | 0.569 | 376 | 464 | 0.81 | 31.8 | 53.3 | 0.596 | 31.8 | 53.3 | 0.596 |
| glr_valid_val_x_a_b_c_n_af35b2__n10000.scala | 34.4 | 46.1 | 0.746 | 740 | 760 | 0.974 | 40 | 33 | 1.21 | 40 | 33 | 1.21 |

### tsx

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_let_x_T_a_T_a_b_c_d_a_n_c27d91__n100000.tsx | 387 | 614 | 0.631 | 8290 | 10300 | 0.802 | 297 | 319 | 0.929 | 297 | 319 | 0.929 |
| block_namespace_A_n_fa454c__n100000.tsx | 41.4 | 74.6 | 0.555 | 981 | 1370 | 0.717 | 52.9 | 78.7 | 0.673 | 52.9 | 78.7 | 0.673 |
| broken_stmt_function_f_return_1_n_4bd8f0__n100000.tsx | 1550 | 2210 | 0.702 | 37300 | 36700 | 1.02 | 181 | 270 | 0.67 | 181 | 270 | 0.67 |
| rep_045310__n30000.tsx | 4540 | 9660 | 0.47 | 161000 | 242000 | 0.663 | 17.9 | 14.8 | 1.21 | 17.9 | 14.8 | 1.21 |
| unterminated_a_a452e7__n100000.tsx | 11700 | 61300 | 0.191 | 226000 | 653000 | 0.346 | 28.8 | 22.4 | 1.29 | 28.8 | 22.4 | 1.29 |
| random_bracket_soup_7022a9__n1000000.tsx | 1830 | 1970 | 0.928 | 47200 | 31300 | 1.51 | 217 | 176 | 1.23 | 217 | 176 | 1.23 |
| random_bytes_697b59__n1000000.tsx | 227 | 644 | 0.353 | 3750 | 10800 | 0.346 | 155 | 220 | 0.706 | 155 | 220 | 0.706 |
| random_printable_cb9992__n100000.tsx | 31.2 | 99.6 | 0.313 | 496 | 1620 | 0.305 | 11.9 | 9.61 | 1.24 | 11.9 | 9.61 | 1.24 |
| pair_f0f6a0__n100000.tsx | 34.5 | 57.9 | 0.596 | 747 | 778 | 0.96 | 47.8 | 71.9 | 0.664 | 47.8 | 71.9 | 0.664 |
| deep_function_f_n_5e1086__n100000.tsx | 96 | 170 | 0.563 | 2100 | 2690 | 0.781 | 110 | 117 | 0.942 | 110 | 117 | 0.942 |
| block_type_A_n_f29830__n100000.tsx | 56400 | KILL_TIME | n/a | 903000 | KILL_TIME | n/a | 97.6 | KILL_TIME | n/a | 97.6 | KILL_TIME | n/a |

### typescript

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| glr_valid_x_a_b_c_d_e_f_g_h_n_f95d77__n100000.ts | 771 | 1210 | 0.639 | 15800 | 19900 | 0.795 | 393 | 383 | 1.03 | 393 | 383 | 1.03 |
| broken_stmt_function_f_return_1_n_4bd8f0__n100000.ts | 1560 | 2210 | 0.705 | 37300 | 36600 | 1.02 | 181 | 271 | 0.667 | 181 | 271 | 0.667 |
| rep_n_aec841__n30000.ts | 22800 | 46200 | 0.494 | 720000 | 1120000 | 0.641 | 31 | 30.7 | 1.01 | 31 | 30.7 | 1.01 |
| unterminated_a_a452e7__n100000.ts | 11000 | 62500 | 0.175 | 226000 | 653000 | 0.346 | 28.7 | 21.6 | 1.33 | 28.7 | 21.6 | 1.33 |
| random_bracket_soup_7022a9__n1000000.ts | 2090 | 2150 | 0.975 | 54000 | 35600 | 1.52 | 227 | 178 | 1.28 | 227 | 178 | 1.28 |
| random_bytes_697b59__n1000000.ts | 240 | 627 | 0.383 | 3880 | 10400 | 0.374 | 175 | 225 | 0.778 | 175 | 225 | 0.778 |
| random_printable_cb9992__n100000.ts | 34.9 | 116 | 0.301 | 510 | 1780 | 0.286 | 12.3 | 8.25 | 1.49 | 12.3 | 8.25 | 1.49 |
| block_for_n_3794d8__n100000.ts | 85.3 | 177 | 0.481 | 1980 | 2220 | 0.891 | 111 | 170 | 0.655 | 111 | 170 | 0.655 |
| pair_b94783__n100000.ts | 158 | 707 | 0.224 | 3340 | 12400 | 0.269 | 45 | 63.5 | 0.709 | 45 | 63.5 | 0.709 |
| rep_n_e980df__n100000.ts | 12600 | 73300 | 0.172 | 291000 | 1100000 | 0.264 | 24.6 | 20.6 | 1.2 | 24.6 | 20.6 | 1.2 |
| big5mb__big5mb.ts | 427 | 667 | 0.639 | 7820 | 10700 | 0.734 | 335 | 328 | 1.02 | 335 | 328 | 1.02 |
| advdeep_100000__ts_100000.ts | 26.7 | 46.9 | 0.568 | 634 | 843 | 0.752 | 40.5 | 37.8 | 1.07 | 40.5 | 37.8 | 1.07 |

### verilog

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| random_bracket_soup_7022a9__n1000000.v | 401 | 451 | 0.889 | 8850 | 7700 | 1.15 | 167 | 182 | 0.918 | 167 | 182 | 0.918 |
| rep_module_n_afbc83__n100000.v | 5470 | 27100 | 0.202 | 101000 | 284000 | 0.355 | 29.1 | 19.6 | 1.48 | 29.1 | 19.6 | 1.48 |
| broken_stmt_module_m_assign_x_a_endmodul_5e9445__n30000.v | 514 | 1810 | 0.285 | 11600 | 36600 | 0.317 | 84.6 | 82.3 | 1.03 | 84.6 | 82.3 | 1.03 |
| rep_n_aec841__n100000.v | 32000 | 70200 | 0.456 | 1180000 | 2030000 | 0.579 | 54.9 | 53.8 | 1.02 | 54.9 | 53.8 | 1.02 |
| rep_module_n_afbc83__n30000.v | 513 | 2500 | 0.205 | 9360 | 27500 | 0.34 | 12.1 | 8.88 | 1.37 | 12.1 | 8.88 | 1.37 |
| random_bytes_697b59__n1000000.v | 191 | 886 | 0.216 | 3070 | 17400 | 0.176 | 160 | 214 | 0.75 | 160 | 214 | 0.75 |
| random_printable_cb9992__n1000000.v | 353 | 2540 | 0.139 | 5340 | 48500 | 0.11 | 60.1 | 67.4 | 0.892 | 60.1 | 67.4 | 0.892 |
| unterminated_a_7f7cb3__n100000.v | 192 | 849 | 0.226 | 3550 | 22600 | 0.157 | 66.5 | 62.8 | 1.06 | 66.5 | 62.8 | 1.06 |
| block_always_posedge_c_begin_n_11163b__n100000.v | 28000 | KILL_TIME | n/a | 456000 | KILL_TIME | n/a | 200 | KILL_TIME | n/a | 200 | KILL_TIME | n/a |
| pair_ebd0a6__n100000.v | 6380 | 40200 | 0.159 | 132000 | 995000 | 0.132 | 24.3 | 9.4 | 2.59 | 24.4 | 9.4 | 2.59 |
| deep_module_m_initial_if_x_a_1_endmodule_bcb2b5__n100000.v | 162 | 309 | 0.524 | 3470 | 4540 | 0.763 | 133 | 145 | 0.915 | 133 | 145 | 0.915 |

### zig

| file | wall ms gs | wall ms C | wall gs/C | instr M gs | instr M C | instr gs/C | RSS MB gs | RSS MB C | RSS gs/C | footprint MB gs | footprint MB C | footprint gs/C |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| random_bracket_soup_7022a9__n1000000.zig | 1160 | 1650 | 0.703 | 22000 | 26500 | 0.83 | 121 | 148 | 0.817 | 121 | 148 | 0.817 |
| broken_stmt_fn_f_void_return_1_n_7c6715__n100000.zig | 2310 | 2460 | 0.941 | 55500 | 46700 | 1.19 | 170 | 178 | 0.957 | 170 | 178 | 0.957 |
| random_bytes_697b59__n1000000.zig | 430 | 1280 | 0.337 | 8050 | 21000 | 0.383 | 133 | 263 | 0.504 | 133 | 263 | 0.504 |
| random_printable_cb9992__n1000000.zig | 799 | 2100 | 0.38 | 12400 | 34000 | 0.364 | 143 | 148 | 0.964 | 143 | 148 | 0.964 |
| rep_04e9d4__n100000.zig | 20.8 | 34.8 | 0.597 | 520 | 549 | 0.947 | 33.4 | 56.8 | 0.588 | 33.4 | 56.8 | 0.588 |
| unterminated_a_ff4b90__n100000.zig | 11700 | 64300 | 0.181 | 227000 | 653000 | 0.348 | 28.4 | 23.8 | 1.2 | 28.4 | 23.8 | 1.2 |
| block_test_a_n_1b664e__n100000.zig | 390 | 1610 | 0.243 | 8620 | 28900 | 0.298 | 92 | 124 | 0.743 | 92 | 124 | 0.743 |
| pair_b94783__n100000.zig | 21.5 | 38.4 | 0.56 | 523 | 595 | 0.88 | 33.1 | 56.9 | 0.581 | 33.1 | 56.9 | 0.581 |
| deep_const_x_struct_const_y_1_n_5ab93d__n100000.zig | 85 | 185 | 0.46 | 1940 | 2820 | 0.688 | 113 | 118 | 0.957 | 113 | 118 | 0.957 |
| unterminated_c_a_3f6b05__n100000.zig | 12600 | 66200 | 0.19 | 227000 | 653000 | 0.348 | 47.8 | 31.8 | 1.5 | 47.8 | 31.8 | 1.5 |
| 058_trunc.zig | 13900 | 54700 | 0.255 | 296000 | 1220000 | 0.243 | 1480 | 1530 | 0.97 | 1480 | 1530 | 0.97 |

## Cases where gositter is slower or uses more memory

Every ratio above 1.00 in this report, every input that gositter did not finish while the other side did, and every input killed on both sides. A killed process hit the memory or time cap of its run (`wd` in the last table); extreme inputs: time cap 30/120 s, memory cap 4096 MB. For an input killed on both sides the only comparison is the RSS peak the watchdog saw before the kill.

### gositter vs pure C -O2

Matrix and steady-state memory: 1189 of 4011 config-metrics have a ratio above 1.00.

| ratio | grammar | set | what | j | metric | gositter | pure C -O2 | unit |
|---|---|---|---|---|---|---|---|---|
| 6.13 | php | realerr (too small) | cursor minus parse | all | instructions min | 0.0287 | 0.00467 | M |
| 4.5 | zig | broken | cursor minus parse | 1 | CPU user+sys min | 52.5 | 11.7 | ms |
| 4.29 | python | realerr (too small) | parse | all | steady RSS above loaded | 5.54 | 1.29 | MB |
| 4.23 | java | realerr (too small) | parse | all | steady RSS above loaded | 4.23 | 1 | MB |
| 4.18 | javascript | realerr (too small) | parse | all | RSS above loaded after 6 passes | 4.83 | 1.16 | MB |
| 4.17 | java | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.35 | 0.805 | MB |
| 4.06 | csharp | realerr | walk minus parse | all | CPU user+sys min | 15.5 | 3.82 | ms |
| 4.06 | zig | broken | cursor minus parse | 1 | cycles min | 280 | 69 | M |
| 4 | java | realerr (too small) | walk minus parse | 1 | CPU user+sys min | 0.004 | 0.001 | ms |
| 3.8 | lua | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.019 | 0.005 | ms |
| 3.55 | javascript | realerr (too small) | parse | all | steady RSS above loaded | 5.89 | 1.66 | MB |
| 3.54 | php | realerr (too small) | cursor minus parse | 1 | cycles min | 0.00607 | 0.00171 | M |
| 3.5 | python | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.59 | 1.02 | MB |
| 3.47 | lua | realerr (too small) | parse | all | steady RSS above loaded | 2.78 | 0.801 | MB |
| 3.33 | kotlin | broken | cursor | 1 | RSS above loaded after 6 passes | 15.8 | 4.75 | MB |
| 3.31 | lua | realerr (too small) | parse | all | RSS above loaded after 6 passes | 2.55 | 0.77 | MB |
| 3.26 | java | real | cursor | 1 | RSS above loaded after 6 passes | 17.9 | 5.5 | MB |
| 3.23 | lua | real | cursor | 1 | RSS above loaded after 6 passes | 23.5 | 7.27 | MB |
| 3.14 | python | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.022 | 0.007 | ms |
| 3.07 | scala | real | cursor | 1 | RSS above loaded after 6 passes | 38.6 | 12.6 | MB |
| 2.99 | kotlin | real | cursor | 1 | RSS above loaded after 6 passes | 24.1 | 8.06 | MB |
| 2.9 | javascript | realerr (too small) | parse | 1 | steady RSS above loaded | 2.32 | 0.801 | MB |
| 2.85 | java | realerr (too small) | parse | 1 | steady RSS above loaded | 2.01 | 0.707 | MB |
| 2.79 | java | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.77 | 0.633 | MB |
| 2.75 | json | broken | cursor | 1 | RSS above loaded after 6 passes | 84.2 | 30.7 | MB |
| 2.74 | javascript | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.75 | 0.641 | MB |
| 2.73 | ruby | real | cursor | 1 | RSS above loaded after 6 passes | 28.8 | 10.5 | MB |
| 2.71 | lua | broken | cursor | 1 | RSS above loaded after 6 passes | 83.5 | 30.9 | MB |
| 2.61 | php | realerr (too small) | parse | all | steady RSS above loaded | 3.5 | 1.34 | MB |
| 2.6 | ruby | broken | cursor | 1 | RSS above loaded after 6 passes | 213 | 81.7 | MB |
| 2.6 | lua | realerr (too small) | parse | 1 | steady RSS above loaded | 1.28 | 0.492 | MB |
| 2.58 | rust | real | cursor | 1 | RSS above loaded after 6 passes | 25.7 | 9.97 | MB |
| 2.55 | lua | broken | cursor | 1 | peak RSS after 6 passes | 91 | 35.6 | MB |
| 2.55 | lua | broken | cursor | 1 | peak footprint after 6 passes | 91 | 35.6 | MB |
| 2.55 | python | realerr (too small) | parse | 1 | steady RSS above loaded | 1.89 | 0.742 | MB |
| 2.55 | php | realerr (too small) | parse | all | instructions | 0.198 | 0.0774 | M |
| 2.55 | typescript | real | cursor | 1 | RSS above loaded after 6 passes | 30.6 | 12 | MB |
| 2.54 | ruby | broken | cursor | 1 | peak RSS after 6 passes | 220 | 86.7 | MB |
| 2.54 | ruby | broken | cursor | 1 | peak footprint after 6 passes | 220 | 86.7 | MB |
| 2.53 | scala | real | cursor | 1 | peak footprint after 6 passes | 48 | 19 | MB |
| 2.53 | scala | real | cursor | 1 | peak RSS after 6 passes | 48 | 19 | MB |
| 2.52 | golang | broken | cursor | 1 | RSS above loaded after 6 passes | 77.9 | 31 | MB |
| 2.48 | lua | realerr (too small) | walk minus parse | all | cycles min | 0.0528 | 0.0213 | M |
| 2.46 | javascript | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 4.66 | 1.89 | MB |
| 2.46 | php | realerr (too small) | walk | all | instructions | 0.195 | 0.0795 | M |
| 2.44 | java | broken | cursor | 1 | RSS above loaded after 6 passes | 108 | 44.3 | MB |
| 2.42 | bash | real | cursor | 1 | RSS above loaded after 6 passes | 14.7 | 6.06 | MB |
| 2.41 | javascript | realerr (too small) | walk | all | RSS above loaded after 6 passes | 4.66 | 1.93 | MB |
| 2.39 | rust | broken | cursor | 1 | RSS above loaded after 6 passes | 109 | 45.7 | MB |
| 2.39 | typescript | broken | cursor | 1 | RSS above loaded after 6 passes | 109 | 45.9 | MB |
| 2.38 | csharp | realerr | walk minus parse | all | cycles min | 67.3 | 28.2 | M |
| 2.38 | c | broken | cursor | 1 | RSS above loaded after 6 passes | 85.5 | 35.9 | MB |
| 2.38 | json | broken | cursor | 1 | peak RSS after 6 passes | 97 | 40.8 | MB |
| 2.38 | json | broken | cursor | 1 | peak footprint after 6 passes | 97 | 40.8 | MB |
| 2.36 | php | realerr (too small) | cursor | all | instructions | 0.194 | 0.0823 | M |
| 2.35 | java | broken | cursor | 1 | peak RSS after 6 passes | 116 | 49.1 | MB |
| 2.35 | java | broken | cursor | 1 | peak footprint after 6 passes | 116 | 49.1 | MB |
| 2.35 | javascript | real | cursor | 1 | RSS above loaded after 6 passes | 33.9 | 14.4 | MB |
| 2.34 | kotlin | broken | cursor | 1 | peak RSS after 6 passes | 21.5 | 9.18 | MB |
| 2.34 | kotlin | broken | cursor | 1 | peak footprint after 6 passes | 21.5 | 9.18 | MB |
| 2.33 | golang | broken | cursor | 1 | peak RSS after 6 passes | 86.6 | 37.1 | MB |
| 2.33 | golang | broken | cursor | 1 | peak footprint after 6 passes | 86.6 | 37.1 | MB |
| 2.31 | python | real | cursor | 1 | RSS above loaded after 6 passes | 27.2 | 11.8 | MB |
| 2.3 | csharp | real | cursor | 1 | RSS above loaded after 6 passes | 35.8 | 15.6 | MB |
| 2.3 | tsx | real | cursor | 1 | RSS above loaded after 6 passes | 13 | 5.67 | MB |
| 2.28 | bash | broken | cursor | 1 | RSS above loaded after 6 passes | 148 | 64.8 | MB |
| 2.27 | typescript | broken | cursor | 1 | peak RSS after 6 passes | 118 | 51.9 | MB |
| 2.27 | typescript | broken | cursor | 1 | peak footprint after 6 passes | 118 | 51.9 | MB |
| 2.26 | rust | broken | cursor | 1 | peak RSS after 6 passes | 118 | 52.1 | MB |
| 2.26 | rust | broken | cursor | 1 | peak footprint after 6 passes | 118 | 52.1 | MB |
| 2.26 | hcl | broken | parse | 1 | steady RSS above loaded | 17 | 7.53 | MB |
| 2.25 | hcl | real | cursor | 1 | RSS above loaded after 6 passes | 16.8 | 7.45 | MB |
| 2.24 | lua | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.22 | 0.543 | MB |
| 2.23 | javascript | realerr (too small) | parse | all | peak footprint after 6 passes | 8.27 | 3.71 | MB |
| 2.23 | bash | broken | cursor | 1 | peak RSS after 6 passes | 155 | 69.7 | MB |
| 2.23 | bash | broken | cursor | 1 | peak footprint after 6 passes | 155 | 69.7 | MB |
| 2.23 | javascript | realerr (too small) | parse | all | peak RSS after 6 passes | 8.25 | 3.71 | MB |
| 2.22 | javascript | realerr (too small) | parse | all | steady peak footprint | 9.33 | 4.21 | MB |
| 2.22 | javascript | broken | cursor | 1 | RSS above loaded after 6 passes | 149 | 67.4 | MB |
| 2.21 | javascript | realerr (too small) | parse | all | steady peak RSS | 9.3 | 4.21 | MB |
| 2.2 | c | broken | cursor | 1 | peak RSS after 6 passes | 95 | 43.2 | MB |
| 2.2 | c | broken | cursor | 1 | peak footprint after 6 passes | 95 | 43.2 | MB |
| 2.19 | json | real | cursor | 1 | RSS above loaded after 6 passes | 14.6 | 6.66 | MB |
| 2.18 | python | realerr (too small) | parse | all | steady peak RSS | 8.8 | 4.04 | MB |
| 2.18 | python | realerr (too small) | parse | all | steady peak footprint | 8.8 | 4.04 | MB |
| 2.16 | julia | realerr | walk minus parse | all | cycles min | 41.8 | 19.3 | M |
| 2.16 | java | realerr (too small) | parse | all | steady peak footprint | 7.52 | 3.47 | MB |
| 2.16 | python | broken | cursor | 1 | RSS above loaded after 6 passes | 108 | 49.8 | MB |
| 2.16 | java | realerr (too small) | parse | all | steady peak RSS | 7.49 | 3.47 | MB |
| 2.15 | lua | realerr (too small) | walk minus parse | all | instructions min | 0.0149 | 0.00692 | M |
| 2.15 | csharp | broken | cursor | 1 | RSS above loaded after 6 passes | 20.9 | 9.7 | MB |
| 2.15 | kotlin | real | cursor | 1 | peak RSS after 6 passes | 36.3 | 16.9 | MB |
| 2.15 | kotlin | real | cursor | 1 | peak footprint after 6 passes | 36.3 | 16.9 | MB |
| 2.15 | kotlin | real | walk minus parse | all | cycles min | 217 | 101 | M |
| 2.14 | javascript | broken | cursor | 1 | peak RSS after 6 passes | 159 | 74.1 | MB |
| 2.14 | javascript | broken | cursor | 1 | peak footprint after 6 passes | 159 | 74.1 | MB |
| 2.14 | bash | broken | walk minus parse | all | instructions min | 828 | 387 | M |
| 2.14 | zig | realerr | walk minus parse | all | CPU user+sys min | 5.99 | 2.8 | ms |
| 2.14 | php | real | cursor | 1 | RSS above loaded after 6 passes | 50.9 | 23.8 | MB |
| 2.13 | rust | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.42 | 1.6 | MB |
| 2.13 | golang | broken | cursor minus parse | all | CPU user+sys min | 69 | 32.4 | ms |
| 2.13 | golang | real | cursor | 1 | RSS above loaded after 6 passes | 26.4 | 12.4 | MB |
| 2.12 | rust | realerr (too small) | parse | all | steady RSS above loaded | 3.54 | 1.67 | MB |
| 2.11 | tsx | realerr | walk minus parse | all | CPU user+sys min | 2.71 | 1.29 | ms |
| 2.09 | bash | real | cursor | 1 | peak footprint after 6 passes | 22.2 | 10.6 | MB |
| 2.09 | bash | real | cursor | 1 | peak RSS after 6 passes | 22.2 | 10.6 | MB |
| 2.09 | python | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.89 | 0.906 | MB |
| 2.08 | scala | realerr | parse | 1 | steady RSS above loaded | 18.5 | 8.87 | MB |
| 2.07 | tsx | realerr | parse | 1 | RSS above loaded after 6 passes | 5.33 | 2.58 | MB |
| 2.06 | scala | broken | cursor | 1 | RSS above loaded after 6 passes | 24.8 | 12 | MB |
| 2.05 | ruby | real | cursor | 1 | peak RSS after 6 passes | 42.9 | 20.9 | MB |
| 2.05 | ruby | real | cursor | 1 | peak footprint after 6 passes | 42.9 | 20.9 | MB |
| 2.05 | python | broken | cursor | 1 | peak footprint after 6 passes | 118 | 57.4 | MB |
| 2.05 | python | broken | cursor | 1 | peak RSS after 6 passes | 118 | 57.4 | MB |
| 2.04 | scala | realerr | parse | 1 | RSS above loaded after 6 passes | 18.3 | 8.96 | MB |
| 2.04 | kotlin | real | walk minus parse | all | CPU user+sys min | 51.8 | 25.4 | ms |
| 2.03 | scala | realerr | cursor | 1 | RSS above loaded after 6 passes | 19.4 | 9.55 | MB |
| 2.02 | java | real | cursor | 1 | peak footprint after 6 passes | 32.5 | 16.1 | MB |
| 2.02 | java | real | cursor | 1 | peak RSS after 6 passes | 32.5 | 16.1 | MB |
| 2.02 | kotlin | broken | parse | 1 | RSS above loaded after 6 passes | 9.86 | 4.89 | MB |
| 2 | php | realerr (too small) | cursor | 1 | CPU user+sys | 0.018 | 0.009 | ms |
| 1.99 | lua | real | cursor | 1 | peak RSS after 6 passes | 40.2 | 20.2 | MB |
| 1.99 | lua | real | cursor | 1 | peak footprint after 6 passes | 40.2 | 20.2 | MB |
| 1.99 | php | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3 | 1.51 | MB |
| 1.99 | java | realerr (too small) | parse | all | peak footprint after 6 passes | 6.64 | 3.34 | MB |
| 1.98 | java | realerr (too small) | parse | all | peak RSS after 6 passes | 6.61 | 3.34 | MB |
| 1.97 | php | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.13 | 1.59 | MB |
| 1.93 | kotlin | broken | parse | 1 | steady RSS above loaded | 9.77 | 5.05 | MB |
| 1.93 | csharp | broken | cursor | 1 | peak footprint after 6 passes | 28.6 | 14.8 | MB |
| 1.93 | csharp | broken | cursor | 1 | peak RSS after 6 passes | 28.5 | 14.8 | MB |
| 1.93 | csharp | real | cursor | 1 | peak RSS after 6 passes | 49.4 | 25.6 | MB |
| 1.93 | csharp | real | cursor | 1 | peak footprint after 6 passes | 49.4 | 25.6 | MB |
| 1.93 | hcl | broken | parse | 1 | steady peak RSS | 22.3 | 11.6 | MB |
| 1.93 | hcl | broken | parse | 1 | steady peak footprint | 22.3 | 11.6 | MB |
| 1.93 | tsx | realerr | parse | 1 | steady RSS above loaded | 5.36 | 2.78 | MB |
| 1.93 | tsx | real | cursor | 1 | peak RSS after 6 passes | 21 | 10.9 | MB |
| 1.93 | tsx | real | cursor | 1 | peak footprint after 6 passes | 21 | 10.9 | MB |
| 1.92 | bash | broken | walk minus parse | 1 | instructions min | 732 | 381 | M |
| 1.92 | lua | realerr (too small) | parse | all | steady peak footprint | 6.17 | 3.21 | MB |
| 1.92 | scala | realerr | parse | 1 | steady peak RSS | 22.7 | 11.9 | MB |
| 1.92 | scala | realerr | parse | 1 | steady peak footprint | 22.7 | 11.9 | MB |
| 1.91 | lua | realerr (too small) | parse | all | steady peak RSS | 6.16 | 3.21 | MB |
| 1.91 | bash | realerr | parse | 1 | steady RSS above loaded | 10.5 | 5.52 | MB |
| 1.91 | scala | realerr | walk | 1 | RSS above loaded after 6 passes | 18.7 | 9.79 | MB |
| 1.9 | rust | real | cursor | 1 | peak RSS after 6 passes | 40.2 | 21.1 | MB |
| 1.9 | rust | real | cursor | 1 | peak footprint after 6 passes | 40.2 | 21.1 | MB |
| 1.9 | python | realerr (too small) | parse | all | peak footprint after 6 passes | 6.88 | 3.62 | MB |
| 1.9 | scala | broken | cursor | 1 | peak footprint after 6 passes | 31.4 | 16.5 | MB |
| 1.9 | scala | broken | cursor | 1 | peak RSS after 6 passes | 31.4 | 16.5 | MB |
| 1.89 | python | realerr (too small) | parse | all | peak RSS after 6 passes | 6.85 | 3.62 | MB |
| 1.89 | scala | realerr | cursor | 1 | peak RSS after 6 passes | 23.6 | 12.5 | MB |
| 1.89 | scala | realerr | cursor | 1 | peak footprint after 6 passes | 23.6 | 12.5 | MB |
| 1.89 | typescript | realerr | parse | 1 | steady RSS above loaded | 7.63 | 4.05 | MB |
| 1.88 | bash | realerr | cursor | 1 | RSS above loaded after 6 passes | 10.4 | 5.54 | MB |
| 1.88 | zig | broken | cursor | 1 | RSS above loaded after 6 passes | 18.1 | 9.64 | MB |
| 1.88 | typescript | real | cursor | 1 | peak RSS after 6 passes | 48 | 25.5 | MB |
| 1.88 | typescript | real | cursor | 1 | peak footprint after 6 passes | 48 | 25.5 | MB |
| 1.88 | hcl | real | cursor | 1 | peak RSS after 6 passes | 26.9 | 14.3 | MB |
| 1.88 | hcl | real | cursor | 1 | peak footprint after 6 passes | 26.9 | 14.3 | MB |
| 1.88 | php | real | cursor | 1 | peak RSS after 6 passes | 66.1 | 35.2 | MB |
| 1.88 | php | real | cursor | 1 | peak footprint after 6 passes | 66.1 | 35.2 | MB |
| 1.87 | bash | realerr | walk | 1 | RSS above loaded after 6 passes | 10.2 | 5.47 | MB |
| 1.87 | lua | realerr (too small) | walk minus parse | 1 | cycles min | 0.00259 | 0.00139 | M |
| 1.86 | bash | real | parse | 1 | steady RSS above loaded | 10.5 | 5.64 | MB |
| 1.86 | scala | real | parse | 1 | steady RSS above loaded | 22.5 | 12.1 | MB |
| 1.85 | scala | realerr | parse | 1 | peak RSS after 6 passes | 22.5 | 12.1 | MB |
| 1.85 | scala | realerr | parse | 1 | peak footprint after 6 passes | 22.5 | 12.1 | MB |
| 1.84 | bash | realerr | parse | 1 | RSS above loaded after 6 passes | 10.2 | 5.55 | MB |
| 1.84 | c | real | cursor | 1 | RSS above loaded after 6 passes | 17.7 | 9.65 | MB |
| 1.83 | zig | broken | walk minus parse | 1 | CPU user+sys min | 117 | 63.8 | ms |
| 1.82 | lua | broken | walk | 1 | RSS above loaded after 6 passes | 65 | 35.7 | MB |
| 1.82 | javascript | real | cursor | 1 | peak RSS after 6 passes | 51.4 | 28.2 | MB |
| 1.82 | javascript | real | cursor | 1 | peak footprint after 6 passes | 51.4 | 28.2 | MB |
| 1.81 | hcl | real | walk minus parse | all | CPU user+sys min | 57.8 | 31.9 | ms |
| 1.81 | javascript | realerr (too small) | cursor | all | peak footprint after 6 passes | 7.97 | 4.41 | MB |
| 1.81 | kotlin | broken | walk | 1 | RSS above loaded after 6 passes | 9.45 | 5.23 | MB |
| 1.8 | javascript | realerr (too small) | cursor | all | peak RSS after 6 passes | 7.95 | 4.41 | MB |
| 1.8 | javascript | realerr (too small) | walk | all | peak footprint after 6 passes | 8.11 | 4.51 | MB |
| 1.8 | php | realerr (too small) | parse | all | steady peak RSS | 6.86 | 3.81 | MB |
| 1.8 | php | realerr (too small) | parse | all | steady peak footprint | 6.86 | 3.81 | MB |
| 1.8 | c | broken | walk | 1 | RSS above loaded after 6 passes | 62.2 | 34.6 | MB |
| 1.79 | zig | broken | walk minus parse | 1 | cycles min | 615 | 343 | M |
| 1.79 | javascript | realerr (too small) | walk | all | peak RSS after 6 passes | 8.09 | 4.51 | MB |
| 1.78 | kotlin | realerr | cursor | 1 | RSS above loaded after 6 passes | 9.8 | 5.5 | MB |
| 1.78 | scala | real | parse | 1 | RSS above loaded after 6 passes | 21.3 | 12 | MB |
| 1.78 | scala | broken | parse | 1 | RSS above loaded after 6 passes | 19.4 | 10.9 | MB |
| 1.77 | lua | broken | walk | 1 | peak footprint after 6 passes | 71.7 | 40.5 | MB |
| 1.77 | lua | broken | walk | 1 | peak RSS after 6 passes | 71.7 | 40.5 | MB |
| 1.77 | bash | real | parse | 1 | steady peak footprint | 18 | 10.2 | MB |
| 1.77 | bash | real | parse | 1 | steady peak RSS | 18 | 10.2 | MB |
| 1.76 | cpp | real | cursor | 1 | RSS above loaded after 6 passes | 19.2 | 10.9 | MB |
| 1.76 | golang | broken | walk | 1 | RSS above loaded after 6 passes | 62.7 | 35.6 | MB |
| 1.76 | php | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.15 | 1.79 | MB |
| 1.76 | kotlin | broken | cursor | all | RSS above loaded after 6 passes | 29.7 | 16.9 | MB |
| 1.75 | java | realerr (too small) | walk minus parse | 1 | cycles min | 0.0139 | 0.00792 | M |
| 1.75 | cpp | realerr | cursor | 1 | RSS above loaded after 6 passes | 28.1 | 16 | MB |
| 1.75 | lua | realerr (too small) | parse | all | peak footprint after 6 passes | 5.84 | 3.33 | MB |
| 1.75 | kotlin | realerr | parse | 1 | steady RSS above loaded | 9.46 | 5.42 | MB |
| 1.75 | scala | realerr | walk | 1 | peak footprint after 6 passes | 22.7 | 13 | MB |
| 1.74 | scala | realerr | walk | 1 | peak RSS after 6 passes | 22.7 | 13 | MB |
| 1.74 | lua | realerr (too small) | parse | all | peak RSS after 6 passes | 5.81 | 3.33 | MB |
| 1.74 | javascript | realerr (too small) | parse | 1 | steady peak footprint | 5.65 | 3.24 | MB |
| 1.74 | lua | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 2.66 | 1.53 | MB |
| 1.74 | kotlin | broken | cursor | all | peak footprint after 6 passes | 36.8 | 21.1 | MB |
| 1.74 | javascript | realerr (too small) | parse | 1 | steady peak RSS | 5.64 | 3.24 | MB |
| 1.74 | typescript | realerr | parse | 1 | steady peak footprint | 11.3 | 6.47 | MB |
| 1.74 | python | real | cursor | 1 | peak RSS after 6 passes | 44.3 | 25.5 | MB |
| 1.74 | python | real | cursor | 1 | peak footprint after 6 passes | 44.3 | 25.5 | MB |
| 1.74 | kotlin | broken | cursor | all | peak RSS after 6 passes | 36.8 | 21.1 | MB |
| 1.74 | scala | broken | parse | 1 | steady RSS above loaded | 19.3 | 11.1 | MB |
| 1.74 | typescript | realerr | parse | 1 | steady peak RSS | 11.2 | 6.47 | MB |
| 1.74 | cpp | broken | cursor | 1 | RSS above loaded after 6 passes | 232 | 133 | MB |
| 1.73 | typescript | realerr | parse | 1 | RSS above loaded after 6 passes | 7.06 | 4.09 | MB |
| 1.73 | kotlin | realerr | walk | 1 | RSS above loaded after 6 passes | 9.37 | 5.42 | MB |
| 1.73 | c | realerr | cursor | 1 | RSS above loaded after 6 passes | 21.7 | 12.6 | MB |
| 1.72 | scala | real | parse | 1 | steady peak footprint | 31.7 | 18.4 | MB |
| 1.72 | scala | real | parse | 1 | steady peak RSS | 31.7 | 18.4 | MB |
| 1.72 | json | broken | walk | 1 | RSS above loaded after 6 passes | 60.5 | 35.2 | MB |
| 1.71 | c | broken | walk | 1 | peak footprint after 6 passes | 71.7 | 41.9 | MB |
| 1.71 | bash | realerr | cursor | 1 | peak footprint after 6 passes | 14.5 | 8.48 | MB |
| 1.71 | c | broken | walk | 1 | peak RSS after 6 passes | 71.7 | 41.9 | MB |
| 1.71 | bash | realerr | cursor | 1 | peak RSS after 6 passes | 14.5 | 8.48 | MB |
| 1.71 | php | realerr (too small) | cursor minus parse | all | cycles min | 0.0429 | 0.0251 | M |
| 1.71 | bash | realerr | parse | 1 | steady peak RSS | 14.5 | 8.48 | MB |
| 1.71 | bash | realerr | parse | 1 | steady peak footprint | 14.5 | 8.48 | MB |
| 1.71 | cpp | broken | cursor | 1 | peak RSS after 6 passes | 244 | 143 | MB |
| 1.71 | cpp | broken | cursor | 1 | peak footprint after 6 passes | 244 | 143 | MB |
| 1.7 | golang | broken | cursor minus parse | all | cycles min | 295 | 173 | M |
| 1.7 | golang | broken | walk | 1 | peak RSS after 6 passes | 71.4 | 41.9 | MB |
| 1.7 | golang | broken | walk | 1 | peak footprint after 6 passes | 71.4 | 41.9 | MB |
| 1.7 | bash | real | parse | 1 | RSS above loaded after 6 passes | 10.4 | 6.13 | MB |
| 1.7 | scala | real | walk | 1 | RSS above loaded after 6 passes | 21.3 | 12.5 | MB |
| 1.7 | java | realerr (too small) | parse | 1 | peak footprint after 6 passes | 5.02 | 2.95 | MB |
| 1.7 | java | realerr (too small) | parse | 1 | peak RSS after 6 passes | 5.02 | 2.95 | MB |
| 1.7 | cpp | real | cursor | 1 | peak footprint after 6 passes | 26.8 | 15.8 | MB |
| 1.7 | bash | real | parse | 1 | peak footprint after 6 passes | 18.1 | 10.7 | MB |
| 1.7 | bash | real | parse | 1 | peak RSS after 6 passes | 18.1 | 10.7 | MB |
| 1.7 | cpp | real | cursor | 1 | peak RSS after 6 passes | 26.8 | 15.8 | MB |
| 1.7 | c | real | cursor | 1 | peak RSS after 6 passes | 25.9 | 15.3 | MB |
| 1.7 | c | real | cursor | 1 | peak footprint after 6 passes | 25.9 | 15.3 | MB |
| 1.7 | kotlin | realerr | parse | 1 | RSS above loaded after 6 passes | 9.37 | 5.53 | MB |
| 1.69 | bash | real | walk | 1 | RSS above loaded after 6 passes | 10.3 | 6.09 | MB |
| 1.69 | bash | realerr | walk | 1 | peak footprint after 6 passes | 14.2 | 8.39 | MB |
| 1.69 | bash | realerr | walk | 1 | peak RSS after 6 passes | 14.2 | 8.39 | MB |
| 1.69 | tsx | realerr | parse | 1 | peak footprint after 6 passes | 9.2 | 5.45 | MB |
| 1.69 | tsx | realerr | parse | 1 | peak RSS after 6 passes | 9.19 | 5.45 | MB |
| 1.68 | golang | real | cursor | 1 | peak RSS after 6 passes | 43.4 | 25.8 | MB |
| 1.68 | golang | real | cursor | 1 | peak footprint after 6 passes | 43.4 | 25.8 | MB |
| 1.68 | python | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3.41 | 2.03 | MB |
| 1.68 | lua | broken | walk minus parse | all | instructions min | 629 | 375 | M |
| 1.68 | bash | realerr | parse | 1 | peak footprint after 6 passes | 14.3 | 8.52 | MB |
| 1.68 | scala | real | parse | 1 | peak footprint after 6 passes | 30.9 | 18.4 | MB |
| 1.68 | scala | real | parse | 1 | peak RSS after 6 passes | 30.9 | 18.4 | MB |
| 1.68 | bash | realerr | parse | 1 | peak RSS after 6 passes | 14.3 | 8.52 | MB |
| 1.68 | kotlin | broken | parse | 1 | steady peak footprint | 15.5 | 9.23 | MB |
| 1.67 | kotlin | broken | parse | 1 | steady peak RSS | 15.5 | 9.23 | MB |
| 1.67 | bash | real | walk | 1 | peak footprint after 6 passes | 17.8 | 10.7 | MB |
| 1.67 | php | realerr (too small) | walk | all | cycles | 0.213 | 0.127 | M |
| 1.67 | bash | real | walk | 1 | peak RSS after 6 passes | 17.8 | 10.7 | MB |
| 1.67 | bash | realerr | cursor minus parse | all | CPU user+sys min | 4.95 | 2.96 | ms |
| 1.67 | rust | realerr (too small) | parse | all | steady peak footprint | 6.83 | 4.09 | MB |
| 1.67 | tsx | realerr | parse | 1 | steady peak footprint | 9.26 | 5.55 | MB |
| 1.67 | php | realerr (too small) | parse | 1 | CPU user+sys | 0.015 | 0.009 | ms |
| 1.66 | tsx | realerr | parse | 1 | steady peak RSS | 9.25 | 5.55 | MB |
| 1.66 | rust | realerr (too small) | parse | all | steady peak RSS | 6.8 | 4.09 | MB |
| 1.66 | javascript | realerr (too small) | parse | 1 | peak footprint after 6 passes | 5.07 | 3.05 | MB |
| 1.66 | kotlin | broken | parse | 1 | peak footprint after 6 passes | 15.6 | 9.37 | MB |
| 1.66 | javascript | realerr (too small) | parse | 1 | peak RSS after 6 passes | 5.06 | 3.05 | MB |
| 1.66 | kotlin | broken | parse | 1 | peak RSS after 6 passes | 15.5 | 9.37 | MB |
| 1.66 | scala | broken | parse | 1 | peak RSS after 6 passes | 26.1 | 15.7 | MB |
| 1.66 | scala | broken | parse | 1 | peak footprint after 6 passes | 26.1 | 15.7 | MB |
| 1.65 | php | realerr (too small) | cursor | 1 | cycles | 0.0424 | 0.0257 | M |
| 1.65 | scala | broken | parse | 1 | steady peak footprint | 26 | 15.7 | MB |
| 1.65 | scala | broken | parse | 1 | steady peak RSS | 25.9 | 15.7 | MB |
| 1.65 | csharp | realerr | cursor | 1 | RSS above loaded after 6 passes | 18.7 | 11.4 | MB |
| 1.64 | tsx | realerr | parse | all | RSS above loaded after 6 passes | 14.3 | 8.71 | MB |
| 1.64 | python | broken | walk | 1 | RSS above loaded after 6 passes | 76 | 46.4 | MB |
| 1.64 | ruby | real | parse | 1 | steady RSS above loaded | 17.2 | 10.5 | MB |
| 1.64 | lua | broken | parse | 1 | RSS above loaded after 6 passes | 38.8 | 23.7 | MB |
| 1.63 | rust | realerr (too small) | walk minus parse | all | instructions min | 0.0937 | 0.0574 | M |
| 1.63 | tsx | real | parse | 1 | steady RSS above loaded | 7.05 | 4.32 | MB |
| 1.62 | verilog | broken | cursor | 1 | RSS above loaded after 6 passes | 41.7 | 25.7 | MB |
| 1.62 | cpp | real | walk minus parse | all | cycles min | 138 | 85.2 | M |
| 1.62 | java | realerr (too small) | parse | 1 | steady peak footprint | 5.17 | 3.18 | MB |
| 1.62 | java | realerr (too small) | parse | 1 | steady peak RSS | 5.16 | 3.18 | MB |
| 1.62 | scala | real | walk | 1 | peak footprint after 6 passes | 30.7 | 18.9 | MB |
| 1.62 | scala | real | walk | 1 | peak RSS after 6 passes | 30.7 | 18.9 | MB |
| 1.62 | json | broken | walk | 1 | peak RSS after 6 passes | 73.4 | 45.3 | MB |
| 1.62 | json | broken | walk | 1 | peak footprint after 6 passes | 73.4 | 45.3 | MB |
| 1.62 | tsx | realerr | parse | all | peak footprint after 6 passes | 18.9 | 11.7 | MB |
| 1.62 | tsx | realerr | parse | all | peak RSS after 6 passes | 18.9 | 11.7 | MB |
| 1.62 | hcl | broken | cursor | 1 | RSS above loaded after 6 passes | 14.1 | 8.72 | MB |
| 1.62 | lua | broken | walk minus parse | 1 | instructions min | 603 | 373 | M |
| 1.61 | kotlin | realerr | parse | 1 | steady peak footprint | 13.4 | 8.27 | MB |
| 1.61 | zig | real | cursor | 1 | RSS above loaded after 6 passes | 29.3 | 18.1 | MB |
| 1.61 | kotlin | realerr | parse | 1 | steady peak RSS | 13.3 | 8.27 | MB |
| 1.61 | lua | broken | parse | 1 | steady RSS above loaded | 38.4 | 23.9 | MB |
| 1.61 | python | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.48 | 2.16 | MB |
| 1.61 | julia | broken | cursor | 1 | RSS above loaded after 6 passes | 30.8 | 19.2 | MB |
| 1.61 | tsx | realerr | cursor | all | RSS above loaded after 6 passes | 15.4 | 9.57 | MB |
| 1.61 | rust | realerr (too small) | parse | all | peak footprint after 6 passes | 6.7 | 4.18 | MB |
| 1.6 | verilog | real | walk minus parse | all | CPU user+sys min | 7.16 | 4.46 | ms |
| 1.6 | julia | real | cursor | 1 | RSS above loaded after 6 passes | 27.6 | 17.2 | MB |
| 1.6 | java | broken | walk minus parse | all | instructions min | 570 | 355 | M |
| 1.6 | php | realerr (too small) | walk | 1 | CPU user+sys | 0.016 | 0.01 | ms |
| 1.6 | python | broken | walk | 1 | peak footprint after 6 passes | 85.9 | 53.7 | MB |
| 1.6 | python | broken | walk | 1 | peak RSS after 6 passes | 85.9 | 53.7 | MB |
| 1.6 | kotlin | broken | walk | 1 | peak RSS after 6 passes | 15.1 | 9.47 | MB |
| 1.6 | kotlin | broken | walk | 1 | peak footprint after 6 passes | 15.1 | 9.47 | MB |
| 1.6 | rust | realerr (too small) | parse | all | peak RSS after 6 passes | 6.68 | 4.18 | MB |
| 1.6 | kotlin | realerr | cursor | 1 | peak footprint after 6 passes | 13.6 | 8.48 | MB |
| 1.6 | kotlin | realerr | cursor | 1 | peak RSS after 6 passes | 13.5 | 8.48 | MB |
| 1.6 | java | broken | walk | 1 | RSS above loaded after 6 passes | 73.3 | 45.9 | MB |
| 1.6 | lua | realerr (too small) | parse | 1 | peak footprint after 6 passes | 4.48 | 2.81 | MB |
| 1.6 | python | realerr (too small) | parse | 1 | steady peak footprint | 5.05 | 3.17 | MB |
| 1.6 | lua | realerr (too small) | parse | 1 | peak RSS after 6 passes | 4.48 | 2.81 | MB |
| 1.59 | python | realerr (too small) | parse | 1 | steady peak RSS | 5.05 | 3.17 | MB |
| 1.59 | lua | broken | parse | 1 | peak RSS after 6 passes | 45.4 | 28.5 | MB |
| 1.59 | lua | broken | parse | 1 | peak footprint after 6 passes | 45.4 | 28.5 | MB |
| 1.59 | lua | realerr (too small) | walk | all | RSS above loaded after 6 passes | 2.65 | 1.66 | MB |
| 1.59 | typescript | realerr | parse | 1 | peak footprint after 6 passes | 10.5 | 6.62 | MB |
| 1.59 | tsx | realerr | cursor | all | peak footprint after 6 passes | 20.2 | 12.7 | MB |
| 1.59 | tsx | realerr | cursor | all | peak RSS after 6 passes | 20.2 | 12.7 | MB |
| 1.59 | typescript | realerr | parse | 1 | peak RSS after 6 passes | 10.5 | 6.62 | MB |
| 1.59 | java | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3.12 | 1.96 | MB |
| 1.59 | java | broken | walk | 1 | peak RSS after 6 passes | 80.5 | 50.7 | MB |
| 1.59 | java | broken | walk | 1 | peak footprint after 6 passes | 80.5 | 50.7 | MB |
| 1.58 | php | broken | cursor | 1 | RSS above loaded after 6 passes | 38.9 | 24.6 | MB |
| 1.58 | scala | broken | walk | 1 | RSS above loaded after 6 passes | 18.9 | 11.9 | MB |
| 1.58 | lua | broken | parse | 1 | steady peak RSS | 45.3 | 28.6 | MB |
| 1.58 | lua | broken | parse | 1 | steady peak footprint | 45.3 | 28.6 | MB |
| 1.58 | java | broken | walk minus parse | 1 | instructions min | 544 | 344 | M |
| 1.58 | json | realerr | parse | all | peak footprint after 6 passes | 12.1 | 7.66 | MB |
| 1.58 | json | realerr | parse | all | peak RSS after 6 passes | 12.1 | 7.66 | MB |
| 1.58 | tsx | real | parse | 1 | steady peak RSS | 15 | 9.48 | MB |
| 1.58 | tsx | real | parse | 1 | steady peak footprint | 15 | 9.48 | MB |
| 1.58 | verilog | broken | cursor | 1 | peak footprint after 6 passes | 50.8 | 32.2 | MB |
| 1.57 | verilog | broken | cursor | 1 | peak RSS after 6 passes | 50.8 | 32.2 | MB |
| 1.57 | zig | broken | cursor | 1 | peak footprint after 6 passes | 31.3 | 19.9 | MB |
| 1.57 | zig | broken | cursor | 1 | peak RSS after 6 passes | 31.3 | 19.9 | MB |
| 1.57 | kotlin | realerr | parse | 1 | peak footprint after 6 passes | 13.2 | 8.37 | MB |
| 1.57 | json | realerr | parse | 1 | steady RSS above loaded | 1.39 | 0.887 | MB |
| 1.57 | csharp | broken | parse | 1 | steady RSS above loaded | 14.8 | 9.41 | MB |
| 1.57 | kotlin | realerr | parse | 1 | peak RSS after 6 passes | 13.1 | 8.37 | MB |
| 1.57 | tsx | realerr | cursor | 1 | RSS above loaded after 6 passes | 5.67 | 3.62 | MB |
| 1.57 | kotlin | realerr | walk | 1 | peak footprint after 6 passes | 13.1 | 8.37 | MB |
| 1.56 | kotlin | realerr | walk | 1 | peak RSS after 6 passes | 13.1 | 8.37 | MB |
| 1.56 | bash | broken | walk | 1 | RSS above loaded after 6 passes | 119 | 76.5 | MB |
| 1.56 | bash | broken | walk | 1 | peak RSS after 6 passes | 127 | 81.4 | MB |
| 1.56 | bash | broken | walk | 1 | peak footprint after 6 passes | 127 | 81.4 | MB |
| 1.56 | kotlin | broken | walk | all | peak footprint after 6 passes | 31.7 | 20.4 | MB |
| 1.56 | kotlin | broken | walk | all | peak RSS after 6 passes | 31.7 | 20.4 | MB |
| 1.56 | verilog | realerr | cursor | 1 | RSS above loaded after 6 passes | 41.4 | 26.6 | MB |
| 1.55 | php | broken | cursor | 1 | peak footprint after 6 passes | 47.6 | 30.7 | MB |
| 1.55 | php | realerr (too small) | parse | 1 | steady RSS above loaded | 1.96 | 1.27 | MB |
| 1.55 | php | broken | cursor | 1 | peak RSS after 6 passes | 47.6 | 30.7 | MB |
| 1.55 | c | broken | walk minus parse | all | instructions min | 1120 | 724 | M |
| 1.55 | scala | broken | walk | 1 | peak RSS after 6 passes | 25.6 | 16.5 | MB |
| 1.55 | scala | broken | walk | 1 | peak footprint after 6 passes | 25.6 | 16.5 | MB |
| 1.55 | php | realerr (too small) | parse | all | peak footprint after 6 passes | 6.42 | 4.14 | MB |
| 1.55 | bash | broken | walk minus parse | all | cycles min | 283 | 183 | M |
| 1.55 | tsx | real | parse | 1 | RSS above loaded after 6 passes | 7.02 | 4.54 | MB |
| 1.54 | kotlin | broken | walk | all | RSS above loaded after 6 passes | 24.7 | 16 | MB |
| 1.54 | csharp | broken | parse | 1 | steady peak footprint | 22.5 | 14.6 | MB |
| 1.54 | php | realerr (too small) | parse | all | peak RSS after 6 passes | 6.39 | 4.14 | MB |
| 1.54 | julia | broken | cursor | 1 | peak footprint after 6 passes | 40.2 | 26.1 | MB |
| 1.54 | julia | broken | cursor | 1 | peak RSS after 6 passes | 40.2 | 26.1 | MB |
| 1.54 | csharp | broken | parse | 1 | steady peak RSS | 22.5 | 14.6 | MB |
| 1.54 | java | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.21 | 2.08 | MB |
| 1.54 | cpp | realerr | cursor | 1 | peak footprint after 6 passes | 42.6 | 27.6 | MB |
| 1.54 | cpp | realerr | cursor | 1 | peak RSS after 6 passes | 42.6 | 27.6 | MB |
| 1.54 | csharp | realerr | cursor | 1 | peak footprint after 6 passes | 23.9 | 15.5 | MB |
| 1.54 | csharp | realerr | cursor | 1 | peak RSS after 6 passes | 23.9 | 15.5 | MB |
| 1.54 | typescript | broken | walk | 1 | RSS above loaded after 6 passes | 73 | 47.4 | MB |
| 1.54 | tsx | realerr | parse | all | steady peak footprint | 23.2 | 15.1 | MB |
| 1.54 | tsx | realerr | walk | all | peak RSS after 6 passes | 19.4 | 12.6 | MB |
| 1.54 | tsx | realerr | walk | all | peak footprint after 6 passes | 19.4 | 12.6 | MB |
| 1.54 | tsx | realerr | parse | all | steady peak RSS | 23.2 | 15.1 | MB |
| 1.54 | csharp | broken | parse | 1 | RSS above loaded after 6 passes | 14.9 | 9.73 | MB |
| 1.54 | hcl | broken | cursor | 1 | peak footprint after 6 passes | 19.4 | 12.6 | MB |
| 1.53 | hcl | broken | cursor | 1 | peak RSS after 6 passes | 19.4 | 12.6 | MB |
| 1.53 | csharp | broken | walk | 1 | peak footprint after 6 passes | 22.6 | 14.8 | MB |
| 1.53 | csharp | broken | parse | 1 | peak footprint after 6 passes | 22.7 | 14.8 | MB |
| 1.53 | csharp | broken | walk | 1 | RSS above loaded after 6 passes | 15.1 | 9.82 | MB |
| 1.53 | rust | broken | walk | 1 | RSS above loaded after 6 passes | 72.5 | 47.3 | MB |
| 1.53 | csharp | broken | walk | 1 | peak RSS after 6 passes | 22.6 | 14.8 | MB |
| 1.53 | c | realerr | cursor | 1 | peak footprint after 6 passes | 34.3 | 22.4 | MB |
| 1.53 | csharp | broken | parse | 1 | peak RSS after 6 passes | 22.6 | 14.8 | MB |
| 1.53 | tsx | real | parse | 1 | peak RSS after 6 passes | 15 | 9.78 | MB |
| 1.53 | tsx | real | parse | 1 | peak footprint after 6 passes | 15 | 9.78 | MB |
| 1.53 | c | realerr | cursor | 1 | peak RSS after 6 passes | 34.3 | 22.4 | MB |
| 1.53 | java | real | parse | 1 | steady RSS above loaded | 6.38 | 4.18 | MB |
| 1.53 | julia | real | cursor | 1 | peak footprint after 6 passes | 36.6 | 24 | MB |
| 1.52 | typescript | broken | walk | 1 | peak footprint after 6 passes | 81.5 | 53.4 | MB |
| 1.52 | typescript | broken | walk | 1 | peak RSS after 6 passes | 81.5 | 53.4 | MB |
| 1.52 | julia | real | cursor | 1 | peak RSS after 6 passes | 36.6 | 24 | MB |
| 1.52 | rust | realerr (too small) | parse | 1 | steady RSS above loaded | 1.97 | 1.3 | MB |
| 1.52 | verilog | realerr | cursor | 1 | peak footprint after 6 passes | 51.5 | 33.9 | MB |
| 1.52 | rust | broken | walk | 1 | peak footprint after 6 passes | 81.5 | 53.7 | MB |
| 1.52 | rust | broken | walk | 1 | peak RSS after 6 passes | 81.5 | 53.7 | MB |
| 1.52 | verilog | realerr | cursor | 1 | peak RSS after 6 passes | 51.4 | 33.9 | MB |
| 1.52 | json | real | cursor | 1 | peak footprint after 6 passes | 31.3 | 20.6 | MB |
| 1.52 | golang | broken | walk minus parse | all | instructions min | 840 | 554 | M |
| 1.52 | json | real | cursor | 1 | peak RSS after 6 passes | 31.3 | 20.6 | MB |
| 1.51 | tsx | realerr | walk | all | RSS above loaded after 6 passes | 14.5 | 9.64 | MB |
| 1.51 | php | realerr (too small) | walk | all | peak footprint after 6 passes | 6.28 | 4.17 | MB |
| 1.51 | javascript | broken | walk | 1 | RSS above loaded after 6 passes | 97.7 | 64.8 | MB |
| 1.5 | json | realerr | parse | 1 | RSS above loaded after 6 passes | 1.36 | 0.906 | MB |
| 1.5 | php | realerr (too small) | walk | all | peak RSS after 6 passes | 6.25 | 4.17 | MB |
| 1.5 | tsx | realerr | parse | all | steady RSS above loaded | 18.2 | 12.2 | MB |
| 1.5 | php | realerr (too small) | cursor | all | peak footprint after 6 passes | 6.44 | 4.3 | MB |
| 1.49 | kotlin | broken | parse | all | peak RSS after 6 passes | 31.6 | 21.1 | MB |
| 1.49 | kotlin | broken | parse | all | peak footprint after 6 passes | 31.6 | 21.1 | MB |
| 1.49 | zig | real | walk minus parse | all | CPU user+sys min | 134 | 89.7 | ms |
| 1.49 | bash | broken | walk minus parse | all | CPU user+sys min | 60.7 | 40.6 | ms |
| 1.49 | php | realerr (too small) | cursor | all | peak RSS after 6 passes | 6.41 | 4.3 | MB |
| 1.49 | javascript | broken | walk | 1 | peak RSS after 6 passes | 107 | 71.6 | MB |
| 1.49 | javascript | broken | walk | 1 | peak footprint after 6 passes | 107 | 71.6 | MB |
| 1.49 | ruby | real | parse | 1 | steady peak footprint | 31.3 | 21 | MB |
| 1.49 | ruby | real | parse | 1 | steady peak RSS | 31.2 | 21 | MB |
| 1.48 | kotlin | real | parse | 1 | steady RSS above loaded | 12.5 | 8.4 | MB |
| 1.48 | typescript | realerr | cursor | 1 | RSS above loaded after 6 passes | 7.63 | 5.14 | MB |
| 1.48 | typescript | realerr | walk | 1 | RSS above loaded after 6 passes | 7.48 | 5.04 | MB |
| 1.48 | tsx | realerr | cursor | 1 | peak footprint after 6 passes | 9.5 | 6.41 | MB |
| 1.48 | tsx | realerr | cursor | 1 | peak RSS after 6 passes | 9.48 | 6.41 | MB |
| 1.47 | csharp | realerr | parse | 1 | steady RSS above loaded | 16.3 | 11.1 | MB |
| 1.47 | kotlin | real | parse | 1 | RSS above loaded after 6 passes | 12.4 | 8.43 | MB |
| 1.47 | php | realerr (too small) | parse | 1 | cycles | 0.0356 | 0.0242 | M |
| 1.47 | lua | realerr (too small) | cursor | all | peak footprint after 6 passes | 5.96 | 4.05 | MB |
| 1.47 | php | realerr (too small) | cursor | all | cycles | 0.215 | 0.146 | M |
| 1.47 | python | realerr (too small) | walk | all | peak footprint after 6 passes | 6.71 | 4.56 | MB |
| 1.47 | lua | realerr (too small) | walk minus parse | 1 | instructions min | 0.0102 | 0.00694 | M |
| 1.47 | hcl | real | walk minus parse | all | cycles min | 246 | 167 | M |
| 1.47 | typescript | realerr | walk | 1 | peak footprint after 6 passes | 11.1 | 7.56 | MB |
| 1.46 | lua | realerr (too small) | cursor | all | peak RSS after 6 passes | 5.94 | 4.05 | MB |
| 1.46 | python | realerr (too small) | walk | all | peak RSS after 6 passes | 6.68 | 4.56 | MB |
| 1.46 | kotlin | broken | parse | all | RSS above loaded after 6 passes | 24.3 | 16.6 | MB |
| 1.46 | json | realerr | parse | all | RSS above loaded after 6 passes | 7.44 | 5.09 | MB |
| 1.46 | lua | realerr (too small) | walk | all | peak footprint after 6 passes | 5.93 | 4.05 | MB |
| 1.46 | typescript | realerr | walk | 1 | peak RSS after 6 passes | 11.1 | 7.56 | MB |
| 1.46 | lua | realerr (too small) | walk | all | peak RSS after 6 passes | 5.92 | 4.05 | MB |
| 1.46 | java | real | parse | 1 | RSS above loaded after 6 passes | 6.27 | 4.3 | MB |
| 1.46 | php | realerr (too small) | parse | 1 | steady peak footprint | 5.12 | 3.52 | MB |
| 1.46 | lua | realerr (too small) | parse | 1 | steady peak footprint | 4.44 | 3.05 | MB |
| 1.46 | php | realerr (too small) | parse | 1 | steady peak RSS | 5.12 | 3.52 | MB |
| 1.45 | java | realerr (too small) | walk | all | peak footprint after 6 passes | 6.41 | 4.41 | MB |
| 1.45 | lua | realerr (too small) | parse | 1 | steady peak RSS | 4.44 | 3.05 | MB |
| 1.45 | golang | broken | walk minus parse | 1 | instructions min | 813 | 559 | M |
| 1.45 | zig | real | cursor | 1 | peak footprint after 6 passes | 44.2 | 30.4 | MB |
| 1.45 | zig | real | cursor | 1 | peak RSS after 6 passes | 44.2 | 30.4 | MB |
| 1.45 | json | realerr | parse | 1 | steady peak footprint | 4.99 | 3.44 | MB |
| 1.45 | python | realerr (too small) | parse | 1 | peak footprint after 6 passes | 5.05 | 3.48 | MB |
| 1.45 | zig | real | walk minus parse | all | cycles min | 574 | 396 | M |
| 1.45 | typescript | realerr | cursor | 1 | peak RSS after 6 passes | 11.2 | 7.73 | MB |
| 1.45 | typescript | realerr | cursor | 1 | peak footprint after 6 passes | 11.2 | 7.73 | MB |
| 1.45 | python | realerr (too small) | parse | 1 | peak RSS after 6 passes | 5.05 | 3.48 | MB |
| 1.45 | json | realerr | parse | 1 | steady peak RSS | 4.98 | 3.44 | MB |
| 1.45 | kotlin | real | parse | 1 | steady peak footprint | 24.6 | 17 | MB |
| 1.45 | java | realerr (too small) | walk | all | peak RSS after 6 passes | 6.38 | 4.41 | MB |
| 1.45 | kotlin | real | parse | 1 | steady peak RSS | 24.6 | 17 | MB |
| 1.45 | json | realerr | parse | all | steady peak footprint | 14.5 | 10 | MB |
| 1.44 | json | realerr | parse | all | steady peak RSS | 14.5 | 10 | MB |
| 1.44 | csharp | realerr | walk | 1 | RSS above loaded after 6 passes | 16.7 | 11.5 | MB |
| 1.44 | golang | broken | parse | 1 | RSS above loaded after 6 passes | 33.9 | 23.5 | MB |
| 1.44 | php | broken | parse | 1 | steady peak footprint | 42.1 | 29.3 | MB |
| 1.44 | php | broken | parse | 1 | steady peak RSS | 42.1 | 29.3 | MB |
| 1.44 | csharp | realerr | parse | 1 | RSS above loaded after 6 passes | 16.5 | 11.5 | MB |
| 1.44 | csharp | real | parse | 1 | steady RSS above loaded | 21.4 | 14.9 | MB |
| 1.43 | kotlin | real | walk | 1 | RSS above loaded after 6 passes | 12.3 | 8.56 | MB |
| 1.43 | golang | broken | parse | 1 | peak RSS after 6 passes | 42.5 | 29.7 | MB |
| 1.43 | golang | broken | parse | 1 | peak footprint after 6 passes | 42.5 | 29.7 | MB |
| 1.43 | kotlin | real | parse | 1 | peak footprint after 6 passes | 24.5 | 17.1 | MB |
| 1.43 | kotlin | real | parse | 1 | peak RSS after 6 passes | 24.5 | 17.1 | MB |
| 1.43 | php | broken | parse | 1 | steady RSS above loaded | 33.1 | 23.2 | MB |
| 1.43 | rust | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.14 | 2.2 | MB |
| 1.42 | tsx | real | walk | 1 | peak RSS after 6 passes | 15.1 | 10.6 | MB |
| 1.42 | tsx | real | walk | 1 | peak footprint after 6 passes | 15.1 | 10.6 | MB |
| 1.42 | golang | broken | parse | 1 | steady peak RSS | 42.1 | 29.6 | MB |
| 1.42 | golang | broken | parse | 1 | steady peak footprint | 42.1 | 29.6 | MB |
| 1.42 | rust | realerr (too small) | parse | 1 | steady peak footprint | 5.12 | 3.6 | MB |
| 1.42 | rust | realerr (too small) | parse | 1 | steady peak RSS | 5.12 | 3.6 | MB |
| 1.42 | zig | real | walk minus parse | all | instructions min | 1430 | 1010 | M |
| 1.42 | csharp | realerr | parse | 1 | steady peak footprint | 21.6 | 15.2 | MB |
| 1.42 | php | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 2.08 | 1.46 | MB |
| 1.42 | csharp | realerr | parse | 1 | steady peak RSS | 21.5 | 15.2 | MB |
| 1.42 | csharp | real | parse | 1 | steady peak footprint | 35.1 | 24.8 | MB |
| 1.42 | csharp | real | parse | 1 | steady peak RSS | 35.1 | 24.8 | MB |
| 1.41 | scala | real | walk minus parse | all | instructions min | 561 | 397 | M |
| 1.41 | golang | broken | parse | 1 | steady RSS above loaded | 33.3 | 23.5 | MB |
| 1.41 | java | broken | parse | 1 | peak footprint after 6 passes | 54.7 | 38.7 | MB |
| 1.41 | java | broken | parse | 1 | peak RSS after 6 passes | 54.6 | 38.7 | MB |
| 1.41 | verilog | real | cursor | 1 | RSS above loaded after 6 passes | 21.7 | 15.4 | MB |
| 1.41 | bash | broken | walk minus parse | 1 | cycles min | 172 | 122 | M |
| 1.41 | python | realerr (too small) | cursor | all | peak footprint after 6 passes | 6.77 | 4.8 | MB |
| 1.41 | kotlin | real | walk | 1 | peak RSS after 6 passes | 24.4 | 17.3 | MB |
| 1.41 | kotlin | real | walk | 1 | peak footprint after 6 passes | 24.4 | 17.3 | MB |
| 1.41 | java | broken | parse | 1 | steady peak footprint | 54.6 | 38.8 | MB |
| 1.41 | java | broken | parse | 1 | steady peak RSS | 54.6 | 38.8 | MB |
| 1.41 | zig | realerr | cursor minus parse | all | CPU user+sys min | 5.78 | 4.1 | ms |
| 1.41 | java | broken | parse | 1 | RSS above loaded after 6 passes | 48.1 | 34.1 | MB |
| 1.41 | lua | real | walk minus parse | all | instructions min | 1590 | 1130 | M |
| 1.41 | lua | real | walk minus parse | 1 | instructions min | 1590 | 1130 | M |
| 1.41 | verilog | real | cursor | 1 | peak footprint after 6 passes | 25.7 | 18.3 | MB |
| 1.41 | verilog | real | cursor | 1 | peak RSS after 6 passes | 25.7 | 18.3 | MB |
| 1.4 | python | realerr (too small) | cursor | all | peak RSS after 6 passes | 6.74 | 4.8 | MB |
| 1.4 | julia | realerr | walk minus parse | all | CPU user+sys min | 9.4 | 6.7 | ms |
| 1.4 | tsx | realerr | walk | 1 | RSS above loaded after 6 passes | 5.41 | 3.86 | MB |
| 1.4 | rust | realerr (too small) | cursor | all | peak footprint after 6 passes | 6.43 | 4.58 | MB |
| 1.4 | java | realerr (too small) | cursor | all | peak footprint after 6 passes | 6.51 | 4.64 | MB |
| 1.4 | scala | realerr | walk minus parse | 1 | instructions min | 91.4 | 65.2 | M |
| 1.4 | bash | realerr | walk minus parse | all | instructions min | 77.5 | 55.3 | M |
| 1.4 | ruby | real | walk minus parse | all | instructions min | 1210 | 861 | M |
| 1.4 | java | realerr (too small) | walk minus parse | all | instructions min | 0.0658 | 0.047 | M |
| 1.4 | java | real | parse | 1 | peak footprint after 6 passes | 20.9 | 14.9 | MB |
| 1.4 | zig | realerr | walk minus parse | all | instructions min | 103 | 74 | M |
| 1.4 | java | real | parse | 1 | peak RSS after 6 passes | 20.9 | 14.9 | MB |
| 1.4 | java | realerr (too small) | cursor | all | peak RSS after 6 passes | 6.48 | 4.64 | MB |
| 1.4 | rust | realerr (too small) | cursor | all | peak RSS after 6 passes | 6.4 | 4.58 | MB |
| 1.39 | php | realerr (too small) | parse | 1 | peak footprint after 6 passes | 5.23 | 3.75 | MB |
| 1.39 | python | realerr (too small) | cursor | all | instructions | 2.63 | 1.89 | M |
| 1.39 | scala | real | walk minus parse | 1 | instructions min | 561 | 402 | M |
| 1.39 | php | realerr (too small) | parse | 1 | peak RSS after 6 passes | 5.23 | 3.75 | MB |
| 1.39 | bash | broken | walk minus parse | 1 | CPU user+sys min | 33.2 | 23.9 | ms |
| 1.39 | json | realerr | cursor | all | peak RSS after 6 passes | 13.3 | 9.58 | MB |
| 1.39 | json | realerr | cursor | all | peak footprint after 6 passes | 13.3 | 9.58 | MB |
| 1.39 | tsx | realerr | walk | 1 | peak footprint after 6 passes | 9.24 | 6.65 | MB |
| 1.39 | verilog | real | parse | 1 | steady RSS above loaded | 20.5 | 14.7 | MB |
| 1.39 | kotlin | broken | parse | all | steady peak RSS | 35.1 | 25.3 | MB |
| 1.39 | kotlin | broken | parse | all | steady peak footprint | 35.1 | 25.3 | MB |
| 1.39 | verilog | broken | parse | 1 | RSS above loaded after 6 passes | 35.9 | 25.9 | MB |
| 1.39 | verilog | broken | parse | 1 | peak footprint after 6 passes | 44.9 | 32.3 | MB |
| 1.39 | verilog | broken | parse | 1 | peak RSS after 6 passes | 44.9 | 32.3 | MB |
| 1.39 | verilog | real | parse | 1 | steady peak RSS | 24.5 | 17.7 | MB |
| 1.39 | verilog | real | parse | 1 | steady peak footprint | 24.5 | 17.7 | MB |
| 1.39 | java | real | parse | 1 | steady peak footprint | 20.7 | 14.9 | MB |
| 1.39 | tsx | realerr | walk | 1 | peak RSS after 6 passes | 9.23 | 6.65 | MB |
| 1.39 | java | real | parse | 1 | steady peak RSS | 20.7 | 14.9 | MB |
| 1.39 | javascript | realerr (too small) | walk minus parse | all | instructions min | 0.221 | 0.16 | M |
| 1.39 | ruby | broken | walk | 1 | peak footprint after 6 passes | 110 | 79.3 | MB |
| 1.38 | ruby | broken | walk | 1 | peak RSS after 6 passes | 110 | 79.3 | MB |
| 1.38 | php | broken | parse | 1 | RSS above loaded after 6 passes | 32.2 | 23.3 | MB |
| 1.38 | php | broken | parse | 1 | peak RSS after 6 passes | 40.8 | 29.5 | MB |
| 1.38 | php | broken | parse | 1 | peak footprint after 6 passes | 40.8 | 29.5 | MB |
| 1.38 | verilog | broken | walk | 1 | RSS above loaded after 6 passes | 36 | 26.1 | MB |
| 1.38 | kotlin | realerr | walk | all | peak footprint after 6 passes | 34.7 | 25.1 | MB |
| 1.38 | kotlin | realerr | walk | all | peak RSS after 6 passes | 34.7 | 25.1 | MB |
| 1.38 | typescript | realerr | walk minus parse | all | cycles min | 7.83 | 5.67 | M |
| 1.38 | golang | broken | walk minus parse | all | CPU user+sys min | 64.4 | 46.7 | ms |
| 1.38 | golang | realerr | parse | 1 | steady RSS above loaded | 9.94 | 7.2 | MB |
| 1.38 | ruby | broken | walk | 1 | RSS above loaded after 6 passes | 102 | 74.2 | MB |
| 1.38 | csharp | realerr | parse | 1 | peak RSS after 6 passes | 21.7 | 15.8 | MB |
| 1.38 | csharp | realerr | parse | 1 | peak footprint after 6 passes | 21.7 | 15.8 | MB |
| 1.38 | golang | broken | walk minus parse | all | cycles min | 296 | 215 | M |
| 1.38 | ruby | real | walk minus parse | 1 | instructions min | 1210 | 876 | M |
| 1.38 | csharp | realerr | walk | 1 | peak footprint after 6 passes | 21.8 | 15.9 | MB |
| 1.38 | verilog | broken | walk | 1 | peak RSS after 6 passes | 45 | 32.7 | MB |
| 1.38 | verilog | broken | walk | 1 | peak footprint after 6 passes | 45 | 32.7 | MB |
| 1.38 | csharp | realerr | walk | 1 | peak RSS after 6 passes | 21.8 | 15.9 | MB |
| 1.38 | golang | realerr | parse | 1 | steady peak footprint | 14.2 | 10.3 | MB |
| 1.38 | c | broken | walk minus parse | 1 | instructions min | 942 | 685 | M |
| 1.37 | scala | realerr | walk minus parse | all | instructions min | 91.8 | 66.8 | M |
| 1.37 | golang | realerr | parse | 1 | steady peak RSS | 14.2 | 10.3 | MB |
| 1.37 | java | broken | parse | 1 | steady RSS above loaded | 47 | 34.2 | MB |
| 1.37 | tsx | realerr | walk minus parse | all | instructions min | 28.1 | 20.5 | M |
| 1.37 | c | broken | parse | 1 | RSS above loaded after 6 passes | 32.8 | 23.9 | MB |
| 1.37 | php | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.99 | 1.46 | MB |
| 1.37 | zig | real | walk minus parse | 1 | instructions min | 1430 | 1050 | M |
| 1.37 | verilog | realerr | cursor minus parse | all | cycles min | 366 | 268 | M |
| 1.37 | bash | real | walk minus parse | 1 | instructions min | 252 | 185 | M |
| 1.36 | verilog | broken | parse | 1 | steady RSS above loaded | 35.5 | 26 | MB |
| 1.36 | zig | realerr | walk minus parse | 1 | instructions min | 103 | 75.7 | M |
| 1.36 | json | broken | parse | 1 | RSS above loaded after 6 passes | 32 | 23.5 | MB |
| 1.36 | csharp | realerr | walk minus parse | all | instructions min | 212 | 155 | M |
| 1.36 | tsx | realerr | walk minus parse | 1 | instructions min | 26.5 | 19.5 | M |
| 1.36 | verilog | real | cursor | all | peak RSS after 6 passes | 37.9 | 27.9 | MB |
| 1.36 | verilog | real | cursor | all | peak footprint after 6 passes | 37.9 | 27.9 | MB |
| 1.36 | verilog | broken | parse | 1 | steady peak footprint | 44.5 | 32.7 | MB |
| 1.36 | verilog | broken | parse | 1 | steady peak RSS | 44.5 | 32.7 | MB |
| 1.36 | c | real | walk | 1 | peak RSS after 6 passes | 20.2 | 14.9 | MB |
| 1.36 | c | real | walk | 1 | peak footprint after 6 passes | 20.2 | 14.9 | MB |
| 1.36 | typescript | realerr | walk minus parse | 1 | instructions min | 24 | 17.7 | M |
| 1.35 | java | realerr (too small) | walk minus parse | 1 | instructions min | 0.0571 | 0.0421 | M |
| 1.35 | verilog | real | parse | 1 | peak RSS after 6 passes | 24.6 | 18.2 | MB |
| 1.35 | verilog | real | parse | 1 | peak footprint after 6 passes | 24.6 | 18.2 | MB |
| 1.35 | c | broken | parse | 1 | steady RSS above loaded | 32.3 | 23.8 | MB |
| 1.35 | c | broken | parse | 1 | peak RSS after 6 passes | 42.2 | 31.2 | MB |
| 1.35 | c | broken | parse | 1 | peak footprint after 6 passes | 42.2 | 31.2 | MB |
| 1.35 | bash | real | walk minus parse | all | instructions min | 252 | 186 | M |
| 1.35 | cpp | real | parse | 1 | peak footprint after 6 passes | 21.3 | 15.8 | MB |
| 1.35 | kotlin | realerr | walk | all | RSS above loaded after 6 passes | 29.9 | 22.1 | MB |
| 1.35 | cpp | real | parse | 1 | peak RSS after 6 passes | 21.3 | 15.8 | MB |
| 1.35 | json | broken | parse | 1 | steady RSS above loaded | 31.8 | 23.5 | MB |
| 1.35 | csharp | broken | walk minus parse | all | instructions min | 337 | 250 | M |
| 1.35 | json | realerr | walk | all | peak footprint after 6 passes | 12.1 | 8.96 | MB |
| 1.35 | c | broken | parse | 1 | steady peak RSS | 41.8 | 31 | MB |
| 1.35 | c | broken | parse | 1 | steady peak footprint | 41.8 | 31 | MB |
| 1.35 | csharp | real | walk minus parse | 1 | instructions min | 936 | 694 | M |
| 1.35 | json | realerr | walk | all | peak RSS after 6 passes | 12.1 | 8.96 | MB |
| 1.35 | cpp | broken | walk minus parse | all | instructions min | 1200 | 887 | M |
| 1.35 | verilog | real | parse | 1 | RSS above loaded after 6 passes | 20.7 | 15.4 | MB |
| 1.35 | golang | realerr | parse | 1 | RSS above loaded after 6 passes | 9.82 | 7.29 | MB |
| 1.35 | verilog | realerr | walk | 1 | peak footprint after 6 passes | 45.6 | 33.9 | MB |
| 1.35 | verilog | realerr | walk | 1 | peak RSS after 6 passes | 45.6 | 33.9 | MB |
| 1.35 | c | real | parse | 1 | peak RSS after 6 passes | 20 | 14.9 | MB |
| 1.35 | c | real | parse | 1 | peak footprint after 6 passes | 20 | 14.9 | MB |
| 1.35 | php | broken | walk | 1 | peak RSS after 6 passes | 40.5 | 30.1 | MB |
| 1.35 | php | broken | walk | 1 | peak footprint after 6 passes | 40.5 | 30.1 | MB |
| 1.35 | verilog | real | walk | 1 | peak RSS after 6 passes | 24.7 | 18.3 | MB |
| 1.35 | verilog | real | walk | 1 | peak footprint after 6 passes | 24.7 | 18.3 | MB |
| 1.35 | hcl | real | parse | 1 | steady peak RSS | 17.5 | 13 | MB |
| 1.35 | hcl | real | parse | 1 | steady peak footprint | 17.5 | 13 | MB |
| 1.34 | cpp | broken | parse | 1 | RSS above loaded after 6 passes | 173 | 129 | MB |
| 1.34 | kotlin | broken | parse | all | steady RSS above loaded | 28.1 | 20.9 | MB |
| 1.34 | typescript | realerr | walk | all | peak footprint after 6 passes | 28.3 | 21.1 | MB |
| 1.34 | hcl | broken | parse | 1 | RSS above loaded after 6 passes | 10.1 | 7.54 | MB |
| 1.34 | typescript | realerr | walk | all | peak RSS after 6 passes | 28.3 | 21.1 | MB |
| 1.34 | cpp | real | walk | 1 | peak footprint after 6 passes | 21.1 | 15.8 | MB |
| 1.34 | tsx | real | walk minus parse | 1 | instructions min | 356 | 266 | M |
| 1.34 | cpp | real | walk | 1 | peak RSS after 6 passes | 21.1 | 15.8 | MB |
| 1.34 | python | real | parse | 1 | RSS above loaded after 6 passes | 14.2 | 10.6 | MB |
| 1.34 | php | realerr (too small) | cursor | 1 | peak footprint after 6 passes | 5.36 | 4 | MB |
| 1.34 | cpp | broken | parse | 1 | peak RSS after 6 passes | 185 | 138 | MB |
| 1.34 | cpp | broken | parse | 1 | peak footprint after 6 passes | 185 | 138 | MB |
| 1.34 | verilog | realerr | parse | 1 | steady peak footprint | 45.5 | 34 | MB |
| 1.34 | php | realerr (too small) | cursor | 1 | peak RSS after 6 passes | 5.35 | 4 | MB |
| 1.34 | verilog | realerr | parse | 1 | steady peak RSS | 45.5 | 34 | MB |
| 1.34 | json | broken | parse | 1 | peak RSS after 6 passes | 44.9 | 33.5 | MB |
| 1.34 | json | broken | parse | 1 | peak footprint after 6 passes | 44.9 | 33.5 | MB |
| 1.34 | json | realerr | parse | all | steady RSS above loaded | 9.86 | 7.36 | MB |
| 1.34 | python | broken | parse | 1 | steady peak RSS | 55.8 | 41.7 | MB |
| 1.34 | python | broken | parse | 1 | steady peak footprint | 55.8 | 41.7 | MB |
| 1.34 | tsx | real | walk minus parse | all | instructions min | 357 | 267 | M |
| 1.34 | typescript | realerr | cursor minus parse | all | cycles min | 9.69 | 7.25 | M |
| 1.34 | python | real | walk minus parse | 1 | instructions min | 1500 | 1120 | M |
| 1.34 | python | broken | parse | 1 | steady RSS above loaded | 45.9 | 34.4 | MB |
| 1.34 | csharp | realerr | walk minus parse | 1 | instructions min | 211 | 158 | M |
| 1.33 | java | real | walk minus parse | all | instructions min | 916 | 686 | M |
| 1.33 | cpp | real | parse | 1 | steady peak RSS | 21 | 15.8 | MB |
| 1.33 | cpp | real | parse | 1 | steady peak footprint | 21 | 15.8 | MB |
| 1.33 | java | real | walk minus parse | 1 | instructions min | 916 | 687 | M |
| 1.33 | rust | realerr (too small) | parse | 1 | peak footprint after 6 passes | 5.25 | 3.93 | MB |
| 1.33 | verilog | realerr | walk | 1 | RSS above loaded after 6 passes | 35.4 | 26.6 | MB |
| 1.33 | python | broken | parse | 1 | RSS above loaded after 6 passes | 46 | 34.5 | MB |
| 1.33 | php | realerr (too small) | walk | 1 | peak footprint after 6 passes | 5.3 | 3.98 | MB |
| 1.33 | rust | realerr (too small) | parse | 1 | peak RSS after 6 passes | 5.24 | 3.93 | MB |
| 1.33 | typescript | real | walk minus parse | 1 | instructions min | 1490 | 1120 | M |
| 1.33 | hcl | real | parse | 1 | peak RSS after 6 passes | 17.8 | 13.3 | MB |
| 1.33 | hcl | real | parse | 1 | peak footprint after 6 passes | 17.8 | 13.3 | MB |
| 1.33 | php | realerr (too small) | walk | 1 | peak RSS after 6 passes | 5.3 | 3.98 | MB |
| 1.33 | verilog | realerr | parse | 1 | peak footprint after 6 passes | 45.3 | 34 | MB |
| 1.33 | tsx | broken | cursor | 1 | peak RSS after 6 passes | 27.7 | 20.8 | MB |
| 1.33 | tsx | broken | cursor | 1 | peak footprint after 6 passes | 27.7 | 20.8 | MB |
| 1.33 | php | real | walk minus parse | all | instructions min | 1300 | 974 | M |
| 1.33 | verilog | realerr | parse | 1 | peak RSS after 6 passes | 45.3 | 34 | MB |
| 1.33 | cpp | broken | walk | 1 | RSS above loaded after 6 passes | 182 | 137 | MB |
| 1.33 | json | broken | parse | 1 | steady peak RSS | 44.7 | 33.6 | MB |
| 1.33 | json | broken | parse | 1 | steady peak footprint | 44.7 | 33.6 | MB |
| 1.33 | golang | realerr | walk minus parse | all | instructions min | 150 | 113 | M |
| 1.33 | cpp | broken | walk | 1 | peak RSS after 6 passes | 195 | 147 | MB |
| 1.33 | cpp | broken | walk | 1 | peak footprint after 6 passes | 195 | 147 | MB |
| 1.33 | typescript | realerr | cursor | all | peak footprint after 6 passes | 29 | 21.9 | MB |
| 1.33 | hcl | broken | parse | 1 | peak RSS after 6 passes | 15.4 | 11.6 | MB |
| 1.33 | hcl | broken | parse | 1 | peak footprint after 6 passes | 15.4 | 11.6 | MB |
| 1.33 | verilog | realerr | parse | 1 | RSS above loaded after 6 passes | 35.1 | 26.5 | MB |
| 1.33 | php | real | parse | 1 | peak footprint after 6 passes | 45.8 | 34.5 | MB |
| 1.33 | typescript | realerr | cursor | all | peak RSS after 6 passes | 29 | 21.9 | MB |
| 1.33 | php | real | parse | 1 | peak RSS after 6 passes | 45.8 | 34.5 | MB |
| 1.33 | verilog | real | walk | 1 | RSS above loaded after 6 passes | 20.6 | 15.5 | MB |
| 1.33 | python | realerr (too small) | walk minus parse | 1 | instructions min | 0.103 | 0.0778 | M |
| 1.33 | python | broken | parse | 1 | peak RSS after 6 passes | 55.8 | 42.1 | MB |
| 1.33 | python | broken | parse | 1 | peak footprint after 6 passes | 55.8 | 42.1 | MB |
| 1.33 | golang | realerr | cursor | 1 | RSS above loaded after 6 passes | 11.5 | 8.66 | MB |
| 1.32 | golang | realerr | cursor | 1 | peak footprint after 6 passes | 15.9 | 12 | MB |
| 1.32 | verilog | realerr | parse | 1 | steady RSS above loaded | 35 | 26.4 | MB |
| 1.32 | php | real | parse | 1 | steady peak footprint | 46.4 | 35 | MB |
| 1.32 | golang | realerr | walk minus parse | 1 | instructions min | 149 | 113 | M |
| 1.32 | php | real | parse | 1 | steady peak RSS | 46.4 | 35 | MB |
| 1.32 | php | broken | walk | 1 | RSS above loaded after 6 passes | 31.8 | 24 | MB |
| 1.32 | typescript | real | walk minus parse | all | CPU user+sys min | 104 | 78.5 | ms |
| 1.32 | golang | realerr | cursor | 1 | peak RSS after 6 passes | 15.8 | 12 | MB |
| 1.32 | php | real | parse | 1 | steady RSS above loaded | 31.4 | 23.8 | MB |
| 1.32 | java | realerr (too small) | walk | 1 | peak footprint after 6 passes | 5.1 | 3.86 | MB |
| 1.32 | php | real | parse | 1 | RSS above loaded after 6 passes | 30.8 | 23.4 | MB |
| 1.32 | java | realerr (too small) | walk | 1 | peak RSS after 6 passes | 5.09 | 3.86 | MB |
| 1.32 | verilog | real | parse | all | peak RSS after 6 passes | 36.7 | 27.8 | MB |
| 1.32 | verilog | real | parse | all | peak footprint after 6 passes | 36.7 | 27.8 | MB |
| 1.32 | cpp | real | walk minus parse | 1 | instructions min | 411 | 312 | M |
| 1.32 | csharp | broken | walk minus parse | 1 | instructions min | 338 | 257 | M |
| 1.32 | cpp | realerr | walk minus parse | all | instructions min | 1330 | 1010 | M |
| 1.32 | golang | real | walk minus parse | all | instructions min | 1290 | 982 | M |
| 1.32 | cpp | broken | walk minus parse | 1 | instructions min | 1170 | 891 | M |
| 1.32 | python | real | walk minus parse | all | instructions min | 1500 | 1140 | M |
| 1.32 | verilog | real | cursor | all | RSS above loaded after 6 passes | 32.8 | 24.9 | MB |
| 1.31 | golang | realerr | parse | 1 | peak footprint after 6 passes | 14.1 | 10.7 | MB |
| 1.31 | golang | realerr | parse | 1 | peak RSS after 6 passes | 14.1 | 10.7 | MB |
| 1.31 | ruby | real | walk | 1 | peak RSS after 6 passes | 27.1 | 20.7 | MB |
| 1.31 | ruby | real | walk | 1 | peak footprint after 6 passes | 27.1 | 20.7 | MB |
| 1.31 | csharp | real | walk minus parse | all | instructions min | 936 | 714 | M |
| 1.31 | bash | realerr | walk minus parse | 1 | instructions min | 77.4 | 59 | M |
| 1.31 | c | real | parse | 1 | steady peak RSS | 20 | 15.3 | MB |
| 1.31 | c | real | parse | 1 | steady peak footprint | 20 | 15.3 | MB |
| 1.31 | verilog | real | walk | all | peak RSS after 6 passes | 36.4 | 27.8 | MB |
| 1.31 | verilog | real | walk | all | peak footprint after 6 passes | 36.4 | 27.8 | MB |
| 1.31 | cpp | broken | walk minus parse | all | cycles min | 333 | 254 | M |
| 1.31 | golang | real | walk minus parse | 1 | instructions min | 1290 | 986 | M |
| 1.31 | tsx | real | cursor | all | peak footprint after 6 passes | 47.8 | 36.5 | MB |
| 1.31 | tsx | real | cursor | all | peak RSS after 6 passes | 47.8 | 36.5 | MB |
| 1.31 | cpp | real | walk minus parse | all | instructions min | 411 | 314 | M |
| 1.31 | cpp | realerr | walk minus parse | 1 | instructions min | 1330 | 1020 | M |
| 1.31 | cpp | broken | walk minus parse | all | CPU user+sys min | 79.1 | 60.5 | ms |
| 1.31 | kotlin | real | cursor | all | peak footprint after 6 passes | 81.5 | 62.3 | MB |
| 1.31 | kotlin | real | cursor | all | peak RSS after 6 passes | 81.5 | 62.3 | MB |
| 1.31 | zig | realerr | walk minus parse | all | cycles min | 31.4 | 24 | M |
| 1.31 | json | realerr | parse | 1 | peak footprint after 6 passes | 4.83 | 3.7 | MB |
| 1.31 | golang | broken | walk minus parse | 1 | CPU user+sys min | 34.1 | 26.1 | ms |
| 1.31 | zig | realerr | parse | 1 | steady peak footprint | 15.3 | 11.7 | MB |
| 1.3 | json | realerr | cursor | all | RSS above loaded after 6 passes | 8.93 | 6.84 | MB |
| 1.3 | zig | realerr | parse | 1 | steady peak RSS | 15.3 | 11.7 | MB |
| 1.3 | json | realerr | parse | 1 | peak RSS after 6 passes | 4.82 | 3.7 | MB |
| 1.3 | cpp | realerr | parse | 1 | steady RSS above loaded | 20.6 | 15.9 | MB |
| 1.3 | bash | realerr | cursor minus parse | all | cycles min | 29.7 | 22.9 | M |
| 1.3 | typescript | real | walk minus parse | all | instructions min | 1490 | 1150 | M |
| 1.3 | cpp | broken | parse | 1 | steady peak RSS | 179 | 138 | MB |
| 1.3 | cpp | broken | parse | 1 | steady peak footprint | 179 | 138 | MB |
| 1.3 | cpp | broken | parse | 1 | steady RSS above loaded | 167 | 129 | MB |
| 1.3 | python | real | parse | 1 | peak footprint after 6 passes | 31.4 | 24.2 | MB |
| 1.3 | python | real | parse | 1 | peak RSS after 6 passes | 31.4 | 24.2 | MB |
| 1.3 | julia | real | walk minus parse | all | cycles min | 277 | 214 | M |
| 1.29 | c | realerr | walk minus parse | 1 | instructions min | 971 | 750 | M |
| 1.29 | zig | realerr | parse | 1 | steady RSS above loaded | 11.4 | 8.78 | MB |
| 1.29 | kotlin | realerr | parse | all | peak footprint after 6 passes | 33.7 | 26 | MB |
| 1.29 | c | real | walk | 1 | RSS above loaded after 6 passes | 11.9 | 9.16 | MB |
| 1.29 | kotlin | realerr | parse | all | peak RSS after 6 passes | 33.7 | 26 | MB |
| 1.29 | rust | realerr (too small) | walk | all | instructions | 1.22 | 0.945 | M |
| 1.29 | csharp | real | parse | 1 | peak footprint after 6 passes | 33.2 | 25.7 | MB |
| 1.29 | julia | realerr | walk minus parse | all | instructions min | 152 | 117 | M |
| 1.29 | csharp | real | parse | 1 | peak RSS after 6 passes | 33.2 | 25.7 | MB |
| 1.29 | javascript | realerr (too small) | walk | 1 | peak footprint after 6 passes | 5.07 | 3.92 | MB |
| 1.29 | golang | broken | walk minus parse | 1 | cycles min | 178 | 138 | M |
| 1.29 | rust | realerr (too small) | walk | all | peak footprint after 6 passes | 6.53 | 5.06 | MB |
| 1.29 | javascript | realerr (too small) | walk | 1 | peak RSS after 6 passes | 5.06 | 3.92 | MB |
| 1.29 | ruby | real | parse | 1 | peak RSS after 6 passes | 26.8 | 20.8 | MB |
| 1.29 | ruby | real | parse | 1 | peak footprint after 6 passes | 26.8 | 20.8 | MB |
| 1.29 | kotlin | real | cursor | all | RSS above loaded after 6 passes | 69.1 | 53.6 | MB |
| 1.29 | rust | realerr (too small) | walk | all | peak RSS after 6 passes | 6.52 | 5.06 | MB |
| 1.29 | php | real | walk minus parse | 1 | instructions min | 1300 | 1010 | M |
| 1.29 | c | realerr | walk minus parse | all | instructions min | 971 | 754 | M |
| 1.29 | typescript | realerr | walk | all | RSS above loaded after 6 passes | 23.8 | 18.5 | MB |
| 1.29 | javascript | real | walk minus parse | 1 | instructions min | 1620 | 1260 | M |
| 1.28 | cpp | real | parse | 1 | RSS above loaded after 6 passes | 13.7 | 10.6 | MB |
| 1.28 | cpp | real | walk minus parse | all | CPU user+sys min | 27.6 | 21.5 | ms |
| 1.28 | ruby | broken | walk minus parse | all | instructions min | 961 | 750 | M |
| 1.28 | csharp | real | walk | 1 | peak RSS after 6 passes | 33.1 | 25.9 | MB |
| 1.28 | csharp | real | walk | 1 | peak footprint after 6 passes | 33.1 | 25.9 | MB |
| 1.28 | bash | broken | parse | 1 | steady peak RSS | 63.1 | 49.3 | MB |
| 1.28 | bash | broken | parse | 1 | steady peak footprint | 63.1 | 49.3 | MB |
| 1.28 | java | realerr (too small) | cursor | 1 | peak footprint after 6 passes | 5.01 | 3.91 | MB |
| 1.28 | java | realerr (too small) | cursor | 1 | peak RSS after 6 passes | 5 | 3.91 | MB |
| 1.28 | tsx | real | walk | 1 | RSS above loaded after 6 passes | 7.08 | 5.54 | MB |
| 1.28 | javascript | realerr (too small) | walk minus parse | 1 | instructions min | 0.214 | 0.167 | M |
| 1.27 | typescript | realerr | cursor | all | RSS above loaded after 6 passes | 24.6 | 19.3 | MB |
| 1.27 | c | real | parse | 1 | RSS above loaded after 6 passes | 11.8 | 9.3 | MB |
| 1.27 | c | realerr | parse | 1 | peak RSS after 6 passes | 28.2 | 22.2 | MB |
| 1.27 | c | realerr | parse | 1 | peak footprint after 6 passes | 28.2 | 22.2 | MB |
| 1.27 | bash | broken | parse | 1 | peak RSS after 6 passes | 62.9 | 49.5 | MB |
| 1.27 | bash | broken | parse | 1 | peak footprint after 6 passes | 62.9 | 49.5 | MB |
| 1.27 | json | realerr | walk | all | RSS above loaded after 6 passes | 7.91 | 6.22 | MB |
| 1.27 | verilog | real | parse | all | RSS above loaded after 6 passes | 31.4 | 24.7 | MB |
| 1.27 | c | real | walk minus parse | 1 | instructions min | 448 | 353 | M |
| 1.27 | ruby | broken | walk minus parse | 1 | instructions min | 927 | 730 | M |
| 1.27 | ruby | real | walk | 1 | RSS above loaded after 6 passes | 13.1 | 10.3 | MB |
| 1.27 | python | real | parse | 1 | steady RSS above loaded | 13.3 | 10.5 | MB |
| 1.27 | typescript | broken | parse | 1 | steady peak RSS | 53 | 41.8 | MB |
| 1.27 | typescript | broken | parse | 1 | steady peak footprint | 53 | 41.8 | MB |
| 1.27 | javascript | broken | parse | 1 | peak footprint after 6 passes | 75.5 | 59.6 | MB |
| 1.27 | javascript | broken | parse | 1 | steady peak footprint | 75.4 | 59.5 | MB |
| 1.27 | javascript | broken | parse | 1 | peak RSS after 6 passes | 75.5 | 59.6 | MB |
| 1.27 | javascript | broken | parse | 1 | steady peak RSS | 75.4 | 59.5 | MB |
| 1.27 | rust | realerr (too small) | walk minus parse | 1 | instructions min | 0.0727 | 0.0573 | M |
| 1.27 | c | realerr | walk | 1 | RSS above loaded after 6 passes | 16 | 12.6 | MB |
| 1.27 | c | realerr | walk | 1 | peak footprint after 6 passes | 28.6 | 22.5 | MB |
| 1.27 | hcl | real | walk | 1 | peak RSS after 6 passes | 17.5 | 13.8 | MB |
| 1.27 | hcl | real | walk | 1 | peak footprint after 6 passes | 17.5 | 13.8 | MB |
| 1.27 | c | realerr | walk | 1 | peak RSS after 6 passes | 28.5 | 22.5 | MB |
| 1.27 | typescript | broken | parse | 1 | peak RSS after 6 passes | 53 | 41.8 | MB |
| 1.27 | typescript | broken | parse | 1 | peak footprint after 6 passes | 53 | 41.8 | MB |
| 1.27 | verilog | real | walk minus parse | all | instructions min | 72.6 | 57.3 | M |
| 1.27 | rust | real | parse | 1 | peak footprint after 6 passes | 25.2 | 19.9 | MB |
| 1.27 | rust | real | parse | 1 | peak RSS after 6 passes | 25.2 | 19.9 | MB |
| 1.27 | cpp | realerr | parse | 1 | steady peak footprint | 35.1 | 27.8 | MB |
| 1.27 | cpp | realerr | parse | 1 | steady peak RSS | 35.1 | 27.8 | MB |
| 1.27 | typescript | real | parse | 1 | peak footprint after 6 passes | 30.7 | 24.3 | MB |
| 1.26 | typescript | real | parse | 1 | peak RSS after 6 passes | 30.7 | 24.3 | MB |
| 1.26 | zig | realerr | parse | 1 | RSS above loaded after 6 passes | 11.1 | 8.8 | MB |
| 1.26 | rust | realerr (too small) | parse | all | instructions | 1.12 | 0.888 | M |
| 1.26 | zig | realerr | parse | 1 | peak footprint after 6 passes | 15 | 11.9 | MB |
| 1.26 | c | realerr | parse | 1 | RSS above loaded after 6 passes | 15.8 | 12.5 | MB |
| 1.26 | verilog | real | walk | all | RSS above loaded after 6 passes | 31.3 | 24.8 | MB |
| 1.26 | python | real | parse | 1 | steady peak RSS | 30.4 | 24.1 | MB |
| 1.26 | python | real | parse | 1 | steady peak footprint | 30.4 | 24.1 | MB |
| 1.26 | java | real | walk | 1 | peak RSS after 6 passes | 20.7 | 16.4 | MB |
| 1.26 | java | real | walk | 1 | peak footprint after 6 passes | 20.7 | 16.4 | MB |
| 1.26 | tsx | broken | cursor | 1 | RSS above loaded after 6 passes | 20.2 | 16 | MB |
| 1.26 | zig | realerr | parse | 1 | peak RSS after 6 passes | 15 | 11.9 | MB |
| 1.26 | c | real | walk minus parse | all | instructions min | 448 | 355 | M |
| 1.26 | bash | realerr | walk minus parse | all | cycles min | 27.6 | 21.9 | M |
| 1.26 | rust | real | parse | 1 | steady peak RSS | 25.2 | 20 | MB |
| 1.26 | rust | real | parse | 1 | steady peak footprint | 25.2 | 20 | MB |
| 1.26 | julia | real | walk minus parse | all | instructions min | 759 | 602 | M |
| 1.26 | rust | broken | parse | 1 | steady peak RSS | 52.7 | 41.8 | MB |
| 1.26 | rust | broken | parse | 1 | steady peak footprint | 52.7 | 41.8 | MB |
| 1.26 | typescript | real | parse | 1 | steady peak RSS | 30.5 | 24.2 | MB |
| 1.26 | typescript | real | parse | 1 | steady peak footprint | 30.5 | 24.2 | MB |
| 1.26 | julia | realerr | walk minus parse | 1 | instructions min | 151 | 120 | M |
| 1.26 | cpp | realerr | walk | 1 | RSS above loaded after 6 passes | 20.5 | 16.3 | MB |
| 1.26 | rust | broken | parse | 1 | peak RSS after 6 passes | 52.8 | 42 | MB |
| 1.26 | rust | broken | parse | 1 | peak footprint after 6 passes | 52.8 | 42 | MB |
| 1.26 | verilog | realerr | cursor minus parse | all | CPU user+sys min | 83.3 | 66.2 | ms |
| 1.26 | kotlin | realerr | parse | all | RSS above loaded after 6 passes | 28.9 | 23 | MB |
| 1.26 | php | realerr (too small) | walk | 1 | cycles | 0.0346 | 0.0275 | M |
| 1.26 | cpp | real | walk | 1 | RSS above loaded after 6 passes | 13.6 | 10.8 | MB |
| 1.26 | typescript | real | parse | 1 | RSS above loaded after 6 passes | 13.5 | 10.7 | MB |
| 1.26 | javascript | broken | parse | 1 | steady RSS above loaded | 66.5 | 52.9 | MB |
| 1.26 | rust | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3.27 | 2.6 | MB |
| 1.26 | javascript | broken | parse | 1 | RSS above loaded after 6 passes | 66.5 | 52.9 | MB |
| 1.26 | lua | real | parse | 1 | steady peak footprint | 23.8 | 18.9 | MB |
| 1.26 | lua | real | parse | 1 | steady peak RSS | 23.8 | 18.9 | MB |
| 1.26 | julia | real | walk minus parse | 1 | instructions min | 759 | 605 | M |
| 1.26 | cpp | real | parse | 1 | steady RSS above loaded | 13.6 | 10.8 | MB |
| 1.25 | python | real | walk minus parse | all | CPU user+sys min | 134 | 106 | ms |
| 1.25 | typescript | broken | parse | 1 | steady RSS above loaded | 44.5 | 35.5 | MB |
| 1.25 | java | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 1.83 | 1.46 | MB |
| 1.25 | rust | realerr (too small) | cursor | all | instructions | 1.24 | 0.986 | M |
| 1.25 | verilog | real | walk minus parse | 1 | instructions min | 72.3 | 57.7 | M |
| 1.25 | bash | broken | parse | 1 | steady RSS above loaded | 55.3 | 44.2 | MB |
| 1.25 | typescript | real | walk minus parse | all | cycles min | 490 | 392 | M |
| 1.25 | cpp | realerr | walk | 1 | peak RSS after 6 passes | 35.1 | 28.1 | MB |
| 1.25 | cpp | realerr | walk | 1 | peak footprint after 6 passes | 35.1 | 28.1 | MB |
| 1.25 | rust | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 2 | 1.6 | MB |
| 1.25 | tsx | real | cursor | all | RSS above loaded after 6 passes | 38.9 | 31.1 | MB |
| 1.25 | zig | real | walk minus parse | 1 | CPU user+sys min | 71.4 | 57.2 | ms |
| 1.25 | php | real | walk | 1 | peak footprint after 6 passes | 43.7 | 35.1 | MB |
| 1.25 | php | real | walk | 1 | peak RSS after 6 passes | 43.7 | 35.1 | MB |
| 1.24 | csharp | real | parse | 1 | RSS above loaded after 6 passes | 19.6 | 15.8 | MB |
| 1.24 | ruby | real | parse | 1 | RSS above loaded after 6 passes | 12.7 | 10.3 | MB |
| 1.24 | c | realerr | parse | 1 | steady peak footprint | 27.4 | 22.1 | MB |
| 1.24 | rust | real | walk minus parse | 1 | instructions min | 1500 | 1210 | M |
| 1.24 | rust | real | walk minus parse | all | instructions min | 1500 | 1210 | M |
| 1.24 | c | realerr | parse | 1 | steady peak RSS | 27.4 | 22.1 | MB |
| 1.24 | javascript | real | walk minus parse | all | instructions min | 1620 | 1310 | M |
| 1.24 | typescript | broken | parse | 1 | RSS above loaded after 6 passes | 44.3 | 35.7 | MB |
| 1.24 | cpp | realerr | parse | 1 | peak footprint after 6 passes | 34.7 | 28 | MB |
| 1.24 | cpp | realerr | parse | 1 | peak RSS after 6 passes | 34.7 | 28 | MB |
| 1.24 | bash | broken | parse | 1 | RSS above loaded after 6 passes | 55.3 | 44.6 | MB |
| 1.24 | cpp | realerr | parse | 1 | RSS above loaded after 6 passes | 20.2 | 16.3 | MB |
| 1.24 | lua | broken | walk minus parse | 1 | cycles min | 124 | 99.9 | M |
| 1.24 | zig | real | walk minus parse | 1 | cycles min | 369 | 298 | M |
| 1.24 | rust | broken | parse | 1 | steady RSS above loaded | 43.6 | 35.2 | MB |
| 1.24 | verilog | real | parse | all | steady peak RSS | 40.7 | 33 | MB |
| 1.24 | verilog | real | parse | all | steady peak footprint | 40.7 | 33 | MB |
| 1.24 | java | real | cursor | all | peak footprint after 6 passes | 73.4 | 59.4 | MB |
| 1.23 | java | real | cursor | all | peak RSS after 6 passes | 73.4 | 59.4 | MB |
| 1.23 | typescript | real | parse | 1 | steady RSS above loaded | 13.2 | 10.7 | MB |
| 1.23 | rust | broken | parse | 1 | RSS above loaded after 6 passes | 43.8 | 35.5 | MB |
| 1.23 | zig | broken | parse | 1 | steady peak footprint | 22.4 | 18.1 | MB |
| 1.23 | zig | broken | parse | 1 | steady peak RSS | 22.4 | 18.1 | MB |
| 1.23 | csharp | real | walk | 1 | RSS above loaded after 6 passes | 19.5 | 15.9 | MB |
| 1.23 | json | real | walk minus parse | all | instructions min | 1130 | 919 | M |
| 1.23 | csharp | real | walk minus parse | all | cycles min | 373 | 304 | M |
| 1.23 | lua | real | parse | 1 | peak footprint after 6 passes | 23.7 | 19.3 | MB |
| 1.23 | lua | real | parse | 1 | peak RSS after 6 passes | 23.7 | 19.3 | MB |
| 1.22 | c | real | parse | 1 | steady RSS above loaded | 11.9 | 9.7 | MB |
| 1.22 | bash | realerr | walk minus parse | 1 | CPU user+sys min | 3.58 | 2.93 | ms |
| 1.22 | golang | real | parse | 1 | peak footprint after 6 passes | 29.4 | 24.1 | MB |
| 1.22 | c | broken | walk minus parse | 1 | CPU user+sys min | 40.1 | 32.8 | ms |
| 1.22 | golang | real | parse | 1 | peak RSS after 6 passes | 29.4 | 24.1 | MB |
| 1.22 | json | real | walk minus parse | 1 | instructions min | 1130 | 926 | M |
| 1.22 | scala | real | walk minus parse | 1 | CPU user+sys min | 26.5 | 21.7 | ms |
| 1.22 | bash | realerr | walk minus parse | all | CPU user+sys min | 4.77 | 3.91 | ms |
| 1.22 | golang | real | parse | 1 | steady peak RSS | 29.5 | 24.2 | MB |
| 1.22 | golang | real | parse | 1 | steady peak footprint | 29.5 | 24.2 | MB |
| 1.22 | c | broken | walk minus parse | 1 | cycles min | 209 | 172 | M |
| 1.22 | bash | real | walk minus parse | all | cycles min | 86.6 | 71.1 | M |
| 1.22 | scala | real | walk minus parse | 1 | cycles min | 138 | 113 | M |
| 1.22 | php | realerr (too small) | walk minus parse | 1 | instructions min | 0.00252 | 0.00208 | M |
| 1.22 | julia | realerr | cursor | 1 | peak footprint after 6 passes | 27.2 | 22.4 | MB |
| 1.22 | kotlin | realerr | cursor | all | peak footprint after 6 passes | 32.6 | 26.8 | MB |
| 1.22 | php | realerr (too small) | walk | 1 | instructions | 0.0853 | 0.0702 | M |
| 1.22 | kotlin | realerr | cursor | all | peak RSS after 6 passes | 32.6 | 26.8 | MB |
| 1.22 | julia | realerr | cursor | 1 | peak RSS after 6 passes | 27.2 | 22.4 | MB |
| 1.21 | hcl | broken | walk | 1 | peak RSS after 6 passes | 15.3 | 12.6 | MB |
| 1.21 | hcl | broken | walk | 1 | peak footprint after 6 passes | 15.3 | 12.6 | MB |
| 1.21 | kotlin | real | walk minus parse | 1 | instructions min | 663 | 546 | M |
| 1.21 | lua | broken | walk minus parse | 1 | CPU user+sys min | 24.2 | 19.9 | ms |
| 1.21 | typescript | realerr | parse | all | peak footprint after 6 passes | 26.6 | 21.9 | MB |
| 1.21 | julia | broken | walk | 1 | peak RSS after 6 passes | 31.6 | 26 | MB |
| 1.21 | julia | broken | walk | 1 | peak footprint after 6 passes | 31.6 | 26 | MB |
| 1.21 | typescript | realerr | parse | all | peak RSS after 6 passes | 26.6 | 21.9 | MB |
| 1.21 | bash | realerr | walk minus parse | 1 | cycles min | 18.7 | 15.4 | M |
| 1.21 | rust | real | parse | 1 | steady RSS above loaded | 10.7 | 8.83 | MB |
| 1.21 | php | realerr (too small) | parse | 1 | instructions | 0.0824 | 0.0681 | M |
| 1.21 | rust | real | parse | 1 | RSS above loaded after 6 passes | 10.7 | 8.87 | MB |
| 1.21 | rust | real | walk | 1 | peak RSS after 6 passes | 25.5 | 21.1 | MB |
| 1.21 | rust | real | walk | 1 | peak footprint after 6 passes | 25.5 | 21.1 | MB |
| 1.21 | c | realerr | parse | 1 | steady RSS above loaded | 15 | 12.4 | MB |
| 1.21 | julia | realerr | cursor | 1 | RSS above loaded after 6 passes | 23.2 | 19.3 | MB |
| 1.21 | kotlin | realerr | walk minus parse | 1 | instructions min | 29.8 | 24.7 | M |
| 1.2 | java | real | cursor | all | RSS above loaded after 6 passes | 58.6 | 48.7 | MB |
| 1.2 | php | realerr (too small) | cursor | 1 | instructions | 0.0877 | 0.0728 | M |
| 1.2 | kotlin | real | walk minus parse | all | instructions min | 663 | 551 | M |
| 1.2 | python | realerr (too small) | cursor | 1 | peak footprint after 6 passes | 5.18 | 4.3 | MB |
| 1.2 | julia | broken | parse | 1 | peak footprint after 6 passes | 31.6 | 26.2 | MB |
| 1.2 | julia | broken | parse | 1 | peak RSS after 6 passes | 31.6 | 26.2 | MB |
| 1.2 | python | realerr (too small) | cursor | 1 | peak RSS after 6 passes | 5.17 | 4.3 | MB |
| 1.2 | julia | broken | parse | 1 | steady peak footprint | 31.3 | 26 | MB |
| 1.2 | julia | broken | parse | 1 | steady peak RSS | 31.3 | 26 | MB |
| 1.2 | kotlin | broken | walk minus parse | 1 | instructions min | 145 | 121 | M |
| 1.2 | golang | realerr | walk minus parse | 1 | cycles min | 34.9 | 29 | M |
| 1.2 | javascript | real | parse | 1 | peak footprint after 6 passes | 32.2 | 26.8 | MB |
| 1.2 | javascript | real | parse | 1 | peak RSS after 6 passes | 32.2 | 26.8 | MB |
| 1.2 | lua | real | parse | 1 | steady RSS above loaded | 7 | 5.84 | MB |
| 1.2 | php | real | walk | 1 | RSS above loaded after 6 passes | 28.6 | 23.9 | MB |
| 1.2 | ruby | broken | parse | 1 | steady peak footprint | 81.8 | 68.3 | MB |
| 1.2 | ruby | broken | parse | 1 | steady peak RSS | 81.8 | 68.3 | MB |
| 1.2 | typescript | real | walk | 1 | peak RSS after 6 passes | 30.6 | 25.5 | MB |
| 1.2 | typescript | real | walk | 1 | peak footprint after 6 passes | 30.6 | 25.5 | MB |
| 1.2 | php | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 2.06 | 1.72 | MB |
| 1.2 | verilog | real | parse | all | steady RSS above loaded | 35.7 | 29.8 | MB |
| 1.2 | kotlin | broken | walk minus parse | all | instructions min | 145 | 121 | M |
| 1.19 | kotlin | realerr | parse | all | steady peak RSS | 41.3 | 34.6 | MB |
| 1.19 | kotlin | realerr | parse | all | steady peak footprint | 41.3 | 34.6 | MB |
| 1.19 | golang | realerr | walk minus parse | 1 | CPU user+sys min | 6.67 | 5.59 | ms |
| 1.19 | python | real | walk minus parse | all | cycles min | 606 | 509 | M |
| 1.19 | python | real | walk | 1 | peak RSS after 6 passes | 30.4 | 25.5 | MB |
| 1.19 | python | real | walk | 1 | peak footprint after 6 passes | 30.4 | 25.5 | MB |
| 1.19 | julia | realerr | parse | 1 | steady peak footprint | 26.6 | 22.4 | MB |
| 1.19 | julia | realerr | parse | 1 | steady peak RSS | 26.6 | 22.4 | MB |
| 1.19 | hcl | real | parse | 1 | RSS above loaded after 6 passes | 7.6 | 6.39 | MB |
| 1.19 | javascript | real | parse | 1 | steady peak footprint | 32 | 26.9 | MB |
| 1.19 | javascript | real | parse | 1 | steady peak RSS | 31.9 | 26.9 | MB |
| 1.19 | julia | realerr | parse | 1 | peak footprint after 6 passes | 26.8 | 22.6 | MB |
| 1.19 | java | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 1.84 | 1.55 | MB |
| 1.19 | julia | realerr | parse | 1 | peak RSS after 6 passes | 26.8 | 22.6 | MB |
| 1.18 | json | real | parse | 1 | peak footprint after 6 passes | 23.4 | 19.7 | MB |
| 1.18 | julia | realerr | parse | 1 | RSS above loaded after 6 passes | 22.8 | 19.3 | MB |
| 1.18 | json | real | parse | 1 | peak RSS after 6 passes | 23.4 | 19.7 | MB |
| 1.18 | typescript | realerr | walk minus parse | all | instructions min | 23.6 | 19.9 | M |
| 1.18 | json | real | parse | 1 | steady peak footprint | 23.3 | 19.7 | MB |
| 1.18 | json | real | parse | 1 | steady peak RSS | 23.3 | 19.7 | MB |
| 1.18 | julia | real | parse | 1 | peak RSS after 6 passes | 28.4 | 24 | MB |
| 1.18 | julia | real | parse | 1 | peak footprint after 6 passes | 28.4 | 24 | MB |
| 1.18 | julia | real | parse | 1 | steady peak footprint | 28.4 | 24 | MB |
| 1.18 | julia | real | parse | 1 | steady peak RSS | 28.4 | 24 | MB |
| 1.18 | tsx | real | parse | all | peak RSS after 6 passes | 41.6 | 35.3 | MB |
| 1.18 | tsx | real | parse | all | peak footprint after 6 passes | 41.6 | 35.3 | MB |
| 1.18 | zig | broken | parse | 1 | peak RSS after 6 passes | 22.2 | 18.8 | MB |
| 1.18 | zig | broken | parse | 1 | peak footprint after 6 passes | 22.2 | 18.8 | MB |
| 1.18 | java | broken | walk minus parse | 1 | CPU user+sys min | 21.5 | 18.2 | ms |
| 1.18 | tsx | real | walk | all | peak footprint after 6 passes | 42.3 | 35.9 | MB |
| 1.18 | tsx | real | walk | all | peak RSS after 6 passes | 42.3 | 35.9 | MB |
| 1.18 | csharp | real | walk minus parse | all | CPU user+sys min | 83.9 | 71.3 | ms |
| 1.18 | java | real | walk minus parse | 1 | CPU user+sys min | 37.3 | 31.7 | ms |
| 1.17 | hcl | real | parse | 1 | steady RSS above loaded | 7.48 | 6.36 | MB |
| 1.17 | golang | realerr | walk | 1 | peak footprint after 6 passes | 14.3 | 12.2 | MB |
| 1.17 | kotlin | realerr | cursor | all | RSS above loaded after 6 passes | 28 | 23.8 | MB |
| 1.17 | ruby | broken | parse | 1 | peak RSS after 6 passes | 80.5 | 68.5 | MB |
| 1.17 | ruby | broken | parse | 1 | peak footprint after 6 passes | 80.5 | 68.5 | MB |
| 1.17 | julia | realerr | walk | 1 | peak footprint after 6 passes | 26.6 | 22.6 | MB |
| 1.17 | julia | realerr | walk | 1 | peak RSS after 6 passes | 26.6 | 22.6 | MB |
| 1.17 | golang | realerr | walk | 1 | peak RSS after 6 passes | 14.3 | 12.2 | MB |
| 1.17 | json | real | parse | 1 | RSS above loaded after 6 passes | 6.73 | 5.74 | MB |
| 1.17 | zig | broken | parse | 1 | steady RSS above loaded | 9.29 | 7.92 | MB |
| 1.17 | hcl | real | walk minus parse | all | instructions min | 432 | 369 | M |
| 1.17 | typescript | realerr | cursor minus parse | all | CPU user+sys min | 2 | 1.71 | ms |
| 1.17 | julia | real | walk | 1 | peak RSS after 6 passes | 28.2 | 24.1 | MB |
| 1.17 | julia | real | walk | 1 | peak footprint after 6 passes | 28.2 | 24.1 | MB |
| 1.17 | ruby | broken | parse | 1 | steady RSS above loaded | 74.1 | 63.4 | MB |
| 1.17 | julia | realerr | parse | 1 | steady RSS above loaded | 22.5 | 19.2 | MB |
| 1.17 | scala | realerr | walk minus parse | 1 | CPU user+sys min | 4.71 | 4.03 | ms |
| 1.17 | csharp | broken | walk minus parse | 1 | CPU user+sys min | 16 | 13.7 | ms |
| 1.17 | javascript | real | walk | 1 | peak footprint after 6 passes | 32.5 | 27.8 | MB |
| 1.17 | javascript | real | walk | 1 | peak RSS after 6 passes | 32.5 | 27.8 | MB |
| 1.17 | julia | realerr | walk | 1 | RSS above loaded after 6 passes | 22.5 | 19.3 | MB |
| 1.17 | java | real | walk minus parse | all | cycles min | 308 | 264 | M |
| 1.17 | java | real | walk minus parse | 1 | cycles min | 194 | 166 | M |
| 1.16 | java | real | walk minus parse | all | CPU user+sys min | 69.4 | 59.6 | ms |
| 1.16 | php | realerr (too small) | cursor minus parse | 1 | instructions min | 0.00548 | 0.00471 | M |
| 1.16 | lua | real | walk | 1 | peak footprint after 6 passes | 23.7 | 20.4 | MB |
| 1.16 | lua | real | walk | 1 | peak RSS after 6 passes | 23.7 | 20.4 | MB |
| 1.16 | kotlin | realerr | parse | all | steady RSS above loaded | 36.5 | 31.3 | MB |
| 1.16 | julia | real | walk minus parse | all | CPU user+sys min | 60.2 | 51.7 | ms |
| 1.16 | bash | real | walk minus parse | 1 | CPU user+sys min | 11.4 | 9.77 | ms |
| 1.16 | julia | broken | walk | 1 | RSS above loaded after 6 passes | 22.2 | 19.2 | MB |
| 1.16 | golang | real | parse | 1 | RSS above loaded after 6 passes | 12.4 | 10.7 | MB |
| 1.16 | lua | realerr (too small) | cursor | all | cycles | 0.351 | 0.303 | M |
| 1.16 | verilog | realerr | walk minus parse | all | cycles min | 556 | 480 | M |
| 1.16 | bash | real | walk minus parse | 1 | cycles min | 59.3 | 51.2 | M |
| 1.16 | java | broken | walk minus parse | 1 | cycles min | 109 | 94.5 | M |
| 1.16 | typescript | realerr | parse | all | steady peak RSS | 41.5 | 35.9 | MB |
| 1.16 | typescript | realerr | parse | all | steady peak footprint | 41.5 | 35.9 | MB |
| 1.15 | csharp | broken | walk minus parse | 1 | cycles min | 83 | 71.9 | M |
| 1.15 | julia | broken | parse | 1 | RSS above loaded after 6 passes | 22.2 | 19.3 | MB |
| 1.15 | hcl | broken | walk | 1 | RSS above loaded after 6 passes | 9.97 | 8.66 | MB |
| 1.15 | julia | broken | parse | 1 | steady RSS above loaded | 22 | 19.1 | MB |
| 1.15 | c | realerr | walk minus parse | 1 | cycles min | 238 | 207 | M |
| 1.15 | python | realerr (too small) | walk | 1 | peak footprint after 6 passes | 5.19 | 4.52 | MB |
| 1.15 | python | realerr (too small) | walk | 1 | peak RSS after 6 passes | 5.18 | 4.52 | MB |
| 1.15 | ruby | broken | parse | 1 | RSS above loaded after 6 passes | 72.6 | 63.4 | MB |
| 1.14 | julia | broken | walk minus parse | 1 | instructions min | 1000 | 877 | M |
| 1.14 | golang | real | walk | 1 | peak RSS after 6 passes | 29.4 | 25.7 | MB |
| 1.14 | golang | real | walk | 1 | peak footprint after 6 passes | 29.4 | 25.7 | MB |
| 1.14 | typescript | realerr | parse | all | RSS above loaded after 6 passes | 22 | 19.3 | MB |
| 1.14 | ruby | broken | walk minus parse | 1 | cycles min | 196 | 171 | M |
| 1.14 | php | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.016 | 0.014 | ms |
| 1.14 | c | realerr | walk minus parse | 1 | CPU user+sys min | 45.6 | 40 | ms |
| 1.14 | zig | broken | walk | 1 | peak RSS after 6 passes | 22.1 | 19.4 | MB |
| 1.14 | zig | broken | walk | 1 | peak footprint after 6 passes | 22.1 | 19.4 | MB |
| 1.14 | javascript | real | parse | 1 | RSS above loaded after 6 passes | 14.7 | 12.9 | MB |
| 1.14 | julia | realerr | walk minus parse | 1 | CPU user+sys min | 7.44 | 6.53 | ms |
| 1.14 | scala | realerr | walk minus parse | 1 | cycles min | 24.4 | 21.5 | M |
| 1.14 | zig | realerr | cursor | 1 | peak footprint after 6 passes | 15.2 | 13.4 | MB |
| 1.14 | zig | realerr | cursor | 1 | peak RSS after 6 passes | 15.2 | 13.4 | MB |
| 1.14 | lua | realerr (too small) | walk | 1 | peak footprint after 6 passes | 4.34 | 3.82 | MB |
| 1.14 | zig | real | parse | 1 | steady peak RSS | 33 | 29.1 | MB |
| 1.14 | zig | real | parse | 1 | steady peak footprint | 33 | 29.1 | MB |
| 1.13 | lua | realerr (too small) | walk | 1 | peak RSS after 6 passes | 4.34 | 3.82 | MB |
| 1.13 | kotlin | realerr | walk minus parse | all | instructions min | 29.6 | 26.1 | M |
| 1.13 | golang | real | parse | 1 | steady RSS above loaded | 12.1 | 10.6 | MB |
| 1.13 | golang | realerr | walk | 1 | RSS above loaded after 6 passes | 10 | 8.86 | MB |
| 1.13 | bash | realerr | parse | all | peak footprint after 6 passes | 38.8 | 34.2 | MB |
| 1.13 | rust | real | walk minus parse | all | CPU user+sys min | 114 | 101 | ms |
| 1.13 | bash | realerr | parse | all | peak RSS after 6 passes | 38.8 | 34.2 | MB |
| 1.13 | julia | broken | walk minus parse | all | instructions min | 1010 | 893 | M |
| 1.13 | zig | realerr | walk | 1 | peak footprint after 6 passes | 15 | 13.3 | MB |
| 1.13 | julia | realerr | walk minus parse | 1 | cycles min | 38.6 | 34.2 | M |
| 1.13 | zig | realerr | walk | 1 | peak RSS after 6 passes | 15 | 13.3 | MB |
| 1.13 | verilog | real | walk minus parse | all | cycles min | 34.5 | 30.5 | M |
| 1.13 | kotlin | realerr | walk minus parse | all | cycles min | 10.3 | 9.1 | M |
| 1.13 | javascript | real | parse | 1 | steady RSS above loaded | 14.5 | 12.9 | MB |
| 1.12 | zig | realerr | cursor | 1 | RSS above loaded after 6 passes | 11.4 | 10.1 | MB |
| 1.12 | julia | real | parse | 1 | steady RSS above loaded | 19.3 | 17.2 | MB |
| 1.12 | tsx | realerr | walk minus parse | 1 | cycles min | 6.56 | 5.85 | M |
| 1.12 | julia | real | parse | 1 | RSS above loaded after 6 passes | 19.3 | 17.2 | MB |
| 1.12 | javascript | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 1.76 | 1.57 | MB |
| 1.12 | zig | real | parse | 1 | peak RSS after 6 passes | 32.7 | 29.3 | MB |
| 1.12 | zig | real | parse | 1 | peak footprint after 6 passes | 32.7 | 29.3 | MB |
| 1.12 | python | real | walk | 1 | RSS above loaded after 6 passes | 13.3 | 11.9 | MB |
| 1.12 | ruby | broken | cursor minus parse | 1 | cycles min | 211 | 189 | M |
| 1.12 | ruby | broken | walk minus parse | 1 | CPU user+sys min | 37.5 | 33.6 | ms |
| 1.12 | python | real | walk minus parse | 1 | CPU user+sys min | 68.4 | 61.2 | ms |
| 1.12 | lua | realerr (too small) | cursor minus parse | all | instructions min | 0.0166 | 0.0148 | M |
| 1.11 | verilog | real | walk minus parse | 1 | CPU user+sys min | 3.22 | 2.89 | ms |
| 1.11 | javascript | realerr (too small) | cursor | 1 | peak footprint after 6 passes | 4.98 | 4.47 | MB |
| 1.11 | typescript | real | walk | 1 | RSS above loaded after 6 passes | 13.4 | 12 | MB |
| 1.11 | hcl | real | walk minus parse | 1 | instructions min | 432 | 388 | M |
| 1.11 | javascript | realerr (too small) | cursor | 1 | peak RSS after 6 passes | 4.98 | 4.47 | MB |
| 1.11 | julia | real | walk | 1 | RSS above loaded after 6 passes | 19.2 | 17.3 | MB |
| 1.11 | python | real | walk minus parse | 1 | cycles min | 356 | 320 | M |
| 1.11 | json | real | walk | 1 | peak footprint after 6 passes | 23.4 | 21 | MB |
| 1.11 | typescript | realerr | parse | all | steady RSS above loaded | 37 | 33.3 | MB |
| 1.11 | json | real | walk | 1 | peak RSS after 6 passes | 23.4 | 21 | MB |
| 1.11 | c | real | cursor | all | peak footprint after 6 passes | 52.7 | 47.5 | MB |
| 1.11 | c | real | cursor | all | peak RSS after 6 passes | 52.7 | 47.5 | MB |
| 1.11 | cpp | broken | walk minus parse | 1 | CPU user+sys min | 48.8 | 44 | ms |
| 1.11 | rust | realerr (too small) | walk | 1 | peak footprint after 6 passes | 5.27 | 4.74 | MB |
| 1.11 | rust | realerr (too small) | walk | 1 | peak RSS after 6 passes | 5.26 | 4.74 | MB |
| 1.11 | tsx | real | walk | all | RSS above loaded after 6 passes | 33.5 | 30.3 | MB |
| 1.11 | tsx | realerr | walk minus parse | 1 | CPU user+sys min | 1.24 | 1.12 | ms |
| 1.11 | kotlin | real | walk minus parse | 1 | CPU user+sys min | 32.2 | 29.1 | ms |
| 1.1 | zig | realerr | walk | 1 | RSS above loaded after 6 passes | 11.2 | 10.1 | MB |
| 1.1 | tsx | real | parse | all | steady peak footprint | 48.2 | 43.7 | MB |
| 1.1 | tsx | real | parse | all | steady peak RSS | 48.2 | 43.7 | MB |
| 1.1 | cpp | realerr | walk minus parse | 1 | CPU user+sys min | 58.2 | 52.9 | ms |
| 1.1 | tsx | real | parse | all | RSS above loaded after 6 passes | 32.8 | 29.8 | MB |
| 1.1 | python | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 1.91 | 1.74 | MB |
| 1.1 | rust | realerr (too small) | cursor | all | cycles | 0.581 | 0.53 | M |
| 1.1 | csharp | realerr | walk minus parse | 1 | CPU user+sys min | 10.2 | 9.28 | ms |
| 1.09 | c | real | walk minus parse | 1 | cycles min | 94.4 | 86.3 | M |
| 1.09 | cpp | broken | walk minus parse | 1 | cycles min | 253 | 231 | M |
| 1.09 | cpp | realerr | walk minus parse | 1 | cycles min | 304 | 278 | M |
| 1.09 | rust | real | walk | 1 | RSS above loaded after 6 passes | 11 | 10.1 | MB |
| 1.09 | csharp | realerr | walk minus parse | 1 | cycles min | 53 | 48.5 | M |
| 1.09 | lua | real | parse | 1 | RSS above loaded after 6 passes | 6.89 | 6.31 | MB |
| 1.09 | json | real | walk minus parse | 1 | CPU user+sys min | 43.1 | 39.5 | ms |
| 1.09 | json | real | walk minus parse | 1 | cycles min | 224 | 206 | M |
| 1.09 | kotlin | broken | walk minus parse | 1 | CPU user+sys min | 7.69 | 7.08 | ms |
| 1.09 | zig | broken | parse | 1 | RSS above loaded after 6 passes | 9.07 | 8.35 | MB |
| 1.08 | verilog | real | walk minus parse | 1 | cycles min | 16.8 | 15.5 | M |
| 1.08 | c | real | walk minus parse | 1 | CPU user+sys min | 18 | 16.6 | ms |
| 1.08 | kotlin | broken | walk minus parse | 1 | cycles min | 40.2 | 37.1 | M |
| 1.08 | ruby | broken | cursor minus parse | 1 | CPU user+sys min | 42.5 | 39.3 | ms |
| 1.08 | csharp | broken | cursor | all | peak footprint after 6 passes | 73.2 | 67.8 | MB |
| 1.08 | bash | realerr | parse | all | RSS above loaded after 6 passes | 33.6 | 31.1 | MB |
| 1.08 | csharp | broken | cursor | all | peak RSS after 6 passes | 73.2 | 67.8 | MB |
| 1.08 | php | realerr (too small) | walk | all | wall median | 0.043 | 0.0398 | ms |
| 1.08 | tsx | broken | parse | 1 | peak footprint after 6 passes | 21.5 | 20 | MB |
| 1.08 | tsx | broken | parse | 1 | peak RSS after 6 passes | 21.5 | 20 | MB |
| 1.08 | golang | real | walk minus parse | 1 | CPU user+sys min | 55.3 | 51.3 | ms |
| 1.08 | golang | real | walk minus parse | 1 | cycles min | 288 | 267 | M |
| 1.08 | json | real | parse | 1 | steady RSS above loaded | 6.25 | 5.81 | MB |
| 1.08 | kotlin | real | walk minus parse | 1 | cycles min | 167 | 156 | M |
| 1.07 | rust | realerr (too small) | cursor | 1 | peak footprint after 6 passes | 5.27 | 4.91 | MB |
| 1.07 | rust | realerr (too small) | cursor | 1 | peak RSS after 6 passes | 5.27 | 4.91 | MB |
| 1.07 | lua | realerr (too small) | cursor | 1 | peak footprint after 6 passes | 4.36 | 4.06 | MB |
| 1.07 | tsx | broken | parse | 1 | steady peak footprint | 21.5 | 20.1 | MB |
| 1.07 | lua | realerr (too small) | cursor | 1 | peak RSS after 6 passes | 4.35 | 4.06 | MB |
| 1.07 | tsx | broken | parse | 1 | steady peak RSS | 21.5 | 20.1 | MB |
| 1.07 | javascript | real | walk | 1 | RSS above loaded after 6 passes | 15 | 14 | MB |
| 1.07 | zig | real | walk | 1 | peak RSS after 6 passes | 32.9 | 30.7 | MB |
| 1.07 | zig | real | walk | 1 | peak footprint after 6 passes | 32.9 | 30.7 | MB |
| 1.07 | java | real | walk | 1 | RSS above loaded after 6 passes | 6.25 | 5.85 | MB |
| 1.07 | javascript | broken | walk minus parse | all | instructions min | 2540 | 2370 | M |
| 1.07 | zig | broken | cursor minus parse | all | CPU user+sys min | 120 | 112 | ms |
| 1.07 | zig | real | cursor minus parse | all | cycles min | 675 | 633 | M |
| 1.06 | scala | realerr | parse | all | peak footprint after 6 passes | 56.9 | 53.4 | MB |
| 1.06 | json | realerr | walk | 1 | peak footprint after 6 passes | 4.98 | 4.68 | MB |
| 1.06 | scala | realerr | parse | all | peak RSS after 6 passes | 56.9 | 53.4 | MB |
| 1.06 | zig | real | parse | 1 | steady RSS above loaded | 18.1 | 17 | MB |
| 1.06 | tsx | real | walk minus parse | 1 | cycles min | 85.7 | 80.6 | M |
| 1.06 | tsx | real | walk minus parse | 1 | CPU user+sys min | 16.4 | 15.5 | ms |
| 1.06 | json | realerr | walk | 1 | peak RSS after 6 passes | 4.97 | 4.68 | MB |
| 1.06 | julia | real | cursor minus parse | all | cycles min | 443 | 417 | M |
| 1.06 | julia | real | cursor minus parse | all | CPU user+sys min | 102 | 96.8 | ms |
| 1.06 | javascript | real | walk minus parse | 1 | CPU user+sys min | 67.1 | 63.5 | ms |
| 1.06 | lua | real | cursor | all | peak footprint after 6 passes | 81.7 | 77.3 | MB |
| 1.06 | lua | real | cursor | all | peak RSS after 6 passes | 81.7 | 77.3 | MB |
| 1.06 | javascript | real | walk minus parse | 1 | cycles min | 350 | 331 | M |
| 1.05 | rust | realerr (too small) | parse | all | cycles | 0.5 | 0.474 | M |
| 1.05 | java | realerr (too small) | walk | all | instructions | 1.11 | 1.05 | M |
| 1.05 | c | real | cursor | all | RSS above loaded after 6 passes | 43.9 | 41.7 | MB |
| 1.05 | python | realerr (too small) | walk minus parse | all | instructions min | 0.0822 | 0.0783 | M |
| 1.05 | lua | realerr (too small) | parse | all | cycles | 0.345 | 0.329 | M |
| 1.05 | verilog | realerr | walk minus parse | all | CPU user+sys min | 118 | 113 | ms |
| 1.05 | java | realerr (too small) | parse | all | instructions | 1.06 | 1.01 | M |
| 1.05 | ruby | broken | cursor | all | peak RSS after 6 passes | 313 | 299 | MB |
| 1.05 | ruby | broken | cursor | all | peak footprint after 6 passes | 313 | 299 | MB |
| 1.05 | php | realerr (too small) | parse | all | cycles | 0.141 | 0.135 | M |
| 1.05 | c | broken | walk minus parse | all | cycles min | 349 | 334 | M |
| 1.05 | rust | realerr (too small) | cursor minus parse | all | instructions min | 0.104 | 0.099 | M |
| 1.04 | zig | real | parse | 1 | RSS above loaded after 6 passes | 17.8 | 17 | MB |
| 1.04 | ruby | real | walk minus parse | 1 | cycles min | 260 | 249 | M |
| 1.04 | lua | realerr (too small) | cursor | all | wall median | 0.0797 | 0.0764 | ms |
| 1.04 | ruby | real | walk minus parse | 1 | CPU user+sys min | 50 | 48 | ms |
| 1.04 | tsx | realerr | walk minus parse | all | cycles min | 13.6 | 13 | M |
| 1.04 | lua | realerr (too small) | cursor minus parse | 1 | instructions min | 0.0154 | 0.0148 | M |
| 1.04 | csharp | broken | cursor | all | RSS above loaded after 6 passes | 64.9 | 62.4 | MB |
| 1.04 | java | realerr (too small) | cursor | all | instructions | 1.14 | 1.09 | M |
| 1.04 | tsx | real | parse | all | steady RSS above loaded | 39.9 | 38.4 | MB |
| 1.04 | bash | real | cursor | all | peak footprint after 6 passes | 51.1 | 49.2 | MB |
| 1.04 | rust | real | walk minus parse | all | cycles min | 506 | 488 | M |
| 1.04 | bash | real | cursor | all | peak RSS after 6 passes | 51 | 49.2 | MB |
| 1.04 | ruby | broken | cursor | all | RSS above loaded after 6 passes | 305 | 294 | MB |
| 1.04 | scala | realerr | parse | all | RSS above loaded after 6 passes | 51.9 | 50.1 | MB |
| 1.04 | scala | realerr | walk | all | peak footprint after 6 passes | 57 | 55 | MB |
| 1.04 | hcl | real | walk | 1 | RSS above loaded after 6 passes | 7.37 | 7.11 | MB |
| 1.04 | scala | realerr | walk | all | peak RSS after 6 passes | 57 | 55 | MB |
| 1.04 | rust | real | walk minus parse | 1 | CPU user+sys min | 64.4 | 62.2 | ms |
| 1.03 | c | realerr | walk minus parse | all | cycles min | 324 | 314 | M |
| 1.03 | hcl | real | walk minus parse | 1 | CPU user+sys min | 18 | 17.4 | ms |
| 1.03 | rust | real | walk minus parse | 1 | cycles min | 335 | 326 | M |
| 1.03 | tsx | broken | walk | 1 | peak RSS after 6 passes | 21.5 | 21 | MB |
| 1.03 | tsx | broken | walk | 1 | peak footprint after 6 passes | 21.5 | 21 | MB |
| 1.02 | java | realerr (too small) | walk minus parse | all | cycles min | 0.0345 | 0.0337 | M |
| 1.02 | csharp | real | walk minus parse | 1 | CPU user+sys min | 39.9 | 39 | ms |
| 1.02 | julia | real | walk minus parse | 1 | cycles min | 172 | 169 | M |
| 1.02 | julia | real | walk minus parse | 1 | CPU user+sys min | 32.9 | 32.3 | ms |
| 1.02 | scala | real | walk minus parse | all | CPU user+sys min | 34.8 | 34.1 | ms |
| 1.02 | csharp | real | walk minus parse | 1 | cycles min | 209 | 205 | M |
| 1.02 | scala | broken | walk minus parse | all | CPU user+sys min | 106 | 104 | ms |
| 1.01 | bash | realerr | walk | all | peak footprint after 6 passes | 38.2 | 37.6 | MB |
| 1.01 | bash | realerr | walk | all | peak RSS after 6 passes | 38.2 | 37.6 | MB |
| 1.01 | golang | real | walk | 1 | RSS above loaded after 6 passes | 12.3 | 12.2 | MB |
| 1.01 | zig | realerr | walk minus parse | 1 | cycles min | 26.1 | 25.8 | M |
| 1.01 | zig | real | cursor minus parse | all | CPU user+sys min | 156 | 155 | ms |
| 1.01 | scala | broken | walk minus parse | all | cycles min | 538 | 532 | M |
| 1.01 | bash | realerr | cursor | all | peak RSS after 6 passes | 39 | 38.6 | MB |
| 1.01 | bash | realerr | cursor | all | peak footprint after 6 passes | 39 | 38.6 | MB |
| 1.01 | lua | real | cursor | all | RSS above loaded after 6 passes | 64.9 | 64.2 | MB |
| 1.01 | verilog | realerr | walk minus parse | 1 | CPU user+sys min | 84.1 | 83.3 | ms |
| 1.01 | tsx | realerr | cursor minus parse | all | CPU user+sys min | 2.11 | 2.09 | ms |
| 1.01 | scala | realerr | cursor | all | peak footprint after 6 passes | 57.6 | 57.1 | MB |
| 1.01 | ruby | real | cursor | all | peak footprint after 6 passes | 111 | 110 | MB |
| 1.01 | scala | realerr | cursor | all | peak RSS after 6 passes | 57.6 | 57.1 | MB |
| 1.01 | ruby | real | cursor | all | peak RSS after 6 passes | 111 | 110 | MB |
| 1.01 | hcl | real | walk minus parse | 1 | cycles min | 93.7 | 93.1 | M |
| 1.01 | verilog | realerr | walk minus parse | 1 | cycles min | 439 | 437 | M |
| 1 | typescript | real | walk minus parse | 1 | cycles min | 348 | 346 | M |
| 1 | python | realerr (too small) | parse | all | instructions | 1.73 | 1.73 | M |
| 1 | ruby | broken | walk minus parse | all | CPU user+sys min | 61.6 | 61.5 | ms |

Extreme: 283 of 1016 file-metrics have a ratio above 1.00.

| ratio | grammar | item | metric | gositter | pure C -O2 | unit |
|---|---|---|---|---|---|---|
| 3.51 | ruby | rep_q_n_428283__n30000.rb | RSS | 233 | 66.3 | MB |
| 3.51 | ruby | rep_q_n_428283__n30000.rb | footprint | 233 | 66.3 | MB |
| 2.76 | hcl | rep_6c2f79__n100000.hcl | footprint | 445 | 162 | MB |
| 2.76 | hcl | rep_6c2f79__n100000.hcl | RSS | 445 | 162 | MB |
| 2.59 | verilog | pair_ebd0a6__n100000.v | footprint | 24.4 | 9.4 | MB |
| 2.59 | verilog | pair_ebd0a6__n100000.v | RSS | 24.3 | 9.4 | MB |
| 2.43 | hcl | unterminated_a_ac3879__n100000.hcl | footprint | 576 | 237 | MB |
| 2.43 | hcl | unterminated_a_ac3879__n100000.hcl | RSS | 576 | 237 | MB |
| 2.32 | bash | random_bracket_soup_7022a9__n100000.sh | footprint | 9.86 | 4.25 | MB |
| 2.31 | bash | random_bracket_soup_7022a9__n100000.sh | RSS | 9.84 | 4.25 | MB |
| 2.18 | hcl | unterminated_if_a_f41bf6__n100000.hcl | footprint | 627 | 288 | MB |
| 2.18 | hcl | unterminated_if_a_f41bf6__n100000.hcl | RSS | 627 | 288 | MB |
| 1.97 | csharp | unterminated_a_ff4b90__n30000.cs | footprint | 14.3 | 7.27 | MB |
| 1.96 | csharp | unterminated_a_ff4b90__n30000.cs | RSS | 14.3 | 7.27 | MB |
| 1.8 | hcl | deep_x_1_n_b2e152__n100000.hcl | RSS | 376 | 209 | MB |
| 1.8 | hcl | deep_x_1_n_b2e152__n100000.hcl | footprint | 376 | 209 | MB |
| 1.74 | scala | random_printable_cb9992__n100000.scala | RSS | 26.2 | 15 | MB |
| 1.74 | scala | random_printable_cb9992__n100000.scala | footprint | 26.2 | 15 | MB |
| 1.66 | csharp | unterminated_a_adca5b__n30000.cs | footprint | 18.7 | 11.3 | MB |
| 1.66 | csharp | unterminated_a_adca5b__n30000.cs | RSS | 18.7 | 11.3 | MB |
| 1.62 | c | unterminated_define_A_c611ad__n10000.c | footprint | 6.78 | 4.17 | MB |
| 1.62 | c | unterminated_define_A_c611ad__n10000.c | RSS | 6.77 | 4.17 | MB |
| 1.62 | cpp | x_n_x7_a_x5_26d2cc__n10000.cpp | RSS | 17 | 10.5 | MB |
| 1.62 | cpp | x_n_x7_a_x5_26d2cc__n10000.cpp | footprint | 17 | 10.5 | MB |
| 1.53 | php | random_printable_cb9992__n1000000.php | instr | 56800 | 37100 | M |
| 1.53 | ruby | random_bracket_soup_7022a9__n1000000.rb | RSS | 11.5 | 7.55 | MB |
| 1.53 | ruby | random_bracket_soup_7022a9__n1000000.rb | footprint | 11.5 | 7.55 | MB |
| 1.52 | golang | rep_n_b5fbe6__n10000.go | RSS | 20.7 | 13.6 | MB |
| 1.52 | golang | rep_n_b5fbe6__n10000.go | footprint | 20.7 | 13.6 | MB |
| 1.52 | typescript | random_bracket_soup_7022a9__n1000000.ts | instr | 54000 | 35600 | M |
| 1.52 | lua | rep_n_160caa__n10000.lua | footprint | 7.7 | 5.07 | MB |
| 1.51 | lua | rep_n_160caa__n10000.lua | RSS | 7.68 | 5.07 | MB |
| 1.51 | rust | unterminated_r_a_4676a0__n10000.rs | footprint | 10.6 | 7.04 | MB |
| 1.51 | tsx | random_bracket_soup_7022a9__n1000000.tsx | instr | 47200 | 31300 | M |
| 1.51 | rust | unterminated_r_a_4676a0__n10000.rs | RSS | 10.6 | 7.04 | MB |
| 1.5 | javascript | random_bracket_soup_7022a9__n1000000.js | instr | 50800 | 33700 | M |
| 1.5 | zig | unterminated_c_a_3f6b05__n100000.zig | footprint | 47.8 | 31.8 | MB |
| 1.5 | zig | unterminated_c_a_3f6b05__n100000.zig | RSS | 47.8 | 31.8 | MB |
| 1.5 | julia | random_printable_cb9992__n100000.jl | RSS | 11.1 | 7.45 | MB |
| 1.5 | julia | random_printable_cb9992__n100000.jl | footprint | 11.1 | 7.45 | MB |
| 1.49 | typescript | random_printable_cb9992__n100000.ts | footprint | 12.3 | 8.25 | MB |
| 1.49 | typescript | random_printable_cb9992__n100000.ts | RSS | 12.3 | 8.25 | MB |
| 1.48 | verilog | rep_module_n_afbc83__n100000.v | footprint | 29.1 | 19.6 | MB |
| 1.48 | verilog | rep_module_n_afbc83__n100000.v | RSS | 29.1 | 19.6 | MB |
| 1.47 | python | random_bracket_soup_7022a9__n1000000.py | instr | 162000 | 110000 | M |
| 1.45 | cpp | unterminated_define_A_c611ad__n10000.cpp | footprint | 6.41 | 4.42 | MB |
| 1.45 | cpp | unterminated_define_A_c611ad__n10000.cpp | RSS | 6.41 | 4.42 | MB |
| 1.41 | bash | random_bracket_soup_7022a9__n100000.sh | instr | 105 | 74.8 | M |
| 1.4 | c | new_two_seq_A_n_2_V_B_n_2_define_A_n_232200__n30000.c | RSS | 28.2 | 20.2 | MB |
| 1.4 | c | new_two_seq_A_n_2_V_B_n_2_define_A_n_232200__n30000.c | footprint | 28.2 | 20.2 | MB |
| 1.39 | golang | new_two_seq_A_n_2_V_B_n_2_n_a_bedb31__n30000.go | RSS | 34.2 | 24.7 | MB |
| 1.39 | golang | new_two_seq_A_n_2_V_B_n_2_n_a_bedb31__n30000.go | footprint | 34.2 | 24.7 | MB |
| 1.38 | lua | random_bracket_soup_7022a9__n1000000.lua | footprint | 8.18 | 5.93 | MB |
| 1.38 | lua | unterminated_a_1609f6__n10000.lua | footprint | 9 | 6.53 | MB |
| 1.38 | lua | random_bracket_soup_7022a9__n1000000.lua | RSS | 8.16 | 5.93 | MB |
| 1.37 | lua | unterminated_a_1609f6__n10000.lua | RSS | 8.96 | 6.53 | MB |
| 1.37 | cpp | x_n_x7_a_x5_26d2cc__n10000.cpp | instr | 155000 | 113000 | M |
| 1.37 | verilog | rep_module_n_afbc83__n30000.v | RSS | 12.1 | 8.88 | MB |
| 1.37 | verilog | rep_module_n_afbc83__n30000.v | footprint | 12.1 | 8.88 | MB |
| 1.36 | php | random_printable_cb9992__n1000000.php | RSS | 54.4 | 39.9 | MB |
| 1.36 | php | random_printable_cb9992__n1000000.php | footprint | 54.4 | 39.9 | MB |
| 1.34 | julia | rep_b0b883__n100000.jl | RSS | 27.2 | 20.3 | MB |
| 1.34 | julia | rep_b0b883__n100000.jl | footprint | 27.2 | 20.3 | MB |
| 1.33 | csharp | unterminated_a_a452e7__n100000.cs | RSS | 26.5 | 19.9 | MB |
| 1.33 | csharp | unterminated_a_a452e7__n100000.cs | footprint | 26.5 | 19.9 | MB |
| 1.33 | typescript | unterminated_a_a452e7__n100000.ts | footprint | 28.7 | 21.6 | MB |
| 1.33 | typescript | unterminated_a_a452e7__n100000.ts | RSS | 28.7 | 21.6 | MB |
| 1.33 | php | random_bracket_soup_7022a9__n1000000.php | instr | 21800 | 16400 | M |
| 1.31 | lua | broken_stmt_function_f_return_1_end_n_a7c1b1__n3000.lua | footprint | 10.6 | 8.09 | MB |
| 1.31 | c | advdeep_100000__c_100000.c | footprint | 34.5 | 26.3 | MB |
| 1.31 | lua | broken_stmt_function_f_return_1_end_n_a7c1b1__n3000.lua | RSS | 10.6 | 8.09 | MB |
| 1.31 | c | advdeep_100000__c_100000.c | RSS | 34.5 | 26.3 | MB |
| 1.31 | lua | random_printable_cb9992__n1000000.lua | footprint | 6.91 | 5.28 | MB |
| 1.31 | lua | random_printable_cb9992__n1000000.lua | RSS | 6.9 | 5.28 | MB |
| 1.3 | scala | rep_n_656002__n30000.scala | instr | 599 | 460 | M |
| 1.3 | csharp | rep_n_8ed84f__n100000.cs | instr | 893000 | 687000 | M |
| 1.29 | kotlin | unterminated_a_ac3879__n100000.kt | RSS | 40.4 | 31.2 | MB |
| 1.29 | kotlin | unterminated_a_ac3879__n100000.kt | footprint | 40.4 | 31.2 | MB |
| 1.29 | scala | rep_n_7fd5aa__n100000.scala | instr | 1980 | 1530 | M |
| 1.29 | tsx | unterminated_a_a452e7__n100000.tsx | footprint | 28.8 | 22.4 | MB |
| 1.29 | tsx | unterminated_a_a452e7__n100000.tsx | RSS | 28.8 | 22.4 | MB |
| 1.28 | javascript | unterminated_a_a452e7__n100000.js | footprint | 27.9 | 21.9 | MB |
| 1.28 | javascript | unterminated_a_a452e7__n100000.js | RSS | 27.9 | 21.9 | MB |
| 1.28 | typescript | random_bracket_soup_7022a9__n1000000.ts | footprint | 227 | 178 | MB |
| 1.28 | typescript | random_bracket_soup_7022a9__n1000000.ts | RSS | 227 | 178 | MB |
| 1.28 | javascript | block_class_A_n_31164b__n100000.js | RSS | 43.4 | 34.1 | MB |
| 1.28 | javascript | block_class_A_n_31164b__n100000.js | footprint | 43.4 | 34.1 | MB |
| 1.27 | lua | advdeep_100000__lua_100000.lua | RSS | 32.9 | 25.8 | MB |
| 1.27 | lua | advdeep_100000__lua_100000.lua | footprint | 32.9 | 25.8 | MB |
| 1.27 | bash | unterminated_EOF_na_n_f319ea__n30000.sh | RSS | 62 | 48.8 | MB |
| 1.27 | bash | unterminated_EOF_na_n_f319ea__n30000.sh | footprint | 62 | 48.8 | MB |
| 1.27 | scala | random_bracket_soup_7022a9__n10000.scala | footprint | 9.61 | 7.58 | MB |
| 1.27 | scala | random_bracket_soup_7022a9__n10000.scala | RSS | 9.6 | 7.58 | MB |
| 1.26 | kotlin | rep_n_93b953__n100000.kt | instr | 1320 | 1040 | M |
| 1.25 | javascript | random_bracket_soup_7022a9__n1000000.js | RSS | 218 | 175 | MB |
| 1.25 | javascript | random_bracket_soup_7022a9__n1000000.js | footprint | 218 | 175 | MB |
| 1.25 | golang | advdeep_100000__go_100000.go | footprint | 32.6 | 26.1 | MB |
| 1.25 | json | advdeep_100000__json_100000.json | footprint | 32.3 | 25.9 | MB |
| 1.25 | golang | advdeep_100000__go_100000.go | RSS | 32.6 | 26.1 | MB |
| 1.25 | json | advdeep_100000__json_100000.json | RSS | 32.3 | 25.9 | MB |
| 1.24 | tsx | random_printable_cb9992__n100000.tsx | RSS | 11.9 | 9.61 | MB |
| 1.24 | tsx | random_printable_cb9992__n100000.tsx | footprint | 11.9 | 9.61 | MB |
| 1.23 | tsx | random_bracket_soup_7022a9__n1000000.tsx | footprint | 217 | 176 | MB |
| 1.23 | golang | x_n_x3_a_x5_7103f9__n80000.go | RSS | 30 | 24.3 | MB |
| 1.23 | golang | x_n_x3_a_x5_7103f9__n80000.go | footprint | 30 | 24.3 | MB |
| 1.23 | tsx | random_bracket_soup_7022a9__n1000000.tsx | RSS | 217 | 176 | MB |
| 1.23 | python | pair_0dd45c__n100000.py | RSS | 32.6 | 26.5 | MB |
| 1.23 | python | pair_0dd45c__n100000.py | footprint | 32.6 | 26.5 | MB |
| 1.23 | javascript | rep_045310__n30000.js | RSS | 16.8 | 13.7 | MB |
| 1.23 | javascript | rep_045310__n30000.js | footprint | 16.8 | 13.7 | MB |
| 1.23 | hcl | unterminated_a_adca5b__n100000.hcl | RSS | 54.3 | 44.3 | MB |
| 1.23 | hcl | unterminated_a_adca5b__n100000.hcl | footprint | 54.3 | 44.3 | MB |
| 1.22 | java | rep_n_b5fbe6__n100000.java | instr | 1180 | 960 | M |
| 1.21 | scala | glr_valid_val_x_a_b_c_n_af35b2__n10000.scala | RSS | 40 | 33 | MB |
| 1.21 | scala | glr_valid_val_x_a_b_c_n_af35b2__n10000.scala | footprint | 40 | 33 | MB |
| 1.21 | tsx | rep_045310__n30000.tsx | footprint | 17.9 | 14.8 | MB |
| 1.21 | c | new_two_seq_A_n_2_V_B_n_2_define_A_n_232200__n30000.c | instr | 1300000 | 1070000 | M |
| 1.21 | c | advdeep_100000__cblk_100000.c | footprint | 31.8 | 26.3 | MB |
| 1.21 | c | advdeep_100000__cblk_100000.c | RSS | 31.7 | 26.3 | MB |
| 1.21 | tsx | rep_045310__n30000.tsx | RSS | 17.9 | 14.8 | MB |
| 1.2 | typescript | rep_n_e980df__n100000.ts | footprint | 24.6 | 20.6 | MB |
| 1.2 | zig | unterminated_a_ff4b90__n100000.zig | footprint | 28.4 | 23.8 | MB |
| 1.2 | bash | rep_6c2f79__n100000.sh | RSS | 21 | 17.5 | MB |
| 1.2 | bash | rep_6c2f79__n100000.sh | footprint | 21 | 17.5 | MB |
| 1.2 | typescript | rep_n_e980df__n100000.ts | RSS | 24.6 | 20.6 | MB |
| 1.2 | zig | unterminated_a_ff4b90__n100000.zig | RSS | 28.4 | 23.8 | MB |
| 1.19 | ruby | pair_0dd45c__n100000.rb | footprint | 34.5 | 29 | MB |
| 1.19 | ruby | pair_0dd45c__n100000.rb | RSS | 34.5 | 29 | MB |
| 1.19 | zig | broken_stmt_fn_f_void_return_1_n_7c6715__n100000.zig | instr | 55500 | 46700 | M |
| 1.19 | cpp | advdeep_100000__cpp_100000.cpp | instr | 26600 | 22400 | M |
| 1.18 | kotlin | glr_valid_val_x_a_b_c_n_837d02__n100000.kt | RSS | 259 | 220 | MB |
| 1.18 | kotlin | glr_valid_val_x_a_b_c_n_837d02__n100000.kt | footprint | 259 | 220 | MB |
| 1.18 | kotlin | random_printable_cb9992__n1000000.kt | RSS | 9.79 | 8.32 | MB |
| 1.18 | kotlin | random_printable_cb9992__n1000000.kt | footprint | 9.79 | 8.32 | MB |
| 1.17 | scala | broken_stmt_val_x_f_1_n_46f8f5__n3000.scala | footprint | 13 | 11.1 | MB |
| 1.17 | scala | broken_stmt_val_x_f_1_n_46f8f5__n3000.scala | RSS | 13 | 11.1 | MB |
| 1.16 | bash | advdeep_100000__sh_100000.sh | RSS | 53.7 | 46.2 | MB |
| 1.16 | bash | advdeep_100000__sh_100000.sh | footprint | 53.7 | 46.2 | MB |
| 1.16 | kotlin | random_bracket_soup_7022a9__n1000000.kt | footprint | 7.28 | 6.26 | MB |
| 1.16 | kotlin | random_bracket_soup_7022a9__n1000000.kt | RSS | 7.28 | 6.26 | MB |
| 1.15 | golang | rep_n_b5fbe6__n10000.go | instr | 574000 | 497000 | M |
| 1.15 | julia | random_bracket_soup_7022a9__n100000.jl | RSS | 15.1 | 13.1 | MB |
| 1.15 | julia | random_bracket_soup_7022a9__n100000.jl | footprint | 15.1 | 13.1 | MB |
| 1.15 | python | unterminated_f_a_7130d5__n100000.py | RSS | 79.1 | 68.8 | MB |
| 1.15 | python | unterminated_f_a_7130d5__n100000.py | footprint | 79.1 | 68.8 | MB |
| 1.15 | cpp | advdeep_100000__cpp_100000.cpp | RSS | 722 | 628 | MB |
| 1.15 | cpp | advdeep_100000__cpp_100000.cpp | footprint | 722 | 628 | MB |
| 1.15 | csharp | rep_n_8ed84f__n100000.cs | footprint | 56.3 | 49 | MB |
| 1.15 | csharp | rep_n_8ed84f__n100000.cs | RSS | 56.3 | 49 | MB |
| 1.15 | verilog | random_bracket_soup_7022a9__n1000000.v | instr | 8850 | 7700 | M |
| 1.15 | golang | new_two_seq_A_n_2_V_B_n_2_n_a_bedb31__n30000.go | instr | 1290000 | 1130000 | M |
| 1.15 | ruby | unterminated_a_1942ab__n30000.rb | footprint | 30 | 26.2 | MB |
| 1.15 | kotlin | glr_valid_val_y_a_b_c_n_fa3a79__n100000.kt | RSS | 253 | 221 | MB |
| 1.15 | kotlin | glr_valid_val_y_a_b_c_n_fa3a79__n100000.kt | footprint | 253 | 221 | MB |
| 1.14 | ruby | unterminated_a_1942ab__n30000.rb | RSS | 30 | 26.2 | MB |
| 1.14 | rust | random_printable_cb9992__n1000000.rs | footprint | 30 | 26.4 | MB |
| 1.14 | rust | random_printable_cb9992__n1000000.rs | RSS | 30 | 26.4 | MB |
| 1.14 | java | random_bracket_soup_7022a9__n1000000.java | footprint | 63.4 | 55.7 | MB |
| 1.14 | java | random_bracket_soup_7022a9__n1000000.java | RSS | 63.4 | 55.7 | MB |
| 1.14 | ruby | unterminated_a_1942ab__n100000.rb | footprint | 86.6 | 76.3 | MB |
| 1.14 | ruby | unterminated_a_1942ab__n100000.rb | RSS | 86.6 | 76.3 | MB |
| 1.13 | rust | block_x_n_756d62__n100000.rs | instr | 13500 | 12000 | M |
| 1.12 | c | glr_valid_x_a_b_n_31270c__n100000.c | RSS | 106 | 94.8 | MB |
| 1.12 | c | glr_valid_x_a_b_n_31270c__n100000.c | footprint | 106 | 94.8 | MB |
| 1.12 | scala | block_if_x_n_412d56__n10000.scala | instr | 724 | 647 | M |
| 1.12 | kotlin | random_bytes_697b59__n1000000.kt | footprint | 6.89 | 6.17 | MB |
| 1.12 | kotlin | random_bytes_697b59__n1000000.kt | RSS | 6.89 | 6.17 | MB |
| 1.11 | scala | rep_n_0ca2f1__n100000.scala | footprint | 70.1 | 63.1 | MB |
| 1.11 | scala | rep_n_0ca2f1__n100000.scala | RSS | 70.1 | 63.1 | MB |
| 1.11 | cpp | glr_valid_void_f_a_b_c_d_e_f_g_h_n_8e9ccf__n100000.cpp | footprint | 487 | 439 | MB |
| 1.11 | cpp | glr_valid_void_f_a_b_c_d_e_f_g_h_n_8e9ccf__n100000.cpp | RSS | 487 | 439 | MB |
| 1.11 | julia | deep_x_1_n_be5de1__n100000.jl | footprint | 128 | 116 | MB |
| 1.11 | julia | deep_x_1_n_be5de1__n100000.jl | RSS | 128 | 116 | MB |
| 1.11 | php | rep_EOT_fa7752__n100000.php | RSS | 80.5 | 72.8 | MB |
| 1.11 | php | rep_EOT_fa7752__n100000.php | footprint | 80.5 | 72.8 | MB |
| 1.1 | kotlin | broken_stmt_val_x_f_1_n_46f8f5__n10000.kt | footprint | 23.3 | 21.1 | MB |
| 1.1 | kotlin | broken_stmt_val_x_f_1_n_46f8f5__n10000.kt | RSS | 23.3 | 21.1 | MB |
| 1.1 | cpp | big5mb__big5mb.cpp | RSS | 271 | 246 | MB |
| 1.1 | cpp | big5mb__big5mb.cpp | footprint | 271 | 246 | MB |
| 1.09 | scala | random_bytes_697b59__n100000.scala | footprint | 15.6 | 14.3 | MB |
| 1.09 | scala | random_bytes_697b59__n100000.scala | RSS | 15.6 | 14.3 | MB |
| 1.09 | json | new_rnd_A_B_C_D_a_b_efebf3__n30000.json | instr | 1940 | 1770 | M |
| 1.09 | scala | rep_n_656002__n30000.scala | footprint | 32.1 | 29.5 | MB |
| 1.09 | scala | rep_n_656002__n30000.scala | RSS | 32.1 | 29.5 | MB |
| 1.09 | java | block_for_n_eb9345__n100000.java | instr | 2520 | 2320 | M |
| 1.09 | golang | unterminated_a_266e40__n30000.go | footprint | 21.9 | 20.1 | MB |
| 1.08 | golang | unterminated_a_266e40__n30000.go | RSS | 21.8 | 20.1 | MB |
| 1.08 | php | deep_x_a_1_n_0eb324__n100000.php | footprint | 201 | 186 | MB |
| 1.08 | php | deep_x_a_1_n_0eb324__n100000.php | RSS | 201 | 186 | MB |
| 1.08 | java | advdeep_100000__java_100000.java | footprint | 39.5 | 36.5 | MB |
| 1.08 | python | advdeep_100000__py_100000.py | footprint | 40.1 | 37.1 | MB |
| 1.08 | csharp | broken_stmt_var_x_new_A_1_n_82c71b__n100000.cs | RSS | 262 | 242 | MB |
| 1.08 | csharp | broken_stmt_var_x_new_A_1_n_82c71b__n100000.cs | footprint | 262 | 242 | MB |
| 1.08 | python | advdeep_100000__py_100000.py | RSS | 40.1 | 37.1 | MB |
| 1.08 | java | advdeep_100000__java_100000.java | RSS | 39.4 | 36.5 | MB |
| 1.08 | rust | advdeep_100000__rs_100000.rs | footprint | 40 | 37 | MB |
| 1.08 | rust | advdeep_100000__rs_100000.rs | RSS | 40 | 37 | MB |
| 1.08 | javascript | glr_valid_a_b_c_n_d20807__n100000.js | footprint | 135 | 125 | MB |
| 1.08 | javascript | glr_valid_a_b_c_n_d20807__n100000.js | RSS | 135 | 125 | MB |
| 1.08 | scala | rep_n_59d967__n100000.scala | RSS | 57.7 | 53.5 | MB |
| 1.08 | scala | rep_n_59d967__n100000.scala | footprint | 57.7 | 53.5 | MB |
| 1.08 | golang | x_n_x3_a_x5_7103f9__n80000.go | instr | 799000 | 743000 | M |
| 1.07 | csharp | glr_valid_x_a_b_c_n_003570__n100000.cs | RSS | 324 | 301 | MB |
| 1.07 | csharp | glr_valid_x_a_b_c_n_003570__n100000.cs | footprint | 324 | 301 | MB |
| 1.07 | kotlin | rep_n_93b953__n100000.kt | wall | 66.1 | 61.5 | ms |
| 1.07 | java | unterminated_a_266e40__n30000.java | footprint | 25.4 | 23.7 | MB |
| 1.07 | java | unterminated_a_266e40__n30000.java | RSS | 25.4 | 23.7 | MB |
| 1.07 | csharp | random_bracket_soup_7022a9__n100000.cs | RSS | 21.5 | 20 | MB |
| 1.07 | csharp | random_bracket_soup_7022a9__n100000.cs | footprint | 21.5 | 20 | MB |
| 1.07 | typescript | advdeep_100000__ts_100000.ts | RSS | 40.5 | 37.8 | MB |
| 1.07 | typescript | advdeep_100000__ts_100000.ts | footprint | 40.5 | 37.8 | MB |
| 1.07 | java | glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n100000.java | footprint | 528 | 492 | MB |
| 1.07 | java | glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n100000.java | RSS | 528 | 492 | MB |
| 1.07 | javascript | advdeep_100000__js_100000.js | RSS | 39.3 | 36.8 | MB |
| 1.07 | javascript | advdeep_100000__js_100000.js | footprint | 39.3 | 36.8 | MB |
| 1.07 | php | unterminated_a_266e40__n30000.php | footprint | 29 | 27.2 | MB |
| 1.06 | php | unterminated_a_266e40__n30000.php | RSS | 28.9 | 27.2 | MB |
| 1.06 | cpp | x_n_x7_a_x5_26d2cc__n10000.cpp | wall | 6960 | 6550 | ms |
| 1.06 | verilog | unterminated_a_7f7cb3__n100000.v | footprint | 66.5 | 62.8 | MB |
| 1.06 | verilog | unterminated_a_7f7cb3__n100000.v | RSS | 66.5 | 62.8 | MB |
| 1.06 | java | pair_b94783__n10000.java | instr | 1550 | 1470 | M |
| 1.05 | java | pair_b94783__n100000.java | instr | 15400 | 14700 | M |
| 1.05 | scala | rep_n_656002__n30000.scala | wall | 30 | 28.7 | ms |
| 1.04 | python | glr_valid_print_a_for_a_in_b_n_0a52da__n100000.py | footprint | 179 | 172 | MB |
| 1.04 | python | glr_valid_print_a_for_a_in_b_n_0a52da__n100000.py | RSS | 179 | 172 | MB |
| 1.04 | java | big5mb__big5mb.java | footprint | 291 | 279 | MB |
| 1.04 | java | big5mb__big5mb.java | RSS | 291 | 279 | MB |
| 1.04 | golang | broken_stmt_func_f_return_1_n_n_5b0a65__n100000.go | RSS | 144 | 139 | MB |
| 1.04 | golang | broken_stmt_func_f_return_1_n_n_5b0a65__n100000.go | footprint | 144 | 139 | MB |
| 1.04 | php | random_bracket_soup_7022a9__n1000000.php | footprint | 182 | 176 | MB |
| 1.04 | php | random_bracket_soup_7022a9__n1000000.php | RSS | 182 | 176 | MB |
| 1.03 | rust | big5mb__big5mb.rs | RSS | 307 | 297 | MB |
| 1.03 | rust | big5mb__big5mb.rs | footprint | 307 | 297 | MB |
| 1.03 | rust | random_bytes_697b59__n1000000.rs | footprint | 102 | 98.8 | MB |
| 1.03 | rust | random_bytes_697b59__n1000000.rs | RSS | 102 | 98.8 | MB |
| 1.03 | verilog | broken_stmt_module_m_assign_x_a_endmodul_5e9445__n30000.v | RSS | 84.6 | 82.3 | MB |
| 1.03 | verilog | broken_stmt_module_m_assign_x_a_endmodul_5e9445__n30000.v | footprint | 84.6 | 82.3 | MB |
| 1.03 | golang | big5mb__big5mb.go | footprint | 306 | 298 | MB |
| 1.03 | golang | big5mb__big5mb.go | RSS | 306 | 298 | MB |
| 1.03 | c | big5mb__big5mb.c | RSS | 313 | 305 | MB |
| 1.03 | c | big5mb__big5mb.c | footprint | 313 | 305 | MB |
| 1.03 | typescript | glr_valid_x_a_b_c_d_e_f_g_h_n_f95d77__n100000.ts | RSS | 393 | 383 | MB |
| 1.03 | typescript | glr_valid_x_a_b_c_d_e_f_g_h_n_f95d77__n100000.ts | footprint | 393 | 383 | MB |
| 1.03 | bash | random_bracket_soup_7022a9__n100000.sh | wall | 5.45 | 5.31 | ms |
| 1.03 | python | big5mb__big5mb.py | RSS | 312 | 304 | MB |
| 1.03 | python | big5mb__big5mb.py | footprint | 312 | 304 | MB |
| 1.02 | java | rep_n_b5fbe6__n100000.java | footprint | 44.1 | 43 | MB |
| 1.02 | java | rep_n_b5fbe6__n100000.java | RSS | 44 | 43 | MB |
| 1.02 | javascript | big5mb__big5mb.js | footprint | 341 | 333 | MB |
| 1.02 | javascript | big5mb__big5mb.js | RSS | 341 | 333 | MB |
| 1.02 | julia | unterminated_a_716541__n10000.jl | RSS | 20.4 | 19.9 | MB |
| 1.02 | julia | unterminated_a_716541__n10000.jl | footprint | 20.4 | 19.9 | MB |
| 1.02 | cpp | glr_valid_void_f_a_b_c_d_e_f_g_h_n_8e9ccf__n100000.cpp | instr | 63500 | 62100 | M |
| 1.02 | typescript | big5mb__big5mb.ts | RSS | 335 | 328 | MB |
| 1.02 | typescript | big5mb__big5mb.ts | footprint | 335 | 328 | MB |
| 1.02 | typescript | broken_stmt_function_f_return_1_n_4bd8f0__n100000.ts | instr | 37300 | 36600 | M |
| 1.02 | verilog | rep_n_aec841__n100000.v | RSS | 54.9 | 53.8 | MB |
| 1.02 | verilog | rep_n_aec841__n100000.v | footprint | 54.9 | 53.8 | MB |
| 1.02 | ruby | advdeep_100000__rb_100000.rb | footprint | 65.8 | 64.6 | MB |
| 1.02 | ruby | advdeep_100000__rb_100000.rb | RSS | 65.8 | 64.6 | MB |
| 1.02 | ruby | glr_valid_f_a_b_do_c_d_end_n_e39762__n100000.rb | footprint | 345 | 339 | MB |
| 1.02 | ruby | glr_valid_f_a_b_do_c_d_end_n_e39762__n100000.rb | RSS | 345 | 339 | MB |
| 1.02 | tsx | broken_stmt_function_f_return_1_n_4bd8f0__n100000.tsx | instr | 37300 | 36700 | M |
| 1.02 | php | unterminated_a_122a46__n100000.php | footprint | 114 | 112 | MB |
| 1.02 | php | unterminated_a_122a46__n100000.php | RSS | 114 | 112 | MB |
| 1.02 | kotlin | rep_n_93b953__n100000.kt | RSS | 54.8 | 54 | MB |
| 1.02 | kotlin | rep_n_93b953__n100000.kt | footprint | 54.8 | 54 | MB |
| 1.01 | typescript | rep_n_aec841__n30000.ts | RSS | 31 | 30.7 | MB |
| 1.01 | typescript | rep_n_aec841__n30000.ts | footprint | 31 | 30.7 | MB |
| 1.01 | java | glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n10000.java | RSS | 52.3 | 51.8 | MB |
| 1.01 | java | glr_valid_class_A_void_f_a_b_c_d_e_f_g_h_e3ad75__n10000.java | footprint | 52.3 | 51.8 | MB |
| 1.01 | golang | glr_valid_func_f_if_a_T_x_a_b_c_n_b85f6e__n100000.go | footprint | 350 | 346 | MB |
| 1.01 | golang | glr_valid_func_f_if_a_T_x_a_b_c_n_b85f6e__n100000.go | RSS | 350 | 346 | MB |
| 1.01 | python | random_bracket_soup_7022a9__n1000000.py | footprint | 252 | 250 | MB |
| 1.01 | python | random_bracket_soup_7022a9__n1000000.py | RSS | 252 | 250 | MB |
| 1 | json | broken_stmt_1_2_3_n_6f817b__n100000.json | instr | 5640 | 5620 | M |
| 1 | scala | rep_n_7fd5aa__n100000.scala | wall | 97.3 | 97 | ms |
| 1 | scala | rep_n_7fd5aa__n100000.scala | footprint | 84.2 | 83.9 | MB |
| 1 | scala | rep_n_7fd5aa__n100000.scala | RSS | 84.2 | 83.9 | MB |
| 1 | golang | random_printable_cb9992__n1000000.go | footprint | 42.2 | 42.1 | MB |
| 1 | golang | random_printable_cb9992__n1000000.go | RSS | 42.2 | 42.1 | MB |
| 1 | json | big5mb__big5mb.json | RSS | 342 | 342 | MB |
| 1 | json | big5mb__big5mb.json | footprint | 342 | 342 | MB |

Extreme killed on both: 3 of 4 file-metrics have a ratio above 1.00.

| ratio | grammar | item | metric | gositter | pure C -O2 | unit |
|---|---|---|---|---|---|---|
| 1.69 | csharp | rep_n_aec841__n30000.cs | RSS peak at the kill | 44 | 26 | MB |
| 1.61 | c | rep_n_aec841__n30000.c | RSS peak at the kill | 37 | 23 | MB |
| 1.6 | cpp | rep_n_aec841__n30000.cpp | RSS peak at the kill | 40 | 25 | MB |

First parse: 371 of 504 file-metrics have a ratio above 1.00.

| ratio | grammar | item | metric | gositter | pure C -O2 | unit |
|---|---|---|---|---|---|---|
| 2.78 | bash | #1 1444 B | exec to exit instr kernel | 2.74 | 0.988 | M |
| 2.78 | lua | #0 2045 B | exec to exit instr kernel | 2.85 | 1.03 | M |
| 2.76 | c | #0 2050 B | exec to exit instr kernel | 2.72 | 0.983 | M |
| 2.76 | lua | #1 2857 B | exec to exit instr kernel | 2.85 | 1.03 | M |
| 2.76 | zig | #0 2021 B | exec to exit instr kernel | 2.75 | 0.996 | M |
| 2.76 | javascript | #1 2198 B | exec to exit instr kernel | 2.68 | 0.97 | M |
| 2.75 | bash | #0 2053 B | exec to exit instr kernel | 2.77 | 1.01 | M |
| 2.73 | json | #1 1464 B | exec to exit instr kernel | 2.66 | 0.976 | M |
| 2.71 | java | #0 2048 B | exec to exit instr kernel | 2.63 | 0.969 | M |
| 2.7 | golang | #1 2374 B | exec to exit instr kernel | 2.73 | 1.01 | M |
| 2.69 | tsx | #0 2041 B | exec to exit instr kernel | 2.8 | 1.04 | M |
| 2.68 | javascript | #0 2047 B | exec to exit instr kernel | 2.68 | 0.998 | M |
| 2.67 | tsx | #1 975 B | exec to exit instr kernel | 2.81 | 1.05 | M |
| 2.66 | typescript | #0 2046 B | exec to exit instr kernel | 2.8 | 1.05 | M |
| 2.66 | java | #1 2232 B | exec to exit instr kernel | 2.7 | 1.02 | M |
| 2.65 | typescript | #1 1131 B | exec to exit instr kernel | 2.77 | 1.04 | M |
| 2.65 | json | #0 2035 B | exec to exit instr kernel | 2.65 | 0.998 | M |
| 2.65 | kotlin | #1 2051 B | exec to exit instr kernel | 3.03 | 1.15 | M |
| 2.62 | c | #1 4736 B | exec to exit instr kernel | 2.78 | 1.06 | M |
| 2.62 | golang | #0 2052 B | exec to exit instr kernel | 2.63 | 1 | M |
| 2.62 | hcl | #1 924 B | exec to exit instr kernel | 2.69 | 1.03 | M |
| 2.62 | verilog | #1 1245 B | exec to exit instr kernel | 2.82 | 1.08 | M |
| 2.61 | rust | #0 2052 B | exec to exit instr kernel | 2.72 | 1.05 | M |
| 2.58 | python | #0 2052 B | exec to exit instr kernel | 2.73 | 1.06 | M |
| 2.58 | kotlin | #0 2049 B | exec to exit instr kernel | 2.89 | 1.12 | M |
| 2.56 | cpp | #1 3845 B | exec to exit instr kernel | 3.14 | 1.23 | M |
| 2.55 | rust | #1 3520 B | exec to exit instr kernel | 2.78 | 1.09 | M |
| 2.55 | python | #1 4532 B | exec to exit instr kernel | 2.83 | 1.11 | M |
| 2.51 | php | #0 2046 B | exec to exit instr kernel | 2.83 | 1.13 | M |
| 2.5 | cpp | #0 2060 B | exec to exit instr kernel | 2.78 | 1.11 | M |
| 2.49 | hcl | #0 2049 B | exec to exit instr kernel | 2.72 | 1.09 | M |
| 2.48 | php | #1 2155 B | exec to exit instr kernel | 2.76 | 1.12 | M |
| 2.46 | verilog | #0 2048 B | exec to exit instr kernel | 3.31 | 1.35 | M |
| 2.46 | ruby | #0 2047 B | exec to exit instr kernel | 2.81 | 1.14 | M |
| 2.41 | ruby | #1 1869 B | exec to exit instr kernel | 2.83 | 1.17 | M |
| 2.4 | scala | #0 2049 B | exec to exit instr kernel | 2.91 | 1.22 | M |
| 2.36 | scala | #1 2732 B | exec to exit instr kernel | 3.03 | 1.29 | M |
| 2.34 | verilog | #1 1245 B | exec to exit task-clock | 0.75 | 0.32 | ms |
| 2.3 | bash | #0 2053 B | init | 0.00891 | 0.00387 | ms |
| 2.29 | json | #1 1464 B | exec to exit task-clock | 0.71 | 0.31 | ms |
| 2.27 | java | #0 2048 B | exec to exit task-clock | 0.75 | 0.33 | ms |
| 2.26 | bash | #1 1444 B | exec to exit task-clock | 0.77 | 0.34 | ms |
| 2.26 | bash | #1 1444 B | init | 0.00837 | 0.0037 | ms |
| 2.25 | csharp | #0 2048 B | exec to exit instr kernel | 3.11 | 1.38 | M |
| 2.21 | julia | #0 2045 B | exec to exit instr kernel | 2.95 | 1.33 | M |
| 2.21 | julia | #1 3673 B | exec to exit instr kernel | 3.06 | 1.39 | M |
| 2.21 | javascript | #1 2198 B | exec to exit task-clock | 0.75 | 0.34 | ms |
| 2.2 | csharp | #1 2109 B | exec to exit instr kernel | 2.89 | 1.32 | M |
| 2.17 | verilog | #1 1245 B | exec to exit cycles user+kernel | 3.64 | 1.68 | M |
| 2.15 | zig | #1 11749 B | exec to exit instr kernel | 3.24 | 1.51 | M |
| 2.12 | json | #1 1464 B | exec to exit cycles user+kernel | 3.4 | 1.6 | M |
| 2.11 | bash | #1 1444 B | exec to exit cycles user+kernel | 3.76 | 1.78 | M |
| 2.09 | java | #0 2048 B | exec to exit cycles user+kernel | 3.56 | 1.7 | M |
| 2.08 | c | #0 2050 B | exec to exit task-clock | 0.79 | 0.38 | ms |
| 2.07 | json | #0 2035 B | exec to exit task-clock | 0.83 | 0.4 | ms |
| 2.04 | c | #0 2050 B | exec to exit cycles user+kernel | 3.9 | 1.91 | M |
| 2 | tsx | #1 975 B | exec to exit task-clock | 0.84 | 0.42 | ms |
| 2 | javascript | #1 2198 B | exec to exit cycles user+kernel | 3.51 | 1.76 | M |
| 1.97 | json | #1 1464 B | process user+sys | 0.783 | 0.397 | ms |
| 1.97 | verilog | #1 1245 B | process user+sys | 0.819 | 0.416 | ms |
| 1.93 | bash | #0 2053 B | exec to exit task-clock | 0.87 | 0.45 | ms |
| 1.93 | typescript | #1 1131 B | exec to exit task-clock | 0.83 | 0.43 | ms |
| 1.93 | bash | #1 1444 B | process user+sys | 0.833 | 0.432 | ms |
| 1.91 | csharp | #1 2109 B | exec to exit task-clock | 0.9 | 0.47 | ms |
| 1.9 | json | #0 2035 B | exec to exit cycles user+kernel | 3.95 | 2.08 | M |
| 1.89 | javascript | #0 2047 B | exec to exit task-clock | 0.85 | 0.45 | ms |
| 1.89 | java | #0 2048 B | process user+sys | 0.802 | 0.425 | ms |
| 1.89 | zig | #0 2021 B | exec to exit task-clock | 0.83 | 0.44 | ms |
| 1.87 | tsx | #1 975 B | exec to exit cycles user+kernel | 4.02 | 2.15 | M |
| 1.86 | tsx | #0 2041 B | exec to exit task-clock | 0.91 | 0.49 | ms |
| 1.85 | javascript | #1 2198 B | process user+sys | 0.816 | 0.44 | ms |
| 1.84 | csharp | #1 2109 B | init | 0.0103 | 0.00557 | ms |
| 1.84 | json | #0 2035 B | init | 0.0106 | 0.00576 | ms |
| 1.82 | typescript | #1 1131 B | exec to exit cycles user+kernel | 4.07 | 2.24 | M |
| 1.81 | bash | #0 2053 B | exec to exit cycles user+kernel | 4.21 | 2.33 | M |
| 1.81 | golang | #0 2052 B | exec to exit task-clock | 0.85 | 0.47 | ms |
| 1.8 | c | #0 2050 B | process user+sys | 0.852 | 0.474 | ms |
| 1.79 | javascript | #0 2047 B | exec to exit cycles user+kernel | 4.2 | 2.34 | M |
| 1.79 | zig | #0 2021 B | exec to exit cycles user+kernel | 4.06 | 2.26 | M |
| 1.79 | tsx | #1 975 B | process user+sys | 0.906 | 0.507 | ms |
| 1.79 | lua | #0 2045 B | exec to exit task-clock | 1 | 0.56 | ms |
| 1.78 | typescript | #0 2046 B | exec to exit task-clock | 0.89 | 0.5 | ms |
| 1.78 | csharp | #1 2109 B | exec to exit cycles user+kernel | 4.34 | 2.44 | M |
| 1.77 | cpp | #0 2060 B | init | 0.00976 | 0.00551 | ms |
| 1.77 | json | #0 2035 B | process user+sys | 0.889 | 0.502 | ms |
| 1.76 | hcl | #1 924 B | exec to exit task-clock | 0.86 | 0.49 | ms |
| 1.75 | tsx | #0 2041 B | exec to exit cycles user+kernel | 4.46 | 2.55 | M |
| 1.74 | golang | #1 2374 B | exec to exit task-clock | 0.99 | 0.57 | ms |
| 1.73 | python | #0 2052 B | init | 0.00943 | 0.00544 | ms |
| 1.73 | bash | #0 2053 B | process user+sys | 0.932 | 0.54 | ms |
| 1.72 | golang | #0 2052 B | exec to exit cycles user+kernel | 4.2 | 2.44 | M |
| 1.72 | julia | #1 3673 B | init | 0.00872 | 0.00508 | ms |
| 1.71 | lua | #1 2857 B | exec to exit task-clock | 1.01 | 0.59 | ms |
| 1.71 | verilog | #1 1245 B | init | 0.00888 | 0.00519 | ms |
| 1.7 | csharp | #0 2048 B | init | 0.00944 | 0.00555 | ms |
| 1.7 | java | #1 2232 B | exec to exit task-clock | 0.85 | 0.5 | ms |
| 1.7 | rust | #0 2052 B | exec to exit task-clock | 0.85 | 0.5 | ms |
| 1.7 | php | #0 2046 B | exec to exit task-clock | 0.95 | 0.56 | ms |
| 1.69 | lua | #0 2045 B | exec to exit cycles user+kernel | 4.91 | 2.9 | M |
| 1.69 | csharp | #1 2109 B | process user+sys | 0.967 | 0.571 | ms |
| 1.69 | verilog | #1 1245 B | exec to exit instr user+kernel | 4.35 | 2.57 | M |
| 1.69 | zig | #0 2021 B | process user+sys | 0.894 | 0.529 | ms |
| 1.69 | typescript | #1 1131 B | process user+sys | 0.888 | 0.526 | ms |
| 1.69 | javascript | #0 2047 B | process user+sys | 0.938 | 0.556 | ms |
| 1.68 | bash | #1 1444 B | exec to exit instr user+kernel | 4.53 | 2.69 | M |
| 1.68 | kotlin | #1 2051 B | init | 0.00944 | 0.00561 | ms |
| 1.68 | ruby | #0 2047 B | exec to exit task-clock | 0.84 | 0.5 | ms |
| 1.68 | typescript | #0 2046 B | exec to exit cycles user+kernel | 4.3 | 2.56 | M |
| 1.68 | golang | #0 2052 B | process user+sys | 0.943 | 0.562 | ms |
| 1.68 | tsx | #0 2041 B | process user+sys | 0.982 | 0.586 | ms |
| 1.67 | php | #1 2155 B | init | 0.0093 | 0.00556 | ms |
| 1.67 | hcl | #1 924 B | exec to exit cycles user+kernel | 4.25 | 2.55 | M |
| 1.66 | json | #1 1464 B | init | 0.00921 | 0.00554 | ms |
| 1.66 | typescript | #0 2046 B | process user+sys | 0.975 | 0.589 | ms |
| 1.65 | golang | #0 2052 B | init | 0.00906 | 0.00549 | ms |
| 1.64 | scala | #0 2049 B | init | 0.00898 | 0.00548 | ms |
| 1.64 | php | #1 2155 B | exec to exit task-clock | 0.9 | 0.55 | ms |
| 1.63 | cpp | #0 2060 B | exec to exit task-clock | 0.93 | 0.57 | ms |
| 1.63 | hcl | #0 2049 B | init | 0.00887 | 0.00544 | ms |
| 1.63 | javascript | #0 2047 B | init | 0.00952 | 0.00584 | ms |
| 1.63 | lua | #1 2857 B | exec to exit cycles user+kernel | 4.96 | 3.05 | M |
| 1.62 | hcl | #1 924 B | process user+sys | 0.939 | 0.579 | ms |
| 1.62 | verilog | #0 2048 B | init | 0.009 | 0.00555 | ms |
| 1.62 | c | #1 4736 B | init | 0.00882 | 0.00544 | ms |
| 1.62 | lua | #0 2045 B | process user+sys | 1.06 | 0.652 | ms |
| 1.62 | php | #0 2046 B | exec to exit cycles user+kernel | 4.68 | 2.89 | M |
| 1.61 | java | #1 2232 B | exec to exit cycles user+kernel | 4.14 | 2.57 | M |
| 1.61 | rust | #0 2052 B | init | 0.0089 | 0.00553 | ms |
| 1.6 | php | #0 2046 B | process user+sys | 1.03 | 0.642 | ms |
| 1.6 | cpp | #1 3845 B | init | 0.00847 | 0.0053 | ms |
| 1.6 | python | #1 4532 B | init | 0.00885 | 0.00554 | ms |
| 1.6 | kotlin | #0 2049 B | exec to exit task-clock | 0.99 | 0.62 | ms |
| 1.6 | java | #1 2232 B | peak footprint | 4.29 | 2.69 | MB |
| 1.6 | json | #1 1464 B | exec to exit instr user+kernel | 4.93 | 3.09 | M |
| 1.59 | typescript | #0 2046 B | init | 0.00936 | 0.00587 | ms |
| 1.59 | rust | #0 2052 B | exec to exit cycles user+kernel | 4.11 | 2.58 | M |
| 1.59 | java | #0 2048 B | peak footprint | 4.2 | 2.64 | MB |
| 1.59 | golang | #1 2374 B | exec to exit cycles user+kernel | 4.7 | 2.96 | M |
| 1.58 | javascript | #1 2198 B | exec to exit instr user+kernel | 4.34 | 2.74 | M |
| 1.58 | lua | #1 2857 B | process user+sys | 1.08 | 0.681 | ms |
| 1.58 | golang | #1 2374 B | init | 0.00888 | 0.00562 | ms |
| 1.58 | ruby | #1 1869 B | exec to exit task-clock | 0.9 | 0.57 | ms |
| 1.58 | php | #0 2046 B | init | 0.00909 | 0.00576 | ms |
| 1.58 | java | #1 2232 B | process user+sys | 0.929 | 0.589 | ms |
| 1.57 | ruby | #0 2047 B | exec to exit cycles user+kernel | 4.09 | 2.6 | M |
| 1.57 | hcl | #1 924 B | init | 0.00896 | 0.0057 | ms |
| 1.56 | java | #0 2048 B | exec to exit instr user+kernel | 4.21 | 2.7 | M |
| 1.56 | kotlin | #0 2049 B | init | 0.00913 | 0.00584 | ms |
| 1.56 | rust | #0 2052 B | process user+sys | 0.927 | 0.593 | ms |
| 1.56 | golang | #1 2374 B | process user+sys | 1.05 | 0.672 | ms |
| 1.56 | cpp | #0 2060 B | exec to exit cycles user+kernel | 4.57 | 2.94 | M |
| 1.55 | typescript | #1 1131 B | init | 0.00905 | 0.00582 | ms |
| 1.55 | java | #1 2232 B | peak RSS | 4.26 | 2.75 | MB |
| 1.55 | python | #0 2052 B | exec to exit task-clock | 1.02 | 0.66 | ms |
| 1.53 | lua | #0 2045 B | init | 0.00873 | 0.00569 | ms |
| 1.53 | ruby | #0 2047 B | process user+sys | 0.907 | 0.594 | ms |
| 1.53 | cpp | #0 2060 B | process user+sys | 0.997 | 0.653 | ms |
| 1.53 | tsx | #1 975 B | exec to exit instr user+kernel | 5.5 | 3.6 | M |
| 1.52 | php | #1 2155 B | exec to exit cycles user+kernel | 4.38 | 2.88 | M |
| 1.52 | kotlin | #0 2049 B | exec to exit cycles user+kernel | 4.9 | 3.22 | M |
| 1.52 | rust | #1 3520 B | exec to exit task-clock | 0.94 | 0.62 | ms |
| 1.51 | julia | #0 2045 B | init | 0.00894 | 0.00592 | ms |
| 1.51 | scala | #0 2049 B | exec to exit task-clock | 1.01 | 0.67 | ms |
| 1.51 | lua | #1 2857 B | init | 0.00819 | 0.00544 | ms |
| 1.5 | java | #0 2048 B | peak RSS | 4.16 | 2.77 | MB |
| 1.5 | kotlin | #0 2049 B | process user+sys | 1.07 | 0.712 | ms |
| 1.49 | python | #0 2052 B | peak footprint | 4.28 | 2.88 | MB |
| 1.49 | ruby | #0 2047 B | init | 0.00834 | 0.00561 | ms |
| 1.49 | c | #0 2050 B | init | 0.00829 | 0.00558 | ms |
| 1.48 | c | #0 2050 B | exec to exit instr user+kernel | 4.71 | 3.17 | M |
| 1.48 | scala | #1 2732 B | init | 0.00857 | 0.00579 | ms |
| 1.48 | csharp | #0 2048 B | exec to exit task-clock | 1.11 | 0.75 | ms |
| 1.48 | ruby | #1 1869 B | process user+sys | 0.976 | 0.66 | ms |
| 1.48 | php | #1 2155 B | process user+sys | 0.955 | 0.647 | ms |
| 1.47 | ruby | #1 1869 B | exec to exit cycles user+kernel | 4.34 | 2.95 | M |
| 1.47 | kotlin | #1 2051 B | exec to exit task-clock | 1.16 | 0.79 | ms |
| 1.47 | java | #0 2048 B | init | 0.00881 | 0.006 | ms |
| 1.46 | tsx | #0 2041 B | exec to exit instr user+kernel | 6.85 | 4.7 | M |
| 1.46 | zig | #0 2021 B | init | 0.00779 | 0.00535 | ms |
| 1.45 | bash | #0 2053 B | exec to exit instr user+kernel | 5.96 | 4.1 | M |
| 1.45 | python | #0 2052 B | process user+sys | 1.1 | 0.756 | ms |
| 1.45 | python | #0 2052 B | exec to exit cycles user+kernel | 4.98 | 3.43 | M |
| 1.45 | golang | #0 2052 B | peak footprint | 3.86 | 2.67 | MB |
| 1.45 | lua | #0 2045 B | exec to exit instr user+kernel | 9.27 | 6.42 | M |
| 1.45 | lua | #1 2857 B | peak footprint | 3.93 | 2.72 | MB |
| 1.44 | hcl | #1 924 B | peak footprint | 3.76 | 2.6 | MB |
| 1.44 | javascript | #1 2198 B | init | 0.00852 | 0.00592 | ms |
| 1.43 | golang | #1 2374 B | peak footprint | 3.89 | 2.71 | MB |
| 1.43 | javascript | #1 2198 B | peak footprint | 4.07 | 2.84 | MB |
| 1.43 | json | #1 1464 B | peak footprint | 3.57 | 2.49 | MB |
| 1.43 | python | #0 2052 B | peak RSS | 4.25 | 2.97 | MB |
| 1.43 | verilog | #0 2048 B | exec to exit task-clock | 1.27 | 0.89 | ms |
| 1.42 | tsx | #0 2041 B | init | 0.00827 | 0.00582 | ms |
| 1.42 | hcl | #0 2049 B | peak footprint | 3.8 | 2.68 | MB |
| 1.42 | c | #0 2050 B | peak footprint | 4.43 | 3.12 | MB |
| 1.42 | javascript | #0 2047 B | peak footprint | 4.13 | 2.91 | MB |
| 1.42 | rust | #1 3520 B | exec to exit cycles user+kernel | 4.56 | 3.22 | M |
| 1.42 | rust | #1 3520 B | process user+sys | 1.01 | 0.715 | ms |
| 1.41 | bash | #0 2053 B | peak footprint | 4.91 | 3.47 | MB |
| 1.41 | kotlin | #1 2051 B | process user+sys | 1.24 | 0.878 | ms |
| 1.41 | tsx | #1 975 B | init | 0.00873 | 0.00618 | ms |
| 1.41 | c | #1 4736 B | exec to exit task-clock | 1.03 | 0.73 | ms |
| 1.41 | hcl | #0 2049 B | peak RSS | 3.77 | 2.68 | MB |
| 1.4 | csharp | #0 2048 B | process user+sys | 1.17 | 0.834 | ms |
| 1.4 | kotlin | #1 2051 B | exec to exit cycles user+kernel | 5.69 | 4.06 | M |
| 1.4 | hcl | #0 2049 B | exec to exit task-clock | 1.01 | 0.72 | ms |
| 1.4 | lua | #1 2857 B | peak RSS | 3.9 | 2.78 | MB |
| 1.4 | c | #1 4736 B | peak footprint | 4.41 | 3.15 | MB |
| 1.4 | hcl | #1 924 B | peak RSS | 3.73 | 2.66 | MB |
| 1.4 | golang | #0 2052 B | peak RSS | 3.83 | 2.73 | MB |
| 1.4 | scala | #0 2049 B | process user+sys | 1.07 | 0.766 | ms |
| 1.4 | php | #0 2046 B | peak footprint | 5.02 | 3.6 | MB |
| 1.39 | lua | #1 2857 B | exec to exit instr user+kernel | 9.61 | 6.9 | M |
| 1.39 | javascript | #1 2198 B | peak RSS | 4.04 | 2.9 | MB |
| 1.39 | csharp | #0 2048 B | exec to exit cycles user+kernel | 5.45 | 3.92 | M |
| 1.39 | bash | #0 2053 B | peak RSS | 4.88 | 3.5 | MB |
| 1.39 | python | #1 4532 B | peak footprint | 4.25 | 3.06 | MB |
| 1.39 | ruby | #1 1869 B | init | 0.00759 | 0.00546 | ms |
| 1.39 | scala | #0 2049 B | exec to exit cycles user+kernel | 4.85 | 3.49 | M |
| 1.39 | typescript | #1 1131 B | exec to exit instr user+kernel | 5.85 | 4.22 | M |
| 1.38 | golang | #1 2374 B | peak RSS | 3.87 | 2.79 | MB |
| 1.38 | zig | #0 2021 B | peak footprint | 4.34 | 3.14 | MB |
| 1.38 | rust | #1 3520 B | init | 0.00845 | 0.00611 | ms |
| 1.38 | lua | #0 2045 B | peak footprint | 3.93 | 2.84 | MB |
| 1.38 | javascript | #0 2047 B | peak RSS | 4.1 | 2.98 | MB |
| 1.37 | java | #1 2232 B | init | 0.00841 | 0.00612 | ms |
| 1.37 | javascript | #0 2047 B | exec to exit instr user+kernel | 5.64 | 4.11 | M |
| 1.37 | php | #0 2046 B | peak RSS | 4.99 | 3.64 | MB |
| 1.37 | json | #0 2035 B | peak footprint | 3.5 | 2.55 | MB |
| 1.37 | golang | #0 2052 B | exec to exit instr user+kernel | 6.87 | 5.02 | M |
| 1.37 | golang | #1 2374 B | exec to exit instr user+kernel | 7.97 | 5.83 | M |
| 1.37 | verilog | #0 2048 B | exec to exit cycles user+kernel | 6.34 | 4.64 | M |
| 1.37 | c | #0 2050 B | peak RSS | 4.39 | 3.22 | MB |
| 1.36 | php | #1 2155 B | peak footprint | 4.63 | 3.4 | MB |
| 1.36 | verilog | #0 2048 B | process user+sys | 1.33 | 0.982 | ms |
| 1.35 | json | #1 1464 B | peak RSS | 3.54 | 2.61 | MB |
| 1.35 | c | #1 4736 B | peak RSS | 4.38 | 3.24 | MB |
| 1.35 | zig | #1 11749 B | init | 0.00734 | 0.00543 | ms |
| 1.35 | zig | #0 2021 B | exec to exit instr user+kernel | 5.8 | 4.3 | M |
| 1.35 | zig | #1 11749 B | peak footprint | 4.71 | 3.49 | MB |
| 1.35 | zig | #0 2021 B | peak RSS | 4.32 | 3.2 | MB |
| 1.35 | lua | #0 2045 B | peak RSS | 3.89 | 2.89 | MB |
| 1.35 | c | #1 4736 B | exec to exit cycles user+kernel | 5.11 | 3.79 | M |
| 1.34 | c | #1 4736 B | process user+sys | 1.1 | 0.823 | ms |
| 1.34 | typescript | #0 2046 B | peak footprint | 5.3 | 3.96 | MB |
| 1.34 | python | #1 4532 B | exec to exit task-clock | 1.15 | 0.86 | ms |
| 1.33 | json | #0 2035 B | exec to exit instr user+kernel | 6.67 | 5 | M |
| 1.33 | python | #1 4532 B | peak RSS | 4.23 | 3.18 | MB |
| 1.33 | hcl | #1 924 B | exec to exit instr user+kernel | 7.63 | 5.74 | M |
| 1.33 | scala | #1 2732 B | exec to exit task-clock | 1.13 | 0.85 | ms |
| 1.33 | php | #1 2155 B | peak RSS | 4.6 | 3.46 | MB |
| 1.33 | typescript | #0 2046 B | exec to exit instr user+kernel | 5.87 | 4.43 | M |
| 1.32 | rust | #1 3520 B | peak footprint | 4.9 | 3.7 | MB |
| 1.32 | typescript | #0 2046 B | peak RSS | 5.27 | 3.99 | MB |
| 1.32 | hcl | #0 2049 B | process user+sys | 1.08 | 0.822 | ms |
| 1.32 | julia | #0 2045 B | exec to exit task-clock | 1.04 | 0.79 | ms |
| 1.32 | rust | #1 3520 B | peak RSS | 4.87 | 3.7 | MB |
| 1.31 | rust | #0 2052 B | exec to exit instr user+kernel | 6.22 | 4.73 | M |
| 1.31 | rust | #0 2052 B | peak footprint | 4.86 | 3.7 | MB |
| 1.31 | hcl | #0 2049 B | exec to exit cycles user+kernel | 4.93 | 3.77 | M |
| 1.31 | bash | #1 1444 B | peak footprint | 4.76 | 3.64 | MB |
| 1.31 | zig | #1 11749 B | peak RSS | 4.68 | 3.58 | MB |
| 1.3 | json | #0 2035 B | peak RSS | 3.48 | 2.68 | MB |
| 1.29 | tsx | #1 975 B | peak footprint | 5.05 | 3.93 | MB |
| 1.28 | rust | #0 2052 B | peak RSS | 4.82 | 3.76 | MB |
| 1.28 | bash | #1 1444 B | peak RSS | 4.73 | 3.7 | MB |
| 1.28 | php | #0 2046 B | exec to exit instr user+kernel | 7.02 | 5.5 | M |
| 1.27 | tsx | #1 975 B | peak RSS | 5.02 | 3.96 | MB |
| 1.27 | scala | #1 2732 B | process user+sys | 1.2 | 0.945 | ms |
| 1.26 | python | #0 2052 B | exec to exit instr user+kernel | 8.8 | 6.96 | M |
| 1.26 | julia | #0 2045 B | exec to exit cycles user+kernel | 5.19 | 4.11 | M |
| 1.26 | julia | #0 2045 B | process user+sys | 1.11 | 0.877 | ms |
| 1.26 | tsx | #0 2041 B | peak footprint | 4.95 | 3.93 | MB |
| 1.26 | scala | #1 2732 B | exec to exit cycles user+kernel | 5.59 | 4.44 | M |
| 1.26 | cpp | #1 3845 B | peak footprint | 6.8 | 5.41 | MB |
| 1.26 | python | #1 4532 B | process user+sys | 1.21 | 0.96 | ms |
| 1.26 | tsx | #0 2041 B | parse1 | 0.234 | 0.186 | ms |
| 1.25 | tsx | #0 2041 B | init+parse1 | 0.241 | 0.193 | ms |
| 1.24 | cpp | #1 3845 B | peak RSS | 6.78 | 5.48 | MB |
| 1.24 | cpp | #1 3845 B | exec to exit task-clock | 1.26 | 1.02 | ms |
| 1.23 | python | #1 4532 B | exec to exit cycles user+kernel | 5.54 | 4.49 | M |
| 1.23 | tsx | #0 2041 B | peak RSS | 4.92 | 3.99 | MB |
| 1.23 | typescript | #1 1131 B | peak footprint | 4.97 | 4.03 | MB |
| 1.23 | csharp | #1 2109 B | exec to exit instr user+kernel | 5.44 | 4.43 | M |
| 1.22 | typescript | #1 1131 B | peak RSS | 4.94 | 4.03 | MB |
| 1.22 | java | #1 2232 B | exec to exit instr user+kernel | 6.79 | 5.54 | M |
| 1.22 | julia | #0 2045 B | peak footprint | 5.99 | 4.9 | MB |
| 1.21 | cpp | #0 2060 B | exec to exit instr user+kernel | 8.23 | 6.8 | M |
| 1.2 | cpp | #1 3845 B | process user+sys | 1.34 | 1.11 | ms |
| 1.2 | julia | #0 2045 B | peak RSS | 5.96 | 4.96 | MB |
| 1.2 | julia | #1 3673 B | peak footprint | 6.32 | 5.28 | MB |
| 1.19 | cpp | #1 3845 B | exec to exit cycles user+kernel | 6.32 | 5.3 | M |
| 1.19 | lua | #0 2045 B | exec to exit instr user | 6.43 | 5.39 | M |
| 1.19 | julia | #1 3673 B | exec to exit task-clock | 1.26 | 1.06 | ms |
| 1.19 | cpp | #0 2060 B | peak footprint | 5.32 | 4.48 | MB |
| 1.18 | verilog | #1 1245 B | peak footprint | 5.63 | 4.79 | MB |
| 1.18 | ruby | #0 2047 B | exec to exit instr user+kernel | 6.76 | 5.74 | M |
| 1.17 | julia | #1 3673 B | peak RSS | 6.29 | 5.36 | MB |
| 1.17 | php | #1 2155 B | exec to exit instr user+kernel | 7.45 | 6.35 | M |
| 1.17 | kotlin | #1 2051 B | peak footprint | 6.49 | 5.55 | MB |
| 1.17 | bash | #1 1444 B | init+parse1 | 0.123 | 0.106 | ms |
| 1.17 | cpp | #0 2060 B | peak RSS | 5.29 | 4.52 | MB |
| 1.16 | julia | #1 3673 B | process user+sys | 1.33 | 1.15 | ms |
| 1.16 | verilog | #0 2048 B | peak footprint | 8.72 | 7.52 | MB |
| 1.16 | verilog | #0 2048 B | exec to exit instr user+kernel | 11.6 | 10 | M |
| 1.16 | csharp | #0 2048 B | peak footprint | 7.36 | 6.36 | MB |
| 1.16 | lua | #1 2857 B | exec to exit instr user | 6.78 | 5.86 | M |
| 1.15 | json | #1 1464 B | init+parse1 | 0.0831 | 0.072 | ms |
| 1.15 | csharp | #0 2048 B | peak RSS | 7.33 | 6.36 | MB |
| 1.15 | verilog | #1 1245 B | init+parse1 | 0.115 | 0.1 | ms |
| 1.15 | kotlin | #1 2051 B | peak RSS | 6.47 | 5.62 | MB |
| 1.15 | kotlin | #0 2049 B | exec to exit instr user+kernel | 7.97 | 6.93 | M |
| 1.15 | ruby | #1 1869 B | exec to exit instr user+kernel | 7.2 | 6.27 | M |
| 1.15 | verilog | #0 2048 B | peak RSS | 8.69 | 7.58 | MB |
| 1.15 | julia | #1 3673 B | exec to exit cycles user+kernel | 6.31 | 5.51 | M |
| 1.15 | hcl | #0 2049 B | exec to exit instr user+kernel | 10.6 | 9.3 | M |
| 1.14 | verilog | #1 1245 B | peak RSS | 5.61 | 4.92 | MB |
| 1.14 | rust | #1 3520 B | exec to exit instr user+kernel | 7.82 | 6.87 | M |
| 1.13 | json | #1 1464 B | parse1 | 0.0749 | 0.0662 | ms |
| 1.12 | kotlin | #1 2051 B | exec to exit instr user+kernel | 8.71 | 7.77 | M |
| 1.12 | ruby | #0 2047 B | peak footprint | 4.71 | 4.2 | MB |
| 1.12 | python | #1 4532 B | exec to exit instr user+kernel | 12 | 10.8 | M |
| 1.11 | ruby | #1 1869 B | peak footprint | 4.89 | 4.4 | MB |
| 1.11 | csharp | #1 2109 B | peak footprint | 6.84 | 6.17 | MB |
| 1.11 | scala | #0 2049 B | exec to exit instr user+kernel | 9.13 | 8.25 | M |
| 1.11 | tsx | #0 2041 B | exec to exit instr user | 4.04 | 3.65 | M |
| 1.1 | ruby | #0 2047 B | peak RSS | 4.68 | 4.26 | MB |
| 1.09 | json | #0 2035 B | init+parse1 | 0.127 | 0.116 | ms |
| 1.09 | csharp | #1 2109 B | peak RSS | 6.81 | 6.23 | MB |
| 1.09 | ruby | #1 1869 B | peak RSS | 4.86 | 4.46 | MB |
| 1.08 | golang | #1 2374 B | exec to exit instr user | 5.22 | 4.81 | M |
| 1.08 | bash | #1 1444 B | parse1 | 0.11 | 0.102 | ms |
| 1.08 | scala | #1 2732 B | peak footprint | 6.69 | 6.17 | MB |
| 1.08 | verilog | #1 1245 B | parse1 | 0.103 | 0.0951 | ms |
| 1.08 | scala | #0 2049 B | peak footprint | 6.4 | 5.94 | MB |
| 1.08 | scala | #1 2732 B | peak RSS | 6.66 | 6.18 | MB |
| 1.07 | scala | #0 2049 B | peak RSS | 6.38 | 5.98 | MB |
| 1.06 | csharp | #0 2048 B | exec to exit instr user+kernel | 9.32 | 8.75 | M |
| 1.06 | kotlin | #0 2049 B | peak footprint | 5.73 | 5.38 | MB |
| 1.06 | bash | #0 2053 B | init+parse1 | 0.168 | 0.158 | ms |
| 1.06 | tsx | #1 975 B | init+parse1 | 0.155 | 0.146 | ms |
| 1.06 | kotlin | #0 2049 B | peak RSS | 5.7 | 5.38 | MB |
| 1.06 | zig | #0 2021 B | init+parse1 | 0.156 | 0.147 | ms |
| 1.06 | bash | #1 1444 B | exec to exit instr user | 1.8 | 1.7 | M |
| 1.05 | tsx | #1 975 B | exec to exit instr user | 2.69 | 2.55 | M |
| 1.05 | scala | #1 2732 B | exec to exit instr user+kernel | 11.5 | 11 | M |
| 1.05 | hcl | #1 924 B | exec to exit instr user | 4.94 | 4.71 | M |
| 1.05 | zig | #0 2021 B | parse1 | 0.148 | 0.142 | ms |
| 1.05 | golang | #1 2374 B | init+parse1 | 0.234 | 0.224 | ms |
| 1.04 | golang | #0 2052 B | exec to exit instr user | 4.19 | 4.01 | M |
| 1.04 | json | #1 1464 B | exec to exit instr user | 2.2 | 2.11 | M |
| 1.04 | json | #0 2035 B | parse1 | 0.114 | 0.11 | ms |
| 1.04 | c | #0 2050 B | init+parse1 | 0.127 | 0.123 | ms |
| 1.04 | cpp | #1 3845 B | exec to exit instr user+kernel | 12.6 | 12.1 | M |
| 1.03 | golang | #1 2374 B | parse1 | 0.226 | 0.218 | ms |
| 1.03 | julia | #0 2045 B | exec to exit instr user+kernel | 9.31 | 9.01 | M |
| 1.03 | lua | #0 2045 B | parse2 | 0.165 | 0.16 | ms |
| 1.03 | bash | #0 2053 B | exec to exit instr user | 3.19 | 3.09 | M |
| 1.03 | bash | #0 2053 B | parse1 | 0.158 | 0.154 | ms |
| 1.03 | verilog | #1 1245 B | exec to exit instr user | 1.53 | 1.49 | M |
| 1.02 | python | #0 2052 B | exec to exit instr user | 6.03 | 5.91 | M |
| 1.02 | java | #0 2048 B | init+parse1 | 0.099 | 0.097 | ms |
| 1.02 | kotlin | #1 2051 B | init+parse1 | 0.368 | 0.362 | ms |
| 1.02 | c | #0 2050 B | parse1 | 0.119 | 0.117 | ms |
| 1.02 | c | #1 4736 B | exec to exit instr user+kernel | 9.16 | 9.01 | M |
| 1.01 | tsx | #1 975 B | parse1 | 0.143 | 0.141 | ms |
| 1.01 | javascript | #0 2047 B | init+parse1 | 0.168 | 0.166 | ms |
| 1.01 | kotlin | #1 2051 B | parse1 | 0.359 | 0.356 | ms |
| 1.01 | verilog | #0 2048 B | init+parse1 | 0.437 | 0.433 | ms |
| 1.01 | lua | #1 2857 B | parse2 | 0.175 | 0.173 | ms |
| 1.01 | php | #0 2046 B | init+parse1 | 0.246 | 0.244 | ms |

Allocs: 1 of 122 file-metrics have a ratio above 1.00.

| ratio | grammar | item | metric | gositter | pure C -O2 | unit |
|---|---|---|---|---|---|---|
| 1.41 | php | realerr cold | allocations per parse | 41 | 29 |  |

Extreme files: 254 finished on both sides, 0 gositter killed and pure C -O2 finished, 9 pure C -O2 killed and gositter finished, 4 killed on both.

pure C -O2 killed, gositter finished:

- bash/random_bytes_697b59__n1000000.sh (C rc=245_no_output after 0.3 s at 51 MB; gositter 157 ms, 62.1 MB)
- golang/x_n_x3_a_x5_7103f9__n132000.go (C KILL_TIME after 120.09 s at 37 MB; gositter 105000 ms, 44.9 MB)
- json/unterminated_a_b_ceeb36__n100000.json (C KILL_TIME after 120.01 s at 62 MB; gositter 29100 ms, 87.2 MB)
- kotlin/015_stray.kt (C KILL_RSS after 28.53 s at 4098 MB; gositter 16500 ms, 3840 MB)
- kotlin/075_stray.kt (C KILL_RSS after 20.52 s at 4096 MB; gositter 13100 ms, 3830 MB)
- kotlin/128_stray.kt (C KILL_TIME after 120.12 s at 1913 MB; gositter 120000 ms, 3580 MB)
- rust/unterminated_r_a_bcb8a3__n100000.rs (C KILL_TIME after 120.09 s at 43 MB; gositter 55200 ms, 59.5 MB)
- tsx/block_type_A_n_f29830__n100000.tsx (C KILL_TIME after 120.09 s at 62 MB; gositter 56400 ms, 97.6 MB)
- verilog/block_always_posedge_c_begin_n_11163b__n100000.v (C KILL_TIME after 120.07 s at 144 MB; gositter 28000 ms, 200 MB)

killed on both sides, not compared (time cap 30/120 s, memory cap 4096 MB):

- c/rep_n_aec841__n30000.c (gositter KILL_TIME after 120.08 s at 37 MB; C KILL_TIME after 120.08 s at 23 MB)
- cpp/rep_n_aec841__n30000.cpp (gositter KILL_TIME after 120.08 s at 40 MB; C KILL_TIME after 120.08 s at 25 MB)
- csharp/rep_n_aec841__n30000.cs (gositter KILL_TIME after 120.08 s at 44 MB; C KILL_TIME after 120.08 s at 26 MB)
- kotlin/079_stray.kt (gositter KILL_RSS after 25.04 s at 4098 MB; C KILL_RSS after 42.98 s at 4103 MB)

### gositter vs pure C -O2 without the pragma

Matrix and steady-state memory: 118 of 966 config-metrics have a ratio above 1.00.

| ratio | grammar | set | what | j | metric | gositter | pure C -O2 without the pragma | unit |
|---|---|---|---|---|---|---|---|---|
| 2.36 | verilog | realerr | cursor minus parse | all | CPU user+sys min | 83.3 | 35.4 | ms |
| 2.24 | csharp | realerr | cursor minus parse | all | CPU user+sys min | 21.6 | 9.61 | ms |
| 2.19 | bash | broken | walk minus parse | all | instructions min | 828 | 378 | M |
| 1.92 | bash | broken | walk minus parse | 1 | instructions min | 732 | 381 | M |
| 1.89 | verilog | realerr | walk minus parse | all | CPU user+sys min | 118 | 62.3 | ms |
| 1.84 | verilog | realerr | cursor minus parse | all | cycles min | 366 | 199 | M |
| 1.76 | kotlin | broken | walk minus parse | all | CPU user+sys min | 10.1 | 5.71 | ms |
| 1.68 | csharp | realerr | walk minus parse | 1 | CPU user+sys min | 10.2 | 6.05 | ms |
| 1.66 | bash | realerr | walk minus parse | all | cycles min | 27.6 | 16.6 | M |
| 1.66 | csharp | realerr | walk minus parse | 1 | cycles min | 53 | 31.9 | M |
| 1.64 | c | broken | walk minus parse | all | instructions min | 1120 | 684 | M |
| 1.63 | bash | broken | walk minus parse | 1 | cycles min | 172 | 106 | M |
| 1.63 | bash | real | cursor minus parse | all | CPU user+sys min | 23.5 | 14.4 | ms |
| 1.57 | bash | broken | walk minus parse | 1 | CPU user+sys min | 33.2 | 21.2 | ms |
| 1.54 | csharp | realerr | walk minus parse | all | CPU user+sys min | 15.5 | 10.1 | ms |
| 1.52 | csharp | realerr | cursor minus parse | all | cycles min | 84.4 | 55.5 | M |
| 1.5 | kotlin | broken | walk minus parse | all | cycles min | 43.9 | 29.2 | M |
| 1.47 | verilog | realerr | walk minus parse | all | cycles min | 556 | 378 | M |
| 1.43 | bash | broken | walk minus parse | all | CPU user+sys min | 60.7 | 42.5 | ms |
| 1.4 | ruby | real | walk minus parse | all | instructions min | 1210 | 862 | M |
| 1.4 | bash | real | walk minus parse | all | instructions min | 252 | 180 | M |
| 1.4 | kotlin | realerr | walk minus parse | all | CPU user+sys min | 1.89 | 1.35 | ms |
| 1.39 | csharp | real | walk minus parse | all | instructions min | 936 | 675 | M |
| 1.38 | ruby | real | walk minus parse | 1 | instructions min | 1210 | 876 | M |
| 1.38 | c | broken | walk minus parse | 1 | instructions min | 942 | 685 | M |
| 1.37 | csharp | realerr | walk minus parse | all | instructions min | 212 | 154 | M |
| 1.37 | verilog | real | walk minus parse | all | cycles min | 34.5 | 25.2 | M |
| 1.37 | bash | real | walk minus parse | 1 | instructions min | 252 | 185 | M |
| 1.36 | bash | realerr | walk minus parse | all | instructions min | 77.5 | 57.2 | M |
| 1.35 | csharp | real | walk minus parse | 1 | instructions min | 936 | 694 | M |
| 1.34 | csharp | realerr | walk minus parse | 1 | instructions min | 211 | 158 | M |
| 1.33 | bash | broken | walk minus parse | all | cycles min | 283 | 212 | M |
| 1.33 | bash | real | walk minus parse | all | cycles min | 86.6 | 65.2 | M |
| 1.33 | csharp | broken | walk minus parse | all | instructions min | 337 | 254 | M |
| 1.32 | cpp | realerr | walk minus parse | all | instructions min | 1330 | 1010 | M |
| 1.32 | cpp | broken | walk minus parse | all | instructions min | 1200 | 907 | M |
| 1.32 | cpp | real | walk minus parse | 1 | instructions min | 411 | 312 | M |
| 1.32 | csharp | broken | walk minus parse | 1 | instructions min | 338 | 257 | M |
| 1.32 | cpp | broken | walk minus parse | 1 | instructions min | 1170 | 891 | M |
| 1.31 | csharp | realerr | walk minus parse | all | cycles min | 67.3 | 51.2 | M |
| 1.31 | ruby | broken | walk minus parse | all | instructions min | 961 | 732 | M |
| 1.31 | bash | realerr | walk minus parse | 1 | instructions min | 77.4 | 59 | M |
| 1.31 | cpp | realerr | walk minus parse | 1 | instructions min | 1330 | 1020 | M |
| 1.31 | csharp | broken | walk minus parse | all | cycles min | 124 | 94.5 | M |
| 1.31 | cpp | realerr | walk minus parse | all | CPU user+sys min | 102 | 78.2 | ms |
| 1.31 | verilog | real | walk minus parse | all | CPU user+sys min | 7.16 | 5.48 | ms |
| 1.31 | c | realerr | walk minus parse | all | instructions min | 971 | 744 | M |
| 1.3 | bash | realerr | walk minus parse | all | CPU user+sys min | 4.77 | 3.67 | ms |
| 1.29 | c | realerr | walk minus parse | 1 | instructions min | 971 | 750 | M |
| 1.29 | cpp | real | walk minus parse | all | instructions min | 411 | 318 | M |
| 1.29 | cpp | realerr | walk minus parse | 1 | cycles min | 304 | 236 | M |
| 1.29 | verilog | real | walk minus parse | all | instructions min | 72.6 | 56.5 | M |
| 1.28 | cpp | realerr | walk minus parse | 1 | CPU user+sys min | 58.2 | 45.4 | ms |
| 1.28 | c | real | walk minus parse | all | instructions min | 448 | 350 | M |
| 1.28 | cpp | realerr | walk minus parse | all | cycles min | 467 | 365 | M |
| 1.27 | c | real | walk minus parse | 1 | instructions min | 448 | 353 | M |
| 1.27 | ruby | broken | walk minus parse | 1 | instructions min | 927 | 730 | M |
| 1.27 | julia | real | walk minus parse | all | instructions min | 759 | 598 | M |
| 1.26 | bash | real | cursor minus parse | all | cycles min | 104 | 82.2 | M |
| 1.26 | julia | realerr | walk minus parse | 1 | instructions min | 151 | 120 | M |
| 1.26 | julia | real | walk minus parse | 1 | instructions min | 759 | 605 | M |
| 1.25 | verilog | real | walk minus parse | 1 | instructions min | 72.3 | 57.7 | M |
| 1.25 | kotlin | realerr | walk minus parse | all | instructions min | 29.6 | 23.7 | M |
| 1.24 | c | broken | walk minus parse | all | cycles min | 349 | 280 | M |
| 1.23 | julia | realerr | walk minus parse | all | instructions min | 152 | 123 | M |
| 1.21 | kotlin | real | walk minus parse | 1 | instructions min | 663 | 546 | M |
| 1.21 | kotlin | realerr | walk minus parse | 1 | instructions min | 29.8 | 24.7 | M |
| 1.2 | kotlin | broken | walk minus parse | 1 | instructions min | 145 | 121 | M |
| 1.2 | kotlin | real | walk minus parse | all | instructions min | 663 | 553 | M |
| 1.19 | kotlin | broken | walk minus parse | all | instructions min | 145 | 122 | M |
| 1.18 | ruby | broken | walk minus parse | all | CPU user+sys min | 61.6 | 52.2 | ms |
| 1.17 | kotlin | real | walk minus parse | all | CPU user+sys min | 51.8 | 44.2 | ms |
| 1.16 | c | realerr | walk minus parse | 1 | cycles min | 238 | 205 | M |
| 1.16 | c | realerr | walk minus parse | 1 | CPU user+sys min | 45.6 | 39.4 | ms |
| 1.14 | julia | broken | walk minus parse | 1 | instructions min | 1000 | 877 | M |
| 1.14 | bash | realerr | walk minus parse | 1 | CPU user+sys min | 3.58 | 3.13 | ms |
| 1.14 | csharp | broken | walk minus parse | all | CPU user+sys min | 22.8 | 19.9 | ms |
| 1.14 | julia | broken | walk minus parse | all | instructions min | 1010 | 887 | M |
| 1.14 | cpp | broken | walk minus parse | 1 | CPU user+sys min | 48.8 | 42.9 | ms |
| 1.14 | bash | realerr | walk minus parse | 1 | cycles min | 18.7 | 16.4 | M |
| 1.13 | cpp | broken | walk minus parse | 1 | cycles min | 253 | 225 | M |
| 1.12 | bash | broken | cursor minus parse | all | cycles min | 230 | 205 | M |
| 1.12 | bash | real | walk minus parse | 1 | cycles min | 59.3 | 53.2 | M |
| 1.11 | bash | real | walk minus parse | 1 | CPU user+sys min | 11.4 | 10.2 | ms |
| 1.11 | bash | broken | cursor minus parse | all | CPU user+sys min | 48.5 | 43.7 | ms |
| 1.11 | ruby | real | walk minus parse | 1 | cycles min | 260 | 235 | M |
| 1.11 | ruby | broken | walk minus parse | 1 | CPU user+sys min | 37.5 | 33.9 | ms |
| 1.11 | ruby | real | walk minus parse | 1 | CPU user+sys min | 50 | 45.2 | ms |
| 1.1 | ruby | broken | walk minus parse | 1 | cycles min | 196 | 177 | M |
| 1.09 | c | broken | walk minus parse | 1 | cycles min | 209 | 191 | M |
| 1.09 | verilog | real | walk minus parse | 1 | CPU user+sys min | 3.22 | 2.95 | ms |
| 1.09 | verilog | real | walk minus parse | 1 | cycles min | 16.8 | 15.4 | M |
| 1.08 | kotlin | broken | walk minus parse | 1 | cycles min | 40.2 | 37.1 | M |
| 1.08 | julia | realerr | walk minus parse | 1 | CPU user+sys min | 7.44 | 6.87 | ms |
| 1.08 | c | broken | walk minus parse | 1 | CPU user+sys min | 40.1 | 37.1 | ms |
| 1.08 | csharp | real | walk minus parse | 1 | cycles min | 209 | 193 | M |
| 1.08 | kotlin | broken | walk minus parse | 1 | CPU user+sys min | 7.69 | 7.12 | ms |
| 1.08 | c | real | walk minus parse | 1 | CPU user+sys min | 18 | 16.7 | ms |
| 1.08 | csharp | broken | walk minus parse | 1 | CPU user+sys min | 16 | 14.8 | ms |
| 1.08 | csharp | broken | walk minus parse | 1 | cycles min | 83 | 77 | M |
| 1.07 | julia | realerr | walk minus parse | 1 | cycles min | 38.6 | 36 | M |
| 1.07 | c | real | walk minus parse | 1 | cycles min | 94.4 | 88.2 | M |
| 1.07 | ruby | broken | cursor minus parse | all | cycles min | 259 | 243 | M |
| 1.07 | cpp | real | walk minus parse | 1 | CPU user+sys min | 17.9 | 16.8 | ms |
| 1.06 | csharp | real | walk minus parse | 1 | CPU user+sys min | 39.9 | 37.5 | ms |
| 1.06 | csharp | broken | cursor minus parse | all | cycles min | 170 | 160 | M |
| 1.06 | c | realerr | walk minus parse | all | cycles min | 324 | 307 | M |
| 1.06 | kotlin | realerr | walk minus parse | all | cycles min | 10.3 | 9.7 | M |
| 1.06 | cpp | real | walk minus parse | 1 | cycles min | 93.8 | 88.7 | M |
| 1.06 | cpp | real | walk minus parse | all | CPU user+sys min | 27.6 | 26.1 | ms |
| 1.05 | kotlin | real | walk minus parse | all | cycles min | 217 | 208 | M |
| 1.04 | bash | real | walk minus parse | all | CPU user+sys min | 14.5 | 13.9 | ms |
| 1.04 | cpp | real | walk minus parse | all | cycles min | 138 | 133 | M |
| 1.03 | kotlin | real | walk minus parse | 1 | cycles min | 167 | 162 | M |
| 1.02 | kotlin | real | walk minus parse | 1 | CPU user+sys min | 32.2 | 31.5 | ms |
| 1.02 | cpp | broken | cursor minus parse | all | CPU user+sys min | 101 | 98.7 | ms |
| 1.01 | ruby | broken | walk minus parse | all | cycles min | 282 | 280 | M |
| 1 | kotlin | broken | cursor minus parse | all | CPU user+sys min | 20.4 | 20.4 | ms |

### gositter vs pure C -O3

Matrix and steady-state memory: 54 of 1581 config-metrics have a ratio above 1.00.

| ratio | grammar | set | what | j | metric | gositter | pure C -O3 | unit |
|---|---|---|---|---|---|---|---|---|
| 11.6 | lua | realerr (too small) | cursor minus parse | all | cycles min | 0.0575 | 0.00497 | M |
| 7.16 | php | realerr (too small) | cursor minus parse | all | cycles min | 0.0429 | 0.00598 | M |
| 4.96 | java | realerr (too small) | cursor minus parse | all | cycles min | 0.0901 | 0.0182 | M |
| 4.06 | php | realerr (too small) | cursor minus parse | all | instructions min | 0.0287 | 0.00705 | M |
| 3.5 | rust | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.035 | 0.01 | ms |
| 3.49 | php | realerr (too small) | cursor minus parse | 1 | cycles min | 0.00607 | 0.00174 | M |
| 2.75 | python | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.022 | 0.008 | ms |
| 2.68 | php | realerr (too small) | parse | all | instructions | 0.198 | 0.0736 | M |
| 2.47 | php | realerr (too small) | cursor | all | instructions | 0.194 | 0.0787 | M |
| 2 | php | realerr (too small) | cursor | 1 | CPU user+sys | 0.018 | 0.009 | ms |
| 1.99 | zig | broken | cursor minus parse | 1 | CPU user+sys min | 52.5 | 26.5 | ms |
| 1.95 | python | realerr (too small) | cursor minus parse | all | cycles min | 0.105 | 0.0538 | M |
| 1.94 | zig | broken | cursor minus parse | 1 | cycles min | 280 | 144 | M |
| 1.86 | hcl | real | cursor minus parse | all | CPU user+sys min | 58.6 | 31.5 | ms |
| 1.8 | php | realerr (too small) | cursor | 1 | cycles | 0.0424 | 0.0236 | M |
| 1.67 | php | realerr (too small) | parse | 1 | CPU user+sys | 0.015 | 0.009 | ms |
| 1.64 | php | realerr (too small) | parse | 1 | cycles | 0.0356 | 0.0217 | M |
| 1.55 | php | realerr (too small) | cursor | all | cycles | 0.215 | 0.139 | M |
| 1.42 | python | realerr (too small) | cursor | all | instructions | 2.63 | 1.86 | M |
| 1.29 | rust | realerr (too small) | parse | all | instructions | 1.12 | 0.869 | M |
| 1.28 | php | realerr (too small) | parse | 1 | instructions | 0.0824 | 0.0644 | M |
| 1.27 | rust | realerr (too small) | cursor | all | instructions | 1.24 | 0.97 | M |
| 1.27 | php | realerr (too small) | cursor | 1 | instructions | 0.0877 | 0.0692 | M |
| 1.25 | python | realerr (too small) | cursor minus parse | 1 | cycles min | 0.0305 | 0.0244 | M |
| 1.22 | rust | realerr (too small) | cursor | all | cycles | 0.581 | 0.477 | M |
| 1.22 | julia | real | cursor minus parse | all | cycles min | 443 | 364 | M |
| 1.22 | hcl | real | cursor minus parse | all | cycles min | 243 | 200 | M |
| 1.21 | scala | real | cursor minus parse | all | CPU user+sys min | 64.7 | 53.3 | ms |
| 1.2 | python | realerr (too small) | cursor minus parse | 1 | CPU user+sys min | 0.006 | 0.005 | ms |
| 1.18 | julia | real | cursor minus parse | all | CPU user+sys min | 102 | 86.5 | ms |
| 1.17 | kotlin | real | cursor minus parse | all | CPU user+sys min | 82.8 | 71.1 | ms |
| 1.13 | scala | real | cursor minus parse | all | cycles min | 293 | 258 | M |
| 1.13 | php | realerr (too small) | cursor minus parse | 1 | instructions min | 0.00548 | 0.00485 | M |
| 1.13 | java | broken | cursor minus parse | all | CPU user+sys min | 40.6 | 36.1 | ms |
| 1.11 | java | realerr (too small) | parse | all | instructions | 1.06 | 0.952 | M |
| 1.1 | java | realerr (too small) | cursor | all | instructions | 1.14 | 1.03 | M |
| 1.1 | bash | realerr | cursor minus parse | all | cycles min | 29.7 | 27.1 | M |
| 1.09 | rust | realerr (too small) | cursor | all | wall median | 0.115 | 0.106 | ms |
| 1.07 | lua | realerr (too small) | cursor minus parse | all | instructions min | 0.0166 | 0.0155 | M |
| 1.07 | php | realerr (too small) | parse | all | cycles | 0.141 | 0.132 | M |
| 1.07 | php | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.016 | 0.015 | ms |
| 1.06 | json | realerr | cursor minus parse | 1 | cycles min | 2.28 | 2.16 | M |
| 1.05 | hcl | broken | cursor minus parse | all | cycles min | 68.3 | 65.1 | M |
| 1.04 | rust | realerr (too small) | parse | all | cycles | 0.5 | 0.479 | M |
| 1.04 | java | broken | cursor minus parse | all | cycles min | 179 | 172 | M |
| 1.04 | kotlin | real | cursor minus parse | all | cycles min | 341 | 327 | M |
| 1.04 | lua | realerr (too small) | parse | all | cycles | 0.345 | 0.333 | M |
| 1.02 | python | realerr (too small) | parse | all | instructions | 1.73 | 1.69 | M |
| 1.02 | json | realerr | cursor minus parse | 1 | CPU user+sys min | 0.432 | 0.423 | ms |
| 1.02 | rust | realerr (too small) | cursor minus parse | all | instructions min | 0.104 | 0.102 | M |
| 1.01 | lua | realerr (too small) | cursor minus parse | 1 | instructions min | 0.0154 | 0.0152 | M |
| 1.01 | java | realerr (too small) | cursor minus parse | all | instructions min | 0.0889 | 0.0881 | M |
| 1.01 | ruby | broken | cursor minus parse | 1 | cycles min | 211 | 209 | M |
| 1 | ruby | broken | cursor minus parse | 1 | CPU user+sys min | 42.5 | 42.5 | ms |

### gositter vs pure C -O2 built by clang

Matrix and steady-state memory: 38 of 1581 config-metrics have a ratio above 1.00.

| ratio | grammar | set | what | j | metric | gositter | pure C -O2 built by clang | unit |
|---|---|---|---|---|---|---|---|---|
| 8.26 | rust | realerr (too small) | cursor minus parse | all | cycles min | 0.122 | 0.0148 | M |
| 6.68 | python | realerr (too small) | cursor minus parse | 1 | cycles min | 0.0305 | 0.00457 | M |
| 6 | python | realerr (too small) | cursor minus parse | 1 | CPU user+sys min | 0.006 | 0.001 | ms |
| 5.33 | php | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.016 | 0.003 | ms |
| 3.8 | php | realerr (too small) | cursor minus parse | all | instructions min | 0.0287 | 0.00754 | M |
| 3.18 | rust | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.035 | 0.011 | ms |
| 2.95 | php | realerr (too small) | parse | all | instructions | 0.198 | 0.067 | M |
| 2.6 | php | realerr (too small) | cursor | all | instructions | 0.194 | 0.0747 | M |
| 2.18 | php | realerr (too small) | cursor minus parse | all | cycles min | 0.0429 | 0.0197 | M |
| 1.9 | php | realerr (too small) | parse | all | cycles | 0.141 | 0.0741 | M |
| 1.86 | verilog | realerr | cursor minus parse | all | CPU user+sys min | 83.3 | 44.8 | ms |
| 1.85 | java | realerr (too small) | cursor minus parse | all | cycles min | 0.0901 | 0.0487 | M |
| 1.75 | php | realerr (too small) | parse | 1 | cycles | 0.0356 | 0.0203 | M |
| 1.67 | php | realerr (too small) | parse | 1 | CPU user+sys | 0.015 | 0.009 | ms |
| 1.56 | javascript | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.014 | 0.009 | ms |
| 1.49 | php | realerr (too small) | cursor | all | cycles | 0.215 | 0.144 | M |
| 1.42 | python | realerr (too small) | cursor | all | instructions | 2.63 | 1.85 | M |
| 1.38 | php | realerr (too small) | parse | 1 | instructions | 0.0824 | 0.0598 | M |
| 1.36 | rust | realerr (too small) | parse | all | instructions | 1.12 | 0.826 | M |
| 1.35 | php | realerr (too small) | cursor | 1 | instructions | 0.0877 | 0.065 | M |
| 1.32 | rust | realerr (too small) | cursor | all | instructions | 1.24 | 0.935 | M |
| 1.3 | php | realerr (too small) | parse | all | wall median | 0.0255 | 0.0196 | ms |
| 1.23 | rust | realerr (too small) | cursor | all | cycles | 0.581 | 0.472 | M |
| 1.22 | lua | realerr (too small) | parse | all | cycles | 0.345 | 0.284 | M |
| 1.16 | lua | realerr (too small) | cursor | all | cycles | 0.351 | 0.302 | M |
| 1.13 | verilog | realerr | cursor minus parse | all | cycles min | 366 | 323 | M |
| 1.13 | rust | realerr (too small) | parse | all | cycles | 0.5 | 0.444 | M |
| 1.1 | java | realerr (too small) | parse | all | instructions | 1.06 | 0.959 | M |
| 1.09 | lua | realerr (too small) | parse | all | wall median | 0.0763 | 0.07 | ms |
| 1.08 | java | realerr (too small) | cursor | all | instructions | 1.14 | 1.05 | M |
| 1.06 | php | realerr (too small) | cursor | 1 | CPU user+sys | 0.018 | 0.017 | ms |
| 1.05 | lua | realerr (too small) | cursor minus parse | all | cycles min | 0.0575 | 0.0547 | M |
| 1.04 | php | realerr (too small) | cursor minus parse | 1 | instructions min | 0.00548 | 0.00525 | M |
| 1.03 | php | realerr (too small) | cursor | all | wall median | 0.0383 | 0.037 | ms |
| 1.03 | python | realerr (too small) | parse | all | instructions | 1.73 | 1.67 | M |
| 1.02 | rust | realerr (too small) | cursor | all | wall median | 0.115 | 0.113 | ms |
| 1.01 | lua | realerr (too small) | cursor minus parse | all | instructions min | 0.0166 | 0.0163 | M |
| 1.01 | lua | realerr (too small) | cursor | all | wall median | 0.0797 | 0.0787 | ms |

### gositter vs cgo

Matrix and steady-state memory: 146 of 4005 config-metrics have a ratio above 1.00.

| ratio | grammar | set | what | j | metric | gositter | cgo | unit |
|---|---|---|---|---|---|---|---|---|
| 3.74 | php | realerr (too small) | walk minus parse | all | cycles min | 0.00128 | 0.000344 | M |
| 2.2 | python | realerr (too small) | parse | all | steady RSS above loaded | 5.54 | 2.51 | MB |
| 2.18 | lua | realerr (too small) | parse | all | RSS above loaded after 6 passes | 2.55 | 1.17 | MB |
| 2.06 | java | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.35 | 1.62 | MB |
| 2.04 | java | realerr (too small) | parse | 1 | steady RSS above loaded | 2.01 | 0.988 | MB |
| 1.9 | lua | realerr (too small) | parse | 1 | steady RSS above loaded | 1.28 | 0.672 | MB |
| 1.9 | python | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.59 | 1.89 | MB |
| 1.88 | python | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.89 | 1.01 | MB |
| 1.84 | java | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.77 | 0.957 | MB |
| 1.84 | java | realerr (too small) | parse | all | steady RSS above loaded | 4.23 | 2.3 | MB |
| 1.83 | javascript | realerr (too small) | parse | 1 | steady RSS above loaded | 2.32 | 1.27 | MB |
| 1.79 | lua | realerr (too small) | parse | all | steady RSS above loaded | 2.78 | 1.55 | MB |
| 1.78 | lua | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 2.66 | 1.49 | MB |
| 1.73 | lua | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.22 | 0.703 | MB |
| 1.73 | lua | realerr (too small) | walk | all | RSS above loaded after 6 passes | 2.65 | 1.53 | MB |
| 1.73 | python | realerr (too small) | parse | 1 | steady RSS above loaded | 1.89 | 1.09 | MB |
| 1.68 | javascript | realerr (too small) | parse | all | RSS above loaded after 6 passes | 4.83 | 2.88 | MB |
| 1.62 | php | realerr (too small) | cursor | all | wall min pass | 0.021 | 0.0129 | ms |
| 1.6 | php | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.13 | 1.96 | MB |
| 1.59 | java | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.21 | 2.02 | MB |
| 1.59 | rust | realerr (too small) | parse | all | RSS above loaded after 6 passes | 3.42 | 2.15 | MB |
| 1.55 | python | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3.41 | 2.2 | MB |
| 1.55 | python | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.48 | 2.25 | MB |
| 1.53 | java | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3.12 | 2.04 | MB |
| 1.51 | javascript | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.75 | 1.16 | MB |
| 1.51 | javascript | realerr (too small) | walk | all | RSS above loaded after 6 passes | 4.66 | 3.09 | MB |
| 1.48 | javascript | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 4.66 | 3.14 | MB |
| 1.46 | lua | realerr (too small) | cursor minus parse | all | CPU user+sys min | 0.019 | 0.013 | ms |
| 1.45 | php | realerr (too small) | parse | all | steady RSS above loaded | 3.5 | 2.42 | MB |
| 1.44 | java | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 1.83 | 1.27 | MB |
| 1.41 | java | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 1.84 | 1.31 | MB |
| 1.4 | php | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.15 | 2.25 | MB |
| 1.39 | rust | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3.27 | 2.35 | MB |
| 1.39 | javascript | realerr (too small) | parse | all | steady RSS above loaded | 5.89 | 4.23 | MB |
| 1.39 | lua | broken | parse | 1 | RSS above loaded after 6 passes | 38.8 | 27.9 | MB |
| 1.38 | php | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 1.99 | 1.45 | MB |
| 1.38 | python | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 1.91 | 1.39 | MB |
| 1.36 | python | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 1.91 | 1.4 | MB |
| 1.36 | rust | realerr (too small) | parse | all | steady RSS above loaded | 3.54 | 2.6 | MB |
| 1.35 | php | realerr (too small) | walk | all | RSS above loaded after 6 passes | 3 | 2.21 | MB |
| 1.35 | php | realerr (too small) | parse | 1 | steady RSS above loaded | 1.96 | 1.45 | MB |
| 1.31 | rust | realerr (too small) | parse | 1 | steady RSS above loaded | 1.97 | 1.51 | MB |
| 1.29 | rust | realerr (too small) | parse | 1 | RSS above loaded after 6 passes | 2 | 1.54 | MB |
| 1.29 | rust | realerr (too small) | cursor | all | RSS above loaded after 6 passes | 3.14 | 2.43 | MB |
| 1.28 | lua | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 1.19 | 0.926 | MB |
| 1.26 | java | broken | parse | 1 | RSS above loaded after 6 passes | 48.1 | 38.1 | MB |
| 1.25 | cpp | broken | parse | 1 | RSS above loaded after 6 passes | 173 | 138 | MB |
| 1.25 | php | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 2.08 | 1.66 | MB |
| 1.25 | lua | broken | parse | 1 | peak RSS after 6 passes | 45.4 | 36.4 | MB |
| 1.25 | lua | broken | parse | 1 | peak footprint after 6 passes | 45.4 | 36.4 | MB |
| 1.23 | php | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 2.06 | 1.67 | MB |
| 1.23 | lua | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 1.18 | 0.961 | MB |
| 1.22 | cpp | broken | parse | 1 | peak RSS after 6 passes | 185 | 152 | MB |
| 1.22 | cpp | broken | parse | 1 | peak footprint after 6 passes | 185 | 152 | MB |
| 1.21 | lua | realerr (too small) | walk minus parse | all | CPU user+sys min | 0.017 | 0.014 | ms |
| 1.21 | python | realerr (too small) | parse | all | steady peak RSS | 8.8 | 7.28 | MB |
| 1.21 | python | realerr (too small) | parse | all | steady peak footprint | 8.8 | 7.28 | MB |
| 1.2 | typescript | realerr | parse | 1 | RSS above loaded after 6 passes | 7.06 | 5.9 | MB |
| 1.18 | javascript | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 1.8 | 1.52 | MB |
| 1.17 | golang | broken | parse | 1 | RSS above loaded after 6 passes | 33.9 | 29 | MB |
| 1.17 | java | broken | parse | 1 | peak footprint after 6 passes | 54.7 | 46.8 | MB |
| 1.17 | java | broken | parse | 1 | peak RSS after 6 passes | 54.6 | 46.8 | MB |
| 1.16 | javascript | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 1.76 | 1.52 | MB |
| 1.13 | javascript | broken | parse | 1 | RSS above loaded after 6 passes | 66.5 | 58.9 | MB |
| 1.13 | verilog | broken | parse | 1 | RSS above loaded after 6 passes | 35.9 | 31.9 | MB |
| 1.13 | golang | broken | parse | 1 | steady RSS above loaded | 33.3 | 29.6 | MB |
| 1.12 | python | broken | parse | 1 | RSS above loaded after 6 passes | 46 | 41.1 | MB |
| 1.11 | lua | broken | parse | 1 | steady RSS above loaded | 38.4 | 34.7 | MB |
| 1.1 | rust | realerr (too small) | walk | 1 | RSS above loaded after 6 passes | 2.03 | 1.84 | MB |
| 1.1 | php | realerr (too small) | walk | all | CPU user+sys | 0.078 | 0.071 | ms |
| 1.1 | java | broken | parse | 1 | steady RSS above loaded | 47 | 42.9 | MB |
| 1.09 | csharp | realerr | parse | 1 | RSS above loaded after 6 passes | 16.5 | 15.1 | MB |
| 1.09 | python | broken | parse | 1 | steady RSS above loaded | 45.9 | 42 | MB |
| 1.09 | golang | broken | parse | 1 | peak RSS after 6 passes | 42.5 | 39 | MB |
| 1.09 | golang | broken | parse | 1 | peak footprint after 6 passes | 42.5 | 39 | MB |
| 1.09 | javascript | realerr (too small) | parse | all | peak footprint after 6 passes | 8.27 | 7.59 | MB |
| 1.09 | javascript | broken | parse | 1 | peak footprint after 6 passes | 75.5 | 69.3 | MB |
| 1.09 | javascript | broken | parse | 1 | peak RSS after 6 passes | 75.5 | 69.3 | MB |
| 1.09 | bash | real | parse | 1 | RSS above loaded after 6 passes | 10.4 | 9.58 | MB |
| 1.09 | csharp | broken | parse | 1 | RSS above loaded after 6 passes | 14.9 | 13.7 | MB |
| 1.09 | javascript | realerr (too small) | parse | all | peak RSS after 6 passes | 8.25 | 7.59 | MB |
| 1.09 | verilog | real | parse | 1 | RSS above loaded after 6 passes | 20.7 | 19.1 | MB |
| 1.09 | ruby | broken | parse | 1 | steady RSS above loaded | 74.1 | 68.3 | MB |
| 1.08 | php | broken | parse | 1 | RSS above loaded after 6 passes | 32.2 | 29.8 | MB |
| 1.08 | kotlin | broken | parse | 1 | RSS above loaded after 6 passes | 9.86 | 9.11 | MB |
| 1.07 | c | broken | parse | 1 | RSS above loaded after 6 passes | 32.8 | 30.5 | MB |
| 1.07 | ruby | broken | parse | 1 | RSS above loaded after 6 passes | 72.6 | 67.7 | MB |
| 1.07 | typescript | broken | parse | 1 | RSS above loaded after 6 passes | 44.3 | 41.3 | MB |
| 1.07 | bash | realerr | parse | 1 | RSS above loaded after 6 passes | 10.2 | 9.59 | MB |
| 1.07 | bash | realerr | parse | 1 | steady RSS above loaded | 10.5 | 9.89 | MB |
| 1.07 | verilog | broken | parse | 1 | peak footprint after 6 passes | 44.9 | 42.1 | MB |
| 1.07 | verilog | broken | parse | 1 | peak RSS after 6 passes | 44.9 | 42.1 | MB |
| 1.07 | csharp | realerr | parse | 1 | steady RSS above loaded | 16.3 | 15.3 | MB |
| 1.07 | golang | broken | parse | 1 | steady peak RSS | 42.1 | 39.5 | MB |
| 1.07 | golang | broken | parse | 1 | steady peak footprint | 42.1 | 39.5 | MB |
| 1.07 | typescript | broken | parse | 1 | steady RSS above loaded | 44.5 | 41.7 | MB |
| 1.06 | python | broken | parse | 1 | peak RSS after 6 passes | 55.8 | 52.6 | MB |
| 1.06 | python | broken | parse | 1 | peak footprint after 6 passes | 55.8 | 52.6 | MB |
| 1.06 | java | broken | parse | 1 | steady peak footprint | 54.6 | 51.4 | MB |
| 1.06 | rust | broken | parse | 1 | RSS above loaded after 6 passes | 43.8 | 41.3 | MB |
| 1.06 | java | broken | parse | 1 | steady peak RSS | 54.6 | 51.4 | MB |
| 1.06 | rust | realerr (too small) | cursor | 1 | RSS above loaded after 6 passes | 2.03 | 1.91 | MB |
| 1.06 | ruby | broken | parse | 1 | steady peak footprint | 81.8 | 77.3 | MB |
| 1.06 | ruby | broken | parse | 1 | steady peak RSS | 81.8 | 77.3 | MB |
| 1.06 | verilog | realerr | parse | 1 | RSS above loaded after 6 passes | 35.1 | 33.3 | MB |
| 1.05 | java | realerr (too small) | parse | all | steady peak footprint | 7.52 | 7.13 | MB |
| 1.05 | verilog | real | parse | 1 | steady RSS above loaded | 20.5 | 19.4 | MB |
| 1.05 | java | realerr (too small) | parse | all | steady peak RSS | 7.49 | 7.13 | MB |
| 1.05 | ruby | broken | parse | 1 | peak RSS after 6 passes | 80.5 | 76.7 | MB |
| 1.05 | ruby | broken | parse | 1 | peak footprint after 6 passes | 80.5 | 76.7 | MB |
| 1.05 | rust | broken | parse | 1 | steady RSS above loaded | 43.6 | 41.6 | MB |
| 1.05 | python | broken | parse | 1 | steady peak RSS | 55.8 | 53.2 | MB |
| 1.05 | python | broken | parse | 1 | steady peak footprint | 55.8 | 53.2 | MB |
| 1.05 | verilog | realerr | parse | 1 | steady RSS above loaded | 35 | 33.4 | MB |
| 1.05 | lua | broken | parse | 1 | steady peak RSS | 45.3 | 43.2 | MB |
| 1.05 | lua | broken | parse | 1 | steady peak footprint | 45.3 | 43.2 | MB |
| 1.05 | python | realerr (too small) | parse | all | peak footprint after 6 passes | 6.88 | 6.58 | MB |
| 1.04 | java | realerr (too small) | parse | all | peak footprint after 6 passes | 6.64 | 6.36 | MB |
| 1.04 | javascript | realerr (too small) | parse | all | steady peak footprint | 9.33 | 8.94 | MB |
| 1.04 | python | realerr (too small) | parse | all | peak RSS after 6 passes | 6.85 | 6.58 | MB |
| 1.04 | javascript | realerr (too small) | parse | all | steady peak RSS | 9.3 | 8.94 | MB |
| 1.04 | java | realerr (too small) | parse | all | peak RSS after 6 passes | 6.61 | 6.36 | MB |
| 1.04 | c | broken | parse | 1 | steady RSS above loaded | 32.3 | 31 | MB |
| 1.04 | typescript | broken | parse | 1 | peak RSS after 6 passes | 53 | 51.1 | MB |
| 1.04 | typescript | broken | parse | 1 | peak footprint after 6 passes | 53 | 51.1 | MB |
| 1.03 | kotlin | broken | parse | 1 | steady RSS above loaded | 9.77 | 9.46 | MB |
| 1.03 | php | broken | parse | 1 | peak RSS after 6 passes | 40.8 | 39.7 | MB |
| 1.03 | php | broken | parse | 1 | peak footprint after 6 passes | 40.8 | 39.7 | MB |
| 1.03 | typescript | broken | parse | 1 | steady peak RSS | 53 | 51.6 | MB |
| 1.03 | typescript | broken | parse | 1 | steady peak footprint | 53 | 51.6 | MB |
| 1.03 | rust | broken | parse | 1 | peak RSS after 6 passes | 52.8 | 51.5 | MB |
| 1.03 | rust | broken | parse | 1 | peak footprint after 6 passes | 52.8 | 51.5 | MB |
| 1.02 | c | broken | parse | 1 | peak RSS after 6 passes | 42.2 | 41.3 | MB |
| 1.02 | c | broken | parse | 1 | peak footprint after 6 passes | 42.2 | 41.3 | MB |
| 1.02 | javascript | realerr (too small) | walk | all | peak footprint after 6 passes | 8.11 | 7.96 | MB |
| 1.02 | javascript | realerr (too small) | walk | all | peak RSS after 6 passes | 8.09 | 7.95 | MB |
| 1.02 | javascript | realerr (too small) | cursor | all | peak footprint after 6 passes | 7.97 | 7.84 | MB |
| 1.01 | rust | broken | parse | 1 | steady peak RSS | 52.7 | 51.9 | MB |
| 1.01 | rust | broken | parse | 1 | steady peak footprint | 52.7 | 51.9 | MB |
| 1.01 | javascript | realerr (too small) | cursor | all | peak RSS after 6 passes | 7.95 | 7.83 | MB |
| 1.01 | verilog | realerr | parse | 1 | steady peak footprint | 45.5 | 45.1 | MB |
| 1.01 | verilog | realerr | parse | 1 | steady peak RSS | 45.5 | 45.1 | MB |
| 1.01 | verilog | real | parse | 1 | peak RSS after 6 passes | 24.6 | 24.4 | MB |
| 1.01 | verilog | real | parse | 1 | peak footprint after 6 passes | 24.6 | 24.4 | MB |
| 1.01 | verilog | realerr | parse | 1 | peak footprint after 6 passes | 45.3 | 45 | MB |
| 1.01 | verilog | realerr | parse | 1 | peak RSS after 6 passes | 45.3 | 45 | MB |

Matrix, allocs and mem processes: 0 killed on gositter and finished on cgo, 5 killed on cgo and finished on gositter.

cgo killed, gositter finished:

- matrix scala|real|parse|all|1|cgo (KILL_RSS at 8429 MB, gositter peak RSS 63.7 MB)
- matrix scala|real|cursor|all|1|cgo (KILL_RSS at 8397 MB, gositter peak RSS 79.3 MB)
- mem cpp|broken|all|cgo (KILL_RSS at 2100 MB, gositter peak RSS 398 MB)
- mem scala|real|all|cgo (KILL_RSS at 8404 MB, gositter peak RSS 71.8 MB)
- mem scala|realerr|all|cgo (KILL_RSS at 8353 MB, gositter peak RSS 61.5 MB)

First parse: 2 of 504 file-metrics have a ratio above 1.00.

| ratio | grammar | item | metric | gositter | cgo | unit |
|---|---|---|---|---|---|---|
| 1.08 | tsx | #0 2041 B | parse1 | 0.234 | 0.216 | ms |
| 1.06 | tsx | #0 2041 B | init+parse1 | 0.241 | 0.228 | ms |

Allocs: 1 of 122 file-metrics have a ratio above 1.00.

| ratio | grammar | item | metric | gositter | cgo | unit |
|---|---|---|---|---|---|---|
| 1.41 | php | realerr cold | allocations per parse | 41 | 29 |  |

### Killed or failed processes: 22

| file | key | status | wd | stderr tail |
|---|---|---|---|---|
| matrix | scala\|real\|parse\|all\|1\|cgo | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 2.42, 'peak_mb': 8429, 'maxrss_mb': 8436} | WD rc=KILL_RSS wall=2.42 peak_mb=8429 maxrss_mb=8436 |
| matrix | scala\|real\|cursor\|all\|1\|cgo | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 2.4, 'peak_mb': 8397, 'maxrss_mb': 8399} | WD rc=KILL_RSS wall=2.40 peak_mb=8397 maxrss_mb=8399 |
| mem | cpp\|broken\|all\|cgo | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 25.45, 'peak_mb': 2100, 'maxrss_mb': 2100} | WD rc=KILL_RSS wall=25.45 peak_mb=2100 maxrss_mb=2100 |
| mem | scala\|real\|all\|cgo | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 2.6, 'peak_mb': 8404, 'maxrss_mb': 8404} | WD rc=KILL_RSS wall=2.60 peak_mb=8404 maxrss_mb=8404 |
| mem | scala\|realerr\|all\|cgo | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 4.41, 'peak_mb': 8353, 'maxrss_mb': 8365} | WD rc=KILL_RSS wall=4.41 peak_mb=8353 maxrss_mb=8365 |
| extreme | bash\|/data/files/gositter-bench-9j5/extreme/bash/random_bytes_697b59__n1000000.sh\|c | rc=245_no_output | {'rc': '-11', 'wall': 0.3, 'peak_mb': 51, 'maxrss_mb': 52} | WD rc=-11 wall=0.30 peak_mb=51 maxrss_mb=52 |
| extreme | c\|/data/files/gositter-bench-9j5/extreme/c/rep_n_aec841__n30000.c\|gositter | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.08, 'peak_mb': 37, 'maxrss_mb': 37} | WD rc=KILL_TIME wall=120.08 peak_mb=37 maxrss_mb=37 |
| extreme | c\|/data/files/gositter-bench-9j5/extreme/c/rep_n_aec841__n30000.c\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.08, 'peak_mb': 23, 'maxrss_mb': 23} | WD rc=KILL_TIME wall=120.08 peak_mb=23 maxrss_mb=23 |
| extreme | cpp\|/data/files/gositter-bench-9j5/extreme/cpp/rep_n_aec841__n30000.cpp\|gositter | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.08, 'peak_mb': 40, 'maxrss_mb': 40} | WD rc=KILL_TIME wall=120.08 peak_mb=40 maxrss_mb=40 |
| extreme | cpp\|/data/files/gositter-bench-9j5/extreme/cpp/rep_n_aec841__n30000.cpp\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.08, 'peak_mb': 25, 'maxrss_mb': 25} | WD rc=KILL_TIME wall=120.08 peak_mb=25 maxrss_mb=25 |
| extreme | csharp\|/data/files/gositter-bench-9j5/extreme/csharp/rep_n_aec841__n30000.cs\|gositter | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.08, 'peak_mb': 44, 'maxrss_mb': 44} | WD rc=KILL_TIME wall=120.08 peak_mb=44 maxrss_mb=44 |
| extreme | csharp\|/data/files/gositter-bench-9j5/extreme/csharp/rep_n_aec841__n30000.cs\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.08, 'peak_mb': 26, 'maxrss_mb': 26} | WD rc=KILL_TIME wall=120.08 peak_mb=26 maxrss_mb=26 |
| extreme | golang\|/data/files/gositter-bench-9j5/extreme/golang/x_n_x3_a_x5_7103f9__n132000.go\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.09, 'peak_mb': 37, 'maxrss_mb': 37} | WD rc=KILL_TIME wall=120.09 peak_mb=37 maxrss_mb=37 |
| extreme | json\|/data/files/gositter-bench-9j5/extreme/json/unterminated_a_b_ceeb36__n100000.json\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.01, 'peak_mb': 62, 'maxrss_mb': 62} | WD rc=KILL_TIME wall=120.01 peak_mb=62 maxrss_mb=62 |
| extreme | kotlin\|/data/files/gositter-bench-9j5/broken/kotlin/015_stray.kt\|c | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 28.53, 'peak_mb': 4098, 'maxrss_mb': 4098} | WD rc=KILL_RSS wall=28.53 peak_mb=4098 maxrss_mb=4098 |
| extreme | kotlin\|/data/files/gositter-bench-9j5/broken/kotlin/075_stray.kt\|c | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 20.52, 'peak_mb': 4096, 'maxrss_mb': 4096} | WD rc=KILL_RSS wall=20.52 peak_mb=4096 maxrss_mb=4096 |
| extreme | kotlin\|/data/files/gositter-bench-9j5/broken/kotlin/079_stray.kt\|gositter | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 25.04, 'peak_mb': 4098, 'maxrss_mb': 4098} | WD rc=KILL_RSS wall=25.04 peak_mb=4098 maxrss_mb=4098 |
| extreme | kotlin\|/data/files/gositter-bench-9j5/broken/kotlin/079_stray.kt\|c | KILL_RSS | {'rc': 'KILL_RSS', 'wall': 42.98, 'peak_mb': 4103, 'maxrss_mb': 4103} | WD rc=KILL_RSS wall=42.98 peak_mb=4103 maxrss_mb=4103 |
| extreme | kotlin\|/data/files/gositter-bench-9j5/broken/kotlin/128_stray.kt\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.12, 'peak_mb': 1913, 'maxrss_mb': 1913} | WD rc=KILL_TIME wall=120.12 peak_mb=1913 maxrss_mb=1913 |
| extreme | rust\|/data/files/gositter-bench-9j5/extreme/rust/unterminated_r_a_bcb8a3__n100000.rs\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.09, 'peak_mb': 43, 'maxrss_mb': 43} | WD rc=KILL_TIME wall=120.09 peak_mb=43 maxrss_mb=43 |
| extreme | tsx\|/data/files/gositter-bench-9j5/extreme/tsx/block_type_A_n_f29830__n100000.tsx\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.09, 'peak_mb': 62, 'maxrss_mb': 62} | WD rc=KILL_TIME wall=120.09 peak_mb=62 maxrss_mb=62 |
| extreme | verilog\|/data/files/gositter-bench-9j5/extreme/verilog/block_always_posedge_c_begin_n_11163b__n100000.v\|c | KILL_TIME | {'rc': 'KILL_TIME', 'wall': 120.07, 'peak_mb': 144, 'maxrss_mb': 144} | WD rc=KILL_TIME wall=120.07 peak_mb=144 maxrss_mb=144 |

