# gositter

A Go port of the tree-sitter parser runtime, version 0.25.0. No cgo.

The goal is the same syntax tree as the C runtime for every input, broken
files included. Parser tables come from each grammar's generated `parser.c`.
External scanners are ported by hand.

Status: prototype.
