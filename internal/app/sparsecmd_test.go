package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/sparse"
)

func sparseCheckoutApp(t *testing.T) (*App, string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	initTestRepoWithBranch(t, target, "main")
	setTestUserIdentity(t, target)
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"top.txt", "sub/kept.txt", "other/gone.txt"}
	for _, name := range names {
		dir, file := filepath.Split(name)
		if err := writeFile(filepath.Join(target, filepath.FromSlash(dir)), file, name+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ops.Stage(t.Context(), r, names, ops.StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Commit(t.Context(), r, ops.CommitOptions{Message: "initial"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	a := activatedWorkingApp(t, target)
	waitForWorkingIdle(t, a)
	return a, target
}

func captureSparseViews(t *testing.T) *[]*sparse.View {
	t.Helper()
	views := &[]*sparse.View{}
	prev := newSparseView
	newSparseView = func(model sparse.Model) (*sparse.View, error) {
		view, err := prev(model)
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newSparseView = prev })
	return views
}

func TestTheSparseCheckoutDialogNarrowsTheWorkingTree(t *testing.T) {
	a, target := sparseCheckoutApp(t)
	views := captureSparseViews(t)

	readOnDispatcher(t, a, func() bool { a.openSparseCheckout(); return true })
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool {
		view.OnOK(sparse.Model{Enabled: true, Cone: true, Patterns: []string{"sub"}})
		return true
	})

	waitForStatusText(t, a, i18n.T("Status.SparseApplied"))
	if _, err := os.Stat(filepath.Join(target, "other", "gone.txt")); err == nil {
		t.Fatal("other/gone.txt stayed in the working tree outside the sparse cone")
	}
	data, err := os.ReadFile(filepath.Join(target, ".git", "info", "sparse-checkout"))
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if string(data) != "/*\n!/*/\n/sub/\n" {
		t.Fatalf("sparse-checkout file = %q", data)
	}
}

func TestTheSparseCheckoutDialogOpensOnTheRecordedPatterns(t *testing.T) {
	a, _ := sparseCheckoutApp(t)
	views := captureSparseViews(t)

	readOnDispatcher(t, a, func() bool { a.openSparseCheckout(); return true })
	if got := (*views)[0].Model(); got.Enabled {
		t.Fatalf("model = %+v, want a repository without a sparse checkout", got)
	}
	readOnDispatcher(t, a, func() bool {
		(*views)[0].OnOK(sparse.Model{Enabled: true, Cone: true, Patterns: []string{"sub"}})
		return true
	})
	waitForStatusText(t, a, i18n.T("Status.SparseApplied"))

	readOnDispatcher(t, a, func() bool { a.openSparseCheckout(); return true })
	got := (*views)[1].Model()
	if !got.Enabled || !got.Cone || len(got.Patterns) != 1 || got.Patterns[0] != "sub" {
		t.Fatalf("model = %+v", got)
	}
}

func TestSwitchingOffTheSparseCheckoutRestoresTheWorkingTree(t *testing.T) {
	a, target := sparseCheckoutApp(t)
	views := captureSparseViews(t)

	readOnDispatcher(t, a, func() bool { a.openSparseCheckout(); return true })
	readOnDispatcher(t, a, func() bool {
		(*views)[0].OnOK(sparse.Model{Enabled: true, Cone: true, Patterns: []string{"sub"}})
		return true
	})
	waitForStatusText(t, a, i18n.T("Status.SparseApplied"))

	readOnDispatcher(t, a, func() bool { a.applySparseCheckout(sparse.Model{}); return true })
	waitForStatusText(t, a, i18n.T("Status.SparseDisabled"))
	if _, err := os.Stat(filepath.Join(target, "other", "gone.txt")); err != nil {
		t.Fatalf("other/gone.txt was not restored: %v", err)
	}
}

func TestCancellingTheSparseCheckoutDialogChangesNothing(t *testing.T) {
	a, target := sparseCheckoutApp(t)
	views := captureSparseViews(t)

	readOnDispatcher(t, a, func() bool { a.openSparseCheckout(); return true })
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })

	if _, err := os.Stat(filepath.Join(target, "other", "gone.txt")); err != nil {
		t.Fatalf("other/gone.txt disappeared: %v", err)
	}
}

func TestAFailedSparseCheckoutIsReported(t *testing.T) {
	a, _ := sparseCheckoutApp(t)
	prev := setSparseCheckout
	setSparseCheckout = func(context.Context, *gitrepo.Repository, []string, ops.SparseCheckoutOptions) error {
		return errors.New("no")
	}
	t.Cleanup(func() { setSparseCheckout = prev })

	readOnDispatcher(t, a, func() bool {
		a.applySparseCheckout(sparse.Model{Enabled: true, Cone: true, Patterns: []string{"sub"}})
		return true
	})

	waitForStatusText(t, a, i18n.Tf("Status.SparseFailed", errors.New("no")))
}

func TestTheSparseCheckoutNeedsARepositoryAndADialog(t *testing.T) {
	a := newTestApp(t)
	views := captureSparseViews(t)

	a.openSparseCheckout()
	a.applySparseCheckout(sparse.Model{})
	a.reopenAfterSparseCheckout()

	if len(*views) != 0 {
		t.Fatal("the dialog opened without a repository")
	}

	b, _ := sparseCheckoutApp(t)
	prev := newSparseView
	newSparseView = func(sparse.Model) (*sparse.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newSparseView = prev })
	readOnDispatcher(t, b, func() bool { b.openSparseCheckout(); return true })
}

func TestTheSparseModelFallsBackWhenTheSettingsCannotBeRead(t *testing.T) {
	a, _ := sparseCheckoutApp(t)
	prev := listSparseCheckout
	listSparseCheckout = func(*gitrepo.Repository) ([]string, error) { return nil, errors.New("no") }
	t.Cleanup(func() { listSparseCheckout = prev })

	model := readOnDispatcher(t, a, func() sparse.Model { return a.sparseModel(a.opened().repo) })

	if model.Enabled || !model.Cone {
		t.Fatalf("model = %+v, want a disabled cone default", model)
	}
}
