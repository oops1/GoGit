package settings

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

func findSearchSection(v *View, id string) *searchSectionState {
	for _, s := range v.searchSections {
		if s.id == id {
			return s
		}
	}
	return nil
}

func navHeaderFor(v *View, id string) string {
	for i, s := range sectionOrder {
		if s.id == id {
			return v.nav.Caption(i)
		}
	}
	return ""
}

func TestSearchMatchesByFieldLabel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("journal")

	if v.sectionGeneral.RowDefs[19].Value == 0 {
		t.Fatal("journalFullAuthorName row must stay visible: its label matches")
	}
	if v.sectionGeneral.RowDefs[2].Value != 0 {
		t.Fatal("language row must collapse: nothing about it matches")
	}
	if v.sectionGeneral.RowDefs[4].Value != 0 {
		t.Fatal("theme row must collapse: nothing about it matches")
	}
}

func TestSearchMatchesByFieldHint(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")

	v.applySearch("periodically")

	if v.sectionGit.RowDefs[4].Value == 0 {
		t.Fatal("autoFetch row must stay visible: its hint matches")
	}
	if v.sectionGit.RowDefs[2].Value != 0 {
		t.Fatal("logMaxCount row must collapse: nothing about it matches")
	}
}

func TestSearchMatchesByGroupName(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")

	v.applySearch("sync")

	fetchSyncFieldRows := [][]int{{2}, {4, 5}, {7}, {9}, {11, 12}}
	for _, rows := range fetchSyncFieldRows {
		for _, r := range rows {
			if v.sectionGit.RowDefs[r].Value == 0 {
				t.Fatalf("row %d must stay visible: the query matches its group name", r)
			}
		}
	}
	if v.sectionGit.RowDefs[0].Value == 0 {
		t.Fatal("the Fetch & sync group header must stay visible")
	}
	if v.sectionGit.RowDefs[gitAdvancedStackRow].Value != 0 {
		t.Fatal("the advanced group must collapse: the query does not reach it")
	}
}

func TestSearchMatchesByFieldValue(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")
	v.defaultRemote.SetText("upstream-mirror")

	v.applySearch("mirror")

	if v.sectionGit.RowDefs[9].Value == 0 {
		t.Fatal("defaultRemote row must stay visible: its current value matches")
	}
	if v.sectionGit.RowDefs[2].Value != 0 {
		t.Fatal("logMaxCount row must collapse: nothing about it matches")
	}
}

func TestSearchMatchesByNumericFieldValue(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")
	v.logMaxCount.SetValue(4242)

	v.applySearch("4242")

	if v.sectionGit.RowDefs[2].Value == 0 {
		t.Fatal("logMaxCount row must stay visible: its current value matches")
	}
	if v.sectionGit.RowDefs[4].Value != 0 {
		t.Fatal("autoFetch row must collapse: nothing about it matches")
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("JOURNAL")

	if v.sectionGeneral.RowDefs[19].Value == 0 {
		t.Fatal("uppercase query must still match the lowercase label text")
	}
}

func TestSearchFiltersCredentialsTableRows(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetCredentials([]SecretEntry{
		{Resource: "github.com", Username: "alice"},
		{Resource: "gitlab.com", Username: "bob"},
	})

	v.applySearch("alice")

	src := v.credentialsTable.Grid.ItemsSource()
	if src == nil || src.Count() != 1 {
		t.Fatalf("expected exactly 1 filtered row, got %v", src)
	}
	entry := src.Get(0).(SecretEntry)
	if entry.Resource != "github.com" {
		t.Fatalf("filtered row = %+v, want github.com", entry)
	}
}

func TestSearchKeepsTheSelectedCredentialRowWhenItStillMatches(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetCredentials([]SecretEntry{
		{Resource: "github.com", Username: "alice"},
		{Resource: "gitlab.com", Username: "bob"},
	})
	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	v.applySearch("git")

	if !v.hasCredSelection || v.selectedResource != "github.com" {
		t.Fatalf("selection lost while filtering to a set that still contains it: hasSelection=%v resource=%q",
			v.hasCredSelection, v.selectedResource)
	}
	if !v.credentialRemoveBtn.IsEnabled() || !v.credentialEditBtn.IsEnabled() {
		t.Fatal("edit and remove must stay enabled while the selected row is still visible")
	}
	if got, _ := v.credentialsTable.Grid.SelectedItem().(SecretEntry); got.Resource != "github.com" {
		t.Fatalf("grid selection = %+v, want github.com still highlighted", got)
	}
}

func TestSearchDropsTheSelectedCredentialRowOnceItIsFilteredOut(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetCredentials([]SecretEntry{
		{Resource: "github.com", Username: "alice"},
		{Resource: "gitlab.com", Username: "bob"},
	})
	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	v.applySearch("gitlab")

	if v.hasCredSelection || v.credentialRemoveBtn.IsEnabled() || v.credentialEditBtn.IsEnabled() {
		t.Fatal("selection must clear once the selected row no longer matches the query")
	}
}

func TestSearchKeepsTheSelectedSSHRowWhenItStillMatches(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetKeys([]KeyEntry{
		{Host: "github.com", Path: "/keys/a"},
		{Host: "gitlab.com", Path: "/keys/b"},
	})
	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	v.applySearch("git")

	if !v.hasKeySelection || v.selectedHost != "github.com" {
		t.Fatalf("selection lost while filtering to a set that still contains it: hasSelection=%v host=%q",
			v.hasKeySelection, v.selectedHost)
	}
	if !v.sshRemoveBtn.IsEnabled() || !v.sshEditBtn.IsEnabled() {
		t.Fatal("edit and remove must stay enabled while the selected row is still visible")
	}
}

func TestSearchDropsTheSelectedSSHRowOnceItIsFilteredOut(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetKeys([]KeyEntry{
		{Host: "github.com", Path: "/keys/a"},
		{Host: "gitlab.com", Path: "/keys/b"},
	})
	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	v.applySearch("gitlab")

	if v.hasKeySelection || v.sshRemoveBtn.IsEnabled() || v.sshEditBtn.IsEnabled() {
		t.Fatal("selection must clear once the selected row no longer matches the query")
	}
}

func TestSearchFiltersCredentialsTableRowsByResource(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetCredentials([]SecretEntry{
		{Resource: "github.com", Username: "alice"},
		{Resource: "gitlab.com", Username: "bob"},
	})

	v.applySearch("gitlab")

	src := v.credentialsTable.Grid.ItemsSource()
	if src == nil || src.Count() != 1 {
		t.Fatalf("expected exactly 1 filtered row, got %v", src)
	}
	if entry := src.Get(0).(SecretEntry); entry.Username != "bob" {
		t.Fatalf("filtered row = %+v, want bob", entry)
	}
}

func TestSearchFiltersSSHTableRowsByHostAndKeyFile(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetKeys([]KeyEntry{
		{Host: "bitbucket.org", Path: "/home/me/.ssh/id_ed25519"},
		{Host: "*", Path: "/home/me/.ssh/id_rsa"},
	})

	v.applySearch("ed25519")

	src := v.sshTable.Grid.ItemsSource()
	if src == nil || src.Count() != 1 {
		t.Fatalf("expected exactly 1 filtered row, got %v", src)
	}
	if entry := src.Get(0).(KeyEntry); entry.Host != "bitbucket.org" {
		t.Fatalf("filtered row = %+v, want bitbucket.org", entry)
	}

	v.applySearch("bitbucket")
	src = v.sshTable.Grid.ItemsSource()
	if src == nil || src.Count() != 1 {
		t.Fatalf("expected exactly 1 filtered row by host, got %v", src)
	}
}

func TestSearchWithNoMatchesShowsEmptyMessageInsteadOfContent(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("zzz-nothing-matches-zzz")

	general := findSearchSection(v, "general")
	if general == nil || !general.emptyLabel.IsVisible() {
		t.Fatal("the general section must show the empty-result message")
	}
	if got, want := general.emptyLabel.Text(), i18n.T("Dialog.Settings.Search.Empty"); got != want {
		t.Fatalf("empty message = %q, want %q", got, want)
	}
	for i, def := range v.sectionGeneral.RowDefs {
		if i == general.emptyRow {
			continue
		}
		if def.Value != 0 {
			t.Fatalf("row %d must collapse when the section has no matches", i)
		}
	}
}

func TestSearchEmptyMessageClearsWhenAMatchAppearsInAnotherSection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("journal")

	ssh := findSearchSection(v, "ssh")
	if ssh == nil || !ssh.emptyLabel.IsVisible() {
		t.Fatal("the ssh section has no fields and no rows: it must show the empty message")
	}
	general := findSearchSection(v, "general")
	if general == nil || general.emptyLabel.IsVisible() {
		t.Fatal("the general section has a match: it must not show the empty message")
	}
}

func TestClearingSearchRestoresOriginalRowHeights(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	before := append([]widget.GridDefinition(nil), v.sectionGeneral.RowDefs...)

	v.applySearch("journal")
	v.applySearch("")

	after := v.sectionGeneral.RowDefs
	if len(before) != len(after) {
		t.Fatalf("row count changed: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("row %d = %+v after clearing, want %+v", i, after[i], before[i])
		}
	}
}

func TestClearingSearchRestoresTheAdvancedGroupCollapsedState(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")
	if v.gitAdvanced.IsExpanded {
		t.Fatal("advanced settings must start collapsed")
	}

	v.applySearch("shallow")
	if !v.gitAdvanced.IsExpanded {
		t.Fatal("a match inside the advanced group must expand it while searching")
	}

	v.applySearch("")
	if v.gitAdvanced.IsExpanded {
		t.Fatal("clearing the search must restore the pre-search collapsed state")
	}
}

func TestClearingSearchRestoresAPreviouslyExpandedAdvancedGroup(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")
	v.gitAdvanced.SetExpanded(true)

	v.applySearch("toolbar")
	if v.gitAdvanced.IsExpanded {
		t.Fatal("no match inside the advanced group while searching must collapse it")
	}

	v.applySearch("")
	if !v.gitAdvanced.IsExpanded {
		t.Fatal("clearing the search must restore the pre-search expanded state")
	}
}

func TestSearchDoesNotMarkTheDialogAsModified(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("journal")
	v.applySearch("zzz-nothing-matches-zzz")
	v.applySearch("")

	if v.Modified() {
		t.Fatal("filtering must not be treated as an edit")
	}
	if v.okBtn.IsEnabled() {
		t.Fatal("save must stay disabled after searching")
	}
}

func TestSearchAnnotatesNavigationWithMatchesInOtherSections(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("sync")

	want := i18n.Tf("Dialog.Settings.Nav.MatchCount", i18n.T("Dialog.Settings.Nav.Git"), 5)
	if got := navHeaderFor(v, "git"); got != want {
		t.Fatalf("git nav header = %q, want %q", got, want)
	}
	if got, want := navHeaderFor(v, "general"), i18n.T("Dialog.Settings.Nav.General"); got != want {
		t.Fatalf("general nav header = %q, want %q (no matches)", got, want)
	}
}

func TestSearchNavigationCountIncludesTableRowMatches(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetCredentials([]SecretEntry{
		{Resource: "github.com", Username: "alice"},
		{Resource: "gitlab.com", Username: "alice"},
	})

	v.applySearch("alice")

	want := i18n.Tf("Dialog.Settings.Nav.MatchCount", i18n.T("Dialog.Settings.Nav.Credentials"), 2)
	if got := navHeaderFor(v, "credentials"); got != want {
		t.Fatalf("credentials nav header = %q, want %q", got, want)
	}
}

func TestSearchEmptyLabelApplyThemeAlwaysUsesSecondaryTextNotLabelOrPlaceholder(t *testing.T) {
	l := newSearchEmptyLabel()

	secondary := color.RGBA{R: 11, G: 22, B: 33, A: 255}
	theme := &widget.Theme{
		SecondaryText:    secondary,
		LabelText:        color.RGBA{R: 200, G: 200, B: 200, A: 255},
		InputPlaceholder: color.RGBA{R: 100, G: 100, B: 100, A: 255},
	}

	l.ApplyTheme(theme)

	if l.TextColor != secondary {
		t.Fatalf("TextColor = %+v, want SecondaryText %+v", l.TextColor, secondary)
	}
}

func TestNavKeyForReturnsEmptyForUnknownID(t *testing.T) {
	if got := navKeyFor("bogus"); got != "" {
		t.Fatalf("navKeyFor(bogus) = %q, want empty", got)
	}
	for _, s := range sectionOrder {
		if navKeyFor(s.id) != s.navKey {
			t.Fatalf("navKeyFor(%q) = %q, want %q", s.id, navKeyFor(s.id), s.navKey)
		}
	}
}

func TestClearingSearchRestoresPlainNavigationLabels(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("sync")
	v.applySearch("")

	for _, s := range sectionOrder {
		if got, want := navHeaderFor(v, s.id), i18n.T(s.navKey); got != want {
			t.Fatalf("%s nav header = %q, want %q after clearing", s.id, got, want)
		}
	}
}
