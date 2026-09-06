package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/clone"
	"github.com/oops1/gogit/internal/ui/remotes"
)

func newRemoteTestApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return newFakeWatcher() }
	return a
}

func initRemoteServerRepo(t *testing.T, dir, branch string) hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Init(dir, gitrepo.InitOptions{Bare: true, InitialBranch: branch})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return addRemoteServerCommit(t, dir, branch, "first.txt", "first\n")
}

func addRemoteServerCommit(t *testing.T, dir, branch, name, content string) hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var parents []hash.ObjectID
	if head, err := store.Lookup(refs.BranchName(branch)); err == nil {
		parents = append(parents, head.Target)
	}
	treeID := putChangesTree(t, db, map[string]string{name: content})
	commitID := putChangesCommit(t, db, treeID, parents...)
	setRef(t, store, refs.BranchName(branch), commitID)
	return commitID
}

func readRemoteRef(t *testing.T, gitDir string, name refs.Name) (hash.ObjectID, bool) {
	t.Helper()
	store, err := refs.Open(refs.Options{GitDir: gitDir, Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	ref, err := store.Lookup(name)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return ref.Target, true
}

func cloneIntoRegistry(t *testing.T, a *App, sourceDir, destDir string) {
	t.Helper()
	r, err := ops.Clone(context.Background(), sourceDir, destDir, ops.CloneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	node, err := a.registry.AddRepository("local", destDir, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)
}

func TestHasRemotesReflectsTheOpenRepository(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	plain := filepath.Join(dir, "plain")
	initRemoteServerRepo(t, server, "main")
	initTestRepoWithBranch(t, plain, "main")

	a := newRemoteTestApp(t)
	if a.hasRemotes() {
		t.Fatal("no open repository must report no remotes")
	}

	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)
	if a.hasRemotes() {
		t.Fatal("a repository without remotes must report none")
	}
	if a.State().HasRemotes {
		t.Fatal("state must not report remotes")
	}

	cloneIntoRegistry(t, a, server, local)
	if !a.hasRemotes() {
		t.Fatal("a clone must have an origin remote")
	}
	if !a.State().HasRemotes {
		t.Fatal("state must report remotes once a clone is active")
	}
}

func TestRemoteCommandGatingWithoutAnOpenRepository(t *testing.T) {
	a := newRemoteTestApp(t)
	if !a.State().Enabled(CmdClone) {
		t.Fatal("clone must always be enabled")
	}
	for _, cmd := range []CommandID{CmdFetch, CmdPull, CmdPush, CmdSync, CmdPrune, CmdManageRemotes} {
		if a.State().Enabled(cmd) {
			t.Fatalf("%s must be disabled without an open repository", cmd)
		}
	}
}

func TestStartCloneClonesAndActivatesTheRepository(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	dest := filepath.Join(dir, "cloned")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)

	a.startClone(clone.Result{URL: server, Directory: dest})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	check, err := gitrepo.Open(dest, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatalf("clone did not create a repository: %v", err)
	}
	if err := check.Close(); err != nil {
		t.Fatal(err)
	}
	node, ok := a.registry.FindByPath(dest)
	if !ok {
		t.Fatal("cloned repository was not added to the registry")
	}
	if a.State().ActiveRepository != node.ID {
		t.Fatal("cloned repository must become active")
	}
}

func TestOpenCloneDialogWiresOKToStartClone(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	dest := filepath.Join(dir, "cloned")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	prev := newCloneView
	var captured *clone.View
	t.Cleanup(func() { newCloneView = prev })
	newCloneView = func(eng widget.ModalShower, req clone.Request) (*clone.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		captured = view
		return view, nil
	}

	a.openClone()
	captured.OnOK(clone.Result{URL: server, Directory: dest})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	if _, ok := a.registry.FindByPath(dest); !ok {
		t.Fatal("OK on the clone dialog must start the clone operation")
	}
}

func TestOpenCloneDialogFailureIsLoggedAndDoesNothing(t *testing.T) {
	a := newRemoteTestApp(t)
	prev := newCloneView
	wantErr := errors.New("boom")
	newCloneView = func(widget.ModalShower, clone.Request) (*clone.View, error) { return nil, wantErr }
	t.Cleanup(func() { newCloneView = prev })

	a.openClone()
}

func TestCheckCloneURLReportsBranchesFromTheServer(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	initRemoteServerRepo(t, server, "main")
	addRemoteServerCommit(t, server, "second-branch", "second.txt", "second\n")

	a := newRemoteTestApp(t)
	view, err := newCloneView(a.eng, clone.Request{})
	if err != nil {
		t.Fatal(err)
	}
	a.checkCloneURL(view, server)
	drainPostQueue(t, a)
}

func TestCheckCloneURLReportsFailureForAnUnreachableServer(t *testing.T) {
	a := newRemoteTestApp(t)
	view, err := newCloneView(a.eng, clone.Request{})
	if err != nil {
		t.Fatal(err)
	}
	a.checkCloneURL(view, filepath.Join(t.TempDir(), "missing"))
	drainPostQueue(t, a)
}

func drainPostQueue(t *testing.T, a *App) {
	t.Helper()
	readOnDispatcher(t, a, func() bool { return true })
}

func TestCloneBranchesFromRefsFindsHeadAndBranches(t *testing.T) {
	refList := []transport.Ref{
		{Name: "HEAD", Symref: "refs/heads/main"},
		{Name: "refs/heads/main"},
		{Name: "refs/heads/dev"},
		{Name: "refs/tags/v1"},
	}
	branchNames, head := cloneBranchesFromRefs(refList)
	if head != "main" {
		t.Fatalf("head = %q", head)
	}
	if len(branchNames) != 2 || branchNames[0] != "dev" || branchNames[1] != "main" {
		t.Fatalf("branches = %v", branchNames)
	}
}

func TestFetchUpdatesRemoteTrackingBranchesAndLogsObjectCount(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	newCommit := addRemoteServerCommit(t, server, "main", "second.txt", "second\n")

	if !a.Dispatch(CmdFetch) {
		t.Fatal("fetch must be dispatched once a remote exists")
	}
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.RemoteBranchName("origin", "main"))
	if !ok || got != newCommit {
		t.Fatalf("remote-tracking ref = %v (found=%v), want %v", got, ok, newCommit)
	}
}

func TestFetchReportsUpToDateWhenNothingChanged(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	a.startFetch()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
}

func TestPullFastForwardsTheCurrentBranch(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	newCommit := addRemoteServerCommit(t, server, "main", "second.txt", "second\n")

	a.startPull()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.BranchName("main"))
	if !ok || got != newCommit {
		t.Fatalf("local branch = %v (found=%v), want %v", got, ok, newCommit)
	}
}

func TestPushSendsLocalCommitsToTheServer(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	newCommit := addRemoteServerCommit(t, local, "main", "local.txt", "local\n")

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if !ok || got != newCommit {
		t.Fatalf("server branch = %v (found=%v), want %v", got, ok, newCommit)
	}
}

func TestSyncPullsThenPushes(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	serverCommit := addRemoteServerCommit(t, server, "main", "second.txt", "second\n")

	a.startSync()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.BranchName("main"))
	if !ok || got != serverCommit {
		t.Fatalf("sync must pull first, local branch = %v (found=%v), want %v", got, ok, serverCommit)
	}
}

func TestPruneRemovesStaleRemoteTrackingBranches(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")
	addRemoteServerCommit(t, server, "feature", "feature.txt", "feature\n")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)
	a.startFetch()
	waitForFinishedOperation(t, a, lastOperationView(t, views))

	if _, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.RemoteBranchName("origin", "feature")); !ok {
		t.Fatal("feature branch must have been fetched")
	}

	if err := ops.RemoveRemote(mustOpenRepo(t, server), "does-not-exist"); err == nil {
		t.Fatal("removing a missing remote must fail")
	}

	deleteServerBranch(t, server, "feature")

	a.startPrune()
	waitForFinishedOperation(t, a, lastOperationView(t, views))

	if _, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.RemoteBranchName("origin", "feature")); ok {
		t.Fatal("prune must remove the stale remote-tracking branch")
	}
}

func mustOpenRepo(t *testing.T, dir string) *gitrepo.Repository {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func deleteServerBranch(t *testing.T, dir, branch string) {
	t.Helper()
	store, err := refs.Open(refs.Options{GitDir: dir, Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	name := refs.BranchName(branch)
	current, err := store.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	tx := store.Begin()
	if err := tx.Delete(name, current.Target); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestFetchCancellationStopsTheOperation(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	a.startFetch()
	view := lastOperationView(t, views)
	readOnDispatcher(t, a, func() bool { view.OnCancel(); return true })
	waitForFinishedOperation(t, a, view)
}

func TestManageRemotesAddEditAndRemove(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	initTestRepoWithBranch(t, plain, "main")

	a := newRemoteTestApp(t)
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	var view *remotes.View
	prev := newRemotesView
	newRemotesView = func(eng widget.ModalShower, entries []remotes.Entry) (*remotes.View, error) {
		v, err := prev(eng, entries)
		if err != nil {
			return nil, err
		}
		view = v
		return v, nil
	}
	t.Cleanup(func() { newRemotesView = prev })

	a.openManageRemotes()
	if view == nil {
		t.Fatal("remotes dialog was not opened")
	}
	if len(a.remoteEntries(a.opened())) != 0 {
		t.Fatal("a fresh repository must have no remotes")
	}

	view.OnAdd("origin", "https://example.invalid/repo.git")
	if !a.hasRemotes() {
		t.Fatal("adding a remote must be reflected immediately")
	}

	view.OnEdit("origin", "https://example.invalid/other.git")
	entries := a.remoteEntries(a.opened())
	if len(entries) != 1 || entries[0].FetchURL != "https://example.invalid/other.git" {
		t.Fatalf("entries = %+v", entries)
	}

	view.OnRemove("origin")
	if a.hasRemotes() {
		t.Fatal("removing the only remote must clear HasRemotes")
	}

	view.OnAdd("", "")
	if a.hasRemotes() {
		t.Fatal("a rejected add must not create a remote")
	}

	view.OnClose()
}

func TestManageRemotesWithoutAnOpenRepositoryDoesNothing(t *testing.T) {
	a := newRemoteTestApp(t)
	called := false
	prev := newRemotesView
	newRemotesView = func(widget.ModalShower, []remotes.Entry) (*remotes.View, error) {
		called = true
		return prev(a.eng, nil)
	}
	t.Cleanup(func() { newRemotesView = prev })
	a.openManageRemotes()
	if called {
		t.Fatal("manage remotes must not open without an active repository")
	}
}

func TestOpenManageRemotesFailureIsLogged(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	initTestRepoWithBranch(t, plain, "main")
	a := newRemoteTestApp(t)
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	prev := newRemotesView
	wantErr := errors.New("boom")
	newRemotesView = func(widget.ModalShower, []remotes.Entry) (*remotes.View, error) { return nil, wantErr }
	t.Cleanup(func() { newRemotesView = prev })
	a.openManageRemotes()
}

func TestPullReportsNonFastForwardWhenHistoriesDiverge(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	addRemoteServerCommit(t, server, "main", "server-only.txt", "server\n")
	addRemoteServerCommit(t, local, "main", "local-only.txt", "local\n")

	a.startPull()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	lines := readOnDispatcher(t, a, view.Lines)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, i18n.T("Operation.Log.NonFastForward")) {
		t.Fatalf("log = %v, want a non-fast-forward line", lines)
	}
}

func TestPushRejectedWhenTheServerHasDivergedCommits(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	addRemoteServerCommit(t, server, "main", "server-only.txt", "server\n")
	a.startFetch()
	waitForFinishedOperation(t, a, lastOperationView(t, views))

	addRemoteServerCommit(t, local, "main", "local-only.txt", "local\n")

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	lines := readOnDispatcher(t, a, view.Lines)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, i18n.T("Operation.Log.Rejected")) {
		t.Fatalf("log = %v, want a rejected line", lines)
	}
}

func TestPushFailsOnADetachedHead(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	head, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.BranchName("main"))
	if !ok {
		t.Fatal("local branch must exist")
	}
	detachTestHead(t, local, head)

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	if view.Running() {
		t.Fatal("push on a detached head must still finish")
	}
}

func TestSyncStopsAtPullFailureWithoutPushing(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	addRemoteServerCommit(t, server, "main", "server-only.txt", "server\n")
	localOnly := addRemoteServerCommit(t, local, "main", "local-only.txt", "local\n")

	a.startSync()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if !ok {
		t.Fatal("server branch must still exist")
	}
	if got == localOnly {
		t.Fatal("sync must not push once the pull stage fails")
	}
}

func TestAddClonedRepositoryFailsWhenThePathIsAlreadyRegistered(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	dest := filepath.Join(dir, "cloned")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	if _, err := a.registry.AddRepository("existing", dest, ""); err != nil {
		t.Fatal(err)
	}
	views := captureOperationViews(t)

	a.startClone(clone.Result{URL: server, Directory: dest})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	if a.State().ActiveRepository != "" {
		t.Fatal("a duplicate path must not become active")
	}
}

func TestHasRemotesReturnsFalseWhenTheRepositoryCannotBeReopened(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	cloneIntoRegistry(t, a, server, local)

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })

	if a.hasRemotes() {
		t.Fatal("a repository that cannot be reopened must report no remotes")
	}
}

func TestRunRemoteJobDoesNothingWithoutAnOpenRepository(t *testing.T) {
	a := newRemoteTestApp(t)
	views := captureOperationViews(t)

	a.startFetch()

	if len(*views) != 0 {
		t.Fatal("a remote job must not open an operation window without an open repository")
	}
}

func TestFetchFailsWhenTheRepositoryCannotBeReopened(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })

	a.startFetch()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
}

func TestFetchUsesTheConfiguredDefaultRemoteWhenTheBranchHasNoUpstream(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	upstream := filepath.Join(dir, "upstream")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, origin, "main")
	upstreamCommit := initRemoteServerRepo(t, upstream, "main")
	initTestRepoWithBranch(t, local, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	node, err := a.registry.AddRepository("local", local, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	r, err := a.freshRepo(a.opened())
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(r, "origin", origin); err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(r, "upstream", upstream); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	a.cfg.Git.DefaultRemote = "upstream"
	a.startFetch()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.RemoteBranchName("upstream", "main"))
	if !ok || got != upstreamCommit {
		t.Fatalf("upstream remote-tracking ref = %v (found=%v), want %v", got, ok, upstreamCommit)
	}
	if _, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.RemoteBranchName("origin", "main")); ok {
		t.Fatal("fetch must not touch a remote other than the configured default")
	}
}

func TestPullFailsWhenTheRepositoryCannotBeReopened(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })

	a.startPull()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
}

func TestPushFailsWhenTheRepositoryCannotBeReopened(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
}

func TestRemoteEntriesReturnsNilWhenTheRepositoryCannotBeReopened(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	initTestRepoWithBranch(t, plain, "main")

	a := newRemoteTestApp(t)
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })

	if entries := a.remoteEntries(a.opened()); entries != nil {
		t.Fatalf("entries = %+v, want nil when the repository cannot be reopened", entries)
	}
}

func TestMutateRemoteReportsErrorWhenTheRepositoryCannotBeReopened(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	initTestRepoWithBranch(t, plain, "main")

	a := newRemoteTestApp(t)
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	var view *remotes.View
	prev := newRemotesView
	newRemotesView = func(eng widget.ModalShower, entries []remotes.Entry) (*remotes.View, error) {
		v, err := prev(eng, entries)
		if err != nil {
			return nil, err
		}
		view = v
		return v, nil
	}
	t.Cleanup(func() { newRemotesView = prev })
	a.openManageRemotes()

	prevOpen := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }

	view.OnAdd("origin", "https://example.invalid/repo.git")
	openGitRepository = prevOpen
	if a.hasRemotes() {
		t.Fatal("a mutation that failed to reopen the repository must not add a remote")
	}
}

func TestManageRemotesEntriesAreSortedByName(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	initTestRepoWithBranch(t, plain, "main")

	a := newRemoteTestApp(t)
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	o := a.opened()
	r, err := a.freshRepo(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(r, "zzz", "https://example.invalid/zzz.git"); err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(r, "aaa", "https://example.invalid/aaa.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	entries := a.remoteEntries(o)
	if len(entries) != 2 || entries[0].Name != "aaa" || entries[1].Name != "zzz" {
		t.Fatalf("entries = %+v, want sorted by name", entries)
	}
}

func TestPullReportsUpToDateWhenNothingChanged(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	a.startPull()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	lines := readOnDispatcher(t, a, view.Lines)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, i18n.T("Operation.Log.UpToDate")) {
		t.Fatalf("log = %v, want an up-to-date line", lines)
	}
}

func TestPullFailsWithoutAnUpstream(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	plain := filepath.Join(dir, "plain")
	initRemoteServerRepo(t, server, "main")
	initTestRepoWithBranch(t, plain, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)

	r, err := a.freshRepo(a.opened())
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(r, "origin", server); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	a.startPull()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	if view.Running() {
		t.Fatal("a pull without an upstream must still finish")
	}
}

func TestDefaultPushRefspecFailsWhenTheBranchSnapshotCannotLoad(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)

	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("boom") }
	t.Cleanup(func() { loadBranchSnapshot = prev })

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	if view.Running() {
		t.Fatal("push must finish even when the branch snapshot cannot load")
	}
}

func TestStartCloneReturnsAnErrorFromAnUnreachableServer(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "cloned")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)

	a.startClone(clone.Result{URL: filepath.Join(dir, "missing"), Directory: dest})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	if _, ok := a.registry.FindByPath(dest); ok {
		t.Fatal("a failed clone must not register a repository")
	}
}

func TestAddClonedRepositoryLogsWhenSavingTheConfigFails(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	dest := filepath.Join(dir, "cloned")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)

	if err := writeFile(a.paths.ConfigFile(), "placeholder", "x"); err != nil {
		t.Fatal(err)
	}

	a.startClone(clone.Result{URL: server, Directory: dest})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	if _, ok := a.registry.FindByPath(dest); !ok {
		t.Fatal("the repository must still be registered even when saving the config fails")
	}
}

func TestOpenCloneDialogCheckInvokesTheURLCheck(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	var captured *clone.View
	prev := newCloneView
	newCloneView = func(eng widget.ModalShower, req clone.Request) (*clone.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		captured = view
		return view, nil
	}
	t.Cleanup(func() { newCloneView = prev })

	a.openClone()
	if captured == nil {
		t.Fatal("clone dialog was not opened")
	}
	captured.OnCheck(server)
	drainPostQueue(t, a)
}

func TestOpenCloneDialogCancelClosesTheModal(t *testing.T) {
	a := newRemoteTestApp(t)
	var captured *clone.View
	prev := newCloneView
	newCloneView = func(eng widget.ModalShower, req clone.Request) (*clone.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		captured = view
		return view, nil
	}
	t.Cleanup(func() { newCloneView = prev })

	a.openClone()
	if captured == nil {
		t.Fatal("clone dialog was not opened")
	}
	captured.OnCancel()
}
