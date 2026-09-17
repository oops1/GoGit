package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

func (p *libProject) cloneSuper(t *testing.T, name string, opts CloneOptions) *testRepo {
	t.Helper()
	dir := filepath.Join(p.world.root, name)
	opts.Open = repo.OpenOptions{NoSystem: true, GlobalFile: p.world.global}
	r, err := Clone(t.Context(), p.world.url("super"), dir, opts)
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: t, dir: dir, repo: r, clock: 1900000000, globalFile: p.world.global}
}

func TestCloneWithRecurseSubmodulesInitialisesAndChecksOutEverySubmodule(t *testing.T) {
	n := newNestedProject(t)
	events, got := collectEvents()

	clone := n.cloneSuper(t, "clone", CloneOptions{RecurseSubmodules: true, SubmoduleEvents: events})

	config := clone.configText()
	if !strings.Contains(config, "[submodule]\n\tactive = .\n[remote \"origin\"]") || strings.Contains(config, "recurse") {
		t.Fatalf("config = %q", config)
	}
	if submoduleHead(t, clone.path("libs/lib")) != n.two || submoduleHead(t, clone.path("libs/lib/inner")) != n.innerOne {
		t.Fatal("the submodules were not checked out")
	}
	if !slices.Contains(eventKinds(*got), SubmoduleCloning) {
		t.Fatalf("events = %+v", *got)
	}
}

func TestCloneWithStickyRecursionRecordsSubmoduleRecurse(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	if err := os.WriteFile(p.world.global, []byte(submoduleGlobalConfig+"[submodule]\n\tstickyRecursiveClone = true\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	clone := p.cloneSuper(t, "sticky", CloneOptions{RecurseSubmodules: true, ShallowSubmodules: true})

	if config := clone.configText(); !strings.Contains(config, "[submodule]\n\tactive = .\n\trecurse = true\n") {
		t.Fatalf("config = %q", config)
	}
	if clone.readFile(".git/modules/lib/shallow") == "" {
		t.Fatal("the shallow submodule clone left no shallow file")
	}
}

func TestCloneStopsBeforeSubmodulesWhenTheHookRejectsTheCheckout(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	hooksDir := filepath.Join(p.world.root, "hooks")
	installTestHook(t, hooksDir, hookPostCheckout, testHook{exit: 2})
	if err := os.WriteFile(p.world.global, []byte(submoduleGlobalConfig+"[core]\n\thooksPath = "+filepath.ToSlash(hooksDir)+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(p.world.root, "hooked")
	r, err := Clone(t.Context(), p.world.url("super"), dir, CloneOptions{
		RecurseSubmodules: true,
		Open:              repo.OpenOptions{NoSystem: true, GlobalFile: p.world.global},
	})
	if r != nil {
		_ = r.Close()
	}
	if err == nil || fileExists(filepath.Join(dir, "libs", "lib", ".git")) {
		t.Fatalf("Clone returned %v and cloned the submodule anyway", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestCloneCleansUpASeparateGitDirAfterAFailure(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, "modules", "x")
	_, err := Clone(t.Context(), filepath.Join(root, "missing"), filepath.Join(root, "work"), CloneOptions{SeparateGitDir: gitDir})
	if err == nil || fileExists(gitDir) {
		t.Fatalf("Clone returned %v and kept %s", err, gitDir)
	}
}

func TestSwitchUpdatesChangedSubmodulesWhenRecursionIsOn(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.super.createBranch("topic", p.super.branchTarget("main"))
	if err := Switch(t.Context(), p.super.reopen(), "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	p.record(p.two)
	if err := Switch(t.Context(), p.super.reopen(), "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if p.libHead(t) != p.one {
		t.Fatal("a switch without recursion moved the submodule")
	}

	p.super.appendConfig("[submodule]\n\trecurse = true\n")
	events, got := collectEvents()
	if err := Switch(t.Context(), p.super.reopen(), "topic", SwitchOptions{SubmoduleEvents: events}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if p.libHead(t) != p.two || !slices.Equal(eventKinds(*got), []SubmoduleEventKind{SubmoduleCheckedOut}) {
		t.Fatalf("head = %s, events = %+v", p.libHead(t), *got)
	}
	p.super.createBranch("same", p.super.branchTarget("topic"))
	*got = nil
	if err := Switch(t.Context(), p.super.reopen(), "same", SwitchOptions{SubmoduleEvents: events}); err != nil || len(*got) != 0 {
		t.Fatalf("a switch without gitlink changes = %v, %+v", err, *got)
	}
}

func TestSwitchRecursionSkipsUnchangedAndUnpopulatedSubmodules(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"steady\"]\n\tpath = steady\n\turl = ../lib\n[submodule \"absent\"]\n\tpath = absent\n\turl = ../lib\n")
	p.super.setGitlink("steady", p.one)
	p.super.setGitlink("absent", p.one)
	p.super.commitAll("more")
	if _, err := p.update(t, []string{"libs/lib", "steady"}, SubmoduleUpdateOptions{Init: true}); err != nil {
		t.Fatal(err)
	}
	p.super.appendConfig("[submodule \"absent\"]\n\tactive = true\n")
	p.super.createBranch("topic", p.super.branchTarget("main"))
	if err := Switch(t.Context(), p.super.reopen(), "topic", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	p.super.setGitlink("absent", p.two)
	p.record(p.two)
	if err := Switch(t.Context(), p.super.reopen(), "main", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	p.super.writeFile("steady/lib.txt", "moved away\n")

	events, got := collectEvents()
	if err := Switch(t.Context(), p.super.reopen(), "topic", SwitchOptions{RecurseSubmodules: true, SubmoduleEvents: events}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if !slices.Equal(eventKinds(*got), []SubmoduleEventKind{SubmoduleCheckedOut}) || (*got)[0].Path != "libs/lib" || p.super.exists("absent/.git") {
		t.Fatalf("events = %+v", *got)
	}
}

func TestSwitchWithExplicitRecursionIgnoresTheConfig(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.super.createBranch("topic", p.super.branchTarget("main"))
	if err := Switch(t.Context(), p.super.reopen(), "topic", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	p.record(p.two)
	p.super.appendConfig("[submodule]\n\trecurse = false\n")

	if err := Switch(t.Context(), p.super.reopen(), "main", SwitchOptions{RecurseSubmodules: true}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if p.libHead(t) != p.one {
		t.Fatal("explicit recursion did not move the submodule back")
	}
}

func TestSwitchRefusesABrokenRecurseSetting(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.super.createBranch("topic", p.super.branchTarget("main"))
	p.super.appendConfig("[submodule]\n\trecurse = sometimes\n")

	if err := Switch(t.Context(), p.super.reopen(), "topic", SwitchOptions{}); err == nil {
		t.Fatal("Switch accepted submodule.recurse = sometimes")
	}
}

func TestPullUpdatesSubmodulesWhenRecursionIsOn(t *testing.T) {
	tests := []struct {
		name   string
		config string
		kind   SubmoduleEventKind
	}{
		{"merge", "", SubmoduleCheckedOut},
		{"rebase", "[pull]\n\trebase = true\n", SubmoduleRebased},
		{"rebase with ff only", "[pull]\n\trebase = true\n\tff = only\n", SubmoduleCheckedOut},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			clone := p.cloneSuper(t, "work", CloneOptions{RecurseSubmodules: true})
			clone.appendConfig("[submodule]\n\trecurse = true\n" + tc.config)
			p.record(p.two)
			events, got := collectEvents()

			_, err := Pull(t.Context(), clone.reopen(), PullOptions{SubmoduleEvents: events})

			if err != nil || submoduleHead(t, clone.path("libs/lib")) != p.two || !slices.Contains(eventKinds(*got), tc.kind) {
				t.Fatalf("Pull = %v, events = %+v", err, *got)
			}
		})
	}
}

func TestPullLeavesSubmodulesAloneWithoutRecursionOrOnFailure(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	clone := p.cloneSuper(t, "work", CloneOptions{RecurseSubmodules: true})
	p.record(p.two)

	if _, err := Pull(t.Context(), clone.reopen(), PullOptions{}); err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if submoduleHead(t, clone.path("libs/lib")) != p.one {
		t.Fatal("a pull without recursion moved the submodule")
	}

	clone.appendConfig("[submodule]\n\trecurse = maybe\n")
	if _, err := Pull(t.Context(), clone.reopen(), PullOptions{}); err == nil {
		t.Fatal("Pull accepted submodule.recurse = maybe")
	}

	clone.writeRawHead(clone.branchTarget("main").String() + "\n")
	if _, err := Pull(t.Context(), clone.reopen(), PullOptions{RecurseSubmodules: true}); !errors.Is(err, ErrDetachedHead) {
		t.Fatalf("a detached pull returned %v", err)
	}
}
