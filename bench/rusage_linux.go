//go:build linux

package main

// Linux counters: perf_event_open as a raw syscall, memory from
// /proc/self/status. No cgo, so gsbench and cgobench share this file.
//
// An event covers one thread, plus (inherit) the threads that thread creates
// later. The Go runtime has several threads before init runs, so one pair of
// events is opened per existing thread and all reads are summed. Counting is
// user mode only and starts at init, not at exec.

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// struct perf_event_attr, PERF_ATTR_SIZE_VER0 (64 bytes).
type perfAttr struct {
	typ, size  uint32
	config     uint64
	_          [2]uint64 // sample_period, sample_type
	readFormat uint64
	flags      uint64
	_          [2]uint64 // wakeup_events, bp_type, config1
}

const (
	perfInherit       = 1 << 1
	perfExcludeKernel = 1 << 5
	perfExcludeHV     = 1 << 6
	perfFDCloexec     = 1 << 3
)

// PERF_TYPE_HARDWARE configs: instructions, cycles.
var perfConfigs = [2]uint64{1, 0}

var perfFDs [2][]int

// BENCH_NOPERF: run.py first counts with perf stat, these would compete.
func init() {
	if os.Getenv("BENCH_NOPERF") == "" {
		perfOpen()
	}
}

func tids() []int {
	ents, _ := os.ReadDir("/proc/self/task")
	var out []int
	for _, e := range ents {
		if n, err := strconv.Atoi(e.Name()); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func perfClose() {
	for i := range perfFDs {
		for _, fd := range perfFDs[i] {
			syscall.Close(fd)
		}
		perfFDs[i] = nil
	}
}

func perfOpen() {
	for try := 0; try < 10; try++ {
		before := tids()
		for _, tid := range before {
			for i, cfg := range perfConfigs {
				a := perfAttr{size: 64, config: cfg, flags: perfInherit | perfExcludeKernel | perfExcludeHV}
				fd, _, e := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN, uintptr(unsafe.Pointer(&a)),
					uintptr(tid), ^uintptr(0), ^uintptr(0), perfFDCloexec, 0)
				if e == syscall.ESRCH { // thread gone
					continue
				}
				if e != 0 {
					fmt.Fprintf(os.Stderr, "bench: WARNING perf_event_open failed (%v): instr and cycles are 0\n", e)
					perfClose()
					return
				}
				perfFDs[i] = append(perfFDs[i], int(fd))
			}
		}
		// A thread made meanwhile may or may not have inherited: start over.
		if slices.Equal(before, tids()) {
			return
		}
		perfClose()
	}
	fmt.Fprintln(os.Stderr, "bench: WARNING thread set kept changing during perf setup: instr and cycles are 0")
}

func perfSum(fds []int) (sum uint64) {
	var v uint64
	for _, fd := range fds {
		if n, _ := syscall.Read(fd, (*[8]byte)(unsafe.Pointer(&v))[:]); n == 8 {
			sum += v
		}
	}
	return sum
}

// User-mode instructions and cycles of all threads since init.
func counters() (instr, cycles uint64) {
	return perfSum(perfFDs[0]), perfSum(perfFDs[1])
}

// statusKB returns a "VmRSS:"-style kB field of /proc/self/status.
func statusKB(key string) int64 {
	data, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(data), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == key {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			return n
		}
	}
	return -1
}

func residentKB() int64 { return statusKB("VmRSS:") }

// Linux has no phys_footprint: this is VmHWM, the peak resident size.
func footprintPeakKB() int64 { return statusKB("VmHWM:") }

// /proc/self/stat has the start time in 10 ms ticks only: not measured.
func startToMainNS(time.Time) int64 { return -1 }
