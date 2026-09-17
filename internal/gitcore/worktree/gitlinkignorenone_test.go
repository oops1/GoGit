package worktree

import "testing"

func TestIgnoreNoSubmoduleOverridesEveryIgnoreSetting(t *testing.T) {
	super := dirtySubmoduleFixture(t, libModule+"\tignore = all\n", "[diff]\n\tignoreSubmodules = all\n[submodule \"lib\"]\n\tignore = all\n")

	entry, ok := submoduleEntry(t, super.openWith(Options{IgnoreNoSubmodule: true}))

	want := SubmoduleChange{CommitChanged: true, Modified: true, Untracked: true}
	if !ok || entry.Submodule != want {
		t.Fatalf("sub = %+v, %v; want %+v", entry, ok, want)
	}
}
