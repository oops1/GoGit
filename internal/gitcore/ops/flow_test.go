package ops

import (
	"errors"
	"os"
	"slices"
	"strings"
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

func startFlowRelease(t *testing.T, r *testRepo, version string, net FlowNetwork) {
	t.Helper()
	if _, err := StartFlow(t.Context(), r.repo, FlowKindRelease, version, StartFlowOptions{Network: net}); err != nil {
		t.Fatalf("StartRelease returned error %v", err)
	}
}

func newReleaseRepo(t *testing.T) *testRepo {
	t.Helper()
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "VERSION", "1.0\n", "bump")
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

func flowParents(t *testing.T, r *testRepo, id hash.ObjectID) []hash.ObjectID {
	t.Helper()
	commit, err := r.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	return commit.Parents
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

	if ok || cfg != DefaultFlowConfig() {
		t.Fatalf("ReadFlowConfig = %+v, %v; want the defaults and not configured", cfg, ok)
	}
}

func TestFlowConfigRoundTripsThroughTheRepositoryConfig(t *testing.T) {
	r := newTestRepo(t)
	want := FlowConfig{Master: "main", Develop: "dev", FeaturePrefix: "f/", ReleasePrefix: "r/", HotfixPrefix: "h/", SupportPrefix: "s/", VersionTagPrefix: "v"}

	useFlowConfig(t, r, want)

	if got, ok := ReadFlowConfig(r.repo); !ok || got != want {
		t.Fatalf("ReadFlowConfig = %+v, %v; want %+v", got, ok, want)
	}
}

func TestWriteFlowConfigNeedsTheLocalConfig(t *testing.T) {
	r := newTestRepo(t)
	if err := os.Remove(r.repo.CommonPath("config")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}

	if err := WriteFlowConfig(r.reopen(), DefaultFlowConfig()); !errors.Is(err, ErrNoLocalConfig) {
		t.Fatalf("WriteFlowConfig returned %v, want %v", err, ErrNoLocalConfig)
	}
}

func TestWriteFlowConfigRejectsAnUnusableKey(t *testing.T) {
	r := newTestRepo(t)
	prev := flowConfigSection
	flowConfigSection = "git flow"
	t.Cleanup(func() { flowConfigSection = prev })

	if err := WriteFlowConfig(r.repo, DefaultFlowConfig()); err == nil {
		t.Fatal("WriteFlowConfig accepted a key git cannot store")
	}
}

func TestWriteFlowConfigReportsASaveFailure(t *testing.T) {
	r := newTestRepo(t)
	path := r.repo.CommonPath("config")
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := os.MkdirAll(path+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}

	if err := WriteFlowConfig(r.repo, DefaultFlowConfig()); err == nil {
		t.Fatal("WriteFlowConfig reported success over a directory")
	}
}

func TestStartReleaseNeedsGitFlowAndAName(t *testing.T) {
	bare := newTestRepo(t)
	if _, err := StartFlow(t.Context(), bare.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("StartRelease without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := StartFlow(t.Context(), r.repo, FlowKindRelease, "  ", StartFlowOptions{}); !errors.Is(err, ErrFlowEmptyName) {
		t.Fatalf("StartRelease without a name returned %v", err)
	}
}

func TestStartReleaseBranchesFromDevelopWithoutTrackingAndSwitches(t *testing.T) {
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	tip := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	switchFlowBranch(t, r, "main")

	name, err := StartFlow(t.Context(), r.repo, FlowKindRelease, "1.0", StartFlowOptions{})
	if err != nil {
		t.Fatalf("StartRelease returned error %v", err)
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
		t.Fatal("StartRelease accepted a develop name that is no refspec")
	}
	if !refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the release branch was created anyway")
	}
}

func TestStartReleaseReportsTheFailingStep(t *testing.T) {
	missingRemote, _ := newFlowRepo(t)
	if _, err := StartFlow(t.Context(), missingRemote.repo, FlowKindRelease, "1.0", StartFlowOptions{Network: FlowNetwork{Remote: "nowhere"}}); err == nil {
		t.Fatal("StartRelease fetched from a remote that does not exist")
	}

	taken, base := newFlowRepo(t)
	taken.createBranch("release/1.0", base)
	if _, err := StartFlow(t.Context(), taken.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("StartRelease over an existing branch returned %v", err)
	}

	dirty, _ := newFlowRepo(t)
	switchFlowBranch(t, dirty, "develop")
	commitFlowFile(t, dirty, "a.txt", "develop\n", "develop work")
	switchFlowBranch(t, dirty, "main")
	dirty.writeFile("a.txt", "local\n")
	if _, err := StartFlow(t.Context(), dirty.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("StartRelease over local changes returned %v", err)
	}
}

func TestStartReleaseFetchesDevelopFromTheRemote(t *testing.T) {
	r, base := newFlowRepo(t)
	flowServer(t, r)

	startFlowRelease(t, r, "1.0", originFlow)

	tracking, err := r.refs().Lookup(refs.RemoteBranchName("origin", "develop"))
	if err != nil || tracking.Target != base {
		t.Fatalf("origin/develop = %+v, %v; want %s", tracking, err, base)
	}
}

func TestFinishReleaseNeedsGitFlowAndAName(t *testing.T) {
	bare := newTestRepo(t)
	if _, err := FinishFlow(t.Context(), bare.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("FinishRelease without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "", FinishFlowOptions{}); !errors.Is(err, ErrFlowEmptyName) {
		t.Fatalf("FinishRelease without a name returned %v", err)
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
			t.Fatalf("FinishRelease accepted %+v", cfg)
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

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Message: "Release 1.0", Push: true, DeleteBranch: true, When: when})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishRelease = %+v, %v", result, err)
	}

	mainTip := r.branchTarget("main")
	if got := flowParents(t, r, mainTip); !slices.Equal(got, []hash.ObjectID{mainBefore, release}) {
		t.Fatalf("main merge parents = %v, want %v and %v", got, mainBefore, release)
	}
	if !result.Tag.Annotated() || result.Tag.Target != mainTip || result.Tag.Name != "1.0" {
		t.Fatalf("tag = %+v, want an annotated 1.0 on %s", result.Tag, mainTip)
	}
	if got := flowParents(t, r, r.branchTarget("develop")); !slices.Equal(got, []hash.ObjectID{developBefore, mainTip}) {
		t.Fatalf("develop merge parents = %v, want %v and %v", got, developBefore, mainTip)
	}
	commit, err := r.db().Commit(mainTip)
	if err != nil || commit.Message != "Finish 1.0\n" || !commit.Committer.When.Equal(when) {
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

func TestFinishReleaseKeepsTheBranchAndNamesTheTagAfterTheFinishWhenAsked(t *testing.T) {
	r := newReleaseRepo(t)

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishRelease = %+v, %v", result, err)
	}

	if refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the release branch was deleted without being asked")
	}
	_, data, err := r.db().Get(result.Tag.Tag)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	parsed, err := object.ParseTag(data)
	if err != nil || parsed.Message != "Finish 1.0\n" {
		t.Fatalf("tag message = %+v, %v", parsed, err)
	}
}

func TestFinishReleasePushesDevelopMainAndTheTag(t *testing.T) {
	r, _ := newFlowRepo(t)
	server := flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	commitFlowFile(t, r, "VERSION", "1.0\n", "bump")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, DeleteBranch: true, Network: originFlow})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishRelease = %+v, %v", result, err)
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
}

func TestFinishReleaseRefusesABranchBehindItsRemote(t *testing.T) {
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

	_, err = FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Network: originFlow})

	if !errors.Is(err, ErrFlowBehind) {
		t.Fatalf("FinishRelease returned %v, want %v", err, ErrFlowBehind)
	}
}

func TestFinishReleaseReportsAFailedFetch(t *testing.T) {
	r := newReleaseRepo(t)

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Network: FlowNetwork{Remote: "nowhere"}}); err == nil {
		t.Fatal("FinishRelease fetched from a remote that does not exist")
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

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Network: originFlow}); !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("FinishRelease returned %v, want %v", err, ErrBranchNotFound)
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
		t.Fatalf("FinishRelease = %+v, %v; want a stop on a.txt while merging into main", stopped, err)
	}
	pending, found, err := PendingFlowFinish(r.repo)
	if err != nil || !found || pending != (FlowFinish{Kind: "release", Name: "1.0", Step: FlowStepMergeMaster}) {
		t.Fatalf("PendingFlowFinish = %+v, %v, %v", pending, found, err)
	}
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("FinishRelease before the commit returned %v", err)
	}

	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "Finish 1.0"}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	resumed, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})

	if err != nil || !resumed.Finished() || !resumed.Tag.Annotated() {
		t.Fatalf("resumed FinishRelease = %+v, %v", resumed, err)
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
		t.Fatalf("FinishRelease = %+v, %v; want the tag and a stop at develop", result, err)
	}
}

func TestFinishReleaseRefusesAnotherOrDamagedPendingFinish(t *testing.T) {
	for text, want := range map[string]error{
		"release\n2.0\n1\ntrue\ntrue\nmessage": ErrFlowPending,
		"release\n1.0":                         ErrFlowState,
		"release\n1.0\nx\ntrue\ntrue\n":        ErrFlowState,
	} {
		r := newReleaseRepo(t)
		if err := writeStateFile(r.repo, flowStateFile, text); err != nil {
			t.Fatalf("writeStateFile returned error %v", err)
		}

		if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, want) {
			t.Fatalf("state %q: FinishRelease returned %v, want %v", text, err, want)
		}
	}
}

func TestFinishReleaseReportsUnreadableState(t *testing.T) {
	flow := newReleaseRepo(t)
	if err := os.MkdirAll(flow.repo.GitPath(flowStateFile)+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := FinishFlow(t.Context(), flow.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
		t.Fatal("FinishRelease read a directory as its state")
	}

	merge := newReleaseRepo(t)
	if err := writeStateFile(merge.repo, flowStateFile, "release\n1.0\n1\nfalse\nfalse\n"); err != nil {
		t.Fatalf("writeStateFile returned error %v", err)
	}
	if err := os.MkdirAll(merge.repo.GitPath(mergeHeadFile)+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := FinishFlow(t.Context(), merge.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
		t.Fatal("FinishRelease resumed over an unreadable merge state")
	}
}

func TestFlowConfigNamesTheBranchOfEveryKind(t *testing.T) {
	cfg := mainFlowConfig()
	for kind, want := range map[string]FlowBranch{
		FlowKindFeature: {Prefix: "feature/", Base: "develop"},
		FlowKindRelease: {Prefix: "release/", Base: "develop", Tagged: true},
		FlowKindHotfix:  {Prefix: "hotfix/", Base: "main", Tagged: true},
	} {
		if got, err := cfg.Branch(kind); err != nil || got != want {
			t.Errorf("Branch(%s) = %+v, %v; want %+v", kind, got, err, want)
		}
	}
	if _, err := cfg.Branch("support"); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("Branch(support) returned %v, want %v", err, ErrFlowKind)
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
		{mainFlowConfig(), "hotfix/", "", "", false},
		{mainFlowConfig(), "develop", "", "", false},
		{noFeatures, "login", "", "", false},
	} {
		kind, name, ok := c.cfg.BranchKind(c.branch)
		if kind != c.kind || name != c.name || ok != c.ok {
			t.Errorf("BranchKind(%q) = %q, %q, %v", c.branch, kind, name, ok)
		}
	}
}

func TestFlowRefusesAnUnknownKind(t *testing.T) {
	r, _ := newFlowRepo(t)

	if _, err := StartFlow(t.Context(), r.repo, "support", "1.0", StartFlowOptions{}); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("StartFlow returned %v, want %v", err, ErrFlowKind)
	}
	if _, err := FinishFlow(t.Context(), r.repo, "support", "1.0", FinishFlowOptions{}); !errors.Is(err, ErrFlowKind) {
		t.Fatalf("FinishFlow returned %v, want %v", err, ErrFlowKind)
	}
}

func TestStartFlowBranchesFeaturesFromDevelopAndHotfixesFromMain(t *testing.T) {
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	develop := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	switchFlowBranch(t, r, "main")
	main := commitFlowFile(t, r, "h.txt", "main\n", "main work")

	for kind, want := range map[string]struct {
		branch string
		at     hash.ObjectID
	}{
		FlowKindFeature: {"feature/login", develop},
		FlowKindHotfix:  {"hotfix/1.0.1", main},
	} {
		name := strings.TrimPrefix(strings.TrimPrefix(want.branch, "feature/"), "hotfix/")
		got, err := StartFlow(t.Context(), r.repo, kind, name, StartFlowOptions{})
		if err != nil || got != refs.BranchName(want.branch) || r.branchTarget(want.branch) != want.at {
			t.Fatalf("StartFlow(%s) = %s, %v at %s; want %s at %s", kind, got, err, r.branchTarget(want.branch), want.branch, want.at)
		}
		if head, _ := r.headSymbolicTarget(); head != got {
			t.Fatalf("HEAD = %s, want %s", head, got)
		}
	}
}

func newFeatureRepo(t *testing.T, net FlowNetwork) *testRepo {
	t.Helper()
	r, _ := newFlowRepo(t)
	if net.enabled() {
		flowServer(t, r)
	}
	if _, err := StartFlow(t.Context(), r.repo, FlowKindFeature, "login", StartFlowOptions{Network: net}); err != nil {
		t.Fatalf("StartFlow returned error %v", err)
	}
	commitFlowFile(t, r, "login.txt", "login\n", "login")
	return r
}

func TestFinishFeatureMergesIntoDevelopOnlyAndDeletesTheBranch(t *testing.T) {
	r := newFeatureRepo(t, FlowNetwork{})
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
	commit, err := r.db().Commit(develop)
	if err != nil || commit.Message != "Finish login\n" || !commit.Committer.When.Equal(when) {
		t.Fatalf("develop merge = %+v, %v", commit, err)
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

func TestFinishFeatureUsesTheGivenMergeMessageAndPushesOnlyDevelop(t *testing.T) {
	r := newFeatureRepo(t, originFlow)
	mainBefore := r.branchTarget("main")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Message: "Add login", Push: true, Network: originFlow})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	commit, err := r.db().Commit(r.branchTarget("develop"))
	if err != nil || commit.Message != "Add login\n" {
		t.Fatalf("develop merge = %+v, %v", commit, err)
	}
	server, err := r.refs().Lookup(refs.RemoteBranchName("origin", "develop"))
	if err != nil || server.Target != r.branchTarget("develop") {
		t.Fatalf("origin/develop = %+v, %v", server, err)
	}
	if server, err := r.refs().Lookup(refs.RemoteBranchName("origin", "main")); err != nil || server.Target != mainBefore {
		t.Fatalf("origin/main = %+v, %v; want it untouched at %s", server, err, mainBefore)
	}
	if refMissing(t, r, refs.BranchName("feature/login")) {
		t.Fatal("the feature branch was deleted without being asked")
	}
}

func TestFinishFeatureStopsOnAConflictAndResumesAfterTheCommit(t *testing.T) {
	r, _ := newFlowRepo(t)
	if _, err := StartFlow(t.Context(), r.repo, FlowKindFeature, "login", StartFlowOptions{}); err != nil {
		t.Fatalf("StartFlow returned error %v", err)
	}
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

func TestFinishHotfixMergesIntoMainTagsAndMergesTheTagIntoDevelop(t *testing.T) {
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	if _, err := StartFlow(t.Context(), r.repo, FlowKindHotfix, "1.0.1", StartFlowOptions{}); err != nil {
		t.Fatalf("StartFlow returned error %v", err)
	}
	commitFlowFile(t, r, "fix.txt", "fix\n", "fix")
	developBefore := r.branchTarget("develop")
	mainBefore := r.branchTarget("main")
	hotfix := r.branchTarget("hotfix/1.0.1")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindHotfix, "1.0.1", FinishFlowOptions{Message: "Hotfix 1.0.1", DeleteBranch: true})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	mainTip := r.branchTarget("main")
	if got := flowParents(t, r, mainTip); !slices.Equal(got, []hash.ObjectID{mainBefore, hotfix}) {
		t.Fatalf("main merge parents = %v, want %v and %v", got, mainBefore, hotfix)
	}
	if result.Tag.Name != "1.0.1" || result.Tag.Target != mainTip {
		t.Fatalf("tag = %+v, want 1.0.1 on %s", result.Tag, mainTip)
	}
	if got := flowParents(t, r, r.branchTarget("develop")); !slices.Equal(got, []hash.ObjectID{developBefore, mainTip}) {
		t.Fatalf("develop merge parents = %v, want %v and %v", got, developBefore, mainTip)
	}
	if !refMissing(t, r, refs.BranchName("hotfix/1.0.1")) {
		t.Fatal("the hotfix branch is still there")
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
				t.Fatal("FinishRelease reported success")
			}
		})
	}
	r, _ := newFlowRepo(t)
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "9.9", FinishFlowOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("FinishRelease without the release branch returned %v", err)
	}
}
