package merge

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

var ErrUnknownRename = errors.New("merge: rename names a path the trees do not have")

type Renames map[string]string

func DetectRenames(ctx context.Context, objects diff.Objects, base, side hash.ObjectID) (Renames, error) {
	files, err := diff.Trees(ctx, objects, base, side, diff.Options{
		DetectRenames:   true,
		RenameThreshold: diff.DefaultRenameThreshold,
		RenameLimit:     diff.DefaultRenameLimit,
	})
	if err != nil {
		return nil, err
	}
	renames := Renames{}
	for _, file := range files {
		if file.Status == diff.StatusRenamed {
			renames[file.OldPath] = file.NewPath
		}
	}
	return renames, nil
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
	}
	for _, from := range slices.Sorted(maps.Keys(opts.OurRenames)) {
		ourPath := opts.OurRenames[from]
		theirPath, both := opts.TheirRenames[from]
		switch {
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
		if _, both := opts.OurRenames[from]; !both {
			a.follow(from, opts.TheirRenames[from], a.ours, a.theirs, true)
		}
	}
	return a, nil
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
	nested.File.MarkerSize = nested.File.markerSize() + 1
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
