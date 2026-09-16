package worktree

import (
	"os"
	"testing"
)

func TestStatusReadsAnIndexAnotherClientWroteAfterOpening(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	w := tr.open()
	tr.writeFile("b.txt", "second\n")
	if status, err := w.Status(t.Context()); err != nil || entryMap(status.Entries)["b.txt"].Unstaged != StatusUntracked {
		t.Fatalf("status before staging = %#v, %v", status, err)
	}

	tr.stage("b.txt", "second\n")
	tr.saveIndex()

	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if entry := entryMap(status.Entries)["b.txt"]; entry.Staged != StatusAdded || entry.Unstaged != StatusUnmodified {
		t.Fatalf("b.txt entry = %#v, want the staged addition another client wrote", entry)
	}
	if _, ok := w.Index().Get("b.txt", 0); !ok {
		t.Fatal("Index must return the index read from disk")
	}
}

func TestStatusFailsWhenTheIndexOnDiskBecameUnreadable(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	w := tr.open()
	if err := os.WriteFile(tr.repo.IndexFile(), []byte("not an index file"), 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Status(t.Context()); err == nil {
		t.Fatal("Status must report an index it cannot read")
	}
}
