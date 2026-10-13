#!/usr/bin/env python3
"""Build real/first/broken lists and SUMMARY.tsv for the benchmark.
mklists.py [grammar...]: only these grammars (default all). Run screen.py after."""
import os, sys, random
sys.path.insert(0, os.path.dirname(__file__))
import mkbroken
from mkbroken import CACHE, stride

OLD = "/Users/christopherdobbyn/.cache/nocgo-c41e"
GRAMMARS = "bash c cpp csharp golang hcl java javascript json julia kotlin lua php python ruby rust scala tsx typescript verilog zig".split()
EXT = {
    ".go": "golang", ".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
    ".tsx": "tsx", ".ts": "typescript", ".mts": "typescript", ".cts": "typescript",
    ".tf": "hcl", ".tfvars": "hcl", ".hcl": "hcl", ".json": "json", ".py": "python", ".rs": "rust",
    ".c": "c", ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp",
    ".java": "java", ".cs": "csharp", ".rb": "ruby", ".rake": "ruby", ".php": "php", ".phtml": "php",
    ".sh": "bash", ".bash": "bash", ".scala": "scala", ".sc": "scala", ".jl": "julia",
    ".v": "verilog", ".sv": "verilog", ".svh": "verilog", ".vh": "verilog",
    ".kt": "kotlin", ".kts": "kotlin", ".lua": "lua", ".zig": "zig",
}
CPP = [b"class ", b"namespace ", b"template", b"::", b"public:", b"private:", b"protected:"]
SKIPDIR = {"node_modules", "vendor", "third_party", ".git"}
MB = 1 << 20
DIRNAME = {"go": "golang", "c-sharp": "csharp"}

def grammar_of(p):
    e = os.path.splitext(p)[1].lower()
    if e != ".h":
        return EXT.get(e)
    head = open(p, "rb").read(256 * 1024)
    return "cpp" if any(m in head for m in CPP) else "c"

def walk(root):
    for dp, dns, fns in os.walk(root):
        dns[:] = sorted(d for d in dns if d not in SKIPDIR)
        for f in sorted(fns):
            p = os.path.join(dp, f)
            if os.path.isfile(p) and not os.path.islink(p):
                yield p

def pick(c, maxn, maxb):
    n = min(len(c), maxn)
    while True:
        s = stride(c, n)
        if sum(os.path.getsize(p) for p in s) <= maxb or n <= 1:
            return s
        n = int(n * 0.95)

def main():
    cache = CACHE
    roots = sorted(os.path.join(cache, "corpora", d) for d in os.listdir(cache + "/corpora")
                   if os.path.isdir(os.path.join(cache, "corpora", d)))
    roots += [f"{OLD}/corpora/{d}" for d in sorted(os.listdir(f"{OLD}/corpora"))]
    repo = {g: [] for g in GRAMMARS}; zoo = {g: [] for g in GRAMMARS}
    big = {g: 0 for g in GRAMMARS}; bigb = {g: 0 for g in GRAMMARS}
    for root in roots:
        for p in walk(root):
            g = grammar_of(p)
            if g not in repo: continue
            # tsc baselines are compiler output, 80% parse with errors
            if g == "javascript" and "/typescript/tsc/testdata/" in p: continue
            sz = os.path.getsize(p)
            if sz > MB: big[g] += 1
            elif sz > 0: repo[g].append(p)
    zroot = f"{OLD}/refute/zoo"
    for p in walk(zroot):
        rel = os.path.relpath(p, zroot).split(os.sep)
        g = DIRNAME.get(rel[1], rel[1]) if len(rel) > 2 else None
        if g not in zoo: continue
        sz = os.path.getsize(p)
        if sz > MB: big[g] += 1
        elif sz > 0: zoo[g].append(p)
    os.makedirs(cache + "/lists", exist_ok=True)
    real = {}; nzoo = {}; nrepo = {}
    only = sys.argv[1:] or GRAMMARS
    for g in only:
        c = repo[g]
        if len(c) < 400:
            c = c + zoo[g]
        s = pick(c, 1500, 12 * MB)
        real[g] = s
        zs = set(zoo[g])
        nzoo[g] = sum(p in zs for p in s); nrepo[g] = len(s) - nzoo[g]
        open(f"{cache}/lists/{g}.real.txt", "w").write("".join(p + "\n" for p in s))
        sz = sorted(s, key=os.path.getsize)
        a = min(s, key=lambda p: (abs(os.path.getsize(p) - 2048), p))
        m = sz[len(sz) // 2]
        if m == a and len(sz) > 1: m = sz[len(sz) // 2 + 1] if len(sz) // 2 + 1 < len(sz) else sz[0]
        open(f"{cache}/lists/{g}.first.txt", "w").write(f"{a}\n{m}\n")
    # existing broken
    ex = {g: [] for g in GRAMMARS}; src = {g: {"adv": 0, "advdeep": 0, "craft": 0} for g in GRAMMARS}
    bbig = {g: 0 for g in GRAMMARS}
    for sub in ("adv", "advdeep", "craft"):
        for p in walk(f"{OLD}/refute/{sub}"):
            if "_orig" in os.path.basename(p): continue
            g = grammar_of(p)
            if g not in ex: continue
            if os.path.getsize(p) > MB: bbig[g] += 1; continue
            ex[g].append((sub, p))
    sys.argv = [sys.argv[0]] + only
    muts = mkbroken.main()
    rows = []
    for g in only:
        m = muts[g]
        # cap existing to 150 so mutants keep room
        e = stride(ex[g], 150)
        allb = [p for _, p in e] + m
        while sum(os.path.getsize(p) for p in allb) > 8 * MB:
            # drop the biggest mutant
            allb.remove(max(m, key=os.path.getsize)); m = [x for x in m if x in allb]
        for sub, p in e: src[g][sub] += 1
        open(f"{cache}/lists/{g}.broken.txt", "w").write("".join(p + "\n" for p in allb))
        rb = sum(os.path.getsize(p) for p in real[g])
        bb = sum(os.path.getsize(p) for p in allb)
        rows.append([g, len(real[g]), rb, nrepo[g], nzoo[g], len(allb), bb,
                     src[g]["adv"], src[g]["advdeep"], src[g]["craft"], len(m), big[g], bbig[g], len(repo[g])])
    hdr = "grammar real_files real_bytes real_from_repos real_from_zoo broken_files broken_bytes broken_adv broken_advdeep broken_craft broken_mutants skipped_over_1MB_real skipped_over_1MB_broken repo_candidates".split()
    sp = f"{cache}/lists/SUMMARY.tsv"
    old = [l.rstrip("\n").split("\t") for l in open(sp)][1:] if os.path.exists(sp) else []
    new = {r[0]: list(map(str, r)) for r in old}
    new.update({r[0]: list(map(str, r)) for r in rows})
    with open(sp, "w") as f:
        f.write("\t".join(hdr) + "\n")
        for g in GRAMMARS:
            if g in new: f.write("\t".join(new[g]) + "\n")

main()
