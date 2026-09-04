package worktree

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (r *testRepo) stageContent(rel, storedContent string) hash.ObjectID {
	r.t.Helper()
	id, err := r.db.Put(object.TypeBlob, []byte(storedContent))
	if err != nil {
		r.t.Fatalf("Put returned error %v", err)
	}
	r.idx.Add(index.Entry{
		Path: rel,
		Mode: object.ModeBlob,
		ID:   id,
		Stat: index.Stat{MTime: time.Unix(1, 0), CTime: time.Unix(1, 0), Size: uint32(len(storedContent))},
	})
	return id
}

func TestStatusIsEmptyForACleanWorktree(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("dir/b.txt", "world\n")
	tr.commit("initial")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if len(status.Entries) != 0 {
		t.Fatalf("Entries = %#v, want none", status.Entries)
	}
	if status.HeadBranch != "main" || status.Detached {
		t.Fatalf("HeadBranch=%q Detached=%v, want main/false", status.HeadBranch, status.Detached)
	}
	if status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("Ahead=%d Behind=%d, want 0/0", status.Ahead, status.Behind)
	}
}

func TestStatusOnEmptyWorktreeIsEmpty(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if len(status.Entries) != 0 {
		t.Fatalf("Entries = %#v, want none", status.Entries)
	}
}

func TestStatusOnUnbornHeadReportsStagedFilesAsAdded(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["a.txt"]
	if !ok || entry.Staged != StatusAdded {
		t.Fatalf("a.txt entry = %#v, want Staged=Added", entry)
	}
	if status.HeadBranch != "main" || status.Detached {
		t.Fatalf("HeadBranch=%q Detached=%v, want main/false", status.HeadBranch, status.Detached)
	}
}

func TestStatusReportsUnstagedModification(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("a.txt", "goodbye\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["a.txt"]
	if !ok || entry.Unstaged != StatusModified || entry.Staged != StatusUnmodified {
		t.Fatalf("a.txt entry = %#v, want Unstaged=Modified", entry)
	}
}

func TestStatusReportsStagedModification(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.stage("a.txt", "goodbye\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["a.txt"]
	if !ok || entry.Staged != StatusModified || entry.Unstaged != StatusUnmodified {
		t.Fatalf("a.txt entry = %#v, want Staged=Modified", entry)
	}
}

func TestStatusReportsAddedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.stage("b.txt", "new\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["b.txt"]
	if !ok || entry.Staged != StatusAdded {
		t.Fatalf("b.txt entry = %#v, want Staged=Added", entry)
	}
}

func TestStatusReportsStagedDeletion(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.unstage("a.txt")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["a.txt"]
	if !ok || entry.Staged != StatusDeleted {
		t.Fatalf("a.txt entry = %#v, want Staged=Deleted", entry)
	}
}

func TestStatusReportsUnstagedDeletion(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.remove("a.txt")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["a.txt"]
	if !ok || entry.Unstaged != StatusDeleted || entry.Staged != StatusUnmodified {
		t.Fatalf("a.txt entry = %#v, want Unstaged=Deleted", entry)
	}
}

func TestStatusReportsRenamedFileThroughTheIndex(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("old.txt", "same content\n")
	tr.commit("initial")
	tr.unstage("old.txt")
	tr.remove("old.txt")
	tr.writeFile("new.txt", "same content\n")
	tr.stageExisting("new.txt", object.ModeBlob)
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["new.txt"]
	if !ok || entry.Staged != StatusRenamed || entry.OrigPath != "old.txt" {
		t.Fatalf("new.txt entry = %#v, want Staged=Renamed OrigPath=old.txt", entry)
	}
	if _, ok := entries["old.txt"]; ok {
		t.Fatalf("old.txt should not appear as a separate deletion after being matched as a rename")
	}
}

func TestStatusReportsUntrackedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("new.txt", "content\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["new.txt"]
	if !ok || entry.Unstaged != StatusUntracked || entry.IsDir {
		t.Fatalf("new.txt entry = %#v, want Unstaged=Untracked IsDir=false", entry)
	}
}

func TestStatusCollapsesAFullyUntrackedDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("newdir/one.txt", "1\n")
	tr.writeFile("newdir/nested/two.txt", "2\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["newdir/"]
	if !ok || entry.Unstaged != StatusUntracked || !entry.IsDir {
		t.Fatalf("newdir/ entry = %#v, want a collapsed untracked directory", entry)
	}
	if _, ok := entries["newdir/one.txt"]; ok {
		t.Fatalf("newdir/one.txt should have been collapsed into the parent directory")
	}
}

func TestStatusRecursesIntoAPartiallyTrackedDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("dir/tracked.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("dir/untracked.txt", "new\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["dir/untracked.txt"]
	if !ok || entry.Unstaged != StatusUntracked || entry.IsDir {
		t.Fatalf("dir/untracked.txt entry = %#v, want Unstaged=Untracked", entry)
	}
	if _, ok := entries["dir/"]; ok {
		t.Fatalf("dir/ should not be collapsed since it holds a tracked file")
	}
}

func TestStatusIgnoresAnEmptyUntrackedDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.mkdir("empty")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if len(status.Entries) != 0 {
		t.Fatalf("Entries = %#v, want none", status.Entries)
	}
}

func TestStatusReportsAnIgnoredFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "ignored.txt\n")
	tr.commit("initial")
	tr.writeFile("ignored.txt", "content\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["ignored.txt"]
	if !ok || entry.Staged != StatusUnmodified || entry.Unstaged != StatusIgnored || entry.IsDir {
		t.Fatalf("ignored.txt entry = %#v, want Staged=Unmodified Unstaged=Ignored IsDir=false", entry)
	}
}

func TestStatusCollapsesAnIgnoredDirectoryWithoutTrackedFiles(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "ignored/\n")
	tr.commit("initial")
	tr.writeFile("ignored/one.txt", "content\n")
	tr.writeFile("ignored/nested/two.txt", "content\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["ignored/"]
	if !ok || entry.Staged != StatusUnmodified || entry.Unstaged != StatusIgnored || !entry.IsDir {
		t.Fatalf("ignored/ entry = %#v, want a collapsed ignored directory", entry)
	}
	if _, ok := entries["ignored/one.txt"]; ok {
		t.Fatalf("ignored/one.txt should have been collapsed into the parent directory")
	}
}

func TestStatusRecursesIntoAnIgnoredDirectoryHoldingAForceTrackedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "ignored/\n")
	tr.stage("ignored/tracked.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("ignored/tracked.txt", "changed\n")
	tr.writeFile("ignored/untracked.txt", "content\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	if _, ok := entries["ignored/"]; ok {
		t.Fatalf("ignored/ must not collapse since it holds a tracked file")
	}
	tracked, ok := entries["ignored/tracked.txt"]
	if !ok || tracked.Unstaged != StatusModified {
		t.Fatalf("ignored/tracked.txt entry = %#v, want Unstaged=Modified", tracked)
	}
	untracked, ok := entries["ignored/untracked.txt"]
	if !ok || untracked.Staged != StatusUnmodified || untracked.Unstaged != StatusIgnored {
		t.Fatalf("ignored/untracked.txt entry = %#v, want Staged=Unmodified Unstaged=Ignored", untracked)
	}
}

func TestStatusReportsAnIgnoredFileNextToAModifiedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "*.log\n")
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("a.txt", "goodbye\n")
	tr.writeFile("debug.log", "noise\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	modified, ok := entries["a.txt"]
	if !ok || modified.Unstaged != StatusModified {
		t.Fatalf("a.txt entry = %#v, want Unstaged=Modified", modified)
	}
	ignored, ok := entries["debug.log"]
	if !ok || ignored.Staged != StatusUnmodified || ignored.Unstaged != StatusIgnored {
		t.Fatalf("debug.log entry = %#v, want Staged=Unmodified Unstaged=Ignored", ignored)
	}
}

func TestStatusNormalizesCRLFWhenTextAutoIsSet(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitattributes", "* text=auto\n")
	tr.stageContent("crlf.txt", "line1\nline2\n")
	tr.commit("initial")
	tr.writeFile("crlf.txt", "line1\r\nline2\r\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if entry, ok := entryMap(status.Entries)["crlf.txt"]; ok {
		t.Fatalf("crlf.txt entry = %#v, want no entry (content matches after normalization)", entry)
	}
}

func TestStatusDetectsExecutableBitChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows не хранит бит исполняемости в файловой системе NTFS для core.filemode")
	}
	tr := newTestRepo(t)
	tr.stage("run.sh", "echo hi\n")
	tr.commit("initial")
	if err := os.Chmod(tr.path("run.sh"), 0o755); err != nil {
		t.Fatalf("Chmod returned error %v", err)
	}
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["run.sh"]
	if !ok || entry.Unstaged != StatusModified {
		t.Fatalf("run.sh entry = %#v, want Unstaged=Modified", entry)
	}
}

func TestStatusDetectsSymlinkTargetChange(t *testing.T) {
	tr := newTestRepo(t)
	if !tr.symlink("old-target.txt", "link") {
		t.Skip("платформа не позволяет создавать симлинки без повышенных прав")
	}
	tr.stageExisting("link", object.ModeSymlink)
	tr.commit("initial")
	tr.remove("link")
	if !tr.symlink("new-target.txt", "link") {
		t.Skip("платформа не позволяет создавать симлинки без повышенных прав")
	}
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["link"]
	if !ok || entry.Unstaged != StatusModified {
		t.Fatalf("link entry = %#v, want Unstaged=Modified", entry)
	}
}

func TestStatusDetectsTypeChangeFromFileToSymlink(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("thing.txt", "content\n")
	tr.commit("initial")
	tr.remove("thing.txt")
	if !tr.symlink("target.txt", "thing.txt") {
		t.Skip("платформа не позволяет создавать симлинки без повышенных прав")
	}
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	entry, ok := entries["thing.txt"]
	if !ok || entry.Unstaged != StatusTypeChanged {
		t.Fatalf("thing.txt entry = %#v, want Unstaged=TypeChanged", entry)
	}
}

func TestStatusReportsConflicts(t *testing.T) {
	tests := []struct {
		name        string
		hasAncestor bool
		hasOurs     bool
		hasTheirs   bool
		want        ConflictKind
	}{
		{"both modified", true, true, true, ConflictBothModified},
		{"both added", false, true, true, ConflictBothAdded},
		{"deleted by us", true, false, true, ConflictDeletedByUs},
		{"deleted by them", true, true, false, ConflictDeletedByThem},
		{"added by us", false, true, false, ConflictAddedByUs},
		{"added by them", false, false, true, ConflictAddedByThem},
		{"both deleted", true, false, false, ConflictBothDeleted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestRepo(t)
			tr.stage("clean.txt", "clean\n")
			tr.commit("initial")
			if tc.hasAncestor {
				tr.stageBlob("conflict.txt", "base\n", index.StageAncestor)
			}
			if tc.hasOurs {
				tr.stageBlob("conflict.txt", "ours\n", index.StageOurs)
			}
			if tc.hasTheirs {
				tr.stageBlob("conflict.txt", "theirs\n", index.StageTheirs)
			}
			tr.writeFile("conflict.txt", "ours\n")
			w := tr.open()
			status, err := w.Status(t.Context())
			if err != nil {
				t.Fatalf("Status returned error %v", err)
			}
			entries := entryMap(status.Entries)
			entry, ok := entries["conflict.txt"]
			if !ok {
				t.Fatalf("conflict.txt entry missing")
			}
			if entry.Conflict != tc.want {
				t.Fatalf("Conflict = %v, want %v", entry.Conflict, tc.want)
			}
			if entry.Staged != StatusUnmerged || entry.Unstaged != StatusUnmerged {
				t.Fatalf("entry = %#v, want Staged=Unmerged Unstaged=Unmerged", entry)
			}
		})
	}
}

func TestStatusReportsDetachedHead(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	commitID := tr.commit("initial")
	if err := os.WriteFile(tr.repo.GitPath("HEAD"), []byte(commitID.String()+"\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if !status.Detached || status.HeadBranch != "" {
		t.Fatalf("Detached=%v HeadBranch=%q, want true/empty", status.Detached, status.HeadBranch)
	}
}

func TestStatusFailsWhenContextIsAlreadyCanceled(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.Status(ctx); err == nil {
		t.Fatalf("Status returned no error for a canceled context")
	}
}

func TestStatusFailsWhenTheFileLimitIsExceeded(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("one.txt", "1\n")
	tr.writeFile("two.txt", "2\n")
	w := tr.openWith(Options{MaxFiles: 1})
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error, want ErrTooManyFiles")
	}
}

func TestStatusReportsSizeAndModTimeForAnUntrackedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("new.txt", "content\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["new.txt"]
	if !ok {
		t.Fatalf("new.txt entry missing")
	}
	if entry.Size != int64(len("content\n")) {
		t.Fatalf("Size = %d, want %d", entry.Size, len("content\n"))
	}
	if entry.ModTime.IsZero() {
		t.Fatalf("ModTime is zero, want a real timestamp")
	}
}

func TestStatusReportsSizeAndModTimeForAModifiedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("a.txt", "goodbye!!\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["a.txt"]
	if !ok {
		t.Fatalf("a.txt entry missing")
	}
	if entry.Size != int64(len("goodbye!!\n")) {
		t.Fatalf("Size = %d, want %d", entry.Size, len("goodbye!!\n"))
	}
	if entry.ModTime.IsZero() {
		t.Fatalf("ModTime is zero, want a real timestamp")
	}
}

func TestStatusLeavesSizeAndModTimeZeroForADeletedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.remove("a.txt")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["a.txt"]
	if !ok {
		t.Fatalf("a.txt entry missing")
	}
	if entry.Size != 0 || !entry.ModTime.IsZero() {
		t.Fatalf("entry = %#v, want zero Size/ModTime for a deleted file", entry)
	}
}

func TestStatusLeavesSizeAndModTimeZeroForACollapsedUntrackedDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	tr.writeFile("newdir/one.txt", "1\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["newdir/"]
	if !ok {
		t.Fatalf("newdir/ entry missing")
	}
	if entry.Size != 0 || !entry.ModTime.IsZero() {
		t.Fatalf("entry = %#v, want zero Size/ModTime for a directory entry", entry)
	}
}

func TestStatusEntriesAreSortedByPath(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("z.txt", "z\n")
	tr.commit("initial")
	tr.writeFile("a.txt", "a\n")
	tr.writeFile("m.txt", "m\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	for i := 1; i < len(status.Entries); i++ {
		if status.Entries[i-1].Path >= status.Entries[i].Path {
			t.Fatalf("Entries are not sorted: %q >= %q", status.Entries[i-1].Path, status.Entries[i].Path)
		}
	}
}

func TestStatusOmitsUnmodifiedEntriesByDefault(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("dir/b.txt", "world\n")
	tr.commit("initial")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if len(status.Entries) != 0 {
		t.Fatalf("Entries = %#v, want none", status.Entries)
	}
}

func TestStatusIncludesUnmodifiedEntriesWhenEnabled(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("dir/b.txt", "world\n")
	tr.commit("initial")
	w := tr.openWith(Options{IncludeUnmodified: true})
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	for _, path := range []string{"a.txt", "dir/b.txt"} {
		entry, ok := entries[path]
		if !ok {
			t.Fatalf("entry %q missing, want it reported as unmodified", path)
		}
		if entry.Staged != StatusUnmodified || entry.Unstaged != StatusUnmodified {
			t.Fatalf("entry %q = %#v, want Staged=Unmodified Unstaged=Unmodified", path, entry)
		}
	}
}

func TestStatusIncludesUnmodifiedEntriesAlongsideModifiedOnes(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("b.txt", "unchanged\n")
	tr.commit("initial")
	tr.writeFile("a.txt", "changed\n")
	tr.writeFile("new.txt", "fresh\n")
	w := tr.openWith(Options{IncludeUnmodified: true})
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	if entry, ok := entries["a.txt"]; !ok || entry.Staged != StatusUnmodified || entry.Unstaged != StatusModified {
		t.Fatalf("a.txt entry = %#v, want Staged=Unmodified Unstaged=Modified", entry)
	}
	if entry, ok := entries["b.txt"]; !ok || entry.Staged != StatusUnmodified || entry.Unstaged != StatusUnmodified {
		t.Fatalf("b.txt entry = %#v, want Staged=Unmodified Unstaged=Unmodified", entry)
	}
	if entry, ok := entries["new.txt"]; !ok || entry.Staged != StatusUnmodified || entry.Unstaged != StatusUntracked {
		t.Fatalf("new.txt entry = %#v, want Staged=Unmodified Unstaged=Untracked", entry)
	}
	if len(status.Entries) != 3 {
		t.Fatalf("Entries = %#v, want exactly a.txt, b.txt and new.txt", status.Entries)
	}
	for i := 1; i < len(status.Entries); i++ {
		if status.Entries[i-1].Path >= status.Entries[i].Path {
			t.Fatalf("Entries are not sorted: %q >= %q", status.Entries[i-1].Path, status.Entries[i].Path)
		}
	}
}

func TestStatusFailsWhenTheFileLimitIsExceededByUnmodifiedEntries(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("b.txt", "world\n")
	tr.stage("c.txt", "third\n")
	tr.commit("initial")
	w := tr.openWith(Options{IncludeUnmodified: true, MaxFiles: 2})
	if _, err := w.Status(t.Context()); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("Status returned error %v, want ErrTooManyFiles", err)
	}
}

func TestStatusIncludesUnmodifiedEntriesWithinTheFileLimit(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("b.txt", "world\n")
	tr.commit("initial")
	w := tr.openWith(Options{IncludeUnmodified: true, MaxFiles: 3})
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if len(status.Entries) != 2 {
		t.Fatalf("Entries = %#v, want exactly 2 entries", status.Entries)
	}
}
