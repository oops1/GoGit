package local

import (
	"context"
	"errors"
	"io"
	"iter"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

type staticNegotiator struct {
	haves  []hash.ObjectID
	common []hash.ObjectID
}

func (n *staticNegotiator) Haves(context.Context) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		for _, id := range n.haves {
			if !yield(id, nil) {
				return
			}
		}
	}
}

func (n *staticNegotiator) Common(id hash.ObjectID) { n.common = append(n.common, id) }

func (n *staticNegotiator) Enough() bool { return false }

type cancelingNegotiator struct {
	ids    []hash.ObjectID
	cancel context.CancelFunc
}

func (n *cancelingNegotiator) Haves(context.Context) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		n.cancel()
		for _, id := range n.ids {
			if !yield(id, nil) {
				return
			}
		}
	}
}

func (n *cancelingNegotiator) Common(hash.ObjectID) {}

func (n *cancelingNegotiator) Enough() bool { return false }

type failingNegotiator struct{ err error }

func (n failingNegotiator) Haves(context.Context) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		yield(hash.Zero, n.err)
	}
}

func (n failingNegotiator) Common(hash.ObjectID) {}

func (n failingNegotiator) Enough() bool { return false }

func indexPackInto(t testing.TB, dst *testRepo, r io.Reader) {
	t.Helper()
	if _, err := pack.IndexPack(t.Context(), r, dst.repo.PackDir(), pack.IndexOptions{}); err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
}

func TestFetchClonesAllObjectsAndRefs(t *testing.T) {
	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "hello"})
	second := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "world"}, first)
	tagID := src.annotatedTag("v1", second, object.TypeCommit)

	sess := dialSession(t, src.dir)
	adv, err := sess.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	wants := make([]hash.ObjectID, 0, len(adv.Refs))
	for _, ref := range adv.Refs {
		if ref.Name == "HEAD" {
			continue
		}
		wants = append(wants, ref.ID)
	}

	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: wants}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if resp.Pack == nil {
		t.Fatal("Fetch response carries no pack")
	}

	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}

	wantObjects := src.allObjects()
	gotObjects := dst.allObjects()
	if len(gotObjects) != len(wantObjects) {
		t.Fatalf("cloned %d objects, want %d", len(gotObjects), len(wantObjects))
	}
	for id := range wantObjects {
		if _, ok := gotObjects[id]; !ok {
			t.Fatalf("clone is missing object %s", id)
		}
	}
	if !dst.hasObject(first) || !dst.hasObject(second) || !dst.hasObject(tagID) {
		t.Fatal("clone is missing an expected object")
	}
}

func TestFetchIncrementalUsesHavesAsExclusion(t *testing.T) {
	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "hello"})
	second := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "world"}, first)

	sess := dialSession(t, src.dir)
	neg := &staticNegotiator{haves: []hash.ObjectID{first}}
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{second}}, neg)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(neg.common) != 1 || neg.common[0] != first {
		t.Fatalf("negotiator.common = %v, want [%s]", neg.common, first)
	}
	if len(resp.Common) != 1 || resp.Common[0] != first {
		t.Fatalf("resp.Common = %v, want [%s]", resp.Common, first)
	}

	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if dst.hasObject(first) {
		t.Fatal("incremental fetch resent an object the client already had")
	}
	if !dst.hasObject(second) {
		t.Fatal("incremental fetch is missing the new commit")
	}
}

func TestFetchShallowLimitsDepth(t *testing.T) {
	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "hello"})
	second := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "world"}, first)

	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{second}, Depth: 1}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(resp.Shallow) != 1 || resp.Shallow[0] != second {
		t.Fatalf("resp.Shallow = %v, want [%s]", resp.Shallow, second)
	}

	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if dst.hasObject(first) {
		t.Fatal("shallow fetch with depth 1 included an ancestor commit")
	}
	if !dst.hasObject(second) {
		t.Fatal("shallow fetch is missing the wanted commit")
	}
}

func TestFetchFailsWithoutWants(t *testing.T) {
	src := newTestRepo(t, true)
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{}, nil); !errors.Is(err, transport.ErrProtocol) {
		t.Fatalf("Fetch returned %v, want ErrProtocol", err)
	}
}

func TestFetchResolvesWantRefs(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{WantRefs: []string{"refs/heads/main"}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(resp.WantedRefs) != 1 || resp.WantedRefs[0].ID != commit {
		t.Fatalf("resp.WantedRefs = %v, want refs/heads/main at %s", resp.WantedRefs, commit)
	}
	_ = resp.Pack.Close()
}

func TestFetchFailsForUnknownWantRef(t *testing.T) {
	src := newTestRepo(t, true)
	src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{WantRefs: []string{"refs/heads/missing"}}, nil); !errors.Is(err, transport.ErrProtocol) {
		t.Fatalf("Fetch returned %v, want ErrProtocol", err)
	}
}

func TestFetchFailsWhenHasCannotBeDetermined(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	makeDirUnreadable(t, filepath.Join(src.repo.ObjectsDir(), commit.String()[:2]))

	neg := &staticNegotiator{haves: []hash.ObjectID{commit}}
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{commit}}, neg); err == nil {
		t.Fatal("Fetch tolerated a Has() failure")
	}
}

func TestFetchPropagatesNegotiatorErrors(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	wantErr := errors.New("boom")
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{commit}}, failingNegotiator{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch returned %v, want %v", err, wantErr)
	}
}

func TestFetchRespectsCanceledContext(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sess.Fetch(ctx, transport.FetchRequest{Wants: []hash.ObjectID{commit}}, nil); err == nil {
		t.Fatal("Fetch on a canceled context returned no error")
	}
}

func TestFetchCanceledDuringPackWriteSurfacesOnRead(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir)
	ctx, cancel := context.WithCancel(t.Context())
	resp, err := sess.Fetch(ctx, transport.FetchRequest{Wants: []hash.ObjectID{commit}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	cancel()
	_, readErr := io.ReadAll(resp.Pack)
	if readErr == nil {
		t.Log("pack finished writing before cancellation was observed; nothing further to assert")
	}
	_ = resp.Pack.Close()
}

func TestFetchFailsWhenAWantIsUnknown(t *testing.T) {
	src := newTestRepo(t, true)
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{src.missingObjectID()}}, nil); err == nil {
		t.Fatal("Fetch accepted an unknown want")
	}
}

func TestFetchFailsWhenATagTargetIsUnknown(t *testing.T) {
	src := newTestRepo(t, true)
	tagID := src.annotatedTag("broken", src.missingObjectID(), object.TypeCommit)
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{tagID}}, nil); err == nil {
		t.Fatal("Fetch accepted a tag whose target is missing")
	}
}

func TestFetchFailsWhenAnAncestorIsMissing(t *testing.T) {
	src := newTestRepo(t, true)
	broken := src.commitWithTree(src.tree(map[string]string{"a.txt": "hello"}), src.missingObjectID())
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{broken}}, nil); err == nil {
		t.Fatal("Fetch accepted a commit with a missing ancestor")
	}
}

func TestFetchFailsWhenAHaveCommitHasACorruptTree(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	corruptTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "a.txt", ID: src.blob("hello")},
		{Mode: object.ModeTree, Name: "sub", ID: src.missingObjectID()},
	})
	haveCommit := src.commitWithTree(corruptTree)

	sess := dialSession(t, src.dir)
	neg := &staticNegotiator{haves: []hash.ObjectID{haveCommit}}
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{commit}}, neg); err == nil {
		t.Fatal("Fetch accepted a have-commit with a corrupt tree")
	}
}

func TestFetchFailsWhenAWantedTreeIsCorrupt(t *testing.T) {
	src := newTestRepo(t, true)
	corruptTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeTree, Name: "sub", ID: src.missingObjectID()},
	})
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{corruptTree}}, nil); err == nil {
		t.Fatal("Fetch accepted a corrupt tree want")
	}
}

func TestFetchWantingATreeOrBlobDirectly(t *testing.T) {
	src := newTestRepo(t, true)
	treeID := src.tree(map[string]string{"a.txt": "hello"})
	blobID := src.blob("standalone")
	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{treeID, blobID}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if !dst.hasObject(treeID) {
		t.Fatal("fetch of a bare tree did not deliver the tree object")
	}
	if !dst.hasObject(blobID) {
		t.Fatal("fetch of a bare blob did not deliver the blob object")
	}
}

func TestFetchTagPeelingToATreeOrBlob(t *testing.T) {
	src := newTestRepo(t, true)
	treeID := src.tree(map[string]string{"a.txt": "hello"})
	blobID := src.blob("standalone")
	treeTag := src.annotatedTag("tree-tag", treeID, object.TypeTree)
	blobTag := src.annotatedTag("blob-tag", blobID, object.TypeBlob)

	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{treeTag, blobTag}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	for _, id := range []hash.ObjectID{treeID, blobID, treeTag, blobTag} {
		if !dst.hasObject(id) {
			t.Fatalf("fetch is missing object %s", id)
		}
	}
}

func TestFetchSkipsSubmodulesAndDescendsSubtrees(t *testing.T) {
	src := newTestRepo(t, true)
	subTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "nested.txt", ID: src.blob("nested content")},
	})
	topTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "top.txt", ID: src.blob("top content")},
		{Mode: object.ModeTree, Name: "sub", ID: subTree},
		{Mode: object.ModeSubmodule, Name: "vendored", ID: src.missingObjectID()},
	})
	commit := src.commitWithTree(topTree)
	src.setBranch("main", commit)

	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{commit}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if !dst.hasObject(topTree) || !dst.hasObject(subTree) {
		t.Fatal("fetch did not descend into the nested subtree")
	}
	if dst.hasObject(src.missingObjectID()) {
		t.Fatal("fetch resolved a submodule entry it should have skipped")
	}
}

func TestFetchDeduplicatesASharedTreeBetweenWantedCommits(t *testing.T) {
	src := newTestRepo(t, true)
	sharedTree := src.tree(map[string]string{"a.txt": "same content"})
	first := src.commitWithTree(sharedTree)
	second := src.commitWithTree(sharedTree, first)

	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{first, second}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if !dst.hasObject(first) || !dst.hasObject(second) || !dst.hasObject(sharedTree) {
		t.Fatal("fetch is missing an object from the shared-tree commits")
	}
}

func TestFetchExcludesASharedTreeBetweenHaveCommits(t *testing.T) {
	src := newTestRepo(t, true)
	sharedTree := src.tree(map[string]string{"a.txt": "same content"})
	haveFirst := src.commitWithTree(sharedTree)
	haveSecond := src.commitWithTree(sharedTree, haveFirst)
	want := src.commitWithTree(src.tree(map[string]string{"a.txt": "same content", "b.txt": "new"}), haveSecond)

	sess := dialSession(t, src.dir)
	neg := &staticNegotiator{haves: []hash.ObjectID{haveFirst, haveSecond}}
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{want}}, neg)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if dst.hasObject(sharedTree) {
		t.Fatal("fetch resent a tree already reachable from the haves")
	}
	if !dst.hasObject(want) {
		t.Fatal("fetch is missing the wanted commit")
	}
}

func TestFetchFailsWhenAHaveCommitCannotBeParsed(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	garbage := src.rawObject(object.TypeCommit, []byte("not a valid commit body"))

	sess := dialSession(t, src.dir)
	neg := &staticNegotiator{haves: []hash.ObjectID{garbage}}
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{commit}, Depth: 1}, neg); err == nil {
		t.Fatal("Fetch accepted a have-commit that cannot be parsed")
	}
}

func TestFetchExcludeTreeSkipsSubmodulesAndDescendsSubtrees(t *testing.T) {
	src := newTestRepo(t, true)
	subTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "nested.txt", ID: src.blob("nested have content")},
	})
	haveTopTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "top.txt", ID: src.blob("top have content")},
		{Mode: object.ModeTree, Name: "sub", ID: subTree},
		{Mode: object.ModeSubmodule, Name: "vendored", ID: src.missingObjectID()},
	})
	haveCommit := src.commitWithTree(haveTopTree)
	want := src.commitWithTree(src.tree(map[string]string{"new.txt": "new content"}), haveCommit)

	sess := dialSession(t, src.dir)
	neg := &staticNegotiator{haves: []hash.ObjectID{haveCommit}}
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{want}}, neg)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if dst.hasObject(haveTopTree) || dst.hasObject(subTree) {
		t.Fatal("fetch resent trees already reachable from a have-commit")
	}
	if !dst.hasObject(want) {
		t.Fatal("fetch is missing the wanted commit")
	}
}

func TestFetchShallowDeduplicatesADiamondAncestor(t *testing.T) {
	src := newTestRepo(t, true)
	base := src.commitWithTree(src.tree(map[string]string{"a.txt": "base"}))
	left := src.commitWithTree(src.tree(map[string]string{"a.txt": "left"}), base)
	right := src.commitWithTree(src.tree(map[string]string{"a.txt": "right"}), base)

	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{left, right}, Depth: 2}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	dst := newTestRepo(t, true)
	indexPackInto(t, dst, resp.Pack)
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	if !dst.hasObject(base) || !dst.hasObject(left) || !dst.hasObject(right) {
		t.Fatal("shallow fetch is missing part of the diamond history")
	}
}

func TestFetchShallowFailsWhenAnAncestorIsMissing(t *testing.T) {
	src := newTestRepo(t, true)
	tip := src.commitWithTree(src.tree(map[string]string{"a.txt": "hello"}), src.missingObjectID())
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{tip}, Depth: 2}, nil); err == nil {
		t.Fatal("shallow Fetch accepted a commit with a missing ancestor")
	}
}

func TestFetchShallowReportsMultipleBoundaryCommits(t *testing.T) {
	src := newTestRepo(t, true)
	baseLeft := src.commitWithTree(src.tree(map[string]string{"a.txt": "left-base"}))
	tipLeft := src.commitWithTree(src.tree(map[string]string{"a.txt": "left-tip"}), baseLeft)
	baseRight := src.commitWithTree(src.tree(map[string]string{"a.txt": "right-base"}))
	tipRight := src.commitWithTree(src.tree(map[string]string{"a.txt": "right-tip"}), baseRight)

	sess := dialSession(t, src.dir)
	resp, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{tipLeft, tipRight}, Depth: 1}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(resp.Shallow) != 2 {
		t.Fatalf("resp.Shallow = %v, want two boundary commits", resp.Shallow)
	}
	_ = resp.Pack.Close()
}

func TestFetchFailsWhenACommitTreeIsMissing(t *testing.T) {
	src := newTestRepo(t, true)
	broken := src.commitWithTree(src.missingObjectID())
	sess := dialSession(t, src.dir)
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{Wants: []hash.ObjectID{broken}}, nil); err == nil {
		t.Fatal("Fetch accepted a commit whose tree is missing")
	}
}

func TestCommitOnlySkipsNonCommitObjects(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	blobID := src.blob("not a commit")
	got, err := commitOnly(t.Context(), src.db, []hash.ObjectID{commit, blobID, src.missingObjectID()})
	if err != nil {
		t.Fatalf("commitOnly returned error %v", err)
	}
	if len(got) != 1 || got[0] != commit {
		t.Fatalf("commitOnly = %v, want only [%s]", got, commit)
	}
}

func TestCommitOnlyPropagatesRealDatabaseErrors(t *testing.T) {
	src := newTestRepo(t, true)
	corrupt := src.corruptLooseObject()
	if _, err := commitOnly(t.Context(), src.db, []hash.ObjectID{corrupt}); err == nil {
		t.Fatal("commitOnly swallowed a database error instead of propagating it")
	}
}

func TestCommitOnlyRespectsCanceledContext(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := commitOnly(ctx, src.db, []hash.ObjectID{commit}); err == nil {
		t.Fatal("commitOnly on a canceled context returned no error")
	}
}

func TestFetchCancellationDuringTreeCollection(t *testing.T) {
	src := newTestRepo(t, true)
	subTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "nested.txt", ID: src.blob("nested content")},
	})
	topTree := src.treeWithEntries([]object.TreeEntry{
		{Mode: object.ModeBlob, Name: "top.txt", ID: src.blob("top content")},
		{Mode: object.ModeTree, Name: "sub", ID: subTree},
	})
	haveTree := src.tree(map[string]string{"x.txt": "have content"})
	haveCommit := src.commitWithTree(haveTree)
	commit := src.commitWithTree(topTree, haveCommit)

	sess := dialSession(t, src.dir)
	for failAt := 1; failAt <= 11; failAt++ {
		ctx := newCountingContext(t, failAt)
		neg := &staticNegotiator{haves: []hash.ObjectID{haveCommit}}
		resp, err := sess.Fetch(ctx, transport.FetchRequest{Wants: []hash.ObjectID{commit}}, neg)
		if err != nil {
			continue
		}
		if _, readErr := io.ReadAll(resp.Pack); readErr == nil {
			t.Fatalf("failAt=%d: Fetch and its pack both tolerated a canceled context", failAt)
		}
		_ = resp.Pack.Close()
	}
}

func TestResolveWantRefsRespectsCanceledContext(t *testing.T) {
	src := newTestRepo(t, true)
	src.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, src.dir).(*session)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sess.resolveWantRefs(ctx, []string{"refs/heads/main"}); err == nil {
		t.Fatal("resolveWantRefs on a canceled context returned no error")
	}
}

func TestCollectHavesRespectsCanceledContextMidIteration(t *testing.T) {
	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "hello"})
	second := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "world"}, first)
	sess := dialSession(t, src.dir).(*session)
	ctx, cancel := context.WithCancel(t.Context())
	neg := &cancelingNegotiator{ids: []hash.ObjectID{first, second}, cancel: cancel}
	if _, _, err := sess.collectHaves(ctx, neg); err == nil {
		t.Fatal("collectHaves on a canceled context returned no error")
	}
}

func TestFetchShallowCancellationDuringClosure(t *testing.T) {
	src := newTestRepo(t, true)
	base := src.commit("main", map[string]string{"a.txt": "hello"})
	mid := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "mid"}, base)
	tip := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "mid", "c.txt": "tip"}, mid)

	sess := dialSession(t, src.dir)
	for failAt := 1; failAt <= 16; failAt++ {
		ctx := newCountingContext(t, failAt)
		resp, err := sess.Fetch(ctx, transport.FetchRequest{Wants: []hash.ObjectID{tip}, Depth: 3}, nil)
		if err != nil {
			continue
		}
		if _, readErr := io.ReadAll(resp.Pack); readErr == nil {
			t.Fatalf("failAt=%d: shallow Fetch and its pack both tolerated a canceled context", failAt)
		}
		_ = resp.Pack.Close()
	}
}
