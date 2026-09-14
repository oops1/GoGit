package transport

import (
	"os/user"
	"testing"
)

func TestSSHUsernameDropsTheWindowsDomain(t *testing.T) {
	restore := currentUser
	t.Cleanup(func() { currentUser = restore })

	for account, want := range map[string]string{`MACHINE\valer`: "valer", "valer": "valer"} {
		currentUser = func() (*user.User, error) { return &user.User{Username: account}, nil }
		if got := sshUsername(""); got != want {
			t.Fatalf("sshUsername for %q = %q, want %q", account, got, want)
		}
	}
	if got := sshUsername("git"); got != "git" {
		t.Fatalf("an explicit user became %q", got)
	}
}
