package journal

import (
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func applied(t *testing.T, filter Filter, opts Options) Options {
	t.Helper()
	out, err := filter.Apply(opts)
	if err != nil {
		t.Fatalf("Apply returned error %v", err)
	}
	return out
}

func TestAnEmptyFilterAsksForNothing(t *testing.T) {
	if !(Filter{}).Empty() {
		t.Fatal("a blank filter is not empty")
	}
	if !(Filter{Author: "  ", Message: "\t", Path: " ", Content: "  "}).Empty() {
		t.Fatal("a filter of spaces is not empty")
	}
	for _, filter := range []Filter{
		{Branch: "main"}, {Author: "ann"}, {Message: "fix"},
		{Path: "src"}, {Content: "needle"}, {Since: time.Unix(1700000000, 0)},
	} {
		if filter.Empty() {
			t.Errorf("%+v looks empty", filter)
		}
	}
}

func TestTheFilterPutsItsQueriesIntoTheWalk(t *testing.T) {
	tip := hash.SumSHA1("commit", []byte("tip"))
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	filter := Filter{Branch: "main", Tip: tip, Author: "Ann", Message: "a FIX", Path: ` src\ui\ `, Content: "Needle", Since: since}

	opts := applied(t, filter, Options{Walk: revision.Options{MaxCount: 5}})

	if opts.Tip != tip || opts.Walk.MaxCount != 5 || !opts.Walk.Since.Equal(since) {
		t.Fatalf("options = %+v", opts)
	}
	if !opts.Walk.Author.MatchString("ann tester") || opts.Walk.Author.MatchString("bob") {
		t.Fatalf("author = %v", opts.Walk.Author)
	}
	if !opts.Walk.Grep.MatchString("A fix for the thing") || opts.Walk.Grep.MatchString("nothing") {
		t.Fatalf("grep = %v", opts.Walk.Grep)
	}
	if len(opts.Walk.Paths) != 1 || opts.Walk.Paths[0] != "src/ui/" {
		t.Fatalf("paths = %q, want the path with forward slashes", opts.Walk.Paths)
	}
	if opts.Walk.Pickaxe != "Needle" || opts.Walk.PickaxeRegexp != nil {
		t.Fatalf("pickaxe = %q, %v, want the string search as typed", opts.Walk.Pickaxe, opts.Walk.PickaxeRegexp)
	}
}

func TestTheFilterSearchesContentByRegularExpressionWhenAsked(t *testing.T) {
	opts := applied(t, Filter{Content: "ne+dle", ContentRegexp: true}, Options{})

	if opts.Walk.Pickaxe != "" || opts.Walk.PickaxeRegexp == nil || !opts.Walk.PickaxeRegexp.MatchString("a neeedle") {
		t.Fatalf("pickaxe = %q, %v", opts.Walk.Pickaxe, opts.Walk.PickaxeRegexp)
	}
}

func TestAnUnusableContentPatternIsReported(t *testing.T) {
	if _, err := (Filter{Content: "need(le", ContentRegexp: true}).Apply(Options{}); err == nil {
		t.Fatal("a broken regular expression was accepted")
	}
}

func TestAFilterWithoutQueriesLeavesTheWalkAlone(t *testing.T) {
	opts := applied(t, Filter{}, Options{})

	if opts.Walk.Author != nil || opts.Walk.Grep != nil || !opts.Tip.IsZero() ||
		opts.Walk.Paths != nil || opts.Walk.Pickaxe != "" || opts.Walk.PickaxeRegexp != nil || !opts.Walk.Since.IsZero() {
		t.Fatalf("options = %+v", opts)
	}
}

func TestTheFilterQuotesWhatItIsGiven(t *testing.T) {
	opts := applied(t, Filter{Message: "a.b*c"}, Options{})

	if !opts.Walk.Grep.MatchString("xxa.b*cyy") || opts.Walk.Grep.MatchString("aXbbbc") {
		t.Fatalf("grep = %v", opts.Walk.Grep)
	}
}

func TestLoadStartsAtTheTipTheFilterChose(t *testing.T) {
	_, source, a, b, _ := buildLinearRepo(t)

	rows, err := collectRows(t, Load(t.Context(), source, applied(t, Filter{Tip: b}, Options{})))
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
