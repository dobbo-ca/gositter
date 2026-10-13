//go:build !nogo && gs_rust

package main

import "github.com/dobbo-ca/gositter/grammars/rust"

func init() { goLangs["rust"] = rust.Language }
