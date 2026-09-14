package ops

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestTheWalkNamesTreeEntriesByTheirPathLikeGit(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)

	walk := mustWalk(t, r)

	for id, path := range map[hash.ObjectID]string{h.blobA: "dir/a.txt", h.subtree: "dir", h.blobB: "b.txt"} {
		if got, want := walk.names[id], pack.NameHash(path); got != want {
			t.Fatalf("the name hash of %s is %#x, want %#x", path, got, want)
		}
	}
	for _, id := range []hash.ObjectID{h.first, h.second, h.tree1, h.tree2, h.tag} {
		if _, named := walk.names[id]; named {
			t.Fatalf("%s carries a path name", id)
		}
	}
}

func similarHistoryRepo(t *testing.T, config string) (*testRepo, []hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t)
	if config != "" {
		r.appendConfig(config)
		r.repo = r.reopen()
	}
	var text strings.Builder
	for line := range 400 {
		fmt.Fprintf(&text, "line %03d of a file big enough to split\n", line)
	}
	first := putMaintBlob(t, r, text.String())
	second := putMaintBlob(t, r, text.String()+"one more line\n")
	older := putMaintCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "big.txt", ID: first}))
	newer := putMaintCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "big.txt", ID: second}), older)
	setMaintRef(t, r, refs.BranchName("main"), newer)
	return r, []hash.ObjectID{first, second}
}

func packedDeltas(t *testing.T, r *testRepo, name string, ids []hash.ObjectID) int {
	t.Helper()
	index, err := pack.OpenIndex(filepath.Join(r.repo.PackDir(), name+".idx"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = index.Close() }()
	packfile, err := pack.OpenPack(filepath.Join(r.repo.PackDir(), name+".pack"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = packfile.Close() }()
	deltas := 0
	for _, id := range ids {
		offset, ok, err := index.Lookup(id)
		if err != nil || !ok {
			t.Fatalf("Lookup(%s) = (%v, %v)", id, ok, err)
		}
		head, err := packfile.HeaderAt(offset)
		if err != nil {
			t.Fatal(err)
		}
		if head.Kind.IsDelta() {
			deltas++
		}
	}
	return deltas
}

func TestRepackKeepsObjectsAboveTheBigFileThresholdWhole(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
		deltas int
	}{
		{"by default", "", 1},
		{"with a threshold below their size", "[core]\n\tbigFileThreshold = 1k\n", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, blobs := similarHistoryRepo(t, tt.config)

			result, err := Repack(t.Context(), r.repo, RepackOptions{})
			if err != nil {
				t.Fatal(err)
			}

			if got := packedDeltas(t, r, result.Pack, blobs); got != tt.deltas {
				t.Fatalf("%d of the similar blobs are deltas, want %d", got, tt.deltas)
			}
		})
	}
}

func TestRepackingAgainRewritesTheSamePack(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	first, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	second, err := Repack(t.Context(), r.repo, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if second.Pack != first.Pack || second.Bytes != first.Bytes || len(second.Removed) != 0 || len(second.Busy) != 0 {
		t.Fatalf("first = %+v, second = %+v", first, second)
	}
	if onlyPack(t, r) != first.Pack {
		t.Fatal("the pack was replaced by another one")
	}
}
