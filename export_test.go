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

const KeptHeapChunks = keptHeapChunks

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
