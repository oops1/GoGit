package repo

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

type OpenOptions struct {
	Env        func(string) string
	NoSystem   bool
	SystemFile string
	GlobalFile string
}

func (o OpenOptions) discovery() DiscoverOptions {
	return DiscoverOptions{Env: o.Env}
}

func (o OpenOptions) configOptions(layout Layout) config.Options {
	return config.Options{
		GitDir:      layout.CommonDir,
		WorktreeDir: layout.GitDir,
		SystemFile:  o.SystemFile,
		GlobalFile:  o.GlobalFile,
		NoSystem:    o.NoSystem,
	}
}

type Repository struct {
	ObjectFormat hash.Format

	layout     Layout
	cfg        *config.Config
	core       config.Core
	root       *os.Root
	commonRoot *os.Root
}

func Open(start string, opts OpenOptions) (*Repository, error) {
	layout, err := Discover(start, opts.discovery())
	if err != nil {
		return nil, err
	}
	return OpenLayout(layout, opts)
}

func OpenLayout(layout Layout, opts OpenOptions) (*Repository, error) {
	cfg, err := config.Load(opts.configOptions(layout))
	if err != nil {
		return nil, err
	}
	core, err := cfg.Core()
	if err != nil {
		return nil, err
	}
	format, err := objectFormat(cfg, core)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(layout.GitDir)
	if err != nil {
		return nil, err
	}
	commonRoot, err := os.OpenRoot(layout.CommonDir)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return new(Repository{
		ObjectFormat: format,
		layout:       layout,
		cfg:          cfg,
		core:         core,
		root:         root,
		commonRoot:   commonRoot,
	}), nil
}

func objectFormat(cfg *config.Config, core config.Core) (hash.Format, error) {
	switch core.RepositoryFormatVersion {
	case 0:
		return hash.SHA1, nil
	case 1:
	default:
		return 0, fmt.Errorf("%w: %d", ErrUnsupportedFormatVersion, core.RepositoryFormatVersion)
	}
	extensions, err := cfg.Extensions()
	if err != nil {
		return 0, err
	}
	format, err := hash.ParseFormat(extensions.ObjectFormat)
	if err == nil && !format.Supported() {
		err = fmt.Errorf("%w: %s", hash.ErrUnsupportedFormat, format)
	}
	return format, err
}

func (r *Repository) Close() error {
	return errors.Join(r.root.Close(), r.commonRoot.Close())
}

func (r *Repository) Layout() Layout         { return r.layout }
func (r *Repository) Config() *config.Config { return r.cfg }
func (r *Repository) Core() config.Core      { return r.core }
func (r *Repository) Root() *os.Root         { return r.root }
func (r *Repository) CommonRoot() *os.Root   { return r.commonRoot }
func (r *Repository) GitDir() string         { return r.layout.GitDir }
func (r *Repository) CommonDir() string      { return r.layout.CommonDir }
func (r *Repository) WorkTree() string       { return r.layout.WorkTree }
func (r *Repository) IsBare() bool           { return r.layout.Bare }
func (r *Repository) IsWorktree() bool       { return r.layout.IsWorktree }
func (r *Repository) ObjectsDir() string     { return r.CommonPath(objectsDirName) }
func (r *Repository) PackDir() string        { return r.CommonPath(packDirName) }
func (r *Repository) RefsDir() string        { return r.CommonPath(refsDirName) }
func (r *Repository) IndexFile() string      { return r.GitPath(indexName) }
func (r *Repository) InfoExclude() string    { return r.CommonPath(infoExcludeName) }
func (r *Repository) CommonPath(rel string) string {
	return filepath.Join(r.layout.CommonDir, filepath.FromSlash(rel))
}

func (r *Repository) GitPath(rel string) string {
	if isWorktreePath(rel) {
		return filepath.Join(r.layout.GitDir, filepath.FromSlash(rel))
	}
	return r.CommonPath(rel)
}

func (r *Repository) HooksDir() string {
	if r.core.HooksPath == "" {
		return r.CommonPath(hooksDirName)
	}
	if filepath.IsAbs(r.core.HooksPath) {
		return filepath.Clean(r.core.HooksPath)
	}
	base := r.layout.WorkTree
	if base == "" {
		base = r.layout.CommonDir
	}
	return resolveFrom(base, r.core.HooksPath)
}

type commonEntry struct {
	name  string
	isDir bool
}

var commonEntries = []commonEntry{
	{"branches", true},
	{"common", true},
	{"hooks", true},
	{"info", true},
	{"logs", true},
	{"lost-found", true},
	{"objects", true},
	{"refs", true},
	{"remotes", true},
	{"rr-cache", true},
	{"svn", true},
	{"worktrees", true},
	{"config", false},
	{"gc.pid", false},
	{"packed-refs", false},
	{shallowFileName, false},
}

var worktreeEntries = []string{
	"info/sparse-checkout",
	"logs/HEAD",
	"logs/refs/bisect",
	"logs/refs/rewritten",
	"logs/refs/worktree",
	"refs/bisect",
	"refs/rewritten",
	"refs/worktree",
}

func isWorktreePath(rel string) bool {
	clean := path.Clean(filepath.ToSlash(rel))
	for _, entry := range worktreeEntries {
		if clean == entry || strings.HasPrefix(clean, entry+"/") {
			return true
		}
	}
	for _, entry := range commonEntries {
		if clean == entry.name || entry.isDir && strings.HasPrefix(clean, entry.name+"/") {
			return false
		}
	}
	return true
}
