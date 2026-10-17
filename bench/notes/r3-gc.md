# R3: outside-garbage GC rows (fix 5 = ec8d1ae)

**Conclusion: no source change gets main's GC count under caller garbage without main's live heap, and so main's peak RSS under that garbage. I propose no change to ec8d1ae and wrote no fix.patch.** The GC count follows allocation divided by the marked heap: the product of the two is the same in every build within 1 to 8%. Fix 5's marked heap is smaller because the stack node slab a reset gives back is held weakly, and the caller's GCs free it. Holding it (build X, one changed call) brings j1 back to main's GC count and cpu exactly. But the heap under garbage then sits at main's level and slightly above it, and golang broken j1 cursor locks into one GC per pass for +8.5% peak RSS. At j16, X still runs 2.3x main's GCs. Getting GC parity at j16 means main's 1.7 GB peak on cpp broken.

## Setup
- Driver: gsbench plus `-garb KB` (after each file, allocate and touch a KB-sized []byte and drop it), `-memprof` (allocs profile after warm-up and at the end), and NumGC/NextGC and runtime/metrics GC CPU in the end line. Source: `r3gc/main.go`.
- Binaries on the pod: `/data/r3gc/bin/gsb-{m,f,x,d}`.
  - m = main 96f2c29.
  - f = ec8d1ae.
  - x = ec8d1ae with the node slab held like the link slab (`r3gc/variant-x.diff`).
  - d = one binary whose GSDIAG env switches the retention mode, so all three modes share one code layout (`r3gc/variant-dbg-gsdiag.diff`): d0 = ec8d1ae behaviour, d1 = X, d2 = no stack-slab give-back (main's stack retention, fix 5's children reuse).
- Runs: binaries rotate each round. Each process goes through brun.sh, in chunks of 225 s or less (`r3gc/chunk.py`, `outer.sh`). Rows m1 have n=7, dg n=4 (p=0.014 is the smallest p possible at 4 vs 4), and sw1 n=3. p is the one-sided exact Mann-Whitney p that the build is larger (`r3gc/ana.py`).
- Results: `r3gc/m1.jsonl`, `ana-m1.txt`, `ana-dg-vs-d2.txt`. gctrace and profiles are in `r3gc/pod/out`.
- The pod was shared with 3 or 4 other sessions, so main's own wall time ranges over about 3% within a row. Treat cross-binary wall differences under 2% as layout or drift. Instructions and the dg rows are the clean signals.

## Root cause

**GCs × mean marked heap ≈ bytes allocated.** These rows are from gctrace at n=40 (diag3, first 20% of GCs dropped):

| row | build | GCs | mean marked MB | product | alloc MB |
|---|---|---|---|---|---|
| cpp broken j1 | main | 272 | 74.1 | 20155 | 24403 |
| | fix 5 | 430 | 50.0 | 21500 | 26455 |
| | X | 278 | 73.4 | 20405 | 24474 |
| python broken j1 | main | 956 | 18.7 | 17877 | 23072 |
| | fix 5 | 1381 | 12.6 | 17400 | 23877 |
| | X | 954 | 18.5 | 17649 | 23153 |
| cpp broken j16 | main | 49 | 544.7 | 26690 | 25718 |
| | fix 5 | 116 | 229.8 | 26657 | (n/a) |

**Which slab.**
- At j1 it is the stack node slab.
  - An allocs profile over 5 passes with garbage shows fix 5 has one extra site, `(*stack).growNodes`: 240 MB on cpp and 75 MB on python. Main has none.
  - Heap chunks (sync.Pool) and children are equal in both builds (cpp 238 against 241 MB, children 170 against 166).
  - Links do not show up: they are already held.
  - A per-file dump (`r3gc/slabdump.md`) shows a 128Ki-node (6 MiB) slab given back after file 49 in python broken, and a 512Ki-node (24 MiB) slab in cpp broken. Main's marked heap is larger by about that much: python 18.7 against 12.6, cpp 74 against 50.
  - The extra bytes are the regrowth after each outside GC frees the slab: +3.5% on python j1, +8.4% on cpp j1, +7.1% on cpp j16.
- At j16 it is every parser's node and link slabs. Main keeps all 16 parsers' stack slabs for good (mean marked 545 MB, against fix 5's 230). Fix 5 gives them back.
- No library change can lower the allocation floor enough. On cpp j1 the outside garbage alone is 5767 of the 6613 MB fix 5 allocates. Even with zero parser allocation, fix 5 would run about 112 GCs against main's 82.

**Cost.** I measured this with the layout held fixed (dg, d0 against d2). The extra GCs plus the regrowth cost:

| row | wall | cpu | GCs | GC CPU (runtime/metrics) | mutator CPU |
|---|---|---|---|---|---|
| python broken j1 | +1.8% | +3.2% | 1.48x | 1.47x | +3.0% |
| cpp broken j1 | +2.0% | +2.9% | 1.60x | 2.04x | +2.3% |
| cpp broken j16 | +2.2% | +3.3% | 3.2x | | |
| python broken j16 | +2.2% | +0.6% | 1.57x | | |

All p=0.014 except python j16 cpu (p=0.057). The GC CPU and mutator CPU columns were captured only for the j1 rows. gctrace puts a cycle at about 0.45 ms CPU at j1 and about 5 ms at j16: 16 Ps × 0.1 to 0.25 ms of stop-the-world, plus marking. Those per-cycle costs do not differ between builds, so a source change cannot shrink them.

## Rows, cross-binary, n=7 (m / f / x)

| row | wall ms | cpu ms | Minstr | GCs | peak RSS MiB |
|---|---|---|---|---|---|
| cpp broken j1 garbage | 606.6 / 610.9 (p=.049) / 615.8 (p=.036) | 615.0 / 624.6 / 624.6 | 12390 / 1.0013x / 0.9998x | 82 / 129 / 80 | 253.2 / 256.3 / 263.4 (p=.16) |
| python broken j1 garbage | 287.6 / 287.2 / 286.0 | 302.9 / 305.6 (p=.064) / 300.7 | 5629 / 1.0055x / 0.9990x | 581 / 841 / 575 | 91.2 / 73.3 / 92.4 (p=.004) |
| cpp broken j16 garbage | 264.9 / 270.9 (p=.036) / 275.0 (p=.036) | 960.8 / 972.6 / 980.4 | 12400 / 1.0088x / 1.0066x | 46 / 139 / 106 | 1756 / 638 / 663 |
| python broken j16 garbage | 51.76 / 51.60 / 53.53 (p=.036) | 546.9 / 543.6 / 545.7 | 5734 / 1.0105x / 1.0087x | 138 / 229 / 203 | 621 / 397 / 426 |
| golang broken j1 cursor (guard) | 214.6 / 212.0 / 217.0 | 216.5 / 213.3 / 217.9 | 4683 / 0.9994x / 0.9993x | 60 / 51 / 40 | 85.97 / 86.61 (p=.076) / 93.28 (p<.001) |
| cpp broken j1 cursor (guard) | 639.7 / 630.1 / 647.0 (p=.009) | | 13530 / 0.9998x / 0.9997x | 4 / 2 / 1 | 259.6 / 248.3 / 246.8 |

- X's +1.1 to 1.5% wall on cpp broken j1 (garbage and cursor) comes with equal instructions. In the layout-free runs, d1 matches d2 on cpp j1 (603.8 against 603.1, p=0.34), so I read X's cross-binary j1 excess as layout.
- X's j16 excess (+3.4 to 3.8%) is bigger than the dg difference between d1 and d0 (−1.4% on cpp, −0.2% on python). That is also layout, an inference.

## Why X fails the golang cursor guard, and why no retention rule avoids it

**The mechanism (measured):**
- In X every GC in the run reads `83->83->40 MB, 83 MB goal`. That is exactly one GC per pass, and the cycle starts during the cursor walk of the deep file. The inuse profile at the last mark has `(*TreeCursor).scan` at 9.2 MB live.
- Main cycles through goals 80, 71 and 60. With a steady goal of 83, the scavenger keeps about 10% above it, so the peak reaches 93 to 96.
- Main avoids the lock because main still drops and regrows children slabs, about 9 MB of extra garbage per pass.
- d2, which is main's own stack retention with fix 5's children reuse, also peaks at 93.1 on this row. Any rule that keeps the node slab across the caller's GCs hits it.

**Python and golang broken have the same node slab history** (slabdump):
- 100006 against 100008 nodes in file 47, a 131072-node slab, given back after file 49, every 500 parses.
- Holding that slab is what fixes python broken j1, and the same hold is what locks golang cursor.
- So no rule keyed on slab size, cap or recurrence can treat the two rows differently.

**The guard row is a phase effect, not a cost of the design.** I swept golang broken j1 cursor over caller garbage per file (n=3, peak RSS MiB, m / f / x):

| garbage per file | m | f | x |
|---|---|---|---|
| 0 | 85.97 | 86.61 | 93.28 |
| 8 KB | 86.88 | 89.64 (p=.05) | 87.48 |
| 32 KB | 89.43 | 91.17 (p=.05) | 88.34 |
| 128 KB | 89.18 | 85.58 | 88.90 |

- Fix 5 itself is above main at 8 and 32 KB.
- X is at or below main everywhere except the 0 KB point the guard uses.

## Candidates

- **X (hold node slabs as links, 1024 parses, ≤32 MiB).**
  - It fixes j1: GCs 80 against 82 and 575 against 581, instructions 0.999 of main, python j1 cpu 0.993.
  - Rejected for three reasons:
    1. golang broken j1 cursor is +8.5% (p<0.001).
    2. python broken j1 with garbage peaks 1.3% above main (p=0.004).
    3. It gives back fix 5's under-garbage saving at j1 (python 73 to 92 MiB), and j16 still runs 2.3x main's GCs.
- **Cursor stack reuse on Close/Walk, on top of X** (Mac only, `variant-xc-cursor.diff`). It cut golang cursor allocation from 1805 to 146 MB per 40 passes and the GCs from 41 to 3. The peak still rose to 100 against main's 92, because the warm-up heap is never collected. Not pursued.
- **Not built: growth that jumps straight to the given-back size.** By my estimate it would cut fix 5's regrowth bytes by about half, but GCs by only about 3%, since the marked heap stays the same.

## Recommendation
Keep ec8d1ae.
- The extra GCs are the price of the smaller marked heap: under caller garbage, fix 5 peaks at 0.80 of main on python j1 and 0.36 on cpp j16.
- Layout-free, that costs about 2% of wall time on deep lists that repeat their deep file every 500 parses.
- Inference, not measured: a one-pass caller such as graphify-go never parses the deep file again, so holding the slab would only raise its heap goal. That trade belongs to the caller's GOGC, not to the library.
- If the owner prefers GC parity at j1, X is a 2-line change (`variant-x.diff`) plus CONTRACT/test edits. It must then accept the cursor-row lock above.

## Not checked
- The quiet guard rows for X at n≥7. I dropped them for pod time. By construction no GC runs after warm-up in those rows, so weak and strong holding behave the same there, but I did not measure it this round.
- 2, 4 and 8 threads, walk mode, mixed languages, and C at j16.
- golang and bash broken with garbage.
- arm64 timing (the Mac was used only for GC counts and phase).
- GC CPU per class for the j16 dg rows. The capture had a bug that I fixed only after those rows had run.

## Pod paths (mine)
- `/data/r3gc`: binaries, results and out.
- Work trees: `/work/r3gc-{m,f,x,dbg,tools}`.
- Nothing of mine is running.
