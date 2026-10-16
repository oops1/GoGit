package revision

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func walkAll(t *testing.T, opts Options) int {
	t.Helper()
	count := 0
	for _, err := range Walk(t.Context(), opts) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		count++
	}
	return count
}

func TestWalksSharingParsedObjectsReadEachCommitOnce(t *testing.T) {
	b := newBuilder(t)
	b.commit("a")
	b.commit("b", "a")
	b.commit("c", "b")
	source := b.context()
	source.Parsed = NewParsedObjects()

	first := walkAll(t, Options{Context: source, Include: []hash.ObjectID{b.id("c")}})
	reads := b.objects.gets
	second := walkAll(t, Options{Context: source, Include: []hash.ObjectID{b.id("c")}})

	if first != 3 || second != 3 {
		t.Fatalf("the walks listed %d and %d commits, want three each", first, second)
	}
	if b.objects.gets != reads {
		t.Fatalf("the second walk read %d objects, want none", b.objects.gets-reads)
	}
}

func TestWalksWithoutSharedParsedObjectsReadAgain(t *testing.T) {
	b := newBuilder(t)
	b.commit("a")
	b.commit("b", "a")
	source := b.context()

	walkAll(t, Options{Context: source, Include: []hash.ObjectID{b.id("b")}})
	reads := b.objects.gets
	walkAll(t, Options{Context: source, Include: []hash.ObjectID{b.id("b")}})

	if b.objects.gets == reads {
		t.Fatal("a walk without shared parsed objects read nothing")
	}
}
