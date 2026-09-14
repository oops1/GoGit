package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/commit"
)

var errStageBlocked = errors.New("index locked")

func TestCommitIsOfferedForChangesThatAreNotStaged(t *testing.T) {
	if !(State{ActiveRepository: "r", HasChanges: true}).Enabled(CmdCommit) {
		t.Fatal("a working tree with changes must offer the commit")
	}
	if (State{HasChanges: true}).Enabled(CmdCommit) {
		t.Fatal("no commit without a repository")
	}
}

func TestCommitWithoutStagingTakesTheSelectedOrAllChangedFiles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "main")
	buildStagedFileFixture(t, target)
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.Unstage(t.Context(), r, []string{"staged.txt"}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	if err := writeFile(target, "base.txt", "changed\n"); err != nil {
		t.Fatal(err)
	}
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 2)

	var shown []commit.Model
	a.showCommit = func(m commit.Model, _ func(commit.Model, bool)) { shown = append(shown, m) }
	runOnDispatcher(t, a, func() {
		a.filesGrid.Data().Grid.SetSelectedIndex(0)
		a.openCommit()
		a.filesGrid.Data().Grid.SetSelectedIndex(-1)
	})
	a.showCommit = func(m commit.Model, cb func(commit.Model, bool)) {
		shown = append(shown, m)
		cb(commit.Model{Message: "take everything"}, true)
	}
	runOnDispatcher(t, a, a.openCommit)
	a.writeWG.Wait()

	if len(shown) != 2 || shown[0].Files != 1 || shown[1].Files != 2 || shown[1].Staged != 0 {
		t.Fatalf("commit dialogs = %+v", shown)
	}
	waitForWorkingRows(t, a, 0)
}

func TestACommitThatCannotStageIsReported(t *testing.T) {
	target := filepath.Join(t.TempDir(), "main")
	buildStagedFileFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	prev := stageForCommit
	stageForCommit = func(context.Context, *gitrepo.Repository, []string, ops.StageOptions) error { return errStageBlocked }
	t.Cleanup(func() { stageForCommit = prev })

	runOnDispatcher(t, a, func() { a.commitFiles(commit.Model{Message: "x"}, true, []string{"staged.txt"}) })
	a.writeWG.Wait()

	waitForStatusText(t, a, i18n.Tf("Status.CommitFailed", errStageBlocked))
}
