package ops

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

var errConversionFault = errors.New("injected conversion fault")

const lfsFilterConfig = "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge -- %f\n\tprocess = git-lfs filter-process\n\trequired = true\n"

func lfsPointerOf(content string) string {
	return fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%x\nsize %d\n", sha256.Sum256([]byte(content)), len(content))
}

func blobIDOf(t *testing.T, content string) hash.ObjectID {
	t.Helper()
	id, err := hash.Sum(hash.SHA1, "blob", []byte(content))
	if err != nil {
		t.Fatalf("Sum returned error %v", err)
	}
	return id
}

func TestStageRefusesPathsCleanedByAnExternalFilter(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[filter \"indent\"]\n\tclean = indent\n")
	tr.repo = tr.reopen()
	tr.writeFile(".gitattributes", "*.c filter=indent\n")
	tr.writeFile("a.c", "int main;\n")
	err := Stage(t.Context(), tr.repo, []string{".gitattributes", "a.c"}, StageOptions{})
	if !errors.Is(err, ErrFilterUnsupported) {
		t.Fatalf("Stage returned %v, want %v", err, ErrFilterUnsupported)
	}
	if _, ok := entryOf(t, tr.index(), ".gitattributes"); ok {
		t.Fatalf("Stage kept part of the refused change in the index")
	}
}

func TestStageTakesLFSPointersAndUnchangedLFSContent(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig(lfsFilterConfig)
	tr.repo = tr.reopen()
	tr.writeFile(".gitattributes", "*.bin filter=lfs -text\n")
	content := "payload\x00bytes"
	pointer := lfsPointerOf(content)

	tr.writeFile("a.bin", pointer)
	if err := Stage(t.Context(), tr.repo, []string{".gitattributes", "a.bin"}, StageOptions{}); err != nil {
		t.Fatalf("Stage of a pointer returned error %v", err)
	}
	tr.writeFile("a.bin", content)
	if err := Stage(t.Context(), tr.repo, []string{"a.bin"}, StageOptions{}); err != nil {
		t.Fatalf("Stage of unchanged LFS content returned error %v", err)
	}
	if entry, _ := entryOf(t, tr.index(), "a.bin"); entry.ID != blobIDOf(t, pointer) {
		t.Fatalf("a.bin is staged as %s, want the pointer %s", entry.ID, blobIDOf(t, pointer))
	}
	tr.writeFile("a.bin", "changed\x00payload")
	if err := Stage(t.Context(), tr.repo, []string{"a.bin"}, StageOptions{}); !errors.Is(err, ErrFilterUnsupported) {
		t.Fatalf("Stage of changed LFS content returned %v, want %v", err, ErrFilterUnsupported)
	}
}

func TestSwitchDoesNotTakeSmudgedLFSContentForALocalChange(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig(lfsFilterConfig)
	tr.repo = tr.reopen()
	tr.writeFile(".gitattributes", "*.bin filter=lfs -text\n")
	tr.writeFile("a.bin", lfsPointerOf("first"))
	stageAll(t, tr, ".gitattributes", "a.bin")
	tr.createBranch("topic", tr.commitAll("first"))
	if err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch to topic returned error %v", err)
	}
	tr.writeFile("a.bin", lfsPointerOf("second"))
	stageAll(t, tr, "a.bin")
	tr.commitAll("second")
	if err := Switch(t.Context(), tr.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch to main returned error %v", err)
	}
	tr.writeFile("a.bin", "first")
	if err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch over smudged LFS content returned error %v", err)
	}
	if got := tr.readFile("a.bin"); got != lfsPointerOf("second") {
		t.Fatalf("a.bin = %q after the switch", got)
	}
	tr.writeFile("a.bin", "local edit")
	var overwrite *OverwriteError
	if err := Switch(t.Context(), tr.repo, "main", SwitchOptions{}); !errors.As(err, &overwrite) {
		t.Fatalf("Switch over edited LFS content returned %v, want an OverwriteError", err)
	}
}

func stageAll(t *testing.T, tr *testRepo, paths ...string) {
	t.Helper()
	if err := Stage(t.Context(), tr.repo, paths, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
}

func TestStageKeepsCRLFThatTheIndexAlreadyHolds(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "one\r\ntwo\r\n")
	stageAll(t, tr, "a.txt")
	tr.commitAll("crlf")
	tr.appendConfig("[core]\n\tautocrlf = true\n")
	tr.repo = tr.reopen()
	tr.writeFile("a.txt", "one\r\ntwo\r\nthree\r\n")
	if err := Stage(t.Context(), tr.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	if entry, _ := entryOf(t, tr.index(), "a.txt"); entry.ID != blobIDOf(t, "one\r\ntwo\r\nthree\r\n") {
		t.Fatalf("a.txt lost the CRLF endings the index held")
	}
}

func racyIndex(t *testing.T, tr *testRepo, stamp time.Time) {
	t.Helper()
	idx := tr.index()
	for entry := range idx.Entries() {
		entry.Stat.MTime = stamp
		if err := os.Chtimes(tr.path(entry.Path), stamp, stamp); err != nil {
			t.Fatalf("Chtimes returned error %v", err)
		}
	}
	tr.saveIndex(idx)
	if err := os.Chtimes(tr.repo.IndexFile(), stamp, stamp); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
}

func TestIndexWritesSmudgeEntriesThatAreOnlyRacilyClean(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("edited.txt", "aaaa\n")
	tr.writeFile("kept.txt", "bbbb\n")
	if err := Stage(t.Context(), tr.repo, []string{"edited.txt", "kept.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	racyIndex(t, tr, stamp)
	tr.writeFile("edited.txt", "cccc\n")
	if err := os.Chtimes(tr.path("edited.txt"), stamp, stamp); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
	tr.writeFile("other.txt", "x")
	if err := Stage(t.Context(), tr.repo, []string{"other.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := tr.index()
	if entry, _ := entryOf(t, idx, "edited.txt"); entry.Stat.Size != 0 {
		t.Fatalf("the racily clean edited.txt keeps size %d", entry.Stat.Size)
	}
	if entry, _ := entryOf(t, idx, "kept.txt"); entry.Stat.Size != 5 {
		t.Fatalf("the unchanged kept.txt has size %d, want 5", entry.Stat.Size)
	}
}

func TestSmudgeRacilyCleanSmudgesEveryRacyEntryWithoutAWorkingTree(t *testing.T) {
	tr := newBareTestRepo(t)
	stamp := time.Unix(1700000000, 0)
	idx := index.New(index.Version2)
	idx.Add(index.Entry{Path: "a", Stat: index.Stat{MTime: stamp, Size: 3}})
	idx.Timestamp = stamp
	smudgeRacilyClean(tr.repo, idx)
	if entry, _ := entryOf(t, idx, "a"); entry.Stat.Size != 0 {
		t.Fatalf("size = %d, want 0", entry.Stat.Size)
	}
}

func TestRacilyModifiedChecksStatThenContent(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "abc")
	if err := Stage(t.Context(), tr.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	staged, _ := entryOf(t, tr.index(), "a.txt")
	wt, err := openWorkingTree(tr.repo)
	if err != nil {
		t.Fatalf("openWorkingTree returned error %v", err)
	}
	defer func() { _ = wt.close() }()

	if wt.racilyModified(&staged) {
		t.Fatalf("an unchanged file counts as modified")
	}
	missing := staged
	missing.Path = "missing.txt"
	if wt.racilyModified(&missing) {
		t.Fatalf("a missing file counts as racily modified")
	}
	resized := staged
	resized.Stat.Size = 99
	if wt.racilyModified(&resized) {
		t.Fatalf("a file whose stat differs counts as racily modified")
	}
	assumed := staged
	assumed.AssumeValid = true
	assumed.ID = blobIDOf(t, "other")
	if !wt.racilyModified(&assumed) {
		t.Fatalf("an assume-valid entry with other content counts as clean")
	}

	originalHash := hashSum
	hashSum = func(hash.Format, string, []byte) (hash.ObjectID, error) { return hash.Zero, errConversionFault }
	if !wt.racilyModified(&staged) {
		t.Fatalf("a file that cannot be hashed counts as clean")
	}
	hashSum = originalHash

	swapRootReadFile(t, func(*os.Root, string) ([]byte, error) { return nil, errConversionFault })
	if !wt.racilyModified(&staged) {
		t.Fatalf("a file that cannot be read counts as clean")
	}
}

func TestIndexBlobLoadsTheIndexOnceAndClosesTheObjectDatabase(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "one\r\n")
	stageAll(t, tr, "a.txt")
	wt, err := openWorkingTree(tr.repo)
	if err != nil {
		t.Fatalf("openWorkingTree returned error %v", err)
	}
	blob, ok := wt.indexBlob("a.txt")()
	if !ok || string(blob) != "one\r\n" {
		_ = wt.close()
		t.Fatalf("indexBlob = %q, %v", blob, ok)
	}
	if _, ok := wt.indexBlob("untracked.txt")(); ok {
		_ = wt.close()
		t.Fatalf("indexBlob found an untracked path")
	}
	if err := wt.close(); err != nil {
		t.Fatalf("close returned error %v", err)
	}
}

func TestIndexBlobIsMissingWhenTheIndexOrObjectsCannotBeRead(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "one\r\n")
	stageAll(t, tr, "a.txt")

	t.Run("objects", func(t *testing.T) {
		original := odbOpen
		odbOpen = func(string, odb.Options) (*odb.DB, error) { return nil, errConversionFault }
		t.Cleanup(func() { odbOpen = original })
		wt, err := openWorkingTree(tr.repo)
		if err != nil {
			t.Fatalf("openWorkingTree returned error %v", err)
		}
		defer func() { _ = wt.close() }()
		if _, ok := wt.indexBlob("a.txt")(); ok {
			t.Fatalf("indexBlob succeeded without an object database")
		}
	})
	t.Run("index", func(t *testing.T) {
		tr.corruptIndexFile()
		wt, err := openWorkingTree(tr.repo)
		if err != nil {
			t.Fatalf("openWorkingTree returned error %v", err)
		}
		defer func() { _ = wt.close() }()
		if _, ok := wt.indexBlob("a.txt")(); ok {
			t.Fatalf("indexBlob succeeded with a corrupt index")
		}
	})
}
