package app

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/repo"
)

func memoPair(t *testing.T, name, local, remote string) repo.BranchPair {
	t.Helper()
	return repo.BranchPair{
		Name:   refs.BranchName(name),
		Local:  memoID(t, local),
		Remote: memoID(t, remote),
	}
}

func memoID(t *testing.T, seed string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(seed + "0000000000000000000000000000000000000000"[:hash.HexSize-len(seed)])
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	return id
}

type countingCounter struct {
	asked  [][]repo.BranchPair
	answer map[refs.Name]repo.Divergence
	err    error
}

func (c *countingCounter) count(pairs []repo.BranchPair) (map[refs.Name]repo.Divergence, error) {
	c.asked = append(c.asked, pairs)
	return c.answer, c.err
}

func TestDivergenceMemoComputesEachPairOnce(t *testing.T) {
	var memo divergenceMemo
	pair := memoPair(t, "main", "11", "22")
	counter := &countingCounter{answer: map[refs.Name]repo.Divergence{pair.Name: {Ahead: 2, Behind: 1}}}

	for range 3 {
		got, err := memo.count([]repo.BranchPair{pair}, counter.count)
		if err != nil || got[pair.Name] != (repo.Divergence{Ahead: 2, Behind: 1}) {
			t.Fatalf("count returned %v, %v", got, err)
		}
	}

	if len(counter.asked) != 1 {
		t.Fatalf("the counter was asked %d times, want once", len(counter.asked))
	}
}

func TestDivergenceMemoRemembersPairsThatDoNotDiverge(t *testing.T) {
	var memo divergenceMemo
	pair := memoPair(t, "main", "11", "11")
	counter := &countingCounter{answer: map[refs.Name]repo.Divergence{}}

	for range 2 {
		got, err := memo.count([]repo.BranchPair{pair}, counter.count)
		if err != nil || len(got) != 0 {
			t.Fatalf("count returned %v, %v, want nothing", got, err)
		}
	}

	if len(counter.asked) != 1 {
		t.Fatalf("the counter was asked %d times, want once", len(counter.asked))
	}
}

func TestDivergenceMemoAsksOnlyForPairsWhoseTipsChanged(t *testing.T) {
	var memo divergenceMemo
	main := memoPair(t, "main", "11", "22")
	feature := memoPair(t, "feature", "33", "22")
	counter := &countingCounter{answer: map[refs.Name]repo.Divergence{main.Name: {Ahead: 1}, feature.Name: {Behind: 4}}}
	if _, err := memo.count([]repo.BranchPair{main, feature}, counter.count); err != nil {
		t.Fatalf("count returned error %v", err)
	}

	moved := memoPair(t, "main", "44", "22")
	counter.answer = map[refs.Name]repo.Divergence{moved.Name: {Ahead: 2}}
	got, err := memo.count([]repo.BranchPair{moved, feature}, counter.count)

	if err != nil {
		t.Fatalf("count returned error %v", err)
	}
	if len(counter.asked) != 2 || len(counter.asked[1]) != 1 || counter.asked[1][0] != moved {
		t.Fatalf("the second question was %v, want only the moved branch", counter.asked)
	}
	if got[moved.Name] != (repo.Divergence{Ahead: 2}) || got[feature.Name] != (repo.Divergence{Behind: 4}) {
		t.Fatalf("count returned %v", got)
	}
}

func TestDivergenceMemoDoesNotRememberAFailedCount(t *testing.T) {
	var memo divergenceMemo
	pair := memoPair(t, "main", "11", "22")
	boom := errors.New("boom")
	counter := &countingCounter{err: boom}
	if _, err := memo.count([]repo.BranchPair{pair}, counter.count); !errors.Is(err, boom) {
		t.Fatalf("count returned %v, want %v", err, boom)
	}

	counter.err = nil
	counter.answer = map[refs.Name]repo.Divergence{pair.Name: {Ahead: 1}}
	got, err := memo.count([]repo.BranchPair{pair}, counter.count)

	if err != nil || got[pair.Name] != (repo.Divergence{Ahead: 1}) || len(counter.asked) != 2 {
		t.Fatalf("count returned %v, %v after %d questions", got, err, len(counter.asked))
	}
}

func TestDivergenceMemoForgetsPairsThatLeftTheBranchList(t *testing.T) {
	var memo divergenceMemo
	main := memoPair(t, "main", "11", "22")
	gone := memoPair(t, "gone", "33", "22")
	counter := &countingCounter{answer: map[refs.Name]repo.Divergence{}}
	if _, err := memo.count([]repo.BranchPair{main, gone}, counter.count); err != nil {
		t.Fatalf("count returned error %v", err)
	}

	memo.keepOnly([]repo.BranchPair{main})
	if _, err := memo.count([]repo.BranchPair{main, gone}, counter.count); err != nil {
		t.Fatalf("count returned error %v", err)
	}

	if len(counter.asked) != 2 || len(counter.asked[1]) != 1 || counter.asked[1][0] != gone {
		t.Fatalf("the questions were %v, want the forgotten pair asked again", counter.asked)
	}
}

func TestDivergenceMemoKeepsOnlyPairsItHasCounted(t *testing.T) {
	var memo divergenceMemo
	memo.keepOnly([]repo.BranchPair{memoPair(t, "main", "11", "22")})

	if len(memo.known) != 0 {
		t.Fatalf("the memo holds %d pairs it never counted", len(memo.known))
	}
}
