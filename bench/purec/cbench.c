// cbench is the pure C side of the gositter vs C tree-sitter comparison:
// tree-sitter 0.25.0 (lib.c) and the 21 grammars, pthreads, no Go in the
// process. It does the same work as ../main.go and prints the same JSON.
//
//   cbench -g <grammar> -l <filelist> [-j N] [-what parse|walk|cursor] [-n reps] [-warm W] [-allocs]
//   cbench -first -g <grammar> -f <file>

#ifdef __linux__
#define _GNU_SOURCE
#include <linux/perf_event.h>
#include <sys/syscall.h>
#else
#include <libproc.h>
#endif
#include <errno.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#include <tree_sitter/api.h>

#define L(n) const TSLanguage *tree_sitter_##n(void);
L(bash) L(c) L(cpp) L(c_sharp) L(go) L(hcl) L(java) L(javascript) L(json) L(julia) L(kotlin)
L(lua) L(php) L(python) L(ruby) L(rust) L(scala) L(tsx) L(typescript) L(verilog) L(zig)

// Keys are the gositter package names.
static const struct { const char *name; const TSLanguage *(*fn)(void); } langs[] = {
  {"bash", tree_sitter_bash}, {"c", tree_sitter_c}, {"cpp", tree_sitter_cpp},
  {"csharp", tree_sitter_c_sharp}, {"golang", tree_sitter_go}, {"hcl", tree_sitter_hcl},
  {"java", tree_sitter_java}, {"javascript", tree_sitter_javascript}, {"json", tree_sitter_json},
  {"julia", tree_sitter_julia}, {"kotlin", tree_sitter_kotlin}, {"lua", tree_sitter_lua},
  {"php", tree_sitter_php}, {"python", tree_sitter_python}, {"ruby", tree_sitter_ruby},
  {"rust", tree_sitter_rust}, {"scala", tree_sitter_scala}, {"tsx", tree_sitter_tsx},
  {"typescript", tree_sitter_typescript}, {"verilog", tree_sitter_verilog}, {"zig", tree_sitter_zig},
};

static void die(const char *msg, const char *arg) {
  fprintf(stderr, "cbench: %s %s\n", msg, arg ? arg : "");
  exit(2);
}

static const TSLanguage *language(const char *name) {
  for (size_t i = 0; i < sizeof langs / sizeof *langs; i++)
    if (!strcmp(langs[i].name, name)) return langs[i].fn();
  die("no grammar", name);
  return NULL;
}

static TSParser *new_parser(const char *grammar) {
  const TSLanguage *lang = language(grammar);
  TSParser *p = ts_parser_new();
  if (!ts_parser_set_language(p, lang)) die("incompatible language", grammar);
  return p;
}

// ---- counters

typedef struct { int64_t user, sys; uint64_t instr, cycles; } Usage;

#ifdef __linux__

static int64_t now_ns(void) {
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (int64_t)ts.tv_sec * 1000000000 + ts.tv_nsec;
}

// Instructions and cycles, user mode only. Opened on the main thread before
// any pthread_create; inherit makes them cover every later thread.
static int perf_fd[2] = {-1, -1};

static void perf_open(void) {
  static const uint64_t config[2] = {PERF_COUNT_HW_INSTRUCTIONS, PERF_COUNT_HW_CPU_CYCLES};
  for (int i = 0; i < 2; i++) {
    struct perf_event_attr a;
    memset(&a, 0, sizeof a);
    a.type = PERF_TYPE_HARDWARE;
    a.size = sizeof a;
    a.config = config[i];
    a.inherit = 1;
    a.exclude_kernel = 1;
    a.exclude_hv = 1;
    perf_fd[i] = (int)syscall(SYS_perf_event_open, &a, 0, -1, -1, PERF_FLAG_FD_CLOEXEC);
    if (perf_fd[i] < 0) {
      fprintf(stderr, "cbench: WARNING perf_event_open failed (%s): instr and cycles are 0\n", strerror(errno));
      if (i) close(perf_fd[0]);
      perf_fd[0] = perf_fd[1] = -1;
      return;
    }
  }
}

static uint64_t perf_read(int fd) {
  uint64_t v = 0;
  if (fd < 0 || read(fd, &v, sizeof v) != sizeof v) return 0;
  return v;
}

static void counters(uint64_t *instr, uint64_t *cycles) {
  *instr = perf_read(perf_fd[0]);
  *cycles = perf_read(perf_fd[1]);
}

// A kB field of /proc/self/status, e.g. "VmRSS:".
static int64_t status_kb(const char *key) {
  FILE *f = fopen("/proc/self/status", "r");
  char line[256];
  int64_t v = -1;
  size_t n = strlen(key);
  while (f && fgets(line, sizeof line, f))
    if (!strncmp(line, key, n)) {
      v = atoll(line + n);
      break;
    }
  if (f) fclose(f);
  return v;
}

static int64_t resident_kb(void) { return status_kb("VmRSS:"); }
// No phys_footprint on Linux: VmHWM, the peak resident size.
static int64_t footprint_peak_kb(void) { return status_kb("VmHWM:"); }
// /proc/self/stat has the start time in 10 ms ticks only: not measured.
static int64_t start_to_main_ns(int64_t t_main_us) { (void)t_main_us; return -1; }
// Not ru_maxrss: it keeps the peak of the image before exec.
static int64_t maxrss_kb(void) { return footprint_peak_kb(); }

#else

static int64_t now_ns(void) { return (int64_t)clock_gettime_nsec_np(CLOCK_MONOTONIC); }

static void perf_open(void) {}

static struct rusage_info_v4 proc_usage(void) {
  struct rusage_info_v4 ri;
  memset(&ri, 0, sizeof ri);
  proc_pid_rusage(getpid(), RUSAGE_INFO_V4, (rusage_info_t *)&ri);
  return ri;
}

static void counters(uint64_t *instr, uint64_t *cycles) {
  struct rusage_info_v4 ri = proc_usage();
  *instr = ri.ri_instructions;
  *cycles = ri.ri_cycles;
}

static int64_t resident_kb(void) { return (int64_t)(proc_usage().ri_resident_size / 1024); }
static int64_t footprint_peak_kb(void) { return (int64_t)(proc_usage().ri_lifetime_max_phys_footprint / 1024); }

static int64_t start_to_main_ns(int64_t t_main_us) {
  struct proc_bsdinfo bi;
  if (proc_pidinfo(getpid(), PROC_PIDTBSDINFO, 0, &bi, sizeof bi) != sizeof bi) return -1;
  return (t_main_us - ((int64_t)bi.pbi_start_tvsec * 1000000 + (int64_t)bi.pbi_start_tvusec)) * 1000;
}

static int64_t maxrss_kb(void) {
  struct rusage ru;
  getrusage(RUSAGE_SELF, &ru);
  return ru.ru_maxrss / 1024; // darwin reports bytes
}

#endif

static int64_t tv_ns(struct timeval tv) { return (int64_t)tv.tv_sec * 1000000000 + (int64_t)tv.tv_usec * 1000; }

static Usage usage(void) {
  struct rusage ru;
  getrusage(RUSAGE_SELF, &ru);
  Usage u = {tv_ns(ru.ru_utime), tv_ns(ru.ru_stime), 0, 0};
  counters(&u.instr, &u.cycles);
  return u;
}

// ---- counting allocator, installed only with -allocs

static atomic_llong a_calls, a_bytes;

static void count(size_t n) {
  atomic_fetch_add_explicit(&a_calls, 1, memory_order_relaxed);
  atomic_fetch_add_explicit(&a_bytes, (long long)n, memory_order_relaxed);
}
static void *c_malloc(size_t n) { count(n); return malloc(n); }
static void *c_calloc(size_t c, size_t n) { count(c * n); return calloc(c, n); }
static void *c_realloc(void *p, size_t n) { count(n); return realloc(p, n); }

// ---- the work

typedef struct { char *data; uint32_t len; } File;
typedef struct { int64_t nodes, errs; uint64_t sum; } Acc;
typedef struct { TSParser *p; TSTree *tree; } Worker;
enum { PARSE, WALK, CURSOR };

static File *files;
static int nfiles, what;

static inline void visit(TSNode n, Acc *a) {
  const char *k = ts_node_type(n);
  a->sum += (unsigned char)k[0] + (uint64_t)ts_node_start_byte(n) + (uint64_t)ts_node_end_byte(n);
  a->nodes++;
}

static void walk(TSNode n, Acc *a) {
  visit(n, a);
  uint32_t cc = ts_node_child_count(n);
  for (uint32_t i = 0; i < cc; i++) walk(ts_node_child(n, i), a);
}

static void cursor_walk(TSNode root, Acc *a) {
  TSTreeCursor c = ts_tree_cursor_new(root);
  for (;;) {
    visit(ts_tree_cursor_current_node(&c), a);
    if (ts_tree_cursor_goto_first_child(&c)) continue;
    while (!ts_tree_cursor_goto_next_sibling(&c))
      if (!ts_tree_cursor_goto_parent(&c)) {
        ts_tree_cursor_delete(&c);
        return;
      }
  }
}

// Frees the previous tree, parses, returns the root's has_error.
static bool parse(Worker *w, const File *f) {
  if (w->tree) ts_tree_delete(w->tree);
  w->tree = ts_parser_parse_string(w->p, NULL, f->data, f->len);
  return ts_node_has_error(ts_tree_root_node(w->tree));
}

// ---- worker threads: made once, woken for each pass

static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t cv_go = PTHREAD_COND_INITIALIZER, cv_done = PTHREAD_COND_INITIALIZER;
static int gen, running;
static atomic_int next_file;
static Acc total;

static void *worker_main(void *arg) {
  Worker *w = arg;
  int seen = 0;
  for (;;) {
    pthread_mutex_lock(&mu);
    while (gen == seen) pthread_cond_wait(&cv_go, &mu);
    seen = gen;
    pthread_mutex_unlock(&mu);

    Acc a = {0};
    for (;;) {
      int i = atomic_fetch_add(&next_file, 1);
      if (i >= nfiles) break;
      if (parse(w, &files[i])) a.errs++;
      if (what == WALK) walk(ts_tree_root_node(w->tree), &a);
      else if (what == CURSOR) cursor_walk(ts_tree_root_node(w->tree), &a);
    }

    pthread_mutex_lock(&mu);
    total.nodes += a.nodes;
    total.errs += a.errs;
    total.sum += a.sum;
    if (--running == 0) pthread_cond_signal(&cv_done);
    pthread_mutex_unlock(&mu);
  }
}

static Acc pass(int j) {
  pthread_mutex_lock(&mu);
  total = (Acc){0};
  atomic_store(&next_file, 0);
  running = j;
  gen++;
  pthread_cond_broadcast(&cv_go);
  while (running) pthread_cond_wait(&cv_done, &mu);
  Acc t = total;
  pthread_mutex_unlock(&mu);
  return t;
}

static File read_file(const char *path) {
  FILE *f = fopen(path, "rb");
  if (!f) die("cannot read", path);
  fseek(f, 0, SEEK_END);
  long n = ftell(f);
  fseek(f, 0, SEEK_SET);
  File out = {malloc((size_t)n + 1), (uint32_t)n};
  if (fread(out.data, 1, (size_t)n, f) != (size_t)n) die("short read", path);
  fclose(f);
  return out;
}

static int first_main(int64_t t_main_us, const char *grammar, const char *path) {
  File f = read_file(path);
  Worker w = {0};
  int64_t t0 = now_ns();
  w.p = new_parser(grammar);
  int64_t t1 = now_ns();
  parse(&w, &f);
  int64_t t2 = now_ns();
  parse(&w, &f);
  int64_t t3 = now_ns();
  Usage u = usage();
  int64_t start = start_to_main_ns(t_main_us);
  printf("{\"type\":\"first\",\"side\":\"c\",\"grammar\":\"%s\",\"file_bytes\":%u,\"start_to_main_ns\":%lld,"
         "\"init_ns\":%lld,\"parse1_ns\":%lld,\"parse2_ns\":%lld,\"instr\":%llu,\"cycles\":%llu,"
         "\"user_ns\":%lld,\"sys_ns\":%lld,\"maxrss_kb\":%lld,\"footprint_peak_kb\":%lld}\n",
         grammar, f.len, (long long)start, (long long)(t1 - t0), (long long)(t2 - t1), (long long)(t3 - t2),
         (unsigned long long)u.instr, (unsigned long long)u.cycles, (long long)u.user, (long long)u.sys,
         (long long)maxrss_kb(), (long long)footprint_peak_kb());
  return 0;
}

int main(int argc, char **argv) {
  struct timeval tv;
  gettimeofday(&tv, NULL);
  int64_t t_main_us = (int64_t)tv.tv_sec * 1000000 + tv.tv_usec;
  if (!getenv("BENCH_NOPERF")) perf_open();  // run.py first counts with perf stat

  const char *grammar = "", *list = NULL, *file = NULL, *what_s = "parse";
  int j = 1, reps = 3, warm = 1, allocs = 0, first = 0;
  for (int i = 1; i < argc; i++) {
    const char *a = argv[i], *v = i + 1 < argc ? argv[i + 1] : "";
    if (!strcmp(a, "-allocs")) allocs = 1;
    else if (!strcmp(a, "-first")) first = 1;
    else if (!strcmp(a, "-g")) grammar = v, i++;
    else if (!strcmp(a, "-l")) list = v, i++;
    else if (!strcmp(a, "-f")) file = v, i++;
    else if (!strcmp(a, "-what")) what_s = v, i++;
    else if (!strcmp(a, "-j")) j = atoi(v), i++;
    else if (!strcmp(a, "-n")) reps = atoi(v), i++;
    else if (!strcmp(a, "-warm")) warm = atoi(v), i++;
    else die("unknown flag", a);
  }
  if (first) {
    if (!file) die("-first needs -f", NULL);
    return first_main(t_main_us, grammar, file);
  }
  if (!strcmp(what_s, "parse")) what = PARSE;
  else if (!strcmp(what_s, "walk")) what = WALK;
  else if (!strcmp(what_s, "cursor")) what = CURSOR;
  else die("-what must be parse, walk or cursor, got", what_s);
  if (!list || j < 1) die("usage: cbench -g grammar -l filelist [-j N]", NULL);

  FILE *lf = fopen(list, "r");
  if (!lf) die("cannot read", list);
  char line[8192];
  int cap = 0;
  long long bytes = 0;
  while (fgets(line, sizeof line, lf)) {
    char *s = line + strspn(line, " \t\r\n");
    size_t n = strlen(s);
    while (n > 0 && strchr(" \t\r\n", s[n - 1])) s[--n] = 0;
    if (n == 0) continue;
    if (nfiles == cap) files = realloc(files, (size_t)(cap = cap ? cap * 2 : 1024) * sizeof *files);
    files[nfiles] = read_file(s);
    bytes += files[nfiles++].len;
  }
  fclose(lf);

  if (allocs) ts_set_allocator(c_malloc, c_calloc, c_realloc, NULL);

  // The Child(i) walk recurses once per tree level: 512 MB of stack, virtual.
  pthread_attr_t attr;
  pthread_attr_init(&attr);
  pthread_attr_setstacksize(&attr, (size_t)512 << 20);
  Worker *ws = calloc((size_t)j, sizeof *ws);
  for (int i = 0; i < j; i++) {
    ws[i].p = new_parser(grammar);
    pthread_t th;
    if (pthread_create(&th, &attr, worker_main, &ws[i])) die("pthread_create failed", NULL);
  }
  printf("{\"type\":\"meta\",\"side\":\"c\",\"grammar\":\"%s\",\"what\":\"%s\",\"j\":%d,\"files\":%d,\"bytes\":%lld,"
         "\"rss_loaded_kb\":%lld}\n",
         grammar, what_s, j, nfiles, bytes, (long long)resident_kb());

  for (int i = 0; i < warm; i++) pass(j);
  long long calls0 = atomic_load(&a_calls), bytes0 = atomic_load(&a_bytes);
  Acc last = {0};
  for (int r = 0; r < reps; r++) {
    Usage u0 = usage();
    int64_t t0 = now_ns();
    last = pass(j);
    int64_t t1 = now_ns();
    Usage u1 = usage();
    printf("{\"type\":\"rep\",\"rep\":%d,\"wall_ns\":%lld,\"user_ns\":%lld,\"sys_ns\":%lld,\"instr\":%llu,"
           "\"cycles\":%llu,\"nodes\":%lld,\"error_files\":%lld}\n",
           r, (long long)(t1 - t0), (long long)(u1.user - u0.user), (long long)(u1.sys - u0.sys),
           (unsigned long long)(u1.instr - u0.instr), (unsigned long long)(u1.cycles - u0.cycles),
           (long long)last.nodes, (long long)last.errs);
  }
  long long calls = allocs ? atomic_load(&a_calls) - calls0 : -1;
  long long abytes = allocs ? atomic_load(&a_bytes) - bytes0 : -1;
  printf("{\"type\":\"end\",\"maxrss_kb\":%lld,\"footprint_peak_kb\":%lld,\"parses\":%lld,\"allocs\":%lld,"
         "\"alloc_bytes\":%lld,\"go_mallocs\":-1,\"go_alloc_bytes\":-1}\n",
         (long long)maxrss_kb(), (long long)footprint_peak_kb(),
         (long long)reps * nfiles, calls, abytes);
  fprintf(stderr, "checksum %llu\n", (unsigned long long)last.sum);
  return 0;
}
