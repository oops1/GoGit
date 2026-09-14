package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/attributes"
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
	folded map[string]*index.Entry

	sparse     *attributes.Sparse
	leftSparse []string
	report     *CheckoutReport
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
	rules, err := pathRulesOf(r)
	if err != nil {
		return err
	}
	targetTree, err := verifiedTreeEntries(db, commitID, rules)
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

	if err := layoutWorkingTree(ctx, r, wt, db, headTree, targetTree, opts.Force, opts.Report); err != nil {
		return err
	}

	return updateHeadAfterSwitch(store, fromRef, fromCommit, branchRef, commitID)
}

func layoutWorkingTree(ctx context.Context, r *repo.Repository, wt *workingTree, db *odb.DB, headTree, targetTree map[string]treeEntry, force bool, report *CheckoutReport) error {
	sparse, err := sparseCheckoutOf(r)
	if err != nil {
		return err
	}
	lock, err := lockIndex(r)
	if err != nil {
		return err
	}

	sparse.clearPresentSkipWorktree(wt.root, lock.idx)
	sw := &switcher{ctx: ctx, wt: wt, db: db, format: db.Format(), head: headTree, target: targetTree, force: force, sparse: sparse.patterns, report: report}
	currentIndex := indexByPath(lock.idx)
	if r.Core().IgnoreCase {
		sw.folded = foldPaths(currentIndex)
	}

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
	report.sort()
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
		if sw.addedOutsideSparse(rel, currentIndex) {
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
		if sw.addedOutsideSparse(rel, currentIndex) {
			continue
		}
		blocker, err := sw.blockingLeadingPath(rel, currentIndex)
		if err != nil {
			return nil, err
		}
		if blocker != "" && !seen[blocker] {
			seen[blocker] = true
			conflicts = append(conflicts, blocker)
		}
	}
	slices.Sort(conflicts)
	return conflicts, nil
}

func foldPaths(entries map[string]*index.Entry) map[string]*index.Entry {
	out := make(map[string]*index.Entry, len(entries))
	for rel, entry := range entries {
		out[strings.ToLower(rel)] = entry
	}
	return out
}

func (sw *switcher) caseTwin(rel string) (*index.Entry, bool) {
	twin, ok := sw.folded[strings.ToLower(rel)]
	return twin, ok && twin.Path != rel
}

func (sw *switcher) blockingLeadingPath(rel string, currentIndex map[string]*index.Entry) (string, error) {
	if _, tracked := currentIndex[rel]; tracked {
		return "", nil
	}
	dir := parentOf(rel)
	if dir == "" {
		return "", nil
	}
	built := ""
	for part := range strings.SplitSeq(dir, "/") {
		built = joinRel(built, part)
		info, err := fsRootLstat(sw.wt.root, filepath.FromSlash(built))
		switch {
		case missingPath(err):
			return "", nil
		case err != nil:
			return "", err
		case info.Mode().Type() == fs.ModeDir:
			continue
		}
		if _, tracked := currentIndex[built]; tracked {
			return "", nil
		}
		if _, twin := sw.caseTwin(built); twin {
			return "", nil
		}
		return built, nil
	}
	return "", nil
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
		if twin, ok := sw.caseTwin(rel); ok {
			return sw.isDirty(twin.Path, twin, tracked)
		}
		return true, nil
	case idxEntry.SkipWorktree, idxEntry.Mode.IsSubmodule():
		return false, nil
	case notExist:
		return true, nil
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
		if err := sw.removeTracked(rel, currentIndex[rel]); err != nil {
			return err
		}
		removedDirs = append(removedDirs, rel)
	}
	for _, rel := range removedDirs {
		sw.pruneEmptyDirs(parentOf(rel))
	}
	entries := make([]index.Entry, 0, len(sw.target))
	for rel, tgt := range sw.target {
		if err := sw.ctx.Err(); err != nil {
			return err
		}
		if sw.carried(rel) || sw.head != nil && !sw.force && sw.indexHoldsTarget(rel, currentIndex) {
			continue
		}
		entry, err := sw.layoutEntry(rel, tgt, currentIndex[rel])
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	idx.Replace(removedDirs, entries)
	return sw.sparsifyKept(idx, currentIndex)
}

func (sw *switcher) checkoutEntry(rel string, tgt treeEntry, previous *index.Entry) (index.Entry, error) {
	entry := index.Entry{Path: rel, Mode: tgt.mode, ID: tgt.id, Stage: index.StageMerged}
	if previous != nil && previous.SkipWorktree {
		entry.SkipWorktree = true
		return entry, nil
	}
	stat, err := sw.checkout(rel, tgt)
	entry.Stat = stat
	return entry, err
}

func (sw *switcher) removeTracked(rel string, entry *index.Entry) error {
	if entry != nil && entry.SkipWorktree {
		return nil
	}
	err := fsRootRemove(sw.wt.root, filepath.FromSlash(rel))
	if err == nil || missingPath(err) || entry != nil && entry.Mode.IsSubmodule() {
		return nil
	}
	return err
}

func (sw *switcher) checkout(rel string, tgt treeEntry) (index.Stat, error) {
	if tgt.mode.IsSubmodule() {
		return index.Stat{}, writeGitlinkDirectory(sw.wt, rel)
	}
	kind, data, err := sw.db.Get(tgt.id)
	if err != nil {
		return index.Stat{}, err
	}
	if kind != object.TypeBlob {
		return index.Stat{}, nil
	}
	if err := sw.wt.writeCheckedOut(rel, tgt.mode, data, sw.report); err != nil {
		return index.Stat{}, err
	}
	return checkedOutStat(sw.wt.root, rel)
}

func checkedOutStat(root *os.Root, rel string) (index.Stat, error) {
	info, err := fsRootLstat(root, filepath.FromSlash(rel))
	if err != nil {
		return index.Stat{}, err
	}
	return statOf(info), nil
}

func (sw *switcher) statIfClean(rel string, mode object.Mode, id hash.ObjectID) (index.Stat, error) {
	if mode.IsSubmodule() {
		return index.Stat{}, nil
	}
	info, err := fsRootLstat(sw.wt.root, filepath.FromSlash(rel))
	if missingPath(err) || err == nil && info.IsDir() {
		return index.Stat{}, nil
	}
	if err != nil {
		return index.Stat{}, err
	}
	dirty, err := sw.isDirty(rel, &index.Entry{Path: rel, Mode: mode, ID: id}, nil)
	if err != nil || dirty {
		return index.Stat{}, err
	}
	return statOf(info), nil
}

func (sw *switcher) mergedEntry(rel string, mode object.Mode, id hash.ObjectID, previous *index.Entry) (index.Entry, error) {
	entry := index.Entry{Path: rel, Mode: mode, ID: id, Stage: index.StageMerged}
	if previous != nil && previous.SkipWorktree {
		entry.SkipWorktree = true
		return entry, nil
	}
	stat, err := sw.statIfClean(rel, mode, id)
	entry.Stat = stat
	return entry, err
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
