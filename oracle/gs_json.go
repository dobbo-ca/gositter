//go:build !nogo && gs_json

package main

import "github.com/dobbo-ca/gositter/grammars/json"

func init() { goLangs["json"] = json.Language }
