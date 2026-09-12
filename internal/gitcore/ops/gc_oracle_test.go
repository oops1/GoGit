//go:build oracle

package ops

import (
	"strings"
	"testing"
	"time"
)

func TestOracleGCLeavesARepositoryGitFindsTidyAndWhole(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	r := o.openRepo(dir)
	ageEveryLooseObject(t, r.ObjectsDir(), time.Now().AddDate(0, 0, -30))

	result, err := GC(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}

	o.run(dir, "fsck", "--no-dangling", "--no-progress")
	o.run(dir, "commit-graph", "verify")
	counts := gitCountObjects(o, dir)
	if counts["packs"] != 1 || counts["count"] != 0 {
		t.Fatalf("counts = %v, result = %+v", counts, result)
	}
	if left := strings.TrimSpace(o.run(dir, "prune", "-n")); left != "" {
		t.Fatalf("git would still prune %q", left)
	}
	if packed := o.read(dir, ".git/packed-refs"); !strings.Contains(packed, "refs/heads/main") || !strings.Contains(packed, "refs/tags/v1") {
		t.Fatalf("packed-refs = %q", packed)
	}
	reachable := len(gitObjectSet(o.run(dir, "rev-list", "--objects", "--all", "--reflog", "--indexed-objects")))
	if counts["in-pack"] != reachable {
		t.Fatalf("in-pack = %d, reachable = %d", counts["in-pack"], reachable)
	}
}
