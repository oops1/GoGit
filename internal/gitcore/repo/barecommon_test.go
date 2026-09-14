package repo

import (
	"path/filepath"
	"testing"
)

func TestDiscoverOpensALinkedWorktreeOfABareRepository(t *testing.T) {
	base := tempDir(t)
	main, work, gitDir := linkedWorktree(t, base, "second")
	writeFile(t, filepath.Join(main, configFile), "[core]\n\tbare = true\n\tworktree = ../elsewhere\n")

	layout := mustDiscover(t, gitDir, env{envGitDir: gitDir})

	if layout.Bare || layout.WorkTree != work {
		t.Fatalf("layout = Bare %v, WorkTree %q; want the linked work tree %q", layout.Bare, layout.WorkTree, work)
	}
}
