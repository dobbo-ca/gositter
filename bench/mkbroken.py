#!/usr/bin/env python3
"""Write 3 mutants of 150 real files per grammar. Fixed seed, repeatable."""
import os, random, sys

CACHE = os.environ.get("BENCH_DATA") or "/Users/christopherdobbyn/.cache/gositter-bench-9j5"
STRAY = [b"(", b"{", b"[", b")", b"}", b"]", b"'", b'"']

def truncate(r, b):
    return b[: int(len(b) * r.uniform(0.3, 0.9))]

def delete_runs(r, b):
    for _ in range(3):
        if len(b) < 2:
            break
        n = min(r.randint(1, 40), len(b) - 1)
        i = r.randrange(len(b) - n + 1)
        b = b[:i] + b[i + n:]
    return b

def stray(r, b):
    starts = [0] + [i + 1 for i, c in enumerate(b) if c == 10]
    i = r.choice(starts)
    return b[:i] + r.choice(STRAY) + b[i:]

def stride(xs, n):
    if len(xs) <= n:
        return list(xs)
    return [xs[i * len(xs) // n] for i in range(n)]

def main():
    out_lists = {}
    for g in sys.argv[1:]:
        real = [l.rstrip("\n") for l in open(f"{CACHE}/lists/{g}.real.txt")]
        d = f"{CACHE}/broken/{g}"
        os.makedirs(d, exist_ok=True)
        r = random.Random(f"gositter-bench:{g}")
        made = []
        for k, p in enumerate(stride(real, 150)):
            b = open(p, "rb").read()
            ext = os.path.splitext(p)[1]
            for name, f in (("trunc", truncate), ("del", delete_runs), ("stray", stray)):
                q = f"{d}/{k:03d}_{name}{ext}"
                open(q, "wb").write(f(r, b))
                made.append(q)
        out_lists[g] = made
    return out_lists

if __name__ == "__main__":
    main()
