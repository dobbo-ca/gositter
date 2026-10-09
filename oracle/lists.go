package main

// Builds scratch/lists/<grammar>.txt (real + broken) and <grammar>.real.txt
// (real only, for bench).

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// graphify's dispatch, internal/extract/extract.go. ".h" is decided by content.
var extGrammar = map[string]string{
	".go": "golang", ".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".tsx": "tsx", ".ts": "typescript", ".mts": "typescript", ".cts": "typescript",
	".tf": "hcl", ".tfvars": "hcl", ".hcl": "hcl", ".json": "json", ".py": "python", ".rs": "rust",
	".c": "c", ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp",
	".java": "java", ".cs": "csharp", ".rb": "ruby", ".rake": "ruby", ".php": "php", ".phtml": "php",
	".sh": "bash", ".bash": "bash", ".scala": "scala", ".sc": "scala", ".jl": "julia",
	".v": "verilog", ".sv": "verilog", ".svh": "verilog", ".vh": "verilog",
	".kt": "kotlin", ".kts": "kotlin", ".lua": "lua", ".zig": "zig",
}

// graphify's cppHeaderMarkers / isCppHeader.
var cppHeaderMarkers = [][]byte{
	[]byte("class "), []byte("namespace "), []byte("template"), []byte("::"),
	[]byte("public:"), []byte("private:"), []byte("protected:"),
}

func grammarOf(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".h" {
		return extGrammar[ext]
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	head := src[:min(len(src), 256*1024)]
	for _, m := range cppHeaderMarkers {
		if bytes.Contains(head, m) {
			return "cpp"
		}
	}
	return "c"
}

// collect maps grammar -> sorted files under root. With byDir, the path
// element at that depth below root names the grammar (zoo, adv); else the
// extension does.
func collect(into map[string][]string, root string, byDir int) {
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		var g string
		if byDir > 0 {
			rel, _ := filepath.Rel(root, p)
			parts := strings.Split(rel, string(filepath.Separator))
			if len(parts) > byDir {
				g = canon(parts[byDir-1])
			}
			if _, ok := cLangs[g]; !ok {
				return nil
			}
		} else {
			g = grammarOf(p)
		}
		if g != "" {
			into[g] = append(into[g], p)
		}
		return nil
	})
}

func listsMain(args []string) {
	fset := flag.NewFlagSet("lists", flag.ExitOnError)
	out := fset.String("out", "../scratch/lists", "output directory")
	max := fset.Int("max", 3000, "real files per grammar")
	refute := fset.String("refute", "/tmp/gfy-nocgo/refute", "dir with zoo, adv, advdeep, craft")
	corpora := fset.String("corpora", "/tmp/gfy-perf-out/corpora", "corpora dir")
	parseArgs(fset, args)

	zoo, corp, broken := map[string][]string{}, map[string][]string{}, map[string][]string{}
	collect(zoo, filepath.Join(*refute, "zoo"), 2) // zoo/{same,div}/<grammar>/
	collect(corp, *corpora, 0)
	collect(broken, filepath.Join(*refute, "adv"), 1) // adv/<grammar>/
	collect(broken, filepath.Join(*refute, "advdeep"), 0)
	collect(broken, filepath.Join(*refute, "craft"), 0)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		die("%v", err)
	}
	var names []string
	for g := range cLangs {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		// Every zoo file, then an even stride through the corpora.
		real := zoo[g]
		if room := *max - len(real); room > 0 {
			c := corp[g]
			if len(c) <= room {
				real = append(real, c...)
			} else {
				for i := 0; i < room; i++ {
					real = append(real, c[i*len(c)/room])
				}
			}
		} else {
			real = real[:*max]
		}
		name := g
		switch g { // the task's grammar names
		case "golang":
			name = "go"
		case "csharp":
			name = "c-sharp"
		}
		write := func(file string, lines []string) {
			if err := os.WriteFile(filepath.Join(*out, file), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				die("%v", err)
			}
		}
		write(name+".real.txt", real)
		write(name+".txt", append(append([]string{}, real...), broken[g]...))
		println(name, "real:", len(real), "(zoo", len(zoo[g]), "corpora", len(corp[g]), ") broken:", len(broken[g]))
	}
}
