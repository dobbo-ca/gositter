//go:build !darwin

package main

func counters() (instructions, cycles uint64) { return 0, 0 }
