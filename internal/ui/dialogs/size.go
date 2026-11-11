package dialogs

import "github.com/oops1/headless-gui/v4/widget"

func RestoreSize(dlg *widget.Dialog, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	dlg.Resize(width, height)
}

func RememberedSize(dlg *widget.Dialog) (width, height int, ok bool) {
	if dlg.IsMaximized() {
		return 0, 0, false
	}
	b := dlg.Bounds()
	return b.Dx(), b.Dy(), true
}
