package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func Push(ctx context.Context, r *repo.Repository, remoteName string, opts remote.PushOptions) (remote.PushResult, error) {
	if err := ctx.Err(); err != nil {
		return remote.PushResult{}, err
	}
	cfg := r.Config()
	name, err := resolveRemoteName(r, cfg, remoteName)
	if err != nil {
		return remote.PushResult{}, err
	}
	rem, err := remote.Load(cfg, name)
	if err != nil {
		return remote.PushResult{}, err
	}
	result, err := pushRemote(ctx, r, rem, opts)
	if err != nil {
		return result, err
	}
	if err := setPushedUpstreams(r, cfg, name, result.Changes); err != nil {
		return result, err
	}
	return result, nil
}
