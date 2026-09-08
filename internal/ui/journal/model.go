package journal

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type Options struct {
	Walk       revision.Options
	HasRemotes bool
}

func WalkOptions(maxCount int, hasRemotes bool) Options {
	return Options{
		Walk:       revision.Options{MaxCount: maxCount, Order: revision.DateOrder},
		HasRemotes: hasRemotes,
	}
}

func Load(ctx context.Context, source revision.Context, opts Options) iter.Seq2[Row, error] {
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
		unpushed, err := loadUnpushed(ctx, source, head, opts)
		if err != nil {
			yield(Row{}, err)
			return
		}
		walkOpts := opts.Walk
		walkOpts.Context = source
		walkOpts.Include = []hash.ObjectID{head}
		for commit, err := range revision.Walk(ctx, walkOpts) {
			if err != nil {
				yield(Row{}, err)
				return
			}
			if !yield(newRow(commit, decorations, unpushed), nil) {
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

func loadDecorations(source revision.Context) (map[hash.ObjectID][]Ref, error) {
	head := headBranch(source)
	decorations := make(map[hash.ObjectID][]Ref)
	for ref, err := range source.Refs.Prefix(refs.RefsPrefix) {
		if err != nil {
			return nil, err
		}
		kind, ok := kindOf(ref.Name)
		if !ok {
			continue
		}
		id := ref.Target
		if !ref.Peeled.IsZero() {
			id = ref.Peeled
		}
		if id.IsZero() {
			continue
		}
		decorations[id] = append(decorations[id], Ref{
			Name: ref.Name.Short(),
			Kind: kind,
			Head: kind == RefBranch && ref.Name == head,
		})
	}
	for id := range decorations {
		slices.SortStableFunc(decorations[id], byRefImportance)
	}
	return decorations, nil
}

func loadUnpushed(ctx context.Context, source revision.Context, head hash.ObjectID, opts Options) (map[hash.ObjectID]struct{}, error) {
	local := make(map[hash.ObjectID]struct{})
	if !opts.HasRemotes {
		return local, nil
	}
	remotes, err := remoteTips(source)
	if err != nil {
		return nil, err
	}
	walk := opts.Walk
	walk.Context = source
	walk.Include = []hash.ObjectID{head}
	walk.Exclude = remotes
	for commit, err := range revision.Walk(ctx, walk) {
		if err != nil {
			return nil, err
		}
		local[commit.ID] = struct{}{}
	}
	return local, nil
}

func remoteTips(source revision.Context) ([]hash.ObjectID, error) {
	var tips []hash.ObjectID
	for ref, err := range source.Refs.Prefix(refs.RemotesPrefix) {
		if err != nil {
			return nil, err
		}
		if ref.Target.IsZero() {
			continue
		}
		tips = append(tips, ref.Target)
	}
	return tips, nil
}

func kindOf(name refs.Name) (RefKind, bool) {
	switch {
	case name.IsBranch():
		return RefBranch, true
	case name.IsRemote():
		return RefRemote, true
	case name.IsTag():
		return RefTag, true
	}
	return 0, false
}

func byRefImportance(a, b Ref) int {
	if a.Head != b.Head {
		if a.Head {
			return -1
		}
		return 1
	}
	if a.Kind != b.Kind {
		return int(a.Kind) - int(b.Kind)
	}
	return strings.Compare(a.Name, b.Name)
}

func headBranch(source revision.Context) refs.Name {
	name, err := source.Refs.ResolveName(refs.HEAD)
	if err != nil {
		return ""
	}
	return name
}

type Pager struct {
	seq    iter.Seq2[Row, error]
	cancel context.CancelFunc
	next   func() (Row, error, bool)
	stop   func()
}

func NewPager(ctx context.Context, source revision.Context, opts Options) *Pager {
	walkCtx, cancel := context.WithCancel(ctx)
	return &Pager{seq: Load(walkCtx, source, opts), cancel: cancel}
}

func (p *Pager) pull() func() (Row, error, bool) {
	if p.next == nil {
		p.next, p.stop = iter.Pull2(p.seq)
	}
	return p.next
}

func (p *Pager) Next(n int) ([]Row, bool, error) {
	if n <= 0 {
		return nil, false, nil
	}
	next := p.pull()
	rows := make([]Row, 0, n)
	for len(rows) < n {
		row, err, ok := next()
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
	if p.stop != nil {
		p.stop()
	}
}
