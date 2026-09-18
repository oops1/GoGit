package ops

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/pathspec"
)

type untrackedWalk struct {
	m           *merger
	spec        pathspec.Set
	ignored     bool
	trackedPath map[string]bool
	trackedDir  map[string]bool
	files       []string
	nested      bool
}

func newUntrackedWalk(m *merger, idx *index.Index, spec pathspec.Set, ignored bool) *untrackedWalk {
	w := &untrackedWalk{m: m, spec: spec, ignored: ignored, trackedPath: map[string]bool{}, trackedDir: map[string]bool{}}
	for path := range idx.Paths("") {
		w.trackedPath[path] = true
		for dir := parentOf(path); dir != "" && !w.trackedDir[dir]; dir = parentOf(dir) {
			w.trackedDir[dir] = true
		}
	}
	return w
}

func (w *untrackedWalk) list() ([]string, error) {
	if err := w.dir(""); err != nil {
		return nil, err
	}
	slices.Sort(w.files)
	return w.files, nil
}

func (w *untrackedWalk) found() bool {
	return len(w.files) > 0 || w.nested
}

func (w *untrackedWalk) tracked(rel string) bool {
	return w.trackedPath[rel]
}

func (w *untrackedWalk) holdsTracked(rel string) bool {
	return w.trackedDir[rel]
}

func (w *untrackedWalk) dir(rel string) error {
	entries, err := readDirRoot(w.m.wt.root, rel)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := w.m.ctx.Err(); err != nil {
			return err
		}
		if entry.Name() == gitDirName {
			continue
		}
		child := joinRel(rel, entry.Name())
		if w.tracked(child) {
			continue
		}
		if !entry.IsDir() {
			if (w.ignored || !w.m.wt.isIgnored(child, false)) && w.spec.Match(child) {
				w.files = append(w.files, child)
			}
			continue
		}
		if err := w.subdir(child); err != nil {
			return err
		}
	}
	return nil
}

func (w *untrackedWalk) subdir(rel string) error {
	if !w.spec.MayMatchUnder(rel) && !w.spec.Match(rel) {
		return nil
	}
	if w.holdsTracked(rel) {
		return w.dir(rel)
	}
	if !w.ignored && w.m.wt.isIgnored(rel, true) {
		return nil
	}
	entries, err := readDirRoot(w.m.wt.root, rel)
	if err != nil {
		return err
	}
	if holdsRepository(entries) {
		w.nested = true
		return nil
	}
	return w.dir(rel)
}

func (m *merger) untrackedTree(files []string) (hash.ObjectID, error) {
	rules, err := pathRulesOf(m.r)
	if err != nil {
		return hash.Zero, err
	}
	st := &stager{ctx: m.ctx, wt: m.wt, db: m.rc.db, idx: index.New(index.Version2), rules: rules}
	for _, rel := range files {
		if err := m.ctx.Err(); err != nil {
			return hash.Zero, err
		}
		info, err := fsRootLstat(m.wt.root, filepath.FromSlash(rel))
		if err != nil {
			return hash.Zero, err
		}
		if err := st.stageEntry(rel, info); err != nil {
			return hash.Zero, err
		}
	}
	return st.idx.WriteTree(m.rc.db)
}

func (w *untrackedWalk) cleanAll() error {
	_, err := w.clean("")
	return err
}

func (w *untrackedWalk) clean(rel string) (bool, error) {
	entries, err := readDirRoot(w.m.wt.root, rel)
	if err != nil {
		return false, err
	}
	emptied := true
	for _, entry := range entries {
		if err := w.m.ctx.Err(); err != nil {
			return false, err
		}
		child := joinRel(rel, entry.Name())
		removed, err := w.cleanEntry(child, entry.Name(), entry.IsDir())
		if err != nil {
			return false, err
		}
		emptied = emptied && removed
	}
	return emptied, nil
}

func (w *untrackedWalk) cleanEntry(rel, name string, isDir bool) (bool, error) {
	switch {
	case name == gitDirName, w.tracked(rel), !w.ignored && w.m.wt.isIgnored(rel, isDir):
		return false, nil
	case !isDir:
		return true, fsRootRemove(w.m.wt.root, filepath.FromSlash(rel))
	}
	entries, err := readDirRoot(w.m.wt.root, rel)
	if err != nil {
		return false, err
	}
	if holdsRepository(entries) {
		return false, nil
	}
	emptied, err := w.clean(rel)
	if err != nil || !emptied || w.holdsTracked(rel) {
		return false, err
	}
	return true, fsRootRemove(w.m.wt.root, filepath.FromSlash(rel))
}

func (m *merger) removeFiles(files []string) error {
	sw := &switcher{ctx: m.ctx, wt: m.wt}
	for _, rel := range files {
		if err := fsRootRemove(m.wt.root, filepath.FromSlash(rel)); err != nil && !missingPath(err) {
			return err
		}
	}
	for _, rel := range files {
		sw.pruneEmptyDirs(parentOf(rel))
	}
	return nil
}

func (m *merger) restoreUntracked(tree hash.ObjectID) error {
	if tree.IsZero() {
		return nil
	}
	files, err := merge.Read(m.store(), tree)
	if err != nil {
		return err
	}
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	var existing []string
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		blocker, err := m.nonDirectoryParent(rel)
		if err != nil {
			return err
		}
		if blocker != "" {
			return &UntrackedRestoreError{Existing: existing, Blocked: blocker}
		}
		_, err = fsRootLstat(m.wt.root, filepath.FromSlash(rel))
		switch {
		case err == nil:
			existing = append(existing, rel)
			continue
		case !missingPath(err):
			return err
		}
		entry := files[rel]
		if _, err := sw.checkout(rel, treeEntry{mode: entry.Mode, id: entry.ID}); err != nil {
			return err
		}
	}
	if len(existing) > 0 {
		return &UntrackedRestoreError{Existing: existing}
	}
	return nil
}

func (m *merger) nonDirectoryParent(rel string) (string, error) {
	built := ""
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		built = joinRel(built, part)
		info, err := fsRootLstat(m.wt.root, filepath.FromSlash(built))
		switch {
		case missingPath(err):
			return "", nil
		case err != nil:
			return "", err
		case !info.IsDir():
			return built, nil
		}
	}
	return "", nil
}
