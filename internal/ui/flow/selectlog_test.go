package flow

import (
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
)

func logWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"header": label(), "commits": widget.NewDataGridWidget(), "hint": label(),
		"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
}

func newLog(t *testing.T) *LogView {
	t.Helper()
	installStrings(t)
	v, err := NewLogView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func commitOf(text string) hash.ObjectID { return hash.SumSHA1("blob", []byte(text)) }

func twoCommits() []LogCommit {
	when := time.Unix(1700000000, 0).UTC()
	return []LogCommit{
		{Commit: commitOf("second"), When: when, Author: "ann", Message: "Tidy the parser\n\nThe body explains why.\n"},
		{Commit: commitOf("first"), When: when.Add(-time.Hour), Author: "bob", Message: "Add the parser"},
	}
}

func (v *LogView) selectRow(t *testing.T, index int) {
	t.Helper()
	commit := v.commits[index]
	v.table.Grid.SetSelectedIndex(index)
	v.pick(datagrid.SelectionChangedEvent{SelectedItem: LogRow{
		Commit:  shortCommit(commit.Commit),
		Message: logSubject(commit.Message),
	}})
}

func TestTheLogPickerHandsBackTheWholeMessageOfThePickedCommit(t *testing.T) {
	v := newLog(t)
	commits := twoCommits()
	var picked []LogCommit
	v.OnOK = func(commit LogCommit) { picked = append(picked, commit) }

	v.SetCommits(commits)
	if v.okBtn.IsEnabled() {
		t.Fatal("nothing is picked yet, the button must be disabled")
	}
	if got := v.hintLabel.Text(); got != i18n.T("Dialog.FlowLog.Hint.PickOne") {
		t.Fatalf("hint = %q", got)
	}
	v.okBtn.OnClick()
	v.selectRow(t, 0)
	if !v.okBtn.IsEnabled() {
		t.Fatal("a picked commit must enable the button")
	}
	if got := v.hintLabel.Text(); got != i18n.Tf("Dialog.FlowLog.Hint.Ready", shortCommit(commits[0].Commit)) {
		t.Fatalf("hint = %q", got)
	}
	v.okBtn.OnClick()

	if len(picked) != 1 || picked[0].Message != commits[0].Message {
		t.Fatalf("picked = %+v", picked)
	}
	if got := v.Commits(); len(got) != 2 || got[1].Commit != commits[1].Commit {
		t.Fatalf("commits = %+v", got)
	}
	if got := v.Dialog().Title; got != i18n.T("Dialog.FlowLog.Title") {
		t.Fatalf("title = %q", got)
	}
}

func TestTheLogPickerShowsTheSubjectTheShortHashAndTheLocalTime(t *testing.T) {
	v := newLog(t)
	commits := twoCommits()

	v.SetCommits(commits)

	rows := v.table.Grid.ItemsSource().Items()
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	want := LogRow{
		Commit:  commits[0].Commit.String()[:logShortLength],
		When:    commits[0].When.Local().Format(logDateLayout),
		Author:  "ann",
		Message: "Tidy the parser",
	}
	if got := rows[0].(LogRow); got != want {
		t.Fatalf("row = %+v, want %+v", got, want)
	}
}

func TestTheLogPickerSaysWhenThereIsNothingToPick(t *testing.T) {
	v := newLog(t)

	v.SetCommits(nil)

	if v.okBtn.IsEnabled() {
		t.Fatal("an empty log must leave the button disabled")
	}
	if got := v.hintLabel.Text(); got != i18n.T("Dialog.FlowLog.Hint.Empty") {
		t.Fatalf("hint = %q", got)
	}
}

func TestTheLogPickerForgetsTheSelectionWhenTheRowIsGone(t *testing.T) {
	v := newLog(t)
	v.SetCommits(twoCommits())
	v.selectRow(t, 1)

	v.pick(datagrid.SelectionChangedEvent{SelectedItem: nil})

	if v.okBtn.IsEnabled() {
		t.Fatal("a lost selection must disable the button")
	}
	v.OnOK = func(LogCommit) { t.Fatal("the picker confirmed without a selection") }
	v.okBtn.OnClick()
}

func TestTheLogPickerCancels(t *testing.T) {
	v := newLog(t)
	cancelled := 0
	v.OnCancel = func() { cancelled++ }

	v.cancelBtn.OnClick()
	v.Dialog().CancelAction()

	if cancelled != 2 {
		t.Fatalf("cancelled = %d", cancelled)
	}
}

func TestTheLogPickerWithoutCallbacksStaysQuiet(t *testing.T) {
	v := newLog(t)
	v.SetCommits(twoCommits())
	v.selectRow(t, 0)

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestTheLogPickerRestylesEveryWidget(t *testing.T) {
	v := newLog(t)

	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
		if v.okBtn.Background == v.cancelBtn.Background {
			t.Fatal("the primary and the quiet button must differ")
		}
	}
}

func TestTheFinishDialogsAskTheLogForAMessage(t *testing.T) {
	for kind, known := range map[string]FinishKnown{
		ops.FlowKindFeature: featureKnown,
		ops.FlowKindRelease: releaseKnown,
		ops.FlowKindHotfix:  hotfixKnown,
	} {
		v := newFinish(t, kind)
		asked := 0
		v.OnSelectFromLog = func() { asked++ }
		v.SetKnown(known)

		v.logBtn.OnClick()
		v.SetMessage("Tidy the parser")

		if asked != 1 {
			t.Fatalf("%s: the button asked %d times", kind, asked)
		}
		if got := v.Model().Message; got != "Tidy the parser" {
			t.Fatalf("%s: message = %q", kind, got)
		}
		if got := v.logBtn.GetText(); got != i18n.T("Dialog.FlowFinish.SelectFromLog") {
			t.Fatalf("%s: button text = %q", kind, got)
		}
	}
}

func TestTheResumingFinishDialogKeepsTheMessageOutOfReach(t *testing.T) {
	v := newFinish(t, ops.FlowKindRelease)
	asked := 0
	v.OnSelectFromLog = func() { asked++ }
	resuming := releaseKnown
	resuming.Resuming = true
	v.SetKnown(resuming)

	v.logBtn.OnClick()
	v.SetMessage("Tidy the parser")

	if asked != 0 || v.logBtn.IsEnabled() {
		t.Fatalf("asked = %d, enabled = %v while resuming", asked, v.logBtn.IsEnabled())
	}
	if got := v.Model().Message; got != ops.DefaultFlowMessage(resuming.Name) {
		t.Fatalf("message = %q", got)
	}
}

func TestTheFinishDialogWithoutALogCallbackStaysQuiet(t *testing.T) {
	v := newFinish(t, ops.FlowKindFeature)
	v.SetKnown(featureKnown)

	v.logBtn.OnClick()
}
