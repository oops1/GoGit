package ops

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

var ErrNoPromisorRemote = errors.New("ops: the repository has no promisor remote")

const (
	promisorKeySuffix    = ".promisor"
	partialCloneKeySufix = ".partialclonefilter"
	lazyFetchFilter      = transport.FilterBlobNone
)

type promisor struct {
	name   string
	filter string
}

func promisorOf(cfg *config.Config) (promisor, bool, error) {
	for _, rem := range remote.List(cfg) {
		on, err := configFlag(cfg, "remote."+rem.Name+promisorKeySuffix, false)
		if err != nil {
			return promisor{}, false, err
		}
		if !on {
			continue
		}
		filter, _ := cfg.Get("remote." + rem.Name + partialCloneKeySufix)
		return promisor{name: rem.Name, filter: filter}, true, nil
	}
	return promisor{}, false, nil
}

func partialCloneValues(remoteName, filter string) [][2]string {
	if filter == "" {
		return nil
	}
	return [][2]string{
		{"core.repositoryformatversion", "1"},
		{"remote." + remoteName + promisorKeySuffix, "true"},
		{"remote." + remoteName + partialCloneKeySufix, filter},
	}
}

func withPromisorFilter(r *repo.Repository, rem remote.Remote, opts remote.FetchOptions) (remote.FetchOptions, error) {
	if opts.Filter != "" {
		return opts, nil
	}
	found, ok, err := promisorOf(r.Config())
	if err != nil || !ok || found.name != rem.Name {
		return opts, err
	}
	opts.Filter = found.filter
	return opts, nil
}

func objectOptions(ctx context.Context, r *repo.Repository) odb.Options {
	opts := odb.Options{Format: r.ObjectFormat}
	found, ok, err := promisorOf(r.Config())
	if err != nil || !ok {
		return opts
	}
	opts.Missing = func(id hash.ObjectID) error {
		return fetchPromisorObjects(ctx, r, found.name, []hash.ObjectID{id})
	}
	return opts
}

func objectDatabase(ctx context.Context, r *repo.Repository) (*odb.DB, error) {
	return odbOpen(r.ObjectsDir(), objectOptions(ctx, r))
}

func FetchMissingObjects(ctx context.Context, r *repo.Repository, ids []hash.ObjectID) error {
	found, ok, err := promisorOf(r.Config())
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoPromisorRemote
	}
	return fetchPromisorObjects(ctx, r, found.name, ids)
}

func fetchPromisorObjects(ctx context.Context, r *repo.Repository, remoteName string, ids []hash.ObjectID) error {
	rem, err := remote.Load(r.Config(), remoteName)
	if err != nil {
		return err
	}
	_, err = fetchRemote(ctx, r, rem, remote.FetchOptions{
		Wants:       ids,
		Filter:      lazyFetchFilter,
		ObjectsOnly: true,
	})
	return err
}
