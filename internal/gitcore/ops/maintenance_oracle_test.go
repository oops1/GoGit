//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
)

func buildOracleMaintenanceRepo(o *oracle) (string, string) {
	dir := o.repoDir("work")
	worktree := filepath.Join(o.t.TempDir(), "side")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "a\n")
	o.run(dir, "add", "a.txt")
	o.run(dir, "commit", "-q", "-m", "a")
	o.write(dir, "dir/b.txt", "b\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "b")
	o.run(dir, "tag", "-a", "-m", "v1", "v1")
	o.run(dir, "switch", "-q", "-c", "feature")
	o.write(dir, "c.txt", "c\n")
	o.run(dir, "add", "c.txt")
	o.run(dir, "commit", "-q", "-m", "c")
	o.write(dir, "c.txt", "c amended\n")
	o.run(dir, "commit", "-q", "-a", "--amend", "-m", "c amended")
	o.run(dir, "switch", "-q", "main")
	o.write(dir, "staged.txt", "only in the index\n")
	o.run(dir, "add", "staged.txt")
	o.write(dir, "dangling.txt", "nobody keeps this\n")
	o.run(dir, "hash-object", "-w", "dangling.txt")
	o.remove(dir, "dangling.txt")
	o.run(dir, "worktree", "add", "-q", "-b", "side", worktree)
	o.write(worktree, "side.txt", "side\n")
	o.run(worktree, "add", "side.txt")
	o.run(worktree, "commit", "-q", "-m", "side")
	o.write(worktree, "side-staged.txt", "staged in the linked worktree\n")
	o.run(worktree, "add", "side-staged.txt")
	return dir, worktree
}

func gitObjectSet(out string) []string {
	var ids []string
	for line := range strings.Lines(out) {
		if id, _, _ := strings.Cut(strings.TrimSpace(line), " "); id != "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func TestOracleOurReachableObjectsAreTheOnesGitKeeps(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	r := o.openRepo(dir)
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	walk, err := reachableObjects(t.Context(), r, db)
	if err != nil {
		t.Fatal(err)
	}

	var ours []string
	for id := range walk.seen {
		ours = append(ours, id.String())
	}
	slices.Sort(ours)
	theirs := gitObjectSet(o.run(dir, "rev-list", "--objects", "--all", "--reflog", "--indexed-objects"))
	if !slices.Equal(ours, theirs) {
		t.Fatalf("reachable objects differ:\nours   %v\ntheirs %v", ours, theirs)
	}
}

func gitCountObjects(o *oracle, dir string) map[string]int {
	counts := make(map[string]int)
	for line := range strings.Lines(o.run(dir, "count-objects", "-v")) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil {
			o.t.Fatalf("count-objects %s = %q", key, value)
		}
		counts[key] = number
	}
	return counts
}

func TestOracleCountingObjectsAgreesWithGit(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)

	for _, stage := range []string{"loose", "packed without deleting"} {
		if stage != "loose" {
			o.run(dir, "repack", "-a", "-q")
		}
		r := o.openRepo(dir)
		ours, err := CountObjects(r)
		if err != nil {
			t.Fatal(err)
		}
		theirs := gitCountObjects(o, dir)
		got := map[string]int{"count": ours.Loose, "in-pack": ours.InPack, "packs": ours.Packs, "prune-packable": ours.PrunePackable}
		for key, value := range got {
			if theirs[key] != value {
				t.Fatalf("%s: %s = %d, git says %d", stage, key, value, theirs[key])
			}
		}
	}
}

func TestOracleGitVerifiesTheCommitGraphWeWriteForARealRepository(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	r := o.openRepo(dir)

	written, err := WriteCommitGraph(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}

	o.run(dir, "commit-graph", "verify")
	commits, err := strconv.Atoi(strings.TrimSpace(o.run(dir, "rev-list", "--all", "--reflog", "--count")))
	if err != nil {
		t.Fatal(err)
	}
	if written != commits {
		t.Fatalf("written = %d, git counts %d commits", written, commits)
	}
	if _, err := os.Stat(filepath.Join(r.ObjectsDir(), "info", "commit-graph")); err != nil {
		t.Fatal(err)
	}
}
