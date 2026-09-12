package compareview

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
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

func summary() Summary {
	return Summary{
		Left: "main", Right: "feature", Ahead: 2, Behind: 1,
		Changes: []Change{
			{Status: "Modified", Path: "f", Added: 3, Deleted: 1},
			{Status: "Renamed", Path: "moved", Old: "f", Added: 0, Deleted: 0},
		},
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) { return nil, nil, wantErr }
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"sidesLabel":  widget.NewLabel("", widget.CurrentTheme().LabelText),
			"left":        widget.NewDropdown(),
			"right":       widget.NewDropdown(),
			"countsLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"changes":     widget.NewDataGridWidget(),
			"hint":        widget.NewLabel("", widget.CurrentTheme().LabelText),
			"close":       widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog("", 100, 100), named, nil
		}
		if _, err := NewView(); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", name, err)
		}
	}
}

func TestTheDialogAsksForAComparisonWhenASideChanges(t *testing.T) {
	v := newTestView(t)
	var asked [][2]string
	v.OnCompare = func(left, right string) { asked = append(asked, [2]string{left, right}) }

	v.SetSides([]string{"main", "feature"}, "main", "feature")
	v.leftList.SetSelected(1)
	v.leftList.OnChange(1, "feature")

	if left, right := v.Sides(); left != "feature" || right != "feature" {
		t.Fatalf("sides = %q, %q", left, right)
	}
	if len(asked) == 0 || asked[len(asked)-1] != [2]string{"feature", "feature"} {
		t.Fatalf("asked = %v", asked)
	}
}

func TestTheDialogDoesNotAskWithoutSides(t *testing.T) {
	v := newTestView(t)
	asked := false
	v.OnCompare = func(string, string) { asked = true }

	v.leftList.OnChange(0, "")
	v.rightList.OnChange(0, "")

	if asked {
		t.Fatal("the dialog asked for a comparison without sides")
	}
}

func TestTheDialogShowsTheSummaryItIsGiven(t *testing.T) {
	v := newTestView(t)

	v.SetSummary(summary())

	if v.sidesLabel.Text() != i18n.Tf("Dialog.CompareRefs.Sides", "main", "feature") {
		t.Fatalf("sides = %q", v.sidesLabel.Text())
	}
	if v.countsLabel.Text() != i18n.Tf("Dialog.CompareRefs.Counts", 2, 1) {
		t.Fatalf("counts = %q", v.countsLabel.Text())
	}
	if v.hintLabel.Text() != i18n.Tf("Dialog.CompareRefs.Hint.Changes", 2) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if got := v.Summary(); len(got.Changes) != 2 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestARenamedFileShowsBothNames(t *testing.T) {
	if got := pathOf(Change{Path: "moved", Old: "f"}); got != "f → moved" {
		t.Fatalf("path = %q", got)
	}
	if got := pathOf(Change{Path: "f", Old: "f"}); got != "f" {
		t.Fatalf("path = %q", got)
	}
}

func TestTheSameCommitOnBothSidesSaysSo(t *testing.T) {
	v := newTestView(t)

	v.SetSummary(Summary{Left: "main", Right: "main", Same: true})

	if v.hintLabel.Text() != i18n.T("Dialog.CompareRefs.Hint.Same") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTwoPlacesWithoutDifferencesSaySo(t *testing.T) {
	v := newTestView(t)

	v.SetSummary(Summary{Left: "main", Right: "feature", Ahead: 1, Behind: 1})

	if v.hintLabel.Text() != i18n.T("Dialog.CompareRefs.Hint.NoChanges") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestCloseReachesTheCallback(t *testing.T) {
	v := newTestView(t)
	closed := false
	v.OnClose = func() { closed = true }

	v.Dialog().CancelAction()
	v.closeBtn.OnClick()

	if !closed {
		t.Fatal("the close callback did not run")
	}
}

func TestTheCloseCallbackIsOptional(t *testing.T) {
	v := newTestView(t)

	v.closeBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
