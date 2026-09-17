package ops

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func lineHistoryOf(t *testing.T, r *repo.Repository, rev string, specs []linelog.Spec, opts LineHistoryOptions) ([]LineHistoryEntry, error) {
	t.Helper()
	var out []LineHistoryEntry
	for entry, err := range LineHistory(t.Context(), r, rev, specs, opts) {
		if err != nil {
			return out, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func commitsOf(entries []LineHistoryEntry) []hash.ObjectID {
	var ids []hash.ObjectID
	for _, entry := range entries {
		ids = append(ids, entry.Commit)
	}
	return ids
}

func TestLineHistoryListsTheCommitsThatTouchedTheLines(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	edit := tr.commitFiles("edit", map[string]string{"f": changeLine(f, 4, "EDITED")})
	tr.commitFiles("elsewhere", map[string]string{"f": changeLine(changeLine(f, 4, "EDITED"), 9, "LAST")})
	var progress []int

	entries, err := lineHistoryOf(t, tr.repo, "HEAD", []linelog.Spec{{Range: "4,6", Path: "./f"}}, LineHistoryOptions{Progress: func(done, _ int) { progress = append(progress, done) }})

	if err != nil || !slices.Equal(commitsOf(entries), []hash.ObjectID{edit, base}) {
		t.Fatalf("entries = %v, %v", commitsOf(entries), err)
	}
	if entries[0].Subject != "edit" || entries[0].Author.Name != "ann" || len(entries[0].Parents) != 1 || len(entries[0].Files) != 1 {
		t.Fatalf("entry = %+v", entries[0])
	}
	if len(progress) == 0 {
		t.Fatal("no progress was reported")
	}
}

func TestLineHistoryStopsWhenTheCallerStops(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	tr.commitFiles("edit", map[string]string{"f": changeLine(f, 1, "EDITED")})

	count := 0
	for _, err := range LineHistory(t.Context(), tr.repo, "HEAD", []linelog.Spec{{Range: "1,3", Path: "f"}}, LineHistoryOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		count++
		break
	}
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
}

func TestLineHistoryHonoursTheRenamesSetting(t *testing.T) {
	cases := []struct {
		name   string
		config string
		opts   LineHistoryOptions
		want   int
	}{
		{name: "default follows", want: 2},
		{name: "config off", config: "[diff]\n\trenames = false\n", want: 1},
		{name: "config unreadable follows", config: "[diff]\n\trenames = maybe\n", want: 2},
		{name: "option off", opts: LineHistoryOptions{NoRenames: true}, want: 1},
	}
	for _, c := range cases {
		tr := newTestRepo(t)
		f := tenLines("f")
		tr.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
		tr.commitFiles("move", map[string]string{"f": "", "moved": changeLine(f, 1, "EDITED")})
		if c.config != "" {
			tr.appendConfig(c.config)
			tr.repo = tr.reopen()
		}
		entries, err := lineHistoryOf(t, tr.repo, "HEAD", []linelog.Spec{{Range: "1,3", Path: "moved"}}, c.opts)
		if err != nil || len(entries) != c.want {
			t.Errorf("%s: entries = %d, %v", c.name, len(entries), err)
		}
	}
}

func TestLineHistoryReportsFailures(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": tenLines("f")})
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	spec := []linelog.Spec{{Range: "1,2", Path: "f"}}

	cases := []struct {
		name  string
		ctx   context.Context
		rev   string
		specs []linelog.Spec
		want  error
	}{
		{name: "cancelled", ctx: cancelled, rev: "HEAD", specs: spec, want: context.Canceled},
		{name: "outside path", ctx: t.Context(), rev: "HEAD", specs: []linelog.Spec{{Range: "1", Path: "../x"}}, want: ErrInvalidPath},
		{name: "unknown revision", ctx: t.Context(), rev: "nope", specs: spec, want: ErrTargetNotFound},
		{name: "missing path", ctx: t.Context(), rev: "HEAD", specs: []linelog.Spec{{Range: "1", Path: "g"}}, want: linelog.ErrPathNotFound},
	}
	for _, c := range cases {
		var err error
		for _, err = range LineHistory(c.ctx, tr.repo, c.rev, c.specs, LineHistoryOptions{}) {
			if err != nil {
				break
			}
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}

	tr.writeFile(".git/shallow", "not a hash\n")
	if _, err := lineHistoryOf(t, tr.repo, "HEAD", spec, LineHistoryOptions{}); !errors.Is(err, repo.ErrInvalidShallowFile) {
		t.Errorf("a broken shallow file returned %v", err)
	}
}

func swapOdbOpenFailure(t *testing.T) {
	t.Helper()
	original := odbOpen
	odbOpen = func(string, odb.Options) (*odb.DB, error) { return nil, errInjected }
	t.Cleanup(func() { odbOpen = original })
}

func TestLineHistoryReportsAnUnopenableRepository(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": tenLines("f")})
	swapOdbOpenFailure(t)

	if _, err := lineHistoryOf(t, tr.repo, "HEAD", []linelog.Spec{{Range: "1", Path: "f"}}, LineHistoryOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
	if _, err := LineHistorySpecs(t.Context(), tr.repo, "HEAD", LineSelection{Path: "f", First: 1, Last: 2, Shown: []byte("x\n")}); !errors.Is(err, errInjected) {
		t.Fatalf("specs err = %v", err)
	}
}

func TestLineHistorySpecsMapShownLinesToTheRevision(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	shown := []byte("new first line\n" + changeLine(f, 5, "CHANGED"))

	cases := []struct {
		name      string
		selection LineSelection
		want      []linelog.Spec
	}{
		{name: "whole file", selection: LineSelection{Path: "./f"}, want: []linelog.Spec{{Range: ",", Path: "f"}}},
		{name: "plain lines", selection: LineSelection{Path: "f", First: 3, Last: 5}, want: []linelog.Spec{{Range: "3,5", Path: "f"}}},
		{name: "caret", selection: LineSelection{Path: "f", First: 4}, want: []linelog.Spec{{Range: "4,4", Path: "f"}}},
		{name: "shifted", selection: LineSelection{Path: "f", First: 3, Last: 5, Shown: shown}, want: []linelog.Spec{{Range: "2,4", Path: "f"}}},
		{name: "changed line", selection: LineSelection{Path: "f", First: 7, Last: 7, Shown: shown}, want: []linelog.Spec{{Range: "6,6", Path: "f"}}},
		{name: "new line only", selection: LineSelection{Path: "f", First: 1, Last: 1, Shown: shown}},
	}
	for _, c := range cases {
		got, err := LineHistorySpecs(t.Context(), tr.repo, "HEAD", c.selection)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: specs = %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestLineHistorySpecsReportFailures(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": tenLines("f")})
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	shown := []byte("x\n")

	cases := []struct {
		name      string
		ctx       context.Context
		rev       string
		selection LineSelection
		want      error
	}{
		{name: "cancelled", ctx: cancelled, rev: "HEAD", selection: LineSelection{Path: "f"}, want: context.Canceled},
		{name: "outside path", ctx: t.Context(), rev: "HEAD", selection: LineSelection{Path: "../f"}, want: ErrInvalidPath},
		{name: "unknown revision", ctx: t.Context(), rev: "nope", selection: LineSelection{Path: "f", First: 1, Shown: shown}, want: ErrTargetNotFound},
		{name: "missing file", ctx: t.Context(), rev: "HEAD", selection: LineSelection{Path: "g", First: 1, Shown: shown}, want: linelog.ErrPathNotFound},
	}
	for _, c := range cases {
		if _, err := LineHistorySpecs(c.ctx, tr.repo, c.rev, c.selection); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}
