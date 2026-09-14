package ops

import (
	"bytes"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func packAllLoose(t *testing.T, r *testRepo) int {
	t.Helper()
	db := r.db()
	var ids []hash.ObjectID
	for id, err := range db.Loose() {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	var packData bytes.Buffer
	result, err := pack.WritePack(t.Context(), &packData, db, ids, pack.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var indexData bytes.Buffer
	if err := pack.WriteIndex(&indexData, result.Entries, result.Checksum); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(r.repo.PackDir(), "pack-"+result.Checksum.String())
	if err := os.MkdirAll(r.repo.PackDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name+".pack", packData.Bytes(), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name+".idx", indexData.Bytes(), 0o444); err != nil {
		t.Fatal(err)
	}
	return len(ids)
}

func TestCountingObjectsSeesLooseObjectsPacksAndLeftovers(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	writeGitFile(t, r.repo.ObjectsDir(), "tmp_obj_interrupted", "1234")

	loose, err := CountObjects(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if loose.Loose == 0 || loose.LooseBytes == 0 || loose.Packs != 0 || loose.Garbage != 1 || loose.GarbageBytes != 4 {
		t.Fatalf("before packing = %+v", loose)
	}

	packed := packAllLoose(t, r)
	count, err := CountObjects(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if count.Packs != 1 || count.InPack != packed || count.PrunePackable != loose.Loose || count.PackBytes == 0 {
		t.Fatalf("after packing = %+v, packed %d", count, packed)
	}
}

func TestCountingObjectsReportsEveryFailure(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"opening the database", func(t *testing.T) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"listing packs", func(t *testing.T) {
			swapMaint(t, &dbPacks, func(*odb.DB) ([]odb.PackInfo, error) { return nil, boom })
		}},
		{"listing loose objects", func(t *testing.T) {
			swapMaint(t, &dbLooseObjects, func(*odb.DB) iter.Seq2[odb.LooseObject, error] {
				return func(yield func(odb.LooseObject, error) bool) { yield(odb.LooseObject{}, boom) }
			})
		}},
		{"checking the packs for a loose object", func(t *testing.T) {
			swapMaint(t, &dbPacked, func(*odb.DB, hash.ObjectID) (bool, error) { return false, boom })
		}},
		{"listing leftovers", func(t *testing.T) {
			swapMaint(t, &dbTempFiles, func(*odb.DB) ([]odb.TempFile, error) { return nil, boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			buildMaintHistory(t, r)
			tt.setup(t)

			if _, err := CountObjects(r.repo); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want boom", err)
			}
		})
	}
}
