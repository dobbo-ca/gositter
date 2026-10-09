//go:build !nogo && gs_java

package main

import "github.com/dobbo-ca/gositter/grammars/java"

func init() { goLangs["java"] = java.Language }
