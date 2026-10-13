//go:build !nogo && gs_typescript

package main

import "github.com/dobbo-ca/gositter/grammars/typescript"

func init() { goLangs["typescript"] = typescript.Language }
