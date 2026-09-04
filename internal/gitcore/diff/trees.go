package diff

import (
	"context"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const modeTypeMask object.Mode = 0o170000

type Objects interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type pair struct {
	file    File
	oldData []byte
	newData []byte
}

type walker struct {
	ctx    context.Context
	source Objects
	opts   Options
	pairs  []pair
}

func Trees(ctx context.Context, source Objects, oldTree, newTree hash.ObjectID, opts Options) ([]File, error) {
	opts = opts.normalized()
	w := &walker{ctx: ctx, source: source, opts: opts}
	if err := w.walk("", oldTree, newTree); err != nil {
		return nil, err
	}
	pairs := w.pairs
	if opts.DetectRenames || opts.DetectCopies {
		pairs = detectRenames(pairs, opts)
	}
	files := make([]File, 0, len(pairs))
	for _, p := range pairs {
		files = append(files, fillContent(p, opts))
	}
	return files, nil
}

func (w *walker) walk(prefix string, oldID, newID hash.ObjectID) error {
	if oldID == newID {
		return nil
	}
	oldEntries, err := w.tree(oldID)
	if err != nil {
		return err
	}
	newEntries, err := w.tree(newID)
	if err != nil {
		return err
	}
	at, to := 0, 0
	for at < len(oldEntries) || to < len(newEntries) {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		switch {
		case to == len(newEntries):
			err = w.removed(prefix, oldEntries[at])
			at++
		case at == len(oldEntries):
			err = w.created(prefix, newEntries[to])
			to++
		default:
			order := object.CompareEntries(oldEntries[at], newEntries[to])
			switch {
			case order < 0:
				err = w.removed(prefix, oldEntries[at])
				at++
			case order > 0:
				err = w.created(prefix, newEntries[to])
				to++
			default:
				err = w.matched(prefix, oldEntries[at], newEntries[to])
				at++
				to++
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) removed(prefix string, entry object.TreeEntry) error {
	path := prefix + entry.Name
	if entry.Mode.IsTree() {
		if !w.descend(path) {
			return nil
		}
		return w.walk(path+"/", entry.ID, hash.Zero)
	}
	if !w.include(path) {
		return nil
	}
	data, err := w.content(entry.Mode, entry.ID)
	if err != nil {
		return err
	}
	w.pairs = append(w.pairs, pair{
		file:    File{OldPath: path, NewPath: path, OldMode: entry.Mode, OldID: entry.ID, Status: StatusDeleted},
		oldData: data,
	})
	return nil
}

func (w *walker) created(prefix string, entry object.TreeEntry) error {
	path := prefix + entry.Name
	if entry.Mode.IsTree() {
		if !w.descend(path) {
			return nil
		}
		return w.walk(path+"/", hash.Zero, entry.ID)
	}
	if !w.include(path) {
		return nil
	}
	data, err := w.content(entry.Mode, entry.ID)
	if err != nil {
		return err
	}
	w.pairs = append(w.pairs, pair{
		file:    File{OldPath: path, NewPath: path, NewMode: entry.Mode, NewID: entry.ID, Status: StatusAdded},
		newData: data,
	})
	return nil
}

func (w *walker) matched(prefix string, oldEntry, newEntry object.TreeEntry) error {
	path := prefix + oldEntry.Name
	if oldEntry.Mode.IsTree() {
		if !w.descend(path) {
			return nil
		}
		return w.walk(path+"/", oldEntry.ID, newEntry.ID)
	}
	if oldEntry.Mode == newEntry.Mode && oldEntry.ID == newEntry.ID {
		return nil
	}
	if !w.include(path) {
		return nil
	}
	file := File{
		OldPath: path,
		NewPath: path,
		OldMode: oldEntry.Mode,
		NewMode: newEntry.Mode,
		OldID:   oldEntry.ID,
		NewID:   newEntry.ID,
		Status:  StatusModified,
	}
	if (oldEntry.Mode^newEntry.Mode)&modeTypeMask != 0 {
		file.Status = StatusTypeChanged
	}
	oldData, err := w.content(oldEntry.Mode, oldEntry.ID)
	if err != nil {
		return err
	}
	newData, err := w.content(newEntry.Mode, newEntry.ID)
	if err != nil {
		return err
	}
	w.pairs = append(w.pairs, pair{file: file, oldData: oldData, newData: newData})
	return nil
}

func (w *walker) tree(id hash.ObjectID) ([]object.TreeEntry, error) {
	if id.IsZero() {
		return nil, nil
	}
	kind, data, err := w.source.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeTree {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotATree, id, kind)
	}
	tree, err := object.ParseTree(data)
	if err != nil {
		return nil, err
	}
	return tree.Entries, nil
}

func (w *walker) content(mode object.Mode, id hash.ObjectID) ([]byte, error) {
	if mode.IsSubmodule() {
		return []byte("Subproject commit " + id.String() + "\n"), nil
	}
	kind, data, err := w.source.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeBlob {
		return nil, fmt.Errorf("%w: %s is a %s", ErrMissingBlob, id, kind)
	}
	return data, nil
}

func (w *walker) include(path string) bool {
	if len(w.opts.Paths) == 0 {
		return true
	}
	for _, spec := range w.opts.Paths {
		if path == spec || strings.HasPrefix(path, spec+"/") {
			return true
		}
	}
	return false
}

func (w *walker) descend(dir string) bool {
	if len(w.opts.Paths) == 0 {
		return true
	}
	for _, spec := range w.opts.Paths {
		if spec == dir || strings.HasPrefix(spec, dir+"/") || strings.HasPrefix(dir, spec+"/") {
			return true
		}
	}
	return false
}

func fillContent(p pair, opts Options) File {
	file := p.file
	file.OldSize = len(p.oldData)
	file.NewSize = len(p.newData)
	_, newPath := file.paths()
	switch {
	case binaryFor(newPath, p.oldData, opts) || binaryFor(newPath, p.newData, opts):
		file.Binary = true
	case file.OldID != file.NewID:
		file.Hunks = diffLines(splitLines(p.oldData), splitLines(p.newData), opts)
	}
	if file.Status == StatusTypeChanged {
		file.Parts = typeChangeParts(file, p, opts)
	}
	return file
}

func typeChangeParts(file File, p pair, opts Options) []File {
	oldPath, newPath := file.paths()
	removal := File{
		OldPath: oldPath,
		NewPath: newPath,
		OldMode: file.OldMode,
		OldID:   file.OldID,
		Status:  StatusDeleted,
		Binary:  file.Binary,
		OldSize: file.OldSize,
	}
	creation := File{
		OldPath: oldPath,
		NewPath: newPath,
		NewMode: file.NewMode,
		NewID:   file.NewID,
		Status:  StatusAdded,
		Binary:  file.Binary,
		NewSize: file.NewSize,
	}
	if !file.Binary {
		removal.Hunks = diffLines(splitLines(p.oldData), nil, opts)
		creation.Hunks = diffLines(nil, splitLines(p.newData), opts)
	}
	return []File{removal, creation}
}
