package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/config"
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
	ErrFlowKind          = errors.New("ops: git-flow has no such branch kind here")
)

type FlowStep int

const (
	FlowStepMergeMaster FlowStep = iota + 1
	FlowStepTag
	FlowStepRebase
	FlowStepMergeDevelop
	FlowStepPush
	FlowStepDeleteBranch
)

type FlowIntegration int

const (
	FlowMergeCommit FlowIntegration = iota
	FlowSquash
	FlowRebase
)

const (
	flowStateFile    = "GOGIT_FLOW"
	FlowKindFeature  = "feature"
	FlowKindRelease  = "release"
	FlowKindHotfix   = "hotfix"
	FlowKindSupport  = "support"
	flowFinishPrefix = "Finish "
	flowStateFields  = 9
)

var flowConfigSection = "gitflow"

var FlowKinds = []string{FlowKindFeature, FlowKindRelease, FlowKindHotfix, FlowKindSupport}

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
	Remote           string
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
		Remote:        defaultCloneRemoteName,
	}
}

func DefaultLightFlowConfig() FlowConfig {
	return FlowConfig{Develop: "master", FeaturePrefix: "feature/", Remote: defaultCloneRemoteName}
}

func (c FlowConfig) Light() bool { return c.Master == "" }

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
		{key("origin"), &c.Remote},
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
	configured := cfg.Has(entries[1].key)
	if configured && !cfg.Has(entries[0].key) {
		c.Master = ""
	}
	return c, configured
}

func WriteFlowConfig(r *repo.Repository, c FlowConfig) error {
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	light := c.Light()
	for _, e := range c.entries() {
		if e.value == &c.Master && light {
			if err := unsetFlowKey(file, e.key); err != nil {
				return err
			}
			continue
		}
		if err := file.Set(e.key, *e.value); err != nil {
			return err
		}
	}
	return file.Save(file.Path())
}

func SwitchOffFlow(r *repo.Repository) error {
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	var none FlowConfig
	for _, e := range none.entries() {
		if err := unsetFlowKey(file, e.key); err != nil {
			return err
		}
	}
	return file.Save(file.Path())
}

func unsetFlowKey(file *config.File, key string) error {
	if err := file.UnsetAll(key); err != nil && !errors.Is(err, config.ErrNotFound) {
		return err
	}
	return nil
}

func ConfigureFlow(ctx context.Context, r *repo.Repository, c FlowConfig) ([]refs.Name, error) {
	branches := []string{c.Master, c.Develop}
	if c.Light() {
		branches = branches[1:]
	}
	var created []refs.Name
	for _, branch := range branches {
		name := refs.BranchName(branch)
		exists, err := flowRefExists(r, name)
		if err != nil {
			return created, err
		}
		if exists {
			continue
		}
		head, _, err := startPointOf(r, "")
		if err != nil {
			return created, err
		}
		if err := CreateBranch(ctx, r, branch, head, CreateBranchOptions{}); err != nil {
			return created, err
		}
		created = append(created, name)
	}
	return created, WriteFlowConfig(r, c)
}

type FlowBranch struct {
	Prefix string
	Base   string
	Tagged bool
}

func (c FlowConfig) Branch(kind string) (FlowBranch, error) {
	full := !c.Light()
	switch {
	case kind == FlowKindFeature:
		return FlowBranch{Prefix: c.FeaturePrefix, Base: c.Develop}, nil
	case kind == FlowKindRelease && full:
		return FlowBranch{Prefix: c.ReleasePrefix, Base: c.Develop, Tagged: true}, nil
	case kind == FlowKindHotfix && full:
		return FlowBranch{Prefix: c.HotfixPrefix, Base: c.Master, Tagged: true}, nil
	case kind == FlowKindSupport && full:
		return FlowBranch{Prefix: c.SupportPrefix, Base: c.Master}, nil
	}
	return FlowBranch{}, fmt.Errorf("%w: %s", ErrFlowKind, kind)
}

func (c FlowConfig) BranchKind(branch string) (string, string, bool) {
	for _, kind := range FlowKinds {
		b, err := c.Branch(kind)
		if err != nil {
			continue
		}
		if name, cut := strings.CutPrefix(branch, b.Prefix); cut && b.Prefix != "" && name != "" {
			return kind, name, true
		}
	}
	return "", "", false
}

type FlowNetwork struct {
	Remote    string
	Progress  progress.Func
	Transport transport.Options
}

func (n FlowNetwork) enabled() bool { return n.Remote != "" }

func HasRemoteBranch(r *repo.Repository, remoteName, branch string) (bool, error) {
	return flowRefExists(r, refs.RemoteBranchName(remoteName, branch))
}

type StartFlowOptions struct {
	Base    string
	Network FlowNetwork
}

func StartFlow(ctx context.Context, r *repo.Repository, kind, name string, opts StartFlowOptions) (refs.Name, error) {
	_, branch, err := configuredFlow(r, kind, name)
	if err != nil {
		return "", err
	}
	base := cmp.Or(opts.Base, branch.Base)
	start, err := flowStartPoint(r, base)
	if err != nil {
		return "", err
	}
	if start.IsBranch() {
		if err := flowFetchTracked(ctx, r, opts.Network, base); err != nil {
			return "", err
		}
	}
	full := branch.Prefix + name
	if _, err := StartBranch(ctx, r, full, start.String(), StartBranchOptions{}); err != nil {
		return "", err
	}
	return refs.BranchName(full), nil
}

func flowStartPoint(r *repo.Repository, base string) (refs.Name, error) {
	for _, candidate := range []refs.Name{refs.BranchName(base), refs.TagName(base)} {
		exists, err := flowRefExists(r, candidate)
		if err != nil || exists {
			return candidate, err
		}
	}
	return refs.Name(base), nil
}

type IntegrateDevelopOptions struct {
	Rebase bool
	When   time.Time
}

func IntegrateDevelop(ctx context.Context, r *repo.Repository, name string, opts IntegrateDevelopOptions) ([]string, error) {
	_, branch, err := configuredFlow(r, FlowKindFeature, name)
	if err != nil {
		return nil, err
	}
	if err := Switch(ctx, r, branch.Prefix+name, SwitchOptions{}); err != nil {
		return nil, err
	}
	if opts.Rebase {
		result, err := Rebase(ctx, r, branch.Base, RebaseOptions{When: opts.When})
		return result.Conflicts, err
	}
	result, err := Merge(ctx, r, branch.Base, MergeOptions{When: opts.When})
	return result.Conflicts, err
}

type FinishFlowOptions struct {
	Message      string
	Integration  FlowIntegration
	TagName      string
	SkipTag      bool
	SkipDevelop  bool
	Fetch        bool
	Push         bool
	DeleteBranch bool
	When         time.Time
	Network      FlowNetwork
}

type FinishFlowResult struct {
	Tag       TagResult
	Pushed    bool
	Conflicts []string
	Stopped   FlowStep
}

func (r FinishFlowResult) Finished() bool { return r.Stopped == 0 }

type FlowFinish struct {
	Kind string
	Name string
	Step FlowStep
}

func PendingFlowFinish(r *repo.Repository) (FlowFinish, bool, error) {
	state, found, err := readFlowState(r)
	return FlowFinish{Kind: state.kind, Name: state.name, Step: state.step}, found, err
}

func FinishFlow(ctx context.Context, r *repo.Repository, kind, name string, opts FinishFlowOptions) (FinishFlowResult, error) {
	cfg, branch, err := configuredFlow(r, kind, name)
	if err != nil {
		return FinishFlowResult{}, err
	}
	if kind == FlowKindSupport {
		return FinishFlowResult{}, fmt.Errorf("%w: %s cannot be finished", ErrFlowKind, kind)
	}
	state, resumed, err := resumeFlow(r, kind, name)
	if err != nil {
		return FinishFlowResult{}, err
	}
	if !resumed {
		state = newFlowState(cfg, branch, kind, name, opts)
	}
	f := &flowFinisher{ctx: ctx, r: r, cfg: cfg, net: opts.Network, when: opts.When, state: state, branch: branch.Prefix + name}
	if f.push, err = refspec.ParseAll(f.pushTexts()); err != nil {
		return FinishFlowResult{}, err
	}
	if !resumed && opts.Fetch && opts.Network.enabled() {
		if err := f.fetchTargets(f.targets()); err != nil {
			return FinishFlowResult{}, err
		}
	}
	return f.run()
}

func PushFinishedFlow(ctx context.Context, r *repo.Repository, kind, name string, opts FinishFlowOptions) error {
	cfg, branch, err := configuredFlow(r, kind, name)
	if err != nil {
		return err
	}
	opts.Push = true
	f := &flowFinisher{ctx: ctx, r: r, cfg: cfg, net: opts.Network, state: newFlowState(cfg, branch, kind, name, opts), branch: branch.Prefix + name}
	if f.push, err = refspec.ParseAll(f.pushTexts()); err != nil {
		return err
	}
	return f.pushResults()
}

func newFlowState(cfg FlowConfig, branch FlowBranch, kind, name string, opts FinishFlowOptions) flowState {
	state := flowState{kind: kind, name: name, step: FlowStepMergeMaster, push: opts.Push, deleteBranch: opts.DeleteBranch, develop: true, message: opts.Message}
	if !branch.Tagged {
		state.integration = opts.Integration
		return state
	}
	if !opts.SkipTag {
		state.tag = cmp.Or(opts.TagName, cfg.VersionTagPrefix+name)
	}
	state.develop = kind != FlowKindHotfix || !opts.SkipDevelop
	return state
}

type flowFinisher struct {
	ctx    context.Context
	r      *repo.Repository
	cfg    FlowConfig
	net    FlowNetwork
	when   time.Time
	state  flowState
	branch string
	push   []refspec.RefSpec
	result FinishFlowResult
}

func (f *flowFinisher) tagged() bool { return f.state.kind != FlowKindFeature }

func (f *flowFinisher) targets() []string {
	if f.tagged() {
		return []string{f.cfg.Master, f.cfg.Develop}
	}
	return []string{f.cfg.Develop}
}

func (f *flowFinisher) pushTexts() []string {
	var names []refs.Name
	if f.tagged() {
		if f.state.develop {
			names = append(names, refs.BranchName(f.cfg.Develop))
		}
		names = append(names, refs.BranchName(f.cfg.Master))
		if f.state.tag != "" {
			names = append(names, refs.TagName(f.state.tag))
		}
	}
	texts := make([]string, 0, len(names)+1)
	for _, name := range names {
		texts = append(texts, name.String()+":"+name.String())
	}
	return append(texts, ":"+refs.BranchName(f.branch).String())
}

func (f *flowFinisher) fetchTargets(targets []string) error {
	if err := flowFetchTracked(f.ctx, f.r, f.net, targets...); err != nil {
		return err
	}
	for _, target := range targets {
		if err := flowNotBehind(f.r, f.net, target); err != nil {
			return err
		}
	}
	return nil
}

func (f *flowFinisher) run() (FinishFlowResult, error) {
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

func DefaultFlowMessage(name string) string { return flowFinishPrefix + name }

func (f *flowFinisher) message() string {
	return cmp.Or(f.state.message, DefaultFlowMessage(f.state.name))
}

func (f *flowFinisher) do(step FlowStep) ([]string, error) {
	switch step {
	case FlowStepMergeMaster:
		if !f.tagged() {
			return nil, nil
		}
		return flowMerge(f.ctx, f.r, f.cfg.Master, f.branch, f.message(), MergeNoFastForward, f.when)
	case FlowStepTag:
		if f.state.tag == "" {
			return nil, nil
		}
		var err error
		f.result.Tag, err = CreateTag(f.ctx, f.r, f.state.tag, refs.BranchName(f.cfg.Master).String(), CreateTagOptions{
			Message: f.message(),
			Force:   true,
			When:    f.when,
		})
		return nil, err
	case FlowStepRebase:
		if f.state.integration != FlowRebase {
			return nil, nil
		}
		if err := Switch(f.ctx, f.r, f.branch, SwitchOptions{}); err != nil {
			return nil, err
		}
		result, err := Rebase(f.ctx, f.r, f.cfg.Develop, RebaseOptions{When: f.when})
		return result.Conflicts, err
	case FlowStepMergeDevelop:
		if !f.state.develop {
			return nil, nil
		}
		return f.mergeDevelop()
	case FlowStepPush:
		return nil, f.pushResults()
	default:
		if !f.state.deleteBranch {
			return nil, nil
		}
		return nil, flowDeleteBranch(f.ctx, f.r, f.branch, true)
	}
}

func (f *flowFinisher) mergeDevelop() ([]string, error) {
	source := f.branch
	if f.state.tag != "" {
		source = f.state.tag
	}
	switch {
	case f.state.kind == FlowKindHotfix:
		return flowMerge(f.ctx, f.r, f.cfg.Develop, source, "", MergeNoFastForward, f.when)
	case f.state.integration == FlowRebase:
		return flowMerge(f.ctx, f.r, f.cfg.Develop, source, "", MergeFastForwardOnly, f.when)
	case f.state.integration == FlowSquash:
		conflicts, err := flowMerge(f.ctx, f.r, f.cfg.Develop, source, f.message(), MergeSquash, f.when)
		if err != nil || len(conflicts) > 0 {
			return conflicts, err
		}
		_, err = Commit(f.ctx, f.r, CommitOptions{Message: f.message(), When: f.when})
		return nil, err
	}
	return flowMerge(f.ctx, f.r, f.cfg.Develop, source, f.message(), MergeNoFastForward, f.when)
}

func (f *flowFinisher) pushResults() error {
	if !f.state.push || !f.net.enabled() {
		return nil
	}
	f.result.Pushed = true
	specs := f.push
	tracked, err := HasRemoteBranch(f.r, f.net.Remote, f.branch)
	if err != nil {
		return err
	}
	if !tracked {
		specs = specs[:len(specs)-1]
	}
	if len(specs) == 0 {
		return nil
	}
	_, err = flowPush(f.ctx, f.r, f.net.Remote, remote.PushOptions{Refspecs: specs, Progress: f.net.Progress, Transport: f.net.Transport})
	return err
}

func configuredFlow(r *repo.Repository, kind, name string) (FlowConfig, FlowBranch, error) {
	cfg, ok := ReadFlowConfig(r)
	switch {
	case !ok:
		return cfg, FlowBranch{}, ErrFlowNotConfigured
	case strings.TrimSpace(name) == "":
		return cfg, FlowBranch{}, ErrFlowEmptyName
	}
	branch, err := cfg.Branch(kind)
	return cfg, branch, err
}

func flowRefExists(r *repo.Repository, name refs.Name) (bool, error) {
	rc, err := openRepoContext(r)
	if err != nil {
		return false, err
	}
	defer func() { _ = rc.close() }()
	_, err = refsLookup(rc.refs, name)
	if errors.Is(err, refs.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func flowFetchSpecs(net FlowNetwork, branches ...string) []refspec.RefSpec {
	specs := make([]refspec.RefSpec, 0, len(branches))
	for _, branch := range branches {
		specs = append(specs, refspec.RefSpec{Src: refs.BranchName(branch).String(), Dst: refs.RemoteBranchName(net.Remote, branch).String()})
	}
	return specs
}

func flowFetchTracked(ctx context.Context, r *repo.Repository, net FlowNetwork, branches ...string) error {
	if !net.enabled() {
		return nil
	}
	var tracked []string
	for _, branch := range branches {
		exists, err := HasRemoteBranch(r, net.Remote, branch)
		if err != nil {
			return err
		}
		if exists {
			tracked = append(tracked, branch)
		}
	}
	if len(tracked) == 0 {
		return nil
	}
	_, err := Fetch(ctx, r, net.Remote, remote.FetchOptions{Refspecs: flowFetchSpecs(net, tracked...), Prune: true, Force: true, Progress: net.Progress, Transport: net.Transport})
	return err
}

func flowNotBehind(r *repo.Repository, net FlowNetwork, branch string) error {
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

func flowMerge(ctx context.Context, r *repo.Repository, onto, source, message string, mode MergeMode, when time.Time) ([]string, error) {
	if err := Switch(ctx, r, onto, SwitchOptions{}); err != nil {
		return nil, err
	}
	result, err := Merge(ctx, r, source, MergeOptions{Mode: mode, Message: message, When: when})
	return result.Conflicts, err
}

type flowState struct {
	kind         string
	name         string
	step         FlowStep
	push         bool
	deleteBranch bool
	tag          string
	develop      bool
	integration  FlowIntegration
	message      string
}

func (s flowState) encode() string {
	return strings.Join([]string{
		s.kind,
		s.name,
		strconv.Itoa(int(s.step)),
		strconv.FormatBool(s.push),
		strconv.FormatBool(s.deleteBranch),
		s.tag,
		strconv.FormatBool(s.develop),
		strconv.Itoa(int(s.integration)),
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
	develop, developErr := strconv.ParseBool(fields[6])
	integration, integrationErr := strconv.Atoi(fields[7])
	if err := errors.Join(stepErr, pushErr, deleteErr, developErr, integrationErr); err != nil {
		return flowState{}, false, fmt.Errorf("%w: %w", ErrFlowState, err)
	}
	return flowState{
		kind:         fields[0],
		name:         fields[1],
		step:         FlowStep(step),
		push:         push,
		deleteBranch: deleteBranch,
		tag:          fields[5],
		develop:      develop,
		integration:  FlowIntegration(integration),
		message:      fields[8],
	}, true, nil
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
