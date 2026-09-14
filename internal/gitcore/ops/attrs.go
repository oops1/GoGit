package ops

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var ErrFilterUnsupported = attributes.ErrFilterUnsupported

type workingTree struct {
	root       *os.Root
	ignore     *attributes.Matcher
	attrs      *attributes.Attributes
	fileMode   bool
	symlinks   bool
	repo       *repo.Repository
	idx        *index.Index
	db         *odb.DB
	loadErr    error
	lfsObjects string
	lfsFetch   attributes.LFSFetchFilter
}

func openWorkingTree(r *repo.Repository) (*workingTree, error) {
	if r.IsBare() || r.WorkTree() == "" {
		return nil, ErrBareRepository
	}
	root, err := fsOpenRoot(r.WorkTree())
	if err != nil {
		return nil, err
	}
	core := r.Core()
	excludesFile := excludesFileOf(r)
	attributesFile, err := attributesFileOf(r)
	if err != nil {
		_ = root.Close()
		return nil, err
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
		Config:         r.Config(),
		ObjectFormat:   r.ObjectFormat,
	})
	return &workingTree{
		root:       root,
		ignore:     ignore,
		attrs:      attrs,
		fileMode:   core.FileMode,
		symlinks:   core.Symlinks,
		repo:       r,
		lfsObjects: lfsObjectsDir(r),
		lfsFetch:   attributes.NewLFSFetchFilter(r.Config(), core.IgnoreCase),
	}, nil
}

func (w *workingTree) close() error {
	if w.db != nil {
		_ = w.db.Close()
	}
	return w.root.Close()
}

func excludesFileOf(r *repo.Repository) string {
	if r.Core().ExcludesFile != "" {
		return r.Core().ExcludesFile
	}
	return attributes.DefaultExcludesFile(os.Getenv)
}

func attributesFileOf(r *repo.Repository) (string, error) {
	configured, err := r.Config().GetPath("core.attributesfile")
	if errors.Is(err, config.ErrNotFound) {
		return attributes.DefaultAttributesFile(os.Getenv), nil
	}
	if err != nil {
		return "", err
	}
	if configured != "" {
		return configured, nil
	}
	return attributes.DefaultAttributesFile(os.Getenv), nil
}

func (w *workingTree) isIgnored(path string, isDir bool) bool {
	ignored, _ := w.ignore.Ignored(path, isDir)
	return ignored
}

func (w *workingTree) indexBlob(path string) attributes.IndexBlob {
	return func() ([]byte, bool) {
		if !w.loadIndexObjects() {
			return nil, false
		}
		return w.idx.ContentBlob(w.db, path)
	}
}

func (w *workingTree) loadIndexObjects() bool {
	if w.idx == nil && w.loadErr == nil {
		w.idx, w.loadErr = readIndex(w.repo)
	}
	if w.db == nil && w.loadErr == nil {
		w.db, w.loadErr = odbOpen(w.repo.ObjectsDir(), odb.Options{Format: w.repo.ObjectFormat})
	}
	return w.loadErr == nil
}

func (w *workingTree) checkinConvert(path string, data []byte) []byte {
	return w.attrs.Policy(path).CompareToGit(data, w.indexBlob(path))
}

func (w *workingTree) stageConvert(path string, data []byte) ([]byte, error) {
	return w.attrs.Policy(path).ToGit(data, w.indexBlob(path))
}

func (w *workingTree) checkoutConvert(path string, data []byte) []byte {
	return w.attrs.Policy(path).ToWorkingTree(data)
}

func smudgeRacilyClean(r *repo.Repository, idx *index.Index) {
	if !idx.HasRacyEntries() {
		return
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		idx.SmudgeRacilyClean(func(*index.Entry) bool { return true })
		return
	}
	defer func() { _ = wt.close() }()
	wt.idx = idx
	idx.SmudgeRacilyClean(wt.racilyModified)
}

func (w *workingTree) racilyModified(entry *index.Entry) bool {
	name := filepath.FromSlash(entry.Path)
	info, err := fsRootLstat(w.root, name)
	if err != nil {
		return false
	}
	probe := *entry
	probe.AssumeValid, probe.SkipWorktree = false, false
	if !probe.Matches(info, false, w.symlinks) {
		return false
	}
	data, err := (&switcher{wt: w}).readWorktreeBytes(entry.Path, info)
	if err != nil {
		return true
	}
	id, err := hashSum(w.repo.ObjectFormat, "blob", data)
	return err != nil || id != entry.ID
}
