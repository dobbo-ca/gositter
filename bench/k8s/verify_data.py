#!/usr/bin/env python3
# verify_data.py LISTDIR: per list, file count, bytes, sha256 of the
# sorted per-file sha256, and of the same in list order.
import glob, hashlib, os, sys
d = sys.argv[1]
cache = {}
def info(p):
    if p not in cache:
        b = open(p, "rb").read()
        cache[p] = (hashlib.sha256(b).hexdigest(), len(b))
    return cache[p]
for l in sorted(glob.glob(d + "/*.txt") + glob.glob(d + "/*.tsv")):
    ps = []
    for line in open(l, encoding="utf-8", errors="surrogateescape").read().split("\n"):
        f = line.split("\t")
        p = f[0] if l.endswith(".txt") else (f[1] if len(f) > 1 else "")
        if p.startswith("/"):
            ps.append(p)
    h = [info(p) for p in ps]
    print(os.path.basename(l), len(ps), sum(x[1] for x in h),
          hashlib.sha256("\n".join(sorted(x[0] for x in h)).encode()).hexdigest()[:16],
          hashlib.sha256("\n".join(x[0] for x in h).encode()).hexdigest()[:16])
