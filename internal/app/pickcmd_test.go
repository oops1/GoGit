package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/journal"
)

func branchTip(t *testing.T, target, branch string) hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	ref, err := store.Resolve(refs.BranchName(branch))
	if err != nil {
		t.Fatal(err)
	}
	return ref.Target
}

func waitForStatusPrefix(t *testing.T, a *App, key string) {
	t.Helper()
	prefix, _, _ := strings.Cut(i18n.T(key), "%")
	deadline := time.Now().Add(testTimeout)
	for !strings.HasPrefix(readOnDispatcher(t, a, a.statusLabel.Text), prefix) {
		if time.Now().After(deadline) {
			t.Fatalf("status = %q, want it to start with %q", readOnDispatcher(t, a, a.statusLabel.Text), prefix)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheJournalMenuItemsRunTheirCommands(t *testing.T) {
	a := newTestApp(t)
	var reverts []bool
	for _, revert := range []bool{false, true} {
		stub := func(context.Context, *gitrepo.Repository, string, ops.PickOptions) (ops.PickResult, error) {
			reverts = append(reverts, revert)
			return ops.PickResult{}, nil
		}
		if revert {
			prev := runRevert
			runRevert = stub
			t.Cleanup(func() { runRevert = prev })
		} else {
			prev := runCherryPick
			runCherryPick = stub
			t.Cleanup(func() { runCherryPick = prev })
		}
	}
	items := a.pickItems(hash.SumSHA1("commit", []byte("x")))

	items[1].OnClick()
	items[2].OnClick()

	if len(reverts) != 0 || !items[1].Disabled {
		t.Fatalf("without a repository the items ran: %v", reverts)
	}
}

func TestTheJournalMenuOffersToPickAndRevert(t *testing.T) {
	a, target := forkedApp(t, false)
	id := branchTip(t, target, "feature")

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.journalMenu(journal.Row{ID: id}, 0) })

	if len(items) != 7 || items[3].Text != i18n.T("Menu.Context.CherryPick") || items[4].Text != i18n.T("Menu.Context.Revert") || items[3].Disabled {
		t.Fatalf("items = %+v", items)
	}
}

func TestPickingFromTheJournalLandsTheCommit(t *testing.T) {
	a, target := forkedApp(t, false)
	id := branchTip(t, target, "feature")

	readOnDispatcher(t, a, func() bool { a.pickCommit(id, false); return true })

	waitForStatusPrefix(t, a, "Status.Picked")
	if want := i18n.Tf("Status.Picked", shortHash(branchTip(t, target, "main"))); readOnDispatcher(t, a, a.statusLabel.Text) != want {
		t.Fatalf("status = %q, want %q", readOnDispatcher(t, a, a.statusLabel.Text), want)
	}
	if data, err := os.ReadFile(filepath.Join(target, "g.txt")); err != nil || string(data) != "theirs\n" {
		t.Fatalf("g.txt = %q, %v", data, err)
	}
}

func TestRevertingFromTheJournalUndoesTheCommit(t *testing.T) {
	a, target := forkedApp(t, false)
	id := branchTip(t, target, "main")

	readOnDispatcher(t, a, func() bool { a.pickCommit(id, true); return true })

	waitForStatusPrefix(t, a, "Status.Reverted")
	if want := i18n.Tf("Status.Reverted", shortHash(branchTip(t, target, "main"))); readOnDispatcher(t, a, a.statusLabel.Text) != want {
		t.Fatalf("status = %q, want %q", readOnDispatcher(t, a, a.statusLabel.Text), want)
	}
	if data, err := os.ReadFile(filepath.Join(target, "f.txt")); err != nil || strings.Contains(string(data), "OURS") {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
}

func TestAConflictedPickRaisesTheBanner(t *testing.T) {
	a, target := forkedApp(t, true)
	id := branchTip(t, target, "feature")

	readOnDispatcher(t, a, func() bool { a.pickCommit(id, false); return true })

	waitForStatusText(t, a, i18n.Tf("Status.PickConflicts", 1))
	if text := waitForBanner(t, a, true); !strings.HasPrefix(text, strings.SplitN(i18n.T("Banner.CherryPick.Conflicts"), "%", 2)[0]) {
		t.Fatalf("banner = %q", text)
	}
}

func TestAFailedPickIsReported(t *testing.T) {
	a, target := forkedApp(t, false)
	prev := runCherryPick
	runCherryPick = func(context.Context, *gitrepo.Repository, string, ops.PickOptions) (ops.PickResult, error) {
		return ops.PickResult{}, errors.New("no")
	}
	t.Cleanup(func() { runCherryPick = prev })

	readOnDispatcher(t, a, func() bool { a.pickCommit(branchTip(t, target, "feature"), false); return true })

	waitForStatusText(t, a, i18n.Tf("Status.PickFailed", errors.New("no")))
}
