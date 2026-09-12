package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (r *testRepo) movedHistory() (hash.ObjectID, hash.ObjectID) {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
	r.remove("f")
	r.writeFile("moved", f)
	moved := r.commitFiles("move", map[string]string{"f": "", "moved": f})
	return base, moved
}

func historyOf(t *testing.T, tr *testRepo, path string, opts HistoryOptions) []HistoryEntry {
	t.Helper()
	entries, err := FileHistory(t.Context(), tr.repo, "HEAD", path, opts)
	if err != nil {
		t.Fatalf("FileHistory returned error %v", err)
	}
	return entries
}

func TestTheHistoryOfAFileNamesTheCommitsThatTouchedIt(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
	edit := tr.commitFiles("edit", map[string]string{"f": changeLine(f, 2, "EDITED")})
	tr.commitFiles("elsewhere", map[string]string{"keep": "changed\n"})

	entries := historyOf(t, tr, "f", HistoryOptions{})

	if len(entries) != 2 || entries[0].Commit != edit || entries[1].Commit != base {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Path != "f" || entries[0].Renamed() || entries[0].Author.Name != "ann" {
		t.Fatalf("entry = %+v", entries[0])
	}
}

func TestTheHistoryOfAFileStopsAtTheCountAsked(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	last := tr.commitFiles("edit", map[string]string{"f": changeLine(f, 2, "EDITED")})

	entries := historyOf(t, tr, "f", HistoryOptions{MaxCount: 1})

	if len(entries) != 1 || entries[0].Commit != last {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestTheHistoryOfAFileFollowsARename(t *testing.T) {
	tr := newTestRepo(t)
	base, moved := tr.movedHistory()

	plain := historyOf(t, tr, "moved", HistoryOptions{})
	followed := historyOf(t, tr, "moved", HistoryOptions{Follow: true})

	if len(plain) != 1 || plain[0].Commit != moved {
		t.Fatalf("plain = %+v", plain)
	}
	if len(followed) != 2 || followed[0].Commit != moved || followed[1].Commit != base {
		t.Fatalf("followed = %+v", followed)
	}
	if !followed[0].Renamed() || followed[0].Old != "f" || followed[1].Path != "f" {
		t.Fatalf("renamed entry = %+v", followed[0])
	}
}

func TestTheHistoryOfAFileStopsAtTheCountWhileFollowing(t *testing.T) {
	tr := newTestRepo(t)
	_, moved := tr.movedHistory()
	tr.commitFiles("edit", map[string]string{"moved": "changed\n"})

	entries := historyOf(t, tr, "moved", HistoryOptions{Follow: true, MaxCount: 2})

	if len(entries) != 2 || entries[1].Commit != moved {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestTheHistoryOfAMergedFileDoesNotLookForRenames(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.fork(map[string]string{"keep": "ours\n"}, map[string]string{"f": f})
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatal(err)
	}

	entries := historyOf(t, tr, "f", HistoryOptions{Follow: true})

	if len(entries) != 1 || entries[0].Renamed() {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestTheHistoryOfAPathOutsideTheRepositoryFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := FileHistory(t.Context(), tr.repo, "HEAD", "../outside", HistoryOptions{}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheHistoryOfAnUnknownRevisionFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := FileHistory(t.Context(), tr.repo, "nope", "f", HistoryOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheHistoryOfAFileHonoursACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := FileHistory(ctx, tr.repo, "HEAD", "f", HistoryOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestBlameNamesTheCommitBehindEachLine(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	edit := tr.commitFiles("edit", map[string]string{"f": changeLine(f, 3, "EDITED")})

	result, err := Blame(t.Context(), tr.repo, "", "f", BlameOptions{})

	if err != nil {
		t.Fatalf("Blame returned error %v", err)
	}
	if len(result.Lines) != 10 || result.Lines[3].Commit != edit || result.Lines[0].Commit != base {
		t.Fatalf("lines = %+v", result.Lines)
	}
	if result.Lines[3].Summary != "edit" || !strings.Contains(result.Lines[3].Text, "EDITED") {
		t.Fatalf("line = %+v", result.Lines[3])
	}
}

func TestBlameFollowsARename(t *testing.T) {
	tr := newTestRepo(t)
	base, _ := tr.movedHistory()

	result, err := Blame(t.Context(), tr.repo, "HEAD", "moved", BlameOptions{Follow: true})

	if err != nil {
		t.Fatalf("Blame returned error %v", err)
	}
	if result.Lines[0].Commit != base || result.Lines[0].Path != "f" {
		t.Fatalf("line = %+v", result.Lines[0])
	}
}

func TestBlameOfATagReachesTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "one\n"})
	if _, err := CreateTag(t.Context(), tr.repo, "v1", head.String(), tagOptions("tagged")); err != nil {
		t.Fatal(err)
	}

	result, err := Blame(t.Context(), tr.repo, "v1", "f", BlameOptions{})

	if err != nil || len(result.Lines) != 1 || result.Lines[0].Commit != head {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestBlameOfSomethingThatIsNotACommitFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "one\n"})
	tree, err := tr.db().Commit(tr.headCommit(tr.refs()))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Blame(t.Context(), tr.repo, tree.Tree.String(), "f", BlameOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestBlameRefusesAPathOutsideTheRepository(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "one\n"})

	if _, err := Blame(t.Context(), tr.repo, "HEAD", "../outside", BlameOptions{}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestBlameHonoursACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "one\n"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Blame(ctx, tr.repo, "HEAD", "f", BlameOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
