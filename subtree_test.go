package gositter

import (
	"math/rand"
	"testing"
	"unsafe"
)

// Symbols of testLanguage.
const (
	tsEnd Symbol = iota
	tsAnon
	tsIdent
	tsHidden
	tsRule
	tsRepeat
)

func testLanguage() *Language {
	return &Language{
		ABIVersion:  14,
		SymbolCount: 6,
		TokenCount:  4,
		SymbolNames: []string{"end", "a", "ident", "_hidden", "rule", "rule_repeat1"},
		SymbolMetadata: []SymbolMetadata{
			{},
			{Visible: true},
			{Visible: true, Named: true},
			{},
			{Visible: true, Named: true},
			{},
		},
		PublicSymbolMap: []Symbol{0, 1, 2, 3, 4, 5},
	}
}

func newTestPool() *subtreePool {
	sp := &subtreePool{}
	sp.reset()
	return sp
}

func TestSubtreeInlineLayout(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	padding := length{7, Point{3, 5}}
	size := length{9, Point{0, 9}}
	s := sp.newLeaf(tsIdent, padding, size, 11, 0x1234, false, false, true, lang)
	if !s.isInline() {
		t.Fatal("want inline")
	}
	// The C bit layout of SubtreeInlineData, little-endian.
	want := subtree(1 | 1<<1 | 1<<2 | 1<<6 | // is_inline, visible, named, is_keyword
		2<<8 | 0x1234<<16 | 5<<32 | 3<<40 | 11<<44 | 7<<48 | 9<<56)
	if s != want {
		t.Fatalf("bits = %#x, want %#x", uint64(s), uint64(want))
	}
	if sp.symbol(s) != tsIdent || sp.parseState(s) != 0x1234 || sp.padding(s) != padding ||
		sp.size(s) != size || sp.lookaheadBytes(s) != 11 || !sp.isKeyword(s) ||
		!sp.visible(s) || !sp.named(s) || sp.extra(s) || sp.totalBytes(s) != 16 {
		t.Fatal("accessor mismatch")
	}
	if sp.heapLen != 1 {
		t.Fatal("inline leaf allocated")
	}
}

func TestSubtreeCanInline(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	ok := length{1, Point{0, 1}}
	cases := []struct {
		name          string
		symbol        Symbol
		padding, size length
		lookahead     uint32
		external      bool
		inline        bool
	}{
		{"small", tsAnon, ok, ok, 1, false, true},
		{"padding 254", tsAnon, length{254, Point{15, 254}}, ok, 15, false, true},
		{"padding bytes 255", tsAnon, length{255, Point{0, 1}}, ok, 1, false, false},
		{"padding rows 16", tsAnon, length{20, Point{16, 0}}, ok, 1, false, false},
		{"padding columns 255", tsAnon, length{20, Point{0, 255}}, ok, 1, false, false},
		{"size 255", tsAnon, ok, length{255, Point{0, 255}}, 1, false, false},
		{"multi-line size", tsAnon, ok, length{3, Point{1, 0}}, 1, false, false},
		{"lookahead 16", tsAnon, ok, ok, 16, false, false},
		{"external token", tsAnon, ok, ok, 1, true, false},
		{"error symbol", SymbolError, ok, ok, 1, false, false},
	}
	for _, tc := range cases {
		s := sp.newLeaf(tc.symbol, tc.padding, tc.size, tc.lookahead, 1, tc.external, false, false, lang)
		if s.isInline() != tc.inline {
			t.Errorf("%s: inline = %v, want %v", tc.name, s.isInline(), tc.inline)
		}
		if sp.padding(s) != tc.padding || sp.size(s) != tc.size || sp.lookaheadBytes(s) != tc.lookahead {
			t.Errorf("%s: round trip failed", tc.name)
		}
	}
	// The end symbol is extra.
	if !sp.extra(sp.newLeaf(tsEnd, ok, length{}, 1, 1, false, false, false, lang)) {
		t.Error("eof leaf is not extra")
	}
}

func TestSubtreeMissingAndErrorCost(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	m := sp.newMissingLeaf(tsAnon, length{}, 3, lang)
	if !m.isInline() || !sp.missing(m) || sp.errorCost(m) != errorCostPerMissingTree+errorCostPerRecovery {
		t.Fatalf("missing leaf: inline=%v cost=%d", m.isInline(), sp.errorCost(m))
	}
	e := sp.newError('x', length{}, length{2, Point{0, 2}}, 1, 0, lang)
	h := sp.ptr(e)
	if e.isInline() || h.lookaheadChar != 'x' || !sp.fragileLeft(e) || !sp.fragileRight(e) {
		t.Fatal("error leaf fields")
	}
	arr := subtreeArray{}
	sp.arrayPush(&arr, e)
	node := sp.newErrorNode(&arr, false, lang)
	// ERROR_COST_PER_RECOVERY + chars; an error leaf child adds no tree cost.
	if got := sp.errorCost(node); got != errorCostPerRecovery+2 {
		t.Fatalf("error node cost = %d", got)
	}
	if got := sp.string(node, 0, false, lang, false); got != "(ERROR (UNEXPECTED 'x'))" {
		t.Fatalf("string = %q", got)
	}
}

func TestSubtreeNewNodeSummary(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	a := sp.newLeaf(tsAnon, length{2, Point{0, 2}}, length{1, Point{0, 1}}, 1, 1, false, false, false, lang)
	id := sp.newLeaf(tsIdent, length{1, Point{1, 0}}, length{3, Point{0, 3}}, 2, 1, false, false, false, lang)
	hid := sp.newLeaf(tsHidden, length{}, length{1, Point{0, 1}}, 1, 1, false, false, false, lang)

	arr := subtreeArray{}
	for _, c := range []subtree{a, id, hid} {
		sp.arrayPush(&arr, c)
	}
	node := sp.newNode(tsRule, &arr, 0, lang)
	h := sp.ptr(node)
	if h.padding != (length{2, Point{0, 2}}) {
		t.Errorf("padding = %+v", h.padding)
	}
	// a(1) + [newline, ident(3)] + hidden(1): the size crosses one row.
	if h.size != (length{6, Point{1, 4}}) {
		t.Errorf("size = %+v", h.size)
	}
	if h.childCount != 3 || h.visibleChildCount != 2 || h.namedChildCount != 1 || h.visibleDescendantCount != 2 {
		t.Errorf("counts = %d %d %d %d", h.childCount, h.visibleChildCount, h.namedChildCount, h.visibleDescendantCount)
	}
	if h.firstLeafSymbol != tsAnon || h.refCount != 1 || h.lookaheadBytes != 1 {
		t.Errorf("firstLeaf=%d refCount=%d lookahead=%d", h.firstLeafSymbol, h.refCount, h.lookaheadBytes)
	}
	if got := sp.string(node, 0, false, lang, false); got != "(rule (ident))" {
		t.Errorf("string = %q", got)
	}
	// The unused array capacity went back to the slab.
	if uint32(len(sp.children)) != h.children+h.childCount {
		t.Errorf("slab not trimmed: len=%d end=%d", len(sp.children), h.children+h.childCount)
	}
}

func TestSubtreeMakeMutAndRelease(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	big := length{300, Point{0, 300}}
	leaf := sp.newLeaf(tsIdent, length{}, big, 1, 1, false, false, false, lang)
	arr := subtreeArray{}
	sp.arrayPush(&arr, leaf)
	node := sp.newNode(tsRule, &arr, 0, lang)

	// One owner: mutable in place.
	if got := sp.makeMut(node); got != node {
		t.Fatal("make_mut cloned a subtree with one owner")
	}

	// Two owners: a clone, the original loses one reference, and the shared
	// child gains one.
	sp.retain(node)
	clone := sp.makeMut(node)
	if clone == node {
		t.Fatal("make_mut did not clone a shared subtree")
	}
	if sp.ptr(node).refCount != 1 || sp.ptr(clone).refCount != 1 || sp.ptr(leaf).refCount != 2 {
		t.Fatalf("refcounts: node=%d clone=%d leaf=%d",
			sp.ptr(node).refCount, sp.ptr(clone).refCount, sp.ptr(leaf).refCount)
	}
	if sp.ptr(clone).children == sp.ptr(node).children {
		t.Fatal("clone shares the children region")
	}
	if sp.compare(node, clone) != 0 {
		t.Fatal("clone compares unequal")
	}

	// Releasing both frees node, clone and then the leaf.
	sp.release(node)
	if sp.ptr(leaf).refCount != 1 || len(sp.freeTrees) != 1 {
		t.Fatalf("after first release: leaf=%d free=%d", sp.ptr(leaf).refCount, len(sp.freeTrees))
	}
	sp.release(clone)
	if len(sp.freeTrees) != 3 {
		t.Fatalf("free list = %d, want 3", len(sp.freeTrees))
	}
	// A freed index is reused.
	before := sp.heapLen
	sp.newLeaf(tsIdent, length{}, big, 1, 1, false, false, false, lang)
	if sp.heapLen != before || len(sp.freeTrees) != 2 {
		t.Fatal("free index not reused")
	}
}

func TestSubtreeCompare(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	leaf := func(sym Symbol) subtree {
		return sp.newLeaf(sym, length{}, length{1, Point{0, 1}}, 1, 1, false, false, false, lang)
	}
	node := func(children ...subtree) subtree {
		arr := subtreeArray{}
		for _, c := range children {
			sp.arrayPush(&arr, c)
		}
		return sp.newNode(tsRule, &arr, 0, lang)
	}
	a, b := node(leaf(tsAnon), leaf(tsIdent)), node(leaf(tsAnon), leaf(tsHidden))
	if sp.compare(a, b) != -1 || sp.compare(b, a) != 1 || sp.compare(a, a) != 0 {
		t.Fatal("compare by child symbol")
	}
	if sp.compare(node(leaf(tsAnon)), a) != -1 {
		t.Fatal("compare by child count")
	}
	if len(sp.treeStack) != 0 {
		t.Fatal("tree stack not empty after compare")
	}
}

func TestSubtreeArraySplice(t *testing.T) {
	sp := newTestPool()
	arr := subtreeArray{}
	for i := 1; i <= 4; i++ {
		sp.arrayPush(&arr, subtree(i<<8|1))
	}
	// Another region behind it, so growth has to move the array.
	other := subtreeArray{}
	sp.arrayPush(&other, subtree(99<<8|1))

	ins := []subtree{subtree(10<<8 | 1), subtree(11<<8 | 1), subtree(12<<8 | 1), subtree(13<<8 | 1), subtree(14<<8 | 1), subtree(15<<8 | 1)}
	sp.arraySplice(&arr, 1, 1, ins)
	want := []uint64{1, 10, 11, 12, 13, 14, 15, 3, 4}
	got := sp.array(arr)
	if len(got) != len(want) {
		t.Fatalf("len = %d", len(got))
	}
	for i, w := range want {
		if uint64(got[i]>>8) != w {
			t.Fatalf("elem %d = %d, want %d", i, got[i]>>8, w)
		}
	}
	if sp.array(other)[0]>>8 != 99 {
		t.Fatal("neighbour overwritten")
	}
}

// The union quirk: C reads repeat_depth from an external token's inline
// scanner state. (*Parser).lex stores those two bytes in repeatDepth.
func TestSubtreeRepeatDepthOfLeaves(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	inline := sp.newLeaf(tsAnon, length{}, length{1, Point{0, 1}}, 1, 1, false, false, false, lang)
	ext := sp.newLeaf(tsAnon, length{}, length{1, Point{0, 1}}, 1, 1, true, false, false, lang)
	if sp.repeatDepth(inline) != 0 || sp.repeatDepth(ext) != 0 {
		t.Fatal("fresh leaves have a repeat depth")
	}
	if sp.externalScannerState(ext) != nil || !sp.externalScannerStateEq(ext, nullSubtree) {
		t.Fatal("empty scanner state must equal the state of NULL")
	}
	// The state range shares the child count fields of the union.
	sp.ext = append(sp.ext, 1, 2, 3)
	sp.ptr(ext).setExt(1, 2)
	if string(sp.externalScannerState(ext)) != "\x02\x03" || sp.visibleChildCount(ext) != 0 {
		t.Fatal("scanner state range of a leaf")
	}
}

func TestChildClass(t *testing.T) {
	if s := unsafe.Sizeof(subtreeHeap{}); s != 80 {
		t.Errorf("subtreeHeap is %d bytes, want 80", s)
	}
	prevClass, prevSize := uint32(0), uint32(0)
	for _, n := range []uint32{1, 2, 63, 64, 65, 80, 81, 96, 97, 128, 129, 1000, 1 << 20, 1<<20 + 1, 1<<31 - 1, 1 << 31} {
		class, size := childClass(n)
		if size < n || uint64(size)*4 > uint64(n)*5+4 {
			t.Errorf("childClass(%d): size %d", n, size)
		}
		if c, s := childClass(size); c != class || s != size {
			t.Errorf("childClass(%d) = %d, %d but its size maps to %d, %d", n, class, size, c, s)
		}
		if class < prevClass || (class == prevClass) != (size == prevSize) || class >= childClassCount {
			t.Errorf("childClass(%d) = %d, %d after %d, %d", n, class, size, prevClass, prevSize)
		}
		prevClass, prevSize = class, size
	}
	if class, _ := childClass(65); class != exactChildClasses+1 {
		t.Errorf("first rounded class = %d", class)
	}
}

// A released node's children block serves the next request of its class, and
// an array that is copied one element longer each time, the shape of
// ts_parser__recover_to_state, keeps the slab linear in its length.
func TestChildrenBlockReuse(t *testing.T) {
	sp, lang := newTestPool(), testLanguage()
	leaf := subtree(tsAnon)<<inlSymbolShift | inlIsInline
	node := func(n int) subtree {
		arr := subtreeArray{}
		sp.arrayReserve(&arr, uint32(n))
		for i := 0; i < n; i++ {
			sp.arrayPush(&arr, leaf)
		}
		return sp.newNode(tsRule, &arr, 0, lang)
	}
	a, b := node(3), node(5)
	off := sp.ptr(a).children
	sp.release(a)
	if c := node(3); sp.ptr(c).children != off {
		t.Errorf("freed block not reused: %d, want %d", sp.ptr(c).children, off)
	}
	sp.release(b)

	const rounds = 5000
	errorTree := node(1)
	for i := 0; i < rounds; i++ {
		slice := subtreeArray{}
		sp.arrayReserve(&slice, 1)
		sp.arrayPush(&slice, leaf)
		sp.arraySplice(&slice, 0, 0, sp.childrenOf(errorTree))
		sp.release(errorTree)
		errorTree = sp.newNode(tsRule, &slice, 0, lang)
	}
	if n := sp.ptr(errorTree).childCount; n != rounds+1 {
		t.Fatalf("child count = %d", n)
	}
	if len(sp.children) > 40*rounds {
		t.Errorf("children slab has %d entries for a node of %d children", len(sp.children), rounds)
	}
}

// Random allocs, frees and array growth on the children slab. Every live
// block keeps its contents, free blocks merge back (the slab stays within a
// constant of the live peak) and freeing everything empties the slab.
func TestChildrenAllocator(t *testing.T) {
	sp := newTestPool()
	r := rand.New(rand.NewSource(1))
	type block struct {
		a   subtreeArray
		tag subtree
	}
	var live []block
	fill := func(b block, from uint32) {
		for i := from; i < b.a.size; i++ {
			sp.children[b.a.off+i] = b.tag + subtree(i)
		}
	}
	check := func(b block) {
		for i := uint32(0); i < b.a.size; i++ {
			if sp.children[b.a.off+i] != b.tag+subtree(i) {
				t.Fatalf("block at %d size %d: entry %d overwritten", b.a.off, b.a.size, i)
			}
		}
	}
	liveCap, peak := 0, 0
	for step := 0; step < 200000; step++ {
		switch k := r.Intn(10); {
		case k < 4 || len(live) == 0:
			n := uint32(1 + r.Intn(40))
			if r.Intn(4) == 0 {
				n = uint32(1 + r.Intn(3000))
			}
			b := block{tag: subtree(step) << 32}
			if r.Intn(3) == 0 {
				// As reduceInPlace does.
				b.a = subtreeArray{off: sp.bumpChildren(sp.room(n), n), size: n, cap: n}
				fill(b, 0)
				sp.settleChildren(&b.a)
			} else {
				b.a.off, b.a.cap = sp.allocChildren(n)
				b.a.size = n
				fill(b, 0)
			}
			live = append(live, b)
			liveCap += int(b.a.cap)
		case k < 7:
			i := r.Intn(len(live))
			b := &live[i]
			check(*b)
			liveCap -= int(b.a.cap)
			old := b.a.size
			sp.arrayReserve(&b.a, old+uint32(1+r.Intn(int(old)+8)))
			b.a.size = b.a.cap
			fill(*b, old)
			liveCap += int(b.a.cap)
		default:
			i := r.Intn(len(live))
			check(live[i])
			liveCap -= int(live[i].a.cap)
			sp.arrayFree(&live[i].a)
			live[i] = live[len(live)-1]
			live = live[:len(live)-1]
		}
		peak = max(peak, liveCap)
		if free := int(sp.quickFree + sp.mergedFree); 1+liveCap+free != len(sp.children) {
			t.Fatalf("step %d: slab %d entries, live %d, counted free %d", step, len(sp.children), liveCap, free)
		}
		if len(sp.children) > 4*peak+4096 {
			t.Fatalf("step %d: slab %d entries, live peak %d", step, len(sp.children), peak)
		}
	}
	for _, b := range live {
		check(b)
		sp.arrayFree(&b.a)
	}
	sp.flushQuick()
	if len(sp.children) != 1 || sp.freeLists != [3]uint64{} {
		t.Errorf("slab has %d entries and lists %x after freeing everything", len(sp.children), sp.freeLists)
	}
}

// Random subtrees of every kind summarizeChildren tells apart.
type randomTrees struct {
	sp   *subtreePool
	lang *Language
	r    *rand.Rand
	made []subtree
}

// testLanguage with two productions that alias a child, and kinds named.
func newRandomTrees(sp *subtreePool, seed int64) *randomTrees {
	lang := testLanguage()
	lang.MaxAliasSequenceLength = 4
	lang.AliasSequences = []Symbol{0, 0, 0, 0, tsIdent, 0, tsAnon, 0, 0, tsHidden, 0, 0}
	lang.KindMask("a", "ident", "rule", "ERROR")
	return &randomTrees{sp: sp, lang: lang, r: rand.New(rand.NewSource(seed))}
}

func (g *randomTrees) leaf() subtree {
	sp, r := g.sp, g.r
	sym := []Symbol{tsAnon, tsIdent, tsHidden}[r.Intn(3)]
	padding := length{uint32(r.Intn(9)), Point{uint32(r.Intn(3) / 2), uint32(r.Intn(9))}}
	n := uint32(r.Intn(20))
	size := length{n, Point{0, n}}
	state := StateID(r.Intn(50))
	var s subtree
	switch r.Intn(8) {
	case 0: // heap, over several rows
		size = length{300 + n, Point{uint32(r.Intn(3)), n}}
		s = sp.newLeaf(sym, padding, size, uint32(r.Intn(40)), state, false, false, false, g.lang)
	case 1: // external token; the scanner may have read the column
		s = sp.newLeaf(sym, padding, size, uint32(r.Intn(4)), state, true, r.Intn(2) == 0, false, g.lang)
		h := sp.ptr(s)
		if r.Intn(2) == 0 {
			h.flags |= fHasExternalScannerStateChange
		}
		// C reads repeat_depth from the scanner state of such a leaf.
		h.repeatDepth = uint16(r.Intn(4))
	case 2:
		if r.Intn(2) == 0 {
			padding.extent.Row = 20 // not inline
		}
		s = sp.newMissingLeaf(sym, padding, uint32(r.Intn(4)), g.lang)
	case 3:
		s = sp.newError('x', padding, size, uint32(r.Intn(4)), state, g.lang)
	case 4:
		s = sp.newLeaf(tsEnd, padding, length{}, 0, state, false, false, false, g.lang)
	default:
		s = sp.newLeaf(sym, padding, size, uint32(r.Intn(4)), state, false, false, false, g.lang)
	}
	if r.Intn(6) == 0 {
		sp.setExtra(&s, true)
	}
	return s
}

// A child for a new node: a leaf, a new node or one made before.
func (g *randomTrees) child(depth int) subtree {
	switch k := g.r.Intn(10); {
	case k < 2 && len(g.made) > 0:
		s := g.made[g.r.Intn(len(g.made))]
		g.sp.retain(s)
		return s
	case k < 5 && depth > 0:
		return g.node(depth - 1)
	}
	return g.leaf()
}

func (g *randomTrees) node(depth int) subtree {
	sp, r := g.sp, g.r
	n := 1 + r.Intn(5)
	if r.Intn(10) == 0 {
		n = 20 + r.Intn(60)
	}
	sym := []Symbol{tsRule, tsRule, tsRepeat, tsRepeat, tsHidden, SymbolErrorRepeat, SymbolError}[r.Intn(7)]
	arr := subtreeArray{}
	for i := 0; i < n; i++ {
		var c subtree
		if i == 0 && sym == tsRepeat && r.Intn(2) == 0 {
			// a repeat node below a repeat node: a repeat depth
			c = g.node(0)
			sp.ptr(c).symbol = tsRepeat
			sp.ptr(c).flags &^= fVisible | fNamed
			sp.summarizeChildrenRef(c, g.lang)
		} else {
			c = g.child(depth)
		}
		sp.arrayPush(&arr, c)
	}
	var s subtree
	if sym == SymbolError {
		s = sp.newErrorNode(&arr, r.Intn(2) == 0, g.lang)
	} else {
		productionID := uint32(0)
		if n <= 4 && sym != SymbolErrorRepeat {
			productionID = uint32(r.Intn(4) % 3)
		}
		s = sp.newNode(sym, &arr, productionID, g.lang)
		// what ts_parser__reduce sets after ts_subtree_new_node
		h := sp.ptr(s)
		if r.Intn(4) == 0 {
			h.flags |= fExtra
		}
		if r.Intn(3) == 0 {
			h.flags |= fFragileLeft | fFragileRight
			h.parseState = treeStateNone
		} else {
			h.parseState = StateID(r.Intn(50))
		}
		h.dynamicPrecedence += int32(r.Intn(3) - 1)
	}
	sp.retain(s) // made keeps one
	g.made = append(g.made, s)
	return s
}

func (g *randomTrees) check(t *testing.T, s subtree, c *SummaryCheck) {
	t.Helper()
	g.sp.checkSummary(s, g.lang, c)
	if c.Bad != "" {
		t.Fatalf("%s:\n%s", c.Bad, g.sp.string(s, 0, false, g.lang, true))
	}
}

// summarize, summarizeTwoHeap and both loops of summarizeChildren against
// summarizeChildrenRef, on random nodes.
func TestSummarizeRandom(t *testing.T) {
	var c SummaryCheck
	for seed := int64(0); seed < 300; seed++ {
		g := newRandomTrees(newTestPool(), seed)
		for i := 0; i < 40; i++ {
			g.check(t, g.node(3), &c)
		}
	}
	if c.TwoHeap < 1000 || c.Column < 1000 || c.Resumed < 1000 {
		t.Errorf("too few cases: %+v", c)
	}
}

// An ERROR node that takes the children of the one before it and some more,
// as in ts_parser__recover_to_state, must come out as one summarized from
// its first child. A summary left behind must not reach any other node.
func TestSummarizeResume(t *testing.T) {
	var c SummaryCheck
	for seed := int64(0); seed < 300; seed++ {
		sp := newTestPool()
		g := newRandomTrees(sp, seed)
		arr := subtreeArray{}
		sp.arrayPush(&arr, g.child(2))
		errorTree := sp.newErrorNode(&arr, true, g.lang)
		for round := 0; round < 30; round++ {
			slice := subtreeArray{}
			for i := g.r.Intn(4); i > 0; i-- {
				sp.arrayPush(&slice, g.child(2))
			}
			resume := *sp.ptr(errorTree)
			sp.arraySplice(&slice, 0, 0, sp.childrenOf(errorTree))
			for j := uint32(0); j < resume.childCount; j++ {
				sp.retain(sp.children[slice.off+j])
			}
			sp.release(errorTree)
			switch g.r.Intn(8) {
			case 0: // fewer children than the summary covers
				if slice.size > 1 {
					slice.size--
					sp.release(sp.children[slice.off+slice.size])
				}
			case 1: // not an ERROR node
				sp.resume = resume
				other := sp.newNode(tsRule, &slice, 0, g.lang)
				g.check(t, other, &c)
				slice = subtreeArray{}
				sp.arraySplice(&slice, 0, 0, sp.childrenOf(other))
				for _, child := range sp.array(slice) {
					sp.retain(child)
				}
				sp.release(other)
				resume.childCount = 0
			}
			sp.resume = resume
			errorTree = sp.newErrorNode(&slice, true, g.lang)
			if sp.resume.childCount != 0 {
				t.Fatal("summary to resume from left behind")
			}
			g.check(t, errorTree, &c)
		}
	}
}

// ts_subtree_compress as C has it, three summaries a turn, on
// summarizeChildrenRef: the reference for compress.
func (sp *subtreePool) compressRef(self subtree, count uint32, lang *Language, stack *[]subtree) {
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
		sp.summarizeChildrenRef(grandchild, lang)
		sp.summarizeChildrenRef(child, lang)
		sp.summarizeChildrenRef(tree, lang)
	}
}

// ts_parser__balance_subtree as C has it, on compressRef.
func balanceRef(sp *subtreePool, root subtree, lang *Language) {
	sp.treeStack = sp.treeStack[:0]
	if sp.childCount(root) > 0 && sp.ptr(root).refCount == 1 {
		sp.treeStack = append(sp.treeStack, root)
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
					sp.compressRef(tree, i, lang, &sp.treeStack)
					n -= i
				}
			}
		}
		sp.treeStack = sp.treeStack[:len(sp.treeStack)-1]
		for _, child := range sp.childrenOf(tree) {
			if child.isInline() {
				continue
			}
			if h := sp.ptr(child); h.childCount > 0 && h.refCount == 1 {
				sp.treeStack = append(sp.treeStack, child)
			}
		}
	}
}

// A left-leaning chain of repeat nodes, with extras between its children and
// chains inside it.
func (g *randomTrees) chain(depth int) subtree {
	sp, r := g.sp, g.r
	item := func() subtree {
		if depth > 8 && r.Intn(8) == 0 {
			return g.chain(r.Intn(depth / 2))
		}
		if r.Intn(4) == 0 {
			arr := subtreeArray{}
			sp.arrayPush(&arr, g.leaf())
			sp.arrayPush(&arr, g.leaf())
			return sp.newNode(tsRule, &arr, 0, g.lang)
		}
		return g.leaf()
	}
	arr := subtreeArray{}
	sp.arrayPush(&arr, item())
	sp.arrayPush(&arr, item())
	rep := sp.newNode(tsRepeat, &arr, 0, g.lang)
	for i := 0; i < depth; i++ {
		arr = subtreeArray{}
		sp.arrayPush(&arr, rep)
		if r.Intn(3) == 0 {
			extra := g.leaf()
			sp.setExtra(&extra, true)
			sp.arrayPush(&arr, extra)
		}
		sp.arrayPush(&arr, item())
		rep = sp.newNode(tsRepeat, &arr, 0, g.lang)
	}
	return rep
}

// compress, which leaves out a summary C makes, against compressRef: the
// same chains compressed by both must be the same in every field. Half of
// them are balanced, as the parser does it. The other half get one call:
// the passes of balancing summarize most nodes again and hide what a call
// leaves behind.
func TestCompressRandom(t *testing.T) {
	column := 0
	for seed := int64(0); seed < 2000; seed++ {
		build := func() (p *Parser, top, root subtree, count uint32) {
			p = &Parser{}
			p.pool.reset()
			g := newRandomTrees(&p.pool, seed)
			depth := 5 + g.r.Intn(120)
			top = g.chain(depth)
			arr := subtreeArray{}
			p.pool.arrayPush(&arr, top)
			p.pool.arrayPush(&arr, g.leaf())
			p.language = g.lang
			return p, top, p.pool.newNode(tsRule, &arr, 0, g.lang), uint32(1 + g.r.Intn(depth))
		}
		p, top, root, count := build()
		q, _, _, _ := build()
		a, b := &p.pool, &q.pool
		if seed%2 == 0 {
			p.finishedTree = root
			p.balanceSubtree()
			balanceRef(b, root, q.language)
		} else {
			a.compress(top, count, p.language, &a.treeStack)
			b.compressRef(top, count, q.language, &b.treeStack)
		}

		if a.heapLen != b.heapLen || len(a.children) != len(b.children) {
			t.Fatalf("seed %d: slabs differ in length", seed)
		}
		for i := uint32(1); i < a.heapLen; i++ {
			s := subtree(i) << 1
			if *a.ptr(s) != *b.ptr(s) {
				t.Fatalf("seed %d: node %d\n have %+v\n want %+v", seed, i, *a.ptr(s), *b.ptr(s))
			}
			if a.ptr(s).flags&fDependsOnColumn != 0 && a.ptr(s).repeatDepth > 0 {
				column++
			}
		}
		for i := range a.children {
			if a.children[i] != b.children[i] {
				t.Fatalf("seed %d: children slabs differ at %d", seed, i)
			}
		}
	}
	if column < 1000 {
		t.Errorf("%d repeat nodes that depend on the column: too few", column)
	}
}

// A cold parse must allocate as main did, or its GCs fall elsewhere and
// peak RSS moves: the free bits move with a growth, and links
// grow as append grows them.
func TestColdGrowthAsMain(t *testing.T) {
	childSlabs.Lock()
	clear(childSlabs.free[:])
	childSlabs.Unlock()
	linkSlabs.Lock()
	clear(linkSlabs.free[:])
	linkSlabs.Unlock()

	sp := newTestPool()
	a := sp.bumpChildren(sp.room(100), 100)
	sp.bumpChildren(sp.room(1), 1)
	sp.mergeFree(a, 100)
	bits := &sp.freeBits[0]
	sp.room(uint32(cap(sp.children)))
	if &sp.freeBits[0] != bits {
		t.Error("a growth reallocated the free bits")
	}
	if sp.freeBits[a>>5]>>(a&31*2)&1 == 0 {
		t.Error("a growth lost a free block")
	}

	var s stack
	s.init(sp)
	want := append([]moreLinkSet(nil), moreLinkSet{})
	for i := uint32(1); len(want) < shrinkLinks; i++ {
		if len(s.moreLinks) == cap(s.moreLinks) {
			s.growLinks()
		}
		s.moreLink(i, 1)
		want = append(want, moreLinkSet{})
		if cap(s.moreLinks) != cap(want) {
			t.Fatalf("link %d: cap %d, append gives %d", i, cap(s.moreLinks), cap(want))
		}
	}
}
