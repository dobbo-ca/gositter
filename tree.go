package gositter

// tree.c.

// TSTree. It borrows the slabs of the parser that made it.
type Tree struct {
	pool     *subtreePool
	language *Language
	root     uint32 // slot of the root subtree in pool.children
}

// ts_tree_root_node
func (t *Tree) RootNode() Node {
	return nodeNew(t, t.root, t.pool.padding(t.pool.children[t.root]), 0)
}

// ts_tree_language
func (t *Tree) Language() *Language {
	return t.language
}

// ts_tree_delete. The memory belongs to the parser, which reuses it on its
// next Parse, so there is nothing to do.
func (t *Tree) Close() {}
