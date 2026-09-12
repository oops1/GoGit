package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type StartBranchOptions struct {
	Force bool
	Track bool
}

type StartBranchResult struct {
	Branch   refs.Name
	Start    hash.ObjectID
	Upstream refs.Name
	Created  bool
}

func StartBranch(ctx context.Context, r *repo.Repository, name, start string, opts StartBranchOptions) (StartBranchResult, error) {
	if err := ctx.Err(); err != nil {
		return StartBranchResult{}, err
	}
	if err := validateBranchName(name); err != nil {
		return StartBranchResult{}, err
	}
	commit, upstream, err := startPointOf(r, start)
	if err != nil {
		return StartBranchResult{}, err
	}
	result := StartBranchResult{Branch: refs.BranchName(name), Start: commit}
	if err := CreateBranch(ctx, r, name, commit, CreateBranchOptions{Force: opts.Force}); err != nil {
		return result, err
	}
	result.Created = true
	if opts.Track && upstream != "" {
		remoteName, mergeRef, ok := upstreamParts(r, upstream)
		if !ok {
			return result, fmt.Errorf("%w: %s", ErrNoUpstream, upstream)
		}
		if err := setBranchUpstream(r, name, remoteName, mergeRef); err != nil {
			return result, err
		}
		result.Upstream = upstream
	}
	if err := Switch(ctx, r, name, SwitchOptions{Force: opts.Force}); err != nil {
		return result, err
	}
	return result, nil
}

func startPointOf(r *repo.Repository, start string) (hash.ObjectID, refs.Name, error) {
	rc, err := openRepoContext(r)
	if err != nil {
		return hash.Zero, "", err
	}
	defer func() { _ = rc.close() }()

	if start == "" {
		head, err := resolveHeadTarget(rc.refs)
		if err != nil {
			return hash.Zero, "", err
		}
		if head.old.IsZero() {
			return hash.Zero, "", ErrUnbornHead
		}
		return head.old, "", nil
	}
	commit, ref, err := resolveStart(rc, start)
	if err != nil {
		return hash.Zero, "", err
	}
	return commit, ref, nil
}

func resolveStart(rc *repoContext, start string) (hash.ObjectID, refs.Name, error) {
	for _, candidate := range []refs.Name{refs.Name(start), refs.Name(refs.RemotesPrefix + start)} {
		if !candidate.IsRemote() {
			continue
		}
		ref, err := refsLookup(rc.refs, candidate)
		switch {
		case err == nil:
			return ref.Target, candidate, nil
		case !errors.Is(err, refs.ErrNotFound):
			return hash.Zero, "", err
		}
	}
	commit, err := resolveCommittish(rc, start)
	return commit, "", err
}

func upstreamParts(r *repo.Repository, upstream refs.Name) (string, string, bool) {
	short := strings.TrimPrefix(string(upstream), refs.RemotesPrefix)
	for _, known := range r.Config().Remotes() {
		if head, found := strings.CutPrefix(short, known.Name+"/"); found {
			return known.Name, refs.BranchName(head).String(), true
		}
	}
	return "", "", false
}
