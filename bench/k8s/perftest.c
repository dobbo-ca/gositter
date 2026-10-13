#include <linux/perf_event.h>
#include <sys/syscall.h>
#include <sys/ioctl.h>
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <stdint.h>
static int op(unsigned long long cfg) {
  struct perf_event_attr a; memset(&a, 0, sizeof a);
  a.type = PERF_TYPE_HARDWARE; a.size = sizeof a; a.config = cfg;
  a.disabled = 1; a.inherit = 1; a.exclude_kernel = 1; a.exclude_hv = 1;
  return syscall(SYS_perf_event_open, &a, 0, -1, -1, 0);
}
int main(void) {
  int fi = op(PERF_COUNT_HW_INSTRUCTIONS), fc = op(PERF_COUNT_HW_CPU_CYCLES);
  if (fi < 0 || fc < 0) { printf("open failed: %s\n", strerror(errno)); return 1; }
  ioctl(fi, PERF_EVENT_IOC_ENABLE, 0); ioctl(fc, PERF_EVENT_IOC_ENABLE, 0);
  volatile uint64_t s = 0;
  for (uint64_t i = 0; i < 100000000; i++) s += i;
  ioctl(fi, PERF_EVENT_IOC_DISABLE, 0); ioctl(fc, PERF_EVENT_IOC_DISABLE, 0);
  uint64_t ni, nc; read(fi, &ni, 8); read(fc, &nc, 8);
  printf("instructions=%llu cycles=%llu ipc=%.2f\n", (unsigned long long)ni, (unsigned long long)nc, (double)ni / nc);
  return 0;
}
