package pack

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func twoPackStore(t *testing.T) (*Store, *PackFile, *PackFile) {
	t.Helper()
	names := fixtureNames(t)
	store := openStore(t, copyFixtureDir(t, names...))
	files := store.Files()
	if len(files) != 2 {
		t.Fatalf("the store lists %d packfiles, want two", len(files))
	}
	return store, files[0], files[1]
}

func sharedObject(t *testing.T, first, second *PackFile) hash.ObjectID {
	t.Helper()
	entry, err := second.Index.EntryAt(0)
	if err != nil {
		t.Fatalf("EntryAt returned error %v", err)
	}
	for _, file := range []*PackFile{first, second} {
		if _, found, err := file.Index.Lookup(entry.ID); !found || err != nil {
			t.Fatalf("%s does not hold %s (%v, %v)", file.Name, entry.ID, found, err)
		}
	}
	return entry.ID
}

func TestStoreSearchesTheMostRecentlyUsedPackfileFirst(t *testing.T) {
	store, newest, oldest := twoPackStore(t)
	id := sharedObject(t, newest, oldest)
	if holder, _, _ := store.Holder(id); holder != newest.Name {
		t.Fatalf("before any hit the holder is %s, want the newest %s", holder, newest.Name)
	}

	for file := range store.Acquire() {
		if file == oldest {
			break
		}
	}

	if holder, found, err := store.Holder(id); err != nil || !found || holder != oldest.Name {
		t.Fatalf("after a hit in %s the holder is %q (%v, %v)", oldest.Name, holder, found, err)
	}
	if files := store.Files(); files[0] != newest || files[1] != oldest {
		t.Fatalf("Files lists %s then %s, want the listing to stay newest first", files[0].Name, files[1].Name)
	}
}

func TestStoreKeepsTheOrderWhenNothingIsHotOrTheHotPackIsAlreadyFirst(t *testing.T) {
	store, newest, oldest := twoPackStore(t)
	files := store.Files()

	if got := store.preferHot(files); got[0] != newest || got[1] != oldest {
		t.Fatalf("an order without a hot packfile changed to %s, %s", got[0].Name, got[1].Name)
	}
	store.hot.Store(newest)
	if got := store.preferHot(files); got[0] != newest || got[1] != oldest {
		t.Fatalf("an order that already starts with the hot packfile changed to %s, %s", got[0].Name, got[1].Name)
	}
	store.hot.Store(&PackFile{Name: "gone"})
	if got := store.preferHot(files); got[0] != newest || got[1] != oldest {
		t.Fatalf("a hot packfile that is no longer listed changed the order to %s, %s", got[0].Name, got[1].Name)
	}
}

func TestStoreMovesTheHotPackfileToTheFrontWithoutLosingTheOthers(t *testing.T) {
	store, newest, oldest := twoPackStore(t)
	store.hot.Store(oldest)

	got := store.preferHot(store.Files())

	if len(got) != 2 || got[0] != oldest || got[1] != newest {
		t.Fatalf("the search order is %v, want the hot packfile first and the other after it", got)
	}
}

func TestStoreForgetsTheHotPackfileWhenItReloads(t *testing.T) {
	store, _, oldest := twoPackStore(t)
	store.hot.Store(oldest)

	if _, err := store.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}

	if store.hot.Load() != nil {
		t.Fatal("the store kept pointing at a packfile after it reloaded")
	}
}

func TestStoreStillReportsAnUnknownObjectWhenAPackfileIsHot(t *testing.T) {
	store, _, oldest := twoPackStore(t)
	store.hot.Store(oldest)

	if _, found, err := store.Holder(hash.ObjectID{0xee}); found || err != nil {
		t.Fatalf("Holder of an unknown object = %v, %v", found, err)
	}
}
