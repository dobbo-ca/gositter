#!/bin/sh
# fetch.sh [dest]: copies /data/$RESULTS (default results) and
# /data/bin/BUILDINFO*.txt out of the pod. Default dest:
# ~/.cache/gositter-bench-9j5/results-dobbolab, which gets results/ and bin/.
#   report.py --results <dest>/results --bin <dest>/bin
set -eu
r=${RESULTS:-results}
dest=${1:-/Users/christopherdobbyn/.cache/gositter-bench-9j5/results-dobbolab}
mkdir -p "$dest"
kubectl --context dobbolab-local -n gositter-bench exec bench -- sh -c 'cd /data && tar -c "$1" bin/BUILDINFO*.txt' sh "$r" |
  tar -x -C "$dest"
echo "$dest: $(find "$dest/$r" -name '*.jsonl' | wc -l | tr -d ' ') jsonl files, $(cat "$dest/$r"/*.jsonl | wc -l | tr -d ' ') rows"
