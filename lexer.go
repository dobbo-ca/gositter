package gositter

import (
	"strconv"
	"unicode/utf8"
)

// lexer.c. The input is one []byte and the included ranges are the single
// DEFAULT_RANGE {0, UINT32_MAX}, so included_range_count is 1 and
// current_included_range_index is 0, or 1 at end of input. Loops over the
// ranges are collapsed where marked. The chunk fields are kept because
// ts_lexer__advance tests them: the "chunk" is src[chunkStart:].

const (
	byteOrderMark = 0xFEFF
	decodeError   = -1 // TS_DECODE_ERROR

	includedRangeCount = 1
)

// ColumnData
type columnData struct {
	value uint32
	valid bool
}

// Every Lexer field besides Lookahead and ResultSymbol.
type lexerState struct {
	src []byte // TSInput

	currentPosition    length
	tokenStartPosition length
	tokenEndPosition   length

	logger Logger

	currentIncludedRangeIndex uint32
	chunkStart                uint32
	chunkSize                 uint32
	lookaheadSize             uint32
	hasChunk                  bool // chunk != NULL
	didGetColumn              bool
	columnData                columnData

	debugBuf []byte // debug_buffer, for log messages only
}

// The C LOG macro of lexer.c.
func (l *Lexer) logCharacter(message string) {
	b := append(l.debugBuf[:0], message...)
	if c := l.Lookahead; 32 <= c && c < 127 {
		b = append(b, " character:'"...)
		b = append(b, byte(c), '\'')
	} else {
		b = append(b, " character:"...)
		b = strconv.AppendInt(b, int64(c), 10)
	}
	l.debugBuf = b
	l.logger(LogTypeLex, string(b))
}

// ts_lexer__set_column_data
func (l *Lexer) setColumnData(val uint32) {
	l.columnData.valid = true
	l.columnData.value = val
}

// ts_lexer__increment_column_data
func (l *Lexer) incrementColumnData() {
	if l.columnData.valid {
		l.columnData.value++
	}
}

// ts_lexer__invalidate_column_data
func (l *Lexer) invalidateColumnData() {
	l.columnData.valid = false
	l.columnData.value = 0
}

// ts_lexer__eof
func (l *Lexer) EOF() bool {
	return l.currentIncludedRangeIndex == includedRangeCount
}

// ts_lexer__clear_chunk
func (l *Lexer) clearChunk() {
	l.hasChunk = false
	l.chunkSize = 0
	l.chunkStart = 0
}

// ts_lexer__get_chunk, with ts_string_input_read as the callback.
func (l *Lexer) getChunk() {
	l.chunkStart = l.currentPosition.bytes
	if l.currentPosition.bytes >= uint32(len(l.src)) {
		l.chunkSize = 0
		l.currentIncludedRangeIndex = includedRangeCount
		l.hasChunk = false
	} else {
		l.chunkSize = uint32(len(l.src)) - l.currentPosition.bytes
		l.hasChunk = true
	}
}

// ts_lexer__get_lookahead, UTF-8 only.
func (l *Lexer) getLookahead() {
	positionInChunk := l.currentPosition.bytes - l.chunkStart
	size := l.chunkSize - positionInChunk

	if size == 0 {
		l.lookaheadSize = 1
		l.Lookahead = 0
		return
	}

	chunk := l.src[l.currentPosition.bytes:]
	if c := chunk[0]; c < utf8.RuneSelf {
		l.lookaheadSize = 1
		l.Lookahead = int32(c)
		return
	}

	// ts_decode_utf8 is ICU's U8_NEXT. utf8.DecodeRune rejects the same
	// sequences; a valid U+FFFD comes back with size 3, not 1.
	r, n := utf8.DecodeRune(chunk)
	if r == utf8.RuneError && n == 1 {
		// C fetches a fresh chunk here when fewer than 4 bytes remain and
		// decodes again. The input is contiguous, so the result is the same
		// and only chunk_start moves.
		if size < 4 {
			l.getChunk()
		}
		l.Lookahead = decodeError
		l.lookaheadSize = 1
		return
	}
	l.Lookahead = int32(r)
	l.lookaheadSize = uint32(n)
}

// ts_lexer_goto
func (l *Lexer) goTo(position length) {
	if position.bytes != l.currentPosition.bytes {
		l.invalidateColumnData()
	}

	l.currentPosition = position

	// The range loop: the default range ends at UINT32_MAX, so it is always
	// found. Its start is byte 0, point (0,0).
	if l.currentPosition.bytes == 0 {
		l.currentPosition = length{}
	}
	l.currentIncludedRangeIndex = 0

	if l.hasChunk && (l.currentPosition.bytes < l.chunkStart ||
		l.currentPosition.bytes >= l.chunkStart+l.chunkSize) {
		l.clearChunk()
	}

	l.lookaheadSize = 0
	l.Lookahead = 0
}

// ts_lexer__advance, with ts_lexer__do_advance folded in: the lex functions
// call this once per character, and one call is cheaper than two.
func (l *Lexer) Advance(skip bool) {
	if !l.hasChunk {
		return
	}

	if l.logger != nil {
		if skip {
			l.logCharacter("skip")
		} else {
			l.logCharacter("consume")
		}
	}

	// ts_lexer__do_advance
	if l.lookaheadSize != 0 {
		if l.Lookahead == '\n' {
			l.currentPosition.extent.Row++
			l.currentPosition.extent.Column = 0
			l.setColumnData(0)
		} else {
			isBOM := l.currentPosition.bytes == 0 && l.Lookahead == byteOrderMark
			if !isBOM {
				l.incrementColumnData()
			}
			l.currentPosition.extent.Column += l.lookaheadSize
		}
		l.currentPosition.bytes += l.lookaheadSize
	}

	// The loop that steps to the next included range is dead: the position
	// never reaches the default range's end_byte of UINT32_MAX.

	if skip {
		l.tokenStartPosition = l.currentPosition
	}

	// One unsigned compare for "inside the chunk". There, an ASCII byte is
	// ts_lexer__get_lookahead's whole answer, so the call is skipped.
	if pos := l.currentPosition.bytes; pos-l.chunkStart < l.chunkSize {
		if c := l.src[pos]; c < utf8.RuneSelf {
			l.lookaheadSize = 1
			l.Lookahead = int32(c)
			return
		}
	} else {
		l.getChunk()
	}
	l.getLookahead()
}

// AdvanceRun is Advance(skip) repeated for as long as Lookahead is an ASCII
// character in set. It has no C counterpart: generated lex functions call it
// in states that loop on themselves (cmd/gengrammar/run.go). set[0] and
// set[128:] are false.
func (l *Lexer) AdvanceRun(set *[256]bool, skip bool) {
	pos := l.currentPosition.bytes
	if l.logger != nil || l.lookaheadSize != 1 || pos-l.chunkStart >= l.chunkSize {
		// Logging wants one message per character.
		for uint32(l.Lookahead) < 128 && set[l.Lookahead] && l.hasChunk {
			l.Advance(skip)
		}
		return
	}

	// Inside the chunk with a one-byte lookahead, Lookahead is src[pos] or a
	// decode error, and a byte of 0x80 or more ends the run.
	src := l.src
	end := pos
	rows, lineStart := uint32(0), pos
	for int(end) < len(src) && set[src[end]] {
		if src[end] == '\n' {
			rows++
			lineStart = end + 1
		}
		end++
	}
	if end == pos {
		return
	}

	// What ts_lexer__do_advance adds up one character at a time.
	if rows > 0 {
		l.currentPosition.extent.Row += rows
		l.currentPosition.extent.Column = end - lineStart
		l.columnData = columnData{value: end - lineStart, valid: true}
	} else {
		l.currentPosition.extent.Column += end - pos
		if l.columnData.valid {
			l.columnData.value += end - pos
		}
	}
	l.currentPosition.bytes = end

	if skip {
		l.tokenStartPosition = l.currentPosition
	}

	if end-l.chunkStart < l.chunkSize {
		if c := src[end]; c < utf8.RuneSelf {
			l.lookaheadSize = 1
			l.Lookahead = int32(c)
			return
		}
	} else {
		l.getChunk()
	}
	l.getLookahead()
}

// ts_lexer__mark_end
func (l *Lexer) MarkEnd() {
	// The included range boundary case needs range index > 0: dead here.
	l.tokenEndPosition = l.currentPosition
}

// ts_lexer__get_column
func (l *Lexer) GetColumn() uint32 {
	l.didGetColumn = true

	if !l.columnData.valid {
		goalByte := l.currentPosition.bytes

		// Back up to the beginning of the line.
		startOfCol := length{
			l.currentPosition.bytes - l.currentPosition.extent.Column,
			Point{l.currentPosition.extent.Row, 0},
		}
		l.goTo(startOfCol)
		l.setColumnData(0)
		l.getChunk()

		if !l.EOF() {
			l.getLookahead()

			// C calls ts_lexer__do_advance, which does not log.
			logger := l.logger
			l.logger = nil
			for l.currentPosition.bytes < goalByte && !l.EOF() && l.hasChunk {
				l.Advance(false)
				if l.EOF() {
					break
				}
			}
			l.logger = logger
		}
	}

	return l.columnData.value
}

// ts_lexer__is_at_included_range_start
func (l *Lexer) IsAtIncludedRangeStart() bool {
	if l.currentIncludedRangeIndex < includedRangeCount {
		return l.currentPosition.bytes == 0
	}
	return false
}

// ts_lexer_set_input
func (l *Lexer) setInput(src []byte) {
	l.src = src
	l.clearChunk()
	l.goTo(l.currentPosition)
}

// ts_lexer_reset
func (l *Lexer) reset(position length) {
	if position.bytes != l.currentPosition.bytes {
		l.goTo(position)
	}
}

// ts_lexer_start
func (l *Lexer) start() {
	l.tokenStartPosition = l.currentPosition
	l.tokenEndPosition = lengthUndefined
	l.ResultSymbol = 0
	l.didGetColumn = false
	if !l.EOF() {
		if l.chunkSize == 0 {
			l.getChunk()
		}
		if l.lookaheadSize == 0 {
			l.getLookahead()
		}
		if l.currentPosition.bytes == 0 {
			if l.Lookahead == byteOrderMark {
				l.Advance(true)
			}
			l.setColumnData(0)
		}
	}
}

// ts_lexer_finish
func (l *Lexer) finish(lookaheadEndByte *uint32) {
	if lengthIsUndefined(l.tokenEndPosition) {
		l.MarkEnd()
	}

	// Only true at an included range boundary; kept as written.
	if l.tokenEndPosition.bytes < l.tokenStartPosition.bytes {
		l.tokenStartPosition = l.tokenEndPosition
	}

	currentLookaheadEndByte := l.currentPosition.bytes + 1

	// The decoder may have looked at up to 4 bytes to call a sequence invalid.
	if l.Lookahead == decodeError {
		currentLookaheadEndByte += 4
	}

	if currentLookaheadEndByte > *lookaheadEndByte {
		*lookaheadEndByte = currentLookaheadEndByte
	}
}
