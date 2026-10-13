#!/bin/bash
# deploy.sh [pod|tools|sources|data|mods|all]   (default: all)
#   pod      create namespace and pod (pod.yaml)
#   tools    apt packages in the pod
#   sources  sync the repo (git HEAD + working bench/) to /work/gositter
#   data     sync corpora and rewritten lists to /data
#   mods     go mod download in oracle/ and bench/, check parser.c sha256
# Only the dobbolab-local context, namespace gositter-bench.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
cache=/Users/christopherdobbyn/.cache
lists=$cache/gositter-bench-9j5/lists
K="kubectl --context dobbolab-local -n gositter-bench"
step=${1:-all}
want() { [ "$step" = all ] || [ "$step" = "$1" ]; }

if want pod; then
  kubectl --context dobbolab-local apply -f "$here/pod.yaml"
  $K wait --for=condition=Ready pod/bench --timeout=300s
fi

if want tools; then
  $K exec bench -- bash -c 'export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq && apt-get install -y -qq gcc clang python3 procps util-linux time make linux-perf'
fi

if want sources; then
  # tracked files at HEAD, then bench/ as it is on disk (untracked, no binaries)
  t=$(mktemp -d)
  git -C "$repo" archive HEAD | tar -x -C "$t"
  rm -rf "$t/bench"
  mkdir "$t/bench"
  (cd "$repo/bench" && COPYFILE_DISABLE=1 tar -c --no-xattrs --exclude __pycache__ --exclude '*.pyc' .) | tar -x -C "$t/bench"
  $K exec bench -- sh -c 'rm -rf /work/gositter && mkdir -p /work/gositter'
  (cd "$t" && COPYFILE_DISABLE=1 tar -c --no-xattrs .) | $K exec -i bench -- tar -x --warning=no-timestamp -C /work/gositter
  rm -rf "$t"
fi

if want data; then
  # Mac path /Users/christopherdobbyn/.cache/X maps to /data/files/X
  t=$(mktemp -d)
  { cat "$lists"/*.txt; cut -f2 "$lists"/EXTREME.tsv "$lists"/SCREEN.tsv | grep '^/'; } |
    sort -u | sed "s#^$cache/##" > "$t/files.rel"
  mkdir "$t/lists"
  for f in "$lists"/*.txt; do sed "s#^$cache/#/data/files/#" "$f" > "$t/lists/$(basename "$f")"; done
  for f in "$lists"/*.tsv; do
    awk -v c="$cache/" 'BEGIN{FS=OFS="\t"} index($2,c)==1{$2="/data/files/" substr($2,length(c)+1)}1' "$f" > "$t/lists/$(basename "$f")"
  done
  (cd "$cache" && COPYFILE_DISABLE=1 tar -c --no-xattrs -T "$t/files.rel" -f -) |
    $K exec -i bench -- sh -c 'mkdir -p /data/files && tar -x -C /data/files'
  $K exec bench -- sh -c 'rm -rf /data/lists && mkdir -p /data/lists'
  (cd "$t/lists" && COPYFILE_DISABLE=1 tar -c --no-xattrs -f - .) | $K exec -i bench -- tar -x -C /data/lists
  rm -rf "$t"
  # compare counts, bytes and hashes per list, Mac against pod
  python3 "$here/verify_data.py" "$lists" > /tmp/verify.mac
  $K exec -i bench -- python3 - /data/lists < "$here/verify_data.py" > /tmp/verify.pod
  diff /tmp/verify.mac /tmp/verify.pod && echo "data: $(wc -l < /tmp/verify.pod) lists identical"
fi

if want mods; then
  $K exec bench -- bash -c 'set -e
    export PATH=$PATH:/usr/local/go/bin
    (cd /work/gositter/oracle && go mod download)
    (cd /work/gositter/bench && go mod download)
    cd /work/gositter/grammars
    grep -v "^#" grammars.lock | while read -r pkg mod ver dir sha; do
      d=$(go list -m -f "{{.Dir}}" "$mod@$ver" 2>/dev/null || true)
      [ -n "$d" ] || d=$(cd /work/gositter/oracle && go list -m -f "{{.Dir}}" "$mod")
      got=$(sha256sum "$d/$dir/parser.c" | cut -d" " -f1)
      [ "$got" = "$sha" ] && echo "ok   $pkg" || echo "FAIL $pkg $got"
    done'
fi
