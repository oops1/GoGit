package remote

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestNewNegotiatorHasNoCandidatesForAnEmptyRepository(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	neg, err := newNegotiator(t.Context(), store, db, nil)
	if err != nil {
		t.Fatalf("newNegotiator returned error %v", err)
	}
	if !neg.Enough() {
		t.Fatal("Enough returned false with no local refs")
	}
	var haves []hash.ObjectID
	for id, err := range neg.Haves(t.Context()) {
		if err != nil {
			t.Fatalf("Haves yielded error %v", err)
		}
		haves = append(haves, id)
	}
	if len(haves) != 0 {
		t.Fatalf("Haves yielded %v, want none", haves)
	}
}

func TestNewNegotiatorOrdersCandidatesByCommitDateDescending(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := time.Unix(1_700_000_000, 0).UTC()
	first := putLocalCommit(t, db, base, "first")
	second := putLocalCommit(t, db, base.Add(time.Hour), "second", first)
	setLocalBranch(t, store, refs.BranchName("master"), second)
	setLocalHead(t, store, refs.BranchName("master"))

	neg, err := newNegotiator(t.Context(), store, db, nil)
	if err != nil {
		t.Fatalf("newNegotiator returned error %v", err)
	}
	var haves []hash.ObjectID
	for id, err := range neg.Haves(t.Context()) {
		if err != nil {
			t.Fatalf("Haves yielded error %v", err)
		}
		haves = append(haves, id)
	}
	if len(haves) != 2 || haves[0] != second || haves[1] != first {
		t.Fatalf("Haves yielded %v, want [%s %s]", haves, second, first)
	}
}

func TestNegotiatorCommonExcludesTheMarkedCandidate(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := time.Unix(1_700_000_000, 0).UTC()
	first := putLocalCommit(t, db, base, "first")
	second := putLocalCommit(t, db, base.Add(time.Hour), "second", first)
	setLocalBranch(t, store, refs.BranchName("master"), second)

	neg, err := newNegotiator(t.Context(), store, db, nil)
	if err != nil {
		t.Fatalf("newNegotiator returned error %v", err)
	}
	if neg.Enough() {
		t.Fatal("Enough returned true before any common ack")
	}
	neg.Common(second)
	if !neg.Enough() {
		t.Fatal("Enough returned false after a common ack")
	}
	var haves []hash.ObjectID
	for id, err := range neg.Haves(t.Context()) {
		if err != nil {
			t.Fatalf("Haves yielded error %v", err)
		}
		haves = append(haves, id)
	}
	if len(haves) != 1 || haves[0] != first {
		t.Fatalf("Haves yielded %v, want only %s", haves, first)
	}
}

func TestNegotiatorHavesStopsOnCancelledContext(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	first := putLocalCommit(t, db, time.Unix(1_700_000_000, 0).UTC(), "first")
	setLocalBranch(t, store, refs.BranchName("master"), first)

	neg, err := newNegotiator(t.Context(), store, db, nil)
	if err != nil {
		t.Fatalf("newNegotiator returned error %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var sawErr error
	for _, err := range neg.Haves(ctx) {
		sawErr = err
		break
	}
	if !errors.Is(sawErr, context.Canceled) {
		t.Fatalf("Haves yielded %v, want context.Canceled", sawErr)
	}
}

func TestNewNegotiatorPropagatesWalkErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	dangling := hash.SumSHA1("commit", []byte("dangling"))
	setLocalBranch(t, store, refs.BranchName("master"), dangling)

	if _, err := newNegotiator(t.Context(), store, db, nil); err == nil {
		t.Fatal("newNegotiator returned no error for a branch pointing at a missing commit")
	}
}

func TestLocalTipsPropagatesPrefixErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	mustWriteFile(t, r.CommonPath("refs/heads/broken"), "not-a-valid-ref\n")

	if _, err := localTips(store); err == nil {
		t.Fatal("localTips returned no error for a malformed loose branch ref")
	}
}

func TestNegotiatorHavesStopsWhenTheConsumerStopsIterating(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := time.Unix(1_700_000_000, 0).UTC()
	first := putLocalCommit(t, db, base, "first")
	second := putLocalCommit(t, db, base.Add(time.Hour), "second", first)
	setLocalBranch(t, store, refs.BranchName("master"), second)

	neg, err := newNegotiator(t.Context(), store, db, nil)
	if err != nil {
		t.Fatalf("newNegotiator returned error %v", err)
	}
	var haves []hash.ObjectID
	for id, err := range neg.Haves(t.Context()) {
		if err != nil {
			t.Fatalf("Haves yielded error %v", err)
		}
		haves = append(haves, id)
		break
	}
	if len(haves) != 1 || haves[0] != second {
		t.Fatalf("Haves yielded %v after an early stop, want only %s", haves, second)
	}
}

func TestNewNegotiatorCapsCandidatesInsteadOfWalkingTheWholeHistory(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := time.Unix(1_700_000_000, 0).UTC()
	var tip hash.ObjectID
	var parents []hash.ObjectID
	const chainLength = maxHaveCandidates + 50
	for i := range chainLength {
		tip = putLocalCommit(t, db, base.Add(time.Duration(i)*time.Minute), "c", parents...)
		parents = []hash.ObjectID{tip}
	}
	setLocalBranch(t, store, refs.BranchName("master"), tip)

	neg, err := newNegotiator(t.Context(), store, db, nil)
	if err != nil {
		t.Fatalf("newNegotiator returned error %v", err)
	}
	if len(neg.candidates) != maxHaveCandidates {
		t.Fatalf("newNegotiator produced %d candidates from a %d-commit history, want it capped at %d", len(neg.candidates), chainLength, maxHaveCandidates)
	}
	if neg.candidates[0] != tip {
		t.Fatalf("newNegotiator candidates[0] = %s, want the branch tip %s", neg.candidates[0], tip)
	}
}

func TestNewNegotiatorPropagatesRefsStoreErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	tx := store.Begin()
	if err := tx.SetSymbolic(refs.BranchName("a"), refs.BranchName("b")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.SetSymbolic(refs.BranchName("b"), refs.BranchName("a")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	setLocalHead(t, store, refs.BranchName("a"))

	if _, err := newNegotiator(t.Context(), store, db, nil); err == nil {
		t.Fatal("newNegotiator returned no error for a circular HEAD symref")
	}
}
