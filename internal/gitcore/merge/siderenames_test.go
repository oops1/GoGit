package merge

import (
	"errors"
	"maps"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type countingObjects struct {
	*memoryStore
	reads map[hash.ObjectID]int
}

func (c *countingObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	c.reads[id]++
	return c.memoryStore.Get(id)
}

func writeSnapshot(t *testing.T, s *memoryStore, snapshot Snapshot) hash.ObjectID {
	t.Helper()
	id, err := snapshot.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDetectSideRenamesReadsEachObjectOnce(t *testing.T) {
	s := newStore()
	moved := tenLines("moved")
	base := writeSnapshot(t, s, Snapshot{"a": s.blob(t, moved), "k": s.blob(t, "k\n")})
	ours := writeSnapshot(t, s, Snapshot{"ours/a": s.blob(t, moved+"ours\n"), "k": s.blob(t, "k\n")})
	theirs := writeSnapshot(t, s, Snapshot{"theirs/a": s.blob(t, moved+"theirs\n"), "k": s.blob(t, "k\n")})
	counting := &countingObjects{memoryStore: s, reads: map[hash.ObjectID]int{}}

	ourRenames, theirRenames, err := DetectSideRenames(t.Context(), counting, base, ours, theirs)

	if err != nil || !maps.Equal(ourRenames, Renames{"a": "ours/a"}) || !maps.Equal(theirRenames, Renames{"a": "theirs/a"}) {
		t.Fatalf("renames = %v, %v, %v", ourRenames, theirRenames, err)
	}
	for id, reads := range counting.reads {
		if reads != 1 {
			t.Errorf("object %s was read %d times", id, reads)
		}
	}
}

func TestDetectSideRenamesStopsAtAnUnreadableSide(t *testing.T) {
	s := newStore()
	base := writeSnapshot(t, s, Snapshot{"a": s.blob(t, tenLines("a"))})
	missing := hash.ObjectID{1}
	if _, _, err := DetectSideRenames(t.Context(), s, base, missing, base); !errors.Is(err, errStore) {
		t.Errorf("an unreadable ours returned %v", err)
	}
	if _, _, err := DetectSideRenames(t.Context(), s, base, base, missing); !errors.Is(err, errStore) {
		t.Errorf("an unreadable theirs returned %v", err)
	}
}

func TestDetectRenamesLeavesEmptyFilesUnpaired(t *testing.T) {
	s := newStore()
	empty := s.blob(t, "")
	base := writeSnapshot(t, s, Snapshot{"old/__init__.py": empty, "k": s.blob(t, tenLines("k"))})
	side := writeSnapshot(t, s, Snapshot{"new/__init__.py": empty, "k": s.blob(t, tenLines("k"))})

	renames, err := DetectRenames(t.Context(), s, base, side)

	if err != nil || len(renames) != 0 {
		t.Fatalf("renames = %v, %v; want none for an empty file", renames, err)
	}
}
