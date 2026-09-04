package revision

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestWalkStopsAtAShallowCommitBoundary(t *testing.T) {
	b := linearFixture(t)
	delete(b.objects.data, b.id("a"))
	opts := b.options("c")
	opts.Context.Shallow = map[hash.ObjectID]struct{}{b.id("b"): {}}
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"c", "b"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkExposesTheShallowCommitWithoutParents(t *testing.T) {
	b := linearFixture(t)
	delete(b.objects.data, b.id("a"))
	opts := b.options("c")
	opts.Context.Shallow = map[hash.ObjectID]struct{}{b.id("b"): {}}
	for commit, err := range Walk(t.Context(), opts) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		if commit.ID != b.id("b") {
			continue
		}
		if len(commit.Parents) != 0 {
			t.Fatalf("shallow commit carries parents %v, want none", commit.Parents)
		}
	}
}

func TestWalkWithoutAShallowSetStillFailsOnAMissingParent(t *testing.T) {
	b := linearFixture(t)
	delete(b.objects.data, b.id("a"))
	opts := b.options("c")
	if err := walkError(t, Walk(t.Context(), opts)); err == nil {
		t.Fatal("Walk succeeded despite a missing parent object, want an error")
	}
}

func TestWalkTruncatesOnlyTheShallowCommitsOwnParentEdge(t *testing.T) {
	b := mergeFixture(t)
	delete(b.objects.data, b.id("a"))
	opts := b.options("merge")
	opts.Context.Shallow = map[hash.ObjectID]struct{}{b.id("side"): {}}
	if err := walkError(t, Walk(t.Context(), opts)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Walk returned %v, want %v (main1 still reaches the missing parent)", err, ErrNotFound)
	}
}
