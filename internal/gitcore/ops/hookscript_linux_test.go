package ops

import "testing"

func installTestHook(t testing.TB, dir, name string, h testHook) {
	t.Helper()
	installShellHook(t, dir, name, h)
}
