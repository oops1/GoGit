package repo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

func gitDirWithConfig(t *testing.T, dir, text string) string {
	t.Helper()
	gitDir := plainGitDir(t, dir)
	writeFile(t, filepath.Join(gitDir, configFile), text)
	return gitDir
}

func TestOpenReadsLayoutAndConfiguration(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := gitDirWithConfig(t, filepath.Join(work, dotGit), "[user]\n\tname = Тест\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	if repository.GitDir() != gitDir || repository.CommonDir() != gitDir {
		t.Errorf("repository reports %q and %q, want both %q", repository.GitDir(), repository.CommonDir(), gitDir)
	}
	if repository.WorkTree() != work {
		t.Errorf("repository reports work tree %q, want %q", repository.WorkTree(), work)
	}
	if repository.IsBare() || repository.IsWorktree() {
		t.Errorf("repository reports bare %v and worktree %v, want both false", repository.IsBare(), repository.IsWorktree())
	}
	if repository.ObjectFormat != hash.SHA1 {
		t.Errorf("repository reports object format %v, want %v", repository.ObjectFormat, hash.SHA1)
	}
	if name, _ := repository.Config().Get("user.name"); name != "Тест" {
		t.Errorf("configuration holds user.name %q, want %q", name, "Тест")
	}
	if repository.Core().RepositoryFormatVersion != 0 {
		t.Errorf("core reports version %d, want 0", repository.Core().RepositoryFormatVersion)
	}
	if repository.Layout().GitDir != gitDir {
		t.Errorf("layout reports git dir %q, want %q", repository.Layout().GitDir, gitDir)
	}
	if repository.Root().Name() != gitDir || repository.CommonRoot().Name() != gitDir {
		t.Errorf("roots point at %q and %q, want %q", repository.Root().Name(), repository.CommonRoot().Name(), gitDir)
	}
}

func TestOpenReadsFilesThroughTheRoot(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))

	repository := openRepo(t, work, openOptions(t, env{}))
	data, err := repository.Root().ReadFile(headFile)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if string(data) != headRefPrefix+"master\n" {
		t.Fatalf("HEAD holds %q, want %q", data, headRefPrefix+"master\n")
	}
	if _, err := repository.Root().ReadFile("../outside"); err == nil {
		t.Fatal("the root allowed a path outside the git directory")
	}
}

func TestOpenPropagatesDiscoveryFailure(t *testing.T) {
	base := tempDir(t)
	if _, err := Open(filepath.Join(base, "absent"), openOptions(t, env{})); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Open returned %v, want ErrInvalidPath", err)
	}
}

func TestOpenRejectsUnsupportedFormatVersion(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"),
		"[core]\n\tbare = true\n\trepositoryformatversion = 2\n")

	_, err := Open(gitDir, openOptions(t, env{}))
	if !errors.Is(err, ErrUnsupportedFormatVersion) {
		t.Fatalf("Open returned %v, want ErrUnsupportedFormatVersion", err)
	}
}

func TestOpenAcceptsFormatVersionOneWithKnownExtensions(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"),
		"[core]\n\tbare = true\n\trepositoryformatversion = 1\n[extensions]\n\tobjectFormat = sha1\n")

	repository := openRepo(t, gitDir, openOptions(t, env{}))
	if repository.ObjectFormat != hash.SHA1 {
		t.Fatalf("repository reports object format %v, want %v", repository.ObjectFormat, hash.SHA1)
	}
}

func TestOpenRejectsSha256ObjectFormat(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"),
		"[core]\n\tbare = true\n\trepositoryformatversion = 1\n[extensions]\n\tobjectFormat = sha256\n")

	_, err := Open(gitDir, openOptions(t, env{}))
	if !errors.Is(err, hash.ErrUnsupportedFormat) {
		t.Fatalf("Open returned %v, want hash.ErrUnsupportedFormat", err)
	}
}

func TestOpenRejectsUnknownExtension(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"),
		"[core]\n\tbare = true\n\trepositoryformatversion = 1\n[extensions]\n\tsomethingNew = true\n")

	_, err := Open(gitDir, openOptions(t, env{}))
	if !errors.Is(err, config.ErrUnknownExtension) {
		t.Fatalf("Open returned %v, want config.ErrUnknownExtension", err)
	}
}

func TestOpenRejectsUnparsableCoreSection(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"),
		"[core]\n\tbare = true\n\tfilemode = perhaps\n")

	if _, err := Open(gitDir, openOptions(t, env{})); err == nil {
		t.Fatal("Open accepted an unparsable core section")
	}
}

func TestOpenReportsConfigurationLoadFailure(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"), "[core]\n\tbare = true\n")
	broken := writeFile(t, filepath.Join(base, "broken.config"), "[core\n")

	opts := openOptions(t, env{})
	opts.GlobalFile = broken
	if _, err := Open(gitDir, opts); err == nil {
		t.Fatal("Open accepted a broken global configuration")
	}
}

func TestOpenLayoutReportsMissingGitDirectory(t *testing.T) {
	base := tempDir(t)
	layout := Layout{GitDir: filepath.Join(base, "absent"), CommonDir: base}
	if _, err := OpenLayout(layout, openOptions(t, env{})); err == nil {
		t.Fatal("OpenLayout accepted a missing git directory")
	}
}

func TestOpenLayoutReportsMissingCommonDirectory(t *testing.T) {
	base := tempDir(t)
	layout := Layout{GitDir: base, CommonDir: filepath.Join(base, "absent")}
	if _, err := OpenLayout(layout, openOptions(t, env{})); err == nil {
		t.Fatal("OpenLayout accepted a missing common directory")
	}
}

func TestRepositoryPathsPointIntoTheCommonDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))

	repository := openRepo(t, work, openOptions(t, env{}))
	for _, tc := range []struct{ name, got, want string }{
		{"objects", repository.ObjectsDir(), filepath.Join(gitDir, "objects")},
		{"pack", repository.PackDir(), filepath.Join(gitDir, "objects", "pack")},
		{"refs", repository.RefsDir(), filepath.Join(gitDir, "refs")},
		{"index", repository.IndexFile(), filepath.Join(gitDir, "index")},
		{"exclude", repository.InfoExclude(), filepath.Join(gitDir, "info", "exclude")},
		{"hooks", repository.HooksDir(), filepath.Join(gitDir, "hooks")},
	} {
		if tc.got != tc.want {
			t.Errorf("%s path is %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestRepositorySplitsWorktreeAndCommonPaths(t *testing.T) {
	base := tempDir(t)
	main, work, gitDir := linkedWorktree(t, base, "second")

	repository := openRepo(t, work, openOptions(t, env{}))
	for _, tc := range []struct {
		rel  string
		want string
	}{
		{"HEAD", filepath.Join(gitDir, "HEAD")},
		{"index", filepath.Join(gitDir, "index")},
		{"ORIG_HEAD", filepath.Join(gitDir, "ORIG_HEAD")},
		{"MERGE_HEAD", filepath.Join(gitDir, "MERGE_HEAD")},
		{"logs/HEAD", filepath.Join(gitDir, "logs", "HEAD")},
		{"refs/bisect", filepath.Join(gitDir, "refs", "bisect")},
		{"refs/bisect/good", filepath.Join(gitDir, "refs", "bisect", "good")},
		{"refs/worktree/x", filepath.Join(gitDir, "refs", "worktree", "x")},
		{"config.worktree", filepath.Join(gitDir, "config.worktree")},
		{"sequencer", filepath.Join(gitDir, "sequencer")},
		{"rebase-merge/done", filepath.Join(gitDir, "rebase-merge", "done")},
		{"info/sparse-checkout", filepath.Join(gitDir, "info", "sparse-checkout")},
		{"config", filepath.Join(main, "config")},
		{"packed-refs", filepath.Join(main, "packed-refs")},
		{"shallow", filepath.Join(main, "shallow")},
		{"refs/heads/main", filepath.Join(main, "refs", "heads", "main")},
		{"logs/refs/heads/main", filepath.Join(main, "logs", "refs", "heads", "main")},
		{"objects/pack", filepath.Join(main, "objects", "pack")},
		{"hooks/pre-commit", filepath.Join(main, "hooks", "pre-commit")},
		{"worktrees", filepath.Join(main, "worktrees")},
	} {
		if got := repository.GitPath(tc.rel); got != tc.want {
			t.Errorf("GitPath(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
	if got, want := repository.CommonPath("refs/heads"), filepath.Join(main, "refs", "heads"); got != want {
		t.Errorf("CommonPath returned %q, want %q", got, want)
	}
	if repository.IndexFile() != filepath.Join(gitDir, "index") {
		t.Errorf("IndexFile returned %q, want the worktree index", repository.IndexFile())
	}
}

func TestConfigReadsWorktreeConfigFileFromTheLinkedWorktree(t *testing.T) {
	base := tempDir(t)
	main, work, gitDir := linkedWorktree(t, base, "second")
	writeFile(t, filepath.Join(main, configFile), "[extensions]\n\tworktreeConfig = true\n")
	writeFile(t, filepath.Join(gitDir, "config.worktree"), "[core]\n\tsparseCheckout = true\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	on, err := repository.Config().GetBool("core.sparseCheckout")
	if err != nil {
		t.Fatalf("GetBool returned error %v", err)
	}
	if !on {
		t.Fatal("core.sparseCheckout = false, want true from config.worktree")
	}
	origin, ok := repository.Config().Origin("core.sparseCheckout")
	if !ok || origin.Path != filepath.Join(gitDir, "config.worktree") {
		t.Fatalf("origin = %+v, ok=%v, want the linked worktree's config.worktree", origin, ok)
	}
}

func TestConfigIgnoresWorktreeConfigFileForTheMainWorktree(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := gitDirWithConfig(t, filepath.Join(work, dotGit), "[extensions]\n\tworktreeConfig = true\n")
	writeFile(t, filepath.Join(gitDir, "config.worktree"), "[core]\n\tsparseCheckout = true\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	on, err := repository.Config().GetBool("core.sparseCheckout")
	if err != nil {
		t.Fatalf("GetBool returned error %v", err)
	}
	if !on {
		t.Fatal("core.sparseCheckout = false, want true from config.worktree")
	}
}

func TestHooksDirFollowsCoreHooksPath(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	absolute := filepath.Join(base, "shared-hooks")
	gitDirWithConfig(t, filepath.Join(work, dotGit),
		"[core]\n\thooksPath = "+filepath.ToSlash(absolute)+"\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	if got := repository.HooksDir(); got != absolute {
		t.Fatalf("HooksDir returned %q, want %q", got, absolute)
	}
}

func TestHooksDirResolvesRelativeHooksPathAgainstTheWorkTree(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDirWithConfig(t, filepath.Join(work, dotGit), "[core]\n\thooksPath = .githooks\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	if got, want := repository.HooksDir(), filepath.Join(work, ".githooks"); got != want {
		t.Fatalf("HooksDir returned %q, want %q", got, want)
	}
}

func TestHooksDirResolvesRelativeHooksPathAgainstBareRepository(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"),
		"[core]\n\tbare = true\n\thooksPath = shared\n")

	repository := openRepo(t, gitDir, openOptions(t, env{}))
	if got, want := repository.HooksDir(), filepath.Join(gitDir, "shared"); got != want {
		t.Fatalf("HooksDir returned %q, want %q", got, want)
	}
}

func TestIsWorktreePathSeparatesCommonEntries(t *testing.T) {
	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{"HEAD", true},
		{"FETCH_HEAD", true},
		{"index", true},
		{"logs/HEAD", true},
		{"logs/refs/bisect/x", true},
		{"refs/rewritten/x", true},
		{"rebase-merge", true},
		{"gc.pid", false},
		{"branches", false},
		{"common/x", false},
		{"lost-found", false},
		{"remotes/origin", false},
		{"rr-cache/x", false},
		{"svn/x", false},
		{"info/attributes", false},
		{"logs/refs/heads/main", false},
	} {
		if got := isWorktreePath(tc.rel); got != tc.want {
			t.Errorf("isWorktreePath(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestCloseReleasesBothRoots(t *testing.T) {
	base := tempDir(t)
	gitDir := gitDirWithConfig(t, filepath.Join(base, "store.git"), "[core]\n\tbare = true\n")

	repository, err := Open(gitDir, openOptions(t, env{}))
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, err := repository.Root().ReadFile(headFile); err == nil {
		t.Fatal("the git directory root stayed usable after Close")
	}
	if _, err := repository.CommonRoot().ReadFile(headFile); err == nil {
		t.Fatal("the common directory root stayed usable after Close")
	}
}
