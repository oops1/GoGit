package revision

import (
	"context"
	"iter"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const slopMax = 5

type Order uint8

const (
	Default Order = iota
	Topo
	DateOrder
	AuthorDate
)

type Options struct {
	Context       Context
	Include       []hash.ObjectID
	Exclude       []hash.ObjectID
	Order         Order
	Reverse       bool
	FirstParent   bool
	MaxCount      int
	Skip          int
	Since         time.Time
	Until         time.Time
	Author        *regexp.Regexp
	Committer     *regexp.Regexp
	Grep          *regexp.Regexp
	Pickaxe       string
	PickaxeRegexp *regexp.Regexp
	Paths         []string
}

type Commit struct {
	ID hash.ObjectID
	*object.Commit
	Parents []hash.ObjectID
}

type walker struct {
	*graph
	opts     Options
	queue    *queue
	paths    []string
	keys     [][]commitgraph.BloomKey
	slop     int
	when     int64
	haveDate bool
	tips     []*node
}

func Walk(ctx context.Context, opts Options) iter.Seq2[*Commit, error] {
	return func(yield func(*Commit, error) bool) {
		w, err := newWalker(opts)
		if err != nil {
			yield(nil, err)
			return
		}
		switch {
		case w.streamsTopologically():
			w.emitTopological(ctx, yield)
		case w.buffered():
			w.emitBuffered(ctx, yield)
		default:
			w.emitStream(ctx, yield)
		}
	}
}

func newWalker(opts Options) (*walker, error) {
	if opts.Pickaxe != "" && opts.PickaxeRegexp != nil {
		return nil, ErrPickaxeConflict
	}
	w := &walker{
		graph: newGraph(newStore(opts.Context)),
		opts:  opts,
		queue: newQueue(byCommitDate),
		paths: normalizePaths(opts.Paths),
		slop:  slopMax,
	}
	if graph := opts.Context.Graph; graph != nil {
		for _, path := range w.paths {
			w.keys = append(w.keys, graph.BloomKeys(path))
		}
	}
	for _, id := range opts.Exclude {
		if err := w.seed(id, flagUninteresting); err != nil {
			return nil, err
		}
	}
	for _, id := range opts.Include {
		if err := w.seed(id, 0); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func normalizePaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSuffix(strings.TrimPrefix(path, "./"), "/")
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned
}

func (w *walker) seed(id hash.ObjectID, flags nodeFlags) error {
	n, err := w.commit(id)
	if err != nil {
		return err
	}
	n.flags |= flags
	if n.flags&flagSeen != 0 {
		return nil
	}
	n.flags |= flagSeen
	w.queue.push(n)
	w.tips = append(w.tips, n)
	return nil
}

func (w *walker) buffered() bool {
	return w.opts.Order != Default || w.opts.Reverse ||
		len(w.opts.Exclude) > 0 || !w.opts.Since.IsZero()
}

func (w *walker) next() (*node, error) {
	for w.queue.Len() > 0 {
		n := w.queue.pop()
		if !w.opts.Since.IsZero() && time.Unix(n.when, 0).Before(w.opts.Since) {
			n.flags |= flagUninteresting
		}
		if err := w.processParents(n); err != nil {
			return nil, err
		}
		if n.flags&flagUninteresting != 0 {
			w.markParentsUninteresting(n)
			w.slop = w.stillInteresting()
			if w.slop > 0 {
				continue
			}
			return nil, nil
		}
		if w.pastUntil(n) {
			continue
		}
		w.when, w.haveDate = n.when, true
		return n, nil
	}
	return nil, nil
}

func (w *walker) pastUntil(n *node) bool {
	return !w.opts.Until.IsZero() && time.Unix(n.when, 0).After(w.opts.Until)
}

func (w *walker) stillInteresting() int {
	if w.queue.Len() == 0 {
		return 0
	}
	if w.haveDate && w.when <= w.queue.head().when {
		return slopMax
	}
	for _, item := range w.queue.items {
		if item.node.flags&flagUninteresting == 0 {
			return slopMax
		}
	}
	return w.slop - 1
}

func (w *walker) processParents(n *node) error {
	if n.flags&flagUninteresting != 0 {
		return w.processUninterestingParents(n)
	}
	if err := w.simplify(n); err != nil {
		return err
	}
	for _, id := range n.parents {
		parent := w.node(id)
		if parent.flags&flagSeen == 0 {
			parent.flags |= flagSeen
			if err := w.load(parent); err != nil {
				return err
			}
			w.queue.push(parent)
		}
		if w.opts.FirstParent {
			break
		}
	}
	return nil
}

func (w *walker) processUninterestingParents(n *node) error {
	for _, id := range n.parents {
		parent := w.node(id)
		parent.flags |= flagUninteresting
		if err := w.load(parent); err != nil {
			return err
		}
		w.markParentsUninteresting(parent)
		if parent.flags&flagSeen != 0 {
			continue
		}
		parent.flags |= flagSeen
		w.queue.push(parent)
	}
	return nil
}

func (w *walker) markParentsUninteresting(n *node) {
	pending := slices.Clone(n.parents)
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for {
			current := w.node(id)
			if current.flags&flagUninteresting != 0 {
				break
			}
			current.flags |= flagUninteresting
			if !current.loaded || len(current.parents) == 0 {
				break
			}
			pending = append(pending, current.parents[1:]...)
			id = current.parents[0]
		}
	}
}

func (w *walker) show(ctx context.Context, n *node) (bool, error) {
	if n.flags&(flagShown|flagUninteresting) != 0 {
		return false, nil
	}
	if len(w.paths) > 0 && n.flags&flagTreeSame != 0 {
		return false, nil
	}
	matched, err := w.matches(n)
	if err != nil || !matched {
		return false, err
	}
	found, err := w.pickaxe(ctx, n)
	if err != nil || !found {
		return false, err
	}
	n.flags |= flagShown
	return true, nil
}

func (w *walker) matches(n *node) (bool, error) {
	if w.opts.Author == nil && w.opts.Committer == nil && w.opts.Grep == nil {
		return true, nil
	}
	commit, err := w.full(n)
	if err != nil {
		return false, err
	}
	switch {
	case w.opts.Author != nil && !matchLines(w.opts.Author, commit.Author.String()):
		return false, nil
	case w.opts.Committer != nil && !matchLines(w.opts.Committer, commit.Committer.String()):
		return false, nil
	case w.opts.Grep != nil && !matchLines(w.opts.Grep, commit.Message):
		return false, nil
	}
	return true, nil
}

func matchLines(pattern *regexp.Regexp, text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func (w *walker) newCommit(n *node) (*Commit, error) {
	commit, err := w.full(n)
	if err != nil {
		return nil, err
	}
	parents := n.original
	if n.flags&flagShallow != 0 {
		parents = nil
	}
	return &Commit{ID: n.id, Commit: commit, Parents: parents}, nil
}

type limiter struct {
	skip  int
	shown int
	opts  Options
}

func (l *limiter) accept() (bool, bool) {
	if l.skip < l.opts.Skip {
		l.skip++
		return false, true
	}
	if l.opts.MaxCount > 0 && l.shown >= l.opts.MaxCount {
		return false, false
	}
	l.shown++
	return true, true
}

func (w *walker) visible(ctx context.Context, n *node, limit *limiter) (*Commit, bool, error) {
	shown, err := w.show(ctx, n)
	if err != nil || !shown {
		return nil, false, err
	}
	take, more := limit.accept()
	if !more {
		return nil, true, nil
	}
	if !take {
		return nil, false, nil
	}
	commit, err := w.newCommit(n)
	return commit, false, err
}

func (w *walker) emit(ctx context.Context, n *node, limit *limiter, yield func(*Commit, error) bool) bool {
	commit, stop, err := w.visible(ctx, n, limit)
	switch {
	case err != nil:
		yield(nil, err)
		return false
	case stop:
		return false
	case commit == nil:
		return true
	}
	return yield(commit, nil)
}

func (w *walker) emitStream(ctx context.Context, yield func(*Commit, error) bool) {
	limit := &limiter{opts: w.opts}
	for {
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		n, err := w.next()
		if err != nil {
			yield(nil, err)
			return
		}
		if n == nil || !w.emit(ctx, n, limit, yield) {
			return
		}
	}
}

func (w *walker) emitBuffered(ctx context.Context, yield func(*Commit, error) bool) {
	var collected []*node
	for {
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		n, err := w.next()
		if err != nil {
			yield(nil, err)
			return
		}
		if n == nil {
			break
		}
		collected = append(collected, n)
	}
	if err := w.loadAuthorDates(collected); err != nil {
		yield(nil, err)
		return
	}
	limit := &limiter{opts: w.opts}
	var shown []*Commit
	for _, n := range sortNodes(collected, w.opts.Order) {
		commit, stop, err := w.visible(ctx, n, limit)
		if err != nil {
			yield(nil, err)
			return
		}
		if stop {
			break
		}
		if commit != nil {
			shown = append(shown, commit)
		}
	}
	if w.opts.Reverse {
		slices.Reverse(shown)
	}
	for _, commit := range shown {
		if !yield(commit, nil) {
			return
		}
	}
}

func (w *walker) loadAuthorDates(nodes []*node) error {
	if w.opts.Order != AuthorDate {
		return nil
	}
	for _, n := range nodes {
		if _, err := w.full(n); err != nil {
			return err
		}
	}
	return nil
}
