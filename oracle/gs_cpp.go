//go:build !nogo && gs_cpp

package main

import "github.com/dobbo-ca/gositter/grammars/cpp"

func init() { goLangs["cpp"] = cpp.Language }
