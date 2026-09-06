package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func httpV1Discovery(service Service, headCaps string, refs [][2]string) []byte {
	b := newPktBuilder()
	b.line("# service=" + string(service) + "\n").flush()
	for i, r := range refs {
		if i == 0 {
			b.line(r[0] + " " + r[1] + "\x00" + headCaps + "\n")
			continue
		}
		b.line(r[0] + " " + r[1] + "\n")
	}
	b.flush()
	return b.bytes()
}

func httpV2Discovery(service Service, capLines []string) []byte {
	b := newPktBuilder()
	b.line("# service=" + string(service) + "\n").flush()
	b.line("version 2\n")
	for _, c := range capLines {
		b.line(c + "\n")
	}
	b.flush()
	return b.bytes()
}

func linesBody(lines []string) []byte {
	b := newPktBuilder()
	for _, l := range lines {
		b.line(l + "\n")
	}
	b.flush()
	return b.bytes()
}

func fetchV1ResponseBody(ack string, pack []byte) []byte {
	b := newPktBuilder()
	b.line(ack + "\n")
	b.raw(sidebandFrame(SidebandPack, pack))
	b.flush()
	return b.bytes()
}

func fetchV2PackfileBody(pack []byte) []byte {
	b := newPktBuilder()
	b.line("packfile\n")
	b.raw(sidebandFrame(SidebandPack, pack))
	b.flush()
	return b.bytes()
}

func reportStatusBody(unpack string, refLines []string) []byte {
	b := newPktBuilder()
	b.line("unpack " + unpack + "\n")
	for _, l := range refLines {
		b.line(l + "\n")
	}
	b.flush()
	return b.bytes()
}

const testHeadCaps = "multi_ack_detailed side-band-64k ofs-delta shallow report-status report-status-v2 delete-refs push-options agent=git/test"

const testPushHeadCaps = "multi_ack_detailed ofs-delta shallow report-status report-status-v2 delete-refs push-options agent=git/test"

func TestHTTPAdvertiseV1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info/refs") || r.URL.Query().Get("service") != string(UploadPack) {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{
			{idOf(1).String(), "HEAD"},
			{idOf(1).String(), "refs/heads/main"},
			{idOf(2).String(), "refs/heads/other"},
		}))
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Version != 1 {
		t.Fatalf("Version = %d, want 1", adv.Version)
	}
	if len(adv.Refs) != 3 {
		t.Fatalf("Refs = %v, want 3 entries", adv.Refs)
	}
	if !adv.Capabilities.Has(CapSideBand64k) {
		t.Fatalf("Capabilities = %v, want side-band-64k", adv.Capabilities.Names())
	}
}

func TestHTTPAdvertiseV2ListsRefsThroughLsRefs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info/refs"):
			if r.Header.Get("Git-Protocol") != "version=2" {
				t.Fatalf("Git-Protocol header = %q, want version=2", r.Header.Get("Git-Protocol"))
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV2Discovery(UploadPack, []string{"ls-refs", "fetch=shallow", "agent=git/test"}))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-upload-pack"):
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "command=ls-refs") {
				t.Fatalf("body = %q, want it to contain command=ls-refs", body)
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(linesBody([]string{
				idOf(1).String() + " HEAD symref-target:refs/heads/main",
				idOf(1).String() + " refs/heads/main",
				idOf(3).String() + " refs/tags/v1 peeled:" + idOf(4).String(),
			}))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Version != 2 {
		t.Fatalf("Version = %d, want 2", adv.Version)
	}
	if adv.Head != "refs/heads/main" {
		t.Fatalf("Head = %q, want refs/heads/main", adv.Head)
	}
	if len(adv.Refs) != 3 {
		t.Fatalf("Refs = %v, want 3 entries", adv.Refs)
	}
	tag, ok := findRefByName(adv.Refs, "refs/tags/v1")
	if !ok || tag.Peeled != idOf(4) {
		t.Fatalf("refs/tags/v1 = %+v, want Peeled %s", tag, idOf(4))
	}
}

func TestHTTPFetchV1DemultiplexesSidebandPack(t *testing.T) {
	pack := fakePack(7)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "want "+idOf(1).String()) {
				t.Fatalf("body = %q, missing want line", body)
			}
			if !strings.Contains(string(body), "done\n") {
				t.Fatalf("body = %q, missing done", body)
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(fetchV1ResponseBody("NAK", pack))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestHTTPFetchV2SendsPackfileAfterDone(t *testing.T) {
	pack := fakePack(9)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV2Discovery(UploadPack, []string{"ls-refs", "fetch=shallow", "side-band-64k"}))
		case r.Method == http.MethodPost && strings.Contains(readBodyPeek(r), "command=ls-refs"):
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(linesBody([]string{idOf(1).String() + " refs/heads/main"}))
		case r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "done\n") {
				t.Fatalf("body = %q, missing done", body)
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(fetchV2PackfileBody(pack))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func readBodyPeek(r *http.Request) string {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	return string(body)
}

type fakeCredentialSource struct {
	retries []bool
	creds   []Credentials
	err     error
}

func (f *fakeCredentialSource) Credentials(_ context.Context, _ string, retry bool) (Credentials, error) {
	f.retries = append(f.retries, retry)
	if f.err != nil {
		return Credentials{}, f.err
	}
	idx := len(f.retries) - 1
	if idx >= len(f.creds) {
		idx = len(f.creds) - 1
	}
	c := f.creds[idx]
	return Credentials{
		Username: c.Username,
		Password: append([]byte(nil), c.Password...),
		Token:    append([]byte(nil), c.Token...),
	}, nil
}

func TestHTTPAdvertiseRetriesAfter401(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		user, pass, ok := r.BasicAuth()
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !ok || user != "alice" || pass != "hunter2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	}))
	defer server.Close()

	creds := &fakeCredentialSource{creds: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if len(creds.retries) != 1 || creds.retries[0] != false {
		t.Fatalf("Credentials calls = %v, want a single call with retry=false", creds.retries)
	}
}

func TestHTTPAdvertiseFailsAfterTwoBadCredentialAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	creds := &fakeCredentialSource{creds: []Credentials{
		{Username: "alice", Password: []byte("wrong1")},
		{Username: "alice", Password: []byte("wrong2")},
	}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Advertise returned %v, want ErrAuthRequired", err)
	}
	if len(creds.retries) != 2 || creds.retries[0] != false || creds.retries[1] != true {
		t.Fatalf("Credentials calls = %v, want [false true]", creds.retries)
	}
}

func TestHTTPAdvertiseWithoutCredentialSourceReturnsErrNoCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("Advertise returned %v, want ErrNoCredentials", err)
	}
}

func TestHTTPAdvertiseForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	session, _ := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	defer func() { _ = session.Close() }()
	_, err := session.Advertise(t.Context())
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Advertise returned %v, want ErrAccessDenied", err)
	}
}

func TestHTTPAdvertiseNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	session, _ := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	defer func() { _ = session.Close() }()
	_, err := session.Advertise(t.Context())
	if !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("Advertise returned %v, want ErrRepositoryNotFound", err)
	}
}

func TestHTTPAdvertiseInternalServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	session, _ := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	defer func() { _ = session.Close() }()
	_, err := session.Advertise(t.Context())
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("Advertise returned %v, want ErrProtocol", err)
	}
}

func TestHTTPAdvertiseWrongContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not a git response"))
	}))
	defer server.Close()
	session, _ := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	defer func() { _ = session.Close() }()
	_, err := session.Advertise(t.Context())
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("Advertise returned %v, want ErrProtocol", err)
	}
}

func TestHTTPAdvertiseTruncatedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		full := httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
		_, _ = w.Write(full[:len(full)-10])
	}))
	defer server.Close()
	session, _ := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	defer func() { _ = session.Close() }()
	_, err := session.Advertise(t.Context())
	if err == nil {
		t.Fatalf("Advertise succeeded on a truncated body")
	}
}

func TestHTTPPushReportStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(ReceivePack, testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), idOf(1).String()+" "+idOf(2).String()+" refs/heads/main") {
				t.Fatalf("body = %q, missing the ref update command", body)
			}
			w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
			_, _ = w.Write(reportStatusBody("ok", []string{"ok refs/heads/main"}))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    strings.NewReader("PACKDATA"),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("UnpackOK = false, want true")
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK || result.Refs[0].Name != "refs/heads/main" {
		t.Fatalf("Refs = %+v", result.Refs)
	}
}

func TestHTTPPushReportStatusV2WithOptionLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(ReceivePack, testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
			_, _ = w.Write(reportStatusBody("ok", []string{
				"ok refs/heads/main",
				"option refname refs/heads/main",
				"ng refs/heads/broken rejected",
			}))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.Push(t.Context(), PushRequest{
		Updates: []Update{
			{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)},
			{Name: "refs/heads/broken", Old: idOf(1), New: idOf(3)},
		},
		Pack: strings.NewReader("PACKDATA"),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 2 {
		t.Fatalf("Refs = %+v, want 2 entries", result.Refs)
	}
	if result.Refs[0].Message != "refname refs/heads/main" {
		t.Fatalf("Refs[0].Message = %q", result.Refs[0].Message)
	}
	if result.Refs[1].OK || result.Refs[1].Message != "rejected" {
		t.Fatalf("Refs[1] = %+v", result.Refs[1])
	}
}

func TestHTTPFetchCancelledByContext(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
			return
		}
		<-block
	}))
	defer server.Close()
	defer close(block)

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	go cancel()
	_, err = session.Fetch(ctx, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded despite a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned %v, want context.Canceled", err)
	}
}

func TestHTTPUsesEmbeddedURLCredentialsWithoutAnUnauthorizedRoundTrip(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "hunter2" {
			t.Fatalf("BasicAuth = (%q, %q, %v), want (alice, hunter2, true)", user, pass, ok)
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	}))
	defer server.Close()

	url := strings.Replace(server.URL, "http://", "http://alice:hunter2@", 1)
	session, err := Dial(t.Context(), url+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if requests != 1 {
		t.Fatalf("server received %d requests, want 1 (no 401 round trip)", requests)
	}
}

func TestHTTPEnsureAdvertisedSkipsASecondDiscoveryRequest(t *testing.T) {
	var discoveries int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			atomic.AddInt32(&discoveries, 1)
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_, _ = w.Write(fetchV1ResponseBody("NAK", fakePack(41)))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	_ = resp.Pack.Close()
	if discoveries != 1 {
		t.Fatalf("server received %d discovery requests, want 1", discoveries)
	}
}

func TestHTTPFetchWithoutSidebandReadsRawPack(t *testing.T) {
	pack := fakePack(43)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, "multi_ack ofs-delta agent=git/test", [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			body := newPktBuilder().line("NAK\n").bytes()
			_, _ = w.Write(append(body, pack...))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestHTTPAdvertiseCredentialSourceError(t *testing.T) {
	boom := errors.New("boom")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	creds := &fakeCredentialSource{err: boom}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Advertise(t.Context())
	if !errors.Is(err, boom) {
		t.Fatalf("Advertise returned %v, want boom", err)
	}
}

func TestHTTPSessionClosedRejectsEveryMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close returned error %v", err)
	}
	if _, err := session.Advertise(t.Context()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Advertise on a closed session returned %v, want ErrProtocol", err)
	}
	if _, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Fetch on a closed session returned %v, want ErrProtocol", err)
	}
	if _, err := session.Push(t.Context(), PushRequest{Pack: strings.NewReader("")}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Push on a closed session returned %v, want ErrProtocol", err)
	}
}

func TestExpectServiceHeaderMismatchedService(t *testing.T) {
	body := newPktBuilder().line("# service=git-receive-pack\n").flush().bytes()
	err := expectServiceHeader(bytes.NewReader(body), UploadPack)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("expectServiceHeader returned %v, want ErrProtocol", err)
	}
}

func TestExpectServiceHeaderMissingFlush(t *testing.T) {
	body := newPktBuilder().line("# service=git-upload-pack\n").line("stray\n").bytes()
	err := expectServiceHeader(bytes.NewReader(body), UploadPack)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("expectServiceHeader returned %v, want ErrProtocol", err)
	}
}

func TestExpectServiceHeaderEmptyStream(t *testing.T) {
	err := expectServiceHeader(bytes.NewReader(nil), UploadPack)
	if err == nil {
		t.Fatalf("expectServiceHeader succeeded on an empty stream")
	}
}

func TestHTTPPushWrongContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(ReceivePack, testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("not the right content type"))
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    strings.NewReader("PACKDATA"),
	})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("Push returned %v, want ErrProtocol", err)
	}
}

func TestHTTPRoundWrongContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
		case http.MethodPost:
			w.Header().Set("Content-Type", "text/plain")
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("Fetch returned %v, want ErrProtocol", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestResponse(status int, contentType string, body []byte) *http.Response {
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body))}
}

func TestHTTPAttemptFailsOnInvalidRequestURL(t *testing.T) {
	s := newHTTPSession(Endpoint{Scheme: SchemeHTTP, Host: "example.com"}, nil, UploadPack, Options{})
	if _, err := s.attempt(t.Context(), http.MethodGet, "http://\x00", "", nil, nil); err == nil {
		t.Fatalf("attempt succeeded with an invalid request url")
	}
}

func TestHTTPAttemptUsesCustomUserAgent(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{UserAgent: "custom-agent/1.0", Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if gotUA != "custom-agent/1.0" {
		t.Fatalf("User-Agent = %q, want %q", gotUA, "custom-agent/1.0")
	}
}

func TestHTTPDoRequestFailsWhenRetryAttemptFails(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return newTestResponse(http.StatusUnauthorized, "", nil), nil
		}
		return nil, boom
	})
	creds := &fakeCredentialSource{creds: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	session, err := Dial(t.Context(), "https://example.invalid/repo.git", UploadPack, Options{
		Credentials: creds,
		HTTPClient:  &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Advertise(t.Context())
	if !errors.Is(err, boom) {
		t.Fatalf("Advertise returned %v, want boom", err)
	}
}

func TestExpectServiceHeaderTruncatedAfterServiceLine(t *testing.T) {
	body := newPktBuilder().line("# service=" + string(UploadPack) + "\n").bytes()
	body = append(body, []byte("000")...)
	if err := expectServiceHeader(bytes.NewReader(body), UploadPack); err == nil {
		t.Fatalf("expectServiceHeader succeeded on a stream truncated after the service line")
	}
}

func TestHTTPAdvertiseFailsWhenServiceHeaderMismatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(newPktBuilder().line("# service=git-receive-pack\n").flush().bytes())
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("Advertise returned %v, want ErrProtocol", err)
	}
}

func TestHTTPAdvertiseV2FailsWhenLsRefsFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write(httpV2Discovery(UploadPack, []string{"ls-refs"}))
		case http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded despite ls-refs failing")
	}
}

func TestHTTPRoundFailsWhenDoRequestFails(t *testing.T) {
	boom := errors.New("boom")
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return newTestResponse(http.StatusOK, "application/x-git-upload-pack-advertisement",
				httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})), nil
		}
		return nil, boom
	})
	session, err := Dial(t.Context(), "https://example.invalid/repo.git", UploadPack, Options{
		Version:    1,
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Fetch returned %v, want boom", err)
	}
}

func TestHTTPPushFailsWhenAdvertiseFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Push(t.Context(), PushRequest{Pack: strings.NewReader("")}); err == nil {
		t.Fatalf("Push succeeded despite advertise failing")
	}
}

func TestHTTPPushFailsWhenPackReadFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(ReceivePack, testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	}))
	defer server.Close()

	session, err := Dial(t.Context(), server.URL+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	boom := errors.New("boom")
	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    errReader{err: boom},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Push returned %v, want boom", err)
	}
}

func TestHTTPPushFailsWhenDoRequestFails(t *testing.T) {
	boom := errors.New("boom")
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return newTestResponse(http.StatusOK, "application/x-git-receive-pack-advertisement",
				httpV1Discovery(ReceivePack, testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})), nil
		}
		return nil, boom
	})
	session, err := Dial(t.Context(), "https://example.invalid/repo.git", ReceivePack, Options{
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    strings.NewReader("PACKDATA"),
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Push returned %v, want boom", err)
	}
}

func TestHTTPAuthenticateWithBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer abc123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	}))
	defer server.Close()

	creds := &fakeCredentialSource{creds: []Credentials{{Token: []byte("abc123")}}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
}
