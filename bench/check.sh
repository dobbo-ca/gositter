#!/bin/sh
# check.sh <grammar> <filelist>: the three sides must agree on nodes,
# error_files and the walk checksum, for walk and cursor at -j 1 and -j 14.
# Every run goes through brun.sh. Prints one line per run, exits 1 on a difference.
dir=$(cd "$(dirname "$0")" && pwd)
bin=${BENCH_DATA:-/Users/christopherdobbyn/.cache/gositter-bench-9j5}/bin
g=$1 list=$2 rc=0
for what in parse walk cursor; do
  for j in 1 14; do
    ref=""
    for b in cbench cgobench gsbench; do
      out=$("$dir/brun.sh" "$bin/$b" -g "$g" -l "$list" -j $j -what $what -n 2 2>&1)
      key=$(printf '%s\n' "$out" | python3 -c '
import sys, json
reps, chk, wd = [], "", ""
for l in sys.stdin:
    if l.startswith("{"):
        o = json.loads(l)
        if o["type"] == "rep": reps.append(o)
    elif l.startswith("checksum"): chk = l.split()[1]
    elif l.startswith("WD"): wd = l.strip()
if not reps or "rc=0" not in wd: print("FAILED", wd); sys.exit()
assert len({(r["nodes"], r["error_files"]) for r in reps}) == 1, reps
print("nodes=%d error_files=%d checksum=%s" % (reps[0]["nodes"], reps[0]["error_files"], chk))
print("ms=%.1f" % (min(r["wall_ns"] for r in reps) / 1e6), file=sys.stderr)' 2>/tmp/check-ms.$$)
      ms=$(cat /tmp/check-ms.$$); rm -f /tmp/check-ms.$$
      [ -z "$ref" ] && ref=$key
      verdict=same
      [ "$key" = "$ref" ] || { verdict=DIFFERENT; rc=1; }
      case $key in FAILED*) verdict=FAILED; rc=1 ;; esac
      echo "$g $what j=$j $b $key $ms $verdict"
    done
  done
done
exit $rc
