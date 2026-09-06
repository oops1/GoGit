package remote

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func loadTestConfig(t *testing.T, text string) *config.Config {
	t.Helper()
	path := mustWriteFile(t, filepath.Join(t.TempDir(), "gitconfig"), text)
	cfg, err := config.Load(config.Options{NoSystem: true, GlobalFile: path})
	if err != nil {
		t.Fatalf("config.Load returned error %v", err)
	}
	return cfg
}

func TestLoadReturnsTheNamedRemote(t *testing.T) {
	cfg := loadTestConfig(t, "[remote \"origin\"]\n\turl = https://example.com/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	rem, err := Load(cfg, "origin")
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if rem.Name != "origin" || rem.FetchURL() != "https://example.com/repo.git" {
		t.Fatalf("Load returned %+v", rem)
	}
	if len(rem.Fetch) != 1 || rem.Fetch[0].String() != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("Load returned fetch refspecs %v", rem.Fetch)
	}
}

func TestLoadFailsForAMissingRemote(t *testing.T) {
	cfg := loadTestConfig(t, "")
	if _, err := Load(cfg, "origin"); !errors.Is(err, ErrNoRemote) {
		t.Fatalf("Load returned %v, want %v", err, ErrNoRemote)
	}
}

func TestLoadFailsForAMalformedRefspec(t *testing.T) {
	cfg := loadTestConfig(t, "[remote \"origin\"]\n\turl = https://example.com/repo.git\n\tfetch = refs/heads/a:refs/b:refs/c\n")
	if _, err := Load(cfg, "origin"); err == nil {
		t.Fatal("Load returned no error for a malformed refspec")
	}
}

func TestLoadFailsForAMalformedPushRefspec(t *testing.T) {
	cfg := loadTestConfig(t, "[remote \"origin\"]\n\turl = https://example.com/repo.git\n\tpush = refs/heads/a:refs/b:refs/c\n")
	if _, err := Load(cfg, "origin"); err == nil {
		t.Fatal("Load returned no error for a malformed push refspec")
	}
}

func TestListReturnsEveryConfiguredRemote(t *testing.T) {
	cfg := loadTestConfig(t, "[remote \"origin\"]\n\turl = https://example.com/o.git\n"+
		"[remote \"upstream\"]\n\turl = https://example.com/u.git\n")
	remotes := List(cfg)
	if len(remotes) != 2 {
		t.Fatalf("List returned %d remotes, want 2", len(remotes))
	}
}

func TestListSkipsARemoteWithAMalformedRefspec(t *testing.T) {
	cfg := loadTestConfig(t, "[remote \"origin\"]\n\turl = https://example.com/o.git\n\tfetch = refs/heads/a:refs/b:refs/c\n"+
		"[remote \"upstream\"]\n\turl = https://example.com/u.git\n")
	remotes := List(cfg)
	if len(remotes) != 1 || remotes[0].Name != "upstream" {
		t.Fatalf("List returned %+v, want only upstream", remotes)
	}
}

func TestFetchURLReturnsEmptyStringWithoutAnyURL(t *testing.T) {
	rem := Remote{}
	if got := rem.FetchURL(); got != "" {
		t.Fatalf("FetchURL returned %q, want empty", got)
	}
}

func TestFetchURLReturnsTheFirstURL(t *testing.T) {
	rem := Remote{URLs: []string{"a", "b"}}
	if got := rem.FetchURL(); got != "a" {
		t.Fatalf("FetchURL returned %q, want %q", got, "a")
	}
}

func TestPushURLFallsBackToFetchURL(t *testing.T) {
	rem := Remote{URLs: []string{"a"}}
	if got := rem.PushURL(); got != "a" {
		t.Fatalf("PushURL returned %q, want %q", got, "a")
	}
}

func TestPushURLPrefersItsOwnURL(t *testing.T) {
	rem := Remote{URLs: []string{"a"}, PushURLs: []string{"b"}}
	if got := rem.PushURL(); got != "b" {
		t.Fatalf("PushURL returned %q, want %q", got, "b")
	}
}
