package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func (p *libProject) deinit(t *testing.T, paths []string, opts SubmoduleDeinitOptions) ([]SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	opts.Events = events
	err := SubmoduleDeinit(t.Context(), p.super.reopen(), paths, opts)
	return *got, err
}

func (p *libProject) removeSubmodule(t *testing.T, paths []string, opts SubmoduleRemoveOptions) ([]SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	opts.Events = events
	err := SubmoduleRemove(t.Context(), p.super.reopen(), paths, opts)
	return *got, err
}

func TestSubmoduleDeinitClearsTheWorkTreeAndUnregistersLikeGit(t *testing.T) {
	p := updatedLibProject(t)

	events, err := p.deinit(t, []string{"libs/lib"}, SubmoduleDeinitOptions{})

	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCleared, SubmoduleUnregistered}) {
		t.Fatalf("SubmoduleDeinit = %+v, %v", events, err)
	}
	if entries, err := os.ReadDir(p.libDir()); err != nil || len(entries) != 0 {
		t.Fatalf("the submodule directory holds %v, %v", entries, err)
	}
	if strings.Contains(p.super.configText(), "submodule") || strings.Contains(p.super.readFile(".git/modules/lib/config"), "worktree") {
		t.Fatalf("config = %q", p.super.configText())
	}
	if events, err := p.deinit(t, nil, SubmoduleDeinitOptions{All: true}); err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCleared}) {
		t.Fatalf("a second deinit = %+v, %v", events, err)
	}
}

func TestSubmoduleDeinitRefusesWhatGitRefuses(t *testing.T) {
	p := updatedLibProject(t)
	if _, err := p.deinit(t, nil, SubmoduleDeinitOptions{}); !errors.Is(err, ErrSubmoduleDeinitNoPaths) {
		t.Fatalf("no paths returned %v", err)
	}
	if _, err := p.deinit(t, []string{"missing"}, SubmoduleDeinitOptions{}); !errors.Is(err, ErrSubmodulePathspec) {
		t.Fatalf("an unmatched path returned %v", err)
	}
	p.super.writeFile("libs/lib/lib.txt", "changed\n")
	if _, err := p.deinit(t, []string{"libs/lib"}, SubmoduleDeinitOptions{}); !errors.Is(err, ErrSubmoduleLocalChanges) {
		t.Fatalf("a modified submodule returned %v", err)
	}
	if _, err := p.deinit(t, []string{"libs/lib"}, SubmoduleDeinitOptions{Force: true}); err != nil {
		t.Fatalf("a forced deinit returned %v", err)
	}
}

func TestSubmoduleDeinitSkipsUnknownGitlinksAndForeignGitDirs(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.super.setGitlink("stray", p.one)
	p.super.commitAll("stray")
	foreign, err := Clone(t.Context(), p.world.url("lib"), p.libDir(), CloneOptions{SeparateGitDir: filepath.Join(p.world.root, "foreign"), Open: p.super.openOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_ = foreign.Close()
	p.super.appendConfig("[submodule \"lib\"]\n\turl = x\n")

	events, err := p.deinit(t, nil, SubmoduleDeinitOptions{All: true, Force: true})

	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCleared, SubmoduleUnregistered}) {
		t.Fatalf("SubmoduleDeinit = %+v, %v", events, err)
	}
}

func TestSubmoduleDeinitToleratesASectionOnlyInGlobalConfig(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	if err := os.WriteFile(p.world.global, []byte(submoduleGlobalConfig+"[submodule \"lib\"]\n\turl = elsewhere\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	events, err := p.deinit(t, []string{"libs/lib"}, SubmoduleDeinitOptions{})

	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleUnregistered}) {
		t.Fatalf("SubmoduleDeinit = %+v, %v", events, err)
	}
}

func TestSubmoduleRemoveDropsTheSubmoduleAndItsGitmodulesSection(t *testing.T) {
	p := updatedLibProject(t)

	events, err := p.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{})

	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleRemoved}) || p.super.exists("libs/lib") {
		t.Fatalf("SubmoduleRemove = %+v, %v", events, err)
	}
	if _, ok := p.super.index().Get("libs/lib", 0); ok || p.super.readFile(".gitmodules") != "" {
		t.Fatalf(".gitmodules = %q", p.super.readFile(".gitmodules"))
	}
	if staged, ok := p.super.index().Get(".gitmodules", 0); !ok || staged.ID != mustHashBlob(t, "") {
		t.Fatal(".gitmodules was not staged")
	}
	if !p.super.exists(".git/modules/lib/HEAD") || !strings.Contains(p.super.configText(), "[submodule \"lib\"]") {
		t.Fatal("the git directory or the registration was dropped")
	}
}

func TestSubmoduleRemoveRefusesWhatGitRmRefuses(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, p *libProject)
		paths   []string
		force   bool
		want    error
	}{
		{"path outside the work tree", nil, []string{"../x"}, false, ErrInvalidPath},
		{"not a submodule", nil, []string{"super.txt"}, false, ErrSubmoduleNotAGitlink},
		{"unstaged .gitmodules", func(t *testing.T, p *libProject) { p.super.writeFile(".gitmodules", libGitmodules+"\n") }, []string{"libs/lib"}, true, ErrGitmodulesUnstaged},
		{"new commits", func(t *testing.T, p *libProject) { p.record(p.two) }, []string{"libs/lib"}, false, ErrSubmoduleLocalChanges},
		{"staged move", func(t *testing.T, p *libProject) {
			p.super.setGitlink("libs/lib", p.two)
			mustCheckout(t, p, p.two)
		}, []string{"libs/lib"}, false, ErrSubmoduleStagedChanges},
		{"staged move with local changes", func(t *testing.T, p *libProject) {
			p.super.setGitlink("libs/lib", p.two)
			p.super.writeFile("libs/lib/lib.txt", "changed\n")
		}, []string{"libs/lib"}, false, ErrSubmoduleStagedChanges},
		{"file in place of the submodule", func(t *testing.T, p *libProject) {
			if err := os.RemoveAll(p.libDir()); err != nil {
				t.Fatal(err)
			}
			p.super.writeFile("libs/lib", "file\n")
		}, []string{"libs/lib"}, false, ErrSubmoduleLocalChanges},
		{"directory without a git file", func(t *testing.T, p *libProject) {
			if err := os.Remove(p.super.path("libs/lib/.git")); err != nil {
				t.Fatal(err)
			}
		}, []string{"libs/lib"}, false, ErrSubmoduleLocalChanges},
		{"broken git directory", func(t *testing.T, p *libProject) {
			if err := os.Remove(p.super.path("libs/lib/.git")); err != nil {
				t.Fatal(err)
			}
			p.super.writeFile("libs/lib/.git/stray", "x\n")
		}, []string{"libs/lib"}, false, ErrSubmoduleBrokenGitDir},
		{"embedded repository with worktrees", func(t *testing.T, p *libProject) {
			embedAbsorbedLib(t, p.super)
			p.super.writeFile("libs/lib/.git/worktrees/w/HEAD", "x\n")
		}, []string{"libs/lib"}, false, ErrSubmoduleWorktrees},
		{"git directory in the way", func(t *testing.T, p *libProject) {
			embedAbsorbedLib(t, p.super)
			if err := os.Remove(p.super.path(".git/modules")); err != nil {
				t.Fatal(err)
			}
			p.super.writeFile(".git/modules", "file\n")
		}, []string{"libs/lib"}, false, nil},
		{"nested git directory of another module", func(t *testing.T, p *libProject) {
			embedAbsorbedLib(t, p.super)
			p.super.writeFile(".gitmodules", "[submodule \"lib/inner\"]\n\tpath = libs/lib\n\turl = ../lib\n")
			mustStage(t, p.super, ".gitmodules")
			bare, err := repo.Init(p.super.path(".git/modules/lib"), repo.InitOptions{Bare: true, NoSystem: true})
			if err != nil {
				t.Fatal(err)
			}
			_ = bare.Close()
		}, []string{"libs/lib"}, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := updatedLibProject(t)
			if tc.prepare != nil {
				tc.prepare(t, p)
			}
			_, err := p.removeSubmodule(t, tc.paths, SubmoduleRemoveOptions{Force: tc.force})
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("SubmoduleRemove returned %v, want %v", err, tc.want)
			}
		})
	}
}

func mustHashBlob(t *testing.T, text string) hash.ObjectID {
	t.Helper()
	return hash.SumSHA1("blob", []byte(text))
}

func mustCheckout(t *testing.T, p *libProject, commit hash.ObjectID) {
	t.Helper()
	if err := Switch(t.Context(), p.submoduleRepo(t).repo, commit.String(), SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmoduleRemoveAbsorbsEmbeddedAndReconnectsNestedRepositories(t *testing.T) {
	n := updatedNestedProject(t)
	embedAbsorbedLib(t, n.super)
	n.super.writeFile("libs/lib/inner/.git", "gitdir: ../nowhere\n")

	events, err := n.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true})

	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleAbsorbed, SubmoduleRemoved}) {
		t.Fatalf("SubmoduleRemove = %+v, %v", events, err)
	}
	if !n.super.exists(".git/modules/lib/HEAD") {
		t.Fatal("the embedded git directory was not absorbed")
	}
}

func TestSubmoduleRemoveNeedsANameToAbsorbOrReconnect(t *testing.T) {
	p := updatedLibProject(t)
	p.super.setGitlink("stray", p.one)
	p.super.writeFile("stray/.git", "gitdir: ../nowhere\n")
	if _, err := p.removeSubmodule(t, []string{"stray"}, SubmoduleRemoveOptions{Force: true}); !errors.Is(err, ErrSubmoduleUnknownName) {
		t.Fatalf("a broken git file without a name returned %v", err)
	}

	p = updatedLibProject(t)
	p.super.setGitlink("vendored", p.one)
	vendored, err := Clone(t.Context(), p.world.url("lib"), p.super.path("vendored"), CloneOptions{Open: p.super.openOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_ = vendored.Close()
	if _, err := p.removeSubmodule(t, []string{"vendored"}, SubmoduleRemoveOptions{Force: true}); !errors.Is(err, ErrSubmoduleUnknownName) {
		t.Fatalf("an embedded repository without a name returned %v", err)
	}

	p = updatedLibProject(t)
	p.super.writeFile("libs/lib/.git", "gitdir: ../nowhere\n")
	failConfigWriteAt(t, 1)
	if _, err := p.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true}); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("a failing reconnection returned %v", err)
	}
}

func TestSubmoduleRemoveHandlesUnusualGitmodulesStates(t *testing.T) {
	p := updatedLibProject(t)
	p.super.setGitlink("stray", p.one)
	if _, err := p.removeSubmodule(t, []string{"stray"}, SubmoduleRemoveOptions{Force: true}); err != nil {
		t.Fatalf("an unregistered gitlink returned %v", err)
	}

	p.super.remove(".gitmodules")
	if _, err := p.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true}); err != nil {
		t.Fatalf("a missing .gitmodules returned %v", err)
	}

	p = updatedLibProject(t)
	idx := p.super.index()
	staged, _ := idx.Get(".gitmodules", 0)
	idx.Remove(".gitmodules")
	for _, stage := range []index.Stage{1, 2} {
		idx.Add(index.Entry{Path: ".gitmodules", Mode: object.ModeBlob, ID: staged.ID, Stage: stage})
	}
	p.super.saveIndex(idx)
	if _, err := p.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true}); !errors.Is(err, ErrGitmodulesUnmergedChange) {
		t.Fatalf("an unmerged .gitmodules returned %v", err)
	}
}

func TestSubmoduleRemoveSkipsEmptyUnmergedSubmodulesAndUnpopulatedNestedOnes(t *testing.T) {
	n := updatedNestedProject(t)
	if err := os.RemoveAll(n.super.path("libs/lib/inner")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(n.super.path("libs/lib/inner"), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := n.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{}); err != nil {
		t.Fatalf("an unpopulated nested submodule returned %v", err)
	}

	p := newLibProject(t, libGitmodules)
	idx := p.super.index()
	idx.Remove("libs/lib")
	for _, stage := range []index.Stage{2, 3} {
		idx.Add(index.Entry{Path: "libs/lib", Mode: object.ModeSubmodule, ID: p.one, Stage: stage})
	}
	p.super.saveIndex(idx)
	if err := os.MkdirAll(p.libDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := p.removeSubmodule(t, []string{"libs/lib"}, SubmoduleRemoveOptions{}); err != nil {
		t.Fatalf("an empty unmerged submodule returned %v", err)
	}
}

func TestEditConfigFileReportsParseAndEditFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("[broken"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := editConfigFile(path, func(*config.File) error { return nil }); err == nil {
		t.Fatal("a broken config file was accepted")
	}
	if err := editConfigFile(filepath.Join(t.TempDir(), "fresh"), func(*config.File) error { return errSubmoduleFault }); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("an edit failure returned %v", err)
	}
	if err := editConfigFile(t.TempDir(), func(*config.File) error { return nil }); err == nil {
		t.Fatal("a directory was read as a config file")
	}
}
