//go:build oracle

package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sparseCommandSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	for _, rel := range []string{"a/x.txt", "a/sub/z.txt", "b/k.md", "c/v.md", "c/deep/w.txt", "c/deep/inner/q.txt", "top.md"} {
		o.write(dir, rel, "1\n")
	}
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	return dir
}

func (o *oracle) sparseCommandState(dir string) string {
	o.t.Helper()
	var out strings.Builder
	out.WriteString(o.run(dir, "ls-files", "-t"))
	out.WriteString(o.run(dir, "status", "--porcelain=v2"))
	out.WriteString("patterns:\n")
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "sparse-checkout"))
	switch {
	case os.IsNotExist(err):
		out.WriteString("(none)\n")
	case err != nil:
		o.t.Fatalf("ReadFile returned error %v", err)
	default:
		out.WriteString(string(data))
	}
	out.WriteString("config:\n")
	for _, key := range []string{"core.sparseCheckout", "core.sparseCheckoutCone"} {
		value, _ := o.attempt(dir, "config", "--get", key)
		out.WriteString(key + "=" + strings.TrimSpace(value) + "\n")
	}
	out.WriteString("worktree:\n")
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
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

func (o *oracle) requireSameSparseState(gitSide, ourSide, step string) {
	o.t.Helper()
	if got, want := o.sparseCommandState(ourSide), o.sparseCommandState(gitSide); got != want {
		o.t.Fatalf("%s\nours:\n%s\ngit:\n%s", step, got, want)
	}
}

func gitSparseDisableKeepsThePatternFile(o *oracle) bool {
	dir := o.repoDir("sparse-disable-probe")
	newOracleRepo(o, dir)
	o.write(dir, "probe.txt", "probe\n")
	o.run(dir, "add", "probe.txt")
	o.run(dir, "commit", "-q", "-m", "probe")
	o.run(dir, "sparse-checkout", "set", "--cone", "probe")
	o.run(dir, "sparse-checkout", "disable")
	_, err := os.Stat(filepath.Join(dir, ".git", "info", "sparse-checkout"))
	return err == nil
}

func TestOracleSparseCheckoutCommandsMatchGit(t *testing.T) {
	o := newOracle(t)
	gitSide := sparseCommandSide(o, "git")
	ourSide := sparseCommandSide(o, "ours")

	o.run(gitSide, "sparse-checkout", "init", "--cone")
	if err := SparseCheckoutInit(t.Context(), o.openRepo(ourSide), SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutInit returned error %v", err)
	}
	o.requireSameSparseState(gitSide, ourSide, "after init --cone")

	o.run(gitSide, "sparse-checkout", "set", "a", "c/deep")
	if err := SparseCheckoutSet(t.Context(), o.openRepo(ourSide), []string{"a", "c/deep"}, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	o.requireSameSparseState(gitSide, ourSide, "after set a c/deep")

	o.run(gitSide, "sparse-checkout", "add", "b")
	if err := SparseCheckoutAdd(t.Context(), o.openRepo(ourSide), []string{"b"}, SparseCheckoutOptions{}); err != nil {
		t.Fatalf("SparseCheckoutAdd returned error %v", err)
	}
	o.requireSameSparseState(gitSide, ourSide, "after add b")

	for _, dir := range []string{gitSide, ourSide} {
		o.write(dir, ".git/info/sparse-checkout", "/*\n!/*/\n/c/\n!/c/*/\n/c/deep/\n")
	}
	o.run(gitSide, "sparse-checkout", "reapply")
	if err := SparseCheckoutReapply(t.Context(), o.openRepo(ourSide)); err != nil {
		t.Fatalf("SparseCheckoutReapply returned error %v", err)
	}
	o.requireSameSparseState(gitSide, ourSide, "after reapply")

	o.run(gitSide, "sparse-checkout", "disable")
	if err := SparseCheckoutDisable(t.Context(), o.openRepo(ourSide)); err != nil {
		t.Fatalf("SparseCheckoutDisable returned error %v", err)
	}
	if !gitSparseDisableKeepsThePatternFile(o) {
		t.Log("the installed git removes the pattern file on disable, so the pattern files are left out of the comparison")
		for _, dir := range []string{gitSide, ourSide} {
			_ = os.Remove(filepath.Join(dir, ".git", "info", "sparse-checkout"))
		}
	}
	o.requireSameSparseState(gitSide, ourSide, "after disable")
}

func TestOracleSparseCheckoutListMatchesGit(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		opts     SparseCheckoutOptions
	}{
		{"cone", []string{"a", "c/deep"}, SparseCheckoutOptions{Cone: true}},
		{"noCone", []string{"/a/", "*.md", "!/c/"}, SparseCheckoutOptions{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newOracle(t)
			gitSide := sparseCommandSide(o, "git")
			ourSide := sparseCommandSide(o, "ours")
			args := []string{"sparse-checkout", "set"}
			if !tc.opts.Cone {
				args = append(args, "--no-cone")
			}
			o.run(gitSide, append(args, tc.patterns...)...)
			if err := SparseCheckoutSet(t.Context(), o.openRepo(ourSide), tc.patterns, tc.opts); err != nil {
				t.Fatalf("SparseCheckoutSet returned error %v", err)
			}
			o.requireSameSparseState(gitSide, ourSide, "after set")

			listed, err := SparseCheckoutList(o.openRepo(ourSide))
			if err != nil {
				t.Fatalf("SparseCheckoutList returned error %v", err)
			}
			var theirs []string
			for line := range strings.Lines(o.run(gitSide, "sparse-checkout", "list")) {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					theirs = append(theirs, trimmed)
				}
			}
			if !slices.Equal(listed, theirs) {
				t.Fatalf("SparseCheckoutList = %v, git prints %v", listed, theirs)
			}
		})
	}
}
