package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/pathspec"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

var (
	ErrGitmodulesNotInWorkTree = errors.New("ops: the .gitmodules file must be in the working tree")
	ErrSubmoduleURLNotAbsolute = errors.New("ops: the repository url must be absolute or begin with ./ or ../")
	ErrSubmoduleInIndex        = errors.New("ops: the path already exists in the index")
	ErrSubmoduleInIndexNotLink = errors.New("ops: the path already exists in the index and is not a submodule")
	ErrSubmodulePathIgnored    = errors.New("ops: the path is ignored by one of the .gitignore files")
	ErrSubmoduleNameInUse      = errors.New("ops: the submodule name is already used for another path")
	ErrSubmoduleInvalidName    = errors.New("ops: the submodule name is not valid")
	ErrSubmodulePathNotRepo    = errors.New("ops: the path already exists and is not a valid git repository")
	ErrSubmoduleGitDirExists   = errors.New("ops: a git directory for the submodule is found locally")
)

type SubmoduleAddOptions struct {
	Path      string
	Name      string
	Branch    string
	Depth     int
	Force     bool
	Progress  progress.Func
	Transport transport.Options
	Events    SubmoduleEvents
}

type SubmoduleAddResult struct {
	Name     string
	Path     string
	URL      string
	Existing bool
}

func SubmoduleAdd(ctx context.Context, r *repo.Repository, url string, opts SubmoduleAddOptions) (SubmoduleAddResult, error) {
	if err := ctx.Err(); err != nil {
		return SubmoduleAddResult{}, err
	}
	super, err := loadSuperproject(r, nil)
	if err != nil {
		return SubmoduleAddResult{}, err
	}
	if !super.gitmodulesWritable() {
		return SubmoduleAddResult{}, ErrGitmodulesNotInWorkTree
	}
	add, err := super.planAdd(url, opts)
	if err != nil {
		return SubmoduleAddResult{}, err
	}
	if add.Existing, err = super.populateAdded(ctx, add, opts); err != nil {
		return add, err
	}
	return add, super.registerAdded(ctx, add, url, opts)
}

func (s *superproject) gitmodulesWritable() bool {
	if _, err := os.Stat(filepath.Join(s.repo.WorkTree(), submodule.GitmodulesFile)); err == nil {
		return true
	}
	_, staged := s.index.Get(submodule.GitmodulesFile, index.StageMerged)
	return !staged && s.headBlob.IsZero()
}

func (s *superproject) planAdd(url string, opts SubmoduleAddOptions) (SubmoduleAddResult, error) {
	add := SubmoduleAddResult{Path: opts.Path}
	var err error
	if add.Path == "" {
		if add.Path, err = submodule.URLBasename(url); err != nil {
			return add, err
		}
	}
	if add.URL, err = s.addURL(url); err != nil {
		return add, err
	}
	if add.Path, err = cleanRepoPath(filepath.ToSlash(add.Path)); err != nil {
		return add, err
	}
	if err := validateSubmodulePath(s.repo.WorkTree(), add.Path); err != nil {
		return add, err
	}
	if err := s.refuseIndexMatch(add.Path, opts.Force); err != nil {
		return add, err
	}
	if _, found, headErr := gitlink.HeadOf(s.submoduleDir(add.Path)); found && headErr != nil {
		return add, fmt.Errorf("%w: %s", ErrNoCommitCheckedOut, add.Path)
	}
	if !opts.Force {
		if err := s.refuseIgnored(add.Path); err != nil {
			return add, err
		}
	}
	add.Name, err = s.addName(add.Path, opts.Name, opts.Force)
	return add, err
}

func (s *superproject) addURL(url string) (string, error) {
	switch {
	case submodule.IsRelativeURL(url):
		return s.resolveURL(url, "")
	case url != "" && os.IsPathSeparator(url[0]), strings.Contains(url, ":"):
		return url, nil
	}
	return "", fmt.Errorf("%w: %s", ErrSubmoduleURLNotAbsolute, url)
}

func (s *superproject) refuseIndexMatch(rel string, force bool) error {
	set, err := pathspec.Parse([]string{rel})
	if err != nil {
		return err
	}
	for entry := range s.index.Entries() {
		switch {
		case !set.Match(entry.Path), force && entry.Mode.IsSubmodule():
		case force:
			return fmt.Errorf("%w: %s", ErrSubmoduleInIndexNotLink, rel)
		default:
			return fmt.Errorf("%w: %s", ErrSubmoduleInIndex, rel)
		}
	}
	return nil
}

func (s *superproject) refuseIgnored(rel string) error {
	wt, err := openWorkingTree(s.repo)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()
	info, statErr := os.Lstat(s.submoduleDir(rel))
	if wt.isIgnored(rel, statErr == nil && info.IsDir()) {
		return fmt.Errorf("%w: %s", ErrSubmodulePathIgnored, rel)
	}
	return nil
}

func (s *superproject) addName(rel, name string, force bool) (string, error) {
	if name == "" {
		name = rel
	}
	if existing, ok := s.modules.ByName(name); ok && existing.Path != rel {
		if !force {
			return "", fmt.Errorf("%w: %s: %s", ErrSubmoduleNameInUse, name, existing.Path)
		}
		base := name
		for n := 1; ; n++ {
			name = base + strconv.Itoa(n)
			if _, taken := s.modules.ByName(name); !taken {
				break
			}
		}
	}
	if !submodule.NameAllowed(name) {
		return "", fmt.Errorf("%w: %s", ErrSubmoduleInvalidName, name)
	}
	return name, nil
}

func (s *superproject) populateAdded(ctx context.Context, add SubmoduleAddResult, opts SubmoduleAddOptions) (bool, error) {
	dir := s.submoduleDir(add.Path)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		if _, ok := populatedLayout(dir); !ok {
			return false, fmt.Errorf("%w: %s", ErrSubmodulePathNotRepo, add.Path)
		}
		return true, nil
	}
	if info, err := os.Stat(s.modulesGitDir(add.Name)); err == nil && info.IsDir() && !opts.Force {
		return false, fmt.Errorf("%w: %s", ErrSubmoduleGitDirExists, add.Name)
	}
	record := updateRecord{link: gitlinkEntry{path: add.Path, module: submodule.Module{Name: add.Name, Path: add.Path}}, justCloned: true, displayPath: add.Path}
	if err := s.cloneSubmodule(ctx, record, add.URL, SubmoduleUpdateOptions{Depth: opts.Depth, Progress: opts.Progress, Transport: opts.Transport, Events: opts.Events}); err != nil {
		return false, err
	}
	if err := s.checkoutAdded(ctx, dir, opts.Branch); err != nil {
		return false, fmt.Errorf("%w: %s: %w", ErrSubmoduleCheckout, add.Path, err)
	}
	return false, nil
}

func (s *superproject) checkoutAdded(ctx context.Context, dir, branch string) error {
	sub, err := submoduleOpen(dir, s.repo.Options())
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if branch != "" {
		_, err := StartBranch(ctx, sub, branch, defaultCloneRemoteName+"/"+branch, StartBranchOptions{Force: true, Track: true})
		return err
	}
	head, err := gitlink.Head(sub.Layout())
	if err != nil {
		return err
	}
	return CheckoutTree(ctx, sub, head, CheckoutOptions{Force: true})
}

func (s *superproject) registerAdded(ctx context.Context, add SubmoduleAddResult, rawURL string, opts SubmoduleAddOptions) error {
	if err := s.setConfig([2]string{"submodule." + add.Name + ".url", add.URL}); err != nil {
		return err
	}
	if err := Stage(ctx, s.repo, []string{add.Path}, StageOptions{Force: opts.Force}); err != nil {
		return err
	}
	values := [][2]string{{"submodule." + add.Name + ".path", add.Path}, {"submodule." + add.Name + ".url", rawURL}}
	if opts.Branch != "" {
		values = append(values, [2]string{"submodule." + add.Name + ".branch", opts.Branch})
	}
	if err := writeSubmoduleConfig(filepath.Join(s.repo.WorkTree(), submodule.GitmodulesFile), values...); err != nil {
		return err
	}
	if err := Stage(ctx, s.repo, []string{submodule.GitmodulesFile}, StageOptions{Force: true}); err != nil {
		return err
	}
	active := false
	if s.cfg.Has(submoduleActiveKey) {
		var err error
		if active, err = submodule.Active(s.cfg, submodule.Module{Name: add.Name, Path: add.Path}); err != nil {
			return err
		}
	}
	if active {
		return nil
	}
	return s.setConfig([2]string{"submodule." + add.Name + ".active", "true"})
}
