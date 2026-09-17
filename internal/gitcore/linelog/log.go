package linelog

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type Objects interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type Options struct {
	Diff      diff.Options
	NoRenames bool
	Shallow   map[hash.ObjectID]struct{}
	Progress  func(Progress)
}

type Progress struct {
	Done  int
	Total int
}

type Entry struct {
	Commit *revision.Commit
	Files  []File
}

type File struct {
	OldPath string
	NewPath string
	Created bool
	Hunks   []Hunk
}

type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []diff.Line
}

type entryKey struct {
	tree hash.ObjectID
	name string
}

type entryResult struct {
	entry object.TreeEntry
	found bool
}

type logger struct {
	ctx     context.Context
	src     Objects
	opts    Options
	commits map[hash.ObjectID]*revision.Commit
	decor   map[hash.ObjectID]rangeList
	entries map[entryKey]entryResult
}

func Log(ctx context.Context, src Objects, start hash.ObjectID, specs []Spec, opts Options) iter.Seq2[*Entry, error] {
	return func(yield func(*Entry, error) bool) {
		if opts.Diff.RenameThreshold == 0 && opts.Diff.RenameLimit == 0 {
			opts.Diff = diff.Defaults()
		}
		l := &logger{
			ctx:     ctx,
			src:     src,
			opts:    opts,
			commits: map[hash.ObjectID]*revision.Commit{},
			decor:   map[hash.ObjectID]rangeList{},
			entries: map[entryKey]entryResult{},
		}
		if err := l.run(start, specs, yield); err != nil {
			yield(nil, err)
		}
	}
}

func (l *logger) run(start hash.ObjectID, specs []Spec, yield func(*Entry, error) bool) error {
	if len(specs) == 0 {
		return ErrNoRanges
	}
	tip, err := l.commit(start)
	if err != nil {
		return err
	}
	list, err := l.parseLines(tip.Tree, specs)
	if err != nil {
		return err
	}
	order, err := l.topoOrder(start)
	if err != nil {
		return err
	}
	l.decor[start] = list
	for done, c := range order {
		if !l.live() {
			return nil
		}
		if err := l.ctx.Err(); err != nil {
			return err
		}
		if l.opts.Progress != nil {
			l.opts.Progress(Progress{Done: done, Total: len(order)})
		}
		ranges, found := l.decor[c.ID]
		if !found {
			continue
		}
		delete(l.decor, c.ID)
		entry, err := l.process(c, ranges)
		if err != nil {
			return err
		}
		if entry != nil && !yield(entry, nil) {
			return nil
		}
	}
	return nil
}

func (l *logger) commit(id hash.ObjectID) (*object.Commit, error) {
	kind, data, err := l.src.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeCommit {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotCommit, id, kind)
	}
	return object.ParseCommit(data)
}

func (l *logger) topoOrder(start hash.ObjectID) ([]*revision.Commit, error) {
	var order []*revision.Commit
	walk := revision.Walk(l.ctx, revision.Options{
		Context: revision.Context{Objects: l.src, Shallow: l.opts.Shallow},
		Include: []hash.ObjectID{start},
		Order:   revision.Topo,
	})
	for c, err := range walk {
		if err != nil {
			return nil, err
		}
		l.commits[c.ID] = c
		order = append(order, c)
	}
	return order, nil
}

func (l *logger) live() bool {
	for _, list := range l.decor {
		if list.live() {
			return true
		}
	}
	return false
}

func (l *logger) parseLines(tree hash.ObjectID, specs []Spec) (rangeList, error) {
	var list rangeList
	for _, spec := range specs {
		data, err := l.file(tree, spec.Path)
		if err != nil {
			return nil, err
		}
		t := newText(data)
		lines := t.lines()
		anchor := 1
		if existing := list.find(spec.Path); existing != nil {
			anchor = existing.ranges[len(existing.ranges)-1].End + 1
		}
		begin, end, err := resolveRange(spec.Range, t, anchor)
		if err != nil {
			return nil, err
		}
		if lines == 0 && (begin != 0 || end != 0) || lines < begin {
			return nil, fmt.Errorf("%w: %s has only %d lines", ErrTooFewLines, spec.Path, lines)
		}
		begin = max(begin, 1)
		if end < 1 || lines < end {
			end = lines
		}
		list = list.insert(spec.Path, begin-1, end)
	}
	for _, entry := range list {
		entry.ranges = sortAndMerge(entry.ranges)
	}
	return list, nil
}

func (l *logger) process(c *revision.Commit, ranges rangeList) (*Entry, error) {
	if len(c.Parents) > 1 {
		return l.processMerge(c, ranges)
	}
	parentTree := hash.Zero
	if len(c.Parents) == 1 {
		parentTree = l.commits[c.Parents[0]].Tree
	}
	pairs, err := l.queueDiffs(ranges, c.Tree, parentTree)
	if err != nil {
		return nil, err
	}
	parentRanges, changed := processAll(ranges, pairs)
	if len(c.Parents) == 1 {
		l.add(c.Parents[0], parentRanges)
	}
	if !changed {
		return nil, nil
	}
	return &Entry{Commit: c, Files: filesOf(ranges)}, nil
}

func (l *logger) processMerge(c *revision.Commit, ranges rangeList) (*Entry, error) {
	candidates := make([]rangeList, 0, len(c.Parents))
	for _, parent := range c.Parents {
		pairs, err := l.queueDiffs(ranges, c.Tree, l.commits[parent].Tree)
		if err != nil {
			return nil, err
		}
		candidate, changed := processAll(ranges, pairs)
		if !changed {
			l.add(parent, candidate)
			return nil, nil
		}
		candidates = append(candidates, candidate)
	}
	for at, parent := range c.Parents {
		l.add(parent, candidates[at])
	}
	return &Entry{Commit: c}, nil
}

func (l *logger) add(id hash.ObjectID, ranges rangeList) {
	if old, found := l.decor[id]; found {
		ranges = mergeLists(old, ranges)
	}
	l.decor[id] = ranges
}

func processAll(ranges rangeList, pairs []*filePair) (rangeList, bool) {
	out := ranges.copy()
	changed := false
	for _, pair := range pairs {
		entry := out[pair.at]
		if entry.ranges.empty() {
			continue
		}
		entry.path = pair.oldPath
		mapped, touched := mapAcrossDiff(entry.ranges, collectDiff(pair.oldData, pair.newData))
		entry.ranges = mapped
		if len(touched.parent) > 0 {
			changed = true
			ranges[pair.at].pair, ranges[pair.at].touched = pair, touched
		}
	}
	return out, changed
}

func (l *logger) queueDiffs(ranges rangeList, tree, parentTree hash.ObjectID) ([]*filePair, error) {
	var pairs, created []*filePair
	seen := map[string]bool{}
	for at, entry := range ranges {
		if seen[entry.path] {
			continue
		}
		seen[entry.path] = true
		pair, err := l.pairFor(at, entry.path, tree, parentTree)
		if err != nil {
			return nil, err
		}
		switch {
		case pair == nil:
		case pair.oldValid:
			pairs = append(pairs, pair)
		default:
			created = append(created, pair)
		}
	}
	if len(created) == 0 {
		return pairs, nil
	}
	if err := l.findRenames(created, tree, parentTree); err != nil {
		return nil, err
	}
	return append(pairs, created...), nil
}

func (l *logger) pairFor(at int, path string, tree, parentTree hash.ObjectID) (*filePair, error) {
	newEntry, found, err := l.entryAt(tree, path)
	if err != nil || !found {
		return nil, err
	}
	oldEntry, existed, err := l.entryAt(parentTree, path)
	if err != nil {
		return nil, err
	}
	if existed && oldEntry.Mode == newEntry.Mode && oldEntry.ID == newEntry.ID {
		return nil, nil
	}
	pair := &filePair{at: at, oldPath: path, newPath: path}
	if pair.newData, err = l.content(newEntry); err != nil {
		return nil, err
	}
	if existed {
		pair.oldValid = true
		if pair.oldData, err = l.content(oldEntry); err != nil {
			return nil, err
		}
	}
	return pair, nil
}

func (l *logger) findRenames(created []*filePair, tree, parentTree hash.ObjectID) error {
	if l.opts.NoRenames || parentTree.IsZero() {
		return nil
	}
	wanted := map[string]*filePair{}
	for _, pair := range created {
		wanted[pair.newPath] = pair
	}
	files, err := diff.TreeRenamesInto(l.ctx, l.src, parentTree, tree, l.opts.Diff, func(path string) bool { return wanted[path] != nil })
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Status != diff.StatusRenamed {
			continue
		}
		pair := wanted[file.NewPath]
		data, err := l.content(object.TreeEntry{Mode: file.OldMode, ID: file.OldID})
		if err != nil {
			return err
		}
		pair.oldPath, pair.oldValid, pair.oldData = file.OldPath, true, data
	}
	return nil
}

func FileAt(src Objects, commit hash.ObjectID, path string) ([]byte, error) {
	l := &logger{src: src, entries: map[entryKey]entryResult{}}
	c, err := l.commit(commit)
	if err != nil {
		return nil, err
	}
	return l.file(c.Tree, path)
}

func (l *logger) file(tree hash.ObjectID, path string) ([]byte, error) {
	entry, found, err := l.entryAt(tree, path)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrPathNotFound, path)
	}
	return l.content(entry)
}

func (l *logger) content(entry object.TreeEntry) ([]byte, error) {
	if entry.Mode.IsSubmodule() {
		return []byte("Subproject commit " + entry.ID.String() + "\n"), nil
	}
	kind, data, err := l.src.Get(entry.ID)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeBlob {
		return nil, fmt.Errorf("%w: %s is a %s", ErrPathNotFound, entry.ID, kind)
	}
	return data, nil
}

func (l *logger) entryAt(tree hash.ObjectID, path string) (object.TreeEntry, bool, error) {
	if tree.IsZero() {
		return object.TreeEntry{}, false, nil
	}
	for {
		name, rest, deeper := strings.Cut(path, "/")
		entry, found, err := l.lookup(tree, name)
		if err != nil || !found {
			return object.TreeEntry{}, false, err
		}
		if !deeper {
			return entry, !entry.Mode.IsTree(), nil
		}
		if !entry.Mode.IsTree() {
			return object.TreeEntry{}, false, nil
		}
		tree, path = entry.ID, rest
	}
}

func (l *logger) lookup(tree hash.ObjectID, name string) (object.TreeEntry, bool, error) {
	key := entryKey{tree: tree, name: name}
	if known, seen := l.entries[key]; seen {
		return known.entry, known.found, nil
	}
	kind, data, err := l.src.Get(tree)
	if err != nil {
		return object.TreeEntry{}, false, err
	}
	if kind != object.TypeTree {
		return object.TreeEntry{}, false, fmt.Errorf("%w: %s is a %s, not a tree", ErrPathNotFound, tree, kind)
	}
	var result entryResult
	for entry, err := range object.ParseTreeSeq(data) {
		if err != nil {
			return object.TreeEntry{}, false, err
		}
		if entry.Name == name {
			result = entryResult{entry: entry, found: true}
			break
		}
	}
	l.entries[key] = result
	return result.entry, result.found, nil
}
