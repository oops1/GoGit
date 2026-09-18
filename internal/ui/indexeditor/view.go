package indexeditor

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/diffview"
	"github.com/oops1/headless-gui/v3/widget/mergeview"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/icons"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	dialogName         = "index_editor"
	dialogMinWidth     = 900
	dialogMinHeight    = 640
	prevChangeIcon     = "change_prev"
	nextChangeIcon     = "change_next"
	navigationIconSize = 18
	windowMargin       = 48
)

var ErrWidgetMissing = errors.New("indexeditor: named widget missing")

var loadDialog = dialogs.LoadResizable

type File struct {
	Path      string
	HeadLabel string
	Head      []byte
	Index     []byte
	Working   []byte
}

type View struct {
	dlg         *widget.Dialog
	merge       *widget.MergeView
	fromHead    *widget.Button
	keepIndex   *widget.Button
	fromWorking *widget.Button
	prevChange  *widget.Button
	nextChange  *widget.Button
	reset       *widget.Button
	save        *widget.Button
	closeBtn    *widget.Button
	changes     *widget.Label
	position    *widget.Label
	message     *widget.Label

	file   File
	saved  string
	padded bool

	OnSave  func(content string)
	OnClose func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.IndexEditor.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	dlg.SetMinSize(dialogMinWidth, dialogMinHeight)
	dlg.SetResizable(true)
	dlg.SetWindowButtons(true)
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Merge() *widget.MergeView { return v.merge }

func (v *View) FitWithin(width, height int) {
	size := v.dlg.Bounds()
	v.dlg.Resize(
		max(dialogMinWidth, min(size.Dx(), width-windowMargin)),
		max(dialogMinHeight, min(size.Dy(), height-windowMargin)),
	)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.merge, ok = named["merge"].(*widget.MergeView); !ok {
		return fmt.Errorf("%w: merge", ErrWidgetMissing)
	}
	for name, target := range map[string]**widget.Button{
		"fromHead":    &v.fromHead,
		"keepIndex":   &v.keepIndex,
		"fromWorking": &v.fromWorking,
		"prevChange":  &v.prevChange,
		"nextChange":  &v.nextChange,
		"reset":       &v.reset,
		"save":        &v.save,
		"close":       &v.closeBtn,
	} {
		button, ok := named[name].(*widget.Button)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = button
	}
	for name, target := range map[string]**widget.Label{
		"changes":  &v.changes,
		"position": &v.position,
		"message":  &v.message,
	} {
		label, ok := named[name].(*widget.Label)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = label
	}
	return nil
}

func (v *View) wire() {
	v.fromHead.OnClick = func() { v.take(widget.MergeTakeOurs) }
	v.keepIndex.OnClick = func() { v.take(widget.MergeTakeBase) }
	v.fromWorking.OnClick = func() { v.take(widget.MergeTakeTheirs) }
	v.prevChange.OnClick = func() { v.merge.PrevConflict(); v.refresh() }
	v.nextChange.OnClick = func() { v.merge.NextConflict(); v.refresh() }
	v.reset.OnClick = v.restore
	v.save.OnClick = v.requestSave
	v.closeBtn.OnClick = func() {
		if v.OnClose != nil {
			v.OnClose()
		}
	}
	v.merge.OnResolvedChanged = func(int) { v.refresh() }
	v.merge.OnResultEdited = v.refresh
	v.merge.OnCurrentConflict = func(int) { v.refresh() }
	v.merge.OnCaretMoved = func(int, int) { v.refresh() }
	v.merge.OnSaveRequest = v.requestSave
}

func (v *View) take(how widget.MergeResolution) {
	v.merge.ResolveCurrent(how)
	v.refresh()
}

func (v *View) Show(file File) {
	v.file = file
	v.dlg.Title = i18n.Tf("Dialog.IndexEditor.TitleFor", file.Path)
	v.apply()
	v.saved = v.Result()
	v.say("")
}

func (v *View) restore() {
	v.apply()
	v.say(i18n.T("Dialog.IndexEditor.Restored"))
}

func (v *View) apply() {
	v.merge.SetSides(
		widget.MergeSideInfo{Title: v.file.HeadLabel, Note: v.file.Path, Hint: i18n.T("Dialog.IndexEditor.Note.Head")},
		widget.MergeSideInfo{Title: i18n.T("Dialog.IndexEditor.Side.Index"), Note: v.file.Path, Hint: i18n.T("Dialog.IndexEditor.Note.Index")},
		widget.MergeSideInfo{Title: i18n.T("Dialog.IndexEditor.Side.Working"), Note: v.file.Path, Hint: i18n.T("Dialog.IndexEditor.Note.Working")},
	)
	v.merge.SetResultInfo(widget.MergeSideInfo{
		Title: i18n.T("Dialog.IndexEditor.Side.Result"),
		Note:  v.file.Path,
		Hint:  i18n.T("Dialog.IndexEditor.Note.Result"),
	})
	written := writtenText(v.file)
	v.padded = len(v.file.Index) == 0
	v.merge.SetChunks(blocksOf(v.file))
	v.merge.SetResultEOL(written.EOL, written.BOM, written.FinalNL)
	v.merge.ResolveAll(widget.MergeTakeBase)
	v.merge.GoToConflict(0)
	v.refresh()
}

func writtenText(file File) diffview.Text {
	for _, side := range [][]byte{file.Index, file.Working, file.Head} {
		if len(side) > 0 {
			return diffview.Decode(side)
		}
	}
	return diffview.Decode(nil)
}

func linesOf(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return diffview.Decode(data).Lines
}

func blocksOf(file File) []widget.MergeChunk {
	blocks := mergeview.Merge(linesOf(file.Index), linesOf(file.Head), linesOf(file.Working), false)
	for at := range blocks {
		block := &blocks[at]
		if slices.Equal(block.Ours, block.Base) && slices.Equal(block.Theirs, block.Base) {
			block.Merged = block.Base
			continue
		}
		block.Conflict, block.Merged = true, nil
	}
	return blocks
}

func (v *View) Path() string { return v.file.Path }

func (v *View) Changes() int { return v.merge.ConflictCount() }

func (v *View) Result() string {
	if !v.padded {
		return v.merge.Result()
	}
	lines := v.merge.ResultLines()
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	eol, bom, finalNewline := v.merge.ResultEOL()
	return string(diffview.Text{Lines: lines, EOL: eol, BOM: bom, FinalNL: finalNewline}.Encode())
}

func (v *View) Modified() bool { return v.Result() != v.saved }

func (v *View) requestSave() {
	if v.OnSave == nil {
		return
	}
	v.OnSave(v.Result())
}

func (v *View) Saved(content string) {
	v.saved = content
	v.file.Index = []byte(content)
	v.apply()
	v.say(i18n.T("Dialog.IndexEditor.Saved"))
}

func (v *View) SaveFailed(err error) {
	v.say(i18n.Tf("Dialog.IndexEditor.SaveFailed", err))
}

func (v *View) Message() string { return v.message.Text() }

func (v *View) say(text string) {
	v.message.SetText(text)
}

func (v *View) refresh() {
	total := v.merge.ConflictCount()
	v.changes.SetText(i18n.Tf("Dialog.IndexEditor.Changes", total))
	line, col := v.merge.Caret()
	v.position.SetText(i18n.Tf("Dialog.IndexEditor.Position", line+1, col+1))
	for _, button := range []*widget.Button{v.fromHead, v.keepIndex, v.fromWorking, v.prevChange, v.nextChange} {
		button.SetEnabled(total > 0)
	}
}

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.fromHead, v.keepIndex, v.fromWorking, v.prevChange, v.nextChange, v.reset, v.save, v.closeBtn)
	v.prevChange.Icon = icons.Toolbar(prevChangeIcon, navigationIconSize, p.Text)
	v.nextChange.Icon = icons.Toolbar(nextChangeIcon, navigationIconSize, p.Text)
	p.Body(v.changes, v.position)
	p.Hints(v.message)
}
