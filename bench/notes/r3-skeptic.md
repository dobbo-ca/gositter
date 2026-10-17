# R3 skeptic: fix5r2 (c79d10e) vs main (96f2c29)

**Verdict: REFUTED.** The suite is incomplete by its own report, and my own rerun at n=7 to 14 finds rows that are slower or larger than main beyond noise. The worst is the outside-garbage family, plus two thread counts the suite never ran: python broken j8 with 1 MB caller garbage is +8.1% wall (p=0.0001). The memory win holds: 61-list peak RSS fix5r2/main is 0.650 at j1 and 0.652 at j16 when I use the suite's pass counts.

## Where and how

The bench pod is dead (`pod.sh sh` returns "cannot exec into a container in a completed pod; current phase is Failed"). I did not touch kubectl. Everything below ran on this Mac (darwin/arm64, 14 cores, go1.27.2). Other sessions put load average at 4 to 10, so j16 rows are noisier than on the pod. That noise is the same for both binaries, because they alternate. Peak RSS is `getrusage` ru_maxrss. This build has no instruction or cycle counters, so time rows are wall and cpu only.

- Sources: `git archive` of 96f2c29 and c79d10e into `r3sk/src/{main,fix5r2}`. The skep driver is copied from `skeptic/skep` with the linux build tag dropped, plus `rusage_darwin.go`.
- Binaries: `r3sk/bin/skep-main` is 55,326,850 bytes, `skep-fix5r2` is 55,406,818 (+80 KB).
- My scripts are `r3sk/jobs.py`, `run.py` and `ana.py`, plus `sweep.py` for the sweep. `run.py` is rep-major and swaps binary order on each (rep+job). `ana.py` has its own one-sided Mann-Whitney: exact by enumeration, with a normal approximation only when C(N,n1) > 3e6. It reproduces 1/6 for n=2 and 1/12870 for n=8 at full separation. Each process reports the median over its timed passes.
- Results: `r3sk/t.jsonl` and `t-ana.txt` (targeted rows), `r3sk/s.jsonl` (61-list sweep, -n 2 -warm 1, n=7), `r3sk/s4.jsonl` (61-list sweep with the suite's calib passes, about 4 s, n=3). All paths are under $BENCH_DATA/fixes.
- Trees: the check signature (nodes, error files, walk and cursor sums) matches on every targeted row, n=8 to 14. The sweeps ran with -nocheck.

## Hand check of the suite's ana.py (skeptic/results/r3)

- u3.cpp.broken.j16. Raw wall per process: main 277.61 and 279.58, fix5r2 286.29 and 290.19. Medians 278.6 and 288.24 give +3.46%, and cpu 980.0 to 1025.2 gives +4.61%. Both fix5r2 values sit above both main values, so p = 1/C(4,2) = 0.167. ana_all.txt says +3.46, +4.61, p 0.167. It matches.
- u3x.golang.real.j1 RSS. main 52.5 and 58.8 MB, fix5r2 56.6 and 122.5 MB. The medians 55.65 and 89.55 give +61%. The fix5r2 ranks are {2,4}, so p = 2/6 = 0.333. ana_all.txt says +60.99, p 0.333. It matches.
- The math is right. With n=2 the suite cannot flag anything, as its report says.

## Rows worse than main beyond noise (p <= 0.05, my rerun)

Outside garbage (caller drops 1 MB per file; x means 4 KB objects), n=8:

| row | wall | cpu | GCs/pass | alloc/pass | peak RSS |
|---|---|---|---|---|---|
| u3.cpp.broken.j1 | +1.23% p=0.014 | +1.86% p=0.002 | 9 to 13.5 | +8.4% | +4.7% p=0.019 |
| u3x.cpp.broken.j1 | +0.6% p=0.16 | +0.94% p=0.019 | 9 to 13.5 | +8.4% | +1.5% p=0.033 |
| u3.cpp.broken.j16 | +0.9% p=0.32 | -0.7% | 1 to 3 | -7% | -51% |
| u3x.cpp.broken.j16 | +3.6% p=0.080 | +6.8% p=0.0005 | 2 to 4 | -8.6% | -51% |
| u3.python.broken.j1 | +2.62% p=0.005 | +5.24% p=0.0003 | 31 to 53.5 | +3.65% | +6.7% p=0.042 |
| u3x.python.broken.j1 | +1.54% p=0.019 | +3.42% p=0.0035 | 31 to 49 | +3.5% | +1.5% p=0.039 |
| u3.python.broken.j16 | +3.70% p=0.019 | +3.63% p=0.025 | 2 to 4 | +3.6% | -49% |
| u3x.python.broken.j16 | +10.56% p=0.0001 | +8.11% p=0.0001 | 3 to 6 | +3.4% | -38% |
| u3x.golang.real.j16 | +2.36% p=0.025 | +0.5% p=0.14 | equal | equal | +3.5% p=0.17 |
| s16y.bash.real.j16 | +3.93% p=0.025 | +3.80% p=0.005 | 10 to 12 | equal | -14% |

Rows the suite never ran (2/4/8 threads) or that I picked, n=8:

| row | wall | cpu | GCs/pass | alloc/pass | peak RSS |
|---|---|---|---|---|---|
| thr.python.broken.j8.g1 | +8.12% p=0.0001 | +5.40% p=0.0002 | 6 to 13 | +3.4% | -37% |
| thr.cpp.broken.j4.g1 | +2.28% p=0.010 | +2.74% p=0.0035 | 2 to 6 | +16.5% | -42% |
| mix.j4.g1 (6 grammars, 720 files) | +1.2% p=0.14 | +1.3% p=0.12 | 3 to 5 | +8.3% p=0.010 | -26% |

Two patterns stand out. With caller garbage, fix5r2 allocates more per pass than main, not less: python broken is +3.5%, cpp broken j1 +8.4%, cpp j4 +16.5%. It also runs 1.5x to 3x the GCs. The weak free lists seem to empty at each GC the caller forces, and the slabs are then allocated again. That is an inference from these numbers; I did not trace it.

Small peak RSS, mostly 0.1 to 0.2 MB, which is the scale of the 80 KB bigger binary:
- c3.javascript.realerr.j1: 6.95 to 7.03 MB, +1.1%, p=0.008, n=11.
- c3.rust.realerr.j16: 7.66 to 7.78 MB, +1.6%, p=0.019, n=11.
- coldsw.rust.1 cold: 6.36 to 6.48 MB, +2.0%, p=0.003, n=11.
- xslow.golang.rep_n_b5fbe6 cold: 22.47 to 22.80 MB, +1.5%, p=0.039, n=7. Wall is equal (-0.3%).
- Sweep with 3 passes, n=7, realerr j1, peak RSS +1.5 to +3.3% (p 0.0003 to 0.011): java, javascript, lua, php, python, rust.
- Sweep wall at j1: lua.realerr +17% (20 to 24 us), rust.realerr +9.7% (p=0.009), json.realerr +1.8% (p=0.0035), julia.realerr +2.4% (p=0.049). This is the suite's "sweep.rust.realerr.j1 +27%" row, now at n=7 on other hardware, so it is not just noise. The absolute times are microseconds.
- golang broken j1 cursor (u2): RSS +1.8%, p=0.052, which is borderline. Wall is -1.3%.

## Suite rows that did not reproduce (n=8 to 14)

- hclj hcl real/broken j1, j4, j8: all within +-1.1%, p >= 0.10. s16x hcl j16: p >= 0.36.
- u3x.golang.real.j1 RSS: bimodal on both binaries at n=14 (main 54 to 134 MB, fix5r2 53 to 162 MB). The medians are 96.3 and 91.2 MB, p=0.83, so this is not a fix5r2 regression.
- s16y.hcl.real.j16 RSS +8%, p=0.14. s16y.scala.broken.j16 RSS +7%, p=0.40. These are noisy (main ranges 212 to 411 MB and 333 to 577 MB), not significant.
- c3 lua, java, php realerr j16 RSS, typescript realerr j1 and j16, javascript realerr j16: p >= 0.19.
- coldsw.json.0 wall +1.4%, p=0.14 (the suite had +36%). coldsw.rust.1 wall is equal.
- julia rep_b0b883 and verilog pair_ebd0a6 cold: RSS +0.46% and +0.15%, p 0.15 and 0.14. Wall is -6% and -16%.
- u3.python.real.j1, walk.golang.real.j16 and mix.j1 are equal or better.

## Memory win: kept

| sweep | j1 fix5r2/main | j16 fix5r2/main |
|---|---|---|
| suite-style passes (calib, ~4 s), n=3 | 0.650 (0.557..1.043) | 0.652 (0.463..1.133) |
| -n 2 -warm 1, n=7 | 0.838 (0.601..1.033) | 0.910 (0.703..1.056) |

The 0.64 headline depends on run length. With only 3 passes per process, main's heap has not grown as far, so the win is smaller (0.84 and 0.91), though it is still a win. The big-list j16 rows with garbage show fix5r2 at 0.5 to 0.6 of main's peak RSS. I could not measure C on the Mac.

## Not done

- No pod, so no instructions or cycles, and no GLR cycles check.
- No C at j16, no u4list or xall, and no 2/4/8 thread rows without garbage.
- n=3 only for the suite-style sweep.
