package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type PullOptions struct {
	Remote   string
	Fetch    remote.FetchOptions
	Progress progress.Func
	When     time.Time
}

type PullResult struct {
	Fetch    remote.FetchResult
	Old      hash.ObjectID
	New      hash.ObjectID
	Updated  bool
	UpToDate bool
	Merge    MergeResult
	Rebase   RebaseResult
}

const pullNote = "pull"

func pullTrackingRef(r *repo.Repository, name refs.Name) (hash.ObjectID, bool, error) {
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare()})
	if err != nil {
		return hash.Zero, false, err
	}
	defer func() { _ = store.Close() }()
	ref, err := store.Lookup(name)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, false, nil
	}
	if err != nil {
		return hash.Zero, false, err
	}
	return ref.Target, true, nil
}

func isFastForwardCommit(r *repo.Repository, old, newCommit hash.ObjectID) (bool, error) {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	return revision.IsAncestor(revision.Context{Objects: db}, old, newCommit)
}

func commitPulledBranch(r *repo.Repository, branch refs.Name, oldCommit, newCommit hash.ObjectID) error {
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	tx := rc.refs.Begin()
	tx.SetMessage("pull: Fast-forward")
	if err := txUpdate(tx, branch, newCommit, oldCommit); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func Pull(ctx context.Context, r *repo.Repository, opts PullOptions) (PullResult, error) {
	if err := ctx.Err(); err != nil {
		return PullResult{}, err
	}

	branchShort, onBranch, err := currentBranchShort(r)
	if err != nil {
		return PullResult{}, err
	}
	if !onBranch {
		return PullResult{}, ErrDetachedHead
	}

	cfg := r.Config()
	branchCfg, _ := cfg.Branch(branchShort)
	remoteName := opts.Remote
	if remoteName == "" {
		remoteName = branchCfg.Remote
	}
	if remoteName == "" || len(branchCfg.Merge) == 0 {
		return PullResult{}, ErrNoUpstream
	}
	mergeRef := refs.Name(branchCfg.Merge[0])

	rem, err := remote.Load(cfg, remoteName)
	if err != nil {
		return PullResult{}, err
	}

	fetchResult, err := fetchRemote(ctx, r, rem, opts.Fetch)
	if err != nil {
		return PullResult{}, err
	}

	branchRef := refs.BranchName(branchShort)
	oldCommit, _, err := pullTrackingRef(r, branchRef)
	if err != nil {
		return PullResult{Fetch: fetchResult}, err
	}

	trackingName := refs.RemoteBranchName(remoteName, mergeRef.Short())
	newCommit, found, err := pullTrackingRef(r, trackingName)
	if err != nil {
		return PullResult{Fetch: fetchResult}, err
	}
	if !found {
		return PullResult{Fetch: fetchResult}, ErrNoUpstream
	}

	if oldCommit == newCommit {
		return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit, UpToDate: true}, nil
	}

	if r.IsBare() {
		return pullIntoBare(r, branchRef, oldCommit, newCommit, fetchResult)
	}
	mode, rebase := pullModeOf(cfg, branchShort)
	merged, err := pullMerge(ctx, r, incoming{
		commit: newCommit,
		label:  newCommit.String(),
		reflog: pullNote,
		message: func(head headTarget) string {
			return pullMergeMessage(r, mergeRef, rem.FetchURL(), head)
		},
	}, MergeOptions{Mode: mode, Progress: opts.Progress, When: opts.When})
	result := PullResult{Fetch: fetchResult, Old: oldCommit, New: merged.New, Merge: merged, Updated: merged.FastForward || merged.Committed}
	if errors.Is(err, ErrCannotFastForward) && rebase {
		result.Rebase, err = pullRebase(ctx, r, newCommit, opts)
		result.New, result.Updated = result.Rebase.New, err == nil && result.Rebase.Finished()
	}
	var overwrite *OverwriteError
	switch {
	case errors.Is(err, ErrCannotFastForward):
		result.New = newCommit
		return result, ErrNotFastForward
	case errors.As(err, &overwrite):
		return result, fmt.Errorf("%w: %w", ErrDirtyWorkTree, err)
	}
	return result, err
}

func pullMerge(ctx context.Context, r *repo.Repository, in incoming, opts MergeOptions) (MergeResult, error) {
	m, err := openMerger(ctx, r, opts)
	if err != nil {
		return MergeResult{}, err
	}
	defer m.close()
	if err := m.refuseWhileMerging(); err != nil {
		return MergeResult{}, err
	}
	return m.integrate(in)
}

func pullRebase(ctx context.Context, r *repo.Repository, upstream hash.ObjectID, opts PullOptions) (RebaseResult, error) {
	m, err := openRebaser(ctx, r, RebaseOptions{When: opts.When, Progress: opts.Progress})
	if err != nil {
		return RebaseResult{}, err
	}
	defer m.close()
	m.action = pullNote
	return m.rebaseOnto(upstream, upstream, upstream.String(), nil)
}

func pullIntoBare(r *repo.Repository, branchRef refs.Name, oldCommit, newCommit hash.ObjectID, fetchResult remote.FetchResult) (PullResult, error) {
	if !oldCommit.IsZero() {
		ancestor, err := isFastForwardCommit(r, oldCommit, newCommit)
		if err != nil {
			return PullResult{Fetch: fetchResult}, err
		}
		if !ancestor {
			return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit}, ErrNotFastForward
		}
	}
	if err := commitPulledBranch(r, branchRef, oldCommit, newCommit); err != nil {
		return PullResult{Fetch: fetchResult}, err
	}
	return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit, Updated: true}, nil
}

func pullModeOf(cfg *config.Config, branch string) (MergeMode, bool) {
	rebase := false
	for _, key := range []string{"pull.rebase", "branch." + branch + ".rebase"} {
		if value, ok := cfg.Get(key); ok {
			rebase = rebaseValue(value)
		}
	}
	if rebase {
		return MergeFastForwardOnly, true
	}
	switch ff, _ := cfg.Get("pull.ff"); strings.ToLower(ff) {
	case "only":
		return MergeFastForwardOnly, false
	case "false", "no", "off", "0":
		return MergeNoFastForward, false
	}
	return MergeFastForward, false
}

func rebaseValue(value string) bool {
	switch strings.ToLower(value) {
	case "", "false", "no", "off", "0":
		return false
	}
	return true
}

func pullMergeMessage(r *repo.Repository, mergeRef refs.Name, url string, head headTarget) string {
	subject := "Merge branch '" + mergeRef.Short() + "' of " + fetchHeadURL(url)
	if dest := head.ref.Short(); !suppressesDest(r, dest) {
		subject += " into " + dest
	}
	return subject + "\n"
}

func fetchHeadURL(raw string) string {
	if scheme, rest, ok := strings.Cut(raw, "://"); ok {
		host, _, _ := strings.Cut(rest, "/")
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			rest = rest[at+1:]
		}
		raw = scheme + "://" + rest
	} else if user, target, found := strings.Cut(raw, "@"); found && strings.Contains(target, ":") && !strings.Contains(user, "/") {
		raw = target
	}
	raw = strings.TrimRight(raw, "/")
	return strings.TrimSuffix(raw, ".git")
}
