package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func openCloneRefs(t testing.TB, r *repo.Repository) *refs.Store {
	t.Helper()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare()})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func lookupCloneRef(t testing.TB, r *repo.Repository, name refs.Name) (refs.Ref, bool) {
	t.Helper()
	store := openCloneRefs(t, r)
	ref, err := store.Lookup(name)
	if errors.Is(err, refs.ErrNotFound) {
		return refs.Ref{}, false
	}
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	return ref, true
}

func mustCloneDest(t testing.TB) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "dest")
}

func newCloneSource(t testing.TB) *testRepo {
	t.Helper()
	src := newTestRepo(t)
	src.writeFile("a.txt", "hello\n")
	mustStage(t, src, "a.txt")
	first := src.commitAll("initial")
	src.createBranch("feature", first)
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second on main")
	return src
}

func TestCloneFullClonesAllBranchesAndChecksOutDefault(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	if got := r.WorkTree(); got != dest {
		t.Fatalf("WorkTree = %q, want %q", got, dest)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatalf("a.txt was not checked out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "b.txt")); err != nil {
		t.Fatalf("b.txt was not checked out: %v", err)
	}

	mainRef, ok := lookupCloneRef(t, r, refs.BranchName("main"))
	if !ok || mainRef.Target != src.branchTarget("main") {
		t.Fatalf("main ref = %+v ok=%v, want %s", mainRef, ok, src.branchTarget("main"))
	}
	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("origin", "feature")); !ok {
		t.Fatalf("origin/feature was not fetched")
	}

	if url, _ := r.Config().Get("remote.origin.url"); url != src.dir {
		t.Fatalf("remote.origin.url = %q, want %q", url, src.dir)
	}
	if fetchSpec, _ := r.Config().Get("remote.origin.fetch"); fetchSpec != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("remote.origin.fetch = %q", fetchSpec)
	}
	if remoteName, _ := r.Config().Get("branch.main.remote"); remoteName != "origin" {
		t.Fatalf("branch.main.remote = %q, want origin", remoteName)
	}
	if merge, _ := r.Config().Get("branch.main.merge"); merge != "refs/heads/main" {
		t.Fatalf("branch.main.merge = %q, want refs/heads/main", merge)
	}

	headRef, ok := lookupCloneRef(t, r, refs.HEAD)
	if !ok || !headRef.IsSymbolic() || headRef.SymbolicTarget != refs.BranchName("main") {
		t.Fatalf("HEAD = %+v ok=%v, want symbolic refs/heads/main", headRef, ok)
	}
}

func TestCloneBareMirrorsBranchesDirectlyWithoutCheckout(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Bare: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.IsBare() {
		t.Fatalf("repository should be bare")
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err == nil {
		t.Fatalf("a.txt should not have been checked out in a bare clone")
	}
	if _, ok := lookupCloneRef(t, r, refs.BranchName("main")); !ok {
		t.Fatalf("refs/heads/main should exist directly in a bare mirror")
	}
	if _, ok := lookupCloneRef(t, r, refs.BranchName("feature")); !ok {
		t.Fatalf("refs/heads/feature should exist directly in a bare mirror")
	}
	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("origin", "main")); ok {
		t.Fatalf("a bare mirror should not create remote-tracking refs")
	}
	if _, ok := r.Config().Get("branch.main.remote"); ok {
		t.Fatalf("a bare mirror should not record branch tracking config")
	}
	if fetchSpec, _ := r.Config().Get("remote.origin.fetch"); fetchSpec != "+refs/heads/*:refs/heads/*" {
		t.Fatalf("remote.origin.fetch = %q", fetchSpec)
	}
}

func TestCloneWithExplicitBranchChecksOutThatBranchButFetchesAll(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Branch: "feature"})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	headRef, ok := lookupCloneRef(t, r, refs.HEAD)
	if !ok || !headRef.IsSymbolic() || headRef.SymbolicTarget != refs.BranchName("feature") {
		t.Fatalf("HEAD = %+v ok=%v, want symbolic refs/heads/feature", headRef, ok)
	}
	if _, err := os.Stat(filepath.Join(dest, "b.txt")); err == nil {
		t.Fatalf("b.txt belongs to main and should not be checked out on feature")
	}
	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("origin", "main")); !ok {
		t.Fatalf("origin/main should still have been fetched without --single-branch")
	}
}

func TestCloneSingleBranchWithExplicitBranchFetchesOnlyThatBranch(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Branch: "feature", SingleBranch: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("origin", "feature")); !ok {
		t.Fatalf("origin/feature should have been fetched")
	}
	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("origin", "main")); ok {
		t.Fatalf("origin/main should not have been fetched with --single-branch")
	}
}

func TestCloneSingleBranchWithoutExplicitBranchUsesRemoteDefault(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{SingleBranch: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	headRef, ok := lookupCloneRef(t, r, refs.HEAD)
	if !ok || !headRef.IsSymbolic() || headRef.SymbolicTarget != refs.BranchName("main") {
		t.Fatalf("HEAD = %+v ok=%v, want symbolic refs/heads/main", headRef, ok)
	}
	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("origin", "feature")); ok {
		t.Fatalf("origin/feature should not have been fetched")
	}
}

func TestCloneShallowMarksRepositoryShallow(t *testing.T) {
	src := newTestRepo(t)
	src.writeFile("a.txt", "one\n")
	mustStage(t, src, "a.txt")
	src.commitAll("first")
	src.writeFile("a.txt", "two\n")
	mustStage(t, src, "a.txt")
	src.commitAll("second")
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Depth: 1})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	shallow, err := r.IsShallow()
	if err != nil {
		t.Fatalf("IsShallow returned error %v", err)
	}
	if !shallow {
		t.Fatalf("expected a shallow clone")
	}
}

func TestCloneNoCheckoutLeavesWorkingTreeEmpty(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{NoCheckout: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err == nil {
		t.Fatalf("a.txt should not have been checked out with NoCheckout")
	}
	if _, ok := lookupCloneRef(t, r, refs.BranchName("main")); !ok {
		t.Fatalf("refs should still have been created with NoCheckout")
	}
}

func TestCloneCustomRemoteName(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{RemoteName: "upstream"})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	if url, ok := r.Config().Get("remote.upstream.url"); !ok || url != src.dir {
		t.Fatalf("remote.upstream.url = %q ok=%v", url, ok)
	}
	if _, ok := lookupCloneRef(t, r, refs.RemoteBranchName("upstream", "main")); !ok {
		t.Fatalf("upstream/main should have been fetched")
	}
}

func TestCloneFailsWhenTargetDirectoryExistsAndIsNotEmpty(t *testing.T) {
	src := newCloneSource(t)
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("keep\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, ErrCloneTargetNotEmpty) {
		t.Fatalf("err = %v, want ErrCloneTargetNotEmpty", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Fatalf("existing file should not have been touched: %v", err)
	}
}

func TestCloneFailsForUnreachableAddressAndRemovesCreatedDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	dest := mustCloneDest(t)

	_, err := Clone(t.Context(), missing, dest, CloneOptions{})
	if err == nil {
		t.Fatalf("expected an error for an unreachable local address")
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("clone should have removed the directory it created")
	}
}

func TestCloneFailsWhenBranchDoesNotExistOnRemote(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{Branch: "does-not-exist"})
	if !errors.Is(err, ErrRemoteBranchNotFound) {
		t.Fatalf("err = %v, want ErrRemoteBranchNotFound", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("clone should have removed the directory it created")
	}
}

func TestCloneEmptiesPreexistingEmptyDirectoryOnFailure(t *testing.T) {
	src := newCloneSource(t)
	dest := t.TempDir()

	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{Branch: "does-not-exist"})
	if !errors.Is(err, ErrRemoteBranchNotFound) {
		t.Fatalf("err = %v, want ErrRemoteBranchNotFound", err)
	}
	info, statErr := os.Stat(dest)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("the preexisting directory should remain: %v", statErr)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("ReadDir returned error %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory should have been emptied, got %v", entries)
	}
}

func TestCloneContextCanceledReturnsError(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Clone(ctx, src.dir, dest, CloneOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("no directory should have been created")
	}
}

func TestCloneFailsWhenSourceDirectoryDoesNotExist(t *testing.T) {
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), filepath.Join(t.TempDir(), "missing-src"), dest, CloneOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCloneRemoteBranchNotFoundOnBareClone(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{Bare: true, Branch: "does-not-exist"})
	if !errors.Is(err, ErrRemoteBranchNotFound) {
		t.Fatalf("err = %v, want ErrRemoteBranchNotFound", err)
	}
}

func TestCloneOfEmptyRepositorySucceedsWithoutCheckout(t *testing.T) {
	src := newTestRepo(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("ReadDir returned error %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() != ".git" {
			found = true
		}
	}
	if found {
		t.Fatalf("nothing should have been checked out for an empty remote")
	}
}

func TestCloneOfEmptyRepositoryWithSingleBranchSucceeds(t *testing.T) {
	src := newTestRepo(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{SingleBranch: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	_ = r.Close()
}

func TestCloneFromRepositoryWithDetachedHead(t *testing.T) {
	src := newCloneSource(t)
	first := src.branchTarget("main")
	src.writeRawHead(first.String() + "\n")
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	headRef, ok := lookupCloneRef(t, r, refs.HEAD)
	if !ok || headRef.IsSymbolic() {
		t.Fatalf("HEAD = %+v ok=%v, want a detached HEAD", headRef, ok)
	}
	if headRef.Target != first {
		t.Fatalf("HEAD = %s, want %s", headRef.Target, first)
	}
}

func swapCloneReadDir(t testing.TB, replacement func(string) ([]os.DirEntry, error)) {
	t.Helper()
	original := cloneReadDir
	cloneReadDir = replacement
	t.Cleanup(func() { cloneReadDir = original })
}

func swapCloneMkdirAll(t testing.TB, replacement func(string, os.FileMode) error) {
	t.Helper()
	original := cloneMkdirAll
	cloneMkdirAll = replacement
	t.Cleanup(func() { cloneMkdirAll = original })
}

func swapCloneRepoInit(t testing.TB, replacement func(string, repo.InitOptions) (*repo.Repository, error)) {
	t.Helper()
	original := cloneRepoInit
	cloneRepoInit = replacement
	t.Cleanup(func() { cloneRepoInit = original })
}

func swapCloneRepoOpen(t testing.TB, replacement func(string, repo.OpenOptions) (*repo.Repository, error)) {
	t.Helper()
	original := cloneRepoOpen
	cloneRepoOpen = replacement
	t.Cleanup(func() { cloneRepoOpen = original })
}

func swapCloneRepoOpenLayout(t testing.TB, replacement func(repo.Layout, repo.OpenOptions) (*repo.Repository, error)) {
	t.Helper()
	original := cloneRepoOpenLayout
	cloneRepoOpenLayout = replacement
	t.Cleanup(func() { cloneRepoOpenLayout = original })
}

func TestPrepareCloneDirectoryFailsWhenMkdirAllFails(t *testing.T) {
	swapCloneMkdirAll(t, func(string, os.FileMode) error { return errInjected })
	dest := mustCloneDest(t)
	_, err := prepareCloneDirectory(dest)
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestPrepareCloneDirectoryFailsWhenReadDirFailsWithOtherError(t *testing.T) {
	swapCloneReadDir(t, func(string) ([]os.DirEntry, error) { return nil, errInjected })
	dest := mustCloneDest(t)
	_, err := prepareCloneDirectory(dest)
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCleanupCloneDirectoryIgnoresReadDirFailure(t *testing.T) {
	cleanupCloneDirectory(filepath.Join(t.TempDir(), "does-not-exist"), false)
}

func TestCloneFailsWhenRepoInitFails(t *testing.T) {
	swapCloneRepoInit(t, func(string, repo.InitOptions) (*repo.Repository, error) { return nil, errInjected })
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("clone should have removed the directory it created")
	}
}

func TestCloneFailsWhenReopenFails(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	swapCloneRepoOpen(t, func(string, repo.OpenOptions) (*repo.Repository, error) {
		return nil, errInjected
	})
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestFetchCloneObjectsFailsWhenOpenLayoutFails(t *testing.T) {
	swapCloneRepoOpenLayout(t, func(repo.Layout, repo.OpenOptions) (*repo.Repository, error) { return nil, errInjected })
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestPlanCloneFetchFailsWhenLsRemoteFails(t *testing.T) {
	dest := mustCloneDest(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := Clone(t.Context(), missing, dest, CloneOptions{SingleBranch: true})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestWriteCloneRemoteConfigFailsForInvalidRemoteName(t *testing.T) {
	file, err := config.Parse(nil)
	if err != nil {
		t.Fatalf("config.Parse returned error %v", err)
	}
	err = writeCloneRemoteConfig(file, "bad\nname", "https://example.com/repo.git", refspec.DefaultFetch("bad\nname"))
	if err == nil {
		t.Fatalf("expected an error for an invalid remote name")
	}
}

func TestWriteCloneBranchConfigFailsForInvalidBranchName(t *testing.T) {
	file, err := config.Parse(nil)
	if err != nil {
		t.Fatalf("config.Parse returned error %v", err)
	}
	err = writeCloneBranchConfig(file, "origin", "bad\nname")
	if err == nil {
		t.Fatalf("expected an error for an invalid branch name")
	}
}

func TestFinishCloneRefsFailsWhenRefsOpenFails(t *testing.T) {
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestFinishCloneRefsFailsWhenTxUpdateFails(t *testing.T) {
	swapTxUpdate(t, func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error { return errInjected })
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestFinishCloneRefsFailsWhenTxSetSymbolicFails(t *testing.T) {
	swapTxSetSymbolic(t, func(*refs.Transaction, refs.Name, refs.Name) error { return errInjected })
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCloneFailsWhenRemoteConfigCannotBeWritten(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{RemoteName: "bad\nname"})
	if err == nil {
		t.Fatalf("expected an error for an invalid remote name")
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("clone should have removed the directory it created")
	}
}

func TestCloneCommitterUsesConfiguredIdentityWhenPresent(t *testing.T) {
	r := newTestRepo(t)
	sig := cloneCommitter(r.repo)()
	if sig.Name != "ann" || sig.Email != "ann@example.com" {
		t.Fatalf("signature = %+v, want the configured identity", sig)
	}
}

func TestCloneCommitterFallsBackWhenIdentityIsNotConfigured(t *testing.T) {
	dir := t.TempDir()
	global := isolatedGlobalFile(t)
	r, err := repo.Init(dir, repo.InitOptions{NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	sig := cloneCommitter(r)()
	if sig.Name != cloneCommitterName || sig.Email != cloneCommitterEmail {
		t.Fatalf("signature = %+v, want the fallback identity", sig)
	}
}

func TestFinishCloneRefsFailsWhenTxDetachFails(t *testing.T) {
	swapTxDetach(t, func(*refs.Transaction, refs.Name, hash.ObjectID) error { return errInjected })
	src := newCloneSource(t)
	first := src.branchTarget("main")
	src.writeRawHead(first.String() + "\n")
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}
