package credential

import (
	"context"
	"errors"
	"maps"
	"testing"
)

func TestGCMKeyringStoreUsesTheManagerSchema(t *testing.T) {
	k := &fakeKeyring{}
	s := &gcmKeyringStore{open: k.opener(), namespace: "git"}
	ctx := context.Background()
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	live := k.live()
	want := map[string]string{"service": "git:https://github.com", "account": "bob", "xdg:schema": gcmSecretSchema}
	if len(live) != 1 || !maps.Equal(live[0].attributes, want) || live[0].label != "git:https://github.com" || live[0].content != gcmSecretContentType || string(live[0].secret) != "pw" {
		t.Fatalf("stored = %+v", live)
	}
	ans, ok, err := s.get(ctx, "https://github.com", "")
	if err != nil || !ok || ans.Username != "bob" || string(ans.Password) != "pw" {
		t.Fatalf("get = %+v, %v, %v", ans, ok, err)
	}
	if !maps.Equal(k.searches[len(k.searches)-1], map[string]string{"service": "git:https://github.com"}) {
		t.Fatalf("search = %v", k.searches[len(k.searches)-1])
	}
	if k.opens != k.closes {
		t.Fatalf("opens = %d, closes = %d", k.opens, k.closes)
	}
}

func TestGCMKeyringStoreWithoutNamespaceUsesTheBareService(t *testing.T) {
	k := &fakeKeyring{}
	s := &gcmKeyringStore{open: k.opener()}
	if err := s.put(context.Background(), "https://github.com", "", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if live := k.live(); len(live) != 1 || live[0].attributes["service"] != "https://github.com" || live[0].attributes["account"] != "" {
		t.Fatalf("stored = %+v", live)
	}
}

func TestGCMKeyringStorePutSkipsAnUnchangedSecret(t *testing.T) {
	k := &fakeKeyring{}
	s := &gcmKeyringStore{open: k.opener(), namespace: "git"}
	ctx := context.Background()
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	k.storeErr = errors.New("must not store")
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "https://github.com", "bob", []byte("new")); !errors.Is(err, k.storeErr) {
		t.Fatalf("err = %v, want the store failure for a changed secret", err)
	}
}

func TestGCMKeyringStoreRemovesOnlyTheRejectedSecret(t *testing.T) {
	k := &fakeKeyring{}
	s := &gcmKeyringStore{open: k.opener(), namespace: "git"}
	ctx := context.Background()
	if err := s.put(ctx, "https://github.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.remove(ctx, "https://github.com", "bob", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if len(k.live()) != 1 {
		t.Fatal("a stale reject removed the new secret")
	}
	if err := s.remove(ctx, "https://github.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if len(k.live()) != 0 {
		t.Fatal("the rejected secret is still stored")
	}
	if err := s.put(ctx, "https://github.com", "bob", []byte("again")); err != nil {
		t.Fatal(err)
	}
	if err := s.remove(ctx, "https://github.com", "", nil); err != nil {
		t.Fatal(err)
	}
	if len(k.live()) != 0 {
		t.Fatal("remove without a secret must clear every match")
	}
	if _, ok, err := s.get(ctx, "https://github.com", ""); ok || err != nil {
		t.Fatalf("get after remove = %v, %v", ok, err)
	}
}

func TestGCMKeyringStoreReportsKeyringFailures(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()
	for _, setup := range []func(*fakeKeyring){
		func(k *fakeKeyring) { k.openErr = boom },
		func(k *fakeKeyring) { k.searchErr = boom },
	} {
		k := &fakeKeyring{}
		setup(k)
		s := &gcmKeyringStore{open: k.opener(), namespace: "git"}
		if _, _, err := s.get(ctx, "https://github.com", ""); !errors.Is(err, boom) {
			t.Fatalf("get err = %v", err)
		}
		if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); !errors.Is(err, boom) {
			t.Fatalf("put err = %v", err)
		}
		if err := s.remove(ctx, "https://github.com", "bob", nil); !errors.Is(err, boom) {
			t.Fatalf("remove err = %v", err)
		}
	}
	k := &fakeKeyring{removeErr: boom}
	s := &gcmKeyringStore{open: k.opener(), namespace: "git"}
	if err := s.remove(ctx, "https://github.com", "bob", nil); !errors.Is(err, boom) {
		t.Fatalf("remove err = %v", err)
	}
}

func TestLibsecretAttributesFollowTheHelperSchema(t *testing.T) {
	cases := []struct {
		q    Query
		want map[string]string
	}{
		{Query{Protocol: "https", Host: "example.com"}, map[string]string{"protocol": "https", "server": "example.com"}},
		{Query{Protocol: "https", Host: "example.com:8443", Path: "org/repo.git", Username: "bob"}, map[string]string{"protocol": "https", "server": "example.com", "port": "8443", "object": "org/repo.git", "user": "bob"}},
		{Query{Protocol: "https", Host: "example.com:0"}, map[string]string{"protocol": "https", "server": "example.com"}},
		{Query{Protocol: "https", Host: "example.com:65537x"}, map[string]string{"protocol": "https", "server": "example.com", "port": "1"}},
	}
	for _, c := range cases {
		if got := libsecretAttributes(c.q); !maps.Equal(got, c.want) {
			t.Errorf("libsecretAttributes(%+v) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestLibsecretLabelFollowsTheHelper(t *testing.T) {
	if got := libsecretLabel(Query{Protocol: "https", Host: "example.com:8443", Path: "org/repo"}); got != "Git: https://example.com:8443/org/repo" {
		t.Fatalf("label = %q", got)
	}
	if got := libsecretLabel(Query{Protocol: "https", Host: "example.com"}); got != "Git: https://example.com/" {
		t.Fatalf("label = %q", got)
	}
}

func newTestLibsecret() (*libsecretHelper, *fakeKeyring) {
	k := &fakeKeyring{}
	return &libsecretHelper{name: "libsecret", open: k.opener()}, k
}

func TestLibsecretHelperStoresAndFindsACredential(t *testing.T) {
	h, k := newTestLibsecret()
	ctx := context.Background()
	q := Query{Protocol: "https", Host: "example.com:8443"}
	if err := h.Store(ctx, q, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	live := k.live()
	want := map[string]string{"protocol": "https", "server": "example.com", "port": "8443", "user": "bob", "xdg:schema": libsecretSchema}
	if len(live) != 1 || !maps.Equal(live[0].attributes, want) || live[0].label != "Git: https://example.com:8443/" || live[0].content != libsecretContentType {
		t.Fatalf("stored = %+v", live)
	}
	ans, ok, err := h.Get(ctx, q)
	if err != nil || !ok || ans.Username != "bob" || string(ans.Password) != "pw" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if h.Name() != "libsecret" {
		t.Fatalf("Name = %q", h.Name())
	}
}

func TestLibsecretHelperReadsTheFirstLineAndFallsBackToTheQueryUser(t *testing.T) {
	h, k := newTestLibsecret()
	k.items = []fakeKeyringItem{{attributes: map[string]string{"protocol": "https", "server": "example.com"}, secret: []byte("pw\npassword_expiry_utc=1")}}
	ctx := context.Background()
	if _, ok, err := h.Get(ctx, Query{Protocol: "https", Host: "example.com"}); ok || err != nil {
		t.Fatalf("Get without any user = %v, %v", ok, err)
	}
	ans, ok, err := h.Get(ctx, Query{Protocol: "https", Host: "example.com", Username: "bob"})
	if ok || err != nil {
		t.Fatalf("Get = %+v, %v, %v; the stored item has no user attribute to match", ans, ok, err)
	}
	k.items[0].attributes["user"] = "bob"
	ans, ok, err = h.Get(ctx, Query{Protocol: "https", Host: "example.com"})
	if err != nil || !ok || ans.Username != "bob" || string(ans.Password) != "pw" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	h2, k2 := newTestLibsecret()
	k2.items = []fakeKeyringItem{{attributes: map[string]string{"protocol": "https", "server": "example.com"}, secret: []byte("pw")}}
	h2.open = func() (keyring, error) { return queryUserKeyring{k2}, nil }
	ans, ok, err = h2.Get(ctx, Query{Protocol: "https", Host: "example.com", Username: "bob"})
	if err != nil || !ok || ans.Username != "bob" {
		t.Fatalf("Get with the user only in the query = %+v, %v, %v", ans, ok, err)
	}
}

type queryUserKeyring struct {
	*fakeKeyring
}

func (k queryUserKeyring) search(ctx context.Context, attrs map[string]string) ([]keyringItem, error) {
	withoutUser := maps.Clone(attrs)
	delete(withoutUser, "user")
	return k.fakeKeyring.search(ctx, withoutUser)
}

func TestLibsecretHelperIgnoresIncompleteRequests(t *testing.T) {
	h, k := newTestLibsecret()
	ctx := context.Background()
	if _, ok, err := h.Get(ctx, Query{Host: "example.com"}); ok || err != nil {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	for _, c := range []struct {
		q Query
		a Answer
	}{
		{Query{Host: "example.com"}, Answer{Username: "bob", Password: []byte("pw")}},
		{Query{Protocol: "https"}, Answer{Username: "bob", Password: []byte("pw")}},
		{Query{Protocol: "https", Host: "example.com"}, Answer{Password: []byte("pw")}},
		{Query{Protocol: "https", Host: "example.com"}, Answer{Username: "bob"}},
	} {
		if err := h.Store(ctx, c.q, c.a); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Erase(ctx, Query{}, Answer{}); err != nil {
		t.Fatal(err)
	}
	if k.opens != 0 {
		t.Fatalf("opens = %d, incomplete requests reached the keyring", k.opens)
	}
}

func TestLibsecretHelperEraseRemovesOnlyTheRejectedSecret(t *testing.T) {
	h, k := newTestLibsecret()
	ctx := context.Background()
	q := Query{Protocol: "https", Host: "example.com"}
	if err := h.Store(ctx, q, Answer{Username: "bob", Password: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(ctx, q, Answer{Username: "bob", Password: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if len(k.live()) != 1 {
		t.Fatal("a stale reject removed the new secret")
	}
	if err := h.Erase(ctx, q, Answer{Username: "bob", Password: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	if len(k.live()) != 0 {
		t.Fatal("the rejected secret is still stored")
	}
	if err := h.Erase(ctx, Query{Host: "example.com"}, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
}

func TestLibsecretHelperReportsKeyringFailures(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()
	q := Query{Protocol: "https", Host: "example.com"}
	ans := Answer{Username: "bob", Password: []byte("pw")}

	h, k := newTestLibsecret()
	k.openErr = boom
	if _, _, err := h.Get(ctx, q); !errors.Is(err, boom) {
		t.Fatalf("Get err = %v", err)
	}
	if err := h.Store(ctx, q, ans); !errors.Is(err, boom) {
		t.Fatalf("Store err = %v", err)
	}
	if err := h.Erase(ctx, q, ans); !errors.Is(err, boom) {
		t.Fatalf("Erase lookup err = %v", err)
	}
	if err := h.Erase(ctx, q, Answer{Username: "bob"}); !errors.Is(err, boom) {
		t.Fatalf("Erase open err = %v", err)
	}

	h, k = newTestLibsecret()
	k.searchErr = boom
	if err := h.Erase(ctx, q, Answer{Username: "bob"}); !errors.Is(err, boom) {
		t.Fatalf("Erase search err = %v", err)
	}

	h, k = newTestLibsecret()
	k.storeErr = boom
	if err := h.Store(ctx, q, ans); !errors.Is(err, boom) {
		t.Fatalf("Store err = %v", err)
	}
}
