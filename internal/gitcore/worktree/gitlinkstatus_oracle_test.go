//go:build oracle

package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func (o *oracle) submoduleSuperproject(dir string) string {
	o.t.Helper()
	o.write(dir, "top.txt", "top\n")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o777); err != nil {
		o.t.Fatal(err)
	}
	o.run(sub, "init", "-q", "-b", "main", ".")
	o.run(sub, "config", "core.autocrlf", "false")
	o.write(sub, "a.txt", "a\n")
	o.run(sub, "add", ".")
	o.run(sub, "commit", "-q", "-m", "one")
	o.write(sub, "b.txt", "b\n")
	o.run(sub, "add", ".")
	o.run(sub, "commit", "-q", "-m", "two")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "superproject")
	return sub
}

func TestOracleSubmoduleStatusMatchesGitStatusPorcelainV2(t *testing.T) {
	tests := []struct {
		name  string
		setup func(o *oracle, dir, sub string)
	}{
		{"clean submodule", func(*oracle, string, string) {}},
		{"checked-out commit moved", func(o *oracle, _, sub string) {
			o.run(sub, "checkout", "-q", "HEAD~1")
		}},
		{"modified content", func(o *oracle, _, sub string) {
			o.write(sub, "a.txt", "changed\n")
		}},
		{"untracked content", func(o *oracle, _, sub string) {
			o.write(sub, "new.txt", "new\n")
		}},
		{"staged content", func(o *oracle, _, sub string) {
			o.write(sub, "new.txt", "new\n")
			o.run(sub, "add", "new.txt")
		}},
		{"moved with untracked content", func(o *oracle, _, sub string) {
			o.run(sub, "checkout", "-q", "HEAD~1")
			o.write(sub, "new.txt", "new\n")
		}},
		{"moved and staged in the superproject", func(o *oracle, dir, sub string) {
			o.run(sub, "checkout", "-q", "HEAD~1")
			o.run(dir, "add", "sub")
		}},
		{"missing directory", func(o *oracle, _, sub string) {
			if err := os.RemoveAll(sub); err != nil {
				o.t.Fatal(err)
			}
		}},
		{"uninitialised directory", func(o *oracle, _, sub string) {
			if err := os.RemoveAll(sub); err != nil {
				o.t.Fatal(err)
			}
			if err := os.Mkdir(sub, 0o777); err != nil {
				o.t.Fatal(err)
			}
		}},
		{"nested submodule with untracked content", func(o *oracle, _, sub string) {
			inner := o.submoduleSuperproject(sub)
			o.write(inner, "loose.txt", "loose\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newOracle(t)
			dir := o.repoDir("work")
			o.run(dir, "init", "-q", "-b", "main", ".")
			o.run(dir, "config", "core.autocrlf", "false")
			sub := o.submoduleSuperproject(dir)
			tc.setup(o, dir, sub)
			o.compare(dir)
		})
	}
}
