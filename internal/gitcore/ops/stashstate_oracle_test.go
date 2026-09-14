//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stashStateFiles(o *oracle, dir string) string {
	o.t.Helper()
	var out []string
	for _, name := range []string{mergeHeadFile, mergeMsgFile, mergeModeFile, autoMergeFile, squashMsgFile, pickHeadFile, revertFile, "MERGE_RR", origHeadFile} {
		data, err := os.ReadFile(filepath.Join(dir, ".git", name))
		if err != nil {
			out = append(out, name+": <none>")
			continue
		}
		if name == origHeadFile {
			out = append(out, name+": "+string(data))
			continue
		}
		out = append(out, name+": present")
	}
	return strings.Join(out, "\n")
}

func TestOracleStashPushEndsTheOperationInProgressLikeGit(t *testing.T) {
	f := lines("f", 10)
	for name, start := range map[string]func(b *mergeBuilder){
		"a merge without a commit": func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(lines("g", 10), 0, "THEIRS")})(b)
			b.git("merge", "-q", "--no-commit", "feature")
		},
		"a resolved cherry-pick": func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": editLine(f, 4, "THEIRS")})(b)
			_, _ = b.dated().attempt(b.dir, "cherry-pick", "feature")
			b.o.write(b.dir, "f", editLine(f, 4, "RESOLVED"))
			b.o.run(b.dir, "add", "f")
		},
	} {
		t.Run(name, func(t *testing.T) {
			o := newStashOracle(t)
			sides := [2]*mergeBuilder{}
			for i, side := range []string{"git", "ours"} {
				dir := o.repoDir(side)
				newOracleRepo(o, dir)
				o.run(dir, "config", "core.autocrlf", "false")
				o.run(dir, "config", "rerere.enabled", "true")
				sides[i] = &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
				start(sides[i])
				o.write(dir, "keep", "changed\n")
				o.run(dir, "add", "keep")
			}
			gitSide, ourSide := sides[0], sides[1]

			o.run(gitSide.dir, "stash", "push", "-q")
			if _, err := StashPush(t.Context(), o.openRepo(ourSide.dir), StashOptions{When: stashOracleWhen()}); err != nil {
				t.Fatalf("StashPush returned error %v", err)
			}

			if got, want := stashStateFiles(o, ourSide.dir), stashStateFiles(o, gitSide.dir); got != want {
				t.Fatalf("state files differ from git:\n%s\nwant\n%s", got, want)
			}
			for _, dir := range []string{gitSide.dir, ourSide.dir} {
				o.run(dir, "commit", "-q", "--allow-empty", "-m", "next")
			}
			compareStashSides(o, gitSide.dir, ourSide.dir,
				[]string{"rev-parse", "stash", "stash^2"},
				[]string{"status", "--porcelain"},
				[]string{"log", "-1", "--format=%H %P"},
			)
		})
	}
}
