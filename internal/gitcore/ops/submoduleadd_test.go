package ops

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/submodule"
)

func (p *libProject) add(t *testing.T, url string, opts SubmoduleAddOptions) (SubmoduleAddResult, []SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	opts.Events = events
	result, err := SubmoduleAdd(t.Context(), p.super.reopen(), url, opts)
	return result, *got, err
}

func TestSubmoduleAddClonesRegistersAndStagesLikeGit(t *testing.T) {
	p := newLibProject(t, libGitmodules)

	result, events, err := p.add(t, "../lib", SubmoduleAddOptions{Path: "deps/copy/", Branch: "main"})

	if err != nil || result != (SubmoduleAddResult{Name: "deps/copy", Path: "deps/copy", URL: p.world.url("lib")}) {
		t.Fatalf("SubmoduleAdd = %+v, %v", result, err)
	}
	if !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCloning}) {
		t.Fatalf("events = %+v", events)
	}
	if gitmodules := p.super.readFile(".gitmodules"); !strings.HasSuffix(gitmodules, "[submodule \"deps/copy\"]\n\tpath = deps/copy\n\turl = ../lib\n\tbranch = main\n") {
		t.Fatalf(".gitmodules = %q", gitmodules)
	}
	config := p.super.configText()
	if !strings.Contains(config, "[submodule \"deps/copy\"]\n\turl = "+p.world.url("lib")+"\n\tactive = true\n") {
		t.Fatalf("config = %q", config)
	}
	entry, ok := p.super.index().Get("deps/copy", 0)
	if !ok || !entry.Mode.IsSubmodule() || entry.ID != p.two {
		t.Fatalf("index entry = %+v, %v", entry, ok)
	}
	if ref := strings.TrimSpace(p.super.readFile(".git/modules/deps/copy/HEAD")); ref != "ref: refs/heads/main" {
		t.Fatalf("submodule HEAD = %q", ref)
	}
}

func TestSubmoduleAddGuessesThePathAndKeepsAnExistingRepository(t *testing.T) {
	p := newLibProject(t, "[submodule \"core\"]\n\tpath = libs/lib\n\turl = ../lib\n")
	p.super.appendConfig("[submodule]\n\tactive = deps/*\n")

	result, _, err := p.add(t, p.world.url("lib"), SubmoduleAddOptions{})
	if err != nil || result.Path != "lib" || result.Existing {
		t.Fatalf("SubmoduleAdd = %+v, %v", result, err)
	}
	if head := submoduleHead(t, p.super.path("lib")); head != p.two {
		t.Fatalf("checked out %s", head)
	}

	if _, err := Clone(t.Context(), p.world.url("lib"), p.super.path("deps/vendored"), CloneOptions{Open: p.super.openOptions()}); err != nil {
		t.Fatal(err)
	}
	result, events, err := p.add(t, "../lib", SubmoduleAddOptions{Path: "deps/vendored"})
	if err != nil || !result.Existing || len(events) != 0 {
		t.Fatalf("an existing repository = %+v, %+v, %v", result, events, err)
	}
	if config := p.super.configText(); strings.Contains(config, "deps/vendored\"]\n\turl = "+p.world.url("lib")+"\n\tactive = true") {
		t.Fatalf("a path covered by submodule.active was activated: %q", config)
	}
}

func TestSubmoduleAddRefusesWhatGitRefuses(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(p *libProject)
		url     string
		opts    SubmoduleAddOptions
		want    error
	}{
		{"unstaged .gitmodules missing from the work tree", func(p *libProject) { p.super.remove(".gitmodules") }, "../lib", SubmoduleAddOptions{Path: "x"}, ErrGitmodulesNotInWorkTree},
		{"no directory name", nil, "https://.git", SubmoduleAddOptions{}, submodule.ErrNoDirectoryName},
		{"url neither absolute nor relative", nil, "lib", SubmoduleAddOptions{Path: "x"}, ErrSubmoduleURLNotAbsolute},
		{"path outside the work tree", nil, "../lib", SubmoduleAddOptions{Path: "../x"}, ErrInvalidPath},
		{"broken pathspec", nil, "../lib", SubmoduleAddOptions{Path: ":(bogus)x"}, nil},
		{"tracked file", nil, "../lib", SubmoduleAddOptions{Path: "super.txt"}, ErrSubmoduleInIndex},
		{"tracked file forced", nil, "../lib", SubmoduleAddOptions{Path: "super.txt", Force: true}, ErrSubmoduleInIndexNotLink},
		{"ignored path", func(p *libProject) { p.super.writeFile(".git/info/exclude", "deps/\n") }, "../lib", SubmoduleAddOptions{Path: "deps/x"}, ErrSubmodulePathIgnored},
		{"repository without commits", func(p *libProject) { p.world.repoAt("super/empty") }, "../lib", SubmoduleAddOptions{Path: "empty"}, ErrNoCommitCheckedOut},
		{"name used by another path", nil, "../lib", SubmoduleAddOptions{Path: "other", Name: "lib"}, ErrSubmoduleNameInUse},
		{"invalid name", nil, "../lib", SubmoduleAddOptions{Path: "x", Name: "../up"}, ErrSubmoduleInvalidName},
		{"plain directory", func(p *libProject) { p.super.writeFile("plain/a.txt", "a\n") }, "../lib", SubmoduleAddOptions{Path: "plain"}, ErrSubmodulePathNotRepo},
		{"git directory left behind", func(p *libProject) {
			if err := os.MkdirAll(p.super.path(".git/modules/x"), 0o777); err != nil {
				t.Fatal(err)
			}
		}, "../lib", SubmoduleAddOptions{Path: "x"}, ErrSubmoduleGitDirExists},
		{"missing branch", nil, "../lib", SubmoduleAddOptions{Path: "x", Branch: "nope"}, ErrSubmoduleCheckout},
		{"empty remote", func(p *libProject) { p.world.repoAt("void") }, "../void", SubmoduleAddOptions{Path: "x"}, ErrSubmoduleCheckout},
		{"broken active flag", func(p *libProject) {
			p.super.appendConfig("[submodule]\n\tactive = x\n[submodule \"x\"]\n\tactive = maybe\n")
		}, "../lib", SubmoduleAddOptions{Path: "x"}, submodule.ErrInvalidActive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			if tc.prepare != nil {
				tc.prepare(p)
			}
			_, _, err := p.add(t, tc.url, tc.opts)
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("SubmoduleAdd returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSubmoduleAddWithoutAGitmodulesFileAnywhereCreatesOne(t *testing.T) {
	w := newSubmoduleWorld(t)
	lib := w.repoAt("lib")
	lib.commitFile("lib.txt", "one\n", "one")
	super := w.repoAt("super")
	super.commitFile("super.txt", "super\n", "super")

	if _, err := SubmoduleAdd(t.Context(), super.reopen(), "../lib", SubmoduleAddOptions{}); err != nil {
		t.Fatalf("SubmoduleAdd returned error %v", err)
	}
	if gitmodules := super.readFile(".gitmodules"); gitmodules != "[submodule \"lib\"]\n\tpath = lib\n\turl = ../lib\n" {
		t.Fatalf(".gitmodules = %q", gitmodules)
	}
}

func TestSubmoduleAddForcedPicksAFreeNameAndReusesAGitDirectory(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"lib1\"]\n\tpath = elsewhere\n\turl = ../lib\n")

	result, _, err := p.add(t, "../lib", SubmoduleAddOptions{Path: "other", Name: "lib", Force: true})
	if err != nil || result.Name != "lib2" {
		t.Fatalf("SubmoduleAdd = %+v, %v", result, err)
	}

	if err := SubmoduleRemove(t.Context(), p.super.reopen(), []string{"other"}, SubmoduleRemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	result, events, err := p.add(t, "../lib", SubmoduleAddOptions{Path: "other", Name: "lib2", Force: true})
	if err != nil || len(events) != 0 || submoduleHead(t, p.super.path("other")) != p.two {
		t.Fatalf("reactivation = %+v, %+v, %v", result, events, err)
	}
}
