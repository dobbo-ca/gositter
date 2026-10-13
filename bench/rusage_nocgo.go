//go:build !linux && !(cgo && darwin)

package main

// Without cgo there is no proc_pid_rusage: no instructions, cycles, footprint.
// Linux has its own file, rusage_linux.go.

import "time"

func counters() (instr, cycles uint64) { return 0, 0 }

// The peak so far stands in for the current resident size.
func residentKB() int64 { return maxRSSKB() }

func footprintPeakKB() int64 { return -1 }

func startToMainNS(time.Time) int64 { return -1 }
