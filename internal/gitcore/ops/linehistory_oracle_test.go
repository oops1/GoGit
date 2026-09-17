//go:build oracle

package ops

import (
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/linelog"
)

func TestOracleLineHistoryMatchesGitLogL(t *testing.T) {
	for _, renames := range []string{"true", "false"} {
		t.Run("diff.renames="+renames, func(t *testing.T) {
			o := newOracle(t)
			dir := o.repoDir("work")
			newOracleRepo(o, dir)
			o.run(dir, "config", "core.autocrlf", "false")
			o.run(dir, "config", "diff.renames", renames)
			b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
			f := lines("f", 12)
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.commit("edit", map[string]string{"f": editLine(f, 4, "EDITED")})
			b.git("branch", "side")
			b.commit("move", map[string]string{"f": "", "moved": editLine(editLine(f, 4, "EDITED"), 5, "MOVED")})
			b.git("checkout", "-q", "side")
			b.commit("side", map[string]string{"keep": "side\n"})
			b.git("checkout", "-q", "main")
			b.git("merge", "-q", "--no-edit", "side")

			want := strings.Fields(b.git("log", "--format=%H", "-s", "-L", "3,8:moved"))
			var got []string
			for entry, err := range LineHistory(t.Context(), o.openRepo(dir), "HEAD", []linelog.Spec{{Range: "3,8", Path: "moved"}}, LineHistoryOptions{}) {
				if err != nil {
					t.Fatalf("LineHistory returned error %v", err)
				}
				got = append(got, entry.Commit.String())
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("line history = %v, git says %v", got, want)
			}
		})
	}
}
