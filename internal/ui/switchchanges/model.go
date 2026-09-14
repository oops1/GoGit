package switchchanges

import (
	"strings"

	"github.com/oops1/gogit/internal/config"
)

const maxListedPaths = 8

type Choice struct {
	Mode     string
	Remember bool
}

func Automatic(setting string) (string, bool) {
	switch setting {
	case config.SwitchChangesStash, config.SwitchChangesMerge, config.SwitchChangesOverwrite:
		return setting, true
	}
	return "", false
}

func ListPaths(paths []string) string {
	if len(paths) <= maxListedPaths {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:maxListedPaths], ", ") + ", …"
}
