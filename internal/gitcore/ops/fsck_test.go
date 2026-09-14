package ops

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func mustFsck(t *testing.T, r *testRepo) FsckReport {
	t.Helper()
	report, err := Fsck(t.Context(), r.repo)
	if err != nil {
		t.Fatalf("Fsck returned error %v", err)
	}
	return report
}

func problemOf(report FsckReport, kind FsckKind, id hash.ObjectID) bool {
	return slices.ContainsFunc(report.Problems, func(p FsckProblem) bool { return p.Kind == kind && p.ID == id })
}

func overwriteLoose(t *testing.T, r *testRepo, id hash.ObjectID, kind object.Type, content []byte) {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write(object.EncodeLooseRaw(kind, content))
	_ = writer.Close()
	path := loosePath(r, id)
	_ = os.Chmod(path, 0o644)
	if err := os.WriteFile(path, compressed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func flipByte(t *testing.T, path string, at func(size int64) int64) {
	t.Helper()
	_ = os.Chmod(path, 0o644)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	offset := at(info.Size())
	var one [1]byte
	if _, err := file.ReadAt(one[:], offset); err != nil {
		t.Fatal(err)
	}
	one[0] ^= 0xFF
	if _, err := file.WriteAt(one[:], offset); err != nil {
		t.Fatal(err)
	}
}

func TestAHealthyRepositoryHasNoProblemsAndNamesItsUnreachableObjects(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	another := putMaintBlob(t, r, "also unreachable\n")
	want := []hash.ObjectID{h.loose, another}
	slices.SortFunc(want, func(a, b hash.ObjectID) int { return a.Compare(b) })

	report := mustFsck(t, r)

	if !report.Healthy() || !slices.Equal(report.Unreachable, want) || report.Checked == 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestFsckFindsAMissingBlob(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	removeLooseNow(t, r, h.blobB)

	report := mustFsck(t, r)

	if report.Healthy() || !problemOf(report, FsckMissing, h.blobB) {
		t.Fatalf("report = %+v", report)
	}
}

func TestFsckFindsALooseObjectWhoseContentIsNotItsName(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	overwriteLoose(t, r, h.blobA, object.TypeBlob, []byte("swapped content\n"))

	report := mustFsck(t, r)

	if !problemOf(report, FsckCorrupt, h.blobA) {
		t.Fatalf("report = %+v", report)
	}
}

func TestFsckFindsObjectsThatDoNotParseOrHaveTheWrongType(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	garbage, err := r.db().Put(object.TypeCommit, []byte("garbage"))
	if err != nil {
		t.Fatal(err)
	}
	setMaintRef(t, r, refs.BranchName("garbage"), garbage)
	lonelyTree := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "inner", ID: putMaintBlob(t, r, "inner\n")})
	lonelyBlob := putMaintBlob(t, r, "stands where a tree should\n")
	blobAsTree := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "not-a-blob", ID: lonelyTree})
	setMaintRef(t, r, refs.BranchName("mistyped"), putMaintCommit(t, r, blobAsTree))
	setMaintRef(t, r, refs.BranchName("blobtree"), putMaintCommit(t, r, lonelyBlob))
	unsorted, err := r.db().Put(object.TypeTree, (&object.Tree{Entries: []object.TreeEntry{
		{Mode: object.ModeBlob, Name: "z", ID: h.blobA},
		{Mode: object.ModeBlob, Name: "a", ID: h.blobB},
	}}).Encode())
	if err != nil {
		t.Fatal(err)
	}
	setMaintRef(t, r, refs.BranchName("unsorted"), putMaintCommit(t, r, unsorted))

	report := mustFsck(t, r)

	for name, found := range map[string]bool{
		"a commit that does not parse":   problemOf(report, FsckMalformed, garbage),
		"a tree where a blob should be":  problemOf(report, FsckWrongType, lonelyTree),
		"a blob where a tree should be":  problemOf(report, FsckWrongType, lonelyBlob),
		"a tree with entries disordered": problemOf(report, FsckMalformed, unsorted),
	} {
		if !found {
			t.Fatalf("%s was not reported: %+v", name, report.Problems)
		}
	}
}

func TestFsckFindsADamagedPack(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	packAllLoose(t, r)
	count, err := CountObjects(r.repo)
	if err != nil || count.Packs != 1 {
		t.Fatalf("count = %+v, err = %v", count, err)
	}
	names := packNames(t, r)
	flipByte(t, filepath.Join(r.repo.PackDir(), names[0]+".pack"), func(size int64) int64 { return size - hash.Size - 1 })

	report := mustFsck(t, r)

	if !slices.ContainsFunc(report.Problems, func(p FsckProblem) bool { return p.Kind == FsckCorrupt && p.Pack == names[0] }) {
		t.Fatalf("report = %+v", report.Problems)
	}
}

func TestProblemsAreSortedIntoKinds(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		trouble walkTrouble
		err     error
		want    FsckKind
	}{
		{troubleUnreadable, odb.ErrNotFound, FsckMissing},
		{troubleUnreadable, boom, FsckCorrupt},
		{troubleMalformed, boom, FsckMalformed},
		{troubleWrongType, boom, FsckWrongType},
	} {
		if got := fsckKindOf(tt.trouble, tt.err); got != tt.want {
			t.Fatalf("fsckKindOf(%d, %v) = %d, want %d", tt.trouble, tt.err, got, tt.want)
		}
	}
}

func TestAStrictWalkReportsABlobItCannotTypeAndGoesOn(t *testing.T) {
	r := newTestRepo(t)
	db, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	walk, err := newObjectWalk(t.Context(), r.repo, db)
	if err != nil {
		t.Fatal(err)
	}
	walk.strict = true
	var troubles []walkTrouble
	walk.broken = func(_ hash.ObjectID, trouble walkTrouble, _ error) error {
		troubles = append(troubles, trouble)
		return nil
	}
	walk.push(hash.SumSHA1("blob", []byte("absent")), object.TypeBlob)

	if err := walk.run(); err != nil || !slices.Equal(troubles, []walkTrouble{troubleUnreadable}) {
		t.Fatalf("err = %v, troubles = %v", err, troubles)
	}
	_ = db.Close()
}

func TestFsckReportsEveryFailureItCannotWorkAround(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo, cancel context.CancelFunc)
	}{
		{"opening the database", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"listing loose objects", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &dbLooseObjects, failingLooseObjects(boom))
		}},
		{"stopping while checking loose objects", func(t *testing.T, r *testRepo, cancel context.CancelFunc) {
			cancel()
		}},
		{"listing packs", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &dbPacks, func(*odb.DB) ([]odb.PackInfo, error) { return nil, boom })
		}},
		{"stopping while checking packs", func(t *testing.T, r *testRepo, cancel context.CancelFunc) {
			packAllLoose(t, r)
			swapMaint(t, &dbLooseObjects, func(*odb.DB) iter.Seq2[odb.LooseObject, error] {
				return func(func(odb.LooseObject, error) bool) {}
			})
			swapMaint(t, &dbVerifyPack, func(*odb.DB, context.Context, string) iter.Seq2[hash.ObjectID, error] {
				cancel()
				return func(yield func(hash.ObjectID, error) bool) { yield(hash.Zero, boom) }
			})
		}},
		{"reading the shallow file", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			writeGitFile(t, r.repo.CommonDir(), "shallow", "not a hash\n")
		}},
		{"gathering the roots", func(t *testing.T, r *testRepo, _ context.CancelFunc) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"stopping while walking", func(t *testing.T, r *testRepo, cancel context.CancelFunc) {
			swapMaint(t, &dbPacks, func(*odb.DB) ([]odb.PackInfo, error) {
				cancel()
				return nil, nil
			})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			buildMaintHistory(t, r)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tt.setup(t, r, cancel)

			if _, err := Fsck(ctx, r.repo); err == nil {
				t.Fatal("the failure was not reported")
			}
		})
	}
}
