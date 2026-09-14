package credential

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStoreHelperStoreWritesTheAnsweredUsernameWhenTheQueryHasNone(t *testing.T) {
	h, path := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com"}
	if err := h.Store(context.Background(), q, Answer{Username: "bob", Password: []byte("pw")}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "https://bob:pw@example.com" {
		t.Fatalf("credentials file = %q, want the answered username in the line", got)
	}
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "bob"})
	if err != nil || !ok || string(ans.Password) != "pw" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
}

func TestStoreHelperStoreReplacesOnlyTheAnsweredUsersEntry(t *testing.T) {
	h, _ := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("pw1")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store(context.Background(), q, Answer{Username: "bob", Password: []byte("pw2")}); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[string]string{"alice": "pw1", "bob": "pw2"} {
		ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Username: user})
		if err != nil || !ok || string(ans.Password) != want {
			t.Fatalf("Get(%s) = %+v, %v, %v, want %s", user, ans, ok, err, want)
		}
	}
}

func TestStoreHelperEraseKeepsOtherUsersWhenTheRejectedUsernameIsAnswered(t *testing.T) {
	h, _ := newTestStoreHelper(t)
	host := Query{Protocol: "https", Host: "example.com"}
	if err := h.Store(context.Background(), host, Answer{Username: "alice", Password: []byte("pw1")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store(context.Background(), host, Answer{Username: "bob", Password: []byte("pw2")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(context.Background(), host, Answer{Username: "alice", Password: []byte("pw1")}); err != nil {
		t.Fatalf("Erase returned %v", err)
	}
	if _, ok, _ := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "alice"}); ok {
		t.Fatal("the rejected entry survived")
	}
	if _, ok, _ := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "bob"}); !ok {
		t.Fatal("another user's entry was erased")
	}
}

func TestStoreHelperEraseKeepsAnEntryWhosePasswordDiffersFromTheRejectedOne(t *testing.T) {
	h, _ := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("fresh")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(context.Background(), q, Answer{Username: "alice", Password: []byte("stale")}); err != nil {
		t.Fatalf("Erase returned %v", err)
	}
	ans, ok, err := h.Get(context.Background(), q)
	if err != nil || !ok || string(ans.Password) != "fresh" {
		t.Fatalf("Get = %+v, %v, %v, want the newer password to survive", ans, ok, err)
	}
}
