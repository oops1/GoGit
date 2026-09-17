package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pathspec"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var (
	ErrSubmodulePathspec      = errors.New("ops: the pathspec did not match any submodule")
	ErrSubmoduleNotRegistered = errors.New("ops: no url found for the submodule path in .gitmodules")
	ErrSubmoduleNoURL         = errors.New("ops: cannot clone a submodule without a url")
	ErrSubmoduleCommand       = errors.New("ops: the submodule update mode runs a command, which is not started")
	ErrSubmoduleNotEmpty      = errors.New("ops: the submodule directory is not empty")
	ErrSubmoduleNoHead        = errors.New("ops: cannot find the current revision of the submodule")
	ErrSubmoduleCommitMissing = errors.New("ops: the submodule remote does not contain the recorded commit")
	ErrSubmoduleRemoteRef     = errors.New("ops: cannot find the remote-tracking revision of the submodule")
	ErrSubmoduleNoBranch      = errors.New("ops: the submodule follows the superproject branch, but the superproject is not on a branch")
	ErrSubmoduleCheckout      = errors.New("ops: unable to check out the submodule commit")
	ErrSubmoduleRebase        = errors.New("ops: unable to rebase the submodule")
	ErrSubmoduleMerge         = errors.New("ops: unable to merge in the submodule")
	ErrSubmoduleFetch         = errors.New("ops: unable to fetch in the submodule")
	ErrSubmoduleClone         = errors.New("ops: unable to clone the submodule")
)

const (
	submoduleModulesDir   = "modules"
	submoduleRecurseKey   = "submodule.recurse"
	submoduleActiveKey    = "submodule.active"
	submoduleStickyKey    = "submodule.stickyrecursiveclone"
	submoduleIndexVersion = index.Version2
)

type SubmoduleEventKind int

const (
	SubmoduleRegistered SubmoduleEventKind = iota
	SubmoduleCloning
	SubmoduleFetching
	SubmoduleFetchRetry
	SubmoduleCheckedOut
	SubmoduleRebased
	SubmoduleMerged
	SubmoduleSkipped
	SubmoduleSkippedUnmerged
	SubmoduleNotInitialized
	SubmoduleSynchronized
	SubmoduleCommandRefused
	SubmoduleAbsorbed
	SubmoduleCleared
	SubmoduleUnregistered
	SubmoduleRemoved
)

type SubmoduleEvent struct {
	Kind    SubmoduleEventKind
	Path    string
	Name    string
	URL     string
	Commit  hash.ObjectID
	Command string
}

type SubmoduleEvents func(SubmoduleEvent)

func (e SubmoduleEvents) emit(event SubmoduleEvent) {
	if e != nil {
		e(event)
	}
}

type gitlinkEntry struct {
	path   string
	id     hash.ObjectID
	stage  index.Stage
	module submodule.Module
	known  bool
}

type superproject struct {
	repo     *repo.Repository
	cfg      *config.Config
	modules  *submodule.Modules
	links    []gitlinkEntry
	prefix   string
	index    *index.Index
	headBlob hash.ObjectID
}

var (
	submoduleReadIndex   = index.ReadFile
	submoduleOpen        = repo.Open
	submoduleOpenLayout  = repo.OpenLayout
	submoduleLoad        = submodule.Load
	writeSubmoduleConfig = setConfigValues
)

func loadSuperproject(r *repo.Repository, specs []string) (*superproject, error) {
	if r.IsBare() || r.WorkTree() == "" {
		return nil, ErrBareRepository
	}
	idx, err := submoduleReadIndex(r.IndexFile())
	if errors.Is(err, fs.ErrNotExist) {
		idx, err = index.New(submoduleIndexVersion), nil
	}
	if err != nil {
		return nil, err
	}
	headBlob, err := headGitmodulesBlob(r)
	if err != nil {
		return nil, err
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	modules, err := submoduleLoad(submodule.Source{WorkTree: r.WorkTree(), Index: idx, Objects: db, HeadBlob: headBlob})
	if err != nil {
		return nil, err
	}
	links, err := listGitlinks(idx, specs)
	if err != nil {
		return nil, err
	}
	for i := range links {
		links[i].module, links[i].known = modules.ByPath(links[i].path)
	}
	return &superproject{repo: r, cfg: r.Config(), modules: modules, links: links, index: idx, headBlob: headBlob}, nil
}

func listGitlinks(idx *index.Index, specs []string) ([]gitlinkEntry, error) {
	set, err := pathspec.Parse(specs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSubmodulePathspec, err)
	}
	each := make([]pathspec.Set, len(specs))
	for i, spec := range specs {
		each[i], _ = pathspec.Parse([]string{spec})
	}
	matched := make([]bool, len(specs))
	var links []gitlinkEntry
	for entry := range idx.Entries() {
		if !entry.Mode.IsSubmodule() || !set.Match(entry.Path) || len(links) > 0 && links[len(links)-1].path == entry.Path {
			continue
		}
		for i := range each {
			matched[i] = matched[i] || each[i].Match(entry.Path)
		}
		links = append(links, gitlinkEntry{path: entry.Path, id: entry.ID, stage: entry.Stage})
	}
	if missing := slices.Index(matched, false); missing >= 0 {
		return nil, fmt.Errorf("%w: %s", ErrSubmodulePathspec, specs[missing])
	}
	return links, nil
}

func headGitmodulesBlob(r *repo.Repository) (hash.ObjectID, error) {
	rc, err := openRepoContext(r)
	if err != nil {
		return hash.Zero, err
	}
	defer func() { _ = rc.close() }()
	head, err := resolveHeadTarget(rc.refs)
	if errors.Is(err, refs.ErrNotFound) || err == nil && head.old.IsZero() {
		return hash.Zero, nil
	}
	if err != nil {
		return hash.Zero, err
	}
	commit, err := rc.db.Commit(head.old)
	if err != nil {
		return hash.Zero, err
	}
	tree, err := rc.db.Tree(commit.Tree)
	if err != nil {
		return hash.Zero, err
	}
	for _, entry := range tree.Entries {
		if entry.Name == submodule.GitmodulesFile && !entry.Mode.IsTree() {
			return entry.ID, nil
		}
	}
	return hash.Zero, nil
}

func (s *superproject) displayPath(path string) string {
	return s.prefix + path
}

func (s *superproject) active(link gitlinkEntry) (bool, error) {
	if !link.known {
		return false, nil
	}
	return submodule.Active(s.cfg, link.module)
}

func (s *superproject) activeLinks(links []gitlinkEntry) ([]gitlinkEntry, error) {
	var out []gitlinkEntry
	for _, link := range links {
		active, err := s.active(link)
		if err != nil {
			return nil, err
		}
		if active {
			out = append(out, link)
		}
	}
	return out, nil
}

func (s *superproject) submoduleDir(path string) string {
	return filepath.Join(s.repo.WorkTree(), filepath.FromSlash(path))
}

func defaultRemoteName(r *repo.Repository) (string, error) {
	cfg := r.Config()
	branch, onBranch, err := currentBranchShort(r)
	if err != nil {
		return "", err
	}
	if onBranch {
		if name, ok := cfg.GetValue("branch." + branch + ".remote"); ok {
			return name, nil
		}
	}
	if remotes := cfg.Remotes(); len(remotes) == 1 {
		return remotes[0].Name, nil
	}
	return defaultCloneRemoteName, nil
}

func (s *superproject) remoteURL() (string, error) {
	name, err := defaultRemoteName(s.repo)
	if err != nil {
		return "", err
	}
	if url, ok := s.cfg.GetValue("remote." + name + ".url"); ok {
		return url, nil
	}
	return filepath.ToSlash(s.repo.WorkTree()), nil
}

func (s *superproject) resolveURL(url, upPath string) (string, error) {
	if !submodule.IsRelativeURL(url) {
		return url, nil
	}
	base, err := s.remoteURL()
	if err != nil {
		return "", err
	}
	return submodule.ResolveRelativeURL(base, url, upPath)
}

func submoduleRemoteName(sub *repo.Repository, url string) (string, error) {
	if url != "" {
		for _, rem := range remote.List(sub.Config()) {
			if slices.Contains(rem.URLs, url) {
				return rem.Name, nil
			}
		}
	}
	return defaultRemoteName(sub)
}

func populatedLayout(dir string) (repo.Layout, bool) {
	return repo.DiscoverWorkTree(dir)
}

func openSubmodule(s *superproject, path string) (*repo.Repository, bool, error) {
	layout, ok := populatedLayout(s.submoduleDir(path))
	if !ok {
		return nil, false, nil
	}
	sub, err := submoduleOpenLayout(layout, s.repo.Options())
	return sub, true, err
}

func setConfigValues(path string, values ...[2]string) error {
	return editConfigFile(path, func(file *config.File) error {
		for _, value := range values {
			if err := file.Set(value[0], value[1]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *superproject) setConfig(values ...[2]string) error {
	return writeSubmoduleConfig(s.repo.CommonPath("config"), values...)
}

func reopenRepository(r *repo.Repository) (*repo.Repository, error) {
	return repo.OpenLayout(r.Layout(), r.Options())
}
