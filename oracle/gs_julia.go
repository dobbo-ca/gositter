//go:build !nogo && gs_julia

package main

import "github.com/dobbo-ca/gositter/grammars/julia"

func init() { goLangs["julia"] = julia.Language }
