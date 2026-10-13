//go:build !nogo && gs_javascript

package main

import "github.com/dobbo-ca/gositter/grammars/javascript"

func init() { goLangs["javascript"] = javascript.Language }
