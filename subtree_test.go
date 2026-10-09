package gositter

import "testing"

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
	if len(sp.heap) != 1 {
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
	before := len(sp.heap)
	sp.newLeaf(tsIdent, length{}, big, 1, 1, false, false, false, lang)
	if len(sp.heap) != before || len(sp.freeTrees) != 2 {
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
