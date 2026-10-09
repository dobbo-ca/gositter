package main

/*
#include <libproc.h>
#include <unistd.h>

static int counters(uint64_t *instructions, uint64_t *cycles) {
	struct rusage_info_v4 ri;
	if (proc_pid_rusage(getpid(), RUSAGE_INFO_V4, (rusage_info_t *)&ri) != 0) return -1;
	*instructions = ri.ri_instructions;
	*cycles = ri.ri_cycles;
	return 0;
}
*/
import "C"

// counters returns the instructions retired and CPU cycles of this process.
// Unlike wall time they hold when the scheduler starves the benchmark.
func counters() (instructions, cycles uint64) {
	var i, c C.uint64_t
	if C.counters(&i, &c) != 0 {
		return 0, 0
	}
	return uint64(i), uint64(c)
}
