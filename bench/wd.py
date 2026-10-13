#!/usr/bin/env python3
# wd.py [-m MB] [-t SEC] cmd...  : run cmd in own process group; kill group when summed RSS > MB or wall > SEC.
# last stderr line: WD rc=<rc|KILL_RSS|KILL_TIME> wall=<s> peak_mb=<polled group peak> maxrss_mb=<rusage>
import os, sys, time, subprocess, signal, resource
a = sys.argv[1:]; lim = 2048; tl = 300
while a and a[0] in ('-m', '-t'):
    if a[0] == '-m': lim = int(a[1])
    else: tl = float(a[1])
    a = a[2:]
PAGE_KB = os.sysconf('SC_PAGE_SIZE') // 1024
def group_rss_kb(pgid):
    """Summed RSS of the process group: /proc on Linux, ps elsewhere."""
    rss = 0
    if os.path.isdir('/proc/self'):
        for d in os.listdir('/proc'):
            if d.isdigit():
                try:
                    s = open('/proc/%s/stat' % d).read()
                    f = s[s.rindex(')') + 2:].split()  # state ppid pgrp ... rss is field 24
                    if f[2] == str(pgid): rss += int(f[21]) * PAGE_KB
                except (OSError, ValueError, IndexError): pass
        return rss
    try:
        out = subprocess.run(['/bin/ps', '-axo', 'pgid=,rss='], capture_output=True, text=True).stdout
    except Exception:
        out = ''
    for l in out.splitlines():
        f = l.split()
        if len(f) == 2 and f[0] == str(pgid): rss += int(f[1])
    return rss
t0 = time.time()
p = subprocess.Popen(a, start_new_session=True)
peak = 0; why = None
while p.poll() is None:
    rss = group_rss_kb(p.pid)
    peak = max(peak, rss)
    if rss > lim * 1024: why = 'KILL_RSS'
    elif time.time() - t0 > tl: why = 'KILL_TIME'
    if why:
        try: os.killpg(p.pid, signal.SIGKILL)
        except ProcessLookupError: pass
        p.wait(); break
    time.sleep(0.1)
ru = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss / (1048576 if sys.platform == 'darwin' else 1024)  # bytes on macOS, KB on Linux
sys.stderr.write('WD rc=%s wall=%.2f peak_mb=%d maxrss_mb=%d\n' % (why or p.returncode, time.time() - t0, peak // 1024, ru))
sys.exit(137 if why else p.returncode)
