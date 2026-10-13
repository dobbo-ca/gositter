#!/usr/bin/env python3
"""screen.py [--set=broken|real|realerr] [grammar...]: move blow-up inputs out of the broken lists.

A broken list is a steady-state list, so one file that needs seconds or
hundreds of MB makes the whole config a KILL. Each list is parsed in chunks of
25 by cbench, gsbench and cgobench (brun.sh -m 512 -t 5, -j 1 -n 1 -warm 0). The files of
a chunk that is killed, slower than 2 s or above 300 MB are then run alone, and
a file over those limits on either side moves from <g>.broken.txt to
<g>.extreme.txt and EXTREME.tsv. Every single-file run is logged in SCREEN.tsv.
With --set=real or realerr nothing moves: a real file that one side cannot
parse in the limits is a result, so it is only logged and printed.
"""
import os, re, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CACHE = os.environ.get("BENCH_DATA") or "/Users/christopherdobbyn/.cache/gositter-bench-9j5"
LISTS = CACHE + "/lists"
GRAMMARS = ("bash c cpp csharp golang hcl java javascript json julia kotlin lua php "
            "python ruby rust scala tsx typescript verilog zig").split()
SIDES = [("gs", "gsbench"), ("C", "cbench"), ("cgo", "cgobench")]
CAP_MB, CAP_S, MAX_S, MAX_MB, CHUNK = 512, 5, 2.0, 300, 25
WD_RE = re.compile(r"^WD rc=(\S+) wall=([\d.]+) peak_mb=(\d+) maxrss_mb=(\d+)", re.M)


def run(binname, g, files):
    """-> (bad, text) for one process over files."""
    with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as f:
        f.write("\n".join(files) + "\n")
    try:
        r = subprocess.run([HERE + "/brun.sh", "-m", str(CAP_MB), "-t", str(CAP_S), f"{CACHE}/bin/{binname}",
                            "-g", g, "-l", f.name, "-j", "1", "-what", "parse", "-n", "1", "-warm", "0"],
                           capture_output=True, text=True, errors="replace")
    finally:
        os.unlink(f.name)
    m = WD_RE.search(r.stderr)
    if not m:
        sys.exit("no WD line: " + r.stderr[-300:])
    rc, wall, mb = m[1], float(m[2]), max(int(m[3]), int(m[4]))
    return rc != "0" or wall > MAX_S or mb > MAX_MB, f"rc={rc} {wall:.1f}s {mb}MB"


def main():
    log = open(LISTS + "/SCREEN.tsv", "a")
    args, name = sys.argv[1:], "broken"
    if args and args[0].startswith("--set="):
        name = args.pop(0)[6:]
    for g in args or GRAMMARS:
        bp, ep = f"{LISTS}/{g}.{name}.txt", f"{LISTS}/{g}.extreme.txt"
        files = [l.strip() for l in open(bp) if l.strip()]
        suspects = []
        for i in range(0, len(files), CHUNK):
            chunk = files[i:i + CHUNK]
            if any(run(b, g, chunk)[0] for _, b in SIDES):
                suspects += chunk
        moved = []
        for f in suspects:
            res = [(n, *run(b, g, [f])) for n, b in SIDES]
            verdict = "; ".join(f"{n} {t}" for n, _, t in res)
            log.write(f"{g}\t{f}\t{verdict}\n")
            log.flush()
            if any(bad for _, bad, _ in res):
                moved.append((f, verdict))
        print(f"{g} {name}: {len(files)} files, {len(suspects)} run alone, {len(moved)} over the limits", flush=True)
        if name != "broken":
            for f, verdict in moved:
                print(f"  kept {f}: {verdict}", flush=True)
        if not moved or name != "broken":
            continue
        gone = {f for f, _ in moved}
        open(bp, "w").write("".join(f + "\n" for f in files if f not in gone))
        have = {l.strip() for l in open(ep)} if os.path.exists(ep) else set()
        with open(ep, "a") as e, open(LISTS + "/EXTREME.tsv", "a") as tsv:
            for f, verdict in moved:
                print(f"  moved {f}: {verdict}", flush=True)
                if f not in have:
                    e.write(f + "\n")
                    tsv.write(f"{g}\t{f}\t{os.path.getsize(f)}\tbroken-list\tmoved from {g}.broken.txt by screen.py\t"
                              f"screen (-m {CAP_MB} -t {CAP_S}): {verdict}\n")


if __name__ == "__main__":
    main()
