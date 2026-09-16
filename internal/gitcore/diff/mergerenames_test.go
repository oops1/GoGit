package diff

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestTreeRenamesReportTheLimitTheSearchNeeded(t *testing.T) {
	store := newMemoryStore()
	old, updated := treeFiles{}, treeFiles{}
	for at := range 3 {
		old[fmt.Sprintf("old%d.txt", at)] = blobSpec(poem(fmt.Sprintf("body %d", at)))
		updated[fmt.Sprintf("new%d.txt", at)] = blobSpec(poem(fmt.Sprintf("body %d", at)) + "tail\n")
	}
	oldTree, newTree := buildTree(store, old), buildTree(store, updated)

	limited, err := TreeRenames(t.Context(), store, oldTree, newTree, RenameSearch{Limit: 2})
	if err != nil || limited.NeededLimit != 3 || slices.ContainsFunc(limited.Files, func(f File) bool { return f.Status == StatusRenamed }) {
		t.Fatalf("limited search = %+v, %v; want no renames and a needed limit of 3", renameSummary(limited.Files), err)
	}
	unlimited, err := TreeRenames(t.Context(), store, oldTree, newTree, RenameSearch{})
	if err != nil || unlimited.NeededLimit != 0 || len(unlimited.Files) != 3 {
		t.Fatalf("unlimited search = %+v, %v; want three renames", renameSummary(unlimited.Files), err)
	}
}

func TestTreeRenamesPairOnlyRelevantSourcesInexactly(t *testing.T) {
	store := newMemoryStore()
	oldTree := buildTree(store, treeFiles{"a.txt": blobSpec(poem("a")), "b.txt": blobSpec(poem("b")), "e.txt": blobSpec(poem("e"))})
	newTree := buildTree(store, treeFiles{"x.txt": blobSpec(poem("a") + "tail\n"), "y.txt": blobSpec(poem("b") + "tail\n"), "moved/e.txt": blobSpec(poem("e"))})

	report, err := TreeRenames(t.Context(), store, oldTree, newTree, RenameSearch{Relevant: func(path string) bool { return path == "a.txt" }})

	var renames []string
	for _, file := range report.Files {
		if file.Status == StatusRenamed {
			renames = append(renames, file.OldPath+"->"+file.NewPath)
		}
	}
	slices.Sort(renames)
	if err != nil || !slices.Equal(renames, []string{"a.txt->x.txt", "e.txt->moved/e.txt"}) {
		t.Fatalf("renames = %v, %v", renames, err)
	}
}

func TestTreeRenamesReportUnreadableObjects(t *testing.T) {
	store := newMemoryStore()
	base := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "a.txt", ID: store.writeBlob([]byte(poem("a")))}})
	missingSource := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "b.txt", ID: hash.ObjectID{5}}})

	if _, err := TreeRenames(t.Context(), store, base, hash.ObjectID{4}, RenameSearch{}); !errors.Is(err, ErrMissingBlob) {
		t.Errorf("a missing tree returned %v", err)
	}
	if _, err := TreeRenames(t.Context(), store, missingSource, base, RenameSearch{}); !errors.Is(err, ErrMissingBlob) {
		t.Errorf("an unreadable rename source returned %v", err)
	}
}
