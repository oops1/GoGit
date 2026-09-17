package app

import (
	"bytes"
	"errors"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/gitdiff"
)

func TestLocateDiffLinesFindsTheChangedLinesAndHunksInTheRange(t *testing.T) {
	file := diff.File{Hunks: diff.Blobs([]byte(twelveLines), []byte(bothEndsChanged), diff.Defaults())}
	if len(file.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(file.Hunks))
	}

	for _, tt := range []struct {
		spot  gitdiff.Spot
		lines map[[2]int]bool
		hunks map[int]bool
	}{
		{gitdiff.Spot{Side: widget.DiffLeft, From: 0, To: 1}, map[[2]int]bool{{0, 0}: true}, map[int]bool{0: true}},
		{gitdiff.Spot{Side: widget.DiffRight, From: 0, To: 1}, map[[2]int]bool{{0, 1}: true}, map[int]bool{0: true}},
		{gitdiff.Spot{Side: widget.DiffRight, From: 1, To: 2}, map[[2]int]bool{}, map[int]bool{0: true}},
		{gitdiff.Spot{Side: widget.DiffLeft, From: 11, To: 12}, map[[2]int]bool{{1, 3}: true}, map[int]bool{1: true}},
		{gitdiff.Spot{Side: widget.DiffRight, From: 0, To: 12}, map[[2]int]bool{{0, 1}: true, {1, 4}: true}, map[int]bool{0: true, 1: true}},
		{gitdiff.Spot{Side: widget.DiffRight, From: 6, To: 7}, map[[2]int]bool{}, map[int]bool{}},
	} {
		lines, hunks := locateDiffLines(file, tt.spot)
		if !maps.Equal(lines, tt.lines) || !maps.Equal(hunks, tt.hunks) {
			t.Fatalf("%+v -> lines %v hunks %v, want %v %v", tt.spot, lines, hunks, tt.lines, tt.hunks)
		}
	}
}

func TestPickersChooseTheLocatedHunksAndLines(t *testing.T) {
	byHunks := pickHunks(map[int]bool{1: true})
	if !byHunks(1, 7) || byHunks(0, 7) {
		t.Fatal("pickHunks chooses the wrong hunk")
	}
	byLines := pickLines(map[[2]int]bool{{1, 2}: true})
	if !byLines(1, 2) || byLines(1, 3) || byLines(0, 2) {
		t.Fatal("pickLines chooses the wrong line")
	}
}

func TestTheDiffSidesCarryTheNamesAndTexts(t *testing.T) {
	file := diff.File{OldPath: "old.txt", NewPath: "new.txt"}

	left, right := diffSides(diffTarget{kind: diffKindWorktree, file: file, oldData: []byte("a\n"), newData: []byte("b\n")})

	if left != (gitdiff.Side{Title: "old.txt", Text: "a\n"}) || right != (gitdiff.Side{Title: "new.txt", Text: "b\n"}) {
		t.Fatalf("%+v / %+v", left, right)
	}
}

func TestABinaryDiffSaysSoOnBothSides(t *testing.T) {
	left, right := diffSides(diffTarget{file: diff.File{OldPath: "a.bin", NewPath: "a.bin", Binary: true}, oldData: []byte("x\x00"), newData: []byte("y\x00")})

	binary := i18n.T("Diff.Binary")
	if left.Text != binary || right.Text != binary || left.Title != "a.bin" {
		t.Fatalf("%+v / %+v", left, right)
	}
}

func commitDiffDB(t *testing.T) *odb.DB {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	initTestRepoWithBranch(t, target, "main")
	db, _ := withJournalRepo(t, target)
	return db
}

func TestACommitDiffCarriesTheTextsOfBothBlobs(t *testing.T) {
	db := commitDiffDB(t)
	file := diff.File{OldMode: object.ModeBlob, OldID: putChangesBlob(t, db, "one\n"), NewMode: object.ModeBlob, NewID: putChangesBlob(t, db, "two\n")}

	got, err := commitDiffTarget(db, file)

	if err != nil || string(got.oldData) != "one\n" || string(got.newData) != "two\n" || got.file.OldID != file.OldID {
		t.Fatalf("target = %+v, err = %v", got, err)
	}
}

func TestACommitDiffShowsASubmoduleAsItsCommit(t *testing.T) {
	db := commitDiffDB(t)
	id := oid(t, "ab")

	got, err := commitDiffTarget(db, diff.File{NewMode: object.ModeSubmodule, NewID: id})

	if err != nil || got.oldData != nil || string(got.newData) != "Subproject commit "+id.String()+"\n" {
		t.Fatalf("target = %+v, err = %v", got, err)
	}
}

func TestACommitDiffFailsWhenABlobIsMissing(t *testing.T) {
	db := commitDiffDB(t)
	present := putChangesBlob(t, db, "one\n")

	for _, file := range []diff.File{
		{OldMode: object.ModeBlob, OldID: oid(t, "ff")},
		{OldMode: object.ModeBlob, OldID: present, NewMode: object.ModeBlob, NewID: oid(t, "ff")},
	} {
		if _, err := commitDiffTarget(db, file); !errors.Is(err, odb.ErrNotFound) {
			t.Fatalf("file %+v: err = %v, want odb.ErrNotFound", file, err)
		}
	}
}

func TestACommitDiffReadsNothingForABinaryFileOrWithoutADatabase(t *testing.T) {
	db := commitDiffDB(t)
	missing := diff.File{OldMode: object.ModeBlob, OldID: oid(t, "ff")}
	binary := missing
	binary.Binary = true

	for _, tt := range []struct {
		db   *odb.DB
		file diff.File
	}{{nil, missing}, {db, binary}} {
		got, err := commitDiffTarget(tt.db, tt.file)
		if err != nil || got.oldData != nil || got.newData != nil {
			t.Fatalf("target = %+v, err = %v", got, err)
		}
	}
}

func TestACommitWithoutChangesClearsTheDiff(t *testing.T) {
	a := newTestApp(t)
	db := commitDiffDB(t)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "same\n"})
	parent := putChangesCommit(t, db, tree)
	empty := putChangesCommit(t, db, tree, parent)
	readOnDispatcher(t, a, func() bool {
		a.showDiff(diffTarget{file: diff.File{OldPath: "a.txt", NewPath: "a.txt", Hunks: diff.Blobs([]byte("a\n"), []byte("b\n"), diff.Defaults())}, oldData: []byte("a\n"), newData: []byte("b\n")})
		return true
	})

	a.runFilesDiff(t.Context(), db, a.commitFilesLoader(empty))
	waitForPostQueueDrain(t, a)

	if doc := diffDocumentOnDispatcher(t, a); !doc.IsEmpty() || doc.OldName != "" || doc.Left != "" {
		t.Fatalf("diff after an empty commit = %+v", doc)
	}
}

func TestAnUnreadableCommitDiffIsLogged(t *testing.T) {
	a := newTestApp(t)
	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	file := diff.File{OldPath: "gone.txt", OldMode: object.ModeBlob, OldID: oid(t, "ff")}

	got := a.commitTarget(commitDiffDB(t), file)

	if got.file.OldPath != "gone.txt" || !strings.Contains(buf.String(), "load commit diff failed") {
		t.Fatalf("target = %+v, log = %s", got, buf.String())
	}
}
