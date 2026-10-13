//go:build !nogo && gs_zig

package main

import "github.com/dobbo-ca/gositter/grammars/zig"

func init() { goLangs["zig"] = zig.Language }
