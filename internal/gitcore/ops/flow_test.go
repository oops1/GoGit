package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

func mainFlowConfig() FlowConfig {
	cfg := DefaultFlowConfig()
	cfg.Master = "main"
	return cfg
}

func useFlowConfig(t *testing.T, r *testRepo, cfg FlowConfig) {
	t.Helper()
	if err := WriteFlowConfig(r.repo, cfg); err != nil {
		t.Fatalf("WriteFlowConfig returned error %v", err)
	}
	r.repo = r.reopen()
}

func newFlowRepo(t *testing.T) (*testRepo, hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t)
	r.writeFile("a.txt", "base\n")
	mustStage(t, r, "a.txt")
	base := r.commitAll("base")
	r.createBranch("develop", base)
	useFlowConfig(t, r, mainFlowConfig())
	return r, base
}

func switchFlowBranch(t *testing.T, r *testRepo, name string) {
	t.Helper()
	if err := Switch(t.Context(), r.repo, name, SwitchOptions{}); err != nil {
		t.Fatalf("Switch %s returned error %v", name, err)
	}
}

func commitFlowFile(t *testing.T, r *testRepo, rel, text, message string) hash.ObjectID {
	t.Helper()
	r.writeFile(rel, text)
	mustStage(t, r, rel)
	return r.commitAll(message)
}

func startFlow(t *testing.T, r *testRepo, kind, name string, opts StartFlowOptions) {
	t.Helper()
	if _, err := StartFlow(t.Context(), r.repo, kind, name, opts); err != nil {
		t.Fatalf("StartFlow %s %s returned error %v", kind, name, err)
	}
}

func startFlowRelease(t *testing.T, r *testRepo, version string, net FlowNetwork) {
	t.Helper()
	startFlow(t, r, FlowKindRelease, version, StartFlowOptions{Network: net})
}

func newReleaseRepo(t *testing.T) *testRepo {
	t.Helper()
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "VERSION", "1.0\n", "bump")
	return r
}

func newFeatureRepo(t *testing.T) *testRepo {
	t.Helper()
	r, _ := newFlowRepo(t)
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{})
	commitFlowFile(t, r, "login.txt", "login\n", "login")
	return r
}

func flowServer(t *testing.T, r *testRepo) *testRepo {
	t.Helper()
	server := newBareTestRepo(t)
	if err := AddRemote(r.repo, "origin", server.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	r.repo = r.reopen()
	specs := mustPushSpecs(t, "refs/heads/main:refs/heads/main", "refs/heads/develop:refs/heads/develop")
	if _, err := Push(t.Context(), r.repo, "origin", remote.PushOptions{Refspecs: specs}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	return server
}

func pushFlowBranch(t *testing.T, r *testRepo, branch string) {
	t.Helper()
	name := refs.BranchName(branch).String()
	if _, err := Push(t.Context(), r.repo, "origin", remote.PushOptions{Refspecs: mustPushSpecs(t, name+":"+name)}); err != nil {
		t.Fatalf("Push %s returned error %v", branch, err)
	}
}

func breakFlowRemote(t *testing.T, r *testRepo) {
	t.Helper()
	if err := SetRemoteURL(r.repo, "origin", filepath.Join(t.TempDir(), "missing"), false); err != nil {
		t.Fatalf("SetRemoteURL returned error %v", err)
	}
	r.repo = r.reopen()
}

func flowParents(t *testing.T, r *testRepo, id hash.ObjectID) []hash.ObjectID {
	t.Helper()
	commit, err := r.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	return commit.Parents
}

func flowMessage(t *testing.T, r *testRepo, id hash.ObjectID) string {
	t.Helper()
	commit, err := r.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	return commit.Message
}

func refMissing(t *testing.T, r *testRepo, name refs.Name) bool {
	t.Helper()
	_, err := r.refs().Lookup(name)
	return errors.Is(err, refs.ErrNotFound)
}

var originFlow = FlowNetwork{Remote: "origin"}

func TestFlowConfigStaysDefaultUntilWritten(t *testing.T) {
	r := newTestRepo(t)

	cfg, ok := ReadFlowConfig(r.repo)

	if ok || cfg != DefaultFlowConfig() || cfg.Light() {
		t.Fatalf("ReadFlowConfig = %+v, %v; want the full defaults and not configured", cfg, ok)
	}
}

func TestFlowConfigRoundTripsThroughTheRepositoryConfig(t *testing.T) {
	r := newTestRepo(t)
	want := FlowConfig{Master: "main", Develop: "dev", FeaturePrefix: "f/", ReleasePrefix: "r/", HotfixPrefix: "h/", SupportPrefix: "s/", VersionTagPrefix: "v", Remote: "upstream"}

	useFlowConfig(t, r, want)

	if got, ok := ReadFlowConfig(r.repo); !ok || got != want {
		t.Fatalf("ReadFlowConfig = %+v, %v; want %+v", got, ok, want)
	}
}

func TestLightFlowConfigForgetsTheMasterBranch(t *testing.T) {
	r := newTestRepo(t)
	useFlowConfig(t, r, mainFlowConfig())

	useFlowConfig(t, r, DefaultLightFlowConfig())

	got, ok := ReadFlowConfig(r.repo)
	if !ok || !got.Light() || got.Develop != "master" || got.FeaturePrefix != "feature/" || got.ReleasePrefix != "" {
		t.Fatalf("ReadFlowConfig = %+v, %v; want the light settings", got, ok)
	}
	if r.repo.Config().Has("gitflow.branch.master") {
		t.Fatal("the light settings kept a master branch")
	}
}

func TestSwitchOffFlowForgetsTheSettingsButKeepsTheBranches(t *testing.T) {
	r, base := newFlowRepo(t)

	for range 2 {
		if err := SwitchOffFlow(r.repo); err != nil {
			t.Fatalf("SwitchOffFlow returned error %v", err)
		}
		r.repo = r.reopen()
	}

	if _, ok := ReadFlowConfig(r.repo); ok {
		t.Fatal("git-flow is still configured")
	}
	if r.branchTarget("develop") != base {
		t.Fatal("switching git-flow off touched develop")
	}
}

var flowConfigWriters = map[string]func(r *testRepo) error{
	"write full":  func(r *testRepo) error { return WriteFlowConfig(r.repo, DefaultFlowConfig()) },
	"write light": func(r *testRepo) error { return WriteFlowConfig(r.repo, DefaultLightFlowConfig()) },
	"switch off":  func(r *testRepo) error { return SwitchOffFlow(r.repo) },
}

func TestFlowConfigWritersNeedTheLocalConfig(t *testing.T) {
	for name, write := range flowConfigWriters {
		r := newTestRepo(t)
		if err := os.Remove(r.repo.CommonPath("config")); err != nil {
			t.Fatalf("Remove returned error %v", err)
		}
		r.repo = r.reopen()

		if err := write(r); !errors.Is(err, ErrNoLocalConfig) {
			t.Fatalf("%s returned %v, want %v", name, err, ErrNoLocalConfig)
		}
	}
}

func TestFlowConfigWritersRejectAnUnusableKey(t *testing.T) {
	prev := flowConfigSection
	flowConfigSection = "git flow"
	t.Cleanup(func() { flowConfigSection = prev })

	for name, write := range flowConfigWriters {
		if err := write(newTestRepo(t)); err == nil {
			t.Fatalf("%s accepted a key git cannot store", name)
		}
	}
}

func TestFlowConfigWritersReportASaveFailure(t *testing.T) {
	for name, write := range flowConfigWriters {
		r := newTestRepo(t)
		path := r.repo.CommonPath("config")
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove returned error %v", err)
		}
		if err := os.MkdirAll(path+"/blocked", 0o777); err != nil {
			t.Fatalf("MkdirAll returned error %v", err)
		}

		if err := write(r); err == nil {
			t.Fatalf("%s reported success over a directory", name)
		}
	}
}

func TestConfigureFlowCreatesTheMissingBranchesFromHead(t *testing.T) {
	r := newTestRepo(t)
	head := commitFlowFile(t, r, "a.txt", "base\n", "base")
	current, _ := r.headSymbolicTarget()
	light := DefaultLightFlowConfig()
	light.Develop = "trunk"

	created, err := ConfigureFlow(t.Context(), r.repo, light)
	if err != nil || !slices.Equal(created, []refs.Name{refs.BranchName("trunk")}) || r.branchTarget("trunk") != head {
		t.Fatalf("ConfigureFlow light = %v, %v", created, err)
	}
	full := DefaultFlowConfig()
	full.Master = current.Short()
	created, err = ConfigureFlow(t.Context(), r.repo, full)
	if err != nil || !slices.Equal(created, []refs.Name{refs.BranchName("develop")}) {
		t.Fatalf("ConfigureFlow full = %v, %v", created, err)
	}

	if now, _ := r.headSymbolicTarget(); now != current {
		t.Fatalf("HEAD moved to %s", now)
	}
	if _, tracked := r.reopen().Config().Branch("develop"); tracked {
		t.Fatal("the created branch tracks something")
	}
	if got, ok := ReadFlowConfig(r.reopen()); !ok || got != full {
		t.Fatalf("ReadFlowConfig = %+v, %v", got, ok)
	}
}

func TestConfigureFlowReportsWhatItCannotDo(t *testing.T) {
	unborn := newTestRepo(t)
	if _, err := ConfigureFlow(t.Context(), unborn.repo, DefaultFlowConfig()); !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("ConfigureFlow on an unborn HEAD returned %v", err)
	}

	cancelled := newTestRepo(t)
	commitFlowFile(t, cancelled, "a.txt", "base\n", "base")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ConfigureFlow(ctx, cancelled.repo, DefaultFlowConfig()); !errors.Is(err, context.Canceled) {
		t.Fatalf("ConfigureFlow with a cancelled context returned %v", err)
	}

	unsaved := newTestRepo(t)
	commitFlowFile(t, unsaved, "a.txt", "base\n", "base")
	if err := os.Remove(unsaved.repo.CommonPath("config")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	created, err := ConfigureFlow(t.Context(), unsaved.reopen(), DefaultLightFlowConfig())
	if !errors.Is(err, ErrNoLocalConfig) || len(created) != 1 {
		t.Fatalf("ConfigureFlow without a config = %v, %v", created, err)
	}
}

func TestFlowConfigNamesTheBranchOfEveryKind(t *testing.T) {
	cfg := mainFlowConfig()
	for kind, want := range map[string]FlowBranch{
		FlowKindFeature: {Prefix: "feature/", Base: "develop"},
		FlowKindRelease: {Prefix: "release/", Base: "develop", Tagged: true},
		FlowKindHotfix:  {Prefix: "hotfix/", Base: "main", Tagged: true},
		FlowKindSupport: {Prefix: "support/", Base: "main"},
	} {
		if got, err := cfg.Branch(kind); err != nil || got != want {
			t.Errorf("Branch(%s) = %+v, %v; want %+v", kind, got, err, want)
		}
	}
	if _, err := cfg.Branch("bugfix"); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("Branch(bugfix) returned %v, want %v", err, ErrFlowKind)
	}
	light := DefaultLightFlowConfig()
	if got, err := light.Branch(FlowKindFeature); err != nil || got.Base != "master" {
		t.Fatalf("light feature = %+v, %v", got, err)
	}
	for _, kind := range []string{FlowKindRelease, FlowKindHotfix, FlowKindSupport} {
		if _, err := light.Branch(kind); !errors.Is(err, ErrFlowKind) {
			t.Errorf("light Branch(%s) returned %v", kind, err)
		}
	}
}

func TestFlowConfigRecognisesTheKindOfABranch(t *testing.T) {
	noFeatures := mainFlowConfig()
	noFeatures.FeaturePrefix = ""
	for _, c := range []struct {
		cfg        FlowConfig
		branch     string
		kind, name string
		ok         bool
	}{
		{mainFlowConfig(), "feature/login", FlowKindFeature, "login", true},
		{mainFlowConfig(), "release/1.0", FlowKindRelease, "1.0", true},
		{mainFlowConfig(), "hotfix/1.0.1", FlowKindHotfix, "1.0.1", true},
		{mainFlowConfig(), "support/1.x", FlowKindSupport, "1.x", true},
		{mainFlowConfig(), "hotfix/", "", "", false},
		{mainFlowConfig(), "develop", "", "", false},
		{noFeatures, "login", "", "", false},
		{DefaultLightFlowConfig(), "release/1.0", "", "", false},
		{DefaultLightFlowConfig(), "feature/login", FlowKindFeature, "login", true},
	} {
		kind, name, ok := c.cfg.BranchKind(c.branch)
		if kind != c.kind || name != c.name || ok != c.ok {
			t.Errorf("BranchKind(%q) = %q, %q, %v", c.branch, kind, name, ok)
		}
	}
}

func TestFlowRefusesAnUnknownKind(t *testing.T) {
	r, _ := newFlowRepo(t)

	if _, err := StartFlow(t.Context(), r.repo, "bugfix", "1.0", StartFlowOptions{}); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("StartFlow returned %v, want %v", err, ErrFlowKind)
	}
	if _, err := FinishFlow(t.Context(), r.repo, "bugfix", "1.0", FinishFlowOptions{}); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("FinishFlow returned %v, want %v", err, ErrFlowKind)
	}
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindSupport, "1.x", FinishFlowOptions{}); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("FinishFlow of a support branch returned %v, want %v", err, ErrFlowKind)
	}
}

func TestStartReleaseNeedsGitFlowAndAName(t *testing.T) {
	bare := newTestRepo(t)
	if _, err := StartFlow(t.Context(), bare.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("StartFlow without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := StartFlow(t.Context(), r.repo, FlowKindRelease, "  ", StartFlowOptions{}); !errors.Is(err, ErrFlowEmptyName) {
		t.Fatalf("StartFlow without a name returned %v", err)
	}
}

func TestStartReleaseBranchesFromDevelopWithoutTrackingAndSwitches(t *testing.T) {
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	tip := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	switchFlowBranch(t, r, "main")

	name, err := StartFlow(t.Context(), r.repo, FlowKindRelease, "1.0", StartFlowOptions{})
	if err != nil {
		t.Fatalf("StartFlow returned error %v", err)
	}

	if name != refs.BranchName("release/1.0") || r.branchTarget("release/1.0") != tip {
		t.Fatalf("release = %s at %s, want release/1.0 at %s", name, r.branchTarget("release/1.0"), tip)
	}
	if head, _ := r.headSymbolicTarget(); head != name {
		t.Fatalf("HEAD = %s, want %s", head, name)
	}
	if _, tracked := r.reopen().Config().Branch("release/1.0"); tracked {
		t.Fatal("the release branch tracks something")
	}
}

func TestStartReleaseFailsBeforeTouchingTheRepository(t *testing.T) {
	r, _ := newFlowRepo(t)
	cfg := mainFlowConfig()
	cfg.Develop = "de:v"
	useFlowConfig(t, r, cfg)

	if _, err := StartFlow(t.Context(), r.repo, FlowKindRelease, "1.0", StartFlowOptions{}); err == nil {
		t.Fatal("StartFlow accepted a develop name that is no branch")
	}
	if !refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the release branch was created anyway")
	}
}

func TestStartReleaseReportsTheFailingStep(t *testing.T) {
	unreachable, _ := newFlowRepo(t)
	flowServer(t, unreachable)
	breakFlowRemote(t, unreachable)
	if _, err := StartFlow(t.Context(), unreachable.repo, FlowKindRelease, "1.0", StartFlowOptions{Network: originFlow}); err == nil {
		t.Fatal("StartFlow fetched a tracked develop from a remote that does not exist")
	}

	taken, base := newFlowRepo(t)
	taken.createBranch("release/1.0", base)
	if _, err := StartFlow(t.Context(), taken.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("StartFlow over an existing branch returned %v", err)
	}

	dirty, _ := newFlowRepo(t)
	switchFlowBranch(t, dirty, "develop")
	commitFlowFile(t, dirty, "a.txt", "develop\n", "develop work")
	switchFlowBranch(t, dirty, "main")
	dirty.writeFile("a.txt", "local\n")
	if _, err := StartFlow(t.Context(), dirty.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("StartFlow over local changes returned %v", err)
	}
}

func TestStartFlowFetchesOnlyABaseThatHasARemoteBranch(t *testing.T) {
	r, base := newFlowRepo(t)
	if err := AddRemote(r.repo, "origin", newBareTestRepo(t).dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	r.repo = r.reopen()
	breakFlowRemote(t, r)

	startFlowRelease(t, r, "1.0", originFlow)

	if r.branchTarget("release/1.0") != base {
		t.Fatal("the release did not start from develop")
	}
}

func TestStartFlowBranchesFromTheBaseOfEachKindOrAChosenOne(t *testing.T) {
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	develop := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	switchFlowBranch(t, r, "main")
	main := commitFlowFile(t, r, "h.txt", "main\n", "main work")
	tag, err := CreateTag(t.Context(), r.repo, "v1.0", "main", CreateTagOptions{Message: "v1.0"})
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	for _, c := range []struct {
		kind, name, base, branch string
		at                       hash.ObjectID
	}{
		{FlowKindFeature, "login", "", "feature/login", develop},
		{FlowKindHotfix, "1.0.1", "", "hotfix/1.0.1", main},
		{FlowKindSupport, "1.x", "v1.0", "support/1.x", tag.Target},
		{FlowKindFeature, "fix", "main", "feature/fix", main},
		{FlowKindFeature, "raw", main.String(), "feature/raw", main},
	} {
		got, err := StartFlow(t.Context(), r.repo, c.kind, c.name, StartFlowOptions{Base: c.base})
		if err != nil || got != refs.BranchName(c.branch) || r.branchTarget(c.branch) != c.at {
			t.Fatalf("StartFlow(%s %s) = %s, %v at %s; want %s at %s", c.kind, c.name, got, err, r.branchTarget(c.branch), c.branch, c.at)
		}
		if head, _ := r.headSymbolicTarget(); head != got {
			t.Fatalf("HEAD = %s, want %s", head, got)
		}
	}
}

func TestHasRemoteBranchLooksAtTheRemoteTrackingBranch(t *testing.T) {
	r, _ := newFlowRepo(t)
	flowServer(t, r)

	for branch, want := range map[string]bool{"develop": true, "release/1.0": false} {
		if got, err := HasRemoteBranch(r.repo, "origin", branch); err != nil || got != want {
			t.Errorf("HasRemoteBranch(%s) = %v, %v; want %v", branch, got, err, want)
		}
	}
}

func TestFinishReleaseNeedsGitFlowAndAName(t *testing.T) {
	bare := newTestRepo(t)
	if _, err := FinishFlow(t.Context(), bare.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("FinishFlow without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "", FinishFlowOptions{}); !errors.Is(err, ErrFlowEmptyName) {
		t.Fatalf("FinishFlow without a name returned %v", err)
	}
}

func TestFinishReleaseRejectsNamesThatAreNoRefspecs(t *testing.T) {
	for _, bad := range []func(*FlowConfig){
		func(c *FlowConfig) { c.Master = "ma:in" },
		func(c *FlowConfig) { c.VersionTagPrefix = "v:" },
	} {
		r := newReleaseRepo(t)
		cfg := mainFlowConfig()
		bad(&cfg)
		useFlowConfig(t, r, cfg)
		main := r.branchTarget("main")

		if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
			t.Fatalf("FinishFlow accepted %+v", cfg)
		}
		if r.branchTarget("main") != main {
			t.Fatal("main moved although the finish was refused")
		}
	}
}

func TestFinishReleaseMergesIntoMainTagsMergesTheTagIntoDevelopAndDeletesTheBranch(t *testing.T) {
	r := newReleaseRepo(t)
	developBefore := r.branchTarget("develop")
	mainBefore := r.branchTarget("main")
	release := r.branchTarget("release/1.0")
	when := time.Unix(1700009000, 0).UTC()

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, DeleteBranch: true, When: when})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	mainTip := r.branchTarget("main")
	if got := flowParents(t, r, mainTip); !slices.Equal(got, []hash.ObjectID{mainBefore, release}) {
		t.Fatalf("main merge parents = %v, want %v and %v", got, mainBefore, release)
	}
	if !result.Tag.Annotated() || result.Tag.Target != mainTip || result.Tag.Name != "1.0" {
		t.Fatalf("tag = %+v, want an annotated 1.0 on %s", result.Tag, mainTip)
	}
	developTip := r.branchTarget("develop")
	if got := flowParents(t, r, developTip); !slices.Equal(got, []hash.ObjectID{developBefore, mainTip}) {
		t.Fatalf("develop merge parents = %v, want %v and %v", got, developBefore, mainTip)
	}
	commit, err := r.db().Commit(mainTip)
	if err != nil || commit.Message != "Finish 1.0\n" || !commit.Committer.When.Equal(when) || flowMessage(t, r, developTip) != "Finish 1.0\n" {
		t.Fatalf("main merge = %+v, %v", commit, err)
	}
	if !refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the release branch is still there")
	}
	if head, _ := r.headSymbolicTarget(); head != refs.BranchName("develop") {
		t.Fatalf("HEAD = %s, want develop", head)
	}
	if _, pending, err := PendingFlowFinish(r.repo); pending || err != nil {
		t.Fatalf("a finish is still pending: %v, %v", pending, err)
	}
}

func TestFinishReleaseUsesOneMessageForTheMergesAndTheTag(t *testing.T) {
	r := newReleaseRepo(t)

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Message: "Release 1.0", TagName: "rel-1.0"})
	if err != nil || !result.Finished() || result.Tag.Name != "rel-1.0" {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	if refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the release branch was deleted without being asked")
	}
	_, data, err := r.db().Get(result.Tag.Tag)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	parsed, err := object.ParseTag(data)
	if err != nil || parsed.Message != "Release 1.0\n" || flowMessage(t, r, r.branchTarget("main")) != "Release 1.0\n" {
		t.Fatalf("tag message = %+v, %v", parsed, err)
	}
}

func TestFinishReleasePushesDevelopMainTheTagAndRemovesTheRemoteBranch(t *testing.T) {
	r, _ := newFlowRepo(t)
	server := flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	commitFlowFile(t, r, "VERSION", "1.0\n", "bump")
	pushFlowBranch(t, r, "release/1.0")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, DeleteBranch: true, Network: originFlow})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	store := server.refs()
	for name, want := range map[refs.Name]hash.ObjectID{
		refs.BranchName("main"):    r.branchTarget("main"),
		refs.BranchName("develop"): r.branchTarget("develop"),
		refs.TagName("1.0"):        result.Tag.Tag,
	} {
		got, err := store.Lookup(name)
		if err != nil || got.Target != want {
			t.Fatalf("server %s = %+v, %v; want %s", name, got, err, want)
		}
	}
	if _, err := store.Lookup(refs.BranchName("release/1.0")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("server release branch lookup = %v, want it removed", err)
	}
	if !refMissing(t, r, refs.RemoteBranchName("origin", "release/1.0")) {
		t.Fatal("the remote-tracking release branch is still there")
	}
}

func releaseBehindItsRemote(t *testing.T) *testRepo {
	t.Helper()
	r, base := newFlowRepo(t)
	flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	tree, err := r.db().Commit(base)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	sig := testSignature()
	ahead, err := r.db().PutObject(&object.Commit{Tree: tree.Tree, Parents: []hash.ObjectID{base}, Author: sig, Committer: sig, Message: "elsewhere\n"})
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	r.createBranch("elsewhere", ahead)
	if _, err := Push(t.Context(), r.repo, "origin", remote.PushOptions{Refspecs: mustPushSpecs(t, "refs/heads/elsewhere:refs/heads/main")}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	return r
}

func TestFinishReleaseChecksTheRemoteOnlyWhenAskedToFetch(t *testing.T) {
	unchecked := releaseBehindItsRemote(t)
	if _, err := FinishFlow(t.Context(), unchecked.repo, FlowKindRelease, "1.0", FinishFlowOptions{Network: originFlow}); err != nil {
		t.Fatalf("FinishFlow without a fetch returned %v", err)
	}

	checked := releaseBehindItsRemote(t)
	_, err := FinishFlow(t.Context(), checked.repo, FlowKindRelease, "1.0", FinishFlowOptions{Fetch: true, Network: originFlow})

	if !errors.Is(err, ErrFlowBehind) {
		t.Fatalf("FinishFlow returned %v, want %v", err, ErrFlowBehind)
	}
}

func TestFinishReleaseReportsAFailedFetch(t *testing.T) {
	r, _ := newFlowRepo(t)
	flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	breakFlowRemote(t, r)

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Fetch: true, Network: originFlow}); err == nil {
		t.Fatal("FinishFlow fetched from a remote that does not exist")
	}
}

func TestFlowNotBehindAcceptsABranchThatWasNeverFetched(t *testing.T) {
	r, _ := newFlowRepo(t)
	if err := AddRemote(r.repo, "origin", newBareTestRepo(t).dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	r.repo = r.reopen()

	if err := flowNotBehind(r.repo, originFlow, "develop"); err != nil {
		t.Fatalf("flowNotBehind returned %v for a branch without a remote-tracking ref", err)
	}
}

func TestFinishReleaseNeedsTheLocalBranchesItChecks(t *testing.T) {
	r, _ := newFlowRepo(t)
	flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	if err := DeleteBranch(t.Context(), r.repo, "develop", true); err != nil {
		t.Fatalf("DeleteBranch returned error %v", err)
	}

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Fetch: true, Network: originFlow}); !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("FinishFlow returned %v, want %v", err, ErrBranchNotFound)
	}
}

func TestFinishReleaseStopsOnAConflictAndResumesAfterTheCommit(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "a.txt", "release\n", "release change")
	switchFlowBranch(t, r, "main")
	commitFlowFile(t, r, "a.txt", "hotfix\n", "main change")
	switchFlowBranch(t, r, "release/1.0")

	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{DeleteBranch: true})
	if err != nil || stopped.Finished() || stopped.Stopped != FlowStepMergeMaster || !slices.Equal(stopped.Conflicts, []string{"a.txt"}) {
		t.Fatalf("FinishFlow = %+v, %v; want a stop on a.txt while merging into main", stopped, err)
	}
	pending, found, err := PendingFlowFinish(r.repo)
	if err != nil || !found || pending != (FlowFinish{Kind: FlowKindRelease, Name: "1.0", Step: FlowStepMergeMaster}) {
		t.Fatalf("PendingFlowFinish = %+v, %v, %v", pending, found, err)
	}
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("FinishFlow before the commit returned %v", err)
	}

	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "Finish 1.0"}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	resumed, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})

	if err != nil || !resumed.Finished() || !resumed.Tag.Annotated() {
		t.Fatalf("resumed FinishFlow = %+v, %v", resumed, err)
	}
	if !refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the resumed finish forgot the saved choice to delete the branch")
	}
}

func TestFinishReleaseStopsOnAConflictWithDevelop(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "a.txt", "release\n", "release change")
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "a.txt", "develop\n", "develop change")
	switchFlowBranch(t, r, "release/1.0")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})

	if err != nil || result.Stopped != FlowStepMergeDevelop || !result.Tag.Annotated() {
		t.Fatalf("FinishFlow = %+v, %v; want the tag and a stop at develop", result, err)
	}
}

func TestFinishReleaseRefusesAnotherOrDamagedPendingFinish(t *testing.T) {
	for text, want := range map[string]error{
		"release\n2.0\n1\ntrue\ntrue\n2.0\ntrue\n0\nmessage": ErrFlowPending,
		"release\n1.0": ErrFlowState,
		"release\n1.0\nx\ntrue\ntrue\n\ntrue\n0\n":    ErrFlowState,
		"release\n1.0\n1\ntrue\ntrue\n\nmaybe\n0\n":   ErrFlowState,
		"release\n1.0\n1\ntrue\ntrue\n\ntrue\nsome\n": ErrFlowState,
	} {
		r := newReleaseRepo(t)
		if err := writeStateFile(r.repo, flowStateFile, text); err != nil {
			t.Fatalf("writeStateFile returned error %v", err)
		}

		if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, want) {
			t.Fatalf("state %q: FinishFlow returned %v, want %v", text, err, want)
		}
	}
}

func TestFinishReleaseReportsUnreadableState(t *testing.T) {
	flow := newReleaseRepo(t)
	if err := os.MkdirAll(flow.repo.GitPath(flowStateFile)+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := FinishFlow(t.Context(), flow.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
		t.Fatal("FinishFlow read a directory as its state")
	}

	merge := newReleaseRepo(t)
	if err := writeStateFile(merge.repo, flowStateFile, "release\n1.0\n1\nfalse\nfalse\n\ntrue\n0\n"); err != nil {
		t.Fatalf("writeStateFile returned error %v", err)
	}
	if err := os.MkdirAll(merge.repo.GitPath(mergeHeadFile)+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := FinishFlow(t.Context(), merge.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
		t.Fatal("FinishFlow resumed over an unreadable merge state")
	}
}

func TestFinishFeatureMergesIntoDevelopOnlyAndDeletesTheBranch(t *testing.T) {
	r := newFeatureRepo(t)
	developBefore := r.branchTarget("develop")
	mainBefore := r.branchTarget("main")
	feature := r.branchTarget("feature/login")
	when := time.Unix(1700009000, 0).UTC()

	result, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{DeleteBranch: true, When: when})
	if err != nil || !result.Finished() || result.Tag != (TagResult{}) {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	develop := r.branchTarget("develop")
	if got := flowParents(t, r, develop); !slices.Equal(got, []hash.ObjectID{developBefore, feature}) {
		t.Fatalf("develop merge parents = %v, want %v and %v", got, developBefore, feature)
	}
	if got := flowMessage(t, r, develop); got != "Finish login\n" {
		t.Fatalf("develop merge message = %q", got)
	}
	if r.branchTarget("main") != mainBefore {
		t.Fatal("finishing a feature moved main")
	}
	if !refMissing(t, r, refs.BranchName("feature/login")) || !refMissing(t, r, refs.TagName("login")) {
		t.Fatal("the feature branch survived or a tag appeared")
	}
	if head, _ := r.headSymbolicTarget(); head != refs.BranchName("develop") {
		t.Fatalf("HEAD = %s, want develop", head)
	}
}

func TestFinishFeatureRemovesItsRemoteBranchWithoutPushingDevelop(t *testing.T) {
	r, _ := newFlowRepo(t)
	server := flowServer(t, r)
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{Network: originFlow})
	commitFlowFile(t, r, "login.txt", "login\n", "login")
	pushFlowBranch(t, r, "feature/login")
	developOnServer := r.branchTarget("develop")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Message: "Add login", Fetch: true, Push: true, Network: originFlow})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	if got := flowMessage(t, r, r.branchTarget("develop")); got != "Add login\n" {
		t.Fatalf("develop merge message = %q", got)
	}
	store := server.refs()
	if got, err := store.Lookup(refs.BranchName("develop")); err != nil || got.Target != developOnServer {
		t.Fatalf("server develop = %+v, %v; want it untouched at %s", got, err, developOnServer)
	}
	if _, err := store.Lookup(refs.BranchName("feature/login")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("server feature lookup = %v, want it removed", err)
	}
	if refMissing(t, r, refs.BranchName("feature/login")) {
		t.Fatal("the local feature branch was deleted without being asked")
	}

	local := newFeatureRepo(t)
	if _, err := FinishFlow(t.Context(), local.repo, FlowKindFeature, "login", FinishFlowOptions{Push: true, Network: originFlow}); err != nil {
		t.Fatalf("FinishFlow of a feature that was never pushed returned %v", err)
	}
}

func TestFinishFeatureSquashesIntoOneCommit(t *testing.T) {
	r := newFeatureRepo(t)
	commitFlowFile(t, r, "login2.txt", "more\n", "more login")
	developBefore := r.branchTarget("develop")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: FlowSquash, Message: "Add login", DeleteBranch: true})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	develop := r.branchTarget("develop")
	if got := flowParents(t, r, develop); !slices.Equal(got, []hash.ObjectID{developBefore}) || flowMessage(t, r, develop) != "Add login\n" {
		t.Fatalf("develop = parents %v, message %q; want one commit on %s", got, flowMessage(t, r, develop), developBefore)
	}
	if !refMissing(t, r, refs.BranchName("feature/login")) || !r.exists("login2.txt") {
		t.Fatal("the squash lost the feature or kept its branch")
	}
}

func TestFinishFeatureSquashReportsItsFailures(t *testing.T) {
	empty, _ := newFlowRepo(t)
	startFlow(t, empty, FlowKindFeature, "nothing", StartFlowOptions{})
	if _, err := FinishFlow(t.Context(), empty.repo, FlowKindFeature, "nothing", FinishFlowOptions{Integration: FlowSquash}); err == nil {
		t.Fatal("FinishFlow squashed a feature without changes")
	}

	r, _ := newFlowRepo(t)
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{})
	commitFlowFile(t, r, "a.txt", "feature\n", "feature change")
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "a.txt", "develop\n", "develop change")
	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: FlowSquash})

	if err != nil || stopped.Stopped != FlowStepMergeDevelop || !slices.Equal(stopped.Conflicts, []string{"a.txt"}) {
		t.Fatalf("FinishFlow = %+v, %v; want a stop on a.txt", stopped, err)
	}
}

func TestFinishFeatureRebasesAndFastForwardsDevelop(t *testing.T) {
	r := newFeatureRepo(t)
	switchFlowBranch(t, r, "develop")
	developTip := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: FlowRebase, DeleteBranch: true})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	develop := r.branchTarget("develop")
	if got := flowParents(t, r, develop); !slices.Equal(got, []hash.ObjectID{developTip}) || flowMessage(t, r, develop) != "login\n" {
		t.Fatalf("develop = parents %v, message %q; want the rebased feature on %s", got, flowMessage(t, r, develop), developTip)
	}
	if !refMissing(t, r, refs.BranchName("feature/login")) {
		t.Fatal("the feature branch is still there")
	}
}

func TestFinishFeatureRebaseStopsOnAConflictAndResumesAfterTheRebase(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{})
	commitFlowFile(t, r, "a.txt", "feature\n", "feature change")
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "a.txt", "develop\n", "develop change")

	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: FlowRebase, DeleteBranch: true})
	if err != nil || stopped.Stopped != FlowStepRebase || !slices.Equal(stopped.Conflicts, []string{"a.txt"}) {
		t.Fatalf("FinishFlow = %+v, %v; want a stop on a.txt while rebasing", stopped, err)
	}
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("FinishFlow during the rebase returned %v", err)
	}

	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")
	if _, err := ContinueRebase(t.Context(), r.repo, RebaseOptions{}); err != nil {
		t.Fatalf("ContinueRebase returned error %v", err)
	}
	resumed, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{})

	if err != nil || !resumed.Finished() || !refMissing(t, r, refs.BranchName("feature/login")) {
		t.Fatalf("resumed FinishFlow = %+v, %v", resumed, err)
	}
	if r.readFile("a.txt") != "resolved\n" {
		t.Fatalf("develop has %q", r.readFile("a.txt"))
	}
}

func TestFinishFeatureRebaseReportsASwitchFailure(t *testing.T) {
	r := newFeatureRepo(t)
	switchFlowBranch(t, r, "develop")
	r.writeFile("login.txt", "local\n")

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: FlowRebase}); !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("FinishFlow returned %v, want %v", err, ErrWouldOverwrite)
	}
}

func TestFinishFeatureStopsOnAConflictAndResumesAfterTheCommit(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{})
	commitFlowFile(t, r, "a.txt", "feature\n", "feature change")
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "a.txt", "develop\n", "develop change")

	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{DeleteBranch: true})
	if err != nil || stopped.Stopped != FlowStepMergeDevelop || !slices.Equal(stopped.Conflicts, []string{"a.txt"}) {
		t.Fatalf("FinishFlow = %+v, %v; want a stop on a.txt while merging into develop", stopped, err)
	}
	if pending, found, err := PendingFlowFinish(r.repo); err != nil || !found || pending != (FlowFinish{Kind: FlowKindFeature, Name: "login", Step: FlowStepMergeDevelop}) {
		t.Fatalf("PendingFlowFinish = %+v, %v, %v", pending, found, err)
	}
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "login", FinishFlowOptions{}); !errors.Is(err, ErrFlowPending) {
		t.Fatalf("a release finish over a pending feature finish returned %v", err)
	}

	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "Finish login"}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	resumed, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{})

	if err != nil || !resumed.Finished() || !refMissing(t, r, refs.BranchName("feature/login")) {
		t.Fatalf("resumed FinishFlow = %+v, %v", resumed, err)
	}
}

func newHotfixRepo(t *testing.T) *testRepo {
	t.Helper()
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	startFlow(t, r, FlowKindHotfix, "1.0.1", StartFlowOptions{})
	commitFlowFile(t, r, "fix.txt", "fix\n", "fix")
	return r
}

func TestFinishHotfixMergesTheTagIntoDevelopWithGitsOwnMessage(t *testing.T) {
	r := newHotfixRepo(t)
	developBefore := r.branchTarget("develop")
	mainBefore := r.branchTarget("main")
	hotfix := r.branchTarget("hotfix/1.0.1")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindHotfix, "1.0.1", FinishFlowOptions{DeleteBranch: true})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	mainTip := r.branchTarget("main")
	if got := flowParents(t, r, mainTip); !slices.Equal(got, []hash.ObjectID{mainBefore, hotfix}) || flowMessage(t, r, mainTip) != "Finish 1.0.1\n" {
		t.Fatalf("main merge parents = %v, want %v and %v", got, mainBefore, hotfix)
	}
	if result.Tag.Name != "1.0.1" || result.Tag.Target != mainTip {
		t.Fatalf("tag = %+v, want 1.0.1 on %s", result.Tag, mainTip)
	}
	develop := r.branchTarget("develop")
	if got := flowParents(t, r, develop); !slices.Equal(got, []hash.ObjectID{developBefore, mainTip}) {
		t.Fatalf("develop merge parents = %v, want %v and %v", got, developBefore, mainTip)
	}
	if got := flowMessage(t, r, develop); got == "Finish 1.0.1\n" {
		t.Fatalf("develop merge message = %q, want git's own", got)
	}
	if !refMissing(t, r, refs.BranchName("hotfix/1.0.1")) {
		t.Fatal("the hotfix branch is still there")
	}
}

func TestFinishHotfixCanSkipTheTagAndDevelop(t *testing.T) {
	r := newHotfixRepo(t)
	developBefore := r.branchTarget("develop")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindHotfix, "1.0.1", FinishFlowOptions{SkipTag: true, SkipDevelop: true})
	if err != nil || !result.Finished() || result.Tag != (TagResult{}) {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	if r.branchTarget("develop") != developBefore || !refMissing(t, r, refs.TagName("1.0.1")) {
		t.Fatal("the hotfix touched develop or tagged although both were skipped")
	}
	if head, _ := r.headSymbolicTarget(); head != refs.BranchName("main") {
		t.Fatalf("HEAD = %s, want main", head)
	}
}

func TestFinishReleaseWithoutATagMergesTheBranchIntoDevelop(t *testing.T) {
	r := newReleaseRepo(t)
	release := r.branchTarget("release/1.0")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{SkipTag: true, SkipDevelop: true})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	if got := flowParents(t, r, r.branchTarget("develop")); len(got) != 2 || got[1] != release {
		t.Fatalf("develop merge parents = %v, want the release %s merged", got, release)
	}
}

func TestIntegrateDevelopMergesOrRebasesTheFeature(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		r := newFeatureRepo(t)
		switchFlowBranch(t, r, "develop")
		developTip := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")

		integrated, err := IntegrateDevelop(t.Context(), r.repo, "login", IntegrateDevelopOptions{Rebase: rebase})
		if err != nil || !integrated.Clean() {
			t.Fatalf("IntegrateDevelop(rebase=%v) = %+v, %v", rebase, integrated, err)
		}

		feature := r.branchTarget("feature/login")
		parents := flowParents(t, r, feature)
		if want := 2; rebase {
			want = 1
			if len(parents) != want || parents[0] != developTip {
				t.Fatalf("rebased feature parents = %v, want %s", parents, developTip)
			}
		} else if len(parents) != want || parents[1] != developTip {
			t.Fatalf("merged feature parents = %v, want develop %s", parents, developTip)
		}
		if head, _ := r.headSymbolicTarget(); head != refs.BranchName("feature/login") {
			t.Fatalf("HEAD = %s, want the feature", head)
		}
	}
}

func TestIntegrateDevelopReportsWhatItCannotDo(t *testing.T) {
	bare := newTestRepo(t)
	if _, err := IntegrateDevelop(t.Context(), bare.repo, "login", IntegrateDevelopOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("IntegrateDevelop without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := IntegrateDevelop(t.Context(), r.repo, "missing", IntegrateDevelopOptions{}); err == nil {
		t.Fatal("IntegrateDevelop switched to a feature that does not exist")
	}
}

func TestFinishReleaseReportsTheFailingStep(t *testing.T) {
	for name, cfg := range map[string]func(*FlowConfig){
		"main is missing":    func(c *FlowConfig) { c.Master = "gone" },
		"the tag is invalid": func(c *FlowConfig) { c.VersionTagPrefix = "bad " },
		"develop is missing": func(c *FlowConfig) { c.Develop = "gone" },
	} {
		t.Run(name, func(t *testing.T) {
			r := newReleaseRepo(t)
			flow := mainFlowConfig()
			cfg(&flow)
			useFlowConfig(t, r, flow)

			if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
				t.Fatal("FinishFlow reported success")
			}
		})
	}
	r, _ := newFlowRepo(t)
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "9.9", FinishFlowOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("FinishFlow without the release branch returned %v", err)
	}
}
