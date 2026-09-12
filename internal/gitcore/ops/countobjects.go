package ops

import (
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var (
	dbPacks        = (*odb.DB).Packs
	dbLooseObjects = (*odb.DB).LooseObjects
	dbPacked       = (*odb.DB).Packed
	dbTempFiles    = (*odb.DB).TempFiles
)

type ObjectCount struct {
	Loose         int
	LooseBytes    int64
	InPack        int
	Packs         int
	PackBytes     int64
	PrunePackable int
	Garbage       int
	GarbageBytes  int64
}

func CountObjects(r *repo.Repository) (ObjectCount, error) {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return ObjectCount{}, err
	}
	defer func() { _ = db.Close() }()
	var count ObjectCount
	packs, err := dbPacks(db)
	if err != nil {
		return ObjectCount{}, err
	}
	for _, p := range packs {
		count.Packs++
		count.InPack += p.Objects
		count.PackBytes += p.Size
	}
	for loose, err := range dbLooseObjects(db) {
		if err != nil {
			return ObjectCount{}, err
		}
		count.Loose++
		count.LooseBytes += loose.Size
		packed, err := dbPacked(db, loose.ID)
		if err != nil {
			return ObjectCount{}, err
		}
		if packed {
			count.PrunePackable++
		}
	}
	temps, err := dbTempFiles(db)
	if err != nil {
		return ObjectCount{}, err
	}
	for _, temp := range temps {
		count.Garbage++
		count.GarbageBytes += temp.Size
	}
	return count, nil
}
