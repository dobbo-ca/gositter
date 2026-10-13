//go:build !nogo && gs_csharp

package main

import "github.com/dobbo-ca/gositter/grammars/csharp"

func init() { goLangs["csharp"] = csharp.Language }
