// oracle compares gositter with the C tree-sitter runtime (go-tree-sitter
// v0.25.0 over cgo). Build with ./build.sh.
//
//	oracle dump-c|dump-go <grammar> <file>
//	oracle log-c|log-go   <grammar> <file>
//	oracle cmp   <grammar> <dir-or-filelist>... [-j N] [-show N] [-a c] [-b go] [-shuffle seed]
//	oracle bench <grammar> <filelist> [-j N] [-what parse|walk] [-n reps] [-sides c,go]
//	oracle lists [-out dir] [-max N]
//
// Grammar names are the gositter package names; "go" and "c-sharp" also work.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dobbo-ca/gositter/internal/treedump"
)

type parser interface {
	// parse returns the root. It is valid until the next parse or close.
	parse(src []byte) treedump.Node
	// fields lists the language's field names, id order.
	fields() []string
	setLogger(fn func(lex bool, msg string))
	// walk visits the last tree natively and returns the node count.
	walk() int
	close()
}

// bench -what children: the gositter side walks with Node.Children.
var walkChildren bool

type side struct {
	name string
	open func(grammar string) (parser, error)
}

var sides = map[string]side{
	"c":  {"c", openC},
	"go": {"go", openGo},
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "oracle: "+format+"\n", a...)
	os.Exit(2)
}

func canon(grammar string) string {
	switch grammar {
	case "go":
		return "golang"
	case "c-sharp":
		return "csharp"
	}
	return grammar
}

func mustOpen(sideName, grammar string) parser {
	s, ok := sides[sideName]
	if !ok {
		die("unknown side %q (c, go)", sideName)
	}
	p, err := s.open(canon(grammar))
	if err != nil {
		die("%s side: %v", sideName, err)
	}
	return p
}

// parseArgs lets flags follow positional arguments.
func parseArgs(fset *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		fset.Parse(args)
		if fset.NArg() == 0 {
			return pos
		}
		pos = append(pos, fset.Arg(0))
		args = fset.Args()[1:]
	}
}

func main() {
	if len(os.Args) < 2 {
		die("usage: oracle dump-c|dump-go|log-c|log-go|cmp|bench|lists ...")
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "dump-c", "dump-go", "log-c", "log-go":
		if len(args) != 2 {
			die("usage: oracle %s <grammar> <file>", cmd)
		}
		src, err := os.ReadFile(args[1])
		if err != nil {
			die("%v", err)
		}
		verb, sideName, _ := strings.Cut(cmd, "-")
		p := mustOpen(sideName, args[0])
		if verb == "log" {
			out := bufio.NewWriter(os.Stdout)
			p.setLogger(func(lex bool, msg string) {
				if lex {
					out.WriteString("L ")
				} else {
					out.WriteString("P ")
				}
				out.WriteString(msg)
				out.WriteByte('\n')
			})
			defer out.Flush() // also on a panic in the port: the log up to it is the point
			p.parse(src)
			return
		}
		treedump.Dump(os.Stdout, p.parse(src), p.fields(), src)
	case "cmp":
		os.Exit(cmpMain(args))
	case "bench":
		benchMain(args)
	case "lists":
		listsMain(args)
	default:
		die("unknown command %q", cmd)
	}
}

// readList returns the files of a filelist, or of a directory filtered by the
// grammar's extensions.
func readList(grammar, path string) []string {
	st, err := os.Stat(path)
	if err != nil {
		die("%v", err)
	}
	var files []string
	if st.IsDir() {
		filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() && extGrammar[strings.ToLower(filepath.Ext(p))] == canon(grammar) {
				files = append(files, p)
			}
			return nil
		})
		return files
	}
	data, err := os.ReadFile(path)
	if err != nil {
		die("%v", err)
	}
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files
}

type cmpResult struct {
	nodes    int
	hasError bool
	noSexp   bool
	diff     string // empty when equal
}

// safeDump dumps src through p. A panic becomes a PANIC line, and the caller
// must replace the parser.
func safeDump(buf *bytes.Buffer, p parser, src []byte) (nodes int, hasError, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(buf, "\nPANIC: %v\n", r)
			panicked = true
		}
	}()
	root := p.parse(src)
	hasError = root.HasError()
	nodes = treedump.Dump(buf, root, p.fields(), src)
	return
}

func firstDiff(a, b []byte) string {
	clip := func(l []byte) string {
		if len(l) > 600 {
			return string(l[:600]) + "..."
		}
		return string(l)
	}
	for line := 1; ; line++ {
		la, ra, okA := bytes.Cut(a, []byte("\n"))
		lb, rb, okB := bytes.Cut(b, []byte("\n"))
		if !bytes.Equal(la, lb) || okA != okB {
			return fmt.Sprintf("line %d\n    a: %s\n    b: %s", line, clip(la), clip(lb))
		}
		if !okA {
			return "no differing line found"
		}
		a, b = ra, rb
	}
}

func cmpMain(args []string) int {
	fset := flag.NewFlagSet("cmp", flag.ExitOnError)
	j := fset.Int("j", runtime.NumCPU(), "workers")
	show := fset.Int("show", 10, "mismatches to print")
	an := fset.String("a", "c", "reference side")
	bn := fset.String("b", "go", "side under test")
	shuffle := fset.Int64("shuffle", 0, "non-zero: random file order with this seed (-j 1: one reused parser sees them all)")
	pos := parseArgs(fset, args)
	if len(pos) < 2 {
		die("usage: oracle cmp <grammar> <dir-or-filelist>... [-j N] [-show N] [-shuffle seed]")
	}
	grammar := pos[0]
	var files []string
	for _, l := range pos[1:] {
		files = append(files, readList(grammar, l)...)
	}
	if *shuffle != 0 {
		rand.New(rand.NewSource(*shuffle)).Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	}
	mustOpen(*an, grammar).close() // fail before the workers start
	mustOpen(*bn, grammar).close()

	res := make([]cmpResult, len(files))
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < *j; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pa, pb := mustOpen(*an, grammar), mustOpen(*bn, grammar)
			var ba, bb bytes.Buffer
			for {
				i := int(next.Add(1)) - 1
				if i >= len(files) {
					return
				}
				src, err := os.ReadFile(files[i])
				if err != nil {
					res[i].diff = "read: " + err.Error()
					continue
				}
				// A goroutine can't be killed; at least name the file that hangs.
				// The 5 MB inputs take about 70s for both sides at full load.
				slow := time.AfterFunc(5*time.Minute, func() { fmt.Fprintln(os.Stderr, "SLOW (>5m):", files[i]) })
				ba.Reset()
				bb.Reset()
				var pan bool
				if res[i].nodes, res[i].hasError, pan = safeDump(&ba, pa, src); pan {
					pa = mustOpen(*an, grammar)
				}
				if _, _, pan = safeDump(&bb, pb, src); pan {
					pb = mustOpen(*bn, grammar)
				}
				slow.Stop()
				res[i].noSexp = bytes.HasSuffix(ba.Bytes(), []byte(treedump.SexpSkipped))
				if !bytes.Equal(ba.Bytes(), bb.Bytes()) {
					res[i].diff = firstDiff(ba.Bytes(), bb.Bytes())
				}
			}
		}()
	}
	wg.Wait()

	var nodes, withErr, bad, noSexp int
	for i, r := range res {
		nodes += r.nodes
		if r.hasError {
			withErr++
		}
		if r.noSexp {
			noSexp++
		}
		if r.diff != "" {
			if bad < *show {
				fmt.Printf("MISMATCH %s\n  %s\n", files[i], r.diff)
			}
			bad++
		}
	}
	fmt.Printf("%s: a=%s b=%s files=%d nodes=%d trees_with_errors=%d sexp_skipped=%d mismatching_files=%d\n",
		grammar, *an, *bn, len(files), nodes, withErr, noSexp, bad)
	if bad > 0 {
		return 1
	}
	return 0
}

func benchMain(args []string) {
	fset := flag.NewFlagSet("bench", flag.ExitOnError)
	j := fset.Int("j", 1, "goroutines, one parser each")
	what := fset.String("what", "parse", "parse, walk (parse plus a Child(i) walk of every node), or children (walk, with gositter using Node.Children)")
	reps := fset.Int("n", 5, "timed repetitions after one warm-up; best and median are reported")
	sideList := fset.String("sides", "c,go", "sides to time; repetitions alternate between them")
	cpuprofile := fset.String("cpuprofile", "", "write a CPU profile of the timed repetitions")
	memprofile := fset.String("memprofile", "", "write an allocation profile at the end")
	pos := parseArgs(fset, args)
	if *what == "children" {
		walkChildren = true
		*what = "walk"
	}
	if len(pos) != 2 || (*what != "parse" && *what != "walk") {
		die("usage: oracle bench <grammar> <filelist> [-j N] [-what parse|walk] [-n reps]")
	}
	grammar := pos[0]
	var srcs [][]byte
	var total int
	for _, f := range readList(grammar, pos[1]) {
		src, err := os.ReadFile(f)
		if err != nil {
			die("%v", err)
		}
		srcs = append(srcs, src)
		total += len(src)
	}
	fmt.Printf("%s: files=%d bytes=%d j=%d what=%s reps=%d\n", grammar, len(srcs), total, *j, *what, *reps)

	names := strings.Split(*sideList, ",")
	parsers := map[string][]parser{}
	for _, name := range names {
		ps := make([]parser, *j)
		for i := range ps {
			ps[i] = mustOpen(name, grammar)
		}
		parsers[name] = ps
	}

	var nodes atomic.Int64
	type sample struct {
		d             time.Duration
		instr, cycles uint64
	}
	run := func(name string) (sample, uint64) {
		nodes.Store(0)
		var next atomic.Int64
		var wg sync.WaitGroup
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		i0, c0 := counters()
		t0 := time.Now()
		for _, p := range parsers[name] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				n := 0
				for {
					i := int(next.Add(1)) - 1
					if i >= len(srcs) {
						break
					}
					p.parse(srcs[i])
					if *what == "walk" {
						n += p.walk()
					}
				}
				nodes.Add(int64(n))
			}()
		}
		wg.Wait()
		d := time.Since(t0)
		i1, c1 := counters()
		runtime.ReadMemStats(&m1)
		return sample{d, i1 - i0, c1 - c0}, m1.Mallocs - m0.Mallocs
	}

	// Warm-up: page in the tables, grow the slabs, settle the C allocator.
	for _, name := range names {
		run(name)
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			die("%v", err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	samples := map[string][]sample{}
	mallocs := map[string]uint64{}
	for r := 0; r < *reps; r++ {
		for _, name := range names {
			sm, m := run(name)
			samples[name] = append(samples[name], sm)
			mallocs[name] = m
		}
	}
	// best and median of one measure over a side's repetitions
	stat := func(name string, f func(sample) float64) (best, median float64) {
		var v []float64
		for _, sm := range samples[name] {
			v = append(v, f(sm))
		}
		sort.Float64s(v)
		return v[0], v[len(v)/2]
	}
	secs := func(sm sample) float64 { return sm.d.Seconds() }
	instr := func(sm sample) float64 { return float64(sm.instr) }
	cyc := func(sm sample) float64 { return float64(sm.cycles) }
	for _, name := range names {
		best, median := stat(name, secs)
		bi, _ := stat(name, instr)
		bc, mc := stat(name, cyc)
		fmt.Printf("  %-3s best=%.4fs median=%.4fs %7.1f MB/s  instr=%.3fG cycles=%.3fG (median %.3fG)  go_mallocs/file=%.1f",
			name, best, median, float64(total)/1e6/best, bi/1e9, bc/1e9, mc/1e9,
			float64(mallocs[name])/float64(len(srcs)))
		if *what == "walk" {
			fmt.Printf("  nodes=%d", nodes.Load())
		}
		fmt.Println()
	}
	if len(samples["c"]) > 0 && len(samples["go"]) > 0 {
		ratio := func(f func(sample) float64) (float64, float64) {
			gb, gm := stat("go", f)
			cb, cm := stat("c", f)
			return gb / cb, gm / cm
		}
		tb, tm := ratio(secs)
		ib, _ := ratio(instr)
		cb, cm := ratio(cyc)
		fmt.Printf("  go/c ratio: time %.3f (best) %.3f (median), instructions %.3f, cycles %.3f (best) %.3f (median)\n",
			tb, tm, ib, cb, cm)
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			die("%v", err)
		}
		pprof.Lookup("allocs").WriteTo(f, 0)
		f.Close()
	}
	for _, name := range names {
		for _, p := range parsers[name] {
			p.close()
		}
	}
}
