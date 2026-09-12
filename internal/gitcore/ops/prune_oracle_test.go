//go:build oracle

package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func ageEveryLooseObject(t *testing.T, objects string, when time.Time) {
	t.Helper()
	err := filepath.WalkDir(objects, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || len(filepath.Base(filepath.Dir(path))) != 2 {
			return nil
		}
		return os.Chtimes(path, when, when)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOraclePruneTakesExactlyWhatGitPruneWould(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	o.write(dir, "old.txt", "old and dangling\n")
	o.run(dir, "hash-object", "-w", "old.txt")
	o.remove(dir, "old.txt")
	o.write(dir, "needed.txt", "old but needed by a fresh tree\n")
	needed := strings.TrimSpace(o.run(dir, "hash-object", "-w", "needed.txt"))
	o.remove(dir, "needed.txt")
	r := o.openRepo(dir)
	ageEveryLooseObject(t, r.ObjectsDir(), time.Now().Add(-24*time.Hour))
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	neededID, err := hash.Parse(needed)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := db.PutObject(&object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "needed.txt", ID: neededID}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	expire := time.Now().Add(-12 * time.Hour)

	dry, err := Prune(t.Context(), r, PruneOptions{Expire: expire, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	var ours []string
	for _, id := range dry.Unreachable {
		ours = append(ours, id.String())
	}
	slices.Sort(ours)
	theirs := gitObjectSet(o.run(dir, "prune", "-n", "--expire=12.hours.ago"))
	if len(ours) == 0 || !slices.Equal(ours, theirs) {
		t.Fatalf("pruned objects differ:\nours   %v\ntheirs %v", ours, theirs)
	}
	if slices.Contains(ours, needed) || slices.Contains(ours, fresh.String()) {
		t.Fatalf("a fresh tree or what it needs was pruned: %v", ours)
	}

	if _, err := Prune(t.Context(), r, PruneOptions{Expire: expire}); err != nil {
		t.Fatal(err)
	}
	if left := strings.TrimSpace(o.run(dir, "prune", "-n", "--expire=12.hours.ago")); left != "" {
		t.Fatalf("git would still prune %q", left)
	}
	o.run(dir, "fsck", "--no-dangling", "--no-progress")
}
