package revision

import "os"

func init() { _ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1") }
