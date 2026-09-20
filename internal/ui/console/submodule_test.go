package console

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

func TestSubmoduleStatusOfARepositoryWithoutSubmodulesIsEmpty(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if got := r.run("submodule"); got != "" {
		t.Fatalf("out = %q", got)
	}
	if got := r.run("submodule status"); got != "" {
		t.Fatalf("out = %q", got)
	}
}

func TestSubmoduleInitSyncAndUpdateReportWhatTheyDid(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	tests := []struct {
		line string
		want string
	}{
		{"submodule init", "Initialised submodules"},
		{"submodule sync --recursive", "Synchronised submodule URLs"},
		{"submodule update --init --recursive --force", "Updated submodules"},
	}
	for _, tt := range tests {
		if got := r.run(tt.line); got != tt.want {
			t.Fatalf("%q = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestSubmoduleListsARegisteredSubmodule(t *testing.T) {
	inner := newTestRepo(t)
	inner.commit("inner", map[string]string{"in.txt": "in\n"})
	outer := newTestRepoAt(t, filepath.Join(t.TempDir(), "outer"))
	outer.commit("initial", map[string]string{"a.txt": "a\n"})
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")

	if _, err := ops.SubmoduleAdd(t.Context(), outer.repo, inner.dir, ops.SubmoduleAddOptions{Path: "sub"}); err != nil {
		t.Fatal(err)
	}
	outer.reopen()

	got := lines(outer.run("submodule status"))

	if len(got) != 1 || !strings.HasSuffix(got[0], " sub") {
		t.Fatalf("status = %#v", got)
	}
}

func TestSubmoduleRefusesAnUnknownSubcommand(t *testing.T) {
	r := newTestRepo(t)

	if err := r.runFails("submodule nonsense"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmoduleCommandsOfAnUnknownPathFail(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	for _, line := range []string{"submodule update nowhere", "submodule init nowhere", "submodule sync nowhere"} {
		if err := r.runFails(line); err == nil {
			t.Fatalf("%q must fail", line)
		}
	}
}

func TestSubmoduleMarkersFollowTheState(t *testing.T) {
	tests := []struct {
		name string
		item ops.Submodule
		want string
	}{
		{"not initialised", ops.Submodule{State: ops.SubmoduleStateNotInitialized}, "-"},
		{"new commits", ops.Submodule{State: ops.SubmoduleStateNewCommits}, "+"},
		{"conflict", ops.Submodule{State: ops.SubmoduleStateConflict}, "U"},
		{"up to date", ops.Submodule{State: ops.SubmoduleStateUpToDate}, " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := submoduleMarker(tt.item); got != tt.want {
				t.Fatalf("marker = %q, want %q", got, tt.want)
			}
		})
	}
}
