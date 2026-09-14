package worktree

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func (r *testRepo) configureSymlinks(value string) {
	r.t.Helper()
	data, err := r.repo.CommonRoot().ReadFile("config")
	if err != nil {
		r.t.Fatalf("ReadFile returned error %v", err)
	}
	data = append(data, []byte("[core]\n\tsymlinks = "+value+"\n")...)
	if err := r.repo.CommonRoot().WriteFile("config", data, 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
	global := filepath.Join(r.t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
	reopened, err := repo.Open(r.dir, repo.OpenOptions{NoSystem: true, GlobalFile: global})
	if err != nil {
		r.t.Fatalf("repo.Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = reopened.Close() })
	r.repo = reopened
}

func (r *testRepo) stagePlainSymlink(rel, target string) {
	r.t.Helper()
	r.writeFile(rel, target)
	fi, err := os.Lstat(r.path(rel))
	if err != nil {
		r.t.Fatalf("Lstat returned error %v", err)
	}
	id, err := r.db.Put(object.TypeBlob, []byte(target))
	if err != nil {
		r.t.Fatalf("Put returned error %v", err)
	}
	r.idx.Add(index.Entry{
		Path: rel,
		Mode: object.ModeSymlink,
		ID:   id,
		Stat: index.Stat{MTime: fi.ModTime(), CTime: fi.ModTime(), Size: uint32(fi.Size())},
	})
}

func TestStatusComparesAPlainFileWithASymlinkEntryByContentWhenSymlinksAreOff(t *testing.T) {
	tests := []struct {
		name     string
		symlinks string
		change   func(tr *testRepo)
		want     StatusCode
	}{
		{"untouched file", "false", func(tr *testRepo) {}, StatusUnmodified},
		{"same target with a new timestamp", "false", func(tr *testRepo) {
			later := time.Unix(1800000000, 0)
			if err := os.Chtimes(tr.path("link"), later, later); err != nil {
				tr.t.Fatalf("Chtimes returned error %v", err)
			}
		}, StatusUnmodified},
		{"new target", "false", func(tr *testRepo) { tr.writeFile("link", "elsewhere.txt") }, StatusModified},
		{"directory in place of the file", "false", func(tr *testRepo) {
			tr.remove("link")
			tr.writeFile("link/inner.txt", "inner\n")
		}, StatusDeleted},
		{"plain file while symlinks are on", "true", func(tr *testRepo) {}, StatusTypeChanged},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tr := newTestRepo(t)
			tr.configureSymlinks(test.symlinks)
			tr.stagePlainSymlink("link", "target.txt")
			tr.commit("initial")
			test.change(tr)
			status, err := tr.open().Status(t.Context())
			if err != nil {
				t.Fatalf("Status returned error %v", err)
			}
			entry, ok := entryMap(status.Entries)["link"]
			if got := entry.Unstaged; !ok && test.want != StatusUnmodified || ok && got != test.want {
				t.Fatalf("link entry = %#v (reported %v), want Unstaged=%v", entry, ok, test.want)
			}
		})
	}
}
