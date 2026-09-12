package conflict

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func conflictingFile() File {
	return File{
		Path:         "f.txt",
		OursLabel:    "HEAD",
		BaseLabel:    "base",
		TheirsLabel:  "feature",
		Style:        merge.StyleMerge,
		FinalNewline: true,
		Blocks: []merge.Chunk{
			{Ours: []string{"one\n"}, Base: []string{"one\n"}, Theirs: []string{"one\n"}, Merged: []string{"one\n"}},
			{Conflict: true, Ours: []string{"OURS\n"}, Base: []string{"two\n"}, Theirs: []string{"THEIRS\n"}},
			{Ours: []string{"three\n"}, Base: []string{"three\n"}, Theirs: []string{"three\n"}, Merged: []string{"three\n"}},
		},
	}
}

func shown(t *testing.T) *View {
	t.Helper()
	v := newTestView(t)
	v.Show(conflictingFile())
	return v
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		named := map[string]widget.Widget{"merge": widget.NewMergeView("", "")}
		for _, name := range []string{"takeOurs", "takeTheirs", "takeBoth", "undo", "prevConflict", "nextConflict", "save", "close"} {
			named[name] = widget.NewButton("")
		}
		named["showBase"] = widget.NewCheckBox("")
		for _, name := range []string{"unresolved", "position", "message"} {
			named[name] = widget.NewLabel("", widget.CurrentTheme().LabelText)
		}
		return named
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(title string, _ string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog(title, 10, 10), named, nil
		}
		if _, err := NewView(); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %q: err = %v, want ErrWidgetMissing", name, err)
		}
	}
}

func TestTheWindowCanBeResizedButNotCrushed(t *testing.T) {
	v := newTestView(t)

	w, h := v.Dialog().MinSize()

	if !v.Dialog().IsResizable() || w != dialogMinWidth || h != dialogMinHeight {
		t.Fatalf("resizable = %v, min = %dx%d", v.Dialog().IsResizable(), w, h)
	}
}

func TestTheWindowFitsIntoTheMainOne(t *testing.T) {
	v := newTestView(t)

	v.FitWithin(600, 400)

	if size := v.Dialog().Bounds(); size.Dx() != dialogMinWidth || size.Dy() != dialogMinHeight {
		t.Fatalf("size = %v, want the minimum in a small window", size)
	}
}

func TestAShownConflictNamesTheFileAndCountsTheBlocks(t *testing.T) {
	v := shown(t)

	if v.Path() != "f.txt" || v.Dialog().Title != i18n.Tf("Dialog.Conflict.TitleFor", "f.txt") {
		t.Fatalf("path = %q, title = %q", v.Path(), v.Dialog().Title)
	}
	if v.Merge().ConflictCount() != 1 || v.Unresolved() != 1 {
		t.Fatalf("conflicts = %d, unresolved = %d", v.Merge().ConflictCount(), v.Unresolved())
	}
	if want := i18n.Tf("Dialog.Conflict.Unresolved", 1, 1); v.unresolved.Text() != want {
		t.Fatalf("counter = %q, want %q", v.unresolved.Text(), want)
	}
}

func TestTheLinesReachTheControlWithoutTheirNewlines(t *testing.T) {
	v := shown(t)

	blocks := v.Merge().Chunks()

	if len(blocks) != 3 || blocks[1].Ours[0] != "OURS" || blocks[0].Merged[0] != "one" {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestTakingASideResolvesTheBlock(t *testing.T) {
	for _, tt := range []struct {
		name  string
		click func(v *View)
		want  string
	}{
		{"ours", func(v *View) { v.takeOurs.OnClick() }, "one\nOURS\nthree\n"},
		{"theirs", func(v *View) { v.takeTheirs.OnClick() }, "one\nTHEIRS\nthree\n"},
		{"both", func(v *View) { v.takeBoth.OnClick() }, "one\nOURS\nTHEIRS\nthree\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := shown(t)

			tt.click(v)

			if v.Unresolved() != 0 {
				t.Fatalf("unresolved = %d after taking a side", v.Unresolved())
			}
			if got := v.Result(); got != tt.want {
				t.Fatalf("result = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnUnresolvedConflictIsSavedWithMarkers(t *testing.T) {
	v := shown(t)

	if got := v.Result(); !strings.Contains(got, "<<<<<<< HEAD") || !strings.Contains(got, ">>>>>>> feature") {
		t.Fatalf("result = %q, want the markers of both sides", got)
	}
}

func TestUndoBringsTheConflictBack(t *testing.T) {
	v := shown(t)
	v.takeOurs.OnClick()

	v.undo.OnClick()

	if v.Unresolved() != 1 {
		t.Fatalf("unresolved = %d, want the conflict back", v.Unresolved())
	}
}

func TestSavingHandsOverTheResultAndWhetherItIsResolved(t *testing.T) {
	v := shown(t)
	var content string
	var resolved bool
	seen := 0
	v.OnSave = func(text string, done bool) {
		content, resolved, seen = text, done, seen+1
	}

	v.save.OnClick()
	v.takeTheirs.OnClick()
	v.save.OnClick()

	if seen != 2 {
		t.Fatalf("OnSave called %d times", seen)
	}
	if !resolved || content != "one\nTHEIRS\nthree\n" {
		t.Fatalf("content = %q, resolved = %v", content, resolved)
	}
}

func TestSavingWithoutAListenerIsQuiet(t *testing.T) {
	v := shown(t)

	v.save.OnClick()
}

func TestTheWindowReportsWhatHappenedToTheSave(t *testing.T) {
	v := shown(t)

	v.Saved(true)
	if v.message.Text() != i18n.T("Dialog.Conflict.Saved") {
		t.Fatalf("message = %q", v.message.Text())
	}

	v.Saved(false)
	if v.message.Text() != i18n.T("Dialog.Conflict.SavedWithMarkers") {
		t.Fatalf("message = %q", v.message.Text())
	}

	v.SaveFailed(errors.New("disk is full"))
	if v.Message() != i18n.Tf("Dialog.Conflict.SaveFailed", errors.New("disk is full")) {
		t.Fatalf("message = %q", v.Message())
	}
}

func TestClosingCallsBackAndToleratesNoListener(t *testing.T) {
	v := shown(t)
	v.closeBtn.OnClick()

	closed := 0
	v.OnClose = func() { closed++ }
	v.closeBtn.OnClick()

	if closed != 1 {
		t.Fatalf("OnClose called %d times", closed)
	}
}

func TestWalkingBetweenConflictsMovesTheCurrentOne(t *testing.T) {
	v := newTestView(t)
	file := conflictingFile()
	file.Blocks = append(file.Blocks, merge.Chunk{Conflict: true, Ours: []string{"A\n"}, Base: []string{"b\n"}, Theirs: []string{"B\n"}})
	v.Show(file)

	v.nextConflict.OnClick()
	after := v.Merge().CurrentConflict()
	v.prevConflict.OnClick()

	if after == v.Merge().CurrentConflict() {
		t.Fatalf("walking did not move the current conflict, it stayed at %d", after)
	}
}

func TestAFileWithoutConflictsLeavesTheButtonsOff(t *testing.T) {
	v := newTestView(t)
	file := conflictingFile()
	file.Blocks = file.Blocks[:1]

	v.Show(file)

	if v.takeOurs.IsEnabled() || v.nextConflict.IsEnabled() {
		t.Fatal("there is nothing to resolve, so the buttons must be off")
	}
}

func TestTheBaseCanBeHidden(t *testing.T) {
	v := shown(t)

	v.showBase.OnChange(false)

	if v.Merge().ShowBase() {
		t.Fatal("the base panel is still shown")
	}
}

func TestADiff3FileKeepsTheBaseInTheMarkers(t *testing.T) {
	v := newTestView(t)
	file := conflictingFile()
	file.Style = merge.StyleDiff3

	v.Show(file)

	if !strings.Contains(v.Result(), "||||||| base") {
		t.Fatalf("result = %q, want the base between the sides", v.Result())
	}
}

func TestTheWindowWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := shown(t)

	v.Restyle(theme)

	if v.takeOurs.Background != p.Field || v.prevConflict.Icon == nil {
		t.Fatalf("quiet button = %v, icon = %v", v.takeOurs.Background, v.prevConflict.Icon)
	}
}

func TestEditingTheResultRefreshesThePosition(t *testing.T) {
	v := shown(t)

	v.Merge().OnResultEdited()
	v.Merge().OnCaretMoved(3, 4)
	v.Merge().OnCurrentConflict(0)
	v.Merge().OnResolvedChanged(1)

	line, col := v.Merge().Caret()
	if want := i18n.Tf("Dialog.Conflict.Position", line+1, col+1); v.position.Text() != want {
		t.Fatalf("position = %q, want %q", v.position.Text(), want)
	}
}

func TestCtrlSAsksToSave(t *testing.T) {
	v := shown(t)
	saved := 0
	v.OnSave = func(string, bool) { saved++ }

	v.Merge().OnSaveRequest()

	if saved != 1 {
		t.Fatalf("OnSave called %d times", saved)
	}
}
