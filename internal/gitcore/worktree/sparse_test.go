package worktree

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
)

func TestStatusIgnoresSkipWorktreeEntriesMissingFromDisk(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.stage("sparse/b.txt", "world\n")
	tr.commit("initial")
	entry, ok := tr.idx.Get("sparse/b.txt", index.StageMerged)
	if !ok {
		t.Fatal("the sparse entry is not staged")
	}
	entry.SkipWorktree = true
	tr.remove("sparse/b.txt")
	tr.remove("sparse")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if len(status.Entries) != 0 {
		t.Fatalf("entries = %+v, want none", status.Entries)
	}
}
