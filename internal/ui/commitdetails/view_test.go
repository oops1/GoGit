package commitdetails

import (
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
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
	return NewView(widget.NewTabControl())
}

func id(text string) hash.ObjectID { return hash.SumSHA1("commit", []byte(text)) }

func details() Details {
	at := time.Unix(1700000000, 0).UTC()
	return Details{
		Commit:    id("head"),
		Parents:   []hash.ObjectID{id("parent")},
		Author:    "ann",
		AuthorAt:  at,
		Committer: "bob",
		CommitAt:  at.Add(time.Minute),
		Message:   "a subject\n\nand a body\n",
		Branches:  []string{"main", "topic"},
		Tags:      []string{"v1"},
		Changes: []Change{
			{Status: "modified", Path: "f", Added: 2, Deleted: 1},
			{Status: "renamed", Path: "moved", Old: "f"},
		},
		Files: []File{{Path: "f", Size: 12}, {Path: "dir/g", Size: 3}},
	}
}

func TestAFreshPanelInvitesYouToPickACommit(t *testing.T) {
	v := newTestView(t)

	if v.info.Text() != i18n.T("Details.Empty") {
		t.Fatalf("info = %q", v.info.Text())
	}
	if v.tabs.TabHeader(0) != i18n.T("Details.Tab.Info") || v.tabs.TabHeader(2) != i18n.T("Details.Tab.Files") {
		t.Fatalf("tabs = %q, %q, %q", v.tabs.TabHeader(0), v.tabs.TabHeader(1), v.tabs.TabHeader(2))
	}
	if got := v.Details(); !got.Commit.IsZero() {
		t.Fatalf("details = %+v", got)
	}
}

func TestThePanelTellsWhoWroteTheCommit(t *testing.T) {
	v := newTestView(t)

	v.Show(details())

	info := v.info.Text()
	for _, want := range []string{
		i18n.Tf("Details.Info.Commit", id("head").String()),
		i18n.Tf("Details.Info.Author", "ann", when(details().AuthorAt)),
		i18n.Tf("Details.Info.Committer", "bob", when(details().CommitAt)),
		i18n.Tf("Details.Info.Parents", id("parent").String()[:shortLength]),
		i18n.Tf("Details.Info.Branches", "main, topic"),
		i18n.Tf("Details.Info.Tags", "v1"),
		"a subject",
		"and a body",
	} {
		if !strings.Contains(info, want) {
			t.Fatalf("info = %q, want %q in it", info, want)
		}
	}
}

func TestThePanelListsTheChangesAndTheFiles(t *testing.T) {
	v := newTestView(t)

	v.Show(details())

	if got := v.changes.Grid.ItemsSource().Count(); got != 2 {
		t.Fatalf("changes = %d", got)
	}
	if got := v.files.Grid.ItemsSource().Count(); got != 2 {
		t.Fatalf("files = %d", got)
	}
	if got := v.Details(); len(got.Changes) != 2 {
		t.Fatalf("details = %+v", got)
	}
}

func TestAFirstCommitWithoutParentsSaysNothingAboutThem(t *testing.T) {
	v := newTestView(t)
	model := details()
	model.Parents = nil
	model.Branches = nil
	model.Tags = nil

	v.Show(model)

	if strings.Contains(v.info.Text(), i18n.Tf("Details.Info.Parents", "")) {
		t.Fatalf("info = %q", v.info.Text())
	}
}

func TestAnUnsetTimeIsLeftOut(t *testing.T) {
	if got := when(time.Time{}); got != "" {
		t.Fatalf("when = %q", got)
	}
}

func TestARenamedFileShowsBothNames(t *testing.T) {
	if got := pathOf(Change{Path: "moved", Old: "f"}); got != "f → moved" {
		t.Fatalf("path = %q", got)
	}
	if got := pathOf(Change{Path: "f"}); got != "f" {
		t.Fatalf("path = %q", got)
	}
}

func TestAPanelWithMoreFilesSaysSo(t *testing.T) {
	v := newTestView(t)
	model := details()
	model.MoreFiles = true

	v.Show(model)

	if got := v.files.Grid.ItemsSource().Count(); got != 3 {
		t.Fatalf("files = %d", got)
	}
}

func TestShowingAnEmptyCommitClearsThePanel(t *testing.T) {
	v := newTestView(t)
	v.Show(details())

	v.Show(Details{})

	if v.info.Text() != i18n.T("Details.Empty") || !v.Details().Commit.IsZero() {
		t.Fatalf("info = %q", v.info.Text())
	}
}

func TestThePanelFollowsTheLanguage(t *testing.T) {
	v := newTestView(t)
	v.Show(details())

	i18n.Apply("ru")
	t.Cleanup(func() { i18n.Apply("en") })
	v.Retitle()

	if v.tabs.TabHeader(0) != i18n.T("Details.Tab.Info") {
		t.Fatalf("tab = %q", v.tabs.TabHeader(0))
	}
	if !strings.Contains(v.info.Text(), i18n.Tf("Details.Info.Branches", "main, topic")) {
		t.Fatalf("info = %q", v.info.Text())
	}
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
