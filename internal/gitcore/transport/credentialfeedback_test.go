package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type feedbackRecorder struct {
	mu       sync.Mutex
	supply   []Credentials
	asked    []string
	retries  []bool
	approved []string
	rejected []string
}

func describeCredentials(resource string, c Credentials) string {
	return resource + " " + c.Username + ":" + string(c.Password) + string(c.Token)
}

func (f *feedbackRecorder) Credentials(_ context.Context, resource string, retry bool) (Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := min(len(f.asked), len(f.supply)-1)
	f.asked = append(f.asked, resource)
	f.retries = append(f.retries, retry)
	c := f.supply[idx]
	return Credentials{
		Username: c.Username,
		Password: append([]byte(nil), c.Password...),
		Token:    append([]byte(nil), c.Token...),
	}, nil
}

func (f *feedbackRecorder) Approve(_ context.Context, resource string, c Credentials) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved = append(f.approved, describeCredentials(resource, c))
}

func (f *feedbackRecorder) Reject(_ context.Context, resource string, c Credentials) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected = append(f.rejected, describeCredentials(resource, c))
}

func advertisingServer(t *testing.T, accept func(r *http.Request) int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status := accept(r); status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		default:
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(fetchV1ResponseBody("NAK", fakePack(5)))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func acceptBasic(user, password string) func(r *http.Request) int {
	return func(r *http.Request) int {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != password {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	}
}

func advertiseWith(t *testing.T, rawURL string, source CredentialSource) error {
	t.Helper()
	session, err := Dial(t.Context(), rawURL, UploadPack, Options{Credentials: source, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	return err
}

func TestCredentialResourceKeepsSchemeUserHostPortAndPath(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://example.com/org/repo.git", "https://example.com/org/repo.git"},
		{"http://example.com:8080/org/repo.git", "http://example.com:8080/org/repo.git"},
		{"https://bob@example.com/org/repo.git", "https://bob@example.com/org/repo.git"},
		{"https://bob:secret@example.com:8443/repo.git/", "https://bob@example.com:8443/repo.git"},
		{"https://example.com", "https://example.com"},
		{"https://[::1]:8443/repo.git", "https://[::1]:8443/repo.git"},
		{"https://[::1]/repo.git", "https://[::1]/repo.git"},
		{"https://a%40b@example.com/repo.git", "https://a%40b@example.com/repo.git"},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			endpoint, _, err := ParseURL(test.raw)
			if err != nil {
				t.Fatalf("ParseURL returned %v", err)
			}
			got := CredentialResource(endpoint)
			if got != test.want {
				t.Fatalf("CredentialResource = %q, want %q", got, test.want)
			}
			back, password, err := ParseURL(got)
			if err != nil || password != nil {
				t.Fatalf("ParseURL(%q) = %v, password %v", got, err, password)
			}
			if back.Scheme != endpoint.Scheme || back.User != endpoint.User || back.Host != endpoint.Host || back.Port != endpoint.Port {
				t.Fatalf("ParseURL(%q) = %+v, want the parts of %+v", got, back, endpoint)
			}
		})
	}
}

func TestCredentialResourceSeparatesSchemesPortsAndUsers(t *testing.T) {
	seen := map[string]bool{}
	for _, raw := range []string{"https://host/x", "http://host/x", "http://host:8080/x", "https://bob@host/x"} {
		endpoint, _, err := ParseURL(raw)
		if err != nil {
			t.Fatal(err)
		}
		key := CredentialResource(endpoint)
		if seen[key] {
			t.Fatalf("%q shares the credential key %q with another address", raw, key)
		}
		seen[key] = true
	}
}

func TestHTTPAsksForCredentialsWithTheSchemeUserPortAndPathOfTheURL(t *testing.T) {
	server := advertisingServer(t, acceptBasic("bob", "pw"))
	source := &feedbackRecorder{supply: []Credentials{{Username: "bob", Password: []byte("pw")}}}
	address := strings.Replace(server.URL, "http://", "http://bob@", 1) + "/repo.git/"
	if err := advertiseWith(t, address, source); err != nil {
		t.Fatalf("Advertise returned %v", err)
	}
	want := strings.TrimSuffix(address, "/")
	if !slices.Equal(source.asked, []string{want}) {
		t.Fatalf("asked = %v, want [%s]", source.asked, want)
	}
}

func TestHTTPApprovesTheSuppliedCredentialAfterTheServerAcceptsIt(t *testing.T) {
	server := advertisingServer(t, acceptBasic("alice", "hunter2"))
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	if err := advertiseWith(t, server.URL+"/repo.git", source); err != nil {
		t.Fatalf("Advertise returned %v", err)
	}
	resource := server.URL + "/repo.git"
	if !slices.Equal(source.approved, []string{resource + " alice:hunter2"}) {
		t.Fatalf("approved = %v", source.approved)
	}
	if len(source.rejected) != 0 {
		t.Fatalf("rejected = %v, want none", source.rejected)
	}
}

func TestHTTPApprovesAnAcceptedBearerToken(t *testing.T) {
	server := advertisingServer(t, func(r *http.Request) int {
		if r.Header.Get("Authorization") != "Bearer abc123" {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	})
	source := &feedbackRecorder{supply: []Credentials{{Token: []byte("abc123")}}}
	if err := advertiseWith(t, server.URL+"/repo.git", source); err != nil {
		t.Fatalf("Advertise returned %v", err)
	}
	if !slices.Equal(source.approved, []string{server.URL + "/repo.git :abc123"}) {
		t.Fatalf("approved = %v", source.approved)
	}
}

func TestHTTPRejectsEveryCredentialTheServerRefuses(t *testing.T) {
	server := advertisingServer(t, func(*http.Request) int { return http.StatusUnauthorized })
	source := &feedbackRecorder{supply: []Credentials{
		{Username: "alice", Password: []byte("wrong1")},
		{Username: "alice", Password: []byte("wrong2")},
	}}
	err := advertiseWith(t, server.URL+"/repo.git", source)
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Advertise returned %v, want ErrAuthRequired", err)
	}
	resource := server.URL + "/repo.git"
	want := []string{resource + " alice:wrong1", resource + " alice:wrong2"}
	if !slices.Equal(source.rejected, want) {
		t.Fatalf("rejected = %v, want %v", source.rejected, want)
	}
	if len(source.approved) != 0 {
		t.Fatalf("approved = %v, want none", source.approved)
	}
}

func TestHTTPRejectsAWrongCredentialAndApprovesTheCorrectedOne(t *testing.T) {
	server := advertisingServer(t, acceptBasic("alice", "right"))
	source := &feedbackRecorder{supply: []Credentials{
		{Username: "alice", Password: []byte("wrong")},
		{Username: "alice", Password: []byte("right")},
	}}
	if err := advertiseWith(t, server.URL+"/repo.git", source); err != nil {
		t.Fatalf("Advertise returned %v", err)
	}
	resource := server.URL + "/repo.git"
	if !slices.Equal(source.rejected, []string{resource + " alice:wrong"}) {
		t.Fatalf("rejected = %v", source.rejected)
	}
	if !slices.Equal(source.approved, []string{resource + " alice:right"}) {
		t.Fatalf("approved = %v", source.approved)
	}
	if !slices.Equal(source.retries, []bool{false, true}) {
		t.Fatalf("retries = %v, want [false true]", source.retries)
	}
}

func TestHTTPDoesNotApproveACredentialWhenTheServerAnswersNotFound(t *testing.T) {
	server := advertisingServer(t, func(r *http.Request) int {
		if _, _, ok := r.BasicAuth(); !ok {
			return http.StatusUnauthorized
		}
		return http.StatusNotFound
	})
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("pw")}}}
	err := advertiseWith(t, server.URL+"/repo.git", source)
	if !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("Advertise returned %v, want ErrRepositoryNotFound", err)
	}
	if len(source.approved) != 0 || len(source.rejected) != 0 {
		t.Fatalf("approved = %v, rejected = %v, want no feedback", source.approved, source.rejected)
	}
}

func TestHTTPApprovesACredentialOnlyOnceAcrossRequests(t *testing.T) {
	server := uploadPackServer(t, "/repo.git", true)
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("pw")}}}
	fetchThrough(t, server.URL+"/repo.git", Options{Credentials: source})
	if len(source.asked) != 1 || len(source.approved) != 1 || len(source.rejected) != 0 {
		t.Fatalf("asked = %v, approved = %v, rejected = %v", source.asked, source.approved, source.rejected)
	}
}

func TestHTTPRejectsAnApprovedCredentialWhenALaterRequestIsRefused(t *testing.T) {
	server := advertisingServer(t, func(r *http.Request) int {
		if _, _, ok := r.BasicAuth(); !ok || r.Method == http.MethodPost {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	})
	source := &feedbackRecorder{supply: []Credentials{
		{Username: "alice", Password: []byte("first")},
		{Username: "alice", Password: []byte("second")},
	}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: source, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Fetch returned %v, want ErrAuthRequired", err)
	}
	resource := server.URL + "/repo.git"
	if !slices.Equal(source.approved, []string{resource + " alice:first"}) {
		t.Fatalf("approved = %v", source.approved)
	}
	if !slices.Equal(source.rejected, []string{resource + " alice:first", resource + " alice:second"}) {
		t.Fatalf("rejected = %v", source.rejected)
	}
}

func TestHTTPCloseWipesTheSuppliedCredential(t *testing.T) {
	s := newHTTPSession(Endpoint{Scheme: SchemeHTTPS, Host: "example.com", Path: "/repo.git"}, nil, UploadPack, Options{})
	password := []byte("secret")
	s.supplied = &suppliedCredentials{creds: Credentials{Username: "alice", Password: password}}
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}
	if s.supplied != nil || string(password) == "secret" {
		t.Fatalf("supplied = %+v, password = %q, want both wiped", s.supplied, password)
	}
}

func TestHTTPDropsTheURLCredentialsWhenRedirectedToAnotherHost(t *testing.T) {
	var mu sync.Mutex
	var headers []string
	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		default:
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(fetchV1ResponseBody("NAK", fakePack(5)))
		}
	}))
	t.Cleanup(moved.Close)
	old := redirectingServer(t, strings.Replace(moved.URL, "127.0.0.1", "localhost", 1)+"/new.git")
	fetchThrough(t, strings.Replace(old.URL, "http://", "http://alice:hunter2@", 1)+"/old.git", Options{})
	if len(headers) != 2 {
		t.Fatalf("the moved server saw %d requests, want 2", len(headers))
	}
	for _, h := range headers {
		if h != "" {
			t.Fatalf("the moved server received Authorization %q, want none", h)
		}
	}
}
