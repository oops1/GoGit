package ops

import (
	"context"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

type SubmoduleSyncOptions struct {
	Recursive bool
	Events    SubmoduleEvents
}

func SubmoduleSync(ctx context.Context, r *repo.Repository, paths []string, opts SubmoduleSyncOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	super, err := loadSuperproject(r, paths)
	if err != nil {
		return err
	}
	return super.sync(ctx, opts)
}

func (s *superproject) sync(ctx context.Context, opts SubmoduleSyncOptions) error {
	for _, link := range s.links {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.syncOne(ctx, link, opts); err != nil {
			return err
		}
	}
	return nil
}

func (s *superproject) syncURLs(module submodule.Module, path string) (string, string, error) {
	if !submodule.IsRelativeURL(module.URL) {
		return module.URL, module.URL, nil
	}
	origin, err := s.resolveURL(module.URL, submodule.UpPath(path))
	if err != nil {
		return "", "", err
	}
	super, err := s.resolveURL(module.URL, "")
	return origin, super, err
}

func (s *superproject) syncOne(ctx context.Context, link gitlinkEntry, opts SubmoduleSyncOptions) error {
	active, err := s.active(link)
	if err != nil || !active {
		return err
	}
	if err := validateSubmodulePath(s.repo.WorkTree(), link.path); err != nil {
		return err
	}
	module := link.module
	originURL, superURL, err := s.syncURLs(module, link.path)
	if err != nil {
		return err
	}
	if err := s.setConfig([2]string{"submodule." + module.Name + ".url", superURL}); err != nil {
		return err
	}
	sub, populated, err := openSubmodule(s, link.path)
	if err != nil || !populated {
		return err
	}
	defer func() { _ = sub.Close() }()
	remoteName, err := submoduleRemoteName(sub, superURL)
	if err != nil {
		return err
	}
	if err := writeSubmoduleConfig(filepath.Join(sub.GitDir(), "config"), [2]string{"remote." + remoteName + ".url", originURL}); err != nil {
		return err
	}
	opts.Events.emit(SubmoduleEvent{Kind: SubmoduleSynchronized, Path: s.displayPath(link.path), Name: module.Name, URL: originURL})
	if !opts.Recursive {
		return nil
	}
	nested, err := reopenSubmodule(sub)
	if err != nil {
		return err
	}
	defer func() { _ = nested.Close() }()
	inner, err := loadSuperproject(nested, nil)
	if err != nil {
		return err
	}
	inner.prefix = s.displayPath(link.path) + "/"
	return inner.sync(ctx, opts)
}
