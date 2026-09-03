//go:build oracle

package refs

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type oracle struct {
	t    *testing.T
	repo string
	git  string
	env  []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	o := &oracle{
		t:    t,
		repo: filepath.Join(root, "repo"),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_AUTHOR_DATE=1700000000 +0300",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
			"GIT_COMMITTER_DATE=1700000000 +0300",
		},
	}
	o.run(root, "init", "-q", "-b", "main", "repo")
	o.git = filepath.Join(o.repo, ".git")
	return o
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func (o *oracle) git2(args ...string) string {
	o.t.Helper()
	return o.run(o.repo, args...)
}

func (o *oracle) commit(message string) hash.ObjectID {
	o.t.Helper()
	o.git2("commit", "-q", "--allow-empty", "-m", message)
	return o.parse("HEAD")
}

func (o *oracle) parse(revision string) hash.ObjectID {
	o.t.Helper()
	id, err := hash.Parse(strings.TrimSpace(o.git2("rev-parse", revision)))
	if err != nil {
		o.t.Fatalf("Parse returned error %v", err)
	}
	return id
}

func (o *oracle) forEachRef() []string {
	o.t.Helper()
	out := o.git2("for-each-ref", "--format=%(refname) %(objectname) %(*objectname)")
	var records []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimRight(line, " \r"); line != "" {
			records = append(records, line)
		}
	}
	return records
}

func (o *oracle) fsck() {
	o.t.Helper()
	o.git2("fsck", "--no-progress", "--no-dangling")
}

func (o *oracle) store() *Store {
	o.t.Helper()
	store, err := Open(Options{
		GitDir:    o.git,
		Peeler:    gitPeeler{o},
		Committer: testCommitter(),
	})
	if err != nil {
		o.t.Fatalf("Open returned error %v", err)
	}
	o.t.Cleanup(func() {
		if err := store.Close(); err != nil {
			o.t.Errorf("Close returned error %v", err)
		}
	})
	return store
}

type gitPeeler struct {
	o *oracle
}

func (p gitPeeler) PeelTag(id hash.ObjectID) (hash.ObjectID, bool, error) {
	if strings.TrimSpace(p.o.git2("cat-file", "-t", id.String())) != "tag" {
		return hash.Zero, false, nil
	}
	target, err := hash.Parse(strings.TrimSpace(p.o.git2("rev-parse", id.String()+"^{}")))
	return target, true, err
}

func ourRefs(t *testing.T, store *Store) []string {
	t.Helper()
	var records []string
	for ref, err := range store.All() {
		if err != nil {
			t.Fatalf("All returned error %v", err)
		}
		record := string(ref.Name) + " " + ref.Target.String()
		if !ref.Peeled.IsZero() {
			record += " " + ref.Peeled.String()
		}
		records = append(records, record)
	}
	return records
}

func (o *oracle) populate() {
	o.t.Helper()
	o.commit("one")
	o.git2("branch", "feature/one")
	o.commit("two")
	o.git2("branch", "feature/two")
	o.git2("tag", "light")
	o.git2("tag", "-a", "-m", "annotated", "v1")
	o.git2("update-ref", "refs/remotes/origin/main", "refs/heads/main")
	o.git2("update-ref", "refs/remotes/origin/feature/one", "refs/heads/feature/one")
	o.git2("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	o.git2("pack-refs", "--all")
	o.commit("three")
	o.git2("branch", "loose/one")
	o.git2("tag", "-a", "-m", "loose tag", "v2")
}

func TestOracleAllMatchesForEachRef(t *testing.T) {
	o := newOracle(t)
	o.populate()
	if got, want := ourRefs(t, o.store()), o.forEachRef(); !slices.Equal(got, want) {
		t.Fatalf("All returned\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOracleTransactionMatchesGit(t *testing.T) {
	o := newOracle(t)
	o.populate()
	store := o.store()
	head := o.parse("HEAD")
	older := o.parse("HEAD~1")

	tx := store.Begin()
	tx.SetMessage("branch: created from HEAD")
	if err := tx.Update(BranchName("written"), head, hash.Zero); err != nil {
		t.Fatalf("Update returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if got := o.parse("refs/heads/written"); got != head {
		t.Fatalf("git reads %s", got)
	}

	tx = store.Begin()
	tx.SetMessage("reset: moving to HEAD~1")
	if err := tx.Update(HEAD, older, head); err != nil {
		t.Fatalf("Update returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if got := o.parse("refs/heads/main"); got != older {
		t.Fatalf("git reads %s for the branch behind HEAD", got)
	}

	tx = store.Begin()
	tx.SetMessage("checkout: moving from main to feature/one")
	if err := tx.SetSymbolic(HEAD, BranchName("feature/one")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if got := strings.TrimSpace(o.git2("symbolic-ref", "HEAD")); got != "refs/heads/feature/one" {
		t.Fatalf("git symbolic-ref reads %s", got)
	}

	tx = store.Begin()
	tx.SetMessage("branch: deleted")
	if err := tx.Delete(BranchName("loose/one"), head); err != nil {
		t.Fatalf("Delete returned error %v", err)
	}
	if err := tx.Delete(BranchName("feature/two"), hash.Zero); err != nil {
		t.Fatalf("Delete returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if got, want := ourRefs(t, store), o.forEachRef(); !slices.Equal(got, want) {
		t.Fatalf("All returned\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	wantLog := []string{
		older.String() + " reset: moving to HEAD~1",
		o.parse("refs/heads/feature/one").String() + " checkout: moving from main to feature/one",
	}
	if got := o.reflog("HEAD"); !slices.Equal(got[len(got)-2:], wantLog) {
		t.Fatalf("git reflog show HEAD returned\n%s", strings.Join(got, "\n"))
	}
	if got := o.reflog("refs/heads/written"); !slices.Equal(got, []string{head.String() + " branch: created from HEAD"}) {
		t.Fatalf("git reflog show refs/heads/written returned\n%s", strings.Join(got, "\n"))
	}
	o.fsck()
}

func (o *oracle) reflog(name string) []string {
	o.t.Helper()
	out := o.git2("reflog", "show", "--format=%H %gs", name)
	var records []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimRight(line, " \r"); line != "" {
			records = append(records, line)
		}
	}
	slices.Reverse(records)
	return records
}

func TestOraclePackRefsMatchesGit(t *testing.T) {
	o := newOracle(t)
	o.populate()
	store := o.store()
	before := o.forEachRef()

	if err := store.PackRefs(true); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	if got := o.forEachRef(); !slices.Equal(got, before) {
		t.Fatalf("git for-each-ref returned\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(before, "\n"))
	}
	if got := ourRefs(t, store); !slices.Equal(got, before) {
		t.Fatalf("All returned\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(before, "\n"))
	}
	if existsAt(o.git, "refs/heads/loose/one") {
		t.Fatal("loose reference survived pruning")
	}
	o.fsck()

	o.git2("pack-refs", "--all")
	if got := ourRefs(t, store); !slices.Equal(got, before) {
		t.Fatalf("All after git pack-refs returned\n%s", strings.Join(got, "\n"))
	}
}

func TestOracleReadsRepositoryWrittenByGit(t *testing.T) {
	o := newOracle(t)
	o.populate()
	store := o.store()

	head, err := store.Lookup(HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if head.SymbolicTarget != BranchName("main") {
		t.Fatalf("HEAD is %+v", head)
	}
	o.git2("checkout", "-q", "--detach")
	detached, err := store.Lookup(HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if detached.Target != o.parse("HEAD") {
		t.Fatalf("detached HEAD is %+v", detached)
	}
	peeled, err := store.Peel(TagName("v1"))
	if err != nil {
		t.Fatalf("Peel returned error %v", err)
	}
	if peeled != o.parse("refs/tags/v1^{}") {
		t.Fatalf("Peel returned %s", peeled)
	}
	for entry, err := range store.Reflog(BranchName("main")) {
		if err != nil {
			t.Fatalf("Reflog returned error %v", err)
		}
		if entry.Committer.Name != "oracle" {
			t.Fatalf("reflog entry is %+v", entry)
		}
	}
	last, err := store.ReflogLast(BranchName("main"))
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if last.New != o.parse("refs/heads/main") {
		t.Fatalf("last reflog entry is %+v", last)
	}
}
