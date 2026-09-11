package merge

import (
	"slices"
	"testing"
)

func TestValidateExplainsWhatTheMergeWillDo(t *testing.T) {
	known := Known{Current: "main", Candidates: []string{"feature", "origin/main"}}
	for _, c := range []struct {
		name string
		req  Request
		key  string
		args []any
		ok   bool
	}{
		{"nothing chosen", Request{Source: "  "}, hintSourceRequired, nil, false},
		{"the current branch", Request{Source: "main"}, hintSourceIsCurrent, []any{"main"}, false},
		{"fast-forward", Request{Source: "feature"}, hintFastForward, []any{"feature", "main"}, true},
		{"merge commit", Request{Source: "feature", Mode: ModeMergeCommit}, hintMergeCommit, []any{"feature", "main"}, true},
		{"fast-forward only", Request{Source: "feature", Mode: ModeFastForwardOnly}, hintFastForwardOnly, []any{"main", "feature"}, true},
		{"squash", Request{Source: "feature", Mode: ModeSquash, NoCommit: true}, hintSquash, []any{"feature"}, true},
		{"no commit", Request{Source: "feature", NoCommit: true}, hintNoCommit, []any{"feature"}, true},
	} {
		got := Validate(c.req, known)
		if got.Key != c.key || got.OK != c.ok || !slices.Equal(got.Args, c.args) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func TestOnlyCommittingModesCanStopBeforeTheCommit(t *testing.T) {
	for mode, want := range map[Mode]bool{ModeFastForward: true, ModeMergeCommit: true, ModeFastForwardOnly: false, ModeSquash: false} {
		if got := mode.AllowsNoCommit(); got != want {
			t.Errorf("mode %d: %v, want %v", mode, got, want)
		}
	}
}
