package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

const testClock = 1700000000

type testRepo struct {
	t    *testing.T
	repo *gitrepo.Repository
	dir  string
	env  Env
}

func isolatedGlobalFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	return newTestRepoAt(t, filepath.Join(t.TempDir(), "repo"))
}

func newBareTestRepo(t *testing.T) *testRepo {
	t.Helper()
	return initTestRepo(t, filepath.Join(t.TempDir(), "bare.git"), true)
}

func newTestRepoAt(t *testing.T, dir string) *testRepo {
	t.Helper()
	return initTestRepo(t, dir, false)
}

func initTestRepo(t *testing.T, dir string, bare bool) *testRepo {
	t.Helper()
	global := isolatedGlobalFile(t)
	r, err := gitrepo.Init(dir, gitrepo.InitOptions{Bare: bare, InitialBranch: "main", NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatal(err)
	}
	writeConfigIdentity(t, r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := gitrepo.Open(dir, gitrepo.OpenOptions{NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatal(err)
	}
	tr := &testRepo{
		t:    t,
		repo: opened,
		dir:  dir,
		env:  Env{Repo: opened, Now: time.Unix(testClock, 0).UTC()},
	}
	t.Cleanup(func() { _ = tr.repo.Close() })
	return tr
}

func writeConfigIdentity(t *testing.T, r *gitrepo.Repository) {
	t.Helper()
	data, err := r.CommonRoot().ReadFile("config")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("[user]\n\tname = Go Git\n\temail = gogit@example.com\n")...)
	if err := r.CommonRoot().WriteFile("config", data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *testRepo) reopen() {
	r.t.Helper()
	options := r.repo.Options()
	if err := r.repo.Close(); err != nil {
		r.t.Fatal(err)
	}
	opened, err := gitrepo.Open(r.dir, options)
	if err != nil {
		r.t.Fatal(err)
	}
	r.repo = opened
	r.env.Repo = opened
}

func (r *testRepo) write(name, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) remove(name string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, filepath.FromSlash(name))); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) run(line string) string {
	r.t.Helper()
	out, err := Run(r.t.Context(), r.env, line)
	if err != nil {
		r.t.Fatalf("%q returned error %v", line, err)
	}
	return out
}

func (r *testRepo) runFails(line string) error {
	r.t.Helper()
	out, err := Run(r.t.Context(), r.env, line)
	if err == nil {
		r.t.Fatalf("%q unexpectedly succeeded with %q", line, out)
	}
	return err
}

func (r *testRepo) commit(message string, files map[string]string) hash.ObjectID {
	r.t.Helper()
	paths := make([]string, 0, len(files))
	for name, content := range files {
		r.write(name, content)
		paths = append(paths, name)
	}
	if len(paths) > 0 {
		if err := ops.Stage(r.t.Context(), r.repo, paths, ops.StageOptions{}); err != nil {
			r.t.Fatal(err)
		}
	}
	id, err := ops.Commit(r.t.Context(), r.repo, ops.CommitOptions{Message: message, When: r.env.Now})
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *testRepo) setRemoteBranch(remoteName, branch string, target hash.ObjectID) {
	r.t.Helper()
	committer := func() object.Signature {
		return object.Signature{Name: "Go Git", Email: "gogit@example.com", When: r.env.Now}
	}
	store, err := refs.Open(refs.Options{GitDir: r.repo.GitDir(), CommonDir: r.repo.CommonDir(), Committer: committer})
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	tx := store.Begin()
	if err := tx.Set(refs.RemoteBranchName(remoteName, branch), target); err != nil {
		r.t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatal(err)
	}
}

func lines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
