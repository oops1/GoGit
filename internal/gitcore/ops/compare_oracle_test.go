//go:build oracle

package ops

import (
	"strconv"
	"strings"
	"testing"
)

func TestOracleComparingTwoBranchesMatchesGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
	b.git("branch", "feature")
	b.commit("ours one", map[string]string{"f": editLine(f, 0, "OURS")})
	b.commit("ours two", map[string]string{"added": "added\n"})
	b.git("checkout", "-q", "feature")
	b.commit("theirs", map[string]string{"keep": "", "g": "g\n"})
	b.git("checkout", "-q", "main")

	result, err := Compare(t.Context(), o.openRepo(dir), "main", "feature", CompareOptions{})
	if err != nil {
		t.Fatalf("Compare returned error %v", err)
	}

	counts := strings.Fields(o.run(dir, "rev-list", "--left-right", "--count", "main...feature"))
	if len(counts) != 2 {
		t.Fatalf("counts = %v", counts)
	}
	ahead, _ := strconv.Atoi(counts[0])
	behind, _ := strconv.Atoi(counts[1])
	if result.Behind != behind || result.Ahead != ahead {
		t.Fatalf("ahead = %d, behind = %d, git says %d and %d", result.Ahead, result.Behind, ahead, behind)
	}
	if got := strings.TrimSpace(o.run(dir, "merge-base", "main", "feature")); got != result.Base.String() {
		t.Fatalf("base = %s, git says %s", result.Base, got)
	}
	var names []string
	for _, file := range result.Changes {
		names = append(names, file.Status.String()+" "+file.NewPath)
	}
	var want []string
	for line := range strings.SplitSeq(strings.TrimSpace(o.run(dir, "diff", "--name-status", "-M", "main", "feature")), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			want = append(want, fields[0][:1]+" "+fields[len(fields)-1])
		}
	}
	if strings.Join(names, ", ") != strings.Join(want, ", ") {
		t.Fatalf("changes = %v, git says %v", names, want)
	}
}
