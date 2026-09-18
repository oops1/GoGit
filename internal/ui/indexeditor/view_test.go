package indexeditor

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
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

func editedFile() File {
	return File{
		Path:      "f.txt",
		HeadLabel: "main",
		Head:      []byte("one\ntwo\nthree\n"),
		Index:     []byte("one\nTWO\nthree\n"),
		Working:   []byte("one\nTWO\nthree\nfour\n"),
	}
}

func shown(t *testing.T) *View {
	t.Helper()
	v := newTestView(t)
	v.Show(editedFile())
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
		for _, name := range []string{"fromHead", "keepIndex", "fromWorking", "prevChange", "nextChange", "reset", "save", "close"} {
			named[name] = widget.NewButton("")
		}
		for _, name := range []string{"changes", "position", "message"} {
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

func TestTheWindowCanBeMaximized(t *testing.T) {
	v := newTestView(t)

	if !v.Dialog().HasWindowButtons() {
		t.Fatal("the window has no buttons to maximize it")
	}
}

func TestAnOpenedFileStartsFromWhatIsStagedNow(t *testing.T) {
	v := shown(t)

	if v.Path() != "f.txt" || v.Dialog().Title != i18n.Tf("Dialog.IndexEditor.TitleFor", "f.txt") {
		t.Fatalf("path = %q, title = %q", v.Path(), v.Dialog().Title)
	}
	if got := v.Result(); got != "one\nTWO\nthree\n" {
		t.Fatalf("result = %q, want the file as it is staged now", got)
	}
	if v.Changes() != 2 {
		t.Fatalf("changes = %d, want the two places where the sides differ", v.Changes())
	}
	if want := i18n.Tf("Dialog.IndexEditor.Changes", 2); v.changes.Text() != want {
		t.Fatalf("counter = %q, want %q", v.changes.Text(), want)
	}
	if v.Modified() {
		t.Fatal("a window that was just opened counts as changed")
	}
}

func TestTheThreeSidesAreNamedAfterHeadTheIndexAndTheWorkingCopy(t *testing.T) {
	v := shown(t)

	sides := v.Merge().Sides()

	if sides[widget.MergeOurs].Title != "main" || sides[widget.MergeBase].Title != i18n.T("Dialog.IndexEditor.Side.Index") {
		t.Fatalf("sides = %+v", sides)
	}
	if sides[widget.MergeTheirs].Title != i18n.T("Dialog.IndexEditor.Side.Working") {
		t.Fatalf("sides = %+v", sides)
	}
	if v.Merge().ResultInfo().Title != i18n.T("Dialog.IndexEditor.Side.Result") {
		t.Fatalf("result = %+v", v.Merge().ResultInfo())
	}
}

func TestABlockCanComeFromEitherSide(t *testing.T) {
	for _, tt := range []struct {
		name  string
		click func(v *View)
		want  string
	}{
		{"head", func(v *View) { v.fromHead.OnClick() }, "one\ntwo\nthree\n"},
		{"index", func(v *View) { v.keepIndex.OnClick() }, "one\nTWO\nthree\n"},
		{"working", func(v *View) { v.fromWorking.OnClick() }, "one\nTWO\nthree\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := shown(t)

			tt.click(v)

			if got := v.Result(); got != tt.want {
				t.Fatalf("result = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTheLineOnlyTheWorkingCopyHasCanBeStaged(t *testing.T) {
	v := shown(t)

	v.nextChange.OnClick()
	v.fromWorking.OnClick()

	if got := v.Result(); got != "one\nTWO\nthree\nfour\n" {
		t.Fatalf("result = %q, want the working copy line staged", got)
	}
	if !v.Modified() {
		t.Fatal("taking a block did not count as a change")
	}
}

func TestGoingBackToTheIndexThrowsTheEditsAway(t *testing.T) {
	v := shown(t)
	v.fromHead.OnClick()

	v.reset.OnClick()

	if got := v.Result(); got != "one\nTWO\nthree\n" {
		t.Fatalf("result = %q, want what is staged now", got)
	}
	if v.Message() != i18n.T("Dialog.IndexEditor.Restored") {
		t.Fatalf("message = %q", v.Message())
	}
	if v.Modified() {
		t.Fatal("going back to the index still counts as a change")
	}
}

func TestSavingHandsOverTheResult(t *testing.T) {
	v := shown(t)
	seen := 0
	content := ""
	v.OnSave = func(text string) { content, seen = text, seen+1 }

	v.fromHead.OnClick()
	v.save.OnClick()

	if seen != 1 || content != "one\ntwo\nthree\n" {
		t.Fatalf("OnSave called %d times with %q", seen, content)
	}
}

func TestSavingWithoutAListenerIsQuiet(t *testing.T) {
	v := shown(t)

	v.save.OnClick()
}

func TestCtrlSAsksToSave(t *testing.T) {
	v := shown(t)
	saved := 0
	v.OnSave = func(string) { saved++ }

	v.Merge().OnSaveRequest()

	if saved != 1 {
		t.Fatalf("OnSave called %d times", saved)
	}
}

func TestTheWindowReportsWhatHappenedToTheSave(t *testing.T) {
	v := shown(t)

	v.fromHead.OnClick()
	v.Saved(v.Result())
	if v.Message() != i18n.T("Dialog.IndexEditor.Saved") || v.Modified() {
		t.Fatalf("message = %q, modified = %v", v.Message(), v.Modified())
	}

	v.SaveFailed(errors.New("disk is full"))
	if v.Message() != i18n.Tf("Dialog.IndexEditor.SaveFailed", errors.New("disk is full")) {
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

func TestWalkingBetweenChangesMovesTheCurrentOne(t *testing.T) {
	v := shown(t)

	v.nextChange.OnClick()
	after := v.Merge().CurrentConflict()
	v.prevChange.OnClick()

	if after == v.Merge().CurrentConflict() {
		t.Fatalf("walking did not move the current change, it stayed at %d", after)
	}
}

func TestAFileThatIsTheSameEverywhereLeavesTheButtonsOff(t *testing.T) {
	v := newTestView(t)
	same := []byte("one\ntwo\n")

	v.Show(File{Path: "f.txt", HeadLabel: "main", Head: same, Index: same, Working: same})

	if v.Changes() != 0 || v.fromHead.IsEnabled() || v.nextChange.IsEnabled() {
		t.Fatalf("changes = %d, there is nothing to move between the sides", v.Changes())
	}
	if got := v.Result(); got != "one\ntwo\n" {
		t.Fatalf("result = %q", got)
	}
}

func TestAnUntrackedFileIsStagedWholeFromTheWorkingCopy(t *testing.T) {
	v := newTestView(t)

	v.Show(File{Path: "u.txt", HeadLabel: "main", Working: []byte("new\nfile\n")})

	if got := v.Result(); got != "" {
		t.Fatalf("result = %q, want nothing staged yet", got)
	}
	v.fromWorking.OnClick()
	if got := v.Result(); got != "new\nfile\n" {
		t.Fatalf("result = %q, want the whole working copy", got)
	}
}

func TestAFileOnlyTheCommitStillHasCanComeBackWhole(t *testing.T) {
	v := newTestView(t)

	v.Show(File{Path: "f.txt", Head: []byte("gone\n")})

	if got := v.Result(); got != "" {
		t.Fatalf("result = %q, want nothing staged", got)
	}
	v.fromHead.OnClick()
	if got := v.Result(); got != "gone\n" {
		t.Fatalf("result = %q, want the file of the last commit", got)
	}
}

func TestAFileThatIsNowhereHasNothingToShow(t *testing.T) {
	v := newTestView(t)

	v.Show(File{Path: "f.txt"})

	if v.Changes() != 0 || v.Result() != "" {
		t.Fatalf("changes = %d, result = %q", v.Changes(), v.Result())
	}
}

func TestAFileWithWindowsLineEndingsKeepsThem(t *testing.T) {
	v := newTestView(t)

	v.Show(File{
		Path:    "f.txt",
		Head:    []byte("one\r\ntwo\r\n"),
		Index:   []byte("one\r\nTWO\r\n"),
		Working: []byte("one\r\nTWO\r\n"),
	})

	if got := v.Result(); got != "one\r\nTWO\r\n" {
		t.Fatalf("result = %q, want the line endings of the index", got)
	}
}

func TestAFileWithoutALastNewlineKeepsItThatWay(t *testing.T) {
	v := newTestView(t)

	v.Show(File{Path: "f.txt", Head: []byte("one\n"), Index: []byte("one"), Working: []byte("one")})

	if got := v.Result(); got != "one" {
		t.Fatalf("result = %q, want no newline at the end", got)
	}
}

func TestEditingTheResultRefreshesThePosition(t *testing.T) {
	v := shown(t)

	v.Merge().OnResultEdited()
	v.Merge().OnCaretMoved(3, 4)
	v.Merge().OnCurrentConflict(0)
	v.Merge().OnResolvedChanged(1)

	line, col := v.Merge().Caret()
	if want := i18n.Tf("Dialog.IndexEditor.Position", line+1, col+1); v.position.Text() != want {
		t.Fatalf("position = %q, want %q", v.position.Text(), want)
	}
}

func TestTheWindowWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := shown(t)

	v.Restyle(theme)

	if v.fromHead.Background != p.Field || v.prevChange.Icon == nil || v.nextChange.Icon == nil {
		t.Fatalf("quiet button = %v, icon = %v", v.fromHead.Background, v.prevChange.Icon)
	}
}
