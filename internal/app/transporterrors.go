package app

import (
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/version"
)

var remoteUserAgent = "git/2.45.0 (Go.Git " + version.String() + ")"

var transportErrorKeys = []struct {
	err error
	key string
}{
	{transport.ErrAuthSchemeUnsupported, "Operation.Error.AuthSchemeUnsupported"},
	{transport.ErrNTLMAuthFailed, "Operation.Error.NTLMAuthFailed"},
	{transport.ErrNegotiateAuthFailed, "Operation.Error.NegotiateAuthFailed"},
	{transport.ErrClientKeyEncrypted, "Operation.Error.ClientKeyEncrypted"},
	{transport.ErrHeaderTimeout, "Operation.Error.HeaderTimeout"},
	{transport.ErrInvalidProxy, "Operation.Error.InvalidProxy"},
	{transport.ErrLowSpeed, "Operation.Error.LowSpeed"},
	{transport.ErrProtocolNotAllowed, "Operation.Error.ProtocolNotAllowed"},
	{transport.ErrProxyAuthRequired, "Operation.Error.ProxyAuthRequired"},
	{transport.ErrTLSConfig, "Operation.Error.TLSConfig"},
}

func localizeOperationError(err error) error {
	for _, known := range transportErrorKeys {
		if errors.Is(err, known.err) {
			return fmt.Errorf("%s: %w", i18n.T(known.key), err)
		}
	}
	return err
}
