package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var (
	ErrSubmoduleDeinitNoPaths   = errors.New("ops: name the submodules to deinitialise or choose all of them")
	ErrSubmoduleLocalChanges    = errors.New("ops: the submodule work tree contains local modifications")
	ErrSubmoduleStagedChanges   = errors.New("ops: the submodule has changes staged in the index")
	ErrGitmodulesUnstaged       = errors.New("ops: stage the changes to .gitmodules before removing a submodule")
	ErrSubmoduleNotAGitlink     = errors.New("ops: the path is not a submodule in the index")
	ErrSubmoduleWorktrees       = errors.New("ops: cannot move the git directory of a submodule with more than one worktree")
	ErrSubmoduleBrokenGitDir    = errors.New("ops: the .git entry of the submodule is not a git repository")
	ErrSubmoduleUnknownName     = errors.New("ops: could not look up the name of the submodule")
	ErrGitmodulesUnmergedChange = errors.New("ops: cannot change an unmerged .gitmodules")
)

type SubmoduleDeinitOptions struct {
	All    bool
	Force  bool
	Events SubmoduleEvents
}

type SubmoduleRemoveOptions struct {
	Force  bool
	Events SubmoduleEvents
}

var (
	editSubmoduleConfig = editConfigFile
	renameGitDir        = os.Rename
	removeSubmoduleTree = os.RemoveAll
)

func SubmoduleDeinit(ctx context.Context, r *repo.Repository, paths []string, opts SubmoduleDeinitOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(paths) == 0 && !opts.All {
		return ErrSubmoduleDeinitNoPaths
	}
	super, err := loadSuperproject(r, paths)
	if err != nil {
		return err
	}
	for _, link := range super.links {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !link.known {
			continue
		}
		if err := super.deinitOne(ctx, link, opts); err != nil {
			return err
		}
	}
	return nil
}

func (s *superproject) deinitOne(ctx context.Context, link gitlinkEntry, opts SubmoduleDeinitOptions) error {
	dir := s.submoduleDir(link.path)
	path := s.displayPath(link.path)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		if info, err := os.Lstat(filepath.Join(dir, gitDirName)); err == nil && info.IsDir() {
			if err := s.absorbGitDir(link, opts.Events); err != nil {
				return err
			}
		}
		if !opts.Force {
			if err := s.checkRemovable(ctx, []gitlinkEntry{link}); err != nil {
				return err
			}
		}
		if err := removeSubmoduleTree(dir); err != nil {
			return err
		}
		opts.Events.emit(SubmoduleEvent{Kind: SubmoduleCleared, Path: path, Name: link.module.Name})
		if err := s.unsetCoreWorktree(link.module.Name); err != nil {
			return err
		}
	}
	_ = os.Mkdir(dir, directoryMode)
	if !s.hasModuleConfig(link.module.Name) {
		return nil
	}
	err := editSubmoduleConfig(s.repo.CommonPath("config"), func(file *config.File) error {
		return file.RemoveSection("submodule." + link.module.Name)
	})
	if err != nil && !errors.Is(err, config.ErrSectionNotFound) {
		return err
	}
	opts.Events.emit(SubmoduleEvent{Kind: SubmoduleUnregistered, Path: path, Name: link.module.Name, URL: link.module.URL})
	return nil
}

func (s *superproject) hasModuleConfig(name string) bool {
	return slices.Contains(s.cfg.Subsections("submodule"), name)
}

func (s *superproject) unsetCoreWorktree(name string) error {
	path := filepath.Join(s.modulesGitDir(name), "config")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	err := editSubmoduleConfig(path, func(file *config.File) error {
		return file.Unset("core.worktree")
	})
	if errors.Is(err, config.ErrNotFound) {
		return nil
	}
	return err
}

func SubmoduleRemove(ctx context.Context, r *repo.Repository, paths []string, opts SubmoduleRemoveOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	super, err := loadSuperproject(r, nil)
	if err != nil {
		return err
	}
	links, err := super.exactLinks(paths)
	if err != nil {
		return err
	}
	if !super.gitmodulesStaged() {
		return ErrGitmodulesUnstaged
	}
	for _, link := range links {
		if !super.usesGitfile(link.path) {
			if err := super.absorbGitDir(link, opts.Events); err != nil {
				return err
			}
		}
	}
	if !opts.Force {
		if err := super.checkRemovable(ctx, links); err != nil {
			return err
		}
	}
	return super.removeLinks(ctx, links, opts)
}

func (s *superproject) exactLinks(paths []string) ([]gitlinkEntry, error) {
	var links []gitlinkEntry
	for _, p := range paths {
		clean, err := cleanRepoPath(filepath.ToSlash(p))
		if err != nil {
			return nil, err
		}
		found := false
		for _, link := range s.links {
			if link.path == clean {
				links, found = append(links, link), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrSubmoduleNotAGitlink, p)
		}
	}
	return links, nil
}

func (s *superproject) gitmodulesStaged() bool {
	entry, ok := s.index.Get(submodule.GitmodulesFile, index.StageMerged)
	if !ok {
		return true
	}
	data, err := os.ReadFile(filepath.Join(s.repo.WorkTree(), submodule.GitmodulesFile))
	if err != nil {
		return true
	}
	wt, err := openWorkingTree(s.repo)
	if err != nil {
		return false
	}
	defer func() { _ = wt.close() }()
	id, err := hashSum(s.repo.ObjectFormat, "blob", wt.checkinConvert(submodule.GitmodulesFile, data))
	return err == nil && id == entry.ID
}

func (s *superproject) removeLinks(ctx context.Context, links []gitlinkEntry, opts SubmoduleRemoveOptions) error {
	lock, err := lockIndex(s.repo)
	if err != nil {
		return err
	}
	for _, link := range links {
		lock.idx.Remove(link.path)
	}
	gitmodulesChanged := false
	for _, link := range links {
		if err := removeSubmoduleTree(s.submoduleDir(link.path)); err != nil {
			lock.abort()
			return err
		}
		changed, err := s.removeGitmodulesSection(link)
		if err != nil {
			lock.abort()
			return err
		}
		gitmodulesChanged = gitmodulesChanged || changed
		opts.Events.emit(SubmoduleEvent{Kind: SubmoduleRemoved, Path: s.displayPath(link.path), Name: link.module.Name})
	}
	if err := lock.commit(); err != nil {
		return err
	}
	if !gitmodulesChanged {
		return nil
	}
	return Stage(ctx, s.repo, []string{submodule.GitmodulesFile}, StageOptions{Force: true})
}

func (s *superproject) removeGitmodulesSection(link gitlinkEntry) (bool, error) {
	gitmodules := filepath.Join(s.repo.WorkTree(), submodule.GitmodulesFile)
	if _, err := os.Stat(gitmodules); err != nil {
		return false, nil
	}
	if len(s.index.Conflicts(submodule.GitmodulesFile)) > 0 {
		return false, ErrGitmodulesUnmergedChange
	}
	if !link.known {
		return false, nil
	}
	err := editSubmoduleConfig(gitmodules, func(file *config.File) error {
		return file.RemoveSection("submodule." + link.module.Name)
	})
	return err == nil, err
}

func (s *superproject) checkRemovable(ctx context.Context, links []gitlinkEntry) error {
	rc, err := openRepoContext(s.repo)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	head, err := resolveHeadTarget(rc.refs)
	if err != nil {
		return err
	}
	headTree, err := commitTreeEntries(rc.db, head.old)
	if err != nil {
		return err
	}
	for _, link := range links {
		dir := s.submoduleDir(link.path)
		info, err := os.Lstat(dir)
		if err != nil || link.stage != index.StageMerged && emptyDir(dir) {
			continue
		}
		local, err := s.locallyChanged(ctx, link, info)
		if err != nil {
			return err
		}
		recorded, inHead := headTree[link.path]
		staged := head.old.IsZero() || !inHead || !recorded.mode.IsSubmodule() || recorded.id != link.id
		switch {
		case local && staged:
			return fmt.Errorf("%w: %s", ErrSubmoduleStagedChanges, s.displayPath(link.path))
		case local:
			return fmt.Errorf("%w: %s", ErrSubmoduleLocalChanges, s.displayPath(link.path))
		case staged:
			return fmt.Errorf("%w: %s", ErrSubmoduleStagedChanges, s.displayPath(link.path))
		}
	}
	return nil
}

func emptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 0
}

func (s *superproject) locallyChanged(ctx context.Context, link gitlinkEntry, info fs.FileInfo) (bool, error) {
	if !info.IsDir() {
		return true, nil
	}
	dir := s.submoduleDir(link.path)
	if head, found, err := gitlink.HeadOf(dir); found && err == nil && head != link.id {
		return true, nil
	}
	if emptyDir(dir) {
		return false, nil
	}
	if !s.usesGitfile(link.path) {
		return true, nil
	}
	sub, populated, err := openSubmodule(s, link.path)
	if err != nil || !populated {
		return true, err
	}
	defer func() { _ = sub.Close() }()
	return submoduleDirty(ctx, sub, true)
}

func (s *superproject) usesGitfile(path string) bool {
	dir := s.submoduleDir(path)
	info, err := os.Lstat(filepath.Join(dir, gitDirName))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	sub, populated, err := openSubmodule(s, path)
	if err != nil || !populated {
		return false
	}
	defer func() { _ = sub.Close() }()
	nested, err := loadSuperproject(sub, nil)
	if err != nil {
		return false
	}
	for _, link := range nested.links {
		if _, err := os.Lstat(filepath.Join(nested.submoduleDir(link.path), gitDirName)); err != nil {
			continue
		}
		if !nested.usesGitfile(link.path) {
			return false
		}
	}
	return true
}

func (s *superproject) absorbGitDir(link gitlinkEntry, events SubmoduleEvents) error {
	dir := s.submoduleDir(link.path)
	dotGit := filepath.Join(dir, gitDirName)
	info, err := os.Lstat(dotGit)
	if err != nil {
		return nil
	}
	_, populated := populatedLayout(dir)
	switch {
	case info.IsDir() && repo.IsRepository(dotGit):
		if err := s.relocateGitDir(link, dotGit); err != nil {
			return err
		}
		events.emit(SubmoduleEvent{Kind: SubmoduleAbsorbed, Path: s.displayPath(link.path), Name: link.module.Name})
	case info.IsDir():
		return fmt.Errorf("%w: %s", ErrSubmoduleBrokenGitDir, s.displayPath(link.path))
	case !populated && !link.known:
		return fmt.Errorf("%w: %s", ErrSubmoduleUnknownName, s.displayPath(link.path))
	case !populated:
		if err := connectWorkTreeAndGitDir(dir, s.modulesGitDir(link.module.Name)); err != nil {
			return err
		}
	}
	return s.absorbNested(link, events)
}

func (s *superproject) relocateGitDir(link gitlinkEntry, dotGit string) error {
	if entries, err := os.ReadDir(filepath.Join(dotGit, "worktrees")); err == nil && len(entries) > 0 {
		return fmt.Errorf("%w: %s", ErrSubmoduleWorktrees, s.displayPath(link.path))
	}
	if !link.known {
		return fmt.Errorf("%w: %s", ErrSubmoduleUnknownName, s.displayPath(link.path))
	}
	target := s.modulesGitDir(link.module.Name)
	if err := submodule.ValidateGitDir(s.repo.GitPath(submoduleModulesDir), link.module.Name, repo.IsRepository); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), directoryMode); err != nil {
		return err
	}
	if err := renameGitDir(dotGit, target); err != nil {
		return err
	}
	return connectWorkTreeAndGitDir(s.submoduleDir(link.path), target)
}

func (s *superproject) absorbNested(link gitlinkEntry, events SubmoduleEvents) error {
	sub, populated, err := openSubmodule(s, link.path)
	if err != nil || !populated {
		return err
	}
	defer func() { _ = sub.Close() }()
	nested, err := loadSuperproject(sub, nil)
	if err != nil {
		return err
	}
	nested.prefix = s.displayPath(link.path) + "/"
	for _, inner := range nested.links {
		if err := nested.absorbGitDir(inner, events); err != nil {
			return err
		}
	}
	return nil
}

func editConfigFile(path string, edit func(*config.File) error) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := config.Parse(data)
	if err != nil {
		return err
	}
	if err := edit(file); err != nil {
		return err
	}
	return file.Save(path)
}
