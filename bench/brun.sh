#!/bin/sh
# brun.sh [-m MB=1024] [-t SEC=120] cmd...
# Runs one benchmark process at a time on this machine: global lock, nice 19,
# memory and time watchdog (wd.py). MallocNanoZone is unset: this shell
# inherits MallocNanoZone=0, which slows libc malloc and so only the C sides.
# The lock is lockf on macOS, flock on Linux. Data dir: $BENCH_DATA.
dir=$(cd "$(dirname "$0")" && pwd)
cache=${BENCH_DATA:-/Users/christopherdobbyn/.cache/gositter-bench-9j5}
m=1024 t=120
while [ $# -gt 1 ]; do
  case $1 in
    -m) m=$2; shift 2 ;;
    -t) t=$2; shift 2 ;;
    *) break ;;
  esac
done
mkdir -p "$cache"
if [ -x /usr/bin/lockf ]; then lock="/usr/bin/lockf -k"; else lock=flock; fi
exec $lock "$cache/bench.lock" nice -n 19 env -u MallocNanoZone python3 "$dir/wd.py" -m "$m" -t "$t" "$@"
