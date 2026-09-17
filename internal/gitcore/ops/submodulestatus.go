package ops

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

type SubmoduleState int

const (
	SubmoduleStateNotInitialized SubmoduleState = iota
	SubmoduleStateUpToDate
	SubmoduleStateModified
	SubmoduleStateNewCommits
	SubmoduleStateConflict
)

type Submodule struct {
	Name      string
	Path      string
	URL       string
	Recorded  hash.ObjectID
	Head      hash.ObjectID
	Active    bool
	Populated bool
	Modified  bool
	State     SubmoduleState
}

func ListSubmodules(ctx context.Context, r *repo.Repository) ([]Submodule, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	super, err := loadSuperproject(r, nil)
	if errors.Is(err, ErrBareRepository) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Submodule, 0, len(super.links))
	for _, link := range super.links {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, err := super.describe(ctx, link)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *superproject) describe(ctx context.Context, link gitlinkEntry) (Submodule, error) {
	item := Submodule{Name: link.path, Path: link.path, Recorded: link.id}
	if link.known {
		item.Name = link.module.Name
		if url, ok := submodule.URL(s.cfg, link.module); ok {
			item.URL = s.gitmodulesURLOr(url)
		}
		active, err := s.active(link)
		if err != nil {
			return Submodule{}, err
		}
		item.Active = active
	}
	sub, populated, err := openSubmodule(s, link.path)
	if err != nil {
		return Submodule{}, err
	}
	item.Populated = populated
	if populated {
		defer func() { _ = sub.Close() }()
	}
	switch {
	case link.stage != 0:
		item.State = SubmoduleStateConflict
	case !populated:
		item.State = SubmoduleStateNotInitialized
	default:
		if err := describePopulated(ctx, sub, &item); err != nil {
			return Submodule{}, err
		}
	}
	return item, nil
}

func (s *superproject) gitmodulesURLOr(url string) string {
	resolved, err := s.resolveURL(url, "")
	if err != nil {
		return url
	}
	return resolved
}

func describePopulated(ctx context.Context, sub *repo.Repository, item *Submodule) error {
	head, err := gitlink.Head(sub.Layout())
	if err == nil {
		item.Head = head
	}
	dirty, err := submoduleDirty(ctx, sub, false)
	if err != nil {
		return err
	}
	item.Modified = dirty
	switch {
	case item.Head != item.Recorded:
		item.State = SubmoduleStateNewCommits
	case dirty:
		item.State = SubmoduleStateModified
	default:
		item.State = SubmoduleStateUpToDate
	}
	return nil
}

func submoduleDirty(ctx context.Context, sub *repo.Repository, ignoreNoSubmodule bool) (bool, error) {
	db, err := odbOpen(sub.ObjectsDir(), odb.Options{Format: sub.ObjectFormat})
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	tree, err := worktreeOpen(sub, worktree.Options{DB: db, IgnoreNoSubmodule: ignoreNoSubmodule})
	if err != nil {
		return false, err
	}
	defer func() { _ = tree.Close() }()
	status, err := tree.Status(ctx)
	if err != nil {
		return false, err
	}
	for _, entry := range status.Entries {
		if entry.Unstaged != worktree.StatusIgnored {
			return true, nil
		}
	}
	return false, nil
}
