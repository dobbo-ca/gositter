package gositter

import "testing"

func newTestStack() (*stack, *subtreePool, *Language) {
	sp := newTestPool()
	s := &stack{}
	s.init(sp)
	return s, sp, testLanguage()
}

func testLeaf(sp *subtreePool, lang *Language, sym Symbol, heap bool) subtree {
	size := length{1, Point{0, 1}}
	if heap {
		size = length{300, Point{0, 300}}
	}
	return sp.newLeaf(sym, length{}, size, 1, 1, false, false, false, lang)
}

func TestStackPushPopCount(t *testing.T) {
	s, sp, lang := newTestStack()
	if s.versionCount() != 1 || s.state(0) != 1 || s.position(0).bytes != 0 {
		t.Fatal("fresh stack")
	}
	a := testLeaf(sp, lang, tsAnon, false)
	b := testLeaf(sp, lang, tsIdent, true)
	c := testLeaf(sp, lang, tsHidden, false)
	s.push(0, a, false, 2)
	s.push(0, b, false, 3)
	s.push(0, c, false, 4)
	if s.state(0) != 4 || s.position(0).bytes != 302 {
		t.Fatalf("state=%d pos=%d", s.state(0), s.position(0).bytes)
	}
	// Visible subtrees count as nodes: a and b.
	if got := s.nodeCountSinceError(0); got != 2 {
		t.Fatalf("node count = %d", got)
	}

	pop := s.popCount(0, 2)
	if len(pop) != 1 {
		t.Fatalf("slices = %d", len(pop))
	}
	got := sp.array(pop[0].subtrees)
	if len(got) != 2 || got[0] != b || got[1] != c {
		t.Fatalf("popped %v, want [b c] in stack order", got)
	}
	// The pop made a new version at the node below; the old one is intact.
	if s.versionCount() != 2 || pop[0].version != 1 || s.state(1) != 2 || s.state(0) != 4 {
		t.Fatalf("versions=%d slice.version=%d", s.versionCount(), pop[0].version)
	}
	// The slice holds its own reference to the heap leaf.
	if sp.ptr(b).refCount != 2 {
		t.Fatalf("refcount = %d, want 2", sp.ptr(b).refCount)
	}
	s.renumberVersion(1, 0)
	if s.versionCount() != 1 || s.state(0) != 2 || sp.ptr(b).refCount != 1 {
		t.Fatalf("after renumber: versions=%d state=%d ref=%d", s.versionCount(), s.state(0), sp.ptr(b).refCount)
	}
}

func TestStackPopCountSkipsExtras(t *testing.T) {
	s, sp, lang := newTestStack()
	a := testLeaf(sp, lang, tsAnon, false)
	extra := testLeaf(sp, lang, tsIdent, false)
	sp.setExtra(&extra, true)
	s.push(0, a, false, 2)
	s.push(0, extra, false, 2)
	pop := s.popCount(0, 1)
	// The extra does not count towards the goal but is part of the slice.
	if got := sp.array(pop[0].subtrees); len(got) != 2 || got[0] != a || got[1] != extra {
		t.Fatalf("popped %v", got)
	}
	var trailing []subtree
	arr := pop[0].subtrees
	sp.arrayRemoveTrailingExtras(&arr, &trailing)
	if arr.size != 1 || len(trailing) != 1 || trailing[0] != extra {
		t.Fatalf("trailing extras: size=%d trailing=%v", arr.size, trailing)
	}
}

func TestStackMergeAndAmbiguousPop(t *testing.T) {
	s, sp, lang := newTestStack()
	a := testLeaf(sp, lang, tsAnon, true)
	b := testLeaf(sp, lang, tsIdent, true)

	// Two versions reach the same state and position over different subtrees.
	v1 := s.copyVersion(0)
	s.push(0, a, false, 5)
	s.push(v1, b, false, 5)
	if !s.canMerge(0, v1) {
		t.Fatal("versions should be mergeable")
	}
	if !s.merge(0, v1) || s.versionCount() != 1 {
		t.Fatal("merge failed")
	}
	if n := &s.nodes[s.heads[0].node]; n.linkCount != 2 {
		t.Fatalf("links = %d, want 2", n.linkCount)
	}

	// Popping one subtree walks both links. Both paths end at the base node,
	// so both slices carry the same version, first link first.
	pop := s.popCount(0, 1)
	if len(pop) != 2 || pop[0].version != pop[1].version {
		t.Fatalf("slices = %d", len(pop))
	}
	if sp.array(pop[0].subtrees)[0] != a || sp.array(pop[1].subtrees)[0] != b {
		t.Fatal("slice order does not follow link order")
	}

	// A different state does not merge.
	v2 := s.copyVersion(0)
	s.push(v2, testLeaf(sp, lang, tsAnon, false), false, 6)
	if s.canMerge(0, v2) {
		t.Fatal("different states must not merge")
	}
}

func TestStackEquivalentLinksCollapse(t *testing.T) {
	s, sp, lang := newTestStack()
	v1 := s.copyVersion(0)
	// Inline leaves with the same bits are the same subtree value.
	s.push(0, testLeaf(sp, lang, tsAnon, false), false, 5)
	s.push(v1, testLeaf(sp, lang, tsAnon, false), false, 5)
	if !s.merge(0, v1) {
		t.Fatal("merge failed")
	}
	if n := &s.nodes[s.heads[0].node]; n.linkCount != 1 {
		t.Fatalf("links = %d, want 1", n.linkCount)
	}
}

func TestStackPauseResumeHalt(t *testing.T) {
	s, sp, lang := newTestStack()
	la := testLeaf(sp, lang, tsAnon, false)
	base := s.errorCost(0)
	s.pause(0, la)
	if !s.isPaused(0) || s.isActive(0) || s.errorCost(0) != base+errorCostPerRecovery {
		t.Fatal("pause")
	}
	if got := s.resume(0); got != la || !s.isActive(0) {
		t.Fatal("resume")
	}
	s.halt(0)
	if !s.isHalted(0) {
		t.Fatal("halt")
	}
}

func TestStackErrorDiscontinuityAndSummary(t *testing.T) {
	s, sp, lang := newTestStack()
	s.push(0, testLeaf(sp, lang, tsAnon, false), false, 2)
	s.push(0, testLeaf(sp, lang, tsIdent, false), false, 3)
	s.push(0, nullSubtree, false, errorState)
	if got := s.errorCost(0); got != errorCostPerRecovery {
		t.Fatalf("error cost = %d", got)
	}
	if s.nodeCountSinceError(0) != 0 {
		t.Fatal("node count since error should restart at the discontinuity")
	}
	s.recordSummary(0, maxSummaryDepth)
	sum := s.getSummary(0)
	// Depth 0 is the error state itself, then states 3, 2 and the base.
	want := []stackSummaryEntry{
		{length{2, Point{0, 2}}, 0, errorState},
		{length{2, Point{0, 2}}, 1, 3},
		{length{1, Point{0, 1}}, 2, 2},
		{length{}, 3, 1},
	}
	if len(sum) != len(want) {
		t.Fatalf("summary = %+v", sum)
	}
	for i := range want {
		if sum[i] != want[i] {
			t.Fatalf("summary[%d] = %+v, want %+v", i, sum[i], want[i])
		}
	}
	if s.versionCount() != 1 {
		t.Fatal("summary walk changed the versions")
	}
}

func TestStackClearReleases(t *testing.T) {
	s, sp, lang := newTestStack()
	b := testLeaf(sp, lang, tsIdent, true)
	s.push(0, b, false, 2)
	s.copyVersion(0)
	s.clear()
	if s.versionCount() != 1 || s.state(0) != 1 || len(sp.freeTrees) != 1 {
		t.Fatalf("versions=%d state=%d freed=%d", s.versionCount(), s.state(0), len(sp.freeTrees))
	}
	if s.nodes[s.baseNode].refCount != 2 {
		t.Fatalf("base node refcount = %d, want 2", s.nodes[s.baseNode].refCount)
	}
}
