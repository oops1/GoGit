package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSavingAResolutionRefusesAPathGitWouldNotWrite(t *testing.T) {
	tr := conflictedRepo(t)
	config := filepath.Join(tr.repo.GitDir(), "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}

	err = SaveResolution(t.Context(), tr.repo, ".git/config", []byte("[core]\n"), ResolutionOptions{})

	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
	if after, _ := os.ReadFile(config); string(after) != string(before) {
		t.Fatalf("the repository configuration was rewritten to %q", after)
	}
}

func TestSavingAResolutionReplacesAFileInTheLeadingPath(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"dir/f": "ours\n"}, map[string]string{"dir/f": "theirs\n"})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	tr.remove("dir")
	tr.writeFile("dir", "in the way\n")

	if err := SaveResolution(t.Context(), tr.repo, "dir/f", []byte("resolved\n"), ResolutionOptions{}); err != nil {
		t.Fatalf("SaveResolution returned error %v", err)
	}

	if got := tr.readFile("dir/f"); got != "resolved\n" {
		t.Fatalf("dir/f = %q, want the resolution", got)
	}
}

func TestTheRebaseStateReplacesAFileInPlaceOfItsDirectory(t *testing.T) {
	tr := newTestRepo(t)
	stray := filepath.Join(tr.repo.GitDir(), rebaseDir)
	if err := os.WriteFile(stray, []byte("stray\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	if err := writeRebaseState(tr.repo, RebaseState{HeadName: "refs/heads/topic"}); err != nil {
		t.Fatalf("writeRebaseState returned error %v", err)
	}

	if info, err := os.Lstat(stray); err != nil || !info.IsDir() {
		t.Fatalf("%s = %v, %v, want a directory", rebaseDir, info, err)
	}
}
