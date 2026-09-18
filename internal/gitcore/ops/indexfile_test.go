package ops

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func editableRepo(t *testing.T) *testRepo {
	t.Helper()
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "one\ntwo\n"})
	tr.writeFile("f", "one\ntwo\nthree\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	tr.writeFile("f", "one\ntwo\nthree\nfour\n")
	return tr
}

func TestTheIndexEditorSeesAllThreeSidesOfAFile(t *testing.T) {
	tr := editableRepo(t)

	sides, err := ReadIndexSides(t.Context(), tr.repo, "f")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if !sides.HasHead || !sides.HasIndex || !sides.HasWorking || sides.Binary {
		t.Fatalf("sides = %+v", sides)
	}
	if string(sides.Head) != "one\ntwo\n" {
		t.Fatalf("head = %q", sides.Head)
	}
	if string(sides.Index) != "one\ntwo\nthree\n" {
		t.Fatalf("index = %q", sides.Index)
	}
	if string(sides.Working) != "one\ntwo\nthree\nfour\n" || sides.Path != "f" {
		t.Fatalf("working = %q, path = %q", sides.Working, sides.Path)
	}
}

func TestAnUntrackedFileHasOnlyTheWorkingSide(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("u", "new\n")

	sides, err := ReadIndexSides(t.Context(), tr.repo, "u")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if sides.HasHead || sides.HasIndex || !sides.HasWorking {
		t.Fatalf("sides = %+v", sides)
	}
}

func TestAFileDeletedFromTheWorkingCopyKeepsTheOtherSides(t *testing.T) {
	tr := editableRepo(t)
	tr.remove("f")

	sides, err := ReadIndexSides(t.Context(), tr.repo, "f")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if !sides.HasHead || !sides.HasIndex || sides.HasWorking {
		t.Fatalf("sides = %+v", sides)
	}
}

func TestBeforeTheFirstCommitThereIsNoHeadSide(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("f", "new\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	sides, err := ReadIndexSides(t.Context(), tr.repo, "f")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if sides.HasHead || !sides.HasIndex {
		t.Fatalf("sides = %+v", sides)
	}
}

func TestABinaryFileIsReportedAsSuch(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("b", "\x00\x01binary\n")
	if err := Stage(t.Context(), tr.repo, []string{"b"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	sides, err := ReadIndexSides(t.Context(), tr.repo, "b")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if !sides.Binary {
		t.Fatalf("sides = %+v, want a binary file", sides)
	}
}

func TestAPathInsideADirectoryIsReadFromTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"dir/f": "deep\n"})

	sides, err := ReadIndexSides(t.Context(), tr.repo, "dir/f")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if string(sides.Head) != "deep\n" {
		t.Fatalf("head = %q", sides.Head)
	}
}

func TestAPathUnderAFileIsNotFoundInTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	tr.remove("f")
	tr.writeFile("f/inside", "x\n")

	sides, err := ReadIndexSides(t.Context(), tr.repo, "f/inside")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if sides.HasHead {
		t.Fatalf("sides = %+v, want nothing under a file", sides)
	}
}

func TestAMissingNameIsNotFoundInTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"dir/f": "deep\n"})
	tr.writeFile("dir/other", "x\n")

	sides, err := ReadIndexSides(t.Context(), tr.repo, "dir/other")

	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	if sides.HasHead {
		t.Fatalf("sides = %+v, want no side for a name the commit does not know", sides)
	}
}

func TestADirectoryOfTheCommitIsNotAFileToEdit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"dir/f": "deep\n"})

	_, err := ReadIndexSides(t.Context(), tr.repo, "dir")

	if !errors.Is(err, ErrNotARegularFile) {
		t.Fatalf("err = %v, want ErrNotARegularFile", err)
	}
}

func TestADirectoryInTheWorkingCopyIsNotAFileToEdit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("dir/f", "x\n")

	_, err := ReadIndexSides(t.Context(), tr.repo, "dir")

	if !errors.Is(err, ErrNotARegularFile) {
		t.Fatalf("err = %v, want ErrNotARegularFile", err)
	}
}

func TestASubmoduleOfTheIndexIsNotAFileToEdit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	idx := tr.index()
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte("x")), Stage: index.StageMerged})
	tr.saveIndex(idx)

	_, err := ReadIndexSides(t.Context(), tr.repo, "sub")

	if !errors.Is(err, ErrNotARegularFile) {
		t.Fatalf("err = %v, want ErrNotARegularFile", err)
	}
}

func TestAnIndexEntryThatIsNotABlobIsNotAFileToEdit(t *testing.T) {
	tr := editableRepo(t)
	swapSeam(t, &dbGet, func(original func(*odb.DB, hash.ObjectID) (object.Type, []byte, error)) func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) {
		return func(db *odb.DB, id hash.ObjectID) (object.Type, []byte, error) {
			kind, data, err := original(db, id)
			if err != nil {
				return kind, data, err
			}
			return object.TypeTree, data, nil
		}
	})

	_, err := ReadIndexSides(t.Context(), tr.repo, "f")

	if !errors.Is(err, ErrNotARegularFile) {
		t.Fatalf("err = %v, want ErrNotARegularFile", err)
	}
}

func TestAConflictedPathIsNotEditedInTheIndex(t *testing.T) {
	tr := conflictedRepo(t)

	_, readErr := ReadIndexSides(t.Context(), tr.repo, "f")
	saveErr := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n"))

	if !errors.Is(readErr, ErrUnmergedPaths) || !errors.Is(saveErr, ErrUnmergedPaths) {
		t.Fatalf("read = %v, save = %v, want ErrUnmergedPaths", readErr, saveErr)
	}
}

func TestReadingTheSidesOfAnInvalidPathFails(t *testing.T) {
	tr := editableRepo(t)

	if _, err := ReadIndexSides(t.Context(), tr.repo, "../outside"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}

func TestReadingTheSidesStopsWhenTheWorkCanceled(t *testing.T) {
	tr := editableRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ReadIndexSides(ctx, tr.repo, "f"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestReadingTheSidesInABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestReadingTheSidesStopsWhenTheObjectDatabaseCannotOpen(t *testing.T) {
	tr := editableRepo(t)
	swapOdbOpenFailOnCall(t, 1)

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingTheSidesStopsWhenTheIndexCannotBeRead(t *testing.T) {
	tr := editableRepo(t)
	tr.corruptIndexFile()

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); err == nil {
		t.Fatal("a broken index was read as if it were fine")
	}
}

func TestReadingTheSidesStopsWhenHeadCannotBeResolved(t *testing.T) {
	tr := editableRepo(t)
	tr.writeRawHead("ref: refs/heads/main\x00\n")

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); err == nil {
		t.Fatal("a broken HEAD was resolved as if it were fine")
	}
}

func TestReadingTheSidesStopsWhenTheCommitCannotBeRead(t *testing.T) {
	tr := editableRepo(t)
	swapDBCommit(t, func(*odb.DB, hash.ObjectID) (*object.Commit, error) { return nil, errInjected })

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingTheSidesStopsWhenATreeCannotBeRead(t *testing.T) {
	tr := editableRepo(t)
	swapSeam(t, &dbTree, func(func(*odb.DB, hash.ObjectID) (*object.Tree, error)) func(*odb.DB, hash.ObjectID) (*object.Tree, error) {
		return func(*odb.DB, hash.ObjectID) (*object.Tree, error) { return nil, errInjected }
	})

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingTheSidesStopsWhenABlobCannotBeRead(t *testing.T) {
	tr := editableRepo(t)
	swapSeam(t, &dbGet, func(func(*odb.DB, hash.ObjectID) (object.Type, []byte, error)) func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) {
		return func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) { return 0, nil, errInjected }
	})

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingTheSidesStopsWhenTheWorkingFileCannotBeRead(t *testing.T) {
	tr := editableRepo(t)
	swapRootReadFile(t, func(*os.Root, string) ([]byte, error) { return nil, errInjected })

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingTheSidesStopsWhenTheWorkingFileCannotBeExamined(t *testing.T) {
	tr := editableRepo(t)
	swapRootLstatFailForPath(t, "f")

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestReadingTheSidesStopsWhenTheContentCannotBeConverted(t *testing.T) {
	tr := editableRepo(t)
	tr.writeFile(".gitattributes", "f filter=broken\n")
	tr.appendConfig("[filter \"broken\"]\n\tclean = does-not-exist-anywhere\n")
	tr.repo = tr.reopen()

	if _, err := ReadIndexSides(t.Context(), tr.repo, "f"); err == nil {
		t.Fatal("a filter that cannot run was reported as success")
	}
}

func TestTheEditedContentReachesTheIndexAndLeavesTheWorkingCopyAlone(t *testing.T) {
	tr := editableRepo(t)

	err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("one\nTWO\n"))

	if err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}
	if got := tr.stagedText("f"); got != "one\nTWO\n" {
		t.Fatalf("staged = %q", got)
	}
	if got := tr.readFile("f"); got != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("the working copy changed: %q", got)
	}
}

func TestSavingKeepsTheModeTheIndexAlreadyHas(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the executable bit is not kept on windows")
	}
	tr := newTestRepo(t)
	tr.writeFile("f", "a\n")
	tr.chmodExecutable("f")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("b\n")); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	entry, found := tr.index().Get("f", index.StageMerged)
	if !found || entry.Mode != object.ModeExecutable {
		t.Fatalf("entry = %+v, want an executable file", entry)
	}
}

func TestSavingAFileTheIndexDoesNotKnowTakesTheModeFromTheWorkingCopy(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("u", "u\n")

	if err := SaveIndexContent(t.Context(), tr.repo, "u", []byte("staged\n")); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	entry, found := tr.index().Get("u", index.StageMerged)
	if !found || entry.Mode != object.ModeBlob {
		t.Fatalf("entry = %+v, want a plain file", entry)
	}
	if got := tr.readFile("u"); got != "u\n" {
		t.Fatalf("the working copy changed: %q", got)
	}
}

func TestSavingAnExecutableFileTheIndexDoesNotKnowKeepsTheBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("core.filemode is disabled on windows")
	}
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("run.sh", "echo hi\n")
	tr.chmodExecutable("run.sh")

	if err := SaveIndexContent(t.Context(), tr.repo, "run.sh", []byte("echo staged\n")); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	entry, found := tr.index().Get("run.sh", index.StageMerged)
	if !found || entry.Mode != object.ModeExecutable {
		t.Fatalf("entry = %+v, want an executable file", entry)
	}
}

func TestSavingAFileThatIsNowhereElseStillReachesTheIndex(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})

	if err := SaveIndexContent(t.Context(), tr.repo, "ghost", []byte("only staged\n")); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	if got := tr.stagedText("ghost"); got != "only staged\n" {
		t.Fatalf("staged = %q", got)
	}
	if tr.exists("ghost") {
		t.Fatal("the working copy gained a file it never had")
	}
}

func TestSavingOverADirectoryIsRefused(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("dir/f", "x\n")

	err := SaveIndexContent(t.Context(), tr.repo, "dir", []byte("x\n"))

	if !errors.Is(err, ErrNotARegularFile) {
		t.Fatalf("err = %v, want ErrNotARegularFile", err)
	}
}

func TestSavingOverASubmoduleIsRefused(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	idx := tr.index()
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte("x")), Stage: index.StageMerged})
	tr.saveIndex(idx)

	err := SaveIndexContent(t.Context(), tr.repo, "sub", []byte("x\n"))

	if !errors.Is(err, ErrNotARegularFile) {
		t.Fatalf("err = %v, want ErrNotARegularFile", err)
	}
}

func TestSavingAnUnsafePathIsRefused(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})

	err := SaveIndexContent(t.Context(), tr.repo, ".git/config", []byte("x\n"))

	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
}

func TestSavingIntoTheIndexOfAnInvalidPathFails(t *testing.T) {
	tr := editableRepo(t)

	if err := SaveIndexContent(t.Context(), tr.repo, "../outside", []byte("x\n")); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}

func TestSavingIntoTheIndexStopsWhenTheWorkCanceled(t *testing.T) {
	tr := editableRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := SaveIndexContent(ctx, tr.repo, "f", []byte("x\n")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSavingIntoTheIndexOfABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n")); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestSavingIntoTheIndexStopsWhenTheObjectDatabaseCannotOpen(t *testing.T) {
	tr := editableRepo(t)
	swapOdbOpenFailOnCall(t, 1)

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestSavingIntoTheIndexStopsWhenThePathRulesAreInvalid(t *testing.T) {
	tr := editableRepo(t)
	tr.appendConfig("[core]\n\tprotectntfs = maybe\n")
	tr.repo = tr.reopen()

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n")); err == nil {
		t.Fatal("SaveIndexContent accepted an invalid core.protectntfs")
	}
}

func TestSavingIntoTheIndexStopsWhenItIsLocked(t *testing.T) {
	tr := editableRepo(t)
	if err := os.WriteFile(tr.repo.GitPath(indexLockName), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n")); !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
}

func TestSavingIntoTheIndexStopsWhenTheBlobCannotBeWritten(t *testing.T) {
	tr := editableRepo(t)
	swapDBPut(t, func(*odb.DB, object.Type, []byte) (hash.ObjectID, error) { return hash.Zero, errInjected })

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
	if tr.exists(".git/" + indexLockName) {
		t.Fatal("the index lock was left behind")
	}
}

func TestSavingIntoTheIndexStopsWhenTheWorkingFileCannotBeExamined(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("u", "u\n")
	swapRootLstatFailForPath(t, "u")

	if err := SaveIndexContent(t.Context(), tr.repo, "u", []byte("x\n")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestSavingIntoTheIndexStopsWhenItCannotBeWritten(t *testing.T) {
	tr := editableRepo(t)
	swapIdxWrite(t, func(*index.Index, io.Writer, int) error { return errInjected })

	if err := SaveIndexContent(t.Context(), tr.repo, "f", []byte("x\n")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}
