# gositter

A Go port of the tree-sitter parser runtime, version 0.25.0. No cgo.

The goal is the same syntax tree as the C runtime for every input, broken
files included. Parser tables come from each grammar's generated `parser.c`.
External scanners are ported by hand.

Grammars: bash, c, cpp, c-sharp, go, hcl, java, javascript, json, julia,
kotlin, lua, php, python, ruby, rust, scala, typescript, tsx, verilog, zig.

`oracle/` is a separate module that uses cgo to compare every tree with the
C runtime. The root module needs only the standard library.

Status: used by graphify-go. No tagged release yet, the API can change.
