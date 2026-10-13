#!/usr/bin/env python3
"""Markdown report from the runner's JSONL files. Usage: report.py [--results DIR]
Ratios are gositter / other: below 1.00 means gositter is better.
"min" is the fastest timed pass of all rounds, "med" the median pass.
"""
import argparse, json, math, os, statistics, sys
from collections import defaultdict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from run import NOPRAGMA

CACHE = os.environ.get("BENCH_DATA") or os.path.expanduser("~/.cache/gositter-bench-9j5")
SIDES = ["gositter", "cgo", "c", "c-nopragma", "c-O3", "c-clang", "c-pic"]
NAME = {"gositter": "gs", "gositter-nocgo": "gs-nocgo", "cgo": "cgo", "c": "C", "c-nopragma": "C-nopragma",
        "c-O3": "C-O3", "c-clang": "C-clang", "c-pic": "C-pic"}
VS = [("c", "pure C -O2"), ("c-nopragma", "pure C -O2 without the pragma"), ("c-O3", "pure C -O3"),
      ("c-clang", "pure C -O2 built by clang"), ("cgo", "cgo")]
NO_MEM = ("c-nopragma", "c-O3", "c-clang", "c-pic")  # same memory as c: no memory ratios against these
# label, per-pass value, divisor, unit
TIME_M = [("wall", lambda r: r.get("wall_ns", 0), 1e6, "ms"),
          ("CPU user+sys", lambda r: r.get("user_ns", 0) + r.get("sys_ns", 0), 1e6, "ms"),
          ("instructions", lambda r: r.get("instr", 0), 1e6, "M"),
          ("cycles", lambda r: r.get("cycles", 0), 1e6, "M")]
MEM_M = [("peak RSS", "rss"), ("peak footprint", "fp"), ("RSS above loaded", "drss")]
# a (grammar, set) with fewer list bytes is left out of the summary geomeans:
# a pass over it takes microseconds and measures thread wake-up, not parsing
MIN_BYTES = 100000
pv = lambda *ev: lambda f: sum(f["perf"].get(e, 0) for e in ev)  # perf stat counts of run.py first
# the in-process counters start at main (C) or init (Go) and are user only there
LINUX_LAB = {"process instr": "user instr after main/init", "process cycles": "user cycles after main/init"}
# first-parse metrics: label, value from the "first" object, divisor, unit
FIRST_M = [("start to main", lambda f: f["start_to_main_ns"], 1e6, "ms"),
           ("init", lambda f: f["init_ns"], 1e6, "ms"),
           ("parse1", lambda f: f["parse1_ns"], 1e6, "ms"),
           ("init+parse1", lambda f: f["init_ns"] + f["parse1_ns"], 1e6, "ms"),
           ("parse2", lambda f: f["parse2_ns"], 1e6, "ms"),
           ("process user+sys", lambda f: f["user_ns"] + f["sys_ns"], 1e6, "ms"),
           ("process instr", lambda f: f["instr"], 1e6, "M"),
           ("process cycles", lambda f: f["cycles"], 1e6, "M"),
           ("exec to exit instr user+kernel", pv("instructions:u", "instructions:k"), 1e6, "M"),
           ("exec to exit instr user", pv("instructions:u"), 1e6, "M"),
           ("exec to exit instr kernel", pv("instructions:k"), 1e6, "M"),
           ("exec to exit cycles user+kernel", pv("cycles:u", "cycles:k"), 1e6, "M"),
           ("exec to exit task-clock", pv("task-clock"), 1, "ms"),
           ("peak RSS", lambda f: f["maxrss_kb"], 1024, "MB"),
           ("peak footprint", lambda f: f["footprint_peak_kb"], 1024, "MB")]
FIRST_SIDES = ["gositter", "gositter-nocgo", "cgo", "c"]
EXT_M = [("wall", "ms"), ("instr", "M"), ("RSS", "MB"), ("footprint", "MB")]
SETS = ("real", "realerr", "broken")
# traversal alone = min pass of what=walk|cursor minus min pass of what=parse
TRAV_M = ["instructions", "cycles", "CPU user+sys"]
TRAV_W = ["walk minus parse", "cursor minus parse"]
LOW_PAR = 0.7  # j=all rows with CPU/wall under LOW_PAR * j did not get the cores
JORD = lambda j: (0, int(j)) if j.isdigit() else (1, 0)


def f3(x):
    if x is None:
        return "n/a"
    if x == 0:
        return "0"
    s = f"{x:.3g}"
    if "e" in s:
        s = str(int(float(s)))
    return s


def ratio(a, b):
    return a / b if a is not None and b is not None and a > 0 and b > 0 else None


def gm(v):
    return math.exp(statistics.mean(map(math.log, v)))


def tbl(head, rows):
    out = ["| " + " | ".join(head) + " |", "|" + "|".join("---" for _ in head) + "|"]
    out += ["| " + " | ".join(str(c).replace("|", "\\|") for c in r) + " |" for r in rows]
    return "\n".join(out) + "\n"


def load(res, name):
    """One row per key: a rerun with a longer timeout (or cap) replaces the earlier row."""
    best = {}
    lim = lambda r: (r.get("timeout", 0), r.get("cap", 0))
    for l in open(os.path.join(res, name + ".jsonl")) if os.path.exists(os.path.join(res, name + ".jsonl")) else []:
        try:
            r = json.loads(l)
        except ValueError:
            continue
        if r["key"] not in best or lim(r) >= lim(best[r["key"]]):
            best[r["key"]] = r
    return list(best.values())


def npasses(rows):
    """Passes of a steady process, warm-up included, e.g. "6"."""
    n = lambda c: int(c[c.index("-n") + 1]) + int(c[c.index("-warm") + 1])
    return "/".join(str(x) for x in sorted({n(r["cmd"]) for r in rows})) or "?"


def pos(v):
    return v if v is not None and v > 0 else None


def passes(procs, fn, div):
    return [fn(r) / div for p in procs for r in p.get("reps", []) if fn(r) > 0]


def med(v):
    return statistics.median(v) if v else None


def rng(v):
    return f"{f3(min(v))}-{f3(max(v))}" if v else "n/a"


def mem(procs):
    """peak RSS, peak footprint, RSS minus loaded (MB), maxima over processes."""
    rss = [p["end"]["maxrss_kb"] / 1024 for p in procs if pos(p["end"].get("maxrss_kb"))]
    fp = [p["end"]["footprint_peak_kb"] / 1024 for p in procs if pos(p["end"].get("footprint_peak_kb"))]
    d = [(p["end"]["maxrss_kb"] - p["meta"]["rss_loaded_kb"]) / 1024 for p in procs
         if pos(p["end"].get("maxrss_kb")) and pos(p["meta"].get("rss_loaded_kb"))]
    return dict(rss=max(rss, default=None), fp=max(fp, default=None), drss=max(d, default=None))


def cpu_wall(procs):
    reps = [r for p in procs for r in p.get("reps", [])]
    return ratio(sum(r["user_ns"] + r["sys_ns"] for r in reps), sum(r["wall_ns"] for r in reps))


def starved(procs):
    """True when a j>1 config got under LOW_PAR * j cores: its wall is load noise."""
    cw = cpu_wall(procs)
    return bool(procs) and procs[0]["jn"] > 1 and cw is not None and cw < LOW_PAR * procs[0]["jn"]


def ext_val(r, how):
    rp, e = r["reps"][0], r["end"]
    return pos({"wall": rp["wall_ns"] / 1e6, "instr": rp["instr"] / 1e6, "RSS": e["maxrss_kb"] / 1024,
                "footprint": e["footprint_peak_kb"] / 1024}[how])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--results", default=os.path.join(CACHE, "results"))
    ap.add_argument("--bin", default=os.path.join(CACHE, "bin"), help="dir of BUILDINFO.txt")
    a = ap.parse_args()
    res = a.results
    try:  # the machine facts: BUILDINFO.txt up to its first empty line
        facts = open(os.path.join(a.bin, "BUILDINFO.txt")).read().split("\n\n")[0].splitlines()
    except OSError:
        facts = []
    linux = any(l.startswith("host: Linux") for l in facts)
    names = ("cmp", "matrix", "allocs", "mem", "first", "extreme")
    data = {n: load(res, n) for n in names}
    mx, al, me, fi, ex, cm = (data[n] for n in ("matrix", "allocs", "mem", "first", "extreme", "cmp"))
    allrows = [(n, r) for n in names for r in data[n]]
    mxp, mep = npasses(mx), npasses(me)

    cfg = defaultdict(lambda: defaultdict(list))  # (g,set,what,j) -> side -> ok procs
    for r in mx:
        if r["status"] == "ok":
            cfg[(r["grammar"], r["set"], r["what"], r["j"])][r["side"]].append(r)
    # (config, side) -> row of a side with no finished process in that config
    dead = {((r["grammar"], r["set"], r["what"], r["j"]), r["side"]): r for r in mx if r["status"] != "ok"}
    dead = {ks: r for ks, r in dead.items() if not cfg[ks[0]][ks[1]]}
    mm = defaultdict(dict)  # mem: (g,set,j) -> side -> row
    for r in me:
        mm[(r["grammar"], r["set"], r["j"])][r["side"]] = r
    grammars = sorted({k[0] for k in cfg} | {r["grammar"] for r in al + me + fi + ex + cm})
    nbytes = {(r["grammar"], r["set"]): r["meta"]["bytes"] for r in mx + me if r["status"] == "ok"}
    small = {k for k, b in nbytes.items() if b < MIN_BYTES}
    sl = lambda g, st: f"{st} (too small)" if (g, st) in small else st
    first_m = [(LINUX_LAB.get(m[0], m[0]) if linux else m[0],) + m[1:] for m in FIRST_M]

    def fv(k, s, x):
        """A value, or the status of the side that was killed there."""
        return dead[(k, s)]["status"] if x is None and (k, s) in dead else f3(x)
    order = lambda k: (k[1], k[2], JORD(k[3]))

    # R: every comparable matrix (config, metric, vs) with ratio and absolutes
    # R2: the same for allocs, first parse and extreme: (kind, grammar, item, metric, vs, ratio, gs, other, unit)
    R, R2 = [], []

    def add2(kind, g, item, metric, vs, a, b, unit):
        r = ratio(a, b)
        if r is not None:
            R2.append((kind, g, item, metric, vs, r, a, b, unit))

    def cells(k):
        """metric label -> side -> dict(med, lo, hi, v, unit)"""
        out = {}
        for lab, fn, div, unit in TIME_M:
            out[lab] = {}
            for s in SIDES:
                v = passes(cfg[k][s], fn, div)
                out[lab][s] = dict(med=med(v), lo=min(v, default=None), hi=max(v, default=None), unit=unit, v=v)
        for lab, key in MEM_M:
            out[lab] = {}
            for s in SIDES:
                m = mem(cfg[k][s])[key]
                out[lab][s] = dict(med=m, lo=m, hi=m, unit="MB", v=[m] if m else [])
        return out

    C = {k: cells(k) for k in cfg}
    memlabs = [m[0] for m in MEM_M]
    for k, cs in C.items():
        for lab, d in cs.items():
            if lab in memlabs:
                names = [(f"{lab} after {mxp} passes", "med")]
            else:
                names = [("wall min pass", "lo"), ("wall median", "med")] if lab == "wall" else [(lab, "med")]
            for name, stat in names:
                for vs, _ in VS:
                    if vs in NO_MEM and lab in memlabs:  # memory does not depend on -O or the pragma
                        continue
                    r = ratio(d["gositter"][stat], d[vs][stat])
                    if r is not None:
                        R.append((k, name, vs, r, d["gositter"][stat], d[vs][stat], d["gositter"]["unit"]))

    TRAV = {}  # (g,set,"<what> minus parse",j) -> metric -> side -> value or None
    for k in C:
        pk = (k[0], k[1], "parse", k[3])
        if k[2] == "parse" or pk not in C:
            continue
        tk = (k[0], k[1], k[2] + " minus parse", k[3])
        TRAV[tk] = {}
        for lab in TRAV_M:
            d = {s: pos(C[k][lab][s]["lo"] - C[pk][lab][s]["lo"])
                 if C[k][lab][s]["lo"] is not None and C[pk][lab][s]["lo"] is not None else None for s in SIDES}
            TRAV[tk][lab] = d
            for vs, _ in VS:
                r = ratio(d["gositter"], d[vs])
                if r is not None:
                    R.append((tk, lab + " min", vs, r, d["gositter"], d[vs], C[k][lab]["gositter"]["unit"]))

    # steady-state memory from the mem run: one process per (grammar, set, j, side)
    MEMV = {k: {s: mem([r]) if r["status"] == "ok" else None for s, r in d.items()} for k, d in mm.items()}
    for (g, st, j), d in MEMV.items():
        for lab, key in MEM_M:
            for vs in ("c", "cgo"):
                if d.get("gositter") and d.get(vs):
                    r = ratio(d["gositter"][key], d[vs][key])
                    if r is not None:
                        R.append(((g, st, "parse", j), f"steady {lab}", vs, r, d["gositter"][key], d[vs][key], "MB"))

    P = print
    P("# gositter vs C tree-sitter 0.25.0\n")
    P("Ratios are gositter divided by the other side: below 1.00 means gositter is better. "
      "`min` is the fastest timed pass over all rounds, `med` the median pass. The headline time columns are min-pass wall "
      "and user+sys CPU; median wall is secondary because it moves with the load on the machine. `CPU/wall` "
      f"(sum of user+sys over sum of wall) is the number of logical CPUs the process got: a j=all cell under {LOW_PAR} x j is marked `*`, "
      "its wall is load noise and only CPU, instructions and cycles count there. "
      "Wall and CPU are per pass over the whole list; CPU, instructions and cycles are process-wide and count all threads, "
      "including the Go GC. Peak columns are the maximum over the processes of that config. "
      f"A matrix process runs {mxp} passes (warm-up included) and the gositter peak at j=all still grows at that point, "
      f"so the matrix peaks are labelled `after {mxp} passes`; the memory numbers for the steady state are the `steady` "
      f"ones, from one process of {mep} passes per grammar, set, j and side (`run.py mem`). "
      + ("" if linux else "gositter instr and cycles come from the cgo build (gsbench). ") + "3 significant digits.\n")
    P("### Machine\n")
    P("From `bin/BUILDINFO.txt`:\n\n" + "\n".join("    " + l for l in facts) + "\n" if facts else
      f"No BUILDINFO.txt in `{a.bin}`: machine unknown, the notes below assume macOS.\n")
    if linux:
        P("Linux measures. Instructions and cycles are perf_event_open hardware counters, user mode only, of all threads "
          "(one inherited event per thread that exists when counting starts). Counting starts at the first line of main "
          "(C) or at package init (Go), not at exec; a 0 means perf_event_open was refused. That is right for a timed pass "
          "but not for a whole process: it leaves out the loader, the Go runtime start and all kernel work, and what is "
          "left out differs per side. So the cost of a first-parse process is in the `exec to exit` rows: each of these "
          "processes runs under `perf stat`, which counts user and kernel mode from exec to exit, and has its own "
          "counters switched off (BENCH_NOPERF), because the two sets together need more hardware counters than are "
          "free and perf would then scale its counts. Results from before that change have the in-process counts, as "
          "`user instr after main/init` and `user cycles after main/init`. `start to main` is not measured (n/a). `peak RSS` and `peak footprint` are both VmHWM of /proc/self/status, the peak resident set, so the two "
          "columns are equal here. ru_maxrss is not used: on Linux it keeps the peak of the process image before exec "
          "(the watchdog's python, about 10 MB), which is a floor under every small process. `RSS above loaded` is "
          "VmHWM minus VmRSS after the files were read. All Go sides are built without cgo for the counters; gsbench is the CGO_ENABLED=0 build.\n")
    else:
        P("macOS measures. Instructions and cycles are those of proc_pid_rusage (whole process, from exec). `peak RSS` "
          "is ru_maxrss, `peak footprint` is ri_lifetime_max_phys_footprint, which also counts compressed and swapped "
          "pages.\n")
    P("RSS on Linux and on macOS is not the same measure: the two kernels count shared, file-backed and compressed "
      "pages differently, the allocators differ (glibc malloc against the macOS zone allocator) and so does the page "
      "size (4 KB on amd64, 16 KB on Apple silicon). Compare memory only between sides on one machine, never across "
      "the two.\n")
    P("C is `cbench` at -O2 -std=c11, the best stock C build. "
      + ("These are not all the flags cgo uses here: on Linux cgo adds -fPIC (and -m64 -pthread) to every C file, while "
         "`cbench` is the gcc default, PIE, and with gcc -fPIC stops the inlining of exported functions in lib.c. "
         "C-pic is `cbench` with -fPIC on lib.c and the grammars (one round, parse and cursor only): C-pic against C is "
         "the cost of that flag, cgo against C-pic the cost of the binding. Both are printed under the summary. "
         if linux else "On macOS arm64 these are the flags cgo uses. ") +
      "C-O3 is the same sources at -O3, the default of the upstream tree-sitter Makefile; it runs parse and cursor only. "
      "C-nopragma is C with one source line removed, see below. "
      "C-clang, where it ran, is C built by clang instead of the compiler cgo uses (one round, parse and cursor only): "
      "it shows how much of the result is the C compiler. "
      "Exact flags: `bin/BUILDINFO.txt`, `bin/BUILDINFO-O3.txt` and `bin/BUILDINFO-clang.txt`.\n")
    P("`walk` and `cursor` rows time the parse plus the traversal of each tree. The `walk minus parse` and `cursor minus parse` "
      "rows are the traversal alone: min pass of the walk or cursor process minus min pass of the parse process of the same "
      "grammar, set, j and side. They are a difference of two processes, so small ones are noisy; a difference at or "
      "below zero prints n/a.\n")
    P("### What each side pays\n")
    P("Pure C (`C`, `C-O3`) is the baseline for the speed of the runtime. The cgo side measures the go-tree-sitter binding, "
      "not the C runtime.\n")
    P(f"- Optimisation pragma. The pinned parser.c of {len(NOPRAGMA)} grammars ({', '.join(NOPRAGMA)}) carries the line "
      "`#pragma clang optimize off` (for GCC: `#pragma GCC optimize (\"O0\")`), so the compiler builds the lexer and the "
      "tables of these grammars without optimisation at any -O level. That holds for `C`, `C-O3` and cgo alike, and it is how upstream ships these grammars, so the stock "
      "build stays the baseline. `C-nopragma` is `C` (-O2, same lib.o, same scanner objects) with only that line removed "
      f"from these {len(NOPRAGMA)} parser.c; it runs for these grammars only, for the others it would be `C`. gositter has no such "
      "switch: its tables and lexers are compiled like the rest of the Go code.")
    P("- Tree lifetime. A gositter tree lives in slabs owned by its parser and is valid only until the next Parse, Reset or "
      "SetLanguage on that parser; `Tree.Close` is a no-op. A C tree is owned by the caller and the runtime pays malloc and free "
      "per subtree for that. This harness keeps one live tree per parser and frees it at the next parse, which is the only "
      "lifetime gositter supports and its best case. C has no such reuse, by design of the library.\n"
      "- cgo binding. go-tree-sitter v0.25.0 routes every C malloc, calloc, realloc and free through a Go callback (its `init` "
      "calls `ts_set_allocator`), `Kind()` copies the name with `C.GoString`, `Child()` and the cursor's `Node()` allocate a "
      "`*Node` on the Go heap, and `Parser.Parse` reads the source through a callback that copies the rest of the source "
      "(`C.CString(text[i:])`) at each call and frees the copies when the parse ends. One 100 KB scala file needs about "
      "950 MB that way against 11 MB in pure C, so a cgo config can end in KILL_RSS. That is a memory result of the cgo "
      "side, not a missing measurement: its cells print KILL_RSS, the config is listed under `Configs a side did not "
      "finish` with the RSS of the other sides, and it is absent from the cgo ratios (the summary prints the n of each cell).\n"
      + ("  With glibc the freed copies stay in the malloc arena of the thread that made them, so at j above 1 the cgo RSS "
         "grows over the passes as a large file moves between threads. Measured here on one 45 KB scala file alone, "
         "j=16: 335 MB after 2 passes, 652 MB after 6, killed above 1024 MB before 21; 375 MB after 41 passes with "
         "`MALLOC_ARENA_MAX=1`, which no run of this report sets. The largest scala file needs 806 MB alone, so the cgo "
         "scala cells at j=all can be KILL_RSS at a cap well above that.\n" if linux else "") +
      "- Child(i) walk. `ts_node_child` scans from the first child on each call, so the `walk` rows are not the usual C "
      "traversal; `cursor` (TreeCursor) is.\n")
    envp = os.path.join(res, "env.json")
    if os.path.exists(envp):
        P(f"Malloc* and GO* variables seen by the benchmark processes: `{json.load(open(envp))['child_env']}`.\n")
    ld = [r["load1"] for r in mx if "load1" in r]
    if ld:
        P(f"1-minute load average at the end of each matrix process: min {f3(min(ld))}, median {f3(med(ld))}, max {f3(max(ld))}.\n")

    # ---- inputs
    P("## Inputs\n")
    P("`error files` is the number of files whose root has_error is set. `real` holds only the real files that parse "
      "without error; `splitreal.py` moved the others to `realerr` (counts per grammar in `lists/REALSPLIT.tsv`), the real "
      "files whose tree has an error. A grammar with no such file has no `realerr` rows. `broken` is mutated and "
      "adversarial input.\n")
    rows = []
    for g, st in sorted({(k[0], k[1]) for k in cfg}):
        procs = {s: [p for k in cfg if k[:2] == (g, st) for p in cfg[k][s]] for s in SIDES}
        errs = {s: sorted({r["error_files"] for p in procs[s] for r in p.get("reps", [])}) for s in SIDES}
        ref = next((p for s in SIDES for p in procs[s]), None)
        files, e = ref["meta"]["files"], errs["gositter"] or errs["c"]
        seen = {tuple(v) for v in errs.values() if v}
        rows.append([g, st, files, f3(ref["meta"]["bytes"] / 1e6), "/".join(map(str, e)),
                     f3(100 * e[0] / files) if e and files else "n/a", "yes" if len(seen) == 1 else f"NO {errs}",
                     "no, too small" if (g, st) in small else "yes"])
    P(tbl(["grammar", "set", "files", "MB", "error files", "% error files", "sides agree", "in summary"], rows) if rows else "no data\n")

    # ---- agreement
    P("## Cross-side agreement\n")
    P("For every walk and cursor config the node count of each pass and the checksum of the last pass (per node: first "
      "byte of the kind + start byte + end byte, summed over the list) of each side that finished are compared with "
      "gositter. Parse configs visit no node; their error-file counts are in the table above.\n")
    diff, ncmp, nside = [], 0, defaultdict(int)
    sig = lambda procs: (sorted({r["nodes"] for p in procs for r in p["reps"]}),
                         sorted({str(p.get("checksum", "not stored")) for p in procs}))
    for k in sorted((k for k in cfg if k[2] != "parse"), key=lambda k: (k[0],) + order(k)):
        gs = cfg[k]["gositter"]
        if not gs:
            continue
        ncmp += 1
        a = sig(gs)
        for s in SIDES[1:]:
            if cfg[k][s]:
                nside[s] += 1
                b = sig(cfg[k][s])
                if a != b or len(a[0]) != 1 or len(a[1]) != 1 or "not stored" in a[1]:
                    diff.append(f"{k[0]} {k[1]} {k[2]} j={k[3]}: gs nodes {a[0]} checksum {a[1]}, "
                                f"{NAME[s]} nodes {b[0]} checksum {b[1]}")
    with_n = ", ".join(f"{NAME[s]} in {n}" for s, n in nside.items())
    if not ncmp:
        P("no data\n")
    elif diff:
        P(f"Nodes and checksum agree: NO. {len(diff)} side-configs of {ncmp} configs differ ({with_n} compared):\n\n"
          + "\n".join("- " + x for x in diff) + "\n")
    else:
        P(f"Nodes and checksum agree: yes, in all {ncmp} walk and cursor configs ({with_n} compared with gositter).\n")

    # ---- not finished
    okrow = {(n, r["key"]): r for n, r in allrows if r["status"] == "ok"}

    def peer(n, r, side):
        """Peak RSS (MB) of the finished process of another side in the same config."""
        o = okrow.get((n, "|".join(side if x == r.get("side") else x for x in r["key"].split("|"))))
        return f3(o["end"]["maxrss_kb"] / 1024) if o and "end" in o else "n/a"
    unf = [(n, r) for n, r in allrows if n in ("cmp", "matrix", "allocs", "mem") and r["status"] != "ok"]
    P(f"## Configs a side did not finish: {len(unf)}\n")
    if unf:
        P("A process killed by the watchdog at its memory cap (KILL_RSS) or time limit (KILL_TIME). This is a result of that "
          "side, not a missing measurement: with `side` cgo the go-tree-sitter binding needed more memory than the cap for "
          "a list that gositter and pure C parsed with the peak RSS in the last two columns. `cmp` is the oracle, which "
          "holds the C tree through go-tree-sitter next to the gositter tree: a KILL_RSS there is cgo-side memory too and "
          "says nothing about the trees, which stay uncompared for that list. The per-grammar tables print the status in "
          "the cell of that side, and the config is absent from its ratios.\n")
        P(tbl(["run", "grammar", "set", "what", "j", "side", "status", "cap MB", "limit s", "wall s at the kill",
               "RSS MB at the kill", "gs peak RSS MB", "C peak RSS MB"],
              [[n, r["grammar"], r["set"], r.get("what") or r.get("mode") or ("cmp" if n == "cmp" else "parse"),
                r.get("j", "4" if n == "cmp" else "1"), r.get("side", "oracle (cgo + gositter)"), r["status"], r["cap"],
                r["timeout"], r["wd"].get("wall"), r["wd"].get("peak_mb"), peer(n, r, "gositter"), peer(n, r, "c")]
               for n, r in unf]))

    # ---- summary
    P("## Summary across grammars\n")
    P("Each cell: geometric mean (min-max) of the per-grammar ratio, and n = the number of grammars in that cell "
      "(a grammar whose process was killed on one side is missing from the cell, see `Configs a side did not finish`; "
      f"the C-nopragma cells hold only the {len(NOPRAGMA)} grammars with the pragma). "
      "CPU, instructions and cycles use the median pass. `walk` and `cursor` include the parse; the second table of each side "
      "is the traversal alone, from min passes.\n")
    P(f"A list under {MIN_BYTES} bytes is left out of every cell here: one pass over it takes well under a millisecond, so "
      "its numbers, above all at j=all, are thread wake-up and scheduler cost and not parsing. Its per-grammar tables are "
      "still printed, with the set marked `too small`. Left out: "
      + (", ".join(f"{g} {st} ({nbytes[(g, st)]} B)" for g, st in sorted(small)) or "none") + ".\n")
    js = sorted({k[3] for k in cfg}, key=JORD)
    st_n = sum(1 for k in cfg if k[3] != "1" for s in SIDES if cfg[k][s])
    st_bad = sum(1 for k in cfg if k[3] != "1" for s in SIDES if starved(cfg[k][s]))
    if st_n:
        P(f"{st_bad} of {st_n} side-configs at j above 1 got under {LOW_PAR} x j logical CPUs (CPU/wall): the wall columns of the "
          "j=all rows are valid only where that count is 0, CPU and cycles are the j=all numbers otherwise.\n")
    # j=all against j=1, per side and config, lists of MIN_BYTES and more
    pairs = [(C[k], C[k[:3] + ("1",)], s) for k in C if k[3] == "all" and k[:3] + ("1",) in C
             and (k[0], k[1]) not in small for s in SIDES]
    speed = [r for a, b, s in pairs for r in [ratio(b["wall"][s]["lo"], a["wall"][s]["lo"])] if r]
    cyc = [r for a, b, s in pairs for r in [ratio(a["cycles"][s]["med"], b["cycles"][s]["med"])] if r]
    topo = next((l.split(": ", 1)[1] for l in facts if l.startswith("topology: ")), None)
    jall = sorted({p["jn"] for k in cfg if k[3] == "all" for s in SIDES for p in cfg[k][s]})
    if jall:
        P(f"j=all is {'/'.join(map(str, jall))} threads" + (f" on {topo}" if topo else "") + ", the same for every side. "
          + ("With SMT that is two threads per physical core, so a j=all wall is not a one-thread-per-core number. "
             if topo and "smt active 1" in topo else "")
          + (f"In this run the j=1 min-pass wall is {f3(med(speed))}x ({rng(speed)}) the j=all one and a j=all pass costs "
             f"{f3(med(cyc))}x ({rng(cyc)}) the cycles of a j=1 pass: median (range) over {len(speed)} side-configs.\n"
             if speed and cyc else "\n"))

    def summary(whats, labs):
        rows = []
        for st in SETS:
            for j in js:
                for w in whats:
                    row, n = [st, j, w], 0
                    for lab in labs:
                        v = [x[3] for x in R if x[0][1] == st and x[0][3] == j and x[0][2] == w and x[1] == lab and x[2] == vs
                             and x[0][:2] not in small]
                        n = max(n, len(v))
                        row.append(f"{f3(gm(v))} ({f3(min(v))}-{f3(max(v))}) n={len(v)}" if v else "n/a")
                    if n:
                        rows.append(row)
        return tbl(["set", "j", "what"] + labs, rows) if rows else "no data\n"

    for vs, name in VS:
        if not any(x[2] == vs for x in R):  # side not run, e.g. C-nopragma without a pragma grammar
            continue
        P(f"### gositter / {name}\n")
        labs = ["wall min pass", "CPU user+sys", "wall median", "instructions", "cycles"]
        if vs not in NO_MEM:
            labs += [f"peak RSS after {mxp} passes", f"peak footprint after {mxp} passes"]
        P(summary(["parse", "walk", "cursor"], labs))
        P(f"### gositter / {name}: traversal alone\n")
        P(summary(TRAV_W, [m + " min" for m in TRAV_M]))
        if any(x[2] == vs and x[1].startswith("steady ") for x in R):
            P(f"### gositter / {name}: memory in the steady state ({mep} passes)\n")
            P(summary(["parse"], ["steady " + m[0] for m in MEM_M]))
    for side, den, name in (("c-O3", "c", "C -O3 divided by C -O2"), ("c-clang", "c", "C -O2 built by clang divided by C -O2"),
                            ("c-pic", "c", "C-pic divided by C (the -fPIC flag)"),
                            ("cgo", "c-pic", "cgo divided by C-pic (the binding)"), ("cgo", "c", "cgo divided by C (both)")):
        o3 = []
        for lab, stat in (("wall", "lo"), ("CPU user+sys", "med"), ("instructions", "med"), ("cycles", "med")):
            v = [r for r in (ratio(C[k][lab][side][stat], C[k][lab][den][stat]) for k in C
                             if k[:2] not in small and (side != "cgo" or cfg[k]["c-pic"])) if r]
            if v:
                o3.append(f"{lab} {f3(gm(v))} ({f3(min(v))}-{f3(max(v))}, n={len(v)})")
        if o3:
            P(f"{name}, geometric mean (min-max) over the configs that ran both: " + "; ".join(o3) + ".\n")

    # ---- per grammar
    allocmap = {(r["grammar"], r["set"], r["side"], r.get("mode", "steady")): r for r in al}
    for g in grammars:
        P(f"## {g}\n")
        ks = sorted((k for k in cfg if k[0] == g), key=order)
        if ks:
            gsd = [s for s in SIDES if any(cfg[k][s] or (k, s) in dead for k in ks)]  # sides of this grammar
            oth = [s for s in gsd if s != "gositter"]
            mds = [s for s in gsd if s != "c-O3"]
            for lab, fn, div, unit in TIME_M:
                rows = []
                for k in ks:
                    d = C[k][lab]
                    gs = d["gositter"]
                    row = [sl(g, k[1]), k[2], k[3]] + [fv(k, s, d[s]["lo"]) for s in gsd]
                    row += [f3(ratio(gs["lo"], d[v]["lo"])) for v in oth]
                    row += [fv(k, s, d[s]["med"]) for s in mds]
                    row += [f3(ratio(gs["med"], d[v]["med"])) for v in mds if v != "gositter"]
                    row += [rng(gs["v"]), rng(d["c"]["v"])]
                    if lab == "wall":
                        row += [f3(cpu_wall(cfg[k][s])) + (" *" if starved(cfg[k][s]) else "") for s in SIDES[:3]]
                        by = [p["meta"]["bytes"] for p in cfg[k]["gositter"]]
                        row.append(f3(by[0] / 1e6 / (gs["lo"] / 1e3)) if by and gs["lo"] else "n/a")
                    rows.append(row)
                head = (["set", "what", "j"] + [f"{NAME[s]} min" for s in gsd] + [f"min gs/{NAME[s]}" for s in oth] +
                        [f"{NAME[s]} med" for s in mds] + [f"med gs/{NAME[s]}" for s in mds if s != "gositter"] +
                        ["gs min-max", "C min-max"])
                if lab == "wall":
                    head += ["CPU/wall gs", "CPU/wall cgo", "CPU/wall C", "gs MB/s (min pass)"]
                P(f"### {g}: {lab} per pass ({unit})\n")
                P(tbl(head, rows))
            rows = []
            for tk in sorted((t for t in TRAV if t[0] == g), key=order):
                for lab in TRAV_M:
                    d = TRAV[tk][lab]
                    rows.append([sl(g, tk[1]), tk[2], tk[3], f"{lab} {C[(g, tk[1], 'parse', tk[3])][lab]['gositter']['unit']}"] +
                                [f3(d[s]) for s in gsd] + [f3(ratio(d["gositter"], d[v])) for v in oth])
            if rows:
                P(f"### {g}: traversal alone (min pass minus min parse pass)\n")
                P(tbl(["set", "what", "j", "metric"] + [NAME[s] for s in gsd] + [f"gs/{NAME[s]}" for s in oth], rows))
            rows = []
            for k in ks:
                row = [k[1], k[2], k[3]]
                for lab in memlabs:
                    d = C[k][lab]
                    row += [fv(k, s, d[s]["med"]) for s in SIDES[:3]] + [f3(ratio(d["gositter"]["med"], d["cgo"]["med"])),
                                                                       f3(ratio(d["gositter"]["med"], d["c"]["med"]))]
                rows.append(row)
            head = ["set", "what", "j"]
            for n in ("RSS", "footprint", "RSS-loaded"):
                head += [f"{n} gs MB", "cgo MB", "C MB", "gs/cgo", "gs/C"]
            P(f"### {g}: memory after {mxp} passes (matrix, max over processes)\n")
            P(tbl(head, rows))
        # steady-state memory
        rows = []
        for k in sorted((k for k in MEMV if k[0] == g), key=lambda k: (k[1], JORD(k[2]))):
            d, row = MEMV[k], [k[1], k[2]]
            for lab, key in MEM_M:
                v = {s: (d[s] or {}).get(key) if s in d else None for s in SIDES[:3]}
                row += [f3(v[s]) if v[s] is not None or s not in d else mm[k][s]["status"] for s in SIDES[:3]]
                row += [f3(ratio(v["gositter"], v["cgo"])), f3(ratio(v["gositter"], v["c"]))]
            rows.append(row)
        if rows:
            head = ["set", "j"]
            for n in ("RSS", "footprint", "RSS-loaded"):
                head += [f"{n} gs MB", "cgo MB", "C MB", "gs/cgo", "gs/C"]
            P(f"### {g}: memory in the steady state (parse, one process of {mep} passes per cell)\n")
            P(tbl(head, rows))
        # allocations
        rows, gs_steady = [], []
        for st, mode in ((st, m) for st in SETS for m in ("cold", "steady")):
            a = {s: allocmap.get((g, st, s, mode)) for s in SIDES}
            if not any(a.values()):
                continue

            def per(side, key):
                r = a[side]
                if not r or r["status"] != "ok" or r["end"].get("parses", 0) <= 0 or r["end"].get(key, -1) < 0:
                    return None
                return r["end"][key] / r["end"]["parses"]
            gs_n, gs_b = per("gositter", "go_mallocs"), per("gositter", "go_alloc_bytes")
            cg_gn, cg_n, cg_b = per("cgo", "go_mallocs"), per("cgo", "allocs"), per("cgo", "alloc_bytes")
            c_n, c_b = per("c", "allocs"), per("c", "alloc_bytes")
            rows.append([st, mode, f3(gs_n), f3(gs_b), f3(cg_gn), f3(cg_n), f3(cg_b), f3(c_n), f3(c_b), f3(ratio(gs_n, c_n))])
            if mode == "steady" and gs_n is not None:
                gs_steady.append(f"{f3(gs_n)} ({st})")
            for vs, n in (("c", c_n), ("cgo", cg_n)):
                add2("allocs", g, f"{st} {mode}", "allocations per parse", vs, gs_n, n, "")
        if rows:
            P(f"### {g}: allocations per parse\n")
            P("`cold` is the first pass of a new parser (-warm 0), `steady` the pass after one warm-up pass over the same files. "
              "A gositter tree is valid only until the next Parse on its parser, which reuses the tree memory, so its steady "
              "count is what a re-parse still allocates: " + (", ".join(gs_steady) or "n/a") + " mallocs per parse here. "
              "A C tree is owned by the caller and every parse allocates it again. `gs new heap bytes` is Go TotalAlloc, new heap only, not the memory a parse uses; `C bytes` is the sum "
              "of the requested sizes. The two are not compared.\n")
            P(tbl(["set", "parser", "gs mallocs", "gs new heap bytes", "cgo Go mallocs", "cgo C allocs", "cgo C bytes", "C allocs",
                   "C bytes", "gs/C allocs"], rows))
        # first parse
        grp = defaultdict(list)
        for r in fi:
            if r["grammar"] == g and r["status"] == "ok":
                r["first"]["perf"] = r.get("perf") or {}
                grp[(r["file_idx"], r["side"])].append(r)
        rows = []
        fsides = [s for s in FIRST_SIDES if any(k[1] == s for k in grp)]  # sides that ran
        for fx in sorted({k[0] for k in grp}):
            anyr = next(rs[0] for (f, _), rs in grp.items() if f == fx)
            item = f"#{fx} {anyr['first'].get('file_bytes', '')} B"
            for lab, fn, div, unit in first_m:
                v = {s: med([fn(r["first"]) / div for r in grp.get((fx, s), []) if fn(r["first"]) > 0]) for s in FIRST_SIDES}
                if not any(v.values()):  # not measured on this OS
                    continue
                rows.append([item, f"{lab} {unit}"] + [f3(v[s]) for s in fsides] +
                            [f3(ratio(v["gositter"], v["cgo"])), f3(ratio(v["gositter"], v["c"])),
                             max((len(grp.get((fx, s), [])) for s in fsides))])
                for vs in ("c", "cgo"):
                    add2("first parse", g, item, lab, vs, v["gositter"], v[vs], unit)
        if rows:
            P(f"### {g}: first parse in a new process (median of K processes)\n")
            P("`start to main` is process start to entry of main (gs-nocgo cannot read it), `init` is language + parser, "
              "`parse1` the first parse, `parse2` the second parse of the same file. "
              + ("`process user+sys` is getrusage after parse2. The `exec to exit` rows are `perf stat` on the whole "
                 "process, user and kernel, both parses and the exit included: the cost of a new process. Any "
                 "`after main/init` row is a user-mode count from the first line of main (C) or package init (Go) until "
                 "after parse2 and leaves out the loader, the runtime start and the kernel.\n" if linux else
                 "`process` rows cover the whole process "
                 "until after parse2. start to main + init + parse1 is the cost of a new process up to its first tree.\n"))
            P(tbl(["file", "metric"] + [NAME[s] for s in fsides] + ["gs/cgo", "gs/C", "K"], rows))
        # cmp
        rows = [[r["set"], *(r["summary"].get(k, "") for k in ("files", "nodes", "trees_with_errors", "mismatching_files")),
                 "ok" if r["summary"].get("mismatching_files") == 0 else "MISMATCH"] if r["status"] == "ok" else
                [r["set"], "", "", "", "", f"not compared: oracle {r['status']} at the {r['cap']} MB cap "
                 f"({r['wd'].get('peak_mb')} MB at the kill), memory of its cgo side"]
                for r in cm if r["grammar"] == g]
        if rows:
            P(f"### {g}: cmp (C tree vs gositter tree)\n")
            P(tbl(["set", "files", "nodes", "trees_with_errors", "mismatching_files", "result"], rows))

    # ---- extreme
    P("## Extreme inputs (one process each, parse, j=1, 1 pass, no warm-up)\n")
    lim = lambda key: "/".join(str(x) for x in sorted({r[key] for r in ex if r.get(key)})) or "unknown"
    ex_lim = f"time cap {lim('timeout')} s, memory cap {lim('cap')} MB"
    P(f"Each process has a {ex_lim}. An input that needs more on both sides is not compared on time or memory use; "
      "for those the last section lists the wall and the polled RSS peak at the kill. "
      "Sides run here: " + ", ".join(NAME[s] for s in SIDES if any(r["side"] == s for r in ex)) + ".\n")
    ge = defaultdict(lambda: defaultdict(dict))
    for r in ex:
        ge[r["grammar"]][r["file"]][r["side"]] = r
    exs = [s for s in SIDES if any(r["side"] == s for r in ex)]
    others = [s for s in exs if s != "gositter"]
    # per other side: files where both finished, gositter killed only, other killed only, both killed
    tally = {s: dict(both=0, gs_dead=[], other_dead=[], dead=[]) for s in others}
    for g in sorted(ge):
        rows = []
        for f, sd in ge[g].items():
            base = os.path.basename(f)
            ok = {s: sd[s]["status"] == "ok" for s in sd}
            row = [base]
            for how, unit in EXT_M:
                v = {s: ext_val(sd[s], how) if ok.get(s) else None for s in exs}
                row += [f3(v[s]) if ok.get(s) or s not in sd else sd[s]["status"] for s in exs]
                row += [f3(ratio(v["gositter"], v[s])) for s in others]
                for s in others:
                    add2("extreme", g, base, how, s, v.get("gositter"), v[s], unit)
            rows.append(row)
            for s in others:
                if "gositter" not in sd or s not in sd:
                    continue
                t, w = tally[s], lambda x: f"{sd[x]['status']} after {sd[x]['wd'].get('wall')} s at {sd[x]['wd'].get('peak_mb')} MB"
                if ok["gositter"] and ok[s]:
                    t["both"] += 1
                elif ok[s]:
                    t["gs_dead"].append(f"{g}/{base} (gositter {w('gositter')}; {NAME[s]} {f3(ext_val(sd[s], 'wall'))} ms, {f3(ext_val(sd[s], 'RSS'))} MB)")
                elif ok["gositter"]:
                    t["other_dead"].append(f"{g}/{base} ({NAME[s]} {w(s)}; gositter {f3(ext_val(sd['gositter'], 'wall'))} ms, {f3(ext_val(sd['gositter'], 'RSS'))} MB)")
                else:
                    t["dead"].append(f"{g}/{base} (gositter {w('gositter')}; {NAME[s]} {w(s)})")
                    add2("extreme killed on both", g, base, "RSS peak at the kill", s, sd["gositter"]["wd"].get("peak_mb"),
                         sd[s]["wd"].get("peak_mb"), "MB")
        head = ["file"]
        for n, unit in EXT_M:
            head += [f"{n} {unit} {NAME[s]}" for s in exs] + [f"{n} gs/{NAME[s]}" for s in others]
        P(f"### {g}\n")
        P(tbl(head, rows))

    # ---- slower / bigger
    P("## Cases where gositter is slower or uses more memory\n")
    P("Every ratio above 1.00 in this report, every input that gositter did not finish while the other side did, and every "
      "input killed on both sides. A killed process hit the memory or time cap of its run (`wd` in the last table); "
      f"extreme inputs: {ex_lim}. For an input killed on both sides the only comparison is the RSS peak the watchdog saw "
      "before the kill.\n")
    for vs, name in VS:
        nm = sum(1 for x in R if x[2] == vs)
        n2 = sum(1 for x in R2 if x[4] == vs)
        if not nm and not n2 and vs not in tally and not any(r.get("side") == vs for _, r in unf):
            continue
        P(f"### gositter vs {name}\n")
        worse = sorted((x for x in R if x[2] == vs and x[3] > 1.0), key=lambda x: -x[3])
        P(f"Matrix and steady-state memory: {len(worse)} of {nm} config-metrics have a ratio above 1.00.\n")
        if worse:
            P(tbl(["ratio", "grammar", "set", "what", "j", "metric", "gositter", name, "unit"],
                  [[f3(x[3]), x[0][0], sl(*x[0][:2]), x[0][2], x[0][3], x[1], f3(x[4]), f3(x[5]), x[6]] for x in worse]))
        dk = [(n, r) for n, r in unf if r.get("side") in ("gositter", vs)]
        gs_only = [f"{n} {r['key']} ({r['status']} at {r['wd'].get('peak_mb')} MB, {name} peak RSS {peer(n, r, vs)} MB)"
                   for n, r in dk if r["side"] == "gositter" and peer(n, r, vs) != "n/a"]
        vs_only = [f"{n} {r['key']} ({r['status']} at {r['wd'].get('peak_mb')} MB, gositter peak RSS {peer(n, r, 'gositter')} MB)"
                   for n, r in dk if r["side"] == vs and peer(n, r, "gositter") != "n/a"]
        if dk:
            P(f"Matrix, allocs and mem processes: {len(gs_only)} killed on gositter and finished on {name}, "
              f"{len(vs_only)} killed on {name} and finished on gositter.\n")
            for title, v in ((f"gositter killed, {name} finished (worse for gositter)", gs_only),
                             (f"{name} killed, gositter finished", vs_only)):
                if v:
                    P(f"{title}:\n\n" + "\n".join("- " + x for x in v) + "\n")
        for kind in ("extreme", "extreme killed on both", "first parse", "allocs"):
            allk = [x for x in R2 if x[4] == vs and x[0] == kind]
            worse = sorted((x for x in allk if x[5] > 1.0), key=lambda x: -x[5])
            if allk:
                P(f"{kind.capitalize()}: {len(worse)} of {len(allk)} file-metrics have a ratio above 1.00.\n")
            if worse:
                P(tbl(["ratio", "grammar", "item", "metric", "gositter", name, "unit"],
                      [[f3(x[5]), x[1], x[2], x[3], f3(x[6]), f3(x[7]), x[8]] for x in worse]))
        if vs in tally:
            t = tally[vs]
            P(f"Extreme files: {t['both']} finished on both sides, {len(t['gs_dead'])} gositter killed and {name} finished, "
              f"{len(t['other_dead'])} {name} killed and gositter finished, {len(t['dead'])} killed on both.\n")
            for title, key in ((f"gositter killed, {name} finished (worse for gositter)", "gs_dead"),
                               (f"{name} killed, gositter finished", "other_dead"),
                               (f"killed on both sides, not compared ({ex_lim})", "dead")):
                if t[key]:
                    P(f"{title}:\n\n" + "\n".join("- " + x for x in t[key]) + "\n")
    bad = [(n, r) for n, r in allrows if r["status"] != "ok"]
    P(f"### Killed or failed processes: {len(bad)}\n")
    if bad:
        P(tbl(["file", "key", "status", "wd", "stderr tail"],
              [[n, r["key"], r["status"], r["wd"], " / ".join(r.get("stderr_tail", []))[:200]] for n, r in bad]))


if __name__ == "__main__":
    main()
