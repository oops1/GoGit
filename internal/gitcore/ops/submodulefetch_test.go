package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

type fetchProject struct {
	*libProject
	clone *testRepo
	three hash.ObjectID
}

func newFetchProject(t *testing.T) *fetchProject {
	t.Helper()
	p := newLibProject(t, libGitmodules)
	f := &fetchProject{libProject: p, clone: p.cloneSuper(t, "work", CloneOptions{RecurseSubmodules: true})}
	f.three = p.lib.commitFile("lib.txt", "three\n", "three")
	p.record(f.three)
	p.super.commitFile("super.txt", "later\n", "later")
	return f
}

func (f *fetchProject) fetch(t *testing.T, opts SubmoduleFetchOptions) ([]SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	opts.Events = events
	_, err := FetchRecursive(t.Context(), f.clone.reopen(), "", remote.FetchOptions{}, opts)
	return *got, err
}

func fetchedPaths(events []SubmoduleEvent) []string {
	var paths []string
	for _, e := range events {
		if e.Kind == SubmoduleFetching {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

func TestFetchRecursionFetchesChangedSubmodulesOnDemand(t *testing.T) {
	f := newFetchProject(t)

	events, err := f.fetch(t, SubmoduleFetchOptions{})

	if err != nil || !slices.Equal(fetchedPaths(events), []string{"libs/lib"}) || !hasObject(f.clone.reopenAt(t, "libs/lib").repo, f.three) {
		t.Fatalf("FetchRecursive = %+v, %v", events, err)
	}
	if events, err := f.fetch(t, SubmoduleFetchOptions{}); err != nil || len(events) != 0 {
		t.Fatalf("a second fetch = %+v, %v", events, err)
	}
}

func (r *testRepo) reopenAt(t *testing.T, rel string) *testRepo {
	t.Helper()
	sub, err := submoduleOpen(r.path(rel), r.openOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	return &testRepo{t: t, dir: r.path(rel), repo: sub, globalFile: r.globalFile}
}

func TestFetchRecursionFollowsTheConfiguredMode(t *testing.T) {
	tests := []struct {
		name   string
		config string
		opts   SubmoduleFetchOptions
		want   []string
	}{
		{"explicit on", "", SubmoduleFetchOptions{Mode: SubmoduleFetchOn}, []string{"libs/lib"}},
		{"explicit off", "", SubmoduleFetchOptions{Mode: SubmoduleFetchOff}, nil},
		{"fetch.recurseSubmodules off", "[fetch]\n\trecurseSubmodules = no\n", SubmoduleFetchOptions{}, nil},
		{"later setting wins", "[fetch]\n\trecurseSubmodules = false\n[submodule]\n\trecurse\n", SubmoduleFetchOptions{}, []string{"libs/lib"}},
		{"per submodule off", "[submodule \"lib\"]\n\tfetchRecurseSubmodules = false\n", SubmoduleFetchOptions{}, nil},
		{"per submodule on", "[submodule \"lib\"]\n\tfetchRecurseSubmodules = on-demand\n", SubmoduleFetchOptions{}, []string{"libs/lib"}},
		{"per submodule invalid", "[submodule \"lib\"]\n\tfetchRecurseSubmodules = sometimes\n", SubmoduleFetchOptions{}, []string{"libs/lib"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFetchProject(t)
			f.clone.appendConfig(tc.config)

			events, err := f.fetch(t, tc.opts)

			if err != nil || !slices.Equal(fetchedPaths(events), tc.want) {
				t.Fatalf("FetchRecursive = %+v, %v", events, err)
			}
		})
	}
}

func TestFetchRecursionRejectsBrokenSettings(t *testing.T) {
	for _, config := range []string{"[submodule]\n\trecurse = on-demand\n", "[fetch]\n\trecurseSubmodules = sometimes\n"} {
		f := newFetchProject(t)
		f.clone.appendConfig(config)
		if _, err := f.fetch(t, SubmoduleFetchOptions{}); !errors.Is(err, ErrFetchRecurseSetting) {
			t.Fatalf("%q returned %v", config, err)
		}
	}
}

func TestFetchRecursionFindsSubmodulesThroughTheIndexAlone(t *testing.T) {
	f := newFetchProject(t)
	f.clone.remove(".gitmodules")

	events, err := f.fetch(t, SubmoduleFetchOptions{Mode: SubmoduleFetchOn})

	if err != nil || !slices.Equal(fetchedPaths(events), []string{"libs/lib"}) {
		t.Fatalf("FetchRecursive = %+v, %v", events, err)
	}
}

const otherGitmodules = "[submodule \"other\"]\n\tpath = other\n\turl = ../lib\n"

func TestFetchRecursionFetchesUnregisteredAndRemovedSubmodules(t *testing.T) {
	f := newFetchProject(t)
	vendored, err := Clone(t.Context(), f.world.url("lib"), f.clone.path("vendored"), CloneOptions{Open: f.clone.openOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_ = vendored.Close()
	f.clone.setGitlink("vendored", f.one)
	if err := SubmoduleRemove(t.Context(), f.clone.reopen(), []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	f.clone.writeFile(".gitmodules", otherGitmodules)

	events, err := f.fetch(t, SubmoduleFetchOptions{Default: SubmoduleFetchOn})

	if err != nil || !slices.Equal(fetchedPaths(events), []string{"vendored", "libs/lib"}) {
		t.Fatalf("FetchRecursive = %+v, %v", events, err)
	}
}

func TestFetchRecursionSkipsChangedSubmodulesItCannotReach(t *testing.T) {
	tests := map[string]func(f *fetchProject){
		"inactive": func(f *fetchProject) {
			if err := editConfigFile(f.clone.path(".git/config"), func(file *config.File) error {
				return errors.Join(file.RemoveSection("submodule.lib"), file.UnsetAll("submodule.active"))
			}); err != nil {
				f.clone.t.Fatal(err)
			}
		},
		"without a git directory": func(f *fetchProject) {
			if err := os.RemoveAll(f.clone.path(".git/modules/lib")); err != nil {
				f.clone.t.Fatal(err)
			}
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFetchProject(t)
			if err := SubmoduleRemove(t.Context(), f.clone.reopen(), []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true}); err != nil {
				t.Fatal(err)
			}
			f.clone.writeFile(".gitmodules", otherGitmodules)
			prepare(f)

			events, err := f.fetch(t, SubmoduleFetchOptions{})

			if err != nil || len(fetchedPaths(events)) != 0 {
				t.Fatalf("FetchRecursive = %+v, %v", events, err)
			}
		})
	}
}

func TestFetchRecursionReportsInaccessibleAndFailingSubmodules(t *testing.T) {
	f := newFetchProject(t)
	if err := os.RemoveAll(f.clone.path("libs/lib")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.clone.path(".git/modules/lib")); err != nil {
		t.Fatal(err)
	}
	f.clone.writeFile("libs/lib/stray.txt", "x\n")
	if _, err := f.fetch(t, SubmoduleFetchOptions{Mode: SubmoduleFetchOn}); !errors.Is(err, ErrSubmoduleInaccessible) {
		t.Fatalf("an inaccessible submodule returned %v", err)
	}

	f = newFetchProject(t)
	if err := setConfigValues(f.clone.path(".git/modules/lib/config"), [2]string{"remote.origin.url", f.world.url("gone")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fetch(t, SubmoduleFetchOptions{}); !errors.Is(err, ErrSubmoduleFetch) {
		t.Fatalf("a failing submodule fetch returned %v", err)
	}

	f = newFetchProject(t)
	f.lib.writeRawRef("refs/heads/main", f.two.String()+"\n")
	missing := hash.SumSHA1("commit", []byte("missing"))
	f.super.setGitlink("libs/lib", missing)
	f.super.commitAll("points nowhere")
	events, err := f.fetch(t, SubmoduleFetchOptions{})
	if !errors.Is(err, ErrSubmoduleFetch) || !slices.ContainsFunc(events, func(e SubmoduleEvent) bool { return e.Kind == SubmoduleFetchRetry }) {
		t.Fatalf("a commit missing upstream = %+v, %v", events, err)
	}
}

func TestFetchRecursionIgnoresStrayGitlinksAndUnregisteredRepositories(t *testing.T) {
	f := newFetchProject(t)
	f.super.setGitlink("vendored", f.one)
	f.super.commitAll("vendored upstream")
	vendored, err := Clone(t.Context(), f.world.url("lib"), f.clone.path("vendored"), CloneOptions{Open: f.clone.openOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_ = vendored.Close()
	f.clone.setGitlink("stray", f.one)

	events, err := f.fetch(t, SubmoduleFetchOptions{})

	if err != nil || !slices.Equal(fetchedPaths(events), []string{"libs/lib"}) {
		t.Fatalf("FetchRecursive = %+v, %v", events, err)
	}
}

func TestSetConfigValuesRejectsAnInvalidKey(t *testing.T) {
	if err := setConfigValues(filepath.Join(t.TempDir(), "config"), [2]string{"nokey", "x"}); err == nil {
		t.Fatal("an invalid key was written")
	}
}

func TestChangedSubmodulesReportsWalkFailures(t *testing.T) {
	f := newFetchProject(t)
	s, err := loadSuperproject(f.clone.reopen(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bogus := remote.FetchResult{Changes: []remote.Change{{New: hash.SumSHA1("commit", []byte("bogus"))}}}
	if _, err := s.changedSubmodules(t.Context(), nil, bogus); err == nil {
		t.Fatal("a missing fetched commit was walked")
	}
	f.clone.writeFile(".git/shallow", "garbage\n")
	if _, err := s.changedSubmodules(t.Context(), nil, bogus); err == nil {
		t.Fatal("a broken shallow file was accepted")
	}
	if changed, err := s.changedSubmodules(t.Context(), nil, remote.FetchResult{Changes: []remote.Change{{Deleted: true}}}); err != nil || len(changed) != 0 {
		t.Fatalf("a deleted ref = %+v, %v", changed, err)
	}
}

func TestRecordChangeNamesUnregisteredPopulatedSubmodulesByPath(t *testing.T) {
	f := newFetchProject(t)
	s, err := loadSuperproject(f.clone.reopen(), nil)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := submodule.Parse([]byte("[submodule \"libs/lib\"]\n\tpath = elsewhere\n"))
	if err != nil {
		t.Fatal(err)
	}
	changed := map[string]*changedSubmodule{}
	s.recordChange(changed, modules, "libs/lib", f.three)
	s.recordChange(changed, modules, "absent", f.three)
	empty, _ := submodule.Parse(nil)
	s.recordChange(changed, empty, "libs/lib", f.three)
	s.recordChange(changed, empty, "libs/lib", f.two)
	if len(changed) != 1 || changed["libs/lib"].known || !slices.Equal(changed["libs/lib"].commits, []hash.ObjectID{f.three, f.two}) {
		t.Fatalf("changed = %+v", changed)
	}
	if s.submoduleHasCommits(t.Context(), "absent", []hash.ObjectID{f.three}) {
		t.Fatal("an unpopulated submodule was reported to have the commits")
	}
}

func TestModulesInTreeToleratesABrokenGitmodules(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.super.writeFile(".gitmodules", "[broken")
	mustStage(t, p.super, ".gitmodules")
	commit := p.super.commitAll("broken")
	c, err := p.super.db().Commit(commit)
	if err != nil {
		t.Fatal(err)
	}
	cache := map[hash.ObjectID]*submodule.Modules{}
	for range 2 {
		modules, err := modulesInTree(p.super.db(), c.Tree, cache)
		if err != nil || modules.Len() != 0 {
			t.Fatalf("modulesInTree = %v, %v", modules, err)
		}
	}
}

func TestCommitGitlinkChangesComparesEveryParent(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	base := p.super.branchTarget("main")
	p.record(p.two)
	moved := p.super.branchTarget("main")
	db := p.super.db()
	movedCommit, err := db.Commit(moved)
	if err != nil {
		t.Fatal(err)
	}
	if links, err := commitGitlinkChanges(db, movedCommit.Tree, []hash.ObjectID{base}); err != nil || links["libs/lib"] != p.two {
		t.Fatalf("single parent = %v, %v", links, err)
	}
	if links, err := commitGitlinkChanges(db, movedCommit.Tree, []hash.ObjectID{base, moved}); err != nil || len(links) != 0 {
		t.Fatalf("merge = %v, %v", links, err)
	}
}
