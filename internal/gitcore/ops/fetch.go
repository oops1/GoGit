package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func Fetch(ctx context.Context, r *repo.Repository, remoteName string, opts remote.FetchOptions) (remote.FetchResult, error) {
	return FetchRecursive(ctx, r, remoteName, opts, SubmoduleFetchOptions{})
}
