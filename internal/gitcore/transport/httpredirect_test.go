package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type resourceRecorder struct {
	mu        sync.Mutex
	resources []string
}

func (c *resourceRecorder) Credentials(_ context.Context, resource string, _ bool) (Credentials, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resources = append(c.resources, resource)
	return Credentials{Username: "alice", Password: []byte("secret")}, nil
}

func uploadPackServer(t *testing.T, prefix string, needAuth bool) *httptest.Server {
	t.Helper()
	pack := fakePack(5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if needAuth {
			if _, _, ok := r.BasicAuth(); !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == prefix+"/info/refs":
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case r.Method == http.MethodPost && r.URL.Path == prefix+"/git-upload-pack":
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(fetchV1ResponseBody("NAK", pack))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func redirectingServer(t *testing.T, target string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/old.git/info/refs" {
			t.Errorf("%s %s went to the old address", r.Method, r.URL)
		}
		http.Redirect(w, r, target+"/info/refs?service=git-upload-pack", http.StatusMovedPermanently)
	}))
	t.Cleanup(server.Close)
	return server
}

func fetchThrough(t *testing.T, address string, opts Options) {
	t.Helper()
	opts.Version = 1
	session, err := Dial(t.Context(), address, UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	if _, err := io.ReadAll(resp.Pack); err != nil {
		t.Fatalf("reading the pack returned error %v", err)
	}
}

func TestHTTPKeepsTalkingToTheAddressDiscoveryWasRedirectedTo(t *testing.T) {
	moved := uploadPackServer(t, "/new.git", false)
	fetchThrough(t, redirectingServer(t, moved.URL+"/new.git").URL+"/old.git", Options{})
}

func TestHTTPAsksForCredentialsOfTheHostItWasRedirectedTo(t *testing.T) {
	moved := uploadPackServer(t, "/new.git", true)
	old := redirectingServer(t, strings.Replace(moved.URL, "127.0.0.1", "localhost", 1)+"/new.git")
	creds := &resourceRecorder{}
	fetchThrough(t, old.URL+"/old.git", Options{Credentials: creds})
	if len(creds.resources) != 1 || creds.resources[0] != "localhost/new.git" {
		t.Fatalf("credentials were asked for %v, want the redirected host", creds.resources)
	}
}

func TestHTTPIgnoresAResponseWithoutItsRequest(t *testing.T) {
	s := newHTTPSession(Endpoint{Scheme: "https", Host: "example.com", Path: "/repo.git"}, nil, UploadPack, Options{})
	s.adoptRedirect(nil)
	if s.baseURL != "https://example.com/repo.git" {
		t.Fatalf("baseURL = %q", s.baseURL)
	}
}
