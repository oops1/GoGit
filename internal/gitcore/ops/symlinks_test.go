package ops

import (
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func newSymlinkRepo(t *testing.T, symlinks string) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tsymlinks = " + symlinks + "\n")
	r.repo = r.reopen()
	id, err := r.db().Put(object.TypeBlob, []byte("target.txt"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "link", Mode: object.ModeSymlink, ID: id, Stage: index.StageMerged})
	r.saveIndex(idx)
	return r
}

func TestDiscardWritesASymlinkAsAPlainFileWhenSymlinksAreOff(t *testing.T) {
	r := newSymlinkRepo(t, "false")
	if err := Discard(t.Context(), r.repo, []string{"link"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	info, err := os.Lstat(r.path("link"))
	if err != nil {
		t.Fatalf("Lstat returned error %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("link has mode %v, want a regular file", info.Mode())
	}
	if got := r.readFile("link"); got != "target.txt" {
		t.Fatalf("link holds %q, want %q", got, "target.txt")
	}
}

func TestDiscardCreatesASymlinkWhenSymlinksAreOn(t *testing.T) {
	r := newSymlinkRepo(t, "true")
	previous := fsRootSymlink
	t.Cleanup(func() { fsRootSymlink = previous })
	var created []string
	fsRootSymlink = func(_ *os.Root, oldname, newname string) error {
		created = append(created, oldname+" <- "+newname)
		return nil
	}
	if err := Discard(t.Context(), r.repo, []string{"link"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if len(created) != 1 || created[0] != "target.txt <- link" {
		t.Fatalf("Discard created symlinks %q, want one link to target.txt", created)
	}
	if r.exists("link") {
		t.Fatal("Discard wrote a plain file instead of a symlink")
	}
}

func TestStageKeepsTheSymlinkModeOfAPlainFileOnlyWhenSymlinksAreOff(t *testing.T) {
	tests := []struct {
		symlinks string
		want     object.Mode
	}{
		{"false", object.ModeSymlink},
		{"true", object.ModeBlob},
	}
	for _, test := range tests {
		t.Run("symlinks="+test.symlinks, func(t *testing.T) {
			r := newSymlinkRepo(t, test.symlinks)
			r.writeFile("link", "elsewhere.txt")
			mustStage(t, r, "link")
			entry, ok := entryOf(t, r.index(), "link")
			if !ok {
				t.Fatal("link is not staged")
			}
			if entry.Mode != test.want {
				t.Fatalf("mode = %s, want %s", entry.Mode, test.want)
			}
			_, data, err := r.db().Get(entry.ID)
			if err != nil {
				t.Fatalf("Get returned error %v", err)
			}
			if string(data) != "elsewhere.txt" {
				t.Fatalf("staged content = %q, want %q", data, "elsewhere.txt")
			}
		})
	}
}
