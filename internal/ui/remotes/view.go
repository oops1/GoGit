package remotes

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "remotes"

var ErrWidgetMissing = errors.New("remotes: named widget missing")

var loadDialog = dialogs.Load

type Entry struct {
	Name     string
	FetchURL string
	PushURL  string
}

type View struct {
	dlg        *widget.Dialog
	table      *widget.DataGridWidget
	nameInput  *widget.TextInput
	urlInput   *widget.TextInput
	addBtn     *widget.Button
	editBtn    *widget.Button
	removeBtn  *widget.Button
	closeBtn   *widget.Button
	errorLabel *widget.Label

	eng          widget.ModalShower
	entries      []Entry
	selectedName string
	hasSelection bool

	OnAdd    func(name, url string)
	OnEdit   func(name, url string)
	OnRemove func(name string)
	OnClose  func()
}

func NewView(eng widget.ModalShower, entries []Entry) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Remotes.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.buildColumns()
	v.wire()
	v.SetEntries(entries)
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.nameInput, v.urlInput)
	p.Quiet(v.addBtn, v.editBtn, v.removeBtn, v.closeBtn)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.table, ok = named["table"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: table", ErrWidgetMissing)
	}
	if v.nameInput, ok = named["name"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: name", ErrWidgetMissing)
	}
	if v.urlInput, ok = named["url"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: url", ErrWidgetMissing)
	}
	if v.addBtn, ok = named["add"].(*widget.Button); !ok {
		return fmt.Errorf("%w: add", ErrWidgetMissing)
	}
	if v.editBtn, ok = named["edit"].(*widget.Button); !ok {
		return fmt.Errorf("%w: edit", ErrWidgetMissing)
	}
	if v.removeBtn, ok = named["remove"].(*widget.Button); !ok {
		return fmt.Errorf("%w: remove", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	if v.errorLabel, ok = named["error"].(*widget.Label); !ok {
		return fmt.Errorf("%w: error", ErrWidgetMissing)
	}
	return nil
}

const (
	columnHeaderName     = "Dialog.Remotes.Name"
	columnHeaderFetchURL = "Dialog.Remotes.URL"
	columnHeaderPushURL  = "Dialog.Remotes.PushURL"
)

func (v *View) buildColumns() {
	nameCol := datagrid.NewTextColumn(i18n.T(columnHeaderName), "Name")
	nameCol.SetWidth(datagrid.PixelWidth(140))
	fetchCol := datagrid.NewTextColumn(i18n.T(columnHeaderFetchURL), "FetchURL")
	fetchCol.SetWidth(datagrid.StarWidth(1))
	pushCol := datagrid.NewTextColumn(i18n.T(columnHeaderPushURL), "PushURL")
	pushCol.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{nameCol, fetchCol, pushCol})
}

func (v *View) wire() {
	v.table.Grid.OnSelectionChanged = v.onSelectionChanged
	v.addBtn.OnClick = v.onAddClicked
	v.editBtn.OnClick = v.onEditClicked
	v.removeBtn.OnClick = v.onRemoveClicked
	v.closeBtn.OnClick = v.onCloseClicked
	v.dlg.CancelAction = v.onCloseClicked
	v.updateSelection("", false)
}

func (v *View) SetEntries(entries []Entry) {
	v.entries = append([]Entry(nil), entries...)
	items := make([]interface{}, len(v.entries))
	for i, e := range v.entries {
		items[i] = e
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.updateSelection("", false)
}

func (v *View) SetError(text string) {
	v.errorLabel.SetText(text)
}

func (v *View) Error() string {
	return v.errorLabel.Text()
}

func (v *View) onSelectionChanged(ev datagrid.SelectionChangedEvent) {
	entry, ok := ev.SelectedItem.(Entry)
	if !ok {
		v.updateSelection("", false)
		return
	}
	v.nameInput.SetText(entry.Name)
	v.urlInput.SetText(entry.FetchURL)
	v.updateSelection(entry.Name, true)
}

func (v *View) updateSelection(name string, has bool) {
	v.selectedName = name
	v.hasSelection = has
	v.editBtn.SetEnabled(has)
	v.removeBtn.SetEnabled(has)
}

func (v *View) onAddClicked() {
	name := strings.TrimSpace(v.nameInput.GetText())
	url := strings.TrimSpace(v.urlInput.GetText())
	if !v.validate(name, url, "") {
		return
	}
	v.SetError("")
	if v.OnAdd != nil {
		v.OnAdd(name, url)
	}
}

func (v *View) onEditClicked() {
	if !v.hasSelection {
		return
	}
	name := strings.TrimSpace(v.nameInput.GetText())
	url := strings.TrimSpace(v.urlInput.GetText())
	if !v.validate(name, url, v.selectedName) {
		return
	}
	v.SetError("")
	if v.OnEdit != nil {
		v.OnEdit(name, url)
	}
}

func (v *View) validate(name, url, ignoreName string) bool {
	if name == "" {
		v.SetError(i18n.T("Dialog.Remotes.Error.Name"))
		return false
	}
	if url == "" {
		v.SetError(i18n.T("Dialog.Remotes.Error.URL"))
		return false
	}
	for _, e := range v.entries {
		if e.Name == name && e.Name != ignoreName {
			v.SetError(i18n.T("Dialog.Remotes.Error.Duplicate"))
			return false
		}
	}
	return true
}

func (v *View) onRemoveClicked() {
	if !v.hasSelection {
		return
	}
	name := v.selectedName
	if v.OnRemove != nil {
		v.OnRemove(name)
	}
}

func (v *View) onCloseClicked() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
