package ops

import (
	"context"
	"errors"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type FsckKind int

const (
	FsckMissing FsckKind = iota + 1
	FsckCorrupt
	FsckMalformed
	FsckWrongType
)

var (
	dbVerifyLoose = (*odb.DB).VerifyLoose
	dbVerifyPack  = (*odb.DB).VerifyPack
)

type FsckProblem struct {
	Kind FsckKind
	ID   hash.ObjectID
	Pack string
	Err  error
}

type FsckReport struct {
	Problems    []FsckProblem
	Unreachable []hash.ObjectID
	Checked     int
}

func (r FsckReport) Healthy() bool {
	return len(r.Problems) == 0
}

func Fsck(ctx context.Context, r *repo.Repository) (FsckReport, error) {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return FsckReport{}, err
	}
	defer func() { _ = db.Close() }()
	var report FsckReport
	stored, err := verifyStorage(ctx, db, &report)
	if err != nil {
		return report, err
	}
	walk, err := newObjectWalk(ctx, r, db)
	if err != nil {
		return report, err
	}
	walk.strict = true
	walk.broken = func(id hash.ObjectID, trouble walkTrouble, err error) error {
		report.Problems = append(report.Problems, FsckProblem{Kind: fsckKindOf(trouble, err), ID: id, Err: err})
		return nil
	}
	if err := walk.gatherRoots(r); err != nil {
		return report, err
	}
	if err := walk.run(); err != nil {
		return report, err
	}
	for id := range stored {
		if !walk.reachable(id) {
			report.Unreachable = append(report.Unreachable, id)
		}
	}
	slices.SortFunc(report.Unreachable, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return report, nil
}

func verifyStorage(ctx context.Context, db *odb.DB, report *FsckReport) (map[hash.ObjectID]struct{}, error) {
	stored := make(map[hash.ObjectID]struct{})
	for loose, err := range dbLooseObjects(db) {
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stored[loose.ID] = struct{}{}
		report.Checked++
		if err := dbVerifyLoose(db, loose.ID); err != nil {
			report.Problems = append(report.Problems, FsckProblem{Kind: FsckCorrupt, ID: loose.ID, Err: err})
		}
	}
	packs, err := dbPacks(db)
	if err != nil {
		return nil, err
	}
	for _, p := range packs {
		for id := range dbPackObjects(db, p.Name) {
			stored[id] = struct{}{}
		}
		report.Checked += p.Objects
		for id, err := range dbVerifyPack(db, ctx, p.Name) {
			report.Problems = append(report.Problems, FsckProblem{Kind: FsckCorrupt, ID: id, Pack: p.Name, Err: err})
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

func fsckKindOf(trouble walkTrouble, err error) FsckKind {
	switch {
	case trouble == troubleWrongType:
		return FsckWrongType
	case trouble == troubleMalformed:
		return FsckMalformed
	case errors.Is(err, odb.ErrNotFound):
		return FsckMissing
	default:
		return FsckCorrupt
	}
}
