//go:build !nogo && gs_tsx

package main

import "github.com/dobbo-ca/gositter/grammars/tsx"

func init() { goLangs["tsx"] = tsx.Language }
