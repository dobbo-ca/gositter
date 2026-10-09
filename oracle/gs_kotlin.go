//go:build !nogo && gs_kotlin

package main

import "github.com/dobbo-ca/gositter/grammars/kotlin"

func init() { goLangs["kotlin"] = kotlin.Language }
