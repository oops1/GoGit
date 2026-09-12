package app

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/operation"
)

var newOperationView = operation.NewView

type phaseKeys struct {
	starting string
	sofar    string
	outOf    string
}

var phaseLogKeys = map[string]phaseKeys{
	"init":                  {starting: "Operation.Log.Initializing"},
	"connecting":            {starting: "Operation.Log.Connecting"},
	"negotiating":           {starting: "Operation.Log.Negotiating"},
	progress.PhaseReceiving: {starting: "Operation.Log.Receiving"},
	progress.PhaseCounting: {
		starting: "Operation.Log.Counting",
		sofar:    "Operation.Log.CountingObjects",
	},
	progress.PhaseCompressing: {
		starting: "Operation.Log.Compressing",
		outOf:    "Operation.Log.CompressingObjects",
	},
	progress.PhaseWriting: {
		starting: "Operation.Log.Writing",
		outOf:    "Operation.Log.WritingObjects",
	},
	progress.PhaseResolving: {
		starting: "Operation.Log.Resolving",
		outOf:    "Operation.Log.ResolvingDeltas",
	},
	"updating-refs":        {starting: "Operation.Log.Updating"},
	progress.PhaseCheckout: {starting: "Operation.Log.Checkout"},
}

type OperationReporter struct {
	app  *App
	view *operation.View
}

func (r OperationReporter) Log(line string) {
	r.app.Post(func() { r.view.Append(line) })
}

func (r OperationReporter) Track(slot, line string) {
	r.app.Post(func() { r.view.Track(slot, line) })
}

func (r OperationReporter) Start(slot, line string) {
	r.app.Post(func() {
		r.view.ForgetTracked()
		r.view.Track(slot, line)
	})
}

func (r OperationReporter) Status(text string) {
	r.app.Post(func() { r.view.SetStatus(text) })
}

func (r OperationReporter) Progress(fraction float64) {
	r.app.Post(func() { r.view.SetProgress(fraction) })
}

type operationProgressState struct {
	reporter OperationReporter
}

func newOperationProgress(reporter OperationReporter) progress.Func {
	state := &operationProgressState{reporter: reporter}
	return state.report
}

func (s *operationProgressState) report(r progress.Report) {
	if keys, ok := phaseLogKeys[r.Phase]; ok {
		s.reportPhase(r, keys)
	}
	if r.Message != "" {
		s.reporter.Log(r.Message)
	}
	if r.Total > 0 {
		s.reporter.Progress(float64(r.Current) / float64(r.Total))
	}
}

func (s *operationProgressState) reportPhase(r progress.Report, keys phaseKeys) {
	switch {
	case r.Total > 0 && keys.outOf != "":
		s.reporter.Track(r.Phase, i18n.Tf(keys.outOf, r.Current, r.Total))
	case r.Total == 0 && r.Current > 0 && keys.sofar != "":
		s.reporter.Track(r.Phase, i18n.Tf(keys.sofar, r.Current))
	case r.Total == 0 && r.Current == 0:
		s.reporter.Start(r.Phase, i18n.T(keys.starting))
	default:
		s.reporter.Track(r.Phase, i18n.T(keys.starting))
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
	a.showModal(view.Dialog(), view)
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
