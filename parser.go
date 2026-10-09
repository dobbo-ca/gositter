package gositter

import (
	"bytes"
	"errors"
	"math"
	"strconv"
)

// parser.c.
//
// Not ported, because old_tree is always NULL: ts_parser__reuse_node,
// ts_parser__breakdown_lookahead (a lookahead is always a leaf here), the
// ReusableNode, included range differences. Also not ported: wasm,
// cancellation, timeouts and the progress callback (ts_parser__check_progress
// would always return true), LOG_STACK and LOG_TREE (dot graphs).

const (
	maxVersionCount         = 6                            // MAX_VERSION_COUNT
	maxVersionCountOverflow = 4                            // MAX_VERSION_COUNT_OVERFLOW
	maxSummaryDepth         = 16                           // MAX_SUMMARY_DEPTH
	maxCostDifference       = 18 * errorCostPerSkippedTree // MAX_COST_DIFFERENCE

	debugBufferSize = SerializationBufferSize // sizeof(debug_buffer)
)

// TokenCache
type tokenCache struct {
	token             subtree
	lastExternalToken subtree
	byteIndex         uint32
}

// ReduceAction (reduce_action.h)
type reduceAction struct {
	count             uint32
	symbol            Symbol
	dynamicPrecedence int32
	productionID      uint16
}

// TSParser. Not safe for concurrent use; one per goroutine.
type Parser struct {
	lexer           Lexer
	stack           stack
	pool            subtreePool // tree_pool
	language        *Language
	reduceActions   []reduceAction
	finishedTree    subtree
	trailingExtras  []subtree
	trailingExtras2 []subtree
	tokenCache      tokenCache
	externalScanner ExternalScanner // external_scanner_payload
	acceptCount     uint32
	hasError        bool

	// C serializes scanner state into lexer.debug_buffer. Log messages use
	// that buffer too, so the state gets its own.
	scannerBuf [SerializationBufferSize]byte

	tree Tree // the result of the last Parse; borrows the slabs above
}

// ErrorStatus
type errorStatus struct {
	cost              uint32
	nodeCount         uint32
	dynamicPrecedence int32
	isInError         bool
}

// ErrorComparison
const (
	errorComparisonTakeLeft = iota
	errorComparisonPreferLeft
	errorComparisonNone
	errorComparisonPreferRight
	errorComparisonTakeRight
)

// Logging. Every call site is behind "if p.lexer.logger != nil".

// logBuf builds one message without fmt.
type logBuf []byte

func (b logBuf) s(v string) logBuf { return append(b, v...) }
func (b logBuf) u(v uint32) logBuf { return strconv.AppendUint(b, uint64(v), 10) }
func (b logBuf) d(v int32) logBuf  { return strconv.AppendInt(b, int64(v), 10) }

func (p *Parser) lb() logBuf { return logBuf(p.lexer.debugBuf[:0]) }

// The LOG macro plus ts_parser__log. snprintf cuts at sizeof(buffer) - 1.
func (p *Parser) log(b logBuf) {
	p.lexer.debugBuf = b[:0]
	if len(b) > debugBufferSize-1 {
		b = b[:debugBufferSize-1]
	}
	p.lexer.logger(LogTypeParse, string(b))
}

// LOG_LOOKAHEAD
func (p *Parser) logLookahead(symbolName string, size uint32) {
	b := p.lb().s("lexed_lookahead sym:")
	for i := 0; i < len(symbolName) && len(b) < debugBufferSize; i++ {
		switch c := symbolName[i]; c {
		case '\t':
			b = append(b, '\\', 't')
		case '\n':
			b = append(b, '\\', 'n')
		case '\v':
			b = append(b, '\\', 'v')
		case '\f':
			b = append(b, '\\', 'f')
		case '\r':
			b = append(b, '\\', 'r')
		case '\\':
			b = append(b, '\\', '\\')
		default:
			b = append(b, c)
		}
	}
	p.log(b.s(", size:").u(size))
}

// SYM_NAME
func (p *Parser) symName(symbol Symbol) string { return p.language.SymbolName(symbol) }

// TREE_NAME
func (p *Parser) treeName(tree subtree) string { return p.symName(p.pool.symbol(tree)) }

// ts_parser__breakdown_top_of_stack. Without reused nodes no link is ever
// pending, so the pop comes back empty; ported for the stack walk it does.
func (p *Parser) breakdownTopOfStack(version uint32) bool {
	sp := &p.pool
	didBreakDown := false
	pending := false

	for {
		pop := p.stack.popPending(version)
		if len(pop) == 0 {
			break
		}

		didBreakDown = true
		pending = false
		for i := 0; i < len(pop); i++ {
			slice := pop[i]
			state := p.stack.state(slice.version)
			subtrees := sp.array(slice.subtrees)
			parent := subtrees[0]

			for _, child := range sp.childrenOf(parent) {
				pending = sp.childCount(child) > 0

				if sp.isError(child) {
					state = errorState
				} else if !sp.extra(child) {
					state = p.language.nextState(state, sp.symbol(child))
				}

				sp.retain(child)
				p.stack.push(slice.version, child, pending, state)
			}

			for j := 1; j < len(subtrees); j++ {
				p.stack.push(slice.version, subtrees[j], false, state)
			}

			sp.release(parent)
			sp.arrayFree(&slice.subtrees)

			if p.lexer.logger != nil {
				p.log(p.lb().s("breakdown_top_of_stack tree:").s(p.treeName(parent)))
			}
		}
		if !pending {
			break
		}
	}

	return didBreakDown
}

// ts_parser__compare_versions
func compareVersions(a, b errorStatus) int {
	if !a.isInError && b.isInError {
		if a.cost < b.cost {
			return errorComparisonTakeLeft
		}
		return errorComparisonPreferLeft
	}

	if a.isInError && !b.isInError {
		if b.cost < a.cost {
			return errorComparisonTakeRight
		}
		return errorComparisonPreferRight
	}

	if a.cost < b.cost {
		if (b.cost-a.cost)*(1+a.nodeCount) > maxCostDifference {
			return errorComparisonTakeLeft
		}
		return errorComparisonPreferLeft
	}

	if b.cost < a.cost {
		if (a.cost-b.cost)*(1+b.nodeCount) > maxCostDifference {
			return errorComparisonTakeRight
		}
		return errorComparisonPreferRight
	}

	if a.dynamicPrecedence > b.dynamicPrecedence {
		return errorComparisonPreferLeft
	}
	if b.dynamicPrecedence > a.dynamicPrecedence {
		return errorComparisonPreferRight
	}
	return errorComparisonNone
}

// ts_parser__version_status
func (p *Parser) versionStatus(version uint32) errorStatus {
	cost := p.stack.errorCost(version)
	isPaused := p.stack.isPaused(version)
	if isPaused {
		cost += errorCostPerSkippedTree
	}
	return errorStatus{
		cost:              cost,
		nodeCount:         p.stack.nodeCountSinceError(version),
		dynamicPrecedence: p.stack.dynamicPrecedence(version),
		isInError:         isPaused || p.stack.state(version) == errorState,
	}
}

// ts_parser__better_version_exists
func (p *Parser) betterVersionExists(version uint32, isInError bool, cost uint32) bool {
	if p.finishedTree != nullSubtree && p.pool.errorCost(p.finishedTree) <= cost {
		return true
	}

	position := p.stack.position(version)
	status := errorStatus{
		cost:              cost,
		isInError:         isInError,
		dynamicPrecedence: p.stack.dynamicPrecedence(version),
		nodeCount:         p.stack.nodeCountSinceError(version),
	}

	for i, n := uint32(0), p.stack.versionCount(); i < n; i++ {
		if i == version ||
			!p.stack.isActive(i) ||
			p.stack.position(i).bytes < position.bytes {
			continue
		}
		statusI := p.versionStatus(i)
		switch compareVersions(status, statusI) {
		case errorComparisonTakeRight:
			return true
		case errorComparisonPreferRight:
			if p.stack.canMerge(i, version) {
				return true
			}
		}
	}

	return false
}

// ts_parser__external_scanner_create
func (p *Parser) externalScannerCreate() {
	if p.language != nil && p.language.ExternalScanner.States != nil {
		if p.language.ExternalScanner.New != nil {
			p.externalScanner = p.language.ExternalScanner.New()
		}
	}
}

// ts_parser__external_scanner_destroy
func (p *Parser) externalScannerDestroy() {
	p.externalScanner = nil
}

// ts_parser__external_scanner_serialize
func (p *Parser) externalScannerSerialize() uint32 {
	return uint32(p.externalScanner.Serialize(p.scannerBuf[:]))
}

// ts_parser__external_scanner_deserialize
func (p *Parser) externalScannerDeserialize(externalToken subtree) {
	var data []byte
	if externalToken != nullSubtree {
		h := p.pool.ptr(externalToken)
		data = p.pool.ext[h.extOff : h.extOff+h.extLen]
	}
	p.externalScanner.Deserialize(data)
}

// ts_parser__external_scanner_scan
func (p *Parser) externalScannerScan(externalLexState uint16) bool {
	validExternalTokens := p.language.enabledExternalTokens(uint32(externalLexState))
	return p.externalScanner.Scan(&p.lexer, validExternalTokens)
}

// ts_parser__can_reuse_first_leaf
func (p *Parser) canReuseFirstLeaf(state StateID, tree subtree, actionCount int, isReusable bool) bool {
	sp := &p.pool
	leafSymbol := sp.leafSymbol(tree)
	leafState := sp.leafParseState(tree)
	currentLexMode := p.language.lexModeForState(state)
	leafLexMode := p.language.lexModeForState(leafState)

	// At the end of a non-terminal extra node the lexer returns NULL. Don't
	// reuse tokens in this situation.
	if currentLexMode.LexState == 0xFFFF {
		return false
	}

	// If the token was created in a state with the same set of lookaheads,
	// it is reusable.
	if actionCount > 0 &&
		leafLexMode == currentLexMode &&
		(leafSymbol != p.language.KeywordCaptureToken ||
			(!sp.isKeyword(tree) && sp.parseState(tree) == state)) {
		return true
	}

	// Empty tokens are not reusable in states with different lookaheads.
	if sp.size(tree).bytes == 0 && leafSymbol != SymbolEnd {
		return false
	}

	// If the current state allows external tokens or other tokens that
	// conflict with this token, this token is not reusable.
	return currentLexMode.ExternalLexState == 0 && isReusable
}

// ts_parser__lex
func (p *Parser) lex(version uint32, parseState StateID) subtree {
	sp := &p.pool
	lang := p.language
	lexer := &p.lexer

	lexMode := lang.lexModeForState(parseState)
	if lexMode.LexState == 0xFFFF {
		if lexer.logger != nil {
			p.log(p.lb().s("no_lookahead_after_non_terminal_extra"))
		}
		return nullSubtree
	}

	startPosition := p.stack.position(version)
	externalToken := p.stack.lastExternalToken(version)

	foundExternalToken := false
	errorMode := parseState == errorState
	skippedError := false
	calledGetColumn := false
	firstErrorCharacter := int32(0)
	errorStartPosition := length{}
	errorEndPosition := length{}
	lookaheadEndByte := uint32(0)
	externalScannerStateLen := uint32(0)
	externalScannerStateChanged := false
	lexer.reset(startPosition)

	for {
		foundToken := false
		currentPosition := lexer.currentPosition
		columnData := lexer.columnData

		if lexMode.ExternalLexState != 0 {
			if lexer.logger != nil {
				p.log(p.lb().s("lex_external state:").u(uint32(lexMode.ExternalLexState)).
					s(", row:").u(currentPosition.extent.Row).
					s(", column:").u(currentPosition.extent.Column))
			}
			lexer.start()
			p.externalScannerDeserialize(externalToken)
			foundToken = p.externalScannerScan(lexMode.ExternalLexState)
			lexer.finish(&lookaheadEndByte)

			if foundToken {
				externalScannerStateLen = p.externalScannerSerialize()
				externalScannerStateChanged = !bytes.Equal(
					sp.externalScannerState(externalToken),
					p.scannerBuf[:externalScannerStateLen],
				)

				// When recovering from an error, ignore any zero-length
				// external tokens unless they have changed the external
				// scanner's state. This avoids infinite loops, because the
				// lexer is looking for any possible token here.
				if lexer.tokenEndPosition.bytes <= currentPosition.bytes &&
					(errorMode || !p.stack.hasAdvancedSinceError(version)) &&
					!externalScannerStateChanged {
					if lexer.logger != nil {
						p.log(p.lb().s("ignore_empty_external_token symbol:").
							s(p.symName(lang.ExternalScanner.SymbolMap[lexer.ResultSymbol])))
					}
					foundToken = false
				}
			}

			if foundToken {
				foundExternalToken = true
				calledGetColumn = lexer.didGetColumn
				break
			}

			lexer.reset(currentPosition)
			lexer.columnData = columnData
		}

		if lexer.logger != nil {
			p.log(p.lb().s("lex_internal state:").u(uint32(lexMode.LexState)).
				s(", row:").u(currentPosition.extent.Row).
				s(", column:").u(currentPosition.extent.Column))
		}
		lexer.start()
		foundToken = lang.LexFn(lexer, StateID(lexMode.LexState))
		lexer.finish(&lookaheadEndByte)
		if foundToken {
			break
		}

		if !errorMode {
			errorMode = true
			lexMode = lang.lexModeForState(errorState)
			lexer.reset(startPosition)
			continue
		}

		if !skippedError {
			if lexer.logger != nil {
				p.log(p.lb().s("skip_unrecognized_character"))
			}
			skippedError = true
			errorStartPosition = lexer.tokenStartPosition
			errorEndPosition = lexer.tokenStartPosition
			firstErrorCharacter = lexer.Lookahead
		}

		if lexer.currentPosition.bytes == errorEndPosition.bytes {
			if lexer.EOF() {
				lexer.ResultSymbol = SymbolError
				break
			}
			lexer.Advance(false)
		}

		errorEndPosition = lexer.currentPosition
	}

	var result subtree
	if skippedError {
		padding := lengthSub(errorStartPosition, startPosition)
		size := lengthSub(errorEndPosition, errorStartPosition)
		lookaheadBytes := lookaheadEndByte - errorEndPosition.bytes
		result = sp.newError(firstErrorCharacter, padding, size, lookaheadBytes, parseState, lang)
	} else {
		isKeyword := false
		symbol := lexer.ResultSymbol
		padding := lengthSub(lexer.tokenStartPosition, startPosition)
		size := lengthSub(lexer.tokenEndPosition, lexer.tokenStartPosition)
		lookaheadBytes := lookaheadEndByte - lexer.tokenEndPosition.bytes

		if foundExternalToken {
			symbol = lang.ExternalScanner.SymbolMap[symbol]
		} else if symbol == lang.KeywordCaptureToken && symbol != 0 {
			endByte := lexer.tokenEndPosition.bytes
			lexer.reset(lexer.tokenStartPosition)
			lexer.start()

			isKeyword = lang.KeywordLexFn(lexer, 0)

			if isKeyword &&
				lexer.tokenEndPosition.bytes == endByte &&
				(lang.hasActions(parseState, lexer.ResultSymbol) ||
					lang.isReservedWord(parseState, lexer.ResultSymbol)) {
				symbol = lexer.ResultSymbol
			}
		}

		result = sp.newLeaf(
			symbol, padding, size, lookaheadBytes, parseState,
			foundExternalToken, calledGetColumn, isKeyword, lang,
		)

		if foundExternalToken {
			// ts_external_scanner_state_init
			state := p.scannerBuf[:externalScannerStateLen]
			h := sp.ptr(result)
			if prev := sp.externalScannerState(externalToken); !externalScannerStateChanged && len(prev) > 0 {
				// Same bytes as the previous token's state: share them.
				ph := sp.ptr(externalToken)
				h.extOff, h.extLen = ph.extOff, ph.extLen
			} else {
				if len(sp.ext)+len(state) > math.MaxUint32 {
					panic(errSlabFull)
				}
				h.extOff = uint32(len(sp.ext))
				h.extLen = externalScannerStateLen
				sp.ext = append(sp.ext, state...)
			}
			// In C the state shares a union with the node fields, and
			// ts_subtree_repeat_depth reads repeat_depth from it unchecked:
			// bytes 16 and 17 of a state that is stored inline (at most 24
			// bytes). A longer state is a pointer and leaves them zero.
			if n := len(state); n > 16 && n <= 24 {
				h.repeatDepth = uint16(state[16])
				if n > 17 {
					h.repeatDepth |= uint16(state[17]) << 8
				}
			}
			h.setFlag(fHasExternalScannerStateChange, externalScannerStateChanged)
		}
	}

	if lexer.logger != nil {
		p.logLookahead(p.treeName(result), sp.totalSize(result).bytes)
	}
	return result
}

// ts_parser__get_cached_token
func (p *Parser) getCachedToken(state StateID, position uint32, lastExternalToken subtree) (subtree, []ParseAction, bool) {
	cache := &p.tokenCache
	if cache.token != nullSubtree && cache.byteIndex == position &&
		p.pool.externalScannerStateEq(cache.lastExternalToken, lastExternalToken) {
		actions, isReusable := p.language.tableEntry(state, p.pool.symbol(cache.token))
		if p.canReuseFirstLeaf(state, cache.token, len(actions), isReusable) {
			p.pool.retain(cache.token)
			return cache.token, actions, isReusable
		}
		// C leaves the table entry filled in here. Every path that follows a
		// miss overwrites it before use.
	}
	return nullSubtree, nil, false
}

// ts_parser__set_cached_token
func (p *Parser) setCachedToken(byteIndex uint32, lastExternalToken, token subtree) {
	cache := &p.tokenCache
	if token != nullSubtree {
		p.pool.retain(token)
	}
	if lastExternalToken != nullSubtree {
		p.pool.retain(lastExternalToken)
	}
	if cache.token != nullSubtree {
		p.pool.release(cache.token)
	}
	if cache.lastExternalToken != nullSubtree {
		p.pool.release(cache.lastExternalToken)
	}
	cache.token = token
	cache.byteIndex = byteIndex
	cache.lastExternalToken = lastExternalToken
}

// ts_parser__select_tree. Reports whether right should replace left.
func (p *Parser) selectTree(left, right subtree) bool {
	sp := &p.pool
	if left == nullSubtree {
		return true
	}
	if right == nullSubtree {
		return false
	}

	if sp.errorCost(right) < sp.errorCost(left) {
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_smaller_error symbol:").s(p.treeName(right)).
				s(", over_symbol:").s(p.treeName(left)))
		}
		return true
	}

	if sp.errorCost(left) < sp.errorCost(right) {
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_smaller_error symbol:").s(p.treeName(left)).
				s(", over_symbol:").s(p.treeName(right)))
		}
		return false
	}

	if sp.dynamicPrecedence(right) > sp.dynamicPrecedence(left) {
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_higher_precedence symbol:").s(p.treeName(right)).
				s(", prec:").d(sp.dynamicPrecedence(right)).
				s(", over_symbol:").s(p.treeName(left)).
				s(", other_prec:").d(sp.dynamicPrecedence(left)))
		}
		return true
	}

	if sp.dynamicPrecedence(left) > sp.dynamicPrecedence(right) {
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_higher_precedence symbol:").s(p.treeName(left)).
				s(", prec:").d(sp.dynamicPrecedence(left)).
				s(", over_symbol:").s(p.treeName(right)).
				s(", other_prec:").d(sp.dynamicPrecedence(right)))
		}
		return false
	}

	if sp.errorCost(left) > 0 {
		return true
	}

	switch sp.compare(left, right) {
	case -1:
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_earlier symbol:").s(p.treeName(left)).
				s(", over_symbol:").s(p.treeName(right)))
		}
		return false
	case 1:
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_earlier symbol:").s(p.treeName(right)).
				s(", over_symbol:").s(p.treeName(left)))
		}
		return true
	default:
		if p.lexer.logger != nil {
			p.log(p.lb().s("select_existing symbol:").s(p.treeName(left)).
				s(", over_symbol:").s(p.treeName(right)))
		}
		return false
	}
}

// ts_parser__select_children. C builds the temporary node in the reused
// scratch_trees array; here it sits at the end of the slabs and is dropped
// again. It owns no reference to its children.
func (p *Parser) selectChildren(left subtree, children subtreeArray) bool {
	sp := &p.pool
	mark := len(sp.children)
	scratch := subtreeArray{size: children.size, cap: children.size}
	scratch.off = sp.bumpChildren(children.size)
	copy(sp.array(scratch), sp.array(children))

	scratchTree := sp.newNode(sp.symbol(left), &scratch, 0, p.language)
	result := p.selectTree(left, scratchTree)

	sp.free(uint32(scratchTree >> 1))
	sp.children = sp.children[:mark]
	return result
}

// ts_parser__shift
func (p *Parser) shift(version uint32, state StateID, lookahead subtree, extra bool) {
	sp := &p.pool
	isLeaf := sp.childCount(lookahead) == 0
	subtreeToPush := lookahead
	if extra != sp.extra(lookahead) && isLeaf {
		result := sp.makeMut(lookahead)
		sp.setExtra(&result, extra)
		subtreeToPush = result
	}

	p.stack.push(version, subtreeToPush, !isLeaf, state)
	if sp.hasExternalTokens(subtreeToPush) {
		p.stack.setLastExternalToken(version, sp.lastExternalToken(subtreeToPush))
	}
}

// ts_parser__reduce
func (p *Parser) reduce(
	version uint32, symbol Symbol, count uint32,
	dynamicPrecedence int32, productionID uint16,
	isFragile, endOfNonTerminalExtra bool,
) uint32 {
	sp := &p.pool
	initialVersionCount := p.stack.versionCount()

	// Pop the given number of nodes from the given version of the parse
	// stack. If stack versions have previously merged, there may be more
	// than one path back through the stack. For each path, create a new
	// parent node and push it in place of the children.
	pop := p.stack.popCount(version, count)
	removedVersionCount := uint32(0)
	for i := 0; i < len(pop); i++ {
		slice := pop[i]
		sliceVersion := slice.version - removedVersionCount

		// The version count may be exceeded temporarily, but only by a
		// limited threshold.
		if sliceVersion > maxVersionCount+maxVersionCountOverflow {
			p.stack.removeVersion(sliceVersion)
			sp.arrayDelete(&slice.subtrees)
			removedVersionCount++
			for i+1 < len(pop) {
				nextSlice := pop[i+1]
				if nextSlice.version != slice.version {
					break
				}
				sp.arrayDelete(&nextSlice.subtrees)
				i++
			}
			continue
		}

		// Extra tokens on top of the stack are not included in the new
		// parent. They are re-pushed after it.
		children := slice.subtrees
		sp.arrayRemoveTrailingExtras(&children, &p.trailingExtras)

		parent := sp.newNode(symbol, &children, uint32(productionID), p.language)

		// This pop may have caused multiple stack versions to collapse into
		// one. Choose one of the arrays of trees to be the parent node's
		// children, and delete the rest.
		for i+1 < len(pop) {
			nextSlice := pop[i+1]
			if nextSlice.version != slice.version {
				break
			}
			i++

			nextSliceChildren := nextSlice.subtrees
			sp.arrayRemoveTrailingExtras(&nextSliceChildren, &p.trailingExtras2)

			if p.selectChildren(parent, nextSliceChildren) {
				for _, t := range p.trailingExtras {
					sp.release(t)
				}
				p.trailingExtras = p.trailingExtras[:0]
				sp.release(parent)
				p.trailingExtras, p.trailingExtras2 = p.trailingExtras2, p.trailingExtras
				parent = sp.newNode(symbol, &nextSliceChildren, uint32(productionID), p.language)
			} else {
				p.trailingExtras2 = p.trailingExtras2[:0]
				sp.arrayDelete(&nextSlice.subtrees)
			}
		}

		state := p.stack.state(sliceVersion)
		nextState := p.language.nextState(state, symbol)
		h := sp.ptr(parent)
		if endOfNonTerminalExtra && nextState == state {
			h.flags |= fExtra
		}
		if isFragile || len(pop) > 1 || initialVersionCount > 1 {
			h.flags |= fFragileLeft | fFragileRight
			h.parseState = treeStateNone
		} else {
			h.parseState = state
		}
		h.dynamicPrecedence += dynamicPrecedence

		// Push the parent node onto the stack, along with any extra tokens
		// that were previously on top of the stack.
		p.stack.push(sliceVersion, parent, false, nextState)
		for _, t := range p.trailingExtras {
			p.stack.push(sliceVersion, t, false, nextState)
		}

		for j := uint32(0); j < sliceVersion; j++ {
			if j == version {
				continue
			}
			if p.stack.merge(j, sliceVersion) {
				removedVersionCount++
				break
			}
		}
	}

	// Return the first new stack version that was created.
	if p.stack.versionCount() > initialVersionCount {
		return initialVersionCount
	}
	return stackVersionNone
}

// ts_parser__reduce followed by the ts_stack_renumber_version of
// ts_parser__advance, for the last action of a table entry when this version
// owns an unbranched chain and the reduced version could not merge with
// another one. The general path would pop one slice into a new version, fail
// every ts_stack_merge, and move that version back over this one. Reports
// false and changes nothing if the stack is not of that shape.
func (p *Parser) reduceInPlace(
	version uint32, symbol Symbol, count uint32,
	dynamicPrecedence int32, productionID uint16,
	isFragile, endOfNonTerminalExtra bool,
) bool {
	sp := &p.pool
	s := &p.stack
	versionCount := s.versionCount()
	if versionCount > maxVersionCount+maxVersionCountOverflow ||
		s.heads[version].lookaheadWhenPaused != nullSubtree {
		return false
	}
	children, trailing, below, ok := s.popLinear(version, count)
	if !ok {
		return false
	}
	state := s.nodes[below].state
	nextState := p.language.nextState(state, symbol)

	// ts_stack_can_merge needs the state and byte position of the new head.
	// Both are known now: popping and pushing the same subtrees leaves the
	// position where it is.
	if versionCount > 1 {
		position := s.nodes[s.heads[version].node].position.bytes
		for j := range s.heads {
			node := &s.nodes[s.heads[j].node]
			if uint32(j) != version && node.state == nextState && node.position.bytes == position {
				s.undoPopLinear(children)
				return false
			}
		}
	}
	s.heads[version].node = below

	// ts_subtree_array_remove_trailing_extras: they are the last trailing
	// subtrees of the slice.
	children.size -= trailing
	p.trailingExtras = append(p.trailingExtras[:0],
		sp.children[children.off+children.size:children.off+children.size+trailing]...)

	parent := sp.newNode(symbol, &children, uint32(productionID), p.language)

	h := sp.ptr(parent)
	if endOfNonTerminalExtra && nextState == state {
		h.flags |= fExtra
	}
	if isFragile || versionCount > 1 {
		h.flags |= fFragileLeft | fFragileRight
		h.parseState = treeStateNone
	} else {
		h.parseState = state
	}
	h.dynamicPrecedence += dynamicPrecedence

	s.push(version, parent, false, nextState)
	for _, t := range p.trailingExtras {
		s.push(version, t, false, nextState)
	}
	return true
}

// ts_parser__accept
func (p *Parser) accept(version uint32, lookahead subtree) {
	sp := &p.pool
	p.stack.push(version, lookahead, false, 1)

	pop := p.stack.popAll(version)
	for i := 0; i < len(pop); i++ {
		trees := pop[i].subtrees

		root := nullSubtree
		for j := trees.size - 1; j+1 > 0; j-- {
			tree := sp.children[trees.off+j]
			if !sp.extra(tree) {
				children := sp.childrenOf(tree)
				for _, child := range children {
					sp.retain(child)
				}
				sp.arraySplice(&trees, j, 1, children)
				root = sp.newNode(sp.symbol(tree), &trees, uint32(sp.ptr(tree).productionID), p.language)
				sp.release(tree)
				break
			}
		}

		p.acceptCount++

		if p.finishedTree != nullSubtree {
			if p.selectTree(p.finishedTree, root) {
				sp.release(p.finishedTree)
				p.finishedTree = root
			} else {
				sp.release(root)
			}
		} else {
			p.finishedTree = root
		}
	}

	p.stack.removeVersion(pop[0].version)
	p.stack.halt(version)
}

// ts_parser__do_all_potential_reductions
func (p *Parser) doAllPotentialReductions(startingVersion uint32, lookaheadSymbol Symbol) bool {
	initialVersionCount := p.stack.versionCount()

	canShiftLookaheadSymbol := false
	version := startingVersion
	for i := uint32(0); ; i++ {
		versionCount := p.stack.versionCount()
		if version >= versionCount {
			break
		}

		merged := false
		for j := initialVersionCount; j < version; j++ {
			if p.stack.merge(j, version) {
				merged = true
				break
			}
		}
		if merged {
			continue
		}

		state := p.stack.state(version)
		hasShiftAction := false
		p.reduceActions = p.reduceActions[:0]

		var firstSymbol, endSymbol uint32
		if lookaheadSymbol != 0 {
			firstSymbol = uint32(lookaheadSymbol)
			endSymbol = uint32(lookaheadSymbol + 1) // TSSymbol arithmetic: wraps at 0xFFFF
		} else {
			firstSymbol = 1
			endSymbol = uint32(uint16(p.language.TokenCount))
		}

		for symbol := firstSymbol; symbol < endSymbol; symbol++ {
			actions, _ := p.language.tableEntry(state, Symbol(symbol))
			for _, action := range actions {
				switch action.Type {
				case ParseActionTypeShift, ParseActionTypeRecover:
					if !action.Extra() && !action.Repetition() {
						hasShiftAction = true
					}
				case ParseActionTypeReduce:
					if action.ChildCount > 0 {
						p.reduceActionSetAdd(reduceAction{
							symbol:            action.Symbol,
							count:             uint32(action.ChildCount),
							dynamicPrecedence: int32(action.DynamicPrecedence),
							productionID:      action.ProductionID,
						})
					}
				}
			}
		}

		reductionVersion := stackVersionNone
		for j := 0; j < len(p.reduceActions); j++ {
			action := p.reduceActions[j]

			reductionVersion = p.reduce(
				version, action.symbol, action.count,
				action.dynamicPrecedence, action.productionID,
				true, false,
			)
		}

		if hasShiftAction {
			canShiftLookaheadSymbol = true
		} else if reductionVersion != stackVersionNone && i < maxVersionCount {
			p.stack.renumberVersion(reductionVersion, version)
			continue
		} else if lookaheadSymbol != 0 {
			p.stack.removeVersion(version)
		}

		if version == startingVersion {
			version = versionCount
		} else {
			version++
		}
	}

	return canShiftLookaheadSymbol
}

// ts_reduce_action_set_add (reduce_action.h)
func (p *Parser) reduceActionSetAdd(newAction reduceAction) {
	for _, action := range p.reduceActions {
		if action.symbol == newAction.symbol && action.count == newAction.count {
			return
		}
	}
	p.reduceActions = append(p.reduceActions, newAction)
}

// ts_parser__recover_to_state
func (p *Parser) recoverToState(version uint32, depth uint32, goalState StateID) bool {
	sp := &p.pool
	// pop shares its backing array with stack.slices, and popError below
	// runs another stack walk that writes slot 0 of it. C has the same
	// aliasing; slot 0 has been read by then.
	pop := p.stack.popCount(version, depth)
	previousVersion := stackVersionNone

	for i := 0; i < len(pop); i++ {
		slice := pop[i]

		if slice.version == previousVersion {
			sp.arrayDelete(&slice.subtrees)
			pop = append(pop[:i], pop[i+1:]...)
			i--
			continue
		}

		if p.stack.state(slice.version) != goalState {
			p.stack.halt(slice.version)
			sp.arrayDelete(&slice.subtrees)
			pop = append(pop[:i], pop[i+1:]...)
			i--
			continue
		}

		errorTrees := p.stack.popError(slice.version)
		if errorTrees.size > 0 {
			errorTree := sp.children[errorTrees.off]
			errorChildCount := sp.childCount(errorTree)
			if errorChildCount > 0 {
				sp.arraySplice(&slice.subtrees, 0, 0, sp.childrenOf(errorTree))
				for j := uint32(0); j < errorChildCount; j++ {
					sp.retain(sp.children[slice.subtrees.off+j])
				}
			}
			sp.arrayDelete(&errorTrees)
		}

		sp.arrayRemoveTrailingExtras(&slice.subtrees, &p.trailingExtras)

		if slice.subtrees.size > 0 {
			errorNode := sp.newErrorNode(&slice.subtrees, true, p.language)
			p.stack.push(slice.version, errorNode, false, goalState)
		} else {
			sp.arrayFree(&slice.subtrees)
		}

		for _, tree := range p.trailingExtras {
			p.stack.push(slice.version, tree, false, goalState)
		}

		previousVersion = slice.version
	}

	return previousVersion != stackVersionNone
}

// ts_parser__recover
func (p *Parser) recover(version uint32, lookahead subtree) {
	sp := &p.pool
	didRecover := false
	previousVersionCount := p.stack.versionCount()
	position := p.stack.position(version)
	summary := p.stack.getSummary(version)
	nodeCountSinceError := p.stack.nodeCountSinceError(version)
	currentErrorCost := p.stack.errorCost(version)

	// In the error state there are two strategies for recovering with a
	// given lookahead token:
	// 1. Find a previous state on the stack in which that lookahead token
	//    would be valid, pop back to it and wrap the popped subtrees in an
	//    ERROR node.
	// 2. Wrap the lookahead token in an ERROR node, push it and stay in the
	//    error state.
	//
	// First strategy 1, using the summary recorded on entering the error
	// state.
	if summary != nil && !sp.isError(lookahead) {
		for i := 0; i < len(summary); i++ {
			entry := summary[i]

			if entry.state == errorState {
				continue
			}
			if entry.position.bytes == position.bytes {
				continue
			}
			depth := entry.depth
			if nodeCountSinceError > 0 {
				depth++
			}

			// Do not recover in ways that create redundant stack versions.
			wouldMerge := false
			for j := uint32(0); j < previousVersionCount; j++ {
				if p.stack.state(j) == entry.state &&
					p.stack.position(j).bytes == position.bytes {
					wouldMerge = true
					break
				}
			}
			if wouldMerge {
				continue
			}

			// Do not recover if the result would clearly be worse than some
			// existing stack version.
			newCost := currentErrorCost +
				entry.depth*errorCostPerSkippedTree +
				(position.bytes-entry.position.bytes)*errorCostPerSkippedChar +
				(position.extent.Row-entry.position.extent.Row)*errorCostPerSkippedLine
			if p.betterVersionExists(version, false, newCost) {
				break
			}

			// If the current lookahead token is valid in some previous
			// state, recover to that state. Then stop looking.
			if p.language.hasActions(entry.state, sp.symbol(lookahead)) {
				if p.recoverToState(version, depth, entry.state) {
					didRecover = true
					if p.lexer.logger != nil {
						p.log(p.lb().s("recover_to_previous state:").u(uint32(entry.state)).
							s(", depth:").u(depth))
					}
					break
				}
			}
		}
	}

	// Some stack versions may have been created and then halted during the
	// attempt. Remove those versions.
	for i := previousVersionCount; i < p.stack.versionCount(); i++ {
		if !p.stack.isActive(i) {
			p.stack.removeVersion(i)
			i--
		}
	}

	// If strategy 1 succeeded, a new stack version can handle the current
	// lookahead token. In addition, try strategy 2, unless there are
	// already too many stack versions.
	if didRecover && p.stack.versionCount() > maxVersionCount {
		p.stack.halt(version)
		sp.release(lookahead)
		return
	}

	if didRecover && sp.hasExternalScannerStateChange(lookahead) {
		p.stack.halt(version)
		sp.release(lookahead)
		return
	}

	// If the parser is still in the error state at the end of the file,
	// just wrap everything in an ERROR node and terminate.
	if sp.isEOF(lookahead) {
		if p.lexer.logger != nil {
			p.log(p.lb().s("recover_eof"))
		}
		children := subtreeArray{}
		parent := sp.newErrorNode(&children, false, p.language)
		p.stack.push(version, parent, false, 1)
		p.accept(version, lookahead)
		return
	}

	// Do not recover if the result would clearly be worse than some
	// existing stack version.
	newCost := currentErrorCost + errorCostPerSkippedTree +
		sp.totalBytes(lookahead)*errorCostPerSkippedChar +
		sp.totalSize(lookahead).extent.Row*errorCostPerSkippedLine
	if p.betterVersionExists(version, false, newCost) {
		p.stack.halt(version)
		sp.release(lookahead)
		return
	}

	// If the current lookahead token is an extra token, mark it as extra, so
	// it is not counted in error cost calculations.
	actions, _ := p.language.tableEntry(1, sp.symbol(lookahead))
	if n := len(actions); n > 0 && actions[n-1].Type == ParseActionTypeShift && actions[n-1].Extra() {
		mutableLookahead := sp.makeMut(lookahead)
		sp.setExtra(&mutableLookahead, true)
		lookahead = mutableLookahead
	}

	// Wrap the lookahead token in an ERROR.
	if p.lexer.logger != nil {
		p.log(p.lb().s("skip_token symbol:").s(p.treeName(lookahead)))
	}
	children := subtreeArray{}
	sp.arrayReserve(&children, 1)
	sp.arrayPush(&children, lookahead)
	errorRepeat := sp.newNode(SymbolErrorRepeat, &children, 0, p.language)

	// If other tokens have already been skipped, there is already an ERROR
	// at the top of the stack. Pop it and wrap the two together.
	if nodeCountSinceError > 0 {
		pop := p.stack.popCount(version, 1)

		// If multiple stack versions have merged at this point, just pick
		// one of the errors arbitrarily and discard the rest.
		if len(pop) > 1 {
			for i := 1; i < len(pop); i++ {
				sp.arrayDelete(&pop[i].subtrees)
			}
			for p.stack.versionCount() > pop[0].version+1 {
				p.stack.removeVersion(pop[0].version + 1)
			}
		}

		p.stack.renumberVersion(pop[0].version, version)
		sp.arrayPush(&pop[0].subtrees, errorRepeat)
		errorRepeat = sp.newNode(SymbolErrorRepeat, &pop[0].subtrees, 0, p.language)
	}

	// Push the new ERROR onto the stack.
	p.stack.push(version, errorRepeat, false, errorState)
	if sp.hasExternalTokens(lookahead) {
		p.stack.setLastExternalToken(version, sp.lastExternalToken(lookahead))
	}

	hasError := true
	for i := uint32(0); i < p.stack.versionCount(); i++ {
		status := p.versionStatus(i)
		if !status.isInError {
			hasError = false
			break
		}
	}
	p.hasError = hasError
}

// ts_parser__handle_error
func (p *Parser) handleError(version uint32, lookahead subtree) {
	sp := &p.pool
	previousVersionCount := p.stack.versionCount()

	// Perform any reductions that can happen in this state, regardless of
	// the lookahead.
	p.doAllPotentialReductions(version, 0)
	versionCount := p.stack.versionCount()
	position := p.stack.position(version)

	// Push a discontinuity onto the stack. Merge all of the stack versions
	// that were created in the previous step.
	didInsertMissingToken := false
	for v := version; v < versionCount; {
		if !didInsertMissingToken {
			state := p.stack.state(v)
			tokenCount := uint32(uint16(p.language.TokenCount))
			for missingSymbol := uint32(1); missingSymbol < tokenCount; missingSymbol++ {
				stateAfterMissingSymbol := p.language.nextState(state, Symbol(missingSymbol))
				if stateAfterMissingSymbol == 0 || stateAfterMissingSymbol == state {
					continue
				}

				if p.language.hasReduceAction(stateAfterMissingSymbol, sp.leafSymbol(lookahead)) {
					// The lexer snaps to the next included range, and the
					// missing token's padding places it there. With one
					// range the padding is zero.
					p.lexer.reset(position)
					p.lexer.MarkEnd()
					padding := lengthSub(p.lexer.tokenEndPosition, position)
					lookaheadBytes := sp.totalBytes(lookahead) + sp.lookaheadBytes(lookahead)

					versionWithMissingTree := p.stack.copyVersion(v)
					missingTree := sp.newMissingLeaf(Symbol(missingSymbol), padding, lookaheadBytes, p.language)
					p.stack.push(versionWithMissingTree, missingTree, false, stateAfterMissingSymbol)

					if p.doAllPotentialReductions(versionWithMissingTree, sp.leafSymbol(lookahead)) {
						if p.lexer.logger != nil {
							p.log(p.lb().s("recover_with_missing symbol:").s(p.symName(Symbol(missingSymbol))).
								s(", state:").u(uint32(p.stack.state(versionWithMissingTree))))
						}
						didInsertMissingToken = true
						break
					}
				}
			}
		}

		p.stack.push(v, nullSubtree, false, errorState)
		if v == version {
			v = previousVersionCount
		} else {
			v++
		}
	}

	for i := previousVersionCount; i < versionCount; i++ {
		p.stack.merge(version, previousVersionCount)
	}

	p.stack.recordSummary(version, maxSummaryDepth)

	// Begin recovery with the current lookahead node, rather than waiting
	// for the next turn of the parse loop.
	p.recover(version, lookahead)
}

// ts_parser__advance
func (p *Parser) advance(version uint32) bool {
	sp := &p.pool
	lang := p.language
	state := p.stack.state(version)
	position := p.stack.position(version).bytes
	lastExternalToken := p.stack.lastExternalToken(version)

	// No old tree, so nothing to reuse but the token last returned by the
	// lexer.
	lookahead, actions, _ := p.getCachedToken(state, position, lastExternalToken)

	needsLex := lookahead == nullSubtree
	for {
		// Otherwise, re-run the lexer.
		if needsLex {
			needsLex = false
			lookahead = p.lex(version, state)

			if lookahead != nullSubtree {
				p.setCachedToken(position, lastExternalToken, lookahead)
				actions, _ = lang.tableEntry(state, sp.symbol(lookahead))
			} else {
				// When parsing a non-terminal extra, a null lookahead
				// indicates the end of the rule. The reduction is stored in
				// the EOF table entry.
				actions, _ = lang.tableEntry(state, SymbolEnd)
			}
		}

		// Process each parse action for the current lookahead token in the
		// current state. REDUCE actions always create a new stack version,
		// whereas SHIFT actions update the existing stack version and
		// terminate this loop.
		lastReductionVersion := stackVersionNone
		for i := 0; i < len(actions); i++ {
			action := actions[i]

			switch action.Type {
			case ParseActionTypeShift:
				if action.Repetition() {
					break
				}
				var nextState StateID
				if action.Extra() {
					nextState = state
					if p.lexer.logger != nil {
						p.log(p.lb().s("shift_extra"))
					}
				} else {
					nextState = action.State()
					if p.lexer.logger != nil {
						p.log(p.lb().s("shift state:").u(uint32(nextState)))
					}
				}

				p.shift(version, nextState, lookahead, action.Extra())
				return true

			case ParseActionTypeReduce:
				isFragile := len(actions) > 1
				endOfNonTerminalExtra := lookahead == nullSubtree
				if p.lexer.logger != nil {
					p.log(p.lb().s("reduce sym:").s(p.symName(action.Symbol)).
						s(", child_count:").u(uint32(action.ChildCount)))
				}
				if i == len(actions)-1 && p.reduceInPlace(
					version, action.Symbol, uint32(action.ChildCount),
					int32(action.DynamicPrecedence), action.ProductionID,
					isFragile, endOfNonTerminalExtra,
				) {
					lastReductionVersion = version
					break
				}
				reductionVersion := p.reduce(
					version, action.Symbol, uint32(action.ChildCount),
					int32(action.DynamicPrecedence), action.ProductionID,
					isFragile, endOfNonTerminalExtra,
				)
				if reductionVersion != stackVersionNone {
					lastReductionVersion = reductionVersion
				}

			case ParseActionTypeAccept:
				if p.lexer.logger != nil {
					p.log(p.lb().s("accept"))
				}
				p.accept(version, lookahead)
				return true

			case ParseActionTypeRecover:
				p.recover(version, lookahead)
				return true
			}
		}

		// If a reduction was performed, then replace the current stack
		// version with one of the stack versions created by a reduction,
		// and continue processing this version of the stack with the same
		// lookahead symbol.
		if lastReductionVersion != stackVersionNone {
			p.stack.renumberVersion(lastReductionVersion, version)
			state = p.stack.state(version)

			// At the end of a non-terminal extra rule, the lexer returns a
			// null subtree. After the reduction, run the lexer again based
			// on the current parse state.
			if lookahead == nullSubtree {
				needsLex = true
			} else {
				actions, _ = lang.tableEntry(state, sp.leafSymbol(lookahead))
			}

			continue
		}

		// A non-terminal extra rule was reduced and merged into an existing
		// stack version. This version can be discarded.
		if lookahead == nullSubtree {
			p.stack.halt(version)
			return true
		}

		// If the current lookahead token is a keyword that is not valid,
		// but the default word token *is* valid, then treat the lookahead
		// token as the word token instead.
		if sp.isKeyword(lookahead) &&
			sp.symbol(lookahead) != lang.KeywordCaptureToken &&
			!lang.isReservedWord(state, sp.symbol(lookahead)) {
			actions, _ = lang.tableEntry(state, lang.KeywordCaptureToken)
			if len(actions) > 0 {
				if p.lexer.logger != nil {
					p.log(p.lb().s("switch from_keyword:").s(p.treeName(lookahead)).
						s(", to_word_token:").s(p.symName(lang.KeywordCaptureToken)))
				}

				mutableLookahead := sp.makeMut(lookahead)
				sp.setSymbol(&mutableLookahead, lang.KeywordCaptureToken, lang)
				lookahead = mutableLookahead
				continue
			}
		}

		// If the previous subtree on the stack was reused from an old tree,
		// break it down and try again. Never the case here; see
		// breakdownTopOfStack.
		if p.breakdownTopOfStack(version) {
			state = p.stack.state(version)
			sp.release(lookahead)
			needsLex = true
			continue
		}

		// Otherwise, there is definitely an error in this version of the
		// parse stack. Mark this version as paused and continue processing
		// any other stack versions that exist. If all versions end up
		// paused, then error recovery is needed.
		if p.lexer.logger != nil {
			p.log(p.lb().s("detect_error"))
		}
		p.stack.pause(version, lookahead)
		return true
	}
}

// ts_parser__condense_stack
func (p *Parser) condenseStack() uint32 {
	madeChanges := false
	minErrorCost := ^uint32(0)

	// One active version: nothing below prunes, compares, merges or resumes
	// it. ts_parser__version_status is still called, because
	// ts_stack_node_count_since_error can lower node_count_at_last_error.
	if p.stack.versionCount() == 1 && p.stack.isActive(0) {
		if status := p.versionStatus(0); !status.isInError {
			minErrorCost = status.cost
		}
		return minErrorCost
	}

	for i := uint32(0); i < p.stack.versionCount(); i++ {
		// Prune any versions that have been marked for removal.
		if p.stack.isHalted(i) {
			p.stack.removeVersion(i)
			i--
			continue
		}

		// Keep track of the minimum error cost of any stack version so that
		// it can be returned.
		statusI := p.versionStatus(i)
		if !statusI.isInError && statusI.cost < minErrorCost {
			minErrorCost = statusI.cost
		}

		// Examine each pair of stack versions, removing any versions that
		// are clearly worse than another version. Ensure that the versions
		// are ordered from most promising to least promising. i and j wrap
		// below zero and back, as the unsigned C counters do.
		for j := uint32(0); j < i; j++ {
			statusJ := p.versionStatus(j)

			switch compareVersions(statusJ, statusI) {
			case errorComparisonTakeLeft:
				madeChanges = true
				p.stack.removeVersion(i)
				i--
				j = i

			case errorComparisonPreferLeft, errorComparisonNone:
				if p.stack.merge(j, i) {
					madeChanges = true
					i--
					j = i
				}

			case errorComparisonPreferRight:
				madeChanges = true
				if p.stack.merge(j, i) {
					i--
					j = i
				} else {
					p.stack.swapVersions(i, j)
				}

			case errorComparisonTakeRight:
				madeChanges = true
				p.stack.removeVersion(j)
				i--
				j--
			}
		}
	}

	// Enforce a hard upper bound on the number of stack versions by
	// discarding the least promising versions.
	for p.stack.versionCount() > maxVersionCount {
		p.stack.removeVersion(maxVersionCount)
		madeChanges = true
	}

	// If the best-performing stack version is currently paused, or all
	// versions are paused, then resume the best paused version and begin
	// the error recovery process. Otherwise, remove the paused versions.
	if p.stack.versionCount() > 0 {
		hasUnpausedVersion := false
		for i, n := uint32(0), p.stack.versionCount(); i < n; i++ {
			if p.stack.isPaused(i) {
				if !hasUnpausedVersion && p.acceptCount < maxVersionCount {
					if p.lexer.logger != nil {
						p.log(p.lb().s("resume version:").u(i))
					}
					minErrorCost = p.stack.errorCost(i)
					lookahead := p.stack.resume(i)
					p.handleError(i, lookahead)
					hasUnpausedVersion = true
				} else {
					p.stack.removeVersion(i)
					i--
					n--
				}
			} else {
				hasUnpausedVersion = true
			}
		}
	}

	if madeChanges {
		if p.lexer.logger != nil {
			p.log(p.lb().s("condense"))
		}
	}

	return minErrorCost
}

// ts_parser__balance_subtree
func (p *Parser) balanceSubtree() {
	sp := &p.pool
	finishedTree := p.finishedTree

	sp.treeStack = sp.treeStack[:0]
	if sp.childCount(finishedTree) > 0 && sp.ptr(finishedTree).refCount == 1 {
		sp.treeStack = append(sp.treeStack, finishedTree)
	}

	for len(sp.treeStack) > 0 {
		tree := sp.treeStack[len(sp.treeStack)-1]

		if h := sp.ptr(tree); h.repeatDepth > 0 {
			child1 := sp.children[h.children]
			child2 := sp.children[h.children+h.childCount-1]
			repeatDelta := int64(sp.repeatDepth(child1)) - int64(sp.repeatDepth(child2))
			if repeatDelta > 0 {
				n := uint32(repeatDelta)

				for i := n / 2; i > 0; i /= 2 {
					sp.compress(tree, i, p.language, &sp.treeStack)
					n -= i
				}
			}
		}

		sp.treeStack = sp.treeStack[:len(sp.treeStack)-1]

		for _, child := range sp.childrenOf(tree) {
			if sp.childCount(child) > 0 && sp.ptr(child).refCount == 1 {
				sp.treeStack = append(sp.treeStack, child)
			}
		}
	}
}

// Parser - Public

// ts_parser_new
func NewParser() *Parser {
	p := &Parser{}
	p.lexer.debugBuf = make([]byte, 0, debugBufferSize)
	p.reduceActions = make([]reduceAction, 0, 4)
	p.stack.sp = &p.pool
	p.Reset()
	return p
}

// ts_parser_set_language. Errors on an incompatible ABIVersion.
func (p *Parser) SetLanguage(l *Language) error {
	p.Reset()
	p.language = nil

	if l != nil {
		if l.ABIVersion > LanguageVersion || l.ABIVersion < MinCompatibleLanguageVersion {
			return errors.New("gositter: incompatible language ABI version " +
				strconv.FormatUint(uint64(l.ABIVersion), 10))
		}
	}

	if l != nil {
		l.prepare()
	}
	p.language = l
	return nil
}

// ts_parser_set_logger. nil turns logging off.
func (p *Parser) SetLogger(fn Logger) {
	p.lexer.logger = fn
}

// ts_parser_reset. C releases the stack, the token cache and the finished
// tree one reference at a time. Here everything lives in the parser's slabs
// and nothing outlives the reset, so they are truncated instead. The tree
// of the last Parse dies with them.
func (p *Parser) Reset() {
	p.externalScannerDestroy()
	p.lexer.reset(length{})
	p.pool.reset()
	p.stack.reset()
	p.tokenCache = tokenCache{}
	p.finishedTree = nullSubtree
	p.trailingExtras = p.trailingExtras[:0]
	p.trailingExtras2 = p.trailingExtras2[:0]
	p.acceptCount = 0
	p.hasError = false
	p.tree = Tree{}
}

// ts_parser_parse with old_tree NULL and UTF-8 input. Returns nil if no
// language is set. The tree is valid until the next Parse, Reset or
// SetLanguage on p.
func (p *Parser) Parse(src []byte) *Tree {
	if p.language == nil {
		return nil
	}

	// C resets at the end of a parse. The tree borrows the slabs, so the
	// reset happens here, before the next one.
	p.Reset()

	p.lexer.setInput(src)

	p.externalScannerCreate()
	if p.lexer.logger != nil {
		p.log(p.lb().s("new_parse"))
	}

	var position, lastPosition, versionCount uint32
	for {
		for version := uint32(0); ; version++ {
			versionCount = p.stack.versionCount()
			if version >= versionCount {
				break
			}
			for p.stack.isActive(version) {
				if p.lexer.logger != nil {
					pos := p.stack.position(version)
					p.log(p.lb().s("process version:").u(version).
						s(", version_count:").u(p.stack.versionCount()).
						s(", state:").u(uint32(p.stack.state(version))).
						s(", row:").u(pos.extent.Row).
						s(", col:").u(pos.extent.Column))
				}

				p.advance(version)

				position = p.stack.position(version).bytes
				if position > lastPosition || (version > 0 && position == lastPosition) {
					lastPosition = position
					break
				}
			}
		}

		// After advancing each version of the stack, re-sort the versions by
		// their cost, removing any versions that are no longer worth
		// pursuing.
		minErrorCost := p.condenseStack()

		// If there's already a finished parse tree that's better than any
		// in-progress version, then terminate parsing. Clear the parse stack
		// to remove any extra references to subtrees within the finished
		// tree, ensuring that these subtrees can be safely mutated in-place
		// for rebalancing.
		if p.finishedTree != nullSubtree && p.pool.errorCost(p.finishedTree) < minErrorCost {
			p.stack.clear()
			break
		}

		if versionCount == 0 {
			break
		}
	}

	p.balanceSubtree()
	if p.lexer.logger != nil {
		p.log(p.lb().s("done"))
	}

	// ts_tree_new. The root gets a slot in the children slab so that a Node
	// can address it like any other subtree.
	root := p.pool.bumpChildren(1)
	p.pool.children[root] = p.finishedTree
	p.tree = Tree{pool: &p.pool, language: p.language, root: root}
	p.finishedTree = nullSubtree

	p.externalScannerDestroy()
	p.lexer.src = nil
	return &p.tree
}
