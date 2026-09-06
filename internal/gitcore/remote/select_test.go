package remote

import (
	"errors"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func mustParseRefSpec(t *testing.T, text string) refspec.RefSpec {
	t.Helper()
	spec, err := refspec.Parse(text)
	if err != nil {
		t.Fatalf("refspec.Parse(%q) returned error %v", text, err)
	}
	return spec
}

func TestSelectMatchesIgnoresTheHeadPseudoRef(t *testing.T) {
	head := hash.SumSHA1("commit", []byte("head"))
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "HEAD", ID: head},
		{Name: "refs/heads/master", ID: head},
	}}
	specs := []refspec.RefSpec{refspec.DefaultFetch("origin")}
	matched, err := selectMatches(adv, specs, TagsNone, newFakeStore())
	if err != nil {
		t.Fatalf("selectMatches returned error %v", err)
	}
	if len(matched) != 1 || matched[0].ref.Name != "refs/heads/master" {
		t.Fatalf("selectMatches returned %+v, want only refs/heads/master", matched)
	}
}

func TestSelectMatchesSkipsARefAlreadyClaimedByAnEarlierRefspec(t *testing.T) {
	head := hash.SumSHA1("commit", []byte("head"))
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	specs := []refspec.RefSpec{
		mustParseRefSpec(t, "refs/heads/master:refs/remotes/origin/master"),
		mustParseRefSpec(t, "+refs/heads/*:refs/remotes/origin/*"),
	}
	matched, err := selectMatches(adv, specs, TagsNone, newFakeStore())
	if err != nil {
		t.Fatalf("selectMatches returned error %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("selectMatches returned %+v, want a single match", matched)
	}
}

func TestSelectMatchesSkipsARepeatedDestinationFromTwoRefspecs(t *testing.T) {
	head := hash.SumSHA1("commit", []byte("head"))
	other := hash.SumSHA1("commit", []byte("other"))
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/a", ID: head},
		{Name: "refs/heads/b", ID: other},
	}}
	specs := []refspec.RefSpec{
		mustParseRefSpec(t, "refs/heads/a:refs/remotes/origin/x"),
		mustParseRefSpec(t, "refs/heads/b:refs/remotes/origin/x"),
	}
	matched, err := selectMatches(adv, specs, TagsNone, newFakeStore())
	if err != nil {
		t.Fatalf("selectMatches returned error %v", err)
	}
	if len(matched) != 1 || matched[0].ref.Name != "refs/heads/a" {
		t.Fatalf("selectMatches returned %+v, want only the first claim of origin/x", matched)
	}
}

func TestSelectMatchesPropagatesTagFollowErrors(t *testing.T) {
	head := hash.SumSHA1("commit", []byte("head"))
	tag := hash.SumSHA1("tag", []byte("tag"))
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/master", ID: head},
		{Name: "refs/tags/v1", ID: tag},
	}}
	specs := []refspec.RefSpec{refspec.DefaultFetch("origin")}
	store := newFakeStore()
	wantErr := errors.New("boom")
	store.hasErr = wantErr
	if _, err := selectMatches(adv, specs, TagsFollow, store); !errors.Is(err, wantErr) {
		t.Fatalf("selectMatches returned %v, want %v", err, wantErr)
	}
}

func TestSelectTagsAllIncludesATagAlreadyMatchedByExplicitRefspec(t *testing.T) {
	head := hash.SumSHA1("commit", []byte("head"))
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/tags/v1", ID: head}}}
	specs := []refspec.RefSpec{mustParseRefSpec(t, "refs/tags/v1:refs/tags/v1")}
	matched, err := selectMatches(adv, specs, TagsAll, newFakeStore())
	if err != nil {
		t.Fatalf("selectMatches returned error %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("selectMatches returned %+v, want the explicit match kept without duplication", matched)
	}
}

func TestTagReachesFetchedCommitUsesTheLightweightTargetWhenNotPeeled(t *testing.T) {
	head := hash.SumSHA1("commit", []byte("head"))
	tips := map[hash.ObjectID]struct{}{head: {}}
	reached, err := tagReachesFetchedCommit(transport.Ref{ID: head}, newFakeStore(), tips)
	if err != nil {
		t.Fatalf("tagReachesFetchedCommit returned error %v", err)
	}
	if !reached {
		t.Fatal("tagReachesFetchedCommit returned false for a lightweight tag on a fetched tip")
	}
}

func TestPruneStaleIgnoresNonWildcardRefspecs(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	gone := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), gone)

	specs := []refspec.RefSpec{mustParseRefSpec(t, "refs/heads/master:refs/remotes/origin/master")}
	stale, err := pruneStale(store, specs, nil)
	if err != nil {
		t.Fatalf("pruneStale returned error %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("pruneStale returned %+v, want none for a non-wildcard refspec", stale)
	}
}

func TestPruneStaleDeduplicatesRepeatedWildcardPrefixes(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	gone := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), gone)

	specs := []refspec.RefSpec{
		refspec.DefaultFetch("origin"),
		refspec.DefaultFetch("origin"),
	}
	stale, err := pruneStale(store, specs, nil)
	if err != nil {
		t.Fatalf("pruneStale returned error %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("pruneStale returned %+v, want a single deduplicated entry", stale)
	}
}

func TestPruneStalePropagatesStoreErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	mustWriteFile(t, r.CommonPath("refs/remotes/origin/broken"), "not-a-valid-ref\n")

	specs := []refspec.RefSpec{refspec.DefaultFetch("origin")}
	if _, err := pruneStale(store, specs, nil); err == nil {
		t.Fatal("pruneStale returned no error for a malformed loose ref")
	}
}

func TestWildcardPrefixReturnsTheWholeStringWithoutAWildcard(t *testing.T) {
	if got := wildcardPrefix("refs/heads/main"); got != "refs/heads/main" {
		t.Fatalf("wildcardPrefix returned %q, want the whole string unchanged", got)
	}
}

func TestPruneStaleSkipsARefspecWhoseDestinationIsBareWildcard(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	gone := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), gone)

	specs := []refspec.RefSpec{mustParseRefSpec(t, "refs/heads/*:*")}
	stale, err := pruneStale(store, specs, nil)
	if err != nil {
		t.Fatalf("pruneStale returned error %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("pruneStale returned %+v, want none for a bare wildcard destination", stale)
	}
}

func TestPruneStaleFindsAMatchWithTheWildcardNotAtTheEnd(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	gone := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "release-stable"), gone)

	specs := []refspec.RefSpec{mustParseRefSpec(t, "refs/heads/*-stable:refs/remotes/origin/*-stable")}
	stale, err := pruneStale(store, specs, nil)
	if err != nil {
		t.Fatalf("pruneStale returned error %v", err)
	}
	if len(stale) != 1 || stale[0].Name != refs.RemoteBranchName("origin", "release-stable") {
		t.Fatalf("pruneStale returned %+v, want the mid-pattern wildcard match", stale)
	}
}

func TestPruneStaleKeepsARefThatWasJustMatched(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	kept := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "kept")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), kept)

	specs := []refspec.RefSpec{refspec.DefaultFetch("origin")}
	matched := []matchedRef{{dst: refs.RemoteBranchName("origin", "master")}}
	stale, err := pruneStale(store, specs, matched)
	if err != nil {
		t.Fatalf("pruneStale returned error %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("pruneStale returned %+v, want the just-matched ref kept", stale)
	}
}

func TestPruneStaleKeepsARefSharingAPrefixButNotMatchingTheWildcardSuffix(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	unrelated := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "unrelated")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "dev"), unrelated)

	specs := []refspec.RefSpec{mustParseRefSpec(t, "refs/heads/*-stable:refs/remotes/origin/*-stable")}
	stale, err := pruneStale(store, specs, nil)
	if err != nil {
		t.Fatalf("pruneStale returned error %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("pruneStale returned %+v, want origin/dev kept since it does not match *-stable", stale)
	}
}
