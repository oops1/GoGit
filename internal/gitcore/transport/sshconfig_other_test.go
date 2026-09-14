//go:build !windows

package transport

import "testing"

func TestSystemSSHConfigPathIsTheEtcConfig(t *testing.T) {
	if got := systemSSHConfigPath(); got != "/etc/ssh/ssh_config" {
		t.Fatalf("systemSSHConfigPath = %q", got)
	}
}
