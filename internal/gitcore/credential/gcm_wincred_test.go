package credential

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGCMTargetNameMatchesTheManagerConvention(t *testing.T) {
	cases := []struct {
		namespace, service, account, want string
	}{
		{"git", "https://github.com", "", "git:https://github.com"},
		{"git", "https://github.com", "bob", "git:https://bob@github.com"},
		{"git", "https://example.com:8443/org/repo.git", "a@b.c", "git:https://a_b.c@example.com:8443/org/repo.git"},
		{"", "http://example.com:80/", "  ", "http://example.com"},
		{"git", "not a uri", "bob", "git:not a uri"},
	}
	for _, c := range cases {
		if got := gcmTargetName(c.namespace, c.service, c.account); got != c.want {
			t.Errorf("gcmTargetName(%q, %q, %q) = %q, want %q", c.namespace, c.service, c.account, got, c.want)
		}
	}
}

func TestGCMWindowsMatchesLikeTheManager(t *testing.T) {
	cases := []struct {
		name      string
		namespace string
		service   string
		account   string
		cred      winCredential
		want      bool
	}{
		{"exact target", "git", "https://github.com", "", winCredential{target: "git:https://github.com", userName: "bob"}, true},
		{"account in target", "git", "https://github.com", "bob", winCredential{target: "git:https://bob@github.com", userName: "bob"}, true},
		{"legacy prefix", "git", "https://github.com", "bob", winCredential{target: "LegacyGeneric:target=git:https://github.com", userName: "bob"}, true},
		{"wincred entry", "git", "https://example.com", "", winCredential{target: "git:https://bob@example.com", userName: "bob"}, true},
		{"case of host and path", "git", "https://example.com/org/repo", "", winCredential{target: "git:https://EXAMPLE.com/ORG/repo", userName: "bob"}, true},
		{"other account", "git", "https://github.com", "eve", winCredential{target: "git:https://github.com", userName: "bob"}, false},
		{"blank account matches any", "git", "https://github.com", " ", winCredential{target: "git:https://github.com", userName: "bob"}, true},
		{"other namespace", "git", "https://github.com", "", winCredential{target: "work:https://github.com", userName: "bob"}, false},
		{"no namespace", "", "https://github.com", "", winCredential{target: "https://github.com", userName: "bob"}, true},
		{"other scheme", "git", "https://github.com", "", winCredential{target: "git:http://github.com", userName: "bob"}, false},
		{"other host", "git", "https://github.com", "", winCredential{target: "git:https://gitlab.com", userName: "bob"}, false},
		{"other port", "git", "https://github.com", "", winCredential{target: "git:https://github.com:8443", userName: "bob"}, false},
		{"explicit default port", "git", "https://github.com", "", winCredential{target: "git:https://github.com:443", userName: "bob"}, true},
		{"other path", "git", "https://github.com/a", "", winCredential{target: "git:https://github.com/b", userName: "bob"}, false},
		{"target is not a uri", "git", "https://github.com", "", winCredential{target: "git:github.com", userName: "bob"}, false},
		{"service is not a uri", "git", "github.com", "", winCredential{target: "git:https://github.com", userName: "bob"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gcmWindowsMatches(c.namespace, c.service, c.account, c.cred); got != c.want {
				t.Fatalf("gcmWindowsMatches = %v, want %v", got, c.want)
			}
		})
	}
}

func newTestGCMWindowsStore() (*gcmWindowsStore, *fakeCredentialManager) {
	manager := &fakeCredentialManager{}
	return &gcmWindowsStore{manager: manager, namespace: "git"}, manager
}

func TestGCMWindowsStorePutWritesTheAccountlessTargetFirst(t *testing.T) {
	s, manager := newTestGCMWindowsStore()
	ctx := context.Background()
	if err := s.put(ctx, "https://example.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if len(manager.creds) != 1 || manager.creds[0].target != "git:https://example.com" || manager.creds[0].userName != "bob" || manager.creds[0].comment != "" {
		t.Fatalf("creds = %+v", manager.creds)
	}
	ans, ok, err := s.get(ctx, "https://example.com", "bob")
	if err != nil || !ok || ans.Username != "bob" || string(ans.Password) != "pw" {
		t.Fatalf("get = %+v, %v, %v", ans, ok, err)
	}
}

func TestGCMWindowsStorePutUpdatesTheSameAccountAndAddsOthers(t *testing.T) {
	s, manager := newTestGCMWindowsStore()
	ctx := context.Background()
	manager.add("git:https://example.com", "bob", "old")
	if err := s.put(ctx, "https://example.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "https://example.com", "eve@corp", []byte("hers")); err != nil {
		t.Fatal(err)
	}
	targets := make([]string, 0, len(manager.creds))
	for _, c := range manager.creds {
		targets = append(targets, c.target)
	}
	if !slices.Equal(targets, []string{"git:https://example.com", "git:https://eve_corp@example.com"}) {
		t.Fatalf("targets = %q", targets)
	}
	ans, ok, err := s.get(ctx, "https://example.com", "eve@corp")
	if err != nil || !ok || string(ans.Password) != "hers" {
		t.Fatalf("get = %+v, %v, %v", ans, ok, err)
	}
	ans, _, _ = s.get(ctx, "https://example.com", "bob")
	if string(ans.Password) != "new" {
		t.Fatalf("bob password = %q", ans.Password)
	}
}

func TestGCMWindowsStorePutSkipsAnUnchangedCredential(t *testing.T) {
	s, manager := newTestGCMWindowsStore()
	manager.add("git:https://example.com", "bob", "pw")
	manager.writeErr = errors.New("must not write")
	if err := s.put(context.Background(), "https://example.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
}

func TestGCMWindowsStoreGetMisses(t *testing.T) {
	s, manager := newTestGCMWindowsStore()
	manager.add("git:https://example.org", "bob", "pw")
	if _, ok, err := s.get(context.Background(), "https://example.com", ""); ok || err != nil {
		t.Fatalf("get = %v, %v", ok, err)
	}
	if !slices.Equal(manager.filters, []string{""}) {
		t.Fatalf("filters = %q, want the whole credential list", manager.filters)
	}
}

func TestGCMWindowsStoreRemoveDeletesOnlyTheRejectedSecret(t *testing.T) {
	s, manager := newTestGCMWindowsStore()
	ctx := context.Background()
	manager.add("LegacyGeneric:target=git:https://example.com", "bob", "new")
	if err := s.remove(ctx, "https://example.com", "bob", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if len(manager.removed) != 0 {
		t.Fatalf("removed = %q after a stale reject", manager.removed)
	}
	if err := s.remove(ctx, "https://example.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.remove(ctx, "https://example.com", "bob", nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(manager.removed, []string{"git:https://example.com"}) {
		t.Fatalf("removed = %q", manager.removed)
	}
}

func TestGCMWindowsStoreRemoveWithoutPasswordDeletesTheFirstMatch(t *testing.T) {
	s, manager := newTestGCMWindowsStore()
	manager.add("git:https://example.com", "bob", "pw")
	if err := s.remove(context.Background(), "https://example.com", "", nil); err != nil {
		t.Fatal(err)
	}
	if len(manager.creds) != 0 {
		t.Fatalf("creds = %+v", manager.creds)
	}
}

func TestGCMWindowsStoreReportsCredentialManagerFailures(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()

	s, manager := newTestGCMWindowsStore()
	manager.enumerateErr = boom
	if _, _, err := s.get(ctx, "https://example.com", ""); !errors.Is(err, boom) {
		t.Fatalf("get err = %v", err)
	}
	if err := s.remove(ctx, "https://example.com", "", nil); !errors.Is(err, boom) {
		t.Fatalf("remove err = %v", err)
	}

	s, manager = newTestGCMWindowsStore()
	manager.readErr = boom
	if err := s.put(ctx, "https://example.com", "bob", []byte("pw")); !errors.Is(err, boom) {
		t.Fatalf("put read err = %v", err)
	}

	s, manager = newTestGCMWindowsStore()
	manager.writeErr = boom
	if err := s.put(ctx, "https://example.com", "bob", []byte("pw")); !errors.Is(err, boom) {
		t.Fatalf("put write err = %v", err)
	}

	s, manager = newTestGCMWindowsStore()
	manager.add("git:https://example.com", "bob", "pw")
	manager.removeErr = boom
	if err := s.remove(ctx, "https://example.com", "bob", []byte("pw")); !errors.Is(err, boom) {
		t.Fatalf("remove err = %v", err)
	}
}
