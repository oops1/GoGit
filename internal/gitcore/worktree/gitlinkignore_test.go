package worktree

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

func dirtySubmoduleFixture(t *testing.T, gitmodules, config string) *testRepo {
	t.Helper()
	super, nested, _ := submoduleFixture(t)
	if gitmodules != "" {
		super.writeFile(".gitmodules", gitmodules)
	}
	if config != "" {
		super.appendConfig(config)
		super.repo = super.reopen()
	}
	nested.stage("b.txt", "b\n")
	nested.commit("two")
	nested.saveIndex()
	nested.writeFile("a.txt", "changed\n")
	nested.writeFile("loose.txt", "loose\n")
	return super
}

const libModule = "[submodule \"lib\"]\n\tpath = sub\n\turl = ../lib\n"

func TestStatusHonoursTheSubmoduleIgnoreSetting(t *testing.T) {
	tests := []struct {
		name       string
		gitmodules string
		config     string
		want       SubmoduleChange
		hidden     bool
	}{
		{"no setting", libModule, "", SubmoduleChange{CommitChanged: true, Modified: true, Untracked: true}, false},
		{"none", libModule + "\tignore = none\n", "", SubmoduleChange{CommitChanged: true, Modified: true, Untracked: true}, false},
		{"untracked", libModule + "\tignore = untracked\n", "", SubmoduleChange{CommitChanged: true, Modified: true}, false},
		{"dirty", libModule + "\tignore = dirty\n", "", SubmoduleChange{CommitChanged: true}, false},
		{"all", libModule + "\tignore = all\n", "", SubmoduleChange{}, true},
		{"config beats gitmodules", libModule + "\tignore = all\n", "[submodule \"lib\"]\n\tignore = dirty\n", SubmoduleChange{CommitChanged: true}, false},
		{"diff.ignoreSubmodules without a module", "", "[diff]\n\tignoreSubmodules = dirty\n", SubmoduleChange{CommitChanged: true}, false},
		{"diff.ignoreSubmodules all", libModule, "[diff]\n\tignoreSubmodules = all\n", SubmoduleChange{}, true},
		{"module setting beats diff.ignoreSubmodules", libModule + "\tignore = untracked\n", "[diff]\n\tignoreSubmodules = all\n", SubmoduleChange{CommitChanged: true, Modified: true}, false},
		{"diff.ignoreSubmodules without a value", libModule, "[diff]\n\tignoreSubmodules\n", SubmoduleChange{CommitChanged: true, Modified: true, Untracked: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			super := dirtySubmoduleFixture(t, tc.gitmodules, tc.config)

			entry, ok := submoduleEntry(t, super.open())

			if tc.hidden {
				if ok {
					t.Fatalf("sub = %+v, want it hidden", entry)
				}
				return
			}
			if !ok || entry.Unstaged != StatusModified || entry.Submodule != tc.want {
				t.Fatalf("sub = %+v, %v; want %+v", entry, ok, tc.want)
			}
		})
	}
}

func TestIgnoreAllHidesADeletedSubmoduleButKeepsStagedChanges(t *testing.T) {
	super := newTestRepo(t)
	super.idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte("one")), Stage: index.StageMerged})
	super.commit("recorded")
	super.idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte("two")), Stage: index.StageMerged})

	entry, ok := submoduleEntry(t, super.open())
	if !ok || entry.Staged != StatusModified || entry.Unstaged != StatusDeleted {
		t.Fatalf("without a setting sub = %+v, %v", entry, ok)
	}

	super.writeFile(".gitmodules", libModule+"\tignore = all\n")
	entry, ok = submoduleEntry(t, super.open())

	if !ok || entry.Staged != StatusModified || entry.Unstaged != StatusUnmodified {
		t.Fatalf("sub = %+v, %v; want only the staged change", entry, ok)
	}
}

func TestIgnoreUntrackedAlsoHidesUntrackedContentOfNestedSubmodules(t *testing.T) {
	super, nested, _ := submoduleFixture(t)
	super.writeFile(".gitmodules", libModule+"\tignore = untracked\n")
	inner := newTestRepoAt(t, nested.path("inner"))
	inner.stage("i.txt", "i\n")
	innerPointer := inner.commit("inner")
	inner.saveIndex()
	inner.writeFile("loose.txt", "loose\n")
	nested.idx.Add(index.Entry{Path: "inner", Mode: object.ModeSubmodule, ID: innerPointer, Stage: index.StageMerged})
	nested.commit("with inner")
	nested.saveIndex()
	super.idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: nested.headCommit(), Stage: index.StageMerged})
	super.commit("moved")

	if entry, ok := submoduleEntry(t, super.open()); ok {
		t.Fatalf("sub = %+v, want nothing to report", entry)
	}
}

func TestStatusFailsOnBrokenIgnoreSettings(t *testing.T) {
	tests := []struct {
		name       string
		gitmodules string
		config     string
		want       error
	}{
		{"diff.ignoreSubmodules", libModule, "[diff]\n\tignoreSubmodules = sometimes\n", submodule.ErrInvalidIgnore},
		{"submodule ignore", libModule, "[submodule \"lib\"]\n\tignore = sometimes\n", submodule.ErrInvalidIgnore},
		{"gitmodules", "[submodule \"lib\"]\n\tpath\n", "", submodule.ErrInvalidGitmodules},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			super := dirtySubmoduleFixture(t, tc.gitmodules, tc.config)

			_, err := super.open().Status(t.Context())

			if !errors.Is(err, tc.want) {
				t.Fatalf("Status returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStatusReadsGitmodulesFromTheIndexWhenTheFileIsGone(t *testing.T) {
	super := dirtySubmoduleFixture(t, libModule+"\tignore = all\n", "")
	super.stage(".gitmodules", libModule+"\tignore = all\n")
	super.remove(".gitmodules")

	if entry, ok := submoduleEntry(t, super.open()); ok {
		t.Fatalf("sub = %+v, want it hidden by the staged .gitmodules", entry)
	}
}
