package app

import (
	"path/filepath"
	"testing"

	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/ui/clone"
)

func TestAPartialCloneRecordsThePromisorRemote(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	dest := filepath.Join(dir, "cloned")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)

	a.startClone(clone.Result{URL: server, Directory: dest, Partial: true})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	cloned, err := gitrepo.Open(dest, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatalf("the clone did not create a repository: %v", err)
	}
	defer func() { _ = cloned.Close() }()
	if value, _ := cloned.Config().Get("remote.origin.promisor"); value != "true" {
		t.Fatalf("remote.origin.promisor = %q, want true", value)
	}
	if value, _ := cloned.Config().Get("remote.origin.partialclonefilter"); value != "blob:none" {
		t.Fatalf("remote.origin.partialclonefilter = %q, want blob:none", value)
	}
}

func TestAFullCloneAsksForNoFilter(t *testing.T) {
	if got := cloneFilterOf(clone.Result{}); got != "" {
		t.Fatalf("cloneFilterOf = %q, want no filter", got)
	}
	if got := cloneFilterOf(clone.Result{Partial: true}); got != "blob:none" {
		t.Fatalf("cloneFilterOf = %q, want blob:none", got)
	}
}
