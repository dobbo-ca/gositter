#!/bin/sh
# start.sh [run.py args...]   (default: all)
# Starts run.py detached in the pod (setsid nohup), so it outlives kubectl
# exec. Does nothing when a run.py is already running. Prints the pid, which
# is kept in /data/run.pid. Results and run.log go to /data/$RESULTS
# (default results).
#   RESULTS=results-smoke1 start.sh all --smoke
# cgo and the oracle can end in KILL_RSS on scala at the default 2048 MB cap.
# A larger cap reruns only those rows (8192 fits the 12Gi pod limit):
#   start.sh cmp --grammars scala --cap 8192     (then matrix, then mem)
# cgo RSS grows with passes x threads (glibc arenas), so mem j=all may still be killed.
[ $# -gt 0 ] || set -- all
kubectl --context dobbolab-local -n gositter-bench exec bench -- sh -c '
  out=/data/$1; shift
  pid=$(cat /data/run.pid 2> /dev/null)
  if [ -n "$pid" ] && grep -q run.py /proc/$pid/cmdline 2> /dev/null; then echo "already running: pid $pid"; exit 0; fi
  mkdir -p "$out"
  cd /work/gositter
  BENCH_DATA=/data setsid nohup python3 bench/run.py "$@" --out "$out" >> "$out/run.log" 2>&1 < /dev/null &
  pid=$!
  echo $pid > /data/run.pid
  sleep 1
  if grep -q run.py /proc/$pid/cmdline 2> /dev/null; then echo "started: pid $pid, log $out/run.log"; else echo "did not start:"; tail -5 "$out/run.log"; exit 1; fi
' sh "${RESULTS:-results}" "$@"
