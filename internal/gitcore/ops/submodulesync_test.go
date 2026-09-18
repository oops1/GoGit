package ops

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

func (p *libProject) sync(t *testing.T, paths []string, recursive bool) ([]SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	err := SubmoduleSync(t.Context(), p.super.reopen(), paths, SubmoduleSyncOptions{Recursive: recursive, Events: events})
	return *got, err
}

func TestSubmoduleSyncRewritesBothUrls(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../moved\n")

	events, err := p.sync(t, nil, false)

	if err != nil || len(events) != 1 || events[0].Kind != SubmoduleSynchronized || events[0].URL != p.world.url("moved") {
		t.Fatalf("SubmoduleSync = %+v, %v", events, err)
	}
	if !strings.Contains(p.super.configText(), "\turl = "+p.world.url("moved")+"\n") {
		t.Fatalf("superproject config = %q", p.super.configText())
	}
	if config := p.super.readFile(".git/modules/lib/config"); !strings.Contains(config, "\turl = "+p.world.url("moved")+"\n") {
		t.Fatalf("submodule config = %q", config)
	}
}

func TestSubmoduleSyncClimbsOutOfTheSubmoduleForARelativeSuperprojectRemote(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.super.appendConfig("[remote \"origin\"]\n\turl = ../up/super\n")

	if _, err := p.sync(t, nil, false); err != nil {
		t.Fatalf("SubmoduleSync returned error %v", err)
	}

	if !strings.Contains(p.super.configText(), "\turl = ../up/lib\n") {
		t.Fatalf("superproject config = %q", p.super.configText())
	}
	if config := p.super.readFile(".git/modules/lib/config"); !strings.Contains(config, "\turl = ../../../up/lib\n") {
		t.Fatalf("submodule config = %q", config)
	}
}

func TestSubmoduleSyncSkipsInactiveAndOnlyRegistersUnpopulatedModules(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	before := p.super.configText()
	if events, err := p.sync(t, nil, false); err != nil || len(events) != 0 || p.super.configText() != before {
		t.Fatalf("an inactive module was synchronized: %+v, %v", events, err)
	}

	p.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")
	events, err := p.sync(t, []string{"libs/lib"}, false)
	if err != nil || len(events) != 0 || !strings.Contains(p.super.configText(), "\turl = "+p.world.url("lib")+"\n") {
		t.Fatalf("an unpopulated module = %+v, %v, config %q", events, err, p.super.configText())
	}

	p = newLibProject(t, "[submodule \"lib\"]\n\tpath = libs/lib\n")
	p.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")
	if _, err := p.sync(t, nil, false); err != nil || !strings.Contains(p.super.configText(), "\turl = \n") {
		t.Fatalf("a module without a url: %v, config %q", err, p.super.configText())
	}
}

func TestSubmoduleSyncRecursesIntoNestedSubmodules(t *testing.T) {
	n := newNestedProject(t)
	n.mustUpdate(t, SubmoduleUpdateOptions{Init: true, Recursive: true})
	n.super.writeFile("libs/lib/.gitmodules", "[submodule \"inner\"]\n\tpath = inner\n\turl = ../elsewhere\n")

	events, err := n.sync(t, nil, true)

	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleSynchronized, SubmoduleSynchronized}) || events[1].Path != "libs/lib/inner" {
		t.Fatalf("SubmoduleSync = %+v, %v", events, err)
	}
	if config := n.super.readFile(".git/modules/lib/modules/inner/config"); !strings.Contains(config, "\turl = "+n.world.url("elsewhere")+"\n") {
		t.Fatalf("nested config = %q", config)
	}
}

func TestSubmoduleSyncReportsFailures(t *testing.T) {
	boom := errors.New("boom")
	failingWrite := func(t *testing.T, at int) {
		calls := 0
		replaceSeam(t, &writeSubmoduleConfig, func(path string, values ...[2]string) error {
			calls++
			if calls == at {
				return boom
			}
			return setConfigValues(path, values...)
		})
	}
	tests := []struct {
		name  string
		setup func(t *testing.T, p *libProject)
	}{
		{"unresolvable url", func(_ *testing.T, p *libProject) {
			p.super.appendConfig("[remote \"origin\"]\n\turl = relative\n")
			p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../../../../lib\n")
		}},
		{"submodule repository cannot be opened", func(t *testing.T, p *libProject) {
			p.appendSubmoduleConfig(t, "[core]\n\trepositoryformatversion = 9\n")
		}},
		{"superproject config cannot be written", func(t *testing.T, _ *libProject) { failingWrite(t, 1) }},
		{"submodule config cannot be written", func(t *testing.T, _ *libProject) { failingWrite(t, 2) }},
		{"submodule remote cannot be resolved", func(_ *testing.T, p *libProject) {
			p.super.writeFile(".git/modules/lib/HEAD", "ref: refs/heads/..bad\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			p.mustUpdate(t, SubmoduleUpdateOptions{Init: true, Recursive: true})
			tc.setup(t, p)
			if _, err := p.sync(t, nil, true); err == nil {
				t.Fatal("SubmoduleSync succeeded")
			}
		})
	}
}

func TestSubmoduleOperationsRefuseABareRepositoryAndACancelledContext(t *testing.T) {
	bare := newBareTestRepo(t)
	if err := SubmoduleInit(t.Context(), bare.repo, nil, SubmoduleInitOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("SubmoduleInit returned %v", err)
	}
	if err := SubmoduleSync(t.Context(), bare.repo, nil, SubmoduleSyncOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("SubmoduleSync returned %v", err)
	}
	if err := SubmoduleUpdate(t.Context(), bare.repo, nil, SubmoduleUpdateOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("SubmoduleUpdate returned %v", err)
	}
	if list, err := ListSubmodules(t.Context(), bare.repo); err != nil || list != nil {
		t.Fatalf("ListSubmodules = %v, %v", list, err)
	}

	p := newLibProject(t, libGitmodules)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for name, run := range map[string]func() error{
		"init":   func() error { return SubmoduleInit(ctx, p.super.repo, nil, SubmoduleInitOptions{}) },
		"sync":   func() error { return SubmoduleSync(ctx, p.super.repo, nil, SubmoduleSyncOptions{}) },
		"update": func() error { return SubmoduleUpdate(ctx, p.super.repo, nil, SubmoduleUpdateOptions{}) },
		"list": func() error {
			_, err := ListSubmodules(ctx, p.super.repo)
			return err
		},
	} {
		if err := run(); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s returned %v", name, err)
		}
	}
	for name, step := range map[string]func(ctx context.Context) error{
		"init loop":   func(ctx context.Context) error { return p.superproject(t).init(ctx, true, nil) },
		"sync loop":   func(ctx context.Context) error { return p.superproject(t).sync(ctx, SubmoduleSyncOptions{}) },
		"update loop": func(ctx context.Context) error { return p.superproject(t).update(ctx, false, SubmoduleUpdateOptions{}) },
	} {
		if err := step(newCountingContext(t, 1)); err == nil {
			t.Fatalf("%s ignored a cancellation", name)
		}
	}
}

func (p *libProject) superproject(t *testing.T) *superproject {
	t.Helper()
	s, err := loadSuperproject(p.super.reopen(), nil)
	if err != nil {
		t.Fatalf("loadSuperproject returned error %v", err)
	}
	return s
}

func TestLoadSuperprojectReportsReadFailures(t *testing.T) {
	boom := errors.New("boom")
	p := newLibProject(t, libGitmodules)

	replaceSeam(t, &submoduleReadIndex, func(string) (*index.Index, error) { return nil, boom })
	if _, err := loadSuperproject(p.super.repo, nil); !errors.Is(err, boom) {
		t.Fatalf("index failure returned %v", err)
	}
	submoduleReadIndex = index.ReadFile

	replaceSeam(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
	if _, err := loadSuperproject(p.super.repo, nil); !errors.Is(err, boom) {
		t.Fatalf("object database failure returned %v", err)
	}
	odbOpen = odb.Open

	calls := 0
	replaceSeam(t, &odbOpen, func(dir string, opts odb.Options) (*odb.DB, error) {
		calls++
		if calls == 2 {
			return nil, boom
		}
		return odb.Open(dir, opts)
	})
	if _, err := loadSuperproject(p.super.repo, nil); !errors.Is(err, boom) {
		t.Fatalf("second object database failure returned %v", err)
	}
	odbOpen = odb.Open

	if err := os.Remove(p.super.repo.IndexFile()); err != nil {
		t.Fatal(err)
	}
	if s, err := loadSuperproject(p.super.repo, nil); err != nil || len(s.links) != 0 {
		t.Fatalf("a missing index = %+v, %v", s, err)
	}
}

func TestHeadGitmodulesBlobFollowsHead(t *testing.T) {
	boom := errors.New("boom")
	p := newLibProject(t, libGitmodules)
	if id, err := headGitmodulesBlob(p.super.repo); err != nil || id.IsZero() {
		t.Fatalf("headGitmodulesBlob = %s, %v", id, err)
	}

	fresh := newTestRepo(t)
	if id, err := headGitmodulesBlob(fresh.repo); err != nil || !id.IsZero() {
		t.Fatalf("an unborn head = %s, %v", id, err)
	}
	fresh.commitFile("a.txt", "a\n", "a")
	if id, err := headGitmodulesBlob(fresh.repo); err != nil || !id.IsZero() {
		t.Fatalf("a head without .gitmodules = %s, %v", id, err)
	}

	broken := newTestRepo(t)
	broken.writeRawHead("ref: refs/heads/main\n")
	broken.writeRawRef("refs/heads/main", hash.SumSHA1("commit", []byte("missing")).String()+"\n")
	if _, err := headGitmodulesBlob(broken.repo); err == nil {
		t.Fatal("a missing head commit was accepted")
	}

	treeless := newTestRepo(t)
	db := treeless.db()
	sig := object.Signature{Name: "ann", Email: "ann@example.com"}
	commit, err := db.PutObject(&object.Commit{Tree: hash.SumSHA1("tree", []byte("missing")), Author: sig, Committer: sig, Message: "x\n"})
	if err != nil {
		t.Fatal(err)
	}
	treeless.writeRawRef("refs/heads/main", commit.String()+"\n")
	if _, err := headGitmodulesBlob(treeless.repo); err == nil {
		t.Fatal("a missing head tree was accepted")
	}

	corrupt := newTestRepo(t)
	corrupt.writeRawHead("garbage\n")
	if _, err := headGitmodulesBlob(corrupt.repo); err == nil {
		t.Fatal("a corrupt HEAD was accepted")
	}

	replaceSeam(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
	if _, err := headGitmodulesBlob(p.super.repo); !errors.Is(err, boom) {
		t.Fatalf("a refs failure returned %v", err)
	}
}

func TestDefaultRemoteNameFollowsGit(t *testing.T) {
	r := newTestRepo(t)
	r.commitFile("a.txt", "a\n", "a")
	check := func(want string) {
		t.Helper()
		if got, err := defaultRemoteName(r.reopen()); err != nil || got != want {
			t.Fatalf("defaultRemoteName = %q, %v; want %q", got, err, want)
		}
	}
	check("origin")
	r.appendConfig("[remote \"only\"]\n\turl = x\n")
	check("only")
	r.appendConfig("[remote \"second\"]\n\turl = y\n")
	check("origin")
	r.appendConfig("[branch \"main\"]\n\tremote = second\n")
	check("second")
	r.writeRawHead(r.branchTarget("main").String() + "\n")
	check("origin")
	opened := r.reopen()
	replaceSeam(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, errors.New("boom") })
	if _, err := defaultRemoteName(opened); err == nil {
		t.Fatal("a refs failure was accepted")
	}
}

func TestListSubmodulesDescribesEveryState(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"moved\"]\n\tpath = moved\n\turl = ../lib\n[submodule \"dirty\"]\n\tpath = dirty\n\turl = ../lib\n[submodule \"conflict\"]\n\tpath = conflict\n\turl = ../lib\n")
	for _, path := range []string{"moved", "dirty"} {
		p.super.setGitlink(path, p.one)
	}
	p.super.setGitlink("unknown", p.one)
	p.super.commitAll("links")
	if _, err := p.update(t, []string{"libs/lib", "moved", "dirty"}, SubmoduleUpdateOptions{Init: true}); err != nil {
		t.Fatalf("SubmoduleUpdate returned error %v", err)
	}
	p.super.setGitlink("moved", p.two)
	p.super.writeFile("dirty/lib.txt", "dirty\n")
	idx := p.super.index()
	idx.Add(index.Entry{Path: "conflict", Mode: object.ModeSubmodule, ID: p.one, Stage: index.StageOurs})
	p.super.saveIndex(idx)
	p.super.appendConfig("[submodule \"lib\"]\n\turl = ../configured\n")

	list, err := ListSubmodules(t.Context(), p.super.reopen())
	if err != nil {
		t.Fatalf("ListSubmodules returned error %v", err)
	}
	states := map[string]SubmoduleState{}
	for _, item := range list {
		states[item.Path] = item.State
	}
	want := map[string]SubmoduleState{
		"conflict": SubmoduleStateConflict,
		"dirty":    SubmoduleStateModified,
		"libs/lib": SubmoduleStateUpToDate,
		"moved":    SubmoduleStateNewCommits,
		"unknown":  SubmoduleStateNotInitialized,
	}
	if len(states) != len(want) {
		t.Fatalf("states = %v", states)
	}
	for path, state := range want {
		if states[path] != state {
			t.Fatalf("states = %v, want %v", states, want)
		}
	}
	lib := list[slices.IndexFunc(list, func(s Submodule) bool { return s.Path == "libs/lib" })]
	if lib.Name != "lib" || lib.URL != p.world.url("configured") || !lib.Active || !lib.Populated || lib.Head != p.one {
		t.Fatalf("lib = %+v", lib)
	}
	unknown := list[slices.IndexFunc(list, func(s Submodule) bool { return s.Path == "unknown" })]
	if unknown.Name != "unknown" || unknown.Active || unknown.Populated {
		t.Fatalf("unknown = %+v", unknown)
	}
}

func TestListSubmodulesReportsFailures(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name  string
		setup func(t *testing.T, p *libProject)
	}{
		{"broken .gitmodules", func(_ *testing.T, p *libProject) { p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath\n") }},
		{"broken active flag", func(_ *testing.T, p *libProject) { p.super.appendConfig("[submodule \"lib\"]\n\tactive = maybe\n") }},
		{"broken submodule config", func(t *testing.T, p *libProject) {
			p.appendSubmoduleConfig(t, "[core]\n\trepositoryformatversion = 9\n")
		}},
		{"object database", func(t *testing.T, _ *libProject) {
			calls := 0
			replaceSeam(t, &odbOpen, func(dir string, opts odb.Options) (*odb.DB, error) {
				calls++
				if calls == 3 {
					return nil, boom
				}
				return odb.Open(dir, opts)
			})
		}},
		{"worktree", func(t *testing.T, _ *libProject) {
			replaceSeam(t, &worktreeOpen, func(*repo.Repository, worktree.Options) (*worktree.Worktree, error) { return nil, boom })
		}},
		{"index", func(_ *testing.T, p *libProject) { p.super.writeFile(".git/modules/lib/index", "garbage") }},
		{"status", func(_ *testing.T, p *libProject) {
			p.super.writeFile(".git/modules/lib/HEAD", hash.SumSHA1("commit", []byte("missing")).String()+"\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
			tc.setup(t, p)
			if _, err := ListSubmodules(t.Context(), p.super.reopen()); err == nil {
				t.Fatal("ListSubmodules succeeded")
			}
		})
	}
}
