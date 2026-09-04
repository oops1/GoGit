package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func timeAt(seconds int64) time.Time {
	return time.Unix(seconds, 0).UTC()
}

func collectRows(t *testing.T, seq func(func(Row, error) bool)) ([]Row, error) {
	t.Helper()
	var rows []Row
	for row, err := range seq {
		if err != nil {
			return rows, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func buildLinearRepo(t *testing.T) (*repo.Repository, revision.Context, hash.ObjectID, hash.ObjectID, hash.ObjectID) {
	t.Helper()
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)

	tree := putTree(t, db)
	a := putCommit(t, db, tree, timeAt(1700000060), "ann", "a\n")
	b := putCommit(t, db, tree, timeAt(1700000120), "ann", "b\n", a)
	c := putCommit(t, db, tree, timeAt(1700000180), "ann", "c\n", b)
	setRef(t, store, refs.BranchName("main"), c)

	source := revision.Context{Objects: db, Refs: store}
	return r, source, a, b, c
}

func TestLoadWalksHistoryFromHeadNewestFirst(t *testing.T) {
	_, source, a, b, c := buildLinearRepo(t)

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	want := []hash.ObjectID{c, b, a}
	var got []hash.ObjectID
	for _, row := range rows {
		got = append(got, row.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Load visited %v, want %v", got, want)
	}
}

func TestLoadFormatsRowFieldsFromTheCommit(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	when := timeAt(1700000000)
	id := putCommit(t, db, tree, when, "Ann Author", "subject line\n\nbody text\n")
	setRef(t, store, refs.BranchName("main"), id)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Load returned %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.Graph != "*" {
		t.Errorf("Graph = %q, want *", row.Graph)
	}
	if row.Message != "subject line" {
		t.Errorf("Message = %q, want %q", row.Message, "subject line")
	}
	if row.Author != "Ann Author" {
		t.Errorf("Author = %q, want %q", row.Author, "Ann Author")
	}
	if want := when.Local().Format("2006-01-02 15:04"); row.Date != want {
		t.Errorf("Date = %q, want %q", row.Date, want)
	}
	if want := id.String()[:7]; row.ShortHash != want {
		t.Errorf("ShortHash = %q, want %q", row.ShortHash, want)
	}
	if row.ID != id {
		t.Errorf("ID = %s, want %s", row.ID, id)
	}
	if row.Unpushed {
		t.Error("Unpushed = true, want false")
	}
}

func TestLoadWalksACommitWithFileContent(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	blob := putBlob(t, db, "hello world\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "readme.txt", ID: blob})
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "add readme\n")
	setRef(t, store, refs.BranchName("main"), id)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("Load returned %v, want a single row for %s", rows, id)
	}
}

func TestLoadDecoratesCommitsWithBranchesTagsAndRemotes(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "root\n")
	setRef(t, store, refs.BranchName("main"), id)
	setRef(t, store, refs.BranchName("feature"), id)
	setRef(t, store, refs.TagName("v1"), id)
	setRef(t, store, refs.RemoteBranchName("origin", "main"), id)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Load returned %d rows, want 1", len(rows))
	}
	want := []string{"feature", "main", "origin/main", "v1"}
	got := slices.Clone(rows[0].Refs)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("Refs = %v, want %v", got, want)
	}
}

func TestLoadDecoratesAnnotatedTagsByPeeledTarget(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "root\n")
	tag := putAnnotatedTag(t, db, id, "v1", timeAt(1700000000))
	setRef(t, store, refs.BranchName("main"), id)
	setRef(t, store, refs.TagName("v1"), tag)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Load returned %d rows, want 1", len(rows))
	}
	if !slices.Contains(rows[0].Refs, "v1") {
		t.Fatalf("Refs = %v, want it to contain v1", rows[0].Refs)
	}
}

func TestLoadExcludesNonBranchTagRemoteRefsFromDecorations(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "root\n")
	setRef(t, store, refs.BranchName("main"), id)
	setRef(t, store, refs.Name("refs/stash"), id)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Load returned %d rows, want 1", len(rows))
	}
	if !slices.Equal(rows[0].Refs, []string{"main"}) {
		t.Fatalf("Refs = %v, want [main]", rows[0].Refs)
	}
}

func TestLoadSkipsDanglingSymbolicBranchesInDecorations(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "root\n")
	setRef(t, store, refs.BranchName("main"), id)
	tx := store.Begin()
	if err := tx.SetSymbolic(refs.BranchName("dangling"), refs.BranchName("nowhere")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Load returned %d rows, want 1", len(rows))
	}
	if !slices.Equal(rows[0].Refs, []string{"main"}) {
		t.Fatalf("Refs = %v, want [main]", rows[0].Refs)
	}
}

func TestLoadKeepsOnlyTheFirstLineOfAMessageWithoutATrailingNewline(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "no trailing newline")
	setRef(t, store, refs.BranchName("main"), id)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Load returned %d rows, want 1", len(rows))
	}
	if rows[0].Message != "no trailing newline" {
		t.Fatalf("Message = %q, want %q", rows[0].Message, "no trailing newline")
	}
}

func TestLoadReturnsEmptyForUnbornHead(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if rows != nil {
		t.Fatalf("Load returned %v, want nothing", rows)
	}
}

func TestLoadTreatsMissingHeadFileAsUnbornRatherThanError(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	if err := os.Remove(filepath.Join(r.GitDir(), "HEAD")); err != nil {
		t.Fatalf("Remove(HEAD) returned error %v", err)
	}
	source := revision.Context{Objects: db, Refs: store}

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if err != nil {
		t.Fatalf("Load returned error %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("Load returned %v, want nothing", rows)
	}
}

func TestLoadPropagatesNonNotFoundHeadResolutionErrors(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	failure := errors.New("head is unreadable")
	source := revision.Context{Objects: db, Refs: headErrorRefs{inner: store, err: failure}}

	_, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if !errors.Is(err, failure) {
		t.Fatalf("Load returned %v, want %v", err, failure)
	}
}

func TestLoadPropagatesDecorationIterationError(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	id := putCommit(t, db, tree, timeAt(1700000000), "ann", "root\n")
	setRef(t, store, refs.BranchName("main"), id)
	corrupt := filepath.Join(r.CommonDir(), "refs", "tags", "broken")
	if err := os.MkdirAll(filepath.Dir(corrupt), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(corrupt, []byte("not-a-hash\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	source := revision.Context{Objects: db, Refs: store}

	_, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Load returned %v, want ErrMalformedRef", err)
	}
}

func TestLoadPropagatesObjectReadErrors(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	a := putCommit(t, db, tree, timeAt(1700000060), "ann", "a\n")
	b := putCommit(t, db, tree, timeAt(1700000120), "ann", "b\n", a)
	setRef(t, store, refs.BranchName("main"), b)
	failure := errors.New("boom")
	source := revision.Context{Objects: failingObjects{inner: db, fail: a, err: failure}, Refs: store}

	_, err := collectRows(t, Load(t.Context(), source, revision.Options{}))
	if !errors.Is(err, revision.ErrNotFound) {
		t.Fatalf("Load returned %v, want revision.ErrNotFound", err)
	}
}

func TestLoadAppliesWalkOptionsLikeMaxCount(t *testing.T) {
	_, source, _, b, c := buildLinearRepo(t)

	rows, err := collectRows(t, Load(t.Context(), source, revision.Options{MaxCount: 2}))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	want := []hash.ObjectID{c, b}
	var got []hash.ObjectID
	for _, row := range rows {
		got = append(got, row.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Load visited %v, want %v", got, want)
	}
}

func TestLoadStopsOnContextCancellation(t *testing.T) {
	_, source, _, _, _ := buildLinearRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := collectRows(t, Load(ctx, source, revision.Options{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load returned %v, want context.Canceled", err)
	}
}

func TestLoadIsLazyAndStopsReadingWhenTheConsumerStops(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)

	var previous hash.ObjectID
	var tip hash.ObjectID
	for index := range 40 {
		when := timeAt(1700000000 + int64(index)*60)
		if index == 0 {
			tip = putCommit(t, db, tree, when, "ann", "n\n")
		} else {
			tip = putCommit(t, db, tree, when, "ann", "n\n", previous)
		}
		previous = tip
	}
	setRef(t, store, refs.BranchName("main"), tip)

	counting := &countingObjects{inner: db}
	source := revision.Context{Objects: counting, Refs: store}

	seen := 0
	for row, err := range Load(t.Context(), source, revision.Options{}) {
		if err != nil {
			t.Fatalf("Load returned error %v", err)
		}
		if row.ID != tip {
			t.Fatalf("Load started at %s, want %s", row.ID, tip)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("Load yielded %d rows after the consumer stopped, want 1", seen)
	}
	if counting.gets > 4 {
		t.Errorf("Load read %d objects before the first row, want a lazy traversal", counting.gets)
	}
}
