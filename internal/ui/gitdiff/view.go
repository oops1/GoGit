package gitdiff

import (
	"github.com/oops1/headless-gui/v3/widget"
)

const XAMLTag = "GitDiffView"

type Spot struct {
	Side widget.DiffSide
	From int
	To   int
}

type Side struct {
	Title string
	Text  string
}

type View struct {
	*widget.DiffView

	OnMenu func(Spot) []widget.MenuItem
}

func New() *View {
	d := widget.NewDiffView("", "")
	d.SetReadOnly(widget.DiffLeft, true)
	d.SetReadOnly(widget.DiffRight, true)
	d.SetHideUnchanged(true)
	d.SetIgnoreWhitespace(true)
	d.SetShowHeaders(false)
	d.SetShowReadOnlyMark(false)
	return &View{DiffView: d}
}

func Register() {
	widget.RegisterXAMLWidget(XAMLTag, buildFromXAML)
}

func buildFromXAML(widget.XAMLAttrs) (widget.Widget, error) {
	return New(), nil
}

func (v *View) Show(left, right Side) {
	v.SetText(widget.DiffLeft, left.Title, "", left.Text)
	v.SetText(widget.DiffRight, right.Title, "", right.Text)
}

func (v *View) Clear() {
	v.Show(Side{}, Side{})
}

func (v *View) ContextMenuAt(x, y int) *widget.PopupMenu {
	menu := v.DiffView.ContextMenuAt(x, y)
	if menu == nil {
		return nil
	}
	items := usable(menu.Items())
	if v.OnMenu != nil {
		if extra := v.OnMenu(v.spot()); len(extra) > 0 {
			items = append(items, widget.MenuItem{Separator: true})
			items = append(items, extra...)
		}
	}
	menu.SetItems(items)
	return menu
}

func (v *View) spot() Spot {
	side := v.ActiveSide()
	if from, to, ok := v.Selection(side); ok {
		return Spot{Side: side, From: from, To: to}
	}
	line, _ := v.Caret(side)
	return Spot{Side: side, From: line, To: line + 1}
}

func usable(items []widget.MenuItem) []widget.MenuItem {
	out := make([]widget.MenuItem, 0, len(items))
	for _, item := range items {
		if item.Disabled {
			continue
		}
		if item.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, item)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}
