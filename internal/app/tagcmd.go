package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/tag"
)

var newTagView = tag.NewView

var runCreateTag = ops.CreateTag

var runDeleteTag = ops.DeleteTag

func (a *App) tagItems(id hash.ObjectID) []widget.MenuItem {
	item := menuItem("Menu.Context.CreateTag", func() { a.openTag(id) })
	item.Disabled = a.State().ActiveRepository == ""
	return []widget.MenuItem{item}
}

func (a *App) deleteTagItems(ref refs.Name) []widget.MenuItem {
	if !ref.IsTag() {
		return nil
	}
	return []widget.MenuItem{menuItem("Menu.Context.DeleteTag", func() { a.deleteTag(ref.Short()) })}
}

func (a *App) openTag(id hash.ObjectID) {
	o := a.opened()
	if o == nil {
		return
	}
	view, err := newTagView()
	if err != nil {
		a.log.Warn("open tag dialog failed", "error", err)
		return
	}
	view.SetKnown(tag.Known{Commit: shortHash(id), Taken: a.tagNames()})
	view.OnOK = func(model tag.Model) {
		a.eng.CloseModal(view.Dialog())
		a.createTag(id, model)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) tagNames() []string {
	o := a.opened()
	if o == nil {
		return nil
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read tags failed", "error", err)
		return nil
	}
	names := make([]string, 0, len(snap.Tags))
	for _, known := range snap.Tags {
		names = append(names, known.Name.Short())
	}
	return names
}

func (a *App) createTag(id hash.ObjectID, model tag.Model) {
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		_, err := runCreateTag(ctx, r, model.Name, id.String(), ops.CreateTagOptions{Message: model.Message})
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("create tag failed", "tag", model.Name, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.TagFailed", err))
		} else {
			a.statusLabel.SetText(i18n.Tf("Status.TagCreated", model.Name))
		}
		a.RefreshRepository()
	})
}

func (a *App) deleteTag(name string) {
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return runDeleteTag(ctx, r, name)
	}, func(err error) {
		if err != nil {
			a.log.Warn("delete tag failed", "tag", name, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.TagDeleteFailed", err))
		} else {
			a.statusLabel.SetText(i18n.Tf("Status.TagDeleted", name))
		}
		a.RefreshRepository()
	})
}
