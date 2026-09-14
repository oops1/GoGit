package ops

import (
	"maps"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type flatFile struct {
	text string
	mode object.Mode
}

func (r *testRepo) flatTree(files map[string]flatFile) hash.ObjectID {
	r.t.Helper()
	db := r.db()
	store := mergeStore{db: db}
	var tree object.Tree
	for _, name := range slices.Sorted(maps.Keys(files)) {
		id, err := store.Put(object.TypeBlob, []byte(files[name].text))
		if err != nil {
			r.t.Fatal(err)
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{Name: name, Mode: files[name].mode, ID: id})
	}
	id, err := db.PutObject(&tree)
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *testRepo) patchIDOf(oldFiles, newFiles map[string]flatFile) hash.ObjectID {
	r.t.Helper()
	id, err := patchID(r.t.Context(), mergeStore{db: r.db()}, r.flatTree(oldFiles), r.flatTree(newFiles))
	if err != nil {
		r.t.Fatalf("patchID returned error %v", err)
	}
	return id
}

func blobs(texts map[string]string) map[string]flatFile {
	files := map[string]flatFile{}
	for name, text := range texts {
		files[name] = flatFile{text: text, mode: object.ModeBlob}
	}
	return files
}

func TestAPatchIDIgnoresWhereTheChangeLandsAndItsWhitespace(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	elsewhere := changeLine(f, 9, "ELSEWHERE")

	here := tr.patchIDOf(blobs(map[string]string{"f": f}), blobs(map[string]string{"f": changeLine(f, 1, "CHANGED LINE")}))
	there := tr.patchIDOf(blobs(map[string]string{"f": elsewhere}), blobs(map[string]string{"f": changeLine(elsewhere, 1, "CHANGED\t LINE")}))
	other := tr.patchIDOf(blobs(map[string]string{"f": f}), blobs(map[string]string{"f": changeLine(f, 1, "ANOTHER LINE")}))

	if here != there || here == other || here.IsZero() {
		t.Fatalf("here %s, there %s, other %s", here, there, other)
	}
}

func TestAPatchIDCoversEveryKindOfChangeButAModeAlone(t *testing.T) {
	tr := newTestRepo(t)
	before := map[string]flatFile{
		"bin":  {text: "one\x00", mode: object.ModeBlob},
		"gone": {text: "gone\n", mode: object.ModeBlob},
		"run":  {text: "echo one\n", mode: object.ModeBlob},
	}
	after := map[string]flatFile{
		"bin":   {text: "two\x00", mode: object.ModeBlob},
		"added": {text: "added\n", mode: object.ModeBlob},
		"run":   {text: "echo two\n", mode: object.ModeExecutable},
	}

	everything := tr.patchIDOf(before, after)
	modeOnly := tr.patchIDOf(
		map[string]flatFile{"run": {text: "echo\n", mode: object.ModeBlob}},
		map[string]flatFile{"run": {text: "echo\n", mode: object.ModeExecutable}},
	)

	if everything.IsZero() || !modeOnly.IsZero() {
		t.Fatalf("everything %s, mode only %s", everything, modeOnly)
	}
}

func TestAPatchIDNeedsReadableTrees(t *testing.T) {
	tr := newTestRepo(t)
	if _, err := patchID(t.Context(), mergeStore{db: tr.db()}, bogusObjectID(t, tr.repo.ObjectFormat), tr.flatTree(nil)); err == nil {
		t.Fatal("a patch id was computed over a missing tree")
	}
}

func TestPatchIDPartsCarryAcrossBytes(t *testing.T) {
	var id hash.ObjectID
	for i := range len(id) - 1 {
		id[i] = 0xff
	}
	var part [20]byte
	part[0] = 1

	addPatchIDPart(&id, part)

	if want := (hash.ObjectID{19: 1}); id != want {
		t.Fatalf("id = %s", id)
	}
}

func TestARebaseSkipsACommitAlreadyAppliedUpstream(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	tr.createBranch("topic", base)
	tr.commitFiles("main f line 8", map[string]string{"f": changeLine(f, 8, "MAIN")})
	tr.commitFiles("picked f line 2", map[string]string{"f": changeLine(changeLine(f, 8, "MAIN"), 2, "TOPIC")})
	tr.commitFiles("reverted f line 2", map[string]string{"f": changeLine(f, 8, "MAIN")})
	tr.switchTo("topic")
	tr.commitFiles("topic f line 2", map[string]string{"f": changeLine(f, 2, "TOPIC")})
	tr.commitFiles("topic h", map[string]string{"h": "h\n"})

	planned, err := PlannedRebase(t.Context(), tr.repo, "main")
	if err != nil || len(planned) != 1 || planned[0].Subject != "topic h" {
		t.Fatalf("planned = %+v, %v", planned, err)
	}
	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || result.Applied != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.subjects(3); !slices.Equal(got, []string{"topic h", "reverted f line 2", "picked f line 2"}) {
		t.Fatalf("history = %v", got)
	}
}

func TestACommitPatchIDNeedsItsParent(t *testing.T) {
	tr := newTestRepo(t)
	tip := tr.commitFiles("base", map[string]string{"f": "f\n"})
	m, err := openMerger(t.Context(), tr.repo, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	commit, err := tr.db().Commit(tip)
	if err != nil {
		t.Fatal(err)
	}
	broken := &revision.Commit{ID: tip, Commit: commit, Parents: []hash.ObjectID{bogusObjectID(t, tr.repo.ObjectFormat)}}

	if _, err := m.appliedUpstream(broken, map[hash.ObjectID]bool{hash.Zero: true}); err == nil {
		t.Fatal("a patch id was computed without the parent")
	}
	if applied, err := m.appliedUpstream(broken, nil); applied || err != nil {
		t.Fatalf("applied = %v, %v", applied, err)
	}
}
