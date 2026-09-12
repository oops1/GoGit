package ops

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func loosePath(r *testRepo, id hash.ObjectID) string {
	text := id.String()
	return filepath.Join(r.repo.ObjectsDir(), text[:2], text[2:])
}

func looseExists(r *testRepo, id hash.ObjectID) bool {
	_, err := os.Stat(loosePath(r, id))
	return err == nil
}

func ageFile(t testing.TB, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
}

func packMaintObjects(t *testing.T, r *testRepo, ids []hash.ObjectID) {
	t.Helper()
	db := r.db()
	var packData bytes.Buffer
	result, err := pack.WritePack(t.Context(), &packData, db, ids, pack.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var indexData bytes.Buffer
	if err := pack.WriteIndex(&indexData, result.Entries, result.Checksum); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.repo.PackDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(r.repo.PackDir(), "pack-"+result.Checksum.String())
	if err := os.WriteFile(name+".pack", packData.Bytes(), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name+".idx", indexData.Bytes(), 0o444); err != nil {
		t.Fatal(err)
	}
}

func emptyFanouts(t *testing.T, r *testRepo) []string {
	t.Helper()
	entries, err := os.ReadDir(r.repo.ObjectsDir())
	if err != nil {
		t.Fatal(err)
	}
	var empty []string
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 2 {
			continue
		}
		inside, err := os.ReadDir(filepath.Join(r.repo.ObjectsDir(), entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if len(inside) == 0 {
			empty = append(empty, entry.Name())
		}
	}
	return empty
}

func TestPruneRemovesOldUnreachableObjectsAndKeepsTheRest(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	old := putMaintBlob(t, r, "old and forgotten\n")
	recent := putMaintBlob(t, r, "just written\n")
	past := time.Now().Add(-48 * time.Hour)
	for _, id := range []hash.ObjectID{old, h.loose, h.blobA, h.first, h.second, h.tag} {
		ageFile(t, loosePath(r, id), past)
	}

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	want := []hash.ObjectID{old, h.loose}
	slices.SortFunc(want, func(a, b hash.ObjectID) int { return a.Compare(b) })
	slices.SortFunc(result.Unreachable, func(a, b hash.ObjectID) int { return a.Compare(b) })
	if !slices.Equal(result.Unreachable, want) || result.UnreachableBytes == 0 {
		t.Fatalf("unreachable = %v (%d bytes), want %v", result.Unreachable, result.UnreachableBytes, want)
	}
	if looseExists(r, old) || looseExists(r, h.loose) {
		t.Fatal("an old unreachable object survived")
	}
	for _, id := range []hash.ObjectID{recent, h.blobA, h.first, h.second, h.tag} {
		if !looseExists(r, id) {
			t.Fatalf("%s was removed", id)
		}
	}
	if empty := emptyFanouts(t, r); len(empty) != 0 {
		t.Fatalf("empty fanouts left: %v", empty)
	}
}

func TestPruneKeepsWhatARecentObjectStillNeeds(t *testing.T) {
	r := newTestRepo(t)
	old := putMaintBlob(t, r, "needed by a tree written a moment ago\n")
	tree := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: old})
	holey := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "g", ID: hash.SumSHA1("blob", []byte("never written"))})
	ageFile(t, loosePath(r, old), time.Now().Add(-48*time.Hour))

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Unreachable) != 0 || !looseExists(r, old) || !looseExists(r, tree) || !looseExists(r, holey) {
		t.Fatalf("result = %+v, old kept = %v", result, looseExists(r, old))
	}
}

func TestPruneKeepsWhatARecentPackStillNeeds(t *testing.T) {
	r := newTestRepo(t)
	blob := putMaintBlob(t, r, "only a packed commit needs me\n")
	tree := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: blob})
	commit := putMaintCommit(t, r, tree)
	packMaintObjects(t, r, []hash.ObjectID{commit})
	past := time.Now().Add(-48 * time.Hour)
	ageFile(t, loosePath(r, blob), past)
	ageFile(t, loosePath(r, tree), past)
	ageFile(t, loosePath(r, commit), past)

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	if !looseExists(r, blob) || !looseExists(r, tree) || looseExists(r, commit) || result.Packed != 1 {
		t.Fatalf("blob = %v, tree = %v, loose commit = %v, result = %+v", looseExists(r, blob), looseExists(r, tree), looseExists(r, commit), result)
	}
}

func TestPruneDropsLooseCopiesOfPackedObjects(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	packed := packAllLoose(t, r)

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	count, err := CountObjects(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Packed != packed || result.PackedBytes == 0 || count.Loose != 0 {
		t.Fatalf("result = %+v, count = %+v", result, count)
	}
}

func TestPruneRemovesOldLeftoversOfInterruptedWrites(t *testing.T) {
	r := newTestRepo(t)
	writeGitFile(t, r.repo.ObjectsDir(), "tmp_obj_old", "12345")
	writeGitFile(t, r.repo.ObjectsDir(), "tmp_obj_new", "1")
	ageFile(t, filepath.Join(r.repo.ObjectsDir(), "tmp_obj_old"), time.Now().Add(-48*time.Hour))

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	if result.Temps != 1 || result.TempBytes != 5 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(r.repo.ObjectsDir(), "tmp_obj_old")); err == nil {
		t.Fatal("the old leftover survived")
	}
	if _, err := os.Stat(filepath.Join(r.repo.ObjectsDir(), "tmp_obj_new")); err != nil {
		t.Fatalf("the new leftover was removed: %v", err)
	}
}

func TestADryRunOnlyTellsWhatWouldGo(t *testing.T) {
	r := newTestRepo(t)
	old := putMaintBlob(t, r, "old\n")
	past := time.Now().Add(-48 * time.Hour)
	ageFile(t, loosePath(r, old), past)
	writeGitFile(t, r.repo.ObjectsDir(), "tmp_obj_old", "1")
	ageFile(t, filepath.Join(r.repo.ObjectsDir(), "tmp_obj_old"), past)

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Unreachable) != 1 || result.Temps != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !looseExists(r, old) {
		t.Fatal("a dry run removed an object")
	}
	if _, err := os.Stat(filepath.Join(r.repo.ObjectsDir(), "tmp_obj_old")); err != nil {
		t.Fatalf("a dry run removed a leftover: %v", err)
	}
}

func TestPruneWithoutAnExpiryTakesEveryUnreachableObject(t *testing.T) {
	r := newTestRepo(t)
	fresh := putMaintBlob(t, r, "fresh but unreachable\n")
	swapMaint(t, &pruneNow, func() time.Time { return time.Now().Add(time.Hour) })

	result, err := Prune(t.Context(), r.repo, PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Unreachable) != 1 || looseExists(r, fresh) {
		t.Fatalf("result = %+v", result)
	}
}

func failingLooseObjects(boom error) func(*odb.DB) iter.Seq2[odb.LooseObject, error] {
	return func(*odb.DB) iter.Seq2[odb.LooseObject, error] {
		return func(yield func(odb.LooseObject, error) bool) { yield(odb.LooseObject{}, boom) }
	}
}

func TestPruneReportsEveryFailure(t *testing.T) {
	boom := errors.New("boom")
	past := time.Now().Add(-48 * time.Hour)
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo, cancel context.CancelFunc)
	}{
		{"opening the database", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"walking the history", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"listing recent loose objects", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &dbLooseObjects, failingLooseObjects(boom))
		}},
		{"walking from recent objects", func(t *testing.T, r *testRepo, cancel context.CancelFunc) {
			swapMaint(t, &dbPackedSince, func(*odb.DB, time.Time) iter.Seq[hash.ObjectID] {
				cancel()
				return func(func(hash.ObjectID) bool) {}
			})
		}},
		{"listing loose objects to remove", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			real, calls := dbLooseObjects, 0
			swapMaint(t, &dbLooseObjects, func(db *odb.DB) iter.Seq2[odb.LooseObject, error] {
				calls++
				if calls == 1 {
					return real(db)
				}
				return failingLooseObjects(boom)(db)
			})
		}},
		{"stopping when cancelled", func(t *testing.T, r *testRepo, cancel context.CancelFunc) {
			real, calls := dbLooseObjects, 0
			swapMaint(t, &dbLooseObjects, func(db *odb.DB) iter.Seq2[odb.LooseObject, error] {
				calls++
				if calls == 2 {
					cancel()
				}
				return real(db)
			})
		}},
		{"checking the packs", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &dbPacked, func(*odb.DB, hash.ObjectID) (bool, error) { return false, boom })
		}},
		{"removing an object", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			ageFile(t, loosePath(r, putMaintBlob(t, r, "old\n")), past)
			swapMaint(t, &dbRemoveLoose, func(*odb.DB, hash.ObjectID) error { return boom })
		}},
		{"listing leftovers", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &dbTempFiles, func(*odb.DB) ([]odb.TempFile, error) { return nil, boom })
		}},
		{"removing a leftover", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			writeGitFile(t, r.repo.ObjectsDir(), "tmp_obj_old", "1")
			ageFile(t, filepath.Join(r.repo.ObjectsDir(), "tmp_obj_old"), past)
			swapMaint(t, &dbRemoveTemp, func(*odb.DB, string) error { return boom })
		}},
		{"removing empty fanouts", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &dbRemoveEmptyFanouts, func(*odb.DB) error { return boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			buildMaintHistory(t, r)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tt.setup(t, r, cancel)

			if _, err := Prune(ctx, r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)}); err == nil {
				t.Fatal("the failure was not reported")
			}
		})
	}
}
