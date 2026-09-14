//go:build oracle

package ops

import (
	"testing"
	"time"
)

func TestOracleAmendKeepsTheAuthorTheWayGitDoes(t *testing.T) {
	o := newOracle(t)
	sides := [2]*mergeBuilder{}
	for i, name := range []string{"git", "ours"} {
		dir := o.repoDir(name)
		newOracleRepo(o, dir)
		o.run(dir, "config", "core.autocrlf", "false")
		sides[i] = &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
		sides[i].commit("base", map[string]string{"f": "f\n"})
		sides[i].write(map[string]string{"g": "g\n"})
		sides[i].git("commit", "-q", "--author", "Someone Else <else@example.com>", "-m", "second")
		sides[i].write(map[string]string{"h": "h\n"})
	}
	gitSide, ourSide := sides[0], sides[1]

	gitSide.git("commit", "-q", "--amend", "-m", "second amended")
	ourSide.dated()
	if _, err := Commit(t.Context(), o.openRepo(ourSide.dir), CommitOptions{Message: "second amended", Amend: true, When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	compareStashSides(o, gitSide.dir, ourSide.dir,
		[]string{"log", "-1", "--format=%H%n%an %ae %ad%n%cn %ce %cd%n%B"},
		[]string{"log", "-g", "-1", "--format=%gs", "HEAD"},
	)
}
