#!/usr/bin/env python3
"""Serial, resumable benchmark runner. Every process goes through brun.sh.

Usage: run.py {cmp,matrix,allocs,mem,first,extreme,all} [flags]   (see --help)
Each subcommand appends one JSON row per process to <out>/<name>.jsonl.
"""
import argparse, calendar, json, os, re, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))
CACHE = os.environ.get("BENCH_DATA") or os.path.expanduser("~/.cache/gositter-bench-9j5")
GRAMMARS = ("bash c cpp csharp golang hcl java javascript json julia kotlin lua php "
            "python ruby rust scala tsx typescript verilog zig").split()
BINS = {"gositter": "gsbench", "gositter-nocgo": "gsbench-nocgo", "cgo": "cgobench", "c": "cbench",
        "c-nopragma": "cbench-nopragma", "c-O3": "cbench-O3", "c-clang": "cbench-clang", "c-pic": "cbench-pic"}
# Default sides per subcommand (--sides overrides). c-O3 is cbench built at -O3
# and never runs what=walk. Extreme has no cgo side to fit the time budget.
# c-clang (build.sh with BENCH_CC=clang) and c-pic (Linux: cbench with -fPIC,
# as cgo compiles) join the matrix when their binary exists: round 0 only, no walk.
ONE_ROUND = ["c-clang", "c-pic"]
# first mode on Linux runs under perf stat: counts from exec, user and kernel.
# BENCH_NOPERF turns the in-process counters off: 4 events here plus their 2
# are more than the 5 free hardware counters, and perf would scale its counts.
PERF_EV = "instructions:u,instructions:k,cycles:u,cycles:k,task-clock"
SIDES = {"matrix": ["gositter", "cgo", "c", "c-nopragma", "c-O3"], "allocs": ["gositter", "cgo", "c"],
         "mem": ["gositter", "cgo", "c"],
         "first": ["gositter", "gositter-nocgo", "cgo", "c"], "extreme": ["gositter", "c"]}
if sys.platform != "darwin":  # gsbench is the nocgo build there
    SIDES["first"].remove("gositter-nocgo")
# Grammars whose parser.c has "#pragma clang optimize off" and the GCC form of
# it (build.sh prints them into BUILDINFO.txt). c-nopragma runs only for these:
# for the others it is c.
NOPRAGMA = "bash c cpp csharp julia kotlin ruby verilog".split()
MEM_REPS = 40  # timed passes of a mem process, enough for the gositter peak to stop growing
GO_TUNING = ("GOGC", "GOMAXPROCS", "GODEBUG", "GOMEMLIMIT")
WD_RE = re.compile(r"^WD rc=(\S+) wall=([\d.]+) peak_mb=(\d+) maxrss_mb=(\d+)")


def ncpus():
    """CPUs this process may run on: what --js all means."""
    return len(os.sched_getaffinity(0)) if hasattr(os, "sched_getaffinity") else os.cpu_count()


def rot(xs, k):
    k %= len(xs)
    return xs[k:] + xs[:k]


def is_done(row, cap, timeout):
    """A stored row settles its key unless this run gives the limit that killed it more room."""
    st = row.get("status")
    more_mem, more_time = cap > row.get("cap", 0), timeout > row.get("timeout", 0)
    if st == "ok":
        return True
    if st == "KILL_RSS":
        return not more_mem
    if st == "KILL_TIME":
        return not more_time
    return not (more_mem or more_time)


def check_fresh(a):
    """Refuse to append to rows measured with older binaries."""
    try:
        built = next(l.split()[1] for l in open(os.path.join(a.bin, "BUILDINFO.txt")) if l.startswith("built:"))
        built = calendar.timegm(time.strptime(built, "%Y-%m-%dT%H:%M:%SZ"))
    except (OSError, StopIteration, ValueError) as e:
        sys.exit(f"cannot read the build time of {a.bin}/BUILDINFO.txt: {e}")
    for n in sorted(os.listdir(a.out)) if os.path.isdir(a.out) else []:
        if n.endswith(".jsonl"):
            first = open(os.path.join(a.out, n)).readline()
            if first.strip() and json.loads(first)["ts"] < built:
                sys.exit(f"REFUSING TO RESUME: {a.out}/{n} starts before the binaries were built "
                         f"(BUILDINFO.txt built: {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(built))}). "
                         "Its rows are from other binaries. Use a new --out or move the old files away.")


def read_perf(path):
    """event -> count from a "perf stat -x ," file, which is then removed. task-clock is in ms."""
    out = {}
    try:
        for l in open(path):
            f = l.strip().split(",")
            if len(f) > 4 and f[0].replace(".", "").isdigit() and f[4] == "100.00":  # not scaled
                out[f[2]] = float(f[0])
        os.remove(path)
    except OSError:
        pass
    return out


class Runner:
    def __init__(self, a):
        self.a = a
        self.brun = a.brun
        os.makedirs(a.out, exist_ok=True)
        self.tmp = os.path.join(a.out, "tmp")
        os.makedirs(self.tmp, exist_ok=True)
        self.t0 = time.time()
        self.check_env()

    def sides(self, name):
        if self.a.sides:
            return self.a.sides
        extra = [s for s in ONE_ROUND if name == "matrix" and os.path.exists(self.binp(s))]
        return SIDES[name] + extra

    def check_env(self):
        """Record the Malloc*/GO* variables a benchmark process sees. A Malloc* one would skew
        the C sides, a Go runtime tuning one the Go sides: abort on either."""
        r = subprocess.run([self.brun, "-t", "10", "/usr/bin/env"], capture_output=True, text=True)
        env = dict(l.split("=", 1) for l in r.stdout.splitlines() if re.match(r"(Malloc|MALLOC_|GLIBC_TUNABLES|GO)\w*=", l))
        with open(os.path.join(self.a.out, "env.json"), "w") as f:
            json.dump({"child_env": env, "ts": time.time(), "loadavg": os.getloadavg()}, f)
        bad = [k for k in env if k.startswith(("Malloc", "MALLOC_", "GLIBC_TUNABLES")) or k in GO_TUNING]
        if r.returncode or bad:
            sys.exit(f"benchmark environment not clean: rc={r.returncode} {bad}")

    # ---- lists
    def lst(self, g, name):
        p = os.path.join(self.a.lists, f"{g}.{name}.txt")
        if not os.path.exists(p):
            print(f"skip: missing {p}", file=sys.stderr)
            return None
        lines = [l.strip() for l in open(p) if l.strip()]
        if not lines:
            print(f"skip: empty {p}", file=sys.stderr)
            return None
        if self.a.smoke and name in ("real", "realerr", "broken"):
            p = os.path.join(self.tmp, f"smoke.{g}.{name}.txt")
            open(p, "w").write("\n".join(lines[:20]) + "\n")
        return p

    def lines(self, g, name):
        p = self.lst(g, name)
        return [l.strip() for l in open(p) if l.strip()] if p else []

    def one_line_list(self, f):
        fd, p = tempfile.mkstemp(dir=self.tmp, suffix=".txt")
        with os.fdopen(fd, "w") as fh:
            fh.write(f + "\n")
        return p

    def binp(self, side):
        return os.path.join(self.a.bin, BINS.get(side, side))

    # ---- execution
    def stored(self, name):
        """-> path, key -> rows already in the output file."""
        p = os.path.join(self.a.out, name + ".jsonl")
        rows = {}
        if os.path.exists(p):
            for l in open(p):
                try:
                    r = json.loads(l)
                    rows.setdefault(r["key"], []).append(r)
                except (ValueError, KeyError):
                    pass
        return p, rows

    def execute(self, name, tasks):
        """tasks: dicts with key, cmd, cap, timeout, need ('end'|'first'|'summary'), meta."""
        path, rows = self.stored(name)
        todo = [t for t in tasks if not any(is_done(r, t["cap"], t["timeout"]) for r in rows.get(t["key"], []))]
        print(f"{name}: {len(tasks)} planned, {len(tasks) - len(todo)} already done, {len(todo)} to run",
              file=sys.stderr)
        t_start = time.time()
        with open(path, "a") as out:
            for i, t in enumerate(todo, 1):
                row = self.run_task(t)
                out.write(json.dumps(row) + "\n")
                out.flush()
                el = time.time() - t_start
                eta = el / i * (len(todo) - i)
                print(f"[{name} {i}/{len(todo)}] el={el:.0f}s eta={eta:.0f}s {t['key']} "
                      f"-> {row['status']} wall={row['wd'].get('wall')}", file=sys.stderr, flush=True)

    def run_task(self, t):
        cmd = [self.brun, "-m", str(t["cap"]), "-t", str(t["timeout"])] + t["cmd"]
        waited = 0
        while t.get("quiet") and waited < self.a.load_wait and os.getloadavg()[0] > self.a.max_load:
            time.sleep(5)
            waited += 5
        load0 = os.getloadavg()[0]
        t1 = time.perf_counter()
        r = subprocess.run(cmd, capture_output=True, text=True, errors="replace")
        outer = time.perf_counter() - t1  # includes lock wait + wd poll latency
        wd = {}
        for l in reversed(r.stderr.splitlines()):
            m = WD_RE.match(l)
            if m:
                wd = {"rc": m[1], "wall": float(m[2]), "peak_mb": int(m[3]), "maxrss_mb": int(m[4])}
                break
        objs, texts = {}, []
        for l in r.stdout.splitlines():
            if l.startswith("{"):
                try:
                    o = json.loads(l)
                except ValueError:
                    texts.append(l)
                    continue
                if o.get("type") == "rep":
                    objs.setdefault("reps", []).append(o)
                else:
                    objs[o.get("type")] = o
            else:
                texts.append(l)
        row = {"key": t["key"], **t["meta"], "ts": time.time(), "cmd": t["cmd"], "rc": r.returncode,
               "wd": wd, "outer_wall_s": outer, "load1": os.getloadavg()[0], "load1_before": load0,
               "cap": t["cap"], "timeout": t["timeout"]}
        row.update(objs)
        if t.get("perf"):
            row["perf"] = read_perf(t["perf"])
        m = re.search(r"^checksum (\d+)$", r.stderr, re.M)  # the walk checksum of the last pass
        if m:
            row["checksum"] = int(m[1])
        if t["need"] == "summary":
            for l in texts:
                if "files=" in l and "mismatching_files=" in l:
                    row["summary"] = {k: int(v) for k, v in re.findall(r"(\w+)=(\d+)", l)}
            row["mismatch_lines"] = [l for l in texts if l.startswith("MISMATCH")][:20]
            have = "summary" in row
        elif t["need"] == "end":
            have = "end" in objs and len(objs.get("reps", [])) > 0
        else:
            have = t["need"] in objs
        wrc = wd.get("rc", "")
        if wrc.startswith("KILL"):
            row["status"] = wrc
        elif not wd:
            row["status"] = "no_wd_line"
        elif not have:
            row["status"] = f"rc={r.returncode}_no_output"
        else:
            row["status"] = "ok"  # nonzero rc with output (cmp mismatches) is data, not failure
        if row["status"] != "ok":
            row["stderr_tail"] = r.stderr.splitlines()[-6:]
        return row

    # ---- planners
    def grammars(self):
        return self.a.grammars

    def cmp(self):
        tasks = []
        for g in self.grammars():
            for s in self.a.sets:
                p = self.lst(g, s)
                if p:
                    tasks.append(dict(key=f"{g}|{s}", cmd=[self.a.oracle, "cmp", g, p, "-j", "4"],
                                      cap=self.a.cap, timeout=self.a.timeout, need="summary",
                                      meta=dict(grammar=g, set=s)))
        self.execute("cmp", tasks)

    def matrix(self):
        tasks, ci = [], 0
        ncpu = ncpus()
        for g in self.grammars():
            for s in self.a.sets:
                p = self.lst(g, s)
                if not p:
                    continue
                for w in self.a.whats:
                    for jl in self.a.js:
                        jn = ncpu if jl == "all" else int(jl)
                        for r in range(self.a.rounds):
                            for pos, side in enumerate(rot(self.sides("matrix"), r + ci)):
                                if side in ["c-O3"] + ONE_ROUND and w == "walk" or r > 0 and side in self.a.one_round + ONE_ROUND:
                                    continue
                                if side == "c-nopragma" and g not in NOPRAGMA:
                                    continue
                                tasks.append(dict(
                                    key=f"{g}|{s}|{w}|{jl}|{r}|{side}",
                                    cmd=[self.binp(side), "-g", g, "-l", p, "-j", str(jn), "-what", w,
                                         "-n", str(self.a.reps), "-warm", "1"],
                                    cap=self.a.cap, timeout=self.a.timeout, need="end", quiet=jl == "all",
                                    meta=dict(grammar=g, set=s, what=w, j=jl, jn=jn, round=r, side=side,
                                              pos=pos, ci=ci)))
                        ci += 1
        self.execute("matrix", tasks)

    def allocs(self):
        """cold = the first pass of new parsers, steady = the pass after one warm-up pass over the same files."""
        tasks = []
        for g in self.grammars():
            for s in self.a.sets:
                p = self.lst(g, s)
                if not p:
                    continue
                for side in self.sides("allocs"):
                    for mode, warm in (("cold", "0"), ("steady", "1")):
                        tasks.append(dict(key=f"{g}|{s}|{side}|{mode}",
                                          cmd=[self.binp(side), "-g", g, "-l", p, "-what", "parse", "-j", "1",
                                               "-n", "1", "-warm", warm, "-allocs"],
                                          cap=self.a.cap, timeout=self.a.timeout, need="end",
                                          meta=dict(grammar=g, set=s, side=side, mode=mode)))
        self.execute("allocs", tasks)

    def mem(self):
        """Peak memory after MEM_REPS passes: the gositter peak grows over the first passes at j=all."""
        tasks = []
        ncpu = ncpus()
        for g in self.grammars():
            for s in self.a.sets:
                p = self.lst(g, s)
                if not p:
                    continue
                for jl in ("all", "1"):
                    for side in self.sides("mem"):
                        jn = ncpu if jl == "all" else 1
                        tasks.append(dict(key=f"{g}|{s}|{jl}|{side}",
                                          cmd=[self.binp(side), "-g", g, "-l", p, "-j", str(jn), "-what", "parse",
                                               "-n", str(MEM_REPS), "-warm", "1"],
                                          cap=self.a.cap, timeout=self.a.timeout, need="end",
                                          meta=dict(grammar=g, set=s, j=jl, jn=jn, side=side)))
        self.execute("mem", tasks)

    def first(self):
        tasks = []
        pf = os.path.join(self.tmp, "perf.csv")
        pre = ["env", "BENCH_NOPERF=1", "perf", "stat", "-x", ",", "-o", pf, "-e", PERF_EV] if sys.platform == "linux" else []
        for g in self.grammars():
            for fi, f in enumerate(self.lines(g, "first")[:2]):
                for k in range(self.a.k):
                    for side in rot(self.sides("first"), k):
                        tasks.append(dict(key=f"{g}|{fi}|{k}|{side}",
                                          cmd=pre + [self.binp(side), "-first", "-g", g, "-f", f],
                                          cap=self.a.cap, timeout=self.a.timeout, need="first", perf=pre and pf,
                                          meta=dict(grammar=g, file=f, file_idx=fi, k=k, side=side)))
        self.execute("first", tasks)

    def extreme(self):
        tasks = []
        for g in self.grammars():
            files = self.lines(g, "extreme")
            for f in (files[:2] if self.a.smoke else files):
                lp = self.one_line_list(f)
                for side in self.sides("extreme"):
                    tasks.append(dict(key=f"{g}|{f}|{side}",
                                      cmd=[self.binp(side), "-g", g, "-l", lp, "-what", "parse", "-j", "1",
                                           "-n", "1", "-warm", "0"],
                                      cap=self.a.cap, timeout=self.a.timeout, need="end",
                                      meta=dict(grammar=g, file=f, side=side)))
        self.execute("extreme", tasks)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("cmd", choices=["cmp", "matrix", "allocs", "mem", "first", "extreme", "all"])
    ap.add_argument("--out", default=os.path.join(CACHE, "results"))
    ap.add_argument("--bin", default=os.path.join(CACHE, "bin"))
    ap.add_argument("--lists", default=os.path.join(CACHE, "lists"))
    ap.add_argument("--grammars", type=lambda s: s.split(","), default=None)
    ap.add_argument("--sets", type=lambda s: s.split(","), default=["real", "realerr", "broken"],
                    help="real (clean files), realerr (real files with has_error, see splitreal.py), broken")
    ap.add_argument("--whats", type=lambda s: s.split(","), default=["parse", "walk", "cursor"])
    ap.add_argument("--js", type=lambda s: s.split(","), default=["1", "all"], help="e.g. 1,all")
    ap.add_argument("--sides", type=lambda s: s.split(","), default=None,
                    help="gositter,gositter-nocgo,cgo,c,c-nopragma,c-O3,c-clang,c-pic; default per subcommand, see SIDES")
    ap.add_argument("--rounds", type=int, default=3)
    ap.add_argument("--one-round", type=lambda s: s.split(","), default=["none"],
                    help="matrix sides that run in round 0 only, e.g. cgo,c-O3 (default none: all sides every round)")
    ap.add_argument("--max-load", type=float, default=3.0,
                    help="with --load-wait: 1-minute load average a j=all process waits for")
    ap.add_argument("--load-wait", type=int, default=0,
                    help="SEC a j=all process may wait for --max-load (default 0, no wait: the j=all passes "
                         "themselves keep the load average up, so use it only on a quiet machine)")
    ap.add_argument("--reps", type=int, default=5)
    ap.add_argument("-k", type=int, default=15, help="first: fresh processes per side and file")
    ap.add_argument("--cap", type=int, default=None, help="MB; default 2048, extreme 4096")
    ap.add_argument("--timeout", type=int, default=None, help="SEC; default 900, extreme 30")
    ap.add_argument("--brun", default=os.path.join(HERE, "brun.sh"), help=argparse.SUPPRESS)
    ap.add_argument("--smoke", action="store_true")
    a = ap.parse_args()
    a.oracle = os.path.join(a.bin, "oracle")
    if a.smoke:
        a.grammars = a.grammars or ["json", "python"]
        a.rounds, a.reps, a.k = 1, 1, min(a.k, 3)
        if "--out" not in sys.argv:
            a.out = os.path.join(CACHE, "results-smoke")
    a.grammars = a.grammars or GRAMMARS
    if not os.path.exists(a.brun):
        sys.exit(a.brun + " missing")
    check_fresh(a)
    todo = ["cmp", "matrix", "allocs", "mem", "first", "extreme"] if a.cmd == "all" else [a.cmd]
    user_cap, user_to = a.cap, a.timeout
    for c in todo:
        a.cap = user_cap or (4096 if c == "extreme" else 2048)
        a.timeout = user_to or (30 if c == "extreme" else 900)
        if a.smoke:  # hard limits until the full run is approved
            a.cap, a.timeout = min(a.cap, 1024), min(a.timeout, 120)
        Runner(a).__getattribute__(c)()


if __name__ == "__main__":
    main()
