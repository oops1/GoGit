//go:build !linux && !windows

package pack

import (
	"errors"
	"os"
)

var errMappingUnsupported = errors.New("pack: memory mapping is not supported on this platform")

func mapFileView(*os.File, int64) ([]byte, func() error, error) {
	return nil, nil, errMappingUnsupported
}
