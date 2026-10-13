#!/bin/sh
# Builds ./oracle. The gositter side gets every grammar whose package exists
# (grammars/<name>/tables.go). If the root module does not compile right now,
# falls back to the C side only so dump-c, log-c and lists keep working.
#   ./build.sh            all generated grammars
#   ./build.sh python go  only these (package names: golang, csharp)
cd "$(dirname "$0")" || exit 1
tags=""
if [ $# -gt 0 ]; then
  for g in "$@"; do tags="$tags gs_$g"; done
else
  for f in ../grammars/*/tables.go; do
    [ -f "$f" ] && tags="$tags gs_$(basename "$(dirname "$f")")"
  done
fi
echo "gositter grammars:${tags:- none}" >&2
if go build -tags "$tags" -o oracle . ; then exit 0; fi
echo "gositter side does not build; building C side only (-tags nogo)" >&2
go build -tags nogo -o oracle .
