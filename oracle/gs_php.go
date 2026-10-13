//go:build !nogo && gs_php

package main

import "github.com/dobbo-ca/gositter/grammars/php"

func init() { goLangs["php"] = php.Language }
