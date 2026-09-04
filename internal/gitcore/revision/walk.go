package revision

import (
	"context"
	"iter"
	"regexp"
	"slices"
	"strings"
	"time"

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
	Context     Context
	Include     []hash.ObjectID
	Exclude     []hash.ObjectID
	Order       Order
	Reverse     bool
	FirstParent bool
	MaxCount    int
	Skip        int
	Since       time.Time
	Until       time.Time
	Author      *regexp.Regexp
	Committer   *regexp.Regexp
	Grep        *regexp.Regexp
	Paths       []string
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
	slop     int
	date     time.Time
	haveDate bool
}

func Walk(ctx context.Context, opts Options) iter.Seq2[*Commit, error] {
	return func(yield func(*Commit, error) bool) {
		w, err := newWalker(opts)
		if err != nil {
			yield(nil, err)
			return
		}
		if w.buffered() {
			w.emitBuffered(ctx, yield)
			return
		}
		w.emitStream(ctx, yield)
	}
}

func newWalker(opts Options) (*walker, error) {
	w := &walker{
		graph: newGraph(newStore(opts.Context.Objects, opts.Context.Shallow)),
		opts:  opts,
		queue: newQueue(byCommitDate),
		paths: normalizePaths(opts.Paths),
		slop:  slopMax,
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
	return nil
}

func (w *walker) buffered() bool {
	return w.opts.Order != Default || w.opts.Reverse ||
		len(w.opts.Exclude) > 0 || !w.opts.Since.IsZero()
}

func (w *walker) next() (*node, error) {
	for w.queue.Len() > 0 {
		n := w.queue.pop()
		if !w.opts.Since.IsZero() && n.commit.Committer.When.Before(w.opts.Since) {
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
		if !w.opts.Until.IsZero() && n.commit.Committer.When.After(w.opts.Until) {
			continue
		}
		w.date, w.haveDate = n.commit.Committer.When, true
		return n, nil
	}
	return nil, nil
}

func (w *walker) stillInteresting() int {
	if w.queue.Len() == 0 {
		return 0
	}
	if w.haveDate && !w.date.After(w.queue.head().commit.Committer.When) {
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
			if current.commit == nil || len(current.parents) == 0 {
				break
			}
			pending = append(pending, current.parents[1:]...)
			id = current.parents[0]
		}
	}
}

func (w *walker) show(n *node) bool {
	if n.flags&(flagShown|flagUninteresting) != 0 {
		return false
	}
	if len(w.paths) > 0 && n.flags&flagTreeSame != 0 {
		return false
	}
	if !w.matches(n) {
		return false
	}
	n.flags |= flagShown
	return true
}

func (w *walker) matches(n *node) bool {
	if w.opts.Author != nil && !matchLines(w.opts.Author, n.commit.Author.String()) {
		return false
	}
	if w.opts.Committer != nil && !matchLines(w.opts.Committer, n.commit.Committer.String()) {
		return false
	}
	if w.opts.Grep != nil && !matchLines(w.opts.Grep, n.commit.Message) {
		return false
	}
	return true
}

func matchLines(pattern *regexp.Regexp, text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func newCommit(n *node) *Commit {
	parents := n.commit.Parents
	if n.flags&flagShallow != 0 {
		parents = nil
	}
	return &Commit{ID: n.id, Commit: n.commit, Parents: parents}
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
		if n == nil {
			return
		}
		if !w.show(n) {
			continue
		}
		take, more := limit.accept()
		if !more {
			return
		}
		if take && !yield(newCommit(n), nil) {
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
	limit := &limiter{opts: w.opts}
	var shown []*Commit
	for _, n := range sortNodes(collected, w.opts.Order) {
		if !w.show(n) {
			continue
		}
		take, more := limit.accept()
		if !more {
			break
		}
		if take {
			shown = append(shown, newCommit(n))
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
