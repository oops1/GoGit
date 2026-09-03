package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func stubFilesystemID(t *testing.T, stub func(string) (string, error)) {
	t.Helper()
	previous := filesystemIDOf
	filesystemIDOf = stub
	t.Cleanup(func() { filesystemIDOf = previous })
}

func TestDiscoverFindsRepositoryAtStartDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))

	layout := mustDiscover(t, work, env{})
	if layout.GitDir != gitDir || layout.CommonDir != gitDir {
		t.Errorf("layout has GitDir %q and CommonDir %q, want both %q", layout.GitDir, layout.CommonDir, gitDir)
	}
	if layout.WorkTree != work {
		t.Errorf("layout has WorkTree %q, want %q", layout.WorkTree, work)
	}
	if layout.Bare || layout.IsWorktree {
		t.Errorf("layout reports Bare=%v IsWorktree=%v, want both false", layout.Bare, layout.IsWorktree)
	}
}

func TestDiscoverClimbsToTheParentRepository(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	deep := makeDir(t, filepath.Join(work, "a", "b", "c"))

	layout := mustDiscover(t, deep, env{})
	if layout.GitDir != gitDir || layout.WorkTree != work {
		t.Fatalf("layout = %+v, want GitDir %q and WorkTree %q", layout, gitDir, work)
	}
}

func TestDiscoverAcceptsRelativeStartPath(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	makeDir(t, filepath.Join(work, "sub"))
	t.Chdir(work)

	layout := mustDiscover(t, "sub", env{})
	if layout.WorkTree != absClean(work) {
		t.Fatalf("layout has WorkTree %q, want %q", layout.WorkTree, absClean(work))
	}
}

func TestDiscoverFindsBareRepository(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	writeFile(t, filepath.Join(gitDir, configFile), "[core]\n\tbare = true\n")

	layout := mustDiscover(t, gitDir, env{})
	if !layout.Bare || layout.WorkTree != "" {
		t.Fatalf("layout = %+v, want a bare repository without a work tree", layout)
	}
	if layout.GitDir != gitDir {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
}

func TestDiscoverTreatsGitDirWithoutWorkTreeAsBare(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))

	layout := mustDiscover(t, gitDir, env{})
	if !layout.Bare {
		t.Fatalf("layout = %+v, want a bare repository", layout)
	}
}

func TestDiscoverFollowsGitFileWithTrailingCarriageReturn(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit), gitFilePrefix+" ../store.git\r\n")

	layout := mustDiscover(t, work, env{})
	if layout.GitDir != gitDir || layout.WorkTree != work {
		t.Fatalf("layout = %+v, want GitDir %q and WorkTree %q", layout, gitDir, work)
	}
}

func TestDiscoverReportsInvalidGitFile(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit), "nonsense\n")

	if _, err := Discover(work, DiscoverOptions{Env: env{}.get}); !errors.Is(err, ErrInvalidGitDirFile) {
		t.Fatalf("Discover returned %v, want ErrInvalidGitDirFile", err)
	}
}

func TestDiscoverReportsGitFilePointingAtMissingDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit), gitFilePrefix+" ../absent\n")

	if _, err := Discover(work, DiscoverOptions{Env: env{}.get}); !errors.Is(err, ErrInvalidGitDirFile) {
		t.Fatalf("Discover returned %v, want ErrInvalidGitDirFile", err)
	}
}

func TestDiscoverFindsLinkedWorktree(t *testing.T) {
	base := tempDir(t)
	main, work, gitDir := linkedWorktree(t, base, "second")

	layout := mustDiscover(t, work, env{})
	if layout.GitDir != gitDir {
		t.Errorf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
	if layout.CommonDir != main {
		t.Errorf("layout has CommonDir %q, want %q", layout.CommonDir, main)
	}
	if layout.WorkTree != work {
		t.Errorf("layout has WorkTree %q, want %q", layout.WorkTree, work)
	}
	if !layout.IsWorktree || layout.Bare {
		t.Errorf("layout reports IsWorktree=%v Bare=%v, want true and false", layout.IsWorktree, layout.Bare)
	}
}

func TestDiscoverUsesWorktreeGitDirFileWhenNoHintIsAvailable(t *testing.T) {
	base := tempDir(t)
	_, work, gitDir := linkedWorktree(t, base, "second")

	layout := mustDiscover(t, gitDir, env{envGitDir: gitDir})
	if layout.WorkTree != work {
		t.Fatalf("layout has WorkTree %q, want %q", layout.WorkTree, work)
	}
}

func TestDiscoverReportsMissingWorkTreeForLinkedWorktree(t *testing.T) {
	base := tempDir(t)
	main, _, gitDir := linkedWorktree(t, base, "second")
	writeFile(t, filepath.Join(main, configFile), "[core]\n\tbare = false\n")
	if err := os.Remove(filepath.Join(gitDir, gitDirFile)); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}

	_, err := Discover(gitDir, env{envGitDir: gitDir}.discoverOptions())
	if !errors.Is(err, ErrNotBareNoWorkTree) {
		t.Fatalf("Discover returned %v, want ErrNotBareNoWorkTree", err)
	}
}

func TestDiscoverIgnoresEmptyWorktreeGitDirFile(t *testing.T) {
	base := tempDir(t)
	_, _, gitDir := linkedWorktree(t, base, "second")
	writeFile(t, filepath.Join(gitDir, gitDirFile), "\n")

	layout := mustDiscover(t, gitDir, env{envGitDir: gitDir})
	if !layout.Bare {
		t.Fatalf("layout = %+v, want a bare layout when the gitdir file carries no path", layout)
	}
}

func TestDiscoverStopsAtCeilingDirectory(t *testing.T) {
	base := tempDir(t)
	plainGitDir(t, filepath.Join(base, dotGit))
	ceiling := makeDir(t, filepath.Join(base, "ceiling"))
	deep := makeDir(t, filepath.Join(ceiling, "deep"))

	_, err := Discover(deep, env{envCeiling: ceiling}.discoverOptions())
	if !errors.Is(err, ErrCeilingReached) {
		t.Fatalf("Discover returned %v, want ErrCeilingReached", err)
	}
}

func TestDiscoverIgnoresCeilingBelowTheStartDirectory(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, dotGit))
	deep := makeDir(t, filepath.Join(base, "deep"))

	layout := mustDiscover(t, deep, env{envCeiling: filepath.Join(deep, "under")})
	if layout.GitDir != gitDir {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
}

func TestDiscoverReportsNotFoundAtTheFilesystemRoot(t *testing.T) {
	root := volumeRoot(tempDir(t))
	stubFilesystemID(t, func(string) (string, error) { return "same", nil })

	_, err := Discover(root, DiscoverOptions{Env: env{}.get})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Discover returned %v, want ErrNotFound", err)
	}
}

func TestDiscoverStopsAtFilesystemBoundary(t *testing.T) {
	base := tempDir(t)
	plainGitDir(t, filepath.Join(base, dotGit))
	deep := makeDir(t, filepath.Join(base, "deep"))
	stubFilesystemID(t, func(path string) (string, error) { return path, nil })

	_, err := Discover(deep, DiscoverOptions{Env: env{}.get})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Discover returned %v, want ErrNotFound", err)
	}
}

func TestDiscoverCrossesFilesystemBoundaryWhenAllowed(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, dotGit))
	deep := makeDir(t, filepath.Join(base, "deep"))
	stubFilesystemID(t, func(string) (string, error) {
		t.Error("filesystem identity was requested although crossing is allowed")
		return "", nil
	})

	layout := mustDiscover(t, deep, env{envAcrossFilesystem: "1"})
	if layout.GitDir != gitDir {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
}

func TestDiscoverReportsFilesystemIdentityFailureForStart(t *testing.T) {
	base := tempDir(t)
	stubFilesystemID(t, func(string) (string, error) { return "", fmt.Errorf("no identity") })

	if _, err := Discover(base, DiscoverOptions{Env: env{}.get}); err == nil {
		t.Fatal("Discover returned no error although the filesystem identity failed")
	}
}

func TestDiscoverReportsFilesystemIdentityFailureForParent(t *testing.T) {
	base := tempDir(t)
	deep := makeDir(t, filepath.Join(base, "deep"))
	stubFilesystemID(t, func(path string) (string, error) {
		if path == deep {
			return "same", nil
		}
		return "", fmt.Errorf("no identity")
	})

	if _, err := Discover(deep, DiscoverOptions{Env: env{}.get}); err == nil {
		t.Fatal("Discover returned no error although the filesystem identity failed")
	}
}

func TestDiscoverRejectsInvalidAcrossFilesystemFlag(t *testing.T) {
	base := tempDir(t)
	if _, err := Discover(base, env{envAcrossFilesystem: "perhaps"}.discoverOptions()); err == nil {
		t.Fatal("Discover accepted a non boolean GIT_DISCOVERY_ACROSS_FILESYSTEM")
	}
}

func TestDiscoverRejectsMissingStartDirectory(t *testing.T) {
	_, err := Discover(filepath.Join(tempDir(t), "absent"), DiscoverOptions{Env: env{}.get})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Discover returned %v, want ErrInvalidPath", err)
	}
}

func TestDiscoverRejectsFileAsStartPath(t *testing.T) {
	path := writeFile(t, filepath.Join(tempDir(t), "file"), "x")
	if _, err := Discover(path, DiscoverOptions{Env: env{}.get}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Discover returned %v, want ErrInvalidPath", err)
	}
}

func TestDiscoverHonoursGitDirEnvironment(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store", dotGit))
	elsewhere := makeDir(t, filepath.Join(base, "elsewhere"))

	layout := mustDiscover(t, elsewhere, env{envGitDir: gitDir})
	if layout.GitDir != gitDir {
		t.Errorf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
	if want := filepath.Join(base, "store"); layout.WorkTree != want {
		t.Errorf("layout has WorkTree %q, want %q", layout.WorkTree, want)
	}
}

func TestDiscoverFollowsGitDirEnvironmentPointingAtGitFile(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	link := writeFile(t, filepath.Join(base, "work", dotGit), gitFilePrefix+" "+filepath.ToSlash(gitDir)+"\n")

	layout := mustDiscover(t, base, env{envGitDir: link})
	if layout.GitDir != gitDir {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
}

func TestDiscoverRejectsGitDirEnvironmentWithoutRepository(t *testing.T) {
	base := tempDir(t)
	_, err := Discover(base, env{envGitDir: makeDir(t, filepath.Join(base, "empty"))}.discoverOptions())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Discover returned %v, want ErrNotFound", err)
	}
}

func TestDiscoverHonoursGitWorkTreeEnvironment(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	work := makeDir(t, filepath.Join(base, "work"))

	layout := mustDiscover(t, base, env{envGitDir: gitDir, envWorkTree: work})
	if layout.WorkTree != work || layout.Bare {
		t.Fatalf("layout = %+v, want WorkTree %q and Bare false", layout, work)
	}
}

func TestDiscoverHonoursGitCommonDirEnvironment(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store", dotGit))
	common := plainGitDir(t, filepath.Join(base, "common"))

	layout := mustDiscover(t, base, env{envGitDir: gitDir, envCommonDir: common})
	if layout.CommonDir != common || !layout.IsWorktree {
		t.Fatalf("layout = %+v, want CommonDir %q and IsWorktree true", layout, common)
	}
}

func TestDiscoverUsesProcessEnvironmentWhenNoLookupIsGiven(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store", dotGit))
	for _, key := range []string{envWorkTree, envCommonDir, envCeiling, envAcrossFilesystem} {
		t.Setenv(key, "")
	}
	t.Setenv(envGitDir, gitDir)

	layout, err := Discover(base, DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover returned error %v", err)
	}
	if layout.GitDir != gitDir {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
}

func TestDiscoverPrefersBareConfigurationOverDiscoveredWorkTree(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	writeFile(t, filepath.Join(gitDir, configFile), "[core]\n\tbare = true\n")

	layout := mustDiscover(t, work, env{})
	if !layout.Bare || layout.WorkTree != "" {
		t.Fatalf("layout = %+v, want a bare repository", layout)
	}
}

func TestDiscoverUsesCoreWorktreeFromConfiguration(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	work := makeDir(t, filepath.Join(base, "checkout"))
	writeFile(t, filepath.Join(gitDir, configFile), "[core]\n\tworktree = ../checkout\n")

	layout := mustDiscover(t, gitDir, env{})
	if layout.WorkTree != work || layout.Bare {
		t.Fatalf("layout = %+v, want WorkTree %q", layout, work)
	}
}

func TestDiscoverReportsMissingWorkTreeForNonBareGitDir(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	writeFile(t, filepath.Join(gitDir, configFile), "[core]\n\tbare = false\n")

	_, err := Discover(base, env{envGitDir: gitDir}.discoverOptions())
	if !errors.Is(err, ErrNotBareNoWorkTree) {
		t.Fatalf("Discover returned %v, want ErrNotBareNoWorkTree", err)
	}
}

func TestDiscoverRejectsBrokenLocalConfiguration(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	writeFile(t, filepath.Join(gitDir, configFile), "[core\n")

	if _, err := Discover(gitDir, DiscoverOptions{Env: env{}.get}); err == nil {
		t.Fatal("Discover accepted a broken configuration file")
	}
}

func TestDiscoverIgnoresUnparsableConfigurationValues(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	writeFile(t, filepath.Join(gitDir, configFile), "[core]\n\tbare = perhaps\n\tworktree\n")

	layout := mustDiscover(t, work, env{})
	if layout.WorkTree != work {
		t.Fatalf("layout has WorkTree %q, want %q", layout.WorkTree, work)
	}
}

func TestDiscoverRejectsEmptyCommonDirFile(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, "store.git"))
	writeFile(t, filepath.Join(gitDir, commonDirFile), "\n")

	_, err := Discover(base, env{envGitDir: gitDir}.discoverOptions())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Discover returned %v, want ErrNotFound", err)
	}
}

func TestDiscoverSkipsGitDirectoryWithUnreadableCommonDirFile(t *testing.T) {
	base := tempDir(t)
	outer := plainGitDir(t, filepath.Join(base, dotGit))
	work := makeDir(t, filepath.Join(base, "work"))
	broken := plainGitDir(t, filepath.Join(work, dotGit))
	makeDir(t, filepath.Join(broken, commonDirFile))

	layout := mustDiscover(t, work, env{})
	if layout.GitDir != outer {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, outer)
	}
}

func TestDiscoverSkipsIncompleteGitDirectoryAndKeepsClimbing(t *testing.T) {
	base := tempDir(t)
	gitDir := plainGitDir(t, filepath.Join(base, dotGit))
	work := makeDir(t, filepath.Join(base, "work"))
	makeDir(t, filepath.Join(work, dotGit))

	layout := mustDiscover(t, work, env{})
	if layout.GitDir != gitDir {
		t.Fatalf("layout has GitDir %q, want %q", layout.GitDir, gitDir)
	}
}

func TestParseCeilingsDropsRelativeEntriesAndHonoursTheEmptyEntry(t *testing.T) {
	base := tempDir(t)
	separator := string(os.PathListSeparator)
	raw := "relative" + separator + base + separator + "" + separator + filepath.Join(base, "deep")
	got := parseCeilings(raw)
	want := []string{base, filepath.Join(base, "deep")}
	if len(got) != len(want) {
		t.Fatalf("parseCeilings returned %q, want %q", got, want)
	}
	for i := range want {
		if !samePath(got[i], want[i]) {
			t.Fatalf("parseCeilings returned %q, want %q", got, want)
		}
	}
	if parseCeilings("") != nil {
		t.Fatal("parseCeilings returned entries for an empty variable")
	}
}

func TestCeilingFloorPicksTheLongestStrictAncestor(t *testing.T) {
	base := tempDir(t)
	deep := filepath.Join(base, "a", "b")
	ceilings := []string{base, filepath.Join(base, "a"), deep, filepath.Join(base, "other")}
	if got := ceilingFloor(deep, ceilings); got != filepath.Join(base, "a") {
		t.Fatalf("ceilingFloor returned %q, want %q", got, filepath.Join(base, "a"))
	}
	if got := ceilingFloor(deep, nil); got != "" {
		t.Fatalf("ceilingFloor returned %q, want an empty floor", got)
	}
}

func TestIsStrictAncestorComparesWholePathComponents(t *testing.T) {
	base := tempDir(t)
	for _, tc := range []struct {
		parent, child string
		want          bool
	}{
		{base, filepath.Join(base, "child"), true},
		{base + string(filepath.Separator), filepath.Join(base, "child"), true},
		{base, base, false},
		{filepath.Join(base, "child"), base, false},
		{base + "x", filepath.Join(base+"x", "child"), true},
	} {
		if got := isStrictAncestor(tc.parent, tc.child); got != tc.want {
			t.Errorf("isStrictAncestor(%q, %q) = %v, want %v", tc.parent, tc.child, got, tc.want)
		}
	}
}

func TestDerivedWorkTreeUsesTheParentOfADotGitDirectory(t *testing.T) {
	base := tempDir(t)
	got, ok := derivedWorkTree(filepath.Join(base, dotGit), false)
	if !ok || got != base {
		t.Fatalf("derivedWorkTree returned %q, %v, want %q, true", got, ok, base)
	}
	if _, ok := derivedWorkTree(filepath.Join(base, "store.git"), false); ok {
		t.Fatal("derivedWorkTree invented a work tree for a bare directory")
	}
	if _, ok := derivedWorkTree(filepath.Join(base, "absent"), true); ok {
		t.Fatal("derivedWorkTree invented a work tree without a gitdir file")
	}
}

func TestLocalConfigIgnoresMissingFileAndReportsSyntaxErrors(t *testing.T) {
	base := tempDir(t)
	file, err := localConfig(base)
	if err != nil || file != nil {
		t.Fatalf("localConfig returned %v, %v, want nil, nil", file, err)
	}
	writeFile(t, filepath.Join(base, configFile), "[core\n")
	if _, err := localConfig(base); err == nil {
		t.Fatal("localConfig accepted a broken file")
	}
}

func TestLocalValueHelpersTolerateAMissingFile(t *testing.T) {
	if value, ok := localBool(nil, "core.bare"); ok || value {
		t.Errorf("localBool returned %v, %v, want false, false", value, ok)
	}
	if got := localPath(nil, "core.worktree"); got != "" {
		t.Errorf("localPath returned %q, want an empty string", got)
	}
}

func (e env) discoverOptions() DiscoverOptions {
	return DiscoverOptions{Env: e.get}
}
