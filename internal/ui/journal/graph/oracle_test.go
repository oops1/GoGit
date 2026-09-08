//go:build oracle

package graph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type gitRepo struct {
	t   *testing.T
	dir string
	env []string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatal(err)
	}
	r := &gitRepo{
		t:   t,
		dir: t.TempDir(),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
		},
	}
	r.run("init", "-q", "-b", "main", ".")
	return r
}

func (r *gitRepo) run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) commit(name string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name+".txt"), []byte(name+"\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.run("add", ".")
	r.run("commit", "-q", "-m", name)
}

func (r *gitRepo) commits() []Commit {
	r.t.Helper()
	var out []Commit
	for _, line := range strings.Split(r.run("log", "--topo-order", "--all", "--pretty=%H %P"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		c := Commit{ID: mustParse(r.t, fields[0])}
		for _, parent := range fields[1:] {
			c.Parents = append(c.Parents, mustParse(r.t, parent))
		}
		out = append(out, c)
	}
	return out
}

func mustParse(t *testing.T, text string) hash.ObjectID {
	t.Helper()
	parsed, err := hash.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestTheLayoutOfARealHistoryLeadsEveryParentToItsLane(t *testing.T) {
	r := newGitRepo(t)
	r.commit("first")
	r.commit("second")
	r.run("checkout", "-q", "-b", "feature")
	r.commit("feature-one")
	r.commit("feature-two")
	r.run("checkout", "-q", "main")
	r.commit("third")
	r.run("merge", "-q", "--no-ff", "-m", "merge feature", "feature")
	r.run("checkout", "-q", "-b", "other", "HEAD~2")
	r.commit("other-one")
	r.run("checkout", "-q", "main")
	r.commit("fourth")

	commits := r.commits()
	if len(commits) < 8 {
		t.Fatalf("git reported %d commits, want the whole history", len(commits))
	}

	layout := New()
	rows := make([]Row, 0, len(commits))
	for _, c := range commits {
		rows = append(rows, layout.Add(c))
	}

	for i, c := range commits {
		if len(rows[i].Out) != len(c.Parents) {
			t.Fatalf("commit %s has %d parents and %d lines going down",
				c.ID.String()[:7], len(c.Parents), len(rows[i].Out))
		}
	}
	if broken, at := brokenLane(rows); broken {
		t.Fatalf("row %d leaves a line hanging: %+v", at, rows[at])
	}
}

func brokenLane(rows []Row) (bool, int) {
	open := map[int]bool{}
	for at, row := range rows {
		below := map[int]bool{}
		for _, segment := range row.Through {
			below[segment.Lane] = true
		}
		for _, segment := range row.Out {
			below[segment.Lane] = true
		}
		for _, move := range row.Moves {
			below[move.To] = true
		}
		for lane := range open {
			if below[lane] || lane == row.Lane || endsHere(row, lane) {
				continue
			}
			return true, at
		}
		open = below
	}
	return false, 0
}

func endsHere(row Row, lane int) bool {
	for _, move := range row.Moves {
		if move.From == lane {
			return true
		}
	}
	return false
}
