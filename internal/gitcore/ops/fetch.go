package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func Fetch(ctx context.Context, r *repo.Repository, remoteName string, opts remote.FetchOptions) (remote.FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return remote.FetchResult{}, err
	}
	cfg := r.Config()
	name, err := resolveRemoteName(r, cfg, remoteName)
	if err != nil {
		return remote.FetchResult{}, err
	}
	rem, err := remote.Load(cfg, name)
	if err != nil {
		return remote.FetchResult{}, err
	}
	return fetchRemote(ctx, r, rem, opts)
}
