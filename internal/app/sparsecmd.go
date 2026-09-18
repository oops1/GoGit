package app

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/sparse"
)

var (
	newSparseView       = sparse.NewView
	listSparseCheckout  = ops.SparseCheckoutList
	setSparseCheckout   = ops.SparseCheckoutSet
	closeSparseCheckout = ops.SparseCheckoutDisable
)

func (a *App) registerSparseHandlers() {
	a.handlers[CmdSparseCheckout] = a.openSparseCheckout
}

func (a *App) openSparseCheckout() {
	o := a.opened()
	if o == nil {
		return
	}
	view, err := newSparseView(a.sparseModel(o.repo))
	if err != nil {
		a.log.Warn("open sparse checkout dialog failed", "error", err)
		return
	}
	view.OnOK = func(model sparse.Model) {
		a.eng.CloseModal(view.Dialog())
		a.applySparseCheckout(model)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) sparseModel(r *gitrepo.Repository) sparse.Model {
	cone, err := r.Config().GetBool("core.sparsecheckoutcone")
	if err != nil {
		cone = true
	}
	patterns, err := listSparseCheckout(r)
	if err != nil {
		return sparse.Model{Cone: true}
	}
	return sparse.Model{Enabled: true, Cone: cone, Patterns: patterns}
}

func (a *App) applySparseCheckout(model sparse.Model) {
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		if !model.Enabled {
			return closeSparseCheckout(ctx, r)
		}
		return setSparseCheckout(ctx, r, model.Patterns, ops.SparseCheckoutOptions{Cone: model.Cone})
	}, func(err error) {
		if err != nil {
			a.log.Warn("sparse checkout failed", "error", err)
			a.RefreshRepository()
			a.statusLabel.SetText(i18n.Tf("Status.SparseFailed", err))
			return
		}
		a.reopenAfterSparseCheckout()
		a.statusLabel.SetText(i18n.T(sparseStatusKey(model.Enabled)))
	})
}

func (a *App) reopenAfterSparseCheckout() {
	o := a.opened()
	if o == nil {
		return
	}
	a.ActivateRepository(o.id)
}

func sparseStatusKey(enabled bool) string {
	if enabled {
		return "Status.SparseApplied"
	}
	return "Status.SparseDisabled"
}
