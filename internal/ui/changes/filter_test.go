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
