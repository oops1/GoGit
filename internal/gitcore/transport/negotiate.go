package transport

import (
	"context"
	"io"
)

type roundTripper interface {
	round(ctx context.Context, body []byte) (io.ReadCloser, error)
}
