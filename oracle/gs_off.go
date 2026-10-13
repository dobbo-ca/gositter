//go:build nogo

package main

import "errors"

// Built with -tags nogo: the C side only, for when the root module is broken.
func openGo(string) (parser, error) {
	return nil, errors.New("built with -tags nogo: no gositter side")
}
