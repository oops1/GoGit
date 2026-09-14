package ops

import (
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var sparseReadFile = os.ReadFile

type sparseCheckout struct {
	patterns     *attributes.Sparse
	clearPresent bool
}

func sparseCheckoutOf(r *repo.Repository) (sparseCheckout, error) {
	cfg := r.Config()
	enabled, err := configFlag(cfg, "core.sparsecheckout", false)
	if err != nil || !enabled {
		return sparseCheckout{}, err
	}
	expectOutside, err := configFlag(cfg, "sparse.expectfilesoutsideofpatterns", false)
	if err != nil {
		return sparseCheckout{}, err
	}
	cone, err := configFlag(cfg, "core.sparsecheckoutcone", false)
	if err != nil {
		return sparseCheckout{}, err
	}
	state := sparseCheckout{clearPresent: !expectOutside}
	if data, err := sparseReadFile(filepath.Join(r.GitDir(), "info", "sparse-checkout")); err == nil {
		state.patterns = attributes.ParseSparse(data, attributes.SparseOptions{Cone: cone, IgnoreCase: r.Core().IgnoreCase})
	}
	return state, nil
}

func (s sparseCheckout) clearPresentSkipWorktree(root *os.Root, idx *index.Index) {
	if !s.clearPresent {
		return
	}
	for entry := range idx.Entries() {
		if !entry.SkipWorktree {
			continue
		}
		if _, err := fsRootLstat(root, filepath.FromSlash(entry.Path)); err == nil {
			entry.SkipWorktree = false
		}
	}
}

func (sw *switcher) excluded(rel string, mode object.Mode) bool {
	return sw.sparse != nil && !sw.sparse.Includes(rel, mode.IsSubmodule())
}

func (sw *switcher) addedOutsideSparse(rel string, currentIndex map[string]*index.Entry) bool {
	_, tracked := currentIndex[rel]
	return !tracked && sw.excluded(rel, sw.target[rel].mode)
}

func (sw *switcher) layoutEntry(rel string, tgt treeEntry, previous *index.Entry) (index.Entry, error) {
	if sw.sparse == nil {
		return sw.checkoutEntry(rel, tgt, previous)
	}
	entry := index.Entry{Path: rel, Mode: tgt.mode, ID: tgt.id, Stage: index.StageMerged}
	if !sw.excluded(rel, tgt.mode) {
		stat, err := sw.checkout(rel, tgt)
		entry.Stat = stat
		return entry, err
	}
	entry.SkipWorktree = true
	if previous == nil || previous.SkipWorktree {
		return entry, nil
	}
	sw.leftSparse = append(sw.leftSparse, rel)
	return entry, sw.removeTracked(rel, previous)
}

func (sw *switcher) sparsifyKept(idx *index.Index, currentIndex map[string]*index.Entry) error {
	if sw.sparse == nil {
		return nil
	}
	for rel, entry := range currentIndex {
		if held, ok := idx.Get(rel, index.StageMerged); !ok || held != entry || sw.excluded(rel, entry.Mode) == entry.SkipWorktree {
			continue
		}
		transition := sw.leaveSparse
		if entry.SkipWorktree {
			transition = sw.enterSparse
		}
		if err := transition(entry); err != nil {
			return err
		}
	}
	for _, rel := range sw.leftSparse {
		sw.pruneEmptyDirs(parentOf(rel))
	}
	return nil
}

func (sw *switcher) leaveSparse(entry *index.Entry) error {
	info, err := fsRootLstat(sw.wt.root, filepath.FromSlash(entry.Path))
	switch {
	case missingPath(err):
		entry.SkipWorktree = true
		return nil
	case err != nil:
		return err
	case info.IsDir() && !entry.Mode.IsSubmodule():
		return nil
	}
	dirty, err := sw.isDirty(entry.Path, entry, nil)
	if err != nil || dirty {
		return err
	}
	if err := sw.removeTracked(entry.Path, entry); err != nil {
		return err
	}
	entry.SkipWorktree = true
	sw.leftSparse = append(sw.leftSparse, entry.Path)
	return nil
}

func (sw *switcher) enterSparse(entry *index.Entry) error {
	entry.SkipWorktree = false
	if _, err := fsRootLstat(sw.wt.root, filepath.FromSlash(entry.Path)); !missingPath(err) {
		return nil
	}
	stat, err := sw.checkout(entry.Path, treeEntry{mode: entry.Mode, id: entry.ID})
	entry.Stat = stat
	return err
}
