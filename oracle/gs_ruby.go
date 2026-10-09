//go:build !nogo && gs_ruby

package main

import "github.com/dobbo-ca/gositter/grammars/ruby"

func init() { goLangs["ruby"] = ruby.Language }
