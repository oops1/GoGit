package ops

import (
	"crypto/sha1"
	"encoding/binary"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func writeResolveUndoIndex(t *testing.T, r *testRepo, entries ...index.ResolveUndoEntry) {
	t.Helper()
	var payload []byte
	for _, entry := range entries {
		payload = append(payload, entry.Path...)
		payload = append(payload, 0)
		for _, mode := range entry.Modes {
			payload = strconv.AppendUint(payload, uint64(mode), 8)
			payload = append(payload, 0)
		}
		for stage, mode := range entry.Modes {
			if mode != 0 {
				payload = append(payload, entry.IDs[stage][:]...)
			}
		}
	}
	data := binary.BigEndian.AppendUint32([]byte("DIRC"), index.Version2)
	data = binary.BigEndian.AppendUint32(data, 0)
	data = binary.BigEndian.AppendUint32(append(data, "REUC"...), uint32(len(payload)))
	data = append(data, payload...)
	sum := sha1.Sum(data)
	if err := os.WriteFile(r.repo.IndexFile(), append(data, sum[:]...), 0o666); err != nil {
		t.Fatal(err)
	}
	if got := len(r.index().ResolveUndo); got != len(entries) {
		t.Fatalf("the written index holds %d resolve-undo entries, want %d", got, len(entries))
	}
}

func TestPruneKeepsAnObjectWrittenWhileItRuns(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	real, calls := dbLooseObjects, 0
	var late hash.ObjectID
	swapMaint(t, &dbLooseObjects, func(db *odb.DB) iter.Seq2[odb.LooseObject, error] {
		calls++
		if calls == 2 {
			late = putMaintBlob(t, r, "written by another process while prune was walking\n")
		}
		return real(db)
	})

	result, err := Prune(t.Context(), r.repo, PruneOptions{Expire: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	if !looseExists(r, late) || slices.Contains(result.Unreachable, late) {
		t.Fatalf("an object written during the prune was removed: %+v", result)
	}
}

func TestTheWalkSkipsDanglingReflogAndIndexEntries(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	lostCommit := hash.SumSHA1("commit", []byte("pruned by another tool"))
	lostBlob := hash.SumSHA1("blob", []byte("staged and then pruned"))
	writeReflogTo(t, r, lostCommit)
	idx := r.index()
	idx.Add(index.Entry{Path: "lost.txt", Mode: object.ModeBlob, ID: lostBlob})
	r.saveIndex(idx)

	walk := mustWalk(t, r)

	if walk.reachable(lostCommit) || walk.reachable(lostBlob) || !walk.reachable(h.second) {
		t.Fatalf("lost commit = %v, lost blob = %v, main = %v", walk.reachable(lostCommit), walk.reachable(lostBlob), walk.reachable(h.second))
	}
	report, err := Fsck(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}
	missing := map[hash.ObjectID]bool{}
	for _, problem := range report.Problems {
		missing[problem.ID] = problem.Kind == FsckMissing
	}
	if !missing[lostCommit] || !missing[lostBlob] {
		t.Fatalf("fsck stopped reporting dangling roots: %+v", report.Problems)
	}
}

func TestResolveUndoKeepsTheBlobsOfAResolvedConflict(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	ours := putMaintBlob(t, r, "ours before the conflict was resolved\n")
	theirs := putMaintBlob(t, r, "theirs before the conflict was resolved\n")
	absentStage := hash.SumSHA1("blob", []byte("a stage the conflict never had"))
	pruned := hash.SumSHA1("blob", []byte("a stage pruned by another tool"))
	writeResolveUndoIndex(t, r,
		index.ResolveUndoEntry{Path: "conflict.txt", Modes: [3]object.Mode{0, object.ModeBlob, object.ModeBlob}, IDs: [3]hash.ObjectID{absentStage, ours, theirs}},
		index.ResolveUndoEntry{Path: "gone.txt", Modes: [3]object.Mode{object.ModeBlob}, IDs: [3]hash.ObjectID{pruned}},
	)

	walk := mustWalk(t, r)

	if !walk.reachable(ours) || !walk.reachable(theirs) || walk.reachable(absentStage) || walk.reachable(pruned) {
		t.Fatalf("ours = %v, theirs = %v, absent stage = %v, pruned = %v", walk.reachable(ours), walk.reachable(theirs), walk.reachable(absentStage), walk.reachable(pruned))
	}
}

func TestRebaseStateFilesKeepTheirCommitsAlive(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	autostash := putMaintCommit(t, r, h.tree1)
	origHead := putMaintCommit(t, r, h.tree2)
	writeGitFile(t, r.repo.GitDir(), "rebase-merge/autostash", autostash.String()+"\n")
	writeGitFile(t, r.repo.GitDir(), "rebase-apply/orig-head", origHead.String()+"\n")
	writeGitFile(t, r.repo.GitDir(), "rebase-apply/autostash", "not an object name\n")

	walk := mustWalk(t, r)

	if !walk.reachable(autostash) || !walk.reachable(origHead) {
		t.Fatalf("autostash = %v, orig-head = %v", walk.reachable(autostash), walk.reachable(origHead))
	}
}

func TestARootThatCannotBeCheckedStopsGatheringRoots(t *testing.T) {
	unseen := hash.SumSHA1("commit", []byte("never looked at"))
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo)
	}{
		{"the old side of a reflog entry", func(t *testing.T, r *testRepo) {
			line := unseen.String() + " " + hash.Zero.String() + " ann <ann@example.com> 1700000000 +0000\tupdate\n"
			writeGitFile(t, r.repo.CommonDir(), "logs/refs/heads/odd", line)
		}},
		{"the new side of a reflog entry", func(t *testing.T, r *testRepo) {
			writeReflogTo(t, r, unseen)
		}},
		{"an index entry", func(t *testing.T, r *testRepo) {
			idx := r.index()
			idx.Add(index.Entry{Path: "f.txt", Mode: object.ModeBlob, ID: unseen})
			r.saveIndex(idx)
		}},
		{"a resolve-undo stage", func(t *testing.T, r *testRepo) {
			writeResolveUndoIndex(t, r, index.ResolveUndoEntry{Path: "f.txt", Modes: [3]object.Mode{object.ModeBlob}, IDs: [3]hash.ObjectID{unseen}})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			tt.setup(t, r)
			db, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
			if err != nil {
				t.Fatal(err)
			}
			walk, err := newObjectWalk(t.Context(), r.repo, db)
			if err != nil {
				t.Fatal(err)
			}
			_ = db.Close()

			if err := walk.gatherRoots(r.repo); err == nil {
				t.Fatal("a root whose presence could not be checked was ignored")
			}
		})
	}
}

func TestGCKeepsAnOldLooseObjectThatWasWrittenAgain(t *testing.T) {
	r, _, old, _ := gcRepo(t, "")
	content := "garbage that a new commit is about to reuse\n"
	reused := putMaintBlob(t, r, content)
	ageFile(t, loosePath(r, reused), time.Now().AddDate(0, 0, -30))
	if again := putMaintBlob(t, r, content); again != reused {
		t.Fatalf("the second write named %s, want %s", again, reused)
	}

	if _, err := GC(t.Context(), r.repo); err != nil {
		t.Fatal(err)
	}

	if !looseExists(r, reused) || looseExists(r, old) {
		t.Fatalf("reused kept = %v, untouched garbage kept = %v", looseExists(r, reused), looseExists(r, old))
	}
}

func TestGCKeepsAnObjectOfAnOldPackThatWasWrittenAgain(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	content := "packed long ago and needed by a commit being made\n"
	blob := putMaintBlob(t, r, content)
	packMaintObjects(t, r, []hash.ObjectID{blob})
	if err := r.db().RemoveLoose(blob); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.repo.PackDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		ageFile(t, filepath.Join(r.repo.PackDir(), entry.Name()), time.Now().AddDate(0, 0, -30))
	}
	writer, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Put(object.TypeBlob, []byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := GC(t.Context(), r.repo); err != nil {
		t.Fatal(err)
	}

	if has, err := r.db().Has(blob); !has || err != nil {
		t.Fatalf("an object written again into an old pack was dropped: %v, %v", has, err)
	}
}

func TestGCToleratesDanglingReflogAndIndexEntries(t *testing.T) {
	r, h, _, _ := gcRepo(t, "")
	writeReflogTo(t, r, hash.SumSHA1("commit", []byte("pruned by another tool")))
	idx := r.index()
	idx.Add(index.Entry{Path: "lost.txt", Mode: object.ModeBlob, ID: hash.SumSHA1("blob", []byte("lost"))})
	r.saveIndex(idx)

	result, err := GC(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}

	if result.CommitGraph != 2 || r.branchTarget("main") != h.second {
		t.Fatalf("result = %+v", result)
	}
}
