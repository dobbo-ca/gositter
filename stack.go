package gositter

// stack.c: the graph-structured stack. Nodes live in a slab and are
// addressed by index; 0 is the NULL node.

import (
	"math"
	"slices"
	"weak"
)

const (
	maxLinkCount     = 8  // MAX_LINK_COUNT
	maxIteratorCount = 64 // MAX_ITERATOR_COUNT

	stackVersionNone = ^uint32(0) // STACK_VERSION_NONE
)

// StackLink. Field order differs from C: 16 bytes instead of 24.
type stackLink struct {
	subtree   subtree
	node      uint32
	isPending bool
}

// StackNode. 48 bytes: most nodes have one link, so links 1 to 7 are kept
// in stack.moreLinks. Use stack.link for any link but links[0]. A walk down a
// long stack is bound by memory, and this is a third of the C node.
type stackNode struct {
	state             StateID
	linkCount         uint16
	refCount          uint32
	errorCost         uint32
	nodeCount         uint32
	dynamicPrecedence int32
	position          length
	links             [1]stackLink
}

// StackIterator
type stackIterator struct {
	node         uint32
	subtrees     subtreeArray
	subtreeCount uint32
	isPending    bool
	path         uint32 // index into stack.forks, popAll only
}

// Where a path of popAll left the path it was copied from: at node, by
// links[link]. Entry 0 is the path that starts at the head.
type stackFork struct {
	parent uint32
	node   uint32
	link   uint16
}

// StackStatus
const (
	stackStatusActive uint8 = iota
	stackStatusPaused
	stackStatusHalted
)

// StackSummaryEntry
type stackSummaryEntry struct {
	position length
	depth    uint32
	state    StateID
}

// StackHead. summary is nil where C has NULL; a recorded summary always has
// at least the entry for the head node.
type stackHead struct {
	node                 uint32
	summary              []stackSummaryEntry
	nodeCountAtLastError uint32
	lastExternalToken    subtree
	lookaheadWhenPaused  subtree
	status               uint8
}

// StackSlice
type stackSlice struct {
	subtrees subtreeArray
	version  uint32
}

// StackAction
const (
	stackActionNone = 0
	stackActionStop = 1
	stackActionPop  = 2
)

// The StackCallback functions. C passes a function pointer and a payload;
// here iter switches on the callback id and the payload sits in the stack.
const (
	cbPopCount uint8 = iota
	cbPopPending
	cbPopError
	cbSummarize
)

// Stack
type stack struct {
	heads     []stackHead
	slices    []stackSlice
	iterators []stackIterator
	forks     []stackFork // popAll
	chain     []uint32    // popAllSubtrees scratch
	nodes     []stackNode // [0] unused
	// Links 1 to 7 of the nodes that have them: moreLinks[more[node]], where
	// 0 means none yet. A node index keeps its entry until reset.
	more      []uint32
	moreLinks []moreLinkSet // [0] unused
	nodePool  []uint32      // node_pool
	baseNode  uint32

	// The boxes of nodes and moreLinks for the free lists, and the slabs
	// this parse outgrew. A pointer taken before a growth may still read
	// those, so they go to the free lists at reset. Weak: a GC in the parse
	// frees them, as garbage.
	nodesBox *[]stackNode
	linksBox *[]moreLinkSet
	nodesOut []weak.Pointer[[]stackNode]
	linksOut []weak.Pointer[[]moreLinkSet]
	sp       *subtreePool

	// Callback payloads.
	goalSubtreeCount uint32                // pop_count_callback
	foundError       bool                  // pop_error_callback
	summary          []stackSummaryEntry   // SummarizeStackSession
	maxDepth         uint32                // SummarizeStackSession
	summaryPool      [][]stackSummaryEntry // freed summaries, for reuse
}

// links[i] of node self, for i < linkCount.
func (s *stack) link(self uint32, i int) *stackLink {
	if i == 0 {
		return &s.nodes[self].links[0]
	}
	return &s.moreLinks[s.more[self]][i-1]
}

// The slot of links[i], i > 0, for writing link i.
func (s *stack) moreLink(self uint32, i int) *stackLink {
	if uint32(len(s.more)) <= self {
		s.more = append(s.more, make([]uint32, int(self)+1-len(s.more))...)
	}
	if s.more[self] == 0 {
		s.more[self] = uint32(len(s.moreLinks))
		// nodeAddLink made room: growLinks keeps the old slab.
		s.moreLinks = append(s.moreLinks, moreLinkSet{})
	}
	return &s.moreLinks[s.more[self]][i-1]
}

// stack_node_retain
func (s *stack) nodeRetain(n uint32) {
	if n == 0 {
		return
	}
	s.nodes[n].refCount++
}

// stack_node_release
func (s *stack) nodeRelease(self uint32) {
	for {
		n := &s.nodes[self]
		n.refCount--
		if n.refCount > 0 {
			return
		}

		firstPredecessor := uint32(0)
		if n.linkCount > 0 {
			for i := int(n.linkCount) - 1; i > 0; i-- {
				link := *s.link(self, i)
				if link.subtree != nullSubtree {
					s.sp.release(link.subtree)
				}
				s.nodeRelease(link.node)
			}
			link := n.links[0]
			if link.subtree != nullSubtree {
				s.sp.release(link.subtree)
			}
			firstPredecessor = link.node
		}

		// C keeps at most MAX_NODE_POOL_SIZE nodes and frees the rest.
		s.nodePool = append(s.nodePool, self)

		if firstPredecessor == 0 {
			return
		}
		self = firstPredecessor
	}
}

// The slab doubles, as the subtree slabs do: append grows a large slice by a
// quarter, which left four times a deep stack behind as garbage.
func (s *stack) growNodes() {
	n := len(s.nodes)
	if n > math.MaxUint32/2 {
		panic(errSlabFull)
	}
	if s.nodesBox == nil {
		s.nodesBox = new([]stackNode)
	}
	*s.nodesBox = s.nodes
	s.nodesOut = append(s.nodesOut, weak.Make(s.nodesBox))
	if s.nodesBox = nodeSlabs.get(2*n, shrinkNodes); s.nodesBox == nil {
		s.nodesBox = new([]stackNode)
		*s.nodesBox = make([]stackNode, 0, 2*n)
	}
	s.nodes = append(*s.nodesBox, s.nodes...)
}

func (s *stack) growLinks() {
	if s.linksBox == nil {
		s.linksBox = new([]moreLinkSet)
	}
	*s.linksBox = s.moreLinks
	s.linksOut = append(s.linksOut, weak.Make(s.linksBox))
	n := len(s.moreLinks)
	if s.linksBox = linkSlabs.get(2*n, shrinkLinks); s.linksBox == nil {
		s.linksBox = linkSlabs.get(n+1, shrinkLinks)
	}
	if s.linksBox != nil {
		s.moreLinks = append(*s.linksBox, s.moreLinks...)
		return
	}
	// append's growth, as main: doubling held a third more for good
	s.moreLinks = slices.Grow(s.moreLinks, 1)
	s.linksBox = new([]moreLinkSet)
}

// stack__subtree_node_count
func (s *stack) subtreeNodeCount(t subtree) uint32 {
	count := s.sp.visibleDescendantCount(t)
	if s.sp.visible(t) {
		count++
	}

	// Count intermediate error nodes even though they are not visible: the
	// node count measures progress since the last error.
	if s.sp.symbol(t) == SymbolErrorRepeat {
		count++
	}

	return count
}

// stack_node_new
func (s *stack) nodeNew(previousNode uint32, t subtree, isPending bool, state StateID) uint32 {
	var i uint32
	if n := len(s.nodePool); n > 0 {
		i = s.nodePool[n-1]
		s.nodePool = s.nodePool[:n-1]
	} else {
		i = uint32(len(s.nodes))
		if len(s.nodes) == cap(s.nodes) {
			s.growNodes()
		}
		s.nodes = s.nodes[:i+1]
	}
	node := &s.nodes[i]
	node.refCount = 1
	node.state = state

	// C zeroes the whole node. Only links[0] is ever read past linkCount (by
	// errorCost), so the other seven links keep their stale contents.
	if previousNode == 0 {
		node.linkCount = 0
		node.links[0] = stackLink{}
		node.position = length{}
		node.errorCost = 0
		node.dynamicPrecedence = 0
		node.nodeCount = 0
	} else {
		prev := &s.nodes[previousNode]
		node.linkCount = 1
		node.links[0] = stackLink{node: previousNode, subtree: t, isPending: isPending}

		node.position = prev.position
		node.errorCost = prev.errorCost
		node.dynamicPrecedence = prev.dynamicPrecedence
		node.nodeCount = prev.nodeCount

		// ts_subtree_error_cost, ts_subtree_total_size,
		// stack__subtree_node_count and ts_subtree_dynamic_precedence, with
		// the subtree decoded once.
		if t.isInline() {
			if t&inlIsMissing != 0 {
				node.errorCost += errorCostPerMissingTree + errorCostPerRecovery
			}
			node.position = lengthAdd(node.position, s.sp.totalSize(t))
			if t&inlVisible != 0 {
				node.nodeCount++
			}
		} else if t != nullSubtree {
			h := s.sp.ptr(t)
			if h.flags&fIsMissing != 0 {
				node.errorCost += errorCostPerMissingTree + errorCostPerRecovery
			} else {
				node.errorCost += h.errorCost
			}
			node.position = lengthAdd(node.position, lengthAdd(h.padding, h.size))
			if h.childCount > 0 {
				node.nodeCount += h.visibleDescendantCount
				node.dynamicPrecedence += h.dynamicPrecedence
			}
			if h.flags&fVisible != 0 {
				node.nodeCount++
			}
			if h.symbol == SymbolErrorRepeat {
				node.nodeCount++
			}
		}
	}

	return i
}

// stack__subtree_is_equivalent
func (s *stack) subtreeIsEquivalent(left, right subtree) bool {
	sp := s.sp
	if left == right {
		return true
	}
	if left == nullSubtree || right == nullSubtree {
		return false
	}

	// Symbols must match.
	if sp.symbol(left) != sp.symbol(right) {
		return false
	}

	// If both have errors, don't bother keeping both.
	if sp.errorCost(left) > 0 && sp.errorCost(right) > 0 {
		return true
	}

	return sp.padding(left).bytes == sp.padding(right).bytes &&
		sp.size(left).bytes == sp.size(right).bytes &&
		sp.childCount(left) == sp.childCount(right) &&
		sp.extra(left) == sp.extra(right) &&
		sp.externalScannerStateEq(left, right)
}

// stack_node_add_link. Allocates no node, so pointers into s.nodes hold.
// moreLinks can move, but only when a link is added, and no link pointer is
// used after that.
func (s *stack) nodeAddLink(self uint32, link stackLink) {
	if link.node == self {
		return
	}
	sp := s.sp
	n := &s.nodes[self]
	linkNode := &s.nodes[link.node]

	for i := 0; i < int(n.linkCount); i++ {
		existingLink := s.link(self, i)
		if s.subtreeIsEquivalent(existingLink.subtree, link.subtree) {
			// Two links between the same pair of nodes: the ambiguity can be
			// removed ahead of time.
			if existingLink.node == link.node {
				if sp.dynamicPrecedence(link.subtree) > sp.dynamicPrecedence(existingLink.subtree) {
					sp.retain(link.subtree)
					sp.release(existingLink.subtree)
					existingLink.subtree = link.subtree
					n.dynamicPrecedence = linkNode.dynamicPrecedence + sp.dynamicPrecedence(link.subtree)
				}
				return
			}

			// If the previous nodes are mergeable, merge them recursively.
			existing := existingLink.node
			existingNode := &s.nodes[existing]
			if existingNode.state == linkNode.state &&
				existingNode.position.bytes == linkNode.position.bytes &&
				existingNode.errorCost == linkNode.errorCost {
				for j := 0; j < int(linkNode.linkCount); j++ {
					s.nodeAddLink(existing, *s.link(link.node, j))
				}
				dynamicPrecedence := linkNode.dynamicPrecedence
				if link.subtree != nullSubtree {
					dynamicPrecedence += sp.dynamicPrecedence(link.subtree)
				}
				if dynamicPrecedence > n.dynamicPrecedence {
					n.dynamicPrecedence = dynamicPrecedence
				}
				return
			}
		}
	}

	if n.linkCount == maxLinkCount {
		return
	}

	s.nodeRetain(link.node)
	nodeCount := linkNode.nodeCount
	dynamicPrecedence := linkNode.dynamicPrecedence
	if n.linkCount == 0 {
		n.links[0] = link
	} else {
		// Here so that moreLink stays inlined.
		if len(s.moreLinks) == cap(s.moreLinks) && (uint32(len(s.more)) <= self || s.more[self] == 0) {
			s.growLinks()
		}
		*s.moreLink(self, int(n.linkCount)) = link
	}
	n.linkCount++

	if link.subtree != nullSubtree {
		sp.retain(link.subtree)
		nodeCount += s.subtreeNodeCount(link.subtree)
		dynamicPrecedence += sp.dynamicPrecedence(link.subtree)
	}

	if nodeCount > n.nodeCount {
		n.nodeCount = nodeCount
	}
	if dynamicPrecedence > n.dynamicPrecedence {
		n.dynamicPrecedence = dynamicPrecedence
	}
}

func (s *stack) summaryFree(summary []stackSummaryEntry) {
	if summary != nil {
		s.summaryPool = append(s.summaryPool, summary[:0])
	}
}

// stack_head_delete
func (s *stack) headDelete(h *stackHead) {
	if h.node != 0 {
		if h.lastExternalToken != nullSubtree {
			s.sp.release(h.lastExternalToken)
		}
		if h.lookaheadWhenPaused != nullSubtree {
			s.sp.release(h.lookaheadWhenPaused)
		}
		s.summaryFree(h.summary)
		h.summary = nil
		s.nodeRelease(h.node)
	}
}

// ts_stack__add_version
func (s *stack) addVersion(originalVersion uint32, node uint32) uint32 {
	head := stackHead{
		node:                 node,
		nodeCountAtLastError: s.heads[originalVersion].nodeCountAtLastError,
		lastExternalToken:    s.heads[originalVersion].lastExternalToken,
		status:               stackStatusActive,
		lookaheadWhenPaused:  nullSubtree,
	}
	s.heads = append(s.heads, head)
	s.nodeRetain(node)
	if head.lastExternalToken != nullSubtree {
		s.sp.retain(head.lastExternalToken)
	}
	return uint32(len(s.heads) - 1)
}

// ts_stack__add_slice
func (s *stack) addSlice(originalVersion uint32, node uint32, subtrees subtreeArray) {
	for i := len(s.slices) - 1; i >= 0; i-- {
		version := s.slices[i].version
		if s.heads[version].node == node {
			s.slices = append(s.slices, stackSlice{})
			copy(s.slices[i+2:], s.slices[i+1:])
			s.slices[i+1] = stackSlice{subtrees, version}
			return
		}
	}

	version := s.addVersion(originalVersion, node)
	s.slices = append(s.slices, stackSlice{subtrees, version})
}

// The StackCallback functions but pop_all_callback, which popAll has inline.
func (s *stack) callback(cb uint8, iterator *stackIterator) uint32 {
	switch cb {
	case cbPopCount: // pop_count_callback
		if iterator.subtreeCount == s.goalSubtreeCount {
			return stackActionPop | stackActionStop
		}
		return stackActionNone

	case cbPopPending: // pop_pending_callback
		if iterator.subtreeCount >= 1 {
			if iterator.isPending {
				return stackActionPop | stackActionStop
			}
			return stackActionStop
		}
		return stackActionNone

	case cbPopError: // pop_error_callback
		if iterator.subtrees.size > 0 {
			if !s.foundError && s.sp.isError(s.sp.children[iterator.subtrees.off]) {
				s.foundError = true
				return stackActionPop | stackActionStop
			}
			return stackActionStop
		}
		return stackActionNone

	default: // summarize_stack_callback
		if iterator.subtreeCount > s.maxDepth {
			return stackActionStop
		}
		s.summarize(&s.nodes[iterator.node], iterator.subtreeCount)
		return stackActionNone
	}
}

// The part of summarize_stack_callback below its depth test.
func (s *stack) summarize(node *stackNode, depth uint32) {
	state := node.state
	for i := len(s.summary) - 1; i >= 0; i-- {
		entry := &s.summary[i]
		if entry.depth < depth {
			break
		}
		if entry.depth == depth && entry.state == state {
			return
		}
	}
	s.summary = append(s.summary, stackSummaryEntry{
		position: node.position,
		depth:    depth,
		state:    state,
	})
}

// stack__iter. The returned slice is s.slices and is overwritten by the next
// call, as in C.
func (s *stack) iter(version uint32, cb uint8, goalSubtreeCount int) []stackSlice {
	sp := s.sp
	s.slices = s.slices[:0]
	s.iterators = s.iterators[:0]

	newIterator := stackIterator{
		node:      s.heads[version].node,
		isPending: true,
	}

	includeSubtrees := false
	if goalSubtreeCount >= 0 {
		includeSubtrees = true
		// C reserves room for the node's heap data too. That lives in
		// another slab here, and a region at the end of the children slab
		// grows in place, so the goal count is enough.
		sp.arrayReserve(&newIterator.subtrees, uint32(goalSubtreeCount))
	}

	s.iterators = append(s.iterators, newIterator)

	// The common pop: one iterator walking an unbranched chain. Each turn is
	// one turn of the general loop below with no action and one link.
	if cb == cbPopCount {
		it := &s.iterators[0]
		for it.subtreeCount != s.goalSubtreeCount {
			node := &s.nodes[it.node]
			if node.linkCount != 1 {
				break
			}
			link := node.links[0]
			it.node = link.node
			if link.subtree != nullSubtree {
				sp.arrayPush(&it.subtrees, link.subtree)
				sp.retain(link.subtree)
				if !sp.extra(link.subtree) {
					it.subtreeCount++
					if !link.isPending {
						it.isPending = false
					}
				}
			} else {
				it.subtreeCount++
				it.isPending = false
			}
		}
	} else if cb == cbSummarize {
		// The same for the summary walk, which passes every extra on the
		// chain: after many errors in a row that is most of the stack.
		it := &s.iterators[0]
		for it.subtreeCount <= s.maxDepth {
			node := &s.nodes[it.node]
			if node.linkCount != 1 {
				break
			}
			s.summarize(node, it.subtreeCount)
			link := &node.links[0]
			it.node = link.node
			if link.subtree == nullSubtree {
				it.subtreeCount++
				it.isPending = false
			} else if !sp.extra(link.subtree) {
				it.subtreeCount++
				if !link.isPending {
					it.isPending = false
				}
			}
		}
	}

	for len(s.iterators) > 0 {
		for i, size := 0, len(s.iterators); i < size; i++ {
			iterator := &s.iterators[i]
			nodeIndex := iterator.node
			node := &s.nodes[nodeIndex]

			action := s.callback(cb, iterator)
			shouldPop := action&stackActionPop != 0
			shouldStop := action&stackActionStop != 0 || node.linkCount == 0

			if shouldPop {
				subtrees := iterator.subtrees
				if !shouldStop {
					subtrees = sp.arrayCopy(subtrees)
				}
				subtreeSliceReverse(sp.array(subtrees))
				s.addSlice(version, nodeIndex, subtrees)
			}

			if shouldStop {
				if !shouldPop {
					sp.arrayDelete(&iterator.subtrees)
				}
				s.iterators = append(s.iterators[:i], s.iterators[i+1:]...)
				i--
				size--
				continue
			}

			for j := uint16(1); j <= node.linkCount; j++ {
				var nextIterator *stackIterator
				var link stackLink
				if j == node.linkCount {
					link = node.links[0]
					nextIterator = &s.iterators[i]
				} else {
					if len(s.iterators) >= maxIteratorCount {
						continue
					}
					link = *s.link(nodeIndex, int(j))
					s.iterators = append(s.iterators, s.iterators[i])
					nextIterator = &s.iterators[len(s.iterators)-1]
					nextIterator.subtrees = sp.arrayCopy(nextIterator.subtrees)
				}

				nextIterator.node = link.node
				if link.subtree != nullSubtree {
					if includeSubtrees {
						sp.arrayPush(&nextIterator.subtrees, link.subtree)
						sp.retain(link.subtree)
					}

					if !sp.extra(link.subtree) {
						nextIterator.subtreeCount++
						if !link.isPending {
							nextIterator.isPending = false
						}
					}
				} else {
					nextIterator.subtreeCount++
					nextIterator.isPending = false
				}
			}
		}
	}

	return s.slices
}

// ts_stack_new, minus the allocations.
func (s *stack) init(sp *subtreePool) {
	s.sp = sp
	s.reset()
}

// Start of a parse: drop every node and head without walking them. The
// subtree slabs are reset at the same time, so no release is owed.
func (s *stack) reset() {
	for i := range s.heads {
		s.summaryFree(s.heads[i].summary)
		s.heads[i].summary = nil
	}
	s.heads = s.heads[:0]
	s.slices = s.slices[:0]
	s.iterators = s.iterators[:0]
	for _, w := range s.nodesOut {
		nodeSlabs.put(w.Value())
	}
	for _, w := range s.linksOut {
		linkSlabs.put(w.Value())
	}
	s.nodesOut, s.linksOut = s.nodesOut[:0], s.linksOut[:0]
	// len 2: nothing was parsed since the last reset
	n := len(s.nodes)
	parsed := n > 2
	var now uint64
	if parsed {
		now = resets.Add(1)
		linkSlabs.age(now)
	}
	if parsed && shrinkSlab(cap(s.nodes), n, shrinkNodes) {
		*s.nodesBox = s.nodes
		nodeSlabs.put(s.nodesBox)
		if s.nodesBox = nodeSlabs.get(64, shrinkNodes); s.nodesBox == nil {
			s.nodesBox = new([]stackNode)
		}
		s.nodes = *s.nodesBox
		if shrinkSlab(cap(s.nodePool), n, shrinkIndexes) {
			s.nodePool = nil
		}
		if shrinkSlab(cap(s.more), len(s.more), shrinkIndexes) {
			s.more = nil
		}
	}
	if parsed && shrinkSlab(cap(s.moreLinks), len(s.moreLinks), shrinkLinks) {
		*s.linksBox = s.moreLinks
		linkSlabs.keep(s.linksBox, now)
		if s.linksBox = linkSlabs.get(16, shrinkLinks); s.linksBox == nil {
			s.linksBox = new([]moreLinkSet)
		}
		s.moreLinks = *s.linksBox
	}
	if cap(s.nodes) == 0 {
		s.nodes = make([]stackNode, 1, 64)
	}
	s.nodes = s.nodes[:1]
	s.more = s.more[:0]
	s.moreLinks = append(s.moreLinks[:0], moreLinkSet{})
	s.nodePool = s.nodePool[:0]
	s.baseNode = s.nodeNew(0, nullSubtree, false, 1)
	s.clear()
}

// ts_stack_version_count
func (s *stack) versionCount() uint32 {
	return uint32(len(s.heads))
}

// ts_stack_state
func (s *stack) state(version uint32) StateID {
	return s.nodes[s.heads[version].node].state
}

// ts_stack_position
func (s *stack) position(version uint32) length {
	return s.nodes[s.heads[version].node].position
}

// ts_stack_last_external_token
func (s *stack) lastExternalToken(version uint32) subtree {
	return s.heads[version].lastExternalToken
}

// ts_stack_set_last_external_token
func (s *stack) setLastExternalToken(version uint32, token subtree) {
	head := &s.heads[version]
	if token != nullSubtree {
		s.sp.retain(token)
	}
	if head.lastExternalToken != nullSubtree {
		s.sp.release(head.lastExternalToken)
	}
	head.lastExternalToken = token
}

// ts_stack_error_cost
func (s *stack) errorCost(version uint32) uint32 {
	head := &s.heads[version]
	node := &s.nodes[head.node]
	result := node.errorCost
	if head.status == stackStatusPaused ||
		(node.state == errorState && node.links[0].subtree == nullSubtree) {
		result += errorCostPerRecovery
	}
	return result
}

// ts_stack_node_count_since_error
func (s *stack) nodeCountSinceError(version uint32) uint32 {
	head := &s.heads[version]
	nodeCount := s.nodes[head.node].nodeCount
	if nodeCount < head.nodeCountAtLastError {
		head.nodeCountAtLastError = nodeCount
	}
	return nodeCount - head.nodeCountAtLastError
}

// ts_stack_push
func (s *stack) push(version uint32, t subtree, pending bool, state StateID) {
	newNode := s.nodeNew(s.heads[version].node, t, pending, state)
	head := &s.heads[version]
	if t == nullSubtree {
		head.nodeCountAtLastError = s.nodes[newNode].nodeCount
	}
	head.node = newNode
}

// ts_stack_pop_count
func (s *stack) popCount(version uint32, count uint32) []stackSlice {
	s.goalSubtreeCount = count
	return s.iter(version, cbPopCount, int(count))
}

// ts_stack_pop_count for a head that owns an unbranched chain: every node
// down to the goal has one link and one owner. Then the general pop yields
// one slice, and once the caller has moved the new version over this one,
// exactly these nodes have been freed. So the subtrees move to the slice
// without retain and release and the nodes go back to the pool.
//
// popLinear does all of that except moving the head: the caller checks that
// the new version cannot merge and then either sets the head to below or
// calls undoPopLinear. trailing counts the extras on top of the stack, which
// ts_subtree_array_remove_trailing_extras would find at the end of the
// slice. Reports false, with nothing changed, if the chain is not of that
// shape.
func (s *stack) popLinear(version uint32, count uint32) (subtrees subtreeArray, trailing, below uint32, ok bool) {
	sp := s.sp
	mark, poolMark := len(sp.children), len(s.nodePool)
	below = s.heads[version].node
	for counted := uint32(0); counted < count; {
		node := &s.nodes[below]
		link := &node.links[0]
		if node.linkCount != 1 || node.refCount != 1 || link.subtree == nullSubtree {
			sp.children = sp.children[:mark]
			s.nodePool = s.nodePool[:poolMark]
			return subtreeArray{}, 0, 0, false
		}
		if !sp.extra(link.subtree) {
			counted++
		} else if counted == 0 {
			trailing++
		}
		sp.children[sp.bumpChildren(sp.room(1), 1)] = link.subtree
		s.nodePool = append(s.nodePool, below)
		below = link.node
	}
	if n := uint32(len(sp.children) - mark); n > 0 {
		subtrees = subtreeArray{off: uint32(mark), size: n, cap: n}
		subtreeSliceReverse(sp.children[mark:])
	}
	return subtrees, trailing, below, true
}

// undoPopLinear takes back a popLinear whose head was not moved.
func (s *stack) undoPopLinear(subtrees subtreeArray) {
	if subtrees.size > 0 {
		s.sp.children = s.sp.children[:subtrees.off]
	}
	s.nodePool = s.nodePool[:uint32(len(s.nodePool))-subtrees.size]
}

// ts_stack_pop_pending
func (s *stack) popPending(version uint32) []stackSlice {
	pop := s.iter(version, cbPopPending, 0)
	if len(pop) > 0 {
		s.renumberVersion(pop[0].version, version)
		pop[0].version = version
	}
	return pop
}

// ts_stack_pop_error
func (s *stack) popError(version uint32) subtreeArray {
	node := &s.nodes[s.heads[version].node]
	for i := 0; i < int(node.linkCount); i++ {
		if t := s.link(s.heads[version].node, i).subtree; t != nullSubtree && s.sp.isError(t) {
			s.foundError = false
			pop := s.iter(version, cbPopError, 1)
			if len(pop) > 0 {
				s.renumberVersion(pop[0].version, version)
				return pop[0].subtrees
			}
			break
		}
	}
	return subtreeArray{}
}

// ts_stack_pop_all, in two steps. C hands back every slice with its own copy
// of the subtrees, up to 64 arrays as long as the stack; after an error near
// the start of a large input they were most of the memory of the parse.
// Here the walk keeps no subtrees and records where each path forked;
// popAllSubtrees walks one path again, so accept holds one array at a time.
//
// The loop is ts_stack__iter with pop_all_callback and no subtrees. A slice
// names its path in subtrees.off.
func (s *stack) popAll(version uint32) []stackSlice {
	s.slices = s.slices[:0]
	s.forks = append(s.forks[:0], stackFork{})
	s.iterators = append(s.iterators[:0], stackIterator{node: s.heads[version].node})

	for len(s.iterators) > 0 {
		for i, size := 0, len(s.iterators); i < size; i++ {
			nodeIndex := s.iterators[i].node
			node := &s.nodes[nodeIndex]

			if node.linkCount == 0 {
				s.addSlice(version, nodeIndex, subtreeArray{off: s.iterators[i].path})
				s.iterators = append(s.iterators[:i], s.iterators[i+1:]...)
				i--
				size--
				continue
			}

			for j := uint16(1); j <= node.linkCount; j++ {
				if j == node.linkCount {
					s.iterators[i].node = node.links[0].node
				} else {
					if len(s.iterators) >= maxIteratorCount {
						continue
					}
					s.forks = append(s.forks, stackFork{s.iterators[i].path, nodeIndex, j})
					s.iterators = append(s.iterators, stackIterator{
						node: s.link(nodeIndex, int(j)).node,
						path: uint32(len(s.forks) - 1),
					})
				}
			}
		}
	}

	return s.slices
}

// The subtrees of one slice of popAll, as ts_stack__iter collects them. The
// stack must not have changed since popAll.
func (s *stack) popAllSubtrees(version uint32, slice stackSlice) subtreeArray {
	sp := s.sp
	s.chain = s.chain[:0]
	for path := slice.subtrees.off; path != 0; path = s.forks[path].parent {
		s.chain = append(s.chain, path)
	}

	var subtrees subtreeArray
	nodeIndex := s.heads[version].node
	for node := &s.nodes[nodeIndex]; node.linkCount != 0; node = &s.nodes[nodeIndex] {
		link := node.links[0]
		if n := len(s.chain); n > 0 && s.forks[s.chain[n-1]].node == nodeIndex {
			link = *s.link(nodeIndex, int(s.forks[s.chain[n-1]].link))
			s.chain = s.chain[:n-1]
		}
		nodeIndex = link.node
		if link.subtree != nullSubtree {
			sp.arrayPush(&subtrees, link.subtree)
			sp.retain(link.subtree)
		}
	}
	subtreeSliceReverse(sp.array(subtrees))
	return subtrees
}

// ts_stack_record_summary
func (s *stack) recordSummary(version uint32, maxDepth uint32) {
	s.summary = nil
	if n := len(s.summaryPool); n > 0 {
		s.summary = s.summaryPool[n-1]
		s.summaryPool = s.summaryPool[:n-1]
	}
	s.maxDepth = maxDepth
	s.iter(version, cbSummarize, -1)
	head := &s.heads[version]
	s.summaryFree(head.summary)
	head.summary = s.summary
	s.summary = nil
}

// ts_stack_get_summary
func (s *stack) getSummary(version uint32) []stackSummaryEntry {
	return s.heads[version].summary
}

// ts_stack_dynamic_precedence
func (s *stack) dynamicPrecedence(version uint32) int32 {
	return s.nodes[s.heads[version].node].dynamicPrecedence
}

// ts_stack_has_advanced_since_error
func (s *stack) hasAdvancedSinceError(version uint32) bool {
	head := &s.heads[version]
	node := &s.nodes[head.node]
	if node.errorCost == 0 {
		return true
	}
	for {
		if node.linkCount > 0 {
			t := node.links[0].subtree
			if t != nullSubtree {
				if s.sp.totalBytes(t) > 0 {
					return true
				} else if node.nodeCount > head.nodeCountAtLastError && s.sp.errorCost(t) == 0 {
					next := node.links[0].node
					if next == 0 {
						break
					}
					node = &s.nodes[next]
					continue
				}
			}
		}
		break
	}
	return false
}

// ts_stack_remove_version
func (s *stack) removeVersion(version uint32) {
	s.headDelete(&s.heads[version])
	s.heads = append(s.heads[:version], s.heads[version+1:]...)
}

// ts_stack_renumber_version
func (s *stack) renumberVersion(v1, v2 uint32) {
	if v1 == v2 {
		return
	}
	sourceHead := &s.heads[v1]
	targetHead := &s.heads[v2]
	if targetHead.summary != nil && sourceHead.summary == nil {
		sourceHead.summary = targetHead.summary
		targetHead.summary = nil
	}
	s.headDelete(targetHead)
	*targetHead = *sourceHead
	s.heads = append(s.heads[:v1], s.heads[v1+1:]...)
}

// ts_stack_swap_versions
func (s *stack) swapVersions(v1, v2 uint32) {
	s.heads[v1], s.heads[v2] = s.heads[v2], s.heads[v1]
}

// ts_stack_copy_version
func (s *stack) copyVersion(version uint32) uint32 {
	s.heads = append(s.heads, s.heads[version])
	head := &s.heads[len(s.heads)-1]
	s.nodeRetain(head.node)
	if head.lastExternalToken != nullSubtree {
		s.sp.retain(head.lastExternalToken)
	}
	head.summary = nil
	return uint32(len(s.heads) - 1)
}

// ts_stack_merge
func (s *stack) merge(version1, version2 uint32) bool {
	if !s.canMerge(version1, version2) {
		return false
	}
	head1 := &s.heads[version1]
	head2 := &s.heads[version2]
	node2 := &s.nodes[head2.node]
	for i := 0; i < int(node2.linkCount); i++ {
		s.nodeAddLink(head1.node, *s.link(head2.node, i))
	}
	if node1 := &s.nodes[head1.node]; node1.state == errorState {
		head1.nodeCountAtLastError = node1.nodeCount
	}
	s.removeVersion(version2)
	return true
}

// ts_stack_can_merge
func (s *stack) canMerge(version1, version2 uint32) bool {
	head1 := &s.heads[version1]
	head2 := &s.heads[version2]
	node1 := &s.nodes[head1.node]
	node2 := &s.nodes[head2.node]
	return head1.status == stackStatusActive &&
		head2.status == stackStatusActive &&
		node1.state == node2.state &&
		node1.position.bytes == node2.position.bytes &&
		node1.errorCost == node2.errorCost &&
		s.sp.externalScannerStateEq(head1.lastExternalToken, head2.lastExternalToken)
}

// ts_stack_halt
func (s *stack) halt(version uint32) {
	s.heads[version].status = stackStatusHalted
}

// ts_stack_pause
func (s *stack) pause(version uint32, lookahead subtree) {
	head := &s.heads[version]
	head.status = stackStatusPaused
	head.lookaheadWhenPaused = lookahead
	head.nodeCountAtLastError = s.nodes[head.node].nodeCount
}

// ts_stack_is_active
func (s *stack) isActive(version uint32) bool {
	return s.heads[version].status == stackStatusActive
}

// ts_stack_is_halted
func (s *stack) isHalted(version uint32) bool {
	return s.heads[version].status == stackStatusHalted
}

// ts_stack_is_paused
func (s *stack) isPaused(version uint32) bool {
	return s.heads[version].status == stackStatusPaused
}

// ts_stack_resume
func (s *stack) resume(version uint32) subtree {
	head := &s.heads[version]
	result := head.lookaheadWhenPaused
	head.status = stackStatusActive
	head.lookaheadWhenPaused = nullSubtree
	return result
}

// ts_stack_clear
func (s *stack) clear() {
	s.nodeRetain(s.baseNode)
	for i := range s.heads {
		s.headDelete(&s.heads[i])
	}
	s.heads = append(s.heads[:0], stackHead{
		node:   s.baseNode,
		status: stackStatusActive,
	})
}
