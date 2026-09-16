package diff

import (
	"context"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pathspec"
)

const modeTypeMask object.Mode = 0o170000

type Objects interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type pair struct {
	file    File
	oldData []byte
	newData []byte
	oldRead bool
	newRead bool
}

func (p *pair) oldContent(source Objects) ([]byte, error) {
	if !p.oldRead {
		data, err := blobContent(source, p.file.OldMode, p.file.OldID)
		if err != nil {
			return nil, err
		}
		p.oldData, p.oldRead = data, true
	}
	return p.oldData, nil
}

func (p *pair) newContent(source Objects) ([]byte, error) {
	if !p.newRead {
		data, err := blobContent(source, p.file.NewMode, p.file.NewID)
		if err != nil {
			return nil, err
		}
		p.newData, p.newRead = data, true
	}
	return p.newData, nil
}

func (p *pair) load(source Objects) error {
	if p.file.Status != StatusAdded {
		if _, err := p.oldContent(source); err != nil {
			return err
		}
	}
	if p.file.Status != StatusDeleted {
		if _, err := p.newContent(source); err != nil {
			return err
		}
	}
	return nil
}

type walker struct {
	ctx    context.Context
	source Objects
	opts   Options
	paths  pathspec.Set
	pairs  []pair
}

func Trees(ctx context.Context, source Objects, oldTree, newTree hash.ObjectID, opts Options) ([]File, error) {
	opts = opts.normalized()
	pairs, err := changedPairs(ctx, source, oldTree, newTree, opts)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(pairs))
	for at := range pairs {
		if err := pairs[at].load(source); err != nil {
			return nil, err
		}
		files = append(files, fillContent(pairs[at], opts))
	}
	return files, nil
}

func TreeChanges(ctx context.Context, source Objects, oldTree, newTree hash.ObjectID, opts Options) ([]File, error) {
	pairs, err := changedPairs(ctx, source, oldTree, newTree, opts.normalized())
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(pairs))
	for _, p := range pairs {
		files = append(files, p.file)
	}
	return files, nil
}

func changedPairs(ctx context.Context, source Objects, oldTree, newTree hash.ObjectID, opts Options) ([]pair, error) {
	paths, err := pathspec.Parse(opts.Paths)
	if err != nil {
		return nil, err
	}
	w := &walker{ctx: ctx, source: source, opts: opts, paths: paths}
	if err := w.walk("", oldTree, newTree); err != nil {
		return nil, err
	}
	if opts.DetectRenames || opts.DetectCopies {
		return detectRenames(w.pairs, source, opts)
	}
	return w.pairs, nil
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
	if w.include(path) {
		w.pairs = append(w.pairs, pair{
			file: File{OldPath: path, NewPath: path, OldMode: entry.Mode, OldID: entry.ID, Status: StatusDeleted},
		})
	}
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
	if w.include(path) {
		w.pairs = append(w.pairs, pair{
			file: File{OldPath: path, NewPath: path, NewMode: entry.Mode, NewID: entry.ID, Status: StatusAdded},
		})
	}
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
	w.pairs = append(w.pairs, pair{file: file})
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

func blobContent(source Objects, mode object.Mode, id hash.ObjectID) ([]byte, error) {
	if mode.IsSubmodule() {
		return []byte("Subproject commit " + id.String() + "\n"), nil
	}
	kind, data, err := source.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeBlob {
		return nil, fmt.Errorf("%w: %s is a %s", ErrMissingBlob, id, kind)
	}
	return data, nil
}

func (w *walker) include(path string) bool {
	return w.paths.Match(path)
}

func (w *walker) descend(dir string) bool {
	return w.paths.MayMatchUnder(dir)
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
		file.Hunks = patchHunks(p.oldData, p.newData, opts)
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
