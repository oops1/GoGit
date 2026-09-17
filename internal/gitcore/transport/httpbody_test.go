package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type postRecord struct {
	encoding         string
	transferEncoding []string
	contentLength    int64
	body             []byte
}

type postRecorder struct {
	mu    sync.Mutex
	posts []postRecord
}

func (p *postRecorder) record(r *http.Request) postRecord {
	body, _ := io.ReadAll(r.Body)
	rec := postRecord{
		encoding:         r.Header.Get("Content-Encoding"),
		transferEncoding: r.TransferEncoding,
		contentLength:    r.ContentLength,
		body:             body,
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posts = append(p.posts, rec)
	return rec
}

func (p *postRecorder) all() []postRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.posts)
}

func pushServer(t *testing.T, handlePost http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
			_, _ = w.Write(httpV1Discovery(ReceivePack, testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
			return
		}
		handlePost(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func writePushResult(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	_, _ = w.Write(reportStatusBody("ok", []string{"ok refs/heads/main"}))
}

func pushThrough(t *testing.T, rawURL string, opts Options, pack io.Reader) (*PushResult, error) {
	t.Helper()
	session, err := Dial(t.Context(), rawURL, ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	return session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    pack,
	})
}

func largePack(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i * 7)
	}
	return data
}

func swapPushSpool(t *testing.T, create func() (*os.File, error)) {
	t.Helper()
	original := createPushSpool
	createPushSpool = create
	t.Cleanup(func() { createPushSpool = original })
}

func swapDiscoveryHeaderTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	original := discoveryHeaderTimeout
	discoveryHeaderTimeout = d
	t.Cleanup(func() { discoveryHeaderTimeout = original })
}

func TestLowSpeedWatchAbortsWhenTooFewBytesArrive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		watch := startLowSpeedWatch(100, 2, cancel)
		defer watch.stop()
		watch.add(150)
		time.Sleep(2*time.Second + time.Millisecond)
		synctest.Wait()
		if !errors.Is(context.Cause(ctx), ErrLowSpeed) {
			t.Fatalf("cause = %v, want ErrLowSpeed", context.Cause(ctx))
		}
	})
}

func TestLowSpeedWatchKeepsAFastTransferAlive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		watch := startLowSpeedWatch(100, 2, cancel)
		for range 3 {
			watch.add(250)
			time.Sleep(2 * time.Second)
			synctest.Wait()
		}
		if ctx.Err() != nil {
			t.Fatalf("a fast transfer was aborted: %v", context.Cause(ctx))
		}
		watch.stop()
		watch.stop()
		synctest.Wait()
	})
}

func TestLowSpeedWatchIsOffUnlessBothSettingsArePositive(t *testing.T) {
	for _, pair := range [][2]int64{{0, 5}, {5, 0}} {
		watch := startLowSpeedWatch(pair[0], pair[1], func(error) { t.Fatalf("abort called") })
		if watch != nil {
			t.Fatalf("startLowSpeedWatch(%d, %d) started a watch", pair[0], pair[1])
		}
		watch.add(1)
		watch.stop()
	}
}

func TestTransferErrorPrefersTheWatchdogCause(t *testing.T) {
	plain := errors.New("read failed")
	tests := []struct {
		cause error
		want  error
	}{
		{nil, plain},
		{context.Canceled, plain},
		{ErrLowSpeed, ErrLowSpeed},
		{fmt.Errorf("wrapped: %w", ErrHeaderTimeout), ErrHeaderTimeout},
	}
	for _, tt := range tests {
		if got := transferError(plain, tt.cause); !errors.Is(got, tt.want) {
			t.Errorf("transferError(%v) = %v, want %v", tt.cause, got, tt.want)
		}
	}
}

func TestStreamBodyCanBeOpenedOnlyOnce(t *testing.T) {
	body := streamBody(strings.NewReader("data"))
	if _, err := body.open(); err != nil {
		t.Fatalf("first open returned error %v", err)
	}
	if _, err := body.open(); !errors.Is(err, errBodyNotReplayable) {
		t.Fatalf("second open returned %v, want errBodyNotReplayable", err)
	}
}

func TestHTTPAttemptFailsWhenTheBodyCannotBeReopened(t *testing.T) {
	s, err := newHTTPSession(Endpoint{Scheme: SchemeHTTP, Host: "127.0.0.1", Port: "1"}, nil, ReceivePack, Options{})
	if err != nil {
		t.Fatalf("newHTTPSession returned error %v", err)
	}
	body := streamBody(strings.NewReader("data"))
	_, _ = body.open()
	if _, err := s.attempt(t.Context(), http.MethodPost, "http://127.0.0.1:1/git-receive-pack", "", "", body, nil); !errors.Is(err, errBodyNotReplayable) {
		t.Fatalf("attempt returned %v, want errBodyNotReplayable", err)
	}
}

func TestHTTPPushSendsASmallPackWithAContentLength(t *testing.T) {
	recorder := &postRecorder{}
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writePushResult(w)
	})
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{}, strings.NewReader("PACKDATA")); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	posts := recorder.all()
	if len(posts) != 1 || posts[0].contentLength != int64(len(posts[0].body)) || len(posts[0].transferEncoding) != 0 || !bytes.HasSuffix(posts[0].body, []byte("PACKDATA")) {
		t.Fatalf("posts = %+v, want one request with a content length", posts)
	}
}

func TestHTTPPushStreamsALargePackChunkedWithoutCredentials(t *testing.T) {
	recorder := &postRecorder{}
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writePushResult(w)
	})
	swapPushSpool(t, func() (*os.File, error) {
		t.Fatalf("a push without a credential source was spooled")
		return nil, nil
	})
	pack := largePack(3 * minPostBuffer)
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{Config: smallPostBuffer(t)}, bytes.NewReader(pack)); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	posts := recorder.all()
	if len(posts) != 1 || !slices.Equal(posts[0].transferEncoding, []string{"chunked"}) || !bytes.HasSuffix(posts[0].body, pack) {
		t.Fatalf("got %d posts, transfer encoding %v; want one chunked request carrying the pack", len(posts), posts[0].transferEncoding)
	}
}

func TestHTTPPushReplaysASpooledPackAfterAnAuthenticationChallenge(t *testing.T) {
	recorder := &postRecorder{}
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writePushResult(w)
	})
	dir := t.TempDir()
	var spooled string
	swapPushSpool(t, func() (*os.File, error) {
		file, err := os.CreateTemp(dir, "push-*.pack")
		if err == nil {
			spooled = file.Name()
		}
		return file, err
	})
	pack := largePack(2 * minPostBuffer)
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("secret")}}}
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{Credentials: source, Config: smallPostBuffer(t)}, bytes.NewReader(pack)); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	posts := recorder.all()
	if len(posts) != 2 || !bytes.Equal(posts[0].body, posts[1].body) || !bytes.HasSuffix(posts[1].body, pack) {
		t.Fatalf("got %d posts, want the same pack sent twice", len(posts))
	}
	if _, err := os.Stat(spooled); spooled == "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("spool file %q still exists: %v", spooled, err)
	}
}

func TestHTTPPushFailsWhenThePackCannotBeSpooled(t *testing.T) {
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected push request")
	})
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("secret")}}}
	spoolErr := errors.New("disk full")
	swapPushSpool(t, func() (*os.File, error) { return nil, spoolErr })
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{Credentials: source, Config: smallPostBuffer(t)}, bytes.NewReader(largePack(2*minPostBuffer))); !errors.Is(err, spoolErr) {
		t.Fatalf("Push returned %v, want the spool error", err)
	}
	swapPushSpool(t, func() (*os.File, error) { return os.CreateTemp(t.TempDir(), "push-*.pack") })
	readErr := errors.New("pack writer failed")
	broken := io.MultiReader(bytes.NewReader(largePack(minPostBuffer+10)), iotest.ErrReader(readErr))
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{Credentials: source, Config: smallPostBuffer(t)}, broken); !errors.Is(err, readErr) {
		t.Fatalf("Push returned %v, want the pack read error", err)
	}
}

func TestHTTPPushCannotReplayAStreamedPackOnARedirect(t *testing.T) {
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasPrefix(r.URL.Path, "/moved") {
			writePushResult(w)
			return
		}
		http.Redirect(w, r, "/moved/git-receive-pack", http.StatusTemporaryRedirect)
	})
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{Config: smallPostBuffer(t)}, bytes.NewReader(largePack(2*minPostBuffer))); !errors.Is(err, errBodyNotReplayable) {
		t.Fatalf("Push returned %v, want errBodyNotReplayable", err)
	}
}

func manyWants() []hash.ObjectID {
	wants := make([]hash.ObjectID, 0, 40)
	for i := range 40 {
		wants = append(wants, idOf(byte(i+1)))
	}
	return wants
}

func uploadPackRecordingServer(t *testing.T, refuseGzip bool) (*httptest.Server, *postRecorder) {
	t.Helper()
	recorder := &postRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			advertiseV1Handler(w, r)
			return
		}
		rec := recorder.record(r)
		if rec.encoding == "gzip" && refuseGzip {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result; charset=utf-8")
		_, _ = w.Write(fetchV1ResponseBody("NAK", fakePack(3)))
	}))
	t.Cleanup(server.Close)
	return server, recorder
}

func fetchAndDrain(t *testing.T, session Session) {
	t.Helper()
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: manyWants()}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	_, _ = io.ReadAll(resp.Pack)
	_ = resp.Pack.Close()
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader returned error %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading the gzip body returned error %v", err)
	}
	return plain
}

func TestHTTPFetchV1GzipsALargeUploadPackRequest(t *testing.T) {
	server, recorder := uploadPackRecordingServer(t, false)
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	fetchAndDrain(t, session)
	posts := recorder.all()
	if len(posts) != 1 || posts[0].encoding != "gzip" {
		t.Fatalf("posts = %d, encoding %q; want one gzip request", len(posts), posts[0].encoding)
	}
	if plain := gunzip(t, posts[0].body); !strings.Contains(string(plain), "want "+idOf(40).String()) {
		t.Fatalf("decompressed body = %q, want every want line", plain)
	}
}

func TestHTTPFallsBackToPlainRequestsWhenTheServerRefusesGzip(t *testing.T) {
	server, recorder := uploadPackRecordingServer(t, true)
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	fetchAndDrain(t, session)
	fetchAndDrain(t, session)
	var encodings []string
	for _, post := range recorder.all() {
		encodings = append(encodings, post.encoding)
	}
	if !slices.Equal(encodings, []string{"gzip", "", ""}) {
		t.Fatalf("encodings = %q, want one refused gzip request and plain ones afterwards", encodings)
	}
}

func TestHTTPCompressesOnlyLargeProtocolV1UploadPackRequests(t *testing.T) {
	large := make([]byte, gzipRequestThreshold+1)
	tests := []struct {
		name    string
		session *httpSession
		body    []byte
		want    bool
	}{
		{"large v1 upload-pack", &httpSession{service: UploadPack, version: 1}, large, true},
		{"small v1 upload-pack", &httpSession{service: UploadPack, version: 1}, large[:gzipRequestThreshold], false},
		{"protocol v2", &httpSession{service: UploadPack, version: 2}, large, false},
		{"receive-pack", &httpSession{service: ReceivePack, version: 1}, large, false},
		{"server refused gzip", &httpSession{service: UploadPack, version: 1, plainRequests: true}, large, false},
	}
	for _, tt := range tests {
		if got := tt.session.compressesRequest(tt.body); got != tt.want {
			t.Errorf("%s: compressesRequest = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHTTPDiscoveryFailsWhenHeadersDoNotArriveInTime(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	swapDiscoveryHeaderTimeout(t, 50*time.Millisecond)
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{}); !errors.Is(err, ErrHeaderTimeout) {
		t.Fatalf("advertise returned %v, want ErrHeaderTimeout", err)
	}
}

func TestHTTPPushWaitsForSlowServerHooks(t *testing.T) {
	swapDiscoveryHeaderTimeout(t, 50*time.Millisecond)
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(200 * time.Millisecond)
		writePushResult(w)
	})
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{}, strings.NewReader("PACKDATA")); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
}

func setLowSpeedLimit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_HTTP_LOW_SPEED_LIMIT", "1000")
	t.Setenv("GIT_HTTP_LOW_SPEED_TIME", "1")
}

func TestHTTPAbortsADiscoveryThatStallsBelowTheLowSpeedLimit(t *testing.T) {
	setLowSpeedLimit(t)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{}); !errors.Is(err, ErrLowSpeed) {
		t.Fatalf("advertise returned %v, want ErrLowSpeed", err)
	}
}

func TestHTTPAbortsAPushWhoseResultNeverStarts(t *testing.T) {
	setLowSpeedLimit(t)
	release := make(chan struct{})
	server := pushServer(t, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() { close(release) })
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{}, strings.NewReader("PACKDATA")); !errors.Is(err, ErrLowSpeed) {
		t.Fatalf("Push returned %v, want ErrLowSpeed", err)
	}
}

func TestHasMediaTypeComparesOnlyTheMediaType(t *testing.T) {
	const want = "application/x-git-upload-pack-advertisement"
	tests := []struct {
		header string
		ok     bool
	}{
		{want, true},
		{want + "; charset=utf-8", true},
		{"Application/X-Git-Upload-Pack-Advertisement", true},
		{want + "; charset", true},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := hasMediaType(tt.header, want); got != tt.ok {
			t.Errorf("hasMediaType(%q) = %v, want %v", tt.header, got, tt.ok)
		}
	}
}

func TestHTTPAdvertiseAcceptsAContentTypeWithParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement; charset=utf-8")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	}))
	t.Cleanup(server.Close)
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
}

func TestOnlyUnsupportedChallengesSpotsDigestOnlyServers(t *testing.T) {
	tests := []struct {
		values []string
		want   bool
	}{
		{nil, false},
		{[]string{"Negotiate"}, false},
		{[]string{"Negotiate", "NTLM"}, false},
		{[]string{`Digest realm="git", nonce="abc"`}, true},
		{[]string{`Negotiate, Basic realm="git"`}, false},
		{[]string{`Bearer realm="git"`}, false},
		{[]string{`basic realm="a, b"`}, false},
	}
	for _, tt := range tests {
		h := http.Header{}
		for _, v := range tt.values {
			h.Add("WWW-Authenticate", v)
		}
		if got := onlyUnsupportedChallenges(h); got != tt.want {
			t.Errorf("onlyUnsupportedChallenges(%q) = %v, want %v", tt.values, got, tt.want)
		}
	}
}

func TestHTTPRefusesDigestOnlyServersWithoutAskingForCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("WWW-Authenticate", `Digest realm="git", nonce="abc"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	source := &fakeCredentialSource{}
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Credentials: source}); !errors.Is(err, ErrAuthSchemeUnsupported) {
		t.Fatalf("advertise returned %v, want ErrAuthSchemeUnsupported", err)
	}
	if len(source.retries) != 0 {
		t.Fatalf("the credential source was asked %d times, want never", len(source.retries))
	}
}

func TestHTTPReportsNegotiateAndNTLMOnlyServersWithoutCredentials(t *testing.T) {
	cases := map[string]struct {
		scheme string
		want   error
	}{
		"negotiate": {"Negotiate", ErrNoCredentials},
		"ntlm":      {"NTLM", ErrNoCredentials},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Add("WWW-Authenticate", tc.scheme)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)
			cfg := testGitConfig(t, "[http]\n\temptyAuth = false\n")
			if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg}); !errors.Is(err, tc.want) {
				t.Fatalf("advertise returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHTTPSendsExtraHeadersAndAGitUserAgent(t *testing.T) {
	var mu sync.Mutex
	var traces []string
	var agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		traces = r.Header.Values("X-Trace")
		agent = r.Header.Get("User-Agent")
		mu.Unlock()
		advertiseV1Handler(w, r)
	}))
	t.Cleanup(server.Close)
	cfg := testGitConfig(t, "[http]\n\textraHeader = X-Trace: one\n\textraHeader = X-Trace: two\n\textraHeader = NoColon\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(traces, []string{"one", "two"}) {
		t.Fatalf("X-Trace = %q, want one and two", traces)
	}
	if !strings.HasPrefix(agent, "git/") {
		t.Fatalf("User-Agent = %q, want it to start with git/", agent)
	}
}

func TestCreatePushSpoolMakesATemporaryFile(t *testing.T) {
	file, err := createPushSpool()
	if err != nil {
		t.Fatalf("createPushSpool returned error %v", err)
	}
	name := file.Name()
	_ = file.Close()
	if err := os.Remove(name); err != nil {
		t.Fatalf("removing the spool file returned error %v", err)
	}
}

func TestHTTPPushReplaysASmallPackOnATemporaryRedirect(t *testing.T) {
	recorder := &postRecorder{}
	server := pushServer(t, func(w http.ResponseWriter, r *http.Request) {
		rec := recorder.record(r)
		if !strings.HasPrefix(r.URL.Path, "/moved") {
			http.Redirect(w, r, "/moved/git-receive-pack", http.StatusTemporaryRedirect)
			return
		}
		if !bytes.HasSuffix(rec.body, []byte("PACKDATA")) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writePushResult(w)
	})
	if _, err := pushThrough(t, server.URL+"/repo.git", Options{}, strings.NewReader("PACKDATA")); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if posts := recorder.all(); len(posts) != 2 {
		t.Fatalf("got %d posts, want the pack sent to the redirect target too", len(posts))
	}
}

func TestAgentValueReplacesCharactersOutsideTheCapabilityAlphabet(t *testing.T) {
	got := agentValue(Options{UserAgent: "git/2.45.0 (Go.Git v1.2)\té"})
	if got != "git/2.45.0.(Go.Git.v1.2).." {
		t.Fatalf("agentValue = %q, want spaces and non-ASCII replaced by dots", got)
	}
}
