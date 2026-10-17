The pure-Go retention policy closes most of the one-thread gap: one-thread peak RSS drops 24 to 47% against main, and every 16-thread median is at or below main. Trees are unchanged, and a parser now comes back down after a pathological input. Parse instructions are slightly lower than main on all four lists. User-mode cycles are probably not: python real runs about +2% in every batch, and that is not proven to be noise.

## Design (prototype v6)

**The rule.** At `Reset`, a slab goes to a shared pool when it is above a size threshold of about 512 KB and the parse before used under 1/8 of it. The thresholds are children 64Ki entries (the old `shrinkChildren`), stack nodes 8Ki and moreLinks 4Ki. The children slab's freeBits go to the pool with it. The parser then falls back to a small base slab: children 4096 entries (32 KB), nodes 1024 (48 KB), moreLinks 512 (56 KB). Each base is made at that parser's first give-up, so a parser that never gives a slab up holds the same memory as on main.

**Taking from the pool.** Only a parse that outgrows its base looks in the pool. It takes the smallest pooled slab, by power-of-two class, that fits; otherwise it doubles with `make` as today. A slab outgrown mid-parse is never pooled, because `arraySplice` may still read it; it goes to the GC.
- The uint32 slabs (freeTrees, nodePool, stack.more) are not pooled. They are at most 1/12 of the slab they index. They are kept below 1<<20 entries (4 MB) and dropped for the GC above that by the same 1/8 rule, so their hot appends need no guard.
- Heap chunks are unchanged: 8 kept, the rest go to the existing `sync.Pool`.

**Why the pool releases correctly.** The pool is a mutex plus a list of `weak.Pointer` per class.
- Pooled slabs are not marked by the GC, so they never raise the heap goal, and the next GC frees whatever no parser took.
- At one thread, steady garbage falls from 2 to 35 MB per pass to 13 to 117 KB per pass, so no GC runs after warm-up and the pool keeps the slab the parser needs. That gives never-drop's one-thread numbers without per-parser retention.
- At 16 threads, what is retained at a GC is only what parsers hold at that moment. The stack slabs, which main never shrinks, are now given back. That is why cpp broken at 16 threads falls from 1370 to 567 MiB.

**Why not `sync.Pool`.** Its per-P private slot hides the last slab put from a goroutine that moved to another P. Measured in a local test: one pass regrew 6.6 MB with zero GCs. Bytes allocated per pass are not timing, so this was fine to run on the Mac.

**Hot path.** Putting a pool call inside `bumpChildren` stopped it being inlined at 7 sites, which cost +1.5% instructions in v1 and +0.5% in v2. v6 splits it into `bumpChildren(room(n), n)`, where both functions inline and `growChildren` is `//go:noinline` (the same pattern as `api.go:249`). The design is safe because slabs are only given away at `Reset`, when the previous `Tree` is already dead by contract; heap chunks already rely on the same contract. Pooled freeBits are cleared before they are put back. It uses no unsafe, syscall, assembly, build tags or SetGCPercent; `weak` needs Go 1.24 and go.mod is at 1.25.

## Peak RSS (MiB, VmHWM, `-what parse -n 40 -warm 1`, 7 runs per binary, run in alternation)

Binaries: main = `/data/v/fix4/gsbench`, mine = `/data/m5-c/gsbench6`, C = `/data/bin/cbench`.

| row | main | mine | C | mine/main | mine/C |
|---|---|---|---|---|---|
| python real j1 | 54.7 [53.7-55.1] | 34.3 [33.9-35.4] | 24.4 [24.1-24.5] | 0.63 | 1.41 |
| python broken j1 | 80.8 [80.6-87.7] | 53.4 [53.3-53.9] | 41.8 [41.5-42.2] | 0.66 | 1.28 |
| bash real j1 | 27.1 [27.0-28.0] | 19.3 [19.0-19.9] | 10.2 [10.1-10.3] | 0.71 | 1.90 |
| bash broken j1 | 112.2 [109.4-112.7] | 59.1 [59.0-59.6] | 49.1 [48.8-49.3] | 0.53 | 1.20 |
| hcl real j1 | 29.4 [28.8-29.9] | 22.4 [22.1-22.6] | 13.0 [12.7-13.1] | 0.76 | 1.72 |
| golang real j1 | 53.5 [53.2-55.3] | 30.3 [30.1-30.4] | 24.0 [23.9-24.2] | 0.57 | 1.27 |
| cpp broken j1 | 274.9 [269.1-283.2] | 185.3 [184.5-210.4] | 137.5 [137.2-137.7] | 0.67 | 1.35 |
| python real j16 | 152.6 [148.5-155.1] | 148.6 [144.2-158.9] | 170.2 [167.8-170.5] | 0.97 | 0.87 |
| python broken j16 | 345.4 [336.8-359.7] | 232.1 [214.3-244.8] | 328.4 [290.1-339.3] | 0.67 | 0.71 |
| bash real j16 | 81.2 [78.4-83.2] | 61.0 [60.4-66.1] | 68.3 [64.4-70.1] | 0.75 | 0.89 |
| bash broken j16 | 338.9 [322.5-346.0] | 218.5 [205.5-232.0] | 402.3 [354.4-433.9] | 0.64 | 0.54 |
| hcl real j16 | 97.7 [92.4-98.4] | 81.0 [77.4-86.2] | 84.9 [81.8-90.8] | 0.83 | 0.95 |
| golang real j16 | 140.2 [138.5-142.2] | 123.5 [109.8-130.1] | 175.9 [174.6-176.9] | 0.88 | 0.70 |
| cpp broken j16 | 1370.4 [1299.5-1459.0] | 567.2 [535.1-599.1] | 1798.7 [1784.7-2010.2] | 0.41 | 0.32 |

## Allocations per steady pass at -j 1 (mallocs, KiB)

These come from the `go_mallocs` / `go_alloc_bytes` fields of the n=40 end rows divided by 40. I did not use `-allocs`, which only changes the cgo side.

| list | main | mine |
|---|---|---|
| python real | 3511, 18328 | 3993, 104 |
| python broken | 1750, 12328 | 2022, 62 |
| bash real | 904, 2062 | 949, 29 |
| bash broken | 694, 16405 | 791, 29 |
| hcl real | 3528, 2121 | 4176, 117 |
| golang real | 47, 12212 | 448, 13 |
| cpp broken | 544, 35310 | 940, 73 |

The extra mallocs are the box and weak handle each `put` allocates.

## Parse cost, `perf stat` (n12 minus n2) / 10, 5 runs, millions per pass

| list | instructions main → mine | ratio | cycles main → mine | ratio |
|---|---|---|---|---|
| python real | 8480.15 → 8477.22 | 0.9997 | 2121 [2105-2182] → 2162 [2143-2168] | 1.019 |
| golang real | 6950.68 → 6948.72 | 0.9997 | 1727 [1719-1733] → 1728 [1721-1736] | 1.001 |
| hcl real | 8439.01 → 8436.66 | 0.9997 | 1740 [1700-1778] → 1805 [1793-1809] | 1.037 |
| bash real | 1219.00 → 1217.79 | 0.9990 | 323 [319-325] → 327 [324-334] | 1.013 |

Cycle controls, using in-process user-mode counters over 72 reps each:
- **Same batch as v6, byte-identical copy of main:** hcl 0.998, python 0.999, bash 0.999, golang 1.020. v6 in that batch: 1.000, 1.018, 1.018, 1.013.
- **Layout-only control** (main plus the `room`/`bumpChildren` split, no pools): python 0.993, bash 1.010. v6 in that batch: 1.016, 1.019.
- **PMU counters** on python real (counters multiplexed, so rough): cycles:u +2.2%, last-level cache misses +5.9%, dTLB misses −40%, page faults −86%, kernel cycles −25%.

## Correctness

Node counts, error-file counts and the walk and cursor checksums are identical to main on python, bash, golang and cpp, real and broken: all 16 pairs. `nice go test .` passes, including a new `TestSlabsPooled`. On the Mac I also ran `go test -race` on TestRegress, TestSlabsPooled, TestChildren* and TestHeapChunks (these include tests that run parsers in parallel); all passed.

## Pathological mixed order: one input, then 2000 small real files, one process

The extreme input comes first in each list. Cells show Go HeapInuse in MiB at three points, main → mine. "After 2 GCs" means two forced `runtime.GC` with the parser kept alive; my first version of this probe let the parser be collected, so I added `runtime.KeepAlive`. No GC runs during the small files in either binary, so the "end" column still contains garbage.

| first input | after the big one | end | after 2 GCs |
|---|---|---|---|
| python big5mb (6 MB) | 336.8 → 336.8 | 337.2 → 337.2 | 4.1 → 4.1 |
| python broken_stmt n100000 | 198.1 → 198.1 | 198.4 → 198.4 | 56.7 → 8.8 |
| cpp advdeep_100000 | 666.8 → 666.9 | 667.2 → 667.4 | 223.0 → 4.1 |
| cpp.broken list, then small | – | 180-194 → 192 | 54.4 → 9.0 |
| cpp broken_stmt n30000 (10 runs) | 92-104 → 92-104 | about the same | 45.7 → 8.4 |

HeapSys after the big input was equal in both binaries (203, 343 and 835 MiB for the first three rows).

Peak RSS in these runs:
- python and advdeep: equal within 0.3 MiB.
- cpp broken_stmt: median 124.1 [118.9-136.3] for main against 122.6 [119.2-145.9] for mine. Both are bimodal because of GC pacer phase.

What main still holds after 2 GCs: stack.nodes 24-96 MiB and moreLinks 13-101 MiB. What mine still holds: 8 heap chunks (1.25 MiB) plus uint32 slabs under the 4 MB threshold (up to 3.3 MiB each).

## Did any row get larger or slower than main

- **RSS:** no row's median is larger. Single runs above main's maximum: python real j16 at 158.9 (main max 155.1), and one of 10 fresh-process cpp broken_stmt runs at 145.9 (main max 136.3). Peak RSS on the python pathological inputs is +0.0 to +0.3 MiB.
- **Instructions:** lower on all four lists, and the min-max ranges do not overlap.
- **Cycles:** python real is +1.6 to +2.2% in all 5 batches where the identical-copy control moved 0.1%. Bash real is +0.9 to +2.6%. hcl and golang are mixed. I take python's +2% as real, cause unproven (the PMU numbers point to cache-cold reuse of the pooled slab). By the brief's 3% band it is inside the noise; by the same-batch control it is not.

## What this design does not fix

- One thread is still 1.20 to 1.90 times C; bash real (1.90) and hcl real (1.72) are worst. The remainder is the Go runtime, the comb table, heap chunks held live in the `sync.Pool`, and garbage from doubling copies during the warm pass.
- The peak of a first huge parse in a fresh process is set by GC pacer phase, in main and in mine.
- uint32 slabs under 4 MB, `ext`, `treeStack`, summaries and the scan maps are never shrunk. Heap chunks still take 2 GCs to free.
- If the caller's own garbage triggers a GC between a give-up and the next big parse, the slab is freed and regrown, which is main's behaviour.

## Risks

- **Oversized takes:** a medium parse just past its base can take a pooled slab much larger than it needs and holds it until the next reset. This is the likely cause of the j16 python run at 158.9.
- **New invariant:** every `bumpChildren` must follow `room(n)`. A missed `room` panics on slice bounds rather than corrupting.
- **Stale trees:** a caller still using a Tree after the next Parse may read memory another goroutine is writing. Heap chunks already have this exposure.
- **Contention:** one mutex per slab type, taken only on give-up and grow events. Not tested above 16 threads.
- **Cycles:** the cycle cost above.

## What I did not measure

- RSS in walk and cursor modes.
- The realerr and extreme lists at -n 40.
- Grammars outside the seven rows above.
- Thread counts other than 1 and 16.
- This design combined with fix5a (scanner reuse).
- A long-running caller that allocates on its own.
- C in the pathological test.
- The cause of the cycle increase.

## Diff against origin/main (the exact measured source; md5 9b411ef2… matches `/work/m5-c-final`)

```diff
--- a/subtree.go
+++ b/subtree.go
@@ -4,8 +4,10 @@
 	"bytes"
 	"math"
 	"math/bits"
+	"slices"
 	"strconv"
 	"sync"
+	"weak"
 )
 
 // subtree.c and subtree.h.
@@ -137,13 +139,105 @@
 // need, not one high-water mark each.
 const keptHeapChunks = 8
 
-// A reset drops a children slab above shrinkChildren entries when the parse
-// before it used under an eighth of it: a pooled parser held the slab of its
-// largest input for good, and that counted in every heap goal.
-const shrinkChildren = 1 << 16
+// A reset gives a slab above these sizes (about 512 KB) to a slabPool when
+// the parse before it used under an eighth of it: a pooled parser held the
+// slab of its largest input for good, and that counted in every heap goal.
+const (
+	shrinkChildren = 1 << 16 // children, 8-byte entries
+	shrinkNodes    = 1 << 13 // stack.nodes, 48 bytes
+	shrinkLinks    = 1 << 12 // stack.moreLinks, 112 bytes
+)
+
+// A reset that gives a slab up falls back to a base slab of this many
+// entries, made at the first such reset. Only a parse that outgrows the base
+// takes a pooled slab: a small parse that took one would hold it at the next
+// GC, so a pathological input would never be freed.
+const (
+	baseChildren = 1 << 12
+	baseBits     = baseChildren / 32 // freeBits of a base children slab
+	baseNodes    = 1 << 10
+	baseLinks    = 1 << 9
+)
+
+// The uint32 slabs (free lists and stack.more) are a twelfth of the slab they
+// index or less. Above shrinkIndexes entries (4 MB) a reset drops them for the
+// GC by the same rule; below it they are kept, so their growth needs no pool.
+const shrinkIndexes = 1 << 20
 
 var heapChunks = sync.Pool{New: func() any { return new(heapChunk) }}
 
+// Slabs that resets gave up, shared by all parsers. A parser that outgrows a
+// slab takes the smallest pooled one that fits, so a large input does not
+// regrow its slabs by doubling on every parse. The pool holds them by weak
+// pointer, so the next GC frees what no parser took. Not a sync.Pool: that
+// hides the last slab put from the other Ps, and a parse that moved to
+// another P missed it. Only a reset gives a slab back: a grown slab may still
+// be read until the parse ends.
+type slabPool[T any] struct {
+	mu    sync.Mutex
+	slabs [32][]weak.Pointer[[]T] // class k: capacities from 2^k up
+}
+
+var (
+	childSlabs slabPool[subtree]
+	bitSlabs   slabPool[uint64]
+	nodeSlabs  slabPool[stackNode]
+	linkSlabs  slabPool[[maxLinkCount - 1]stackLink]
+)
+
+// get returns an empty pooled slab with room for n > 0 entries, or nil.
+func (p *slabPool[T]) get(n int) []T {
+	p.mu.Lock()
+	defer p.mu.Unlock()
+	for k := bits.Len(uint(n - 1)); k < len(p.slabs); k++ {
+		for l := p.slabs[k]; len(l) > 0; {
+			w := l[len(l)-1]
+			l = l[:len(l)-1]
+			p.slabs[k] = l
+			if s := w.Value(); s != nil {
+				return (*s)[:0]
+			}
+		}
+	}
+	return nil
+}
+
+// grow returns s, moved to a pooled slab with room for n entries if there is
+// one.
+func (p *slabPool[T]) grow(s []T, n int) []T {
+	if g := p.get(n); g != nil {
+		return append(g, s...)
+	}
+	return s
+}
+
+func (p *slabPool[T]) put(s []T) {
+	c := cap(s)
+	if c == 0 {
+		return
+	}
+	box := new([]T)
+	*box = s[:0]
+	p.mu.Lock()
+	k := bits.Len(uint(c)) - 1
+	// Forget what the GC has freed.
+	p.slabs[k] = append(slices.DeleteFunc(p.slabs[k], func(w weak.Pointer[[]T]) bool { return w.Value() == nil }), weak.Make(box))
+	p.mu.Unlock()
+}
+
+// shrink reports whether a reset gives up a slab of c entries above max, of
+// which the parse before used n.
+func shrink(c, n, max int) bool { return c > max && n < c/8 }
+
+// trim is the reset of slab s: nil if it goes to p.
+func (p *slabPool[T]) trim(s []T, n, max int) []T {
+	if shrink(cap(s), n, max) {
+		p.put(s)
+		return nil
+	}
+	return s[:0]
+}
+
 // reserveHeap takes the next chunk from heapChunks, one chunk ahead of need.
 // allocate is inlined into the reduce path and a call there would end that,
 // so it only makes new chunks, when a round of advances outruns this.
@@ -161,6 +255,7 @@
 	heapLen   uint32       // entries handed out, [0] included
 	freeTrees []uint32     // free_trees
 	children  []subtree    // [0] unused
+	base      []subtree    // see baseChildren
 	ext       []byte       // external scanner states
 	treeStack []subtree    // tree_stack
 
@@ -222,7 +317,10 @@
 func (sp *subtreePool) reset() {
 	if cap(sp.children) == 0 {
 		sp.heap = []*heapChunk{new(heapChunk)}
-		sp.children = make([]subtree, 1, 4096)
+		sp.children = make([]subtree, 1, baseChildren)
+	}
+	if shrink(cap(sp.freeTrees), int(sp.heapLen), shrinkIndexes) && sp.heapLen > 1 {
+		sp.freeTrees = nil
 	}
 	sp.heapLen = 1
 	if len(sp.heap) > keptHeapChunks {
@@ -232,14 +330,20 @@
 		}
 		sp.heap = sp.heap[:keptHeapChunks]
 	}
+	// Pooled bit slabs are all zero.
+	clear(sp.freeBits[:min(len(sp.freeBits), (len(sp.children)+31)/32)])
 	// len 1: nothing was parsed since the last reset
-	if n := len(sp.children); n > 1 && cap(sp.children) > shrinkChildren && n < cap(sp.children)/8 {
-		sp.children = make([]subtree, 1, 4096)
-		sp.freeBits = nil
-	} else {
-		clear(sp.freeBits[:min(len(sp.freeBits), (len(sp.children)+31)/32)])
-		sp.children = sp.children[:1]
+	if n := len(sp.children); n > 1 && childSlabs.trim(sp.children, n, shrinkChildren) == nil {
+		if sp.base == nil {
+			sp.base = make([]subtree, 1, baseChildren)
+		}
+		sp.children = sp.base
+		if cap(sp.freeBits) > baseBits {
+			bitSlabs.put(sp.freeBits)
+			sp.freeBits = nil
+		}
 	}
+	sp.children = sp.children[:1]
 	sp.freeBlocks = [childClassCount]uint32{}
 	sp.freeLists = [3]uint64{}
 	sp.quickBlocks = [exactChildClasses + 1]uint32{}
@@ -308,26 +412,37 @@
 	return sp.children[a.off : a.off+a.size]
 }
 
-// Bump allocation of n slots at the end of the children slab.
-func (sp *subtreePool) bumpChildren(n uint32) uint32 {
+// Bump allocation of n slots at the end of the children slab:
+// bumpChildren(room(n), n). room makes space for them and returns where they
+// start. Two calls, so that both are inlined.
+func (sp *subtreePool) room(n uint32) int {
 	off := len(sp.children)
-	need := off + int(n)
-	if need > cap(sp.children) {
-		sp.growChildren(need)
+	if off+int(n) > cap(sp.children) {
+		sp.growChildren(n)
 	}
-	sp.children = sp.children[:need]
+	return off
+}
+
+func (sp *subtreePool) bumpChildren(off int, n uint32) uint32 {
+	sp.children = sp.children[:off+int(n)]
 	return uint32(off)
 }
 
-// The capacity never passes 2^32-1 entries, so every offset and length of
-// the slab fits in 32 bits.
-func (sp *subtreePool) growChildren(need int) {
+// growChildren takes a pooled slab that fits, else doubles. The capacity
+// never passes 2^32-1 entries, so every offset and length of the slab fits
+// in 32 bits. Kept out of line so that room fits the inlining budget.
+//
+//go:noinline
+func (sp *subtreePool) growChildren(n uint32) {
+	need := len(sp.children) + int(n)
 	if need > math.MaxUint32 {
 		panic(errSlabFull)
 	}
-	grown := make([]subtree, len(sp.children), min(max(2*cap(sp.children), need), math.MaxUint32))
-	copy(grown, sp.children)
-	sp.children = grown
+	grown := childSlabs.get(need)
+	if grown == nil {
+		grown = make([]subtree, 0, min(max(2*cap(sp.children), need), math.MaxUint32))
+	}
+	sp.children = append(grown, sp.children...)
 }
 
 // Moves a region at the end of the slab, as popLinear leaves it, into a free
@@ -471,7 +586,7 @@
 	if off = sp.takeFree(class, size); off != 0 {
 		return off, size
 	}
-	return sp.bumpChildren(size), size
+	return sp.bumpChildren(sp.room(size), size), size
 }
 
 // Gives back a block of up to exactChildClasses entries. C calls free here.
@@ -486,8 +601,17 @@
 // the end of the slab is cut off, the rest goes on its list.
 func (sp *subtreePool) mergeFree(off, size uint32) {
 	if n := (cap(sp.children) + 31) / 32; n > len(sp.freeBits) {
-		grown := make([]uint64, n)
+		var grown []uint64
+		if n > baseBits {
+			grown = bitSlabs.get(n)
+		}
+		if grown == nil {
+			grown = make([]uint64, n)
+		}
+		grown = grown[:cap(grown)]
 		copy(grown, sp.freeBits)
+		clear(sp.freeBits)
+		bitSlabs.put(sp.freeBits)
 		sp.freeBits = grown
 	}
 	if end := off + size; end < uint32(len(sp.children)) && sp.freeBits[end>>5]>>(end&31*2)&1 != 0 {
@@ -520,11 +644,11 @@
 	off := sp.takeFree(class, newCap)
 	if off == 0 {
 		if a.cap > 0 && a.off+a.cap == uint32(len(sp.children)) {
-			sp.bumpChildren(newCap - a.cap)
+			sp.bumpChildren(sp.room(newCap-a.cap), newCap-a.cap)
 			a.cap = newCap
 			return
 		}
-		off = sp.bumpChildren(newCap)
+		off = sp.bumpChildren(sp.room(newCap), newCap)
 	}
 	copy(sp.children[off:off+a.size], sp.children[a.off:a.off+a.size])
 	size := a.size
@@ -1618,7 +1742,7 @@
 		if size <= children.cap {
 			sp.children = sp.children[:children.off+size]
 		} else {
-			sp.bumpChildren(size - children.cap)
+			sp.bumpChildren(sp.room(size-children.cap), size-children.cap)
 		}
 	} else {
 		class, _ = childClass(children.cap)
--- a/stack.go
+++ b/stack.go
@@ -105,10 +105,12 @@
 	forks     []stackFork // popAll
 	chain     []uint32    // popAllSubtrees scratch
 	nodes     []stackNode // [0] unused
+	base      []stackNode // see baseNodes
 	// Links 1 to 7 of the nodes that have them: moreLinks[more[node]], where
 	// 0 means none yet. A node index keeps its entry until reset.
 	more      []uint32
 	moreLinks [][maxLinkCount - 1]stackLink // [0] unused
+	linkBase  [][maxLinkCount - 1]stackLink // see baseLinks
 	nodePool  []uint32                      // node_pool
 	baseNode  uint32
 	sp        *subtreePool
@@ -191,7 +193,14 @@
 	if n > math.MaxUint32/2 {
 		panic(errSlabFull)
 	}
-	s.nodes = append(make([]stackNode, 0, 2*n), s.nodes...)
+	var grown []stackNode
+	if n >= baseNodes {
+		grown = nodeSlabs.get(n + 1)
+	}
+	if grown == nil {
+		grown = make([]stackNode, 0, 2*n)
+	}
+	s.nodes = append(grown, s.nodes...)
 }
 
 // stack__subtree_node_count
@@ -365,6 +374,9 @@
 	if n.linkCount == 0 {
 		n.links[0] = link
 	} else {
+		if len(s.moreLinks) == cap(s.moreLinks) && len(s.moreLinks) >= baseLinks {
+			s.moreLinks = linkSlabs.grow(s.moreLinks, len(s.moreLinks)+1)
+		}
 		*s.moreLink(self, int(n.linkCount)) = link
 	}
 	n.linkCount++
@@ -650,6 +662,28 @@
 	s.heads = s.heads[:0]
 	s.slices = s.slices[:0]
 	s.iterators = s.iterators[:0]
+	// The slabs of a deep stack go back as the subtree slabs do. len 2:
+	// nothing was parsed since the last reset.
+	if n := len(s.nodes); n > 2 {
+		if nodeSlabs.trim(s.nodes, n, shrinkNodes) == nil {
+			if s.base == nil {
+				s.base = make([]stackNode, 1, baseNodes)
+			}
+			s.nodes = s.base
+		}
+		if shrink(cap(s.nodePool), n, shrinkIndexes) {
+			s.nodePool = nil
+		}
+		if shrink(cap(s.more), len(s.more), shrinkIndexes) {
+			s.more = nil
+		}
+		if linkSlabs.trim(s.moreLinks, len(s.moreLinks), shrinkLinks) == nil {
+			if s.linkBase == nil {
+				s.linkBase = make([][maxLinkCount - 1]stackLink, 0, baseLinks)
+			}
+			s.moreLinks = s.linkBase
+		}
+	}
 	if cap(s.nodes) == 0 {
 		s.nodes = make([]stackNode, 1, 64)
 	}
@@ -760,7 +794,7 @@
 		} else if counted == 0 {
 			trailing++
 		}
-		sp.children[sp.bumpChildren(1)] = link.subtree
+		sp.children[sp.bumpChildren(sp.room(1), 1)] = link.subtree
 		s.nodePool = append(s.nodePool, below)
 		below = link.node
 	}
--- a/parser.go
+++ b/parser.go
@@ -661,7 +661,7 @@
 	sp := &p.pool
 	mark := len(sp.children)
 	scratch := subtreeArray{size: children.size, cap: children.size}
-	scratch.off = sp.bumpChildren(children.size)
+	scratch.off = sp.bumpChildren(sp.room(children.size), children.size)
 	copy(sp.array(scratch), sp.array(children))
 
 	scratchTree := sp.newNode(sp.symbol(left), &scratch, 0, p.language)
@@ -1820,7 +1820,7 @@
 
 	// ts_tree_new. The root gets a slot in the children slab so that a Node
 	// can address it like any other subtree.
-	root := p.pool.bumpChildren(1)
+	root := p.pool.bumpChildren(p.pool.room(1), 1)
 	p.pool.children[root] = p.finishedTree
 	p.tree = Tree{pool: &p.pool, language: p.language, root: root}
 	p.finishedTree = nullSubtree
--- a/export_test.go
+++ b/export_test.go
@@ -37,12 +37,16 @@
 // ChildrenSlabCap is the capacity of the children slab, in entries.
 func (p *Parser) ChildrenSlabCap() int { return cap(p.pool.children) }
 
+// StackSlabCap is the capacity of the stack node slab, in entries.
+func (p *Parser) StackSlabCap() int { return cap(p.stack.nodes) }
+
 // HeapChunks is the number of heap chunks the parser holds.
 func (p *Parser) HeapChunks() int { return len(p.pool.heap) }
 
 const (
 	KeptHeapChunks = keptHeapChunks
 	ShrinkChildren = shrinkChildren
+	ShrinkNodes    = shrinkNodes
 )
 
 // RepeatFlagsOK reports whether fHasRepeat covers every node with a repeat
--- a/parser_test.go
+++ b/parser_test.go
@@ -1,6 +1,7 @@
 package gositter_test
 
 import (
+	"runtime"
 	"strings"
 	"testing"
 
@@ -426,3 +427,60 @@
 		}
 	}
 }
+
+// Slabs a reset gives up go to pools all parsers share. A parser that takes
+// them over must build the same trees, and a pass that repeats allocates
+// almost nothing: no slab is regrown.
+func TestSlabsPooled(t *testing.T) {
+	deep := []byte("func f() {" + strings.Repeat("g(", 10000))
+	large := []byte(strings.Repeat("func f() { g(1, 2, 3) }\n", 20000))
+	small := []byte("func f() { g(1, 2, 3) }\n")
+	inputs := [][]byte{deep, small, large, small}
+	var want []string
+	q := gositter.NewParser()
+	q.SetLanguage(golang.Language())
+	for _, src := range inputs {
+		want = append(want, q.Parse(src).RootNode().String())
+	}
+	p := gositter.NewParser()
+	p.SetLanguage(golang.Language())
+	pass := func() {
+		for i, src := range inputs {
+			if p.Parse(src).RootNode().String() != want[i] {
+				t.Fatalf("input %d: tree differs on pooled slabs", i)
+			}
+		}
+	}
+	pass()
+	if n := p.StackSlabCap(); n > gositter.ShrinkNodes {
+		t.Errorf("after small parses: stack slab of %d entries, want it given back", n)
+	}
+	if raceEnabled {
+		return
+	}
+	// The GCs of the String calls emptied the pools.
+	parse := func() {
+		for _, src := range inputs {
+			p.Parse(src)
+		}
+	}
+	parse()
+	// The large slabs go to the pools. A parse that fits the base slabs
+	// leaves them there.
+	p.Reset()
+	r := gositter.NewParser()
+	r.SetLanguage(golang.Language())
+	r.Parse([]byte("func f() {" + strings.Repeat("g(", 100)))
+	if r.ChildrenSlabCap() > gositter.ShrinkChildren || r.StackSlabCap() > gositter.ShrinkNodes {
+		t.Error("a small parse took a pooled slab")
+	}
+	var m0, m1 runtime.MemStats
+	runtime.ReadMemStats(&m0)
+	for range 3 {
+		parse()
+	}
+	runtime.ReadMemStats(&m1)
+	if n := (m1.TotalAlloc - m0.TotalAlloc) / 3; n > 64<<10 {
+		t.Errorf("%d bytes allocated per pass, want slabs reused", n)
+	}
+}
--- a/subtree_test.go
+++ b/subtree_test.go
@@ -374,7 +374,7 @@
 			b := block{tag: subtree(step) << 32}
 			if r.Intn(3) == 0 {
 				// As reduceInPlace does.
-				b.a = subtreeArray{off: sp.bumpChildren(n), size: n, cap: n}
+				b.a = subtreeArray{off: sp.bumpChildren(sp.room(n), n), size: n, cap: n}
 				fill(b, 0)
 				sp.settleChildren(&b.a)
 			} else {
```

Scratch only, not in the diff above: `bench/main.go` gained a `GS_HEAPLOG` hook (heap stats after the first file, at the end, and after two GCs, with the parser kept alive) and a `GS_MEMPROF` hook. That diff is in `/data/m5-c/m5-c-bench-scratch.diff`.

## Files

- **Local scratch copy** (`$BENCH_DATA/fixes/m5-c`): deleted. No processes of mine are still running in the pod.
- **Pod, `/data/m5-c/`:**
  - Measured binary: `gsbench6`.
  - Pathological-test builds: `gsbench6-dbg` and `gsbench-base-dbg` (main with the bench hook).
  - Controls: `gsbench-vs` (layout-only) and `gsbench-maincopy`.
  - Earlier iterations: `gsbench-v2` to `gsbench-v4` and `gsbench5`. `gsbench-v1` was moved to `/data/m5-c/gsbench-v1`.
  - Final results: `out/`. Superseded runs: `out-v1*` to `out-v5`.
  - Diffs: `m5-c.diff`, `m5-c-bench-scratch.diff`.
  - Scripts: `summ.py`, `cyc.py`, `pmu.py`, `rep.py`.
- **Pod sources:**
  - `/work/m5-c-final`: the exact measured source.
  - `/work/m5-c`: the same, plus a scratch slab dump.
  - `/work/m5-c-base`: main plus the bench hook.
  - `/work/m5-c-vd`, `/work/m5-c-vs`: experiment variants.