package merge

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/diff"
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
	ConflictDistinctTypes
	ConflictFileLocation
)

type Conflict struct {
	Path   string
	Kind   ConflictKind
	Base   *Entry
	Ours   *Entry
	Theirs *Entry
}

type TreeOptions struct {
	File             Options
	OurRenames       Renames
	TheirRenames     Renames
	Depth            int
	Attributes       func(path string, virtual bool) PathAttributes
	DirectoryRenames DirectoryRenames
	extraMarkers     int
	warnings         *[]Warning
}

type TreeResult struct {
	Tree      Snapshot
	Conflicts []Conflict
	Warnings  []Warning
}

func (r TreeResult) Clean() bool { return len(r.Conflicts) == 0 }

func Trees(base, ours, theirs Snapshot, objects Objects, opts TreeOptions) (TreeResult, error) {
	var warnings []Warning
	opts.warnings = &warnings
	opts.File.Diff.Algorithm = diff.AlgorithmHistogram
	a, err := align(base, ours, theirs, objects, opts)
	if err != nil {
		return TreeResult{}, err
	}
	result := TreeResult{Tree: a.decided, Conflicts: a.conflicts}
	taken := takenPaths(a.decided, a.base, a.ours, a.theirs)
	for _, path := range unionPaths(a.base, a.ours, a.theirs) {
		baseEntry, ourEntry, theirEntry := lookup(a.base, path), lookup(a.ours, path), lookup(a.theirs, path)
		if distinctTypes(baseEntry, ourEntry, theirEntry) {
			if opts.Depth > 0 {
				result.keepBaseOfDistinctTypes(path, baseEntry, ourEntry, theirEntry)
				continue
			}
			result.keepDistinctTypes(path, baseEntry, ourEntry, theirEntry, opts.File.Labels, taken)
			continue
		}
		merged, conflict, err := mergePath(path, baseEntry, ourEntry, theirEntry, objects, a.optionsFor(path, opts))
		if err != nil {
			return TreeResult{}, err
		}
		if merged != nil {
			result.Tree[path] = *merged
		}
		if conflict == nil && a.located[path] {
			conflict = &Conflict{Path: path, Kind: ConflictFileLocation, Base: baseEntry, Ours: ourEntry, Theirs: theirEntry}
		}
		if conflict != nil {
			result.Conflicts = append(result.Conflicts, *conflict)
		}
	}
	moveFilesOutOfTheWay(&result, ours, opts.File.Labels)
	result.Warnings = warnings
	return result, nil
}

func moveFilesOutOfTheWay(result *TreeResult, ours Snapshot, labels Labels) {
	directories := directoriesOf(result.Tree)
	for _, path := range slices.Sorted(maps.Keys(result.Tree)) {
		if !directories[path] {
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
		conflict.Path = asidePath(func(candidate string) bool { _, taken := result.Tree[candidate]; return taken }, path, label)
		delete(result.Tree, path)
		result.Tree[conflict.Path] = entry
		if !result.moveConflicts(path, conflict.Path) {
			result.Conflicts = append(result.Conflicts, conflict)
		}
	}
}

func (r *TreeResult) moveConflicts(from, to string) bool {
	moved := false
	for at := range r.Conflicts {
		if r.Conflicts[at].Path == from {
			r.Conflicts[at].Path = to
			moved = true
		}
	}
	return moved
}

func asidePath(taken func(string) bool, path, label string) string {
	base := path + "~" + strings.ReplaceAll(label, "/", "_")
	candidate := base
	for suffix := 0; taken(candidate); suffix++ {
		candidate = base + "_" + strconv.Itoa(suffix)
	}
	return candidate
}

func takenPaths(snapshots ...Snapshot) map[string]bool {
	taken := map[string]bool{}
	for _, s := range snapshots {
		for path := range s {
			taken[path] = true
		}
		maps.Copy(taken, directoriesOf(s))
	}
	return taken
}

func sameKind(a, b object.Mode) bool {
	return a.IsSubmodule() == b.IsSubmodule() && a.IsSymlink() == b.IsSymlink()
}

func distinctTypes(base, ours, theirs *Entry) bool {
	if ours == nil || theirs == nil || same(base, ours) || same(base, theirs) {
		return false
	}
	return !sameKind(ours.Mode, theirs.Mode)
}

func baseOfKind(base, side *Entry) *Entry {
	if base != nil && sameKind(base.Mode, side.Mode) {
		return base
	}
	return nil
}

func (r *TreeResult) keepDistinctTypes(path string, base, ours, theirs *Entry, labels Labels, taken map[string]bool) {
	isTaken := func(candidate string) bool { return taken[candidate] }
	ourPath, theirPath := path, path
	if ours.Mode.IsRegular() || !theirs.Mode.IsRegular() {
		ourPath = asidePath(isTaken, path, labels.Ours)
		taken[ourPath] = true
	}
	if !ours.Mode.IsRegular() {
		theirPath = asidePath(isTaken, path, labels.Theirs)
		taken[theirPath] = true
	}
	r.Tree[ourPath], r.Tree[theirPath] = *ours, *theirs
	r.Conflicts = append(r.Conflicts,
		Conflict{Path: ourPath, Kind: ConflictDistinctTypes, Base: baseOfKind(base, ours), Ours: ours},
		Conflict{Path: theirPath, Kind: ConflictDistinctTypes, Base: baseOfKind(base, theirs), Theirs: theirs},
	)
}

func (r *TreeResult) keepBaseOfDistinctTypes(path string, base, ours, theirs *Entry) {
	if base != nil {
		r.Tree[path] = *base
	}
	r.Conflicts = append(r.Conflicts, Conflict{Path: path, Kind: ConflictDistinctTypes, Base: base, Ours: ours, Theirs: theirs})
}

func directoriesOf(tree Snapshot) map[string]bool {
	directories := map[string]bool{}
	for path := range tree {
		for at := strings.LastIndexByte(path, '/'); at > 0; at = strings.LastIndexByte(path[:at], '/') {
			parent := path[:at]
			if directories[parent] {
				break
			}
			directories[parent] = true
		}
	}
	return directories
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

func (o TreeOptions) virtual(base, side *Entry) *Entry {
	if o.Depth > 0 {
		return base
	}
	return side
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
		return opts.virtual(base, theirs), conflict, nil
	case theirs == nil:
		conflict.Kind = ConflictModifyDelete
		return opts.virtual(base, ours), conflict, nil
	}
	if ours.Mode.IsSubmodule() || theirs.Mode.IsSubmodule() {
		conflict.Kind = ConflictSubmodule
		return opts.virtual(base, ours), conflict, nil
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
		return opts.virtual(base, ours), conflict, nil
	}
	baseData, err := blobOf(objects, baseOfKind(base, ours))
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
	attrs := opts.attributesFor(path)
	if attrs.Driver == DriverExternal {
		opts.warn(Warning{Kind: WarningExternalDriver, Path: path, Driver: attrs.Name})
	}
	content, conflicted := baseData, false
	if attrs.Driver == DriverBinary || attrs.Driver == DriverExternal ||
		attributes.IsBinaryContent(baseData) || attributes.IsBinaryContent(ourData) || attributes.IsBinaryContent(theirData) {
		conflict.Kind = ConflictBinary
		if opts.Depth == 0 {
			return ours, conflict, nil
		}
	} else {
		merged := File(baseData, ourData, theirData, opts.fileOptionsFor(attrs))
		content, conflicted = merged.Content, merged.Conflicts > 0
	}
	id, err := objects.Put(object.TypeBlob, content)
	if err != nil {
		return nil, nil, err
	}
	entry := &Entry{Mode: mode, ID: id}
	switch {
	case conflicted:
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
