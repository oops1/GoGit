package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type switcher struct {
	ctx    context.Context
	wt     *workingTree
	db     *odb.DB
	format hash.Format
	head   map[string]treeEntry
	target map[string]treeEntry
	force  bool
}

func Switch(ctx context.Context, r *repo.Repository, target string, opts SwitchOptions) error {
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

	sig := reflogIdentity(r, time.Now())
	store, err := refsOpen(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      r.IsBare(),
		Peeler:    db,
		Committer: func() object.Signature { return sig },
	})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	commitID, branchRef, err := resolveSwitchTarget(target, db, store)
	if err != nil {
		return err
	}
	targetTree, err := commitTreeEntries(db, commitID)
	if err != nil {
		return err
	}

	fromRef, fromCommit, err := currentHeadState(store)
	if err != nil {
		return err
	}
	headTree, err := commitTreeEntries(db, fromCommit)
	if err != nil {
		return err
	}

	if err := layoutWorkingTree(ctx, r, wt, db, headTree, targetTree, opts.Force); err != nil {
		return err
	}

	return updateHeadAfterSwitch(store, fromRef, fromCommit, branchRef, commitID)
}

func layoutWorkingTree(ctx context.Context, r *repo.Repository, wt *workingTree, db *odb.DB, headTree, targetTree map[string]treeEntry, force bool) error {
	lock, err := lockIndex(r)
	if err != nil {
		return err
	}

	sw := &switcher{ctx: ctx, wt: wt, db: db, format: db.Format(), head: headTree, target: targetTree, force: force}
	currentIndex := indexByPath(lock.idx)

	conflicts, err := sw.computeOverwrites(currentIndex)
	if err != nil {
		lock.abort()
		return err
	}
	if len(conflicts) > 0 && !force {
		lock.abort()
		return &OverwriteError{Paths: conflicts}
	}

	if err := sw.apply(lock.idx, currentIndex); err != nil {
		lock.abort()
		return err
	}
	return lock.commit()
}

func resolveSwitchTarget(target string, db *odb.DB, store *refs.Store) (hash.ObjectID, refs.Name, error) {
	rev, err := revision.Parse(target, revision.Context{Objects: db, Refs: store, Head: refs.HEAD})
	if err != nil {
		return hash.Zero, "", fmt.Errorf("%w: %s: %w", ErrTargetNotFound, target, err)
	}
	kind, commitID, err := dbPeel(db, rev.ID)
	if err != nil {
		return hash.Zero, "", err
	}
	if kind != object.TypeCommit {
		return hash.Zero, "", fmt.Errorf("%w: %s is a %s, not a commit", ErrTargetNotFound, target, kind)
	}
	if rev.Ref != "" && rev.Ref.IsBranch() {
		return commitID, rev.Ref, nil
	}
	return commitID, "", nil
}

func currentHeadState(store *refs.Store) (refs.Name, hash.ObjectID, error) {
	ref, err := store.Lookup(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return "", hash.Zero, nil
	}
	if err != nil {
		return "", hash.Zero, err
	}
	if ref.IsSymbolic() {
		resolved, err := store.Resolve(refs.HEAD)
		if errors.Is(err, refs.ErrNotFound) {
			return ref.SymbolicTarget, hash.Zero, nil
		}
		if err != nil {
			return "", hash.Zero, err
		}
		return ref.SymbolicTarget, resolved.Target, nil
	}
	return "", ref.Target, nil
}

func updateHeadAfterSwitch(store *refs.Store, fromRef refs.Name, fromCommit hash.ObjectID, toRef refs.Name, toCommit hash.ObjectID) error {
	tx := store.Begin()
	tx.SetMessage("checkout: moving from " + checkoutLabel(fromRef, fromCommit) + " to " + checkoutLabel(toRef, toCommit))
	var err error
	if toRef != "" {
		err = txSetSymbolic(tx, refs.HEAD, toRef)
	} else {
		err = txDetach(tx, refs.HEAD, toCommit)
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func checkoutLabel(ref refs.Name, commit hash.ObjectID) string {
	if ref != "" {
		return ref.Short()
	}
	return abbreviate(commit)
}

const abbreviatedLength = 7

func abbreviate(id hash.ObjectID) string {
	return id.String()[:abbreviatedLength]
}

func indexByPath(idx *index.Index) map[string]*index.Entry {
	out := map[string]*index.Entry{}
	for entry := range idx.Entries() {
		if entry.Stage == index.StageMerged {
			out[entry.Path] = entry
		}
	}
	return out
}

func (sw *switcher) carried(rel string) bool {
	if sw.head == nil || sw.force {
		return false
	}
	old, oldOK := sw.head[rel]
	tgt, tgtOK := sw.target[rel]
	return oldOK == tgtOK && (!oldOK || old.mode == tgt.mode && old.id == tgt.id)
}

func (sw *switcher) staged(rel string, cur *index.Entry, curOK bool) bool {
	if sw.head == nil {
		return false
	}
	old, oldOK := sw.head[rel]
	return curOK != oldOK || curOK && (cur.Mode != old.mode || cur.ID != old.id)
}

func (sw *switcher) indexHoldsTarget(rel string, currentIndex map[string]*index.Entry) bool {
	cur, curOK := currentIndex[rel]
	tgt, tgtOK := sw.target[rel]
	return curOK && tgtOK && cur.Mode == tgt.mode && cur.ID == tgt.id
}

func (sw *switcher) computeOverwrites(currentIndex map[string]*index.Entry) ([]string, error) {
	seen := map[string]bool{}
	var conflicts []string
	check := func(rel string) error {
		if seen[rel] {
			return nil
		}
		if err := sw.ctx.Err(); err != nil {
			return err
		}
		seen[rel] = true
		if sw.carried(rel) || sw.indexHoldsTarget(rel, currentIndex) {
			return nil
		}
		cur, curOK := currentIndex[rel]
		if sw.staged(rel, cur, curOK) {
			conflicts = append(conflicts, rel)
			return nil
		}
		dirty, err := sw.isDirty(rel, cur, func(path string) bool { _, ok := currentIndex[path]; return ok })
		if err != nil {
			return err
		}
		if dirty {
			conflicts = append(conflicts, rel)
		}
		return nil
	}
	for rel := range currentIndex {
		if err := check(rel); err != nil {
			return nil, err
		}
	}
	for rel := range sw.target {
		if err := check(rel); err != nil {
			return nil, err
		}
	}
	slices.Sort(conflicts)
	return conflicts, nil
}

func (sw *switcher) isDirty(rel string, idxEntry *index.Entry, tracked func(string) bool) (bool, error) {
	info, err := fsRootLstat(sw.wt.root, filepath.FromSlash(rel))
	notExist := missingPath(err)
	if err != nil && !notExist {
		return false, err
	}
	switch {
	case idxEntry == nil && notExist:
		return false, nil
	case idxEntry == nil && info.IsDir():
		return sw.holdsUntracked(rel, tracked)
	case idxEntry == nil:
		return true, nil
	case notExist:
		return true, nil
	case idxEntry.Mode.IsSubmodule():
		return false, nil
	}
	data, err := sw.readWorktreeBytes(rel, info)
	if err != nil {
		return false, err
	}
	id, err := hashSum(sw.format, "blob", data)
	if err != nil {
		return false, err
	}
	if id != idxEntry.ID {
		return true, nil
	}
	if sw.wt.fileMode && !idxEntry.Mode.IsSymlink() {
		wantExec := idxEntry.Mode == object.ModeExecutable
		haveExec := info.Mode().Perm()&0o111 != 0
		if wantExec != haveExec {
			return true, nil
		}
	}
	return false, nil
}

func (sw *switcher) holdsUntracked(dir string, tracked func(string) bool) (bool, error) {
	entries, err := readDirRoot(sw.wt.root, dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		child := joinRel(dir, entry.Name())
		if !entry.IsDir() {
			if !tracked(child) {
				return true, nil
			}
			continue
		}
		held, err := sw.holdsUntracked(child, tracked)
		if err != nil || held {
			return held, err
		}
	}
	return false, nil
}

func (sw *switcher) readWorktreeBytes(rel string, info fs.FileInfo) ([]byte, error) {
	name := filepath.FromSlash(rel)
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := fsRootReadlink(sw.wt.root, name)
		if err != nil {
			return nil, err
		}
		return []byte(target), nil
	}
	data, err := fsRootReadFile(sw.wt.root, name)
	if err != nil {
		return nil, err
	}
	return sw.wt.checkinConvert(rel, data), nil
}

func (sw *switcher) apply(idx *index.Index, currentIndex map[string]*index.Entry) error {
	var removedDirs []string
	for rel := range currentIndex {
		if _, ok := sw.target[rel]; ok || sw.carried(rel) {
			continue
		}
		if err := sw.ctx.Err(); err != nil {
			return err
		}
		if err := fsRootRemove(sw.wt.root, filepath.FromSlash(rel)); err != nil && !missingPath(err) {
			return err
		}
		idx.Remove(rel)
		removedDirs = append(removedDirs, rel)
	}
	for _, rel := range removedDirs {
		sw.pruneEmptyDirs(parentOf(rel))
	}
	for rel, tgt := range sw.target {
		if err := sw.ctx.Err(); err != nil {
			return err
		}
		if sw.carried(rel) || sw.head != nil && !sw.force && sw.indexHoldsTarget(rel, currentIndex) {
			continue
		}
		if err := sw.checkout(rel, tgt); err != nil {
			return err
		}
		idx.Add(index.Entry{Path: rel, Mode: tgt.mode, ID: tgt.id, Stage: index.StageMerged})
	}
	return nil
}

func (sw *switcher) checkout(rel string, tgt treeEntry) error {
	if tgt.mode.IsSubmodule() {
		return nil
	}
	kind, data, err := sw.db.Get(tgt.id)
	if err != nil {
		return err
	}
	if kind != object.TypeBlob {
		return nil
	}
	if !tgt.mode.IsSymlink() {
		data = sw.wt.checkoutConvert(rel, data)
	}
	return writeWorktreeBlob(sw.wt, rel, tgt.mode, data)
}

func (sw *switcher) pruneEmptyDirs(dir string) {
	for dir != "" {
		entries, err := readDirRoot(sw.wt.root, dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := fsRootRemove(sw.wt.root, filepath.FromSlash(dir)); err != nil {
			return
		}
		dir = parentOf(dir)
	}
}
