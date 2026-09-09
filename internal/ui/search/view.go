package search

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "search"

var ErrWidgetMissing = errors.New("search: named widget missing")

var loadDialog = dialogs.Load

type Found struct {
	Path     string
	Bare     bool
	Worktree bool
}

type View struct {
	dlg          *widget.Dialog
	rootInput    *widget.TextInput
	browseBtn    *widget.Button
	includeBare  *widget.CheckBox
	scanBtn      *widget.Button
	statusLabel  *widget.Label
	resultsTable *widget.DataGridWidget
	cancelBtn    *widget.Button
	addBtn       *widget.Button

	eng   widget.ModalShower
	found []Found

	OnScan   func(root string, includeBare bool)
	OnAdd    func(paths []string)
	OnCancel func()
	OnBrowse func()
}

func NewView(eng widget.ModalShower) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Search.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.buildColumns()
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.rootInput)
	p.Quiet(v.browseBtn, v.scanBtn, v.cancelBtn)
	p.Primary(v.addBtn)
	p.Hints(v.statusLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.rootInput, ok = named["root"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: root", ErrWidgetMissing)
	}
	if v.browseBtn, ok = named["browse"].(*widget.Button); !ok {
		return fmt.Errorf("%w: browse", ErrWidgetMissing)
	}
	if v.includeBare, ok = named["includeBare"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: includeBare", ErrWidgetMissing)
	}
	if v.scanBtn, ok = named["scan"].(*widget.Button); !ok {
		return fmt.Errorf("%w: scan", ErrWidgetMissing)
	}
	if v.statusLabel, ok = named["status"].(*widget.Label); !ok {
		return fmt.Errorf("%w: status", ErrWidgetMissing)
	}
	if v.resultsTable, ok = named["results"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: results", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	if v.addBtn, ok = named["add"].(*widget.Button); !ok {
		return fmt.Errorf("%w: add", ErrWidgetMissing)
	}
	return nil
}

func (v *View) wire() {
	v.browseBtn.OnClick = v.browse
	v.scanBtn.OnClick = v.scan
	v.cancelBtn.OnClick = v.cancel
	v.addBtn.OnClick = v.add
	v.dlg.DefaultAction = v.add
	v.dlg.CancelAction = v.cancel
}

func (v *View) browse() {
	if v.OnBrowse != nil {
		v.OnBrowse()
	}
}

func (v *View) scan() {
	if v.OnScan != nil {
		v.OnScan(v.Root(), v.includeBare.IsChecked())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}

func (v *View) add() {
	if v.OnAdd != nil {
		v.OnAdd(v.selectedOrAllPaths())
	}
}

func (v *View) selectedOrAllPaths() []string {
	selected := v.resultsTable.Grid.SelectedItems()
	if len(selected) == 0 {
		return v.allPaths()
	}
	paths := make([]string, 0, len(selected))
	for _, item := range selected {
		if f, ok := item.(Found); ok {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

func (v *View) allPaths() []string {
	paths := make([]string, 0, len(v.found))
	for _, f := range v.found {
		paths = append(paths, f.Path)
	}
	return paths
}

func (v *View) SetRoot(path string) { v.rootInput.SetText(path) }

func (v *View) Root() string { return v.rootInput.GetText() }

func (v *View) SetResults(found []Found) {
	v.found = append([]Found(nil), found...)
	items := make([]interface{}, len(v.found))
	for i, f := range v.found {
		items[i] = f
	}
	v.resultsTable.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
}

func (v *View) SetStatus(text string, color color.RGBA) {
	v.statusLabel.SetText(text)
	v.statusLabel.TextColor = color
}

func (v *View) SetScanning(scanning bool) {
	v.scanBtn.SetEnabled(!scanning)
	v.addBtn.SetEnabled(!scanning)
}
