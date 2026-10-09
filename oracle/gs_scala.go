//go:build !nogo && gs_scala

package main

import "github.com/dobbo-ca/gositter/grammars/scala"

func init() { goLangs["scala"] = scala.Language }
