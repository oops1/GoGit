package repo

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func expectedInitConfig(bare bool) []string {
	values := []string{"core.repositoryformatversion=0"}
	if runtime.GOOS == "windows" {
		values = append(values, "core.filemode=false")
	} else {
		values = append(values, "core.filemode=true")
	}
	values = append(values, "core.bare="+boolText(bare))
	if !bare {
		values = append(values, "core.logallrefupdates=true")
	}
	if runtime.GOOS == "windows" {
		values = append(values, "core.symlinks=false", "core.ignorecase=true")
	}
	return values
}

func configRecords(t *testing.T, path string) []string {
	t.Helper()
	file, err := config.Parse([]byte(readFile(t, path)))
	if err != nil {
		t.Fatalf("Parse(%q) returned error %v", path, err)
	}
	var out []string
	for v := range file.Variables() {
		out = append(out, v.Name()+"="+v.Value)
	}
	return out
}

func TestInitCreatesTheStandardLayout(t *testing.T) {
	base := tempDir(t)
	work := filepath.Join(base, "work")

	repository := initRepo(t, work, initOptions(t, env{}))
	gitDir := filepath.Join(work, dotGit)
	if repository.GitDir() != gitDir || repository.WorkTree() != work || repository.IsBare() {
		t.Fatalf("repository = %+v, want a work tree repository at %q", repository.Layout(), work)
	}
	for _, rel := range []string{"objects/info", "objects/pack", "refs/heads", "refs/tags", "info", "hooks"} {
		if !isDirectory(filepath.Join(gitDir, filepath.FromSlash(rel))) {
			t.Errorf("%s is missing", rel)
		}
	}
	if isDirectory(filepath.Join(gitDir, "branches")) {
		t.Error("branches was created although git no longer creates it")
	}
	entries, err := os.ReadDir(filepath.Join(gitDir, hooksDirName))
	if err != nil {
		t.Fatalf("ReadDir returned error %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("hooks holds %d entries, want an empty directory", len(entries))
	}
	if got := readFile(t, filepath.Join(gitDir, headFile)); got != headRefPrefix+"master\n" {
		t.Errorf("HEAD holds %q, want %q", got, headRefPrefix+"master\n")
	}
	if got := readFile(t, filepath.Join(gitDir, descriptionName)); got != descriptionText {
		t.Errorf("description holds %q, want %q", got, descriptionText)
	}
	if got := readFile(t, repository.InfoExclude()); got != excludeText {
		t.Errorf("info/exclude holds %q, want %q", got, excludeText)
	}
	if got, want := configRecords(t, filepath.Join(gitDir, configFile)), expectedInitConfig(false); !slices.Equal(got, want) {
		t.Errorf("config holds %q, want %q", got, want)
	}
	if !IsRepository(work) {
		t.Error("IsRepository rejected the freshly created repository")
	}
}

func TestInitCreatesBareRepository(t *testing.T) {
	base := tempDir(t)
	store := filepath.Join(base, "store.git")

	opts := initOptions(t, env{})
	opts.Bare = true
	repository := initRepo(t, store, opts)
	if !repository.IsBare() || repository.WorkTree() != "" {
		t.Fatalf("repository = %+v, want a bare repository", repository.Layout())
	}
	if repository.GitDir() != store {
		t.Fatalf("repository reports git dir %q, want %q", repository.GitDir(), store)
	}
	if got, want := configRecords(t, filepath.Join(store, configFile)), expectedInitConfig(true); !slices.Equal(got, want) {
		t.Errorf("config holds %q, want %q", got, want)
	}
}

func TestInitUsesTheRequestedInitialBranch(t *testing.T) {
	base := tempDir(t)
	opts := initOptions(t, env{})
	opts.InitialBranch = "trunk"

	repository := initRepo(t, filepath.Join(base, "work"), opts)
	if got := readFile(t, filepath.Join(repository.GitDir(), headFile)); got != headRefPrefix+"trunk\n" {
		t.Fatalf("HEAD holds %q, want %q", got, headRefPrefix+"trunk\n")
	}
}

func TestInitUsesInitDefaultBranchFromGlobalConfiguration(t *testing.T) {
	base := tempDir(t)
	opts := initOptions(t, env{})
	opts.GlobalFile = writeFile(t, filepath.Join(base, "gitconfig"), "[init]\n\tdefaultBranch = main\n")

	repository := initRepo(t, filepath.Join(base, "work"), opts)
	if got := readFile(t, filepath.Join(repository.GitDir(), headFile)); got != headRefPrefix+"main\n" {
		t.Fatalf("HEAD holds %q, want %q", got, headRefPrefix+"main\n")
	}
}

func TestInitRejectsInvalidInitialBranch(t *testing.T) {
	base := tempDir(t)
	opts := initOptions(t, env{})
	opts.InitialBranch = "bad branch"

	if _, err := Init(filepath.Join(base, "work"), opts); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Init returned %v, want ErrInvalidPath", err)
	}
}

func TestInitReportsBrokenGlobalConfiguration(t *testing.T) {
	base := tempDir(t)
	opts := initOptions(t, env{})
	opts.GlobalFile = writeFile(t, filepath.Join(base, "gitconfig"), "[init\n")

	if _, err := Init(filepath.Join(base, "work"), opts); err == nil {
		t.Fatal("Init accepted a broken global configuration")
	}
}

func TestInitWritesSeparateGitDirectory(t *testing.T) {
	base := tempDir(t)
	work := filepath.Join(base, "work")
	store := filepath.Join(base, "store.git")
	opts := initOptions(t, env{})
	opts.SeparateGitDir = store

	repository := initRepo(t, work, opts)
	if repository.GitDir() != store || repository.WorkTree() != work {
		t.Fatalf("repository = %+v, want git dir %q and work tree %q", repository.Layout(), store, work)
	}
	link := readFile(t, filepath.Join(work, dotGit))
	if want := gitFilePrefix + " " + filepath.ToSlash(store) + "\n"; link != want {
		t.Fatalf("the .git file holds %q, want %q", link, want)
	}
	if layout := mustDiscover(t, work, env{}); layout.GitDir != store {
		t.Fatalf("Discover found git dir %q, want %q", layout.GitDir, store)
	}
}

func TestInitRejectsSeparateGitDirectoryForBareRepository(t *testing.T) {
	base := tempDir(t)
	opts := initOptions(t, env{})
	opts.Bare = true
	opts.SeparateGitDir = filepath.Join(base, "store.git")

	if _, err := Init(filepath.Join(base, "work"), opts); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Init returned %v, want ErrInvalidPath", err)
	}
}

func TestInitReportsUnusableSeparateGitDirectory(t *testing.T) {
	base := tempDir(t)
	opts := initOptions(t, env{})
	opts.SeparateGitDir = writeFile(t, filepath.Join(base, "store.git"), "not a directory")

	if _, err := Init(filepath.Join(base, "work"), opts); err == nil {
		t.Fatal("Init accepted a file as the separate git directory")
	}
}

func TestInitReportsUnwritableGitFile(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	makeDir(t, filepath.Join(work, dotGit))
	opts := initOptions(t, env{})
	opts.SeparateGitDir = filepath.Join(base, "store.git")

	if _, err := Init(work, opts); err == nil {
		t.Fatal("Init overwrote an existing .git directory with a gitdir file")
	}
}

func TestInitReusesTheGitDirectoryBehindAnExistingGitFile(t *testing.T) {
	base := tempDir(t)
	work := filepath.Join(base, "work")
	store := filepath.Join(base, "store.git")
	first := initOptions(t, env{})
	first.SeparateGitDir = store
	initRepo(t, work, first)

	again := initRepo(t, work, initOptions(t, env{}))
	if again.GitDir() != store {
		t.Fatalf("the second Init used git dir %q, want %q", again.GitDir(), store)
	}
}

func TestInitAcceptsNonEmptyDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, "existing.txt"), "keep me")

	repository := initRepo(t, work, initOptions(t, env{}))
	if repository.WorkTree() != work {
		t.Fatalf("repository reports work tree %q, want %q", repository.WorkTree(), work)
	}
	if got := readFile(t, filepath.Join(work, "existing.txt")); got != "keep me" {
		t.Fatalf("the existing file holds %q, want %q", got, "keep me")
	}
}

func TestInitRejectsFileAsRepositoryPath(t *testing.T) {
	path := writeFile(t, filepath.Join(tempDir(t), "file"), "x")
	if _, err := Init(path, initOptions(t, env{})); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Init returned %v, want ErrInvalidPath", err)
	}
}

func TestInitRejectsUnusablePath(t *testing.T) {
	path := filepath.Join(tempDir(t), "bad\x00name")
	if _, err := Init(path, initOptions(t, env{})); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Init returned %v, want ErrInvalidPath", err)
	}
}

func TestInitKeepsHeadAndConfigurationOnReinitialisation(t *testing.T) {
	base := tempDir(t)
	work := filepath.Join(base, "work")
	first := initRepo(t, work, initOptions(t, env{}))
	gitDir := first.GitDir()
	writeFile(t, filepath.Join(gitDir, headFile), headRefPrefix+"custom\n")
	writeFile(t, filepath.Join(gitDir, configFile),
		readFile(t, filepath.Join(gitDir, configFile))+"[user]\n\tname = Кто-то\n")

	second := initRepo(t, work, initOptions(t, env{}))
	if got := readFile(t, filepath.Join(gitDir, headFile)); got != headRefPrefix+"custom\n" {
		t.Errorf("HEAD holds %q, want the untouched %q", got, headRefPrefix+"custom\n")
	}
	if name, _ := second.Config().Get("user.name"); name != "Кто-то" {
		t.Errorf("configuration holds user.name %q, want %q", name, "Кто-то")
	}
	if got, want := configRecords(t, filepath.Join(gitDir, configFile)), append(expectedInitConfig(false), "user.name=Кто-то"); !slices.Equal(got, want) {
		t.Errorf("config holds %q, want %q", got, want)
	}
}

func TestInitRepairsUnreadableHead(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit, headFile), "garbage\n")

	repository := initRepo(t, work, initOptions(t, env{}))
	if got := readFile(t, filepath.Join(repository.GitDir(), headFile)); got != headRefPrefix+"master\n" {
		t.Fatalf("HEAD holds %q, want %q", got, headRefPrefix+"master\n")
	}
}

func TestInitReportsUnwritableHead(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	makeDir(t, filepath.Join(work, dotGit, headFile))

	if _, err := Init(work, initOptions(t, env{})); err == nil {
		t.Fatal("Init accepted a directory in place of HEAD")
	}
}

func TestInitReportsBlockedGitDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit), "not a gitdir file")

	if _, err := Init(work, initOptions(t, env{})); err == nil {
		t.Fatal("Init accepted a file in place of the git directory")
	}
}

func TestInitReportsBlockedObjectsDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit, objectsDirName), "not a directory")

	if _, err := Init(work, initOptions(t, env{})); err == nil {
		t.Fatal("Init accepted a file in place of the objects directory")
	}
}

func TestInitReportsUnreadableConfiguration(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	makeDir(t, filepath.Join(work, dotGit, configFile))

	if _, err := Init(work, initOptions(t, env{})); err == nil {
		t.Fatal("Init accepted a directory in place of the configuration file")
	}
}

func TestInitReportsUnparsableConfiguration(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	writeFile(t, filepath.Join(work, dotGit, configFile), "[core\n")

	if _, err := Init(work, initOptions(t, env{})); err == nil {
		t.Fatal("Init accepted a broken configuration file")
	}
}

func TestInitReportsMissingDescriptionDirectory(t *testing.T) {
	base := tempDir(t)
	gitDir := makeDir(t, filepath.Join(base, "gitdir"))
	root, err := os.OpenRoot(gitDir)
	if err != nil {
		t.Fatalf("OpenRoot returned error %v", err)
	}
	defer root.Close()

	if err := writeIfMissing(root, "absent/file", "text"); err == nil {
		t.Fatal("writeIfMissing accepted a path without a parent directory")
	}
}

func TestWriteIfMissingKeepsExistingContent(t *testing.T) {
	base := tempDir(t)
	writeFile(t, filepath.Join(base, descriptionName), "mine")
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot returned error %v", err)
	}
	defer root.Close()

	if err := writeIfMissing(root, descriptionName, descriptionText); err != nil {
		t.Fatalf("writeIfMissing returned error %v", err)
	}
	if got := readFile(t, filepath.Join(base, descriptionName)); got != "mine" {
		t.Fatalf("the file holds %q, want %q", got, "mine")
	}
}

func TestWriteSkeletonReportsMissingGitDirectory(t *testing.T) {
	if err := writeSkeleton(filepath.Join(tempDir(t), "absent"), "master", false); err == nil {
		t.Fatal("writeSkeleton accepted a missing git directory")
	}
}

func TestValidBranchNameFollowsRefFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"master", true},
		{"feature/x", true},
		{"release-1.2", true},
		{"", false},
		{"@", false},
		{"/leading", false},
		{"trailing/", false},
		{".hidden", false},
		{"dot.", false},
		{"name.lock", false},
		{"name.lock/x", false},
		{"a..b", false},
		{"a//b", false},
		{"a@{b", false},
		{"a/.b", false},
		{"with space", false},
		{"tilde~", false},
		{"caret^", false},
		{"colon:", false},
		{"question?", false},
		{"star*", false},
		{"bracket[", false},
		{"back\\slash", false},
		{"del\x7f", false},
	} {
		if got := validBranchName(tc.name); got != tc.want {
			t.Errorf("validBranchName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBoolTextRendersGitBooleans(t *testing.T) {
	if boolText(true) != "true" || boolText(false) != "false" {
		t.Fatalf("boolText returned %q and %q", boolText(true), boolText(false))
	}
}
