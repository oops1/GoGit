package app

import (
	"context"
	"sync"

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
	app   *App
	view  *operation.View
	after *operationFollowUps
}

type operationFollowUps struct {
	actions []func()
}

func (r OperationReporter) Then(action func()) {
	r.after.actions = append(r.after.actions, action)
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

var phaseNoticeKeys = map[string]string{
	progress.PhaseTLSVerifyDisabled: "Operation.Log.TLSVerifyDisabled",
}

type operationProgressState struct {
	reporter OperationReporter
	mu       sync.Mutex
	noticed  map[string]bool
}

func newOperationProgress(reporter OperationReporter) progress.Func {
	state := &operationProgressState{reporter: reporter}
	return state.report
}

func (s *operationProgressState) notice(phase, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.noticed[phase] {
		return
	}
	if s.noticed == nil {
		s.noticed = map[string]bool{}
	}
	s.noticed[phase] = true
	s.reporter.Log(i18n.T(key))
}

func (s *operationProgressState) report(r progress.Report) {
	if key, ok := phaseNoticeKeys[r.Phase]; ok {
		s.notice(r.Phase, key)
	}
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
	ctx, cancel, ok := a.beginNetOperation()
	if !ok {
		a.reportBusy()
		return
	}
	view, err := newOperationView(a.eng, title)
	if err != nil {
		a.log.Warn("open operation dialog failed", "title", title, "error", err)
		cancel()
		a.releaseNetOperation()
		a.netWG.Done()
		return
	}
	view.OnCancel = cancel
	view.OnClose = func() {
		cancel()
		a.eng.CloseModal(view.Dialog())
	}
	view.Dialog().CancelAction = cancel
	a.showModal(view.Dialog(), view)
	after := &operationFollowUps{}
	reporter := OperationReporter{app: a, view: view, after: after}
	go func() {
		defer a.netWG.Done()
		resume := a.holdWatch()
		err := body(ctx, reporter)
		resume()
		cancel()
		a.releaseNetOperation()
		followUps := after.actions
		a.Post(func() {
			view.Finish(redactError(localizeOperationError(err)))
			for _, followUp := range followUps {
				followUp()
			}
		})
	}()
}

func (a *App) beginNetOperation() (context.Context, context.CancelFunc, bool) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.netMu.Lock()
	defer a.netMu.Unlock()
	if a.writeCancel != nil || a.netCancel != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.netCancel = cancel
	a.netWG.Add(1)
	return ctx, cancel, true
}

func (a *App) releaseNetOperation() {
	a.netMu.Lock()
	a.netCancel = nil
	a.netMu.Unlock()
}

func (a *App) busy() bool {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.netMu.Lock()
	defer a.netMu.Unlock()
	return a.writeCancel != nil || a.netCancel != nil
}

func (a *App) reportBusy() {
	a.Post(func() { a.statusLabel.SetText(i18n.T("Status.Busy")) })
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
