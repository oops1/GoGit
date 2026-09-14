//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildDoomedPackedCommit(o *oracle, dir string) string {
	tree := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD^{tree}"))
	doomed := strings.TrimSpace(o.run(dir, "commit-tree", tree, "-p", "HEAD", "-m", "doomed"))
	o.run(dir, "update-ref", "refs/heads/doomed", doomed)
	o.run(dir, "repack", "-a", "-d", "-q")
	o.run(dir, "update-ref", "-d", "refs/heads/doomed")
	return doomed
}

func TestOracleRepackLeavesOneWholePackAndLoosensWhatNobodyReaches(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	doomed := buildDoomedPackedCommit(o, dir)
	r := o.openRepo(dir)

	result, err := Repack(t.Context(), r, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	o.run(dir, "fsck", "--no-dangling", "--no-progress")
	o.run(dir, "verify-pack", filepath.Join(r.PackDir(), result.Pack+".idx"))
	counts := gitCountObjects(o, dir)
	reachable := len(gitObjectSet(o.run(dir, "rev-list", "--objects", "--all", "--reflog", "--indexed-objects")))
	if counts["packs"] != 1 || counts["in-pack"] != reachable || counts["prune-packable"] != 0 {
		t.Fatalf("counts = %v, reachable = %d, result = %+v", counts, reachable, result)
	}
	if _, err := o.attempt(dir, "cat-file", "-e", doomed); err != nil {
		t.Fatalf("the unreachable commit was lost: %v", err)
	}
}

func TestOracleRepackWithAnExpiryDropsWhatAnOldPackHeldForNobody(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	doomed := buildDoomedPackedCommit(o, dir)
	r := o.openRepo(dir)
	past := time.Now().Add(-48 * time.Hour)
	entries, err := os.ReadDir(r.PackDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Chtimes(filepath.Join(r.PackDir(), entry.Name()), past, past); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Repack(t.Context(), r, RepackOptions{Expire: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	o.run(dir, "fsck", "--no-dangling", "--no-progress")
	if _, err := o.attempt(dir, "cat-file", "-e", doomed); err == nil {
		t.Fatal("the unreachable commit of an old pack survived an expiring repack")
	}
}
