package revision

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func mustID(t *testing.T, seed string) hash.ObjectID {
	t.Helper()
	text := seed
	for len(text) < hash.HexSize {
		text += "0"
	}
	id, err := hash.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReflogValueCountsEntriesFromTheNewest(t *testing.T) {
	entries := []refs.ReflogEntry{
		{Old: mustID(t, "aa"), New: mustID(t, "bb")},
		{Old: hash.Zero, New: mustID(t, "cc")},
		{Old: mustID(t, "cc"), New: mustID(t, "dd")},
	}
	for num, want := range map[int]hash.ObjectID{0: mustID(t, "dd"), 1: mustID(t, "cc"), 2: mustID(t, "bb"), 3: mustID(t, "aa")} {
		got, err := reflogValue(refs.HEAD, entries, num)
		if err != nil || got != want {
			t.Fatalf("reflogValue(%d) = %s, %v; want %s", num, got, err, want)
		}
	}
	if _, err := reflogValue(refs.HEAD, entries, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reflogValue past the oldest entry returned %v", err)
	}
	created := []refs.ReflogEntry{{Old: hash.Zero, New: mustID(t, "ee")}}
	if _, err := reflogValue(refs.HEAD, created, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reflogValue before the ref existed returned %v", err)
	}
}
