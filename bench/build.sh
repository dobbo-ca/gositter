#!/bin/sh
# Builds the bench binaries into $BENCH_DATA/bin (default ~/.cache/gositter-bench-9j5):
#   gsbench, gsbench-nocgo (gositter), cgobench (go-tree-sitter), cbench (pure C),
#   cbench-nopragma, oracle. Runs on macOS and Linux. On Linux gsbench is the
#   CGO_ENABLED=0 build and there is no gsbench-nocgo.
# Checks every parser.c against grammars/grammars.lock first. Grammar objects
# are cached in ../obj per compiler and flags. cbench is -O2 (the cgo default);
# the script then runs itself with CBENCH_OPT=-O3 (upstream Makefile default)
# and that pass builds only cbench-O3. Some parser.c carry "#pragma clang
# optimize off" and "#pragma GCC optimize ("O0")"; cbench-nopragma is cbench
# with the one that is active for $CC removed from them.
# On Linux cgo compiles every C file with -fPIC and cbench is the compiler
# default (PIE), so there is also cbench-pic: cbench with -fPIC on lib.c and
# the grammars, the side c-pic. cgo against c-pic is the binding alone.
# CC defaults to "go env CC", the compiler cgo uses. BENCH_CC=clang adds
# cbench-clang: cbench (-O2) built by that compiler, the side c-clang.
set -eu
cd "$(dirname "$0")"
CACHE=${BENCH_DATA:-/Users/christopherdobbyn/.cache/gositter-bench-9j5}
BIN=$CACHE/bin
LOCK=../grammars/grammars.lock
CC=${CC:-$(go env CC)}
OS=$(uname -s)
OPT=${CBENCH_OPT:--O2}
JOBS=${JOBS:-4}
mkdir -p "$BIN"
CB=cbench
[ "$OPT" = -O2 ] || CB=cbench$OPT
CB=$CB${CBENCH_SUFFIX:-}
sha() { if command -v sha256sum > /dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
INFO=$BIN/BUILDINFO${CB#cbench}.txt

# The C sources are the module versions of go.mod, so cgo and pure C compile the same files.
TS=$(go list -m -f '{{.Dir}}' github.com/tree-sitter/go-tree-sitter)
TSVER=$(go list -m -f '{{.Version}}' github.com/tree-sitter/go-tree-sitter)
[ "$TSVER" = v0.25.0 ] || { echo "go-tree-sitter is $TSVER, want v0.25.0" >&2; exit 1; }

PIC=""
[ "${CBENCH_SUFFIX:-}" = -pic ] && PIC=" -fPIC"
LIBFLAGS="$OPT$PIC -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE -I$TS/include -I$TS/src"
GRFLAGS="$OPT$PIC -std=c11"
key=$(printf '%s\n%s\n%s\n' "$($CC --version)" "$LIBFLAGS" "$GRFLAGS" | sha | cut -c1-12)
OBJ=$CACHE/obj/$key
# the pragma form this compiler obeys (parser.c picks by __clang__, then __GNUC__)
if $CC -dM -E - < /dev/null | grep -q __clang__; then
  PRAGMA='#pragma clang optimize off'
else
  PRAGMA='#pragma GCC optimize ("O0")'
fi
mkdir -p "$OBJ"

if [ "$OS" = Darwin ]; then
  cpu="$(sysctl -n machdep.cpu.brand_string), $(sysctl -n hw.ncpu) cpus"
else
  cpu="$(sed -n 's/^model name[^:]*: //p' /proc/cpuinfo | head -1), $(nproc) cpus"
fi

{
  echo "built: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "host: $(uname -srm), $cpu"
  if [ "$OS" = Linux ]; then
    echo "glibc: $(getconf GNU_LIBC_VERSION 2> /dev/null || echo unknown)"
    echo "topology: $(lscpu | sed -n 's/^Socket(s): *//p') socket x $(lscpu | sed -n 's/^Core(s) per socket: *//p') cores x $(lscpu | sed -n 's/^Thread(s) per core: *//p') threads, smt active $(cat /sys/devices/system/cpu/smt/active 2> /dev/null || echo not readable)"
    echo "governor: $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2> /dev/null || echo not readable)"
    echo "cpufreq driver: $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_driver 2> /dev/null || echo not readable), epp $(cat /sys/devices/system/cpu/cpu0/cpufreq/energy_performance_preference 2> /dev/null || echo not readable)"
    echo "cgroup cpu.max: $(cat /sys/fs/cgroup/cpu.max 2> /dev/null || echo not readable)"
    echo "perf_event_paranoid: $(cat /proc/sys/kernel/perf_event_paranoid 2> /dev/null || echo not readable)"
  fi
  echo "go: $(go version)"
  echo "cc: $CC = $($CC --version | head -1)"
  echo "cc default pic/pie macros: $($CC -dM -E - < /dev/null | grep -iE '__pi[ce]__' | tr '\n' ' ')"
  echo "tree-sitter runtime: go-tree-sitter $TSVER src/lib.c, sha256 of src/*.c src/*.h concatenated: $(cat "$TS"/src/*.c "$TS"/src/*.h | sha | cut -d' ' -f1)"
  echo
  echo "$CB (pure C):"
  echo "  lib.c:     $CC -c $LIBFLAGS"
  echo "  parser.c, scanner.c: $CC -c $GRFLAGS -I<grammar src dir>"
  echo "  cbench.c:  $CC -c $OPT -std=c11 -I$TS/include"
  echo "  link:      $CC -o $CB cbench.o lib.o <42 grammar objects> -lpthread"
  echo "  no LTO, no -DNDEBUG (as cgo). object cache: $OBJ"
  echo "cgobench: CGO_ENABLED=1 go build -tags cgoside"
  echo "  CGO_CFLAGS=$(go env CGO_CFLAGS) CGO_LDFLAGS=$(go env CGO_LDFLAGS) CC=$(go env CC)"
  echo "  go-tree-sitter #cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE"
  echo "  grammar bindings #cgo CFLAGS: -std=c11 -fPIC"
  if [ "$OS" = Linux ]; then
    echo "  cgo itself adds -fPIC -m64 -pthread -fmessage-length=0 to every C file (go build -n), so lib.c is"
    echo "  -fPIC in cgobench and PIE (the cc default) in cbench: not the same flags. With gcc -fPIC stops"
    echo "  inlining of exported functions. cbench-pic is cbench with -fPIC on lib.c, parser.c and scanner.c."
  fi
  if [ "$OS" = Darwin ]; then
    echo "gsbench: CGO_ENABLED=1 go build (cgo only for proc_pid_rusage)"
    echo "gsbench-nocgo: CGO_ENABLED=0 go build"
  else
    echo "gsbench: CGO_ENABLED=0 go build (counters by perf_event_open, no cgo)"
  fi
  echo "GOFLAGS=$(go env GOFLAGS) GOARCH=$(go env GOARCH) GOAMD64=$(go env GOAMD64) GOARM64=$(go env GOARM64) GOEXPERIMENT=$(go env GOEXPERIMENT)"
  echo
  echo "grammars (package module version dir, parser.c sha256 = grammars.lock):"
} > "$INFO"

jobs=$OBJ/jobs.txt
: > "$jobs"
objs=""
pragma=""
add() { # src out flags
  objs="$objs $2"
  [ -f "$2" ] || printf '%s\t%s\t%s\n' "$1" "$2" "$3" >> "$jobs"
}
add "$TS/src/lib.c" "$OBJ/lib.o" "$LIBFLAGS"
bad=0
while read -r g mod ver dir sha; do
  case $g in ''|\#*) continue ;; esac
  got=$(go list -m -f '{{.Version}}' "$mod")
  [ "$got" = "$ver" ] || { echo "MISMATCH $g: go.mod has $mod $got, grammars.lock $ver" >&2; bad=1; continue; }
  src=$(go list -m -f '{{.Dir}}' "$mod")/$dir
  have=$(sha "$src/parser.c" | cut -d' ' -f1)
  [ "$have" = "$sha" ] || { echo "MISMATCH $g: $src/parser.c sha256 $have, grammars.lock $sha" >&2; bad=1; continue; }
  echo "  $g $mod $ver $dir $sha OK" >> "$INFO"
  add "$src/parser.c" "$OBJ/$g-parser.o" "$GRFLAGS -I$src"
  if grep -q "^$PRAGMA" "$src/parser.c"; then
    pragma="$pragma $g"
    if [ $CB = cbench ]; then
      # copy without the pragma; the module cache stays as it is
      sed "/^$PRAGMA/d" "$src/parser.c" > "$OBJ/$g-parser-nopragma.c"
      [ -f "$OBJ/$g-parser-nopragma.o" ] ||
        printf '%s\t%s\t%s\n' "$OBJ/$g-parser-nopragma.c" "$OBJ/$g-parser-nopragma.o" "$GRFLAGS -I$src" >> "$jobs"
    fi
  fi
  [ -f "$src/scanner.c" ] && add "$src/scanner.c" "$OBJ/$g-scanner.o" "$GRFLAGS -I$src"
done < "$LOCK"
[ $bad = 0 ] || { echo "parser.c does not match grammars.lock" >&2; exit 1; }

# One compile per line; a .o appears only when its compile succeeded.
tr '\t\n' '\0\0' < "$jobs" | xargs -0 -r -n 3 -P "$JOBS" sh -c 'nice -n 19 '"$CC"' -c $2 -o "$1.tmp" "$0" && mv "$1.tmp" "$1" && echo "compiled $1" >&2'
nice -n 19 $CC -c $OPT -std=c11 -I"$TS/include" -o "$OBJ/cbench.o" purec/cbench.c
nice -n 19 $CC -o "$BIN/$CB" "$OBJ/cbench.o" $objs -lpthread
if [ $CB != cbench ]; then
  {
    echo
    echo "pragma: parser.c of these grammars has the line '$PRAGMA', active for $CC:$pragma"
    echo
    echo "binary (sha256):"
    (cd "$BIN" && sha $CB | sed 's/^/  /')
  } >> "$INFO"
  exit 0
fi
npobjs=$objs
for g in $pragma; do
  npobjs=$(echo "$npobjs" | sed "s|$OBJ/$g-parser.o|$OBJ/$g-parser-nopragma.o|")
done
nice -n 19 $CC -o "$BIN/cbench-nopragma" "$OBJ/cbench.o" $npobjs -lpthread
CC=$CC CBENCH_OPT=-O3 sh ./build.sh > /dev/null  # not "$0": it may be relative to the old cwd
bins="cgobench cbench cbench-nopragma cbench-O3 oracle"
infos="$BIN/BUILDINFO-O3.txt"
if [ "$OS" = Linux ]; then
  CC=$CC CBENCH_SUFFIX=-pic sh ./build.sh > /dev/null
  bins="$bins cbench-pic"
  infos="$infos $BIN/BUILDINFO-pic.txt"
fi
if [ -n "${BENCH_CC:-}" ]; then
  CC=$BENCH_CC CBENCH_SUFFIX=-clang sh ./build.sh > /dev/null
  bins="$bins cbench-clang"
  infos="$infos $BIN/BUILDINFO-clang.txt"
fi

if [ "$OS" = Darwin ]; then
  CGO_ENABLED=1 nice -n 19 go build -o "$BIN/gsbench" .
  CGO_ENABLED=0 nice -n 19 go build -o "$BIN/gsbench-nocgo" .
  bins="gsbench gsbench-nocgo $bins"
else
  CGO_ENABLED=0 nice -n 19 go build -o "$BIN/gsbench" .
  bins="gsbench $bins"
fi
CGO_ENABLED=1 nice -n 19 go build -tags cgoside -o "$BIN/cgobench" .

# oracle, with the tags of oracle/build.sh
tags=""
for f in ../grammars/*/tables.go; do
  [ -f "$f" ] && tags="$tags gs_$(basename "$(dirname "$f")")"
done
(cd ../oracle && nice -n 19 go build -tags "$tags" -o "$BIN/oracle" .)

{
  echo
  echo "pragma: parser.c of these grammars has the line '$PRAGMA', active for $CC, as upstream ships it:$pragma"
  echo "  cbench, cbench-O3, cgobench and oracle compile those parser.c without optimisation at any -O level."
  echo "cbench-nopragma: cbench (same flags, same lib.o, cbench.o and scanner objects) with that one line"
  echo "  removed from those parser.c (sed copy <grammar>-parser-nopragma.c in $OBJ)."
  echo
  echo "oracle: go build -tags \"$tags\""
  echo
  echo "binaries (sha256):"
  (cd "$BIN" && sha $bins | sed 's/^/  /')
} >> "$INFO"
cat "$INFO" $infos
