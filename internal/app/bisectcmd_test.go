package app

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/dialogs/bisect"
)

func buildBisectRepo(t *testing.T, target string, commits int) []hash.ObjectID {
	t.Helper()
	initTestRepoWithBranch(t, target, "main")
	setTestUserIdentity(t, target)
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var ids []hash.ObjectID
	for i := range commits {
		if err := writeFile(target, "f.txt", strconv.Itoa(i)+"\n"); err != nil {
			t.Fatal(err)
		}
		if err := ops.Stage(t.Context(), r, []string{"f.txt"}, ops.StageOptions{}); err != nil {
			t.Fatal(err)
		}
		id, err := ops.Commit(t.Context(), r, ops.CommitOptions{Message: "c" + strconv.Itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := ops.CreateTag(t.Context(), r, "base", ids[2].String(), ops.CreateTagOptions{}); err != nil {
		t.Fatal(err)
	}
	return ids
}

func bisectApp(t *testing.T, commits int) (*App, string, []hash.ObjectID) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	ids := buildBisectRepo(t, target, commits)
	a := activatedWorkingApp(t, target)
	waitForWorkingIdle(t, a)
	return a, target, ids
}

func bisectStatusOf(t *testing.T, target string) ops.BisectStatus {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	status, err := ops.ReadBisectStatus(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func captureBisectViews(t *testing.T) *[]*bisect.View {
	t.Helper()
	views := &[]*bisect.View{}
	prev := newBisectView
	newBisectView = func() (*bisect.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newBisectView = prev })
	return views
}

func bisectBannerButtonsVisible(t *testing.T, a *App) bool {
	t.Helper()
	return readOnDispatcher(t, a, func() bool {
		return a.banner.good.IsVisible() && a.banner.bad.IsVisible() && a.banner.skip.IsVisible()
	})
}

func TestTheBisectDialogStartsTheSearchAndTheBannerDrivesItToTheFirstBadCommit(t *testing.T) {
	a, _, ids := bisectApp(t, 8)
	views := captureBisectViews(t)

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdBisect) })
	if len(*views) != 1 {
		t.Fatalf("views = %d", len(*views))
	}
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool { view.Dialog().DefaultAction(); return true })

	waitForWorkingIdle(t, a)
	waitForBannerText(t, a, i18n.Tf("Banner.Bisect.Testing", "main", 2, 1))
	if !bisectBannerButtonsVisible(t, a) {
		t.Fatal("the bisect banner hides its good, bad and skip buttons")
	}

	readOnDispatcher(t, a, func() bool { a.banner.bad.OnClick(); return true })
	waitForWorkingIdle(t, a)
	waitForBannerText(t, a, i18n.Tf("Banner.Bisect.Testing", "main", 0, 0))

	readOnDispatcher(t, a, func() bool { a.banner.good.OnClick(); return true })
	waitForWorkingIdle(t, a)
	waitForBannerText(t, a, i18n.Tf("Banner.Bisect.Found", shortHash(ids[4]), "c4"))
	waitForStatusText(t, a, i18n.Tf("Status.Bisect.Found", shortHash(ids[4]), "c4"))
	if bisectBannerButtonsVisible(t, a) {
		t.Fatal("a finished bisect still offers its marks")
	}

	readOnDispatcher(t, a, func() bool { a.banner.commit.OnClick(); return true })
	waitForStatusText(t, a, i18n.T("Status.BisectReset"))
	waitForBanner(t, a, false)
}

func TestTheBannerCanSkipARevision(t *testing.T) {
	a, target, ids := bisectApp(t, 8)

	readOnDispatcher(t, a, func() bool { a.startBisect(bisect.Choice{Bad: "main", Good: "base"}); return true })
	waitForWorkingIdle(t, a)
	waitForBannerText(t, a, i18n.Tf("Banner.Bisect.Testing", "main", 2, 1))

	readOnDispatcher(t, a, func() bool { a.banner.skip.OnClick(); return true })
	waitForWorkingIdle(t, a)
	waitForStatusText(t, a, i18n.Tf("Status.Bisect.Testing", 2, 1))

	status := bisectStatusOf(t, target)
	if status.Skipped != 1 || status.Current == ids[4] {
		t.Fatalf("status = %+v, the skipped revision is still being tested", status)
	}
}

func TestAFailedBisectStepIsReported(t *testing.T) {
	a, _, _ := bisectApp(t, 4)
	prev := runMarkBisect
	runMarkBisect = func(context.Context, *gitrepo.Repository, ops.BisectMark) (ops.BisectStatus, error) {
		return ops.BisectStatus{}, errors.New("boom")
	}
	t.Cleanup(func() { runMarkBisect = prev })

	readOnDispatcher(t, a, func() bool { a.markBisect(ops.BisectGood); return true })

	waitForStatusText(t, a, i18n.Tf("Status.BisectFailed", errors.New("boom")))
}

func TestWithoutARepositoryTheBisectCommandsDoNothing(t *testing.T) {
	a := newTestApp(t)
	views := captureBisectViews(t)

	a.openBisect()
	a.startBisect(bisect.Choice{Bad: "main", Good: "base"})

	if len(*views) != 0 {
		t.Fatalf("views = %d", len(*views))
	}
	if status := a.workingBisectStatus(ops.MergeState{Bisecting: true, BisectStart: "main"}); status.Start != "main" {
		t.Fatalf("status = %+v", status)
	}
}

func TestBisectDialogFailuresAreLogged(t *testing.T) {
	a, _, _ := bisectApp(t, 4)
	prevView := newBisectView
	newBisectView = func() (*bisect.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newBisectView = prevView })
	readOnDispatcher(t, a, func() bool { a.openBisect(); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	readOnDispatcher(t, a, func() bool { a.openBisect(); return true })
}

func TestAnUnreadableBisectStatusKeepsTheOriginalBanner(t *testing.T) {
	a, _, _ := bisectApp(t, 4)
	prev := readBisectStatus
	readBisectStatus = func(context.Context, *gitrepo.Repository) (ops.BisectStatus, error) {
		return ops.BisectStatus{}, errors.New("garbled")
	}
	t.Cleanup(func() { readBisectStatus = prev })

	status := a.workingBisectStatus(ops.MergeState{Bisecting: true, BisectStart: "main"})

	if status.Start != "main" || status.Outcome != ops.BisectPending {
		t.Fatalf("status = %+v", status)
	}
	if got := a.workingBisectStatus(ops.MergeState{}); got.Start != "" {
		t.Fatalf("a repository without a bisect reported %+v", got)
	}
}

func TestBisectCandidatesOfferBranchesRemotesAndTags(t *testing.T) {
	snap := branches.Snapshot{
		Current: "topic",
		Local:   []branches.Branch{{Name: refs.BranchName("main")}, {Name: refs.BranchName("topic")}},
		Remotes: []branches.Remote{{Branches: []branches.Branch{
			{Name: refs.RemoteBranchName("origin", "HEAD"), SymbolicTarget: "refs/remotes/origin/main"},
			{Name: refs.RemoteBranchName("origin", "main")},
		}}},
		Tags: []branches.Tag{{Name: refs.TagName("v1.0")}},
	}

	known := bisectCandidates(snap)

	want := []string{"main", "topic", "origin/main", "v1.0"}
	if known.Bad != "topic" || known.Good != "main" {
		t.Fatalf("known = %+v", known)
	}
	for i, rev := range want {
		if i >= len(known.Revs) || known.Revs[i] != rev {
			t.Fatalf("revs = %v, want %v", known.Revs, want)
		}
	}
}

func TestBisectCandidatesFallBackToTheFirstRevisionWhenHeadIsDetached(t *testing.T) {
	snap := branches.Snapshot{Detached: true, Local: []branches.Branch{{Name: refs.BranchName("main")}}}

	known := bisectCandidates(snap)

	if known.Bad != "main" || known.Good != "" {
		t.Fatalf("known = %+v", known)
	}
	if empty := bisectCandidates(branches.Snapshot{}); empty.Bad != "" || empty.Good != "" {
		t.Fatalf("an empty repository offers %+v", empty)
	}
}

func TestTheBisectTextsFollowTheOutcome(t *testing.T) {
	id := hash.SumSHA1("commit", []byte("x"))
	tests := []struct {
		status  ops.BisectStatus
		banner  string
		message string
		marks   bool
	}{
		{ops.BisectStatus{Outcome: ops.BisectPending, Start: "main"},
			i18n.Tf("Banner.Bisect.Active", "main"), i18n.T("Status.Bisect.Pending"), true},
		{ops.BisectStatus{Outcome: ops.BisectTesting, Start: "main", Remaining: 3, Steps: 2},
			i18n.Tf("Banner.Bisect.Testing", "main", 3, 2), i18n.Tf("Status.Bisect.Testing", 3, 2), true},
		{ops.BisectStatus{Outcome: ops.BisectMergeBase, Start: "main", Current: id},
			i18n.Tf("Banner.Bisect.MergeBase", "main", shortHash(id)), i18n.Tf("Status.Bisect.MergeBase", shortHash(id)), true},
		{ops.BisectStatus{Outcome: ops.BisectFound, Start: "main", Current: id, Subject: "broke it"},
			i18n.Tf("Banner.Bisect.Found", shortHash(id), "broke it"), i18n.Tf("Status.Bisect.Found", shortHash(id), "broke it"), false},
		{ops.BisectStatus{Outcome: ops.BisectOnlySkipped, Start: "main", Candidates: []ops.BisectCandidate{{Commit: id}, {Commit: id}}},
			i18n.Tf("Banner.Bisect.OnlySkipped", 2), i18n.Tf("Status.Bisect.OnlySkipped", 2), false},
		{ops.BisectStatus{Outcome: ops.BisectAmbiguous, Start: "main", Current: id},
			i18n.Tf("Banner.Bisect.Ambiguous", shortHash(id)), i18n.Tf("Status.Bisect.Ambiguous", shortHash(id)), false},
	}
	for _, tc := range tests {
		if got := bisectBannerText(tc.status); got != tc.banner {
			t.Errorf("banner = %q, want %q", got, tc.banner)
		}
		if got := bisectStatusText(tc.status); got != tc.message {
			t.Errorf("status = %q, want %q", got, tc.message)
		}
		if got := bisectAcceptsMarks(tc.status); got != tc.marks {
			t.Errorf("marks for %v = %v", tc.status.Outcome, got)
		}
	}
}
