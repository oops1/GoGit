package diff

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func treeOf(t *testing.T, store *memoryStore, files treeFiles) hash.ObjectID {
	t.Helper()
	return buildTree(store, files)
}

func pathsOf(files []File) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.NewPath)
	}
	return out
}

func TestTreesReportsNothingForTheSameTree(t *testing.T) {
	store := newMemoryStore()
	id := treeOf(t, store, treeFiles{"a.txt": blobSpec("a\n")})
	files, err := Trees(t.Context(), store, id, id, Defaults())
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Trees reported %d files for the same tree", len(files))
	}
}

func TestTreesHonoursThePathFilter(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{
		"root.txt":     blobSpec("root\n"),
		"dir/one.txt":  blobSpec("one\n"),
		"dir/two.txt":  blobSpec("two\n"),
		"other/x.txt":  blobSpec("x\n"),
		"gone/old.txt": blobSpec("old\n"),
	})
	updated := treeOf(t, store, treeFiles{
		"root.txt":      blobSpec("ROOT\n"),
		"dir/one.txt":   blobSpec("ONE\n"),
		"dir/two.txt":   blobSpec("TWO\n"),
		"other/x.txt":   blobSpec("X\n"),
		"fresh/new.txt": blobSpec("new\n"),
	})
	cases := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"no filter reports everything", nil, []string{"dir/one.txt", "dir/two.txt", "fresh/new.txt", "gone/old.txt", "other/x.txt", "root.txt"}},
		{"a single file", []string{"root.txt"}, []string{"root.txt"}},
		{"a whole directory", []string{"dir"}, []string{"dir/one.txt", "dir/two.txt"}},
		{"a file inside a directory", []string{"dir/one.txt"}, []string{"dir/one.txt"}},
		{"a directory that only exists on one side", []string{"fresh"}, []string{"fresh/new.txt"}},
		{"a directory that was removed", []string{"gone"}, []string{"gone/old.txt"}},
		{"a path that matches nothing", []string{"missing"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := Defaults()
			opts.DetectRenames = false
			opts.Paths = c.paths
			files, err := Trees(t.Context(), store, old, updated, opts)
			if err != nil {
				t.Fatalf("Trees returned error %v", err)
			}
			got := pathsOf(files)
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Errorf("Trees reported %q instead of %q", got, c.want)
			}
		})
	}
}

func TestTreesStopsOnACancelledContext(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{"a.txt": blobSpec("a\n")})
	updated := treeOf(t, store, treeFiles{"a.txt": blobSpec("b\n")})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Trees(ctx, store, old, updated, Defaults()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Trees returned %v instead of a cancellation", err)
	}
}

func TestTreesReportsUnreadableObjects(t *testing.T) {
	store := newMemoryStore()
	blobID := store.writeBlob([]byte("plain\n"))
	garbage := store.put(object.TypeTree, []byte("not a tree at all"))
	missing := hash.ObjectID{1, 2, 3}
	base := treeOf(t, store, treeFiles{"a.txt": blobSpec("a\n")})

	blobAsTree := store.writeTree([]object.TreeEntry{{Mode: object.ModeTree, Name: "sub", ID: blobID}})
	treeAsBlob := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "a.txt", ID: base}})
	brokenTree := store.writeTree([]object.TreeEntry{{Mode: object.ModeTree, Name: "sub", ID: garbage}})
	missingBlob := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "a.txt", ID: missing}})

	cases := []struct {
		name string
		old  hash.ObjectID
		new  hash.ObjectID
		want error
	}{
		{"the old tree is missing", missing, base, ErrMissingBlob},
		{"the new tree is missing", base, missing, ErrMissingBlob},
		{"a subtree entry points at a blob", base, blobAsTree, ErrNotATree},
		{"a subtree entry points at a blob on the old side", blobAsTree, base, ErrNotATree},
		{"a blob entry points at a tree", base, treeAsBlob, ErrMissingBlob},
		{"a blob entry points at a tree on the old side", treeAsBlob, base, ErrMissingBlob},
		{"a subtree cannot be parsed", base, brokenTree, object.ErrInvalidMode},
		{"a blob is not in the store", base, missingBlob, ErrMissingBlob},
		{"a blob is not in the store on the old side", missingBlob, base, ErrMissingBlob},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Trees(t.Context(), store, c.old, c.new, Defaults()); !errors.Is(err, c.want) {
				t.Fatalf("Trees returned %v instead of %v", err, c.want)
			}
		})
	}
}

func TestTreesSkipsTopLevelFilesOutsideTheFilter(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{"keep.txt": blobSpec("keep\n"), "drop.txt": blobSpec("drop\n")})
	updated := treeOf(t, store, treeFiles{"keep.txt": blobSpec("KEEP\n"), "added.txt": blobSpec("added\n")})
	opts := Defaults()
	opts.DetectRenames = false
	opts.Paths = []string{"keep.txt"}
	files, err := Trees(t.Context(), store, old, updated, opts)
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	if got := pathsOf(files); !slices.Equal(got, []string{"keep.txt"}) {
		t.Errorf("Trees reported %q instead of just the filtered file", got)
	}
}

func TestTreesReportsUnreadableTopLevelBlobs(t *testing.T) {
	store := newMemoryStore()
	missing := hash.ObjectID{9, 9, 9}
	base := treeOf(t, store, treeFiles{"a.txt": blobSpec("a\n")})
	broken := store.writeTree([]object.TreeEntry{{Mode: object.ModeBlob, Name: "b.txt", ID: missing}})
	if _, err := Trees(t.Context(), store, base, broken, Defaults()); !errors.Is(err, ErrMissingBlob) {
		t.Errorf("an unreadable added blob returned %v", err)
	}
	if _, err := Trees(t.Context(), store, broken, base, Defaults()); !errors.Is(err, ErrMissingBlob) {
		t.Errorf("an unreadable deleted blob returned %v", err)
	}
}

func TestTreesSplitsATypeChangeForThePatchOutput(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{"thing": linkSpec("target.txt")})
	updated := treeOf(t, store, treeFiles{"thing": blobSpec("target.txt")})
	files, err := Trees(t.Context(), store, old, updated, Defaults())
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	if len(files) != 1 || files[0].Status != StatusTypeChanged {
		t.Fatalf("Trees reported %d files with status %v", len(files), files[0].Status)
	}
	parts := files[0].Parts
	if len(parts) != 2 {
		t.Fatalf("a type change carries %d parts instead of two", len(parts))
	}
	if parts[0].Status != StatusDeleted || parts[1].Status != StatusAdded {
		t.Errorf("the parts are %v and %v instead of a deletion and a creation", parts[0].Status, parts[1].Status)
	}
	if parts[0].Deleted() != 1 || parts[1].Added() != 1 {
		t.Errorf("the parts hold %d deletions and %d insertions instead of one each", parts[0].Deleted(), parts[1].Added())
	}
}

func TestTreesKeepsBinaryTypeChangesWithoutHunks(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{"thing": blobSpec(binaryBlob(0))})
	updated := treeOf(t, store, treeFiles{"thing": entrySpec{mode: object.ModeSymlink, data: binaryBlob(1)}})
	files, err := Trees(t.Context(), store, old, updated, Defaults())
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	if len(files) != 1 || !files[0].Binary {
		t.Fatalf("Trees reported %d files and binary=%v", len(files), files[0].Binary)
	}
	for _, part := range files[0].Parts {
		if len(part.Hunks) != 0 {
			t.Errorf("a binary part carries %d hunks", len(part.Hunks))
		}
	}
}

func TestTreesMarksBinaryFilesThatOnlyChangeMode(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{"blob.bin": blobSpec(binaryBlob(0))})
	updated := treeOf(t, store, treeFiles{"blob.bin": entrySpec{mode: object.ModeExecutable, data: binaryBlob(0)}})
	files, err := Trees(t.Context(), store, old, updated, Defaults())
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	if len(files) != 1 || !files[0].Binary {
		t.Fatalf("Trees reported %d files and binary=%v", len(files), files[0].Binary)
	}
}

func TestTreesReportsSubmoduleContentAsText(t *testing.T) {
	store := newMemoryStore()
	old := treeOf(t, store, treeFiles{"module": moduleSpec(moduleA)})
	updated := treeOf(t, store, treeFiles{"module": moduleSpec(moduleB)})
	files, err := Trees(t.Context(), store, old, updated, Defaults())
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	if len(files) != 1 || files[0].Binary || files[0].Added() != 1 || files[0].Deleted() != 1 {
		t.Fatalf("a submodule bump produced %+v", files)
	}
}

func TestFilePathsFillInTheMissingSide(t *testing.T) {
	oldPath, newPath := File{NewPath: "b"}.paths()
	if oldPath != "b" || newPath != "b" {
		t.Errorf("paths returned (%q, %q) for a file with only a new path", oldPath, newPath)
	}
	oldPath, newPath = File{OldPath: "a"}.paths()
	if oldPath != "a" || newPath != "a" {
		t.Errorf("paths returned (%q, %q) for a file with only an old path", oldPath, newPath)
	}
}

func TestStatusStringUsesTheGitLetters(t *testing.T) {
	cases := []struct {
		status Status
		want   string
	}{
		{StatusModified, "M"},
		{StatusAdded, "A"},
		{StatusDeleted, "D"},
		{StatusRenamed, "R"},
		{StatusCopied, "C"},
		{StatusTypeChanged, "T"},
		{Status(200), "?"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.want {
			t.Errorf("Status(%d).String() returned %q instead of %q", c.status, got, c.want)
		}
	}
}

func TestAddedAndDeletedCountTheHunkLines(t *testing.T) {
	file := File{Hunks: []Hunk{{Lines: []Line{
		{Kind: KindContext, Text: "a"},
		{Kind: KindDel, Text: "b"},
		{Kind: KindAdd, Text: "c"},
		{Kind: KindAdd, Text: "d"},
	}}}}
	if file.Added() != 2 || file.Deleted() != 1 {
		t.Errorf("the file reports %d insertions and %d deletions instead of 2 and 1", file.Added(), file.Deleted())
	}
}
