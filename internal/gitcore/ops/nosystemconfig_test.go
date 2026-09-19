package ops

import (
	"os"
	"path/filepath"
)

func init() {
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	_ = os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(os.TempDir(), "gogit-tests-have-no-global-config"))
}
