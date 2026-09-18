//go:build oracle

package revision

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

const randomCommits = 90

var randomPaths = []string{"a.txt", "d/b.txt", "d/e/c.txt", "x.txt", "d/e/f/g.txt"}

func buildRandomHistory(t *testing.T, seed uint64) *oracle {
	t.Helper()
	o := newOracle(t)
	rng := rand.New(rand.NewPCG(seed, seed*7+1))
	var script strings.Builder
	data := func(text string) {
		fmt.Fprintf(&script, "data %d\n%s\n", len(text), text)
	}
	for at := range randomCommits {
		fmt.Fprintf(&script, "commit refs/heads/c%d\nmark :%d\n", at, at+1)
		committed := 1700000000 + int64(at)*100 + int64(rng.IntN(900)) - 450
		authored := committed - int64(rng.IntN(5000))
		fmt.Fprintf(&script, "author A <a@example.com> %d +0000\ncommitter C <c@example.com> %d +0000\n", authored, committed)
		data("commit " + strconv.Itoa(at))
		if at > 0 {
			roll := rng.IntN(100)
			var parents []int
			switch {
			case roll < 4:
			case roll < 72:
				parents = []int{rng.IntN(at)}
			case roll < 94:
				parents = []int{rng.IntN(at), rng.IntN(at)}
			default:
				parents = []int{rng.IntN(at), rng.IntN(at), rng.IntN(at)}
			}
			for index, parent := range slices.Compact(parents) {
				if index == 0 {
					fmt.Fprintf(&script, "from :%d\n", parent+1)
					continue
				}
				fmt.Fprintf(&script, "merge :%d\n", parent+1)
			}
		}
		for range rng.IntN(3) {
			fmt.Fprintf(&script, "M 100644 inline %s\n", randomPaths[rng.IntN(len(randomPaths))])
			data("content " + strconv.Itoa(rng.IntN(4)))
		}
	}
	o.gitInput(script.String(), "fast-import", "--quiet")
	return o
}

func (o *oracle) gitInput(input string, args ...string) {
	o.t.Helper()
	cmd := o.command(o.repo, args)
	cmd.Stdin = strings.NewReader(input)
	if out, err := cmd.CombinedOutput(); err != nil {
		o.t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, out)
	}
}

func (o *oracle) randomGraphModes(t *testing.T) []struct {
	name  string
	graph *commitgraph.Graph
} {
	t.Helper()
	objects := filepath.Join(o.repo, ".git", "objects")
	open := func() *commitgraph.Graph {
		graph, err := commitgraph.Open([]string{objects}, commitgraph.OpenOptions{})
		if err != nil || graph == nil {
			t.Fatalf("commitgraph.Open returned %v, %v", graph, err)
		}
		return graph
	}
	o.git("commit-graph", "write", "--reachable", "--changed-paths")
	single := open()
	if err := os.Remove(filepath.Join(objects, "info", commitgraph.FileName)); err != nil {
		t.Fatal(err)
	}
	o.git("-c", "commitGraph.generationVersion=1", "commit-graph", "write", "--reachable")
	levels := open()
	if err := os.Remove(filepath.Join(objects, "info", commitgraph.FileName)); err != nil {
		t.Fatal(err)
	}
	for _, tip := range []string{"c30", "c60", "c89"} {
		o.gitInputQuiet(o.parse(tip).String()+"\n", "commit-graph", "write", "--split=no-merge", "--changed-paths", "--stdin-commits")
	}
	split := open()
	return []struct {
		name  string
		graph *commitgraph.Graph
	}{{"objects", nil}, {"graph", single}, {"levels", levels}, {"split", split}}
}

func (o *oracle) gitInputQuiet(input string, args ...string) {
	o.t.Helper()
	o.gitInput(input, args...)
}

func walkIDs(t *testing.T, opts Options) []string {
	t.Helper()
	var got []string
	for commit, err := range Walk(t.Context(), opts) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		got = append(got, commit.ID.String())
	}
	return got
}

func TestOracleGraphWalksMatchGitOnRandomHistories(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} {
		t.Run("seed "+strconv.FormatUint(seed, 10), func(t *testing.T) {
			o := buildRandomHistory(t, seed)
			db, err := odb.Open(filepath.Join(o.repo, ".git", "objects"), odb.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			base := Context{Objects: db}
			rng := rand.New(rand.NewPCG(seed+100, seed))
			tips := []string{"c89", "c88", "c75", "c61", "c40"}
			type walkCase struct {
				args  []string
				specs []string
				setup func(*Options)
			}
			cases := []walkCase{
				{args: []string{"--topo-order", "c89"}, specs: []string{"c89"}, setup: func(opts *Options) { opts.Order = Topo }},
				{args: []string{"--date-order", "c89"}, specs: []string{"c89"}, setup: func(opts *Options) { opts.Order = DateOrder }},
				{args: []string{"--author-date-order", "c89"}, specs: []string{"c89"}, setup: func(opts *Options) { opts.Order = AuthorDate }},
				{args: []string{"--topo-order", "--first-parent", "c89"}, specs: []string{"c89"}, setup: func(opts *Options) { opts.Order, opts.FirstParent = Topo, true }},
				{args: append([]string{"--topo-order"}, tips...), specs: tips, setup: func(opts *Options) { opts.Order = Topo }},
				{args: append([]string{"--date-order"}, tips...), specs: tips, setup: func(opts *Options) { opts.Order = DateOrder }},
				{args: []string{"--topo-order", "--max-count=7", "c89"}, specs: []string{"c89"}, setup: func(opts *Options) { opts.Order, opts.MaxCount = Topo, 7 }},
				{args: []string{"--topo-order", "c75..c89"}, specs: []string{"c75..c89"}, setup: func(opts *Options) { opts.Order = Topo }},
				{args: []string{"c89"}, specs: []string{"c89"}},
				{args: []string{"c89", "--", "d/b.txt"}, specs: []string{"c89", "--", "d/b.txt"}},
				{args: []string{"c89", "--", "d"}, specs: []string{"c89", "--", "d"}},
				{args: []string{"c89", "--", "d/e/"}, specs: []string{"c89", "--", "d/e/"}},
				{args: []string{"--first-parent", "c89", "--", "a.txt"}, specs: []string{"c89", "--", "a.txt"}, setup: func(opts *Options) { opts.FirstParent = true }},
				{args: []string{"c89", "--", "a.txt", "x.txt"}, specs: []string{"c89", "--", "a.txt", "x.txt"}},
				{args: []string{"--topo-order", "c89", "--", "d/e/c.txt"}, specs: []string{"c89", "--", "d/e/c.txt"}, setup: func(opts *Options) { opts.Order = Topo }},
				{args: []string{"c40...c89"}, specs: []string{"c40...c89"}},
			}
			var pairs [][2]string
			for range 25 {
				pairs = append(pairs, [2]string{"c" + strconv.Itoa(rng.IntN(randomCommits)), "c" + strconv.Itoa(rng.IntN(randomCommits))})
			}
			type pairWant struct {
				left, right hash.ObjectID
				bases       []string
				ancestor    bool
				count       string
			}
			pairWants := make([]pairWant, len(pairs))
			for at, pair := range pairs {
				out, _ := o.command(o.repo, []string{"merge-base", "--all", pair[0], pair[1]}).Output()
				bases := strings.Fields(string(out))
				slices.Sort(bases)
				pairWants[at] = pairWant{
					left: o.parse(pair[0]), right: o.parse(pair[1]), bases: bases,
					ancestor: o.succeeds("merge-base", "--is-ancestor", pair[0], pair[1]),
					count:    strings.TrimSpace(o.git("rev-list", "--count", pair[0]+".."+pair[1])),
				}
			}
			wantWalks := make([][]string, len(cases))
			for at, c := range cases {
				wantWalks[at] = o.lines(append([]string{"rev-list"}, c.args...)...)
			}
			for _, mode := range o.randomGraphModes(t) {
				ctx := base
				ctx.Graph = mode.graph
				ctx.Refs = o.open().Refs
				for at, c := range cases {
					opts, err := Ranges(c.specs, ctx)
					if err != nil {
						t.Fatalf("Ranges(%v) returned error %v", c.specs, err)
					}
					if c.setup != nil {
						c.setup(&opts)
					}
					if got := walkIDs(t, opts); !slices.Equal(got, wantWalks[at]) {
						t.Errorf("%s: rev-list %v differs\nours %v\ngit  %v", mode.name, c.args, got, wantWalks[at])
					}
				}
				for at, pair := range pairs {
					want := pairWants[at]
					bases, err := MergeBase(ctx, want.left, want.right)
					if err != nil {
						t.Fatal(err)
					}
					got := make([]string, 0, len(bases))
					for _, base := range bases {
						got = append(got, base.String())
					}
					slices.Sort(got)
					if !slices.Equal(got, want.bases) {
						t.Errorf("%s: merge-base %v = %v, git %v", mode.name, pair, got, want.bases)
					}
					ancestor, err := IsAncestor(ctx, want.left, want.right)
					if err != nil || ancestor != want.ancestor {
						t.Errorf("%s: is-ancestor %v = %v, %v", mode.name, pair, ancestor, err)
					}
					walked := walkIDs(t, Options{Context: ctx, Include: []hash.ObjectID{want.right}, Exclude: []hash.ObjectID{want.left}})
					if strconv.Itoa(len(walked)) != want.count {
						t.Errorf("%s: rev-list --count %v = %d, git %s", mode.name, pair, len(walked), want.count)
					}
				}
			}
		})
	}
}
