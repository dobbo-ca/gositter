// Package gositter is a Go port of the tree-sitter parser runtime v0.25.0
// (ABI 15). This file is the frozen contract between the runtime, the
// grammar generator and the hand-written scanners. See CONTRACT.md.
// Changes go through scratch/CONTRACT_CHANGES.md.
package gositter

import (
	"sync"
	"sync/atomic"
)

// TSSymbol, TSStateId, TSFieldId.
type (
	Symbol  uint16
	StateID uint16
	FieldID uint16
)

const (
	SymbolEnd         Symbol = 0      // ts_builtin_sym_end
	SymbolError       Symbol = 0xFFFF // ts_builtin_sym_error
	SymbolErrorRepeat Symbol = 0xFFFE // ts_builtin_sym_error_repeat

	// TREE_SITTER_SERIALIZATION_BUFFER_SIZE
	SerializationBufferSize = 1024

	LanguageVersion                  = 15 // TREE_SITTER_LANGUAGE_VERSION
	MinCompatibleLanguageVersion     = 13 // TREE_SITTER_MIN_COMPATIBLE_LANGUAGE_VERSION
	LanguageVersionWithReservedWords = 15
	LanguageVersionWithPrimaryStates = 14
)

// TSPoint. Column counts bytes.
type Point struct {
	Row    uint32
	Column uint32
}

// TSFieldMapEntry (4 bytes).
type FieldMapEntry struct {
	FieldID    FieldID
	ChildIndex uint8
	Inherited  bool
}

// TSMapSlice. Indexes the field and supertype maps.
type MapSlice struct {
	Index  uint16
	Length uint16
}

// TSSymbolMetadata.
type SymbolMetadata struct {
	Visible   bool
	Named     bool
	Supertype bool
}

// TSLexerMode. ABI 14 grammars (TSLexMode) leave ReservedWordSetID zero.
// LexState 0xFFFF is the C (uint16_t)-1 marker and must be kept.
type LexMode struct {
	LexState          uint16
	ExternalLexState  uint16
	ReservedWordSetID uint16
}

// TSCharacterRange, inclusive on both ends.
type CharacterRange struct {
	Start int32
	End   int32
}

// TSParseActionType.
const (
	ParseActionTypeShift   uint8 = 0
	ParseActionTypeReduce  uint8 = 1
	ParseActionTypeAccept  uint8 = 2
	ParseActionTypeRecover uint8 = 3
)

// ParseAction is one 8-byte, pointer-free slot of Language.ParseActions. It
// stands for both C unions, TSParseActionEntry and TSParseAction, and keeps
// the C byte layout. Which variant a slot holds follows from its position: a
// header slot is followed by Count() action slots.
//
//	slot      Type   ChildCount   Symbol   DynamicPrecedence      ProductionID
//	header    count  reusable     0        0                      0
//	shift     0      0            state    bit0 extra, bit1 rep.  0
//	reduce    1      child_count  symbol   dynamic_precedence     production_id
//	accept    2      0            0        0                      0
//	recover   3      0            0        0                      0
//
// The fields are named for the reduce variant; use the methods for the rest.
type ParseAction struct {
	Type              uint8
	ChildCount        uint8
	Symbol            Symbol
	DynamicPrecedence int16
	ProductionID      uint16
}

const (
	shiftExtra      = 1
	shiftRepetition = 2
)

// entry.count of a header slot.
func (a ParseAction) Count() uint8 { return a.Type }

// entry.reusable of a header slot.
func (a ParseAction) Reusable() bool { return a.ChildCount != 0 }

// shift.state
func (a ParseAction) State() StateID { return StateID(a.Symbol) }

// shift.extra
func (a ParseAction) Extra() bool { return a.DynamicPrecedence&shiftExtra != 0 }

// shift.repetition
func (a ParseAction) Repetition() bool { return a.DynamicPrecedence&shiftRepetition != 0 }

// LexFunc is the type of a generated ts_lex / ts_lex_keywords.
type LexFunc func(l *Lexer, state StateID) bool

// ExternalScanner is one instance of a grammar's external scanner. The
// runtime creates it where C calls create and drops it where C calls destroy.
type ExternalScanner interface {
	// scan. validSymbols has ExternalTokenCount elements and is read-only.
	// On success the scanner sets l.ResultSymbol to the external token index.
	Scan(l *Lexer, validSymbols []bool) bool
	// serialize. len(buf) == SerializationBufferSize. Returns bytes written.
	Serialize(buf []byte) int
	// deserialize. len(buf) == 0 means reset to the initial state. buf is
	// only valid during the call.
	Deserialize(buf []byte)
}

// TSLanguage.external_scanner. destroy has no Go counterpart.
type ExternalScannerInfo struct {
	States    []bool   // [external state][ExternalTokenCount], row 0 unused
	SymbolMap []Symbol // external token index -> symbol
	New       func() ExternalScanner
}

// TSLanguageMetadata.
type LanguageMetadata struct {
	MajorVersion uint8
	MinorVersion uint8
	PatchVersion uint8
}

// Language mirrors TSLanguage (ABI 15) field for field. C pointers become
// slices; two-dimensional C arrays are flattened row-major. A Language is
// immutable after construction and shared by all parsers.
type Language struct {
	ABIVersion             uint32
	SymbolCount            uint32
	AliasCount             uint32
	TokenCount             uint32
	ExternalTokenCount     uint32
	StateCount             uint32
	LargeStateCount        uint32
	ProductionIDCount      uint32
	FieldCount             uint32
	MaxAliasSequenceLength uint16

	ParseTable         []uint16 // [LargeStateCount][SymbolCount]
	SmallParseTable    []uint16
	SmallParseTableMap []uint32
	ParseActions       []ParseAction

	SymbolNames     []string // SymbolCount + AliasCount entries
	FieldNames      []string // FieldCount + 1 entries, [0] == ""
	FieldMapSlices  []MapSlice
	FieldMapEntries []FieldMapEntry
	SymbolMetadata  []SymbolMetadata
	PublicSymbolMap []Symbol
	AliasMap        []uint16 // zero-terminated, as in C
	AliasSequences  []Symbol // [ProductionIDCount][MaxAliasSequenceLength]

	LexModes            []LexMode
	LexFn               LexFunc
	KeywordLexFn        LexFunc // nil if the grammar has no word token
	KeywordCaptureToken Symbol

	ExternalScanner ExternalScannerInfo

	PrimaryStateIDs []StateID
	Name            string

	ReservedWords          []Symbol // [set][MaxReservedWordSetSize]
	MaxReservedWordSetSize uint16

	SupertypeCount      uint32
	SupertypeSymbols    []Symbol
	SupertypeMapSlices  []MapSlice
	SupertypeMapEntries []Symbol

	Metadata LanguageMetadata

	// Derived by prepare, see language.go.
	prepareOnce sync.Once
	parsed      atomic.Int64 // source bytes seen while small is nil
	small       atomic.Pointer[smallTable]
}

type smallTable struct {
	base []uint32 // per small state: its offset in comb
	comb []uint32 // (state+1)<<16 | value, 0 if free
}

// set_contains
func SetContains(ranges []CharacterRange, lookahead int32) bool {
	index := uint32(0)
	size := uint32(len(ranges))
	for size > 1 {
		halfSize := size / 2
		midIndex := index + halfSize
		r := &ranges[midIndex]
		if lookahead >= r.Start && lookahead <= r.End {
			return true
		} else if lookahead > r.End {
			index = midIndex
		}
		size -= halfSize
	}
	r := &ranges[index]
	return lookahead >= r.Start && lookahead <= r.End
}

// SetContainsASCII is set_contains with the answers for code points below
// 128 precomputed by the generator: ascii[c] is SetContains(ranges, c).
func SetContainsASCII(ascii *[128]bool, ranges []CharacterRange, lookahead int32) bool {
	if uint32(lookahead) < 128 {
		return ascii[lookahead]
	}
	return setContainsCall(ranges, lookahead)
}

// Kept out of line so that SetContainsASCII fits the inlining budget.
//
//go:noinline
func setContainsCall(ranges []CharacterRange, lookahead int32) bool {
	return SetContains(ranges, lookahead)
}

// Lexer is the C Lexer with its embedded TSLexer. Generated lex functions and
// scanners use only the two exported fields and the exported methods.
type Lexer struct {
	// Current code point: 0 at end of input, -1 for invalid UTF-8.
	Lookahead int32
	// Set by lex functions (a symbol) and scanners (an external token index).
	ResultSymbol Symbol

	lexerState
}

// TSLogType.
type LogType uint8

const (
	LogTypeParse LogType = 0
	LogTypeLex   LogType = 1
)

// Logger receives the same messages the C runtime passes to TSLogger.log.
type Logger func(t LogType, msg string)
