package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func gcRepo(t *testing.T, config string) (*testRepo, maintHistory, hash.ObjectID, hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t)
	if config != "" {
		r.appendConfig(config)
		r.repo = r.reopen()
	}
	h := buildMaintHistory(t, r)
	fresh := putMaintBlob(t, r, "written a moment ago\n")
	ageFile(t, loosePath(r, h.loose), time.Now().AddDate(0, 0, -30))
	return r, h, h.loose, fresh
}

func TestGCPacksRefsAndObjectsPrunesOldGarbageAndWritesTheGraph(t *testing.T) {
	r, h, old, fresh := gcRepo(t, "")

	result, err := GC(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}

	count, err := CountObjects(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if count.Packs != 1 || count.Loose != 1 || looseExists(r, old) || !looseExists(r, fresh) {
		t.Fatalf("count = %+v, old loose = %v, fresh loose = %v", count, looseExists(r, old), looseExists(r, fresh))
	}
	if !result.Pruned || result.CommitGraph != 2 || result.Repack.Pack == "" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(r.repo.CommonDir(), "refs", "heads", "main")); err == nil {
		t.Fatal("the branch is still a loose ref")
	}
	if got := r.branchTarget("main"); got != h.second {
		t.Fatalf("main = %s after packing refs, want %s", got, h.second)
	}
}

func TestGCWithPruningSwitchedOffKeepsOldGarbage(t *testing.T) {
	r, _, old, _ := gcRepo(t, "[gc]\n\tpruneExpire = never\n")

	result, err := GC(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}

	if result.Pruned || !looseExists(r, old) {
		t.Fatalf("result = %+v, old loose = %v", result, looseExists(r, old))
	}
}

func TestGCWithoutTheCommitGraphLeavesItUnwritten(t *testing.T) {
	r, _, _, _ := gcRepo(t, "[gc]\n\twriteCommitGraph = false\n")

	result, err := GC(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}

	if result.CommitGraph != 0 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(r.repo.ObjectsDir(), "info", commitgraph.FileName)); err == nil {
		t.Fatal("a commit graph was written although gc.writeCommitGraph is false")
	}
}

func TestGCRefusesSettingsItCannotRead(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
		want   error
	}{
		{"an expiry git would not understand", "[gc]\n\tpruneExpire = soon\n", ErrInvalidExpiry},
		{"a commit graph switch that is not a boolean", "[gc]\n\twriteCommitGraph = maybe\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, _, _, _ := gcRepo(t, tt.config)

			_, err := GC(t.Context(), r.repo)

			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGCReportsEveryFailingStep(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"opening the database to pack refs", func(t *testing.T) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"opening the ref store", func(t *testing.T) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"packing refs", func(t *testing.T) {
			swapMaint(t, &storePackRefs, func(*refs.Store, bool) error { return boom })
		}},
		{"repacking", func(t *testing.T) {
			swapMaint(t, &dbWritePack, func(*odb.DB, context.Context, []hash.ObjectID, pack.WriteOptions) (pack.IndexResult, error) {
				return pack.IndexResult{}, boom
			})
		}},
		{"pruning", func(t *testing.T) {
			swapMaint(t, &dbTempFiles, func(*odb.DB) ([]odb.TempFile, error) { return nil, boom })
		}},
		{"writing the commit graph", func(t *testing.T) {
			swapMaint(t, &writeCommitGraphFile, func(string, hash.Format, []commitgraph.Commit) error { return boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, _, _, _ := gcRepo(t, "")
			tt.setup(t)

			if _, err := GC(t.Context(), r.repo); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want boom", err)
			}
		})
	}
}

func TestGCTakesTheCurrentMomentForItsExpiry(t *testing.T) {
	r, _, old, _ := gcRepo(t, "[gc]\n\tpruneExpire = 1.year.ago\n")
	swapMaint(t, &gcNow, func() time.Time { return time.Now().AddDate(2, 0, 0) })

	if _, err := GC(t.Context(), r.repo); err != nil {
		t.Fatal(err)
	}

	if looseExists(r, old) {
		t.Fatal("an object a year older than the expiry survived")
	}
}
