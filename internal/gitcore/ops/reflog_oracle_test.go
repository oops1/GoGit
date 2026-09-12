//go:build oracle

package ops

import (
	"strings"
	"testing"
)

func TestOracleTheReflogWeReadIsTheOneGitShows(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f})
	b.git("checkout", "-q", "-b", "feature")
	b.commit("feature work", map[string]string{"f": editLine(f, 2, "FEATURE")})
	b.git("checkout", "-q", "main")
	b.dated().run(b.dir, "merge", "--no-edit", "feature")
	b.git("reset", "-q", "--hard", "HEAD~1")

	for _, name := range []string{"HEAD", "main"} {
		t.Run(name, func(t *testing.T) {
			records, err := Reflog(t.Context(), o.openRepo(dir), name, ReflogOptions{})
			if err != nil {
				t.Fatalf("Reflog returned error %v", err)
			}
			var got []string
			for _, record := range records {
				got = append(got, record.New.String()+" "+record.Message)
			}
			var want []string
			for line := range strings.SplitSeq(strings.TrimSuffix(o.run(dir, "reflog", "--format=%H %gs", name), "\n"), "\n") {
				if line != "" {
					want = append(want, line)
				}
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("reflog =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if records[0].Selector() != name+"@{0}" && name != "HEAD" {
				t.Fatalf("selector = %q", records[0].Selector())
			}
		})
	}
}
