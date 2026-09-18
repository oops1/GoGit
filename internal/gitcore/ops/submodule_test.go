package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

const submoduleGlobalConfig = "[user]\n\tname = ann\n\temail = ann@example.com\n[protocol \"file\"]\n\tallow = always\n"

type submoduleWorld struct {
	t      *testing.T
	root   string
	global string
}

func newSubmoduleWorld(t *testing.T) *submoduleWorld {
	t.Helper()
	root := t.TempDir()
	global := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(global, []byte(submoduleGlobalConfig), 0o666); err != nil {
		t.Fatal(err)
	}
	return &submoduleWorld{t: t, root: root, global: global}
}

func (w *submoduleWorld) repoAt(name string) *testRepo {
	w.t.Helper()
	dir := filepath.Join(w.root, filepath.FromSlash(name))
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", NoSystem: true, GlobalFile: w.global})
	if err != nil {
		w.t.Fatalf("repo.Init returned error %v", err)
	}
	w.t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: w.t, dir: dir, repo: r, clock: 1700000000, globalFile: w.global}
}

func (w *submoduleWorld) url(name string) string {
	return filepath.ToSlash(filepath.Join(w.root, filepath.FromSlash(name)))
}

func (r *testRepo) commitFile(rel, text, message string) hash.ObjectID {
	r.t.Helper()
	r.writeFile(rel, text)
	mustStage(r.t, r, rel)
	return r.commitAll(message)
}

func (r *testRepo) setGitlink(path string, id hash.ObjectID) {
	r.t.Helper()
	idx := r.index()
	idx.Add(index.Entry{Path: path, Mode: object.ModeSubmodule, ID: id, Stage: index.StageMerged})
	r.saveIndex(idx)
}

func (r *testRepo) writeGitmodules(text string) {
	r.t.Helper()
	r.writeFile(".gitmodules", text)
	mustStage(r.t, r, ".gitmodules")
}

func (r *testRepo) configText() string {
	r.t.Helper()
	return r.readFile(".git/config")
}

type libProject struct {
	world *submoduleWorld
	lib   *testRepo
	one   hash.ObjectID
	two   hash.ObjectID
	super *testRepo
}

const libGitmodules = "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../lib\n"

func newLibProject(t *testing.T, gitmodules string) *libProject {
	t.Helper()
	w := newSubmoduleWorld(t)
	p := &libProject{world: w, lib: w.repoAt("lib")}
	p.one = p.lib.commitFile("lib.txt", "one\n", "one")
	p.two = p.lib.commitFile("lib.txt", "two\n", "two")
	p.super = w.repoAt("super")
	p.super.appendConfig("[remote \"origin\"]\n\turl = " + w.url("super") + "\n")
	p.super.repo = p.super.reopen()
	p.super.commitFile("super.txt", "super\n", "super")
	p.super.writeGitmodules(gitmodules)
	p.super.setGitlink("libs/lib", p.one)
	p.super.commitAll("add lib")
	return p
}

func collectEvents() (SubmoduleEvents, *[]SubmoduleEvent) {
	events := new([]SubmoduleEvent)
	return func(e SubmoduleEvent) { *events = append(*events, e) }, events
}

func eventKinds(events []SubmoduleEvent) []SubmoduleEventKind {
	kinds := make([]SubmoduleEventKind, 0, len(events))
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func submoduleHead(t *testing.T, dir string) hash.ObjectID {
	t.Helper()
	id, found, err := gitlink.HeadOf(dir)
	if err != nil || !found {
		t.Fatalf("HeadOf(%s) = %v, %v", dir, found, err)
	}
	return id
}

func TestSubmoduleInitRegistersTheModuleLikeGit(t *testing.T) {
	p := newLibProject(t, libGitmodules+"\tupdate = rebase\n")
	events, got := collectEvents()

	if err := SubmoduleInit(t.Context(), p.super.repo, nil, SubmoduleInitOptions{Events: events}); err != nil {
		t.Fatalf("SubmoduleInit returned error %v", err)
	}

	want := "[submodule \"lib\"]\n\tactive = true\n\turl = " + p.world.url("lib") + "\n\tupdate = rebase\n"
	if config := p.super.configText(); !strings.HasSuffix(config, want) {
		t.Fatalf("config = %q, want suffix %q", config, want)
	}
	if len(*got) != 1 || (*got)[0] != (SubmoduleEvent{Kind: SubmoduleRegistered, Path: "libs/lib", Name: "lib", URL: p.world.url("lib")}) {
		t.Fatalf("events = %+v", *got)
	}

	before := p.super.configText()
	p.super.repo = p.super.reopen()
	*got = nil
	if err := SubmoduleInit(t.Context(), p.super.repo, []string{"libs"}, SubmoduleInitOptions{Events: events}); err != nil {
		t.Fatalf("second SubmoduleInit returned error %v", err)
	}
	if p.super.configText() != before || len(*got) != 0 {
		t.Fatalf("a second init changed the config or reported %+v", *got)
	}
}

func TestSubmoduleInitOnlyTakesActiveModulesWhenSubmoduleActiveIsSet(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"other\"]\n\tpath = other\n\turl = ../other\n")
	p.super.setGitlink("other", p.one)
	p.super.commitAll("add other")
	p.super.appendConfig("[submodule]\n\tactive = other\n")
	p.super.repo = p.super.reopen()

	if err := SubmoduleInit(t.Context(), p.super.repo, nil, SubmoduleInitOptions{}); err != nil {
		t.Fatalf("SubmoduleInit returned error %v", err)
	}

	config := p.super.configText()
	if !strings.Contains(config, "[submodule \"other\"]\n\turl = "+p.world.url("other")) || strings.Contains(config, "[submodule \"lib\"]") {
		t.Fatalf("config = %q", config)
	}
}

func TestSubmoduleInitRefusesWhatGitRefuses(t *testing.T) {
	tests := []struct {
		name  string
		setup func(p *libProject) []string
		want  error
	}{
		{"unmatched pathspec", func(*libProject) []string { return []string{"nothing"} }, ErrSubmodulePathspec},
		{"invalid pathspec", func(*libProject) []string { return []string{":(unknown)x"} }, ErrSubmodulePathspec},
		{"module missing from .gitmodules", func(p *libProject) []string {
			p.super.writeGitmodules("[submodule \"else\"]\n\tpath = else\n\turl = ../else\n")
			return nil
		}, ErrSubmoduleNotRegistered},
		{"module without a url", func(p *libProject) []string {
			p.super.writeGitmodules("[submodule \"lib\"]\n\tpath = libs/lib\n")
			return nil
		}, ErrSubmoduleNotRegistered},
		{"broken .gitmodules", func(p *libProject) []string {
			p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath\n")
			return nil
		}, submodule.ErrInvalidGitmodules},
		{"broken active flag", func(p *libProject) []string {
			p.super.appendConfig("[submodule \"lib\"]\n\tactive = maybe\n")
			p.super.repo = p.super.reopen()
			return nil
		}, submodule.ErrInvalidActive},
		{"url that cannot be resolved", func(p *libProject) []string {
			p.super.writeGitmodules("[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../../../../../../../../../../../../../../../../../../../../../../../../lib\n")
			p.super.appendConfig("[remote \"origin\"]\n\turl = relative\n")
			p.super.repo = p.super.reopen()
			return nil
		}, submodule.ErrCannotStripURL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			paths := tc.setup(p)
			err := SubmoduleInit(t.Context(), p.super.repo, paths, SubmoduleInitOptions{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("SubmoduleInit returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSubmoduleUpdateClonesIntoTheAbsorbedLayout(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	events, got := collectEvents()

	err := SubmoduleUpdate(t.Context(), p.super.repo, nil, SubmoduleUpdateOptions{Init: true, Events: events})
	if err != nil {
		t.Fatalf("SubmoduleUpdate returned error %v", err)
	}

	if link := p.super.readFile("libs/lib/.git"); link != "gitdir: ../../.git/modules/lib\n" {
		t.Fatalf(".git = %q", link)
	}
	if config := p.super.readFile(".git/modules/lib/config"); !strings.Contains(config, "\tworktree = ../../../libs/lib\n") {
		t.Fatalf("submodule config = %q", config)
	}
	if head := p.super.readFile(".git/modules/lib/HEAD"); head != p.one.String()+"\n" {
		t.Fatalf("HEAD = %q, want the recorded commit detached", head)
	}
	if text := p.super.readFile("libs/lib/lib.txt"); text != "one\n" {
		t.Fatalf("lib.txt = %q", text)
	}
	want := []SubmoduleEventKind{SubmoduleRegistered, SubmoduleCloning, SubmoduleCheckedOut}
	if !slices.Equal(eventKinds(*got), want) {
		t.Fatalf("events = %+v", *got)
	}
}

func TestRealPathResolvesANameItsParentDoesNotHaveYet(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "libs", "lib")

	if got, want := realPath(missing), filepath.Join(realPath(parent), "libs", "lib"); got != want {
		t.Fatalf("realPath(%q) = %q, want %q", missing, got, want)
	}
}
