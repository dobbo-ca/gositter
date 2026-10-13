#!/usr/bin/env python3
"""One compact Markdown table per grammar, for BENCHMARKS.md.
Usage: compact.py RESULTS_DIR   (the dir with matrix.jsonl, mem.jsonl, ...)
Ratios are gositter / other side; a ratio above 1.00 is bold."""
import json, os, statistics, sys
from collections import defaultdict

res = sys.argv[1]


def rows(name):
    p = os.path.join(res, name + ".jsonl")
    return [json.loads(l) for l in open(p)] if os.path.exists(p) else []


def fmt(x):
    if x is None:
        return "n/a"
    s = f"{x:.2f}"
    return f"**{s}**" if x > 1.004 else s


def ratio(a, b):
    return a / b if a and b else None


# (grammar, set, what, j, side) -> list of reps, list of peak RSS
reps, peak, mb = defaultdict(list), defaultdict(list), {}
for r in rows("matrix"):
    k = (r["grammar"], r["set"], r["what"], r["j"], r["side"])
    if r["status"] != "ok":
        peak[k].append(None)
        continue
    reps[k] += r["reps"]
    peak[k].append(r["end"]["maxrss_kb"])
    mb[k[:2]] = r["meta"]["bytes"] / 1e6

steady = {}
for r in rows("mem"):
    if r["status"] == "ok":
        steady[(r["grammar"], r["set"], r["j"], r["side"])] = r["end"]["maxrss_kb"]

allocs = {}
for r in rows("allocs"):
    if r["status"] == "ok" and r["mode"] == "steady":
        e = r["end"]
        n = e["go_mallocs"] if r["side"] == "gositter" else e["allocs"]
        allocs[(r["grammar"], r["set"], r["side"])] = n / e["parses"]

first = defaultdict(list)
for r in rows("first"):
    if r["status"] == "ok" and r.get("perf"):
        p = r["perf"]
        first[(r["grammar"], r["file_idx"], r["side"])].append(
            (p["cycles:u"] + p["cycles:k"], r["first"]["parse1_ns"], r["first"]["file_bytes"]))

cmp_ = {(r["grammar"], r["set"]): r for r in rows("cmp")}


def stat(k, f, agg):
    v = [f(x) for x in reps.get(k, [])]
    return agg(v) if v else None


wall = lambda x: x["wall_ns"]
cpu = lambda x: x["user_ns"] + x["sys_ns"]
ins = lambda x: x["instr"]
cyc = lambda x: x["cycles"]
med = statistics.median

grammars = sorted({k[0] for k in reps})
for g in grammars:
    print(f"### {g}\n")
    print("| set | what | threads | wall | CPU | instructions | cycles | peak RSS | steady RSS | cycles vs cgo | gositter MB/s |")
    print("|---|---|---|---|---|---|---|---|---|---|---|")
    for s in ("real", "realerr", "broken"):
        if (g, s) not in mb or mb[(g, s)] < 0.1:
            continue
        for j in ("1", "all"):
            for w in ("parse", "walk", "cursor"):
                gs, c, cg = (g, s, w, j, "gositter"), (g, s, w, j, "c"), (g, s, w, j, "cgo")
                pk = lambda k: max(peak[k]) if peak.get(k) and None not in peak[k] else None
                gw = stat(gs, wall, min)
                cells = [
                    fmt(ratio(gw, stat(c, wall, min))),
                    fmt(ratio(stat(gs, cpu, med), stat(c, cpu, med))),
                    fmt(ratio(stat(gs, ins, med), stat(c, ins, med))),
                    fmt(ratio(stat(gs, cyc, med), stat(c, cyc, med))),
                    fmt(ratio(pk(gs), pk(c))),
                    fmt(ratio(steady.get((g, s, j, "gositter")), steady.get((g, s, j, "c")))) if w == "parse" else "",
                    fmt(ratio(stat(gs, cyc, med), stat(cg, cyc, med))) if reps.get(cg) else "cgo killed",
                    f"{mb[(g, s)] / (gw / 1e9):.0f}" if gw else "n/a",
                ]
                print(f"| {s} | {w} | {j} | " + " | ".join(cells) + " |")
    notes = []
    a = [(s, allocs.get((g, s, "gositter")), allocs.get((g, s, "c"))) for s in ("real", "broken")]
    notes.append("Allocations per parse, warm parser: " + "; ".join(
        f"{s} gositter {x:.1f}, C {y:.0f}" for s, x, y in a if x is not None and y is not None) + ".")
    for i in (0, 1):
        fg, fc = first.get((g, i, "gositter")), first.get((g, i, "c"))
        if fg and fc:
            m = lambda v, n: med([x[n] for x in v])
            notes.append(f"New process, {fg[0][2]} byte file: whole process {m(fg, 0) / 1e6:.2f} M cycles against "
                         f"{m(fc, 0) / 1e6:.2f} M for C ({fmt(m(fg, 0) / m(fc, 0))}), first parse "
                         f"{m(fg, 1) / 1e3:.0f} us against {m(fc, 1) / 1e3:.0f} us ({fmt(m(fg, 1) / m(fc, 1))}).")
    cm = []
    for s in ("real", "realerr", "broken"):
        r = cmp_.get((g, s))
        if r:
            cm.append(f"{s} {r['summary']['mismatching_files']} of {r['summary']['files']}" if r["status"] == "ok"
                      else f"{s} not compared ({r['status']} on the cgo side)")
    notes.append("Trees that differ from C (`oracle cmp`): " + ", ".join(cm) + ".")
    print("\n" + "\n\n".join(notes) + "\n")

# Extreme inputs: one row per grammar, then every input where gositter is worse.
best = {}
for r in rows("extreme"):
    k = (r["grammar"], os.path.basename(r["file"]), r["side"])
    if k not in best or r["timeout"] > best[k]["timeout"]:
        best[k] = r
if best:
    files = sorted({k[:2] for k in best})
    worse, per = [], defaultdict(lambda: defaultdict(int))
    for g, f in files:
        a, c = best.get((g, f, "gositter")), best.get((g, f, "c"))
        if not a or not c:
            continue
        p = per[g]
        p["files"] += 1
        sa, sc = a["status"], c["status"]
        if sa == "ok" and sc == "ok":
            t = a["reps"][0]["cycles"] / c["reps"][0]["cycles"]
            m = a["end"]["maxrss_kb"] / c["end"]["maxrss_kb"]
            p["both"] += 1
            p["faster"] += t <= 1.0
            p["less"] += m <= 1.0
            p["tmax"], p["mmax"] = max(p["tmax"], t), max(p["mmax"], m)
            if t > 1.02 or m > 1.02:
                worse.append((max(t, m), g, f, a, c, t, m))
        else:
            p["gs_only" if sa == "ok" else "c_only" if sc == "ok" else "neither"] += 1
            if sa != "ok" or sc != "ok":
                worse.append((0, g, f, a, c, None, None))
    print("### Extreme inputs\n")
    print("| grammar | inputs | both finished | gositter fewer cycles | gositter less RSS | worst cycles ratio | worst RSS ratio | only gositter finished | only C finished | neither finished |")
    print("|---|---|---|---|---|---|---|---|---|---|")
    for g in sorted(per):
        p = per[g]
        print(f"| {g} | {p['files']} | {p['both']} | {p['faster']} | {p['less']} | {fmt(p['tmax'] or None)} | {fmt(p['mmax'] or None)} | {p['gs_only']} | {p['c_only']} | {p['neither']} |")
    t = defaultdict(int)
    for p in per.values():
        for k, v in p.items():
            if k not in ("tmax", "mmax"):
                t[k] += v
    print(f"| all | {t['files']} | {t['both']} | {t['faster']} | {t['less']} | | | {t['gs_only']} | {t['c_only']} | {t['neither']} |")
    print("\nEvery extreme input where gositter needs over 1.02 times the cycles or the peak RSS of C, or where a side did not finish:\n")
    print("| grammar | input | gositter | C | cycles ratio | RSS ratio |")
    print("|---|---|---|---|---|---|")

    def cell(r):
        if r["status"] != "ok":
            return f"{r['status']} at {r['wd']['wall']:.0f} s, {r['wd']['peak_mb']} MB"
        return f"{r['reps'][0]['wall_ns'] / 1e9:.2f} s, {r['end']['maxrss_kb'] / 1024:.0f} MB"
    for _, g, f, a, c, tr, mr in sorted(worse, key=lambda w: (w[1], -w[0])):
        print(f"| {g} | `{f}` | {cell(a)} | {cell(c)} | {fmt(tr)} | {fmt(mr)} |")
