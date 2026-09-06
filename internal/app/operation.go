package app

import (
	"context"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/operation"
)

var newOperationView = operation.NewView

var phaseLogKeys = map[string]string{
	"connecting":    "Operation.Log.Connecting",
	"negotiating":   "Operation.Log.Negotiating",
	"receiving":     "Operation.Log.Receiving",
	"resolving":     "Operation.Log.Resolving",
	"updating-refs": "Operation.Log.Updating",
	"checkout":      "Operation.Log.Checkout",
}

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

type operationProgressState struct {
	mu        sync.Mutex
	lastPhase string
	reporter  OperationReporter
}

func newOperationProgress(reporter OperationReporter) progress.Func {
	state := &operationProgressState{reporter: reporter}
	return state.report
}

func (s *operationProgressState) report(r progress.Report) {
	s.mu.Lock()
	changed := r.Phase != "" && r.Phase != s.lastPhase
	if changed {
		s.lastPhase = r.Phase
	}
	s.mu.Unlock()
	if changed {
		if key, ok := phaseLogKeys[r.Phase]; ok {
			s.reporter.Log(i18n.T(key))
		}
	}
	if r.Message != "" {
		s.reporter.Log(r.Message)
	}
	if r.Total > 0 {
		s.reporter.Progress(float64(r.Current) / float64(r.Total))
	}
}

func (a *App) RunOperation(title string, body func(context.Context, OperationReporter) error) {
	view, err := newOperationView(a.eng, title)
	if err != nil {
		a.log.Warn("open operation dialog failed", "title", title, "error", err)
		return
	}
	ctx, cancel := a.beginNetOperation()
	view.OnCancel = cancel
	view.OnClose = func() {
		cancel()
		a.eng.CloseModal(view.Dialog())
	}
	view.Dialog().CancelAction = cancel
	a.eng.ShowModal(view.Dialog())
	reporter := OperationReporter{app: a, view: view}
	go func() {
		defer func() {
			cancel()
			a.endNetOperation()
		}()
		err := body(ctx, reporter)
		a.Post(func() { view.Finish(err) })
	}()
}

func (a *App) beginNetOperation() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	a.netMu.Lock()
	a.netCancel = cancel
	a.netMu.Unlock()
	a.netWG.Add(1)
	return ctx, cancel
}

func (a *App) endNetOperation() {
	a.netMu.Lock()
	a.netCancel = nil
	a.netMu.Unlock()
	a.netWG.Done()
}

func (a *App) cancelNetOperation() {
	a.netMu.Lock()
	cancel := a.netCancel
	a.netMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) stopNetOperations() {
	a.cancelNetOperation()
	a.netWG.Wait()
}

func (a *App) registerRemoteHandlers() {
	a.handlers[CmdClone] = a.openClone
	a.handlers[CmdFetch] = a.startFetch
	a.handlers[CmdPull] = a.startPull
	a.handlers[CmdPush] = a.startPush
	a.handlers[CmdSync] = a.startSync
	a.handlers[CmdPrune] = a.startPrune
	a.handlers[CmdManageRemotes] = a.openManageRemotes
}
