#!/usr/bin/env python3
# wd.py [-m MB] [-t SEC] cmd...  : run cmd in own process group; kill its process tree when summed RSS > MB or wall > SEC.
# The tree is the group plus every descendant by parent pid, so a nested wd.py (own session) cannot escape it.
# last stderr line: WD rc=<rc|KILL_RSS|KILL_TIME> wall=<s> peak_mb=<polled tree peak> maxrss_mb=<rusage>
import os, sys, time, subprocess, signal, resource
a = sys.argv[1:]; lim = 2048; tl = 300
while a and a[0] in ('-m', '-t'):
    if a[0] == '-m': lim = int(a[1])
    else: tl = float(a[1])
    a = a[2:]
PAGE_KB = os.sysconf('SC_PAGE_SIZE') // 1024
def procs():
    """{pid: (ppid, pgrp, rss_kb)}: /proc on Linux, ps elsewhere."""
    ps = {}
    if os.path.isdir('/proc/self'):
        for d in os.listdir('/proc'):
            if d.isdigit():
                try:
                    s = open('/proc/%s/stat' % d).read()
                    f = s[s.rindex(')') + 2:].split()  # state ppid pgrp ... rss is field 24
                    ps[int(d)] = (int(f[1]), int(f[2]), int(f[21]) * PAGE_KB)
                except (OSError, ValueError, IndexError): pass
        return ps
    try:
        out = subprocess.run(['/bin/ps', '-axo', 'pid=,ppid=,pgid=,rss='], capture_output=True, text=True).stdout
    except Exception:
        out = ''
    for l in out.splitlines():
        f = l.split()
        if len(f) == 4: ps[int(f[0])] = (int(f[1]), int(f[2]), int(f[3]))
    return ps
def tree(root):
    """(pids, summed RSS kb) of root's group and all descendants."""
    ps = procs()
    pids = {q for q, v in ps.items() if v[1] == root or q == root}
    grew = True
    while grew:
        more = {q for q, v in ps.items() if v[0] in pids} - pids
        pids |= more; grew = bool(more)
    return pids, sum(ps[q][2] for q in pids)
t0 = time.time()
p = subprocess.Popen(a, start_new_session=True)
peak = 0; why = None
while p.poll() is None:
    pids, rss = tree(p.pid)
    peak = max(peak, rss)
    if rss > lim * 1024: why = 'KILL_RSS'
    elif time.time() - t0 > tl: why = 'KILL_TIME'
    if why:
        for q in pids | tree(p.pid)[0]:
            try: os.kill(q, signal.SIGKILL)
            except ProcessLookupError: pass
        p.wait(); break
    time.sleep(0.1)
ru = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss / (1048576 if sys.platform == 'darwin' else 1024)  # bytes on macOS, KB on Linux
sys.stderr.write('WD rc=%s wall=%.2f peak_mb=%d maxrss_mb=%d\n' % (why or p.returncode, time.time() - t0, peak // 1024, ru))
sys.exit(137 if why else p.returncode)
