package gositter

import (
	"math/bits"
	"slices"
	"sync"
	"weak"
)

// Links 1 to 7 of one stack node.
type moreLinkSet = [maxLinkCount - 1]stackLink

// A reset hands a stack slab above these sizes (about 400 KB) to its free
// list when the parse before used under an eighth of it, as children slabs.
// The uint32 slabs are a twelfth of what they index or less: above
// shrinkIndexes entries (4 MB) the same rule drops them for the GC.
const (
	shrinkNodes   = 1 << 13
	shrinkLinks   = 1 << 12
	shrinkIndexes = 1 << 20
)

func shrinkSlab(c, n, max int) bool { return c > max && n < c/8 }

// Slabs that resets gave up or parses outgrew, by floor(log2 cap), shared
// by all parsers and held weakly, as childSlabs.
type slabPool[T any] struct {
	sync.Mutex
	free [33][]weak.Pointer[[]T]
}

var (
	nodeSlabs slabPool[stackNode]
	linkSlabs slabPool[moreLinkSet]
)

// A box holding an empty slab of at least n entries: a free one of
// under eight times n, else a new one.
func (p *slabPool[T]) get(n int) *[]T {
	c := bits.Len(uint(n - 1))
	p.Lock()
	for k := c; k <= c+1 && k < len(p.free); k++ {
		l := p.free[k]
		for len(l) > 0 {
			b := l[len(l)-1].Value()
			l = l[:len(l)-1]
			if b != nil {
				p.free[k] = l
				p.Unlock()
				*b = (*b)[:0]
				return b
			}
		}
		p.free[k] = l
	}
	p.Unlock()
	b := make([]T, 0, n)
	return &b
}

// Hands the slab in box b to the free lists.
func (p *slabPool[T]) put(b *[]T) {
	if b == nil || cap(*b) == 0 {
		return
	}
	k := bits.Len(uint(cap(*b))) - 1
	p.Lock()
	defer p.Unlock()
	l := p.free[k]
	if len(l) == cap(l) { // forget the collected ones before growing
		l = slices.DeleteFunc(l, func(w weak.Pointer[[]T]) bool { return w.Value() == nil })
	}
	p.free[k] = append(l, weak.Make(b))
}
