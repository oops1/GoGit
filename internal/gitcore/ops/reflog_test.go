package ops

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func (r *testRepo) stagedCommit(message string, files map[string]string) hash.ObjectID {
	r.t.Helper()
	for rel, content := range files {
		r.writeFile(rel, content)
	}
	if err := Stage(r.t.Context(), r.repo, slices.Sorted(maps.Keys(files)), StageOptions{}); err != nil {
		r.t.Fatal(err)
	}
	r.clock += 60
	id, err := Commit(r.t.Context(), r.repo, CommitOptions{Message: message, When: time.Unix(r.clock, 0).UTC()})
	if err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
	return id
}

func TestTheReflogOfHeadIsNewestFirst(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	first := tr.stagedCommit("base", map[string]string{"f": f})
	second := tr.stagedCommit("edit", map[string]string{"f": changeLine(f, 2, "EDITED")})

	records, err := Reflog(t.Context(), tr.repo, "HEAD", ReflogOptions{})

	if err != nil {
		t.Fatalf("Reflog returned error %v", err)
	}
	if len(records) != 2 || records[0].New != second || records[1].New != first {
		t.Fatalf("records = %+v", records)
	}
	if records[0].Index != 0 || records[0].Selector() != "HEAD@{0}" || records[0].Ref != refs.HEAD {
		t.Fatalf("record = %+v", records[0])
	}
	if records[0].Subject != "edit" || records[0].Committer.Name != "ann" {
		t.Fatalf("record = %+v", records[0])
	}
}

func TestTheReflogStopsAtTheCountAsked(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	last := tr.commitFiles("edit", map[string]string{"f": changeLine(f, 2, "EDITED")})

	records, err := Reflog(t.Context(), tr.repo, "", ReflogOptions{MaxCount: 1})

	if err != nil || len(records) != 1 || records[0].New != last {
		t.Fatalf("records = %+v, %v", records, err)
	}
}

func TestTheReflogOfABranchAndOfAFullName(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	short, err := Reflog(t.Context(), tr.repo, "main", ReflogOptions{})
	if err != nil {
		t.Fatalf("Reflog returned error %v", err)
	}
	full, err := Reflog(t.Context(), tr.repo, "refs/heads/main", ReflogOptions{})
	if err != nil {
		t.Fatalf("Reflog returned error %v", err)
	}

	if len(short) != 1 || len(full) != 1 || short[0].New != full[0].New {
		t.Fatalf("short = %+v, full = %+v", short, full)
	}
	if short[0].Selector() != "main@{0}" {
		t.Fatalf("selector = %q", short[0].Selector())
	}
}

func TestTheReflogOfATagIsFound(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	if _, err := CreateTag(t.Context(), tr.repo, "v1", head.String(), tagOptions("")); err != nil {
		t.Fatal(err)
	}

	records, err := Reflog(t.Context(), tr.repo, "v1", ReflogOptions{})

	if err != nil || records != nil {
		t.Fatalf("records = %+v, %v", records, err)
	}
}

func TestTheReflogOfAnUnknownNameFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := Reflog(t.Context(), tr.repo, "nope", ReflogOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheReflogKeepsAMessageWithoutAPrefixWhole(t *testing.T) {
	if got := reflogSubject("a message without a colon"); got != "a message without a colon" {
		t.Fatalf("subject = %q", got)
	}
}

func TestTheReflogHonoursACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Reflog(ctx, tr.repo, "HEAD", ReflogOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestACorruptReflogIsReported(t *testing.T) {
	tr := newTestRepo(t)
	tr.stagedCommit("base", map[string]string{"f": "f\n"})
	tr.writeFile(".git/logs/refs/heads/main", "this is not a reflog line\n")

	if _, err := Reflog(t.Context(), tr.repo, "main", ReflogOptions{}); err == nil {
		t.Fatal("a corrupt reflog was accepted")
	}
}
