package ops

import (
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

func packNames(t *testing.T, r *testRepo) []string {
	t.Helper()
	entries, err := os.ReadDir(r.repo.PackDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".pack" {
			names = append(names, entry.Name()[:len(entry.Name())-len(".pack")])
		}
	}
	slices.Sort(names)
	return names
}

func removeLooseNow(t *testing.T, r *testRepo, id hash.ObjectID) {
	t.Helper()
	db, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.RemoveLoose(id); err != nil {
		t.Fatal(err)
	}
}

func agePack(t *testing.T, r *testRepo, name string, when time.Time) {
	t.Helper()
	for _, suffix := range []string{".pack", ".idx"} {
		ageFile(t, filepath.Join(r.repo.PackDir(), name+suffix), when)
	}
}

func onlyPack(t *testing.T, r *testRepo) string {
	t.Helper()
	names := packNames(t, r)
	if len(names) != 1 {
		t.Fatalf("packs = %v, want exactly one", names)
	}
	return names[0]
}

func TestRepackPutsEveryReachableObjectIntoOneNewPack(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	reachable := len(mustWalk(t, r).seen)

	result, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	count, err := CountObjects(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Objects != reachable || result.Pack != onlyPack(t, r) || result.Unpacked != reachable || result.Bytes == 0 {
		t.Fatalf("result = %+v, reachable = %d", result, reachable)
	}
	if count.Packs != 1 || count.InPack != reachable || count.Loose != 1 || !looseExists(r, h.loose) {
		t.Fatalf("count = %+v", count)
	}
}

func TestRepackReplacesOldPacksAndKeepsTheirUnreachableObjectsLoose(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	packMaintObjects(t, r, []hash.ObjectID{h.loose, h.blobA})
	old := onlyPack(t, r)
	removeLooseNow(t, r, h.loose)
	past := time.Now().Add(-48 * time.Hour)
	agePack(t, r, old, past)

	result, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Loosened != 1 || result.Dropped != 0 || !slices.Equal(result.Removed, []string{old}) || len(result.Busy) != 0 {
		t.Fatalf("result = %+v", result)
	}
	info, err := os.Stat(loosePath(r, h.loose))
	if err != nil {
		t.Fatalf("the unreachable blob was lost: %v", err)
	}
	if info.ModTime().After(past.Add(time.Minute)) {
		t.Fatalf("loosened at %v, want the time of its old pack", info.ModTime())
	}
	if onlyPack(t, r) == old {
		t.Fatal("the old pack is still the only one")
	}
}

func TestRepackWithAnExpiryDropsWhatOldPacksHoldForNobody(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	needed := putMaintBlob(t, r, "needed by a fresh tree\n")
	putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: needed})
	packMaintObjects(t, r, []hash.ObjectID{h.loose, needed})
	removeLooseNow(t, r, h.loose)
	removeLooseNow(t, r, needed)
	agePack(t, r, onlyPack(t, r), time.Now().Add(-48*time.Hour))

	result, err := Repack(t.Context(), r.repo, RepackOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	if result.Dropped != 1 || result.Loosened != 1 || looseExists(r, h.loose) || !looseExists(r, needed) {
		t.Fatalf("result = %+v, dropped blob loose = %v, needed loose = %v", result, looseExists(r, h.loose), looseExists(r, needed))
	}
}

func TestRepackLeavesKeptPacksAndTheirObjectsAlone(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	packMaintObjects(t, r, []hash.ObjectID{h.second})
	kept := onlyPack(t, r)
	writeGitFile(t, r.repo.PackDir(), kept+".keep", "fetch in progress\n")
	reachable := len(mustWalk(t, r).seen)

	result, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Objects != reachable-1 || len(result.Removed) != 0 || !slices.Contains(packNames(t, r), kept) {
		t.Fatalf("result = %+v, packs = %v", result, packNames(t, r))
	}
}

func TestRepackReportsOldPacksItCouldNotRemove(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	packMaintObjects(t, r, []hash.ObjectID{h.first})
	old := onlyPack(t, r)
	swapMaint(t, &removePackFiles, func(string, string) error { return errors.New("in use") })

	result, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(result.Busy, []string{old}) || len(result.Removed) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRepackDoesNotRemoveAPackItJustRewroteUnchanged(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	first, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	second, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if second.Pack != first.Pack || len(second.Removed) != 0 || onlyPack(t, r) != first.Pack {
		t.Fatalf("first = %+v, second = %+v", first, second)
	}
}

func TestRepackOfAnEmptyRepositoryWritesNothing(t *testing.T) {
	r := newTestRepo(t)
	if err := os.MkdirAll(r.repo.PackDir(), 0o777); err != nil {
		t.Fatal(err)
	}

	result, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Pack != "" || len(packNames(t, r)) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRepackLeavesAnUnreachableObjectThatIsAlreadyLooseAsItIs(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	packMaintObjects(t, r, []hash.ObjectID{h.loose})
	recent := time.Now()
	ageFile(t, loosePath(r, h.loose), recent)

	result, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Loosened != 0 || !looseExists(r, h.loose) {
		t.Fatalf("result = %+v", result)
	}
}

func TestRepackUsesGitsDeltaDefaultsUnlessToldOtherwise(t *testing.T) {
	if positiveOr(0, repackWindow) != 10 || positiveOr(3, repackWindow) != 3 {
		t.Fatal("the window default is wrong")
	}
}

func TestRepackReportsEveryFailure(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name   string
		expire bool
		setup  func(t *testing.T, r *testRepo, h maintHistory, cancel context.CancelFunc)
	}{
		{"opening the database", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"walking the history", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"walking from recent objects", true, func(t *testing.T, r *testRepo, _ maintHistory, cancel context.CancelFunc) {
			swapMaint(t, &dbPackedSince, func(*odb.DB, time.Time) iter.Seq[hash.ObjectID] {
				cancel()
				return func(func(hash.ObjectID) bool) {}
			})
		}},
		{"listing packs", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &dbPacks, func(*odb.DB) ([]odb.PackInfo, error) { return nil, boom })
		}},
		{"listing loose objects", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &dbLooseObjects, failingLooseObjects(boom))
		}},
		{"writing the pack", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &dbWritePack, func(*odb.DB, context.Context, []hash.ObjectID, pack.WriteOptions) (pack.IndexResult, error) {
				return pack.IndexResult{}, boom
			})
		}},
		{"reading an object to loosen", false, func(t *testing.T, r *testRepo, h maintHistory, _ context.CancelFunc) {
			packUnreachableOnly(t, r, h)
			swapMaint(t, &dbGet, func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) { return 0, nil, boom })
		}},
		{"writing a loosened object", false, func(t *testing.T, r *testRepo, h maintHistory, _ context.CancelFunc) {
			packUnreachableOnly(t, r, h)
			swapMaint(t, &dbPutLoose, func(*odb.DB, object.Type, []byte) (hash.ObjectID, error) { return hash.Zero, boom })
		}},
		{"dating a loosened object", false, func(t *testing.T, r *testRepo, h maintHistory, _ context.CancelFunc) {
			packUnreachableOnly(t, r, h)
			swapMaint(t, &dbTouch, func(*odb.DB, hash.ObjectID, time.Time) error { return boom })
		}},
		{"reopening the database to drop loose copies", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			real, calls := odbOpen, 0
			swapMaint(t, &odbOpen, func(dir string, opts odb.Options) (*odb.DB, error) {
				calls++
				if calls == 2 {
					return nil, boom
				}
				return real(dir, opts)
			})
		}},
		{"listing loose copies", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			real, calls := dbLooseObjects, 0
			swapMaint(t, &dbLooseObjects, func(db *odb.DB) iter.Seq2[odb.LooseObject, error] {
				calls++
				if calls == 2 {
					return failingLooseObjects(boom)(db)
				}
				return real(db)
			})
		}},
		{"stopping when cancelled while dropping loose copies", false, func(t *testing.T, r *testRepo, _ maintHistory, cancel context.CancelFunc) {
			real, calls := dbLooseObjects, 0
			swapMaint(t, &dbLooseObjects, func(db *odb.DB) iter.Seq2[odb.LooseObject, error] {
				calls++
				if calls == 2 {
					cancel()
				}
				return real(db)
			})
		}},
		{"checking a loose copy against the packs", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &dbPacked, func(*odb.DB, hash.ObjectID) (bool, error) { return false, boom })
		}},
		{"removing a loose copy", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &dbRemoveLoose, func(*odb.DB, hash.ObjectID) error { return boom })
		}},
		{"removing empty fanouts", false, func(t *testing.T, r *testRepo, _ maintHistory, _ context.CancelFunc) {
			swapMaint(t, &dbRemoveEmptyFanouts, func(*odb.DB) error { return boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			h := buildMaintHistory(t, r)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tt.setup(t, r, h, cancel)
			opts := RepackOptions{}
			if tt.expire {
				opts.Expire = time.Now().Add(-time.Hour)
			}

			if _, err := Repack(ctx, r.repo, opts); err == nil {
				t.Fatal("the failure was not reported")
			}
		})
	}
}

func packUnreachableOnly(t *testing.T, r *testRepo, h maintHistory) {
	t.Helper()
	packMaintObjects(t, r, []hash.ObjectID{h.loose})
	removeLooseNow(t, r, h.loose)
}
