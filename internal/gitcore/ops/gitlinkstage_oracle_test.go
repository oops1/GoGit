//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func datedOracle(o *oracle) *oracle {
	env := append(slices.Clone(o.env),
		"GIT_AUTHOR_NAME=oracle", "GIT_AUTHOR_EMAIL=oracle@example.com", "GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=oracle", "GIT_COMMITTER_EMAIL=oracle@example.com", "GIT_COMMITTER_DATE=1700000000 +0000")
	return &oracle{t: o.t, home: o.home, env: env}
}

func (o *oracle) nestedRepository(dir string, files ...string) {
	o.t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		o.t.Fatal(err)
	}
	o.run(dir, "init", "-q", "-b", "main", ".")
	for _, name := range files {
		o.write(dir, name, name+"\n")
		o.run(dir, "add", name)
		o.run(dir, "commit", "-q", "-m", name)
	}
}

func TestOracleStagingSubmodulesMatchesGitAdd(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		setup func(o *oracle, dir string)
	}{
		{"moved submodule", "sub", func(o *oracle, dir string) {
			o.nestedRepository(filepath.Join(dir, "sub"), "a", "b")
			o.run(dir, "add", "sub")
			o.run(dir, "commit", "-q", "-m", "superproject")
			o.run(filepath.Join(dir, "sub"), "checkout", "-q", "HEAD~1")
		}},
		{"parent of an uninitialised submodule", "libs", func(o *oracle, dir string) {
			o.write(dir, "top.txt", "top\n")
			o.run(dir, "add", ".")
			o.commitGitlink(dir, "libs/sub", firstGitlinkID, "superproject")
			if err := os.MkdirAll(filepath.Join(dir, "libs", "sub"), 0o777); err != nil {
				o.t.Fatal(err)
			}
			o.write(dir, "libs/x.txt", "x\n")
		}},
		{"parent of a missing submodule", "libs", func(o *oracle, dir string) {
			o.write(dir, "top.txt", "top\n")
			o.run(dir, "add", ".")
			o.commitGitlink(dir, "libs/sub", firstGitlinkID, "superproject")
			o.write(dir, "libs/x.txt", "x\n")
		}},
		{"untracked nested repository", "vendor", func(o *oracle, dir string) {
			o.nestedRepository(filepath.Join(dir, "vendor", "lib"), "a")
			o.write(dir, "vendor/own.txt", "own\n")
		}},
		{"nested repository over tracked files", "vendor", func(o *oracle, dir string) {
			o.write(dir, "vendor/lib/x.txt", "x\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "tracked")
			o.nestedRepository(filepath.Join(dir, "vendor", "lib"), "y.txt")
			o.write(dir, "vendor/lib/x.txt", "changed\n")
		}},
		{"nested repository without a commit", "vendor", func(o *oracle, dir string) {
			o.nestedRepository(filepath.Join(dir, "vendor", "lib"))
			o.write(dir, "vendor/lib/x.txt", "x\n")
			o.write(dir, "vendor/own.txt", "own\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := datedOracle(newOracle(t))
			sides := map[string]string{}
			for _, name := range []string{"git", "ours"} {
				sides[name] = o.repoDir(name)
				newOracleRepo(o, sides[name])
				tc.setup(o, sides[name])
			}

			_, gitErr := o.attempt(sides["git"], "add", tc.path)
			ourErr := Stage(t.Context(), o.openRepo(sides["ours"]), []string{tc.path}, StageOptions{})

			if (gitErr != nil) != (ourErr != nil) {
				t.Fatalf("git add: %v, Stage: %v", gitErr, ourErr)
			}
			if got, want := o.run(sides["ours"], "ls-files", "-s"), o.run(sides["git"], "ls-files", "-s"); got != want {
				t.Fatalf("index\nours:\n%s\ngit:\n%s", got, want)
			}
		})
	}
}
