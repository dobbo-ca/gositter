//go:build !nogo && gs_c

package main

import "github.com/dobbo-ca/gositter/grammars/c"

func init() { goLangs["c"] = c.Language }
