package refs

import (
	"errors"
	"strings"
	"testing"
)

const (
	packedHeaderFull  = "# pack-refs with: peeled fully-peeled sorted \n"
	packedHeaderPlain = "# pack-refs with: sorted \n"
)

func TestParsePackedRefsReadsHeaderTraitsAndEntries(t *testing.T) {
	data := packedHeaderFull +
		"1111111111111111111111111111111111111111 refs/heads/main\n" +
		"2222222222222222222222222222222222222222 refs/tags/v1\n" +
		"^3333333333333333333333333333333333333333\n"
	snapshot, err := parsePackedRefs([]byte(data))
	if err != nil {
		t.Fatalf("parsePackedRefs returned error %v", err)
	}
	if !snapshot.peeled || !snapshot.fullyPeeled {
		t.Fatalf("traits are peeled=%v fully=%v", snapshot.peeled, snapshot.fullyPeeled)
	}
	if len(snapshot.refs) != 2 {
		t.Fatalf("parsePackedRefs returned %d references", len(snapshot.refs))
	}
	tag, ok := snapshot.find(TagName("v1"))
	if !ok || tag.Peeled != oidFrom(t, "3333333333333333333333333333333333333333") {
		t.Fatalf("peeled value of refs/tags/v1 is %v (found %v)", tag.Peeled, ok)
	}
	if _, ok := snapshot.find(BranchName("missing")); ok {
		t.Fatal("find reported a missing reference")
	}
}

func TestParsePackedRefsAcceptsFileWithoutHeader(t *testing.T) {
	data := "1111111111111111111111111111111111111111\trefs/heads/main\n"
	snapshot, err := parsePackedRefs([]byte(data))
	if err != nil {
		t.Fatalf("parsePackedRefs returned error %v", err)
	}
	if snapshot.peeled || len(snapshot.refs) != 1 {
		t.Fatalf("snapshot is %+v", snapshot)
	}
}

func TestParsePackedRefsSortsUnsortedFile(t *testing.T) {
	data := "2222222222222222222222222222222222222222 refs/heads/zeta\n" +
		"1111111111111111111111111111111111111111 refs/heads/alpha\n"
	snapshot, err := parsePackedRefs([]byte(data))
	if err != nil {
		t.Fatalf("parsePackedRefs returned error %v", err)
	}
	if snapshot.refs[0].Name != BranchName("alpha") {
		t.Fatalf("first reference is %s", snapshot.refs[0].Name)
	}
}

func TestParsePackedRefsRejectsBrokenFiles(t *testing.T) {
	broken := map[string]string{
		"unterminated header": "# pack-refs with: sorted",
		"unknown header":      "# something else\n",
		"unterminated line":   "1111111111111111111111111111111111111111 refs/heads/main",
		"empty line":          "\n",
		"short line":          "1111 refs/heads/main\n",
		"bad object id":       "111111111111111111111111111111111111111z refs/heads/main\n",
		"missing separator":   "1111111111111111111111111111111111111111xrefs/heads/main\n",
		"bad name":            "1111111111111111111111111111111111111111 refs/heads/main.lock\n",
		"peel without ref":    "^1111111111111111111111111111111111111111\n",
		"second peel": "1111111111111111111111111111111111111111 refs/tags/v1\n" +
			"^2222222222222222222222222222222222222222\n" +
			"^3333333333333333333333333333333333333333\n",
		"short peel": "1111111111111111111111111111111111111111 refs/tags/v1\n^2222\n",
		"bad peel id": "1111111111111111111111111111111111111111 refs/tags/v1\n" +
			"^222222222222222222222222222222222222222z\n",
		"duplicate name": "1111111111111111111111111111111111111111 refs/heads/main\n" +
			"2222222222222222222222222222222222222222 refs/heads/main\n",
	}
	for description, data := range broken {
		if _, err := parsePackedRefs([]byte(data)); !errors.Is(err, ErrMalformedPacked) {
			t.Errorf("parsePackedRefs of %s returned %v, want ErrMalformedPacked", description, err)
		}
	}
}

func TestEncodePackedRefsWritesGitLayout(t *testing.T) {
	refs := []Ref{
		{Name: BranchName("main"), Target: oidFrom(t, "11")},
		{Name: TagName("v1"), Target: oidFrom(t, "22"), Peeled: oidFrom(t, "33")},
	}
	encoded := string(encodePackedRefs(refs, true))
	if !strings.HasPrefix(encoded, packedHeaderFull) {
		t.Fatalf("encoded header is %q", encoded)
	}
	if !strings.Contains(encoded, "\n^"+oidFrom(t, "33").String()+"\n") {
		t.Fatalf("peeled line is missing in %q", encoded)
	}
	snapshot, err := parsePackedRefs([]byte(encoded))
	if err != nil {
		t.Fatalf("parsePackedRefs returned error %v", err)
	}
	if len(snapshot.refs) != 2 || !snapshot.fullyPeeled {
		t.Fatalf("round trip produced %+v", snapshot)
	}
	if plain := string(encodePackedRefs(nil, false)); plain != packedHeaderPlain {
		t.Fatalf("plain header is %q", plain)
	}
}

func TestLoadPackedReturnsEmptySnapshotWhenFileIsMissing(t *testing.T) {
	store := openStore(t, newGitDir(t))
	snapshot, err := store.loadPacked()
	if err != nil {
		t.Fatalf("loadPacked returned error %v", err)
	}
	if len(snapshot.refs) != 0 {
		t.Fatalf("loadPacked returned %d references", len(snapshot.refs))
	}
}

func TestLoadPackedFailsWhenFileCannotBeRead(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain)
	store := openStore(t, dir)
	swapReadFile(t, func(name string) bool { return name == packedRefsFile }, errors.New("broken"))
	if _, err := store.loadPacked(); !errors.Is(err, ErrReadFailed) {
		t.Fatalf("loadPacked returned %v, want ErrReadFailed", err)
	}
}
