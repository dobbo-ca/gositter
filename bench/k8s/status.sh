#!/bin/sh
# status.sh [lines=15]: is run.py running, the tail of its log, pod memory.
#   RESULTS=results-smoke1 status.sh
kubectl --context dobbolab-local -n gositter-bench exec bench -- sh -c '
  pid=$(cat /data/run.pid 2> /dev/null)
  if [ -n "$pid" ] && grep -q run.py /proc/$pid/cmdline 2> /dev/null; then echo "running: pid $pid, $(ps -o etime= -p $pid | tr -d " ") elapsed"; else echo "not running"; fi
  echo "--- $1/run.log"
  tail -n "$2" "$1/run.log"
  echo "--- rows"
  wc -l "$1"/*.jsonl 2> /dev/null
  echo "--- memory"
  used=$(cat /sys/fs/cgroup/memory.current) max=$(cat /sys/fs/cgroup/memory.max)
  echo "pod: $((used / 1048576)) MB used of $((max / 1048576)) MB limit, $(((max - used) / 1048576)) MB free"
  echo "node: $(awk "/MemAvailable/{print int(\$2 / 1024)}" /proc/meminfo) MB available, load $(cut -d" " -f1-3 /proc/loadavg)"
' sh "/data/${RESULTS:-results}" "${1:-15}"
