package journal

import (
	"context"
	"errors"
	"iter"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func Load(ctx context.Context, source revision.Context, opts revision.Options) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		head, err := resolveHead(source)
		if err != nil {
			yield(Row{}, err)
			return
		}
		if head.IsZero() {
			return
		}
		decorations, err := loadDecorations(source)
		if err != nil {
			yield(Row{}, err)
			return
		}
		walkOpts := opts
		walkOpts.Context = source
		walkOpts.Include = []hash.ObjectID{head}
		for commit, err := range revision.Walk(ctx, walkOpts) {
			if err != nil {
				yield(Row{}, err)
				return
			}
			if !yield(newRow(commit, decorations), nil) {
				return
			}
		}
	}
}

func resolveHead(source revision.Context) (hash.ObjectID, error) {
	ref, err := source.Refs.Resolve(refs.HEAD)
	if err != nil {
		if errors.Is(err, refs.ErrNotFound) {
			return hash.Zero, nil
		}
		return hash.Zero, err
	}
	return ref.Target, nil
}

func loadDecorations(source revision.Context) (map[hash.ObjectID][]string, error) {
	decorations := make(map[hash.ObjectID][]string)
	for ref, err := range source.Refs.Prefix(refs.RefsPrefix) {
		if err != nil {
			return nil, err
		}
		if !ref.Name.IsBranch() && !ref.Name.IsTag() && !ref.Name.IsRemote() {
			continue
		}
		id := ref.Target
		if !ref.Peeled.IsZero() {
			id = ref.Peeled
		}
		if id.IsZero() {
			continue
		}
		decorations[id] = append(decorations[id], ref.Name.Short())
	}
	return decorations, nil
}

type Pager struct {
	cancel context.CancelFunc
	next   func() (Row, error, bool)
	stop   func()
}

func NewPager(ctx context.Context, source revision.Context, opts revision.Options) *Pager {
	walkCtx, cancel := context.WithCancel(ctx)
	next, stop := iter.Pull2(Load(walkCtx, source, opts))
	return &Pager{cancel: cancel, next: next, stop: stop}
}

func (p *Pager) Next(n int) ([]Row, bool, error) {
	if n <= 0 {
		return nil, false, nil
	}
	rows := make([]Row, 0, n)
	for len(rows) < n {
		row, err, ok := p.next()
		if !ok {
			return rows, true, nil
		}
		if err != nil {
			return rows, true, err
		}
		rows = append(rows, row)
	}
	return rows, false, nil
}

func (p *Pager) Cancel() {
	p.cancel()
	p.stop()
}
