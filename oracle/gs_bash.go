//go:build !nogo && gs_bash

package main

import "github.com/dobbo-ca/gositter/grammars/bash"

func init() { goLangs["bash"] = bash.Language }
