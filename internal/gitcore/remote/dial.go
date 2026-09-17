package remote

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/gitcore/transport/local"
)

var (
	localDial      = local.Dial
	networkDial    = transport.Dial
	loadUserConfig = func() (*config.Config, error) { return config.Load(config.Options{}) }
)

func dialAny(ctx context.Context, rawURL string, service transport.Service, opts transport.Options) (transport.Session, error) {
	endpoint, password, err := transport.ParseURL(rawURL)
	password.Wipe()
	if err != nil {
		return nil, err
	}
	if err := transport.ProtocolAllowedFor(opts.Config, endpoint.Scheme, opts.NotFromUser); err != nil {
		return nil, err
	}
	if endpoint.IsLocal() {
		return localDial(ctx, endpoint.Path, service, opts)
	}
	return networkDial(ctx, rawURL, service, opts)
}

func withRepositoryConfig(opts transport.Options, r *repo.Repository, remoteName string) transport.Options {
	opts.Config = r.Config()
	opts.RemoteName = remoteName
	return opts
}
