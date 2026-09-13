package ops

import (
	"errors"
	"os"
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

func startFlowRelease(t *testing.T, r *testRepo, version string, net FlowNetwork) {
	t.Helper()
	if _, err := StartRelease(t.Context(), r.repo, version, StartReleaseOptions{Network: net}); err != nil {
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
	if _, err := StartRelease(t.Context(), bare.repo, "1.0", StartReleaseOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("StartRelease without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := StartRelease(t.Context(), r.repo, "  ", StartReleaseOptions{}); !errors.Is(err, ErrFlowEmptyName) {
		t.Fatalf("StartRelease without a name returned %v", err)
	}
}

func TestStartReleaseBranchesFromDevelopWithoutTrackingAndSwitches(t *testing.T) {
	r, _ := newFlowRepo(t)
	switchFlowBranch(t, r, "develop")
	tip := commitFlowFile(t, r, "g.txt", "develop\n", "develop work")
	switchFlowBranch(t, r, "main")

	name, err := StartRelease(t.Context(), r.repo, "1.0", StartReleaseOptions{})
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

	if _, err := StartRelease(t.Context(), r.repo, "1.0", StartReleaseOptions{}); err == nil {
		t.Fatal("StartRelease accepted a develop name that is no refspec")
	}
	if !refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatal("the release branch was created anyway")
	}
}

func TestStartReleaseReportsTheFailingStep(t *testing.T) {
	missingRemote, _ := newFlowRepo(t)
	if _, err := StartRelease(t.Context(), missingRemote.repo, "1.0", StartReleaseOptions{Network: FlowNetwork{Remote: "nowhere"}}); err == nil {
		t.Fatal("StartRelease fetched from a remote that does not exist")
	}

	taken, base := newFlowRepo(t)
	taken.createBranch("release/1.0", base)
	if _, err := StartRelease(t.Context(), taken.repo, "1.0", StartReleaseOptions{}); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("StartRelease over an existing branch returned %v", err)
	}

	dirty, _ := newFlowRepo(t)
	switchFlowBranch(t, dirty, "develop")
	commitFlowFile(t, dirty, "a.txt", "develop\n", "develop work")
	switchFlowBranch(t, dirty, "main")
	dirty.writeFile("a.txt", "local\n")
	if _, err := StartRelease(t.Context(), dirty.repo, "1.0", StartReleaseOptions{}); !errors.Is(err, ErrWouldOverwrite) {
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
	if _, err := FinishRelease(t.Context(), bare.repo, "1.0", FinishReleaseOptions{}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("FinishRelease without git-flow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if _, err := FinishRelease(t.Context(), r.repo, "", FinishReleaseOptions{}); !errors.Is(err, ErrFlowEmptyName) {
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

		if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{}); err == nil {
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

	result, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{TagMessage: "Release 1.0", Push: true, DeleteBranch: true, When: when})
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

	result, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{})
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

	result, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{Push: true, DeleteBranch: true, Network: originFlow})
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

	_, err = FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{Network: originFlow})

	if !errors.Is(err, ErrFlowBehind) {
		t.Fatalf("FinishRelease returned %v, want %v", err, ErrFlowBehind)
	}
}

func TestFinishReleaseReportsAFailedFetch(t *testing.T) {
	r := newReleaseRepo(t)

	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{Network: FlowNetwork{Remote: "nowhere"}}); err == nil {
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

	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{Network: originFlow}); !errors.Is(err, ErrBranchNotFound) {
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

	stopped, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{DeleteBranch: true})
	if err != nil || stopped.Finished() || stopped.Stopped != FlowStepMergeMaster || !slices.Equal(stopped.Conflicts, []string{"a.txt"}) {
		t.Fatalf("FinishRelease = %+v, %v; want a stop on a.txt while merging into main", stopped, err)
	}
	pending, found, err := PendingFlowFinish(r.repo)
	if err != nil || !found || pending != (FlowFinish{Kind: "release", Name: "1.0", Step: FlowStepMergeMaster}) {
		t.Fatalf("PendingFlowFinish = %+v, %v, %v", pending, found, err)
	}
	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("FinishRelease before the commit returned %v", err)
	}

	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "Finish 1.0"}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	resumed, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{})

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

	result, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{})

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

		if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{}); !errors.Is(err, want) {
			t.Fatalf("state %q: FinishRelease returned %v, want %v", text, err, want)
		}
	}
}

func TestFinishReleaseReportsUnreadableState(t *testing.T) {
	flow := newReleaseRepo(t)
	if err := os.MkdirAll(flow.repo.GitPath(flowStateFile)+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := FinishRelease(t.Context(), flow.repo, "1.0", FinishReleaseOptions{}); err == nil {
		t.Fatal("FinishRelease read a directory as its state")
	}

	merge := newReleaseRepo(t)
	if err := writeStateFile(merge.repo, flowStateFile, "release\n1.0\n1\nfalse\nfalse\n"); err != nil {
		t.Fatalf("writeStateFile returned error %v", err)
	}
	if err := os.MkdirAll(merge.repo.GitPath(mergeHeadFile)+"/blocked", 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := FinishRelease(t.Context(), merge.repo, "1.0", FinishReleaseOptions{}); err == nil {
		t.Fatal("FinishRelease resumed over an unreadable merge state")
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

			if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{}); err == nil {
				t.Fatal("FinishRelease reported success")
			}
		})
	}
	r, _ := newFlowRepo(t)
	if _, err := FinishRelease(t.Context(), r.repo, "9.9", FinishReleaseOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("FinishRelease without the release branch returned %v", err)
	}
}
