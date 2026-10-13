#!/usr/bin/env python3
"""Assemble the extreme-input set from the refute4 sweep logs.

Regenerates inputs with the refute4 generators (fam.py, sweep2.py shapes, glr-valid, explore.py)
so the bytes match the earlier runs. Writes extreme/<grammar>/*, lists/<grammar>.extreme.txt,
lists/EXTREME.tsv under OUT. Does not parse anything.
"""
import collections, hashlib, os, re, shutil, sys

R4 = os.environ.get('R4', '/Users/christopherdobbyn/.cache/nocgo-c41e/refute4')
R = os.path.dirname(R4) + '/refute'
OUT = os.environ.get('OUT') or os.environ.get('BENCH_DATA') or '/Users/christopherdobbyn/.cache/gositter-bench-9j5'
MAXN = 14
sys.path.insert(0, R4)
import random
import fam

GS = {'c-sharp': 'csharp', 'go': 'golang'}  # fam name -> gositter package
EXT = dict(bash='sh', c='c', cpp='cpp', csharp='cs', golang='go', hcl='hcl', java='java', javascript='js', json='json',
           julia='jl', kotlin='kt', lua='lua', php='php', python='py', ruby='rb', rust='rs', scala='scala',
           tsx='tsx', typescript='ts', verilog='v', zig='zig')

# --- generators copied from sweep2.py (t2 shapes, t2x glr-valid) and explore*.py (x stages) ---
T2 = {
 'c': (['#define A( ', '*/\n', "'a ", '"\n'], 'int x;\n'),
 'go': (['*/\n', '"a ', '<\n', ':= '], 'var x int\n'),
 'c-sharp': (['"a ', '@"a ', '"""\n', '*/\n'], 'class A {}\n'),
 'ruby': ([':"a ', '=>\n', 'case x\n', '-> {\n'], 'x = 1\n'),
 'verilog': (['module\n', '( [ ', '`define A \\\n', 'begin '], 'module m; endmodule\n'),
 'rust': (['r#"a ', 'x => {\n', "b'a", '"a '], 'fn f() {}\n'),
 'json': (['{"a": "b', '{ ( ', '{"a": 1,, "b": }\n', '[1, 2 3]\n'], '{"a": 1}\n'),
 'kotlin': (['for (i in a) {\n', '<', '"${a ', ']'], 'val x = 1\n'),
 'python': (['if x:\n', ') ', 'class A:\n', '< > '], 'x = 1\n'),
 'typescript': (['} { ', '</\n', "'a ", '${'], 'let x = 1;\n'),
}
def shapes(lang):
    F, V = T2[lang]; A, B, C, D = F
    def Rn(tag, n): return random.Random('%s/%s/%d' % (lang, tag, n))
    def ramp(n, f):
        out = []; k = 1; t = 0
        while t < n: out.append(f(k)); t += k; k += 1
        return ''.join(out)
    def gsgrand(n, X):
        r = Rn('gsgrand' + X, n); out = []; t = 0
        while t < n: k = r.randint(1, 200); out.append(X * k + V); t += k
        return ''.join(out)
    S = [
     ('step2 A+"a "', lambda n: (A + 'a ') * n),
     ('step3 A+"a b "', lambda n: (A + 'a b ') * n),
     ('stepvar A+"a "*rand(0..4)', lambda n: ''.join(A + 'a ' * k for k in Rn('sv', n).choices(range(5), k=n))),
     ('alt A B', lambda n: (A + B) * (n // 2)),
     ('alt A C', lambda n: (A + C) * (n // 2)),
     ('alt B D', lambda n: (B + D) * (n // 2)),
     ('rnd A B', lambda n: ''.join(Rn('ab', n).choices([A, B], k=n))),
     ('rnd A B C D', lambda n: ''.join(Rn('abcd', n).choices(F, k=n))),
     ('gsg (A*20 V)', lambda n: (A * 20 + V) * (n // 20)),
     ('gsg (B*20 V)', lambda n: (B * 20 + V) * (n // 20)),
     ('gsg ramp (A*k V) k=1..', lambda n: ramp(n, lambda k: A * k + V)),
     ('gsg rand (A*k V) k<=200', lambda n: gsgrand(n, A)),
     ('gsg rand (B*k V) k<=200', lambda n: gsgrand(n, B)),
     ('two seq A*n/2 V B*n/2', lambda n: A * (n // 2) + V + B * (n // 2)),
     ('two inter (A*7 B*5)', lambda n: (A * 7 + B * 5) * (n // 12)),
     ('two inter ramp (A*k B*k)', lambda n: ramp(n // 2, lambda k: A * k + B * k)),
     ('two inter rand runs A/B/V', lambda n: ''.join(x * Rn('ir%d' % i, n).randint(1, 40) for i, x in enumerate(Rn('ir', n).choices([A, B, V], weights=[5, 5, 1], k=max(n // 20, 1))))),
    ]
    return {'new: ' + nm + ' ' + repr(F[:2]): (lambda n, g=g: g(n).encode()) for nm, g in S}

X = {
 'cpp': ['x = a < b > (c);\n', 'f(a<b,c>d);\n', 'a * b;\n', '(a)(b);\n', 'x = a < b < c < d < e < f < g > h;\n', 'void f() { a<b<c<d>>> e(f<g>(h)); }\n'],
 'c': ['a * b;\n', '(a)(b);\n', 'x = (a)*b;\n', 'void f() { a * b; (c)(d); e(f) * g; }\n'],
 'typescript': ['(a, b) => c;\n', 'x = a<b>(c);\n', 'f(async (a, b) => c);\n', 'let x = <T>(a: T) => a;\n', 'x = (a, b, c, d, e, f, g, h);\n'],
 'tsx': ['(a, b) => c;\n', 'x = a<b>(c);\n', 'let x = <T,>(a: T) => <a b={c}>{d}</a>;\n'],
 'javascript': ['({a, b}) => c;\n', '({a, b});\n', 'async (a) => b;\n', 'x = (a, b, c, d, e, f, g, h);\n'],
 'c-sharp': ['F(a<b,c>(d));\n', 'var x = (A)b;\n', 'var f = (a, b) => c;\n', 'x = a < b > (c);\n'],
 'rust': ['fn f() { a < b > (c); }\n', 'fn f() { let x = a as u8 < b; }\n', 'fn f() { if a { b } - c; }\n'],
 'go': ['var x = a{b}\n', 'func f() { if a == (T{}) {}; x := a < b > (c) }\n'],
 'java': ['class A { void f() { a<b> c = (d) e; f(g<h, i>(j)); } }\n'],
 'kotlin': ['val x = a<b>(c)\n', 'val y = a < b > (c)\n', 'val f = { a, b -> c }\n'],
 'python': ['x = [a for a in b if c]\n', 'print(a for a in b)\n', 'x = a if b else c, d\n'],
 'scala': ['val x = a[b](c)\n', 'val f = (a, b) => c\n'],
 'php': ['$x = fn($a) => $a < $b > ($c);\n', '$x = (a)($b);\n'],
 'ruby': ['x = a ? b : c\n', 'f a, b do |c| d end\n', 'x = a /b/ c\n'],
}
def glr(lang):
    pre = '<?php\n' if lang == 'php' else ''
    return {'glr-valid %r' % t: (lambda n, t=t: (pre + t * n).encode()) for t in X.get(lang, [])}

def xgen():  # go (*/\n xa, "a  xb) interleave from explore*.py
    def inter(a, b, x='*/\n', y='"a '): return lambda n: ((x * a + y * b) * (n // (a + b))).encode()
    g = {}
    for a, b in [(7, 5), (1, 1), (3, 2), (2, 1), (1, 2), (20, 15), (70, 50), (1, 10), (10, 1), (3, 5), (3, 3), (3, 1), (4, 1)]:
        g["x: (*/\\n x%d, \"a  x%d)" % (a, b)] = inter(a, b)
    g["x: (*/\\n x7, 'a  x5)"] = inter(7, 5, '*/\n', "'a ")
    g["x: (*/\\n x7, `a  x5)"] = inter(7, 5, '*/\n', "`a ")
    g['x: (*/ x7, "a  x5) no newline'] = inter(7, 5, '*/ ', '"a ')
    g['x: (*/\\n x7, "a\\n x5)'] = inter(7, 5, '*/\n', '"a\n')
    return g

def gen_for(lang, name):
    for d in ({k: g for k, _, g in fam.families(lang)}, shapes(lang) if lang in T2 else {}, glr(lang), xgen() if lang in ('go', 'c', 'cpp', 'c-sharp') else {}):
        if name in d: return d[name]
    return None

def famof(name):
    if name.startswith('rep '): return 'rep'
    for p in ('unterminated', 'pair', 'block', 'broken-stmt', 'glr-valid'):
        if name.startswith(p): return p
    if name.startswith('random '): return name
    if name.startswith('deep ') or name.startswith('rising-indent'): return 'deep'
    return 'combo'  # t2 shapes and x: interleaves

# --- load logs ---
rows = []  # dict per measurement
for l in open(R4 + '/results.tsv'):
    f = l.rstrip('\n').split('\t')
    rows.append(dict(src='sweep', lang=f[0], name=f[1], n=int(f[2]), bytes=f[3], cw=f[4], cr=f[5], gw=f[7], gr=f[8], res=f[10], note=f[11]))
for l in open(R4 + '/results-fix2.tsv'):
    f = l.rstrip('\n').split('\t')
    if f[13] == 'notrun': continue
    rows.append(dict(src='fix2/' + f[0], lang=f[1], name=f[2], n=int(f[3]), bytes=f[4], cw=f[5], cr=f[6], gw=f[9], gr=f[10], res=f[13], note=f[14]))
# last.sh in refute4 (tc-fix2): cpp template n=100000, C 22.7 s 1094 MB, gs KILL_RSS at 2 GB after 2 s
rows.append(dict(src='fix2/last.sh', lang='cpp', name="broken-stmt 'std::vector<std::map<int, x = 1;\\n'", n=100000, bytes='3300000',
                 cw='22.7', cr='1094', gw='2.0', gr='2048', res='gs-over-rss', note='gs KILL_RSS at 2048 MB (wd.py 1.5 GB cap at the time)'))
def num(s):
    try: return float(s)
    except ValueError: return None
for r in rows:
    r['cwn'], r['crn'], r['gwn'], r['grn'] = num(r['cw']), num(r['cr']), num(r['gw']), num(r['gr'])
    r['key'] = (r['lang'], r['name'], r['n'])
def msg(r):
    s = '%s %s:' % (r['src'], r['res'])
    if r['cr']: s += ' C %ss %sMB' % (r['cw'], r['cr'])
    if r['gr']: s += ', gs %ss %sMB' % (r['gw'], r['gr'])
    if r['note']: s += ' [%s]' % r['note'][:160]
    return s

def best(rs, key):
    rs = [r for r in rs if key(r) is not None]
    return max(rs, key=key, default=None)
gsrss = lambda r: r['grn']
ratio = lambda r: r['grn'] / max(r['crn'], 1) if r['grn'] and r['grn'] >= 64 and r['crn'] is not None else None
tratio = lambda r: r['gwn'] / max(r['cwn'], 0.1) if r['gwn'] and r['gwn'] >= 3 and r['gwn'] > r['cwn'] and r['res'] in ('same', 'gs-over-time') else None  # gs slower only
cslow = lambda r: r['cwn'] if r['cwn'] and r['cwn'] >= 10 and r['res'] == 'same' else None  # gs-faster claim evidence

big = {'c': 'c', 'cpp': 'cpp', 'go': 'go', 'java': 'java', 'javascript': 'js', 'json': 'json', 'python': 'py', 'rust': 'rs', 'typescript': 'ts'}
deepf = {'bash': ['sh'], 'c': ['c', 'cblk'], 'cpp': ['cpp'], 'go': ['go'], 'java': ['java'], 'javascript': ['js', 'jsfn'],
         'json': ['json'], 'lua': ['lua'], 'python': ['py'], 'ruby': ['rb'], 'rust': ['rs'], 'typescript': ['ts']}
FAMS = ['random bracket-soup', 'random bytes', 'random printable', 'rep', 'unterminated', 'block', 'broken-stmt', 'pair', 'glr-valid', 'deep']

def main():
    os.makedirs(OUT + '/lists', exist_ok=True)
    tsv = open(OUT + '/lists/EXTREME.tsv', 'w')
    tsv.write('grammar\tpath\tbytes\tfamily\tgenerator_and_n\tearlier_log\n')
    counts = {}; maxrss = []
    for lang in fam.ORDER:
        gs = GS.get(lang, lang); ext = EXT[gs]; d = '%s/extreme/%s' % (OUT, gs); shutil.rmtree(d, ignore_errors=True); os.makedirs(d)
        rs = [r for r in rows if r['lang'] == lang]
        fix = [r for r in rs if r['src'].startswith('fix2')]
        gsrows = lambda L: [r for r in L if r['grn'] is not None]
        killed_gs = [r for r in rs if r['res'] in ('gs-over-rss', 'gs-over-time', 'gs-crash', 'gs-panic', 'C-error')]
        killed_c = sorted([r for r in rs if r['res'] == 'skipped' and r['crn']], key=lambda r: r['cwn'])
        by = lambda L, k: best(L, k)
        picks = []  # (row_or_None, family, label, gen, n, path_override)
        seen = set()
        def add(r, why):
            if r is None or r['key'] in seen or len(picks) >= MAXN: return
            if why.startswith(('worst gs/C time', 'C slowest')) and any(p[0]['lang'] == r['lang'] and p[0]['name'] == r['name'] for p in picks): return
            seen.add(r['key']); picks.append((r, why))
        add(by(gsrows(fix), gsrss), 'worst gs RSS (fix2)')
        add(by(gsrows(fix), ratio), 'worst gs/C RSS (fix2)')
        add(by([r for r in gsrows(fix) if r['key'] not in seen], gsrss), '2nd worst gs RSS (fix2)')
        kf = [r for r in killed_gs if r['src'].startswith('fix2')]
        add(by(kf, lambda r: r['grn'] or 0), 'gs killed (fix2)')
        add(by(killed_gs, lambda r: r['grn'] or 0), 'gs killed (before fixes)')
        add(killed_c[0] if killed_c else None, 'C killed')
        add(by(gsrows(rs), gsrss), 'worst gs RSS (sweep, before fixes)')
        add(by(rs, ratio), 'worst gs/C RSS (sweep, before fixes)')
        have = {famof(p[0]['name']) for p in picks}
        for f in FAMS:
            if f in have or f == 'deep': continue
            fr = [r for r in rs if famof(r['name']) == f]
            r = by(gsrows(fr), gsrss) or by(fr, lambda r: r['crn'])
            if r: add(r, 'family ' + f); have.add(f)
        files = []  # (family, path, tag, how)
        if lang in big:
            files.append(('big5mb', '%s/adv/%s/big5mb.%s' % (R, lang, big[lang]), 'big5mb', 'copy of refute/adv'))
        if lang in deepf:
            for t in deepf[lang]:
                files.append(('deep (advdeep)', '%s/advdeep/100000/%s_100000.%s' % (R, t, {'cblk': 'c', 'jsarr': 'js', 'jsfn': 'js'}.get(t, t)), 'advdeep 100000', 'copy of refute/advdeep'))
        elif 'deep' not in have:
            fr = [r for r in rs if famof(r['name']) == 'deep']
            r = by(gsrows(fr), gsrss) or by(fr, lambda r: r['crn'])
            if r: add(r, 'family deep')
        # leftovers after reserving slots for big and advdeep files
        add(by(fix, tratio), 'worst gs/C time (fix2)')
        add(by(rs, cslow), 'C slowest (gs-faster claim)')
        picks = picks[:MAXN - len(files)]
        lst = []
        for r, why in picks:
            gen = gen_for(lang, r['name'])
            if gen is None: raise SystemExit('no generator for %r' % (r['key'],))
            src = gen(r['n'])
            slug = re.sub(r'\W+', '_', r['name'])[:40].strip('_')
            path = '%s/%s_%s__n%d.%s' % (d, slug, hashlib.sha1(r['name'].encode()).hexdigest()[:6], r['n'], ext)
            open(path, 'wb').write(src)
            same = [x for x in rs if x['key'] == r['key']]
            log = ' | '.join(msg(x) for x in same)
            lst.append((path, len(src), famof(r['name']), '%s n=%d' % (r['name'], r['n']), '%s; %s' % (why, log)))
            for x in same:
                for v in (x['crn'], x['grn']):
                    if v: maxrss.append((v, lang, r['name'], r['n'], 'C' if v == x['crn'] else 'gs', x['src']))
        for fam_, p, tag, how in files:
            if not os.path.exists(p): continue
            dst = '%s/%s__%s' % (d, tag.replace(' ', '_'), os.path.basename(p))
            shutil.copyfile(p, dst)
            lst.append((dst, os.path.getsize(dst), fam_, how + ' ' + os.path.relpath(p, os.path.dirname(R)), 'new'))
        counts[gs] = len(lst)
        with open('%s/lists/%s.extreme.txt' % (OUT, gs), 'w') as o:
            for x in lst:
                o.write(x[0] + '\n'); tsv.write('%s\t%s\t%d\t%s\t%s\t%s\n' % (gs, *x))
    tsv.close()
    print(counts)
    for m in sorted(maxrss, reverse=True)[:12]: print('maxrss', m)

main()
