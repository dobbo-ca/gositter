package gositter

import (
	"bytes"
	"math"
	"math/bits"
	"strconv"
	"sync"
)

// subtree.c and subtree.h.
//
// A subtree is the 8-byte C Subtree union. Bit 0 set: the inline leaf, with
// the little-endian bit layout of SubtreeInlineData. Bit 0 clear: a handle,
// index<<1 into subtreePool.heap. 0 is NULL_SUBTREE. No Go pointer anywhere,
// so the slabs are invisible to the GC.
type subtree uint64

const nullSubtree subtree = 0

const (
	treeStateNone StateID = 0xFFFF // TS_TREE_STATE_NONE

	maxInlineTreeLength = 255 // TS_MAX_INLINE_TREE_LENGTH

	errorState              = 0   // ERROR_STATE
	errorCostPerRecovery    = 500 // ERROR_COST_PER_RECOVERY
	errorCostPerMissingTree = 110 // ERROR_COST_PER_MISSING_TREE
	errorCostPerSkippedTree = 100 // ERROR_COST_PER_SKIPPED_TREE
	errorCostPerSkippedLine = 30  // ERROR_COST_PER_SKIPPED_LINE
	errorCostPerSkippedChar = 1   // ERROR_COST_PER_SKIPPED_CHAR
)

// SubtreeInlineData bits.
const (
	inlIsInline   = 1 << 0
	inlVisible    = 1 << 1
	inlNamed      = 1 << 2
	inlExtra      = 1 << 3
	inlHasChanges = 1 << 4
	inlIsMissing  = 1 << 5
	inlIsKeyword  = 1 << 6

	inlSymbolShift         = 8  // uint8
	inlParseStateShift     = 16 // uint16
	inlPaddingColumnsShift = 32 // uint8
	inlPaddingRowsShift    = 40 // 4 bits
	inlLookaheadBytesShift = 44 // 4 bits
	inlPaddingBytesShift   = 48 // uint8
	inlSizeBytesShift      = 56 // uint8
)

// SubtreeHeapData bit flags.
const (
	fVisible uint16 = 1 << iota
	fNamed
	fExtra
	fFragileLeft
	fFragileRight
	fHasChanges
	fHasExternalTokens
	fHasExternalScannerStateChange
	fDependsOnColumn
	fIsMissing
	fIsKeyword
	// Not in C: this node or one below it has repeatDepth > 0, so
	// balanceSubtree has work in here.
	fHasRepeat
)

// SubtreeHeapData. The C union is spread out: node fields, then the external
// scanner state (a range of subtreePool.ext) and lookahead_char.
type subtreeHeap struct {
	refCount       uint32
	padding        length
	size           length
	lookaheadBytes uint32
	errorCost      uint32
	childCount     uint32
	symbol         Symbol
	parseState     StateID
	flags          uint16

	// Non-terminal subtrees (childCount > 0).
	//
	// repeatDepth is the one union member C reads without checking the
	// variant (ts_subtree_repeat_depth), so on an external token leaf it
	// holds bytes 16 and 17 of the inline scanner state. See
	// (*Parser).lex.
	repeatDepth            uint16
	productionID           uint16
	firstLeafSymbol        Symbol
	firstLeafParseState    StateID
	childClass             uint8 // size class of the children block
	visibleChildCount      uint32
	namedChildCount        uint32
	visibleDescendantCount uint32
	dynamicPrecedence      int32
	children               uint32 // offset into subtreePool.children

	// External terminal subtrees (childCount == 0 && fHasExternalTokens).
	extOff uint32
	extLen uint32

	// Error terminal subtrees (childCount == 0 && symbol == SymbolError).
	lookaheadChar int32
}

// SubtreeArray: a region of subtreePool.children.
type subtreeArray struct {
	off, size, cap uint32
}

// The heap data slab is a list of chunks: it grows without a copy, so a deep
// tree leaves no outgrown slabs behind for the GC.
const (
	heapChunkBits = 11
	heapChunkMask = 1<<heapChunkBits - 1
)

type heapChunk [1 << heapChunkBits]subtreeHeap

// A reset keeps keptHeapChunks chunks and hands the rest to heapChunks, which
// all parsers share: pooled parsers then hold what the inputs being parsed
// need, not one high-water mark each.
const keptHeapChunks = 8

var heapChunks = sync.Pool{New: func() any { return new(heapChunk) }}

// reserveHeap takes the next chunk from heapChunks, one chunk ahead of need.
// allocate is inlined into the reduce path and a call there would end that,
// so it only makes new chunks, when a round of advances outruns this.
func (sp *subtreePool) reserveHeap() {
	// The last possible chunk is left to allocate, which reports a full slab.
	if sp.heapLen>>heapChunkBits+1 >= uint32(len(sp.heap)) && len(sp.heap) < math.MaxUint32>>heapChunkBits {
		sp.heap = append(sp.heap, heapChunks.Get().(*heapChunk))
	}
}

// SubtreePool, and the arenas behind every subtree of one parser. Slabs are
// reset, not freed, between parses.
type subtreePool struct {
	heap      []*heapChunk // entry [0] unused
	heapLen   uint32       // entries handed out, [0] included
	freeTrees []uint32     // free_trees
	children  []subtree    // [0] unused
	ext       []byte       // external scanner states
	treeStack []subtree    // tree_stack

	// Free children blocks, as in a malloc with boundary tags. A block of
	// any size sits in the list of the largest class that fits in it. Its
	// first entry holds the next block and, in the high half, the previous
	// one; its second and its last entry hold its size, unless that is 1.
	// freeBits has two bits per slab entry: a free block starts here, a free
	// block ends here. A block freed next to another one merges with it.
	freeBlocks [childClassCount]uint32
	freeLists  [3]uint64 // the non-empty lists
	freeBits   []uint64

	// Freed blocks of up to 64 entries wait here, by size, linked through
	// their first entry, and serve the next request of their size as they
	// are. flushQuick moves them to freeBlocks.
	quickBlocks [exactChildClasses + 1]uint32

	// Entries in quickBlocks and in freeBlocks. reduceInPlace takes free
	// blocks once they are a quarter of the slab.
	quickFree, mergedFree uint32
}

// Children blocks are handed out in size classes: one class per size up to
// 64, then four per doubling. Free blocks merge and split, so what an array
// leaves behind as it outgrows one class after another serves it again.
const (
	exactChildClasses = 64
	childClassCount   = 165
)

// Offsets and indexes into the slabs are 32 bits.
const errSlabFull = "gositter: input needs a parser slab of more than 2^32 entries"

// childClass returns the class of the smallest block that holds n entries,
// and the size of that block.
func childClass(n uint32) (class, size uint32) {
	if n <= exactChildClasses {
		return n, n
	}
	if n > 1<<31 {
		panic(errSlabFull)
	}
	shift := uint32(bits.Len32(n-1)) - 3
	m := (n-1)>>shift + 1 // 5 to 8
	return shift*4 + m + 44, m << shift
}

// childClassSize is the block size of a class above exactChildClasses.
func childClassSize(class uint32) uint32 {
	shift := (class - 49) / 4
	return (class - 44 - shift*4) << shift
}

func (sp *subtreePool) reset() {
	if cap(sp.children) == 0 {
		sp.heap = []*heapChunk{new(heapChunk)}
		sp.children = make([]subtree, 1, 4096)
	}
	sp.heapLen = 1
	if len(sp.heap) > keptHeapChunks {
		for i, c := range sp.heap[keptHeapChunks:] {
			heapChunks.Put(c)
			sp.heap[keptHeapChunks+i] = nil
		}
		sp.heap = sp.heap[:keptHeapChunks]
	}
	clear(sp.freeBits[:min(len(sp.freeBits), (len(sp.children)+31)/32)])
	sp.children = sp.children[:1]
	sp.freeBlocks = [childClassCount]uint32{}
	sp.freeLists = [3]uint64{}
	sp.quickBlocks = [exactChildClasses + 1]uint32{}
	sp.quickFree, sp.mergedFree = 0, 0
	sp.freeTrees = sp.freeTrees[:0]
	sp.ext = sp.ext[:0]
	sp.treeStack = sp.treeStack[:0]
}

func (s subtree) isInline() bool { return s&inlIsInline != 0 }

func (sp *subtreePool) ptr(s subtree) *subtreeHeap {
	return &sp.heap[s>>(1+heapChunkBits)][s>>1&heapChunkMask]
}

// ts_subtree_pool_allocate
func (sp *subtreePool) allocate() uint32 {
	if n := len(sp.freeTrees); n > 0 {
		i := sp.freeTrees[n-1]
		sp.freeTrees = sp.freeTrees[:n-1]
		return i
	}
	// Every caller overwrites the whole entry, so a reused slot is not
	// zeroed.
	n := sp.heapLen
	if n>>heapChunkBits == uint32(len(sp.heap)) {
		if n == math.MaxUint32&^heapChunkMask { // the last chunk would wrap heapLen
			panic(errSlabFull)
		}
		sp.heap = append(sp.heap, new(heapChunk))
	}
	sp.heapLen = n + 1
	return n
}

// ts_subtree_pool_free. C caps the pool at TS_MAX_TREE_POOL_SIZE and frees
// the rest; a slab index is always reusable.
func (sp *subtreePool) free(i uint32) {
	sp.freeTrees = append(sp.freeTrees, i)
}

// ExternalScannerState

// ts_subtree_external_scanner_state. Empty for anything but an external
// token leaf.
func (sp *subtreePool) externalScannerState(s subtree) []byte {
	if s != nullSubtree && !s.isInline() {
		h := sp.ptr(s)
		if h.flags&fHasExternalTokens != 0 && h.childCount == 0 {
			return sp.ext[h.extOff : h.extOff+h.extLen]
		}
	}
	return nil
}

// ts_subtree_external_scanner_state_eq
func (sp *subtreePool) externalScannerStateEq(s, other subtree) bool {
	return bytes.Equal(sp.externalScannerState(s), sp.externalScannerState(other))
}

// SubtreeArray

func (sp *subtreePool) array(a subtreeArray) []subtree {
	return sp.children[a.off : a.off+a.size]
}

// Bump allocation of n slots at the end of the children slab.
func (sp *subtreePool) bumpChildren(n uint32) uint32 {
	off := len(sp.children)
	need := off + int(n)
	if need > cap(sp.children) {
		sp.growChildren(need)
	}
	sp.children = sp.children[:need]
	return uint32(off)
}

// The capacity never passes 2^32-1 entries, so every offset and length of
// the slab fits in 32 bits.
func (sp *subtreePool) growChildren(need int) {
	if need > math.MaxUint32 {
		panic(errSlabFull)
	}
	grown := make([]subtree, len(sp.children), min(max(2*cap(sp.children), need), math.MaxUint32))
	copy(grown, sp.children)
	sp.children = grown
}

// Moves a region at the end of the slab, as popLinear leaves it, into a free
// block. Without that a parse that frees nodes in error recovery and reduces
// in between would never use what it freed. reduceInPlace calls this once
// much of the slab is free: until then the end of the slab saves the copy.
func (sp *subtreePool) settleChildren(a *subtreeArray) {
	n := a.size
	class, size := childClass(n)
	off := sp.takeFree(class, size)
	if off == 0 && sp.quickFree > 0 {
		sp.flushQuick()
		off = sp.takeMerged(class, size)
	}
	if off == 0 {
		return
	}
	copy(sp.children[off:off+n], sp.children[a.off:a.off+n])
	sp.children = sp.children[:a.off]
	*a = subtreeArray{off: off, size: n, cap: size}
}

// A free block of at least size entries, cut to size, or 0. size is the
// block size of class.
func (sp *subtreePool) takeFree(class, size uint32) uint32 {
	if class <= exactChildClasses {
		if off := sp.quickBlocks[class]; off != 0 {
			sp.quickBlocks[class] = uint32(sp.children[off])
			sp.quickFree -= size
			return off
		}
	}
	off := sp.takeMerged(class, size)
	// Merge what waits in the quick lists before the slab gets a block that
	// they could have made, or grows.
	if off == 0 && (class > exactChildClasses || len(sp.children)+int(size) > cap(sp.children)) {
		sp.flushQuick()
		off = sp.takeMerged(class, size)
	}
	return off
}

func (sp *subtreePool) takeMerged(class, size uint32) uint32 {
	list := class
	if sp.freeBlocks[list] == 0 {
		// The next non-empty list.
		m := sp.freeLists[list>>6] >> (list & 63)
		for m == 0 {
			list = (list>>6 + 1) << 6
			if list >= childClassCount {
				return 0
			}
			m = sp.freeLists[list>>6]
		}
		list += uint32(bits.TrailingZeros64(m))
	}
	off := sp.freeBlocks[list]
	if rest := sp.unlinkFree(off) - size; rest > 0 {
		sp.linkFree(off+size, rest)
	}
	return off
}

func (sp *subtreePool) flushQuick() {
	sp.quickFree = 0
	for size := uint32(1); size <= exactChildClasses; size++ {
		off := sp.quickBlocks[size]
		sp.quickBlocks[size] = 0
		for off != 0 {
			next := uint32(sp.children[off])
			sp.mergeFree(off, size)
			off = next
		}
	}
}

// The list of a free block.
func freeList(size uint32) uint32 {
	if size <= exactChildClasses {
		return size
	}
	if size >= 1<<31 {
		return childClassCount - 1
	}
	class, classSize := childClass(size)
	if classSize != size {
		class--
	}
	return class
}

func (sp *subtreePool) linkFree(off, size uint32) {
	list := freeList(size)
	head := sp.freeBlocks[list]
	sp.children[off] = subtree(head)
	if size > 1 {
		sp.children[off+1] = subtree(size)
		sp.children[off+size-1] = subtree(size)
	}
	if head != 0 {
		sp.children[head] |= subtree(off) << 32
	}
	sp.freeBlocks[list] = off
	sp.freeLists[list>>6] |= 1 << (list & 63)
	sp.mergedFree += size
	last := off + size - 1
	sp.freeBits[off>>5] |= 1 << (off & 31 * 2)
	sp.freeBits[last>>5] |= 2 << (last & 31 * 2)
}

// Takes a free block out of its list and returns its size.
func (sp *subtreePool) unlinkFree(off uint32) uint32 {
	next, prev := uint32(sp.children[off]), uint32(sp.children[off]>>32)
	size := uint32(1)
	if sp.freeBits[off>>5]>>(off&31*2)&2 == 0 {
		size = uint32(sp.children[off+1])
	}
	if prev != 0 {
		sp.children[prev] = sp.children[prev]>>32<<32 | subtree(next)
	} else {
		list := freeList(size)
		sp.freeBlocks[list] = next
		if next == 0 {
			sp.freeLists[list>>6] &^= 1 << (list & 63)
		}
	}
	if next != 0 {
		sp.children[next] = subtree(uint32(sp.children[next])) | subtree(prev)<<32
	}
	last := off + size - 1
	sp.freeBits[off>>5] &^= 1 << (off & 31 * 2)
	sp.freeBits[last>>5] &^= 2 << (last & 31 * 2)
	sp.mergedFree -= size
	return size
}

// A block for n > 0 entries: a free one, else new at the end of the slab. C
// calls malloc here.
func (sp *subtreePool) allocChildren(n uint32) (off, size uint32) {
	class, size := childClass(n)
	if off = sp.takeFree(class, size); off != 0 {
		return off, size
	}
	return sp.bumpChildren(size), size
}

// Gives back a block of up to exactChildClasses entries. C calls free here.
func (sp *subtreePool) freeQuick(off, size uint32) {
	sp.children[off] = subtree(sp.quickBlocks[size])
	sp.quickBlocks[size] = off
	sp.quickFree += size
}

// Gives back a block of any size, or takes one from the quick lists: merges
// it with a free block behind it and one in front of it. What then reaches
// the end of the slab is cut off, the rest goes on its list.
func (sp *subtreePool) mergeFree(off, size uint32) {
	if n := (cap(sp.children) + 31) / 32; n > len(sp.freeBits) {
		grown := make([]uint64, n)
		copy(grown, sp.freeBits)
		sp.freeBits = grown
	}
	if end := off + size; end < uint32(len(sp.children)) && sp.freeBits[end>>5]>>(end&31*2)&1 != 0 {
		size += sp.unlinkFree(end)
	}
	if b := sp.freeBits[(off-1)>>5] >> ((off - 1) & 31 * 2); b&2 != 0 {
		before := uint32(1)
		if b&1 == 0 {
			before = uint32(sp.children[off-1])
		}
		off -= before
		sp.unlinkFree(off)
		size += before
	}
	if off+size == uint32(len(sp.children)) {
		sp.children = sp.children[:off]
		return
	}
	sp.linkFree(off, size)
}

// array_reserve. A free block comes first: a region at the end of the slab
// that only ever grew in place would leave every block freed on the way
// unused. Without one, such a region grows in place.
func (sp *subtreePool) arrayReserve(a *subtreeArray, newCap uint32) {
	if newCap <= a.cap {
		return
	}
	class, newCap := childClass(newCap)
	off := sp.takeFree(class, newCap)
	if off == 0 {
		if a.cap > 0 && a.off+a.cap == uint32(len(sp.children)) {
			sp.bumpChildren(newCap - a.cap)
			a.cap = newCap
			return
		}
		off = sp.bumpChildren(newCap)
	}
	copy(sp.children[off:off+a.size], sp.children[a.off:a.off+a.size])
	size := a.size
	sp.arrayFree(a)
	*a = subtreeArray{off: off, size: size, cap: newCap}
}

// _array__grow
func (sp *subtreePool) arrayGrow(a *subtreeArray, count uint32) {
	newSize := a.size + count
	if newSize > a.cap {
		newCap := max(a.cap*2, 8, newSize)
		sp.arrayReserve(a, newCap)
	}
}

// array_push
func (sp *subtreePool) arrayPush(a *subtreeArray, s subtree) {
	sp.arrayGrow(a, 1)
	sp.children[a.off+a.size] = s
	a.size++
}

// array_splice. elements may alias the slab: a reallocation leaves the old
// backing array readable.
func (sp *subtreePool) arraySplice(a *subtreeArray, index, oldCount uint32, elements []subtree) {
	newCount := uint32(len(elements))
	newSize := a.size + newCount - oldCount
	oldEnd := index + oldCount
	newEnd := index + newCount
	sp.arrayReserve(a, newSize)
	c := sp.children[a.off : a.off+max(a.size, newSize)]
	if a.size > oldEnd {
		copy(c[newEnd:], c[oldEnd:a.size])
	}
	copy(c[index:], elements)
	a.size = newSize
}

// array_delete for a region.
func (sp *subtreePool) arrayFree(a *subtreeArray) {
	if a.cap > 0 {
		if a.off+a.cap == uint32(len(sp.children)) {
			sp.children = sp.children[:a.off]
		} else if a.cap <= exactChildClasses {
			sp.freeQuick(a.off, a.cap)
		} else {
			sp.mergeFree(a.off, a.cap)
		}
	}
	*a = subtreeArray{}
}

// ts_subtree_array_copy
func (sp *subtreePool) arrayCopy(self subtreeArray) subtreeArray {
	dest := self
	if self.cap > 0 {
		dest.off, dest.cap = sp.allocChildren(self.cap)
		copy(sp.children[dest.off:dest.off+self.size], sp.children[self.off:self.off+self.size])
		for _, s := range sp.array(dest) {
			sp.retain(s)
		}
	}
	return dest
}

// ts_subtree_array_delete
func (sp *subtreePool) arrayDelete(a *subtreeArray) {
	for i := uint32(0); i < a.size; i++ {
		sp.release(sp.children[a.off+i])
	}
	sp.arrayFree(a)
}

// ts_subtree_array_remove_trailing_extras. destination is a parser-owned
// scratch slice (trailing_extras).
func (sp *subtreePool) arrayRemoveTrailingExtras(a *subtreeArray, destination *[]subtree) {
	d := (*destination)[:0]
	for a.size > 0 {
		last := sp.children[a.off+a.size-1]
		if sp.extra(last) {
			a.size--
			d = append(d, last)
		} else {
			break
		}
	}
	subtreeSliceReverse(d)
	*destination = d
}

// ts_subtree_array_reverse
func subtreeSliceReverse(s []subtree) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// Subtree accessors (subtree.h).

// ts_subtree_symbol
func (sp *subtreePool) symbol(s subtree) Symbol {
	if s.isInline() {
		return Symbol(uint8(s >> inlSymbolShift))
	}
	return sp.ptr(s).symbol
}

// ts_subtree_visible
func (sp *subtreePool) visible(s subtree) bool {
	if s.isInline() {
		return s&inlVisible != 0
	}
	return sp.ptr(s).flags&fVisible != 0
}

// ts_subtree_named
func (sp *subtreePool) named(s subtree) bool {
	if s.isInline() {
		return s&inlNamed != 0
	}
	return sp.ptr(s).flags&fNamed != 0
}

// ts_subtree_extra
func (sp *subtreePool) extra(s subtree) bool {
	if s.isInline() {
		return s&inlExtra != 0
	}
	return sp.ptr(s).flags&fExtra != 0
}

// ts_subtree_missing
func (sp *subtreePool) missing(s subtree) bool {
	if s.isInline() {
		return s&inlIsMissing != 0
	}
	return sp.ptr(s).flags&fIsMissing != 0
}

// ts_subtree_is_keyword
func (sp *subtreePool) isKeyword(s subtree) bool {
	if s.isInline() {
		return s&inlIsKeyword != 0
	}
	return sp.ptr(s).flags&fIsKeyword != 0
}

// ts_subtree_parse_state
func (sp *subtreePool) parseState(s subtree) StateID {
	if s.isInline() {
		return StateID(uint16(s >> inlParseStateShift))
	}
	return sp.ptr(s).parseState
}

// ts_subtree_lookahead_bytes
func (sp *subtreePool) lookaheadBytes(s subtree) uint32 {
	if s.isInline() {
		return uint32(s>>inlLookaheadBytesShift) & 0xF
	}
	return sp.ptr(s).lookaheadBytes
}

// ts_subtree_children
func (sp *subtreePool) childrenOf(s subtree) []subtree {
	if s.isInline() {
		return nil
	}
	h := sp.ptr(s)
	return sp.children[h.children : h.children+h.childCount]
}

// ts_subtree_set_extra
func (sp *subtreePool) setExtra(s *subtree, isExtra bool) {
	if s.isInline() {
		if isExtra {
			*s |= inlExtra
		} else {
			*s &^= inlExtra
		}
	} else {
		sp.ptr(*s).setFlag(fExtra, isExtra)
	}
}

func (h *subtreeHeap) setFlag(f uint16, on bool) {
	if on {
		h.flags |= f
	} else {
		h.flags &^= f
	}
}

// ts_subtree_leaf_symbol
func (sp *subtreePool) leafSymbol(s subtree) Symbol {
	if s.isInline() {
		return Symbol(uint8(s >> inlSymbolShift))
	}
	h := sp.ptr(s)
	if h.childCount == 0 {
		return h.symbol
	}
	return h.firstLeafSymbol
}

// ts_subtree_leaf_parse_state
func (sp *subtreePool) leafParseState(s subtree) StateID {
	if s.isInline() {
		return StateID(uint16(s >> inlParseStateShift))
	}
	h := sp.ptr(s)
	if h.childCount == 0 {
		return h.parseState
	}
	return h.firstLeafParseState
}

// ts_subtree_padding
func (sp *subtreePool) padding(s subtree) length {
	if s.isInline() {
		return length{
			uint32(uint8(s >> inlPaddingBytesShift)),
			Point{uint32(s>>inlPaddingRowsShift) & 0xF, uint32(uint8(s >> inlPaddingColumnsShift))},
		}
	}
	return sp.ptr(s).padding
}

// ts_subtree_size
func (sp *subtreePool) size(s subtree) length {
	if s.isInline() {
		n := uint32(uint8(s >> inlSizeBytesShift))
		return length{n, Point{0, n}}
	}
	return sp.ptr(s).size
}

// ts_subtree_total_size
func (sp *subtreePool) totalSize(s subtree) length {
	return lengthAdd(sp.padding(s), sp.size(s))
}

// ts_subtree_total_bytes
func (sp *subtreePool) totalBytes(s subtree) uint32 {
	if s.isInline() {
		return uint32(uint8(s>>inlPaddingBytesShift)) + uint32(uint8(s>>inlSizeBytesShift))
	}
	h := sp.ptr(s)
	return h.padding.bytes + h.size.bytes
}

// ts_subtree_child_count
func (sp *subtreePool) childCount(s subtree) uint32 {
	if s.isInline() {
		return 0
	}
	return sp.ptr(s).childCount
}

// ts_subtree_repeat_depth
func (sp *subtreePool) repeatDepth(s subtree) uint32 {
	if s.isInline() {
		return 0
	}
	return uint32(sp.ptr(s).repeatDepth)
}

// ts_subtree_visible_descendant_count
func (sp *subtreePool) visibleDescendantCount(s subtree) uint32 {
	if s.isInline() {
		return 0
	}
	h := sp.ptr(s)
	if h.childCount == 0 {
		return 0
	}
	return h.visibleDescendantCount
}

// ts_subtree_visible_child_count
func (sp *subtreePool) visibleChildCount(s subtree) uint32 {
	if s.isInline() {
		return 0
	}
	h := sp.ptr(s)
	if h.childCount > 0 {
		return h.visibleChildCount
	}
	return 0
}

// ts_subtree_error_cost
func (sp *subtreePool) errorCost(s subtree) uint32 {
	if s.isInline() {
		if s&inlIsMissing != 0 {
			return errorCostPerMissingTree + errorCostPerRecovery
		}
		return 0
	}
	h := sp.ptr(s)
	if h.flags&fIsMissing != 0 {
		return errorCostPerMissingTree + errorCostPerRecovery
	}
	return h.errorCost
}

// ts_subtree_dynamic_precedence
func (sp *subtreePool) dynamicPrecedence(s subtree) int32 {
	if s.isInline() {
		return 0
	}
	h := sp.ptr(s)
	if h.childCount == 0 {
		return 0
	}
	return h.dynamicPrecedence
}

// ts_subtree_fragile_left
func (sp *subtreePool) fragileLeft(s subtree) bool {
	return !s.isInline() && sp.ptr(s).flags&fFragileLeft != 0
}

// ts_subtree_fragile_right
func (sp *subtreePool) fragileRight(s subtree) bool {
	return !s.isInline() && sp.ptr(s).flags&fFragileRight != 0
}

// ts_subtree_has_external_tokens
func (sp *subtreePool) hasExternalTokens(s subtree) bool {
	return !s.isInline() && sp.ptr(s).flags&fHasExternalTokens != 0
}

// ts_subtree_has_external_scanner_state_change
func (sp *subtreePool) hasExternalScannerStateChange(s subtree) bool {
	return !s.isInline() && sp.ptr(s).flags&fHasExternalScannerStateChange != 0
}

// ts_subtree_depends_on_column
func (sp *subtreePool) dependsOnColumn(s subtree) bool {
	return !s.isInline() && sp.ptr(s).flags&fDependsOnColumn != 0
}

// ts_subtree_is_error
func (sp *subtreePool) isError(s subtree) bool {
	return sp.symbol(s) == SymbolError
}

// ts_subtree_is_eof
func (sp *subtreePool) isEOF(s subtree) bool {
	return sp.symbol(s) == SymbolEnd
}

// Subtree constructors (subtree.c).

// ts_subtree_can_inline
func subtreeCanInline(padding, size length, lookaheadBytes uint32) bool {
	return padding.bytes < maxInlineTreeLength &&
		padding.extent.Row < 16 &&
		padding.extent.Column < maxInlineTreeLength &&
		size.bytes < maxInlineTreeLength &&
		size.extent.Row == 0 &&
		size.extent.Column < maxInlineTreeLength &&
		lookaheadBytes < 16
}

// ts_subtree_new_leaf
func (sp *subtreePool) newLeaf(
	symbol Symbol, padding, size length,
	lookaheadBytes uint32, parseState StateID,
	hasExternalTokens, dependsOnColumn, isKeyword bool, lang *Language,
) subtree {
	metadata := lang.symbolMetadata(symbol)
	extra := symbol == SymbolEnd

	isInline := symbol <= 255 &&
		!hasExternalTokens &&
		subtreeCanInline(padding, size, lookaheadBytes)

	if isInline {
		s := subtree(inlIsInline) |
			subtree(symbol)<<inlSymbolShift |
			subtree(parseState)<<inlParseStateShift |
			subtree(padding.extent.Column)<<inlPaddingColumnsShift |
			subtree(padding.extent.Row)<<inlPaddingRowsShift |
			subtree(lookaheadBytes)<<inlLookaheadBytesShift |
			subtree(padding.bytes)<<inlPaddingBytesShift |
			subtree(size.bytes)<<inlSizeBytesShift
		if metadata.Visible {
			s |= inlVisible
		}
		if metadata.Named {
			s |= inlNamed
		}
		if extra {
			s |= inlExtra
		}
		if isKeyword {
			s |= inlIsKeyword
		}
		return s
	}

	var flags uint16
	if metadata.Visible {
		flags |= fVisible
	}
	if metadata.Named {
		flags |= fNamed
	}
	if extra {
		flags |= fExtra
	}
	if hasExternalTokens {
		flags |= fHasExternalTokens
	}
	if dependsOnColumn {
		flags |= fDependsOnColumn
	}
	if isKeyword {
		flags |= fIsKeyword
	}
	i := sp.allocate()
	*sp.ptr(subtree(i) << 1) = subtreeHeap{
		refCount:       1,
		padding:        padding,
		size:           size,
		lookaheadBytes: lookaheadBytes,
		symbol:         symbol,
		parseState:     parseState,
		flags:          flags,
	}
	return subtree(i) << 1
}

// ts_subtree_set_symbol
func (sp *subtreePool) setSymbol(s *subtree, symbol Symbol, lang *Language) {
	metadata := lang.symbolMetadata(symbol)
	if s.isInline() {
		v := *s &^ (0xFF<<inlSymbolShift | inlNamed | inlVisible)
		v |= subtree(uint8(symbol)) << inlSymbolShift
		if metadata.Named {
			v |= inlNamed
		}
		if metadata.Visible {
			v |= inlVisible
		}
		*s = v
	} else {
		h := sp.ptr(*s)
		h.symbol = symbol
		h.setFlag(fNamed, metadata.Named)
		h.setFlag(fVisible, metadata.Visible)
	}
}

// ts_subtree_new_error
func (sp *subtreePool) newError(
	lookaheadChar int32, padding, size length,
	bytesScanned uint32, parseState StateID, lang *Language,
) subtree {
	// SymbolError is above 255, so this is never inline.
	result := sp.newLeaf(SymbolError, padding, size, bytesScanned, parseState, false, false, false, lang)
	h := sp.ptr(result)
	h.flags |= fFragileLeft | fFragileRight
	h.lookaheadChar = lookaheadChar
	return result
}

// ts_subtree_clone
func (sp *subtreePool) clone(s subtree) subtree {
	i := sp.allocate()
	h := sp.ptr(subtree(i) << 1)
	*h = *sp.ptr(s)
	if h.childCount > 0 {
		old := h.children
		off, _ := sp.allocChildren(h.childCount)
		copy(sp.children[off:off+h.childCount], sp.children[old:old+h.childCount])
		h.children = off
		class, _ := childClass(h.childCount)
		h.childClass = uint8(class)
		for _, child := range sp.children[off : off+h.childCount] {
			sp.retain(child)
		}
	}
	// An external scanner state is immutable and never freed during a
	// parse, so the clone shares the bytes instead of copying them.
	h.refCount = 1
	return subtree(i) << 1
}

// ts_subtree_make_mut. Takes ownership; copies if there is another owner.
func (sp *subtreePool) makeMut(s subtree) subtree {
	if s.isInline() {
		return s
	}
	if sp.ptr(s).refCount == 1 {
		return s
	}
	result := sp.clone(s)
	sp.release(s)
	return result
}

// ts_subtree_compress
func (sp *subtreePool) compress(self subtree, count uint32, lang *Language, stack *[]subtree) {
	initialStackSize := len(*stack)

	tree := self
	symbol := sp.ptr(tree).symbol
	for i := uint32(0); i < count; i++ {
		th := sp.ptr(tree)
		if th.refCount > 1 || th.childCount < 2 {
			break
		}

		child := sp.children[th.children]
		if child.isInline() {
			break
		}
		ch := sp.ptr(child)
		if ch.childCount < 2 || ch.refCount > 1 || ch.symbol != symbol {
			break
		}

		grandchild := sp.children[ch.children]
		if grandchild.isInline() {
			break
		}
		gh := sp.ptr(grandchild)
		if gh.childCount < 2 || gh.refCount > 1 || gh.symbol != symbol {
			break
		}

		sp.children[th.children] = grandchild
		sp.children[ch.children] = sp.children[gh.children+gh.childCount-1]
		sp.children[gh.children+gh.childCount-1] = child
		*stack = append(*stack, tree)
		tree = grandchild
	}

	for len(*stack) > initialStackSize {
		tree = (*stack)[len(*stack)-1]
		*stack = (*stack)[:len(*stack)-1]
		child := sp.children[sp.ptr(tree).children]
		ch := sp.ptr(child)
		grandchild := sp.children[ch.children+ch.childCount-1]
		sp.summarizeChildren(grandchild, lang)
		sp.summarizeChildren(child, lang)
		sp.summarizeChildren(tree, lang)
	}
}

// ts_subtree_summarize_children. Allocates nothing, so h and children stay
// valid throughout. Each child is decoded once (inline bits or one heap
// load) and the sums are kept in locals; the C accessor calls it replaces
// are named at each line.
func (sp *subtreePool) summarizeChildren(self subtree, lang *Language) {
	h := sp.ptr(self)

	var namedChildCount, visibleChildCount, errorCost, visibleDescendantCount uint32
	var dynamicPrecedence int32
	flags := h.flags &^ (fHasExternalTokens | fDependsOnColumn | fHasExternalScannerStateChange | fHasRepeat)
	h.repeatDepth = 0

	structuralIndex := uint32(0)
	aliasSequence := lang.aliasSequence(uint32(h.productionID))
	lookaheadEndByte := uint32(0)
	selfIsError := h.symbol == SymbolError || h.symbol == SymbolErrorRepeat
	// Not zero: C tests the old size.extent.row before the first child sets
	// it, and a node without children keeps both.
	padding, size := h.padding, h.size

	heap := sp.heap
	children := sp.children[h.children : h.children+h.childCount]
	for i, child := range children {
		// The child's fields, as ts_subtree_* would return them.
		var (
			cPadding, cSize          length
			cLookaheadBytes          uint32
			cSymbol                  Symbol
			cErrorCost               uint32
			grandchildCount          uint32
			cVisibleChildCount       uint32
			cNamedChildCount         uint32
			cVisibleDescendantCount  uint32
			cDynamicPrecedence       int32
			childExtra, childVisible bool
			childNamed               bool
		)
		if child.isInline() {
			n := uint32(uint8(child >> inlSizeBytesShift))
			cPadding = length{
				uint32(uint8(child >> inlPaddingBytesShift)),
				Point{uint32(child>>inlPaddingRowsShift) & 0xF, uint32(uint8(child >> inlPaddingColumnsShift))},
			}
			cSize = length{n, Point{0, n}}
			cLookaheadBytes = uint32(child>>inlLookaheadBytesShift) & 0xF
			cSymbol = Symbol(uint8(child >> inlSymbolShift))
			if child&inlIsMissing != 0 {
				cErrorCost = errorCostPerMissingTree + errorCostPerRecovery
			}
			childExtra = child&inlExtra != 0
			childVisible = child&inlVisible != 0
			childNamed = child&inlNamed != 0
		} else {
			ch := &heap[child>>(1+heapChunkBits)][child>>1&heapChunkMask]
			cf := ch.flags
			if size.extent.Row == 0 && cf&fDependsOnColumn != 0 {
				flags |= fDependsOnColumn
			}
			flags |= cf & (fHasExternalScannerStateChange | fHasExternalTokens | fHasRepeat)

			cPadding = ch.padding
			cSize = ch.size
			cLookaheadBytes = ch.lookaheadBytes
			cSymbol = ch.symbol
			cErrorCost = ch.errorCost
			if cf&fIsMissing != 0 {
				cErrorCost = errorCostPerMissingTree + errorCostPerRecovery
			}
			grandchildCount = ch.childCount
			if grandchildCount > 0 {
				cVisibleChildCount = ch.visibleChildCount
				cNamedChildCount = ch.namedChildCount
				cVisibleDescendantCount = ch.visibleDescendantCount
				cDynamicPrecedence = ch.dynamicPrecedence
			}
			childExtra = cf&fExtra != 0
			childVisible = cf&fVisible != 0
			childNamed = cf&fNamed != 0

			if cSymbol == SymbolError {
				flags |= fFragileLeft | fFragileRight
				h.parseState = treeStateNone
			}
		}

		if i == 0 {
			padding = cPadding
			size = cSize
		} else {
			size = lengthAdd(size, lengthAdd(cPadding, cSize))
		}

		childLookaheadEndByte := padding.bytes + size.bytes + cLookaheadBytes
		if childLookaheadEndByte > lookaheadEndByte {
			lookaheadEndByte = childLookaheadEndByte
		}

		if cSymbol != SymbolErrorRepeat {
			errorCost += cErrorCost
		}

		if selfIsError {
			if !childExtra && !(cSymbol == SymbolError && grandchildCount == 0) {
				if childVisible {
					errorCost += errorCostPerSkippedTree
				} else if grandchildCount > 0 {
					errorCost += errorCostPerSkippedTree * cVisibleChildCount
				}
			}
		}

		dynamicPrecedence += cDynamicPrecedence
		visibleDescendantCount += cVisibleDescendantCount

		if aliasSequence != nil && aliasSequence[structuralIndex] != 0 && !childExtra {
			visibleDescendantCount++
			visibleChildCount++
			if lang.symbolMetadata(aliasSequence[structuralIndex]).Named {
				namedChildCount++
			}
		} else if childVisible {
			visibleDescendantCount++
			visibleChildCount++
			if childNamed {
				namedChildCount++
			}
		} else if grandchildCount > 0 {
			visibleChildCount += cVisibleChildCount
			namedChildCount += cNamedChildCount
		}

		if !childExtra {
			structuralIndex++
		}
	}

	h.padding = padding
	h.size = size
	h.namedChildCount = namedChildCount
	h.visibleChildCount = visibleChildCount
	h.visibleDescendantCount = visibleDescendantCount
	h.dynamicPrecedence = dynamicPrecedence
	h.lookaheadBytes = lookaheadEndByte - size.bytes - padding.bytes

	if selfIsError {
		errorCost += errorCostPerRecovery +
			errorCostPerSkippedChar*size.bytes +
			errorCostPerSkippedLine*size.extent.Row
	}
	h.errorCost = errorCost

	if h.childCount > 0 {
		firstChild := children[0]
		lastChild := children[h.childCount-1]

		h.firstLeafSymbol = sp.leafSymbol(firstChild)
		h.firstLeafParseState = sp.leafParseState(firstChild)

		if sp.fragileLeft(firstChild) {
			flags |= fFragileLeft
		}
		if sp.fragileRight(lastChild) {
			flags |= fFragileRight
		}

		if h.childCount >= 2 &&
			flags&(fVisible|fNamed) == 0 &&
			sp.symbol(firstChild) == h.symbol {
			if sp.repeatDepth(firstChild) > sp.repeatDepth(lastChild) {
				h.repeatDepth = uint16(sp.repeatDepth(firstChild) + 1)
			} else {
				h.repeatDepth = uint16(sp.repeatDepth(lastChild) + 1)
			}
			flags |= fHasRepeat
		}
	}
	h.flags = flags
}

// ts_subtree_new_node. Takes ownership of the children region. C puts the
// node's heap data behind the children in the same allocation; here the
// region becomes the node's children block. At the end of the slab it is cut
// to the class of its size; elsewhere it keeps its capacity, as in C.
func (sp *subtreePool) newNode(symbol Symbol, children *subtreeArray, productionID uint32, lang *Language) subtree {
	metadata := lang.symbolMetadata(symbol)
	fragile := symbol == SymbolError || symbol == SymbolErrorRepeat

	var class uint32
	if children.size == 0 {
		sp.arrayFree(children)
	} else if children.off+children.cap == uint32(len(sp.children)) {
		var size uint32
		class, size = childClass(children.size)
		if size <= children.cap {
			sp.children = sp.children[:children.off+size]
		} else {
			sp.bumpChildren(size - children.cap)
		}
	} else {
		class, _ = childClass(children.cap)
	}

	var flags uint16
	if metadata.Visible {
		flags |= fVisible
	}
	if metadata.Named {
		flags |= fNamed
	}
	if fragile {
		flags |= fFragileLeft | fFragileRight
	}
	i := sp.allocate()
	*sp.ptr(subtree(i) << 1) = subtreeHeap{
		refCount:     1,
		symbol:       symbol,
		childCount:   children.size,
		flags:        flags,
		productionID: uint16(productionID),
		children:     children.off,
		childClass:   uint8(class),
	}
	result := subtree(i) << 1
	sp.summarizeChildren(result, lang)
	return result
}

// ts_subtree_new_error_node
func (sp *subtreePool) newErrorNode(children *subtreeArray, extra bool, lang *Language) subtree {
	result := sp.newNode(SymbolError, children, 0, lang)
	sp.ptr(result).setFlag(fExtra, extra)
	return result
}

// ts_subtree_new_missing_leaf
func (sp *subtreePool) newMissingLeaf(symbol Symbol, padding length, lookaheadBytes uint32, lang *Language) subtree {
	result := sp.newLeaf(symbol, padding, length{}, lookaheadBytes, 0, false, false, false, lang)
	if result.isInline() {
		result |= inlIsMissing
	} else {
		sp.ptr(result).flags |= fIsMissing
	}
	return result
}

// ts_subtree_retain
func (sp *subtreePool) retain(s subtree) {
	if s.isInline() {
		return
	}
	sp.ptr(s).refCount++
}

// ts_subtree_release
func (sp *subtreePool) release(s subtree) {
	if s.isInline() {
		return
	}
	sp.treeStack = sp.treeStack[:0]

	h := sp.ptr(s)
	h.refCount--
	if h.refCount == 0 {
		sp.treeStack = append(sp.treeStack, s)
	}

	for len(sp.treeStack) > 0 {
		tree := sp.treeStack[len(sp.treeStack)-1]
		sp.treeStack = sp.treeStack[:len(sp.treeStack)-1]
		th := sp.ptr(tree)
		if th.childCount > 0 {
			for _, child := range sp.children[th.children : th.children+th.childCount] {
				if child.isInline() {
					continue
				}
				ch := sp.ptr(child)
				ch.refCount--
				if ch.refCount == 0 {
					sp.treeStack = append(sp.treeStack, child)
				}
			}
			if class := uint32(th.childClass); class <= exactChildClasses {
				sp.freeQuick(th.children, class)
			} else {
				sp.mergeFree(th.children, childClassSize(class))
			}
		}
		// C frees the children block (which holds a parent's heap data) or
		// returns a leaf to the pool.
		sp.free(uint32(tree >> 1))
	}
}

// ts_subtree_compare
func (sp *subtreePool) compare(left, right subtree) int {
	sp.treeStack = append(sp.treeStack, left, right)

	for len(sp.treeStack) > 0 {
		n := len(sp.treeStack)
		right = sp.treeStack[n-1]
		left = sp.treeStack[n-2]
		sp.treeStack = sp.treeStack[:n-2]

		result := 0
		if sp.symbol(left) < sp.symbol(right) {
			result = -1
		} else if sp.symbol(right) < sp.symbol(left) {
			result = 1
		} else if sp.childCount(left) < sp.childCount(right) {
			result = -1
		} else if sp.childCount(right) < sp.childCount(left) {
			result = 1
		}
		if result != 0 {
			sp.treeStack = sp.treeStack[:0]
			return result
		}

		leftChildren := sp.childrenOf(left)
		rightChildren := sp.childrenOf(right)
		for i := len(leftChildren); i > 0; i-- {
			sp.treeStack = append(sp.treeStack, leftChildren[i-1], rightChildren[i-1])
		}
	}

	return 0
}

// ts_subtree_last_external_token
func (sp *subtreePool) lastExternalToken(tree subtree) subtree {
	if !sp.hasExternalTokens(tree) {
		return nullSubtree
	}
	for sp.ptr(tree).childCount > 0 {
		children := sp.childrenOf(tree)
		for i := len(children) - 1; i >= 0; i-- {
			child := children[i]
			if sp.hasExternalTokens(child) {
				tree = child
				break
			}
		}
	}
	return tree
}

// ts_subtree__write_char_to_string
func writeCharToString(b []byte, chr int32) []byte {
	switch {
	case chr == -1:
		return append(b, "INVALID"...)
	case chr == 0:
		return append(b, `'\0'`...)
	case chr == '\n':
		return append(b, `'\n'`...)
	case chr == '\t':
		return append(b, `'\t'`...)
	case chr == '\r':
		return append(b, `'\r'`...)
	case 32 <= chr && chr < 127: // 0 < chr < 128 && isprint(chr)
		return append(b, '\'', byte(chr), '\'')
	default:
		return strconv.AppendInt(b, int64(chr), 10)
	}
}

// ts_subtree__write_to_string. fieldName "" stands for NULL; isRoot for the
// ROOT_FIELD sentinel.
func (sp *subtreePool) writeToString(
	b []byte, self subtree, lang *Language, includeAll bool,
	aliasSymbol Symbol, aliasIsNamed bool, fieldName string, isRoot bool,
) []byte {
	if self == nullSubtree {
		return append(b, "(NULL)"...)
	}

	isVisible := includeAll || sp.missing(self)
	if !isVisible {
		if aliasSymbol != 0 {
			isVisible = aliasIsNamed
		} else {
			isVisible = sp.visible(self) && sp.named(self)
		}
	}

	if isVisible {
		if !isRoot {
			b = append(b, ' ')
			if fieldName != "" {
				b = append(b, fieldName...)
				b = append(b, ": "...)
			}
		}

		if sp.isError(self) && sp.childCount(self) == 0 && sp.ptr(self).size.bytes > 0 {
			b = append(b, "(UNEXPECTED "...)
			b = writeCharToString(b, sp.ptr(self).lookaheadChar)
		} else {
			symbol := aliasSymbol
			if symbol == 0 {
				symbol = sp.symbol(self)
			}
			symbolName := lang.SymbolName(symbol)
			if sp.missing(self) {
				b = append(b, "(MISSING "...)
				if aliasIsNamed || sp.named(self) {
					b = append(b, symbolName...)
				} else {
					b = append(b, '"')
					b = append(b, symbolName...)
					b = append(b, '"')
				}
			} else {
				b = append(b, '(')
				b = append(b, symbolName...)
			}
		}
	} else if isRoot {
		symbol := aliasSymbol
		if symbol == 0 {
			symbol = sp.symbol(self)
		}
		symbolName := lang.SymbolName(symbol)
		if sp.childCount(self) > 0 {
			b = append(b, '(')
			b = append(b, symbolName...)
		} else if sp.named(self) {
			b = append(b, '(')
			b = append(b, symbolName...)
			b = append(b, ')')
		} else {
			b = append(b, `("`...)
			b = append(b, symbolName...)
			b = append(b, `")`...)
		}
	}

	if childCount := sp.childCount(self); childCount != 0 {
		h := sp.ptr(self)
		childrenOff := h.children
		aliasSequence := lang.aliasSequence(uint32(h.productionID))
		fieldMap := lang.fieldMap(uint32(h.productionID))

		structuralChildIndex := uint32(0)
		for i := uint32(0); i < childCount; i++ {
			child := sp.children[childrenOff+i]
			if sp.extra(child) {
				b = sp.writeToString(b, child, lang, includeAll, 0, false, "", false)
			} else {
				var subtreeAliasSymbol Symbol
				if aliasSequence != nil {
					subtreeAliasSymbol = aliasSequence[structuralChildIndex]
				}
				subtreeAliasIsNamed := false
				if subtreeAliasSymbol != 0 {
					subtreeAliasIsNamed = lang.symbolMetadata(subtreeAliasSymbol).Named
				}

				childFieldName := fieldName
				if isVisible {
					childFieldName = ""
				}
				// An invisible root hands ROOT_FIELD down, so its child
				// is printed as a root too.
				childIsRootField := isRoot && !isVisible
				for _, m := range fieldMap {
					if !m.Inherited && uint32(m.ChildIndex) == structuralChildIndex {
						childFieldName = lang.FieldNames[m.FieldID]
						childIsRootField = false
						break
					}
				}

				b = sp.writeToString(
					b, child, lang, includeAll,
					subtreeAliasSymbol, subtreeAliasIsNamed, childFieldName, childIsRootField,
				)
				structuralChildIndex++
			}
		}
	}

	if isVisible {
		b = append(b, ')')
	}
	return b
}

// ts_subtree_string
func (sp *subtreePool) string(self subtree, aliasSymbol Symbol, aliasIsNamed bool, lang *Language, includeAll bool) string {
	return string(sp.writeToString(nil, self, lang, includeAll, aliasSymbol, aliasIsNamed, "", true))
}
