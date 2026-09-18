package diff

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestTreeRenamesIntoLetsOnlyWantedDestinationsTakeASource(t *testing.T) {
	store := newMemoryStore()
	oldTree := buildTree(store, treeFiles{"a.txt": blobSpec(poem("a")), "keep.txt": blobSpec("keep\n")})
	newTree := buildTree(store, treeFiles{
		"x.txt":    blobSpec(poem("a") + "tail\n"),
		"y.txt":    blobSpec(poem("a")),
		"keep.txt": blobSpec("changed\n"),
	})

	files, err := TreeRenamesInto(t.Context(), store, oldTree, newTree, Defaults(), func(path string) bool { return path == "x.txt" })

	if err != nil || !slices.Equal(renameSummary(files), []string{"R a.txt->x.txt 98"}) {
		t.Fatalf("files = %v, %v", renameSummary(files), err)
	}
}

func TestTreeRenamesIntoReportsUnreadableObjects(t *testing.T) {
	store := newMemoryStore()
	base := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "a.txt", ID: store.writeBlob([]byte(poem("a")))}})
	missingSource := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "b.txt", ID: hash.ObjectID{5}}})
	all := func(string) bool { return true }

	if _, err := TreeRenamesInto(t.Context(), store, base, hash.ObjectID{4}, Defaults(), all); !errors.Is(err, ErrMissingBlob) {
		t.Errorf("a missing tree returned %v", err)
	}
	if _, err := TreeRenamesInto(t.Context(), store, missingSource, base, Defaults(), all); !errors.Is(err, ErrMissingBlob) {
		t.Errorf("an unreadable rename source returned %v", err)
	}
}
