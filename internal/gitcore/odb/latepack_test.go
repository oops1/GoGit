package odb

import "testing"

func TestHasAndSizeFindAnObjectPackedAfterOpening(t *testing.T) {
	objects := newObjectsDir(t)
	first := openDB(t, objects, Options{})
	second := openDB(t, objects, Options{})
	packed := packFixtureObjects(t)[0]

	copyFixturePacks(t, objects)

	if known, err := first.Has(packed.id); err != nil || !known {
		t.Fatalf("Has gave (%v, %v) for an object packed after opening", known, err)
	}
	if size, err := second.Size(packed.id); err != nil || size != packed.size {
		t.Fatalf("Size gave (%d, %v), want %d", size, err, packed.size)
	}
}
