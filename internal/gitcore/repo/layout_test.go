package repo

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAbsCleanTurnsEmptyPathIntoWorkingDirectory(t *testing.T) {
	dir := tempDir(t)
	t.Chdir(dir)
	if got, want := absClean(""), absClean("."); got != want {
		t.Fatalf("absClean(%q) = %q, want %q", "", got, want)
	}
	if !filepath.IsAbs(absClean("")) {
		t.Fatalf("absClean(%q) = %q, want an absolute path", "", absClean(""))
	}
}

func TestAbsCleanJoinsRelativePathWithWorkingDirectory(t *testing.T) {
	dir := tempDir(t)
	t.Chdir(dir)
	if got, want := absClean(filepath.Join("a", "..", "b")), filepath.Join(absClean("."), "b"); got != want {
		t.Fatalf("absClean returned %q, want %q", got, want)
	}
}

func TestAbsCleanKeepsAbsolutePath(t *testing.T) {
	dir := tempDir(t)
	if got := absClean(dir + string(filepath.Separator) + "."); got != dir {
		t.Fatalf("absClean returned %q, want %q", got, dir)
	}
}

func TestResolveFromKeepsAbsolutePathAndJoinsRelativeOne(t *testing.T) {
	base := tempDir(t)
	other := filepath.Join(base, "other")
	if got := resolveFrom(base, other); got != other {
		t.Fatalf("resolveFrom returned %q, want %q", got, other)
	}
	if got, want := resolveFrom(base, filepath.Join("..", "x")), filepath.Join(filepath.Dir(base), "x"); got != want {
		t.Fatalf("resolveFrom returned %q, want %q", got, want)
	}
}

func TestReadLimitedStopsAtTheLimit(t *testing.T) {
	path := writeFile(t, filepath.Join(tempDir(t), "data"), strings.Repeat("x", 100))
	data, err := readLimited(path, 10)
	if err != nil {
		t.Fatalf("readLimited returned error %v", err)
	}
	if len(data) != 10 {
		t.Fatalf("readLimited returned %d bytes, want 10", len(data))
	}
}

func TestReadLimitedFailsOnMissingFile(t *testing.T) {
	if _, err := readLimited(filepath.Join(tempDir(t), "absent"), 10); err == nil {
		t.Fatal("readLimited returned no error for a missing file")
	}
}

func TestFirstLineTrimsCarriageReturnAndTail(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"one\r\ntwo\n", "one"},
		{"only", "only"},
		{"spaced \t\n", "spaced"},
		{"", ""},
	} {
		if got := firstLine([]byte(tc.in)); got != tc.want {
			t.Errorf("firstLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsHexObjectIDAcceptsBothHashLengths(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{strings.Repeat("a", 40), true},
		{strings.Repeat("F", 64), true},
		{strings.Repeat("a", 39), false},
		{strings.Repeat("z", 40), false},
		{"", false},
	} {
		if got := isHexObjectID(tc.in); got != tc.want {
			t.Errorf("isHexObjectID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidHeadRefAcceptsSymbolicAndDetachedHeads(t *testing.T) {
	dir := tempDir(t)
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"symbolic", "ref: refs/heads/main\n", true},
		{"symbolicWithTabs", "ref:\trefs/heads/main\n", true},
		{"detached", strings.Repeat("0", 40) + "\n", true},
		{"outsideRefs", "ref: heads/main\n", false},
		{"garbage", "not a head\n", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(dir, tc.name, headFile), tc.text)
			if got := validHeadRef(path); got != tc.want {
				t.Fatalf("validHeadRef(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestValidHeadRefRejectsUnreadableHead(t *testing.T) {
	dir := makeDir(t, filepath.Join(tempDir(t), headFile))
	if validHeadRef(dir) {
		t.Fatal("validHeadRef accepted a directory")
	}
}

func TestIsDirectoryDistinguishesFilesFromDirectories(t *testing.T) {
	base := tempDir(t)
	if !isDirectory(base) {
		t.Fatal("isDirectory rejected an existing directory")
	}
	if isDirectory(writeFile(t, filepath.Join(base, "file"), "x")) {
		t.Fatal("isDirectory accepted a file")
	}
	if isDirectory(filepath.Join(base, "absent")) {
		t.Fatal("isDirectory accepted a missing path")
	}
}

func TestCommonDirOfFallsBackToGitDirWhenFileIsAbsent(t *testing.T) {
	dir := tempDir(t)
	got, err := commonDirOf(dir)
	if err != nil {
		t.Fatalf("commonDirOf returned error %v", err)
	}
	if got != dir {
		t.Fatalf("commonDirOf returned %q, want %q", got, dir)
	}
}

func TestCommonDirOfResolvesRelativeAndAbsolutePaths(t *testing.T) {
	base := tempDir(t)
	relative := makeDir(t, filepath.Join(base, "rel", "gitdir"))
	writeFile(t, filepath.Join(relative, commonDirFile), "../common\n")
	got, err := commonDirOf(relative)
	if err != nil {
		t.Fatalf("commonDirOf returned error %v", err)
	}
	if want := filepath.Join(base, "rel", "common"); got != want {
		t.Fatalf("commonDirOf returned %q, want %q", got, want)
	}

	absolute := makeDir(t, filepath.Join(base, "abs"))
	writeFile(t, filepath.Join(absolute, commonDirFile), filepath.ToSlash(base)+"\r\n")
	got, err = commonDirOf(absolute)
	if err != nil {
		t.Fatalf("commonDirOf returned error %v", err)
	}
	if got != base {
		t.Fatalf("commonDirOf returned %q, want %q", got, base)
	}
}

func TestCommonDirOfRejectsEmptyFile(t *testing.T) {
	dir := tempDir(t)
	writeFile(t, filepath.Join(dir, commonDirFile), "\n")
	if _, err := commonDirOf(dir); !errors.Is(err, ErrInvalidGitDirFile) {
		t.Fatalf("commonDirOf returned %v, want ErrInvalidGitDirFile", err)
	}
}

func TestCommonDirOfReportsReadFailure(t *testing.T) {
	dir := tempDir(t)
	makeDir(t, filepath.Join(dir, commonDirFile))
	if _, err := commonDirOf(dir); err == nil {
		t.Fatal("commonDirOf returned no error for an unreadable file")
	}
}

func TestIsGitDirectoryRequiresHeadObjectsAndRefs(t *testing.T) {
	base := tempDir(t)
	complete := plainGitDir(t, filepath.Join(base, "complete"))
	if !isGitDirectory(complete) {
		t.Fatal("isGitDirectory rejected a complete git directory")
	}

	noHead := makeDir(t, filepath.Join(base, "nohead"))
	makeDir(t, filepath.Join(noHead, objectsDirName))
	makeDir(t, filepath.Join(noHead, refsDirName))
	if isGitDirectory(noHead) {
		t.Error("isGitDirectory accepted a directory without HEAD")
	}

	noObjects := makeDir(t, filepath.Join(base, "noobjects"))
	writeFile(t, filepath.Join(noObjects, headFile), headRefPrefix+"main\n")
	makeDir(t, filepath.Join(noObjects, refsDirName))
	if isGitDirectory(noObjects) {
		t.Error("isGitDirectory accepted a directory without objects")
	}

	noRefs := makeDir(t, filepath.Join(base, "norefs"))
	writeFile(t, filepath.Join(noRefs, headFile), headRefPrefix+"main\n")
	makeDir(t, filepath.Join(noRefs, objectsDirName))
	if isGitDirectory(noRefs) {
		t.Error("isGitDirectory accepted a directory without refs")
	}

	brokenCommon := makeDir(t, filepath.Join(base, "brokencommon"))
	writeFile(t, filepath.Join(brokenCommon, headFile), headRefPrefix+"main\n")
	makeDir(t, filepath.Join(brokenCommon, commonDirFile))
	if isGitDirectory(brokenCommon) {
		t.Error("isGitDirectory accepted a directory with an unreadable commondir")
	}
}

func TestIsGitDirectoryFollowsCommonDirForObjectsAndRefs(t *testing.T) {
	base := tempDir(t)
	_, _, gitDir := linkedWorktree(t, base, "wt")
	if !isGitDirectory(gitDir) {
		t.Fatal("isGitDirectory rejected a linked worktree directory")
	}
}

func TestReadGitFileResolvesRelativeAndAbsoluteTargets(t *testing.T) {
	base := tempDir(t)
	target := plainGitDir(t, filepath.Join(base, "store.git"))
	relative := writeFile(t, filepath.Join(base, "work", dotGit), gitFilePrefix+" ../store.git\r\n")
	got, err := readGitFile(relative)
	if err != nil {
		t.Fatalf("readGitFile returned error %v", err)
	}
	if got != target {
		t.Fatalf("readGitFile returned %q, want %q", got, target)
	}

	absolute := writeFile(t, filepath.Join(base, "other", dotGit), gitFilePrefix+" "+filepath.ToSlash(target))
	got, err = readGitFile(absolute)
	if err != nil {
		t.Fatalf("readGitFile returned error %v", err)
	}
	if got != target {
		t.Fatalf("readGitFile returned %q, want %q", got, target)
	}
}

func TestReadGitFileRejectsBrokenContent(t *testing.T) {
	base := tempDir(t)
	for _, tc := range []struct {
		name string
		text string
	}{
		{"noPrefix", "not a gitdir line\n"},
		{"emptyTarget", gitFilePrefix + "   \n"},
		{"missingTarget", gitFilePrefix + " ../absent\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(base, tc.name, dotGit), tc.text)
			if _, err := readGitFile(path); !errors.Is(err, ErrInvalidGitDirFile) {
				t.Fatalf("readGitFile returned %v, want ErrInvalidGitDirFile", err)
			}
		})
	}
}

func TestReadGitFileReportsUnreadableFile(t *testing.T) {
	if _, err := readGitFile(filepath.Join(tempDir(t), dotGit)); errors.Is(err, ErrInvalidGitDirFile) {
		t.Fatal("readGitFile classified a missing file as an invalid gitdir file")
	}
}

func TestIsRepositoryRecognisesEveryLayout(t *testing.T) {
	base := tempDir(t)
	withDir := makeDir(t, filepath.Join(base, "withdir"))
	plainGitDir(t, filepath.Join(withDir, dotGit))
	if !IsRepository(withDir) {
		t.Error("IsRepository rejected a working tree with a .git directory")
	}

	store := plainGitDir(t, filepath.Join(base, "store.git"))
	if !IsRepository(store) {
		t.Error("IsRepository rejected a bare repository")
	}

	withFile := makeDir(t, filepath.Join(base, "withfile"))
	writeFile(t, filepath.Join(withFile, dotGit), gitFilePrefix+" "+filepath.ToSlash(store)+"\n")
	if !IsRepository(withFile) {
		t.Error("IsRepository rejected a working tree with a .git file")
	}

	if IsRepository(makeDir(t, filepath.Join(base, "plain"))) {
		t.Error("IsRepository accepted a directory without a repository")
	}
}
