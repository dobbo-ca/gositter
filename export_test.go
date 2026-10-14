package gositter

import "slices"

// DenseMismatch compares the row-displacement small-table lookup with the C scan for
// every small state and every symbol value a lookup can carry. It returns
// the first difference, or ok.
func (l *Language) DenseMismatch() (state, symbol uint32, built, ok bool) {
	l.prepare()
	built = l.small.Load() != nil
	if !built {
		return 0, 0, false, true // lookup is the scan itself
	}
	for st := l.LargeStateCount; st < l.StateCount; st++ {
		for sym := uint32(0); sym <= 0xFFFF; sym++ {
			if sym == l.SymbolCount+l.AliasCount+2 {
				sym = 0xFFF0 // nothing in between; still cover the builtin symbols
			}
			if l.lookup(StateID(st), Symbol(sym)) != l.scanSmall(StateID(st), Symbol(sym)) {
				return st, sym, built, false
			}
		}
	}
	return 0, 0, built, true
}

// SmallTableBytes is the size of the derived small parse table.
func (l *Language) SmallTableBytes() int {
	l.prepare()
	t := l.small.Load()
	if t == nil {
		return 0
	}
	return 4 * (len(t.base) + len(t.comb))
}

// ChildrenSlabCap is the capacity of the children slab, in entries.
func (p *Parser) ChildrenSlabCap() int { return cap(p.pool.children) }

// HeapChunks is the number of heap chunks the parser holds.
func (p *Parser) HeapChunks() int { return len(p.pool.heap) }

const (
	KeptHeapChunks = keptHeapChunks
	ShrinkChildren = shrinkChildren
)

// RepeatFlagsOK reports whether fHasRepeat covers every node with a repeat
// depth in the last tree, and each of its ancestors. balanceSubtree skips
// what the flag leaves out.
func (p *Parser) RepeatFlagsOK() bool {
	sp := &p.pool
	stack := []subtree{sp.children[p.tree.root]}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if sp.childCount(s) == 0 {
			continue
		}
		h := sp.ptr(s)
		if h.repeatDepth > 0 && h.flags&fHasRepeat == 0 {
			return false
		}
		for _, c := range sp.childrenOf(s) {
			if sp.childCount(c) > 0 && sp.ptr(c).flags&fHasRepeat != 0 && h.flags&fHasRepeat == 0 {
				return false
			}
			stack = append(stack, c)
		}
	}
	return true
}

// OneChildSummaryOK reports whether summarizeOneChild and summarizeChildren
// agree on every node of the last tree that newNode sent down the short path.
func (p *Parser) OneChildSummaryOK() (nodes int, ok bool) {
	sp := &p.pool
	stack := []subtree{sp.children[p.tree.root]}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if sp.childCount(s) == 0 {
			continue
		}
		stack = append(stack, sp.childrenOf(s)...)
		h := sp.ptr(s)
		if h.childCount != 1 || h.symbol == SymbolError || h.symbol == SymbolErrorRepeat {
			continue
		}
		// The node as newNode leaves it for the summary.
		fresh := subtreeHeap{
			refCount:     1,
			symbol:       h.symbol,
			childCount:   1,
			flags:        h.flags & (fVisible | fNamed),
			productionID: h.productionID,
			children:     h.children,
			childClass:   h.childClass,
		}
		saved := *h
		*h = fresh
		sp.summarizeChildren(s, p.language)
		want := *h
		*h = saved
		got := fresh
		sp.summarizeOneChild(&got, fresh.flags, p.language.aliasSequence(uint32(fresh.productionID)), p.language)
		if got != want {
			return nodes, false
		}
		nodes++
	}
	return nodes, true
}

// ErrorScansOK reports whether every per-state answer the parser kept for
// error handling equals a fresh scan of the table, and how many it kept.
func (p *Parser) ErrorScansOK() (states int, ok bool) {
	saved := p.language
	defer func() { p.language = saved }()
	for key, scan := range p.reduceScans {
		p.language = key.language
		if p.potentialReductions(key.state, 1, uint32(uint16(key.language.TokenCount))) != scan.hasShiftAction ||
			!slices.Equal(p.reduceActions, scan.reduceActions) {
			return states, false
		}
		states++
	}
	for key, tokens := range p.missingScans {
		p.language = key.language
		kept := p.missingScans
		p.missingScans = nil
		fresh := p.missingTokens(key.state)
		p.missingScans = kept
		if !slices.Equal(fresh, tokens) {
			return states, false
		}
		states++
	}
	return states, true
}

// FieldIDMismatch compares FieldIDForName with the C scan on every field
// name, each of its prefixes and each name with a byte added. It returns
// the first name that differs.
func (l *Language) FieldIDMismatch() (string, bool) {
	for _, f := range l.FieldNames {
		for i := 0; i <= len(f); i++ {
			for _, name := range []string{f[:i], f[:i] + "x", f[:i] + "\x00"} {
				if l.FieldIDForName(name) != l.fieldIDForName(name) {
					return name, false
				}
			}
		}
	}
	return "", true
}

// ChildByFieldIDRef is ts_node_child_by_field_id as first ported, on the
// child iterator: the reference for ChildByFieldID.
func (n Node) ChildByFieldIDRef(fieldID FieldID) Node {
	self := n
recur:
	for {
		if fieldID == 0 || self.ChildCount() == 0 {
			return Node{}
		}

		sp := self.tree.pool
		fieldMap := self.tree.language.fieldMap(uint32(sp.ptr(self.subtree()).productionID))
		if len(fieldMap) == 0 {
			return Node{}
		}

		// The field mappings are sorted by their field id. Scan all the
		// mappings to find the ones for the given field id.
		for fieldMap[0].FieldID < fieldID {
			fieldMap = fieldMap[1:]
			if len(fieldMap) == 0 {
				return Node{}
			}
		}
		for fieldMap[len(fieldMap)-1].FieldID > fieldID {
			fieldMap = fieldMap[:len(fieldMap)-1]
			if len(fieldMap) == 0 {
				return Node{}
			}
		}

		var child Node
		iterator := self.iterateChildren()
		for iterator.next(&child) {
			if !sp.extra(child.subtree()) {
				index := iterator.structuralChildIndex - 1
				if index < uint32(fieldMap[0].ChildIndex) {
					continue
				}

				// Hidden nodes' fields are "inherited" by their visible
				// parent.
				if fieldMap[0].Inherited {
					// If this is the *last* possible child node for this
					// field, then perform a tail call to avoid recursion.
					if len(fieldMap) == 1 {
						self = child
						continue recur
					}

					// Otherwise, descend into this child, but if it doesn't
					// contain the field, continue searching subsequent
					// children.
					result := child.ChildByFieldIDRef(fieldID)
					if result.id != 0 {
						return result
					}
					fieldMap = fieldMap[1:]
					if len(fieldMap) == 0 {
						return Node{}
					}
				} else if child.isRelevant(true) {
					return child
				} else if child.ChildCount() > 0 {
					// If the field refers to a hidden node with visible
					// children, return the first visible child.
					return child.Child(0)
				} else {
					// Otherwise, continue searching subsequent children.
					fieldMap = fieldMap[1:]
					if len(fieldMap) == 0 {
						return Node{}
					}
				}
			}
		}

		return Node{}
	}
}

// ts_subtree_summarize_children on the ts_subtree_* accessors, a line of C to
// a line: the reference for summarize, summarizeTwoHeap and both loops of
// summarizeChildren. The kind mask and fHasRepeat are not in C.
func (sp *subtreePool) summarizeChildrenRef(self subtree, lang *Language) {
	h := sp.ptr(self)
	h.namedChildCount = 0
	h.visibleChildCount = 0
	h.errorCost = 0
	h.repeatDepth = 0
	h.visibleDescendantCount = 0
	h.flags &^= fHasExternalTokens | fDependsOnColumn | fHasExternalScannerStateChange | fHasRepeat
	h.dynamicPrecedence = 0
	kinds := uint32(0)

	structuralIndex := 0
	aliasSequence := lang.aliasSequence(uint32(h.productionID))
	lookaheadEndByte := uint32(0)
	selfIsError := h.symbol == SymbolError || h.symbol == SymbolErrorRepeat

	children := sp.children[h.children : h.children+h.childCount]
	for i, child := range children {
		if h.size.extent.Row == 0 && sp.dependsOnColumn(child) {
			h.flags |= fDependsOnColumn
		}
		if sp.hasExternalScannerStateChange(child) {
			h.flags |= fHasExternalScannerStateChange
		}

		if i == 0 {
			h.padding = sp.padding(child)
			h.size = sp.size(child)
		} else {
			h.size = lengthAdd(h.size, sp.totalSize(child))
		}

		childLookaheadEndByte := h.padding.bytes + h.size.bytes + sp.lookaheadBytes(child)
		if childLookaheadEndByte > lookaheadEndByte {
			lookaheadEndByte = childLookaheadEndByte
		}

		if sp.symbol(child) != SymbolErrorRepeat {
			h.errorCost += sp.errorCost(child)
		}

		grandchildCount := sp.childCount(child)
		if selfIsError {
			if !sp.extra(child) && !(sp.isError(child) && grandchildCount == 0) {
				if sp.visible(child) {
					h.errorCost += errorCostPerSkippedTree
				} else if grandchildCount > 0 {
					h.errorCost += errorCostPerSkippedTree * sp.ptr(child).visibleChildCount
				}
			}
		}

		h.dynamicPrecedence += sp.dynamicPrecedence(child)
		h.visibleDescendantCount += sp.visibleDescendantCount(child)
		kinds |= lang.kindBit(sp.symbol(child))
		if grandchildCount > 0 {
			kinds |= sp.ptr(child).kinds()
		}
		if !child.isInline() {
			h.flags |= sp.ptr(child).flags & fHasRepeat
		}

		if aliasSequence != nil && aliasSequence[structuralIndex] != 0 && !sp.extra(child) {
			kinds |= lang.kindBit(aliasSequence[structuralIndex])
			h.visibleDescendantCount++
			h.visibleChildCount++
			if lang.symbolMetadata(aliasSequence[structuralIndex]).Named {
				h.namedChildCount++
			}
		} else if sp.visible(child) {
			h.visibleDescendantCount++
			h.visibleChildCount++
			if sp.named(child) {
				h.namedChildCount++
			}
		} else if grandchildCount > 0 {
			h.visibleChildCount += sp.ptr(child).visibleChildCount
			h.namedChildCount += sp.ptr(child).namedChildCount
		}

		if sp.hasExternalTokens(child) {
			h.flags |= fHasExternalTokens
		}

		if sp.isError(child) {
			h.flags |= fFragileLeft | fFragileRight
			h.parseState = treeStateNone
		}

		if !sp.extra(child) {
			structuralIndex++
		}
	}

	h.lookaheadBytes = lookaheadEndByte - h.size.bytes - h.padding.bytes

	if selfIsError {
		h.errorCost += errorCostPerRecovery +
			errorCostPerSkippedChar*h.size.bytes +
			errorCostPerSkippedLine*h.size.extent.Row
	}

	if h.childCount > 0 {
		firstChild := children[0]
		lastChild := children[h.childCount-1]

		h.firstLeafSymbol = sp.leafSymbol(firstChild)
		h.firstLeafParseState = sp.leafParseState(firstChild)

		if sp.fragileLeft(firstChild) {
			h.flags |= fFragileLeft
		}
		if sp.fragileRight(lastChild) {
			h.flags |= fFragileRight
		}

		if h.childCount >= 2 && h.flags&(fVisible|fNamed) == 0 && sp.symbol(firstChild) == h.symbol {
			if sp.repeatDepth(firstChild) > sp.repeatDepth(lastChild) {
				h.repeatDepth = uint16(sp.repeatDepth(firstChild) + 1)
			} else {
				h.repeatDepth = uint16(sp.repeatDepth(lastChild) + 1)
			}
			h.flags |= fHasRepeat
		}
	}
	h.setKinds(kinds)
}

// SummaryCheck is what SummariesOK went over. Bad is "" or what differed
// first.
type SummaryCheck struct {
	Nodes, TwoHeap, Column, Resumed int
	Bad                             string
}

// summarized puts fresh in the slot of s, summarizes it with f and puts the
// node back.
func (sp *subtreePool) summarized(s subtree, fresh subtreeHeap, lang *Language, f func(subtree, *Language)) subtreeHeap {
	h := sp.ptr(s)
	saved := *h
	*h = fresh
	f(s, lang)
	got := *h
	*h = saved
	return got
}

// checkSummary compares, for one node with children, against
// summarizeChildrenRef: the summary as it stands, what summarize makes of
// the node as newNode leaves it, and what it makes of an ERROR node over the
// same children, from the start and resumed after some of them.
func (sp *subtreePool) checkSummary(s subtree, lang *Language, c *SummaryCheck) {
	h := *sp.ptr(s)
	children := sp.childrenOf(s)
	fresh := subtreeHeap{
		refCount:     1,
		symbol:       h.symbol,
		childCount:   h.childCount,
		flags:        h.flags & (fVisible | fNamed),
		productionID: h.productionID,
		children:     h.children,
		childClass:   h.childClass,
	}
	selfIsError := h.symbol == SymbolError || h.symbol == SymbolErrorRepeat
	if selfIsError {
		fresh.flags |= fFragileLeft | fFragileRight
	}
	c.Nodes++
	if h.childCount == 2 && !children[0].isInline() && !children[1].isInline() {
		c.TwoHeap++
	}
	if h.flags&fDependsOnColumn != 0 {
		c.Column++
	}

	want := sp.summarized(s, fresh, lang, sp.summarizeChildrenRef)
	if got := sp.summarized(s, fresh, lang, sp.summarize); got != want {
		c.Bad = "summarize differs from the reference"
		return
	}

	// What the children alone decide. The parser sets the parse state, the
	// fragile bits and extra, and adds the precedence of the production, on
	// any node but an ERROR node.
	pure := func(x subtreeHeap) subtreeHeap {
		x.refCount = 0
		keep := fHasExternalTokens | fHasExternalScannerStateChange | fHasRepeat | fDependsOnColumn
		// Not the column bit where balancing may have been at work. C
		// leaves it as it was above a repeat node that compress changed,
		// and compress summarizes a node over the size it had before,
		// which C tests for a first child that depends on the column.
		if h.flags&fHasRepeat != 0 || x.flags&(fVisible|fNamed) == 0 && sp.dependsOnColumn(children[0]) {
			keep &^= fDependsOnColumn
		}
		x.flags &= keep
		if x.symbol != SymbolError {
			x.parseState = 0
			x.dynamicPrecedence = 0
		}
		return x
	}
	if pure(h) != pure(want) {
		c.Bad = "summary in the tree differs from the reference"
		return
	}

	e := fresh
	e.symbol, e.productionID = SymbolError, 0
	e.flags = fVisible | fNamed | fFragileLeft | fFragileRight
	want = sp.summarized(s, e, lang, sp.summarizeChildrenRef)
	if got := sp.summarized(s, e, lang, sp.summarize); got != want {
		c.Bad = "summarize of an ERROR node differs from the reference"
		return
	}
	n := e.childCount
	for _, k := range []uint32{1, n / 2, n - 1, n} {
		if k == 0 {
			continue
		}
		r := e
		r.childCount = k
		sp.resume = sp.summarized(s, r, lang, sp.summarizeChildrenRef)
		got := sp.summarized(s, e, lang, sp.summarize)
		if got != want || sp.resume.childCount != 0 {
			c.Bad = "resumed summary of an ERROR node differs from the reference"
			return
		}
		c.Resumed++
	}
}

// SummariesOK runs checkSummary on every node with children of the last
// tree.
func (p *Parser) SummariesOK() SummaryCheck {
	var c SummaryCheck
	sp := &p.pool
	stack := []subtree{sp.children[p.tree.root]}
	for len(stack) > 0 && c.Bad == "" {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if sp.childCount(s) == 0 {
			continue
		}
		stack = append(stack, sp.childrenOf(s)...)
		sp.checkSummary(s, p.language, &c)
	}
	return c
}
