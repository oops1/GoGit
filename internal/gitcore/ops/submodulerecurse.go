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

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var (
	ErrSubmoduleDirtyIndex    = errors.New("ops: the submodule has a dirty index")
	ErrSubmoduleNotUpdated    = errors.New("ops: the submodule could not be updated")
	ErrSubmoduleGitDirMissing = errors.New("ops: the git directory of the submodule is missing")
)

func recurseSubmodulesConfigured(r *repo.Repository, explicit bool) (bool, error) {
	if explicit {
		return true, nil
	}
	recurse, err := r.Config().GetBool(submoduleRecurseKey)
	if errors.Is(err, config.ErrNotFound) {
		return false, nil
	}
	return recurse, err
}

type submoduleMove struct {
	path  string
	oldID hash.ObjectID
	newID hash.ObjectID
}

type submoduleMoves struct {
	moves  []submoduleMove
	before *superproject
	events SubmoduleEvents
}

func gitlinkID(tree map[string]treeEntry, path string) hash.ObjectID {
	if entry, ok := tree[path]; ok && entry.mode.IsSubmodule() {
		return entry.id
	}
	return hash.Zero
}

func gitlinkMoves(headTree, targetTree map[string]treeEntry) []submoduleMove {
	seen := map[string]bool{}
	var moves []submoduleMove
	for _, tree := range []map[string]treeEntry{headTree, targetTree} {
		for path := range tree {
			if seen[path] {
				continue
			}
			seen[path] = true
			move := submoduleMove{path: path, oldID: gitlinkID(headTree, path), newID: gitlinkID(targetTree, path)}
			if move.oldID != move.newID {
				moves = append(moves, move)
			}
		}
	}
	slices.SortFunc(moves, func(a, b submoduleMove) int { return strings.Compare(a.path, b.path) })
	return moves
}

func planSwitchedSubmodules(ctx context.Context, r *repo.Repository, headTree, targetTree map[string]treeEntry, opts SwitchOptions) (*submoduleMoves, error) {
	recurse, err := recurseSubmodulesConfigured(r, opts.RecurseSubmodules)
	if err != nil || !recurse {
		return &submoduleMoves{}, err
	}
	return planSubmoduleMoves(ctx, r, headTree, targetTree, opts.Force, opts.SubmoduleEvents)
}

func planSubmoduleMoves(ctx context.Context, r *repo.Repository, headTree, targetTree map[string]treeEntry, force bool, events SubmoduleEvents) (*submoduleMoves, error) {
	plan := &submoduleMoves{moves: gitlinkMoves(headTree, targetTree), events: events}
	if len(plan.moves) == 0 {
		return plan, nil
	}
	before, err := loadSuperproject(r, nil)
	if err != nil {
		return nil, err
	}
	plan.before = before
	after, err := treeModules(r, targetTree)
	if err != nil {
		return nil, err
	}
	for _, move := range plan.moves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := plan.check(ctx, move, after, force); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func treeModules(r *repo.Repository, tree map[string]treeEntry) (*submodule.Modules, error) {
	entry, ok := tree[submodule.GitmodulesFile]
	if !ok || !entry.mode.IsRegular() {
		return submodule.Parse(nil)
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	_, data, err := dbGet(db, entry.id)
	if err != nil {
		return nil, err
	}
	return submodule.Parse(data)
}

func (s *superproject) activeModule(path string, modules *submodule.Modules) (submodule.Module, bool, error) {
	module, known := modules.ByPath(path)
	if !known {
		return module, false, nil
	}
	active, err := submodule.Active(s.cfg, module)
	return module, active, err
}

func (m *submoduleMoves) check(ctx context.Context, move submoduleMove, after *submodule.Modules, force bool) error {
	s := m.before
	dir := s.submoduleDir(move.path)
	layout, populated := populatedLayout(dir)
	_, active, err := s.activeModule(move.path, s.modules)
	if err != nil {
		return err
	}
	if move.oldID.IsZero() {
		if _, statErr := os.Stat(dir); active && statErr == nil && !populated {
			return fmt.Errorf("%w: %s", ErrSubmoduleNotUpdated, s.displayPath(move.path))
		}
		module, activeAfter, err := s.activeModule(move.path, after)
		if err != nil || populated || !activeAfter || repo.IsRepository(s.modulesGitDir(module.Name)) {
			return err
		}
		return fmt.Errorf("%w: %s", ErrSubmoduleGitDirMissing, s.displayPath(move.path))
	}
	if !active || !populated || force {
		return nil
	}
	sub, err := submoduleOpenLayout(layout, s.repo.Options())
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if err := s.dryRunMove(ctx, sub, move); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrSubmoduleNotUpdated, s.displayPath(move.path), err)
	}
	return nil
}

func (s *superproject) dryRunMove(ctx context.Context, sub *repo.Repository, move submoduleMove) error {
	rc, err := openRepoContext(sub)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	idx, err := readIndex(sub)
	if err != nil {
		return err
	}
	head, err := resolveHeadCommit(rc.refs)
	if err != nil {
		return err
	}
	headCommitTree, err := commitTreeEntries(rc.db, head)
	if err != nil {
		return err
	}
	if indexDiffers(idx, headCommitTree) {
		return ErrSubmoduleDirtyIndex
	}
	rules, err := pathRulesOf(sub)
	if err != nil {
		return err
	}
	headTree, err := commitTreeEntries(rc.db, move.oldID)
	if err != nil {
		return err
	}
	targetTree, err := verifiedTreeEntries(rc.db, move.newID, rules)
	if err != nil {
		return err
	}
	conflicts, err := layoutConflicts(ctx, sub, rc.db, idx, headTree, targetTree)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &OverwriteError{Paths: conflicts}
	}
	_, err = planSubmoduleMoves(ctx, sub, headTree, targetTree, false, nil)
	return err
}

func indexDiffers(idx *index.Index, tree map[string]treeEntry) bool {
	count := 0
	for entry := range idx.Entries() {
		recorded, ok := tree[entry.Path]
		if entry.Stage != index.StageMerged || !ok || recorded.mode != entry.Mode || recorded.id != entry.ID {
			return true
		}
		count++
	}
	return count != len(tree)
}

func layoutConflicts(ctx context.Context, r *repo.Repository, db *odb.DB, idx *index.Index, headTree, targetTree map[string]treeEntry) ([]string, error) {
	wt, err := openWorkingTree(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = wt.close() }()
	sparse, err := sparseCheckoutOf(r)
	if err != nil {
		return nil, err
	}
	sw := &switcher{ctx: ctx, wt: wt, db: db, format: db.Format(), head: headTree, target: targetTree, sparse: sparse.patterns}
	current := indexByPath(idx)
	if r.Core().IgnoreCase {
		sw.folded = foldPaths(current)
	}
	return sw.computeOverwrites(current)
}

func (m *submoduleMoves) apply(ctx context.Context, r *repo.Repository) error {
	if len(m.moves) == 0 {
		return nil
	}
	for _, move := range m.moves {
		if move.newID.IsZero() {
			if err := m.before.moveHead(ctx, move, m.events); err != nil {
				return err
			}
		}
	}
	after, err := loadSuperproject(r, nil)
	if err != nil {
		return err
	}
	after.prefix = m.before.prefix
	for _, move := range m.moves {
		if move.newID.IsZero() {
			continue
		}
		if err := after.moveHead(ctx, move, m.events); err != nil {
			return err
		}
	}
	return nil
}

func (s *superproject) moveHead(ctx context.Context, move submoduleMove, events SubmoduleEvents) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, active, err := s.activeModule(move.path, s.modules)
	if err != nil || !active {
		return err
	}
	link := gitlinkEntry{path: move.path, id: move.newID, module: module, known: true}
	dir := s.submoduleDir(move.path)
	_, populated := populatedLayout(dir)
	switch {
	case populated:
		if err := s.absorbGitDir(link, events); err != nil {
			return err
		}
		if gitDir := s.modulesGitDir(module.Name); repo.IsRepository(gitDir) {
			if err := connectWorkTreeAndGitDir(dir, gitDir); err != nil {
				return err
			}
		}
	case move.oldID.IsZero():
		if err := s.connectFreshSubmodule(dir, module.Name); err != nil {
			return err
		}
	default:
		return nil
	}
	sub, err := submoduleOpen(dir, s.repo.Options())
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if err := s.resetSubmodule(ctx, sub, move, events); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrSubmoduleNotUpdated, s.displayPath(move.path), err)
	}
	if !move.newID.IsZero() {
		events.emit(SubmoduleEvent{Kind: SubmoduleCheckedOut, Path: s.displayPath(move.path), Name: module.Name, Commit: move.newID})
		return nil
	}
	_ = os.Remove(filepath.Join(dir, gitDirName))
	s.pruneEmptyParents(move.path)
	events.emit(SubmoduleEvent{Kind: SubmoduleCleared, Path: s.displayPath(move.path), Name: module.Name})
	return s.unsetCoreWorktree(module.Name)
}

func (s *superproject) connectFreshSubmodule(dir, name string) error {
	gitDir := s.modulesGitDir(name)
	if err := submodule.ValidateGitDir(s.repo.GitPath(submoduleModulesDir), name, repo.IsRepository); err != nil {
		return err
	}
	if err := connectWorkTreeAndGitDir(dir, gitDir); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(gitDir, indexFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *superproject) resetSubmodule(ctx context.Context, sub *repo.Repository, move submoduleMove, events SubmoduleEvents) error {
	idx, err := readIndex(sub)
	if err != nil {
		return err
	}
	rc, err := openRepoContext(sub)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	rules, err := pathRulesOf(sub)
	if err != nil {
		return err
	}
	targetTree, err := verifiedTreeEntries(rc.db, move.newID, rules)
	if err != nil {
		return err
	}
	nested, err := planSubmoduleMoves(ctx, sub, indexTree(idx), targetTree, true, events)
	if err != nil {
		return err
	}
	if nested.before != nil {
		nested.before.prefix = s.displayPath(move.path) + "/"
	}
	wt, err := openWorkingTree(sub)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()
	if err := layoutWorkingTree(ctx, sub, wt, rc.db, nil, targetTree, true, nil); err != nil {
		return err
	}
	if err := nested.apply(ctx, sub); err != nil || move.newID.IsZero() {
		return err
	}
	tx := rc.refs.Begin()
	if err := txDetach(tx, refs.HEAD, move.newID); err != nil {
		tx.Rollback()
		return err
	}
	return txCommit(tx)
}

func (s *superproject) pruneEmptyParents(path string) {
	for path != "" {
		if err := os.Remove(s.submoduleDir(path)); err != nil {
			return
		}
		path = parentOf(path)
	}
}

func indexTree(idx *index.Index) map[string]treeEntry {
	out := map[string]treeEntry{}
	for entry := range idx.Entries() {
		if entry.Stage == index.StageMerged {
			out[entry.Path] = treeEntry{mode: entry.Mode, id: entry.ID}
		}
	}
	return out
}

func updatePulledSubmodules(ctx context.Context, r *repo.Repository, branch string, opts PullOptions) error {
	recurse, err := recurseSubmodulesConfigured(r, opts.RecurseSubmodules)
	if err != nil || !recurse {
		return err
	}
	mode := SubmoduleUpdateCheckout
	if pullRebasesSubmodules(r.Config(), branch) {
		mode = SubmoduleUpdateRebase
	}
	return SubmoduleUpdate(ctx, r, nil, SubmoduleUpdateOptions{
		Mode:      mode,
		Recursive: true,
		Progress:  opts.Progress,
		Transport: opts.Fetch.Transport,
		Events:    opts.SubmoduleEvents,
	})
}
