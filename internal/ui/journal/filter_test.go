package journal

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func TestAnEmptyFilterAsksForNothing(t *testing.T) {
	if !(Filter{}).Empty() {
		t.Fatal("a blank filter is not empty")
	}
	if !(Filter{Author: "  ", Message: "\t"}).Empty() {
		t.Fatal("a filter of spaces is not empty")
	}
	for _, filter := range []Filter{{Branch: "main"}, {Author: "ann"}, {Message: "fix"}} {
		if filter.Empty() {
			t.Errorf("%+v looks empty", filter)
		}
	}
}

func TestTheFilterPutsItsQueriesIntoTheWalk(t *testing.T) {
	tip := hash.SumSHA1("commit", []byte("tip"))
	filter := Filter{Branch: "main", Tip: tip, Author: "Ann", Message: "a FIX"}

	opts := filter.Apply(Options{Walk: revision.Options{MaxCount: 5}})

	if opts.Tip != tip || opts.Walk.MaxCount != 5 {
		t.Fatalf("options = %+v", opts)
	}
	if !opts.Walk.Author.MatchString("ann tester") || opts.Walk.Author.MatchString("bob") {
		t.Fatalf("author = %v", opts.Walk.Author)
	}
	if !opts.Walk.Grep.MatchString("A fix for the thing") || opts.Walk.Grep.MatchString("nothing") {
		t.Fatalf("grep = %v", opts.Walk.Grep)
	}
}

func TestAFilterWithoutQueriesLeavesTheWalkAlone(t *testing.T) {
	opts := Filter{}.Apply(Options{})

	if opts.Walk.Author != nil || opts.Walk.Grep != nil || !opts.Tip.IsZero() {
		t.Fatalf("options = %+v", opts)
	}
}

func TestTheFilterQuotesWhatItIsGiven(t *testing.T) {
	opts := Filter{Message: "a.b*c"}.Apply(Options{})

	if !opts.Walk.Grep.MatchString("xxa.b*cyy") || opts.Walk.Grep.MatchString("aXbbbc") {
		t.Fatalf("grep = %v", opts.Walk.Grep)
	}
}

func TestLoadStartsAtTheTipTheFilterChose(t *testing.T) {
	_, source, a, b, _ := buildLinearRepo(t)

	rows, err := collectRows(t, Load(t.Context(), source, Filter{Tip: b}.Apply(Options{})))
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}

	var got []hash.ObjectID
	for _, row := range rows {
		got = append(got, row.ID)
	}
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("rows = %v", got)
	}
}
