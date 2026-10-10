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
}

func TestChildClass(t *testing.T) {
	if s := unsafe.Sizeof(subtreeHeap{}); s != 88 {
		t.Errorf("subtreeHeap is %d bytes, want 88", s)
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
			b.a.off, b.a.cap = sp.allocChildren(n)
			b.a.size = n
			fill(b, 0)
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
