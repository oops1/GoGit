package merge

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var ErrNotABlob = errors.New("merge: object is not a blob")

type Entry struct {
	Mode object.Mode
	ID   hash.ObjectID
}

type Snapshot map[string]Entry

type Objects interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
	Put(kind object.Type, data []byte) (hash.ObjectID, error)
}

type ConflictKind int

const (
	ConflictContent ConflictKind = iota
	ConflictAddAdd
	ConflictModifyDelete
	ConflictDeleteModify
	ConflictMode
	ConflictBinary
	ConflictSymlink
	ConflictSubmodule
	ConflictFileDirectory
	ConflictRenameDelete
	ConflictRenameRename
)

type Conflict struct {
	Path   string
	Kind   ConflictKind
	Base   *Entry
	Ours   *Entry
	Theirs *Entry
}

type TreeOptions struct {
	File         Options
	OurRenames   Renames
	TheirRenames Renames
}

type TreeResult struct {
	Tree      Snapshot
	Conflicts []Conflict
}

func (r TreeResult) Clean() bool { return len(r.Conflicts) == 0 }

func Trees(base, ours, theirs Snapshot, objects Objects, opts TreeOptions) (TreeResult, error) {
	a, err := align(base, ours, theirs, objects, opts)
	if err != nil {
		return TreeResult{}, err
	}
	result := TreeResult{Tree: a.decided, Conflicts: a.conflicts}
	for _, path := range unionPaths(a.base, a.ours, a.theirs) {
		merged, conflict, err := mergePath(path, lookup(a.base, path), lookup(a.ours, path), lookup(a.theirs, path), objects, a.optionsFor(path, opts))
		if err != nil {
			return TreeResult{}, err
		}
		if merged != nil {
			result.Tree[path] = *merged
		}
		if conflict != nil {
			result.Conflicts = append(result.Conflicts, *conflict)
		}
	}
	moveFilesOutOfTheWay(&result, ours, opts.File.Labels)
	return result, nil
}

func moveFilesOutOfTheWay(result *TreeResult, ours Snapshot, labels Labels) {
	for _, path := range slices.Sorted(maps.Keys(result.Tree)) {
		if !isDirectoryIn(result.Tree, path) {
			continue
		}
		entry := result.Tree[path]
		conflict := Conflict{Kind: ConflictFileDirectory}
		label := labels.Theirs
		if ours[path] == entry {
			label = labels.Ours
			conflict.Ours = &entry
		} else {
			conflict.Theirs = &entry
		}
		conflict.Path = path + "~" + label
		delete(result.Tree, path)
		result.Tree[conflict.Path] = entry
		result.Conflicts = append(result.Conflicts, conflict)
	}
}

func isDirectoryIn(tree Snapshot, path string) bool {
	prefix := path + "/"
	for other := range tree {
		if strings.HasPrefix(other, prefix) {
			return true
		}
	}
	return false
}

func unionPaths(snapshots ...Snapshot) []string {
	seen := map[string]bool{}
	for _, s := range snapshots {
		for path := range maps.Keys(s) {
			seen[path] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func lookup(s Snapshot, path string) *Entry {
	e, ok := s[path]
	if !ok {
		return nil
	}
	return &e
}

func same(a, b *Entry) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func mergePath(path string, base, ours, theirs *Entry, objects Objects, opts TreeOptions) (*Entry, *Conflict, error) {
	switch {
	case same(ours, theirs):
		return ours, nil, nil
	case same(base, ours):
		return theirs, nil, nil
	case same(base, theirs):
		return ours, nil, nil
	}
	conflict := &Conflict{Path: path, Base: base, Ours: ours, Theirs: theirs}
	switch {
	case ours == nil:
		conflict.Kind = ConflictDeleteModify
		return theirs, conflict, nil
	case theirs == nil:
		conflict.Kind = ConflictModifyDelete
		return ours, conflict, nil
	}
	if ours.Mode.IsSubmodule() || theirs.Mode.IsSubmodule() {
		conflict.Kind = ConflictSubmodule
		return ours, conflict, nil
	}
	if ours.Mode.ObjectType() != theirs.Mode.ObjectType() || ours.Mode.IsSymlink() != theirs.Mode.IsSymlink() {
		conflict.Kind = ConflictMode
		return ours, conflict, nil
	}
	mode, modeClean := mergeMode(base, ours, theirs)
	if ours.ID == theirs.ID {
		if !modeClean {
			conflict.Kind = ConflictMode
			return ours, conflict, nil
		}
		return &Entry{Mode: mode, ID: ours.ID}, nil, nil
	}
	return mergeContent(path, base, ours, theirs, mode, modeClean, objects, opts)
}

func mergeMode(base, ours, theirs *Entry) (object.Mode, bool) {
	switch {
	case ours.Mode == theirs.Mode:
		return ours.Mode, true
	case base != nil && base.Mode == ours.Mode:
		return theirs.Mode, true
	case base != nil && base.Mode == theirs.Mode:
		return ours.Mode, true
	}
	return ours.Mode, false
}

func mergeContent(path string, base, ours, theirs *Entry, mode object.Mode, modeClean bool, objects Objects, opts TreeOptions) (*Entry, *Conflict, error) {
	conflict := &Conflict{Path: path, Base: base, Ours: ours, Theirs: theirs, Kind: ConflictContent}
	if base == nil {
		conflict.Kind = ConflictAddAdd
	}
	if ours.Mode.IsSymlink() {
		conflict.Kind = ConflictSymlink
		return ours, conflict, nil
	}
	baseData, err := blobOf(objects, base)
	if err != nil {
		return nil, nil, err
	}
	ourData, err := blobOf(objects, ours)
	if err != nil {
		return nil, nil, err
	}
	theirData, err := blobOf(objects, theirs)
	if err != nil {
		return nil, nil, err
	}
	if attributes.IsBinaryContent(baseData) || attributes.IsBinaryContent(ourData) || attributes.IsBinaryContent(theirData) {
		conflict.Kind = ConflictBinary
		return ours, conflict, nil
	}
	merged := File(baseData, ourData, theirData, opts.File)
	id, err := objects.Put(object.TypeBlob, merged.Content)
	if err != nil {
		return nil, nil, err
	}
	entry := &Entry{Mode: mode, ID: id}
	switch {
	case merged.Conflicts > 0:
		return entry, conflict, nil
	case !modeClean:
		conflict.Kind = ConflictMode
		return entry, conflict, nil
	}
	return entry, nil, nil
}

func blobOf(objects Objects, entry *Entry) ([]byte, error) {
	if entry == nil {
		return nil, nil
	}
	kind, data, err := objects.Get(entry.ID)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeBlob {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotABlob, entry.ID, kind)
	}
	return data, nil
}
