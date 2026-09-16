package merge

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var ErrUnknownRename = errors.New("merge: rename names a path the trees do not have")

type Renames map[string]string

type cachedObject struct {
	kind object.Type
	data []byte
}

type cachedObjects struct {
	objects diff.Objects
	loaded  map[hash.ObjectID]cachedObject
}

func (c *cachedObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if obj, ok := c.loaded[id]; ok {
		return obj.kind, obj.data, nil
	}
	kind, data, err := c.objects.Get(id)
	if err != nil {
		return 0, nil, err
	}
	c.loaded[id] = cachedObject{kind: kind, data: data}
	return kind, data, nil
}

func (c *cachedObjects) Tree(id hash.ObjectID) (*object.Tree, error) {
	_, data, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	return object.ParseTree(data)
}

const DefaultRenameLimit = 7000

type RenameOptions struct {
	Limit            int
	DirectoryRenames bool
}

type SideRenames struct {
	Ours        Renames
	Theirs      Renames
	NeededLimit int
}

func DetectSideRenames(ctx context.Context, objects diff.Objects, base, ours, theirs hash.ObjectID, opts RenameOptions) (SideRenames, error) {
	cached := &cachedObjects{objects: objects, loaded: map[hash.ObjectID]cachedObject{}}
	snapshots := make([]Snapshot, 3)
	for at, tree := range []hash.ObjectID{base, ours, theirs} {
		s, err := Read(cached, tree)
		if err != nil {
			return SideRenames{}, err
		}
		snapshots[at] = s
	}
	var result SideRenames
	targets := []*Renames{&result.Ours, &result.Theirs}
	for at, side := range []hash.ObjectID{ours, theirs} {
		report, err := diff.TreeRenames(ctx, cached, base, side, diff.RenameSearch{
			Limit:    opts.Limit,
			Relevant: relevantSources(snapshots[0], snapshots[1+at], snapshots[2-at], opts.DirectoryRenames),
		})
		if err != nil {
			return SideRenames{}, err
		}
		result.NeededLimit = max(result.NeededLimit, report.NeededLimit)
		*targets[at] = renamesOf(report.Files)
	}
	return result, nil
}

func DetectRenames(ctx context.Context, objects diff.Objects, base, side hash.ObjectID) (Renames, error) {
	report, err := diff.TreeRenames(ctx, objects, base, side, diff.RenameSearch{Limit: DefaultRenameLimit})
	if err != nil {
		return nil, err
	}
	return renamesOf(report.Files), nil
}

func renamesOf(files []diff.File) Renames {
	renames := Renames{}
	for _, file := range files {
		if file.Status == diff.StatusRenamed {
			renames[file.OldPath] = file.NewPath
		}
	}
	return renames
}

type origin struct {
	base   string
	ours   string
	theirs string
}

type aligned struct {
	base      Snapshot
	ours      Snapshot
	theirs    Snapshot
	origins   map[string]origin
	decided   Snapshot
	conflicts []Conflict
	depth     int
	handled   map[string]bool
}

func (r Renames) check(base, side Snapshot) error {
	for _, from := range slices.Sorted(maps.Keys(r)) {
		_, known := base[from]
		_, arrived := side[r[from]]
		if !known || !arrived || from == r[from] {
			return fmt.Errorf("%w: %s to %s", ErrUnknownRename, from, r[from])
		}
	}
	return nil
}

func align(base, ours, theirs Snapshot, objects Objects, opts TreeOptions) (*aligned, error) {
	if err := errors.Join(opts.OurRenames.check(base, ours), opts.TheirRenames.check(base, theirs)); err != nil {
		return nil, err
	}
	a := &aligned{
		base:    maps.Clone(base),
		ours:    maps.Clone(ours),
		theirs:  maps.Clone(theirs),
		origins: map[string]origin{},
		decided: Snapshot{},
		depth:   opts.Depth,
		handled: map[string]bool{},
	}
	if err := a.renamedIntoOnePath(objects, opts); err != nil {
		return nil, err
	}
	for _, from := range slices.Sorted(maps.Keys(opts.OurRenames)) {
		ourPath := opts.OurRenames[from]
		theirPath, both := opts.TheirRenames[from]
		switch {
		case a.handled[from]:
		case !both:
			a.follow(from, ourPath, a.theirs, a.ours, false)
		case theirPath == ourPath:
			a.moveBase(from, ourPath)
			a.origins[ourPath] = origin{base: from, ours: ourPath, theirs: theirPath}
		default:
			if err := a.renamedApart(from, ourPath, theirPath, objects, opts); err != nil {
				return nil, err
			}
		}
	}
	for _, from := range slices.Sorted(maps.Keys(opts.TheirRenames)) {
		if _, both := opts.OurRenames[from]; !both && !a.handled[from] {
			a.follow(from, opts.TheirRenames[from], a.ours, a.theirs, true)
		}
	}
	return a, nil
}

func (a *aligned) renamedIntoOnePath(objects Objects, opts TreeOptions) error {
	ourSources := map[string]string{}
	for from, to := range opts.OurRenames {
		ourSources[to] = from
	}
	for _, theirFrom := range slices.Sorted(maps.Keys(opts.TheirRenames)) {
		to := opts.TheirRenames[theirFrom]
		ourFrom, clash := ourSources[to]
		_, theirsMovedOurSource := opts.TheirRenames[ourFrom]
		_, oursMovedTheirSource := opts.OurRenames[theirFrom]
		if !clash || theirsMovedOurSource || oursMovedTheirSource {
			continue
		}
		if err := a.mergeIntoOnePath(to, ourFrom, theirFrom, objects, opts); err != nil {
			return err
		}
	}
	return nil
}

func (a *aligned) mergeIntoOnePath(to, ourFrom, theirFrom string, objects Objects, opts TreeOptions) error {
	ourMerged, err := mergeRenamedSource(to, lookup(a.base, ourFrom), lookup(a.ours, to), lookup(a.theirs, ourFrom), origin{base: ourFrom, ours: to, theirs: ourFrom}, objects, opts)
	if err != nil {
		return err
	}
	theirMerged, err := mergeRenamedSource(to, lookup(a.base, theirFrom), lookup(a.ours, theirFrom), lookup(a.theirs, to), origin{base: theirFrom, ours: theirFrom, theirs: to}, objects, opts)
	if err != nil {
		return err
	}
	delete(a.base, ourFrom)
	delete(a.theirs, ourFrom)
	delete(a.base, theirFrom)
	delete(a.ours, theirFrom)
	a.ours[to], a.theirs[to] = *ourMerged, *theirMerged
	a.handled[ourFrom], a.handled[theirFrom] = true, true
	return nil
}

func mergeRenamedSource(path string, base, ours, theirs *Entry, o origin, objects Objects, opts TreeOptions) (*Entry, error) {
	switch {
	case ours == nil:
		return theirs, nil
	case theirs == nil:
		return ours, nil
	}
	nested := withOrigin(opts, o)
	nested.extraMarkers = 1
	merged, _, err := mergePath(path, base, ours, theirs, objects, nested)
	return merged, err
}

func (a *aligned) follow(from, to string, stayed, renamer Snapshot, theirsRenamed bool) {
	entry, kept := stayed[from]
	if !kept {
		moved := renamer[to]
		conflict := Conflict{Path: to, Kind: ConflictRenameDelete, Base: lookup(a.base, from)}
		if theirsRenamed {
			conflict.Theirs = &moved
		} else {
			conflict.Ours = &moved
		}
		a.conflicts = append(a.conflicts, conflict)
		if a.depth > 0 {
			delete(renamer, to)
			a.decided[to] = a.base[from]
		}
		return
	}
	if _, taken := stayed[to]; taken {
		return
	}
	delete(stayed, from)
	stayed[to] = entry
	a.moveBase(from, to)
	o := origin{base: from, ours: to, theirs: from}
	if theirsRenamed {
		o = origin{base: from, ours: from, theirs: to}
	}
	a.origins[to] = o
}

func (a *aligned) moveBase(from, to string) {
	if entry, ok := a.base[from]; ok {
		delete(a.base, from)
		a.base[to] = entry
	}
}

func (a *aligned) renamedApart(from, ourPath, theirPath string, objects Objects, opts TreeOptions) error {
	base, ours, theirs := lookup(a.base, from), lookup(a.ours, ourPath), lookup(a.theirs, theirPath)
	delete(a.base, from)
	delete(a.ours, ourPath)
	delete(a.theirs, theirPath)
	nested := withOrigin(opts, origin{base: from, ours: ourPath, theirs: theirPath})
	nested.extraMarkers = 1
	merged, _, err := mergePath(ourPath, base, ours, theirs, objects, nested)
	if err != nil {
		return err
	}
	a.decided[ourPath] = *merged
	a.decided[theirPath] = *merged
	a.conflicts = append(a.conflicts,
		Conflict{Path: from, Kind: ConflictRenameRename, Base: base},
		Conflict{Path: ourPath, Kind: ConflictRenameRename, Ours: merged},
		Conflict{Path: theirPath, Kind: ConflictRenameRename, Theirs: merged},
	)
	return nil
}

func (a *aligned) optionsFor(path string, opts TreeOptions) TreeOptions {
	o, renamed := a.origins[path]
	if !renamed {
		return opts
	}
	return withOrigin(opts, o)
}

func withOrigin(opts TreeOptions, o origin) TreeOptions {
	labels := opts.File.Labels
	opts.File.Labels = Labels{
		Base:   labels.Base + ":" + o.base,
		Ours:   labels.Ours + ":" + o.ours,
		Theirs: labels.Theirs + ":" + o.theirs,
	}
	return opts
}
