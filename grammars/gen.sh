#!/bin/sh
# Regenerates tables.go, lex.go and language.go of every grammar (or of the
# ones named) from the parser.c files in the Go module cache, at the versions
# and hashes pinned in grammars.lock.
#
#   ./gen.sh                  all grammars
#   ./gen.sh python golang    only these
#   ./gen.sh -update          after changing a version in grammars.lock
#
# Rerun it for a grammar after adding its scanner.go: language.go only wires
# ExternalScanner.New when scanner.go exists.
set -e
cd "$(dirname "$0")/.."
modcache=$(go env GOMODCACHE)
grep -v '^#' grammars/grammars.lock | while read -r pkg module version dir sum; do
  [ -z "$pkg" ] || [ -d "$modcache/$module@$version" ] || GOFLAGS=-mod=mod go mod download "$module@$version"
done
exec go run ./cmd/gengrammar -lock grammars/grammars.lock -out grammars -modcache "$modcache" "$@"
