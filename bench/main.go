// bench measures one side of the gositter vs C tree-sitter comparison.
// The side is a build tag: default is gositter (pure Go), -tags cgoside is
// the C runtime through go-tree-sitter v0.25.0. purec/cbench.c is the third
// side and does the same work without Go. Build with ./build.sh.
//
//	bench -g <grammar> -l <filelist> [-j N] [-what parse|walk|cursor] [-n reps] [-warm W] [-allocs]
//	bench -first -g <grammar> -f <file>
//	bench -errs -g <grammar> -l <filelist>
//
// Steady mode loads every file, makes one parser per worker, runs W untimed
// passes and n timed ones. First mode times the first two parses of a new
// process. Output is JSON lines on stdout, a walk checksum on stderr.
// -errs prints the paths whose root has_error is set, for splitreal.py.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// acc is what one worker sums over a pass.
type acc struct {
	nodes, errs int64
	sum         uint64 // per node: first byte of kind + start byte + end byte
}

// One parser and its last tree. Each side has one implementation.
type worker interface {
	// parse frees the previous tree, parses src, returns the root's has_error.
	parse(src []byte) bool
	walk(a *acc)   // recursive Child(i) walk of the last tree
	cursor(a *acc) // the same visit with a TreeCursor
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "bench: "+format+"\n", a...)
	os.Exit(2)
}

func cpuTimes() (user, sys int64) {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return ru.Utime.Nano(), ru.Stime.Nano()
}

func maxRSSKB() int64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	if runtime.GOOS == "linux" { // ru_maxrss keeps the peak of the image before exec
		return footprintPeakKB()
	}
	return int64(ru.Maxrss) / 1024 // darwin reports bytes
}

func main() {
	tMain := time.Now()
	grammar := flag.String("g", "", "grammar (gositter package name)")
	list := flag.String("l", "", "file list, one path per line")
	j := flag.Int("j", 1, "workers, one parser each")
	what := flag.String("what", "parse", "parse, walk or cursor")
	reps := flag.Int("n", 3, "timed passes")
	warm := flag.Int("warm", 1, "untimed warm-up passes")
	allocs := flag.Bool("allocs", false, "count the C runtime's allocations (cgo side)")
	first := flag.Bool("first", false, "first-parse mode")
	file := flag.String("f", "", "file for -first")
	errs := flag.Bool("errs", false, "print the listed paths whose root has an error, no timing")
	flag.Parse()

	if *first {
		firstMain(tMain, *grammar, *file)
		return
	}
	if *what != "parse" && *what != "walk" && *what != "cursor" {
		die("-what must be parse, walk or cursor")
	}
	data, err := os.ReadFile(*list)
	if err != nil {
		die("%v", err)
	}
	var srcs [][]byte
	var paths []string
	var total int
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		src, err := os.ReadFile(l)
		if err != nil {
			die("%v", err)
		}
		srcs = append(srcs, src)
		paths = append(paths, l)
		total += len(src)
	}
	if *errs {
		w, err := newWorker(*grammar)
		if err != nil {
			die("%v", err)
		}
		for i, src := range srcs {
			if w.parse(src) {
				fmt.Println(paths[i])
			}
		}
		return
	}
	if *allocs {
		countCAllocs()
	}
	ws := make([]worker, *j)
	for i := range ws {
		if ws[i], err = newWorker(*grammar); err != nil {
			die("%v", err)
		}
	}
	fmt.Printf(`{"type":"meta","side":%q,"grammar":%q,"what":%q,"j":%d,"files":%d,"bytes":%d,"rss_loaded_kb":%d}`+"\n",
		sideName, *grammar, *what, *j, len(srcs), total, residentKB())

	pass := func() acc {
		var tot acc
		var next atomic.Int64
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, w := range ws {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var a acc
				for {
					i := int(next.Add(1)) - 1
					if i >= len(srcs) {
						break
					}
					if w.parse(srcs[i]) {
						a.errs++
					}
					switch *what {
					case "walk":
						w.walk(&a)
					case "cursor":
						w.cursor(&a)
					}
				}
				mu.Lock()
				tot.nodes += a.nodes
				tot.errs += a.errs
				tot.sum += a.sum
				mu.Unlock()
			}()
		}
		wg.Wait()
		return tot
	}

	for i := 0; i < *warm; i++ {
		pass()
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	ca0, cb0 := cAllocCounts()
	var last acc
	for r := 0; r < *reps; r++ {
		u0, s0 := cpuTimes()
		i0, c0 := counters()
		t0 := time.Now()
		last = pass()
		d := time.Since(t0)
		i1, c1 := counters()
		u1, s1 := cpuTimes()
		fmt.Printf(`{"type":"rep","rep":%d,"wall_ns":%d,"user_ns":%d,"sys_ns":%d,"instr":%d,"cycles":%d,"nodes":%d,"error_files":%d}`+"\n",
			r, d.Nanoseconds(), u1-u0, s1-s0, i1-i0, c1-c0, last.nodes, last.errs)
	}
	ca1, cb1 := cAllocCounts()
	runtime.ReadMemStats(&m1)
	calls, bytes := ca1-ca0, cb1-cb0
	if ca0 < 0 { // not counted
		calls, bytes = -1, -1
	}
	fmt.Printf(`{"type":"end","maxrss_kb":%d,"footprint_peak_kb":%d,"parses":%d,"allocs":%d,"alloc_bytes":%d,"go_mallocs":%d,"go_alloc_bytes":%d}`+"\n",
		maxRSSKB(), footprintPeakKB(), *reps*len(srcs), calls, bytes, m1.Mallocs-m0.Mallocs, m1.TotalAlloc-m0.TotalAlloc)
	fmt.Fprintf(os.Stderr, "checksum %d\n", last.sum)
}

func firstMain(tMain time.Time, grammar, file string) {
	src, err := os.ReadFile(file)
	if err != nil {
		die("%v", err)
	}
	t0 := time.Now()
	w, err := newWorker(grammar)
	if err != nil {
		die("%v", err)
	}
	t1 := time.Now()
	w.parse(src)
	t2 := time.Now()
	w.parse(src)
	t3 := time.Now()
	instr, cycles := counters()
	user, sys := cpuTimes()
	fmt.Printf(`{"type":"first","side":%q,"grammar":%q,"file_bytes":%d,"start_to_main_ns":%d,"init_ns":%d,"parse1_ns":%d,"parse2_ns":%d,"instr":%d,"cycles":%d,"user_ns":%d,"sys_ns":%d,"maxrss_kb":%d,"footprint_peak_kb":%d}`+"\n",
		sideName, grammar, len(src), startToMainNS(tMain), t1.Sub(t0).Nanoseconds(), t2.Sub(t1).Nanoseconds(), t3.Sub(t2).Nanoseconds(),
		instr, cycles, user, sys, maxRSSKB(), footprintPeakKB())
}
