//go:build oracle

package commitgraph

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

type oracle struct {
	t    *testing.T
	repo string
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
		t.Fatal(err)
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
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=gc.auto",
			"GIT_CONFIG_VALUE_0=0",
			"GIT_CONFIG_KEY_1=maintenance.auto",
			"GIT_CONFIG_VALUE_1=false",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
		},
	}
	o.run(root, 0, "init", "-q", "-b", "main", "repo")
	o.git(0, "config", "core.autocrlf", "false")
	o.git(0, "config", "core.commitGraph", "true")
	return o
}

func (o *oracle) run(dir string, when int64, args ...string) string {
	o.t.Helper()
	date := strconv.FormatInt(1700000000+when, 10) + " +0300"
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	env := slices.Clone(o.env)
	env = append(env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

func (o *oracle) git(when int64, args ...string) string {
	o.t.Helper()
	return o.run(o.repo, when, args...)
}

func (o *oracle) commitFile(when int64, name, content string) {
	o.t.Helper()
	if err := os.WriteFile(filepath.Join(o.repo, name), []byte(content), 0o644); err != nil {
		o.t.Fatal(err)
	}
	o.git(when, "add", name)
	o.git(when, "commit", "-q", "-m", name+" at "+strconv.FormatInt(when, 10))
}

func (o *oracle) history() []Commit {
	o.t.Helper()
	db, err := odb.Open(filepath.Join(o.repo, ".git", "objects"), odb.Options{})
	if err != nil {
		o.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var commits []Commit
	for objectID, err := range db.All() {
		if err != nil {
			o.t.Fatal(err)
		}
		kind, err := db.Type(objectID)
		if err != nil {
			o.t.Fatal(err)
		}
		if kind != object.TypeCommit {
			continue
		}
		commit, err := db.Commit(objectID)
		if err != nil {
			o.t.Fatal(err)
		}
		commits = append(commits, Commit{ID: objectID, Tree: commit.Tree, Parents: commit.Parents, Time: commit.Committer.When.Unix()})
	}
	return commits
}

func buildTangledHistory(o *oracle) {
	o.commitFile(10, "base.txt", "base\n")
	for i, branch := range []string{"one", "two", "three"} {
		o.git(20, "switch", "-q", "-c", branch, "main")
		o.commitFile(int64(30+i), branch+".txt", branch+"\n")
	}
	o.git(40, "switch", "-q", "main")
	o.commitFile(50, "main.txt", "main\n")
	o.git(60, "merge", "-q", "--no-edit", "one")
	o.git(70, "merge", "-q", "--no-edit", "two", "three")
	o.commitFile(80, "after.txt", "after\n")
}

func TestOurGraphIsByteForByteTheOneGitWrites(t *testing.T) {
	o := newOracle(t)
	buildTangledHistory(o)

	o.git(0, "-c", "commitGraph.generationVersion=1", "commit-graph", "write", "--reachable", "--no-changed-paths")
	gitGraph, err := os.ReadFile(filepath.Join(o.repo, ".git", "objects", "info", FileName))
	if err != nil {
		t.Fatal(err)
	}

	ours, err := Encode(hash.SHA1, o.history())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ours, gitGraph) {
		t.Fatalf("graphs differ: ours %d bytes, git %d bytes", len(ours), len(gitGraph))
	}
}

func TestGitVerifiesTheGraphWeWrite(t *testing.T) {
	o := newOracle(t)
	buildTangledHistory(o)

	if err := WriteFile(filepath.Join(o.repo, ".git", "objects", "info"), hash.SHA1, o.history()); err != nil {
		t.Fatal(err)
	}

	o.git(0, "commit-graph", "verify")
	if log := o.git(0, "log", "--oneline", "--all"); strings.Count(log, "\n") != len(o.history()) {
		t.Fatalf("log through our graph = %q", log)
	}
}
