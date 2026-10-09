# gositter contract

Frozen interface between the runtime, `cmd/gengrammar` and the hand-written
scanners. The compilable form is `api.go`. To change either file, append a
dated entry to `scratch/CONTRACT_CHANGES.md` and make the smallest change.

Source of truth for behaviour is tree-sitter v0.25.0:
`$GOMODCACHE/github.com/tree-sitter/go-tree-sitter@v0.25.0/src`.

## 1. Scope

In: `ts_parser_parse` with `old_tree == NULL`, one contiguous UTF-8 `[]byte`,
the single default included range, error recovery, nodes, cursors, logging.

Out: queries, wasm, incremental parsing and `ts_tree_edit`, changed ranges,
user included ranges, UTF-16, cancellation, timeouts, progress callbacks.

Code that only exists for the "out" list is not ported. Code on a shared path
is ported as written, even when one branch can't fire here (for example the
included-range loop in `ts_lexer__do_advance`), unless the runtime agent shows
it is dead and says so in a comment.

## 2. Types and names

| C | Go |
|---|---|
| `TSSymbol`, `TSStateId`, `TSFieldId` | `Symbol`, `StateID`, `FieldID` (all `uint16`) |
| `ts_builtin_sym_end`, `_error`, `_error_repeat` | `SymbolEnd` 0, `SymbolError` 0xFFFF, `SymbolErrorRepeat` 0xFFFE |
| `TSPoint` | `Point{Row, Column uint32}` |
| `TSFieldMapEntry` | `FieldMapEntry{FieldID, ChildIndex uint8, Inherited bool}` |
| `TSMapSlice` (ABI 14 `TSFieldMapSlice`) | `MapSlice{Index, Length uint16}` |
| `TSSymbolMetadata` | `SymbolMetadata{Visible, Named, Supertype bool}` |
| `TSLexerMode` (ABI 14 `TSLexMode`) | `LexMode{LexState, ExternalLexState, ReservedWordSetID uint16}` |
| `TSCharacterRange` | `CharacterRange{Start, End int32}` |
| `TSParseActionEntry`, `TSParseAction` | `ParseAction` (section 4) |
| `TSLanguage` | `Language` (section 3) |
| `TSLexer` + `Lexer` | `Lexer` (section 5) |
| `TREE_SITTER_SERIALIZATION_BUFFER_SIZE` | `SerializationBufferSize` = 1024 |

Naming rules for ported code:

- One Go file per C file, same basename. Header-only C (`length.h`, `point.h`,
  `reduce_action.h`, `reusable_node.h`, `language.h`, `subtree.h`) goes in the
  Go file of the matching `.c`, or its own file with the header's basename.
- Above every ported function, a comment with the exact C name:
  `// ts_parser__lex`.
- Go name: drop the `ts_<type>_` prefix, make the rest lowerCamel, hang it on
  the Go type for that C type. `ts_parser__do_all_potential_reductions` is
  `(*Parser).doAllPotentialReductions`. Free helpers keep the type word:
  `ts_subtree_new_leaf` is `subtreeNewLeaf` or a method, runtime's choice.
- Initialisms: `ID`, `EOF`, `ABI`.
- Local variable names follow C where it helps a side by side diff.

## 3. Language

`Language` mirrors `TSLanguage` field for field, in C order. Pointers become
slices. Two-dimensional C arrays are flattened row-major and indexed the way
the C runtime indexes them.

| Field | C | Shape |
|---|---|---|
| `ABIVersion` | `abi_version` (ABI 14: `version`) | value of `LANGUAGE_VERSION` |
| `SymbolCount` ... `FieldCount`, `MaxAliasSequenceLength` | same names | counts |
| `ParseTable []uint16` | `parse_table` | `[LargeStateCount][SymbolCount]` |
| `SmallParseTable []uint16` | `small_parse_table` | as in C |
| `SmallParseTableMap []uint32` | `small_parse_table_map` | index by `state - LargeStateCount` |
| `ParseActions []ParseAction` | `parse_actions` | section 4 |
| `SymbolNames []string` | `symbol_names` | `SymbolCount + AliasCount` entries |
| `FieldNames []string` | `field_names` | `FieldCount + 1` entries, `[0] == ""` |
| `FieldMapSlices`, `FieldMapEntries` | same | index by production id |
| `SymbolMetadata`, `PublicSymbolMap` | same | `SymbolCount + AliasCount` entries |
| `AliasMap []uint16` | `alias_map` | keep the trailing 0 |
| `AliasSequences []Symbol` | `alias_sequences` | `[ProductionIDCount][MaxAliasSequenceLength]` |
| `LexModes []LexMode` | `lex_modes` | `StateCount` entries |
| `LexFn`, `KeywordLexFn LexFunc` | `lex_fn`, `keyword_lex_fn` | `KeywordLexFn` nil if absent |
| `KeywordCaptureToken` | same | 0 if absent |
| `ExternalScanner.States []bool` | `external_scanner.states` | `[n][ExternalTokenCount]`, row 0 unused |
| `ExternalScanner.SymbolMap []Symbol` | `external_scanner.symbol_map` | `ExternalTokenCount` entries |
| `ExternalScanner.New` | `create` | nil if the grammar has no scanner |
| `PrimaryStateIDs []StateID` | `primary_state_ids` | `StateCount` entries |
| `Name` | `name` | see below |
| `ReservedWords []Symbol` | `reserved_words` | `[sets][MaxReservedWordSetSize]`, nil if absent |
| `MaxReservedWordSetSize` | same | 0 if absent |
| `SupertypeCount`, `SupertypeSymbols`, `SupertypeMapSlices`, `SupertypeMapEntries` | same | zero/nil if absent |
| `Metadata` | `metadata` | zero if absent |

Rules for the generator:

- C designated initialisers leave gaps zero. Keep every gap, so indexes match.
  A table declared with a dimension gets exactly that many entries.
- `(uint16_t)-1` in `lex_modes` is `0xFFFF`.
- `Name` is always the `<name>` of `tree_sitter_<name>()`, also for ABI 14
  grammars where C has NULL. Nothing in a tree depends on it.
- A missing C field stays zero or nil. The runtime tests `ExternalScanner.New
  != nil` where C tests `external_scanner.create`, and `KeywordLexFn != nil`
  where C tests `keyword_lex_fn`.
- 9 of the 21 grammars are ABI 14 (cpp, java, json, kotlin, ruby, typescript,
  tsx, verilog, zig); the rest are ABI 15. The generator reads both dialects
  (`.version`, `TSLexMode`, `TSFieldMapSlice`). The runtime keeps every
  `abi_version` test from `language.c` (`ts_language_is_reserved_word`,
  `ts_language_lex_mode_for_state`, supertypes, name, metadata).
- A `Language` is immutable and shared by all parsers and goroutines.
  `Language()` returns the same pointer on every call. The one exception is
  internal: the first `SetLanguage` builds derived lookup tables under a
  `sync.Once`. Do not copy a `Language` value.

## 4. ParseAction encoding

`TSParseActionEntry` and `TSParseAction` are 8-byte C unions. Go uses one
8-byte pointer-free struct for every slot of `ParseActions` and keeps the C
byte layout (little-endian), so nothing needs `unsafe`:

```go
type ParseAction struct {
	Type              uint8
	ChildCount        uint8
	Symbol            Symbol
	DynamicPrecedence int16
	ProductionID      uint16
}
```

Field names are those of the reduce variant. The other variants reuse them:

| C initialiser | Go literal `{Type, ChildCount, Symbol, DynamicPrecedence, ProductionID}` |
|---|---|
| `{.entry = {.count = n, .reusable = r}}` | `{n, r ? 1 : 0, 0, 0, 0}` |
| `SHIFT(s)` | `{0, 0, s, 0, 0}` |
| `SHIFT_REPEAT(s)` | `{0, 0, s, 2, 0}` |
| `SHIFT_EXTRA()` | `{0, 0, 0, 1, 0}` |
| `REDUCE(sym, n, prec, prod)` | `{1, n, sym, prec, prod}` |
| `ACCEPT_INPUT()` | `{2, 0, 0, 0, 0}` |
| `RECOVER()` | `{3, 0, 0, 0, 0}` |

Readers: `Type` is the action type (`ParseActionTypeShift` 0, `Reduce` 1,
`Accept` 2, `Recover` 3). Header slot: `Count()`, `Reusable()`. Shift slot:
`State()`, `Extra()`, `Repetition()`. Reduce slot: the fields. All five
methods inline (checked with `-gcflags=-m`).

`ParseActions[0]` is the C `[0] = {.entry = {.count = 0, .reusable = false}}`.
The table value from `ts_language_lookup` indexes a header slot; its actions
are the next `Count()` slots. `ts_language_table_entry` therefore returns
`ParseActions[i+1 : i+1+count]`, a subslice of static data, no allocation.
The generator emits positional literals so the table is static data and costs
nothing at init.

## 5. Lexer, as seen by lex functions and scanners

`Lexer` is a concrete struct. Its methods are direct calls, so they can
inline. There is no interface and no function-pointer field.

| C | Go |
|---|---|
| `lexer->lookahead` | `l.Lookahead` (`int32`) |
| `lexer->result_symbol` | `l.ResultSymbol` (`Symbol`) |
| `lexer->advance(lexer, skip)` | `l.Advance(skip)` |
| `lexer->mark_end(lexer)` | `l.MarkEnd()` |
| `lexer->get_column(lexer)` | `l.GetColumn() uint32` |
| `lexer->is_at_included_range_start(lexer)` | `l.IsAtIncludedRangeStart() bool` |
| `lexer->eof(lexer)` | `l.EOF() bool` |
| `lexer->log(...)` | not ported; no scanner in the 21 grammars calls it |
| none | `l.AdvanceRun(set, skip)`, generated lex functions only (section 6) |

Semantics the runtime must keep, taken from `lexer.c`:

- `Lookahead` is 0 at end of input and also for a real NUL byte. Only `EOF()`
  tells them apart.
- Invalid UTF-8 gives `Lookahead == -1` (`TS_DECODE_ERROR`) and a width of one
  byte. C decodes with ICU `U8_NEXT`. `utf8.DecodeRune` rejects the same
  sequences as far as I know (surrogates, overlongs, above U+10FFFF, truncated
  tails); the runtime agent confirms that against `/tmp/gfy-nocgo/refute/adv`
  and does not take it from this file. A valid U+FFFD is 3 bytes and is not an
  error.
- `Advance` after end of input is a no-op: it does not move and does not log
  (`if (!self->chunk) return`).
- `Advance` logs `consume character:...` or `skip character:...` with
  `LogTypeLex` when a logger is set, in the two formats of the C `LOG` macro
  (`'%c'` for 32..126, else `%d`).
- A leading BOM is skipped in `ts_lexer_start`, not by grammars.
- `GetColumn` counts code points since line start and can re-scan the line.
  bash and scala use it. `IsAtIncludedRangeStart` is used by javascript and
  ruby; with the default range it is true only at byte 0 of a non-empty input.

Everything else in `Lexer` is private to the runtime (`lexerState` in the
stub). Generated code and scanners touch only the names in the table.

## 6. Generated lex function

```go
type LexFunc func(l *Lexer, state StateID) bool
```

`ts_lex` becomes `lex`, `ts_lex_keywords` becomes `lexKeywords`, both in
`lex.go`. The runtime calls them through `Language.LexFn`, one indirect call
per token, same as C.

The body is a statement-for-statement translation. Statement order inside a
`case` is the C order, because the first matching `if` wins.

```go
func lex(l *gositter.Lexer, state gositter.StateID) bool {
	// START_LEXER
	result := false
	skip := false
	eof := false
	var lookahead int32
	goto start
next_state:
	l.Advance(skip)
start:
	skip = false
	lookahead = l.Lookahead
	eof = l.EOF() // the C statement after START_LEXER(), inside the loop
	_, _ = eof, lookahead
	switch state {
	case 0:
		if eof {
			state = 21 // ADVANCE(21)
			goto next_state
		}
		switch lookahead { // ADVANCE_MAP
		case '"':
			state = 28
			goto next_state
		case ',':
			state = 23
			goto next_state
		}
		if ('\t' <= lookahead && lookahead <= '\r') || lookahead == ' ' {
			skip = true // SKIP(20)
			state = 20
			goto next_state
		}
		if gositter.SetContainsASCII(&sym_identifier_character_set_1_ascii, sym_identifier_character_set_1[:], lookahead) {
			state = 147
			goto next_state
		}
		return result // END_STATE
	case 21:
		result = true // ACCEPT_TOKEN(ts_builtin_sym_end)
		l.ResultSymbol = ts_builtin_sym_end
		l.MarkEnd()
		return result
	default:
		return false
	}
}
```

| C macro | Go |
|---|---|
| `START_LEXER()` plus `eof = lexer->eof(lexer);` | the prologue above. All variables are declared before the first label, which Go's `goto` rules need. |
| `ADVANCE(n)` | `state = n; goto next_state` |
| `SKIP(n)` | `skip = true; state = n; goto next_state` |
| `ACCEPT_TOKEN(s)` | `result = true; l.ResultSymbol = s; l.MarkEnd()` |
| `END_STATE()` | `return result` |
| `ADVANCE_MAP(c1, s1, ...)` | `switch lookahead { case c1: state = s1; goto next_state ... }` with no default, then fall through to the next statement |
| `set_contains(tbl, n, lookahead)` | `gositter.SetContainsASCII(&tbl_ascii, tbl[:], lookahead)` |
| `default: return false;` | same |

Details:

- `ADVANCE_MAP` in C is a linear scan of a `uint16_t` table. A Go `switch` is
  equal as long as keys are unique and at most 0xFFFF. Duplicate keys fail to
  compile in Go. The generator must fail if a key is above 0xFFFF, because C
  would truncate it.
- Character sets: `static const TSCharacterRange name[] = {...}` becomes a
  package-level `var name = [...]gositter.CharacterRange{...}` in `lex.go`,
  same name. The generator checks that the `n` passed to `set_contains`
  equals the table length, then drops it. `SetContains` is the C binary
  search, ported line for line. Next to each set the generator emits
  `var name_ascii = [128]bool{...}`, entry `c` holding what that search
  answers for code point `c`; `SetContainsASCII` reads it and calls
  `SetContains` for everything from 128 up and for -1.
- Conditions are copied as C wrote them, including `!eof &&` guards,
  `lookahead != 0`, open ranges and hex code points. `lookahead` is `int32`
  and can be -1.
- C character literals that are not valid Go rune literals are emitted as
  numbers: `'\0'`, `'\"'`, `'\?'`, `'\e'`, octal with fewer than 3 digits.
  The generator may emit every literal as a number if that is simpler.
- Self-loops. For each state the generator finds the ASCII characters whose
  first matching statement is `ADVANCE(self)` (or `SKIP(self)`), by
  evaluating the state's conditions for code points 1 to 127 with `eof`
  false (`cmd/gengrammar/run.go`). If there are any, the case starts with

  ```go
  if uint32(lookahead) < 128 && lexRunN[lookahead] {
  	l.AdvanceRun(&lexRunN, false) // true for a SKIP run
  	lookahead = l.Lookahead
  	eof = l.EOF()
  }
  ```

  and the statements follow unchanged. `AdvanceRun(set, skip)` is defined as
  `Advance(skip)` repeated while `Lookahead` is an ASCII character in `set`,
  so the state's statements run on the first character after the run, as
  they would in C after the last trip through the loop. `lexRunN` is a
  `[256]bool` with entries 0 and 128 to 255 false; equal sets share a table.
- If a function never uses `skip`, keep the prologue anyway. The `_, _ =`
  line is there so unused `eof` or `lookahead` still compiles.
- The generated code holds no state between calls and never allocates.

## 7. External scanners

```go
type ExternalScanner interface {
	Scan(l *Lexer, validSymbols []bool) bool
	Serialize(buf []byte) int
	Deserialize(buf []byte)
}
```

Each grammar with a `scanner.c` has a hand-written `scanner.go` with
`func newScanner() gositter.ExternalScanner`; the generator wires it into
`ExternalScanner.New`.

| C | Go |
|---|---|
| `create()` | `New()`, called where C calls `ts_parser__external_scanner_create` |
| `destroy(payload)` | nothing; the runtime drops the reference |
| `scan(payload, lexer, valid_symbols)` | `Scan(l, validSymbols)` |
| `serialize(payload, buffer)` | `Serialize(buf)`, `len(buf) == 1024`, returns the length |
| `deserialize(payload, buffer, length)` | `Deserialize(buf)`, `len(buf)` is the length |

- `validSymbols` is `States[n*id : n*(id+1)]` with `n = ExternalTokenCount`,
  the same row C points at. Read-only. The runtime only calls `Scan` when the
  external lex state is non-zero, as C does.
- `Scan` sets `l.ResultSymbol` to the external token index (the scanner's own
  enum). The runtime maps it through `SymbolMap`.
- `Deserialize` with an empty slice is the C `(NULL, 0)` reset. The slice is
  only valid during the call; copy what you keep.
- The serialized bytes must be byte-identical to C. The runtime compares them
  with `memcmp` semantics (`ts_external_scanner_state_eq`) and that decides
  stack merges and token reuse, so a different encoding changes the tree.
- A scanner keeps its arrays as slices that it truncates and refills. No
  allocation per `Scan` once warm.
- C `char` is signed on darwin/arm64 and x86 and unsigned on linux/arm64. If a
  scanner's result depends on that, note the spot in `scratch/` and follow
  the signed behaviour, which is what the local cgo oracle does.
- 10 scanners (bash, csharp, cpp, hcl, javascript, kotlin, lua, ruby, rust,
  scala) call `iswspace`, `iswalpha`, `iswalnum` and friends. Those are
  libc and locale dependent. Port them once, in one shared internal package
  under `grammars/`, and check the result against the cgo oracle with
  non-ASCII input before trusting it. Open issue, see section 12.
- The interface call happens once per external scan, not per character. The
  per-character calls inside a scanner are direct `*Lexer` method calls.

## 8. Generated packages

- `grammars/<name>/`, package name equals the directory name. Names: `bash`,
  `c`, `cpp`, `csharp`, `golang`, `hcl`, `java`, `javascript`, `json`,
  `julia`, `kotlin`, `lua`, `php`, `python`, `ruby`, `rust`, `scala`,
  `typescript`, `tsx`, `verilog`, `zig`. `go` is a keyword and `c-sharp` is
  not an identifier, hence `golang` and `csharp`.
- Sources: `php` is `php/src` (not `php_only`), `hcl` is `src` (not
  `dialects/terraform`), `typescript` and `tsx` are separate packages. All
  three checked against graphify-go's imports.
- `tables.go` (generated): symbol, field and external-token constants, every
  table, `var language`, and `func Language() *gositter.Language`.
- `lex.go` (generated): character-set tables, `lex`, `lexKeywords`.
- `scanner.go` (hand-written, only with a scanner): `newScanner`.
- Constants keep the C enum names unchanged: `sym_identifier`,
  `anon_sym_LBRACE`, `aux_sym_object_repeat1`, `alias_sym_x`, `field_name`,
  `ts_external_token__newline`, `ts_builtin_sym_end`. They all start lower
  case, so they stay unexported, and `lex.go` diffs cleanly against C.
- Table variables keep the C names too (`ts_parse_table`, `ts_lex_modes`).
- Generated files start with `// Code generated by gengrammar from <module>@<version>. DO NOT EDIT.`
- Tables are composite literals of pointer-free element types, so they are
  static data: no init work, nothing for the GC to scan. If compile time or
  binary size forces it, the generator may decode a table from a string
  constant at package init instead. The `Language` field types do not change.
- No dependency other than `github.com/dobbo-ca/gositter` and the standard
  library. No cgo, no `unsafe`.

## 9. Public API of the runtime

Signatures are pinned as stubs at the bottom of `api.go`. The runtime agent
moves each stub into the file named after its C file and deletes it from
`api.go`. Moving needs no change entry. Changing a signature does.

```go
func NewParser() *Parser
func (p *Parser) SetLanguage(l *Language) error
func (p *Parser) Parse(src []byte) *Tree
func (p *Parser) Reset()
func (p *Parser) SetLogger(fn Logger) // type Logger func(t LogType, msg string)

func (t *Tree) RootNode() Node
func (t *Tree) Language() *Language
func (t *Tree) Close()

func (l *Language) FieldIDForName(name string) FieldID
func (l *Language) FieldNameForID(id FieldID) string
func (l *Language) SymbolName(s Symbol) string

// Node is a value. The zero Node is the null node.
func (n Node) IsNull() bool
func (n Node) Language() *Language
func (n Node) Kind() string            // ts_node_type
func (n Node) Symbol() Symbol          // ts_node_symbol
func (n Node) IsNamed() bool
func (n Node) IsError() bool
func (n Node) IsMissing() bool
func (n Node) IsExtra() bool
func (n Node) HasError() bool
func (n Node) StartByte() uint32
func (n Node) EndByte() uint32
func (n Node) StartPosition() Point
func (n Node) EndPosition() Point
func (n Node) ChildCount() uint32
func (n Node) Child(i uint32) Node
func (n Node) NamedChildCount() uint32
func (n Node) NamedChild(i uint32) Node
func (n Node) Children(buf []Node) []Node      // appends Child(0..ChildCount()-1), one pass
func (n Node) NamedChildren(buf []Node) []Node // the same for NamedChild
func (n Node) ChildByFieldName(name string) Node
func (n Node) ChildByFieldID(id FieldID) Node
func (n Node) FieldNameForChild(i uint32) string // "" where C returns NULL
func (n Node) Parent() Node
func (n Node) Equals(other Node) bool
func (n Node) Utf8Text(src []byte) string
func (n Node) String() string          // ts_node_string, byte-identical
func (n Node) Walk() *TreeCursor
func (n Node) ChildrenByFieldName(name string, c *TreeCursor) []Node

func (c *TreeCursor) Reset(n Node)
func (c *TreeCursor) Node() Node
func (c *TreeCursor) FieldID() FieldID
func (c *TreeCursor) FieldName() string
func (c *TreeCursor) GotoFirstChild() bool
func (c *TreeCursor) GotoNextSibling() bool
func (c *TreeCursor) GotoParent() bool
func (c *TreeCursor) Close()
```

Differences from go-tree-sitter that the graphify adapter has to absorb:

- `Node` is a value, not `*Node`. Where go-tree-sitter returns `nil`, gositter
  returns the zero `Node`; test with `IsNull()`. This is what removes the
  per-node heap object.
- Byte offsets, counts and indexes are `uint32`, as in C, not `uint`.
- `Parse` takes no old tree.
- There is no `Parser.Close` and no `Language` wrapper.

Semantics:

- Every `Node` method is a port of the `ts_node_*` function named in the stub
  comment, including its behaviour on ERROR, MISSING, extra and aliased
  nodes. `ChildByFieldName` is `ts_language_field_id_for_name` followed by
  `ts_node_child_by_field_id`, including the inherited-field and hidden-child
  logic. `ChildByFieldID` lets a caller resolve the name once.
- `ChildrenByFieldName` and `Utf8Text` are not C functions. They copy
  go-tree-sitter's `node.go` (v0.25.0, lines 265 and 362).
- `Parent` is `ts_node_parent` (walk down from the root), not a stored link.
- Lifetime: a `Tree` borrows the parser's arenas. It and all of its nodes and
  cursors are valid until the next `Parse`, `Reset` or `SetLanguage` on that
  parser. `Close` lets the parser reuse the memory earlier. graphify's
  `parseRoot` already closes the tree before it lets go of the parser, so
  this costs it nothing. `Parse` may return a `*Tree` owned by the parser.
- `Parse` returns nil only when no language is set.
- A `Parser` is for one goroutine. Different parsers run in parallel.

## 10. Logging

`ts_parser`'s `LOG` and `LOG_LOOKAHEAD` calls are ported at the same places
with the same text, each behind `if p.logger != nil`. The message is built by
appending to a reusable byte buffer; no `fmt`. Lexer messages use
`LogTypeLex`, parser messages `LogTypeParse`.

C formats into a 1024-byte buffer with `snprintf`, so a message is cut at
1023 bytes. Do the same, or log diffs against the oracle break on long
symbol names.

The line diff of this log against the cgo oracle's log is the main tool for
finding the first diverging parser action.

`LOG_STACK` and `LOG_TREE` write dot graphs to `dot_graph_file`, not to the
logger. They are outside the contract. The runtime agent may port them behind
its own nil check or leave a comment at each call site.

## 11. Data layout rules for the runtime

The runtime agent owns the details. These are the constraints.

- Control flow and data flow follow C exactly: same order of stack versions,
  same iteration order, same tie-breaks, same constants (`MAX_VERSION_COUNT`,
  `MAX_COST_DIFFERENCE`, `ERROR_COST_PER_*`, `MAX_LINK_COUNT`,
  `MAX_NODE_POOL_SIZE`, `TS_MAX_INLINE_TREE_LENGTH`,
  `TS_MAX_TREE_POOL_SIZE`, ...). Where C behaviour depends on pool reuse or on
  array capacity, keep it.
- `Subtree` stays an 8-byte value: either the inline leaf with the bit layout
  of `SubtreeInlineData`, or a handle (index) into a per-parser slab of
  `SubtreeHeapData`. No Go pointer inside a `Subtree`, so slabs of them are
  invisible to the GC. Whether a leaf is inline follows
  `ts_subtree_can_inline` exactly.
- `ref_count` stays a real field with the C retain/release calls at the C
  places. `ts_subtree_make_mut` clones when `ref_count > 1`, same as C. No
  atomics: one parser, one goroutine.
- Children live in a slab too, addressed by offset. External scanner state
  (`ExternalScannerState`, 24 bytes inline or longer) lives in a byte slab.
- Stack nodes, links, heads, the reduce-action and tree-pool arrays are slabs
  or reused slices owned by the parser.
- Slabs are reset, not freed, between parses. A warm parser allocates nothing
  per file in the steady state, apart from growth.
- No maps, no `fmt`, no interface or closure call per character or per parse
  action. The only indirect calls are `LexFn`/`KeywordLexFn` (per token) and
  `ExternalScanner` methods (per external scan, per serialize).
- No `unsafe` and no assembly until a profile of a real corpus shows the spot.
- Root module: standard library only, no cgo. cgo lives in `oracle/` only.
- When unsure about inlining, escapes or bounds checks on a hot function,
  read `go build -gcflags='-m -d=ssa/check_bce'` instead of guessing.

## 12. Open issues

- `isw*` in scanners is platform libc. graphify-go today gets whatever the
  build machine's libc does, so macOS and Linux may already differ for
  non-ASCII input. Not measured. The scanner agent measures it with the cgo
  oracle and records which behaviour the port follows.
- `utf8.DecodeRune` against ICU `U8_NEXT`: believed equal, not measured here.
- Compile time and binary size of the big grammars (verilog and scala
  `parser.c` are 1.2M lines each) as Go literals: not measured here.
