//go:build !nogo && gs_python

package main

import "github.com/dobbo-ca/gositter/grammars/python"

func init() { goLangs["python"] = python.Language }
