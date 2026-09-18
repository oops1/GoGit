package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/linelog"
)

func TestOpenCommitGraphFollowsTheRepositorySettings(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	if graph, err := OpenCommitGraph(r.repo, r.db()); graph != nil || err != nil {
		t.Fatalf("a repository without a graph returned %v, %v", graph, err)
	}
	if _, err := WriteCommitGraph(t.Context(), r.repo); err != nil {
		t.Fatal(err)
	}
	if graph, err := OpenCommitGraph(r.repo, r.db()); graph == nil || err != nil || graph.Len() != 2 {
		t.Fatalf("a written graph returned %v, %v", graph, err)
	}
	r.appendConfig("[core]\n\tcommitGraph = false\n")
	r.repo = r.reopen()
	if graph, err := OpenCommitGraph(r.repo, r.db()); graph != nil || err != nil {
		t.Fatalf("a switched off graph returned %v, %v", graph, err)
	}
}

func TestReadersReportBrokenCommitGraphSettings(t *testing.T) {
	tr := newTestRepo(t)
	tr.comparableFork()
	tr.appendConfig("[core]\n\tcommitGraph = maybe\n")
	tr.repo = tr.reopen()

	if _, err := Compare(t.Context(), tr.repo, "main", "feature", CompareOptions{}); !errors.Is(err, config.ErrInvalidBool) {
		t.Errorf("Compare returned %v", err)
	}
	if _, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{}); !errors.Is(err, config.ErrInvalidBool) {
		t.Errorf("Details returned %v", err)
	}
	if _, err := FileHistory(t.Context(), tr.repo, "HEAD", "f", HistoryOptions{}); !errors.Is(err, config.ErrInvalidBool) {
		t.Errorf("FileHistory returned %v", err)
	}
	if _, err := lineHistoryOf(t, tr.repo, "HEAD", []linelog.Spec{{Range: "1", Path: "f"}}, LineHistoryOptions{}); !errors.Is(err, config.ErrInvalidBool) {
		t.Errorf("LineHistory returned %v", err)
	}
}
