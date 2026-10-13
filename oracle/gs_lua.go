//go:build !nogo && gs_lua

package main

import "github.com/dobbo-ca/gositter/grammars/lua"

func init() { goLangs["lua"] = lua.Language }
