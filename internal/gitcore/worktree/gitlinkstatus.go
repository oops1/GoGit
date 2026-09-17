package worktree

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var (
	nestedOpenRepository = repo.OpenLayout
	nestedOpenObjects    = odb.Open
)

type unstagedChange struct {
	code      StatusCode
	submodule SubmoduleChange
}

type ignoreFlags struct {
	all       bool
	dirty     bool
	untracked bool
}

func ignoreFlagsOf(mode submodule.Ignore) ignoreFlags {
	return ignoreFlags{all: mode == submodule.IgnoreAll, dirty: mode == submodule.IgnoreDirty, untracked: mode == submodule.IgnoreUntracked}
}

type gitlinkRules struct {
	cfg     submodule.ConfigReader
	modules *submodule.Modules
	base    ignoreFlags
}

func hasGitlink(entries []*index.Entry) bool {
	for _, entry := range entries {
		if entry.Mode.IsSubmodule() {
			return true
		}
	}
	return false
}

func (w *Worktree) gitlinkRulesFor(entries []*index.Entry, headTree map[string]headEntry) (gitlinkRules, error) {
	rules := gitlinkRules{cfg: w.repo.Config(), modules: &submodule.Modules{}, base: ignoreFlags{untracked: w.hideUntracked}}
	if !hasGitlink(entries) || w.ignoreNoSubmodule {
		return rules, nil
	}
	if value, ok := rules.cfg.GetValue("diff.ignoresubmodules"); ok {
		mode, err := submodule.ParseIgnore(value)
		if err != nil {
			return gitlinkRules{}, err
		}
		rules.base = ignoreFlagsOf(mode)
		rules.base.untracked = rules.base.untracked || w.hideUntracked
	}
	modules, err := submodule.Load(submodule.Source{
		WorkTree: w.repo.WorkTree(),
		Index:    w.currentIndex(),
		Objects:  w.db,
		HeadBlob: headTree[submodule.GitmodulesFile].ID,
	})
	if err != nil {
		return gitlinkRules{}, err
	}
	rules.modules = modules
	return rules, nil
}

func (r gitlinkRules) flagsFor(path string) (ignoreFlags, error) {
	module, ok := r.modules.ByPath(path)
	if !ok {
		return r.base, nil
	}
	mode, err := submodule.ConfiguredIgnore(r.cfg, module)
	if err != nil || mode == submodule.IgnoreUnset {
		return r.base, err
	}
	return ignoreFlagsOf(mode), nil
}

func (w *Worktree) unstagedChangeOf(ctx context.Context, entry *index.Entry, rules gitlinkRules) (unstagedChange, error) {
	code, err := w.compareToWorktree(entry)
	if err != nil || entry.SkipWorktree || !entry.Mode.IsSubmodule() {
		return unstagedChange{code: code}, err
	}
	flags, err := rules.flagsFor(entry.Path)
	switch {
	case err != nil:
		return unstagedChange{}, fmt.Errorf("%w: %s: %w", ErrReadSubmodule, entry.Path, err)
	case flags.all:
		return unstagedChange{code: StatusUnmodified}, nil
	case code != StatusUnmodified:
		return unstagedChange{code: code}, nil
	}
	change, err := w.gitlinkChange(ctx, entry, flags)
	if err != nil || change == (SubmoduleChange{}) {
		return unstagedChange{code: StatusUnmodified}, err
	}
	return unstagedChange{code: StatusModified, submodule: change}, nil
}

func (w *Worktree) gitlinkChange(ctx context.Context, entry *index.Entry, flags ignoreFlags) (SubmoduleChange, error) {
	layout, ok := repo.DiscoverWorkTree(filepath.Join(w.repo.WorkTree(), filepath.FromSlash(entry.Path)))
	if !ok {
		return SubmoduleChange{}, nil
	}
	head, headErr := gitlink.Head(layout)
	change := SubmoduleChange{CommitChanged: headErr == nil && head != entry.ID}
	if flags.dirty {
		return change, nil
	}
	modified, untracked, err := w.nestedChanges(ctx, layout, flags.untracked)
	if err != nil {
		return SubmoduleChange{}, fmt.Errorf("%w: %s: %w", ErrReadSubmodule, entry.Path, err)
	}
	change.Modified, change.Untracked = modified, untracked
	return change, nil
}

func (w *Worktree) nestedChanges(ctx context.Context, layout repo.Layout, hideUntracked bool) (modified, untracked bool, err error) {
	r, err := nestedOpenRepository(layout, w.repo.Options())
	if err != nil {
		return false, false, err
	}
	defer func() { _ = r.Close() }()
	db, err := nestedOpenObjects(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false, false, err
	}
	defer func() { _ = db.Close() }()
	nested, err := Open(r, Options{DB: db, Env: w.env, Workers: w.workers, MaxFiles: w.maxFiles})
	if err != nil {
		return false, false, err
	}
	defer func() { _ = nested.Close() }()
	nested.hideUntracked = hideUntracked
	status, err := nested.Status(ctx)
	if err != nil {
		return false, false, err
	}
	for _, e := range status.Entries {
		untracked = untracked || e.Unstaged == StatusUntracked || e.Submodule.Untracked
		modified = modified || recordsTrackedChange(e) && e.Submodule != SubmoduleChange{Untracked: true}
	}
	return modified, untracked, nil
}

func recordsTrackedChange(e Entry) bool {
	if e.Conflict != ConflictNone || e.Staged != StatusUnmodified {
		return true
	}
	return e.Unstaged != StatusUnmodified && e.Unstaged != StatusUntracked && e.Unstaged != StatusIgnored
}
