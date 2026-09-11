package compare

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/icons"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	dialogName         = "compare"
	dialogMinWidth     = 720
	dialogMinHeight    = 420
	prevChangeIcon     = "change_prev"
	nextChangeIcon     = "change_next"
	navigationIconSize = 18
)

var ErrWidgetMissing = errors.New("compare: named widget missing")

var loadDialog = dialogs.Load

var codeExtensions = map[string]bool{
	".go": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cs": true,
	".java": true, ".kt": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".py": true, ".rb": true, ".rs": true, ".swift": true, ".php": true, ".sh": true,
	".ps1": true, ".sql": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".xml": true, ".xaml": true, ".html": true, ".css": true, ".md": true,
}

type View struct {
	dlg              *widget.Dialog
	diff             *widget.DiffView
	openLeft         *widget.Button
	openRight        *widget.Button
	save             *widget.Button
	undo             *widget.Button
	redo             *widget.Button
	prevChange       *widget.Button
	nextChange       *widget.Button
	closeBtn         *widget.Button
	hideUnchanged    *widget.CheckBox
	ignoreWhitespace *widget.CheckBox
	changeCount      *widget.Label
	position         *widget.Label
	message          *widget.Label

	OnPick   func(side widget.DiffSide)
	OnSaveAs func(side widget.DiffSide)
	OnClose  func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Compare.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	dlg.SetMinSize(dialogMinWidth, dialogMinHeight)
	dlg.SetResizable(true)
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Diff() *widget.DiffView { return v.diff }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.openLeft, v.openRight, v.save, v.undo, v.redo, v.prevChange, v.nextChange, v.closeBtn)
	v.prevChange.Icon = icons.Toolbar(prevChangeIcon, navigationIconSize, p.Text)
	v.nextChange.Icon = icons.Toolbar(nextChangeIcon, navigationIconSize, p.Text)
	p.Body(v.changeCount, v.position)
	p.Hints(v.message)
}

func (v *View) Load(side widget.DiffSide, path string) error {
	if err := v.diff.LoadFile(side, path); err != nil {
		v.say(i18n.Tf("Dialog.Compare.LoadFailed", path, err))
		return err
	}
	v.diff.SetSyntaxHighlight(v.showsCode())
	v.refresh()
	return nil
}

func (v *View) Modified() bool {
	return v.diff.IsModified(widget.DiffLeft) || v.diff.IsModified(widget.DiffRight)
}

func (v *View) SaveAll() {
	saved := 0
	for _, side := range []widget.DiffSide{widget.DiffLeft, widget.DiffRight} {
		if !v.diff.IsModified(side) {
			continue
		}
		err := v.diff.Save(side)
		switch {
		case errors.Is(err, widget.ErrDiffNoPath):
			if v.OnSaveAs != nil {
				v.OnSaveAs(side)
			}
		case err != nil:
			v.say(i18n.Tf("Dialog.Compare.SaveFailed", err))
			return
		default:
			saved++
		}
	}
	if saved == 0 && !v.Modified() {
		v.say(i18n.T("Dialog.Compare.NothingToSave"))
	}
	v.refresh()
}

func (v *View) SaveAs(side widget.DiffSide, path string) error {
	if err := v.diff.SaveAs(side, path); err != nil {
		v.say(i18n.Tf("Dialog.Compare.SaveFailed", err))
		return err
	}
	v.refresh()
	return nil
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.diff, ok = named["diff"].(*widget.DiffView); !ok {
		return fmt.Errorf("%w: diff", ErrWidgetMissing)
	}
	buttons := []struct {
		name   string
		target **widget.Button
	}{
		{"openLeft", &v.openLeft}, {"openRight", &v.openRight}, {"save", &v.save},
		{"undo", &v.undo}, {"redo", &v.redo}, {"prevChange", &v.prevChange},
		{"nextChange", &v.nextChange}, {"close", &v.closeBtn},
	}
	for _, b := range buttons {
		if *b.target, ok = named[b.name].(*widget.Button); !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, b.name)
		}
	}
	checks := []struct {
		name   string
		target **widget.CheckBox
	}{{"hideUnchanged", &v.hideUnchanged}, {"ignoreWhitespace", &v.ignoreWhitespace}}
	for _, c := range checks {
		if *c.target, ok = named[c.name].(*widget.CheckBox); !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, c.name)
		}
	}
	labels := []struct {
		name   string
		target **widget.Label
	}{{"changeCount", &v.changeCount}, {"position", &v.position}, {"message", &v.message}}
	for _, l := range labels {
		if *l.target, ok = named[l.name].(*widget.Label); !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, l.name)
		}
	}
	return nil
}

func (v *View) wire() {
	v.openLeft.OnClick = func() { v.pick(widget.DiffLeft) }
	v.openRight.OnClick = func() { v.pick(widget.DiffRight) }
	v.save.OnClick = v.SaveAll
	v.undo.OnClick = func() { v.diff.Undo(); v.refresh() }
	v.redo.OnClick = func() { v.diff.Redo(); v.refresh() }
	v.prevChange.OnClick = v.diff.PrevChange
	v.nextChange.OnClick = v.diff.NextChange
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
	v.hideUnchanged.OnChange = v.diff.SetHideUnchanged
	v.ignoreWhitespace.OnChange = v.diff.SetIgnoreWhitespace
	v.diff.OnDiffChanged = func(int) { v.refresh() }
	v.diff.OnModifiedChanged = func(widget.DiffSide, bool) { v.refresh() }
	v.diff.OnTextChanged = func(widget.DiffSide) { v.refresh() }
	v.diff.OnCaretMoved = func(side widget.DiffSide, line, col int) { v.showPosition(side, line, col) }
	v.diff.OnActiveSideChanged = func(side widget.DiffSide) {
		line, col := v.diff.Caret(side)
		v.showPosition(side, line, col)
	}
	v.diff.OnSaveRequest = func(widget.DiffSide) { v.SaveAll() }
	v.diff.OnFileSaved = func(_ widget.DiffSide, path string) { v.say(i18n.Tf("Dialog.Compare.Saved", path)) }
	v.diff.OnFileChangedOnDisk = func(_ widget.DiffSide, path string, deleted bool) {
		if deleted {
			v.say(i18n.Tf("Dialog.Compare.DeletedOnDisk", path))
			return
		}
		v.say(i18n.Tf("Dialog.Compare.ChangedOnDisk", path))
	}
	v.diff.OnError = func(_ widget.DiffSide, err error) { v.say(err.Error()) }
}

func (v *View) pick(side widget.DiffSide) {
	if v.OnPick != nil {
		v.OnPick(side)
	}
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}

func (v *View) refresh() {
	count := v.diff.ChangeCount()
	switch {
	case v.diff.FilePath(widget.DiffLeft) == "" && v.diff.FilePath(widget.DiffRight) == "":
		v.changeCount.SetText("")
	case count == 0:
		v.changeCount.SetText(i18n.T("Dialog.Compare.Identical"))
	default:
		v.changeCount.SetText(i18n.Tf("Dialog.Compare.Changes", count))
	}
	v.prevChange.SetEnabled(count > 0)
	v.nextChange.SetEnabled(count > 0)
	v.undo.SetEnabled(v.diff.CanUndo())
	v.redo.SetEnabled(v.diff.CanRedo())
	v.save.SetEnabled(v.Modified())
}

func (v *View) showPosition(side widget.DiffSide, line, col int) {
	key := "Dialog.Compare.Side.Left"
	if side == widget.DiffRight {
		key = "Dialog.Compare.Side.Right"
	}
	v.position.SetText(i18n.Tf("Dialog.Compare.Position", i18n.T(key), line+1, col+1))
}

func (v *View) say(text string) {
	v.message.SetText(text)
}

func (v *View) showsCode() bool {
	for _, side := range []widget.DiffSide{widget.DiffLeft, widget.DiffRight} {
		if codeExtensions[strings.ToLower(filepath.Ext(v.diff.FilePath(side)))] {
			return true
		}
	}
	return false
}
