package gositter_test

import (
	"testing"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/bash"
	"github.com/dobbo-ca/gositter/grammars/c"
	"github.com/dobbo-ca/gositter/grammars/cpp"
	"github.com/dobbo-ca/gositter/grammars/csharp"
	"github.com/dobbo-ca/gositter/grammars/golang"
	"github.com/dobbo-ca/gositter/grammars/hcl"
	"github.com/dobbo-ca/gositter/grammars/java"
	"github.com/dobbo-ca/gositter/grammars/javascript"
	"github.com/dobbo-ca/gositter/grammars/json"
	"github.com/dobbo-ca/gositter/grammars/julia"
	"github.com/dobbo-ca/gositter/grammars/kotlin"
	"github.com/dobbo-ca/gositter/grammars/lua"
	"github.com/dobbo-ca/gositter/grammars/php"
	"github.com/dobbo-ca/gositter/grammars/python"
	"github.com/dobbo-ca/gositter/grammars/ruby"
	"github.com/dobbo-ca/gositter/grammars/rust"
	"github.com/dobbo-ca/gositter/grammars/scala"
	"github.com/dobbo-ca/gositter/grammars/tsx"
	"github.com/dobbo-ca/gositter/grammars/typescript"
	"github.com/dobbo-ca/gositter/grammars/verilog"
	"github.com/dobbo-ca/gositter/grammars/zig"
)

// The dense small parse table must answer every lookup as the C scan does,
// for all 21 grammars. verilog is over the size cap and keeps the scan.
func TestDenseLookup(t *testing.T) {
	langs := map[string]func() *gositter.Language{
		"bash": bash.Language, "c": c.Language, "cpp": cpp.Language, "csharp": csharp.Language,
		"golang": golang.Language, "hcl": hcl.Language, "java": java.Language,
		"javascript": javascript.Language, "json": json.Language, "julia": julia.Language,
		"kotlin": kotlin.Language, "lua": lua.Language, "php": php.Language, "python": python.Language,
		"ruby": ruby.Language, "rust": rust.Language, "scala": scala.Language, "tsx": tsx.Language,
		"typescript": typescript.Language, "verilog": verilog.Language, "zig": zig.Language,
	}
	for name, lang := range langs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state, symbol, built, ok := lang().DenseMismatch()
			if !ok {
				t.Fatalf("state %d symbol %d: dense lookup differs from the scan", state, symbol)
			}
			if built != (name != "verilog") {
				t.Errorf("dense table built = %v", built)
			}
		})
	}
}
