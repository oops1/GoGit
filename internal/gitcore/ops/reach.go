package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const reflogDirName = "logs"

var (
	reachWalkDir   = filepath.WalkDir
	reachReadIndex = index.ReadFile
)

var reachHeads = []refs.Name{refs.HEAD, refs.OrigHead, refs.MergeHead, refs.CherryPickHead, refs.RebaseHead, refs.BisectHead}

type walkTrouble int

const (
	troubleUnreadable walkTrouble = iota + 1
	troubleMalformed
	troubleWrongType
)

var ErrWrongObjectType = errors.New("ops: object has another type than the link to it says")

type walkItem struct {
	id   hash.ObjectID
	kind object.Type
}

type objectWalk struct {
	ctx     context.Context
	db      *odb.DB
	shallow map[hash.ObjectID]struct{}
	seen    map[hash.ObjectID]struct{}
	commits []commitgraph.Commit
	stack   []walkItem
	strict  bool
	broken  func(id hash.ObjectID, trouble walkTrouble, err error) error
}

func newObjectWalk(ctx context.Context, r *repo.Repository, db *odb.DB) (*objectWalk, error) {
	shallow, err := r.Shallow()
	if err != nil {
		return nil, err
	}
	return &objectWalk{
		ctx:     ctx,
		db:      db,
		shallow: shallow,
		seen:    make(map[hash.ObjectID]struct{}),
		broken:  func(_ hash.ObjectID, _ walkTrouble, err error) error { return err },
	}, nil
}

func reachableObjects(ctx context.Context, r *repo.Repository, db *odb.DB) (*objectWalk, error) {
	walk, err := newObjectWalk(ctx, r, db)
	if err != nil {
		return nil, err
	}
	if err := walk.gatherRoots(r); err != nil {
		return nil, err
	}
	if err := walk.run(); err != nil {
		return nil, err
	}
	return walk, nil
}

func (w *objectWalk) reachable(id hash.ObjectID) bool {
	_, ok := w.seen[id]
	return ok
}

func (w *objectWalk) push(id hash.ObjectID, kind object.Type) {
	if id.IsZero() {
		return
	}
	if _, ok := w.seen[id]; ok {
		return
	}
	w.seen[id] = struct{}{}
	w.stack = append(w.stack, walkItem{id: id, kind: kind})
}

func (w *objectWalk) gatherRoots(r *repo.Repository) error {
	dirs, err := gitDirsOf(r)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := w.gatherGitDir(r, dir); err != nil {
			return err
		}
	}
	return nil
}

func gitDirsOf(r *repo.Repository) ([]string, error) {
	dirs := []string{r.CommonDir()}
	linked := filepath.Join(r.CommonDir(), worktreesDir)
	entries, err := worktreeReadDir(linked)
	if errors.Is(err, fs.ErrNotExist) {
		return dirs, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(linked, entry.Name()))
		}
	}
	return dirs, nil
}

func (w *objectWalk) gatherGitDir(r *repo.Repository, gitDir string) error {
	store, err := refsOpen(refs.Options{GitDir: gitDir, CommonDir: r.CommonDir(), Bare: r.IsBare(), Peeler: w.db})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	for ref, err := range store.All() {
		if err != nil {
			return err
		}
		w.push(ref.Target, 0)
	}
	for _, name := range reachHeads {
		ref, err := store.Resolve(name)
		if errors.Is(err, refs.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		w.push(ref.Target, 0)
	}
	if err := w.gatherReflogs(store, gitDir); err != nil {
		return err
	}
	return w.gatherIndex(filepath.Join(gitDir, indexFileName))
}

func (w *objectWalk) gatherReflogs(store *refs.Store, gitDir string) error {
	logs := filepath.Join(gitDir, reflogDirName)
	return reachWalkDir(logs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == logs && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := refs.Name(strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(logs)+"/"))
		if name.Validate() != nil {
			return nil
		}
		for record, err := range store.Reflog(name) {
			if err != nil {
				return err
			}
			w.push(record.Old, 0)
			w.push(record.New, 0)
		}
		return nil
	})
}

func (w *objectWalk) gatherIndex(path string) error {
	idx, err := reachReadIndex(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for entry := range idx.Entries() {
		if entry.Mode.IsSubmodule() || entry.IntentToAdd {
			continue
		}
		w.push(entry.ID, entry.Mode.ObjectType())
	}
	w.pushCacheTree(idx.CacheTree)
	return nil
}

func (w *objectWalk) pushCacheTree(tree *index.CacheTree) {
	if tree == nil {
		return
	}
	if tree.Valid() {
		w.push(tree.ID, object.TypeTree)
	}
	for _, sub := range tree.Subtrees {
		w.pushCacheTree(sub)
	}
}

func (w *objectWalk) run() error {
	for len(w.stack) > 0 {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		item := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		if err := w.visit(item); err != nil {
			return err
		}
	}
	return nil
}

func (w *objectWalk) visit(item walkItem) error {
	if item.kind == object.TypeBlob {
		return w.visitBlob(item.id)
	}
	kind, data, err := w.db.Get(item.id)
	if err != nil {
		return w.broken(item.id, troubleUnreadable, err)
	}
	if w.strict && item.kind != 0 && kind != item.kind {
		return w.broken(item.id, troubleWrongType, fmt.Errorf("%w: %s is a %s, not a %s", ErrWrongObjectType, item.id, kind, item.kind))
	}
	if err := w.expand(item.id, kind, data); err != nil {
		return w.broken(item.id, troubleMalformed, err)
	}
	return nil
}

func (w *objectWalk) visitBlob(id hash.ObjectID) error {
	if w.strict {
		kind, err := w.db.Type(id)
		if err != nil {
			return w.broken(id, troubleUnreadable, err)
		}
		if kind != object.TypeBlob {
			return w.broken(id, troubleWrongType, fmt.Errorf("%w: %s is a %s, not a blob", ErrWrongObjectType, id, kind))
		}
		return nil
	}
	has, err := w.db.Has(id)
	if err == nil && !has {
		err = fmt.Errorf("%w: %s", odb.ErrNotFound, id)
	}
	if err != nil {
		return w.broken(id, troubleUnreadable, err)
	}
	return nil
}

func (w *objectWalk) expand(id hash.ObjectID, kind object.Type, data []byte) error {
	switch kind {
	case object.TypeCommit:
		commit, err := object.ParseCommit(data)
		if err != nil {
			return err
		}
		w.push(commit.Tree, object.TypeTree)
		parents := commit.Parents
		if _, cut := w.shallow[id]; cut {
			parents = nil
		}
		w.commits = append(w.commits, commitgraph.Commit{ID: id, Tree: commit.Tree, Parents: parents, Time: commit.Committer.When.Unix()})
		for _, parent := range parents {
			w.push(parent, object.TypeCommit)
		}
	case object.TypeTree:
		tree, err := object.ParseTree(data)
		if err != nil {
			return err
		}
		for _, entry := range tree.Entries {
			if entry.Mode.IsSubmodule() {
				continue
			}
			w.push(entry.ID, entry.Mode.ObjectType())
		}
		if w.strict && !tree.IsSorted() {
			return fmt.Errorf("%w: the entries of tree %s are out of order", object.ErrMalformed, id)
		}
	case object.TypeTag:
		tag, err := object.ParseTag(data)
		if err != nil {
			return err
		}
		w.push(tag.Object, tag.ObjectType)
	}
	return nil
}
