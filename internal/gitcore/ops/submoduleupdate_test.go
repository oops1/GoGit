package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func replaceSeam[T any](t *testing.T, seam *T, replacement T) {
	t.Helper()
	original := *seam
	*seam = replacement
	t.Cleanup(func() { *seam = original })
}

func (p *libProject) update(t *testing.T, paths []string, opts SubmoduleUpdateOptions) ([]SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	opts.Events = events
	err := SubmoduleUpdate(t.Context(), p.super.reopen(), paths, opts)
	return *got, err
}

func (p *libProject) mustUpdate(t *testing.T, opts SubmoduleUpdateOptions) []SubmoduleEvent {
	t.Helper()
	events, err := p.update(t, nil, opts)
	if err != nil {
		t.Fatalf("SubmoduleUpdate returned error %v", err)
	}
	return events
}

func (p *libProject) libDir() string {
	return p.super.path("libs/lib")
}

func (p *libProject) libHead(t *testing.T) hash.ObjectID {
	t.Helper()
	return submoduleHead(t, p.libDir())
}

func (p *libProject) record(id hash.ObjectID) {
	p.super.setGitlink("libs/lib", id)
	p.super.commitAll("move lib")
}

func (p *libProject) submoduleRepo(t *testing.T) *testRepo {
	t.Helper()
	r, err := repo.Open(p.libDir(), repo.OpenOptions{NoSystem: true, GlobalFile: p.world.global})
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: t, dir: p.libDir(), repo: r, clock: 1800000000, globalFile: p.world.global}
}

func (p *libProject) appendSubmoduleConfig(t *testing.T, text string) {
	t.Helper()
	path := p.super.path(".git/modules/lib/config")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, text...), 0o666); err != nil {
		t.Fatal(err)
	}
}

func (p *libProject) setSubmoduleOrigin(t *testing.T, url string) {
	t.Helper()
	if err := setConfigValues(p.super.path(".git/modules/lib/config"), [2]string{"remote.origin.url", url}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmoduleUpdateMovesToTheRecordedCommitOnlyWhenItChanged(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.record(p.two)

	events := p.mustUpdate(t, SubmoduleUpdateOptions{})

	if p.libHead(t) != p.two || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCheckedOut}) {
		t.Fatalf("head = %s, events = %+v", p.libHead(t), events)
	}
	if events := p.mustUpdate(t, SubmoduleUpdateOptions{}); len(events) != 0 {
		t.Fatalf("an update at the recorded commit reported %+v", events)
	}
	if events := p.mustUpdate(t, SubmoduleUpdateOptions{Force: true}); !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCheckedOut}) {
		t.Fatalf("a forced update reported %+v", events)
	}
}

func TestSubmoduleUpdateFetchesACommitTheCloneLacks(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	three := p.lib.commitFile("lib.txt", "three\n", "three")
	p.record(three)

	events := p.mustUpdate(t, SubmoduleUpdateOptions{})

	if p.libHead(t) != three || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleFetching, SubmoduleCheckedOut}) {
		t.Fatalf("head = %s, events = %+v", p.libHead(t), events)
	}
}

func TestSubmoduleUpdateFetchesAnUnadvertisedCommitDirectly(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	three := p.lib.commitFile("lib.txt", "three\n", "three")
	p.lib.writeRawRef("refs/heads/main", p.two.String()+"\n")
	p.record(three)

	events := p.mustUpdate(t, SubmoduleUpdateOptions{})

	if p.libHead(t) != three || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleFetching, SubmoduleFetching, SubmoduleCheckedOut}) {
		t.Fatalf("head = %s, events = %+v", p.libHead(t), events)
	}
}

func TestSubmoduleUpdateRetriesThroughTheRemoteWithTheModuleURL(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	three := p.lib.commitFile("lib.txt", "three\n", "three")
	p.record(three)
	p.appendSubmoduleConfig(t, "[remote \"good\"]\n\turl = "+p.world.url("lib")+"\n")
	p.setSubmoduleOrigin(t, p.world.url("gone"))

	events := p.mustUpdate(t, SubmoduleUpdateOptions{})

	want := []SubmoduleEventKind{SubmoduleFetching, SubmoduleFetchRetry, SubmoduleFetching, SubmoduleCheckedOut}
	if p.libHead(t) != three || !slices.Equal(eventKinds(events), want) {
		t.Fatalf("head = %s, events = %+v", p.libHead(t), events)
	}
}

func TestSubmoduleUpdateFailsWhenTheRemoteLacksTheCommit(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.record(hash.SumSHA1("commit", []byte("nowhere")))

	if _, err := p.update(t, nil, SubmoduleUpdateOptions{}); !errors.Is(err, ErrSubmoduleCommitMissing) {
		t.Fatalf("SubmoduleUpdate returned %v", err)
	}
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{NoFetch: true}); !errors.Is(err, ErrSubmoduleCheckout) {
		t.Fatalf("SubmoduleUpdate without fetching returned %v", err)
	}
}

func TestSubmoduleUpdateHonoursTheUpdateMode(t *testing.T) {
	tests := []struct {
		name   string
		config string
		opts   SubmoduleUpdateOptions
		kinds  []SubmoduleEventKind
		head   func(p *libProject) hash.ObjectID
	}{
		{"configured none", "\tupdate = none\n", SubmoduleUpdateOptions{}, []SubmoduleEventKind{SubmoduleSkipped}, func(p *libProject) hash.ObjectID { return p.one }},
		{"explicit none", "", SubmoduleUpdateOptions{Mode: SubmoduleUpdateNone}, []SubmoduleEventKind{SubmoduleSkipped}, func(p *libProject) hash.ObjectID { return p.one }},
		{"explicit checkout beats none", "\tupdate = none\n", SubmoduleUpdateOptions{Mode: SubmoduleUpdateCheckout}, []SubmoduleEventKind{SubmoduleCheckedOut}, func(p *libProject) hash.ObjectID { return p.two }},
		{"configured merge", "\tupdate = merge\n", SubmoduleUpdateOptions{}, []SubmoduleEventKind{SubmoduleMerged}, func(p *libProject) hash.ObjectID { return p.two }},
		{"explicit merge", "", SubmoduleUpdateOptions{Mode: SubmoduleUpdateMerge}, []SubmoduleEventKind{SubmoduleMerged}, func(p *libProject) hash.ObjectID { return p.two }},
		{"configured rebase", "\tupdate = rebase\n", SubmoduleUpdateOptions{}, []SubmoduleEventKind{SubmoduleRebased}, func(p *libProject) hash.ObjectID { return p.two }},
		{"explicit rebase", "", SubmoduleUpdateOptions{Mode: SubmoduleUpdateRebase}, []SubmoduleEventKind{SubmoduleRebased}, func(p *libProject) hash.ObjectID { return p.two }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
			if tc.config != "" {
				p.super.appendConfig("[submodule \"lib\"]\n" + tc.config)
			}
			p.record(p.two)

			events := p.mustUpdate(t, tc.opts)

			if !slices.Equal(eventKinds(events), tc.kinds) || p.libHead(t) != tc.head(p) {
				t.Fatalf("events = %+v, head = %s", events, p.libHead(t))
			}
		})
	}
}

func TestSubmoduleUpdateChecksOutAFreshCloneWhateverTheMode(t *testing.T) {
	p := newLibProject(t, libGitmodules+"\tupdate = rebase\n")

	events := p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})

	if !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleRegistered, SubmoduleCloning, SubmoduleCheckedOut}) {
		t.Fatalf("events = %+v", events)
	}
}

func TestSubmoduleUpdateReportsConflictingMergesAndRebases(t *testing.T) {
	for _, mode := range []SubmoduleUpdateMode{SubmoduleUpdateMerge, SubmoduleUpdateRebase} {
		p := newLibProject(t, libGitmodules)
		p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
		sub := p.submoduleRepo(t)
		sub.writeFile("lib.txt", "local\n")
		mustStage(t, sub, "lib.txt")
		if _, err := Commit(t.Context(), sub.repo, CommitOptions{Message: "local"}); err != nil {
			t.Fatalf("Commit returned error %v", err)
		}
		p.record(p.two)

		_, err := p.update(t, nil, SubmoduleUpdateOptions{Mode: mode})

		want := map[SubmoduleUpdateMode]error{SubmoduleUpdateMerge: ErrSubmoduleMerge, SubmoduleUpdateRebase: ErrSubmoduleRebase}[mode]
		if !errors.Is(err, want) {
			t.Fatalf("mode %d returned %v, want %v", mode, err, want)
		}
	}
}

func TestSubmoduleUpdateRefusesBrokenUpdateModes(t *testing.T) {
	tests := map[string]error{
		"!make":    ErrSubmoduleCommand,
		"sideways": submodule.ErrInvalidUpdate,
	}
	for value, want := range tests {
		p := newLibProject(t, libGitmodules)
		p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
		p.super.appendConfig("[submodule \"lib\"]\n\tupdate = " + value + "\n")
		p.record(p.two)

		if _, err := p.update(t, nil, SubmoduleUpdateOptions{}); !errors.Is(err, want) {
			t.Fatalf("update = %s returned %v, want %v", value, err, want)
		}
	}
}

func TestSubmoduleUpdateFollowsTheRemoteBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		opts   SubmoduleUpdateOptions
		kinds  []SubmoduleEventKind
	}{
		{"remote head", "", SubmoduleUpdateOptions{Remote: true}, []SubmoduleEventKind{SubmoduleFetching, SubmoduleCheckedOut}},
		{"named branch", "\tbranch = main\n", SubmoduleUpdateOptions{Remote: true}, []SubmoduleEventKind{SubmoduleFetching, SubmoduleCheckedOut}},
		{"superproject branch", "\tbranch = .\n", SubmoduleUpdateOptions{Remote: true, NoFetch: true}, []SubmoduleEventKind{SubmoduleCheckedOut}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules+tc.branch)
			p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})

			events := p.mustUpdate(t, tc.opts)

			if p.libHead(t) != p.two || !slices.Equal(eventKinds(events), tc.kinds) {
				t.Fatalf("head = %s, events = %+v", p.libHead(t), events)
			}
		})
	}
}

func TestSubmoduleUpdateRefusesARemoteBranchItCannotFind(t *testing.T) {
	tests := []struct {
		name  string
		setup func(p *libProject)
		opts  SubmoduleUpdateOptions
		want  error
	}{
		{"missing branch", func(p *libProject) {
			p.super.appendConfig("[submodule \"lib\"]\n\tbranch = missing\n")
		}, SubmoduleUpdateOptions{Remote: true, NoFetch: true}, ErrSubmoduleRemoteRef},
		{"detached superproject", func(p *libProject) {
			p.super.appendConfig("[submodule \"lib\"]\n\tbranch = .\n")
			p.super.writeRawHead(p.super.branchTarget("main").String() + "\n")
		}, SubmoduleUpdateOptions{Remote: true, NoFetch: true}, ErrSubmoduleNoBranch},
		{"unreachable remote", func(p *libProject) {
			p.setSubmoduleOrigin(t, p.world.url("gone"))
		}, SubmoduleUpdateOptions{Remote: true}, ErrSubmoduleFetch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
			tc.setup(p)

			if _, err := p.update(t, nil, tc.opts); !errors.Is(err, tc.want) {
				t.Fatalf("SubmoduleUpdate returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSubmoduleUpdateReportsSkippedSubmodules(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"other\"]\n\tpath = other\n\turl = ../other\n")
	p.super.setGitlink("other", p.one)
	p.super.setGitlink("unknown", p.one)
	p.super.commitAll("more links")

	events, err := p.update(t, nil, SubmoduleUpdateOptions{})
	if err != nil || len(events) != 0 {
		t.Fatalf("an update of nothing initialized = %+v, %v", events, err)
	}

	events, err = p.update(t, []string{"other", "unknown"}, SubmoduleUpdateOptions{})
	if err != nil || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleNotInitialized, SubmoduleNotInitialized}) {
		t.Fatalf("named paths = %+v, %v", events, err)
	}

	idx := p.super.index()
	idx.Remove("libs/lib")
	idx.Add(index.Entry{Path: "libs/lib", Mode: object.ModeSubmodule, ID: p.one, Stage: index.StageOurs})
	idx.Add(index.Entry{Path: "libs/lib", Mode: object.ModeSubmodule, ID: p.two, Stage: index.StageTheirs})
	p.super.saveIndex(idx)
	events, err = p.update(t, nil, SubmoduleUpdateOptions{})
	if err != nil || len(events) != 1 || events[0] != (SubmoduleEvent{Kind: SubmoduleSkippedUnmerged, Path: "libs/lib"}) {
		t.Fatalf("unmerged = %+v, %v", events, err)
	}
}

func TestSubmoduleUpdateRefillsTheWorkTreeFromTheAbsorbedGitDir(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	if err := os.RemoveAll(p.libDir()); err != nil {
		t.Fatal(err)
	}

	events := p.mustUpdate(t, SubmoduleUpdateOptions{})

	if !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCheckedOut}) || p.super.readFile("libs/lib/lib.txt") != "one\n" {
		t.Fatalf("events = %+v", events)
	}
	if link := p.super.readFile("libs/lib/.git"); link != "gitdir: ../../.git/modules/lib\n" {
		t.Fatalf(".git = %q", link)
	}
}

func TestSubmoduleUpdateRefusesToCloneOverContent(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.super.writeFile("libs/lib/stray.txt", "stray\n")

	if _, err := p.update(t, nil, SubmoduleUpdateOptions{RequireInit: true}); !errors.Is(err, ErrSubmoduleNotEmpty) {
		t.Fatalf("RequireInit returned %v", err)
	}
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{Init: true}); !errors.Is(err, ErrSubmoduleClone) || !errors.Is(err, ErrCloneTargetNotEmpty) {
		t.Fatalf("Init returned %v", err)
	}
}

func TestSubmoduleUpdateKeepsGoingAfterACheckoutRefusal(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"other\"]\n\tpath = other\n\turl = ../lib\n")
	p.super.setGitlink("other", p.one)
	p.super.commitAll("other")
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	p.super.setGitlink("other", p.two)
	p.record(p.two)
	p.super.writeFile("libs/lib/lib.txt", "dirty\n")

	_, err := p.update(t, nil, SubmoduleUpdateOptions{})

	if !errors.Is(err, ErrSubmoduleCheckout) || submoduleHead(t, p.super.path("other")) != p.two {
		t.Fatalf("SubmoduleUpdate returned %v and left other at %s", err, submoduleHead(t, p.super.path("other")))
	}
}

func TestSubmoduleUpdateFailsWithoutAUsableURL(t *testing.T) {
	p := newLibProject(t, "[submodule \"lib\"]\n\tpath = libs/lib\n")
	p.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{}); !errors.Is(err, ErrSubmoduleNoURL) {
		t.Fatalf("no url returned %v", err)
	}

	p = newLibProject(t, "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../../../../../../../../../../../../../../../../../../../../../lib\n")
	p.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n[remote \"origin\"]\n\turl = relative\n")
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{}); !errors.Is(err, submoduleErrCannotStrip()) {
		t.Fatalf("unresolvable url returned %v", err)
	}

	p = newLibProject(t, "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../gone\n")
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{Init: true}); !errors.Is(err, ErrSubmoduleClone) || p.super.exists(".git/modules/lib") {
		t.Fatalf("missing remote returned %v", err)
	}
}

func submoduleErrCannotStrip() error { return submodule.ErrCannotStripURL }

func TestSubmoduleCloneNeedsTheFileProtocolAllowed(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	if err := os.WriteFile(p.world.global, []byte("[user]\n\tname = ann\n\temail = ann@example.com\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	if _, err := p.update(t, nil, SubmoduleUpdateOptions{Init: true}); !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("SubmoduleUpdate returned %v", err)
	}
}

func TestSubmoduleUpdateRefusesAGitDirInsideAnother(t *testing.T) {
	p := newLibProject(t, libGitmodules+"[submodule \"lib/hooks\"]\n\tpath = zz\n\turl = ../lib\n")
	p.super.setGitlink("zz", p.one)
	p.super.commitAll("nested name")

	if _, err := p.update(t, nil, SubmoduleUpdateOptions{Init: true}); !errors.Is(err, submodule.ErrGitDirInsideGitDir) {
		t.Fatalf("SubmoduleUpdate returned %v", err)
	}
}

func TestSubmoduleUpdateUsesTheRecommendedShallowDepth(t *testing.T) {
	p := newLibProject(t, libGitmodules+"\tshallow = true\n")

	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true, RecommendShallow: true})

	if shallow := p.super.readFile(".git/modules/lib/shallow"); shallow == "" {
		t.Fatal("the recommended shallow clone left no shallow file")
	}
}

type nestedProject struct {
	*libProject
	inner    *testRepo
	innerOne hash.ObjectID
}

func newNestedProject(t *testing.T) *nestedProject {
	t.Helper()
	p := newLibProject(t, libGitmodules)
	n := &nestedProject{libProject: p, inner: p.world.repoAt("inner")}
	n.innerOne = n.inner.commitFile("inner.txt", "inner\n", "inner")
	p.lib.writeGitmodules("[submodule \"inner\"]\n\tpath = inner\n\turl = ../inner\n")
	p.lib.setGitlink("inner", n.innerOne)
	p.two = p.lib.commitAll("with inner")
	p.record(p.two)
	return n
}

func TestSubmoduleUpdateRecursesIntoNestedSubmodules(t *testing.T) {
	n := newNestedProject(t)

	events := n.mustUpdate(t, SubmoduleUpdateOptions{Init: true, Recursive: true})

	if submoduleHead(t, n.super.path("libs/lib/inner")) != n.innerOne {
		t.Fatal("the nested submodule was not checked out")
	}
	if link := n.super.readFile("libs/lib/inner/.git"); link != "gitdir: ../../../.git/modules/lib/modules/inner\n" {
		t.Fatalf("nested .git = %q", link)
	}
	var paths []string
	for _, e := range events {
		paths = append(paths, e.Path)
	}
	if !slices.Contains(paths, "libs/lib/inner") {
		t.Fatalf("events = %+v", events)
	}
}

func TestConnectWorkTreeAndGitDirFailsOnUnwritableTargets(t *testing.T) {
	dir := t.TempDir()
	if err := connectWorkTreeAndGitDir(filepath.Join(dir, "missing", "deeper"), filepath.Join(dir, "gitdir")); err == nil {
		t.Fatal("writing .git into a missing directory succeeded")
	}
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gitdir", "config"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := connectWorkTreeAndGitDir(filepath.Join(dir, "work"), filepath.Join(dir, "gitdir")); err == nil {
		t.Fatal("writing a config that is a directory succeeded")
	}
}

func TestSetConfigValuesReportsEveryFailure(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken")
	if err := os.WriteFile(broken, []byte("[unterminated\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValues(broken, [2]string{"a.b", "c"}); err == nil {
		t.Fatal("an unparsable config was accepted")
	}
	if err := setConfigValues(filepath.Join(dir, "fresh"), [2]string{"nokey", "c"}); err == nil {
		t.Fatal("an invalid key was accepted")
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValues(filepath.Join(dir, "adir"), [2]string{"a.b", "c"}); err == nil {
		t.Fatal("a directory was read as a config")
	}
	if err := setConfigValues(filepath.Join(dir, "fresh"), [2]string{"a.b", "c"}); err != nil {
		t.Fatalf("a new config failed: %v", err)
	}
	if text, _ := os.ReadFile(filepath.Join(dir, "fresh")); !strings.Contains(string(text), "b = c") {
		t.Fatalf("config = %q", text)
	}
}

func TestRelativeSlashPathFallsBackToTheTarget(t *testing.T) {
	if got := relativeSlashPath("rel/target", "/abs/base"); got != "rel/target" {
		t.Fatalf("relativeSlashPath = %q", got)
	}
}
