package worktree

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func lfsPointerOf(content string) string {
	return fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%x\nsize %d\n", sha256.Sum256([]byte(content)), len(content))
}

func (r *testRepo) stageContentCheckedOutAs(rel, storedContent, worktreeContent string) {
	r.t.Helper()
	id := r.stageContent(rel, storedContent)
	r.idx.Add(index.Entry{
		Path: rel,
		Mode: object.ModeBlob,
		ID:   id,
		Stat: index.Stat{MTime: time.Unix(1, 0), CTime: time.Unix(1, 0), Size: uint32(len(worktreeContent))},
	})
}

func TestStatusComparesLFSContentWithThePointerInTheIndex(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[filter \"lfs\"]\n\tprocess = git-lfs filter-process\n")
	tr.repo = tr.reopen()
	tr.stage(".gitattributes", "*.bin filter=lfs -text\n")
	content := "binary\x00payload"
	pointer := lfsPointerOf(content)
	for rel, worktreeContent := range map[string]string{"smudged.bin": content, "pointer.bin": pointer, "changed.bin": "changed\x00payload"} {
		tr.stageContentCheckedOutAs(rel, pointer, worktreeContent)
	}
	tr.commit("lfs")
	tr.writeFile("smudged.bin", content)
	tr.writeFile("pointer.bin", pointer)
	tr.writeFile("changed.bin", "changed\x00payload")

	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	for _, rel := range []string{"smudged.bin", "pointer.bin"} {
		if entry, ok := entries[rel]; ok {
			t.Errorf("%s is reported as %#v, want clean", rel, entry)
		}
	}
	if entry := entries["changed.bin"]; entry.Unstaged != StatusModified {
		t.Errorf("changed.bin entry = %#v, want Unstaged=Modified", entry)
	}
	data, ok, err := w.WorkingFile("smudged.bin")
	if err != nil || !ok || string(data) != pointer {
		t.Fatalf("WorkingFile = %q, %v, %v, want the pointer", data, ok, err)
	}
}

func TestStatusExpandsIdentBeforeComparing(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitattributes", "*.c ident\n")
	expanded := "$Id: 0123456789abcdef0123456789abcdef01234567 $\n"
	tr.stageContentCheckedOutAs("a.c", "$Id$\n", expanded)
	tr.commit("ident")
	tr.writeFile("a.c", expanded)
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if entry, ok := entryMap(status.Entries)["a.c"]; ok {
		t.Fatalf("a.c is reported as %#v, want clean", entry)
	}
}
