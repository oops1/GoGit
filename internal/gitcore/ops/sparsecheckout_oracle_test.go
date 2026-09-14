//go:build oracle

package ops

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func sparseCheckoutSide(o *oracle, name string, setArgs []string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	for _, rel := range []string{"a/x.txt", "a/y.md", "a/sub/z.txt", "b/sub/z.txt", "b/k.md", "top.md", "c/deep/w.txt", "c/v.md", "gone.md"} {
		o.write(dir, rel, "1\n")
	}
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.run(dir, "checkout", "-q", "-b", "topic")
	for _, rel := range []string{"a/x.txt", "a/y.md", "b/sub/z.txt", "top.md", "c/v.md", "b/sub/new.txt", "new.md", "c/deep/n.txt", "d/fresh.txt"} {
		o.write(dir, rel, "2\n")
	}
	o.run(dir, "rm", "-q", "gone.md")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.run(dir, append([]string{"sparse-checkout", "set"}, setArgs...)...)
	return dir
}

func (o *oracle) sparseCheckoutState(dir string) string {
	o.t.Helper()
	var out strings.Builder
	out.WriteString(o.run(dir, "status", "--porcelain=v2"))
	out.WriteString(o.run(dir, "ls-files", "-t", "-s"))
	out.WriteString(o.run(dir, "-c", "sparse.expectFilesOutsideOfPatterns=true", "ls-files", "-t"))
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case rel == ".git":
			return filepath.SkipDir
		case entry.IsDir():
			out.WriteString(rel + "/\n")
		default:
			out.WriteString(rel + "=" + o.read(dir, rel))
		}
		return nil
	})
	if err != nil {
		o.t.Fatalf("WalkDir returned error %v", err)
	}
	return out.String()
}

func (o *oracle) switchSparseSides(gitSide, ourSide, target, step string) {
	o.t.Helper()
	_, gitErr := o.attempt(gitSide, "checkout", "-q", target)
	ourErr := Switch(o.t.Context(), o.openRepo(ourSide), target, SwitchOptions{})
	if (gitErr == nil) != (ourErr == nil) {
		o.t.Fatalf("%s: git returned %v, we returned %v", step, gitErr, ourErr)
	}
	if got, want := o.sparseCheckoutState(ourSide), o.sparseCheckoutState(gitSide); got != want {
		o.t.Fatalf("%s\nours:\n%s\ngit:\n%s", step, got, want)
	}
}

func TestOracleSwitchFollowsSparseCheckoutPatternsLikeGit(t *testing.T) {
	tests := []struct {
		name      string
		setArgs   []string
		local     map[string]string
		narrowed  string
		dirtyPath string
	}{
		{"cone", []string{"a", "c/deep"}, map[string]string{"b/k.md": "1\n", "d/fresh.txt": "untracked\n"}, "/*\n!/*/\n/b/\n", "a/sub/z.txt"},
		{"noCone", []string{"--no-cone", "*.txt", "!sub/", "b/sub/"}, map[string]string{"top.md": "1\n"}, "/b/\n*.md\n", "a/sub/z.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newOracle(t)
			gitSide := sparseCheckoutSide(o, "git", tc.setArgs)
			ourSide := sparseCheckoutSide(o, "ours", tc.setArgs)
			for _, dir := range []string{gitSide, ourSide} {
				for rel, content := range tc.local {
					o.write(dir, rel, content)
				}
			}
			o.switchSparseSides(gitSide, ourSide, "topic", "after switching to topic")
			o.switchSparseSides(gitSide, ourSide, "main", "after switching back to main")
			for _, dir := range []string{gitSide, ourSide} {
				o.write(dir, ".git/info/sparse-checkout", tc.narrowed)
				o.write(dir, tc.dirtyPath, "dirty\n")
			}
			o.switchSparseSides(gitSide, ourSide, "topic", "after changing the patterns")
		})
	}
}
