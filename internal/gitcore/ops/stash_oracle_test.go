//go:build oracle

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const stashOracleDate = "1700000000 +0300"

func stashOracleWhen() time.Time {
	return time.Unix(1700000000, 0).In(time.FixedZone("", 3*60*60))
}

func newStashOracle(t *testing.T) *oracle {
	o := newOracle(t)
	o.env = append(o.env, "GIT_AUTHOR_DATE="+stashOracleDate, "GIT_COMMITTER_DATE="+stashOracleDate)
	return o
}

func stashSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	for _, file := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		o.write(dir, file, file+"\n")
	}
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "first line\nbody\n\nmore")
	o.write(dir, "a.txt", "a2\n")
	o.write(dir, "b.txt", "b2\n")
	o.run(dir, "add", "b.txt")
	o.write(dir, "new.txt", "new\n")
	o.run(dir, "add", "new.txt")
	o.run(dir, "rm", "-q", "c.txt")
	o.remove(dir, "d.txt")
	o.write(dir, "untracked.txt", "untracked\n")
	return dir
}

func compareStashSides(o *oracle, gitSide, ourSide string, commands ...[]string) {
	o.t.Helper()
	for _, args := range commands {
		want, wantErr := o.attempt(gitSide, args...)
		got, gotErr := o.attempt(ourSide, args...)
		if want != got || (wantErr == nil) != (gotErr == nil) {
			o.t.Fatalf("git %v differs:\ngit:  %q (%v)\nours: %q (%v)", args, want, wantErr, got, gotErr)
		}
	}
}

func TestOracleStashPushMatchesGit(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")

	o.run(gitSide, "stash", "push", "-q")
	if _, err := StashPush(t.Context(), o.openRepo(ourSide), StashOptions{When: stashOracleWhen()}); err != nil {
		t.Fatalf("StashPush returned error %v", err)
	}

	compareStashSides(o, gitSide, ourSide,
		[]string{"rev-parse", "stash", "stash^2"},
		[]string{"stash", "list"},
		[]string{"status", "--porcelain"},
		[]string{"ls-files", "-s"},
		[]string{"log", "-g", "--format=%H %gs", "HEAD"},
		[]string{"log", "-g", "--format=%H %gs", "refs/heads/main"},
		[]string{"log", "-g", "--format=%H %gs", "refs/stash"},
	)
	o.run(ourSide, "fsck", "--strict")
}

func TestOracleStashPushWithMessageOnDetachedHeadMatchesGit(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.run(dir, "checkout", "-q", "--detach")
	}

	o.run(gitSide, "stash", "push", "-q", "-m", "keep this")
	if _, err := StashPush(t.Context(), o.openRepo(ourSide), StashOptions{Message: "keep this", When: stashOracleWhen()}); err != nil {
		t.Fatalf("StashPush returned error %v", err)
	}

	compareStashSides(o, gitSide, ourSide,
		[]string{"rev-parse", "stash", "stash^2"},
		[]string{"stash", "list"},
		[]string{"status", "--porcelain"},
		[]string{"log", "-g", "--format=%H %gs", "HEAD"},
	)
}

func TestOracleStashListMatchesGit(t *testing.T) {
	o := newStashOracle(t)
	dir := stashSide(o, "list")
	o.run(dir, "stash", "push", "-q")
	o.write(dir, "a.txt", "again\n")
	o.run(dir, "stash", "push", "-q", "-m", "second")

	entries, err := StashList(t.Context(), o.openRepo(dir))
	if err != nil {
		t.Fatalf("StashList returned error %v", err)
	}
	var got string
	for _, entry := range entries {
		got += entry.Selector() + ": " + entry.Message + "\n"
	}
	if want := o.run(dir, "stash", "list"); got != want {
		t.Fatalf("StashList = %q, git stash list = %q", got, want)
	}
}

func TestOracleStashApplyMatchesGit(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.run(dir, "stash", "push", "-q")
	}

	o.run(gitSide, "stash", "apply", "-q")
	result, err := StashApply(t.Context(), o.openRepo(ourSide), 0, StashApplyOptions{})
	if err != nil || !result.Clean() || result.Dropped {
		t.Fatalf("StashApply = %+v, %v", result, err)
	}

	compareStashSides(o, gitSide, ourSide,
		[]string{"status", "--porcelain"},
		[]string{"ls-files", "-s"},
		[]string{"stash", "list"},
		[]string{"diff"},
	)
}

func TestOracleStashPopWithConflictKeepsTheEntryLikeGit(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.run(dir, "stash", "push", "-q")
		o.write(dir, "a.txt", "upstream\n")
		o.run(dir, "commit", "-q", "-am", "upstream")
	}

	if _, err := o.attempt(gitSide, "stash", "pop"); err == nil {
		t.Fatal("git stash pop succeeded despite the conflict")
	}
	result, err := StashPop(t.Context(), o.openRepo(ourSide), 0, StashApplyOptions{})
	if err != nil || result.Dropped || len(result.Conflicts) != 1 || result.Conflicts[0] != "a.txt" {
		t.Fatalf("StashPop = %+v, %v", result, err)
	}

	compareStashSides(o, gitSide, ourSide,
		[]string{"status", "--porcelain"},
		[]string{"ls-files", "-s"},
		[]string{"stash", "list"},
	)
	if want, got := o.read(gitSide, "a.txt"), o.read(ourSide, "a.txt"); want != got {
		t.Fatalf("conflicted a.txt = %q, git wrote %q", got, want)
	}
}

func TestOracleStashPopDropsTheEntryLikeGit(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.run(dir, "stash", "push", "-q")
	}

	o.run(gitSide, "stash", "pop", "-q")
	result, err := StashPop(t.Context(), o.openRepo(ourSide), 0, StashApplyOptions{})
	if err != nil || !result.Dropped {
		t.Fatalf("StashPop = %+v, %v", result, err)
	}

	compareStashSides(o, gitSide, ourSide,
		[]string{"status", "--porcelain"},
		[]string{"stash", "list"},
		[]string{"rev-parse", "-q", "--verify", "refs/stash"},
	)
	if _, err := os.Stat(filepath.Join(ourSide, ".git", "logs", "refs", "stash")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stash reflog survived the last drop: %v", err)
	}
}

func TestOracleStashDropRewritesTheReflogLikeGit(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.run(dir, "stash", "push", "-q")
		o.write(dir, "a.txt", "second\n")
		o.run(dir, "stash", "push", "-q", "-m", "second")
		o.write(dir, "b.txt", "third\n")
		o.run(dir, "stash", "push", "-q", "-m", "third")
	}

	o.run(gitSide, "stash", "drop", "-q", "stash@{1}")
	if err := StashDrop(t.Context(), o.openRepo(ourSide), 1); err != nil {
		t.Fatalf("StashDrop returned error %v", err)
	}
	reflog := []string{"log", "-g", "--format=%H %gs", "refs/stash"}
	compareStashSides(o, gitSide, ourSide, []string{"stash", "list"}, []string{"rev-parse", "stash"}, reflog)

	o.run(gitSide, "stash", "drop", "-q", "stash@{0}")
	if err := StashDrop(t.Context(), o.openRepo(ourSide), 0); err != nil {
		t.Fatalf("StashDrop returned error %v", err)
	}
	compareStashSides(o, gitSide, ourSide, []string{"stash", "list"}, []string{"rev-parse", "stash"}, reflog)
	if want, got := stashIDColumns(o, gitSide), stashIDColumns(o, ourSide); want != got {
		t.Fatalf("stash reflog ids = %q, git kept %q", got, want)
	}
}

func stashIDColumns(o *oracle, dir string) string {
	o.t.Helper()
	var columns strings.Builder
	for line := range strings.Lines(o.read(dir, ".git/logs/refs/stash")) {
		columns.WriteString(line[:81] + "\n")
	}
	return columns.String()
}

func TestOracleSwitchMergingCarriesChangesLikeGitStash(t *testing.T) {
	o := newStashOracle(t)
	gitSide := stashSide(o, "git")
	ourSide := stashSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.run(dir, "stash", "push", "-q")
		o.run(dir, "branch", "topic")
		o.run(dir, "checkout", "-q", "topic")
		o.write(dir, "b.txt", "topic\n")
		o.run(dir, "commit", "-q", "-am", "topic")
		o.run(dir, "checkout", "-q", "main")
		o.run(dir, "stash", "pop", "-q")
	}

	o.run(gitSide, "stash", "push", "-q")
	o.run(gitSide, "checkout", "-q", "topic")
	_, popErr := o.attempt(gitSide, "stash", "pop")
	result, err := SwitchMerging(t.Context(), o.openRepo(ourSide), "topic")
	if err != nil || !result.Stashed || (popErr == nil) != result.Clean() {
		t.Fatalf("SwitchMerging = %+v, %v; git pop error %v", result, err, popErr)
	}

	compareStashSides(o, gitSide, ourSide,
		[]string{"symbolic-ref", "HEAD"},
		[]string{"status", "--porcelain"},
		[]string{"ls-files", "-s"},
		[]string{"stash", "list"},
	)
}
