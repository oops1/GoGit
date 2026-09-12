package conflict

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/icons"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	dialogName         = "conflict"
	dialogMinWidth     = 900
	dialogMinHeight    = 640
	prevChangeIcon     = "change_prev"
	nextChangeIcon     = "change_next"
	navigationIconSize = 18
	windowMargin       = 48
)

var ErrWidgetMissing = errors.New("conflict: named widget missing")

var loadDialog = dialogs.Load

type File struct {
	Path         string
	OursLabel    string
	BaseLabel    string
	TheirsLabel  string
	Blocks       []merge.Chunk
	Style        merge.Style
	FinalNewline bool
}

type View struct {
	dlg          *widget.Dialog
	merge        *widget.MergeView
	takeOurs     *widget.Button
	takeTheirs   *widget.Button
	takeBoth     *widget.Button
	undo         *widget.Button
	prevConflict *widget.Button
	nextConflict *widget.Button
	save         *widget.Button
	closeBtn     *widget.Button
	showBase     *widget.CheckBox
	unresolved   *widget.Label
	position     *widget.Label
	message      *widget.Label

	path         string
	finalNewline bool
	saved        string

	OnSave  func(content string, resolved bool)
	OnClose func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Conflict.Title"))
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
		"takeOurs":     &v.takeOurs,
		"takeTheirs":   &v.takeTheirs,
		"takeBoth":     &v.takeBoth,
		"undo":         &v.undo,
		"prevConflict": &v.prevConflict,
		"nextConflict": &v.nextConflict,
		"save":         &v.save,
		"close":        &v.closeBtn,
	} {
		button, ok := named[name].(*widget.Button)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = button
	}
	if v.showBase, ok = named["showBase"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: showBase", ErrWidgetMissing)
	}
	for name, target := range map[string]**widget.Label{
		"unresolved": &v.unresolved,
		"position":   &v.position,
		"message":    &v.message,
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
	v.takeOurs.OnClick = func() { v.resolve(widget.MergeTakeOurs) }
	v.takeTheirs.OnClick = func() { v.resolve(widget.MergeTakeTheirs) }
	v.takeBoth.OnClick = func() { v.resolve(widget.MergeTakeOursThenTheirs) }
	v.undo.OnClick = func() { v.merge.Undo(); v.refresh() }
	v.prevConflict.OnClick = func() { v.merge.PrevConflict(); v.refresh() }
	v.nextConflict.OnClick = func() { v.merge.NextConflict(); v.refresh() }
	v.save.OnClick = v.requestSave
	v.closeBtn.OnClick = func() {
		if v.OnClose != nil {
			v.OnClose()
		}
	}
	v.showBase.SetChecked(true)
	v.showBase.OnChange = v.merge.SetShowBase
	v.merge.OnResolvedChanged = func(int) { v.refresh() }
	v.merge.OnResultEdited = v.refresh
	v.merge.OnCurrentConflict = func(int) { v.refresh() }
	v.merge.OnCaretMoved = func(int, int) { v.refresh() }
	v.merge.OnSaveRequest = v.requestSave
}

func (v *View) resolve(how widget.MergeResolution) {
	v.merge.ResolveCurrent(how)
	v.refresh()
}

func (v *View) Show(file File) {
	v.path = file.Path
	v.finalNewline = file.FinalNewline
	v.dlg.Title = i18n.Tf("Dialog.Conflict.TitleFor", file.Path)
	v.merge.SetStyle(styleOf(file.Style))
	v.merge.SetSides(
		widget.MergeSideInfo{Title: file.OursLabel, Note: i18n.T("Dialog.Conflict.Note.Ours")},
		widget.MergeSideInfo{Title: file.BaseLabel, Note: i18n.T("Dialog.Conflict.Note.Base")},
		widget.MergeSideInfo{Title: file.TheirsLabel, Note: i18n.T("Dialog.Conflict.Note.Theirs")},
	)
	v.merge.SetChunks(blocksOf(file.Blocks))
	v.merge.GoToConflict(0)
	v.say("")
	v.refresh()
	v.saved = v.Result()
}

func styleOf(s merge.Style) widget.MergeStyle {
	if s == merge.StyleMerge {
		return widget.MergeStyleMerge
	}
	return widget.MergeStyleDiff3
}

func blocksOf(blocks []merge.Chunk) []widget.MergeChunk {
	out := make([]widget.MergeChunk, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, widget.MergeChunk{
			Conflict: block.Conflict,
			Ours:     trimmed(block.Ours),
			Base:     trimmed(block.Base),
			Theirs:   trimmed(block.Theirs),
			Merged:   trimmed(block.Merged),
		})
	}
	return out
}

func trimmed(lines []string) []string {
	if lines == nil {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
	}
	return out
}

func (v *View) Path() string { return v.path }

func (v *View) Unresolved() int { return v.merge.Unresolved() }

func (v *View) Result() string {
	text := v.merge.Result()
	if v.finalNewline && text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func (v *View) requestSave() {
	if v.OnSave == nil {
		return
	}
	v.OnSave(v.Result(), v.merge.Unresolved() == 0)
}

func (v *View) Modified() bool { return v.Result() != v.saved }

func (v *View) Saved(content string, resolved bool) {
	v.saved = content
	if resolved {
		v.say(i18n.T("Dialog.Conflict.Saved"))
		return
	}
	v.say(i18n.T("Dialog.Conflict.SavedWithMarkers"))
}

func (v *View) SaveFailed(err error) {
	v.say(i18n.Tf("Dialog.Conflict.SaveFailed", err))
}

func (v *View) Message() string { return v.message.Text() }

func (v *View) say(text string) {
	v.message.SetText(text)
}

func (v *View) refresh() {
	unresolved, total := v.merge.Unresolved(), v.merge.ConflictCount()
	v.unresolved.SetText(i18n.Tf("Dialog.Conflict.Unresolved", unresolved, total))
	line, col := v.merge.Caret()
	v.position.SetText(i18n.Tf("Dialog.Conflict.Position", line+1, col+1))
	hasConflicts := total > 0
	for _, button := range []*widget.Button{v.takeOurs, v.takeTheirs, v.takeBoth, v.prevConflict, v.nextConflict} {
		button.SetEnabled(hasConflicts)
	}
}

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.takeOurs, v.takeTheirs, v.takeBoth, v.undo, v.prevConflict, v.nextConflict, v.save, v.closeBtn)
	v.prevConflict.Icon = icons.Toolbar(prevChangeIcon, navigationIconSize, p.Text)
	v.nextConflict.Icon = icons.Toolbar(nextChangeIcon, navigationIconSize, p.Text)
	p.Body(v.unresolved, v.position)
	p.Hints(v.message)
}
