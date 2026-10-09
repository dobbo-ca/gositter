package main

import (
	"unsafe"

	hcl "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go"
	kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	lua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	zig "github.com/tree-sitter-grammars/tree-sitter-zig/bindings/go"
	bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	julia "github.com/tree-sitter/tree-sitter-julia/bindings/go"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	verilog "github.com/tree-sitter/tree-sitter-verilog/bindings/go"
)

// The C grammars, compiled by cgo from the same parser.c files. Keys are the
// gositter package names; cName is the <name> of tree_sitter_<name>.
var cLangs = map[string]struct {
	lang  func() unsafe.Pointer
	cName string
}{
	"bash": {bash.Language, "bash"}, "c": {c.Language, "c"}, "cpp": {cpp.Language, "cpp"},
	"csharp": {csharp.Language, "c_sharp"}, "golang": {golang.Language, "go"},
	"hcl": {hcl.Language, "hcl"}, "java": {java.Language, "java"},
	"javascript": {javascript.Language, "javascript"}, "json": {json.Language, "json"},
	"julia": {julia.Language, "julia"}, "kotlin": {kotlin.Language, "kotlin"},
	"lua": {lua.Language, "lua"}, "php": {php.LanguagePHP, "php"},
	"python": {python.Language, "python"}, "ruby": {ruby.Language, "ruby"},
	"rust": {rust.Language, "rust"}, "scala": {scala.Language, "scala"},
	"typescript": {typescript.LanguageTypescript, "typescript"},
	"tsx":        {typescript.LanguageTSX, "tsx"}, "verilog": {verilog.Language, "verilog"},
	"zig": {zig.Language, "zig"},
}
