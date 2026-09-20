//go:build oracle

package ops

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func partialCloneSource(o *oracle) string {
	o.t.Helper()
	dir := o.repoDir("src")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.run(dir, "config", "uploadpack.allowFilter", "true")
	o.run(dir, "config", "uploadpack.allowAnySHA1InWant", "true")
	o.write(dir, "a.txt", "one\n")
	o.write(dir, "d/b.txt", "two\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "one")
	o.write(dir, "a.txt", "three\n")
	o.run(dir, "commit", "-q", "-a", "-m", "two")
	return dir
}

func fileURLOf(dir string) string {
	return "file:///" + strings.TrimPrefix(filepath.ToSlash(dir), "/")
}

func (o *oracle) missingObjects(dir string) []string {
	o.t.Helper()
	var missing []string
	for line := range strings.Lines(o.run(dir, "rev-list", "--objects", "--all", "--missing=print")) {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "?"); ok {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}

func promisorPacks(t *testing.T, dir string) int {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "*.promisor"))
	if err != nil {
		t.Fatalf("Glob returned error %v", err)
	}
	return len(names)
}

func partialCloneConfig(o *oracle, dir string) string {
	o.t.Helper()
	var out strings.Builder
	for _, key := range []string{"core.repositoryformatversion", "remote.origin.promisor", "remote.origin.partialclonefilter"} {
		value, _ := o.attempt(dir, "config", "--get", key)
		out.WriteString(key + "=" + strings.TrimSpace(value) + "\n")
	}
	return out.String()
}

func TestOraclePartialCloneWithoutCheckoutLeavesTheSameObjectsMissing(t *testing.T) {
	o := newOracle(t)
	src := partialCloneSource(o)
	url := fileURLOf(src)

	gitSide := filepath.Join(o.t.TempDir(), "git")
	o.run(o.t.TempDir(), "clone", "-q", "--filter=blob:none", "--no-checkout", url, gitSide)

	ourSide := filepath.Join(o.t.TempDir(), "ours")
	r, err := Clone(t.Context(), url, ourSide, CloneOptions{Filter: "blob:none", NoCheckout: true, Open: o.options()})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	if got, want := partialCloneConfig(o, ourSide), partialCloneConfig(o, gitSide); got != want {
		t.Fatalf("config:\nours:\n%s\ngit:\n%s", got, want)
	}
	if got, want := promisorPacks(t, ourSide), promisorPacks(t, gitSide); got != want || got == 0 {
		t.Fatalf("promisor packs = %d, git wrote %d", got, want)
	}
	if got, want := o.missingObjects(ourSide), o.missingObjects(gitSide); !slices.Equal(got, want) || len(got) == 0 {
		t.Fatalf("missing objects:\nours   %v\ngit    %v", got, want)
	}
	o.run(ourSide, "fsck", "--no-dangling", "--no-progress")
}

func TestOraclePartialCloneFetchesTheCheckedOutBlobsOnDemand(t *testing.T) {
	o := newOracle(t)
	src := partialCloneSource(o)
	url := fileURLOf(src)

	gitSide := filepath.Join(o.t.TempDir(), "git")
	o.run(o.t.TempDir(), "clone", "-q", "--filter=blob:none", url, gitSide)

	ourSide := filepath.Join(o.t.TempDir(), "ours")
	r, err := Clone(t.Context(), url, ourSide, CloneOptions{Filter: "blob:none", Open: o.options()})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	for _, rel := range []string{"a.txt", "d/b.txt"} {
		if got, want := o.read(ourSide, rel), o.read(gitSide, rel); got != want {
			t.Fatalf("%s = %q, git checked out %q", rel, got, want)
		}
	}
	if status := o.run(ourSide, "status", "--porcelain=v2"); status != "" {
		t.Fatalf("git status is not clean: %q", status)
	}
	if got, want := o.missingObjects(ourSide), o.missingObjects(gitSide); !slices.Equal(got, want) {
		t.Fatalf("missing objects after the checkout:\nours   %v\ngit    %v", got, want)
	}
	o.run(ourSide, "fsck", "--no-dangling", "--no-progress")
}

func parseObjectIDs(t *testing.T, names []string) []hash.ObjectID {
	t.Helper()
	ids := make([]hash.ObjectID, 0, len(names))
	for _, name := range names {
		id, err := hash.Parse(name)
		if err != nil {
			t.Fatalf("hash.Parse returned error %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestOraclePartialCloneStillFetchesMissingObjectsOnDemand(t *testing.T) {
	o := newOracle(t)
	src := partialCloneSource(o)
	url := fileURLOf(src)

	ourSide := filepath.Join(o.t.TempDir(), "ours")
	r, err := Clone(t.Context(), url, ourSide, CloneOptions{Filter: "blob:none", NoCheckout: true, Open: o.options()})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	missing := o.missingObjects(ourSide)
	if len(missing) == 0 {
		t.Fatal("the partial clone left no object missing")
	}
	ids := parseObjectIDs(t, missing)
	beforeHead := o.read(ourSide, ".git/FETCH_HEAD")
	beforeRefs := o.run(ourSide, "for-each-ref")
	if err := FetchMissingObjects(t.Context(), r, ids); err != nil {
		t.Fatalf("FetchMissingObjects returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if left := o.missingObjects(ourSide); len(left) != 0 {
		t.Fatalf("objects are still missing: %v", left)
	}
	if got := o.read(ourSide, ".git/FETCH_HEAD"); got != beforeHead {
		t.Fatalf("FETCH_HEAD = %q, want the clone to have left %q", got, beforeHead)
	}
	if got := o.run(ourSide, "for-each-ref"); got != beforeRefs {
		t.Fatalf("refs = %q, want %q", got, beforeRefs)
	}
	o.run(ourSide, "fsck", "--no-dangling", "--no-progress")
}
