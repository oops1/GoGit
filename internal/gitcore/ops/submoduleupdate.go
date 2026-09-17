package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/submodule"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

type SubmoduleUpdateMode int

const (
	SubmoduleUpdateConfigured SubmoduleUpdateMode = iota
	SubmoduleUpdateCheckout
	SubmoduleUpdateRebase
	SubmoduleUpdateMerge
	SubmoduleUpdateNone
)

type SubmoduleUpdateOptions struct {
	Init             bool
	RequireInit      bool
	Recursive        bool
	Mode             SubmoduleUpdateMode
	Depth            int
	Remote           bool
	NoFetch          bool
	Force            bool
	RecommendShallow bool
	Progress         progress.Func
	Transport        transport.Options
	Events           SubmoduleEvents

	noSingleBranch bool
}

const gitFileMode = 0o666

var (
	validateSubmodulePath = submodule.ValidatePath
	reopenSubmodule       = reopenRepository
	readSubmoduleDir      = os.ReadDir
	errNotPopulated       = errors.New("ops: the submodule directory holds no repository")
)

type updateRecord struct {
	link        gitlinkEntry
	justCloned  bool
	displayPath string
}

func SubmoduleUpdate(ctx context.Context, r *repo.Repository, paths []string, opts SubmoduleUpdateOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	opts.Init = opts.Init || opts.RequireInit
	super, err := loadSuperproject(r, paths)
	if err != nil {
		return err
	}
	return super.update(ctx, len(paths) > 0, opts)
}

func (s *superproject) update(ctx context.Context, explicitPaths bool, opts SubmoduleUpdateOptions) error {
	if opts.Init {
		if err := s.init(ctx, !explicitPaths, opts.Events); err != nil {
			return err
		}
		fresh, err := reopenSubmodule(s.repo)
		if err != nil {
			return err
		}
		defer func() { _ = fresh.Close() }()
		s.repo, s.cfg = fresh, fresh.Config()
	}
	var records []updateRecord
	for _, link := range s.links {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, ok, err := s.prepareUpdate(ctx, link, explicitPaths, opts)
		if err != nil {
			return err
		}
		if ok {
			records = append(records, record)
		}
	}
	var failures []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := s.updateOne(ctx, record, opts)
		if errors.Is(err, ErrSubmoduleCheckout) || errors.Is(err, ErrSubmoduleCommand) {
			failures = append(failures, err)
			continue
		}
		if err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

func (s *superproject) skipsUpdate(link gitlinkEntry, opts SubmoduleUpdateOptions) bool {
	switch opts.Mode {
	case SubmoduleUpdateNone:
		return true
	case SubmoduleUpdateConfigured:
		return submodule.SkipsUpdate(s.cfg, link.module)
	}
	return false
}

func (s *superproject) prepareUpdate(ctx context.Context, link gitlinkEntry, explicitPaths bool, opts SubmoduleUpdateOptions) (updateRecord, bool, error) {
	path := s.displayPath(link.path)
	notInitialized := func() (updateRecord, bool, error) {
		if explicitPaths {
			opts.Events.emit(SubmoduleEvent{Kind: SubmoduleNotInitialized, Path: path, Name: link.module.Name})
		}
		return updateRecord{}, false, nil
	}
	switch {
	case link.stage != 0:
		opts.Events.emit(SubmoduleEvent{Kind: SubmoduleSkippedUnmerged, Path: path})
		return updateRecord{}, false, nil
	case !link.known:
		return notInitialized()
	case s.skipsUpdate(link, opts):
		opts.Events.emit(SubmoduleEvent{Kind: SubmoduleSkipped, Path: path, Name: link.module.Name})
		return updateRecord{}, false, nil
	}
	active, err := s.active(link)
	if err != nil {
		return updateRecord{}, false, err
	}
	if !active {
		return notInitialized()
	}
	url, err := s.cloneURL(link.module)
	if err != nil {
		return updateRecord{}, false, err
	}
	dir := s.submoduleDir(link.path)
	_, statErr := os.Stat(filepath.Join(dir, gitDirName))
	record := updateRecord{link: link, justCloned: statErr != nil, displayPath: path}
	if !record.justCloned {
		return record, true, nil
	}
	return record, true, s.cloneSubmodule(ctx, record, url, opts)
}

func (s *superproject) cloneURL(module submodule.Module) (string, error) {
	url, ok := s.cfg.GetValue("submodule." + module.Name + ".url")
	var err error
	if !ok {
		url, err = s.resolveURL(module.URL, "")
	}
	if err == nil && url == "" {
		err = fmt.Errorf("%w: %s", ErrSubmoduleNoURL, module.Name)
	}
	return url, err
}

func (s *superproject) gitmodulesURL(module submodule.Module) string {
	url, err := s.resolveURL(module.URL, "")
	if err != nil {
		return ""
	}
	return url
}

func (s *superproject) modulesGitDir(name string) string {
	return s.repo.GitPath(submoduleModulesDir + "/" + name)
}

func emptyOrMissingDir(dir string) (bool, error) {
	entries, err := readSubmoduleDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return len(entries) == 0, err
}

func (s *superproject) cloneSubmodule(ctx context.Context, record updateRecord, url string, opts SubmoduleUpdateOptions) error {
	module := record.link.module
	if err := validateSubmodulePath(s.repo.WorkTree(), record.link.path); err != nil {
		return err
	}
	dir := s.submoduleDir(record.link.path)
	gitDir := s.modulesGitDir(module.Name)
	modulesDir := s.repo.GitPath(submoduleModulesDir)
	if err := submodule.ValidateGitDir(modulesDir, module.Name, repo.IsRepository); err != nil {
		return err
	}
	if opts.RequireInit {
		empty, err := emptyOrMissingDir(dir)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%w: %s", ErrSubmoduleNotEmpty, record.displayPath)
		}
	}
	if _, err := os.Stat(gitDir); err == nil {
		if err := os.MkdirAll(dir, directoryMode); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(gitDir, "index")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return connectWorkTreeAndGitDir(dir, gitDir)
	}
	opts.Events.emit(SubmoduleEvent{Kind: SubmoduleCloning, Path: record.displayPath, Name: module.Name, URL: url})
	depth := opts.Depth
	if opts.RecommendShallow && module.ShallowSet && module.Shallow {
		depth = 1
	}
	transportOpts := opts.Transport
	transportOpts.NotFromUser = true
	cloned, err := Clone(ctx, url, dir, CloneOptions{
		NoCheckout:     true,
		Depth:          depth,
		SeparateGitDir: gitDir,
		SingleBranch:   depth > 0 && !opts.noSingleBranch,
		Open:           s.repo.Options(),
		Progress:       opts.Progress,
		Transport:      transportOpts,
	})
	if cloned != nil {
		_ = cloned.Close()
	}
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrSubmoduleClone, record.displayPath, err)
	}
	return connectWorkTreeAndGitDir(dir, gitDir)
}

func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func relativeSlashPath(target, base string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func connectWorkTreeAndGitDir(workTree, gitDir string) error {
	gitDirReal, workTreeReal := realPath(gitDir), realPath(workTree)
	link := []byte("gitdir: " + relativeSlashPath(gitDirReal, workTreeReal) + "\n")
	if err := os.WriteFile(filepath.Join(workTree, gitDirName), link, gitFileMode); err != nil {
		return err
	}
	return writeSubmoduleConfig(filepath.Join(gitDir, "config"), [2]string{"core.worktree", relativeSlashPath(workTreeReal, gitDirReal)})
}

func (s *superproject) ensureCoreWorktree(sub *repo.Repository, path string) error {
	if _, set := sub.Config().GetValue("core.worktree"); !set {
		return nil
	}
	return writeSubmoduleConfig(filepath.Join(sub.GitDir(), "config"), [2]string{"core.worktree", relativeSlashPath(s.submoduleDir(path), realPath(sub.GitDir()))})
}

func (s *superproject) strategyFor(record updateRecord, opts SubmoduleUpdateOptions) (submodule.UpdateType, error) {
	kind, command := submodule.UpdateCheckout, ""
	switch opts.Mode {
	case SubmoduleUpdateRebase:
		kind = submodule.UpdateRebase
	case SubmoduleUpdateMerge:
		kind = submodule.UpdateMerge
	case SubmoduleUpdateConfigured:
		strategy, err := submodule.Strategy(s.cfg, record.link.module)
		if err != nil {
			return 0, fmt.Errorf("%w: %s", err, record.displayPath)
		}
		if strategy.Type != submodule.UpdateUnspecified {
			kind, command = strategy.Type, strategy.Command
		}
	}
	if record.justCloned && kind != submodule.UpdateCommand {
		kind = submodule.UpdateCheckout
	}
	if kind == submodule.UpdateCommand {
		word, _, _ := strings.Cut(strings.Join(strings.Fields(command), " "), " ")
		opts.Events.emit(SubmoduleEvent{Kind: SubmoduleCommandRefused, Path: record.displayPath, Name: record.link.module.Name, Command: word})
		return 0, fmt.Errorf("%w: %s", ErrSubmoduleCommand, record.displayPath)
	}
	return kind, nil
}

func (s *superproject) updateOne(ctx context.Context, record updateRecord, opts SubmoduleUpdateOptions) error {
	path := record.link.path
	if err := validateSubmodulePath(s.repo.WorkTree(), path); err != nil {
		return err
	}
	sub, populated, err := openSubmodule(s, path)
	if err != nil || !populated {
		return fmt.Errorf("%w: %s: %w", ErrSubmoduleNoHead, record.displayPath, errors.Join(err, errNotPopulated))
	}
	defer func() { _ = sub.Close() }()
	if err := s.ensureCoreWorktree(sub, path); err != nil {
		return err
	}
	kind, err := s.strategyFor(record, opts)
	if err != nil {
		return err
	}
	current := hash.Zero
	if !record.justCloned {
		if current, err = gitlink.Head(sub.Layout()); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrSubmoduleNoHead, record.displayPath, err)
		}
	}
	target := record.link.id
	if opts.Remote {
		if target, err = s.remoteTarget(ctx, sub, record, opts); err != nil {
			return err
		}
	}
	if target != current || opts.Force {
		if err := s.runUpdate(ctx, sub, record, kind, target, current.IsZero() || opts.Force, opts); err != nil {
			return err
		}
	}
	if !opts.Recursive {
		return nil
	}
	return s.updateNested(ctx, sub, record, opts)
}

func (s *superproject) updateNested(ctx context.Context, sub *repo.Repository, record updateRecord, opts SubmoduleUpdateOptions) error {
	nested, err := reopenSubmodule(sub)
	if err != nil {
		return err
	}
	defer func() { _ = nested.Close() }()
	inner, err := loadSuperproject(nested, nil)
	if err != nil {
		return err
	}
	inner.prefix = record.displayPath + "/"
	return inner.update(ctx, false, opts)
}

func (s *superproject) remoteTarget(ctx context.Context, sub *repo.Repository, record updateRecord, opts SubmoduleUpdateOptions) (hash.ObjectID, error) {
	remoteName, err := submoduleRemoteName(sub, s.gitmodulesURL(record.link.module))
	if err != nil {
		return hash.Zero, err
	}
	branch, err := s.remoteBranch(record)
	if err != nil {
		return hash.Zero, err
	}
	if !opts.NoFetch {
		if _, err := s.fetchSubmodule(ctx, sub, record, remoteName, opts, nil); err != nil {
			return hash.Zero, fmt.Errorf("%w: %s: %w", ErrSubmoduleFetch, record.displayPath, err)
		}
	}
	tracking := refs.Name(refs.RemotesPrefix + remoteName + "/" + branch)
	id, err := resolveRef(sub, tracking)
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %s: %s: %w", ErrSubmoduleRemoteRef, record.displayPath, tracking, err)
	}
	return id, nil
}

func (s *superproject) remoteBranch(record updateRecord) (string, error) {
	branch := submodule.Branch(s.cfg, record.link.module)
	switch branch {
	case "":
		return string(refs.HEAD), nil
	case ".":
		current, onBranch, err := currentBranchShort(s.repo)
		if err != nil {
			return "", err
		}
		if !onBranch {
			return "", fmt.Errorf("%w: %s", ErrSubmoduleNoBranch, record.link.module.Name)
		}
		return current, nil
	}
	return branch, nil
}

func resolveRef(r *repo.Repository, name refs.Name) (hash.ObjectID, error) {
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		return hash.Zero, err
	}
	defer func() { _ = store.Close() }()
	ref, err := store.Resolve(name)
	if err != nil {
		return hash.Zero, err
	}
	return ref.Target, nil
}

func (s *superproject) fetchSubmodule(ctx context.Context, sub *repo.Repository, record updateRecord, remoteName string, opts SubmoduleUpdateOptions, want []hash.ObjectID) (remote.FetchResult, error) {
	opts.Events.emit(SubmoduleEvent{Kind: SubmoduleFetching, Path: record.displayPath, Name: record.link.module.Name})
	transportOpts := opts.Transport
	transportOpts.NotFromUser = true
	return Fetch(ctx, sub, remoteName, remote.FetchOptions{
		Wants:     want,
		Depth:     opts.Depth,
		Progress:  opts.Progress,
		Transport: transportOpts,
	})
}

func (s *superproject) runUpdate(ctx context.Context, sub *repo.Repository, record updateRecord, kind submodule.UpdateType, target hash.ObjectID, force bool, opts SubmoduleUpdateOptions) error {
	if !opts.NoFetch {
		if err := s.ensureReachable(ctx, sub, record, target, opts); err != nil {
			return err
		}
	}
	event := SubmoduleEvent{Path: record.displayPath, Name: record.link.module.Name, Commit: target}
	switch kind {
	case submodule.UpdateRebase:
		event.Kind = SubmoduleRebased
		result, err := Rebase(ctx, sub, target.String(), RebaseOptions{Progress: opts.Progress, allowDetached: true})
		if err == nil && (result.Conflicted() || !result.Finished()) {
			err = ErrMergeInProgress
		}
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrSubmoduleRebase, record.displayPath, err)
		}
	case submodule.UpdateMerge:
		event.Kind = SubmoduleMerged
		result, err := Merge(ctx, sub, target.String(), MergeOptions{Progress: opts.Progress})
		if err == nil && !result.Clean() {
			err = ErrMergeInProgress
		}
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrSubmoduleMerge, record.displayPath, err)
		}
	default:
		event.Kind = SubmoduleCheckedOut
		if err := Switch(ctx, sub, target.String(), SwitchOptions{Force: force, SubmoduleEvents: opts.Events}); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrSubmoduleCheckout, record.displayPath, err)
		}
	}
	opts.Events.emit(event)
	return nil
}

func (s *superproject) ensureReachable(ctx context.Context, sub *repo.Repository, record updateRecord, target hash.ObjectID, opts SubmoduleUpdateOptions) error {
	if tipReachable(ctx, sub, target) {
		return nil
	}
	fetchRemote, _ := defaultRemoteName(sub)
	if _, err := s.fetchSubmodule(ctx, sub, record, fetchRemote, opts, nil); err != nil {
		opts.Events.emit(SubmoduleEvent{Kind: SubmoduleFetchRetry, Path: record.displayPath, Name: record.link.module.Name, Commit: target})
	}
	if tipReachable(ctx, sub, target) {
		return nil
	}
	directRemote, err := submoduleRemoteName(sub, s.gitmodulesURL(record.link.module))
	if err != nil {
		return err
	}
	_, fetchErr := s.fetchSubmodule(ctx, sub, record, directRemote, opts, []hash.ObjectID{target})
	if fetchErr != nil || !hasObject(sub, target) {
		return fmt.Errorf("%w: %s: %s: %w", ErrSubmoduleCommitMissing, record.displayPath, target, errors.Join(fetchErr))
	}
	return nil
}

func hasObject(r *repo.Repository, id hash.ObjectID) bool {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()
	has, err := db.Has(id)
	return err == nil && has
}

func tipReachable(ctx context.Context, r *repo.Repository, id hash.ObjectID) bool {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()
	has, hasErr := db.Has(id)
	tips, ok := refTips(r, db)
	shallow, shallowErr := r.Shallow()
	if hasErr != nil || !has || !ok || shallowErr != nil {
		return false
	}
	for range revision.Walk(ctx, revision.Options{
		Context:  revision.Context{Objects: db, Shallow: shallow},
		Include:  []hash.ObjectID{id},
		Exclude:  tips,
		MaxCount: 1,
	}) {
		return false
	}
	return true
}

func refTips(r *repo.Repository, db *odb.DB) ([]hash.ObjectID, bool) {
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Peeler: db})
	if err != nil {
		return nil, false
	}
	defer func() { _ = store.Close() }()
	var targets []hash.ObjectID
	if head, err := store.Resolve(refs.HEAD); err == nil {
		targets = append(targets, head.Target)
	}
	for ref, err := range store.Prefix("refs/") {
		if err != nil {
			return nil, false
		}
		targets = append(targets, ref.Target)
	}
	var tips []hash.ObjectID
	for _, target := range targets {
		if kind, peeled, err := db.Peel(target); err == nil && kind == object.TypeCommit {
			tips = append(tips, peeled)
		}
	}
	return tips, true
}
