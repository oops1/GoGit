package app

import (
	"slices"
	"testing"
)

func TestCredentialResourceKeyNormalizesTypedWebAddresses(t *testing.T) {
	tests := []struct {
		typed string
		want  string
	}{
		{"https://example.com/org/", "https://example.com/org"},
		{"https://bob:secret@example.com:8443/repo.git", "https://bob@example.com:8443/repo.git"},
		{"http://example.com:8080", "http://example.com:8080"},
		{"example.com/org", "example.com/org"},
		{"git@example.com:org/repo.git", "git@example.com:org/repo.git"},
		{"https://", "https://"},
		{"", ""},
	}
	for _, test := range tests {
		if got := credentialResourceKey(test.typed); got != test.want {
			t.Fatalf("credentialResourceKey(%q) = %q, want %q", test.typed, got, test.want)
		}
	}
}

func TestOnAddCredentialStoresATypedAddressUnderItsURLKey(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnAddCredential("https://alice:ignored@example.com/org/", "alice", []byte("token"))
	secretsWG.Wait()

	if !slices.Equal(v.Resources(), []string{"https://alice@example.com/org"}) {
		t.Fatalf("resources = %v", v.Resources())
	}
}
