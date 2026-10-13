#!/usr/bin/env python3
"""splitreal.py [grammar...]: keep only clean files in the real lists.

One `gsbench -errs` process per grammar (through brun.sh) prints the files of
<g>.real.txt whose root has_error is set. They move to <g>.realerr.txt, so
"real" measures clean parsing and "realerr" the real files the grammar cannot
parse. <g>.first.txt is picked again from the clean list when one of its files
moved. Counts go to REALSPLIT.tsv, and the real_* columns of SUMMARY.tsv are
set from the lists again. Run after mklists.py; a second run moves nothing.
"""
import os, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
CACHE = os.environ.get("BENCH_DATA") or "/Users/christopherdobbyn/.cache/gositter-bench-9j5"
LISTS = CACHE + "/lists"
GRAMMARS = ("bash c cpp csharp golang hcl java javascript json julia kotlin lua php "
            "python ruby rust scala tsx typescript verilog zig").split()


def read(p):
    return [l.strip() for l in open(p) if l.strip()] if os.path.exists(p) else []


def refresh_summary():
    """Recount the real_* columns of SUMMARY.tsv (written by mklists.py before the split)."""
    sp = LISTS + "/SUMMARY.tsv"
    rows = [l.rstrip("\n").split("\t") for l in open(sp)]
    col = {n: i for i, n in enumerate(rows[0])}
    for r in rows[1:]:
        files = read(f"{LISTS}/{r[0]}.real.txt")
        zoo = sum("/refute/zoo/" in f for f in files)
        for n, v in (("real_files", len(files)), ("real_bytes", sum(map(os.path.getsize, files))),
                     ("real_from_repos", len(files) - zoo), ("real_from_zoo", zoo)):
            r[col[n]] = str(v)
    open(sp, "w").write("".join("\t".join(r) + "\n" for r in rows))


def main():
    tsv = open(LISTS + "/REALSPLIT.tsv", "a")
    for g in sys.argv[1:] or GRAMMARS:
        rp, ep, fp = (f"{LISTS}/{g}.{n}.txt" for n in ("real", "realerr", "first"))
        files = read(rp)
        r = subprocess.run([HERE + "/brun.sh", "-m", "1024", "-t", "100", CACHE + "/bin/gsbench", "-errs", "-g", g, "-l", rp],
                           capture_output=True, text=True, errors="replace")
        if "WD rc=0 " not in r.stderr:
            sys.exit(f"{g}: {r.stderr[-300:]}")
        bad = set(r.stdout.splitlines())
        assert bad <= set(files), g
        clean = [f for f in files if f not in bad]
        old = read(ep)
        err = old + [f for f in files if f in bad and f not in set(old)]
        open(rp, "w").write("".join(f + "\n" for f in clean))
        open(ep, "w").write("".join(f + "\n" for f in err))
        first = read(fp)
        if not set(first) <= set(clean):  # the rule of mklists.py
            a = min(clean, key=lambda p: (abs(os.path.getsize(p) - 2048), p))
            sz = sorted(clean, key=os.path.getsize)
            m = sz[len(sz) // 2]
            if m == a:
                m = sz[(len(sz) // 2 + 1) % len(sz)]
            open(fp, "w").write(f"{a}\n{m}\n")
            first = ["repicked"]
        line = f"{g}\treal_before={len(files)}\tmoved={len(bad)}\treal={len(clean)}\trealerr={len(err)}\tfirst={'repicked' if first == ['repicked'] else 'kept'}"
        tsv.write(line + "\n")
        print(line, flush=True)
    refresh_summary()


if __name__ == "__main__":
    main()
