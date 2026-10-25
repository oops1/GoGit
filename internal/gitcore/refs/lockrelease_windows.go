package refs

import (
	"errors"

	"golang.org/x/sys/windows"
)

func lockMayBeReleasing(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
