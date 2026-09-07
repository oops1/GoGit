package settings

import (
	"strconv"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

const (
	gitAdvancedRow    = 15
	credentialFormRow = 16
	sshFormRow        = 7
)

type searchField struct {
	section    string
	grid       *widget.Grid
	rows       []int
	labelKey   string
	hintKey    string
	groupKey   string
	value      func() string
	isAdvanced bool
}

type searchGroupSpec struct {
	grid         *widget.Grid
	rows         []int
	fieldIndexes []int
}

type searchSectionState struct {
	id           string
	grid         *widget.Grid
	originalRows []widget.GridDefinition
	emptyLabel   *searchEmptyLabel
	emptyRow     int
}

type searchEmptyLabel struct {
	*widget.Label
}

func newSearchEmptyLabel() *searchEmptyLabel {
	lbl := widget.NewLabel(i18n.T("Dialog.Settings.Search.Empty"), widget.CurrentTheme().SecondaryText)
	lbl.TextAlign = widget.TextAlignCenter
	return &searchEmptyLabel{Label: lbl}
}

func (l *searchEmptyLabel) ApplyTheme(t *widget.Theme) {
	l.TextColor = t.SecondaryText
}

func (v *View) logMaxCountText() string   { return strconv.Itoa(int(v.logMaxCount.Value())) }
func (v *View) fetchIntervalText() string { return strconv.Itoa(int(v.fetchInterval.Value())) }
func (v *View) workTreeDepthText() string { return strconv.Itoa(int(v.workTreeDepth.Value())) }
func (v *View) shallowDepthText() string  { return strconv.Itoa(int(v.shallowDepth.Value())) }

func (v *View) buildSearchIndex() {
	v.searchFields = []searchField{
		{section: "general", grid: v.sectionGeneral, rows: []int{3, 4},
			labelKey: "Dialog.Settings.Language", hintKey: "Dialog.Settings.Language.Hint",
			groupKey: "Dialog.Settings.Group.LanguageAppearance", value: v.language.SelectedText},
		{section: "general", grid: v.sectionGeneral, rows: []int{5, 6},
			labelKey: "Dialog.Settings.Theme", hintKey: "Dialog.Settings.Theme.Hint",
			groupKey: "Dialog.Settings.Group.LanguageAppearance", value: v.theme.SelectedText},
		{section: "general", grid: v.sectionGeneral, rows: []int{12, 13, 14},
			labelKey: "Dialog.Settings.ShowToolbar", hintKey: "Dialog.Settings.ShowToolbar.Hint",
			groupKey: "Dialog.Settings.Group.Interface"},
		{section: "general", grid: v.sectionGeneral, rows: []int{15, 16, 17},
			labelKey: "Dialog.Settings.ShowStatusBar", hintKey: "Dialog.Settings.ShowStatusBar.Hint",
			groupKey: "Dialog.Settings.Group.Interface"},
		{section: "general", grid: v.sectionGeneral, rows: []int{18, 19, 20},
			labelKey: "Dialog.Settings.ToolbarCaptions", hintKey: "Dialog.Settings.ToolbarCaptions.Hint",
			groupKey: "Dialog.Settings.Group.Interface"},
		{section: "general", grid: v.sectionGeneral, rows: []int{21, 22},
			labelKey: "Dialog.Settings.JournalFullAuthorName", hintKey: "Dialog.Settings.JournalFullAuthorName.Hint",
			groupKey: "Dialog.Settings.Group.Interface"},

		{section: "git", grid: v.sectionGit, rows: []int{3, 4},
			labelKey: "Dialog.Settings.LogMaxCount", hintKey: "Dialog.Settings.LogMaxCount.Hint",
			groupKey: "Dialog.Settings.Group.FetchSync", value: v.logMaxCountText},
		{section: "git", grid: v.sectionGit, rows: []int{5, 6, 7},
			labelKey: "Dialog.Settings.AutoFetch", hintKey: "Dialog.Settings.AutoFetch.Hint",
			groupKey: "Dialog.Settings.Group.FetchSync"},
		{section: "git", grid: v.sectionGit, rows: []int{8, 9},
			labelKey: "Dialog.Settings.FetchInterval", hintKey: "Dialog.Settings.FetchInterval.Hint",
			groupKey: "Dialog.Settings.Group.FetchSync", value: v.fetchIntervalText},
		{section: "git", grid: v.sectionGit, rows: []int{10, 11},
			labelKey: "Dialog.Settings.DefaultRemote", hintKey: "Dialog.Settings.DefaultRemote.Hint",
			groupKey: "Dialog.Settings.Group.FetchSync", value: v.defaultRemote.GetText},
		{section: "git", grid: v.sectionGit, rows: []int{12, 13, 14},
			labelKey: "Dialog.Settings.PruneOnFetch", hintKey: "Dialog.Settings.PruneOnFetch.Hint",
			groupKey: "Dialog.Settings.Group.FetchSync"},

		{section: "git", grid: v.gitAdvancedContent, rows: []int{0, 1},
			labelKey: "Dialog.Settings.WorkTreeDepth", hintKey: "Dialog.Settings.WorkTreeDepth.Hint",
			groupKey: "Dialog.Settings.Group.Advanced", value: v.workTreeDepthText, isAdvanced: true},
		{section: "git", grid: v.gitAdvancedContent, rows: []int{2, 3},
			labelKey: "Dialog.Settings.PullStrategy", hintKey: "Dialog.Settings.PullStrategy.Hint",
			groupKey: "Dialog.Settings.Group.Advanced", value: v.pullStrategy.GetText, isAdvanced: true},
		{section: "git", grid: v.gitAdvancedContent, rows: []int{4, 5},
			labelKey: "Dialog.Settings.ShallowDepth", hintKey: "Dialog.Settings.ShallowDepth.Hint",
			groupKey: "Dialog.Settings.Group.Advanced", value: v.shallowDepthText, isAdvanced: true},

		{section: "credentials", grid: v.sectionCredentials, rows: []int{0, 1, 2, 3, 4, 5, 6, 7, 8},
			labelKey: "Dialog.Settings.CredentialSource", value: v.credentialSource.SelectedText},
	}

	groupRowsByGrid := []struct {
		grid *widget.Grid
		rows map[string][]int
	}{
		{v.sectionGeneral, map[string][]int{
			"Dialog.Settings.Group.LanguageAppearance": {0, 1, 2, 6, 7, 8},
			"Dialog.Settings.Group.Interface":          {9, 10, 11},
		}},
		{v.sectionGit, map[string][]int{
			"Dialog.Settings.Group.FetchSync": {0, 1, 2},
		}},
	}

	var groups []searchGroupSpec
	for _, gb := range groupRowsByGrid {
		groupIndex := map[string]int{}
		for i, f := range v.searchFields {
			if f.grid != gb.grid {
				continue
			}
			if gi, exists := groupIndex[f.groupKey]; exists {
				groups[gi].fieldIndexes = append(groups[gi].fieldIndexes, i)
				continue
			}
			groupIndex[f.groupKey] = len(groups)
			groups = append(groups, searchGroupSpec{grid: gb.grid, rows: gb.rows[f.groupKey], fieldIndexes: []int{i}})
		}
	}
	v.searchGroups = groups
}

func (v *View) buildSearchSections() {
	v.searchSections = []*searchSectionState{
		v.newSearchSection("general", v.sectionGeneral),
		v.newSearchSection("git", v.sectionGit),
		v.newSearchSection("credentials", v.sectionCredentials),
		v.newSearchSection("ssh", v.sectionSSH),
	}
	v.searchAdvancedOriginalRows = append([]widget.GridDefinition(nil), v.gitAdvancedContent.RowDefs...)
}

func (v *View) newSearchSection(id string, grid *widget.Grid) *searchSectionState {
	label := newSearchEmptyLabel()
	emptyRow := len(grid.RowDefs)
	label.SetGridProps(emptyRow, 0, 1, grid.Cols())
	label.SetVisible(false)
	grid.AddChild(label)
	grid.RowDefs = append(grid.RowDefs, widget.GridDefinition{Mode: widget.GridSizeStar, Value: 0})
	grid.SetBounds(grid.Bounds())
	return &searchSectionState{
		id:           id,
		grid:         grid,
		originalRows: append([]widget.GridDefinition(nil), grid.RowDefs...),
		emptyLabel:   label,
		emptyRow:     emptyRow,
	}
}

func matchesSearchField(query string, f searchField) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(i18n.T(f.labelKey)), query) {
		return true
	}
	if f.hintKey != "" && strings.Contains(strings.ToLower(i18n.T(f.hintKey)), query) {
		return true
	}
	if f.groupKey != "" && strings.Contains(strings.ToLower(i18n.T(f.groupKey)), query) {
		return true
	}
	if f.value != nil && strings.Contains(strings.ToLower(f.value()), query) {
		return true
	}
	return false
}

func (v *View) applySearch(text string) {
	query := strings.ToLower(strings.TrimSpace(text))
	searching := query != ""
	if searching && !v.searchActive {
		v.searchAdvancedExpandedBefore = v.gitAdvanced.IsExpanded
	}
	v.searchActive = searching

	matchedField := make([]bool, len(v.searchFields))
	sectionFieldMatches := map[string]int{}
	advancedMatches := 0
	for i, f := range v.searchFields {
		matched := matchesSearchField(query, f)
		matchedField[i] = matched
		if matched {
			sectionFieldMatches[f.section]++
			if f.isAdvanced {
				advancedMatches++
			}
		}
	}

	hiddenRows := map[*widget.Grid]map[int]bool{}
	hide := func(grid *widget.Grid, rows []int) {
		set := hiddenRows[grid]
		if set == nil {
			set = map[int]bool{}
			hiddenRows[grid] = set
		}
		for _, r := range rows {
			set[r] = true
		}
	}

	if searching {
		for i, f := range v.searchFields {
			if !matchedField[i] {
				hide(f.grid, f.rows)
			}
		}
		for _, g := range v.searchGroups {
			matched := false
			for _, idx := range g.fieldIndexes {
				if matchedField[idx] {
					matched = true
					break
				}
			}
			if !matched {
				hide(g.grid, g.rows)
			}
		}
		if advancedMatches == 0 {
			hide(v.sectionGit, []int{gitAdvancedRow})
		}
		v.gitAdvanced.SetExpanded(advancedMatches > 0)
	} else {
		v.gitAdvanced.SetExpanded(v.searchAdvancedExpandedBefore)
	}

	matchedCredentialRows := v.filterCredentialsTable(query)
	matchedKeyRows := v.filterSSHTable(query)

	sectionEmpty := map[string]bool{
		"general":     searching && sectionFieldMatches["general"] == 0,
		"git":         searching && sectionFieldMatches["git"] == 0,
		"credentials": searching && sectionFieldMatches["credentials"] == 0 && matchedCredentialRows == 0,
		"ssh":         searching && matchedKeyRows == 0,
	}

	for _, s := range v.searchSections {
		v.applySearchSection(s, hiddenRows[s.grid], sectionEmpty[s.id])
	}
	v.applyAdvancedContentRows(hiddenRows[v.gitAdvancedContent])

	counts := map[string]int{
		"general":     sectionFieldMatches["general"],
		"git":         sectionFieldMatches["git"],
		"credentials": sectionFieldMatches["credentials"] + matchedCredentialRows,
		"ssh":         matchedKeyRows,
	}
	v.updateNavMatchCounts(counts, searching)
}

func (v *View) applySearchSection(s *searchSectionState, hidden map[int]bool, empty bool) {
	defs := make([]widget.GridDefinition, len(s.originalRows))
	copy(defs, s.originalRows)
	for i := range defs {
		if i == s.emptyRow {
			continue
		}
		if empty || hidden[i] {
			defs[i].Value = 0
		}
	}
	if empty {
		defs[s.emptyRow].Value = 1
	}
	s.grid.RowDefs = defs
	s.grid.SetBounds(s.grid.Bounds())
	s.emptyLabel.SetVisible(empty)
	hideRowContents(s.grid, hidden, empty, s.emptyLabel)
	v.syncScroll()
}

func (v *View) applyAdvancedContentRows(hidden map[int]bool) {
	defs := make([]widget.GridDefinition, len(v.searchAdvancedOriginalRows))
	copy(defs, v.searchAdvancedOriginalRows)
	for i := range defs {
		if hidden[i] {
			defs[i].Value = 0
		}
	}
	v.gitAdvancedContent.RowDefs = defs
	v.gitAdvancedContent.SetBounds(v.gitAdvancedContent.Bounds())
	hideRowContents(v.gitAdvancedContent, hidden, false, nil)
}

func (v *View) filterCredentialsTable(query string) int {
	entries := v.credentials
	if query != "" {
		filtered := make([]SecretEntry, 0, len(entries))
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Resource), query) ||
				strings.Contains(strings.ToLower(e.Username), query) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	items := make([]interface{}, len(entries))
	for i, e := range entries {
		items[i] = e
	}
	v.credentialsTable.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.restoreCredentialSelectionAfterFilter(entries)
	return len(entries)
}

func (v *View) restoreCredentialSelectionAfterFilter(entries []SecretEntry) {
	if v.hasCredSelection {
		for i, e := range entries {
			if e.Resource == v.selectedResource {
				v.credentialsTable.Grid.SetSelectedIndex(i)
				return
			}
		}
	}
	v.updateCredentialSelection("", false)
}

func (v *View) filterSSHTable(query string) int {
	entries := v.keys
	if query != "" {
		filtered := make([]KeyEntry, 0, len(entries))
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Host), query) ||
				strings.Contains(strings.ToLower(e.Path), query) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	items := make([]interface{}, len(entries))
	for i, e := range entries {
		items[i] = e
	}
	v.sshTable.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.restoreKeySelectionAfterFilter(entries)
	return len(entries)
}

func (v *View) restoreKeySelectionAfterFilter(entries []KeyEntry) {
	if v.hasKeySelection {
		for i, e := range entries {
			if e.Host == v.selectedHost {
				v.sshTable.Grid.SetSelectedIndex(i)
				return
			}
		}
	}
	v.updateKeySelection("", false)
}

func (v *View) updateNavMatchCounts(counts map[string]int, searching bool) {
	for i, s := range sectionOrder {
		id := s.id
		base := i18n.T(navKeyFor(id))
		if searching && counts[id] > 0 {
			v.setNavCaption(i, i18n.Tf("Dialog.Settings.Nav.MatchCount", base, counts[id]))
			continue
		}
		v.setNavCaption(i, base)
	}
}

func navKeyFor(id string) string {
	for _, s := range sectionOrder {
		if s.id == id {
			return s.navKey
		}
	}
	return ""
}

type gridChild interface {
	GetGridRow() int
	GetGridRowSpan() int
	SetVisible(bool)
}

func hideRowContents(grid *widget.Grid, hidden map[int]bool, empty bool, skip widget.Widget) {
	for _, child := range grid.Children() {
		placed, ok := child.(gridChild)
		if !ok || child == skip {
			continue
		}
		placed.SetVisible(!empty && !spansHiddenRow(placed, hidden))
	}
}

func spansHiddenRow(placed gridChild, hidden map[int]bool) bool {
	first := placed.GetGridRow()
	for row := first; row < first+placed.GetGridRowSpan(); row++ {
		if hidden[row] {
			return true
		}
	}
	return false
}
