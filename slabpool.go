package gositter

import (
	"math/bits"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"weak"
)

// Links 1 to 7 of one stack node.
type moreLinkSet = [maxLinkCount - 1]stackLink

// A reset hands a stack slab above these sizes (768 and 448 KiB) to its
// free list when the parse before used under an eighth of it, as children
// slabs. Its regrowth chain costs as much again.
// The uint32 slabs are a twelfth of what they index or less: above
// shrinkIndexes entries (4 MB) the same rule drops them for the GC.
const (
	shrinkNodes   = 1 << 14
	shrinkLinks   = 1 << 12
	shrinkIndexes = 1 << 20
)

func shrinkSlab(c, n, max int) bool { return c > max && n < c/8 }

// Slabs that resets gave up or parses outgrew, by floor(log2 cap), shared
// by all parsers and held weakly, as childSlabs. A link slab a reset gave
// up is also held strongly (kept) for keepResets parses: a caller's own
// garbage runs GCs that empty the weak lists, and a deep input every few
// hundred files then regrew its links after each one. Over keepBytes it is
// a pathological input's: weak at once, so the process comes back down.
// Node slabs are not kept: a kept one raised the heap goal of a caller
// whose GCs fell in its deep input.
type slabPool[T any] struct {
	sync.Mutex
	free [33][]weak.Pointer[[]T]
	kept []keptSlab[T]
	due  atomic.Uint64 // when the oldest kept slab goes weak, 0 if none
}

type keptSlab[T any] struct {
	b    *[]T
	tick uint64
}

const (
	keepResets = 1024
	keepBytes  = 32 << 20
)

var (
	nodeSlabs slabPool[stackNode]
	linkSlabs slabPool[moreLinkSet]

	// Parses ended by all parsers: the clock of kept slabs.
	resets atomic.Uint64
)

// A box holding an empty slab of at least n entries: a free one of under
// eight times n, else, for n over big, the smallest kept one. nil if none.
func (p *slabPool[T]) get(n, big int) *[]T {
	p.Lock()
	defer p.Unlock()
	for k := bits.Len(uint(n)) - 1; k <= bits.Len(uint(n-1))+1 && k < len(p.free); k++ {
		l := p.free[k]
		for i := len(l) - 1; i >= 0; i-- {
			b := l[i].Value()
			if b != nil && cap(*b) < n {
				continue
			}
			l = slices.Delete(l, i, i+1)
			if b != nil {
				p.free[k] = l
				*b = (*b)[:0]
				return b
			}
		}
		p.free[k] = l
	}
	if n <= big {
		return nil
	}
	i := -1
	for j, k := range p.kept {
		if cap(*k.b) >= n && (i < 0 || cap(*k.b) < cap(*p.kept[i].b)) {
			i = j
		}
	}
	if i < 0 {
		return nil
	}
	b := p.kept[i].b
	p.kept = slices.Delete(p.kept, i, i+1)
	*b = (*b)[:0]
	return b
}

// Hands the slab in box b to the free lists.
func (p *slabPool[T]) put(b *[]T) {
	if b == nil || cap(*b) == 0 {
		return
	}
	p.Lock()
	defer p.Unlock()
	p.putLocked(b)
}

func (p *slabPool[T]) putLocked(b *[]T) {
	k := bits.Len(uint(cap(*b))) - 1
	l := p.free[k]
	if len(l) == cap(l) { // forget the collected ones before growing
		l = slices.DeleteFunc(l, func(w weak.Pointer[[]T]) bool { return w.Value() == nil })
	}
	p.free[k] = append(l, weak.Make(b))
}

// Hands the slab in box b, which a reset gave up at tick now, to the
// free lists and keeps it.
func (p *slabPool[T]) keep(b *[]T, now uint64) {
	if uintptr(cap(*b))*reflect.TypeFor[T]().Size() > keepBytes {
		p.put(b)
		return
	}
	p.Lock()
	defer p.Unlock()
	p.kept = append(p.kept, keptSlab[T]{b, now})
	if p.due.Load() == 0 {
		p.due.Store(now + keepResets)
	}
}

// At tick now, makes the kept slabs keepResets old weak.
func (p *slabPool[T]) age(now uint64) {
	if d := p.due.Load(); d == 0 || now < d {
		return
	}
	p.Lock()
	defer p.Unlock()
	due := uint64(0)
	p.kept = slices.DeleteFunc(p.kept, func(k keptSlab[T]) bool {
		if now-k.tick >= keepResets {
			p.putLocked(k.b)
			return true
		}
		if due == 0 || k.tick+keepResets < due {
			due = k.tick + keepResets
		}
		return false
	})
	p.due.Store(due)
}
