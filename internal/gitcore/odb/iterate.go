package odb

import (
	"io/fs"
	"iter"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (d *DB) Loose() iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		d.eachLoose(yield)
	}
}

func (d *DB) All() iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		d.eachAll(make(map[hash.ObjectID]struct{}), yield)
	}
}

func (d *DB) eachLoose(yield func(hash.ObjectID, error) bool) bool {
	fanouts, err := d.readDir(".")
	if err != nil {
		return yield(hash.Zero, err)
	}
	for _, fanout := range sortEntries(fanouts) {
		if !fanout.IsDir() || len(fanout.Name()) != fanoutLength {
			continue
		}
		if !d.eachInFanout(fanout.Name(), yield) {
			return false
		}
	}
	return true
}

func (d *DB) eachInFanout(fanout string, yield func(hash.ObjectID, error) bool) bool {
	entries, err := d.readDir(fanout)
	if err != nil {
		return yield(hash.Zero, err)
	}
	for _, entry := range sortEntries(entries) {
		if entry.IsDir() {
			continue
		}
		id, ok := looseID(fanout, entry.Name())
		if !ok {
			continue
		}
		if !yield(id, nil) {
			return false
		}
	}
	return true
}

func (d *DB) eachAll(seen map[hash.ObjectID]struct{}, yield func(hash.ObjectID, error) bool) bool {
	for id, err := range d.Loose() {
		if err != nil {
			if !yield(hash.Zero, err) {
				return false
			}
			continue
		}
		if !emit(seen, id, yield) {
			return false
		}
	}
	if store := d.store(); store != nil {
		for id := range store.Objects() {
			if !emit(seen, id, yield) {
				return false
			}
		}
	}
	for _, alternate := range d.alternates {
		if !alternate.eachAll(seen, yield) {
			return false
		}
	}
	return true
}

func emit(seen map[hash.ObjectID]struct{}, id hash.ObjectID, yield func(hash.ObjectID, error) bool) bool {
	if _, ok := seen[id]; ok {
		return true
	}
	seen[id] = struct{}{}
	return yield(id, nil)
}

func sortEntries(entries []fs.DirEntry) []fs.DirEntry {
	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return entries
}
