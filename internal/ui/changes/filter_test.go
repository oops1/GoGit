package changes

import (
	"slices"
	"testing"
)

func namesOf(rows []Row) []string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	return names
}

func TestFilterRowsReturnsAllRowsForAnEmptyQuery(t *testing.T) {
	rows := []Row{{Name: "main.go", RelPath: "src/main.go"}, {Name: "readme.md", RelPath: "readme.md"}}
	got := FilterRows(rows, "   ")
	if !slices.Equal(namesOf(got), namesOf(rows)) {
		t.Fatalf("FilterRows(empty) = %v, want all rows", namesOf(got))
	}
}

func TestFilterRowsMatchesNameCaseInsensitively(t *testing.T) {
	rows := []Row{{Name: "Main.go", RelPath: "src/Main.go"}, {Name: "readme.md", RelPath: "readme.md"}}
	got := FilterRows(rows, "MAIN")
	if !slices.Equal(namesOf(got), []string{"Main.go"}) {
		t.Fatalf("FilterRows(MAIN) = %v, want [Main.go]", namesOf(got))
	}
}

func TestFilterRowsMatchesPathWhenNameDoesNotMatch(t *testing.T) {
	rows := []Row{{Name: "main.go", RelPath: "src/pkg/main.go"}, {Name: "other.go", RelPath: "other/other.go"}}
	got := FilterRows(rows, "pkg")
	if !slices.Equal(namesOf(got), []string{"main.go"}) {
		t.Fatalf("FilterRows(pkg) = %v, want [main.go]", namesOf(got))
	}
}

func TestFilterRowsExcludesNonMatchingRows(t *testing.T) {
	rows := []Row{{Name: "main.go", RelPath: "src/main.go"}, {Name: "readme.md", RelPath: "readme.md"}}
	got := FilterRows(rows, "zzz")
	if len(got) != 0 {
		t.Fatalf("FilterRows(zzz) = %v, want empty", namesOf(got))
	}
}

func allowAll() map[StatusFilter]bool {
	return AllowedStatusFilters(nil)
}

func TestFilterRowsByStatusKeepsEverythingWhenAllStatusesAllowedAndQueryIsEmpty(t *testing.T) {
	rows := []Row{
		{Name: "main.go", Status: RowModified},
		{Name: "new.go", Status: RowAdded},
		{Name: "readme.md", Status: RowUnchanged},
	}
	got := FilterRowsByStatus(rows, "", allowAll())
	if !slices.Equal(namesOf(got), namesOf(rows)) {
		t.Fatalf("FilterRowsByStatus(all allowed) = %v, want all rows", namesOf(got))
	}
}

func TestFilterRowsByStatusHidesRowsOfADisabledStatus(t *testing.T) {
	rows := []Row{
		{Name: "main.go", Status: RowModified},
		{Name: "new.go", Status: RowAdded},
	}
	allowed := allowAll()
	allowed[FilterAdded] = false
	got := FilterRowsByStatus(rows, "", allowed)
	if !slices.Equal(namesOf(got), []string{"main.go"}) {
		t.Fatalf("FilterRowsByStatus(added disabled) = %v, want [main.go]", namesOf(got))
	}
}

func TestFilterRowsByStatusHidesRowsWithoutAKnownStatusOnlyWhenTheirStatusIsDisabled(t *testing.T) {
	rows := []Row{{Name: "truncated"}}
	allowed := map[StatusFilter]bool{}
	got := FilterRowsByStatus(rows, "", allowed)
	if !slices.Equal(namesOf(got), []string{"truncated"}) {
		t.Fatalf("FilterRowsByStatus(no statuses allowed) = %v, want [truncated] to stay visible", namesOf(got))
	}
}

func TestFilterRowsByStatusHidesAStagedRowWhenStagedIsDisabledEvenIfItsStatusIsAllowed(t *testing.T) {
	rows := []Row{{Name: "staged.go", Status: RowAdded, IndexState: "Added"}}
	allowed := allowAll()
	allowed[FilterStaged] = false
	got := FilterRowsByStatus(rows, "", allowed)
	if len(got) != 0 {
		t.Fatalf("FilterRowsByStatus(staged disabled) = %v, want empty", namesOf(got))
	}
}

func TestFilterRowsByStatusKeepsAnUnstagedRowWhenStagedIsDisabled(t *testing.T) {
	rows := []Row{{Name: "unstaged.go", Status: RowModified}}
	allowed := allowAll()
	allowed[FilterStaged] = false
	got := FilterRowsByStatus(rows, "", allowed)
	if !slices.Equal(namesOf(got), []string{"unstaged.go"}) {
		t.Fatalf("FilterRowsByStatus(staged disabled, unstaged row) = %v, want [unstaged.go]", namesOf(got))
	}
}

func TestFilterRowsByStatusKeepsAConflictRowVisibleWhenStagedIsDisabledEvenIfItsIndexStateIsSet(t *testing.T) {
	rows := []Row{{Name: "conflict.go", Status: RowConflict, IndexState: "Conflict"}}
	allowed := allowAll()
	allowed[FilterStaged] = false
	got := FilterRowsByStatus(rows, "", allowed)
	if !slices.Equal(namesOf(got), []string{"conflict.go"}) {
		t.Fatalf("FilterRowsByStatus(staged disabled, conflict row) = %v, want [conflict.go]", namesOf(got))
	}
}

func TestFilterRowsByStatusCombinesTextAndStatusFiltering(t *testing.T) {
	rows := []Row{
		{Name: "main.go", RelPath: "src/main.go", Status: RowModified},
		{Name: "main_test.go", RelPath: "src/main_test.go", Status: RowAdded},
		{Name: "readme.md", RelPath: "readme.md", Status: RowModified},
	}
	allowed := allowAll()
	allowed[FilterAdded] = false
	got := FilterRowsByStatus(rows, "main", allowed)
	if !slices.Equal(namesOf(got), []string{"main.go"}) {
		t.Fatalf("FilterRowsByStatus(main, added disabled) = %v, want [main.go]", namesOf(got))
	}
}

func TestFilterRowsByStatusMapsEachRowStatusToItsFilter(t *testing.T) {
	cases := []struct {
		status RowStatus
		filter StatusFilter
	}{
		{RowModified, FilterModified},
		{RowTypeChanged, FilterModified},
		{RowAdded, FilterAdded},
		{RowDeleted, FilterDeleted},
		{RowRenamed, FilterRenamed},
		{RowCopied, FilterRenamed},
		{RowUntracked, FilterUntracked},
		{RowIgnored, FilterIgnored},
		{RowConflict, FilterConflict},
		{RowUnchanged, FilterUnchanged},
	}
	for _, c := range cases {
		rows := []Row{{Name: "f", Status: c.status}}
		allowed := allowAll()
		allowed[c.filter] = false
		if got := FilterRowsByStatus(rows, "", allowed); len(got) != 0 {
			t.Fatalf("status %v: FilterRowsByStatus(%v disabled) = %v, want empty", c.status, c.filter, namesOf(got))
		}
	}
}

func TestNormalizeStatusFiltersDropsUnknownNamesAndDuplicates(t *testing.T) {
	got := NormalizeStatusFilters([]string{"modified", "MODIFIED", " added ", "bogus", ""})
	want := []StatusFilter{FilterModified, FilterAdded}
	if !slices.Equal(got, want) {
		t.Fatalf("NormalizeStatusFilters = %v, want %v", got, want)
	}
}

func TestNormalizeStatusFiltersOfNilReturnsEmpty(t *testing.T) {
	got := NormalizeStatusFilters(nil)
	if len(got) != 0 {
		t.Fatalf("NormalizeStatusFilters(nil) = %v, want empty", got)
	}
}

func TestAllowedStatusFiltersDefaultsToAllTrueWhenNothingIsDisabled(t *testing.T) {
	allowed := AllowedStatusFilters(nil)
	for _, f := range AllStatusFilters {
		if !allowed[f] {
			t.Fatalf("AllowedStatusFilters(nil)[%v] = false, want true", f)
		}
	}
}

func TestAllowedStatusFiltersMarksDisabledEntriesFalse(t *testing.T) {
	allowed := AllowedStatusFilters([]StatusFilter{FilterIgnored, FilterUntracked})
	if allowed[FilterIgnored] || allowed[FilterUntracked] {
		t.Fatal("disabled filters must be false")
	}
	if !allowed[FilterModified] {
		t.Fatal("filters not in the disabled list must stay true")
	}
}

func TestDisabledStatusFiltersRoundTripsWithAllowedStatusFilters(t *testing.T) {
	disabled := []StatusFilter{FilterIgnored, FilterUntracked}
	want := []StatusFilter{FilterUntracked, FilterIgnored}
	got := DisabledStatusFilters(AllowedStatusFilters(disabled))
	if !slices.Equal(got, want) {
		t.Fatalf("DisabledStatusFilters round trip = %v, want %v", got, want)
	}
}

func TestFilterRowsByDirectoryReturnsAllRowsForAnEmptyDirectory(t *testing.T) {
	rows := []Row{{Name: "main.go", RelDir: "src"}, {Name: "readme.md", RelDir: ""}}
	got := FilterRowsByDirectory(rows, "")
	if !slices.Equal(namesOf(got), namesOf(rows)) {
		t.Fatalf("FilterRowsByDirectory(empty) = %v, want all rows", namesOf(got))
	}
}

func TestFilterRowsByDirectoryKeepsRowsDirectlyInsideTheDirectory(t *testing.T) {
	rows := []Row{
		{Name: "main.go", RelDir: "src"},
		{Name: "readme.md", RelDir: ""},
		{Name: "other.go", RelDir: "other"},
	}
	got := FilterRowsByDirectory(rows, "src")
	if !slices.Equal(namesOf(got), []string{"main.go"}) {
		t.Fatalf("FilterRowsByDirectory(src) = %v, want [main.go]", namesOf(got))
	}
}

func TestFilterRowsByDirectoryKeepsRowsInSubdirectories(t *testing.T) {
	rows := []Row{
		{Name: "main.go", RelDir: "src/pkg"},
		{Name: "readme.md", RelDir: ""},
	}
	got := FilterRowsByDirectory(rows, "src")
	if !slices.Equal(namesOf(got), []string{"main.go"}) {
		t.Fatalf("FilterRowsByDirectory(src) = %v, want [main.go] from the nested subdirectory", namesOf(got))
	}
}

func TestFilterRowsByDirectoryExcludesADirectoryWithTheSamePrefixButDifferentName(t *testing.T) {
	rows := []Row{{Name: "main.go", RelDir: "srcOther"}}
	got := FilterRowsByDirectory(rows, "src")
	if len(got) != 0 {
		t.Fatalf("FilterRowsByDirectory(src) = %v, want empty (srcOther is not a subdirectory of src)", namesOf(got))
	}
}

func TestFilterRowsByDirectoryExcludesRootFilesWhenADirectoryIsSelected(t *testing.T) {
	rows := []Row{{Name: "readme.md", RelDir: ""}}
	got := FilterRowsByDirectory(rows, "src")
	if len(got) != 0 {
		t.Fatalf("FilterRowsByDirectory(src) = %v, want root files excluded", namesOf(got))
	}
}

func TestDisabledStatusFiltersOfAllAllowedIsEmpty(t *testing.T) {
	got := DisabledStatusFilters(AllowedStatusFilters(nil))
	if len(got) != 0 {
		t.Fatalf("DisabledStatusFilters(all allowed) = %v, want empty", got)
	}
}

func TestFilterRowsByStatusShowsUnchangedRowsWhenTheFilterIsEnabled(t *testing.T) {
	rows := []Row{
		{Name: "main.go", Status: RowModified},
		{Name: "assets.go", Status: RowUnchanged},
	}
	got := FilterRowsByStatus(rows, "", allowAll())
	if !slices.Equal(namesOf(got), []string{"main.go", "assets.go"}) {
		t.Fatalf("FilterRowsByStatus(unchanged allowed) = %v, want both rows", namesOf(got))
	}
}

func TestFilterRowsByStatusHidesUnchangedRowsWhenTheFilterIsDisabled(t *testing.T) {
	rows := []Row{
		{Name: "main.go", Status: RowModified},
		{Name: "assets.go", Status: RowUnchanged},
	}
	allowed := allowAll()
	allowed[FilterUnchanged] = false
	got := FilterRowsByStatus(rows, "", allowed)
	if !slices.Equal(namesOf(got), []string{"main.go"}) {
		t.Fatalf("FilterRowsByStatus(unchanged disabled) = %v, want [main.go]", namesOf(got))
	}
}
