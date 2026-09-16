//go:build oracle

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func bisectSide(o *oracle, name string, detached bool) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	for i := range 5 {
		o.write(dir, "f", strconv.Itoa(i)+"\n")
		o.run(dir, "add", "f")
		o.run(dir, "commit", "-q", "-m", "c"+strconv.Itoa(i))
	}
	if detached {
		o.run(dir, "checkout", "-q", "HEAD~1")
	}
	o.run(dir, "bisect", "start")
	o.run(dir, "bisect", "bad")
	o.run(dir, "bisect", "good", "HEAD~3")
	return dir
}

func bisectFilesOf(o *oracle, dir string) string {
	o.t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".git"))
	if err != nil {
		o.t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "BISECT_") {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return strings.Join(names, " ")
}

func bisectStateOf(o *oracle, dir string) string {
	o.t.Helper()
	branch, _ := o.attempt(dir, "symbolic-ref", "-q", "HEAD")
	return "== status\n" + o.run(dir, "status", "--porcelain=v2", "--branch") +
		"== branch\n" + branch +
		"== refs\n" + o.run(dir, "for-each-ref") +
		"== files\n" + bisectFilesOf(o, dir)
}

func TestOracleBisectStartedByGitIsSeenAndEndedLikeGitBisectReset(t *testing.T) {
	for _, detached := range []bool{false, true} {
		t.Run("detached="+strconv.FormatBool(detached), func(t *testing.T) {
			o := datedOracle(newOracle(t))
			gitSide, ourSide := bisectSide(o, "git", detached), bisectSide(o, "ours", detached)
			r := o.openRepo(ourSide)

			state, err := ReadMergeState(r)
			wantStart := strings.TrimSpace(o.read(ourSide, ".git/BISECT_START"))
			if err != nil || !state.Bisecting || state.BisectStart != wantStart || state.InProgress() {
				t.Fatalf("state = %+v, %v; want a bisect from %s", state, err, wantStart)
			}

			if !detached {
				if _, gitErr := o.attempt(gitSide, "branch", "-D", "main"); gitErr == nil {
					t.Fatal("git deleted the bisected branch")
				}
				if err := DeleteBranch(t.Context(), r, "main", true); !errors.Is(err, ErrBranchCheckedOut) {
					t.Fatalf("DeleteBranch = %v", err)
				}
				if _, gitErr := o.attempt(gitSide, "branch", "-m", "main", "renamed"); gitErr == nil {
					t.Fatal("git renamed the bisected branch")
				}
				if err := RenameBranch(t.Context(), r, "main", "renamed", false); !errors.Is(err, ErrBranchBisected) {
					t.Fatalf("RenameBranch = %v", err)
				}
			}

			o.run(gitSide, "bisect", "reset")
			if err := ResetBisect(t.Context(), r); err != nil {
				t.Fatalf("ResetBisect returned error %v", err)
			}

			if got, want := bisectStateOf(o, ourSide), bisectStateOf(o, gitSide); got != want {
				t.Fatalf("after the reset\nours:\n%s\ngit:\n%s", got, want)
			}
		})
	}
}
