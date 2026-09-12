package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/reflog"
	"github.com/oops1/gogit/internal/ui/reset"
)

func captureReflogViews(t *testing.T) *[]*reflog.View {
	t.Helper()
	views := &[]*reflog.View{}
	prev := newReflogView
	newReflogView = func() (*reflog.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newReflogView = prev })
	return views
}

func waitForReflogView(t *testing.T, a *App, views *[]*reflog.View) *reflog.View {
	t.Helper()
	waitForDialog(t, a, func() int { return len(*views) }, "the reflog dialog")
	return (*views)[len(*views)-1]
}

func TestTheReflogDialogListsWhereTheBranchHasBeen(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureReflogViews(t)

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdReflog) })
	view := waitForReflogView(t, a, views)

	records := readOnDispatcher(t, a, view.Records)
	if len(records) == 0 || records[0].Selector == "" {
		t.Fatalf("records = %+v", records)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheBranchMenuOpensTheReflog(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureReflogViews(t)

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.reflogItems(refs.BranchName("feature")) })
	if len(items) != 1 || items[0].Text != i18n.T("Menu.Context.Reflog") || items[0].Disabled {
		t.Fatalf("items = %+v", items)
	}
	readOnDispatcher(t, a, func() bool { items[0].OnClick(); return true })
	view := waitForReflogView(t, a, views)

	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheReflogIsOfferedForBranchesOnly(t *testing.T) {
	a, _ := forkedApp(t, false)

	if items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.reflogItems(refs.TagName("v1")) }); items != nil {
		t.Fatalf("items = %+v", items)
	}
}

func TestTheReflogDialogOpensTheResetDialog(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureReflogViews(t)
	resetViews := captureResetViews(t)

	readOnDispatcher(t, a, func() bool { a.openReflog("main"); return true })
	view := waitForReflogView(t, a, views)
	records := readOnDispatcher(t, a, view.Records)
	readOnDispatcher(t, a, func() bool { view.OnReset(records[0]); return true })

	if len(*resetViews) != 1 {
		t.Fatal("the reset dialog did not open")
	}
	readOnDispatcher(t, a, func() bool { (*resetViews)[0].Dialog().CancelAction(); return true })
	if readOnDispatcher(t, a, func() reset.Mode { return (*resetViews)[0].Mode() }) != reset.ModeMixed {
		t.Fatal("the reset dialog did not start in the mixed mode")
	}
}

func TestAFailedReflogIsReported(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := readReflog
	readReflog = func(context.Context, *gitrepo.Repository, string, ops.ReflogOptions) ([]ops.ReflogRecord, error) {
		return nil, errors.New("no reflog")
	}
	t.Cleanup(func() { readReflog = prev })

	readOnDispatcher(t, a, func() bool { a.openReflog("main"); return true })

	waitForStatusText(t, a, i18n.Tf("Status.ReflogFailed", errors.New("no reflog")))
}

func TestTheReflogDialogFailureIsReported(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := newReflogView
	newReflogView = func() (*reflog.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newReflogView = prev })

	readOnDispatcher(t, a, func() bool { a.openReflog("main"); return true })

	waitForStatusText(t, a, i18n.Tf("Status.ReflogFailed", errors.New("no dialog")))
}

func TestTheReflogNeedsARepository(t *testing.T) {
	a := newTestApp(t)
	views := captureReflogViews(t)

	a.openReflog("main")
	items := a.reflogItems(refs.BranchName("main"))

	if len(*views) != 0 || !items[0].Disabled {
		t.Fatal("the reflog dialog opened without a repository")
	}
}
