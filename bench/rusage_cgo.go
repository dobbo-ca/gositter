//go:build cgo && darwin

package main

// Process counters from proc_pid_rusage. cgo is used here only for that.

/*
#include <libproc.h>
#include <unistd.h>

static int usage(struct rusage_info_v4 *ri) {
	return proc_pid_rusage(getpid(), RUSAGE_INFO_V4, (rusage_info_t *)ri);
}

// Process start time in microseconds since the epoch.
static int64_t start_us(void) {
	struct proc_bsdinfo bi;
	if (proc_pidinfo(getpid(), PROC_PIDTBSDINFO, 0, &bi, sizeof bi) != sizeof bi) return -1;
	return (int64_t)bi.pbi_start_tvsec * 1000000 + (int64_t)bi.pbi_start_tvusec;
}
*/
import "C"

import "time"

// Instructions retired and CPU cycles of the whole process.
func counters() (instr, cycles uint64) {
	var ri C.struct_rusage_info_v4
	if C.usage(&ri) != 0 {
		return 0, 0
	}
	return uint64(ri.ri_instructions), uint64(ri.ri_cycles)
}

func residentKB() int64 {
	var ri C.struct_rusage_info_v4
	if C.usage(&ri) != 0 {
		return -1
	}
	return int64(ri.ri_resident_size) / 1024
}

func footprintPeakKB() int64 {
	var ri C.struct_rusage_info_v4
	if C.usage(&ri) != 0 {
		return -1
	}
	return int64(ri.ri_lifetime_max_phys_footprint) / 1024
}

func startToMainNS(tMain time.Time) int64 {
	s := int64(C.start_us())
	if s < 0 {
		return -1
	}
	return tMain.UnixNano() - s*1000
}
