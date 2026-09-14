package ops

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func subprojectText(id hash.ObjectID) string {
	return "Subproject commit " + id.String() + "\n"
}

func TestAConflictedSubmoduleReadsAsTheCommitsEachSideRecorded(t *testing.T) {
	r := newTestRepo(t)
	base, ours, theirs := hash.SumSHA1("commit", []byte("base")), hash.SumSHA1("commit", []byte("ours")), hash.SumSHA1("commit", []byte("theirs"))
	idx := r.index()
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: base, Stage: index.StageAncestor})
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: ours, Stage: index.StageOurs})
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: theirs, Stage: index.StageTheirs})
	r.saveIndex(idx)

	file, err := ReadConflict(t.Context(), r.repo, "sub")

	if err != nil {
		t.Fatalf("ReadConflict returned error %v", err)
	}
	if string(file.Base) != subprojectText(base) || string(file.Ours) != subprojectText(ours) || string(file.Theirs) != subprojectText(theirs) {
		t.Fatalf("file = %q / %q / %q", file.Base, file.Ours, file.Theirs)
	}
	if file.Binary || len(file.Blocks) == 0 {
		t.Fatalf("file = %+v", file)
	}
}

func TestASubmoduleModifiedAgainstAFileDeletionMixesBothKinds(t *testing.T) {
	r := newTestRepo(t)
	pointer := hash.SumSHA1("commit", []byte("ours"))
	idx := r.index()
	idx.Add(index.Entry{Path: "p", Mode: object.ModeBlob, ID: storedBlob(t, r, "base\n"), Stage: index.StageAncestor})
	idx.Add(index.Entry{Path: "p", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageOurs})
	r.saveIndex(idx)

	file, err := ReadConflict(t.Context(), r.repo, "p")

	if err != nil || string(file.Base) != "base\n" || string(file.Ours) != subprojectText(pointer) || file.HasTheirs {
		t.Fatalf("file = %+v, %v", file, err)
	}
}
