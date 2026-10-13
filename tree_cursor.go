package gositter

// tree_cursor.c: the part behind Walk and ChildrenByFieldName.

// TreeCursorEntry. subtree is a slot in the children slab.
type treeCursorEntry struct {
	subtree              uint32
	position             length
	childIndex           uint32
	structuralChildIndex uint32
	alias                Symbol // under its parent; 0 for an extra
}

// TSTreeCursor.
type TreeCursor struct {
	tree  *Tree
	stack []treeCursorEntry
}

// TreeCursorStep
const (
	treeCursorStepNone = iota
	treeCursorStepHidden
	treeCursorStepVisible
)

func (c *TreeCursor) entrySubtree(i int) subtree {
	return c.tree.pool.children[c.stack[i].subtree]
}

// ts_tree_cursor_is_entry_visible, alias read from the entry.
func (c *TreeCursor) isEntryVisible(index int) bool {
	entry := &c.stack[index]
	return index == 0 || entry.alias != 0 || c.tree.pool.visible(c.tree.pool.children[entry.subtree])
}

// Not a port: the loop of ts_tree_cursor_child_iterator_next over the
// children of h from i on, each decoded once. Pushes the first one that is
// visible or has visible children.
func (c *TreeCursor) scan(h *subtreeHeap, i, structural uint32, position length) int {
	sp := c.tree.pool
	aliasSequence := c.tree.language.aliasSequence(uint32(h.productionID))
	children := sp.children[h.children : h.children+h.childCount]
	for ; i < uint32(len(children)); i++ {
		child := children[i]
		var padding, size length
		var visible, extra bool
		var visibleChildren uint32
		if child.isInline() {
			padding, size = sp.padding(child), sp.size(child)
			visible, extra = child&inlVisible != 0, child&inlExtra != 0
		} else {
			ch := sp.ptr(child)
			padding, size = ch.padding, ch.size
			visible, extra = ch.flags&fVisible != 0, ch.flags&fExtra != 0
			if ch.childCount > 0 {
				visibleChildren = ch.visibleChildCount
			}
		}
		var alias Symbol
		if !extra && aliasSequence != nil {
			alias = aliasSequence[structural]
		}
		if i > 0 {
			position = lengthAdd(position, padding)
		}
		if visible || alias != 0 || visibleChildren > 0 {
			// Field stores: a struct built and copied is a wide load
			// behind narrow stores.
			n := len(c.stack)
			if n < cap(c.stack) {
				c.stack = c.stack[:n+1]
			} else {
				c.stack = append(c.stack, treeCursorEntry{})
			}
			e := &c.stack[n]
			e.subtree = h.children + i
			e.position.bytes = position.bytes
			e.position.extent.Row = position.extent.Row
			e.position.extent.Column = position.extent.Column
			e.childIndex = i
			e.structuralChildIndex = structural
			e.alias = alias
			if visible || alias != 0 {
				return treeCursorStepVisible
			}
			return treeCursorStepHidden
		}
		if !extra {
			structural++
		}
		position = lengthAdd(position, size)
	}
	return treeCursorStepNone
}

// ts_tree_cursor_init, with the root alias kept in the entry.
func (c *TreeCursor) init(n Node) {
	c.tree = n.tree
	alias := n.alias()
	if n.tree.pool.extra(n.subtree()) {
		alias = 0
	}
	c.stack = append(c.stack[:0], treeCursorEntry{
		subtree:  n.id,
		position: length{n.StartByte(), n.StartPosition()},
		alias:    alias,
	})
}

// ts_tree_cursor_reset
func (c *TreeCursor) Reset(n Node) {
	c.init(n)
}

// ts_tree_cursor_delete
func (c *TreeCursor) Close() {
	c.stack = nil
}

// ts_tree_cursor_goto_first_child, with the internal variant and the
// child_count guard of ts_tree_cursor_iterate_children folded in.
func (c *TreeCursor) GotoFirstChild() bool {
	sp := c.tree.pool
	for {
		e := &c.stack[len(c.stack)-1]
		t := sp.children[e.subtree]
		if t.isInline() {
			return false
		}
		h := sp.ptr(t)
		if h.childCount == 0 {
			return false
		}
		switch c.scan(h, 0, 0, e.position) {
		case treeCursorStepHidden:
			continue
		case treeCursorStepVisible:
			return true
		default:
			return false
		}
	}
}

// ts_tree_cursor_goto_sibling_internal, with scan as the advance function
// and the first ts_tree_cursor_child_iterator_next call open-coded.
func (c *TreeCursor) gotoNextSiblingInternal() int {
	sp := c.tree.pool
	initialSize := len(c.stack)

	for len(c.stack) > 1 {
		// scan may overwrite the popped slot, so read it first.
		popped := &c.stack[len(c.stack)-1]
		slot, childIndex, structural, alias := popped.subtree, popped.childIndex, popped.structuralChildIndex, popped.alias
		end := popped.position
		c.stack = c.stack[:len(c.stack)-1]
		h := sp.ptr(sp.children[c.stack[len(c.stack)-1].subtree])

		child := sp.children[slot]
		var size length
		var visible, extra bool
		if child.isInline() {
			size = sp.size(child)
			visible, extra = child&inlVisible != 0, child&inlExtra != 0
		} else {
			ch := sp.ptr(child)
			size = ch.size
			visible, extra = ch.flags&fVisible != 0, ch.flags&fExtra != 0
		}
		if (visible || alias != 0) && len(c.stack)+1 < initialSize {
			break
		}
		if !extra {
			structural++
		}
		if step := c.scan(h, childIndex+1, structural, lengthAdd(end, size)); step != treeCursorStepNone {
			return step
		}
	}

	// The popped entries are still in the backing array.
	c.stack = c.stack[:initialSize]
	return treeCursorStepNone
}

// ts_tree_cursor_goto_next_sibling
func (c *TreeCursor) GotoNextSibling() bool {
	switch c.gotoNextSiblingInternal() {
	case treeCursorStepHidden:
		c.GotoFirstChild()
		return true
	case treeCursorStepVisible:
		return true
	default:
		return false
	}
}

// ts_tree_cursor_goto_parent
func (c *TreeCursor) GotoParent() bool {
	for i := len(c.stack) - 2; i >= 0; i-- {
		if c.isEntryVisible(i) {
			c.stack = c.stack[:i+1]
			return true
		}
	}
	return false
}

// ts_tree_cursor_current_node, alias read from the entry.
func (c *TreeCursor) Node() Node {
	e := &c.stack[len(c.stack)-1]
	return nodeNew(c.tree, e.subtree, e.position, e.alias)
}

// ts_tree_cursor_current_field_id
func (c *TreeCursor) FieldID() FieldID {
	sp := c.tree.pool

	// Walk up the tree, visiting the current node and its invisible
	// ancestors.
	for i := len(c.stack) - 1; i > 0; i-- {
		entry := &c.stack[i]

		// Stop walking up when another visible node is found.
		if i != len(c.stack)-1 && c.isEntryVisible(i) {
			break
		}

		if sp.extra(sp.children[entry.subtree]) {
			break
		}

		parent := sp.ptr(c.entrySubtree(i - 1))
		for _, m := range c.tree.language.fieldMap(uint32(parent.productionID)) {
			if !m.Inherited && uint32(m.ChildIndex) == entry.structuralChildIndex {
				return m.FieldID
			}
		}
	}
	return 0
}

// ts_tree_cursor_current_field_name
func (c *TreeCursor) FieldName() string {
	if id := c.FieldID(); id != 0 {
		return c.tree.language.FieldNames[id]
	}
	return ""
}
