MEMORY BUDGET: gositter peak RSS at one thread (origin/main, measured in the bench pod)

Units are MiB (kB/1024), the same unit as BENCHMARKS.md. "std" is /data/v/fix4/gsbench, 7 runs. "C" is /data/bin/cbench, 3 runs. Every run is "-what parse -n 40 -warm 1". Pod outputs are in /data/membudget-q7/out/ and the scratch sources are in /work/membudget-q7 and /work/membudget-q7-5a.

CONCLUSION

The 2x at one thread is not the tree. Two things cause it.
1. The children slab is dropped and regrown. That is about 97% of all steady-state allocated bytes.
2. That garbage fills the GC headroom. The heap swings up to NextGC = 2 x (live heap at the last mark). The live heap includes the caller's own data (here the 12 MiB of loaded source) and the 22 to 34 MiB of heap chunks that sit in the sync.Pool.

Remove the steady-state garbage and the heap stops growing after the warm-up pass. For python real, peak RSS falls from 54.7 to 33.8 MiB with never-drop, and to 30.1 MiB with never-drop plus scanner reuse. No GC runs after the warm-up.

Slab slack (i) and transient copy (iv) are small, about 1 to 2 MiB each. Slab sizes are already at C's level.

Never-drop is ruled out as the fix: at 16 threads it is +20% on python real and +45% on cpp broken.

One finding outside the question: the stack slabs (stack.nodes, stack.moreLinks, freeTrees, nodePool) are never shrunk. After cpp broken they hold 45 MiB per parser, and 753 MiB across 16 parsers. That already breaks the "no huge slab held forever" rule.

1. BUDGET AT THE VmHWM MOMENT

Instrumentation (scratch only):
- A Parser.MemDump that reports len and cap in bytes for 22 slabs and reused slices.
- A garbage-free /proc/self/status reader that runs after every parse. When VmHWM rises it snapshots runtime.ReadMemStats and the slabs.
- The one-parser slab high-water.
- Knobs set by environment variables: NEVERDROP, GROWPCT, PRECHILDREN/PRECHUNKS, SRCMMAP (sources copied into an anonymous mmap so they are outside the Go heap), GCAFTERWARM.

The instrumented binary with no knobs set matches std: python real 55.4 against 54.7, python broken 81.0 against 81.0.

Columns: HWM std median [range] | C median | (a) source (Go heap holding it after load) | (b) Go tiny / C tiny, and Go loaded-minus-source / C loaded-minus-source | (c) table | (d) one-parser slab high-water, cap (len) | RssFile at HWM | heap retained R = HeapSys - HeapReleased | live at mark = NextGC/2 | headroom = NextGC - live | retained above goal = R - NextGC | non-heap anon = RssAnon - R | live residual = live - (source heap + table + slabs) | (f) C minus source.

- python real j1: 54.7 [54.3-55.4] | 24.1 | 11.25 (12.41) | 4.80/3.32, 6.08/2.30 | 0.80 | 9.79 (8.66) | 3.96 | 49.3 | 22.5 | 22.5 | 4.4 | 2.3 | -0.5 | 12.8
- python broken j1: 81.0 [80.5-89.1] | 41.8 | 5.05 (5.85) | 4.80/3.32, 4.93/2.38 | 0.80 | 34.26 (32.21) | 4.02 | 85.0 | 38 (43 in the high runs) | 38-43 | 9.1 | 0.8 | -3.0, or +2.1 against the 86 MiB goal in gctrace
- bash real j1: 27.2 [27.0-27.6] | 10.3 | 2.28 (2.90) | 4.86/2.69, 5.36/2.46 | 2.13 | 4.34 (3.66) | 4.96 | 22.2 | 9.9 | 9.9 | 2.3 | 1.2 | +0.6 | 8.0
- hcl real j1: 29.2 [28.9-29.4] | 13.0 | 4.40 (5.27) | 4.61/2.58, 5.75/2.43 | 0.17 | 5.75 (4.96) | 3.77 | 24.8 | 10.9 | 10.9 | 3.0 | 2.1 | -0.3 | 8.6
- golang real j1: 53.8 [53.2-55.3] | 24.1 | 11.00 (12.35) | 4.87/2.84, 6.32/2.36 | 0.42 | 9.87 (8.90) | 3.71 | 50.3 | 23.4 | 23.4 | 3.4 | 2.1 | +0.8 | 13.1
- cpp broken j1: 278.0 [268.9-278.6] | 137.4 | 7.11 (7.98) | 5.35/3.57, 5.30/2.21 | 1.94 | 111.9 (90.3) | 7.09 | 285.1 | 130 | 130 | 25 | -8.5 | +8.3 | 130.2
- python real j16: 153.6 [149.1-156.9] | 169.8 | 11.25 | same | 0.80 | 16 parsers hold 29.7 at the HWM moment and 38.0 at end | 3.96 | 139.7 | 68 | 68 | 3.7 | 2.8 | +25 | 158.6
- cpp broken j16: 1431.5 [1274-1534] | 1888.9 | 7.11 | same | 1.94 | 855 cap (115 len) across 16 parsers at end, of which 753 are stack slabs | 7.15 | 1503.6 | 817 | not reached (R - NextGC = -130) | -130 | -95 | -49 | 1881.8

How to read the table:
- The identity is HWM = RssFile + R + non-heap anon. R splits into live at mark, headroom and the scavenger retention above the goal (the scavenger keeps about 10% over the goal).
- The live part is explained to within -0.5 to +0.8 MiB for python real, bash, hcl and golang. For cpp broken j1 it is within 8 MiB (6%), which is likely pool chunks plus kept stack slabs.
- Not explained: python real j16 has +25 MiB of live at the mark that is neither source nor the slabs captured. My inference is chunks parked in the shared sync.Pool plus other parsers' children slabs at the mark moment; I did not capture the slabs at the mark itself. In cpp broken j16, R overstates RSS by 95 MiB. My inference: the untouched tails of fresh doubled slabs, since Go does not fault in fresh zero spans.

gctrace lines at the peak (python, fix4):
- python real, every pass: "47->47->22 MB, 47 MB goal", about 1 GC per pass, 36 GCs in 41 passes.
- python broken, normal cycle: "73->73->37 MB, 75 MB goal".
- python broken, before the high peak: "gc 15 80->80->42 MB, 80 MB goal; gc 16 84->84->37 MB, 86 MB goal".
- cpp broken: "268->268->129 MB, 268 MB goal".

Fixed costs:
- (b) The Go runtime at process start is 3.4 to 3.7 MiB ("loaded" of a one-file run). After 40 parses of a 10 to 34 byte file it is 4.6 to 5.4 MiB, against 2.6 to 3.6 for C. Go costs about 1.5 to 2.2 MiB more. The claim "3 to 4 MB" holds as an absolute number.
- (c) The comb table plus base is 0.80 (python), 2.13 (bash), 0.17 (hcl), 0.42 (golang) and 1.94 (cpp) MiB. That is narrower than the claimed "1 to 3.5 MB": hcl and golang are below 1. The real lists also touch 0.3 to 1.5 MiB more binary pages than the tiny run (RssFile).
- Slabs against C's parser memory (C minus source minus C's loaded overhead): python real 9.8 against 10.6, python broken 34.3 against 34.4, cpp broken 111.9 against 128. "Nodes are already the size of C's" is verified.

2. STEADY-STATE ALLOCATIONS PER PARSE ON MAIN

Method: MemProfileRate=1, the allocs profile written after warm-up and after 5 passes, then "pprof -base". Line numbers are origin/main.

The dominant site is subtree.go:328, the growChildren make:

| list | bytes per parse | allocations per parse |
|---|---|---|
| python real | 17.2 KiB | 0.036 |
| python broken | 23.3 KiB | 0.034 |
| golang real | 8.2 KiB | 0.015 |
| cpp broken | 68 KiB | 0.028 |
| bash real | 3.0 KiB | 0.008 |
| hcl real | 1.3 KiB | small (count not checked) |

Smaller sites:
- subtree.go:237, the drop's fresh 4096-entry slab, 32 KiB each: python real 0.22 KiB per parse. This is the drop itself.
- subtree.go:489, the freeBits regrow: python broken 0.17 KiB, cpp broken 1.0 KiB per parse.
- subtree.go:145, new heapChunk from the pool: bash 0.10 KiB, hcl 0.02 KiB per parse, 0 on python real.
- External scanners, by count but tiny in bytes:
  - python: newScanner (scanner.go:59) 1 per parse, Deserialize :357 0.55, Scan :235 0.40 and :310 0.43. About 88 B per parse in total (the never-drop run allocates 3.6 MB per 40880 parses).
  - bash: newScanner :80 1 per parse plus heredoc pushes, about 48 B per parse.
  - hcl: newScanner :319 1 and push :75 1.1 per parse, about 5 B and 4 B.
  - cpp: newScanner :104 1 per parse.

The "18 KB per parse" figure is right as an average: python real allocates 751 MB over 40880 parses, 18.4 KB per parse. But it is not per parse. It is about 37 drop-and-regrow series per pass, each after a large file. A drop happens when cap is over 64Ki entries and the next parse used under 1/8 of it, and the regrow then doubles from 4096 entries back up, so each series allocates about 2x the final cap. The scanner allocations are 4.4 per parse by count and about 0.5% by bytes.

3. WHY 2x: python real (54.7 against C 24.1, gap 30.6) and python broken (81.0/89 against 41.8)

- (iii) Headroom up to the GC goal is the main term.
  - python real: 22.5 MiB headroom plus 4.4 retained above the goal.
  - python broken: 38 to 43 headroom plus 9.1 retained.
  - The source's share: moving the source out of the Go heap (SRCMMAP) gives python real 41.4 [40.8-42.0], -13.3 MiB, which equals the source's 12.4 MiB of headroom. bash real goes 27.2 to 25.0. python broken goes 81.0 to 84.5 [84.1-88.1]: no gain, because its live heap is mostly pool chunks.
  - Live at mark for python broken is source 5.85 + table 0.8 + 8.9 of kept slabs (6.0 of it stack.nodes, never dropped) + 21.9 of heap chunks cycling through the sync.Pool = 37.4. That matches gctrace's 37.
- (ii) The garbage that fills the headroom: 17.6 MiB per pass (python real) and 12.6 MiB per pass (python broken), almost all growChildren. Never-drop removes it: python real 33.8 [32.9-34.1] with 3.6 MB allocated in 40 passes, python broken 52.2 [52.2-52.4]. GCAFTERWARM has no effect: 55.1 and 81.2.
- (i) Slack: python real total cap 9.79 against len 8.66, so 1.1 MiB (children 2.0 against 1.07). python broken 34.26 against 32.21, so 2.1 MiB (stack.nodes 6.0 against 4.6).
  - Never-drop plus preallocation to the needed size: python real 32.5 [32.4-32.6], -1.3 against never-drop. With fix5a: 28.9 against 30.1.
  - python broken with preallocation is worse: 55.2 [53.6-56.4] against 52.2. The early large live heap raises the goal, so no GC ever collects the warm-pass garbage.
  - Growth factor 1.25: python real 51.5 [50.7-51.9] (-3.2, with 2x the allocated bytes). python broken 90.2 [89.7-92.5], worse. Not a lever.
- (iv) Transient copy on growth: the old and new slab live together. 1 MiB for python real and 2 MiB for python broken at the last children doubling (3 MiB for stack.nodes, warm-up only). This shows up as the live-at-mark spike 37 to 42 on python broken. Not isolated by its own experiment.
- (v) Other:
  - Go fixed cost: +1.5 MiB runtime and binary, +0.8 table, +1.2 rounding of the source allocations, +1.5 to 2 non-heap anon (GC metadata grows with the heap).
  - Warm-pass garbage that no GC collects once there is no steady-state garbage: about 3 MiB (python real) and about 8 MiB (python broken, from stack.nodes and children doubling copies plus prepare temporaries of 1.3 MiB).

4. THE fix5a OBSERVATION (python broken at 80.7 or 89.9)

Every GC cycle marks 37 MiB live (goal 75, RSS about 80.7), unless the cycle starts during the late part of file 47. File 47 is 200 KB and its slabs reach 34 MiB. Then the copy-doubled children slab and its stack are live too, the cycle marks 42 MiB, the goal is 84 to 86, and RSS is about 89.9.

Since about 12.6 MiB of garbage per pass gives one GC every 3 passes, whether any of the roughly 12 cycles lands in file 47 is a phase effect of the allocation sequence:
- main: 0 or 1 of 12 cycles in 3 traced runs (82.7 / 90.2 / 89.9 MB).
- fix5a: 4 of 12 cycles in its trace, and 7 of 7 runs high (89.1-90.1).

fix5a changes almost no bytes (python broken allocates 503.4 against 504.9 MB per run). I infer that it shifts the phase rather than adds memory. I did not prove why it lands on file 47 more often. Logging the file index at each GC start for both binaries would settle it.

What makes it always the low value: no steady-state garbage, so no cycle runs after warm-up.
- never-drop: 52.2 [52.2-52.4], 7 of 7 runs, range 0.2 MiB.
- fix5a plus never-drop: 51.4 [51.2-51.6].

Making the live heap at a mark independent of file position would also work, but it is weaker.

5. FLOOR FOR A PURE GO-HEAP DESIGN

The theoretical floor is source + Go loaded overhead + extra binary pages + table + slab len, with no slack and no garbage. "Measured best" is fix5a + never-drop + preallocation.

| list | theoretical floor | measured best | C | mmap prototype |
|---|---|---|---|---|
| python real | 11.25 + 6.08 + 0.5 + 0.8 + 8.66 = 27.3 MiB (1.13x C) | 28.9 [28.8-29.0] (1.20x) | 24.1 | 1.12 to 1.35 |
| bash real | 2.28 + 5.36 + 1.5 + 2.13 + 3.66 = 14.9 MiB (1.45x C) | 17.0 (1.65x); 16.3 with the source outside the heap (1.58x) | 10.3 | 1.70 |
| python broken | about 43.5 MiB (1.04x C) | 51.4 (1.23x) | 41.8 | not given |
| golang real | not computed | 29.4 with never-drop (1.22x) | 24.1 | not given |

For bash, about 4.3 MiB of the gap is fixed: 2.2 MiB of Go runtime over C plus the 2.1 MiB table.

The gap between the measured best and the floor is warm-pass copy garbage. Only growth without copying (chunked slabs) removes it.

16-thread constraint, measured:

| config | python real | cpp broken |
|---|---|---|
| std today | 153.6 | 1431.5 |
| never-drop | 183.8 | 2072.5 |
| fix5a + never-drop | 179.6 | 2007.8 |
| fix5a alone | 150.3 | 1387.7 |
| C | 169.8 | 1888.9 |

Recommendation (inference, not tested): reach zero steady-state garbage without per-parser retention.
- Hand dropped or outgrown children and stack slabs to a shared, size-classed sync.Pool, the same way heap chunks work today, or chunk them so growth never copies.
- At one thread this behaves like never-drop: no GC runs, so the pool keeps its contents.
- At 16 threads the pool bounds retention to what the inputs in flight need, and the caller's own GCs drain it.
- The stack slabs need the same drop rule anyway.
- Preallocation and growth factor 1.25 are dead ends.

COMMANDS IN OUTLINE

1. I made one scratch copy from git archive origin/main, added memdbg.go and bench/memdbg.go plus 3 knob lines in subtree.go, and ran go build, go vet and go test on the Mac (124 tests passed).
2. I sent it with "pod.sh put", built under brun.sh into /data/membudget-q7/gsbench, then applied the fix5a non-test diff and built gsbench5a.
3. A detached driver (drv.sh, one brun.sh call per run, -m 4096) ran 3 job lists: tiny-file runs, instrumented runs, C runs, gctrace runs, MemProfileRate=1 runs, std runs and the experiment runs, 7 runs each (cpp broken j1 never-drop: 5).
4. I ran pprof under brun. The local scratch copy is deleted; the pod files are left. No processes of mine are running.

NOT MEASURED

- Per-site bytes of the small scanner allocations: only the aggregate from the never-drop run.
- gctrace for bash, hcl and golang: those use NextGC from the snapshot.
- Slabs at the exact mark moment: this is the j16 +25 MiB residual.
- C's RssFile/anon split: cbench does not print it.
- The walk and cursor modes, and the realerr and extreme lists.
- 16-thread budgets for hcl, golang and bash.
- Never-drop applied only to children, or only to heap chunks.
- The transient copy isolated by its own experiment.
- The mechanism behind fix5a's phase.
- The instrumented runs are 1 run each; their HWM sits 0.9 MiB above the std median on python real.