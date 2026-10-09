//go:build !nogo && gs_golang

package main

import "github.com/dobbo-ca/gositter/grammars/golang"

func init() { goLangs["golang"] = golang.Language }
