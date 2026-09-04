package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "operation"

const maxLogLines = 2000

var ErrWidgetMissing = errors.New("operation: named widget missing")

var loadDialog = dialogs.Load

type View struct {
	dlg         *widget.Dialog
	statusLabel *widget.Label
	progressBar *widget.ProgressBar
	logList     *widget.ListView
	cancelBtn   *widget.Button
	closeBtn    *widget.Button

	eng      widget.ModalShower
	lines    []string
	running  bool
	finished bool

	OnCancel func()
	OnClose  func()
}

func NewView(eng widget.ModalShower, title string) (*View, error) {
	dlg, named, err := loadDialog(dialogName, title)
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.wire()
	v.applyInitialState()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.statusLabel, ok = named["status"].(*widget.Label); !ok {
		return fmt.Errorf("%w: status", ErrWidgetMissing)
	}
	if v.progressBar, ok = named["progress"].(*widget.ProgressBar); !ok {
		return fmt.Errorf("%w: progress", ErrWidgetMissing)
	}
	if v.logList, ok = named["log"].(*widget.ListView); !ok {
		return fmt.Errorf("%w: log", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) wire() {
	v.cancelBtn.OnClick = v.onCancelClicked
	v.closeBtn.OnClick = v.onCloseClicked
}

func (v *View) applyInitialState() {
	v.running = true
	v.statusLabel.SetText(i18n.T("Operation.Status.Running"))
	v.cancelBtn.SetText(i18n.T("Operation.Cancel"))
	v.closeBtn.SetText(i18n.T("Operation.Close"))
	v.progressBar.SetIndeterminate(true)
	v.cancelBtn.SetEnabled(true)
	v.closeBtn.SetEnabled(false)
}

func (v *View) SetStatus(text string) {
	v.statusLabel.SetText(text)
}

func (v *View) Append(line string) {
	v.lines = append(v.lines, line)
	if len(v.lines) > maxLogLines {
		v.lines = v.lines[len(v.lines)-maxLogLines:]
	}
	v.logList.SetItems(v.lines)
	v.logList.ScrollToBottom()
}

func (v *View) Lines() []string {
	out := make([]string, len(v.lines))
	copy(out, v.lines)
	return out
}

func (v *View) SetIndeterminate(on bool) {
	v.progressBar.SetIndeterminate(on)
}

func (v *View) SetProgress(fraction float64) {
	switch {
	case fraction < 0:
		fraction = 0
	case fraction > 1:
		fraction = 1
	}
	v.progressBar.SetIndeterminate(false)
	v.progressBar.SetValue(fraction)
}

func (v *View) Finish(err error) {
	if v.finished {
		return
	}
	v.finished = true
	v.running = false

	v.progressBar.SetIndeterminate(false)
	v.progressBar.SetValue(1)

	switch {
	case err == nil:
		v.statusLabel.SetText(i18n.T("Operation.Status.Done"))
	case errors.Is(err, context.Canceled):
		v.statusLabel.SetText(i18n.T("Operation.Status.Canceled"))
		v.Append(err.Error())
	default:
		v.statusLabel.SetText(i18n.T("Operation.Status.Failed"))
		v.Append(err.Error())
	}

	v.cancelBtn.SetEnabled(false)
	v.closeBtn.SetEnabled(true)
}

func (v *View) Running() bool {
	return v.running
}

func (v *View) onCancelClicked() {
	v.Append(i18n.T("Operation.Log.Canceling"))
	v.cancelBtn.SetEnabled(false)
	if v.OnCancel != nil {
		v.OnCancel()
	}
}

func (v *View) onCloseClicked() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
