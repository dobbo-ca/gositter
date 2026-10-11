package gositter

// node.c.

// TSNode. A small comparable value; the zero Node is the null node. id is
// the slot of the subtree in the tree's children slab, where C has a
// const Subtree *.
type Node struct {
	tree    *Tree
	id      uint32
	context [4]uint32 // start byte, start row, start column, alias symbol
}

// NodeChildIterator
type nodeChildIterator struct {
	tree                 *Tree
	parent               subtree
	position             length
	childIndex           uint32
	structuralChildIndex uint32
	aliasSequence        []Symbol
	children             uint32 // the parent's children, as a slab offset
	childCount           uint32
}

// ts_node_new
func nodeNew(tree *Tree, id uint32, position length, alias Symbol) Node {
	return Node{
		tree:    tree,
		id:      id,
		context: [4]uint32{position.bytes, position.extent.Row, position.extent.Column, uint32(alias)},
	}
}

// ts_node__alias
func (n Node) alias() Symbol {
	return Symbol(n.context[3])
}

// ts_node__subtree
func (n Node) subtree() subtree {
	return n.tree.pool.children[n.id]
}

// ts_node_iterate_children
func (n Node) iterateChildren() nodeChildIterator {
	sp := n.tree.pool
	t := n.subtree()
	if sp.childCount(t) == 0 {
		return nodeChildIterator{tree: n.tree}
	}
	h := sp.ptr(t)
	return nodeChildIterator{
		tree:          n.tree,
		parent:        t,
		position:      length{n.context[0], Point{n.context[1], n.context[2]}},
		aliasSequence: n.tree.language.aliasSequence(uint32(h.productionID)),
		children:      h.children,
		childCount:    h.childCount,
	}
}

// ts_node_child_iterator_next
func (it *nodeChildIterator) next(result *Node) bool {
	if it.parent == nullSubtree || it.childIndex == it.childCount {
		return false
	}
	sp := it.tree.pool
	slot := it.children + it.childIndex
	child := sp.children[slot]
	var aliasSymbol Symbol
	if !sp.extra(child) {
		if it.aliasSequence != nil {
			aliasSymbol = it.aliasSequence[it.structuralChildIndex]
		}
		it.structuralChildIndex++
	}
	if it.childIndex > 0 {
		it.position = lengthAdd(it.position, sp.padding(child))
	}
	*result = nodeNew(it.tree, slot, it.position, aliasSymbol)
	it.position = lengthAdd(it.position, sp.size(child))
	it.childIndex++
	return true
}

// ts_node__is_relevant
func (n Node) isRelevant(includeAnonymous bool) bool {
	sp := n.tree.pool
	t := n.subtree()
	if includeAnonymous {
		return sp.visible(t) || n.alias() != 0
	}
	if alias := n.alias(); alias != 0 {
		return n.tree.language.symbolMetadata(alias).Named
	}
	return sp.visible(t) && sp.named(t)
}

// ts_node__relevant_child_count
func (n Node) relevantChildCount(includeAnonymous bool) uint32 {
	sp := n.tree.pool
	t := n.subtree()
	if sp.childCount(t) > 0 {
		if includeAnonymous {
			return sp.ptr(t).visibleChildCount
		}
		return sp.ptr(t).namedChildCount
	}
	return 0
}

// ts_node__child
func (n Node) child(childIndex uint32, includeAnonymous bool) Node {
	result := n
	didDescend := true

	for didDescend {
		didDescend = false

		var child Node
		index := uint32(0)
		iterator := result.iterateChildren()
		for iterator.next(&child) {
			if child.isRelevant(includeAnonymous) {
				if index == childIndex {
					return child
				}
				index++
			} else {
				grandchildIndex := childIndex - index
				grandchildCount := child.relevantChildCount(includeAnonymous)
				if grandchildIndex < grandchildCount {
					didDescend = true
					result = child
					childIndex = grandchildIndex
					break
				}
				index += grandchildCount
			}
		}
	}

	return Node{}
}

// ts_node_is_null
func (n Node) IsNull() bool {
	return n.id == 0
}

// ts_node_language
func (n Node) Language() *Language {
	return n.tree.language
}

// ts_node_type
func (n Node) Kind() string {
	symbol := n.alias()
	if symbol == 0 {
		symbol = n.tree.pool.symbol(n.subtree())
	}
	return n.tree.language.SymbolName(symbol)
}

// ts_node_symbol
func (n Node) Symbol() Symbol {
	symbol := n.alias()
	if symbol == 0 {
		symbol = n.tree.pool.symbol(n.subtree())
	}
	return n.tree.language.publicSymbol(symbol)
}

// ts_node_is_named
func (n Node) IsNamed() bool {
	if alias := n.alias(); alias != 0 {
		return n.tree.language.symbolMetadata(alias).Named
	}
	return n.tree.pool.named(n.subtree())
}

// ts_node_is_error
func (n Node) IsError() bool {
	return n.Symbol() == SymbolError
}

// ts_node_is_missing
func (n Node) IsMissing() bool {
	return n.tree.pool.missing(n.subtree())
}

// ts_node_is_extra
func (n Node) IsExtra() bool {
	return n.tree.pool.extra(n.subtree())
}

// ts_node_has_error
func (n Node) HasError() bool {
	return n.tree.pool.errorCost(n.subtree()) > 0
}

// ts_node_start_byte
func (n Node) StartByte() uint32 {
	return n.context[0]
}

// ts_node_end_byte
func (n Node) EndByte() uint32 {
	return n.context[0] + n.tree.pool.size(n.subtree()).bytes
}

// ts_node_start_point
func (n Node) StartPosition() Point {
	return Point{n.context[1], n.context[2]}
}

// ts_node_end_point
func (n Node) EndPosition() Point {
	return pointAdd(n.StartPosition(), n.tree.pool.size(n.subtree()).extent)
}

// ts_node_child_count
func (n Node) ChildCount() uint32 {
	return n.relevantChildCount(true)
}

// ts_node_child
func (n Node) Child(i uint32) Node {
	return n.child(i, true)
}

// Children appends the children of n to buf: the nodes Child(0) to
// Child(ChildCount()-1), found in one pass. ts_node_child rescans from the
// first child on every call, so a Child(i) loop is quadratic. No C
// counterpart.
func (n Node) Children(buf []Node) []Node {
	return n.appendChildren(buf, true)
}

// NamedChildren is Children for NamedChild.
func (n Node) NamedChildren(buf []Node) []Node {
	return n.appendChildren(buf, false)
}

// The enumeration ts_node__child walks: a child that is not relevant stands
// for its own relevant children.
func (n Node) appendChildren(buf []Node, includeAnonymous bool) []Node {
	var child Node
	iterator := n.iterateChildren()
	for iterator.next(&child) {
		if child.isRelevant(includeAnonymous) {
			buf = append(buf, child)
		} else if child.relevantChildCount(includeAnonymous) > 0 {
			buf = child.appendChildren(buf, includeAnonymous)
		}
	}
	return buf
}

// Preorder calls fn for n and, where fn returns true, for the nodes below
// it in document order: the walk a recursive Children loop makes, with no
// slice per node. No C counterpart.
func (n Node) Preorder(fn func(Node) bool) {
	if fn(n) {
		n.preorderChildren(0, fn)
	}
}

// PreorderMask is Preorder for an fn that returns true and does nothing else
// on a node whose kind is not in mask (see Language.KindMask): fn is called
// for the nodes of those kinds only, and a subtree that holds none of them
// is not entered. No C counterpart.
func (n Node) PreorderMask(mask uint32, fn func(Node) bool) {
	if mask == 0 {
		n.Preorder(fn)
		return
	}
	sp := n.tree.pool
	t := n.subtree()
	symbol := n.alias()
	if symbol == 0 {
		symbol = sp.symbol(t)
	}
	if n.tree.language.kindBit(symbol)&mask != 0 && !fn(n) {
		return
	}
	if sp.childCount(t) > 0 && sp.ptr(t).kinds()&mask != 0 {
		n.preorderChildren(mask, fn)
	}
}

// The loop of ts_node_child_iterator_next with ts_node__is_relevant and
// ts_node__relevant_child_count folded in: each child subtree is decoded
// once, and a leaf costs no iterator. A mask of 0 walks every node.
func (n Node) preorderChildren(mask uint32, fn func(Node) bool) {
	sp := n.tree.pool
	t := n.subtree()
	if t.isInline() {
		return
	}
	h := sp.ptr(t)
	if h.childCount == 0 {
		return
	}
	lang := n.tree.language
	aliasSequence := lang.aliasSequence(uint32(h.productionID))
	position := length{n.context[0], Point{n.context[1], n.context[2]}}
	structuralChildIndex := 0
	for i, child := range sp.children[h.children : h.children+h.childCount] {
		var padding, size length
		var visible, extra, descend, descendHidden bool
		var symbol Symbol
		if child.isInline() {
			padding, size = sp.padding(child), sp.size(child)
			visible, extra = child&inlVisible != 0, child&inlExtra != 0
			symbol = Symbol(uint8(child >> inlSymbolShift))
		} else {
			c := sp.ptr(child)
			padding, size = c.padding, c.size
			visible, extra = c.flags&fVisible != 0, c.flags&fExtra != 0
			symbol = c.symbol
			descend = c.childCount > 0 && (mask == 0 || c.kinds()&mask != 0)
			descendHidden = descend && c.visibleChildCount > 0
		}
		var aliasSymbol Symbol
		if !extra {
			if aliasSequence != nil {
				aliasSymbol = aliasSequence[structuralChildIndex]
			}
			structuralChildIndex++
		}
		if i > 0 {
			position = lengthAdd(position, padding)
		}
		if visible || aliasSymbol != 0 {
			if aliasSymbol != 0 {
				symbol = aliasSymbol
			}
			node := nodeNew(n.tree, h.children+uint32(i), position, aliasSymbol)
			if mask == 0 || lang.kindBit(symbol)&mask != 0 {
				descend = fn(node) && descend
			}
			if descend {
				node.preorderChildren(mask, fn)
			}
		} else if descendHidden {
			nodeNew(n.tree, h.children+uint32(i), position, aliasSymbol).preorderChildren(mask, fn)
		}
		position = lengthAdd(position, size)
	}
}

// ts_node_named_child_count
func (n Node) NamedChildCount() uint32 {
	return n.relevantChildCount(false)
}

// ts_node_named_child
func (n Node) NamedChild(i uint32) Node {
	return n.child(i, false)
}

// ts_node_child_by_field_name
func (n Node) ChildByFieldName(name string) Node {
	return n.ChildByFieldID(n.tree.language.FieldIDForName(name))
}

// ts_node_child_by_field_id
func (n Node) ChildByFieldID(fieldID FieldID) Node {
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
					result := child.ChildByFieldID(fieldID)
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

// ts_node__field_name_from_language
func (n Node) fieldNameFromLanguage(structuralChildIndex uint32) string {
	lang := n.tree.language
	fieldMap := lang.fieldMap(uint32(n.tree.pool.ptr(n.subtree()).productionID))
	for _, m := range fieldMap {
		if !m.Inherited && uint32(m.ChildIndex) == structuralChildIndex {
			return lang.FieldNames[m.FieldID]
		}
	}
	return ""
}

// ts_node_field_name_for_child. Returns "" where C returns NULL.
func (n Node) FieldNameForChild(childIndex uint32) string {
	result := n
	didDescend := true
	inheritedFieldName := ""

	for didDescend {
		didDescend = false

		var child Node
		index := uint32(0)
		iterator := result.iterateChildren()
		for iterator.next(&child) {
			if child.isRelevant(true) {
				if index == childIndex {
					if child.IsExtra() {
						return ""
					}
					fieldName := result.fieldNameFromLanguage(iterator.structuralChildIndex - 1)
					if fieldName != "" {
						return fieldName
					}
					return inheritedFieldName
				}
				index++
			} else {
				grandchildIndex := childIndex - index
				grandchildCount := child.relevantChildCount(true)
				if grandchildIndex < grandchildCount {
					fieldName := result.fieldNameFromLanguage(iterator.structuralChildIndex - 1)
					if fieldName != "" {
						inheritedFieldName = fieldName
					}

					didDescend = true
					result = child
					childIndex = grandchildIndex
					break
				}
				index += grandchildCount
			}
		}
	}

	return ""
}

// ts_node_parent
func (n Node) Parent() Node {
	node := n.tree.RootNode()
	if node.id == n.id {
		return Node{}
	}

	for {
		nextNode := node.childWithDescendant(n)
		if nextNode.id == n.id || nextNode.IsNull() {
			break
		}
		node = nextNode
	}

	return node
}

// ts_node_child_with_descendant
func (n Node) childWithDescendant(descendant Node) Node {
	self := n
	startByte := descendant.StartByte()
	endByte := descendant.EndByte()
	isEmpty := startByte == endByte

	for {
		iter := self.iterateChildren()
		for {
			if !iter.next(&self) || self.StartByte() > startByte {
				return Node{}
			}
			if self.id == descendant.id {
				return self
			}

			// If the descendant is empty, and the end byte is within
			// `self`, we check whether `self` contains it or not.
			if isEmpty && iter.position.bytes >= endByte && self.ChildCount() > 0 {
				child := self.childWithDescendant(descendant)
				// If the child is not null, return self if it's relevant,
				// else return the child.
				if !child.IsNull() {
					if self.isRelevant(true) {
						return self
					}
					return child
				}
			}

			var more bool
			if isEmpty {
				more = iter.position.bytes <= endByte
			} else {
				more = iter.position.bytes < endByte
			}
			if !(more || self.ChildCount() == 0) {
				break
			}
		}
		if self.isRelevant(true) {
			break
		}
	}

	return self
}

// ts_node_eq
func (n Node) Equals(other Node) bool {
	return n.tree == other.tree && n.id == other.id
}

// string(src[StartByte:EndByte]), as go-tree-sitter does.
func (n Node) Utf8Text(src []byte) string {
	return string(src[n.StartByte():n.EndByte()])
}

// ts_node_string
func (n Node) String() string {
	aliasSymbol := n.alias()
	lang := n.tree.language
	return n.tree.pool.string(
		n.subtree(),
		aliasSymbol,
		lang.symbolMetadata(aliasSymbol).Visible,
		lang,
		false,
	)
}

// ts_tree_cursor_new
func (n Node) Walk() *TreeCursor {
	c := &TreeCursor{}
	c.init(n)
	return c
}

// Same loop as go-tree-sitter's Node.ChildrenByFieldName.
func (n Node) ChildrenByFieldName(name string, c *TreeCursor) []Node {
	fieldID := n.Language().FieldIDForName(name)
	done := fieldID == 0
	if !done {
		c.Reset(n)
		c.GotoFirstChild()
	}
	result := make([]Node, 0)
	for !done {
		for c.FieldID() != fieldID {
			if !c.GotoNextSibling() {
				return result
			}
		}
		result = append(result, c.Node())
		if !c.GotoNextSibling() {
			done = true
		}
	}
	return result
}
