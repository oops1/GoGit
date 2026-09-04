package addrepo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

func initRepo(t *testing.T, path string, bare bool) {
	t.Helper()
	r, err := repo.Init(path, repo.InitOptions{Bare: bare, Env: noEnv})
	if err != nil {
		t.Fatalf("Init(%q) returned error %v", path, err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
}

func TestValidateReturnsPathRequiredWhenPathIsBlank(t *testing.T) {
	hint := Validate(Request{Path: "   "})
	if hint.Key != hintPathRequired || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateOpenReturnsPathNotFoundWhenPathMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope")
	hint := Validate(Request{Path: path, Mode: ModeOpen})
	if hint.Key != hintPathNotFound || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
	if hint.Args[0] != path {
		t.Fatalf("args = %+v", hint.Args)
	}
}

func TestValidateOpenReturnsPathNotDirectoryWhenPathIsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	hint := Validate(Request{Path: file, Mode: ModeOpen})
	if hint.Key != hintPathNotDirectory || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateOpenReturnsNotARepositoryWhenDirectoryHasNoGit(t *testing.T) {
	dir := t.TempDir()
	hint := Validate(Request{Path: dir, Mode: ModeOpen})
	if hint.Key != hintNotARepository || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateOpenReturnsOpenFoundWithRootAndBranch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)

	hint := Validate(Request{Path: target, Mode: ModeOpen})
	if !hint.OK || hint.Key != hintOpenFound {
		t.Fatalf("hint = %+v", hint)
	}
	if len(hint.Args) != 2 {
		t.Fatalf("args = %+v", hint.Args)
	}
	root, ok := hint.Args[0].(string)
	if !ok || filepath.Clean(root) != filepath.Clean(target) {
		t.Fatalf("root = %v, want %v", hint.Args[0], target)
	}
	branch, ok := hint.Args[1].(string)
	if !ok || branch == "" {
		t.Fatalf("branch = %v", hint.Args[1])
	}
}

func TestValidateOpenFindsRepositoryFromNestedDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	nested := filepath.Join(target, "sub")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	hint := Validate(Request{Path: nested, Mode: ModeOpen})
	if !hint.OK || hint.Key != hintOpenFound {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateCreateReturnsWillCreateForFreshDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	hint := Validate(Request{Path: dir, Name: "fresh", Mode: ModeCreate})
	if !hint.OK || hint.Key != hintWillCreate {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateCreateReturnsWillCreateForEmptyExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	hint := Validate(Request{Path: dir, Name: "x", Mode: ModeCreate})
	if !hint.OK || hint.Key != hintWillCreate {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateCreateReturnsPathNotDirectoryWhenPathIsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	hint := Validate(Request{Path: file, Name: "x", Mode: ModeCreate})
	if hint.Key != hintPathNotDirectory || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateCreateReturnsAlreadyRepositoryWhenPathIsAGitDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)

	hint := Validate(Request{Path: target, Name: "repo", Mode: ModeCreate})
	if hint.Key != hintAlreadyRepository || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestValidateCreateReturnsNameRequiredWhenNameIsBlank(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	hint := Validate(Request{Path: dir, Name: "   ", Mode: ModeCreate})
	if hint.Key != hintNameRequired || hint.OK {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestApplyOpenReturnsDiscoveredLayoutNameAndPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)

	result, err := Apply(Request{Path: target, Mode: ModeOpen})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "repo" {
		t.Fatalf("name = %q", result.Name)
	}
	if filepath.Clean(result.Path) != filepath.Clean(target) {
		t.Fatalf("path = %q, want %q", result.Path, target)
	}
	if result.Layout.WorkTree == "" {
		t.Fatalf("layout = %+v", result.Layout)
	}
}

func TestApplyOpenReturnsErrorWhenNotARepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := Apply(Request{Path: dir, Mode: ModeOpen}); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyCreateInitializesRepositoryAndReturnsResult(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "created")
	result, err := Apply(Request{Path: dir, Name: "created", Mode: ModeCreate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "created" {
		t.Fatalf("name = %q", result.Name)
	}
	if !repo.IsRepository(result.Path) {
		t.Fatalf("path %q is not a repository", result.Path)
	}
}

func TestApplyCreateHonoursBareOption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bare.git")
	result, err := Apply(Request{Path: dir, Name: "bare", Bare: true, Mode: ModeCreate})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Layout.Bare {
		t.Fatalf("layout = %+v", result.Layout)
	}
	if result.Layout.WorkTree != "" {
		t.Fatalf("bare repository must have no work tree: %+v", result.Layout)
	}
}

func TestApplyCreateDefaultsNameToDirectoryBaseWhenBlank(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "auto-name")
	result, err := Apply(Request{Path: dir, Mode: ModeCreate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "auto-name" {
		t.Fatalf("name = %q", result.Name)
	}
}

func TestApplyCreateReturnsErrorWhenPathIsAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(Request{Path: file, Name: "x", Mode: ModeCreate}); err == nil {
		t.Fatal("expected error")
	}
}

func TestBranchOfReturnsEmptyStringWhenHeadCannotBeOpened(t *testing.T) {
	if branchOf(repo.Layout{}) != "" {
		t.Fatal("expected empty branch for an invalid layout")
	}
}

func TestBranchOfReturnsEmptyStringWhenHeadFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	layout, err := discover(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(layout.GitDir, "HEAD")); err != nil {
		t.Fatal(err)
	}
	if branchOf(layout) != "" {
		t.Fatal("expected empty branch when HEAD is missing")
	}
}

func TestBranchOfReturnsRawContentWhenHeadIsDetached(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	layout, err := discover(target)
	if err != nil {
		t.Fatal(err)
	}
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := os.WriteFile(filepath.Join(layout.GitDir, "HEAD"), []byte(hash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := branchOf(layout); got != hash {
		t.Fatalf("branch = %q, want %q", got, hash)
	}
}

func TestNoEnvReturnsEmptyStringForAnyKey(t *testing.T) {
	if noEnv("GIT_DIR") != "" {
		t.Fatal("noEnv must always return an empty string")
	}
}
