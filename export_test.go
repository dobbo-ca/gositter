package gositter

// DenseMismatch compares the row-displacement small-table lookup with the C scan for
// every small state and every symbol value a lookup can carry. It returns
// the first difference, or ok.
func (l *Language) DenseMismatch() (state, symbol uint32, built, ok bool) {
	l.prepare()
	built = l.smallBase != nil
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
	return 4 * (len(l.smallBase) + len(l.smallComb))
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
