package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

var (
	ErrFlowNotConfigured = errors.New("ops: git-flow is not configured")
	ErrFlowEmptyName     = errors.New("ops: git-flow name is empty")
	ErrFlowBehind        = errors.New("ops: branch is behind its remote")
	ErrFlowPending       = errors.New("ops: another git-flow finish is waiting")
	ErrFlowState         = errors.New("ops: git-flow state is damaged")
)

type FlowStep int

const (
	FlowStepMergeMaster FlowStep = iota + 1
	FlowStepTag
	FlowStepMergeDevelop
	FlowStepPush
	FlowStepDeleteBranch
)

const (
	flowStateFile    = "GOGIT_FLOW"
	FlowKindRelease  = "release"
	flowFinishPrefix = "Finish "
	flowStateFields  = 6
)

var flowConfigSection = "gitflow"

var (
	flowPush         = Push
	flowDeleteBranch = DeleteBranch
)

type FlowConfig struct {
	Master           string
	Develop          string
	FeaturePrefix    string
	ReleasePrefix    string
	HotfixPrefix     string
	SupportPrefix    string
	VersionTagPrefix string
}

type flowEntry struct {
	key   string
	value *string
}

func DefaultFlowConfig() FlowConfig {
	return FlowConfig{
		Master:        "master",
		Develop:       "develop",
		FeaturePrefix: "feature/",
		ReleasePrefix: "release/",
		HotfixPrefix:  "hotfix/",
		SupportPrefix: "support/",
	}
}

func (c *FlowConfig) entries() []flowEntry {
	key := func(name string) string { return flowConfigSection + "." + name }
	return []flowEntry{
		{key("branch.master"), &c.Master},
		{key("branch.develop"), &c.Develop},
		{key("prefix.feature"), &c.FeaturePrefix},
		{key("prefix.release"), &c.ReleasePrefix},
		{key("prefix.hotfix"), &c.HotfixPrefix},
		{key("prefix.support"), &c.SupportPrefix},
		{key("prefix.versiontag"), &c.VersionTagPrefix},
	}
}

func ReadFlowConfig(r *repo.Repository) (FlowConfig, bool) {
	c := DefaultFlowConfig()
	cfg := r.Config()
	entries := c.entries()
	for _, e := range entries {
		if value, ok := cfg.Get(e.key); ok {
			*e.value = value
		}
	}
	return c, cfg.Has(entries[0].key) && cfg.Has(entries[1].key)
}

func WriteFlowConfig(r *repo.Repository, c FlowConfig) error {
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	for _, e := range c.entries() {
		if err := file.Set(e.key, *e.value); err != nil {
			return err
		}
	}
	return file.Save(file.Path())
}

type FlowNetwork struct {
	Remote    string
	Progress  progress.Func
	Transport transport.Options
}

func (n FlowNetwork) enabled() bool { return n.Remote != "" }

type StartReleaseOptions struct {
	Network FlowNetwork
}

func StartRelease(ctx context.Context, r *repo.Repository, version string, opts StartReleaseOptions) (refs.Name, error) {
	cfg, err := configuredFlow(r, version)
	if err != nil {
		return "", err
	}
	fetch, err := flowFetchSpecs(opts.Network, cfg.Develop)
	if err != nil {
		return "", err
	}
	if err := flowFetch(ctx, r, opts.Network, fetch); err != nil {
		return "", err
	}
	name := cfg.ReleasePrefix + version
	if _, err := StartBranch(ctx, r, name, refs.BranchName(cfg.Develop).String(), StartBranchOptions{}); err != nil {
		return "", err
	}
	return refs.BranchName(name), nil
}

type FinishReleaseOptions struct {
	TagMessage   string
	Push         bool
	DeleteBranch bool
	When         time.Time
	Network      FlowNetwork
}

type FinishReleaseResult struct {
	Tag       TagResult
	Conflicts []string
	Stopped   FlowStep
}

func (r FinishReleaseResult) Finished() bool { return r.Stopped == 0 }

type FlowFinish struct {
	Kind string
	Name string
	Step FlowStep
}

func PendingFlowFinish(r *repo.Repository) (FlowFinish, bool, error) {
	state, found, err := readFlowState(r)
	return FlowFinish{Kind: state.kind, Name: state.name, Step: state.step}, found, err
}

func FinishRelease(ctx context.Context, r *repo.Repository, version string, opts FinishReleaseOptions) (FinishReleaseResult, error) {
	cfg, err := configuredFlow(r, version)
	if err != nil {
		return FinishReleaseResult{}, err
	}
	tag := cfg.VersionTagPrefix + version
	fetch, err := flowFetchSpecs(opts.Network, cfg.Master, cfg.Develop)
	if err != nil {
		return FinishReleaseResult{}, err
	}
	push, err := refspec.ParseAll(flowPushTexts(refs.BranchName(cfg.Develop), refs.BranchName(cfg.Master), refs.TagName(tag)))
	if err != nil {
		return FinishReleaseResult{}, err
	}
	state, resumed, err := resumeFlow(r, FlowKindRelease, version)
	if err != nil {
		return FinishReleaseResult{}, err
	}
	if !resumed {
		state = flowState{kind: FlowKindRelease, name: version, step: FlowStepMergeMaster, push: opts.Push, deleteBranch: opts.DeleteBranch, message: opts.TagMessage}
		if err := flowFetch(ctx, r, opts.Network, fetch); err != nil {
			return FinishReleaseResult{}, err
		}
		for _, branch := range []string{cfg.Master, cfg.Develop} {
			if err := flowNotBehind(r, opts.Network, branch); err != nil {
				return FinishReleaseResult{}, err
			}
		}
	}
	f := &releaseFinish{ctx: ctx, r: r, cfg: cfg, net: opts.Network, when: opts.When, state: state, tag: tag, push: push}
	return f.run()
}

type releaseFinish struct {
	ctx    context.Context
	r      *repo.Repository
	cfg    FlowConfig
	net    FlowNetwork
	when   time.Time
	state  flowState
	tag    string
	push   []refspec.RefSpec
	result FinishReleaseResult
}

func (f *releaseFinish) run() (FinishReleaseResult, error) {
	for ; f.state.step <= FlowStepDeleteBranch; f.state.step++ {
		conflicts, err := f.do(f.state.step)
		if err != nil {
			return f.result, err
		}
		if len(conflicts) > 0 {
			f.result.Conflicts, f.result.Stopped = conflicts, f.state.step
			return f.result, writeStateFile(f.r, flowStateFile, f.state.encode())
		}
	}
	return f.result, removeStateFiles(f.r, flowStateFile)
}

func (f *releaseFinish) do(step FlowStep) ([]string, error) {
	release := f.cfg.ReleasePrefix + f.state.name
	message := flowFinishPrefix + f.state.name
	switch step {
	case FlowStepMergeMaster:
		return flowMerge(f.ctx, f.r, f.cfg.Master, release, message, f.when)
	case FlowStepTag:
		var err error
		f.result.Tag, err = CreateTag(f.ctx, f.r, f.tag, refs.BranchName(f.cfg.Master).String(), CreateTagOptions{
			Message: cmp.Or(f.state.message, message),
			Force:   true,
			When:    f.when,
		})
		return nil, err
	case FlowStepMergeDevelop:
		return flowMerge(f.ctx, f.r, f.cfg.Develop, f.tag, message, f.when)
	case FlowStepPush:
		if !f.state.push || !f.net.enabled() {
			return nil, nil
		}
		_, err := flowPush(f.ctx, f.r, f.net.Remote, remote.PushOptions{Refspecs: f.push, Progress: f.net.Progress, Transport: f.net.Transport})
		return nil, err
	default:
		if !f.state.deleteBranch {
			return nil, nil
		}
		return nil, flowDeleteBranch(f.ctx, f.r, release, true)
	}
}

func configuredFlow(r *repo.Repository, name string) (FlowConfig, error) {
	cfg, ok := ReadFlowConfig(r)
	switch {
	case !ok:
		return cfg, ErrFlowNotConfigured
	case strings.TrimSpace(name) == "":
		return cfg, ErrFlowEmptyName
	}
	return cfg, nil
}

func flowFetchSpecs(net FlowNetwork, branches ...string) ([]refspec.RefSpec, error) {
	texts := make([]string, 0, len(branches))
	for _, branch := range branches {
		texts = append(texts, refs.BranchName(branch).String()+":"+refs.RemoteBranchName(cmp.Or(net.Remote, defaultCloneRemoteName), branch).String())
	}
	return refspec.ParseAll(texts)
}

func flowPushTexts(names ...refs.Name) []string {
	texts := make([]string, 0, len(names))
	for _, name := range names {
		texts = append(texts, name.String()+":"+name.String())
	}
	return texts
}

func flowFetch(ctx context.Context, r *repo.Repository, net FlowNetwork, specs []refspec.RefSpec) error {
	if !net.enabled() {
		return nil
	}
	_, err := Fetch(ctx, r, net.Remote, remote.FetchOptions{Refspecs: specs, Prune: true, Force: true, Progress: net.Progress, Transport: net.Transport})
	return err
}

func flowNotBehind(r *repo.Repository, net FlowNetwork, branch string) error {
	if !net.enabled() {
		return nil
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	local, err := refsLookup(rc.refs, refs.BranchName(branch))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrBranchNotFound, branch, err)
	}
	tracking, err := refsLookup(rc.refs, refs.RemoteBranchName(net.Remote, branch))
	if errors.Is(err, refs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	contained, err := revision.IsAncestor(revision.Context{Objects: rc.db}, tracking.Target, local.Target)
	if err != nil {
		return err
	}
	if !contained {
		return fmt.Errorf("%w: %s", ErrFlowBehind, branch)
	}
	return nil
}

func flowMerge(ctx context.Context, r *repo.Repository, onto, source, message string, when time.Time) ([]string, error) {
	if err := Switch(ctx, r, onto, SwitchOptions{}); err != nil {
		return nil, err
	}
	result, err := Merge(ctx, r, source, MergeOptions{Mode: MergeNoFastForward, Message: message, When: when})
	return result.Conflicts, err
}

type flowState struct {
	kind         string
	name         string
	step         FlowStep
	push         bool
	deleteBranch bool
	message      string
}

func (s flowState) encode() string {
	return strings.Join([]string{
		s.kind,
		s.name,
		strconv.Itoa(int(s.step)),
		strconv.FormatBool(s.push),
		strconv.FormatBool(s.deleteBranch),
		s.message,
	}, "\n")
}

func readFlowState(r *repo.Repository) (flowState, bool, error) {
	text, err := readStateFile(r, flowStateFile)
	if err != nil || text == "" {
		return flowState{}, false, err
	}
	fields := strings.SplitN(text, "\n", flowStateFields)
	if len(fields) != flowStateFields {
		return flowState{}, false, ErrFlowState
	}
	step, stepErr := strconv.Atoi(fields[2])
	push, pushErr := strconv.ParseBool(fields[3])
	deleteBranch, deleteErr := strconv.ParseBool(fields[4])
	if err := errors.Join(stepErr, pushErr, deleteErr); err != nil {
		return flowState{}, false, fmt.Errorf("%w: %w", ErrFlowState, err)
	}
	return flowState{kind: fields[0], name: fields[1], step: FlowStep(step), push: push, deleteBranch: deleteBranch, message: fields[5]}, true, nil
}

func resumeFlow(r *repo.Repository, kind, name string) (flowState, bool, error) {
	state, found, err := readFlowState(r)
	if err != nil || !found {
		return state, false, err
	}
	if state.kind != kind || state.name != name {
		return state, false, fmt.Errorf("%w: %s %s", ErrFlowPending, state.kind, state.name)
	}
	merge, err := ReadMergeState(r)
	if err != nil {
		return state, false, err
	}
	if merge.InProgress() {
		return state, false, ErrMergeInProgress
	}
	state.step++
	return state, true, nil
}
