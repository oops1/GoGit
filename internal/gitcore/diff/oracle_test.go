//go:build oracle

package diff

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

var updateFixtures = flag.Bool("update", false, "rewrite testdata fixtures from the system git")

type oracle struct {
	t    *testing.T
	root string
	repo string
	home string
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
	o := &oracle{t: t, root: root, repo: filepath.Join(root, "repo"), home: home}
	o.run(root, nil, "init", "-q", "--bare", "repo")
	return o
}

func (o *oracle) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"HOME=" + o.home,
		"USERPROFILE=" + o.home,
		"XDG_CONFIG_HOME=" + filepath.Join(o.home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	}
}

func (o *oracle) run(dir string, stdin []byte, args ...string) string {
	o.t.Helper()
	settings := []string{
		"-c", "core.autocrlf=false",
		"-c", "core.eol=lf",
		"-c", "core.quotepath=true",
		"-c", "core.abbrev=7",
		"-c", "diff.noprefix=false",
		"-c", "diff.indentHeuristic=true",
		"-c", "color.ui=false",
		"-c", "color.diff=false",
	}
	cmd := exec.CommandContext(o.t.Context(), "git", append(settings, args...)...)
	cmd.Dir = dir
	cmd.Env = o.env()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, fail bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &fail
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		o.t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, fail.String())
	}
	return out.String()
}

func (o *oracle) parseID(text string) hash.ObjectID {
	o.t.Helper()
	id, err := hash.Parse(strings.TrimSpace(text))
	if err != nil {
		o.t.Fatalf("git produced an unusable object id %q: %v", text, err)
	}
	return id
}

func (o *oracle) writeBlob(data []byte) hash.ObjectID {
	o.t.Helper()
	return o.parseID(o.run(o.repo, data, "hash-object", "-w", "-t", "blob", "--stdin"))
}

func (o *oracle) writeTree(entries []object.TreeEntry) hash.ObjectID {
	o.t.Helper()
	var in bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&in, "%s %s %s\t%s%c", entry.Mode, entry.Mode.ObjectType(), entry.ID, entry.Name, 0)
	}
	return o.parseID(o.run(o.repo, in.Bytes(), "mktree", "-z", "--missing"))
}

func (o *oracle) objects() *odb.DB {
	o.t.Helper()
	db, err := odb.Open(filepath.Join(o.repo, "objects"), odb.Options{})
	if err != nil {
		o.t.Fatalf("odb.Open returned error %v", err)
	}
	o.t.Cleanup(func() { _ = db.Close() })
	return db
}

func (o *oracle) writePair(pair corpusPair) {
	o.t.Helper()
	for side, content := range map[string]string{"old": pair.old, "new": pair.new} {
		path := filepath.Join(o.root, filepath.FromSlash(pairPath(side, pair.name)))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			o.t.Fatalf("MkdirAll returned error %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			o.t.Fatalf("WriteFile returned error %v", err)
		}
	}
}

func (o *oracle) noIndex(pair corpusPair, args []string) string {
	o.t.Helper()
	o.writePair(pair)
	full := append([]string{"diff", "--no-index"}, args...)
	full = append(full, pairPath("old", pair.name), pairPath("new", pair.name))
	return o.run(o.root, nil, full...)
}

func (o *oracle) treeDiff(oldTree, newTree hash.ObjectID, args []string) string {
	o.t.Helper()
	full := append([]string{"diff-tree"}, args...)
	full = append(full, oldTree.String(), newTree.String())
	return o.run(o.repo, nil, full...)
}

func TestBlobDiffMatchesGit(t *testing.T) {
	o := newOracle(t)
	for _, pair := range corpus() {
		for _, v := range variants() {
			t.Run(pair.name+"/"+v.name, func(t *testing.T) {
				want := o.noIndex(pair, v.args)
				got := renderPair(t, pair, v)
				if got != want {
					t.Errorf("output differs from git\n--- git ---\n%s\n--- ours ---\n%s", want, got)
				}
			})
		}
	}
}

func TestTreeDiffMatchesGit(t *testing.T) {
	o := newOracle(t)
	db := o.objects()
	for _, pair := range treeCorpus() {
		store := newMemoryStore()
		oldTree, newTree := buildTree(o, pair.old), buildTree(o, pair.new)
		if own := buildTree(store, pair.old); own != oldTree {
			t.Fatalf("%s: our tree id %s differs from git %s", pair.name, own, oldTree)
		}
		if own := buildTree(store, pair.new); own != newTree {
			t.Fatalf("%s: our tree id %s differs from git %s", pair.name, own, newTree)
		}
		if _, err := db.Reload(); err != nil {
			t.Fatalf("Reload returned error %v", err)
		}
		for _, v := range treeVariants() {
			t.Run(pair.name+"/"+v.name, func(t *testing.T) {
				want := o.treeDiff(oldTree, newTree, v.args)
				files, err := Trees(t.Context(), db, oldTree, newTree, v.opts)
				if err != nil {
					t.Fatalf("Trees returned error %v", err)
				}
				got := renderFiles(t, files, v.kind, v.opts)
				if got != want {
					t.Errorf("output differs from git\n--- git ---\n%s\n--- ours ---\n%s", want, got)
				}
			})
		}
	}
}

func TestUpdateFixtures(t *testing.T) {
	if !*updateFixtures {
		t.Skip("run with -update to rewrite the fixtures")
	}
	o := newOracle(t)
	pairsDir := filepath.Join("testdata", "pairs")
	if err := os.MkdirAll(pairsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	for _, pair := range corpus() {
		writeFixture(t, filepath.Join(pairsDir, pair.name+".old"), pair.old)
		writeFixture(t, filepath.Join(pairsDir, pair.name+".new"), pair.new)
	}
	for _, v := range variants() {
		var golden bytes.Buffer
		for _, pair := range corpus() {
			fmt.Fprintf(&golden, "%s%s\n", fixtureSeparator, pair.name)
			golden.WriteString(o.noIndex(pair, v.args))
		}
		writeFixture(t, filepath.Join("testdata", "blobs", v.name+".diff"), golden.String())
	}

	db := o.objects()
	for _, v := range treeVariants() {
		var golden bytes.Buffer
		for _, pair := range treeCorpus() {
			oldTree, newTree := buildTree(o, pair.old), buildTree(o, pair.new)
			fmt.Fprintf(&golden, "%s%s %s %s\n", fixtureSeparator, pair.name, oldTree, newTree)
			golden.WriteString(o.treeDiff(oldTree, newTree, v.args))
		}
		writeFixture(t, filepath.Join("testdata", "trees", v.name+".diff"), golden.String())
	}
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", path, err)
	}
}
