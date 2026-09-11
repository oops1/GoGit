package compare

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

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

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loaded(t *testing.T, left, right string) *View {
	t.Helper()
	v := newTestView(t)
	if err := v.Load(widget.DiffLeft, writeFile(t, "left.txt", left)); err != nil {
		t.Fatal(err)
	}
	if err := v.Load(widget.DiffRight, writeFile(t, "right.txt", right)); err != nil {
		t.Fatal(err)
	}
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
		named := map[string]widget.Widget{"diff": widget.NewDiffView("", "")}
		for _, name := range []string{"openLeft", "openRight", "save", "undo", "redo", "prevChange", "nextChange", "close"} {
			named[name] = widget.NewButton("")
		}
		for _, name := range []string{"hideUnchanged", "ignoreWhitespace"} {
			named[name] = widget.NewCheckBox("")
		}
		for _, name := range []string{"changeCount", "position", "message"} {
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
		t.Fatalf("resizable = %v, min = %dx%d, want a resizable window no smaller than %dx%d",
			v.Dialog().IsResizable(), w, h, dialogMinWidth, dialogMinHeight)
	}
}

func TestTwoFilesAreComparedAndTheirChangesCounted(t *testing.T) {
	v := loaded(t, "one\ntwo\nthree\n", "one\nTWO\nthree\nfour\n")

	if v.Diff().ChangeCount() == 0 {
		t.Fatal("different files must show their changes")
	}
	if want := i18n.Tf("Dialog.Compare.Changes", v.Diff().ChangeCount()); v.changeCount.Text() != want {
		t.Fatalf("counter = %q, want %q", v.changeCount.Text(), want)
	}
	if !v.nextChange.IsEnabled() || !v.prevChange.IsEnabled() {
		t.Fatal("with changes to walk through the navigation must be on")
	}
	if v.save.IsEnabled() {
		t.Fatal("nothing has been edited yet, so there is nothing to save")
	}
}

func TestIdenticalFilesSayWhatTheyAre(t *testing.T) {
	v := loaded(t, "same\n", "same\n")

	if v.changeCount.Text() != i18n.T("Dialog.Compare.Identical") {
		t.Fatalf("counter = %q, want the files called identical", v.changeCount.Text())
	}
	if v.nextChange.IsEnabled() {
		t.Fatal("there is nothing to navigate to between identical files")
	}
}

func TestAnEmptyWindowCountsNothing(t *testing.T) {
	v := newTestView(t)

	if v.changeCount.Text() != "" {
		t.Fatalf("counter = %q, want it empty until a file is opened", v.changeCount.Text())
	}
}

func TestAFileThatCannotBeOpenedIsReportedInTheWindow(t *testing.T) {
	v := newTestView(t)
	missing := filepath.Join(t.TempDir(), "gone.txt")

	if err := v.Load(widget.DiffLeft, missing); err == nil {
		t.Fatal("a missing file must fail to load")
	}
	if !strings.Contains(v.message.Text(), missing) {
		t.Fatalf("message = %q, want the path that failed", v.message.Text())
	}
}

func TestCodeIsHighlightedButProseIsNot(t *testing.T) {
	v := newTestView(t)
	if err := v.Load(widget.DiffLeft, writeFile(t, "notes.txt", "a\n")); err != nil {
		t.Fatal(err)
	}
	if v.showsCode() {
		t.Fatal("a text file is not code")
	}
	if err := v.Load(widget.DiffRight, writeFile(t, "main.GO", "package main\n")); err != nil {
		t.Fatal(err)
	}
	if !v.showsCode() {
		t.Fatal("a Go file on either side makes the comparison code")
	}
}

func TestAnEditIsSavedToItsFile(t *testing.T) {
	v := loaded(t, "one\n", "two\n")
	left := v.Diff().FilePath(widget.DiffLeft)

	v.Diff().SetActiveSide(widget.DiffLeft)
	v.Diff().SetCaret(widget.DiffLeft, 0, 0)
	v.Diff().InsertText("new ")
	if !v.Modified() || !v.save.IsEnabled() || !v.undo.IsEnabled() {
		t.Fatal("an edit must make the window modified, savable and undoable")
	}

	v.SaveAll()

	data, err := os.ReadFile(left)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new one\n" || v.Modified() {
		t.Fatalf("file = %q, modified = %v, want the edit on disk", data, v.Modified())
	}
}

func TestSavingWithNothingChangedSaysSo(t *testing.T) {
	v := loaded(t, "one\n", "two\n")

	v.SaveAll()

	if v.message.Text() != i18n.T("Dialog.Compare.NothingToSave") {
		t.Fatalf("message = %q, want nothing to save", v.message.Text())
	}
}

func TestAnUnnamedSideIsSavedThroughTheApplication(t *testing.T) {
	v := newTestView(t)
	v.Diff().SetText(widget.DiffRight, "draft", "", "")
	v.Diff().SetActiveSide(widget.DiffRight)
	v.Diff().InsertText("typed\n")
	var asked []widget.DiffSide
	v.OnSaveAs = func(side widget.DiffSide) { asked = append(asked, side) }

	v.SaveAll()

	if len(asked) != 1 || asked[0] != widget.DiffRight {
		t.Fatalf("asked = %v, want a name for the right side", asked)
	}
	target := filepath.Join(t.TempDir(), "saved.txt")
	if err := v.SaveAs(widget.DiffRight, target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "typed\n" {
		t.Fatalf("file = %q, want what was typed", data)
	}
}

func TestSavingToAPlaceThatCannotBeWrittenIsReported(t *testing.T) {
	v := newTestView(t)
	v.Diff().SetText(widget.DiffRight, "draft", "", "x\n")

	if err := v.SaveAs(widget.DiffRight, t.TempDir()); err == nil {
		t.Fatal("a directory cannot be saved over")
	}
	prefix, _, _ := strings.Cut(i18n.T("Dialog.Compare.SaveFailed"), "%")
	if !strings.HasPrefix(v.message.Text(), prefix) {
		t.Fatalf("message = %q, want the failure", v.message.Text())
	}
}

func TestTheButtonsReachTheApplication(t *testing.T) {
	v := newTestView(t)
	var picked []widget.DiffSide
	closed := 0
	v.OnPick = func(side widget.DiffSide) { picked = append(picked, side) }
	v.OnClose = func() { closed++ }

	v.openLeft.OnClick()
	v.openRight.OnClick()
	v.closeBtn.OnClick()
	v.Dialog().CancelAction()

	if len(picked) != 2 || picked[0] != widget.DiffLeft || picked[1] != widget.DiffRight {
		t.Fatalf("picked = %v, want left then right", picked)
	}
	if closed != 2 {
		t.Fatalf("closed = %d, want the button and Escape to ask the application", closed)
	}
}

func TestTheWindowWorksWithoutCallbacks(t *testing.T) {
	v := newTestView(t)

	v.openLeft.OnClick()
	v.closeBtn.OnClick()
	v.Diff().SetText(widget.DiffRight, "draft", "", "")
	v.Diff().SetActiveSide(widget.DiffRight)
	v.Diff().InsertText("x")
	v.SaveAll()
}

func TestTheCaretPositionNamesTheSide(t *testing.T) {
	v := loaded(t, "one\ntwo\n", "one\n")

	v.Diff().OnCaretMoved(widget.DiffRight, 1, 3)

	want := i18n.Tf("Dialog.Compare.Position", i18n.T("Dialog.Compare.Side.Right"), 2, 4)
	if v.position.Text() != want {
		t.Fatalf("position = %q, want %q", v.position.Text(), want)
	}
	v.Diff().OnActiveSideChanged(widget.DiffLeft)
	if !strings.HasPrefix(v.position.Text(), i18n.T("Dialog.Compare.Side.Left")) {
		t.Fatalf("position = %q, want the left side named", v.position.Text())
	}
}

func TestWhatHappensToTheFilesIsSaidInTheStatusLine(t *testing.T) {
	v := loaded(t, "one\n", "two\n")

	v.Diff().OnFileChangedOnDisk(widget.DiffLeft, "a.txt", false)
	if v.message.Text() != i18n.Tf("Dialog.Compare.ChangedOnDisk", "a.txt") {
		t.Fatalf("message = %q", v.message.Text())
	}
	v.Diff().OnFileChangedOnDisk(widget.DiffLeft, "a.txt", true)
	if v.message.Text() != i18n.Tf("Dialog.Compare.DeletedOnDisk", "a.txt") {
		t.Fatalf("message = %q", v.message.Text())
	}
	v.Diff().OnFileSaved(widget.DiffLeft, "a.txt")
	if v.message.Text() != i18n.Tf("Dialog.Compare.Saved", "a.txt") {
		t.Fatalf("message = %q", v.message.Text())
	}
	v.Diff().OnError(widget.DiffLeft, errors.New("broken"))
	if v.message.Text() != "broken" {
		t.Fatalf("message = %q", v.message.Text())
	}
}

func TestTheToolbarDrivesTheComparison(t *testing.T) {
	v := loaded(t, "a\nb\nc\nd\ne\nf\ng\n", "a\nB\nc\nd\ne\nF\ng\n")

	v.nextChange.OnClick()
	first := v.Diff().CurrentChange()
	v.nextChange.OnClick()
	if v.Diff().CurrentChange() == first {
		t.Fatal("next must move to the next change")
	}
	v.prevChange.OnClick()
	if v.Diff().CurrentChange() != first {
		t.Fatal("previous must come back")
	}

	v.hideUnchanged.OnChange(true)
	if !v.Diff().HideUnchanged() {
		t.Fatal("the checkbox must fold identical lines")
	}
	v.ignoreWhitespace.OnChange(true)

	v.Diff().SetActiveSide(widget.DiffLeft)
	v.Diff().InsertText("x")
	v.undo.OnClick()
	if v.Modified() {
		t.Fatal("undo must take the edit back")
	}
	v.redo.OnClick()
	if !v.Modified() {
		t.Fatal("redo must put the edit back")
	}
	v.Diff().OnSaveRequest(widget.DiffLeft)
	if v.Modified() {
		t.Fatal("Ctrl+S must save")
	}
}

func TestTheWindowWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t)

	v.Restyle(theme)

	if v.closeBtn.Background != p.Field || v.openLeft.Background != p.Field {
		t.Fatalf("buttons = %v, want the quiet fill", v.closeBtn.Background)
	}
	if v.message.TextColor != p.Secondary || v.position.TextColor != p.Text {
		t.Fatal("the status line must use the text and hint colours")
	}
	if v.prevChange.Icon == nil || v.nextChange.Icon == nil {
		t.Fatal("the navigation buttons must carry their icons")
	}
}

func TestAFileThatCannotBeSavedStopsTheSaveAndSaysWhy(t *testing.T) {
	v := loaded(t, "one\n", "two\n")
	left := v.Diff().FilePath(widget.DiffLeft)
	v.Diff().SetActiveSide(widget.DiffLeft)
	v.Diff().InsertText("x")
	if err := os.Remove(left); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(left, 0o750); err != nil {
		t.Fatal(err)
	}

	v.SaveAll()

	prefix, _, _ := strings.Cut(i18n.T("Dialog.Compare.SaveFailed"), "%")
	if !strings.HasPrefix(v.message.Text(), prefix) || !v.Modified() {
		t.Fatalf("message = %q, modified = %v, want the failure and the edit kept", v.message.Text(), v.Modified())
	}
}
