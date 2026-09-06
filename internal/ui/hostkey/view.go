package hostkey

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "hostkey"

var ErrWidgetMissing = errors.New("hostkey: named widget missing")

var loadDialog = dialogs.Load

type Request struct {
	Host        string
	Algorithm   string
	Fingerprint string
	Changed     bool
}

type View struct {
	dlg             *widget.Dialog
	hostText        *widget.Label
	algorithmText   *widget.Label
	fingerprintText *widget.Label
	warningLabel    *widget.Label
	acceptBtn       *widget.Button
	rejectBtn       *widget.Button

	eng widget.ModalShower

	OnAccept func()
	OnReject func()
}

func NewView(eng widget.ModalShower, req Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.HostKey.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.applyRequest(req)
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.hostText, ok = named["host"].(*widget.Label); !ok {
		return fmt.Errorf("%w: host", ErrWidgetMissing)
	}
	if v.algorithmText, ok = named["algorithm"].(*widget.Label); !ok {
		return fmt.Errorf("%w: algorithm", ErrWidgetMissing)
	}
	if v.fingerprintText, ok = named["fingerprint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: fingerprint", ErrWidgetMissing)
	}
	if v.warningLabel, ok = named["warning"].(*widget.Label); !ok {
		return fmt.Errorf("%w: warning", ErrWidgetMissing)
	}
	if v.acceptBtn, ok = named["accept"].(*widget.Button); !ok {
		return fmt.Errorf("%w: accept", ErrWidgetMissing)
	}
	if v.rejectBtn, ok = named["reject"].(*widget.Button); !ok {
		return fmt.Errorf("%w: reject", ErrWidgetMissing)
	}
	return nil
}

func (v *View) applyRequest(req Request) {
	v.hostText.SetText(req.Host)
	v.algorithmText.SetText(req.Algorithm)
	v.fingerprintText.SetText(req.Fingerprint)
	v.warningLabel.SetText(i18n.T("Dialog.HostKey.Changed"))
	v.warningLabel.SetVisible(req.Changed)
	v.acceptBtn.SetEnabled(!req.Changed)
}

func (v *View) wire() {
	v.acceptBtn.OnClick = v.accept
	v.rejectBtn.OnClick = v.reject
	v.dlg.DefaultAction = v.accept
	v.dlg.CancelAction = v.reject
}

func (v *View) accept() {
	if !v.acceptBtn.IsEnabled() {
		return
	}
	if v.OnAccept != nil {
		v.OnAccept()
	}
}

func (v *View) reject() {
	if v.OnReject != nil {
		v.OnReject()
	}
}
