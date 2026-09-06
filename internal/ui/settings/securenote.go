package settings

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

type secureNote struct {
	*widget.Label

	icon *widget.ImageWidget
}

func newSecureNote(icon *widget.ImageWidget) *secureNote {
	n := &secureNote{
		Label: widget.NewLabel(i18n.T("Dialog.Settings.Secrets.SecureStorage"), widget.CurrentTheme().LabelText),
		icon:  icon,
	}
	n.FontSize = secureNoteFontSize
	n.icon.Stretch = widget.ImageStretchUniform
	n.ApplyTheme(widget.CurrentTheme())
	return n
}

func (n *secureNote) ApplyTheme(t *widget.Theme) {
	tint := StatusSaved.DotColor(t)
	n.TextColor = tint
	n.icon.SetImage(buildLockIcon(tint))
}
