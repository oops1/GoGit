package index

import (
	"fmt"
	"iter"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	Version2 = 2
	Version3 = 3
	Version4 = 4
)

type Index struct {
	Version      int
	SkipHash     bool
	Timestamp    time.Time
	CacheTree    *CacheTree
	ResolveUndo  []ResolveUndoEntry
	Untracked    *UntrackedCache
	EndOfEntries *EndOfEntries
	OffsetTable  *OffsetTable

	entries    []*Entry
	extensions []extension
}

func New(version int) *Index {
	return &Index{Version: version}
}

func timeOf(seconds, nanoseconds uint32) time.Time {
	return time.Unix(int64(seconds), int64(nanoseconds))
}

func (x *Index) Len() int {
	return len(x.entries)
}

func (x *Index) At(position int) *Entry {
	return x.entries[position]
}

func (x *Index) Entries() iter.Seq[*Entry] {
	return func(yield func(*Entry) bool) {
		for _, entry := range x.entries {
			if !yield(entry) {
				return
			}
		}
	}
}

func (x *Index) search(path string, stage Stage) (int, bool) {
	return slices.BinarySearchFunc(x.entries, path, func(entry *Entry, target string) int {
		return comparePathStage(entry.Path, entry.Stage, target, stage)
	})
}

func (x *Index) Get(path string, stage Stage) (*Entry, bool) {
	at, found := x.search(path, stage)
	if !found {
		return nil, false
	}
	return x.entries[at], true
}

func (x *Index) Add(entry Entry) {
	stored := entry
	at, found := x.search(entry.Path, entry.Stage)
	if found {
		x.entries[at] = &stored
	} else {
		x.entries = slices.Insert(x.entries, at, &stored)
	}
	x.invalidate(entry.Path)
}

func (x *Index) Remove(path string) bool {
	first, _ := x.search(path, StageMerged)
	last := first
	for last < len(x.entries) && x.entries[last].Path == path {
		last++
	}
	if last == first {
		return false
	}
	x.entries = slices.Delete(x.entries, first, last)
	x.invalidate(path)
	return true
}

func (x *Index) invalidate(path string) {
	if x.CacheTree != nil {
		x.CacheTree.invalidatePath(path)
	}
}

func (x *Index) Conflicts(path string) []Entry {
	first, _ := x.search(path, StageMerged)
	var conflicts []Entry
	for at := first; at < len(x.entries) && x.entries[at].Path == path; at++ {
		if x.entries[at].Conflicted() {
			conflicts = append(conflicts, *x.entries[at])
		}
	}
	return conflicts
}

func (x *Index) HasConflicts() bool {
	return slices.ContainsFunc(x.entries, (*Entry).Conflicted)
}

func (x *Index) Paths(prefix string) iter.Seq[string] {
	return func(yield func(string) bool) {
		first, _ := x.search(prefix, StageMerged)
		previous := ""
		for at := first; at < len(x.entries); at++ {
			path := x.entries[at].Path
			if !strings.HasPrefix(path, prefix) {
				return
			}
			if at > first && path == previous {
				continue
			}
			previous = path
			if !yield(path) {
				return
			}
		}
	}
}

func (x *Index) IsRacy(entry *Entry) bool {
	if x.Timestamp.IsZero() || entry.Mode.IsSubmodule() {
		return false
	}
	stamp, modified := x.Timestamp.Unix(), entry.Stat.MTime.Unix()
	if stamp != modified {
		return stamp < modified
	}
	return x.Timestamp.Nanosecond() <= entry.Stat.MTime.Nanosecond()
}

func (x *Index) MatchesFile(entry *Entry, fi os.FileInfo) bool {
	return entry.Matches(fi, x.IsRacy(entry))
}

type Writer interface {
	Put(kind object.Type, data []byte) (hash.ObjectID, error)
}

func (x *Index) WriteTree(objects Writer) (hash.ObjectID, error) {
	if x.HasConflicts() {
		return hash.Zero, fmt.Errorf("%w: a tree cannot be written while conflicts remain", ErrUnmerged)
	}
	if x.CacheTree == nil {
		x.CacheTree = &CacheTree{EntryCount: -1}
	}
	id, used, err := x.updateTree(objects, x.CacheTree, 0, "")
	if err != nil {
		return hash.Zero, err
	}
	if used != len(x.entries) {
		return hash.Zero, fmt.Errorf("%w: the cache tree covers %d of %d entries", ErrMalformed, used, len(x.entries))
	}
	x.ensureExtension(extCacheTree)
	return id, nil
}

func (x *Index) updateTree(objects Writer, node *CacheTree, start int, prefix string) (hash.ObjectID, int, error) {
	if node.Valid() {
		if start+node.EntryCount > len(x.entries) {
			return hash.Zero, 0, fmt.Errorf("%w: the cache tree of %q claims %d entries", ErrMalformed, prefix, node.EntryCount)
		}
		return node.ID, node.EntryCount, nil
	}
	tree := &object.Tree{}
	subtrees := make([]*CacheTree, 0, len(node.Subtrees))
	used := 0
	for start+used < len(x.entries) {
		entry := x.entries[start+used]
		if !strings.HasPrefix(entry.Path, prefix) {
			break
		}
		rest := entry.Path[len(prefix):]
		name, _, nested := strings.Cut(rest, "/")
		if !nested {
			tree.Entries = append(tree.Entries, object.TreeEntry{Mode: entry.Mode, Name: rest, ID: entry.ID})
			used++
			continue
		}
		sub := node.Find(name)
		if sub == nil {
			sub = &CacheTree{Path: name, EntryCount: -1}
		}
		id, count, err := x.updateTree(objects, sub, start+used, prefix+name+"/")
		if err != nil {
			return hash.Zero, 0, err
		}
		if count == 0 {
			return hash.Zero, 0, fmt.Errorf("%w: the cache tree of %q covers no entries", ErrMalformed, prefix+name)
		}
		used += count
		subtrees = append(subtrees, sub)
		tree.Entries = append(tree.Entries, object.TreeEntry{Mode: object.ModeTree, Name: name, ID: id})
	}
	tree.Sort()
	for at := 1; at < len(tree.Entries); at++ {
		if tree.Entries[at].Name == tree.Entries[at-1].Name {
			return hash.Zero, 0, fmt.Errorf("%w: %q appears twice under %q", ErrMalformed, tree.Entries[at].Name, prefix)
		}
	}
	id, err := objects.Put(object.TypeTree, tree.Encode())
	if err != nil {
		return hash.Zero, 0, fmt.Errorf("index: store the tree of %q: %w", prefix, err)
	}
	node.ID = id
	node.EntryCount = used
	node.Subtrees = subtrees
	node.sortSubtrees()
	return id, used, nil
}
