package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/config"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

func newLoggedGraphApp(t *testing.T, level slog.Level) (*App, *bytes.Buffer) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}))
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, &buf
}

func TestOpeningARepositoryWritesItsCommitGraphInTheBackground(t *testing.T) {
	a, _, local := cloneForDivergence(t)
	a.graphWrite.wg.Wait()

	if _, err := os.Stat(filepath.Join(local, ".git", "objects", "info", "commit-graph")); err != nil {
		t.Fatalf("the commit graph was not written: %v", err)
	}
}

func TestTheCommitGraphWriteIsLoggedWhenItProducesOne(t *testing.T) {
	a, buf := newLoggedGraphApp(t, slog.LevelInfo)
	previous := ensureCommitGraph
	ensureCommitGraph = func(context.Context, *gitrepo.Repository) (int, error) { return 7, nil }
	t.Cleanup(func() { ensureCommitGraph = previous })

	a.ActivateRepository("r1")
	a.graphWrite.wg.Wait()

	if !strings.Contains(buf.String(), "commit graph written") || !strings.Contains(buf.String(), "commits=7") {
		t.Fatalf("the write was not logged: %s", buf.String())
	}
}

func TestTheCommitGraphWriteIsSilentWhenThereIsNothingToWrite(t *testing.T) {
	a, buf := newLoggedGraphApp(t, slog.LevelDebug)
	previous := ensureCommitGraph
	ensureCommitGraph = func(context.Context, *gitrepo.Repository) (int, error) { return 0, nil }
	t.Cleanup(func() { ensureCommitGraph = previous })

	a.ActivateRepository("r1")
	a.graphWrite.wg.Wait()

	if strings.Contains(buf.String(), "commit graph") {
		t.Fatalf("an empty write was logged: %s", buf.String())
	}
}

func TestAFailedCommitGraphWriteIsLoggedAsAWarning(t *testing.T) {
	a, buf := newLoggedGraphApp(t, slog.LevelWarn)
	previous := ensureCommitGraph
	ensureCommitGraph = func(context.Context, *gitrepo.Repository) (int, error) { return 0, errors.New("disk full") }
	t.Cleanup(func() { ensureCommitGraph = previous })

	a.ActivateRepository("r1")
	a.graphWrite.wg.Wait()

	if !strings.Contains(buf.String(), "write commit graph failed") || !strings.Contains(buf.String(), "disk full") {
		t.Fatalf("the failure was not logged: %s", buf.String())
	}
}

func TestClosingTheRepositoryCancelsARunningCommitGraphWriteWithoutLoggingIt(t *testing.T) {
	a, buf := newLoggedGraphApp(t, slog.LevelDebug)
	started := make(chan struct{})
	previous := ensureCommitGraph
	ensureCommitGraph = func(ctx context.Context, _ *gitrepo.Repository) (int, error) {
		close(started)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	t.Cleanup(func() { ensureCommitGraph = previous })
	a.ActivateRepository("r1")
	<-started

	a.CloseRepository()

	if strings.Contains(buf.String(), "write commit graph failed") {
		t.Fatalf("a cancelled write was logged as a failure: %s", buf.String())
	}
}

func TestStoppingACommitGraphWriteThatNeverStartedDoesNothing(t *testing.T) {
	a, _ := newLoggedGraphApp(t, slog.LevelWarn)

	a.stopGraphWrite()
}
