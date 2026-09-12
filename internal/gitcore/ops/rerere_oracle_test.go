//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/rerere"
)

func rerereRepo(t *testing.T, o *oracle, name string, autoupdate bool) *mergeBuilder {
	t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.run(dir, "config", "rerere.enabled", "true")
	if autoupdate {
		o.run(dir, "config", "rerere.autoupdate", "true")
	}
	b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
	f := lines("f", 10)
	forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": editLine(f, 4, "THEIRS")})(b)
	return b
}

const rerereResolution = "RESOLVED BY HAND\n"

func resolveByHand(t *testing.T, b *mergeBuilder) {
	t.Helper()
	b.o.write(b.dir, "f", editLine(lines("f", 10), 4, strings.TrimSuffix(rerereResolution, "\n")))
}

func rerereTree(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, ".git", rerere.CacheDir)
	var out []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, "== "+filepath.ToSlash(rel)+"\n"+string(data))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	merged, readErr := os.ReadFile(filepath.Join(dir, ".git", rerere.MergeRRFile))
	if readErr != nil {
		out = append(out, "== MERGE_RR\n<none>")
	} else {
		out = append(out, "== MERGE_RR\n"+strings.ReplaceAll(string(merged), "\x00", "\\0"))
	}
	return strings.Join(out, "\n")
}

func TestOracleARecordedResolutionIsWrittenTheSameWayGitWritesIt(t *testing.T) {
	o := newOracle(t)
	gitSide := rerereRepo(t, o, "git", false)
	ourSide := rerereRepo(t, o, "ours", false)

	if _, err := gitSide.dated().attempt(gitSide.dir, "merge", "--no-edit", "feature"); err == nil {
		t.Fatal("git merged without a conflict")
	}
	ourSide.dated()
	result, err := Merge(t.Context(), o.openRepo(ourSide.dir), "feature", MergeOptions{When: time.Unix(ourSide.clock, 0).UTC()})
	if err != nil || result.Clean() {
		t.Fatalf("merge = %+v, %v", result, err)
	}

	if got, want := rerereTree(t, ourSide.dir), rerereTree(t, gitSide.dir); got != want {
		t.Fatalf("rr-cache differs from git: %s", sectionDiff(got, want))
	}

	for _, side := range []*mergeBuilder{gitSide, ourSide} {
		resolveByHand(t, side)
		side.git("add", "f")
	}
	gitSide.git("commit", "-q", "--no-edit")
	if _, err := Commit(t.Context(), o.openRepo(ourSide.dir), CommitOptions{Message: "merged", When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if got, want := rerereTree(t, ourSide.dir), rerereTree(t, gitSide.dir); got != want {
		t.Fatalf("after the commit the rr-cache differs from git: %s", sectionDiff(got, want))
	}
}

func redoTheMerge(t *testing.T, b *mergeBuilder, byGit bool) {
	t.Helper()
	b.git("reset", "-q", "--hard", "HEAD~1")
	if byGit {
		_, _ = b.dated().attempt(b.dir, "merge", "--no-edit", "feature")
		return
	}
	b.dated()
	if _, err := Merge(t.Context(), b.o.openRepo(b.dir), "feature", MergeOptions{When: time.Unix(b.clock, 0).UTC()}); err != nil {
		t.Fatalf("Merge returned error %v", err)
	}
}

func TestOracleWeReplayAResolutionGitRecorded(t *testing.T) {
	o := newOracle(t)
	b := rerereRepo(t, o, "shared", false)
	if _, err := b.dated().attempt(b.dir, "merge", "--no-edit", "feature"); err == nil {
		t.Fatal("git merged without a conflict")
	}
	resolveByHand(t, b)
	b.git("add", "f")
	b.git("commit", "-q", "--no-edit")

	redoTheMerge(t, b, false)

	if got := b.o.read(b.dir, "f"); !strings.Contains(got, strings.TrimSuffix(rerereResolution, "\n")) || strings.Contains(got, "<<<<<<<") {
		t.Fatalf("f = %q", got)
	}
}

func TestOracleGitReplaysAResolutionWeRecorded(t *testing.T) {
	o := newOracle(t)
	b := rerereRepo(t, o, "shared", false)
	b.dated()
	if _, err := Merge(t.Context(), o.openRepo(b.dir), "feature", MergeOptions{When: time.Unix(b.clock, 0).UTC()}); err != nil {
		t.Fatalf("Merge returned error %v", err)
	}
	resolveByHand(t, b)
	b.git("add", "f")
	if _, err := Commit(t.Context(), o.openRepo(b.dir), CommitOptions{Message: "merged", When: time.Unix(b.clock, 0).UTC()}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	redoTheMerge(t, b, true)

	if got := b.o.read(b.dir, "f"); !strings.Contains(got, strings.TrimSuffix(rerereResolution, "\n")) || strings.Contains(got, "<<<<<<<") {
		t.Fatalf("f = %q", got)
	}
}

func TestOracleAutoUpdateStagesTheReplayedResolutionAsGitDoes(t *testing.T) {
	o := newOracle(t)
	gitSide := rerereRepo(t, o, "git", true)
	ourSide := rerereRepo(t, o, "ours", true)
	for _, side := range []*mergeBuilder{gitSide, ourSide} {
		_, _ = side.dated().attempt(side.dir, "merge", "--no-edit", "feature")
		resolveByHand(t, side)
		side.git("add", "f")
		side.git("commit", "-q", "--no-edit")
		side.git("reset", "-q", "--hard", "HEAD~1")
	}

	_, _ = gitSide.dated().attempt(gitSide.dir, "merge", "--no-edit", "feature")
	ourSide.dated()
	if _, err := Merge(t.Context(), o.openRepo(ourSide.dir), "feature", MergeOptions{When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
		t.Fatalf("Merge returned error %v", err)
	}

	if got, want := o.run(ourSide.dir, "status", "--porcelain"), o.run(gitSide.dir, "status", "--porcelain"); got != want {
		t.Fatalf("status = %q, git = %q", got, want)
	}
	if got, want := rerereTree(t, ourSide.dir), rerereTree(t, gitSide.dir); got != want {
		t.Fatalf("rr-cache differs from git: %s", sectionDiff(got, want))
	}
}

func TestOracleAbortingOrResettingForgetsTheRememberedPaths(t *testing.T) {
	for _, undo := range []string{"abort", "reset"} {
		t.Run(undo, func(t *testing.T) {
			o := newOracle(t)
			gitSide := rerereRepo(t, o, "git", false)
			ourSide := rerereRepo(t, o, "ours", false)
			if _, err := gitSide.dated().attempt(gitSide.dir, "merge", "--no-edit", "feature"); err == nil {
				t.Fatal("git merged without a conflict")
			}
			ourSide.dated()
			if _, err := Merge(t.Context(), o.openRepo(ourSide.dir), "feature", MergeOptions{When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
				t.Fatalf("Merge returned error %v", err)
			}

			if undo == "abort" {
				gitSide.git("merge", "--abort")
				if err := AbortOperation(t.Context(), o.openRepo(ourSide.dir)); err != nil {
					t.Fatalf("AbortOperation returned error %v", err)
				}
			} else {
				gitSide.git("reset", "-q", "--hard", "HEAD")
				if _, err := Reset(t.Context(), o.openRepo(ourSide.dir), "HEAD", resetOptions(ResetHard)); err != nil {
					t.Fatalf("Reset returned error %v", err)
				}
			}

			if got, want := rerereTree(t, ourSide.dir), rerereTree(t, gitSide.dir); got != want {
				t.Fatalf("rr-cache differs from git: %s", sectionDiff(got, want))
			}
		})
	}
}
