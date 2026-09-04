package worktree

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type Options struct {
	DB       *odb.DB
	Refs     *refs.Store
	Env      func(string) string
	Workers  int
	MaxFiles int
}

type Worktree struct {
	repo     *repo.Repository
	db       *odb.DB
	refs     *refs.Store
	ownRefs  bool
	index    *index.Index
	root     *os.Root
	ignore   *attributes.Matcher
	ignoreMu sync.Mutex
	attrs    *attributes.Attributes
	attrsMu  sync.Mutex
	format   hash.Format
	fileMode bool
	workers  int
	maxFiles int
}

func Open(r *repo.Repository, opts Options) (*Worktree, error) {
	if opts.DB == nil {
		return nil, ErrNoObjectDatabase
	}
	if r.IsBare() || r.WorkTree() == "" {
		return nil, ErrBareRepository
	}
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}
	idx, err := loadIndex(r.IndexFile())
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.WorkTree())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadWorkingTree, err)
	}
	refsStore, ownRefs, err := openRefs(r, opts)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	core := r.Core()
	excludesFile := excludesFileOf(r, env)
	attributesFile, err := attributesFileOf(r, env)
	if err != nil {
		return closeAndReturn(root, refsStore, ownRefs, err)
	}
	ignore := attributes.NewMatcher(attributes.IgnoreOptions{
		Work:         attributes.RootLoader(root),
		Global:       attributes.OSLoader(""),
		InfoExclude:  r.InfoExclude(),
		ExcludesFile: excludesFile,
		IgnoreCase:   core.IgnoreCase,
	})
	attrs := attributes.New(attributes.AttributeOptions{
		Work:           attributes.RootLoader(root),
		Global:         attributes.OSLoader(""),
		InfoFile:       r.CommonPath("info/attributes"),
		AttributesFile: attributesFile,
		IgnoreCase:     core.IgnoreCase,
		AutoCRLF:       core.AutoCRLF,
		EOL:            core.EOL,
	})
	return &Worktree{
		repo:     r,
		db:       opts.DB,
		refs:     refsStore,
		ownRefs:  ownRefs,
		index:    idx,
		root:     root,
		ignore:   ignore,
		attrs:    attrs,
		format:   opts.DB.Format(),
		fileMode: core.FileMode,
		workers:  workerCount(opts.Workers),
		maxFiles: opts.MaxFiles,
	}, nil
}

func loadIndex(path string) (*index.Index, error) {
	idx, err := index.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return index.New(index.Version2), nil
	}
	if err != nil {
		return nil, err
	}
	return idx, nil
}

func openRefs(r *repo.Repository, opts Options) (*refs.Store, bool, error) {
	if opts.Refs != nil {
		return opts.Refs, false, nil
	}
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare()})
	if err != nil {
		return nil, false, err
	}
	return store, true, nil
}

func closeAndReturn(root *os.Root, store *refs.Store, ownRefs bool, err error) (*Worktree, error) {
	_ = root.Close()
	if ownRefs {
		_ = store.Close()
	}
	return nil, err
}

func excludesFileOf(r *repo.Repository, env func(string) string) string {
	if r.Core().ExcludesFile != "" {
		return r.Core().ExcludesFile
	}
	return attributes.DefaultExcludesFile(env)
}

func attributesFileOf(r *repo.Repository, env func(string) string) (string, error) {
	configured, err := r.Config().GetPath("core.attributesfile")
	if errors.Is(err, config.ErrNotFound) {
		return attributes.DefaultAttributesFile(env), nil
	}
	if err != nil {
		return "", err
	}
	if configured != "" {
		return configured, nil
	}
	return attributes.DefaultAttributesFile(env), nil
}

func workerCount(requested int) int {
	if requested > 0 {
		return requested
	}
	return runtime.NumCPU()
}

func (w *Worktree) Close() error {
	var errs []error
	if w.ownRefs {
		errs = append(errs, w.refs.Close())
	}
	errs = append(errs, w.root.Close())
	return errors.Join(errs...)
}

func (w *Worktree) isIgnored(path string, isDir bool) bool {
	w.ignoreMu.Lock()
	defer w.ignoreMu.Unlock()
	ignored, _ := w.ignore.Ignored(path, isDir)
	return ignored
}

func (w *Worktree) textPolicy(path string) attributes.TextPolicy {
	w.attrsMu.Lock()
	defer w.attrsMu.Unlock()
	return w.attrs.Text(path)
}

func (w *Worktree) Index() *index.Index { return w.index }

func (w *Worktree) Repository() *repo.Repository { return w.repo }
