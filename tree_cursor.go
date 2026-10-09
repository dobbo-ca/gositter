package gositter

// tree_cursor.c: the part behind Walk and ChildrenByFieldName.

// TreeCursorEntry. subtree is a slot in the children slab.
type treeCursorEntry struct {
	subtree              uint32
	position             length
	childIndex           uint32
	structuralChildIndex uint32
	descendantIndex      uint32
}

// TSTreeCursor.
type TreeCursor struct {
	tree            *Tree
	stack           []treeCursorEntry
	rootAliasSymbol Symbol
}

// CursorChildIterator
type cursorChildIterator struct {
	tree                 *Tree
	parent               subtree
	position             length
	childIndex           uint32
	structuralChildIndex uint32
	descendantIndex      uint32
	aliasSequence        []Symbol
	children             uint32 // the parent's children, as a slab offset
	childCount           uint32
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

// ts_tree_cursor_is_entry_visible
func (c *TreeCursor) isEntryVisible(index int) bool {
	sp := c.tree.pool
	entry := &c.stack[index]
	t := sp.children[entry.subtree]
	if index == 0 || sp.visible(t) {
		return true
	} else if !sp.extra(t) {
		parent := sp.ptr(c.entrySubtree(index - 1))
		return c.tree.language.aliasAt(uint32(parent.productionID), entry.structuralChildIndex) != 0
	}
	return false
}

// ts_tree_cursor_iterate_children
func (c *TreeCursor) iterateChildren() cursorChildIterator {
	sp := c.tree.pool
	last := len(c.stack) - 1
	lastEntry := &c.stack[last]
	t := sp.children[lastEntry.subtree]
	if sp.childCount(t) == 0 {
		return cursorChildIterator{tree: c.tree}
	}
	h := sp.ptr(t)

	descendantIndex := lastEntry.descendantIndex
	if c.isEntryVisible(last) {
		descendantIndex++
	}

	return cursorChildIterator{
		tree:            c.tree,
		parent:          t,
		position:        lastEntry.position,
		descendantIndex: descendantIndex,
		aliasSequence:   c.tree.language.aliasSequence(uint32(h.productionID)),
		children:        h.children,
		childCount:      h.childCount,
	}
}

// ts_tree_cursor_child_iterator_next
func (it *cursorChildIterator) next(result *treeCursorEntry, visible *bool) bool {
	if it.parent == nullSubtree || it.childIndex == it.childCount {
		return false
	}
	sp := it.tree.pool
	slot := it.children + it.childIndex
	child := sp.children[slot]
	*result = treeCursorEntry{
		subtree:              slot,
		position:             it.position,
		childIndex:           it.childIndex,
		structuralChildIndex: it.structuralChildIndex,
		descendantIndex:      it.descendantIndex,
	}
	*visible = sp.visible(child)
	if !sp.extra(child) {
		if it.aliasSequence != nil {
			*visible = *visible || it.aliasSequence[it.structuralChildIndex] != 0
		}
		it.structuralChildIndex++
	}

	it.descendantIndex += sp.visibleDescendantCount(child)
	if *visible {
		it.descendantIndex++
	}

	it.position = lengthAdd(it.position, sp.size(child))
	it.childIndex++

	if it.childIndex < it.childCount {
		nextChild := sp.children[it.children+it.childIndex]
		it.position = lengthAdd(it.position, sp.padding(nextChild))
	}

	return true
}

// ts_tree_cursor_init
func (c *TreeCursor) init(n Node) {
	c.tree = n.tree
	c.rootAliasSymbol = n.alias()
	c.stack = append(c.stack[:0], treeCursorEntry{
		subtree:  n.id,
		position: length{n.StartByte(), n.StartPosition()},
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

// ts_tree_cursor_goto_first_child_internal
func (c *TreeCursor) gotoFirstChildInternal() int {
	var visible bool
	var entry treeCursorEntry
	iterator := c.iterateChildren()
	for iterator.next(&entry, &visible) {
		if visible {
			c.stack = append(c.stack, entry)
			return treeCursorStepVisible
		}
		if c.tree.pool.visibleChildCount(c.tree.pool.children[entry.subtree]) > 0 {
			c.stack = append(c.stack, entry)
			return treeCursorStepHidden
		}
	}
	return treeCursorStepNone
}

// ts_tree_cursor_goto_first_child
func (c *TreeCursor) GotoFirstChild() bool {
	for {
		switch c.gotoFirstChildInternal() {
		case treeCursorStepHidden:
			continue
		case treeCursorStepVisible:
			return true
		default:
			return false
		}
	}
}

// ts_tree_cursor_goto_sibling_internal, with
// ts_tree_cursor_child_iterator_next as the advance function.
func (c *TreeCursor) gotoNextSiblingInternal() int {
	initialSize := len(c.stack)

	for len(c.stack) > 1 {
		entry := c.stack[len(c.stack)-1]
		c.stack = c.stack[:len(c.stack)-1]
		iterator := c.iterateChildren()
		iterator.childIndex = entry.childIndex
		iterator.structuralChildIndex = entry.structuralChildIndex
		iterator.position = entry.position
		iterator.descendantIndex = entry.descendantIndex

		visible := false
		iterator.next(&entry, &visible)
		if visible && len(c.stack)+1 < initialSize {
			break
		}

		for iterator.next(&entry, &visible) {
			if visible {
				c.stack = append(c.stack, entry)
				return treeCursorStepVisible
			}

			if c.tree.pool.visibleChildCount(c.tree.pool.children[entry.subtree]) != 0 {
				c.stack = append(c.stack, entry)
				return treeCursorStepHidden
			}
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

// ts_tree_cursor_current_node
func (c *TreeCursor) Node() Node {
	sp := c.tree.pool
	last := len(c.stack) - 1
	lastEntry := &c.stack[last]
	isExtra := sp.extra(sp.children[lastEntry.subtree])
	var aliasSymbol Symbol
	if !isExtra {
		aliasSymbol = c.rootAliasSymbol
	}
	if len(c.stack) > 1 && !isExtra {
		parent := sp.ptr(c.entrySubtree(last - 1))
		aliasSymbol = c.tree.language.aliasAt(uint32(parent.productionID), lastEntry.structuralChildIndex)
	}
	return nodeNew(c.tree, lastEntry.subtree, lastEntry.position, aliasSymbol)
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
