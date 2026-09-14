package odb

import "testing"

func TestAnIntactLooseCopyServesAnObjectWhosePackedCopyIsDamaged(t *testing.T) {
	intact := openDB(t, func() string {
		dir := newObjectsDir(t)
		copyFixturePacks(t, dir)
		return dir
	}(), Options{})
	damaged, _ := corruptedPackDB(t, ".pack", func(size int) int { return size / 2 })

	var broken []fixtureObject
	for _, packed := range packFixtureObjects(t) {
		if _, _, err := damaged.Get(packed.id); err != nil {
			broken = append(broken, packed)
		}
	}
	if len(broken) < 2 {
		t.Fatalf("the damaged pack broke %d objects, want at least 2", len(broken))
	}
	rescued, lost := broken[0], broken[1]
	kind, data, err := intact.Get(rescued.id)
	if err != nil {
		t.Fatal(err)
	}
	writeLooseFile(t, damaged.dir, rescued.id, looseBytes(t, kind, data))

	if gotKind, got, err := damaged.Get(rescued.id); err != nil || gotKind != kind || string(got) != string(data) {
		t.Fatalf("Get gave (%v, %d bytes, %v) with an intact loose copy", gotKind, len(got), err)
	}
	if gotKind, size, err := damaged.Info(rescued.id); err != nil || gotKind != kind || size != rescued.size {
		t.Fatalf("Info gave (%v, %d, %v), want (%v, %d)", gotKind, size, err, kind, rescued.size)
	}
	if _, _, err := damaged.Get(lost.id); err == nil {
		t.Fatal("Get hid the pack damage of an object without another copy")
	}
}
