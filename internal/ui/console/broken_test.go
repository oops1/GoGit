package console

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

var errStoreClosed = errors.New("the store cannot be opened")

func breakRefsStore(t *testing.T) {
	t.Helper()
	prev := openRefsStore
	openRefsStore = func(refs.Options) (*refs.Store, error) { return nil, errStoreClosed }
	t.Cleanup(func() { openRefsStore = prev })
}

func breakObjectDatabase(t *testing.T) {
	t.Helper()
	prev := openObjectsDB
	openObjectsDB = func(string, odb.Options) (*odb.DB, error) { return nil, errStoreClosed }
	t.Cleanup(func() { openObjectsDB = prev })
}

func TestEveryCommandReportsAnUnreadableRefStore(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	breakRefsStore(t)

	for _, line := range []string{"status", "log", "branch", "branch feature", "branch -m trunk", "push"} {
		if err := r.runFails(line); !errors.Is(err, errStoreClosed) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestEveryCommandReportsAnUnreadableObjectDatabase(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	breakObjectDatabase(t)

	for _, line := range []string{"status", "log", "branch -v", "branch feature"} {
		if err := r.runFails(line); !errors.Is(err, errStoreClosed) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestCommitAllReportsAFailedStaging(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "one\n"})
	r.write("a.txt", "two\n")
	prev := stageForCommit
	stageForCommit = func(context.Context, *gitrepo.Repository, []string, ops.StageOptions) error {
		return errStoreClosed
	}
	t.Cleanup(func() { stageForCommit = prev })

	if err := r.runFails("commit -a -m nope"); !errors.Is(err, errStoreClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestStashRestoreRefusesAnUnknownOption(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("stash apply --bogus"); !errors.Is(err, ErrUnknownOption) {
		t.Fatalf("err = %v", err)
	}
}

func TestLogReportsABrokenCommitGraphSetting(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("config core.commitGraph maybe")
	r.reopen()

	if err := r.runFails("log"); err == nil {
		t.Fatal("an unreadable commit graph setting must be reported")
	}
}

func TestPullReportsAnUnreachableRemote(t *testing.T) {
	_, client := clonedPair(t)
	client.run("remote set-url origin " + client.dir + "-gone")
	client.reopen()

	if err := client.runFails("pull"); err == nil {
		t.Fatal("pulling from a missing remote must fail")
	}
}
