package transport

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func unsetEnvironment(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
}

func TestProtocolAllowedFollowsGitPolicies(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		env     map[string]string
		scheme  Scheme
		allowed bool
	}{
		{"https is always allowed by default", "", nil, SchemeHTTPS, true},
		{"file is allowed for the user by default", "", nil, SchemeFile, true},
		{"unrelated config keeps the defaults", "[core]\n\tbare = false\n", nil, SchemeSSH, true},
		{"ext is never allowed by default", "", nil, Scheme("ext"), false},
		{"user policy refuses a request not from the user", "", map[string]string{"GIT_PROTOCOL_FROM_USER": "0"}, SchemeFile, false},
		{"user policy refuses an unparsable from-user flag", "", map[string]string{"GIT_PROTOCOL_FROM_USER": "maybe"}, SchemeFile, false},
		{"user policy allows an explicit from-user flag", "", map[string]string{"GIT_PROTOCOL_FROM_USER": "1"}, SchemeFile, true},
		{"protocol.allow covers every scheme", "[protocol]\n\tallow = never\n", nil, SchemeHTTPS, false},
		{"protocol.name.allow beats protocol.allow", "[protocol]\n\tallow = never\n[protocol \"https\"]\n\tallow = always\n", nil, SchemeHTTPS, true},
		{"GIT_ALLOW_PROTOCOL allows the listed schemes", "[protocol]\n\tallow = never\n", map[string]string{"GIT_ALLOW_PROTOCOL": "ssh:https"}, SchemeHTTPS, true},
		{"GIT_ALLOW_PROTOCOL refuses the other schemes", "", map[string]string{"GIT_ALLOW_PROTOCOL": "ssh"}, SchemeHTTPS, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnvironment(t, "GIT_ALLOW_PROTOCOL", "GIT_PROTOCOL_FROM_USER")
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			err := ProtocolAllowed(protocolTestConfig(t, tt.config), tt.scheme)
			if tt.allowed && err != nil {
				t.Fatalf("ProtocolAllowed returned %v, want the scheme allowed", err)
			}
			if !tt.allowed && !errors.Is(err, ErrProtocolNotAllowed) {
				t.Fatalf("ProtocolAllowed returned %v, want ErrProtocolNotAllowed", err)
			}
		})
	}
}

func TestProtocolAllowedRejectsAnUnknownPolicy(t *testing.T) {
	unsetEnvironment(t, "GIT_ALLOW_PROTOCOL", "GIT_PROTOCOL_FROM_USER")
	err := ProtocolAllowed(testGitConfig(t, "[protocol \"file\"]\n\tallow = sometimes\n"), SchemeFile)
	if !errors.Is(err, ErrProtocolNotAllowed) || !strings.Contains(err.Error(), "sometimes") {
		t.Fatalf("ProtocolAllowed returned %v, want an unknown policy error", err)
	}
}

func protocolTestConfig(t *testing.T, text string) *config.Config {
	t.Helper()
	if text == "" {
		return nil
	}
	return testGitConfig(t, text)
}
