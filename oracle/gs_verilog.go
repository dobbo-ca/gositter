//go:build !nogo && gs_verilog

package main

import "github.com/dobbo-ca/gositter/grammars/verilog"

func init() { goLangs["verilog"] = verilog.Language }
