package blame

import (
	"bytes"
	"cmp"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var ErrPathNotFound = errors.New("blame: the path is not in the commit")

type Objects interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type Options struct {
	Diff            diff.Options
	NoFollowRenames bool
}

type Line struct {
	Commit  hash.ObjectID
	Author  object.Signature
	Summary string
	Path    string
	Source  int
	Number  int
	Text    string
}

type Result struct {
	Path  string
	Lines []Line
}

const modeTypeMask object.Mode = 0o170000

type span struct {
	result int
	source int
	count  int
}

type node struct {
	id      hash.ObjectID
	commit  *object.Commit
	origins []*origin
}

type origin struct {
	node  *node
	path  string
	entry object.TreeEntry
	data  []byte
	read  bool
	spans []span
}

type queued struct {
	node *node
	when int64
	seq  int
}

type queue []queued

func (q queue) Len() int { return len(q) }

func (q queue) Less(i, j int) bool {
	if q[i].when != q[j].when {
		return q[i].when > q[j].when
	}
	return q[i].seq < q[j].seq
}

func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *queue) Push(x any) { *q = append(*q, x.(queued)) }

func (q *queue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

type entryKey struct {
	tree hash.ObjectID
	name string
}

type entryResult struct {
	entry object.TreeEntry
	found bool
}

type blamer struct {
	ctx     context.Context
	src     Objects
	opts    Options
	lines   []string
	out     []Line
	pending queue
	seq     int
	nodes   map[hash.ObjectID]*node
	entries map[entryKey]entryResult
}

func File(ctx context.Context, src Objects, start hash.ObjectID, path string, opts Options) (Result, error) {
	if opts.Diff.Algorithm == 0 && opts.Diff.RenameThreshold == 0 {
		opts.Diff = diff.Defaults()
	}
	opts.Diff.Context = 0
	b := &blamer{ctx: ctx, src: src, opts: opts, nodes: map[hash.ObjectID]*node{}, entries: map[entryKey]entryResult{}}
	first, err := b.node(start)
	if err != nil {
		return Result{}, err
	}
	entry, found, err := b.entryAt(first.commit.Tree, path)
	if err != nil {
		return Result{}, err
	}
	if !found || entry.Mode.IsTree() || entry.Mode.IsSubmodule() {
		return Result{}, fmt.Errorf("%w: %s", ErrPathNotFound, path)
	}
	top := first.origin(path, entry)
	data, err := b.dataOf(top)
	if err != nil {
		return Result{}, err
	}
	b.lines = splitLines(data)
	b.out = make([]Line, len(b.lines))
	if len(b.lines) == 0 {
		return Result{Path: path}, nil
	}
	b.queue(top, []span{{result: 1, source: 1, count: len(b.lines)}})
	for b.pending.Len() > 0 {
		if err := b.ctx.Err(); err != nil {
			return Result{}, err
		}
		next := heap.Pop(&b.pending).(queued)
		if err := b.process(next.node); err != nil {
			return Result{}, err
		}
	}
	return Result{Path: path, Lines: b.out}, nil
}

func (n *node) origin(path string, entry object.TreeEntry) *origin {
	for _, o := range n.origins {
		if o.path == path {
			return o
		}
	}
	o := &origin{node: n, path: path, entry: entry}
	n.origins = append(n.origins, o)
	return o
}

func (b *blamer) queue(o *origin, spans []span) {
	if len(o.spans) == 0 {
		heap.Push(&b.pending, queued{node: o.node, when: o.node.commit.Committer.When.Unix(), seq: b.seq})
		b.seq++
	}
	o.spans = append(o.spans, spans...)
}

func (b *blamer) process(n *node) error {
	for _, o := range n.origins {
		if len(o.spans) == 0 {
			continue
		}
		if err := b.passBlame(o); err != nil {
			return err
		}
		b.assign(o)
		o.spans, o.data, o.read = nil, nil, false
	}
	return nil
}

func (b *blamer) passBlame(o *origin) error {
	parents := o.node.commit.Parents
	found := make([]*origin, len(parents))
	for pass := range b.passes() {
		for at, id := range parents {
			if found[at] != nil {
				continue
			}
			parent, err := b.node(id)
			if err != nil {
				return err
			}
			older, err := b.find(parent, o, pass == 1)
			if err != nil {
				return err
			}
			if older == nil {
				continue
			}
			if older.entry.ID == o.entry.ID {
				b.queue(older, o.spans)
				o.spans = nil
				return nil
			}
			if !slices.ContainsFunc(found[:at], func(f *origin) bool { return f != nil && f.entry.ID == older.entry.ID }) {
				found[at] = older
			}
		}
	}
	for _, older := range found {
		if older == nil {
			continue
		}
		if err := b.passToParent(o, older); err != nil {
			return err
		}
		if len(o.spans) == 0 {
			return nil
		}
	}
	return nil
}

func (b *blamer) passes() int {
	if b.opts.NoFollowRenames {
		return 1
	}
	return 2
}

func (b *blamer) find(parent *node, o *origin, renamed bool) (*origin, error) {
	if renamed {
		return b.findRename(parent, o)
	}
	entry, found, err := b.entryAt(parent.commit.Tree, o.path)
	if err != nil || !found || (entry.Mode^o.entry.Mode)&modeTypeMask != 0 {
		return nil, err
	}
	return parent.origin(o.path, entry), nil
}

func (b *blamer) findRename(parent *node, o *origin) (*origin, error) {
	opts := b.opts.Diff
	opts.DetectRenames, opts.DetectCopies, opts.Paths = true, false, nil
	files, err := diff.TreeChanges(b.ctx, b.src, parent.commit.Tree, o.node.commit.Tree, opts)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.NewPath == o.path && file.Status == diff.StatusRenamed {
			return parent.origin(file.OldPath, object.TreeEntry{Mode: file.OldMode, ID: file.OldID}), nil
		}
	}
	return nil, nil
}

func (b *blamer) passToParent(o, older *origin) error {
	newer, err := b.dataOf(o)
	if err != nil {
		return err
	}
	previous, err := b.dataOf(older)
	if err != nil {
		return err
	}
	passed, kept := splitSpans(o.spans, unchangedSegments(diff.Changes(previous, newer, b.opts.Diff), lineCount(newer)))
	if len(passed) > 0 {
		b.queue(older, passed)
	}
	o.spans = kept
	return nil
}

type segment struct {
	newer int
	older int
	count int
}

func unchangedSegments(changes []diff.Change, total int) []segment {
	var segments []segment
	newer, older := 0, 0
	for _, c := range changes {
		if c.NewIndex > newer {
			segments = append(segments, segment{newer: newer, older: older, count: c.NewIndex - newer})
		}
		newer, older = c.NewIndex+c.NewCount, c.OldIndex+c.OldCount
	}
	if total > newer {
		segments = append(segments, segment{newer: newer, older: older, count: total - newer})
	}
	return segments
}

func splitSpans(spans []span, segments []segment) (passed, kept []span) {
	slices.SortFunc(spans, func(x, y span) int {
		return cmp.Or(cmp.Compare(x.source, y.source), cmp.Compare(x.result, y.result))
	})
	first := 0
	for _, s := range spans {
		start, end := s.source-1, s.source-1+s.count
		for first < len(segments) && segments[first].newer+segments[first].count <= start {
			first++
		}
		at := start
		for _, seg := range segments[first:] {
			if seg.newer >= end {
				break
			}
			if seg.newer > at {
				kept = add(kept, span{result: s.result + at - start, source: at + 1, count: seg.newer - at})
				at = seg.newer
			}
			stop := min(end, seg.newer+seg.count)
			passed = add(passed, span{result: s.result + at - start, source: seg.older + at - seg.newer + 1, count: stop - at})
			at = stop
		}
		if at < end {
			kept = add(kept, span{result: s.result + at - start, source: at + 1, count: end - at})
		}
	}
	return passed, kept
}

func add(spans []span, next span) []span {
	if last := len(spans) - 1; last >= 0 {
		tail := spans[last]
		if tail.result+tail.count == next.result && tail.source+tail.count == next.source {
			spans[last].count += next.count
			return spans
		}
	}
	return append(spans, next)
}

func (b *blamer) assign(o *origin) {
	commit := o.node.commit
	summary := summaryOf(commit.Message)
	for _, s := range o.spans {
		for i := range s.count {
			number := s.result + i
			b.out[number-1] = Line{
				Commit:  o.node.id,
				Author:  commit.Author,
				Summary: summary,
				Path:    o.path,
				Source:  s.source + i,
				Number:  number,
				Text:    b.lines[number-1],
			}
		}
	}
}

func summaryOf(message string) string {
	line, _, _ := strings.Cut(strings.TrimLeft(message, "\n"), "\n")
	return line
}

func (b *blamer) node(id hash.ObjectID) (*node, error) {
	if n, seen := b.nodes[id]; seen {
		return n, nil
	}
	kind, data, err := b.src.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeCommit {
		return nil, fmt.Errorf("blame: %s is a %s, not a commit", id, kind)
	}
	commit, err := object.ParseCommit(data)
	if err != nil {
		return nil, err
	}
	n := &node{id: id, commit: commit}
	b.nodes[id] = n
	return n, nil
}

func (b *blamer) dataOf(o *origin) ([]byte, error) {
	if o.read {
		return o.data, nil
	}
	kind, data, err := b.src.Get(o.entry.ID)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeBlob {
		return nil, fmt.Errorf("%w: %s is a %s", ErrPathNotFound, o.path, kind)
	}
	o.data, o.read = data, true
	return data, nil
}

func lineCount(data []byte) int {
	count := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		count++
	}
	return count
}

func splitLines(data []byte) []string {
	lines := strings.SplitAfter(string(data), "\n")
	return lines[:lineCount(data)]
}

func (b *blamer) entryAt(tree hash.ObjectID, path string) (object.TreeEntry, bool, error) {
	for {
		name, rest, deeper := strings.Cut(path, "/")
		entry, found, err := b.lookup(tree, name)
		if err != nil || !found || !deeper {
			return entry, found, err
		}
		if !entry.Mode.IsTree() {
			return object.TreeEntry{}, false, nil
		}
		tree, path = entry.ID, rest
	}
}

func (b *blamer) lookup(tree hash.ObjectID, name string) (object.TreeEntry, bool, error) {
	key := entryKey{tree: tree, name: name}
	if known, seen := b.entries[key]; seen {
		return known.entry, known.found, nil
	}
	kind, data, err := b.src.Get(tree)
	if err != nil {
		return object.TreeEntry{}, false, err
	}
	var result entryResult
	if kind == object.TypeTree {
		for entry, err := range object.ParseTreeSeq(data) {
			if err != nil {
				return object.TreeEntry{}, false, err
			}
			if entry.Name == name {
				result = entryResult{entry: entry, found: true}
				break
			}
		}
	}
	b.entries[key] = result
	return result.entry, result.found, nil
}
