package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func captureDialOptions(t *testing.T, err error) *transport.Options {
	t.Helper()
	captured := new(transport.Options)
	previous := dial
	dial = func(_ context.Context, _ string, _ transport.Service, opts transport.Options) (transport.Session, error) {
		*captured = opts
		return nil, err
	}
	t.Cleanup(func() { dial = previous })
	return captured
}

func swapLoadUserConfig(t *testing.T, replacement func() (*config.Config, error)) {
	t.Helper()
	previous := loadUserConfig
	loadUserConfig = replacement
	t.Cleanup(func() { loadUserConfig = previous })
}

func TestFetchHandsTheRepositoryConfigAndRemoteNameToTheTransport(t *testing.T) {
	r := newTestRepo(t, "[http]\n\tproxy = proxy.example:3128\n")
	boom := errors.New("boom")
	captured := captureDialOptions(t, boom)
	_, err := Fetch(t.Context(), r, Remote{Name: "origin", URLs: []string{"https://example.com/repo.git"}}, FetchOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("Fetch returned %v, want boom", err)
	}
	if captured.Config != r.Config() || captured.RemoteName != "origin" {
		t.Fatalf("transport options = %+v, want the repository config and remote name", captured)
	}
}

func TestPushHandsTheRepositoryConfigAndRemoteNameToTheTransport(t *testing.T) {
	r := newTestRepo(t, "")
	boom := errors.New("boom")
	captured := captureDialOptions(t, boom)
	_, err := Push(t.Context(), r, Remote{Name: "upstream", URLs: []string{"https://example.com/repo.git"}}, PushOptions{
		Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"),
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Push returned %v, want boom", err)
	}
	if captured.Config != r.Config() || captured.RemoteName != "upstream" {
		t.Fatalf("transport options = %+v, want the repository config and remote name", captured)
	}
}

func TestLsRemoteReadsTheUserConfigWhenNoneIsGiven(t *testing.T) {
	cfg := newTestRepo(t, "").Config()
	swapLoadUserConfig(t, func() (*config.Config, error) { return cfg, nil })
	boom := errors.New("boom")
	captured := captureDialOptions(t, boom)
	if _, err := LsRemote(t.Context(), "https://example.com/repo.git", transport.Options{}); !errors.Is(err, boom) {
		t.Fatalf("LsRemote returned %v, want boom", err)
	}
	if captured.Config != cfg {
		t.Fatalf("LsRemote passed config %p, want the user config %p", captured.Config, cfg)
	}
}

func TestLsRemoteKeepsTheGivenConfig(t *testing.T) {
	cfg := newTestRepo(t, "").Config()
	swapLoadUserConfig(t, func() (*config.Config, error) {
		t.Fatalf("LsRemote loaded the user config although a config was given")
		return nil, nil
	})
	captured := captureDialOptions(t, errors.New("boom"))
	_, _ = LsRemote(t.Context(), "https://example.com/repo.git", transport.Options{Config: cfg})
	if captured.Config != cfg {
		t.Fatalf("LsRemote passed config %p, want %p", captured.Config, cfg)
	}
}

func TestLsRemoteFailsWhenTheUserConfigCannotBeLoaded(t *testing.T) {
	boom := errors.New("broken config")
	swapLoadUserConfig(t, func() (*config.Config, error) { return nil, boom })
	if _, err := LsRemote(t.Context(), "https://example.com/repo.git", transport.Options{}); !errors.Is(err, boom) {
		t.Fatalf("LsRemote returned %v, want the config error", err)
	}
}

func TestDialAnyRefusesAProtocolForbiddenByTheConfig(t *testing.T) {
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("localDial should not be called for a forbidden protocol")
		return nil, nil
	})
	cfg := newTestRepo(t, "[protocol \"file\"]\n\tallow = never\n").Config()
	if _, err := dialAny(t.Context(), "/srv/repos/example.git", transport.UploadPack, transport.Options{Config: cfg}); !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("dialAny returned %v, want ErrProtocolNotAllowed", err)
	}
}

func TestDialAnyRefusesALocalRepositoryTheUserDidNotAskFor(t *testing.T) {
	t.Setenv("GIT_PROTOCOL_FROM_USER", "1")
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("localDial should not be called for a request that is not from the user")
		return nil, nil
	})
	cfg := newTestRepo(t, "").Config()
	if _, err := dialAny(t.Context(), "/srv/repos/example.git", transport.UploadPack, transport.Options{Config: cfg, NotFromUser: true}); !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("dialAny returned %v, want ErrProtocolNotAllowed", err)
	}
}
