package revision

import (
	"cmp"
	"context"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
)

type topoWalk struct {
	indegree *queue
	topo     *queue
	minimum  uint64
	err      error
}

func (w *walker) streamsTopologically() bool {
	return w.opts.Context.Graph != nil && w.opts.Order != Default && !w.opts.Reverse &&
		len(w.opts.Exclude) == 0 && w.opts.Since.IsZero() && w.opts.Until.IsZero() && len(w.paths) == 0
}

func (w *walker) emitTopological(ctx context.Context, yield func(*Commit, error) bool) {
	t := w.startTopological()
	limit := &limiter{opts: w.opts}
	for {
		if t.err == nil {
			t.err = ctx.Err()
		}
		if t.err != nil {
			yield(nil, t.err)
			return
		}
		if t.topo.Len() == 0 {
			return
		}
		n := t.topo.pop()
		n.degree = 0
		w.expandTopological(t, n)
		if t.err == nil && !w.emit(ctx, n, limit, yield) {
			return
		}
	}
}

func (w *walker) startTopological() *topoWalk {
	t := &topoWalk{
		indegree: newQueue(byGenerationThenDate),
		topo:     newQueue(comparatorFor(w.opts.Order)),
		minimum:  commitgraph.GenerationInfinity,
	}
	tips := slices.Clone(w.tips)
	slices.SortStableFunc(tips, func(a, b *node) int { return cmp.Compare(b.when, a.when) })
	for _, n := range tips {
		n.flags |= flagIndegree
		t.indegree.push(n)
		t.minimum = min(t.minimum, n.generation)
		n.degree = 1
	}
	w.indegreesToDepth(t, t.minimum)
	var ready []*node
	for _, n := range tips {
		if n.degree == 1 {
			ready = append(ready, n)
		}
	}
	if w.opts.Order == Topo {
		slices.Reverse(ready)
	}
	for _, n := range ready {
		w.pushReady(t, n)
	}
	return t
}

func (w *walker) pushReady(t *topoWalk, n *node) {
	if t.err != nil {
		return
	}
	if w.opts.Order == AuthorDate {
		if _, t.err = w.full(n); t.err != nil {
			return
		}
	}
	t.topo.push(n)
}

func (w *walker) indegreesToDepth(t *topoWalk, cutoff uint64) {
	for t.err == nil && t.indegree.Len() > 0 && t.indegree.head().generation >= cutoff {
		n := t.indegree.pop()
		for _, id := range n.parents {
			parent := w.node(id)
			if t.err = w.load(parent); t.err != nil {
				return
			}
			if parent.degree > 0 {
				parent.degree++
			} else {
				parent.degree = 2
			}
			if parent.flags&flagIndegree == 0 {
				parent.flags |= flagIndegree
				t.indegree.push(parent)
			}
			if w.opts.FirstParent {
				break
			}
		}
	}
}

func (w *walker) expandTopological(t *topoWalk, n *node) {
	for _, id := range n.parents {
		parent := w.node(id)
		if parent.generation < t.minimum {
			t.minimum = parent.generation
			w.indegreesToDepth(t, t.minimum)
		}
		parent.degree--
		if parent.degree == 1 {
			w.pushReady(t, parent)
		}
		if w.opts.FirstParent {
			break
		}
	}
}
