package app

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/operation"
)

var ErrRemoteUnavailable = errors.New("app: remote operations are not implemented")

var newOperationView = operation.NewView

type OperationReporter struct {
	app  *App
	view *operation.View
}

func (r OperationReporter) Log(line string) {
	r.app.Post(func() { r.view.Append(line) })
}

func (r OperationReporter) Status(text string) {
	r.app.Post(func() { r.view.SetStatus(text) })
}

func (r OperationReporter) Progress(fraction float64) {
	r.app.Post(func() { r.view.SetProgress(fraction) })
}

func (a *App) RunOperation(title string, body func(context.Context, OperationReporter) error) {
	view, err := newOperationView(a.eng, title)
	if err != nil {
		a.log.Warn("open operation dialog failed", "title", title, "error", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	view.OnCancel = cancel
	view.OnClose = func() {
		cancel()
		a.eng.CloseModal(view.Dialog())
	}
	a.eng.ShowModal(view.Dialog())
	reporter := OperationReporter{app: a, view: view}
	go func() {
		defer cancel()
		err := body(ctx, reporter)
		a.Post(func() { view.Finish(err) })
	}()
}

func (a *App) registerRemoteHandlers() {
	a.handlers[CmdPull] = func() { a.runRemoteOperation("Operation.Title.Pull") }
	a.handlers[CmdSync] = func() { a.runRemoteOperation("Operation.Title.Sync") }
	a.handlers[CmdPush] = func() { a.runRemoteOperation("Operation.Title.Push") }
}

func (a *App) runRemoteOperation(titleKey string) {
	a.RunOperation(i18n.T(titleKey), func(_ context.Context, reporter OperationReporter) error {
		reporter.Log(i18n.T("Operation.Log.RemoteUnavailable"))
		return ErrRemoteUnavailable
	})
}
