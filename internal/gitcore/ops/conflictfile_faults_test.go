package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func TestReadingAConflictInABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)

	if _, err := ReadConflict(t.Context(), tr.repo, "f"); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestReadingAConflictStopsWhenTheIndexCannotBeRead(t *testing.T) {
	tr := conflictedRepo(t)
	tr.corruptIndexFile()

	if _, err := ReadConflict(t.Context(), tr.repo, "f"); err == nil {
		t.Fatal("a broken index was read as if it were fine")
	}
}

func TestReadingAConflictStopsWhenTheObjectDatabaseCannotOpen(t *testing.T) {
	tr := conflictedRepo(t)
	swapOdbOpenFailOnCall(t, 1)

	if _, err := ReadConflict(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingAConflictStopsWhenASideCannotBeRead(t *testing.T) {
	tr := conflictedRepo(t)
	original := dbGet
	dbGet = func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) { return 0, nil, errInjected }
	t.Cleanup(func() { dbGet = original })

	if _, err := ReadConflict(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAConflictOverSomethingThatIsNotABlobHasNoSides(t *testing.T) {
	tr := conflictedRepo(t)
	original := dbGet
	dbGet = func(db *odb.DB, id hash.ObjectID) (object.Type, []byte, error) {
		kind, data, err := original(db, id)
		if err != nil {
			return kind, data, err
		}
		return object.TypeCommit, data, nil
	}
	t.Cleanup(func() { dbGet = original })

	file, err := ReadConflict(t.Context(), tr.repo, "f")

	if err != nil {
		t.Fatalf("ReadConflict returned error %v", err)
	}
	if file.HasOurs || file.HasTheirs || file.HasBase {
		t.Fatalf("file = %+v, want no side taken from a commit", file)
	}
}

func TestAConflictWithoutAnyStageOfTheThreeIsReported(t *testing.T) {
	tr := conflictedRepo(t)
	idx := tr.index()
	idx.Remove("f")
	idx.Add(index.Entry{Path: "f", Mode: object.ModeBlob, ID: hash.SumSHA1("blob", []byte("x")), Stage: index.StageMerged})
	tr.saveIndex(idx)

	if _, err := ReadConflict(t.Context(), tr.repo, "f"); !errors.Is(err, ErrNotConflicted) {
		t.Fatalf("err = %v, want ErrNotConflicted", err)
	}
}

func TestSavingAResolutionInABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)

	err := SaveResolution(t.Context(), tr.repo, "f", []byte("resolved\n"), ResolutionOptions{})

	if !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestSavingAResolutionStopsWhenTheDirectoryCannotBeMade(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"dir/f": "ours\n"}, map[string]string{"dir/f": "theirs\n"})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	swapRootMkdirAllFailForPath(t, "dir")

	err := SaveResolution(t.Context(), tr.repo, "dir/f", []byte("resolved\n"), ResolutionOptions{})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestSavingAResolutionStopsWhenTheFileCannotBeOpened(t *testing.T) {
	tr := conflictedRepo(t)
	swapRootOpenFileFailForPath(t, "f")

	err := SaveResolution(t.Context(), tr.repo, "f", []byte("resolved\n"), ResolutionOptions{})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestSavingAResolutionReportsAFileItCannotWrite(t *testing.T) {
	tr := conflictedRepo(t)
	swapRootOpenFileReturnsClosedFile(t, "f")

	err := SaveResolution(t.Context(), tr.repo, "f", []byte("resolved\n"), ResolutionOptions{})

	if err == nil {
		t.Fatal("writing into a closed file was reported as success")
	}
}
