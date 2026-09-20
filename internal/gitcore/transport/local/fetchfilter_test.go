package local

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func fetchedObjects(t *testing.T, src *testRepo, req transport.FetchRequest) map[hash.ObjectID]struct{} {
	t.Helper()
	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), req, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	return dst.allObjects()
}

func TestFetchLeavesOutTheBlobsTheFilterOmits(t *testing.T) {
	src := newTestRepo(t, true)
	small := src.blob("tiny")
	large := src.blob("a blob well past the configured limit")
	nested := src.blob("nested and also past the configured limit")
	inner := src.treeWithEntries([]object.TreeEntry{{Mode: object.ModeBlob, Name: "n.txt", ID: nested}})
	root := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "small.txt", ID: small},
		{Mode: object.ModeBlob, Name: "large.txt", ID: large},
		{Mode: object.ModeTree, Name: "sub", ID: inner},
	})
	head := src.commitWithTree(root)
	src.setBranch("main", head)

	tests := []struct {
		name    string
		filter  string
		present []hash.ObjectID
		absent  []hash.ObjectID
	}{
		{"blobNone", "blob:none", []hash.ObjectID{head, root, inner}, []hash.ObjectID{small, large, nested}},
		{"blobLimit", "blob:limit=8", []hash.ObjectID{head, root, inner, small}, []hash.ObjectID{large, nested}},
		{"treeRoot", "tree:0", []hash.ObjectID{head}, []hash.ObjectID{root, inner, small, large, nested}},
		{"treeOne", "tree:1", []hash.ObjectID{head, root}, []hash.ObjectID{inner, small, large, nested}},
		{"none", "", []hash.ObjectID{head, root, inner, small, large, nested}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fetchedObjects(t, src, transport.FetchRequest{Wants: []hash.ObjectID{head}, Filter: tc.filter})
			for _, id := range tc.present {
				if _, ok := got[id]; !ok {
					t.Fatalf("the pack is missing %s", id)
				}
			}
			for _, id := range tc.absent {
				if _, ok := got[id]; ok {
					t.Fatalf("the pack carries %s although the filter omits it", id)
				}
			}
		})
	}
}

func TestFetchWeighsABlobSharedByTwoTreesOnlyOnce(t *testing.T) {
	src := newTestRepo(t, true)
	shared := src.blob("small")
	inner := src.treeWithEntries([]object.TreeEntry{{Mode: object.ModeBlob, Name: "n.txt", ID: shared}})
	root := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "a.txt", ID: shared},
		{Mode: object.ModeTree, Name: "sub", ID: inner},
	})
	head := src.commitWithTree(root)
	src.setBranch("main", head)

	got := fetchedObjects(t, src, transport.FetchRequest{Wants: []hash.ObjectID{head}, Filter: "blob:limit=8"})
	if _, ok := got[shared]; !ok {
		t.Fatalf("the pack is missing the shared blob %s", shared)
	}
}

func TestFetchSendsTheBlobsAskedForByNameDespiteTheFilter(t *testing.T) {
	src := newTestRepo(t, true)
	wanted := src.blob("fetched on demand")
	root := src.treeWithEntries([]object.TreeEntry{{Mode: object.ModeBlob, Name: "a.txt", ID: wanted}})
	head := src.commitWithTree(root)
	src.setBranch("main", head)

	sess := dialSession(t, src.dir)
	neg := &staticNegotiator{haves: []hash.ObjectID{head}}
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{wanted}, Filter: "blob:none"}, neg)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if !dst.hasObject(wanted) {
		t.Fatalf("the pack is missing the blob %s that was asked for by name", wanted)
	}
}

func TestFetchRejectsAFilterItCannotApply(t *testing.T) {
	src := newTestRepo(t, true)
	head := src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	_, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{head}, Filter: "sparse:oid=HEAD"}, nil)
	if !errors.Is(err, transport.ErrUnsupportedFilter) {
		t.Fatalf("Fetch returned %v, want ErrUnsupportedFilter", err)
	}
}

func TestFetchReportsAFailureWhileSizingAFilteredBlob(t *testing.T) {
	src := newTestRepo(t, true)
	broken := src.corruptLooseObject()
	root := src.treeWithEntries([]object.TreeEntry{{Mode: object.ModeBlob, Name: "a.txt", ID: broken}})
	head := src.commitWithTree(root)
	src.setBranch("main", head)
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{head}, Filter: "blob:limit=8"}, nil); err == nil {
		t.Fatal("Fetch returned nil, want the failure of reading the corrupt blob")
	}
}
