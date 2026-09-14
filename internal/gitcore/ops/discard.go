package ops

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type discarder struct {
	ctx  context.Context
	wt   *workingTree
	db   *odb.DB
	idx  *index.Index
	opts DiscardOptions
}

func Discard(ctx context.Context, r *repo.Repository, paths []string, opts DiscardOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()

	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	idx, err := readIndex(r)
	if err != nil {
		return err
	}

	d := &discarder{ctx: ctx, wt: wt, db: db, idx: idx, opts: opts}
	for _, p := range paths {
		clean, err := cleanRepoPath(p)
		if err != nil {
			return err
		}
		if err := d.discard(clean); err != nil {
			return err
		}
	}
	return nil
}

func (d *discarder) discard(rel string) error {
	if err := d.ctx.Err(); err != nil {
		return err
	}
	if entry, ok := d.idx.Get(rel, index.StageMerged); ok {
		if err := d.restoreEntry(entry); err != nil {
			return err
		}
	}
	prefix := rel + "/"
	for _, tracked := range slices.Collect(d.idx.Paths(prefix)) {
		entry, ok := d.idx.Get(tracked, index.StageMerged)
		if !ok {
			continue
		}
		if err := d.restoreEntry(entry); err != nil {
			return err
		}
	}
	if d.opts.RemoveUntracked {
		return d.removeUntracked(rel)
	}
	return nil
}

func (d *discarder) restoreEntry(entry *index.Entry) error {
	if entry.Mode.IsSubmodule() {
		return nil
	}
	kind, data, err := d.db.Get(entry.ID)
	if err != nil {
		return err
	}
	if kind != object.TypeBlob {
		return nil
	}
	if !entry.Mode.IsSymlink() {
		data = d.wt.checkoutConvert(entry.Path, data)
	}
	return writeWorktreeBlob(d.wt, entry.Path, entry.Mode, data)
}

func parentOf(rel string) string {
	for at := len(rel) - 1; at >= 0; at-- {
		if rel[at] == '/' {
			return rel[:at]
		}
	}
	return ""
}

func (d *discarder) removeUntracked(rel string) error {
	name := filepath.FromSlash(rel)
	info, err := fsRootLstat(d.wt.root, name)
	if missingPath(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if _, tracked := d.idx.Get(rel, index.StageMerged); tracked || len(d.idx.Conflicts(rel)) > 0 {
			return nil
		}
		if d.wt.isIgnored(rel, false) {
			return nil
		}
		return fsRootRemove(d.wt.root, name)
	}
	if d.wt.isIgnored(rel, true) {
		return nil
	}
	entries, err := readDirRoot(d.wt.root, rel)
	if err != nil {
		return err
	}
	if holdsRepository(entries) {
		return nil
	}
	for _, entry := range entries {
		if err := d.removeUntracked(joinRel(rel, entry.Name())); err != nil {
			return err
		}
	}
	remaining, err := readDirRoot(d.wt.root, rel)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return fsRootRemove(d.wt.root, name)
	}
	return nil
}
