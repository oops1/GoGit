package odb

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

var _ revision.PrefixResolver = (*DB)(nil)

func idBytes(values ...byte) hash.ObjectID {
	var id hash.ObjectID
	copy(id[:], values)
	return id
}

func sortedIDs(ids []hash.ObjectID) []hash.ObjectID {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return sorted
}

func writePackedIDs(t testing.TB, objects, name string, ids ...hash.ObjectID) {
	t.Helper()
	builder := newThinPack()
	for range ids {
		builder.addRefDelta(t, hash.Zero, []byte{0, 0})
	}
	writeCustomPack(t, objects, name, builder, ids)
}

func TestResolveShortRejectsInvalidPrefixes(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	cases := []string{
		"",
		"abc",
		strings.Repeat("a", hash.HexSize+1),
		"ABCD",
		"abcg",
	}
	for _, prefix := range cases {
		t.Run(prefix, func(t *testing.T) {
			if _, err := db.ResolveShort(prefix); !errors.Is(err, ErrInvalidPrefix) {
				t.Fatalf("ResolveShort(%q) returned %v, want %v", prefix, err, ErrInvalidPrefix)
			}
		})
	}
}

func TestResolveShortReturnsTheSingleMatchForAFullHash(t *testing.T) {
	db := newFixtureDB(t)
	want := looseFixtureObjects(t)[0]
	got, err := db.ResolveShort(want.id.String())
	if err != nil || len(got) != 1 || got[0] != want.id {
		t.Fatalf("ResolveShort(full) = %v, %v, want [%s]", got, err, want.id)
	}
}

func TestResolveShortReturnsNoMatchesForAnAbsentFullHash(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	got, err := db.ResolveShort(hash.Zero.String())
	if err != nil || len(got) != 0 {
		t.Fatalf("ResolveShort(zero) = %v, %v, want none", got, err)
	}
}

func TestResolveShortFindsAPrefixOnlyPresentInLooseStorage(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	id := idBytes(0xde, 0xad, 0x01)
	writeLooseFile(t, objects, id, []byte("garbage"))
	got, err := db.ResolveShort("dead")
	if err != nil || len(got) != 1 || got[0] != id {
		t.Fatalf("ResolveShort(dead) = %v, %v, want [%s]", got, err, id)
	}
}

func TestResolveShortFindsAPrefixOnlyPresentInAPackfile(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	id := idBytes(0xbe, 0xef, 0x01)
	writePackedIDs(t, objects, "pack-only", id)
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	got, err := db.ResolveShort("beef")
	if err != nil || len(got) != 1 || got[0] != id {
		t.Fatalf("ResolveShort(beef) = %v, %v, want [%s]", got, err, id)
	}
}

func TestResolveShortFindsObjectsThroughAlternates(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	id := storeBlob(t, shared, []byte("via alternate\n"))
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	short := id.String()[:MinShortPrefix]
	got, err := db.ResolveShort(short)
	if err != nil {
		t.Fatalf("ResolveShort returned error %v", err)
	}
	if !slices.Contains(got, id) {
		t.Fatalf("ResolveShort(%q) = %v, want it to include %s", short, got, id)
	}
}

func TestResolveShortIsAmbiguousBetweenLooseAndPacked(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	looseID := idBytes(0xab, 0xcd, 0x01)
	packedID := idBytes(0xab, 0xcd, 0x02)
	writeLooseFile(t, objects, looseID, []byte("garbage"))
	writePackedIDs(t, objects, "pack-collide", packedID)
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	got, err := db.ResolveShort("abcd")
	if err != nil {
		t.Fatalf("ResolveShort returned error %v", err)
	}
	want := sortedIDs([]hash.ObjectID{looseID, packedID})
	if !slices.Equal(got, want) {
		t.Fatalf("ResolveShort(abcd) = %v, want %v", got, want)
	}
}

func TestResolveShortDeduplicatesObjectsFoundInMultiplePlaces(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	writeAlternates(t, main, shared)
	id := idBytes(0xfa, 0xce, 0x01)
	writeLooseFile(t, main, id, []byte("garbage"))
	writeLooseFile(t, shared, id, []byte("garbage"))
	db := openDB(t, main, Options{})
	got, err := db.ResolveShort("face")
	if err != nil {
		t.Fatalf("ResolveShort returned error %v", err)
	}
	if len(got) != 1 || got[0] != id {
		t.Fatalf("ResolveShort(face) = %v, want exactly one match for %s", got, id)
	}
}

func TestResolveShortLimitsTheNumberOfMatches(t *testing.T) {
	objects := newObjectsDir(t)
	var ids []hash.ObjectID
	for i := byte(1); i <= 5; i++ {
		id := idBytes(0xaa, 0xaa, i)
		ids = append(ids, id)
		writeLooseFile(t, objects, id, []byte("garbage"))
	}
	db := openDB(t, objects, Options{MaxShortMatches: 2})
	got, err := db.ResolveShort("aaaa")
	if err != nil {
		t.Fatalf("ResolveShort returned error %v", err)
	}
	want := sortedIDs(ids)[:2]
	if !slices.Equal(got, want) {
		t.Fatalf("ResolveShort(aaaa) = %v, want %v", got, want)
	}
}

func TestResolveShortPropagatesLooseReadErrors(t *testing.T) {
	db := newFixtureDB(t)
	swapRootOpen(t, always, errInjected)
	if _, err := db.ResolveShort("abcd1234"); !errors.Is(err, errInjected) {
		t.Fatalf("ResolveShort returned %v, want %v", err, errInjected)
	}
}

func TestResolveShortPropagatesAlternateReadErrors(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	alternate := db.Alternates()[0]
	swapRootOpen(t, onlyIn(alternate), errInjected)
	if _, err := db.ResolveShort("dead1234"); !errors.Is(err, errInjected) {
		t.Fatalf("ResolveShort returned %v, want %v", err, errInjected)
	}
}

func TestResolveShortPropagatesHasErrorsForFullHashes(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootStat(t, always, errInjected)
	if _, err := db.ResolveShort(hash.Zero.String()); !errors.Is(err, errInjected) {
		t.Fatalf("ResolveShort returned %v, want %v", err, errInjected)
	}
}

func TestResolveShortSkipsEntriesThatArentValidObjectNames(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	writeFile(t, filepath.Join(objects, "de", "adbeef"), []byte("not an object"))
	got, err := db.ResolveShort("dead")
	if err != nil {
		t.Fatalf("ResolveShort returned error %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ResolveShort matched a malformed loose file name: %v", got)
	}
}

func TestAbbreviateIDReturnsShortestUniquePrefix(t *testing.T) {
	db := newFixtureDB(t)
	id := looseFixtureObjects(t)[0].id
	short, err := db.AbbreviateID(id, MinShortPrefix)
	if err != nil {
		t.Fatalf("AbbreviateID returned error %v", err)
	}
	ids, err := db.ResolveShort(short)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("ResolveShort(%q) = %v, %v, want [%s]", short, ids, err, id)
	}
}

func TestAbbreviateIDDefaultsToSevenCharacters(t *testing.T) {
	db := newFixtureDB(t)
	id := looseFixtureObjects(t)[0].id
	short, err := db.AbbreviateID(id, 0)
	if err != nil {
		t.Fatalf("AbbreviateID returned error %v", err)
	}
	if len(short) < DefaultAbbrevLength {
		t.Fatalf("AbbreviateID gave %q, want at least %d characters", short, DefaultAbbrevLength)
	}
}

func TestAbbreviateIDClampsMinLenToTheShortestAllowedPrefix(t *testing.T) {
	db := newFixtureDB(t)
	id := looseFixtureObjects(t)[0].id
	short, err := db.AbbreviateID(id, 1)
	if err != nil {
		t.Fatalf("AbbreviateID returned error %v", err)
	}
	if len(short) < MinShortPrefix {
		t.Fatalf("AbbreviateID gave %q, want at least %d characters", short, MinShortPrefix)
	}
}

func TestAbbreviateIDGrowsUntilUnique(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	idA := idBytes(0x12, 0x34, 0x56, 0x78, 0x9a, 0x01)
	idB := idBytes(0x12, 0x34, 0x56, 0x78, 0x9a, 0x02)
	writeLooseFile(t, objects, idA, []byte("a"))
	writeLooseFile(t, objects, idB, []byte("b"))
	short, err := db.AbbreviateID(idA, MinShortPrefix)
	if err != nil {
		t.Fatalf("AbbreviateID returned error %v", err)
	}
	if len(short) <= MinShortPrefix {
		t.Fatalf("AbbreviateID gave %q, want it longer than the shared prefix", short)
	}
	ids, err := db.ResolveShort(short)
	if err != nil || len(ids) != 1 || ids[0] != idA {
		t.Fatalf("ResolveShort(%q) = %v, %v, want [%s]", short, ids, err, idA)
	}
	shorter := short[:len(short)-1]
	ids, err = db.ResolveShort(shorter)
	if err != nil {
		t.Fatalf("ResolveShort(%q) returned error %v", shorter, err)
	}
	if len(ids) == 1 && ids[0] == idA {
		t.Fatalf("ResolveShort(%q) uniquely resolved to idA, want it still ambiguous", shorter)
	}
}

func TestAbbreviateIDFallsBackToFullHashWhenOnlyTheLastNibbleDiffers(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	base := slices.Repeat([]byte{0x77}, hash.Size-1)
	idA := idBytes(append(slices.Clone(base), 0x10)...)
	idB := idBytes(append(slices.Clone(base), 0x1f)...)
	writeLooseFile(t, objects, idA, []byte("a"))
	writeLooseFile(t, objects, idB, []byte("b"))
	short, err := db.AbbreviateID(idA, MinShortPrefix)
	if err != nil {
		t.Fatalf("AbbreviateID returned error %v", err)
	}
	if short != idA.String() {
		t.Fatalf("AbbreviateID = %q, want the full hash %s", short, idA)
	}
}

func TestAbbreviateIDReportsMissingObjects(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	if _, err := db.AbbreviateID(hash.Zero, MinShortPrefix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AbbreviateID returned %v, want %v", err, ErrNotFound)
	}
}

func TestAbbreviateIDPropagatesHasErrors(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	id := idBytes(0x99, 0x99, 0x99)
	swapRootStat(t, always, errInjected)
	if _, err := db.AbbreviateID(id, MinShortPrefix); !errors.Is(err, errInjected) {
		t.Fatalf("AbbreviateID returned %v, want %v", err, errInjected)
	}
}

func TestAbbreviateIDPropagatesResolveShortErrors(t *testing.T) {
	db := newFixtureDB(t)
	id := looseFixtureObjects(t)[0].id
	swapRootOpen(t, always, errInjected)
	if _, err := db.AbbreviateID(id, MinShortPrefix); !errors.Is(err, errInjected) {
		t.Fatalf("AbbreviateID returned %v, want %v", err, errInjected)
	}
}
