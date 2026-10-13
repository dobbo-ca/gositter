//go:build !nogo && gs_hcl

package main

import "github.com/dobbo-ca/gositter/grammars/hcl"

func init() { goLangs["hcl"] = hcl.Language }
