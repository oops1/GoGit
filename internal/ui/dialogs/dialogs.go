package dialogs

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/assets"
)

var source = assets.Dialog

func Load(name, title string) (*widget.Dialog, map[string]widget.Widget, error) {
	data, err := source(name)
	if err != nil {
		return nil, nil, err
	}
	root, named, err := widget.LoadUIFromXAML(data)
	if err != nil {
		return nil, nil, err
	}
	dlg := sizedDialog(title, root.Bounds())
	dlg.AddChild(root)
	root.SetBounds(dlg.ContentBounds())
	return dlg, named, nil
}

func sizedDialog(title string, content image.Rectangle) *widget.Dialog {
	probe := widget.NewDialog("", content.Dx(), content.Dy())
	inner := probe.ContentBounds()
	padX := content.Dx() - inner.Dx()
	padY := content.Dy() - inner.Dy()
	return widget.NewDialog(title, content.Dx()+padX, content.Dy()+padY)
}
