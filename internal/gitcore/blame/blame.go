package blame

import (
	"bytes"
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

type span struct {
	result int
	source int
	count  int
}

type work struct {
	commit *object.Commit
	id     hash.ObjectID
	path   string
	data   []byte
	spans  []span
}

type blamer struct {
	ctx   context.Context
	src   Objects
	opts  Options
	lines []string
	out   []Line
	queue []*work
	known map[string]*work
}

func File(ctx context.Context, src Objects, start hash.ObjectID, path string, opts Options) (Result, error) {
	if opts.Diff.Algorithm == 0 && opts.Diff.RenameThreshold == 0 {
		opts.Diff = diff.Defaults()
	}
	opts.Diff.Context = 0
	b := &blamer{ctx: ctx, src: src, opts: opts, known: map[string]*work{}}
	first, err := b.commit(start)
	if err != nil {
		return Result{}, err
	}
	data, err := b.blobAt(first.Tree, path)
	if err != nil {
		return Result{}, err
	}
	b.lines = splitLines(data)
	b.out = make([]Line, len(b.lines))
	if len(b.lines) == 0 {
		return Result{Path: path}, nil
	}
	b.push(first, start, path, data, []span{{result: 1, source: 1, count: len(b.lines)}})
	for len(b.queue) > 0 {
		if err := b.ctx.Err(); err != nil {
			return Result{}, err
		}
		if err := b.step(b.take()); err != nil {
			return Result{}, err
		}
	}
	return Result{Path: path, Lines: b.out}, nil
}

func (b *blamer) push(commit *object.Commit, id hash.ObjectID, path string, data []byte, spans []span) {
	key := id.String() + "\x00" + path
	if known, seen := b.known[key]; seen {
		known.spans = append(known.spans, spans...)
		return
	}
	item := &work{commit: commit, id: id, path: path, data: data, spans: spans}
	b.known[key] = item
	b.queue = append(b.queue, item)
}

func (b *blamer) take() *work {
	newest := 0
	for i, item := range b.queue {
		if item.commit.Committer.When.After(b.queue[newest].commit.Committer.When) {
			newest = i
		}
	}
	item := b.queue[newest]
	b.queue = slices.Delete(b.queue, newest, newest+1)
	delete(b.known, item.id.String()+"\x00"+item.path)
	return item
}

type version struct {
	commit *object.Commit
	id     hash.ObjectID
	path   string
	data   []byte
}

func (b *blamer) step(item *work) error {
	versions := make([]*version, len(item.commit.Parents))
	for pass := range b.passes() {
		for at, id := range item.commit.Parents {
			if versions[at] != nil {
				continue
			}
			parent, err := b.commit(id)
			if err != nil {
				return err
			}
			path, older, found, err := b.parentVersion(parent, item.commit.Tree, item.path, pass == 1)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if bytes.Equal(older, item.data) {
				b.push(parent, id, path, older, item.spans)
				return nil
			}
			if !slices.ContainsFunc(versions[:at], func(v *version) bool { return v != nil && bytes.Equal(v.data, older) }) {
				versions[at] = &version{commit: parent, id: id, path: path, data: older}
			}
		}
	}
	left := item.spans
	for _, v := range versions {
		if v == nil {
			continue
		}
		passed, kept := splitSpans(left, lineMap(v.data, item.data, b.opts.Diff))
		if len(passed) > 0 {
			b.push(v.commit, v.id, v.path, v.data, passed)
		}
		left = kept
		if len(left) == 0 {
			return nil
		}
	}
	b.assign(item.commit, item.id, item.path, left)
	return nil
}

func (b *blamer) passes() int {
	if b.opts.NoFollowRenames {
		return 1
	}
	return 2
}

func (b *blamer) assign(commit *object.Commit, id hash.ObjectID, path string, spans []span) {
	for _, s := range spans {
		for i := range s.count {
			number := s.result + i
			b.out[number-1] = Line{
				Commit:  id,
				Author:  commit.Author,
				Summary: summaryOf(commit.Message),
				Path:    path,
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

func (b *blamer) parentVersion(parent *object.Commit, childTree hash.ObjectID, path string, renamed bool) (string, []byte, bool, error) {
	if !renamed {
		data, err := b.blobAt(parent.Tree, path)
		switch {
		case err == nil:
			return path, data, true, nil
		case errors.Is(err, ErrPathNotFound):
			return "", nil, false, nil
		default:
			return "", nil, false, err
		}
	}
	older, err := b.renamedFrom(parent.Tree, childTree, path)
	if err != nil || older == "" {
		return "", nil, false, err
	}
	data, err := b.blobAt(parent.Tree, older)
	if err != nil {
		return "", nil, false, err
	}
	return older, data, true, nil
}

func (b *blamer) renamedFrom(oldTree, newTree hash.ObjectID, path string) (string, error) {
	opts := b.opts.Diff
	opts.DetectRenames, opts.Paths = true, nil
	files, err := diff.Trees(b.ctx, b.src, oldTree, newTree, opts)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file.NewPath == path && file.Status == diff.StatusRenamed {
			return file.OldPath, nil
		}
	}
	return "", nil
}

func lineMap(older, newer []byte, opts diff.Options) map[int]int {
	hunks := diff.Blobs(older, newer, opts)
	same := map[int]int{}
	oldLine, newLine := 1, 1
	for _, hunk := range hunks {
		for newLine < hunk.NewStart {
			same[newLine] = oldLine
			oldLine++
			newLine++
		}
		oldLine, newLine = hunk.OldStart+hunk.OldLines, hunk.NewStart+hunk.NewLines
	}
	total := len(splitLines(newer))
	for newLine <= total {
		same[newLine] = oldLine
		oldLine++
		newLine++
	}
	return same
}

func splitSpans(spans []span, same map[int]int) (passed, kept []span) {
	for _, s := range spans {
		for i := range s.count {
			result, source := s.result+i, s.source+i
			older, unchanged := same[source]
			if unchanged {
				passed = add(passed, span{result: result, source: older, count: 1})
				continue
			}
			kept = add(kept, span{result: result, source: source, count: 1})
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

func (b *blamer) commit(id hash.ObjectID) (*object.Commit, error) {
	kind, data, err := b.src.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeCommit {
		return nil, fmt.Errorf("blame: %s is a %s, not a commit", id, kind)
	}
	return object.ParseCommit(data)
}

func (b *blamer) blobAt(tree hash.ObjectID, path string) ([]byte, error) {
	id, err := b.entryAt(tree, path)
	if err != nil {
		return nil, err
	}
	kind, data, err := b.src.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeBlob {
		return nil, fmt.Errorf("%w: %s is a %s", ErrPathNotFound, path, kind)
	}
	return data, nil
}

func (b *blamer) entryAt(tree hash.ObjectID, path string) (hash.ObjectID, error) {
	name, rest, deeper := strings.Cut(path, "/")
	kind, data, err := b.src.Get(tree)
	if err != nil {
		return hash.Zero, err
	}
	if kind != object.TypeTree {
		return hash.Zero, fmt.Errorf("%w: %s", ErrPathNotFound, path)
	}
	parsed, err := object.ParseTree(data)
	if err != nil {
		return hash.Zero, err
	}
	for _, entry := range parsed.Entries {
		if entry.Name != name {
			continue
		}
		if !deeper {
			return entry.ID, nil
		}
		return b.entryAt(entry.ID, rest)
	}
	return hash.Zero, fmt.Errorf("%w: %s", ErrPathNotFound, path)
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	if last := len(lines) - 1; lines[last] == "" {
		lines = lines[:last]
	}
	return lines
}
