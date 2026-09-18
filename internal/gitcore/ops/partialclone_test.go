package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func promisorPackMarks(t *testing.T, r *repo.Repository) []string {
	t.Helper()
	marks, err := filepath.Glob(filepath.Join(r.PackDir(), "*.promisor"))
	if err != nil {
		t.Fatalf("Glob returned error %v", err)
	}
	return marks
}

func blobOf(t *testing.T, r *repo.Repository, content string) hash.ObjectID {
	t.Helper()
	id, err := hash.Sum(r.ObjectFormat, "blob", []byte(content))
	if err != nil {
		t.Fatalf("hash.Sum returned error %v", err)
	}
	return id
}

func TestCloneWithAFilterRecordsThePromisorRemote(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Filter: transport.FilterBlobNone, NoCheckout: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	if value, _ := r.Config().Get("remote.origin.promisor"); value != "true" {
		t.Fatalf("remote.origin.promisor = %q, want true", value)
	}
	if value, _ := r.Config().Get("remote.origin.partialclonefilter"); value != transport.FilterBlobNone {
		t.Fatalf("remote.origin.partialclonefilter = %q, want %q", value, transport.FilterBlobNone)
	}
	if value, _ := r.Config().Get("core.repositoryformatversion"); value != "1" {
		t.Fatalf("core.repositoryformatversion = %q, want 1", value)
	}
	if marks := promisorPackMarks(t, r); len(marks) != 1 {
		t.Fatalf("the clone left %d promisor marks, want one", len(marks))
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	defer func() { _ = db.Close() }()
	if has, err := db.Has(blobOf(t, r, "hello\n")); err != nil || has {
		t.Fatalf("the filtered clone brought the blob: %v, %v", has, err)
	}
}

func TestCloneWithAFilterChecksOutByFetchingTheBlobsOnDemand(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)

	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Filter: transport.FilterBlobNone})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	for rel, content := range map[string]string{"a.txt": "hello\n", "b.txt": "world\n"} {
		data, err := os.ReadFile(filepath.Join(dest, rel))
		if err != nil {
			t.Fatalf("ReadFile returned error %v", err)
		}
		if string(data) != content {
			t.Fatalf("%s = %q, want %q", rel, data, content)
		}
	}
}

func TestCloneRejectsAFilterItCannotApply(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	_, err := Clone(t.Context(), src.dir, dest, CloneOptions{Filter: "sparse:oid=HEAD"})
	if !errors.Is(err, transport.ErrUnsupportedFilter) {
		t.Fatalf("Clone returned %v, want ErrUnsupportedFilter", err)
	}
}

func TestFetchMissingObjectsNeedsAPromisorRemote(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{NoCheckout: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()
	if err := FetchMissingObjects(t.Context(), r, nil); !errors.Is(err, ErrNoPromisorRemote) {
		t.Fatalf("FetchMissingObjects returned %v, want ErrNoPromisorRemote", err)
	}
}

func TestFetchMissingObjectsBringsTheFilteredBlobs(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Filter: transport.FilterBlobNone, NoCheckout: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	wanted := blobOf(t, r, "hello\n")
	if err := FetchMissingObjects(t.Context(), r, []hash.ObjectID{wanted}); err != nil {
		t.Fatalf("FetchMissingObjects returned error %v", err)
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	defer func() { _ = db.Close() }()
	if has, err := db.Has(wanted); err != nil || !has {
		t.Fatalf("the blob was not fetched: %v, %v", has, err)
	}
}

func TestPromisorFilterOnlyAppliesToThePromisorRemote(t *testing.T) {
	src := newCloneSource(t)
	dest := mustCloneDest(t)
	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{Filter: transport.FilterBlobNone, NoCheckout: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	defer func() { _ = r.Close() }()

	tests := []struct {
		name string
		rem  remote.Remote
		opts remote.FetchOptions
		want string
	}{
		{"promisor", remote.Remote{Name: "origin"}, remote.FetchOptions{}, transport.FilterBlobNone},
		{"otherRemote", remote.Remote{Name: "upstream"}, remote.FetchOptions{}, ""},
		{"explicit", remote.Remote{Name: "origin"}, remote.FetchOptions{Filter: "blob:limit=1k"}, "blob:limit=1k"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withPromisorFilter(r, tc.rem, tc.opts)
			if err != nil {
				t.Fatalf("withPromisorFilter returned error %v", err)
			}
			if got.Filter != tc.want {
				t.Fatalf("filter = %q, want %q", got.Filter, tc.want)
			}
		})
	}
}

func TestPromisorLookupReportsAnUnreadableSetting(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[remote \"origin\"]\n\turl = ../src\n\tpromisor = perhaps\n")
	r.repo = r.reopen()
	if _, _, err := promisorOf(r.repo.Config()); err == nil {
		t.Fatal("promisorOf returned nil, want the invalid boolean to be reported")
	}
	if err := FetchMissingObjects(t.Context(), r.repo, nil); err == nil {
		t.Fatal("FetchMissingObjects returned nil, want the invalid boolean to be reported")
	}
	if _, err := withPromisorFilter(r.repo, remote.Remote{Name: "origin"}, remote.FetchOptions{}); err == nil {
		t.Fatal("withPromisorFilter returned nil, want the invalid boolean to be reported")
	}
	if opts := objectOptions(t.Context(), r.repo); opts.Missing != nil {
		t.Fatal("objectOptions installed a lazy fetch although the setting is unreadable")
	}
}

func TestPartialCloneValuesAreEmptyWithoutAFilter(t *testing.T) {
	if values := partialCloneValues("origin", ""); values != nil {
		t.Fatalf("partialCloneValues = %v, want none", values)
	}
}
