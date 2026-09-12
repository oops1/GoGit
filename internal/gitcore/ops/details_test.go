package ops

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestTheDetailsOfACommitTellWhoWroteItAndWhatItChanged(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f, "dir/keep": "keep\n"})
	head := tr.commitFiles("edit\n\nwith a body", map[string]string{"f": changeLine(f, 2, "EDITED"), "added": "added\n"})

	details, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	if details.Commit != head || len(details.Parents) != 1 || details.Parents[0] != base {
		t.Fatalf("details = %+v", details)
	}
	if details.Subject() != "edit" || details.Author.Name != "ann" || details.Committer.Name != "ann" {
		t.Fatalf("details = %+v", details)
	}
	if len(details.Changes) != 2 {
		t.Fatalf("changes = %+v", details.Changes)
	}
	if !slices.Equal(details.Branches, []string{"main"}) || details.Tags != nil {
		t.Fatalf("branches = %v, tags = %v", details.Branches, details.Tags)
	}
	paths := make([]string, 0, len(details.Files))
	for _, file := range details.Files {
		paths = append(paths, file.Path)
	}
	if !slices.Equal(paths, []string{"added", "dir/keep", "f"}) || details.MoreFiles {
		t.Fatalf("files = %v", paths)
	}
	if details.Files[0].Size != len("added\n") || details.Files[0].Mode != object.ModeBlob {
		t.Fatalf("file = %+v", details.Files[0])
	}
}

func TestTheDetailsOfTheFirstCommitCompareAgainstNothing(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	details, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	if len(details.Parents) != 0 || len(details.Changes) != 1 || details.Changes[0].Status != diff.StatusAdded {
		t.Fatalf("details = %+v", details)
	}
}

func TestTheDetailsNameTheBranchesAndTagsAround(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.createBranch("topic", base)
	if _, err := CreateTag(t.Context(), tr.repo, "v1", base.String(), tagOptions("release")); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTag(t.Context(), tr.repo, "light", base.String(), tagOptions("")); err != nil {
		t.Fatal(err)
	}
	tr.commitFiles("later", map[string]string{"f": "later\n"})

	details, err := Details(t.Context(), tr.repo, base.String(), DetailsOptions{})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	if !slices.Equal(details.Branches, []string{"main", "topic"}) {
		t.Fatalf("branches = %v", details.Branches)
	}
	if !slices.Equal(details.Tags, []string{"light", "v1"}) {
		t.Fatalf("tags = %v", details.Tags)
	}
}

func TestTheDetailsStopListingFilesAtTheLimit(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n", "b": "b\n", "dir/c": "c\n"})

	details, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{FileLimit: 2})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	if len(details.Files) != 2 || !details.MoreFiles {
		t.Fatalf("files = %+v, more = %v", details.Files, details.MoreFiles)
	}
}

func TestTheDetailsOfAnUnknownRevisionFail(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := Details(t.Context(), tr.repo, "nope", DetailsOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheDetailsHonourACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Details(ctx, tr.repo, "HEAD", DetailsOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheDetailsKeepTheDiffOptionsTheyAreGiven(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	tr.remove("f")
	tr.commitFiles("move", map[string]string{"f": "", "moved": f})

	renames, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{Diff: diff.Defaults()})
	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	plain, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{Diff: diff.Options{RenameThreshold: 100}})
	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}

	if len(renames.Changes) != 1 || renames.Changes[0].Status != diff.StatusRenamed {
		t.Fatalf("with renames = %+v", renames.Changes)
	}
	if len(plain.Changes) != 2 {
		t.Fatalf("without renames = %+v", plain.Changes)
	}
}

func TestTheDetailsListASubmoduleWithoutItsSize(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	idx := tr.index()
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: tr.headCommit(tr.refs()), Stage: index.StageMerged})
	tr.saveIndex(idx)
	head := tr.commitAll("with a submodule")

	details, err := Details(t.Context(), tr.repo, head.String(), DetailsOptions{})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	for _, file := range details.Files {
		if file.Path == "sub" && file.Size == 0 && file.Mode == object.ModeSubmodule {
			return
		}
	}
	t.Fatalf("files = %+v", details.Files)
}

func TestTheDetailsReportARefTheyCannotRead(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.writeRawRef("refs/heads/broken", "this is not an object id\n")

	if _, err := Details(t.Context(), tr.repo, head.String(), DetailsOptions{}); err == nil {
		t.Fatal("a broken ref was ignored")
	}
}

func TestTheDetailsReportABranchTipTheyCannotRead(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	missing := hash.SumSHA1("commit", []byte("gone"))
	tr.writeRawRef("refs/heads/dangling", missing.String()+"\n")

	if _, err := Details(t.Context(), tr.repo, head.String(), DetailsOptions{}); err == nil {
		t.Fatal("a dangling branch was ignored")
	}
}
