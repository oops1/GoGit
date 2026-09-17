package ops

import (
	"context"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

type SubmoduleInitOptions struct {
	Events SubmoduleEvents
}

func SubmoduleInit(ctx context.Context, r *repo.Repository, paths []string, opts SubmoduleInitOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	super, err := loadSuperproject(r, paths)
	if err != nil {
		return err
	}
	return super.init(ctx, len(paths) == 0, opts.Events)
}

func (s *superproject) init(ctx context.Context, allPaths bool, events SubmoduleEvents) error {
	links := s.links
	if _, set := s.cfg.GetValue(submoduleActiveKey); allPaths && set {
		active, err := s.activeLinks(links)
		if err != nil {
			return err
		}
		links = active
	}
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.initOne(link, events); err != nil {
			return err
		}
	}
	return nil
}

func (s *superproject) initOne(link gitlinkEntry, events SubmoduleEvents) error {
	path := s.displayPath(link.path)
	if !link.known {
		return fmt.Errorf("%w: %s", ErrSubmoduleNotRegistered, path)
	}
	module := link.module
	active, err := s.active(link)
	if err != nil {
		return err
	}
	if !active {
		if err := s.setConfig([2]string{"submodule." + module.Name + ".active", "true"}); err != nil {
			return err
		}
	}
	urlKey := "submodule." + module.Name + ".url"
	if _, set := s.cfg.GetValue(urlKey); !set {
		if module.URL == "" {
			return fmt.Errorf("%w: %s", ErrSubmoduleNotRegistered, path)
		}
		url, err := s.resolveURL(module.URL, "")
		if err != nil {
			return err
		}
		if err := s.setConfig([2]string{urlKey, url}); err != nil {
			return err
		}
		events.emit(SubmoduleEvent{Kind: SubmoduleRegistered, Path: path, Name: module.Name, URL: url})
	}
	updateKey := "submodule." + module.Name + ".update"
	if _, set := s.cfg.GetValue(updateKey); set || module.Update.Type == submodule.UpdateUnspecified {
		return nil
	}
	return s.setConfig([2]string{updateKey, module.Update.Type.String()})
}
