package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/transport"
)

func swapLocalDial(t *testing.T, replacement func(context.Context, string, transport.Service, transport.Options) (transport.Session, error)) {
	t.Helper()
	original := localDial
	localDial = replacement
	t.Cleanup(func() { localDial = original })
}

func swapNetworkDial(t *testing.T, replacement func(context.Context, string, transport.Service, transport.Options) (transport.Session, error)) {
	t.Helper()
	original := networkDial
	networkDial = replacement
	t.Cleanup(func() { networkDial = original })
}

func TestDialAnyRoutesPlainPathToLocalTransport(t *testing.T) {
	var gotPath string
	var gotService transport.Service
	swapLocalDial(t, func(_ context.Context, path string, service transport.Service, _ transport.Options) (transport.Session, error) {
		gotPath, gotService = path, service
		return &fakeSession{}, nil
	})
	swapNetworkDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("networkDial should not be called for a local path")
		return nil, nil
	})

	if _, err := dialAny(t.Context(), "/srv/repos/example.git", transport.UploadPack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if gotPath != "/srv/repos/example.git" {
		t.Fatalf("path = %q, want /srv/repos/example.git", gotPath)
	}
	if gotService != transport.UploadPack {
		t.Fatalf("service = %q, want %q", gotService, transport.UploadPack)
	}
}

func TestDialAnyRoutesWindowsDrivePathToLocalTransport(t *testing.T) {
	var gotPath string
	swapLocalDial(t, func(_ context.Context, path string, _ transport.Service, _ transport.Options) (transport.Session, error) {
		gotPath = path
		return &fakeSession{}, nil
	})
	swapNetworkDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("networkDial should not be called for a windows path")
		return nil, nil
	})

	if _, err := dialAny(t.Context(), `C:\repos\example.git`, transport.UploadPack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if gotPath != `C:\repos\example.git` {
		t.Fatalf("path = %q, want C:\\repos\\example.git", gotPath)
	}
}

func TestDialAnyRoutesRelativePathToLocalTransport(t *testing.T) {
	var gotPath string
	swapLocalDial(t, func(_ context.Context, path string, _ transport.Service, _ transport.Options) (transport.Session, error) {
		gotPath = path
		return &fakeSession{}, nil
	})
	swapNetworkDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("networkDial should not be called for a relative path")
		return nil, nil
	})

	if _, err := dialAny(t.Context(), "../sibling/repo", transport.UploadPack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if gotPath != "../sibling/repo" {
		t.Fatalf("path = %q, want ../sibling/repo", gotPath)
	}
}

func TestDialAnyRoutesFileURLToLocalTransportStrippingScheme(t *testing.T) {
	var gotPath string
	swapLocalDial(t, func(_ context.Context, path string, _ transport.Service, _ transport.Options) (transport.Session, error) {
		gotPath = path
		return &fakeSession{}, nil
	})
	swapNetworkDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("networkDial should not be called for a file url")
		return nil, nil
	})

	if _, err := dialAny(t.Context(), "file:///srv/repos/example.git", transport.UploadPack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if gotPath != "/srv/repos/example.git" {
		t.Fatalf("path = %q, want /srv/repos/example.git", gotPath)
	}
}

func TestDialAnyRoutesHTTPURLToNetworkTransport(t *testing.T) {
	var gotURL string
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("localDial should not be called for an http url")
		return nil, nil
	})
	swapNetworkDial(t, func(_ context.Context, rawURL string, _ transport.Service, _ transport.Options) (transport.Session, error) {
		gotURL = rawURL
		return &fakeSession{}, nil
	})

	if _, err := dialAny(t.Context(), "https://example.com/repo.git", transport.ReceivePack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if gotURL != "https://example.com/repo.git" {
		t.Fatalf("url = %q, want https://example.com/repo.git", gotURL)
	}
}

func TestDialAnyRoutesSCPLikeAddressToNetworkTransport(t *testing.T) {
	var gotURL string
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("localDial should not be called for an scp-like address")
		return nil, nil
	})
	swapNetworkDial(t, func(_ context.Context, rawURL string, _ transport.Service, _ transport.Options) (transport.Session, error) {
		gotURL = rawURL
		return &fakeSession{}, nil
	})

	const address = "git@example.com:org/repo.git"
	if _, err := dialAny(t.Context(), address, transport.UploadPack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if gotURL != address {
		t.Fatalf("url = %q, want %s", gotURL, address)
	}
}

func TestDialAnyRoutesSSHSchemeToNetworkTransport(t *testing.T) {
	called := false
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("localDial should not be called for an ssh url")
		return nil, nil
	})
	swapNetworkDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		called = true
		return &fakeSession{}, nil
	})

	if _, err := dialAny(t.Context(), "ssh://git@example.com/repo.git", transport.UploadPack, transport.Options{}); err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	if !called {
		t.Fatalf("networkDial was not called")
	}
}

func TestDialAnyPropagatesInvalidURLError(t *testing.T) {
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("localDial should not be called for an invalid url")
		return nil, nil
	})
	swapNetworkDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		t.Fatalf("networkDial should not be called for an invalid url")
		return nil, nil
	})

	_, err := dialAny(t.Context(), "", transport.UploadPack, transport.Options{})
	if !errors.Is(err, transport.ErrInvalidURL) {
		t.Fatalf("err = %v, want ErrInvalidURL", err)
	}
}

func TestDialAnyPropagatesLocalDialError(t *testing.T) {
	wantErr := errors.New("boom")
	swapLocalDial(t, func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		return nil, wantErr
	})
	_, err := dialAny(t.Context(), "/srv/repos/example.git", transport.UploadPack, transport.Options{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestDialAnyOpensRealLocalRepository(t *testing.T) {
	r := newTestRepo(t, "")
	path := r.GitDir()

	session, err := dialAny(t.Context(), path, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("dialAny returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
}
