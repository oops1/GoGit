package remote

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/gitcore/transport/local"
)

var (
	localDial   = local.Dial
	networkDial = transport.Dial
)

func dialAny(ctx context.Context, rawURL string, service transport.Service, opts transport.Options) (transport.Session, error) {
	endpoint, password, err := transport.ParseURL(rawURL)
	password.Wipe()
	if err != nil {
		return nil, err
	}
	if endpoint.IsLocal() {
		return localDial(ctx, endpoint.Path, service, opts)
	}
	return networkDial(ctx, rawURL, service, opts)
}
