package repo

import (
	"path/filepath"
	"testing"
)

func TestDiscoverWorkTreeFindsTheRepositoryInsideADirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))

	layout, ok := DiscoverWorkTree(work)

	if !ok || layout.GitDir != gitDir || layout.WorkTree != work {
		t.Fatalf("layout = %+v, %v", layout, ok)
	}
}

func TestDiscoverWorkTreeFollowsAGitFile(t *testing.T) {
	base := tempDir(t)
	store := plainGitDir(t, filepath.Join(base, "store.git"))
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit), gitFilePrefix+" ../store.git\n")

	layout, ok := DiscoverWorkTree(work)

	if !ok || layout.GitDir != store || layout.WorkTree != work {
		t.Fatalf("layout = %+v, %v", layout, ok)
	}
}

func TestDiscoverWorkTreeLooksNeitherUpwardsNorAtABareDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	bare := plainGitDir(t, filepath.Join(base, "bare.git"))

	for _, dir := range []string{makeDir(t, filepath.Join(work, "libs", "sub")), bare} {
		if layout, ok := DiscoverWorkTree(dir); ok {
			t.Errorf("%s: layout = %+v", dir, layout)
		}
	}
}

func TestDiscoverWorkTreeTreatsABrokenGitFileAsNoRepository(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit), gitFilePrefix+" ../absent\n")

	if layout, ok := DiscoverWorkTree(work); ok {
		t.Fatalf("layout = %+v", layout)
	}
}

func TestDiscoverWorkTreeIgnoresTheWorkTreeOfTheEnvironment(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	t.Setenv(envWorkTree, makeDir(t, filepath.Join(base, "elsewhere")))

	if layout, ok := DiscoverWorkTree(work); !ok || layout.WorkTree != work {
		t.Fatalf("layout = %+v, %v", layout, ok)
	}
}

func TestOpenKeepsTheOptionsItWasOpenedWith(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	opts := openOptions(t, env{})

	repository := openRepo(t, work, opts)

	if got := repository.Options(); got.GlobalFile != opts.GlobalFile || !got.NoSystem {
		t.Fatalf("options = %+v, want %+v", got, opts)
	}
}
