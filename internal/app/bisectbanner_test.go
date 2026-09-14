package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
)

func TestTheBannerOffersToEndABisectStartedElsewhere(t *testing.T) {
	a, target := forkedApp(t, false)
	for name, text := range map[string]string{"BISECT_LOG": "git bisect start\n", "BISECT_START": "main\n"} {
		if err := os.WriteFile(filepath.Join(target, ".git", name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	readOnDispatcher(t, a, func() bool { a.RefreshRepository(); return true })

	if text := waitForBanner(t, a, true); text != i18n.Tf("Banner.Bisect.Active", "main") {
		t.Fatalf("banner = %q", text)
	}
	if got := readOnDispatcher(t, a, func() string { return a.banner.commit.Text }); got != i18n.T("Banner.Bisect.Reset") {
		t.Fatalf("button = %q", got)
	}
	if readOnDispatcher(t, a, a.banner.abort.IsVisible) || a.State().Merging {
		t.Fatalf("a bisect offers to abort or counts as a merge: %+v", a.State())
	}

	readOnDispatcher(t, a, func() bool { a.banner.commit.OnClick(); return true })

	waitForStatusText(t, a, i18n.T("Status.BisectReset"))
	waitForBanner(t, a, false)
	if _, err := os.Stat(filepath.Join(target, ".git", "BISECT_LOG")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("BISECT_LOG = %v", err)
	}
}

func TestAFailedBisectResetIsReported(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := runResetBisect
	runResetBisect = func(context.Context, *gitrepo.Repository) error { return errors.New("boom") }
	t.Cleanup(func() { runResetBisect = prev })

	readOnDispatcher(t, a, func() bool { a.endBisect(); return true })

	waitForStatusText(t, a, i18n.Tf("Status.BisectResetFailed", errors.New("boom")))
}

func TestABisectFromADetachedHeadNamesTheCommit(t *testing.T) {
	id := hash.SumSHA1("commit", []byte("x"))

	if got := bisectOriginLabel(id.String()); got != shortHash(id) {
		t.Fatalf("label = %q", got)
	}
	if got := bisectOriginLabel("main"); got != "main" {
		t.Fatalf("label = %q", got)
	}
}
