package journal

import (
	"errors"
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func buildChainRepo(t *testing.T, length int) (revision.Context, []hash.ObjectID) {
	t.Helper()
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)

	ids := make([]hash.ObjectID, 0, length)
	var previous hash.ObjectID
	var tip hash.ObjectID
	for index := range length {
		when := timeAt(1700000000 + int64(index)*60)
		if index == 0 {
			tip = putCommit(t, db, tree, when, "ann", "n\n")
		} else {
			tip = putCommit(t, db, tree, when, "ann", "n\n", previous)
		}
		ids = append(ids, tip)
		previous = tip
	}
	setRef(t, store, refs.BranchName("main"), tip)

	source := revision.Context{Objects: db, Refs: store}
	return source, ids
}

func reversed(ids []hash.ObjectID) []hash.ObjectID {
	out := make([]hash.ObjectID, len(ids))
	for i, id := range ids {
		out[len(ids)-1-i] = id
	}
	return out
}

func TestPagerNextReturnsRowsInPages(t *testing.T) {
	source, ids := buildChainRepo(t, 7)
	want := reversed(ids)

	pager := NewPager(t.Context(), source, revision.Options{})
	t.Cleanup(pager.Cancel)

	first, done, err := pager.Next(3)
	if err != nil {
		t.Fatalf("Next(3) returned error %v", err)
	}
	if done {
		t.Fatal("Next(3) reported done, want more pages")
	}
	if len(first) != 3 {
		t.Fatalf("Next(3) returned %d rows, want 3", len(first))
	}
	for i, row := range first {
		if row.ID != want[i] {
			t.Fatalf("page 1 row %d = %s, want %s", i, row.ID, want[i])
		}
	}

	second, done, err := pager.Next(3)
	if err != nil {
		t.Fatalf("second Next(3) returned error %v", err)
	}
	if done {
		t.Fatal("second Next(3) reported done, want one more page")
	}
	for i, row := range second {
		if row.ID != want[3+i] {
			t.Fatalf("page 2 row %d = %s, want %s", i, row.ID, want[3+i])
		}
	}

	third, done, err := pager.Next(3)
	if err != nil {
		t.Fatalf("third Next(3) returned error %v", err)
	}
	if !done {
		t.Fatal("third Next(3) did not report done")
	}
	if len(third) != 1 {
		t.Fatalf("third Next(3) returned %d rows, want 1", len(third))
	}
	if third[0].ID != want[6] {
		t.Fatalf("last row = %s, want %s", third[0].ID, want[6])
	}
}

func TestPagerIsLazyAndOnlyReadsRequestedObjects(t *testing.T) {
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

	pager := NewPager(t.Context(), source, revision.Options{})
	t.Cleanup(pager.Cancel)

	rows, done, err := pager.Next(1)
	if err != nil {
		t.Fatalf("Next(1) returned error %v", err)
	}
	if done {
		t.Fatal("Next(1) reported done, want more pages")
	}
	if len(rows) != 1 || rows[0].ID != tip {
		t.Fatalf("Next(1) = %v, want the tip commit", rows)
	}
	if counting.gets > 4 {
		t.Errorf("Pager read %d objects for one row, want a lazy pull", counting.gets)
	}
}

func TestPagerNextWithNonPositiveCountReturnsNothing(t *testing.T) {
	source, _ := buildChainRepo(t, 2)
	pager := NewPager(t.Context(), source, revision.Options{})
	t.Cleanup(pager.Cancel)

	rows, done, err := pager.Next(0)
	if rows != nil || done || err != nil {
		t.Fatalf("Next(0) = %v, %v, %v, want nil, false, nil", rows, done, err)
	}
}

func TestPagerNextReportsDoneOnEmptyHistory(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	source := revision.Context{Objects: db, Refs: store}

	pager := NewPager(t.Context(), source, revision.Options{})
	t.Cleanup(pager.Cancel)

	rows, done, err := pager.Next(3)
	if err != nil {
		t.Fatalf("Next(3) returned error %v", err)
	}
	if !done {
		t.Fatal("Next(3) did not report done for an unborn branch")
	}
	if len(rows) != 0 {
		t.Fatalf("Next(3) = %v, want nothing", rows)
	}
}

func TestPagerNextPropagatesWalkErrors(t *testing.T) {
	r := initTestRepo(t, "main")
	db := openTestDB(t, r)
	store := openTestStore(t, r, db)
	tree := putTree(t, db)
	a := putCommit(t, db, tree, timeAt(1700000060), "ann", "a\n")
	b := putCommit(t, db, tree, timeAt(1700000120), "ann", "b\n", a)
	setRef(t, store, refs.BranchName("main"), b)
	failure := errors.New("boom")
	source := revision.Context{Objects: failingObjects{inner: db, fail: a, err: failure}, Refs: store}

	pager := NewPager(t.Context(), source, revision.Options{})
	t.Cleanup(pager.Cancel)

	rows, done, err := pager.Next(5)
	if !errors.Is(err, revision.ErrNotFound) {
		t.Fatalf("Next(5) returned error %v, want revision.ErrNotFound", err)
	}
	if !done {
		t.Fatal("Next(5) must report done after an error")
	}
	if len(rows) != 0 {
		t.Fatalf("Next(5) rows = %v, want none: the walk fails while resolving b's parent, before b is emitted", rows)
	}
}

func TestPagerCancelStopsFurtherIteration(t *testing.T) {
	source, _ := buildChainRepo(t, 5)
	pager := NewPager(t.Context(), source, revision.Options{})

	rows, done, err := pager.Next(1)
	if err != nil || done || len(rows) != 1 {
		t.Fatalf("Next(1) = %v, %v, %v, want one row and no error", rows, done, err)
	}

	pager.Cancel()

	rows, done, err = pager.Next(5)
	if err != nil {
		t.Fatalf("Next after Cancel returned error %v, want nil", err)
	}
	if !done {
		t.Fatal("Next after Cancel must report done")
	}
	if len(rows) != 0 {
		t.Fatalf("Next after Cancel returned %d rows, want none since the walk was abandoned", len(rows))
	}
}

func TestPagerCancelIsSafeToCallTwice(t *testing.T) {
	source, _ := buildChainRepo(t, 2)
	pager := NewPager(t.Context(), source, revision.Options{})
	pager.Cancel()
	pager.Cancel()
}

func TestNewPagerDefersIterationUntilTheFirstNext(t *testing.T) {
	source, ids := buildChainRepo(t, 3)
	counted := &countingObjects{inner: source.Objects}
	source.Objects = counted
	pager := NewPager(t.Context(), source, revision.Options{})
	t.Cleanup(pager.Cancel)
	if counted.gets != 0 {
		t.Fatalf("NewPager read %d objects before the first Next", counted.gets)
	}
	rows, _, err := pager.Next(1)
	if err != nil || len(rows) != 1 || rows[0].ID != ids[len(ids)-1] {
		t.Fatalf("Next returned %d rows, error %v", len(rows), err)
	}
	if counted.gets == 0 {
		t.Fatal("Next did not read any object")
	}
}

func TestPagerNextWorksWhenNewPagerRanOnAThreadLockedGoroutine(t *testing.T) {
	source, _ := buildChainRepo(t, 3)
	created := make(chan *Pager, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		created <- NewPager(t.Context(), source, revision.Options{})
	}()
	pager := <-created
	done := make(chan error, 1)
	go func() {
		rows, _, err := pager.Next(2)
		pager.Cancel()
		if err == nil && len(rows) != 2 {
			err = errors.New("pager returned no rows")
		}
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatalf("Next on another goroutine returned %v", err)
	}
}
