//go:build oracle

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func (o *oracle) putTreeAt(dir, rel string) string {
	o.t.Helper()
	r := o.openRepo(dir)
	db, err := odb.Open(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		o.t.Fatalf("odb.Open returned error %v", err)
	}
	defer func() { _ = db.Close() }()
	blob, err := db.Put(object.TypeBlob, []byte("[core]\n\thooksPath = evil\n"))
	if err != nil {
		o.t.Fatalf("Put returned error %v", err)
	}
	parts := strings.Split(rel, "/")
	id, mode := blob, object.ModeBlob
	for at := len(parts) - 1; at >= 0; at-- {
		tree := &object.Tree{Entries: []object.TreeEntry{{Mode: mode, Name: parts[at], ID: id}}}
		var next hash.ObjectID
		if next, err = db.PutObject(tree); err != nil {
			o.t.Fatalf("PutObject returned error %v", err)
		}
		id, mode = next, object.ModeTree
	}
	return id.String()
}

func checkoutSafetySide(o *oracle, name, rel string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.protectHFS", "false")
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	tree := o.putTreeAt(dir, rel)
	commit := strings.TrimSpace(o.run(dir, "commit-tree", tree, "-p", "HEAD", "-m", "unsafe"))
	o.run(dir, "branch", "unsafe", commit)
	return dir
}

func TestOracleCheckoutRefusesTheSamePathsAsGit(t *testing.T) {
	for _, rel := range []string{".GIT/config", "git~1/config", ".git. /config", "sub/.git/config", ".g‌it/config", "dir/aux.txt", "dir/nul"} {
		t.Run(rel, func(t *testing.T) {
			o := newOracle(t)
			gitSide := checkoutSafetySide(o, "git", rel)
			ourSide := checkoutSafetySide(o, "ours", rel)
			configBefore := o.read(ourSide, ".git/config")

			_, gitErr := o.attempt(gitSide, "checkout", "-q", "unsafe")
			ourErr := Switch(t.Context(), o.openRepo(ourSide), "unsafe", SwitchOptions{})

			if (gitErr == nil) != (ourErr == nil) {
				t.Fatalf("git: %v\nours: %v", gitErr, ourErr)
			}
			if ourErr != nil && !errors.Is(ourErr, ErrUnsafePath) {
				t.Fatalf("ours: %v, want ErrUnsafePath", ourErr)
			}
			if o.read(ourSide, ".git/config") != configBefore {
				t.Fatal("the git config was rewritten")
			}
			if got, want := o.run(ourSide, "status", "--porcelain"), o.run(gitSide, "status", "--porcelain"); got != want {
				t.Fatalf("status\nours:\n%s\ngit:\n%s", got, want)
			}
		})
	}
}

func submoduleSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	head := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))
	o.run(dir, "checkout", "-q", "-b", "withsub")
	o.run(dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",sub")
	o.run(dir, "commit", "-q", "-m", "submodule")
	o.write(dir, "sub/file", "inside\n")
	return dir
}

func TestOracleSwitchKeepsAPopulatedSubmoduleDirectoryLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := submoduleSide(o, "git")
	ourSide := submoduleSide(o, "ours")

	o.run(gitSide, "checkout", "-q", "main")
	if err := Switch(t.Context(), o.openRepo(ourSide), "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}

	state := func(dir string) string {
		return o.run(dir, "status", "--porcelain", "--untracked-files=all") + o.run(dir, "ls-files", "-s") + o.read(dir, "sub/file")
	}
	if got, want := state(ourSide), state(gitSide); got != want {
		t.Fatalf("after the switch\nours:\n%s\ngit:\n%s", got, want)
	}
}

func leadingFileSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "a.txt", "one\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.write(dir, "a.txt", "two\n")
	o.write(dir, "d/x", "x\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.write(dir, "d", "untracked\n")
	return dir
}

func TestOracleSwitchRefusesAnUntrackedFileInTheLeadingPathLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := leadingFileSide(o, "git")
	ourSide := leadingFileSide(o, "ours")

	_, gitErr := o.attempt(gitSide, "checkout", "-q", "topic")
	ourErr := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{})
	if gitErr == nil || !strings.Contains(gitErr.Error(), "untracked working tree files would be overwritten") {
		t.Fatalf("git: %v", gitErr)
	}
	var overwrite *OverwriteError
	if !errors.As(ourErr, &overwrite) || !slices.Equal(overwrite.Paths, []string{"d"}) {
		t.Fatalf("ours: %v", ourErr)
	}
	state := func(dir string) string {
		return o.run(dir, "status", "--porcelain") + o.run(dir, "symbolic-ref", "HEAD") + o.read(dir, "a.txt") + o.read(dir, "d")
	}
	if got, want := state(ourSide), state(gitSide); got != want {
		t.Fatalf("after the refused switch\nours:\n%s\ngit:\n%s", got, want)
	}
}

func sparseSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "a/y", "one\n")
	o.write(dir, "b/x", "one\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.write(dir, "a/y", "two\n")
	o.write(dir, "b/x", "two\n")
	o.run(dir, "commit", "-q", "-am", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.run(dir, "sparse-checkout", "set", "--no-cone", "/a/")
	return dir
}

func TestOracleSwitchKeepsSparseEntriesLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := sparseSide(o, "git")
	ourSide := sparseSide(o, "ours")

	o.run(gitSide, "checkout", "-q", "topic")
	if err := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := Stage(t.Context(), o.openRepo(ourSide), []string{"b"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}

	state := func(dir string) string {
		_, err := os.Lstat(filepath.Join(dir, "b", "x"))
		return o.run(dir, "status", "--porcelain") + o.run(dir, "ls-files", "-t", "-s") + o.read(dir, "a/y") + strings.Repeat("present", btoi(err == nil))
	}
	if got, want := state(ourSide), state(gitSide); got != want {
		t.Fatalf("after the switch\nours:\n%s\ngit:\n%s", got, want)
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func caseRenameSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.write(dir, "readme", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.run(dir, "mv", "readme", "README")
	o.run(dir, "commit", "-q", "-m", "rename")
	o.run(dir, "checkout", "-q", "main")
	return dir
}

func TestOracleSwitchRenamesByCaseLikeGit(t *testing.T) {
	if !caseInsensitiveFileSystem(t) {
		t.Skip("the file system is case-sensitive")
	}
	o := newOracle(t)
	gitSide := caseRenameSide(o, "git")
	ourSide := caseRenameSide(o, "ours")

	o.run(gitSide, "checkout", "-q", "topic")
	if err := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	state := func(dir string) string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir returned error %v", err)
		}
		out := o.run(dir, "status", "--porcelain") + o.run(dir, "ls-files")
		for _, entry := range entries {
			out += entry.Name() + "\n"
		}
		return out
	}
	if got, want := state(ourSide), state(gitSide); got != want {
		t.Fatalf("after the switch\nours:\n%s\ngit:\n%s", got, want)
	}
}
