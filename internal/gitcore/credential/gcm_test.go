package credential

import (
	"context"
	"errors"
	"testing"
)

func TestGCMServiceNameFollowsTheRemoteURI(t *testing.T) {
	cases := []struct {
		q    Query
		want string
	}{
		{Query{Protocol: "https", Host: "GitHub.com"}, "https://github.com"},
		{Query{Protocol: "HTTPS", Host: "example.com:443"}, "https://example.com"},
		{Query{Protocol: "http", Host: "example.com:80"}, "http://example.com"},
		{Query{Protocol: "https", Host: "example.com:8443"}, "https://example.com:8443"},
		{Query{Protocol: "https", Host: "example.com:port"}, "https://example.com"},
		{Query{Protocol: "https", Host: "example.com", Path: "org/my repo.git"}, "https://example.com/org/my%20repo.git"},
		{Query{Protocol: "https", Host: "example.com", Path: "/org/"}, "https://example.com/org"},
		{Query{Protocol: "https", Host: "gist.github.com", Path: "abc"}, "https://github.com"},
		{Query{Protocol: "ssh", Host: "example.com:22"}, "ssh://example.com:22"},
		{Query{Protocol: "https", Username: "bob"}, ""},
		{Query{Host: "example.com"}, ""},
	}
	for _, c := range cases {
		if got := gcmServiceName(c.q); got != c.want {
			t.Errorf("gcmServiceName(%+v) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestParseGCMURIResolvesDefaultPortsAndPaths(t *testing.T) {
	cases := []struct {
		raw  string
		want gcmURI
		ok   bool
	}{
		{"https://Example.com", gcmURI{scheme: "https", host: "example.com", port: 443, path: "/"}, true},
		{"HTTP://bob_x@example.com:8080/Org", gcmURI{scheme: "http", host: "example.com", port: 8080, path: "/Org"}, true},
		{"ssh://example.com", gcmURI{scheme: "ssh", host: "example.com", port: -1, path: "/"}, true},
		{"example.com", gcmURI{}, false},
		{"https://example.com:99999999999999999999", gcmURI{}, false},
		{"https://exa mple.com", gcmURI{}, false},
	}
	for _, c := range cases {
		got, ok := parseGCMURI(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("parseGCMURI(%q) = %+v, %v; want %+v, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

type recordingGCMStore struct {
	answer   Answer
	found    bool
	err      error
	calls    []string
	services []string
	accounts []string
	secrets  []string
}

func (s *recordingGCMStore) record(call, service, account string, secret []byte) {
	s.calls = append(s.calls, call)
	s.services = append(s.services, service)
	s.accounts = append(s.accounts, account)
	s.secrets = append(s.secrets, string(secret))
}

func (s *recordingGCMStore) get(_ context.Context, service, account string) (Answer, bool, error) {
	s.record("get", service, account, nil)
	return s.answer, s.found, s.err
}

func (s *recordingGCMStore) put(_ context.Context, service, account string, secret []byte) error {
	s.record("put", service, account, secret)
	return s.err
}

func (s *recordingGCMStore) remove(_ context.Context, service, account string, secret []byte) error {
	s.record("remove", service, account, secret)
	return s.err
}

func TestManagerHelperPassesServiceAndAccountToTheStore(t *testing.T) {
	store := &recordingGCMStore{answer: Answer{Username: "bob", Password: []byte("pw")}, found: true}
	h := &managerHelper{name: "manager", store: store}
	ctx := context.Background()
	q := Query{Protocol: "https", Host: "example.com", Path: "org/repo.git"}

	ans, ok, err := h.Get(ctx, q)
	if err != nil || !ok || ans.Username != "bob" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if err := h.Store(ctx, q, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(ctx, q, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"get", "put", "remove"}
	for i, call := range wantCalls {
		if store.calls[i] != call || store.services[i] != "https://example.com/org/repo.git" {
			t.Fatalf("call %d = %s %s", i, store.calls[i], store.services[i])
		}
	}
	if store.accounts[0] != "" || store.accounts[1] != "bob" || store.secrets[1] != "pw" || store.accounts[2] != "bob" || store.secrets[2] != "pw" {
		t.Fatalf("accounts = %q, secrets = %q", store.accounts, store.secrets)
	}
	if h.Name() != "manager" {
		t.Fatalf("Name = %q", h.Name())
	}
}

func TestManagerHelperSkipsRequestsWithoutAService(t *testing.T) {
	store := &recordingGCMStore{err: errors.New("must not be called")}
	h := &managerHelper{name: "manager", store: store}
	ctx := context.Background()
	if _, ok, err := h.Get(ctx, Query{Protocol: "https"}); ok || err != nil {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if err := h.Store(ctx, Query{Protocol: "https"}, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store(ctx, Query{Protocol: "https", Host: "example.com"}, Answer{}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(ctx, Query{Host: "example.com"}, Answer{}); err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("calls = %q", store.calls)
	}
}
