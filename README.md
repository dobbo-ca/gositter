# gositter

A Go port of the tree-sitter parser runtime, version 0.25.0. No cgo.

The goal is the same syntax tree as the C runtime for every input, broken
files included. Parser tables come from each grammar's generated `parser.c`.
External scanners are ported by hand.

Grammars: bash, c, cpp, c-sharp, go, hcl, java, javascript, json, julia,
kotlin, lua, php, python, ruby, rust, scala, typescript, tsx, verilog, zig.

`oracle/` is a separate module that uses cgo to compare every tree with the
C runtime. The root module needs only the standard library.

## Install

    go get github.com/dobbo-ca/gositter

Needs Go 1.25 or later.

## Usage

```go
package main

import (
	"fmt"
	"strings"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/python"
)

func main() {
	src := []byte("def add(a, b):\n    return a + b\n")

	p := gositter.NewParser()
	if err := p.SetLanguage(python.Language()); err != nil {
		panic(err)
	}
	tree := p.Parse(src)

	var walk func(n gositter.Node, depth int)
	walk = func(n gositter.Node, depth int) {
		fmt.Printf("%s%s [%d, %d)\n", strings.Repeat("  ", depth), n.Kind(), n.StartByte(), n.EndByte())
		for _, c := range n.NamedChildren(nil) {
			walk(c, depth+1)
		}
	}
	walk(tree.RootNode(), 0)
}
```

The same code runs as `Example` in `example_test.go`. A `Parser` is not safe
for concurrent use, and a tree is valid until the next `Parse`, `Reset` or
`SetLanguage` on its parser.

`ParseBudget(src, maxStall)` is `Parse` that returns nil once the parser has
advanced more than `maxStall` times in a row without passing the furthest
byte it has reached, which bounds the time spent on pathological input.

## Oracle

The oracle needs cgo and a C compiler. It parses every file with both
runtimes and reports the trees that differ:

    cd oracle
    ./build.sh
    ./oracle cmp python /path/to/python/files

## Status

v0.x: the API can change in any minor release. Used by graphify-go.

## License

MIT, see `LICENSE`. The runtime is a port of tree-sitter and each package
under `grammars/` derives from its upstream grammar. `NOTICE.md` lists them
and the license text of each.
