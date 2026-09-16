package credential

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestWincredTargetFollowsTheHelperFormat(t *testing.T) {
	cases := []struct {
		q    Query
		want string
	}{
		{Query{Protocol: "https", Host: "example.com", Username: "bob"}, "git:https://bob@example.com"},
		{Query{Protocol: "https", Host: "example.com:8443", Path: "org/repo.git", Username: "a@b.c"}, "git:https://a@b.c@example.com:8443/org/repo.git"},
		{Query{Protocol: "https", Host: "example.com"}, "git:https://example.com"},
	}
	for _, c := range cases {
		if got := wincredTarget(c.q); got != c.want {
			t.Errorf("wincredTarget(%+v) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestWincredMatchesLikeTheHelper(t *testing.T) {
	cases := []struct {
		name   string
		cred   winCredential
		q      Query
		secret string
		want   bool
	}{
		{"own entry", winCredential{target: "git:https://bob@example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com", Username: "bob"}, "", true},
		{"any user", winCredential{target: "git:https://bob@example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", true},
		{"manager entry is skipped when the user is known", winCredential{target: "git:https://example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com", Username: "bob"}, "", false},
		{"manager entry without user in target", winCredential{target: "git:https://example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", true},
		{"email user", winCredential{target: "git:https://a@b.c@example.com", userName: "a@b.c"}, Query{Protocol: "https", Host: "example.com", Username: "a@b.c"}, "", true},
		{"other user", winCredential{target: "git:https://bob@example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com", Username: "eve"}, "", false},
		{"user field differs from target", winCredential{target: "git:https://bob@example.com", userName: "eve"}, Query{Protocol: "https", Host: "example.com", Username: "bob"}, "", false},
		{"other host", winCredential{target: "git:https://bob@example.org", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", false},
		{"other protocol", winCredential{target: "git:http://example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", false},
		{"not a git target", winCredential{target: "other:https://example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", false},
		{"missing scheme separator", winCredential{target: "git:https", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", false},
		{"missing colon", winCredential{target: "gitlab", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", false},
		{"entry with path when none asked", winCredential{target: "git:https://example.com/org/repo", userName: "bob"}, Query{Protocol: "https", Host: "example.com"}, "", true},
		{"path asked and stored", winCredential{target: "git:https://example.com/org/repo", userName: "bob"}, Query{Protocol: "https", Host: "example.com", Path: "org/repo"}, "", true},
		{"path asked but host only stored", winCredential{target: "git:https://example.com", userName: "bob"}, Query{Protocol: "https", Host: "example.com", Path: "org/repo"}, "", false},
		{"password matches", winCredential{target: "git:https://example.com", userName: "bob", blob: utf16LEFromUTF8([]byte("pw"))}, Query{Protocol: "https", Host: "example.com"}, "pw", true},
		{"password differs", winCredential{target: "git:https://example.com", userName: "bob", blob: utf16LEFromUTF8([]byte("pw"))}, Query{Protocol: "https", Host: "example.com"}, "old", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wincredMatches(c.cred, c.q, []byte(c.secret)); got != c.want {
				t.Fatalf("wincredMatches = %v, want %v", got, c.want)
			}
		})
	}
}

func TestWincredPasswordTakesTheFirstLineOfTheBlob(t *testing.T) {
	cases := map[string]string{
		"pw":                              "pw",
		"pw\r\noauth_refresh_token=token": "pw",
		"\r\npw\nrest":                    "pw",
		"":                                "",
		"\r\n":                            "",
		"пароль":                          "пароль",
	}
	for blob, want := range cases {
		got := wincredPassword(utf16LEFromUTF8([]byte(blob)))
		if got == nil || string(got) != want {
			t.Errorf("wincredPassword(%q) = %q, want %q", blob, got, want)
		}
	}
}

func newTestWincred() (*wincredHelper, *fakeCredentialManager) {
	manager := &fakeCredentialManager{}
	return &wincredHelper{name: "wincred", manager: manager}, manager
}

func TestWincredHelperStoresAndFindsACredential(t *testing.T) {
	h, manager := newTestWincred()
	ctx := context.Background()
	q := Query{Protocol: "https", Host: "example.com"}
	if err := h.Store(ctx, q, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if len(manager.creds) != 1 {
		t.Fatalf("creds = %+v", manager.creds)
	}
	stored := manager.creds[0]
	if stored.target != "git:https://bob@example.com" || stored.userName != "bob" || stored.comment != savedByHelperComment || string(utf8FromUTF16LE(stored.blob)) != "pw" {
		t.Fatalf("stored = %+v", stored)
	}
	ans, ok, err := h.Get(ctx, q)
	if err != nil || !ok || ans.Username != "bob" || string(ans.Password) != "pw" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if !slices.Equal(manager.filters, []string{wincredFilter}) {
		t.Fatalf("filters = %q", manager.filters)
	}
	if h.Name() != "wincred" {
		t.Fatalf("Name = %q", h.Name())
	}
}

func TestWincredHelperIgnoresIncompleteRequests(t *testing.T) {
	h, manager := newTestWincred()
	ctx := context.Background()
	if _, ok, err := h.Get(ctx, Query{Host: "example.com"}); ok || err != nil {
		t.Fatalf("Get without protocol = %v, %v", ok, err)
	}
	if err := h.Store(ctx, Query{Protocol: "https", Host: "example.com"}, Answer{Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store(ctx, Query{Protocol: "https", Host: "example.com"}, Answer{Username: "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store(ctx, Query{Protocol: "https"}, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(ctx, Query{Host: "example.com"}, Answer{}); err != nil {
		t.Fatal(err)
	}
	if len(manager.creds) != 0 || len(manager.filters) != 0 {
		t.Fatalf("incomplete requests reached the credential manager: %+v %q", manager.creds, manager.filters)
	}
}

func TestWincredHelperGetMissesWhenNothingMatches(t *testing.T) {
	h, manager := newTestWincred()
	manager.add("git:https://bob@example.org", "bob", "pw")
	if _, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"}); ok || err != nil {
		t.Fatalf("Get = %v, %v", ok, err)
	}
}

func TestWincredHelperEraseRemovesOnlyTheRejectedCredential(t *testing.T) {
	h, manager := newTestWincred()
	manager.add("git:https://bob@example.com", "bob", "old")
	manager.add("git:https://example.com", "bob", "new")
	manager.add("git:https://eve@example.com", "eve", "old")
	err := h.Erase(context.Background(), Query{Protocol: "https", Host: "example.com"}, Answer{Username: "bob", Password: []byte("old")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(manager.removed, []string{"git:https://bob@example.com"}) {
		t.Fatalf("removed = %q", manager.removed)
	}
}

func TestWincredHelperEraseWithoutPasswordRemovesEveryMatch(t *testing.T) {
	h, manager := newTestWincred()
	manager.add("git:https://bob@example.com", "bob", "old")
	manager.add("git:https://example.com", "bob", "new")
	if err := h.Erase(context.Background(), Query{Protocol: "https", Host: "example.com"}, Answer{}); err != nil {
		t.Fatal(err)
	}
	if len(manager.creds) != 0 {
		t.Fatalf("creds = %+v", manager.creds)
	}
}

func TestWincredHelperReportsCredentialManagerFailures(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()
	q := Query{Protocol: "https", Host: "example.com"}
	ans := Answer{Username: "bob", Password: []byte("pw")}

	h, manager := newTestWincred()
	manager.enumerateErr = boom
	if _, _, err := h.Get(ctx, q); !errors.Is(err, boom) {
		t.Fatalf("Get err = %v", err)
	}
	if err := h.Erase(ctx, q, ans); !errors.Is(err, boom) {
		t.Fatalf("Erase err = %v", err)
	}

	h, manager = newTestWincred()
	manager.writeErr = boom
	if err := h.Store(ctx, q, ans); !errors.Is(err, boom) {
		t.Fatalf("Store err = %v", err)
	}

	h, manager = newTestWincred()
	manager.add("git:https://bob@example.com", "bob", "pw")
	manager.removeErr = boom
	if err := h.Erase(ctx, q, ans); !errors.Is(err, boom) {
		t.Fatalf("Erase remove err = %v", err)
	}
}
